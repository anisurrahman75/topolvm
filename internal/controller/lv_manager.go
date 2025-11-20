package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	topolvmv1 "github.com/topolvm/topolvm/api/v1"
	"github.com/topolvm/topolvm/pkg/lvmd/proto"
	"google.golang.org/grpc/codes"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
)

// ===========================
// LV Manager
// ===========================

type lvManager struct {
	reconciler *LogicalVolumeReconciler
}

func newLVManager(r *LogicalVolumeReconciler) *lvManager {
	return &lvManager{reconciler: r}
}

// shouldExpand determines if the LV needs expansion
func (m *lvManager) shouldExpand(lv *topolvmv1.LogicalVolume) bool {
	// If CurrentSize is not set, we need to expand to set it
	if lv.Status.CurrentSize == nil {
		return true
	}

	// If requested size is larger than current size, we need to expand
	return lv.Spec.Size.Cmp(*lv.Status.CurrentSize) > 0
}

// create creates a new logical volume
func (m *lvManager) create(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume, restoreFromSnapshot bool) error {
	log.Info("Creating LogicalVolume", "name", lv.Name, "uid", lv.UID, "restoreFromSnapshot", restoreFromSnapshot)

	// If status code is not OK, creation has already failed
	if lv.Status.Code != codes.OK {
		log.Info("LogicalVolume creation previously failed", "code", lv.Status.Code, "message", lv.Status.Message)
		return nil
	}

	// Check if volume already exists (for idempotency)
	exists, err := m.checkVolumeExists(ctx, log, lv)
	if err != nil {
		return m.updateStatusWithError(ctx, log, lv, codes.Internal, "failed to check volume existence", err)
	}

	if exists {
		log.Info("LogicalVolume already exists, updating status", "uid", lv.UID)
		return m.updateStatusForExisting(ctx, log, lv)
	}

	// Create new volume
	if lv.Spec.Source != "" && !restoreFromSnapshot {
		return m.createSnapshotLV(ctx, log, lv)
	}

	return m.createRegularLV(ctx, log, lv)
}

// createRegularLV creates a regular logical volume
func (m *lvManager) createRegularLV(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume) error {
	log.Info("Creating regular LV", "name", lv.Name, "size", lv.Spec.Size.String())

	resp, err := m.reconciler.lvService.CreateLV(ctx, &proto.CreateLVRequest{
		Name:                string(lv.UID),
		DeviceClass:         lv.Spec.DeviceClass,
		LvcreateOptionClass: lv.Spec.LvcreateOptionClass,
		SizeBytes:           lv.Spec.Size.Value(),
	})

	if err != nil {
		code, message := extractFromError(err)
		log.Error(err, "Failed to create LV", "code", code, "message", message)
		return m.updateStatusWithError(ctx, log, lv, code, message, err)
	}

	return m.updateStatusAfterCreation(ctx, log, lv, resp.Volume)
}

// createSnapshotLV creates a snapshot logical volume
func (m *lvManager) createSnapshotLV(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume) error {
	log.Info("Creating snapshot LV", "name", lv.Name, "source", lv.Spec.Source)

	// Validate access type
	if lv.Spec.AccessType != "ro" && lv.Spec.AccessType != "rw" {
		err := fmt.Errorf("invalid access type: %s, must be 'ro' or 'rw'", lv.Spec.AccessType)
		return m.updateStatusWithError(ctx, log, lv, codes.InvalidArgument, err.Error(), err)
	}

	// Get source LV
	sourceLV, err := m.getSourceLV(ctx, lv)
	if err != nil {
		return m.updateStatusWithError(ctx, log, lv, codes.NotFound, "failed to fetch source LV", err)
	}

	// Validate size
	currentSize := sourceLV.Status.CurrentSize.Value()
	requestedSize := lv.Spec.Size.Value()
	if requestedSize < currentSize {
		err := fmt.Errorf("requested size %d is smaller than source LV size %d", requestedSize, currentSize)
		return m.updateStatusWithError(ctx, log, lv, codes.InvalidArgument, err.Error(), err)
	}

	// Create snapshot
	resp, err := m.reconciler.lvService.CreateLVSnapshot(ctx, &proto.CreateLVSnapshotRequest{
		Name:         string(lv.UID),
		DeviceClass:  lv.Spec.DeviceClass,
		SourceVolume: sourceLV.Status.VolumeID,
		SizeBytes:    requestedSize,
		AccessType:   lv.Spec.AccessType,
	})

	if err != nil {
		code, message := extractFromError(err)
		log.Error(err, "Failed to create snapshot LV", "code", code, "message", message)
		return m.updateStatusWithError(ctx, log, lv, code, message, err)
	}

	return m.updateStatusAfterCreation(ctx, log, lv, resp.Snapshot)
}

