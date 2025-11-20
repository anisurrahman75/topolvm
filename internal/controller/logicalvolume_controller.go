package controller

//
//import (
//	"context"
//	"fmt"
//	"strings"
//	"time"
//
//	"github.com/go-logr/logr"
//	snapshot_api "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
//	"github.com/topolvm/topolvm"
//	topolvmlegacyv1 "github.com/topolvm/topolvm/api/legacy/v1"
//	topolvmv1 "github.com/topolvm/topolvm/api/v1"
//	"github.com/topolvm/topolvm/internal/executor"
//	"github.com/topolvm/topolvm/internal/mounter"
//	"github.com/topolvm/topolvm/pkg/lvmd/proto"
//	"google.golang.org/grpc/codes"
//	"google.golang.org/grpc/status"
//	corev1 "k8s.io/api/core/v1"
//	apierrs "k8s.io/apimachinery/pkg/api/errors"
//	"k8s.io/apimachinery/pkg/api/resource"
//	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
//	"k8s.io/apimachinery/pkg/types"
//	ctrl "sigs.k8s.io/controller-runtime"
//	"sigs.k8s.io/controller-runtime/pkg/client"
//	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
//	"sigs.k8s.io/controller-runtime/pkg/event"
//	crlog "sigs.k8s.io/controller-runtime/pkg/log"
//)
//
//// LogicalVolumeReconciler reconciles a LogicalVolume object
//type LogicalVolumeReconciler struct {
//	client    client.Client
//	nodeName  string
//	vgService proto.VGServiceClient
//	lvService proto.LVServiceClient
//	lvMount   *mounter.LVMount
//	executor  executor.Executor
//}
//
////+kubebuilder:rbac:groups=topolvm.io,resources=logicalvolumes,verbs=get;list;watch;update;patch
////+kubebuilder:rbac:groups=topolvm.io,resources=logicalvolumes/status,verbs=get;update;patch
//
//func NewLogicalVolumeReconcilerWithServices(client client.Client, nodeName string, vgService proto.VGServiceClient, lvService proto.LVServiceClient) *LogicalVolumeReconciler {
//	return &LogicalVolumeReconciler{
//		client:    client,
//		nodeName:  nodeName,
//		vgService: vgService,
//		lvService: lvService,
//		lvMount:   mounter.NewLVMount(client, vgService, lvService),
//	}
//}
//
//// Reconcile creates/deletes LVM logical volume for a LogicalVolume.
//func (r *LogicalVolumeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
//	log := crlog.FromContext(ctx)
//
//	lv := new(topolvmv1.LogicalVolume)
//	if err := r.client.Get(ctx, req.NamespacedName, lv); err != nil {
//		if !apierrs.IsNotFound(err) {
//			log.Error(err, "unable to fetch LogicalVolume")
//			return ctrl.Result{}, err
//		}
//		return ctrl.Result{}, nil
//	}
//	if lv.Spec.NodeName != r.nodeName {
//		log.Info("unfiltered logical value", "nodeName", lv.Spec.NodeName)
//		return ctrl.Result{}, nil
//	}
//
//	if lv.Annotations != nil {
//		_, pendingDeletion := lv.Annotations[topolvm.GetLVPendingDeletionKey()]
//		if pendingDeletion {
//			if controllerutil.ContainsFinalizer(lv, topolvm.GetLogicalVolumeFinalizer()) {
//				log.Error(nil, "logical volume was pending deletion but still has finalizer", "name", lv.Name)
//			} else {
//				log.Info("skipping finalizer for logical volume due to its pending deletion", "name", lv.Name)
//			}
//			return ctrl.Result{}, nil
//		}
//	}
//
//	if lv.DeletionTimestamp == nil {
//		if !controllerutil.ContainsFinalizer(lv, topolvm.GetLogicalVolumeFinalizer()) {
//			lv2 := lv.DeepCopy()
//			controllerutil.AddFinalizer(lv2, topolvm.GetLogicalVolumeFinalizer())
//			patch := client.MergeFrom(lv)
//			if err := r.client.Patch(ctx, lv2, patch); err != nil {
//				log.Error(err, "failed to add finalizer", "name", lv.Name)
//				return ctrl.Result{}, err
//			}
//			return ctrl.Result{RequeueAfter: requeueIntervalForSimpleUpdate}, nil
//		}
//
//		if !containsKeyAndValue(lv.Labels, topolvm.CreatedbyLabelKey, topolvm.CreatedbyLabelValue) {
//			lv2 := lv.DeepCopy()
//			if lv2.Labels == nil {
//				lv2.Labels = map[string]string{}
//			}
//			lv2.Labels[topolvm.CreatedbyLabelKey] = topolvm.CreatedbyLabelValue
//			patch := client.MergeFrom(lv)
//			if err := r.client.Patch(ctx, lv2, patch); err != nil {
//				log.Error(err, "failed to add label", "name", lv.Name)
//				return ctrl.Result{}, err
//			}
//			return ctrl.Result{RequeueAfter: requeueIntervalForSimpleUpdate}, nil
//		}
//
//		snapshotLV, err := r.getSnapshotLV(ctx, lv)
//		if err != nil {
//			log.Error(err, "failed to get snapshot LV", "name", lv.Name)
//			return ctrl.Result{}, err
//		}
//
//		var restoreFmSnap bool
//		var vsClass *snapshot_api.VolumeSnapshotClass
//		var vsContent *snapshot_api.VolumeSnapshotContent
//
//		if snapshotLV != nil {
//			log.Info("snapshot LV found", "name", snapshotLV.Name)
//			vsContent, vsClass, err = r.getVSContentAndClassIfExist(ctx, snapshotLV)
//			if err != nil {
//				log.Error(err, "failed to get VolumeSnapshotContent and VolumeSnapshotClass", "name", lv.Name)
//				return ctrl.Result{}, err
//			}
//			restoreFmSnap, err = r.shouldRestoreFromSnapshot(snapshotLV, vsClass)
//			if err != nil {
//				log.Error(err, "failed to check whether to restore from snapshot", "name", lv.Name)
//				return ctrl.Result{}, err
//			}
//		}
//
//		if lv.Status.VolumeID == "" {
//			err := r.createLV(ctx, log, lv, restoreFmSnap)
//			if err != nil {
//				log.Error(err, "failed to create LV", "name", lv.Name)
//			}
//			return ctrl.Result{}, err
//		}
//
//		if shouldExpandPV(lv) {
//			if err := r.expandLV(ctx, log, lv); err != nil {
//				log.Error(err, "failed to expand LV", "name", lv.Name)
//				return ctrl.Result{}, err
//			}
//		}
//
//		if restoreFmSnap {
//			exist, err := r.ensurePVExist(lv)
//			if err != nil {
//				log.Error(err, "failed to check whether PV exists", "name", lv.Name)
//				return ctrl.Result{}, err
//			}
//			if !exist {
//				log.Info("PV doesn't exist yet, waiting for it to be created", "name", lv.Name)
//				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
//			}
//			err = r.restoreFromSnapshot(ctx, log, lv, snapshotLV, vsClass)
//			if err != nil {
//				return ctrl.Result{}, err
//			}
//
//			if lv.Status.Snapshot != nil && lv.Status.Snapshot.Phase == topolvmv1.OperationPhaseSucceeded {
//				log.Info("snapshot restore completed successfully", "name", lv.Name)
//				if err := r.lvMount.Unmount(ctx, lv); err != nil {
//					log.Error(err, "failed to unmount LV", "name", lv.Name)
//					return ctrl.Result{}, err
//				}
//				log.Info("successfully unmounted LV", "name", lv.Name, "uid", lv.UID)
//			}
//		}
//
//		vsContent, vsClass, err = r.getVSContentAndClassIfExist(ctx, lv)
//		if err != nil {
//			log.Error(err, "failed to get VolumeSnapshotContent and VolumeSnapshotClass", "name", lv.Name)
//			return ctrl.Result{}, err
//		}
//		yes, err := r.shouldTakeSnapshot(vsClass)
//		if err != nil {
//			log.Error(err, "failed to check whether to take snapshot", "name", lv.Name)
//			return ctrl.Result{}, err
//		}
//		if yes {
//			err := r.takeSnapshot(ctx, log, lv, vsContent, vsClass)
//			if err != nil {
//				return ctrl.Result{}, err
//			}
//		}
//
//		return ctrl.Result{}, nil
//	}
//
//	// finalization
//	if !controllerutil.ContainsFinalizer(lv, topolvm.GetLogicalVolumeFinalizer()) {
//		// Our finalizer has finished, so the reconciler can do nothing.
//		return ctrl.Result{}, nil
//	}
//
//	log.Info("start finalizing LogicalVolume", "name", lv.Name)
//	err := r.removeLVIfExists(ctx, log, lv)
//	if err != nil {
//		return ctrl.Result{}, err
//	}
//
//	lv2 := lv.DeepCopy()
//	controllerutil.RemoveFinalizer(lv2, topolvm.GetLogicalVolumeFinalizer())
//	patch := client.MergeFrom(lv)
//	if err := r.client.Patch(ctx, lv2, patch); err != nil {
//		log.Error(err, "failed to remove finalizer", "name", lv.Name)
//		return ctrl.Result{}, err
//	}
//	return ctrl.Result{}, nil
//}
//
//func (r *LogicalVolumeReconciler) ensurePVExist(lv *topolvmv1.LogicalVolume) (bool, error) {
//	if lv.Spec.Name == "" {
//		return false, nil
//	}
//	pv := &corev1.PersistentVolume{
//		ObjectMeta: metav1.ObjectMeta{
//			Name: lv.Spec.Name,
//		},
//	}
//	err := r.client.Get(context.Background(), client.ObjectKeyFromObject(pv), pv)
//	if err != nil {
//		return false, client.IgnoreNotFound(err)
//	}
//	return true, nil
//}
//
//func (r *LogicalVolumeReconciler) getVSClassFromContent(ctx context.Context, content *snapshot_api.VolumeSnapshotContent) (*snapshot_api.VolumeSnapshotClass, error) {
//	vsClass := &snapshot_api.VolumeSnapshotClass{
//		ObjectMeta: metav1.ObjectMeta{
//			Name: *content.Spec.VolumeSnapshotClassName,
//		},
//	}
//	if err := r.client.Get(ctx, client.ObjectKeyFromObject(vsClass), vsClass); err != nil {
//		return nil, fmt.Errorf("unable to fetch VolumeSnapshotClass %s: %v", *content.Spec.VolumeSnapshotClassName, err)
//	}
//	return vsClass, nil
//}
//
//func (r *LogicalVolumeReconciler) getVSContentIfExist(ctx context.Context, lv *topolvmv1.LogicalVolume) (*snapshot_api.VolumeSnapshotContent, error) {
//	var content *snapshot_api.VolumeSnapshotContent
//	var err error
//	if lv.Spec.Source != "" {
//		content, err = r.getSnapshotContent(ctx, lv)
//	}
//	return content, err
//}
//
//func (r *LogicalVolumeReconciler) getVSContentAndClassIfExist(ctx context.Context, lv *topolvmv1.LogicalVolume) (*snapshot_api.VolumeSnapshotContent, *snapshot_api.VolumeSnapshotClass, error) {
//	content, err := r.getVSContentIfExist(ctx, lv)
//	if err != nil {
//		return nil, nil, client.IgnoreNotFound(err)
//	}
//	if content == nil {
//		return nil, nil, nil
//	}
//	vsClass, err := r.getVSClassFromContent(ctx, content)
//	return content, vsClass, err
//}
//
//func (r *LogicalVolumeReconciler) shouldTakeSnapshot(vsClass *snapshot_api.VolumeSnapshotClass) (bool, error) {
//	var takeSnapshot bool
//	if vsClass == nil {
//		return takeSnapshot, nil
//	}
//	onlineSnapshotParam, ok := vsClass.Parameters[SnapshotMode]
//	if ok && onlineSnapshotParam == SnapshotModeOnline {
//		takeSnapshot = true
//	}
//	return takeSnapshot, nil
//}
//
//func (r *LogicalVolumeReconciler) getSnapshotLV(ctx context.Context, lv *topolvmv1.LogicalVolume) (*topolvmv1.LogicalVolume, error) {
//	var snapshotLv *topolvmv1.LogicalVolume
//	if lv.Spec.Source == "" {
//		return snapshotLv, nil
//	}
//	snapshotLv = &topolvmv1.LogicalVolume{
//		ObjectMeta: metav1.ObjectMeta{
//			Name: lv.Spec.Source,
//		},
//	}
//	// Ignore errors if the snapshot LV doesn't exist For VolumeSnapshot LV it doesn't exist. It'll only exist while restoring from any VS.
//	if err := r.client.Get(ctx, client.ObjectKeyFromObject(snapshotLv), snapshotLv); err != nil {
//		return snapshotLv, client.IgnoreNotFound(err)
//	}
//	return snapshotLv, nil
//}
//
//func (r *LogicalVolumeReconciler) shouldRestoreFromSnapshot(snapshotLv *topolvmv1.LogicalVolume, vsClass *snapshot_api.VolumeSnapshotClass) (bool, error) {
//	var takeRestore bool
//	if snapshotLv.Status.Snapshot == nil ||
//		(snapshotLv.Status.Snapshot != nil && snapshotLv.Status.Snapshot.Phase == topolvmv1.OperationPhaseSucceeded) {
//		if vsClass == nil {
//			return takeRestore, nil
//		}
//		onlineSnapshotParam, ok := vsClass.Parameters[SnapshotMode]
//		if ok && onlineSnapshotParam == SnapshotModeOnline {
//			takeRestore = true
//		}
//	}
//	return takeRestore, nil
//}
//
//func (r *LogicalVolumeReconciler) takeSnapshot(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume,
//	vsContent *snapshot_api.VolumeSnapshotContent, vsClass *snapshot_api.VolumeSnapshotClass) error {
//
//	//// Refresh the LV to get the latest status
//	//if err := r.client.Get(ctx, client.ObjectKeyFromObject(lv), lv); err != nil {
//	//	log.Error(err, "failed to get latest LogicalVolume", "name", lv.Name)
//	//	return err
//	//}
//
//	if lv.Status.Snapshot == nil || lv.Status.Snapshot.Phase == "" {
//		msg := fmt.Sprintf("Initializing online snapshot")
//		if err := r.updateSnapshotStatus(ctx, lv, topolvmv1.OperationBackup, topolvmv1.OperationPhasePending, msg, nil); err != nil {
//			log.Error(err, "failed to set online snapshot status to Pending", "name", lv.Name)
//			return err
//		}
//		log.Info("updated online snapshot status", "name", lv.Name, "phase", topolvmv1.OperationPhasePending, "message", msg)
//	}
//
//	if lv.Status.Snapshot.Phase == topolvmv1.OperationPhaseSucceeded ||
//		lv.Status.Snapshot.Phase == topolvmv1.OperationPhaseFailed {
//		log.Info("online snapshot already processed", "name", lv.Name, "phase", lv.Status.Snapshot.Phase)
//		return nil
//	}
//
//	if lv.Status.Snapshot.Phase == topolvmv1.OperationPhaseRunning {
//		log.Info("online snapshot is currently running", "name", lv.Name)
//		return nil
//	}
//
//	resp, err := r.lvMount.Mount(ctx, lv, []string{"ro", "norecovery"})
//	if err != nil {
//		log.Error(err, "failed to mount LV", "name", lv.Name)
//		// Set the failed phase with the mount error
//		mountErr := &topolvmv1.SnapshotError{
//			Code:    "VolumeMountFailed",
//			Message: fmt.Sprintf("failed to mount logical volume: %v", err),
//		}
//		msg := fmt.Sprintf("Failed to mount logical volume: %v", err)
//		if updateErr := r.updateSnapshotStatus(ctx, lv, topolvmv1.OperationBackup, topolvmv1.OperationPhaseFailed, msg, mountErr); updateErr != nil {
//			log.Error(updateErr, "failed to set online snapshot status to Failed after mount error", "name", lv.Name)
//		}
//		log.Info("updated online snapshot status", "name", lv.Name, "phase", topolvmv1.OperationPhaseFailed, "message", msg)
//		return err
//	}
//
//	// Execute the snapshot
//	r.executor = executor.NewSnapshotExecutor(r.client, lv, resp, vsContent, vsClass)
//	if execErr := r.executor.Execute(); execErr != nil {
//		log.Error(execErr, "failed to execute snapshot", "name", lv.Name)
//		// Set the failed phase with the execution error
//		executeErr := &topolvmv1.SnapshotError{
//			Code:    "SnapshotExecutionFailed",
//			Message: fmt.Sprintf("failed to execute snapshot: %v", execErr),
//		}
//		msg := fmt.Sprintf("Failed to execute snapshot: %v", execErr)
//		if updateErr := r.updateSnapshotStatus(ctx, lv, topolvmv1.OperationBackup, topolvmv1.OperationPhaseFailed, msg, executeErr); updateErr != nil {
//			log.Error(updateErr, "failed to set online snapshot status to Failed after execution error", "name", lv.Name)
//		}
//		log.Info("updated online snapshot status", "name", lv.Name, "phase", topolvmv1.OperationPhaseFailed, "message", msg)
//		return execErr
//	}
//	return nil
//}
//
//func (r *LogicalVolumeReconciler) restoreFromSnapshot(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume,
//	snapshotLv *topolvmv1.LogicalVolume, vsClass *snapshot_api.VolumeSnapshotClass) error {
//	//// Refresh the LV to get the latest status
//	//if err := r.client.Get(ctx, client.ObjectKeyFromObject(lv), lv); err != nil {
//	//	log.Error(err, "failed to get latest LogicalVolume", "name", lv.Name)
//	//	return err
//	//}
//	if lv.Status.Snapshot != nil && (lv.Status.Snapshot.Phase == topolvmv1.OperationPhaseSucceeded ||
//		lv.Status.Snapshot.Phase == topolvmv1.OperationPhaseFailed) {
//		log.Info("online restore already processed", "name", lv.Name, "phase", lv.Status.Snapshot.Phase)
//		return nil
//	}
//
//	if lv.Status.Snapshot == nil || lv.Status.Snapshot.Phase == "" {
//		msg := fmt.Sprintf("Initializing online restore")
//		if err := r.updateSnapshotStatus(ctx, lv, topolvmv1.OperationRestore, topolvmv1.OperationPhasePending, msg, nil); err != nil {
//			log.Error(err, "failed to set online restore status to Pending", "name", lv.Name)
//			return err
//		}
//		log.Info("updated online restore status", "name", lv.Name, "phase", topolvmv1.OperationPhasePending, "message", msg)
//	}
//
//	if lv.Status.Snapshot.Phase == topolvmv1.OperationPhaseRunning {
//		log.Info("online restore is currently running", "name", lv.Name)
//		return nil
//	}
//
//	// Use bind mount for restore since the LV is already mounted by the pod
//	resp, err := r.lvMount.Mount(ctx, lv, []string{})
//	if err != nil {
//		log.Error(err, "failed to bind mount LV", "name", lv.Name)
//		// Set the failed phase with the mount error
//		mountErr := &topolvmv1.SnapshotError{
//			Code:    "VolumeBindMountFailed",
//			Message: fmt.Sprintf("failed to bind mount logical volume: %v", err),
//		}
//		msg := fmt.Sprintf("Failed to bind mount logical volume: %v", err)
//		if updateErr := r.updateSnapshotStatus(ctx, lv, topolvmv1.OperationRestore, topolvmv1.OperationPhaseFailed, msg, mountErr); updateErr != nil {
//			log.Error(updateErr, "failed to set snapshot status to Failed after bind mount error", "name", lv.Name)
//		}
//		log.Info("updated snapshot status", "name", lv.Name, "phase", topolvmv1.OperationPhaseFailed, "message", msg)
//		return err
//	}
//
//	// Execute the snapshot
//	r.executor = executor.NewRestoreExecutor(r.client, lv, snapshotLv, resp, vsClass)
//	if execErr := r.executor.Execute(); execErr != nil {
//		log.Error(execErr, "failed to execute restore", "name", lv.Name)
//		// Set the failed phase with the execution error
//		executeErr := &topolvmv1.SnapshotError{
//			Code:    "RestoreExecutionFailed",
//			Message: fmt.Sprintf("failed to execute restore: %v", execErr),
//		}
//		msg := fmt.Sprintf("Failed to execute restore: %v", execErr)
//		if updateErr := r.updateSnapshotStatus(ctx, lv, topolvmv1.OperationBackup, topolvmv1.OperationPhaseFailed, msg, executeErr); updateErr != nil {
//			log.Error(updateErr, "failed to set snapshot status to Failed after execution error", "name", lv.Name)
//		}
//		log.Info("updated snapshot status", "name", lv.Name, "phase", topolvmv1.OperationPhaseFailed, "message", msg)
//		return execErr
//	}
//	return nil
//}
//
//func (r *LogicalVolumeReconciler) getSnapshotContent(ctx context.Context, lv *topolvmv1.LogicalVolume) (*snapshot_api.VolumeSnapshotContent, error) {
//	content := &snapshot_api.VolumeSnapshotContent{
//		ObjectMeta: metav1.ObjectMeta{
//			// https://github.com/kubernetes-csi/external-snapshotter/blob/master/pkg/utils/util.go#L283
//			Name: fmt.Sprintf("snapcontent%s", strings.TrimPrefix(lv.Spec.Name, "snapshot")),
//		},
//	}
//	if err := r.client.Get(ctx, client.ObjectKeyFromObject(content), content); err != nil {
//		return nil, err
//	}
//	return content, nil
//}
//
//func (r *LogicalVolumeReconciler) getVSClass(ctx context.Context, content *snapshot_api.VolumeSnapshotContent) (*snapshot_api.VolumeSnapshotClass, error) {
//	if content == nil {
//		return nil, nil
//	}
//	vsClass := &snapshot_api.VolumeSnapshotClass{
//		ObjectMeta: metav1.ObjectMeta{
//			Name: *content.Spec.VolumeSnapshotClassName,
//		},
//	}
//	if err := r.client.Get(ctx, client.ObjectKeyFromObject(vsClass), vsClass); err != nil {
//		return nil, fmt.Errorf("unable to fetch VolumeSnapshotClass %s: %v", *content.Spec.VolumeSnapshotClassName, err)
//	}
//	return vsClass, nil
//}
//
//func (r *LogicalVolumeReconciler) updateSnapshotStatus(ctx context.Context, lv *topolvmv1.LogicalVolume,
//	opType topolvmv1.OperationType, phase topolvmv1.OperationPhase, message string, snapshotErr *topolvmv1.SnapshotError) error {
//	// Refresh the LogicalVolume object to get the latest version
//	freshLV := &topolvmv1.LogicalVolume{}
//	if err := r.client.Get(ctx, client.ObjectKeyFromObject(lv), freshLV); err != nil {
//		return fmt.Errorf("failed to get latest LogicalVolume: %v", err)
//	}
//
//	// Initialize OnlineSnapshot status if it doesn't exist
//	if freshLV.Status.Snapshot == nil {
//		freshLV.Status.Snapshot = &topolvmv1.SnapshotStatus{}
//		freshLV.Status.Snapshot.StartTime = metav1.Now()
//	}
//
//	// Update phase and message
//	freshLV.Status.Snapshot.Operation = opType
//	freshLV.Status.Snapshot.Phase = phase
//	freshLV.Status.Snapshot.Message = message
//
//	// Set error if provided
//	if snapshotErr != nil {
//		freshLV.Status.Snapshot.Error = snapshotErr
//	}
//
//	// Update the status
//	if err := r.client.Status().Update(ctx, freshLV); err != nil {
//		return err
//	}
//
//	// Update the original lv object with the latest status to keep it in sync
//	lv.Status = freshLV.Status
//	lv.ObjectMeta.ResourceVersion = freshLV.ObjectMeta.ResourceVersion
//
//	return nil
//}
//
//func shouldExpandPV(lv *topolvmv1.LogicalVolume) bool {
//	if lv.Status.CurrentSize == nil {
//		// topolvm-node may be crashed before setting Status.CurrentSize.
//		// Since the actual volume size is unknown,
//		// we need to do resizing to set Status.CurrentSize to the same value as Spec.Size.
//		return true
//	}
//	if lv.Spec.Size.Cmp(*lv.Status.CurrentSize) > 0 {
//		return true
//	}
//	return false
//}
//
//// SetupWithManager sets up the controller with the Manager.
//func (r *LogicalVolumeReconciler) SetupWithManager(mgr ctrl.Manager) error {
//	builder := ctrl.NewControllerManagedBy(mgr)
//	if topolvm.UseLegacy() {
//		builder = builder.For(&topolvmlegacyv1.LogicalVolume{})
//	} else {
//		builder = builder.For(&topolvmv1.LogicalVolume{})
//	}
//	return builder.WithEventFilter(&logicalVolumeFilter{r.nodeName}).Complete(r)
//}
//
//func (r *LogicalVolumeReconciler) removeLVIfExists(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume) error {
//	// First, unmount the LV if it's mounted (e.g., for online snapshots)
//	if err := r.lvMount.Unmount(ctx, lv); err != nil {
//		log.Error(err, "failed to unmount LV before removal", "name", lv.Name, "uid", lv.UID)
//		// Continue with removal even if unmount fails, as the LV might not be mounted
//		// or the mount might have been manually removed
//	} else {
//		log.Info("successfully unmounted LV", "name", lv.Name, "uid", lv.UID)
//	}
//
//	// Finalizer's process ( RemoveLV then removeString ) is not atomic,
//	// so checking existence of LV to ensure its idempotence
//	_, err := r.lvService.RemoveLV(ctx, &proto.RemoveLVRequest{Name: string(lv.UID), DeviceClass: lv.Spec.DeviceClass})
//	if status.Code(err) == codes.NotFound {
//		log.Info("LV already removed", "name", lv.Name, "uid", lv.UID)
//		return nil
//	}
//	if err != nil {
//		log.Error(err, "failed to remove LV", "name", lv.Name, "uid", lv.UID)
//		return err
//	}
//	log.Info("removed LV", "name", lv.Name, "uid", lv.UID)
//	return nil
//}
//
//func (r *LogicalVolumeReconciler) volumeExists(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume) (bool, error) {
//	respList, err := r.vgService.GetLVList(ctx, &proto.GetLVListRequest{DeviceClass: lv.Spec.DeviceClass})
//	if err != nil {
//		log.Error(err, "failed to get list of LV")
//		return false, err
//	}
//
//	for _, v := range respList.Volumes {
//		if v.Name != string(lv.UID) {
//			continue
//		}
//		return true, nil
//	}
//	return false, nil
//}
//
//func (r *LogicalVolumeReconciler) createLV(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume, restoreFrSnapshot bool) error {
//	// When lv.Status.Code is not codes.OK (== 0), CreateLV has already failed.
//	// LogicalVolume CRD will be deleted soon by the controller.
//	if lv.Status.Code != codes.OK {
//		return nil
//	}
//
//	reqBytes := lv.Spec.Size.Value()
//
//	err := func() error {
//		// In case the controller crashed just after LVM LV creation, LV may already exist.
//		found, err := r.volumeExists(ctx, log, lv)
//		if err != nil {
//			lv.Status.Code = codes.Internal
//			lv.Status.Message = "failed to check volume existence"
//			return err
//		}
//		if found {
//			log.Info("set volumeID to existing LogicalVolume", "name", lv.Name, "uid", lv.UID, "status.volumeID", lv.Status.VolumeID)
//			// Don't set CurrentSize here because the Spec.Size field may be updated after the LVM LV is created.
//			lv.Status.VolumeID = string(lv.UID)
//			lv.Status.Code = codes.OK
//			lv.Status.Message = ""
//			return nil
//		}
//
//		var volume *proto.LogicalVolume
//
//		// Create a snapshot LV
//		if lv.Spec.Source != "" && !restoreFrSnapshot {
//			// accessType should be either "readonly" or "readwrite".
//			if lv.Spec.AccessType != "ro" && lv.Spec.AccessType != "rw" {
//				return fmt.Errorf("invalid access type for source volume: %s", lv.Spec.AccessType)
//			}
//			sourcelv := new(topolvmv1.LogicalVolume)
//			if err := r.client.Get(ctx, types.NamespacedName{Namespace: lv.Namespace, Name: lv.Spec.Source}, sourcelv); err != nil {
//				log.Error(err, "unable to fetch source LogicalVolume", "name", lv.Name)
//				return err
//			}
//			sourceVolID := sourcelv.Status.VolumeID
//			currentSize := sourcelv.Status.CurrentSize.Value()
//			if reqBytes < currentSize {
//				return fmt.Errorf("cannot create new LV, requested size %d is smaller than source LV size %d", reqBytes, currentSize)
//			}
//
//			// Create a snapshot lv
//			resp, err := r.lvService.CreateLVSnapshot(ctx, &proto.CreateLVSnapshotRequest{
//				Name:         string(lv.UID),
//				DeviceClass:  lv.Spec.DeviceClass,
//				SourceVolume: sourceVolID,
//				SizeBytes:    reqBytes,
//				AccessType:   lv.Spec.AccessType,
//			})
//			if err != nil {
//				code, message := extractFromError(err)
//				log.Error(err, message)
//				lv.Status.Code = code
//				lv.Status.Message = message
//				return err
//			}
//			volume = resp.Snapshot
//		} else {
//			// Create a regular lv
//			resp, err := r.lvService.CreateLV(ctx, &proto.CreateLVRequest{
//				Name:                string(lv.UID),
//				DeviceClass:         lv.Spec.DeviceClass,
//				LvcreateOptionClass: lv.Spec.LvcreateOptionClass,
//				SizeBytes:           reqBytes,
//			})
//			if err != nil {
//				code, message := extractFromError(err)
//				log.Error(err, message)
//				lv.Status.Code = code
//				lv.Status.Message = message
//				return err
//			}
//			volume = resp.Volume
//		}
//
//		lv.Status.VolumeID = volume.Name
//		lv.Status.CurrentSize = resource.NewQuantity(volume.SizeBytes, resource.BinarySI)
//		lv.Status.Code = codes.OK
//		lv.Status.Message = ""
//		return nil
//	}()
//
//	if err != nil {
//		if err2 := r.client.Status().Update(ctx, lv); err2 != nil {
//			// err2 is logged but not returned because err is more important
//			log.Error(err2, "failed to update status", "name", lv.Name, "uid", lv.UID)
//		}
//		return err
//	}
//
//	if err := r.client.Status().Update(ctx, lv); err != nil {
//		log.Error(err, "failed to update status", "name", lv.Name, "uid", lv.UID)
//		return err
//	}
//
//	log.Info("created new LV", "name", lv.Name, "uid", lv.UID, "status.volumeID", lv.Status.VolumeID)
//
//	return nil
//}
//
//func (r *LogicalVolumeReconciler) expandLV(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume) error {
//	// We denote unknown size as -1.
//	var origBytes int64 = -1
//	switch {
//	case lv.Status.CurrentSize == nil:
//		// topolvm-node may be crashed before setting Status.CurrentSize.
//		// Since the actual volume size is unknown,
//		// we need to do resizing to set Status.CurrentSize to the same value as Spec.Size.
//	case lv.Spec.Size.Cmp(*lv.Status.CurrentSize) <= 0:
//		return nil
//	default:
//		origBytes = (*lv.Status.CurrentSize).Value()
//	}
//
//	reqBytes := lv.Spec.Size.Value()
//
//	err := func() error {
//		resp, err := r.lvService.ResizeLV(ctx, &proto.ResizeLVRequest{
//			Name:        string(lv.UID),
//			SizeBytes:   reqBytes,
//			DeviceClass: lv.Spec.DeviceClass,
//		})
//		if err != nil {
//			code, message := extractFromError(err)
//			log.Error(err, message)
//			lv.Status.Code = code
//			lv.Status.Message = message
//			return err
//		}
//
//		lv.Status.CurrentSize = resource.NewQuantity(resp.SizeBytes, resource.BinarySI)
//		lv.Status.Code = codes.OK
//		lv.Status.Message = ""
//		return nil
//	}()
//
//	if err != nil {
//		if err2 := r.client.Status().Update(ctx, lv); err2 != nil {
//			// err2 is logged but not returned because err is more important
//			log.Error(err2, "failed to update status", "name", lv.Name, "uid", lv.UID)
//		}
//		return err
//	}
//
//	if err := r.client.Status().Update(ctx, lv); err != nil {
//		log.Error(err, "failed to update status", "name", lv.Name, "uid", lv.UID)
//		return err
//	}
//
//	log.Info("expanded LV", "name", lv.Name, "uid", lv.UID, "status.volumeID", lv.Status.VolumeID,
//		"original status.currentSize", origBytes, "status.currentSize", lv.Status.CurrentSize, "spec.size", reqBytes)
//	return nil
//}
//
//type logicalVolumeFilter struct {
//	nodeName string
//}
//
//func (f logicalVolumeFilter) filter(obj client.Object) bool {
//	var name string
//	if topolvm.UseLegacy() {
//		lv, ok := obj.(*topolvmlegacyv1.LogicalVolume)
//		if !ok {
//			return false
//		}
//		name = lv.Spec.NodeName
//	} else {
//		lv, ok := obj.(*topolvmv1.LogicalVolume)
//		if !ok {
//			return false
//		}
//		name = lv.Spec.NodeName
//	}
//	if name == f.nodeName {
//		return true
//	}
//	return false
//}
//
//func (f logicalVolumeFilter) Create(e event.CreateEvent) bool {
//	return f.filter(e.Object)
//}
//
//func (f logicalVolumeFilter) Delete(e event.DeleteEvent) bool {
//	return f.filter(e.Object)
//}
//
//func (f logicalVolumeFilter) Update(e event.UpdateEvent) bool {
//	return f.filter(e.ObjectNew)
//}
//
//func (f logicalVolumeFilter) Generic(e event.GenericEvent) bool {
//	return f.filter(e.Object)
//}
//
//func extractFromError(err error) (codes.Code, string) {
//	s, ok := status.FromError(err)
//	if !ok {
//		return codes.Internal, err.Error()
//	}
//	return s.Code(), s.Message()
//}
//
//func containsKeyAndValue(labels map[string]string, key, value string) bool {
//	for k, v := range labels {
//		if k == key && v == value {
//			return true
//		}
//	}
//	return false
//}
