package domain

import "time"

// DCPReleaseArbiterIncident is the single bounded I13 Stage 2 incident/action
// record. It is subordinate to an existing admission incident and never acts
// as a task, card, queue, scheduler, or general arbitration registry.
type DCPReleaseArbiterIncident struct {
	IncidentID             string
	Generation             int64
	IdentityDigest         string
	AdmissionID            string
	IncidentLeaseID        string
	SourcePacketJSON       string
	SourcePacketDigest     string
	InputJSON              string
	InputDigest            string
	TaskID                 string
	SessionID              SessionID
	WorktreePath           string
	SourceBranch           string
	PRURL                  string
	PRNumber               int64
	TargetSHA              string
	ReviewedBaseSHA        string
	CurrentBaseSHA         string
	ReviewID               string
	ReviewRunID            string
	BatchID                string
	ScopeDigest            string
	HistoryDigest          string
	DiffDigest             string
	CheckSetDigest         string
	ReviewSetDigest        string
	FrozenQueueDigest      string
	MechanicalDigest       string
	Model                  string
	Reasoning              string
	TokenBudget            int64
	RuntimeHandleID        string
	LaunchID               string
	Status                 DCPReleaseArbiterStatus
	ModelCallCount         int64
	DecisionJSON           string
	DecisionDigest         string
	RecoveryOwnerSessionID SessionID
	RecoveryPath           string
	RecoveryWakeCount      int64
	RecoveryReviewRunID    string
	RecoveryTargetSHA      string
	ErrorCode              string
	CreatedAt              time.Time
	UpdatedAt              time.Time
	DecisionAt             *time.Time
	FinishedAt             *time.Time
}

type DCPReleaseArbiterStatus string

const (
	DCPArbiterRequested        DCPReleaseArbiterStatus = "requested"
	DCPArbiterPreflightFailed  DCPReleaseArbiterStatus = "preflight_failed"
	DCPArbiterRunning          DCPReleaseArbiterStatus = "running"
	DCPArbiterDecided          DCPReleaseArbiterStatus = "decided"
	DCPArbiterSafeStopped      DCPReleaseArbiterStatus = "safe_stopped"
	DCPArbiterRepairing        DCPReleaseArbiterStatus = "repairing"
	DCPArbiterRecoveryReviewed DCPReleaseArbiterStatus = "recovery_reviewed"
	DCPArbiterSucceeded        DCPReleaseArbiterStatus = "succeeded"
	DCPArbiterFailed           DCPReleaseArbiterStatus = "failed"
)
