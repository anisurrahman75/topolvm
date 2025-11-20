package controller

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	snapshot_api "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	topolvmv1 "github.com/topolvm/topolvm/api/v1"
	"github.com/topolvm/topolvm/internal/executor"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ===========================
// Snapshot Context
// ===========================

// snapshotContext holds all snapshot-related information for a reconciliation
type snapshotContext struct {
	// Source information
	sourceLV  *topolvmv1.LogicalVolume
	vsContent *snapshot_api.VolumeSnapshotContent
	vsClass   *snapshot_api.VolumeSnapshotClass

	// Decisions
	shouldBackup  bool
	shouldRestore bool

	// Metadata
	isOnlineMode bool
}

// ===========================
// Snapshot Handler
// ===========================

type snapshotHandler struct {
	reconciler *LogicalVolumeReconciler
}

func newSnapshotHandler(r *LogicalVolumeReconciler) *snapshotHandler {
	return &snapshotHandler{reconciler: r}
}

// buildContext builds the snapshot context for the current reconciliation
func (h *snapshotHandler) buildContext(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume) (*snapshotContext, error) {
	sCtx := &snapshotContext{}

	// Get source LV if specified
	if lv.Spec.Source != "" {
		sourceLV, err := h.getSourceLV(ctx, lv)
		if err != nil {
			// Ignore not found - might be a VolumeSnapshot source
			if !client.IgnoreNotFound(err) != nil {
				return nil, fmt.Errorf("failed to get source LV: %w", err)
			}
		}
		sCtx.sourceLV = sourceLV
	}

	// Get VolumeSnapshotContent and Class for the current LV
	vsContent, vsClass, err := h.getVolumeSnapshotInfo(ctx, lv)
	if err != nil {
		return nil, fmt.Errorf("failed to get volume snapshot info: %w", err)
	}
	sCtx.vsContent = vsContent
	sCtx.vsClass = vsClass

	// Determine if online mode is enabled
	sCtx.isOnlineMode = h.isOnlineSnapshotMode(vsClass)

	// Determine if we should take a backup
	sCtx.shouldBackup = sCtx.isOnlineMode && vsContent != nil

	// Determine if we should restore
	if sCtx.sourceLV != nil {
		// Get VolumeSnapshotClass for source LV
		sourceVSContent, sourceVSClass, err := h.getVolumeSnapshotInfo(ctx, sCtx.sourceLV)
		if err != nil {
			return nil, fmt.Errorf("failed to get source volume snapshot info: %w", err)
		}

		sCtx.shouldRestore = h.shouldRestoreFromSnapshot(sCtx.sourceLV, sourceVSClass)

		// Use source's VSClass and Content for restore
		if sCtx.shouldRestore {
			sCtx.vsContent = sourceVSContent
			sCtx.vsClass = sourceVSClass
		}
	}

	log.V(1).Info("Built snapshot context",
		"shouldBackup", sCtx.shouldBackup,
		"shouldRestore", sCtx.shouldRestore,
		"isOnlineMode", sCtx.isOnlineMode,
		"hasSourceLV", sCtx.sourceLV != nil)

	return sCtx, nil
}

