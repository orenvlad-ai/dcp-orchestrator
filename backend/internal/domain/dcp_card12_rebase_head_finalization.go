package domain

import "time"

// DCPCard12RebaseHeadFinalization is the additive, daemon-owned successor to
// the consumed cold-start recovery. It may adopt one exact retained candidate,
// push it once, and fence at most one fresh reviewer. It owns no worker or
// arbiter budget and performs no local Git write.
type DCPCard12RebaseHeadFinalization struct {
	FinalizationID, IdentityDigest, ContractCommit string
	Generation                                     int64
	PredecessorRecoveryID, IncidentID, AdmissionID string
	SessionID                                      SessionID
	TaskID, ProjectID, Repository                  string
	WorktreePath, SourceBranch, PRURL              string
	PRNumber                                       int64
	OldHead, CandidateHead, CurrentMain            string
	ProviderBase, ConflictPath                     string
	ResolvedBytesDigest, ResolvedBlob              string
	CandidateDiffDigest, CleanStatusDigest         string
	RebaseHeadDigest, OrigHeadDigest               string
	BackupPath, BackupDigest                       string
	PushRef, PushLeaseOldHead, ProviderNewHead     string
	UnauthorizedWorkerTokens11                     int64
	UnauthorizedWorkerTokens12                     int64
	WorkerModelCallCount, ArbiterModelCallCount    int64
	ModelFreeActionCount, ReviewerModelCallCount   int64
	ReviewRunID, ReviewID, ReviewBatchID, CheckID  string
	MergeCommitSHA                                 string
	Status                                         DCPCard12RebaseHeadFinalizationStatus
	Revision                                       int64
	ErrorCode                                      string
	AuthorizedAt, UpdatedAt                        time.Time
	FinishedAt                                     *time.Time
}

type DCPCard12RebaseHeadFinalizationStatus string

const (
	DCPRebaseHeadFinalizationAuthorized       DCPCard12RebaseHeadFinalizationStatus = "authorized"
	DCPRebaseHeadFinalizationRunning          DCPCard12RebaseHeadFinalizationStatus = "running"
	DCPRebaseHeadFinalizationCandidateReady   DCPCard12RebaseHeadFinalizationStatus = "candidate_ready"
	DCPRebaseHeadFinalizationReviewRunning    DCPCard12RebaseHeadFinalizationStatus = "review_running"
	DCPRebaseHeadFinalizationRecoveryReviewed DCPCard12RebaseHeadFinalizationStatus = "recovery_reviewed"
	DCPRebaseHeadFinalizationSucceeded        DCPCard12RebaseHeadFinalizationStatus = "succeeded"
	DCPRebaseHeadFinalizationFailed           DCPCard12RebaseHeadFinalizationStatus = "failed"
)
