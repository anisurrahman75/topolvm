package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	topolvmv1 "github.com/topolvm/topolvm/api/v1"
	"github.com/topolvm/topolvm/internal/executor"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ===========================
// Restore Handler
// ===========================

type restoreHandler struct {
	reconciler *LogicalVolumeReconciler
}

func newRestoreHandler(r *LogicalVolumeReconciler) *restoreHandler {
	return &restoreHandler{reconciler: r}
}

// restoreFromSnapshot performs an online snapshot restore
func (h *restoreHandler) restoreFromSnapshot(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume, sCtx *snapshotContext) error {
	log.Info("Restoring from snapshot", "name", lv.Name, "source", lv.Spec.Source)

	// Check current restore status
	phase := h.getCurrentPhase(lv)

	// Check if already completed
	if phase == topolvmv1.OperationPhaseSucceeded || phase == topolvmv1.OperationPhaseFailed {
		log.Info("Restore already completed", "name", lv.Name, "phase", phase)
		return nil
	}

	// Initialize if needed
	if phase == "" {
		log.Info("Initializing restore operation")
		if err := h.updateStatus(ctx, lv, topolvmv1.OperationRestore, topolvmv1.OperationPhasePending, "Initializing online restore", nil); err != nil {
			log.Error(err, "Failed to initialize restore status")
			return err
		}
		return nil // Requeue to proceed
	}

	// Check if currently running
	if phase == topolvmv1.OperationPhaseRunning {
		log.Info("Restore is currently running", "name", lv.Name)
		return nil
	}

	// Mount the LV for restore (read-write mode)
	log.Info("Mounting LV for restore", "name", lv.Name)
	mountResp, err := h.reconciler.lvMount.Mount(ctx, lv, []string{})
	if err != nil {
		log.Error(err, "Failed to mount LV for restore")
		return h.handleMountError(ctx, lv, err)
	}

	// Execute restore
	log.Info("Executing restore operation", "name", lv.Name, "mountPath", mountResp.MountPath)
	executor := executor.NewRestoreExecutor(h.reconciler.client, lv, sCtx.sourceLV, mountResp, sCtx.vsClass)
	if err := executor.Execute(); err != nil {
		log.Error(err, "Failed to execute restore")
		return h.handleExecutionError(ctx, lv, err)
	}

	log.Info("Restore operation completed successfully", "name", lv.Name)
	return nil
}

// checkPVExists verifies if the PersistentVolume exists
func (h *restoreHandler) checkPVExists(ctx context.Context, lv *topolvmv1.LogicalVolume) (bool, error) {
	if lv.Spec.Name == "" {
		return false, nil
	}

	pv := &corev1.PersistentVolume{}
	pv.Name = lv.Spec.Name

	if err := h.reconciler.client.Get(ctx, client.ObjectKeyFromObject(pv), pv); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

// isRestoreCompleted checks if the restore operation is completed
func (h *restoreHandler) isRestoreCompleted(lv *topolvmv1.LogicalVolume) bool {
	if lv.Status.Snapshot == nil {
		return false
	}
	return lv.Status.Snapshot.Phase == topolvmv1.OperationPhaseSucceeded
}

// Helper methods

func (h *restoreHandler) getCurrentPhase(lv *topolvmv1.LogicalVolume) topolvmv1.OperationPhase {
	if lv.Status.Snapshot == nil {
		return ""
	}
	return lv.Status.Snapshot.Phase
}

func (h *restoreHandler) updateStatus(ctx context.Context, lv *topolvmv1.LogicalVolume, operation topolvmv1.OperationType, phase topolvmv1.OperationPhase, message string, snapshotErr *topolvmv1.SnapshotError) error {
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
		return fmt.Errorf("failed to update restore status: %w", err)
	}

	// Sync back to original object
	lv.Status = freshLV.Status
	lv.ObjectMeta.ResourceVersion = freshLV.ObjectMeta.ResourceVersion

	return nil
}

func (h *restoreHandler) handleMountError(ctx context.Context, lv *topolvmv1.LogicalVolume, err error) error {
	snapshotErr := &topolvmv1.SnapshotError{
		Code:    "VolumeBindMountFailed",
		Message: fmt.Sprintf("failed to mount logical volume for restore: %v", err),
	}

	message := fmt.Sprintf("Failed to mount logical volume: %v", err)

	if updateErr := h.updateStatus(ctx, lv, topolvmv1.OperationRestore, topolvmv1.OperationPhaseFailed, message, snapshotErr); updateErr != nil {
		return fmt.Errorf("failed to update status after mount error: %w (original error: %w)", updateErr, err)
	}

	return err
}

func (h *restoreHandler) handleExecutionError(ctx context.Context, lv *topolvmv1.LogicalVolume, err error) error {
	snapshotErr := &topolvmv1.SnapshotError{
		Code:    "RestoreExecutionFailed",
		Message: fmt.Sprintf("failed to execute restore: %v", err),
	}

	message := fmt.Sprintf("Failed to execute restore: %v", err)

	if updateErr := h.updateStatus(ctx, lv, topolvmv1.OperationRestore, topolvmv1.OperationPhaseFailed, message, snapshotErr); updateErr != nil {
		return fmt.Errorf("failed to update status after execution error: %w (original error: %w)", updateErr, err)
	}

	return err
}
