# LogicalVolume Controller Refactoring

## Overview

The LogicalVolume controller has been refactored to follow Kubernetes controller best practices with clean separation of concerns, better error handling, and improved maintainability.

## Architecture

### Main Components

```
LogicalVolumeReconciler (Main Controller)
├── Finalizer Handler   - Manages finalizers, labels, and cleanup
├── Validator          - Validates LogicalVolume state and configuration
├── LV Manager         - Handles LV creation, expansion, and LVM operations
├── Snapshot Handler   - Manages online snapshot backup operations
└── Restore Handler    - Manages online snapshot restore operations
```

### File Structure

```
internal/controller/
├── logicalvolume_controller_refactored.go  - Main reconciler and orchestration
├── finalizer_handler.go                    - Finalizer and cleanup logic
├── validator.go                            - Validation logic
├── lv_manager.go                          - LV lifecycle management
├── snapshot_handler.go                     - Snapshot backup operations
└── restore_handler.go                      - Snapshot restore operations
```

## Reconciliation Flow

### Normal Reconciliation (Create/Update)

```
┌─────────────────────────────────────────────────────────────┐
│                   Reconcile Normal Flow                      │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
                   ┌──────────────────────┐
                   │  1. Ensure Finalizer │
                   └──────────┬───────────┘
                              │
                              ▼
                   ┌──────────────────────┐
                   │   2. Ensure Labels   │
                   └──────────┬───────────┘
                              │
                              ▼
                   ┌──────────────────────────┐
                   │ 3. Check Pending Delete  │
                   └──────────┬───────────────┘
                              │
                              ▼
                   ┌───────────────────────────┐
                   │ 4. Build Snapshot Context │
                   └──────────┬────────────────┘
                              │
                              ▼
                   ┌──────────────────────┐
                   │   5. Create LV if    │◄─── If VolumeID == ""
                   │      not exists      │
                   └──────────┬───────────┘
                              │
                              ▼
                   ┌──────────────────────┐
                   │  6. Expand LV if     │◄─── If size increased
                   │      needed          │
                   └──────────┬───────────┘
                              │
                              ▼
                   ┌──────────────────────┐
                   │ 7. Restore from      │◄─── If shouldRestore
                   │    Snapshot          │
                   └──────────┬───────────┘
                              │
                              ▼
                   ┌──────────────────────┐
                   │ 8. Take Snapshot     │◄─── If shouldBackup
                   │    Backup            │
                   └──────────┬───────────┘
                              │
                              ▼
                   ┌──────────────────────┐
                   │    Reconciliation    │
                   │      Complete        │
                   └──────────────────────┘
```

### Deletion Flow

```
┌────────────────────────────────────────┐
│         Reconcile Delete Flow           │
└────────────────────────────────────────┘
                   │
                   ▼
        ┌──────────────────────┐
        │ 1. Check Finalizer   │
        └──────────┬───────────┘
                   │
                   ▼
        ┌──────────────────────┐
        │ 2. Unmount LV        │
        └──────────┬───────────┘
                   │
                   ▼
        ┌──────────────────────┐
        │ 3. Remove LV from    │
        │    LVM               │
        └──────────┬───────────┘
                   │
                   ▼
        ┌──────────────────────┐
        │ 4. Remove Finalizer  │
        └──────────┬───────────┘
                   │
                   ▼
        ┌──────────────────────┐
        │      Complete        │
        └──────────────────────┘
```

## Snapshot Operations

### Snapshot Context

The `snapshotContext` holds all snapshot-related information:

```go
type snapshotContext struct {
    sourceLV      *LogicalVolume          // Source LV for restore
    vsContent     *VolumeSnapshotContent  // Snapshot content metadata
    vsClass       *VolumeSnapshotClass    // Snapshot class configuration
    shouldBackup  bool                    // Should take snapshot backup
    shouldRestore bool                    // Should restore from snapshot
    isOnlineMode  bool                    // Is online snapshot mode enabled
}
```

### Snapshot State Machine

```
┌────────────┐
│  (empty)   │
└──────┬─────┘
       │
       ▼
┌────────────┐     Mount Failed      ┌────────────┐
│  Pending   ├─────────────────────►│   Failed   │
└──────┬─────┘                       └────────────┘
       │
       │ Mount Success
       ▼
┌────────────┐     Exec Failed       ┌────────────┐
│  Running   ├─────────────────────►│   Failed   │
└──────┬─────┘                       └────────────┘
       │
       │ Exec Success
       ▼
┌────────────┐
│ Succeeded  │
└────────────┘
```

## Best Practices Implemented

### 1. **Separation of Concerns**
- Each handler has a single responsibility
- Clear boundaries between components
- Easy to test and maintain

### 2. **Idempotency**
- All operations check current state before acting
- Status is refreshed before updates to avoid conflicts
- Proper handling of "already exists" scenarios

### 3. **Error Handling**
- Structured error types with context
- Proper error propagation
- Status updates include error details

### 4. **Logging**
- Consistent logging across all components
- Structured logging with key-value pairs
- Different log levels for different scenarios

### 5. **Status Management**
- Always fetch fresh object before updating
- Update status subresource separately
- Sync ResourceVersion to avoid conflicts

