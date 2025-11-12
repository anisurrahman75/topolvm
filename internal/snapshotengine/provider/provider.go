package provider

import (
	"context"
	"time"
)

// RepoParam includes the parameters to manipulate a backup repository
type RepoParam struct {
	Repository string
	Hostname   string
}

// BackupParam includes parameters for backup operations
type BackupParam struct {
	RepoParam
	BackupPaths []string
	Exclude     []string
	Args        []string
}

// RestoreParam includes parameters for restore operations
type RestoreParam struct {
	RepoParam
	SnapshotID   string
	RestorePaths []string
	Destination  string
	Exclude      []string
	Include      []string
	Args         []string
}

// SnapshotInfo contains information about a snapshot
type SnapshotInfo struct {
	ID       string
	Time     time.Time
	Hostname string
	Paths    []string
	Tags     []string
}

// RepositoryStats contains statistics about the repository
type RepositoryStats struct {
	Integrity                     *bool
	Size                          string
	SnapshotCount                 int64
	SnapshotsRemovedOnLastCleanup int64
}

// BackupResult contains the result of a backup operation
// This is a generic structure that works for both Restic and Kopia
type BackupResult struct {
	// SnapshotID is the unique identifier of the created snapshot
	SnapshotID string `json:"snapshotID"`

	// Repository is the backup repository URL/path
	Repository string `json:"repository,omitempty"`

	// BackupTime is when the backup was taken
	BackupTime time.Time `json:"backupTime,omitempty"`

	// Size contains size information about the backup
	Size BackupSizeInfo `json:"size,omitempty"`

	// Files contains file statistics
	Files BackupFileInfo `json:"files,omitempty"`

	// Duration is how long the backup took
	Duration string `json:"duration,omitempty"`

	// Phase indicates success or failure
	Phase BackupPhase `json:"phase"`

	// ErrorMessage contains error details if backup failed
	ErrorMessage string `json:"errorMessage,omitempty"`

	// Hostname is the hostname used for the backup
	Hostname string `json:"hostname,omitempty"`

	// Paths are the paths that were backed up
	Paths []string `json:"paths,omitempty"`

	// Provider identifies the backup engine used (restic, kopia)
	Provider string `json:"provider,omitempty"`

	// Version is the version of the backup tool used
	Version string `json:"version,omitempty"`
}

// BackupPhase represents the phase of backup operation
type BackupPhase string

const (
	BackupPhaseSucceeded BackupPhase = "Succeeded"
	BackupPhaseFailed    BackupPhase = "Failed"
)

// BackupSizeInfo contains size-related statistics
type BackupSizeInfo struct {
	// TotalBytes is the total size of data processed
	TotalBytes int64 `json:"totalBytes,omitempty"`

	// UploadedBytes is the amount of data actually uploaded (may be less due to deduplication)
	UploadedBytes int64 `json:"uploadedBytes,omitempty"`

	// TotalFormatted is human-readable total size (e.g., "1.5 GiB")
	TotalFormatted string `json:"totalFormatted,omitempty"`

	// UploadedFormatted is human-readable uploaded size
	UploadedFormatted string `json:"uploadedFormatted,omitempty"`
}

// BackupFileInfo contains file-related statistics
type BackupFileInfo struct {
	// Total is the total number of files processed
	Total int64 `json:"total,omitempty"`

	// New is the number of new files
	New int64 `json:"new,omitempty"`

	// Modified is the number of modified files
	Modified int64 `json:"modified,omitempty"`

	// Unmodified is the number of unchanged files
	Unmodified int64 `json:"unmodified,omitempty"`
}

// Provider defines the common interface for snapshot engines (restic, kopia, etc.)
type Provider interface {
	// ValidateConnection checks if the connection to the repository is valid
	ValidateConnection(ctx context.Context) error

	// Backup creates a new snapshot and returns detailed backup result
	Backup(ctx context.Context, param BackupParam) (*BackupResult, error)

	// Backup // InitRepo initializes a new repository in the storage backend
	//InitRepo(ctx context.Context, param RepoParam) error
	//
	//// ConnectToRepo establishes connection to an existing repository
	//ConnectToRepo(ctx context.Context, param RepoParam) error
	//
	//// PrepareRepo combines InitRepo and ConnectToRepo - initializes if needed, connects otherwise
	//PrepareRepo(ctx context.Context, param RepoParam) error
	//
	//// BoostRepoConnect re-ensures local connection to the repo (useful after pod restarts)
	//BoostRepoConnect(ctx context.Context, param RepoParam) error
	//
	//// EnsureUnlockRepo removes any stale file locks in the storage
	//EnsureUnlockRepo(ctx context.Context, param RepoParam) error
	//
	//// PruneRepo performs full maintenance/pruning of the repository
	//PruneRepo(ctx context.Context, param RepoParam) error

	//
	//// Restore restores files from a snapshot
	//Restore(ctx context.Context, param RestoreParam) error
	//
	//// Snapshot Management
	//
	//// ListSnapshots lists all snapshots in the repository
	//ListSnapshots(ctx context.Context, param RepoParam) ([]SnapshotInfo, error)
	//
	//// DeleteSnapshot deletes a specific snapshot by ID
	//DeleteSnapshot(ctx context.Context, snapshotID string, param RepoParam) error
	//
	//// Forget removes a snapshot from the repository (alias for DeleteSnapshot)
	//Forget(ctx context.Context, snapshotID string, param RepoParam) error
	//
	//// BatchForget removes multiple snapshots
	//BatchForget(ctx context.Context, snapshotIDs []string, param RepoParam) []error
	//
	//// CheckRepository verifies the repository integrity
	//CheckRepository(ctx context.Context, param RepoParam) error
	//
	//// Stats & Maintenance
	//
	//// DefaultMaintenanceFrequency returns the default frequency to run maintenance
	//DefaultMaintenanceFrequency(ctx context.Context, param RepoParam) time.Duration
	//
	//// GetRepositoryStats returns statistics about the repository
	//GetRepositoryStats(ctx context.Context, param RepoParam) (*RepositoryStats, error)
}
