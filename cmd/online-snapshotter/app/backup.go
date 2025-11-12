package app

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	topolvmv1 "github.com/topolvm/topolvm/api/v1"
	"github.com/topolvm/topolvm/internal/snapshotengine/provider"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	masterURL      string
	kubeconfigPath string
)

type options struct {
	log    logr.Logger
	client client.Client

	lvName      string
	nodeName    string
	deviceClass string
	mountPath   string

	timeout            time.Duration
	targetedPVCRef     types.NamespacedName
	snapshotStorageRef types.NamespacedName
	snapshotStorage    *topolvmv1.OnlineSnapshotStorage
	logicalVol         *topolvmv1.LogicalVolume
}

var opt = new(options)

func newBackupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Take an online snapshot of a logical volume",
		Long: `The backup command performs an online snapshot of a logical volume.
		It backs up the mounted filesystem to a remote repository using Restic or Kopia.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			//cmd.SilenceUsage = true
			ctx := context.Background()
			opt.log = ctrl.Log.WithName("Backup")
			cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfigPath)
			if err != nil {
				return err
			}
			opt.client, err = NewRuntimeClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to get kubernetes client: %w", err)
			}

			opt.snapshotStorage, err = opt.getSnapshotStorage(ctx)
			if err != nil {
				return fmt.Errorf("failed to get OnlineSnapshotStorage: %w", err)
			}

			opt.logicalVol, err = opt.getLogicalVolume(ctx)
			if err != nil {
				return fmt.Errorf("failed to get LogicalVolume: %w", err)
			}

			if err = opt.updateLVSnapshotStatusRunning(ctx); err != nil {
				return fmt.Errorf("failed to update LV snapshot status: %w", err)
			}

			err = opt.runBackup(ctx)
			return err
		},
	}
	parseBackupFlags(cmd)
	return cmd
}

func parseBackupFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&masterURL, "master", masterURL, "The address of the Kubernetes API server (overrides any value in kubeconfig)")
	cmd.Flags().StringVar(&kubeconfigPath, "kubeconfig", kubeconfigPath, "Path to kubeconfig file with authorization information (the master location is set by the master flag)")

	cmd.Flags().StringVar(&opt.lvName, "lv-name", "", "Name of the logical volume to backup (required)")
	cmd.Flags().StringVar(&opt.nodeName, "node-name", "", "Node name where the logical volume resides (required)")
	cmd.Flags().StringVar(&opt.mountPath, "mount-path", "", "Mount path of the logical volume (required)")

	cmd.Flags().StringVar(&opt.targetedPVCRef.Namespace, "targeted-pvc-namespace", "", "Namespace of the targeted PVC")
	cmd.Flags().StringVar(&opt.targetedPVCRef.Name, "targeted-pvc-name", "", "Name of the targeted PVC")

	cmd.Flags().StringVar(&opt.snapshotStorageRef.Namespace, "snapshot-storage-namespace", "", "Namespace of the OnlineSnapshotStorage CR")
	cmd.Flags().StringVar(&opt.snapshotStorageRef.Name, "snapshot-storage-name", "", "Name of the OnlineSnapshotStorage CR")

	//_ = cmd.MarkFlagRequired("logical-volume")
	//_ = cmd.MarkFlagRequired("node-name")
	//_ = cmd.MarkFlagRequired("mount-path")
}

func (opt *options) runBackup(ctx context.Context) error {
	opt.log.Info("Starting online snapshot backup", "lvName", opt.lvName,
		"nodeName", opt.nodeName, "deviceClass", opt.deviceClass, "mountPath", opt.mountPath, "snapshotStorageRef", opt.snapshotStorageRef)

	pvider, err := provider.GetProvider(opt.client, opt.snapshotStorage)
	if err != nil {
		updateErr := opt.updateBackupStatusFailed(ctx, fmt.Sprintf("failed to get snapshot provider: %v", err))
		if updateErr != nil {
			opt.log.Error(updateErr, "failed to update status after provider error")
		}
		return fmt.Errorf("failed to get snapshot engine: %w", err)
	}

	backupParams := opt.getBackupParams()
	result, err := pvider.Backup(ctx, backupParams)
	if err != nil || (result != nil && result.Phase == provider.BackupPhaseFailed) {
		errorMsg := ""
		if err != nil {
			errorMsg = err.Error()
		} else if result != nil {
			errorMsg = result.ErrorMessage
		}
		updateErr := opt.updateBackupStatusFailed(ctx, errorMsg)
		if updateErr != nil {
			opt.log.Error(updateErr, "failed to update status after backup failure")
		}
		return fmt.Errorf("backup failed: %w", err)
	}

	if result != nil {
		updateErr := opt.updateBackupStatusSuccess(ctx, result)
		if updateErr != nil {
			opt.log.Error(updateErr, "failed to update status after successful backup")
			return updateErr
		}
		opt.log.Info("Backup completed successfully", "snapshotID", result.SnapshotID, "duration", result.Duration)
	}
	return nil
}

func (opt *options) getBackupParams() provider.BackupParam {
	params := provider.BackupParam{
		RepoParam: provider.RepoParam{
			Repository: filepath.Join(opt.targetedPVCRef.Namespace, opt.targetedPVCRef.Name),
			Hostname:   "filesystem",
		},
		BackupPaths: []string{opt.mountPath},
	}
	return params
}

func (opt *options) getSnapshotStorage(ctx context.Context) (*topolvmv1.OnlineSnapshotStorage, error) {
	snapshotStorage := &topolvmv1.OnlineSnapshotStorage{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      opt.snapshotStorageRef.Name,
			Namespace: opt.snapshotStorageRef.Namespace,
		},
	}
	err := opt.client.Get(ctx, client.ObjectKeyFromObject(snapshotStorage), snapshotStorage)
	return snapshotStorage, err

}

func (opt *options) getLogicalVolume(ctx context.Context) (*topolvmv1.LogicalVolume, error) {
	lv := &topolvmv1.LogicalVolume{
		ObjectMeta: ctrl.ObjectMeta{
			Name: opt.lvName,
		},
	}
	err := opt.client.Get(ctx, client.ObjectKeyFromObject(lv), lv)
	return lv, err
}

func (opt *options) updateBackupStatusSuccess(ctx context.Context, result *provider.BackupResult) error {
	// Refresh the LogicalVolume to get latest version
	lv, err := opt.getLogicalVolume(ctx)
	if err != nil {
		return fmt.Errorf("failed to get logical volume: %w", err)
	}

	// Initialize OnlineSnapshot status if it doesn't exist
	if lv.Status.OnlineSnapshot == nil {
		lv.Status.OnlineSnapshot = &topolvmv1.OnlineSnapshotStatus{}
	}

	now := metav1.Now()

	// Update status with backup result
	lv.Status.OnlineSnapshot.Phase = topolvmv1.SnapshotSucceeded
	lv.Status.OnlineSnapshot.SnapshotID = result.SnapshotID
	lv.Status.OnlineSnapshot.Message = fmt.Sprintf("Backup completed successfully in %s", result.Duration)
	lv.Status.OnlineSnapshot.CompletionTime = &now
	lv.Status.OnlineSnapshot.Version = result.Provider
	lv.Status.OnlineSnapshot.Error = nil // Clear any previous errors

	// Set progress information
	lv.Status.OnlineSnapshot.Progress = &topolvmv1.BackupProgress{
		TotalBytes: result.Size.TotalBytes,
		BytesDone:  result.Size.UploadedBytes,
	}

	// Calculate percentage if we have valid data
	if result.Size.TotalBytes > 0 {
		percentage := float64(result.Size.UploadedBytes) / float64(result.Size.TotalBytes) * 100
		lv.Status.OnlineSnapshot.Progress.Percentage = fmt.Sprintf("%.2f%%", percentage)
	} else {
		lv.Status.OnlineSnapshot.Progress.Percentage = "100%"
	}

	// Update the URL/Repository if available
	if result.Repository != "" {
		lv.Status.OnlineSnapshot.Repository = result.Repository
	}

	// Update the status
	if err := opt.client.Status().Update(ctx, lv); err != nil {
		opt.log.Error(err, "failed to update online snapshot status to Completed", "name", lv.Name)
		return err
	}

	opt.log.Info("updated online snapshot status to Completed",
		"name", lv.Name,
		"snapshotID", result.SnapshotID,
		"uploaded", result.Size.UploadedFormatted,
		"total", result.Size.TotalFormatted,
		"files", result.Files.Total,
		"duration", result.Duration)

	return nil
}

func (opt *options) updateLVSnapshotStatusRunning(ctx context.Context) error {
	now := metav1.Now()
	updateErr := opt.updateOnlineSnapshotStatus(ctx, topolvmv1.SnapshotRunning, "Snapshot execution in progress", nil, &now)
	if updateErr != nil {
		opt.log.Error(updateErr, "failed to set online snapshot status to Running", "name", opt.lvName)
		return updateErr
	}
	return nil
}

func (opt *options) updateBackupStatusFailed(ctx context.Context, errorMessage string) error {
	now := metav1.Now()
	snapshotErr := &topolvmv1.OnlineSnapshotError{
		Code:    "BackupFailed",
		Message: errorMessage,
	}
	return opt.updateOnlineSnapshotStatus(ctx, topolvmv1.SnapshotFailed,
		fmt.Sprintf("Backup failed: %s", errorMessage),
		snapshotErr, &now)
}

func (opt *options) updateOnlineSnapshotStatus(ctx context.Context,
	phase topolvmv1.SnapshotPhase, message string, snapshotErr *topolvmv1.OnlineSnapshotError, completionTime *metav1.Time) error {
	// Initialize OnlineSnapshot status if it doesn't exist
	if opt.logicalVol.Status.OnlineSnapshot == nil {
		opt.logicalVol.Status.OnlineSnapshot = &topolvmv1.OnlineSnapshotStatus{}
	}

	// Update phase and message
	opt.logicalVol.Status.OnlineSnapshot.Phase = phase
	opt.logicalVol.Status.OnlineSnapshot.Message = message

	// Set error if provided
	if snapshotErr != nil {
		opt.logicalVol.Status.OnlineSnapshot.Error = snapshotErr
	}

	// Set completion time if provided
	if completionTime != nil {
		opt.logicalVol.Status.OnlineSnapshot.CompletionTime = completionTime
	}

	// Update the status
	if err := opt.client.Status().Update(ctx, opt.logicalVol); err != nil {
		return fmt.Errorf("failed to update online snapshot status: %w", err)
	}
	return nil
}