### 6. **State Transitions**
- Clear phase transitions
- Prevents invalid state transitions
- Proper handling of concurrent reconciliations

## Component Details

### Finalizer Handler

**Responsibilities:**
- Add/remove finalizers
- Manage required labels
- Cleanup operations on deletion
- Unmount and remove LVs

**Key Methods:**
- `ensureFinalizer()` - Adds finalizer if missing
- `ensureLabels()` - Adds required labels
- `cleanup()` - Performs cleanup before deletion
- `removeFinalizer()` - Removes finalizer after cleanup

### Validator

**Responsibilities:**
- Validate LogicalVolume state
- Check for pending deletion
- Validate access types

**Key Methods:**
- `isPendingDeletion()` - Checks for pending deletion annotation
- `isValidAccessType()` - Validates snapshot access type

### LV Manager

**Responsibilities:**
- Create regular and snapshot LVs
- Expand existing LVs
- Manage LV lifecycle

**Key Methods:**
- `create()` - Creates new LV (regular or snapshot)
- `expand()` - Expands existing LV
- `shouldExpand()` - Determines if expansion is needed
- `checkVolumeExists()` - Checks if LV already exists

### Snapshot Handler

**Responsibilities:**
- Build snapshot context
- Take online snapshots
- Manage snapshot state machine
- Update snapshot status

**Key Methods:**
- `buildContext()` - Builds complete snapshot context
- `takeSnapshot()` - Performs snapshot backup
- `getVolumeSnapshotInfo()` - Fetches VolumeSnapshot metadata
- `isOnlineSnapshotMode()` - Checks if online mode is enabled

### Restore Handler

**Responsibilities:**
- Restore from online snapshots
- Check PV readiness
- Manage restore state machine
- Cleanup after restore

**Key Methods:**
- `restoreFromSnapshot()` - Performs restore operation
- `checkPVExists()` - Verifies PV existence
- `isRestoreCompleted()` - Checks if restore is done

## Usage Example

### Migration from Old Controller

The refactored controller is a drop-in replacement. To use it:

1. **Backup the old controller:**
   ```bash
   mv logicalvolume_controller.go logicalvolume_controller_old.go
   ```

2. **Rename the refactored controller:**
   ```bash
   mv logicalvolume_controller_refactored.go logicalvolume_controller.go
   ```

3. **Build and test:**
   ```bash
   go build ./internal/controller/
   ```

### Testing

Each component can be tested independently:

```go
// Test finalizer handler
func TestFinalizerHandler(t *testing.T) {
    // Create test reconciler
    r := NewLogicalVolumeReconcilerWithServices(...)
    
    // Test ensuring finalizer
    result, err := r.finalizer.ensureFinalizer(ctx, log, lv)
    // Assert results
}

// Test LV manager
func TestLVManager(t *testing.T) {
    // Test creation
    err := r.lvManager.create(ctx, log, lv, false)
    // Assert results
}
```

## Configuration

### Mount Options

Mount options are now configurable per operation:

```go
// For snapshot (read-only)
mountResp, err := r.lvMount.Mount(ctx, lv, []string{"ro", "norecovery"})

// For restore (read-write)
mountResp, err := r.lvMount.Mount(ctx, lv, []string{})
```

### Snapshot Mode

Online snapshot mode is controlled via VolumeSnapshotClass parameters:

```yaml
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshotClass
metadata:
  name: topolvm-snapshot
driver: topolvm.io
deletionPolicy: Delete
parameters:
  snapshot-mode: "online"  # Enable online snapshots
```

## Troubleshooting

### Common Issues

1. **"Object has been modified" errors**
   - Fixed by refreshing objects before updates
   - Status is always fetched fresh

2. **Mount failures**
   - Check mount options compatibility
   - Verify filesystem detection

3. **Snapshot stuck in Pending**
   - Check VolumeSnapshotContent exists
   - Verify VolumeSnapshotClass parameters

### Debug Logging

Enable verbose logging:

```go
// In your code
log.V(1).Info("Debug message", "key", value)
```

Set log level in deployment:

```yaml
args:
  - --v=1  # or higher for more verbose
```

## Performance Considerations

1. **Reduced API calls** - Status is fetched only when needed
2. **Better caching** - ResourceVersion tracking prevents stale updates
3. **Efficient reconciliation** - Early returns for completed operations
4. **Parallel operations** - Independent operations don't block each other

## Future Improvements

- [ ] Add metrics for each operation phase
- [ ] Implement retry with exponential backoff
- [ ] Add webhook validation
- [ ] Create comprehensive integration tests
- [ ] Add performance benchmarks

## Contributing

When modifying the controller:

1. Keep handlers focused on single responsibility
2. Add logging for important operations
3. Handle errors gracefully with proper status updates
4. Write tests for new functionality
5. Update this documentation

## References

- [Kubernetes Controller Best Practices](https://kubernetes.io/docs/concepts/architecture/controller/)
- [Controller Runtime](https://github.com/kubernetes-sigs/controller-runtime)
- [Writing Controllers](https://book.kubebuilder.io/cronjob-tutorial/controller-implementation.html)

