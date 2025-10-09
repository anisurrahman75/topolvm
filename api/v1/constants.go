package v1

type SnapshotPhase string

const (
	SnapshotPending   SnapshotPhase = "Pending"
	SnapshotRunning   SnapshotPhase = "Running"
	SnapshotSucceeded SnapshotPhase = "Succeeded"
	SnapshotFailed    SnapshotPhase = "Failed"
)
