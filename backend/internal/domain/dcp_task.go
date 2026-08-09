package domain

import "time"

// DCPTaskID is a durable DCP task identity. It is deliberately distinct from
// SessionID: an I11 task has no worker, runtime, worktree, or AO session.
type DCPTaskID string

// DCPTaskState is the persisted DCP lifecycle state. I11 implements exactly
// one state and no transition that starts work.
type DCPTaskState string

const (
	DCPTaskSubmitted DCPTaskState = "SUBMITTED"
)

// DCPApprovedTask is the compact immutable task representation accepted by
// the model-free I11 lab surface.
type DCPApprovedTask struct {
	SchemaVersion string `json:"schemaVersion" enum:"dcp.task/v1"`
	Title         string `json:"title"`
	Description   string `json:"description"`
}

// DCPApprovedScope is the compact immutable approved-scope representation.
// It is plain owner-approved text, not a chat or model transcript.
type DCPApprovedScope struct {
	SchemaVersion string `json:"schemaVersion" enum:"dcp.scope/v1"`
	Statement     string `json:"statement"`
}

// DCPRepositoryIdentity is the exact model-free snapshot of the only target
// repository authorized in I11.
type DCPRepositoryIdentity struct {
	SchemaVersion  string `json:"schemaVersion" enum:"dcp.repository/v1"`
	ProjectID      string `json:"projectId" enum:"dcp-lab"`
	Repository     string `json:"repository" enum:"dcp-lab"`
	Path           string `json:"path"`
	HeadSHA        string `json:"headSha"`
	MarkerDigest   string `json:"markerDigest"`
	IdentityDigest string `json:"identityDigest"`
}

// DCPTask is the durable I11 read model.
type DCPTask struct {
	ID             DCPTaskID             `json:"taskId"`
	IdempotencyKey string                `json:"idempotencyKey"`
	ApprovedTask   DCPApprovedTask       `json:"approvedTask"`
	ApprovedScope  DCPApprovedScope      `json:"approvedScope"`
	ApprovedDigest string                `json:"approvedDigest"`
	Target         DCPRepositoryIdentity `json:"target"`
	State          DCPTaskState          `json:"state" enum:"SUBMITTED"`
	Revision       int64                 `json:"revision"`
	CreatedAt      time.Time             `json:"createdAt"`
	UpdatedAt      time.Time             `json:"updatedAt"`
}

// DCPTaskEvent is one immutable entry in a per-task sequence.
type DCPTaskEvent struct {
	TaskID          DCPTaskID     `json:"taskId"`
	Sequence        int64         `json:"sequence"`
	EventID         string        `json:"eventId"`
	SchemaVersion   string        `json:"schemaVersion"`
	EventType       string        `json:"eventType"`
	SourceKind      string        `json:"sourceKind"`
	SourceID        string        `json:"sourceId"`
	CorrelationID   string        `json:"correlationId"`
	CausationID     string        `json:"causationId,omitempty"`
	IdempotencyKey  string        `json:"idempotencyKey"`
	FromState       *DCPTaskState `json:"fromState,omitempty"`
	ToState         DCPTaskState  `json:"toState"`
	TaskRevision    int64         `json:"taskRevision"`
	OccurredAt      time.Time     `json:"occurredAt"`
	RecordedAt      time.Time     `json:"recordedAt"`
	Payload         string        `json:"payload"`
	EvidenceDigest  string        `json:"evidenceDigest"`
	IntegrityDigest string        `json:"integrityDigest"`
}
