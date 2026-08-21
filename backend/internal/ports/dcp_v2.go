package ports

import (
	"context"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// DCPV2ModelRunner is stateless transport behind the DCP-owned lifecycle. It
// receives a pre-fenced runtime identity and cannot access SQLite or decide a
// Task transition.
type DCPV2ModelRunner interface {
	Prepare(context.Context, DCPV2ModelPrepareRequest) (DCPV2ModelWorkspaceReceipt, error)
	Launch(context.Context, DCPV2ModelLaunchRequest) (DCPV2ModelLaunchReceipt, error)
	Observe(context.Context, DCPV2ModelLaunchRequest) (domain.DCPV2RuntimeObservation, error)
	Terminal(context.Context, DCPV2ModelLaunchRequest) (domain.DCPV2ModelTerminalReceipt, bool, error)
}

type DCPV2ModelPrepareRequest struct {
	TaskID, RevisionID, CommandID, ActionID                        string
	Repository, RepositoryPath, BaseRef, BaseSHA, HeadRef, HeadSHA string
	Role                                                           domain.DCPV2ActionRole
}

type DCPV2ModelWorkspaceReceipt struct {
	Branch, Worktree, WorktreeDigest string
	ExpectedOldHead                  string
}

type DCPV2ModelLaunchRequest struct {
	TaskID, RevisionID, CommandID, ActionID string
	Role                                    domain.DCPV2ActionRole
	Attempt                                 int64
	Model, Reasoning                        string
	TokenBudget, TimeBudgetSec              int64
	InputDigest, PromptDigest               string
	TaskInputJSON, TaskInputDigest          string
	CommandPayloadJSON                      string
	Repository, BaseRef, BaseSHA            string
	HeadRef, HeadSHA, Branch                string
	Worktree, WorktreeDigest                string
	AllowedPaths                            []string
	LaunchFence, EffectFence, RuntimeID     string
	ExpectedOldHead                         string
}

type DCPV2ModelLaunchReceipt struct {
	ActionID, LaunchFence, RuntimeID string
	ProviderRequestID                string
	ProviderRequestDigest            string
	StartedAt                        time.Time
}

type DCPV2PublicationRequest struct {
	TaskID, RevisionID, CommandID string
	Repository, BaseRef, BaseSHA  string
	Branch, CommitSHA, TreeSHA    string
	Worktree, WorktreeDigest      string
	ExpectedOldHead, EffectFence  string
}

type DCPV2PublicationReceipt struct {
	ExternalID, Branch, CommitSHA, TreeSHA string
	PRNumber                               int64
	EvidenceDigest                         string
}

type DCPV2Publisher interface {
	Publish(context.Context, DCPV2PublicationRequest) (DCPV2PublicationReceipt, error)
	ReconcilePublication(context.Context, DCPV2PublicationRequest) (DCPV2PublicationReceipt, bool, error)
}

// DCPV2Repository is the provider-neutral repository/check/readmission seam.
// Implementations are added by separately reviewed target-adapter stages; the
// core owns no repository allowlist or live provider credential.
type DCPV2Repository interface {
	ObserveRevision(context.Context, domain.DCPV2Task, domain.DCPV2Revision) (DCPV2RepositoryObservation, error)
	MaterializeReadmission(context.Context, domain.DCPV2Task, domain.DCPV2Revision, string, string) (DCPV2RepositoryEffect, error)
}

type DCPV2RepositoryObservation struct {
	Repository      string
	BaseRef         string
	BaseSHA         string
	PRNumber        int64
	HeadSHA         string
	RequiredCheckID string
	RequiredCheckOK bool
	EvidenceDigest  string
}

type DCPV2RepositoryEffect struct {
	ExternalID     string
	OldHeadSHA     string
	NewHeadSHA     string
	BaseSHA        string
	EvidenceDigest string
}

// DCPV2Release dispatches one exact immutable Admission manifest and observes
// a repository-owned release proof. It deliberately exposes no direct merge
// method: target Release Trains remain the only physical merge actors.
type DCPV2Release interface {
	DispatchAdmission(context.Context, domain.DCPV2Task, domain.DCPV2Revision, domain.DCPV2Admission, string) (DCPV2ReleaseReceipt, error)
	ObserveRelease(context.Context, domain.DCPV2Task, domain.DCPV2Revision, domain.DCPV2Admission, int64) (DCPV2ReleaseObservation, error)
}

type DCPV2ReleaseReceipt struct {
	ExternalID     string
	Provider       string
	RunID          string
	Actor          string
	AdmissionID    string
	ManifestDigest string
	EvidenceDigest string
}

type DCPV2ReleaseObservation struct {
	ProofID        string
	Provider       string
	RunID          string
	Actor          string
	AdmissionID    string
	ManifestDigest string
	MergeSHA       string
	ArtifactDigest string
	Readmission    bool
	CurrentMainSHA string
	EvidenceDigest string
}

// DCPV2Deployment verifies an immutable target-owned deployment proof. The
// interface cannot install, restart, redeploy or select an alternate artifact.
type DCPV2Deployment interface {
	ObserveDeployment(context.Context, domain.DCPV2Task, domain.DCPV2Revision, domain.DCPV2Admission, string) (DCPV2DeploymentObservation, error)
}

type DCPV2DeploymentObservation struct {
	ProofID        string
	Provider       string
	RunID          string
	Actor          string
	AdmissionID    string
	ManifestDigest string
	MergeSHA       string
	ArtifactDigest string
	DeployedSHA    string
	Environment    string
	Service        string
	ProbeDigest    string
	EvidenceDigest string
	Succeeded      bool
	FailureCode    string
}
