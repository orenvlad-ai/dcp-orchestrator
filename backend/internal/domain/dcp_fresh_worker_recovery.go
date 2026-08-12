package domain

import "time"

// DCPCard12FreshWorkerRecovery is the one separately-audited stateless
// worker action authorized after the preserved native card-12 resume failed.
// It never replaces or repairs the native session or successor rows.
type DCPCard12FreshWorkerRecovery struct {
	RecoveryID, RecoveryIdentityDigest          string
	RecoveryGeneration                          int64
	IncidentID                                  string
	IncidentGeneration                          int64
	SuccessorAttemptID, SuccessorIdentityDigest string
	SuccessorAttemptGeneration                  int64
	AcceptedDecisionDigest, AdmissionID         string
	SessionID                                   SessionID
	TaskID, ProjectID, Repository, WorktreePath string
	SourceBranch, PRURL                         string
	PRNumber                                    int64
	OldHead, CurrentMain                        string
	PredecessorStatus, PredecessorError         string
	OldRuntimeHandleID, OldAgentSessionID       string
	OldRuntimeLaunchID, ContractCommit          string
	Model, Reasoning                            string
	TokenBudget, WorkerModelCallCount           int64
	ReviewerModelCallCount                      int64
	RuntimeActionID, RuntimeHandleID, LaunchID  string
	InputJSON, InputDigest, InputPath           string
	ResultPath, LogPath                         string
	WorkerCodexSessionID                        string
	WorkerTokenCount                            int64
	WorkerResultDigest, WorkerLogDigest         string
	NewHead, NewCommit                          string
	RecoveryReviewRunID, RecoveryReviewID       string
	RecoveryReviewBatchID, RecoveryCheckID      string
	MergeCommitSHA                              string
	Status                                      DCPCard12FreshWorkerRecoveryStatus
	Revision                                    int64
	ErrorCode                                   string
	AuthorizedAt, UpdatedAt                     time.Time
	FinishedAt                                  *time.Time
}

type DCPCard12FreshWorkerRecoveryStatus string

const (
	DCPFreshWorkerAuthorized       DCPCard12FreshWorkerRecoveryStatus = "authorized"
	DCPFreshWorkerRequested        DCPCard12FreshWorkerRecoveryStatus = "requested"
	DCPFreshWorkerPreflightFailed  DCPCard12FreshWorkerRecoveryStatus = "preflight_failed"
	DCPFreshWorkerRunning          DCPCard12FreshWorkerRecoveryStatus = "running"
	DCPFreshWorkerSucceeded        DCPCard12FreshWorkerRecoveryStatus = "worker_succeeded"
	DCPFreshReviewerRunning        DCPCard12FreshWorkerRecoveryStatus = "review_running"
	DCPFreshWorkerRecoveryReviewed DCPCard12FreshWorkerRecoveryStatus = "recovery_reviewed"
	DCPFreshWorkerComplete         DCPCard12FreshWorkerRecoveryStatus = "succeeded"
	DCPFreshWorkerFailed           DCPCard12FreshWorkerRecoveryStatus = "failed"
)