// takeSnapshot performs an online snapshot backup
func (h *snapshotHandler) takeSnapshot(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume, sCtx *snapshotContext) error {
	log.Info("Taking online snapshot", "name", lv.Name)

	// Check current snapshot status
	phase := h.getCurrentPhase(lv)

	// Initialize if needed
	if phase == "" {
		log.Info("Initializing snapshot operation")
		if err := h.updateStatus(ctx, lv, topolvmv1.OperationBackup, topolvmv1.OperationPhasePending, "Initializing online snapshot", nil); err != nil {
			log.Error(err, "Failed to initialize snapshot status")
			return err
		}
		return nil // Requeue to proceed
	}

	// Check if already completed
	if phase == topolvmv1.OperationPhaseSucceeded {
		log.Info("Snapshot already succeeded", "name", lv.Name)
		return nil
	}

	if phase == topolvmv1.OperationPhaseFailed {
		log.Info("Snapshot previously failed", "name", lv.Name)
		return nil
	}

	// Check if currently running
	if phase == topolvmv1.OperationPhaseRunning {
		log.Info("Snapshot is currently running", "name", lv.Name)
		return nil
	}

	// Mount the LV for snapshot
	log.Info("Mounting LV for snapshot", "name", lv.Name)
	mountResp, err := h.reconciler.lvMount.Mount(ctx, lv, []string{"ro", "norecovery"})
	if err != nil {
		log.Error(err, "Failed to mount LV for snapshot")
		return h.handleMountError(ctx, lv, topolvmv1.OperationBackup, err)
	}

	// Execute snapshot
	log.Info("Executing snapshot operation", "name", lv.Name, "mountPath", mountResp.MountPath)
	executor := executor.NewSnapshotExecutor(h.reconciler.client, lv, mountResp, sCtx.vsContent, sCtx.vsClass)
	if err := executor.Execute(); err != nil {
		log.Error(err, "Failed to execute snapshot")
		return h.handleExecutionError(ctx, lv, topolvmv1.OperationBackup, err)
	}

	log.Info("Snapshot operation completed successfully", "name", lv.Name)
	return nil
}

// Helper methods

func (h *snapshotHandler) getSourceLV(ctx context.Context, lv *topolvmv1.LogicalVolume) (*topolvmv1.LogicalVolume, error) {
	if lv.Spec.Source == "" {
		return nil, nil
	}

	sourceLV := &topolvmv1.LogicalVolume{}
	sourceLV.Name = lv.Spec.Source

	if err := h.reconciler.client.Get(ctx, client.ObjectKeyFromObject(sourceLV), sourceLV); err != nil {
		return nil, err
	}

	return sourceLV, nil
}

func (h *snapshotHandler) getVolumeSnapshotInfo(ctx context.Context, lv *topolvmv1.LogicalVolume) (*snapshot_api.VolumeSnapshotContent, *snapshot_api.VolumeSnapshotClass, error) {
	// Get VolumeSnapshotContent
	vsContent, err := h.getVolumeSnapshotContent(ctx, lv)
	if err != nil {
		// Ignore not found errors
		if client.IgnoreNotFound(err) == nil {
			return nil, nil, nil
		}
		return nil, nil, err
	}

	if vsContent == nil {
		return nil, nil, nil
	}

	// Get VolumeSnapshotClass
	vsClass, err := h.getVolumeSnapshotClass(ctx, vsContent)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get VolumeSnapshotClass: %w", err)
	}

	return vsContent, vsClass, nil
}

func (h *snapshotHandler) getVolumeSnapshotContent(ctx context.Context, lv *topolvmv1.LogicalVolume) (*snapshot_api.VolumeSnapshotContent, error) {
	if lv.Spec.Source == "" {
		return nil, nil
	}

	// The VolumeSnapshotContent name is derived from the PV name
	// Format: snapcontent-{uuid} where uuid comes from "snapshot-{uuid}" PV name
	contentName := fmt.Sprintf("snapcontent%s", strings.TrimPrefix(lv.Spec.Name, "snapshot"))

	vsContent := &snapshot_api.VolumeSnapshotContent{}
	vsContent.Name = contentName

	if err := h.reconciler.client.Get(ctx, client.ObjectKeyFromObject(vsContent), vsContent); err != nil {
		return nil, err
	}

	return vsContent, nil
}

func (h *snapshotHandler) getVolumeSnapshotClass(ctx context.Context, vsContent *snapshot_api.VolumeSnapshotContent) (*snapshot_api.VolumeSnapshotClass, error) {
	if vsContent == nil || vsContent.Spec.VolumeSnapshotClassName == nil {
		return nil, nil
	}

	vsClass := &snapshot_api.VolumeSnapshotClass{}
	vsClass.Name = *vsContent.Spec.VolumeSnapshotClassName

	if err := h.reconciler.client.Get(ctx, client.ObjectKeyFromObject(vsClass), vsClass); err != nil {
		return nil, fmt.Errorf("unable to fetch VolumeSnapshotClass %s: %w", *vsContent.Spec.VolumeSnapshotClassName, err)
	}

	return vsClass, nil
}

