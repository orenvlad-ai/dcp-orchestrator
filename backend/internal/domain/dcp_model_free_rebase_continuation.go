package domain

import "time"

// DCPCard12ModelFreeRebaseContinuation is the one exact, daemon-owned Git
// continuation subordinate to the immutable failed card-12 fresh-worker row.
// It owns no worker or arbiter model budget and may fence one fresh review.
type DCPCard12ModelFreeRebaseContinuation struct {
	ContinuationID, IdentityDigest, ContractCommit string
	Generation                                     int64
	PredecessorRecoveryID, IncidentID, AdmissionID string
	SessionID                                      SessionID
	TaskID, ProjectID, Repository, WorktreePath    string
	SourceBranch, PRURL                            string
	PRNumber                                       int64
	OldHead, CurrentMain                           string
	PredecessorInputDigest                         string
	InputArtifactDigest, ResultArtifactDigest      string
	LogArtifactDigest, RebaseMetadataDigest        string
	ResolvedBytesDigest                            string
	ModelFreeActionCount, ReviewerModelCallCount   int64
	LocalRefBefore, LocalRefAfter                  string
	PushRef, PushLeaseOldHead                      string
	NewHead, NewCommit, ProviderNewHead            string
	RecoveryReviewRunID, RecoveryReviewID          string
	RecoveryReviewBatchID, RecoveryCheckID         string
	MergeCommitSHA                                 string
	Status                                         DCPCard12ModelFreeRebaseContinuationStatus
	Revision                                       int64
	ErrorCode                                      string
	AuthorizedAt, UpdatedAt                        time.Time
	FinishedAt                                     *time.Time
}

type DCPCard12ModelFreeRebaseContinuationStatus string

const (
	DCPModelFreeRebaseAuthorized       DCPCard12ModelFreeRebaseContinuationStatus = "authorized"
	DCPModelFreeRebaseRunning          DCPCard12ModelFreeRebaseContinuationStatus = "running"
	DCPModelFreeRebaseCandidateReady   DCPCard12ModelFreeRebaseContinuationStatus = "candidate_ready"
	DCPModelFreeRebaseReviewRunning    DCPCard12ModelFreeRebaseContinuationStatus = "review_running"
	DCPModelFreeRebaseRecoveryReviewed DCPCard12ModelFreeRebaseContinuationStatus = "recovery_reviewed"
	DCPModelFreeRebaseSucceeded        DCPCard12ModelFreeRebaseContinuationStatus = "succeeded"
	DCPModelFreeRebaseFailed           DCPCard12ModelFreeRebaseContinuationStatus = "failed"
)
