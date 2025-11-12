package v1

type SnapshotPhase string

const (
	SnapshotPending   SnapshotPhase = "Pending"
	SnapshotRunning   SnapshotPhase = "Running"
	SnapshotSucceeded SnapshotPhase = "Succeeded"
	SnapshotFailed    SnapshotPhase = "Failed"
)

type StorageProvider string

const (
	ProviderLocal StorageProvider = "local"
	ProviderS3    StorageProvider = "s3"
	ProviderGCS   StorageProvider = "gcs"
	ProviderAzure StorageProvider = "azure"
	ProviderB2    StorageProvider = "b2"
	ProviderSwift StorageProvider = "swift"
	ProviderRest  StorageProvider = "rest"
)

type BackupEngine string

const (
	EngineRestic BackupEngine = "restic"
	EngineKopia  BackupEngine = "kopia"
)

const (
	// PhaseReady Phase constants for OnlineSnapshotStorage
	PhaseReady = "Ready"
	PhaseError = "Error"
)