func (h *snapshotHandler) isOnlineSnapshotMode(vsClass *snapshot_api.VolumeSnapshotClass) bool {
	if vsClass == nil || vsClass.Parameters == nil {
		return false
	}

	mode, ok := vsClass.Parameters[SnapshotMode]
	return ok && mode == SnapshotModeOnline
}

func (h *snapshotHandler) shouldRestoreFromSnapshot(sourceLV *topolvmv1.LogicalVolume, vsClass *snapshot_api.VolumeSnapshotClass) bool {
	// Only restore if source snapshot is completed or doesn't have snapshot status
	if sourceLV.Status.Snapshot != nil {
		phase := sourceLV.Status.Snapshot.Phase
		if phase != topolvmv1.OperationPhaseSucceeded && phase != "" {
			return false
		}
	}

	return h.isOnlineSnapshotMode(vsClass)
}

func (h *snapshotHandler) getCurrentPhase(lv *topolvmv1.LogicalVolume) topolvmv1.OperationPhase {
	if lv.Status.Snapshot == nil {
		return ""
	}
	return lv.Status.Snapshot.Phase
}

func (h *snapshotHandler) updateStatus(ctx context.Context, lv *topolvmv1.LogicalVolume, operation topolvmv1.OperationType, phase topolvmv1.OperationPhase, message string, snapshotErr *topolvmv1.SnapshotError) error {
	// Refresh the LV to get the latest version
	freshLV := &topolvmv1.LogicalVolume{}
	if err := h.reconciler.client.Get(ctx, client.ObjectKeyFromObject(lv), freshLV); err != nil {
		return fmt.Errorf("failed to get latest LogicalVolume: %w", err)
	}

	// Initialize snapshot status if needed
	if freshLV.Status.Snapshot == nil {
		freshLV.Status.Snapshot = &topolvmv1.SnapshotStatus{
			StartTime: metav1.Now(),
		}
	}

	// Update fields
	freshLV.Status.Snapshot.Operation = operation
	freshLV.Status.Snapshot.Phase = phase
	freshLV.Status.Snapshot.Message = message

	if snapshotErr != nil {
		freshLV.Status.Snapshot.Error = snapshotErr
	}

	// Update status
	if err := h.reconciler.client.Status().Update(ctx, freshLV); err != nil {
		return fmt.Errorf("failed to update snapshot status: %w", err)
	}

	// Sync back to original object
	lv.Status = freshLV.Status
	lv.ObjectMeta.ResourceVersion = freshLV.ObjectMeta.ResourceVersion

	return nil
}

func (h *snapshotHandler) handleMountError(ctx context.Context, lv *topolvmv1.LogicalVolume, operation topolvmv1.OperationType, err error) error {
	snapshotErr := &topolvmv1.SnapshotError{
		Code:    "VolumeMountFailed",
		Message: fmt.Sprintf("failed to mount logical volume: %v", err),
	}

	message := fmt.Sprintf("Failed to mount logical volume: %v", err)

	if updateErr := h.updateStatus(ctx, lv, operation, topolvmv1.OperationPhaseFailed, message, snapshotErr); updateErr != nil {
		return fmt.Errorf("failed to update status after mount error: %w (original error: %w)", updateErr, err)
	}

	return err
}

func (h *snapshotHandler) handleExecutionError(ctx context.Context, lv *topolvmv1.LogicalVolume, operation topolvmv1.OperationType, err error) error {
	snapshotErr := &topolvmv1.SnapshotError{
		Code:    "ExecutionFailed",
		Message: fmt.Sprintf("failed to execute operation: %v", err),
	}

	message := fmt.Sprintf("Failed to execute operation: %v", err)

	if updateErr := h.updateStatus(ctx, lv, operation, topolvmv1.OperationPhaseFailed, message, snapshotErr); updateErr != nil {
		return fmt.Errorf("failed to update status after execution error: %w (original error: %w)", updateErr, err)
	}

	return err
}
