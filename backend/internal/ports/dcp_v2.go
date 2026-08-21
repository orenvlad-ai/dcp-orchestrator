package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

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
