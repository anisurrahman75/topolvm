package controller

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	"github.com/topolvm/topolvm"
	topolvmlegacyv1 "github.com/topolvm/topolvm/api/legacy/v1"
	topolvmv1 "github.com/topolvm/topolvm/api/v1"
	"github.com/topolvm/topolvm/internal/mounter"
	"github.com/topolvm/topolvm/pkg/lvmd/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	requeueIntervalForPVWait = 5 * time.Second
)

// ReconciliationPhase represents the current phase of reconciliation
type ReconciliationPhase string

const (
	PhaseValidation   ReconciliationPhase = "Validation"
	PhaseCreation     ReconciliationPhase = "Creation"
	PhaseExpansion    ReconciliationPhase = "Expansion"
	PhaseSnapshot     ReconciliationPhase = "Snapshot"
	PhaseRestore      ReconciliationPhase = "Restore"
	PhaseFinalization ReconciliationPhase = "Finalization"
)

// ===========================
// Main Reconciler Structure
// ===========================

// LogicalVolumeReconciler reconciles a LogicalVolume object
type LogicalVolumeReconciler struct {
	client    client.Client
	nodeName  string
	vgService proto.VGServiceClient
	lvService proto.LVServiceClient
	lvMount   *mounter.LVMount

	// Helper components
	finalizer *finalizerHandler
	validator *validator
	lvManager *lvManager
	snapshot  *snapshotHandler
	restore   *restoreHandler
}

//+kubebuilder:rbac:groups=topolvm.io,resources=logicalvolumes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=topolvm.io,resources=logicalvolumes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshotcontents,verbs=get;list;watch
//+kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshotclasses,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=get;list;watch

func NewLogicalVolumeReconcilerWithServices(client client.Client, nodeName string, vgService proto.VGServiceClient, lvService proto.LVServiceClient) *LogicalVolumeReconciler {
	lvMount := mounter.NewLVMount(client, vgService, lvService)

	r := &LogicalVolumeReconciler{
		client:    client,
		nodeName:  nodeName,
		vgService: vgService,
		lvService: lvService,
		lvMount:   lvMount,
	}

	// Initialize helper components
	r.finalizer = newFinalizerHandler(r)
	r.validator = newValidator(r)
	r.lvManager = newLVManager(r)
	r.snapshot = newSnapshotHandler(r)
	r.restore = newRestoreHandler(r)

	return r
}

// ===========================
// Main Reconcile Loop
// ===========================

