package domain

import "time"

const (
	// DCPFutureArbiterModel is the immutable ordinary-card arbiter model.
	DCPFutureArbiterModel = "gpt-5.6-sol"
	// DCPFutureArbiterReasoning is the immutable reasoning effort.
	DCPFutureArbiterReasoning = "xhigh"
	// DCPFutureArbiterTokenBudget is the hard one-call rollout ceiling.
	DCPFutureArbiterTokenBudget = int64(16384)
)

// DCPFutureArbiterIncident is one immutable ordinary-card incident generation
// plus its one-shot action/result lifecycle. It is subordinate to the existing
// policy task and admission; it is not another task registry or scheduler.
type DCPFutureArbiterIncident struct {
	IncidentID          string
	Generation          int64
	IdentityDigest      string
	TaskID              string
	SessionID           SessionID
	AdmissionID         string
	AdmissionSequence   int64
	IncidentLeaseID     string
	IncidentKind        string
	SourcePacketJSON    string
	SourcePacketDigest  string
	PRURL               string
	PRNumber            int64
	CandidateHeadSHA    string
	ReviewedBaseSHA     string
	CurrentMainSHA      string
	ReviewRunID         string
	AffectedPathsJSON   string
	CohortJSON          string
	CohortDigest        string
	EvidenceJSON        string
	EvidenceDigest      string
	InputJSON           string
	InputDigest         string
	ModelActionID       string
	RuntimeHandleID     string
	Status              DCPFutureArbiterStatus
	ModelCallCount      int64
	DecisionJSON        string
	DecisionDigest      string
	Verdict             DCPFutureArbiterVerdict
	OrderJSON           string
	RepairTaskID        string
	RepairObjective     string
	RepairPathsJSON     string
	HumanQuestion       string
	RepairActionID      string
	RecoveryReviewRunID string
	RecoveryHeadSHA     string
	MergeCommitSHA      string
	ErrorCode           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	DecisionAt          *time.Time
	FinishedAt          *time.Time
}

// DCPFutureArbiterSchemaRecovery is the one exact additive authorization for
// a provider-rejected pre-inference schema generation. It preserves the
// terminal predecessor and cannot become a general retry policy.
type DCPFutureArbiterSchemaRecovery struct {
	RecoveryID                string
	PredecessorIncidentID     string
	PredecessorIdentityDigest string
	PredecessorInputDigest    string
	PredecessorModelActionID  string
	PredecessorSchemaDigest   string
	ProviderErrorJSON         string
	ProviderErrorDigest       string
	ProviderInferenceTokens   int64
	SuccessorGeneration       int64
	Status                    string
	SuccessorIncidentID       string
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
	ConsumedAt                *time.Time
}

// DCPFutureArbiterResultRecovery is the immutable failure audit and one-way
// model-free validation grant for one already-produced exact result. It never
// authorizes another incident generation or model call.
type DCPFutureArbiterResultRecovery struct {
	RecoveryID            string
	IncidentID            string
	IdentityDigest        string
	InputDigest           string
	ModelActionID         string
	PriorStatus           string
	PriorErrorCode        string
	PriorFinishedAt       time.Time
	PriorModelCallCount   int64
	PriorDecisionDigest   string
	RuntimeHandleID       string
	PhysicalRuntimeHandle string
	InputArtifactDigest   string
	InputArtifactSize     int64
	SchemaArtifactDigest  string
	SchemaArtifactSize    int64
	ResultArtifactDigest  string
	ResultArtifactSize    int64
	CodexSessionID        string
	InferenceTokens       int64
	ContractCommit        string
	Status                string
	ErrorCode             string
	CreatedAt             time.Time
	FinishedAt            *time.Time
}

// DCPFutureArbiterStatus is the durable lifecycle of one exact incident generation.
type DCPFutureArbiterStatus string

const (
	// DCPFutureArbiterRequested is the initial durable queued state.
	DCPFutureArbiterRequested DCPFutureArbiterStatus = "requested"
	// DCPFutureArbiterClaimed holds one slot before the call fence.
	DCPFutureArbiterClaimed DCPFutureArbiterStatus = "claimed"
	// DCPFutureArbiterRunning marks the sole fenced call in progress.
	DCPFutureArbiterRunning DCPFutureArbiterStatus = "running"
	// DCPFutureArbiterHold is a passive zero-token ordering hold.
	DCPFutureArbiterHold DCPFutureArbiterStatus = "hold"
	// DCPFutureArbiterRepairQueued queues the one bounded successor repair.
	DCPFutureArbiterRepairQueued DCPFutureArbiterStatus = "repair_queued"
	// DCPFutureArbiterRecoveryReviewed binds the fresh reviewed successor.
	DCPFutureArbiterRecoveryReviewed DCPFutureArbiterStatus = "recovery_reviewed"
	// DCPFutureArbiterHumanGate fails closed on ambiguous owner intent.
	DCPFutureArbiterHumanGate DCPFutureArbiterStatus = "human_gate"
	// DCPFutureArbiterSucceeded marks a trusted terminal merge.
	DCPFutureArbiterSucceeded DCPFutureArbiterStatus = "succeeded"
	// DCPFutureArbiterFailed closes a malformed, stale, or failed generation.
	DCPFutureArbiterFailed DCPFutureArbiterStatus = "failed"
)

// DCPFutureArbiterVerdict is the bounded structured decision class.
type DCPFutureArbiterVerdict string

const (
	// DCPFutureVerdictOrderHold keeps the cohort passive until exact ordering advances.
	DCPFutureVerdictOrderHold DCPFutureArbiterVerdict = "deterministic_order_hold"
	// DCPFutureVerdictRepair permits one exact bounded successor repair.
	DCPFutureVerdictRepair DCPFutureArbiterVerdict = "successor_repair"
	// DCPFutureVerdictHumanGate requires a short owner question without mutation.
	DCPFutureVerdictHumanGate DCPFutureArbiterVerdict = "human_gate"
)
