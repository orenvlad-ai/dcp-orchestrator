package domain

import "time"

// DCPCard12ColdStartRecovery is the separately authorized, daemon-owned
// continuation after the immutable card-12 identity_drift failure. It owns no
// worker/arbiter budget, may perform one model-free Git action, and may fence
// at most one exact-head reviewer.
type DCPCard12ColdStartRecovery struct {
	RecoveryID, IdentityDigest, ContractCommit string
	Generation                                 int64
	PredecessorContinuationID                  string
	IncidentID, AdmissionID                    string
	SessionID                                  SessionID
	TaskID, ProjectID, Repository              string
	WorktreePath, SourceBranch, PRURL          string
	PRNumber                                   int64
	OldHead, CurrentMain, ProviderBase         string
	ConflictPath, MarkerDigest, StatusDigest   string
	Stage1Blob, Stage2Blob, Stage3Blob         string
	ResolvedBytesDigest, ResolvedBlob          string
	PushRef, PushLeaseOldHead                  string
	UnauthorizedWorkerThread11                 string
	UnauthorizedWorkerThread12                 string
	UnauthorizedWorkerTokens11                 int64
	UnauthorizedWorkerTokens12                 int64
	WorkerModelCallCount                       int64
	ArbiterModelCallCount                      int64
	ModelFreeActionCount                       int64
	ReviewerModelCallCount                     int64
	BackupPath, BackupDigest                   string
	LocalRefBefore, LocalRefAfter              string
	NewHead, NewCommit, ProviderNewHead        string
	RecoveryReviewRunID, RecoveryReviewID      string
	RecoveryReviewBatchID, RecoveryCheckID     string
	MergeCommitSHA                             string
	Status                                     DCPCard12ColdStartRecoveryStatus
	Revision                                   int64
	ErrorCode                                  string
	AuthorizedAt, UpdatedAt                    time.Time
	FinishedAt                                 *time.Time
}

type DCPCard12ColdStartRecoveryStatus string

const (
	DCPColdStartRecoveryAuthorized       DCPCard12ColdStartRecoveryStatus = "authorized"
	DCPColdStartRecoveryBackedUp         DCPCard12ColdStartRecoveryStatus = "backed_up"
	DCPColdStartRecoveryRunning          DCPCard12ColdStartRecoveryStatus = "running"
	DCPColdStartRecoveryCandidateReady   DCPCard12ColdStartRecoveryStatus = "candidate_ready"
	DCPColdStartRecoveryReviewRunning    DCPCard12ColdStartRecoveryStatus = "review_running"
	DCPColdStartRecoveryRecoveryReviewed DCPCard12ColdStartRecoveryStatus = "recovery_reviewed"
	DCPColdStartRecoverySucceeded        DCPCard12ColdStartRecoveryStatus = "succeeded"
	DCPColdStartRecoveryFailed           DCPCard12ColdStartRecoveryStatus = "failed"
)