// Reconcile is the main reconciliation loop
func (r *LogicalVolumeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := crlog.FromContext(ctx).WithName("LogicalVolumeReconciler")
	log.Info("Starting reconciliation", "request", req.NamespacedName)

	// Step 1: Fetch the LogicalVolume
	lv := &topolvmv1.LogicalVolume{}
	if err := r.client.Get(ctx, req.NamespacedName, lv); err != nil {
		if apierrs.IsNotFound(err) {
			log.Info("LogicalVolume not found, likely deleted", "name", req.Name)
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to fetch LogicalVolume")
		return ctrl.Result{}, err
	}

	// Step 2: Filter by node name
	if lv.Spec.NodeName != r.nodeName {
		log.V(1).Info("Skipping LogicalVolume for different node",
			"lvNode", lv.Spec.NodeName, "thisNode", r.nodeName)
		return ctrl.Result{}, nil
	}

	// Step 3: Handle deletion
	if lv.DeletionTimestamp != nil {
		return r.reconcileDelete(ctx, log, lv)
	}

	// Step 4: Reconcile normal operations
	return r.reconcileNormal(ctx, log, lv)
}

// reconcileNormal handles the normal reconciliation flow (create/update)
func (r *LogicalVolumeReconciler) reconcileNormal(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume) (ctrl.Result, error) {
	log.Info("Reconciling normal operations", "phase", PhaseValidation)

	// Phase 1: Ensure finalizer
	if result, err := r.finalizer.ensureFinalizer(ctx, log, lv); err != nil || result.Requeue {
		return result, err
	}

	// Phase 2: Ensure labels
	if result, err := r.finalizer.ensureLabels(ctx, log, lv); err != nil || result.Requeue {
		return result, err
	}

	// Phase 3: Check pending deletion annotation
	if r.validator.isPendingDeletion(lv) {
		log.Info("LogicalVolume is pending deletion, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	// Phase 4: Get snapshot context
	snapshotCtx, err := r.snapshot.buildContext(ctx, log, lv)
	if err != nil {
		log.Error(err, "Failed to build snapshot context")
		return ctrl.Result{}, err
	}

	// Phase 5: Create LV if not exists
	if lv.Status.VolumeID == "" {
		log.Info("LogicalVolume not created yet", "phase", PhaseCreation)
		return r.reconcileCreation(ctx, log, lv, snapshotCtx)
	}

	// Phase 6: Expand LV if needed
	if r.lvManager.shouldExpand(lv) {
		log.Info("LogicalVolume needs expansion", "phase", PhaseExpansion)
		if err := r.lvManager.expand(ctx, log, lv); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Phase 7: Handle restore from snapshot
	if snapshotCtx.shouldRestore {
		log.Info("Processing restore from snapshot", "phase", PhaseRestore)
		if result, err := r.reconcileRestore(ctx, log, lv, snapshotCtx); err != nil || result.Requeue {
			return result, err
		}
	}

	// Phase 8: Handle snapshot backup
	if snapshotCtx.shouldBackup {
		log.Info("Processing snapshot backup", "phase", PhaseSnapshot)
		if err := r.reconcileSnapshot(ctx, log, lv, snapshotCtx); err != nil {
			return ctrl.Result{}, err
		}
	}

	log.Info("Reconciliation completed successfully")
	return ctrl.Result{}, nil
}

// reconcileCreation handles LV creation
func (r *LogicalVolumeReconciler) reconcileCreation(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume, snapshotCtx *snapshotContext) (ctrl.Result, error) {
	if err := r.lvManager.create(ctx, log, lv, snapshotCtx.shouldRestore); err != nil {
		log.Error(err, "Failed to create LogicalVolume")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// reconcileSnapshot handles snapshot backup operations
func (r *LogicalVolumeReconciler) reconcileSnapshot(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume, snapshotCtx *snapshotContext) error {
	return r.snapshot.takeSnapshot(ctx, log, lv, snapshotCtx)
}

// reconcileRestore handles restore operations
func (r *LogicalVolumeReconciler) reconcileRestore(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume, snapshotCtx *snapshotContext) (ctrl.Result, error) {
	// Check if PV exists before restoring
	pvExists, err := r.restore.checkPVExists(ctx, lv)
	if err != nil {
		log.Error(err, "Failed to check PV existence")
		return ctrl.Result{}, err
	}

	if !pvExists {
		log.Info("PV not ready yet, waiting...", "pvName", lv.Spec.Name)
		return ctrl.Result{RequeueAfter: requeueIntervalForPVWait}, nil
	}

	// Perform restore
	if err := r.restore.restoreFromSnapshot(ctx, log, lv, snapshotCtx); err != nil {
		return ctrl.Result{}, err
	}

	// Cleanup mount after successful restore
	if r.restore.isRestoreCompleted(lv) {
		log.Info("Restore completed, cleaning up mount")
		if err := r.lvMount.Unmount(ctx, lv); err != nil {
			log.Error(err, "Failed to unmount after restore")
			// Don't fail the reconciliation, just log the error
		}
	}

	return ctrl.Result{}, nil
}

// reconcileDelete handles LV deletion
func (r *LogicalVolumeReconciler) reconcileDelete(ctx context.Context, log logr.Logger, lv *topolvmv1.LogicalVolume) (ctrl.Result, error) {
	log.Info("Reconciling deletion", "phase", PhaseFinalization)

	if !controllerutil.ContainsFinalizer(lv, topolvm.GetLogicalVolumeFinalizer()) {
		log.Info("Finalizer already removed, nothing to do")
		return ctrl.Result{}, nil
	}

	// Remove LV
	if err := r.finalizer.cleanup(ctx, log, lv); err != nil {
		log.Error(err, "Failed to cleanup LogicalVolume")
		return ctrl.Result{}, err
	}

	// Remove finalizer
	if err := r.finalizer.removeFinalizer(ctx, log, lv); err != nil {
		log.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	log.Info("Deletion completed successfully")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *LogicalVolumeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr)
	if topolvm.UseLegacy() {
		builder = builder.For(&topolvmlegacyv1.LogicalVolume{})
	} else {
		builder = builder.For(&topolvmv1.LogicalVolume{})
	}
	return builder.WithEventFilter(&logicalVolumeFilter{r.nodeName}).Complete(r)
}

// ===========================
// Event Filter
// ===========================

type logicalVolumeFilter struct {
	nodeName string
}

func (f logicalVolumeFilter) filter(obj client.Object) bool {
	var name string
	if topolvm.UseLegacy() {
		lv, ok := obj.(*topolvmlegacyv1.LogicalVolume)
		if !ok {
			return false
		}
		name = lv.Spec.NodeName
	} else {
		lv, ok := obj.(*topolvmv1.LogicalVolume)
		if !ok {
			return false
		}
		name = lv.Spec.NodeName
	}
	return name == f.nodeName
}

func (f logicalVolumeFilter) Create(e event.CreateEvent) bool {
	return f.filter(e.Object)
}

func (f logicalVolumeFilter) Delete(e event.DeleteEvent) bool {
	return f.filter(e.Object)
}

func (f logicalVolumeFilter) Update(e event.UpdateEvent) bool {
	return f.filter(e.ObjectNew)
}

func (f logicalVolumeFilter) Generic(e event.GenericEvent) bool {
	return f.filter(e.Object)
}

// ===========================
// Utility Functions
// ===========================

func extractFromError(err error) (codes.Code, string) {
	s, ok := status.FromError(err)
	if !ok {
		return codes.Internal, err.Error()
	}
	return s.Code(), s.Message()
}

func containsKeyAndValue(labels map[string]string, key, value string) bool {
	v, ok := labels[key]
	return ok && v == value
}
