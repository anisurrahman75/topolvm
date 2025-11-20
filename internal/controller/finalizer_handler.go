package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/topolvm/topolvm"
	topolvmv1 "github.com/topolvm/topolvm/api/v1"
	"github.com/topolvm/topolvm/pkg/lvmd/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ===========================
// Finalizer Handler
// ===========================

type finalizerHandler struct {
	reconciler *LogicalVolumeReconciler
}

func newFinalizerHandler(r *LogicalVolumeReconciler) *finalizerHandler {
	return &finalizerHandler{reconciler: r}
}

// ensureFinalizer ensures the LogicalVolume has the required finalizer
func (h *finalizerHandler) ensureFinalizer(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume) (ctrl.Result, error) {
	if controllerutil.ContainsFinalizer(lv, topolvm.GetLogicalVolumeFinalizer()) {
		return ctrl.Result{}, nil
	}

	log.Info("Adding finalizer to LogicalVolume", "finalizer", topolvm.GetLogicalVolumeFinalizer())

	lvCopy := lv.DeepCopy()
	controllerutil.AddFinalizer(lvCopy, topolvm.GetLogicalVolumeFinalizer())

	if err := h.reconciler.client.Patch(ctx, lvCopy, client.MergeFrom(lv)); err != nil {
		log.Error(err, "Failed to add finalizer")
		return ctrl.Result{}, err
	}

	return ctrl.Result{Requeue: true}, nil
}

// ensureLabels ensures the LogicalVolume has the required labels
func (h *finalizerHandler) ensureLabels(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume) (ctrl.Result, error) {
	if containsKeyAndValue(lv.Labels, topolvm.CreatedbyLabelKey, topolvm.CreatedbyLabelValue) {
		return ctrl.Result{}, nil
	}

	log.Info("Adding required labels to LogicalVolume")

	lvCopy := lv.DeepCopy()
	if lvCopy.Labels == nil {
		lvCopy.Labels = make(map[string]string)
	}
	lvCopy.Labels[topolvm.CreatedbyLabelKey] = topolvm.CreatedbyLabelValue

	if err := h.reconciler.client.Patch(ctx, lvCopy, client.MergeFrom(lv)); err != nil {
		log.Error(err, "Failed to add labels")
		return ctrl.Result{}, err
	}

	return ctrl.Result{Requeue: true}, nil
}

// removeFinalizer removes the finalizer from LogicalVolume
func (h *finalizerHandler) removeFinalizer(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume) error {
	log.Info("Removing finalizer from LogicalVolume")

	lvCopy := lv.DeepCopy()
	controllerutil.RemoveFinalizer(lvCopy, topolvm.GetLogicalVolumeFinalizer())

	if err := h.reconciler.client.Patch(ctx, lvCopy, client.MergeFrom(lv)); err != nil {
		log.Error(err, "Failed to remove finalizer")
		return err
	}

	return nil
}

// cleanup performs cleanup operations before deletion
func (h *finalizerHandler) cleanup(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume) error {
	log.Info("Starting cleanup for LogicalVolume", "name", lv.Name, "uid", lv.UID)

	// Step 1: Unmount the LV if mounted
	if err := h.unmountLV(ctx, log, lv); err != nil {
		// Log but continue - the LV might not be mounted
		log.Info("Note: Unmount completed with status", "error", err)
	}

	// Step 2: Remove the LV
	if err := h.removeLV(ctx, log, lv); err != nil {
		return fmt.Errorf("failed to remove LV: %w", err)
	}

	log.Info("Cleanup completed successfully")
	return nil
}

// unmountLV unmounts the LogicalVolume if it's mounted
func (h *finalizerHandler) unmountLV(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume) error {
	log.V(1).Info("Attempting to unmount LogicalVolume", "name", lv.Name)

	if err := h.reconciler.lvMount.Unmount(ctx, lv); err != nil {
		// This is not a critical error - the LV might not be mounted
		log.V(1).Info("Unmount result", "name", lv.Name, "status", err.Error())
		return err
	}

	log.Info("Successfully unmounted LogicalVolume", "name", lv.Name)
	return nil
}

// removeLV removes the logical volume from LVM
func (h *finalizerHandler) removeLV(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume) error {
	log.Info("Removing LV from LVM", "name", lv.Name, "uid", lv.UID)

	_, err := h.reconciler.lvService.RemoveLV(ctx, &proto.RemoveLVRequest{
		Name:        string(lv.UID),
		DeviceClass: lv.Spec.DeviceClass,
	})

	if err != nil {
		code := status.Code(err)
		if code == codes.NotFound {
			log.Info("LV already removed or not found", "name", lv.Name)
			return nil
		}
		log.Error(err, "Failed to remove LV", "name", lv.Name, "uid", lv.UID)
		return fmt.Errorf("failed to remove LV %s: %w", lv.Name, err)
	}

	log.Info("Successfully removed LV from LVM", "name", lv.Name, "uid", lv.UID)
	return nil
}