// expand expands an existing logical volume
func (m *lvManager) expand(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume) error {
	originalSize := int64(-1)
	if lv.Status.CurrentSize != nil {
		originalSize = lv.Status.CurrentSize.Value()
	}

	requestedSize := lv.Spec.Size.Value()

	log.Info("Expanding LogicalVolume",
		"name", lv.Name,
		"currentSize", originalSize,
		"requestedSize", requestedSize)

	resp, err := m.reconciler.lvService.ResizeLV(ctx, &proto.ResizeLVRequest{
		Name:        string(lv.UID),
		SizeBytes:   requestedSize,
		DeviceClass: lv.Spec.DeviceClass,
	})

	if err != nil {
		code, message := extractFromError(err)
		log.Error(err, "Failed to expand LV", "code", code, "message", message)
		lv.Status.Code = code
		lv.Status.Message = message

		if updateErr := m.reconciler.client.Status().Update(ctx, lv); updateErr != nil {
			log.Error(updateErr, "Failed to update status after expand error")
		}
		return err
	}

	// Update status with new size
	lv.Status.CurrentSize = resource.NewQuantity(resp.SizeBytes, resource.BinarySI)
	lv.Status.Code = codes.OK
	lv.Status.Message = ""

	if err := m.reconciler.client.Status().Update(ctx, lv); err != nil {
		log.Error(err, "Failed to update status after expansion")
		return err
	}

	log.Info("Successfully expanded LogicalVolume",
		"name", lv.Name,
		"originalSize", originalSize,
		"newSize", resp.SizeBytes)

	return nil
}

// Helper methods

func (m *lvManager) checkVolumeExists(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume) (bool, error) {
	respList, err := m.reconciler.vgService.GetLVList(ctx, &proto.GetLVListRequest{
		DeviceClass: lv.Spec.DeviceClass,
	})

	if err != nil {
		log.Error(err, "Failed to get LV list")
		return false, err
	}

	for _, v := range respList.Volumes {
		if v.Name == string(lv.UID) {
			return true, nil
		}
	}

	return false, nil
}

func (m *lvManager) getSourceLV(ctx context.Context, lv *topolvmv1.LogicalVolume) (*topolvmv1.LogicalVolume, error) {
	sourceLV := &topolvmv1.LogicalVolume{}
	key := types.NamespacedName{
		Namespace: lv.Namespace,
		Name:      lv.Spec.Source,
	}

	if err := m.reconciler.client.Get(ctx, key, sourceLV); err != nil {
		return nil, fmt.Errorf("unable to fetch source LogicalVolume %s: %w", lv.Spec.Source, err)
	}

	return sourceLV, nil
}

func (m *lvManager) updateStatusForExisting(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume) error {
	lv.Status.VolumeID = string(lv.UID)
	lv.Status.Code = codes.OK
	lv.Status.Message = ""

	if err := m.reconciler.client.Status().Update(ctx, lv); err != nil {
		log.Error(err, "Failed to update status for existing volume")
		return err
	}

	return nil
}

func (m *lvManager) updateStatusAfterCreation(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume, volume *proto.LogicalVolume) error {
	lv.Status.VolumeID = volume.Name
	lv.Status.CurrentSize = resource.NewQuantity(volume.SizeBytes, resource.BinarySI)
	lv.Status.Code = codes.OK
	lv.Status.Message = ""

	if err := m.reconciler.client.Status().Update(ctx, lv); err != nil {
		log.Error(err, "Failed to update status after creation")
		return err
	}

	log.Info("Successfully created LogicalVolume",
		"name", lv.Name,
		"uid", lv.UID,
		"volumeID", lv.Status.VolumeID,
		"size", volume.SizeBytes)

	return nil
}

func (m *lvManager) updateStatusWithError(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume, code codes.Code, message string, err error) error {
	lv.Status.Code = code
	lv.Status.Message = message

	if updateErr := m.reconciler.client.Status().Update(ctx, lv); updateErr != nil {
		log.Error(updateErr, "Failed to update status with error")
	}

	return err
}
