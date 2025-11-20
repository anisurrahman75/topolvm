package controller

import (
	"github.com/topolvm/topolvm"
	topolvmv1 "github.com/topolvm/topolvm/api/v1"
)

// ===========================
// Validator
// ===========================

type validator struct {
	reconciler *LogicalVolumeReconciler
}

func newValidator(r *LogicalVolumeReconciler) *validator {
	return &validator{reconciler: r}
}

// isPendingDeletion checks if the LogicalVolume has a pending deletion annotation
func (v *validator) isPendingDeletion(lv *topolvmv1.LogicalVolume) bool {
	if lv.Annotations == nil {
		return false
	}

	_, exists := lv.Annotations[topolvm.GetLVPendingDeletionKey()]
	return exists
}

// isValidAccessType checks if the access type is valid for snapshot LVs
func (v *validator) isValidAccessType(accessType string) bool {
	return accessType == "ro" || accessType == "rw"
}
