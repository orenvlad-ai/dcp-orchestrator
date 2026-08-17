package ports

import (
	"context"
	"errors"
)

var (
	ErrSCMReleaseIdentityChanged = errors.New("scm: release identity changed")
	ErrSCMReleaseStateInvalid    = errors.New("scm: release state invalid")
)

// SCMReleaseReadyRequest is the sole DCP mutation permitted for a repository
// whose physical merge actor is a GitHub Actions Release Train.
type SCMReleaseReadyRequest struct {
	PR                 SCMPRRef
	ExpectedHeadSHA    string
	ExpectedBaseBranch string
	RequiredTaskLabel  string
	RequiredScopeLabel string
}

// SCMReleaseObservation is the exact provider-owned release state. It is read
// directly from the pull request so label/body facts are not inferred from the
// ordinary AO persistence projection.
type SCMReleaseObservation struct {
	Number         int
	URL            string
	State          string
	Draft          bool
	Merged         bool
	HeadRepository string
	HeadBranch     string
	HeadSHA        string
	BaseBranch     string
	Author         string
	MergeCommitSHA string
	Labels         []string
	Body           string
}

// SCMReleaseTrain applies only the release-ready handoff and observes terminal
// facts. It has no merge method.
type SCMReleaseTrain interface {
	ApplyReleaseReady(context.Context, SCMReleaseReadyRequest) error
	ObserveRelease(context.Context, SCMPRRef) (SCMReleaseObservation, error)
}
