// Package dcpterminalmerge owns the bounded I13 mechanical Admission
// Controller for the synthetic DCP review lab. It extends the historical
// exact-head terminal merge without becoming a general auto-merge policy:
// native cards and ReviewRuns keep their identity, SQLite owns one FIFO lease,
// and every repository/PR/head/review/check/provider fact is fail-closed.
package dcpterminalmerge

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	ProjectID          = "dcp-review-lab"
	SessionPrefix      = "dcp-review-lab"
	ProfileAgentRules  = "DCP synthetic PR profile v3. Work only in this exact synthetic repository and the current AO branch. Do not create subagents, extra branches, worktrees, remotes, additional pull requests, or network services. On the initial call implement only the direct task, create one commit, push the current branch, open one ready pull request targeting main, and then stop. If the trusted DCP daemon issues the single bounded admission-refresh continuation, rebase only onto the exact named origin/main and abort without push on any conflict or ambiguity. Only for native cards 11/12, if the trusted daemon supplies the exact I13 arbiter recovery identity, approved scope digest, old head, current main and conflict path, resolve only that one conflict within the original task, keep the same branch and pull request, run the check, push with exact force-with-lease, and stop. Do not merge; only the trusted DCP daemon may perform terminal merge after fresh exact-head review, checks, and admission."
	TaskDisplayPrefix  = "DCP:"
	TaskPromptPrefix   = "DCP synthetic task "
	RepositoryFullName = "orenvlad-ai/dcp-review-lab"
	RepositoryURL      = "https://github.com/orenvlad-ai/dcp-review-lab.git"
	TargetBranch       = "main"
	RequiredCheckName  = "dcp-review-lab"
	HistoricalSession  = "dcp-review-lab-7"
	AdmissionSessionA  = "dcp-review-lab-9"
	AdmissionSessionB  = "dcp-review-lab-10"
	structuredChannel  = "structured_dcp_v1"
)

var (
	errCanonicalDiverged  = errors.New("dcp admission: canonical main cannot fast-forward to provider base")
	errCanonicalBaseDrift = errors.New("dcp admission: provider and fetched main differ")
)

type Store interface {
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	ListAllSessions(context.Context) ([]domain.SessionRecord, error)
	GetProject(context.Context, string) (domain.ProjectRecord, bool, error)
	ListPRsBySession(context.Context, domain.SessionID) ([]domain.PullRequest, error)
	ListReviewRunsBySession(context.Context, domain.SessionID) ([]domain.ReviewRun, error)
	EnqueueDCPReviewLabAdmission(context.Context, domain.DCPReviewLabAdmission) (domain.DCPReviewLabAdmission, bool, error)
	GetDCPReviewLabAdmissionByRun(context.Context, string) (domain.DCPReviewLabAdmission, bool, error)
	GetClaimedDCPReviewLabAdmission(context.Context) (domain.DCPReviewLabAdmission, bool, error)
	ListDCPReviewLabAdmissions(context.Context) ([]domain.DCPReviewLabAdmission, error)
	GetRefreshingDCPReviewLabAdmissionBySession(context.Context, domain.SessionID) (domain.DCPReviewLabAdmission, bool, error)
	RecoverDCPReviewLabCanonicalBaseIncident(context.Context, domain.DCPReviewLabAdmission, time.Time) (bool, error)
	ResumeDCPReviewLabAdmissionAfterRefresh(context.Context, domain.DCPReviewLabAdmission, domain.ReviewRun, string, time.Time) (bool, error)
	ClaimDCPReviewLabAdmission(context.Context, domain.DCPReviewLabAdmission, string, string, time.Time) (bool, error)
	CompleteDCPReviewLabAdmission(context.Context, domain.DCPReviewLabAdmission, string, time.Time) (bool, error)
	FailDCPReviewLabAdmission(context.Context, domain.DCPReviewLabAdmission, string, time.Time) (bool, error)
	StartDCPReviewLabRefresh(context.Context, domain.DCPReviewLabAdmission, string, string, time.Time) (bool, error)
	RecordDCPReviewLabIncident(context.Context, domain.DCPReviewLabAdmission, string, string, string, string, time.Time) (bool, error)
}

type SCM interface {
	FetchPullRequests(context.Context, []ports.SCMPRRef) ([]ports.SCMObservation, error)
	FetchReviewThreads(context.Context, ports.SCMPRRef) (ports.SCMReviewObservation, error)
	MergePullRequest(context.Context, ports.SCMMergeRequest) (ports.SCMMergeResult, error)
}

type RefreshWaker func(context.Context, domain.SessionID, string) error

type Engine struct {
	store   Store
	scm     SCM
	dataDir string
	mu      sync.Mutex
	git     func(context.Context, string, ...string) (string, error)
	wake    RefreshWaker
	arbiter ArbiterLauncher
	clock   func() time.Time
}

func New(store Store, scm SCM, dataDir string) *Engine {
	return &Engine{
		store: store, scm: scm, dataDir: filepath.Clean(dataDir),
		git: gitOutput, clock: func() time.Time { return time.Now().UTC() },
	}
}

func (e *Engine) SetRefreshWaker(wake RefreshWaker) { e.wake = wake }

// ReconcileStartup first fences or completes the one persisted merge owner,
// then deterministically enrols exact approved sessions and drains at most the
// bounded FIFO. It starts no model merely to discover state.
func (e *Engine) ReconcileStartup(ctx context.Context) error {
	if err := e.configured(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if claimed, ok, err := e.store.GetClaimedDCPReviewLabAdmission(ctx); err != nil {
		return err
	} else if ok {
		continued, reconcileErr := e.reconcileClaimed(ctx, claimed)
		if reconcileErr != nil || !continued {
			return reconcileErr
		}
	}
	if err := e.recoverCanonicalBaseIncidents(ctx); err != nil {
		return err
	}
	if err := e.reconcileStage2Arbiter(ctx); err != nil {
		return err
	}

	sessions, err := e.store.ListAllSessions(ctx)
	if err != nil {
		return err
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].CreatedAt.Equal(sessions[j].CreatedAt) {
			return sessions[i].ID < sessions[j].ID
		}
		return sessions[i].CreatedAt.Before(sessions[j].CreatedAt)
	})
	for _, session := range sessions {
		if eligibleSessionID(session.ID) {
			if err := e.enrol(ctx, session.ID); err != nil {
				return err
			}
		}
	}
	return e.drain(ctx)
}

// recoverCanonicalBaseIncidents is a one-shot startup repair for the exact
// false-positive produced when the first cohort merge advanced origin/main
// while the provider still reported the reviewed base SHA on the compatible
// second PR. It preserves the original packet in SQLite, proves both advances
// are fast-forwards plus a clean merge tree, and never wakes a model.
func (e *Engine) recoverCanonicalBaseIncidents(ctx context.Context) error {
	rows, err := e.store.ListDCPReviewLabAdmissions(ctx)
	if err != nil {
		return err
	}
	for _, admission := range rows {
		if admission.Status != domain.DCPAdmissionIncident || admission.ErrorCode != "canonical_main_diverged" ||
			admission.RefreshWakeCount != 0 || admission.RecoveredIncidentPacket != "" {
			continue
		}
		candidate, ok, candidateErr := e.candidateForAdmission(ctx, admission)
		if candidateErr != nil {
			return candidateErr
		}
		if !ok {
			continue
		}
		observation, review, freshErr := e.fresh(ctx, candidate.pr)
		if freshErr != nil {
			return freshErr
		}
		if !admissionFacts(candidate, observation, review) {
			continue
		}
		canonicalBase, syncErr := e.syncCanonicalMain(ctx, candidate, strings.ToLower(observation.PR.BaseSHA))
		if syncErr != nil || strings.EqualFold(canonicalBase, observation.PR.BaseSHA) {
			continue
		}
		if compatibilityErr := e.validateMergeCompatibility(ctx, candidate, observation.PR.HeadSHA, canonicalBase); compatibilityErr != nil {
			continue
		}
		recovered, recoverErr := e.store.RecoverDCPReviewLabCanonicalBaseIncident(ctx, admission, e.clock())
		if recoverErr != nil {
			return recoverErr
		}
		if !recovered {
			return errors.New("dcp admission: exact canonical-base incident recovery was unavailable")
		}
	}
	return nil
}

// Try is the single event entry. Lifecycle and stock SCM callbacks may race,
// but the process mutex and SQLite's partial unique index admit one owner.
func (e *Engine) Try(ctx context.Context, sessionID domain.SessionID) error {
	if err := e.configured(); err != nil {
		return err
	}
	if !eligibleSessionID(sessionID) {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.enrol(ctx, sessionID); err != nil {
		return err
	}
	return e.drain(ctx)
}

func (e *Engine) configured() error {
	if e == nil || e.store == nil || e.scm == nil || strings.TrimSpace(e.dataDir) == "" || e.clock == nil {
		return errors.New("dcp admission: dependencies are not configured")
	}
	return nil
}

func (e *Engine) enrol(ctx context.Context, sessionID domain.SessionID) error {
	candidate, ok, err := e.candidate(ctx, sessionID)
	if err != nil || !ok {
		return err
	}
	if candidate.run.TerminalMergeStatus != "" {
		return nil
	}
	if existing, ok, err := e.store.GetDCPReviewLabAdmissionByRun(ctx, candidate.run.ID); err != nil {
		return err
	} else if ok {
		if existing.Status == domain.DCPAdmissionRefreshing && candidate.session.Metadata.RuntimeLaunchID == "" {
			observation, _, freshErr := e.fresh(ctx, candidate.pr)
			if freshErr != nil {
				return freshErr
			}
			if strings.EqualFold(observation.PR.HeadSHA, existing.TargetSHA) {
				return e.recordIncident(ctx, existing, candidate, observation, "refresh_did_not_produce_new_head")
			}
		}
		return nil
	}
	observation, review, err := e.fresh(ctx, candidate.pr)
	if err != nil {
		return err
	}
	if !admissionFacts(candidate, observation, review) {
		return nil
	}
	now := e.clock()
	if refreshing, ok, err := e.store.GetRefreshingDCPReviewLabAdmissionBySession(ctx, candidate.session.ID); err != nil {
		return err
	} else if ok {
		if refreshing.PRURL != candidate.pr.URL || refreshing.PRNumber != int64(candidate.pr.Number) ||
			strings.EqualFold(refreshing.TargetSHA, candidate.run.TargetSHA) || refreshing.RefreshWakeCount != 1 {
			return e.recordIncident(ctx, refreshing, candidate, observation, "refresh_identity_drift")
		}
		updated, updateErr := e.store.ResumeDCPReviewLabAdmissionAfterRefresh(ctx, refreshing, candidate.run, strings.ToLower(observation.PR.BaseSHA), now)
		if updateErr != nil {
			return updateErr
		}
		if !updated {
			return e.recordIncident(ctx, refreshing, candidate, observation, "refresh_transition_rejected")
		}
		return nil
	}
	if candidate.session.ID == ArbiterSessionA || candidate.session.ID == ArbiterSessionB {
		arbiterStore, storeErr := e.arbiterStore()
		if storeErr != nil {
			return storeErr
		}
		arbiter, ok, err := arbiterStore.GetDCPReleaseArbiterIncidentBySession(ctx, candidate.session.ID)
		if err != nil {
			return err
		}
		if ok && arbiter.Status == domain.DCPArbiterRepairing {
			original, found, getErr := arbiterStore.GetDCPReviewLabAdmissionByID(ctx, arbiter.AdmissionID)
			if getErr != nil || !found {
				return errors.New("dcp arbiter: repairing admission is unavailable")
			}
			if original.Status != domain.DCPAdmissionIncident || original.SessionID != candidate.session.ID ||
				original.PRURL != candidate.pr.URL || original.TargetSHA != arbiter.TargetSHA ||
				strings.EqualFold(candidate.run.TargetSHA, arbiter.TargetSHA) ||
				!strings.EqualFold(observation.PR.BaseSHA, arbiter.CurrentBaseSHA) ||
				arbiter.RecoveryOwnerSessionID != candidate.session.ID || arbiter.RecoveryPath != "same_worker_conflict_repair" ||
				arbiter.RecoveryWakeCount != 1 {
				_, _ = arbiterStore.FailDCPReleaseArbiterAfterDecision(ctx, arbiter.IncidentID, "repair_identity_drift", now)
				return errors.New("dcp arbiter: repaired exact-head identity drifted")
			}
			if validateErr := e.validateArbiterRecoveryCandidate(ctx, candidate, arbiter); validateErr != nil {
				_, _ = arbiterStore.FailDCPReleaseArbiterAfterDecision(ctx, arbiter.IncidentID, "repair_scope_drift", now)
				return validateErr
			}
			rebound, updateErr := arbiterStore.RebindDCPAdmissionAfterArbiterRepair(ctx, original, arbiter, candidate.run, strings.ToLower(observation.PR.BaseSHA), now)
			if updateErr != nil || !rebound {
				_, _ = arbiterStore.FailDCPReleaseArbiterAfterDecision(ctx, arbiter.IncidentID, "repair_rebind_rejected", now)
				return errors.Join(updateErr, errors.New("dcp arbiter: repaired exact-head rebind was rejected"))
			}
			return nil
		}
	}
	_, _, err = e.store.EnqueueDCPReviewLabAdmission(ctx, domain.DCPReviewLabAdmission{
		ID: "dcp-admission-" + candidate.run.ID, ReviewRunID: candidate.run.ID, ReviewID: candidate.run.ReviewID,
		SessionID: candidate.session.ID, PRURL: candidate.pr.URL, PRNumber: int64(candidate.pr.Number),
		TargetSHA: strings.ToLower(candidate.run.TargetSHA), ReviewBaseSHA: strings.ToLower(observation.PR.BaseSHA),
		Status: domain.DCPAdmissionWaiting, CreatedAt: now, UpdatedAt: now,
	})
	return err
}

// drain processes only the durable queue head. A successful merge immediately
// re-reads the next row in this same model-free event; pending provider facts,
// refresh, failure, or incident stop without a timer or poll loop.
func (e *Engine) drain(ctx context.Context) error {
	for {
		if claimed, ok, err := e.store.GetClaimedDCPReviewLabAdmission(ctx); err != nil {
			return err
		} else if ok {
			continued, reconcileErr := e.reconcileClaimed(ctx, claimed)
			if reconcileErr != nil || !continued {
				return reconcileErr
			}
			continue
		}
		admission, ok, err := e.nextPending(ctx)
		if err != nil || !ok {
			return err
		}
		if admission.Status == domain.DCPAdmissionRefreshing || admission.Status == domain.DCPAdmissionIncident {
			return nil
		}
		if admission.Status != domain.DCPAdmissionWaiting {
			return errors.New("dcp admission: invalid pending queue state")
		}
		if ready, cohortErr := e.cohortReady(ctx, admission); cohortErr != nil || !ready {
			return cohortErr
		}
		continued, err := e.processWaiting(ctx, admission)
		if err != nil || !continued {
			return err
		}
	}
}

func (e *Engine) nextPending(ctx context.Context) (domain.DCPReviewLabAdmission, bool, error) {
	rows, err := e.store.ListDCPReviewLabAdmissions(ctx)
	if err != nil {
		return domain.DCPReviewLabAdmission{}, false, err
	}
	for _, row := range rows {
		switch row.Status {
		case domain.DCPAdmissionWaiting, domain.DCPAdmissionRefreshing, domain.DCPAdmissionIncident:
			return row, true, nil
		case domain.DCPAdmissionClaimed:
			return domain.DCPReviewLabAdmission{}, false, errors.New("dcp admission: claimed row escaped owner reconciliation")
		}
	}
	return domain.DCPReviewLabAdmission{}, false, nil
}

func (e *Engine) cohortReady(ctx context.Context, admission domain.DCPReviewLabAdmission) (bool, error) {
	cohortA, cohortB := domain.SessionID(AdmissionSessionA), domain.SessionID(AdmissionSessionB)
	if admission.SessionID == ArbiterSessionA || admission.SessionID == ArbiterSessionB {
		cohortA, cohortB = ArbiterSessionA, ArbiterSessionB
	} else if admission.SessionID != AdmissionSessionA && admission.SessionID != AdmissionSessionB {
		return true, nil
	}
	rows, err := e.store.ListDCPReviewLabAdmissions(ctx)
	if err != nil {
		return false, err
	}
	present := map[domain.SessionID]bool{}
	for _, row := range rows {
		if row.SessionID == cohortA || row.SessionID == cohortB {
			present[row.SessionID] = true
		}
	}
	return present[cohortA] && present[cohortB], nil
}

func (e *Engine) reconcileClaimed(ctx context.Context, admission domain.DCPReviewLabAdmission) (bool, error) {
	candidate, ok, err := e.candidateForAdmission(ctx, admission)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, e.recordIncident(ctx, admission, mergeCandidate{}, ports.SCMObservation{}, "claimed_identity_drift")
	}
	observation, _, err := e.fresh(ctx, candidate.pr)
	if err != nil {
		return false, err
	}
	if observation.PR.Merged && strings.EqualFold(observation.PR.HeadSHA, admission.TargetSHA) && validSHA(observation.PR.MergeCommitSHA) {
		updated, updateErr := e.store.CompleteDCPReviewLabAdmission(ctx, admission, strings.ToLower(observation.PR.MergeCommitSHA), e.clock())
		if updateErr != nil {
			return false, updateErr
		}
		if !updated {
			return false, errors.New("dcp admission: claimed action could not be reconciled")
		}
		return true, nil
	}
	return false, e.recordIncident(ctx, admission, candidate, observation, "uncertain_restart")
}

func (e *Engine) processWaiting(ctx context.Context, admission domain.DCPReviewLabAdmission) (bool, error) {
	candidate, ok, err := e.candidateForAdmission(ctx, admission)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, e.recordIncident(ctx, admission, mergeCandidate{}, ports.SCMObservation{}, "waiting_identity_drift")
	}
	observation, review, err := e.fresh(ctx, candidate.pr)
	if err != nil {
		return false, err
	}
	if !admissionFacts(candidate, observation, review) {
		if providerIdentityDrift(candidate, observation) {
			return false, e.recordIncident(ctx, admission, candidate, observation, "provider_identity_drift")
		}
		return false, nil
	}
	baseSHA := strings.ToLower(observation.PR.BaseSHA)
	switch mergeDisposition(observation) {
	case dispositionWait:
		return false, nil
	case dispositionIncident:
		return false, e.recordIncident(ctx, admission, candidate, observation, "merge_conflict_or_ambiguity")
	case dispositionRefresh:
		if admission.SessionID == HistoricalSession {
			return false, e.recordIncident(ctx, admission, candidate, observation, "refresh_not_authorized")
		}
		canonicalBase, err := e.syncCanonicalMain(ctx, candidate, baseSHA)
		if err != nil {
			if errors.Is(err, errCanonicalDiverged) || errors.Is(err, errCanonicalBaseDrift) {
				return false, e.recordIncident(ctx, admission, candidate, observation, "canonical_main_diverged")
			}
			return false, err
		}
		if err := e.validateGit(ctx, candidate, observation.PR.HeadSHA, canonicalBase); err != nil {
			return false, err
		}
		leaseID := "dcp-refresh-" + admission.ID
		started, err := e.store.StartDCPReviewLabRefresh(ctx, admission, leaseID, canonicalBase, e.clock())
		if err != nil || !started {
			return false, err
		}
		admission.Status, admission.LeaseID, admission.AdmittedBaseSHA, admission.RefreshWakeCount = domain.DCPAdmissionRefreshing, leaseID, canonicalBase, 1
		if e.wake == nil {
			return false, e.recordIncident(ctx, admission, candidate, observation, "refresh_waker_unavailable")
		}
		if err := e.wake(ctx, admission.SessionID, refreshPrompt(candidate, admission, canonicalBase)); err != nil {
			if incidentErr := e.recordIncident(ctx, admission, candidate, observation, "refresh_launch_failed"); incidentErr != nil {
				return false, errors.Join(err, incidentErr)
			}
			return false, err
		}
		return false, nil
	case dispositionMerge:
		canonicalBase, err := e.syncCanonicalMain(ctx, candidate, baseSHA)
		if err != nil {
			if errors.Is(err, errCanonicalDiverged) || errors.Is(err, errCanonicalBaseDrift) {
				return false, e.recordIncident(ctx, admission, candidate, observation, "canonical_main_diverged")
			}
			return false, err
		}
		if !strings.EqualFold(canonicalBase, baseSHA) {
			if err := e.validateMergeCompatibility(ctx, candidate, observation.PR.HeadSHA, canonicalBase); err != nil {
				return false, e.recordIncident(ctx, admission, candidate, observation, "merge_conflict_or_ambiguity")
			}
		}
		if err := e.validateGit(ctx, candidate, observation.PR.HeadSHA, canonicalBase); err != nil {
			return false, err
		}
		leaseID := "dcp-merge-" + admission.ID
		claimed, err := e.store.ClaimDCPReviewLabAdmission(ctx, admission, leaseID, canonicalBase, e.clock())
		if err != nil || !claimed {
			return false, err
		}
		admission.Status, admission.LeaseID, admission.AdmittedBaseSHA = domain.DCPAdmissionClaimed, leaseID, canonicalBase
		result, mergeErr := e.scm.MergePullRequest(ctx, ports.SCMMergeRequest{
			PR: ports.SCMPRRef{
				Repo:   ports.SCMRepo{Provider: "github", Host: "github.com", Owner: "orenvlad-ai", Name: "dcp-review-lab", Repo: RepositoryFullName},
				Number: candidate.pr.Number, URL: candidate.pr.URL,
			},
			ExpectedHeadSHA: candidate.run.TargetSHA,
			Method:          ports.SCMMergeSquash,
		})
		if mergeErr != nil {
			if incidentErr := e.recordIncident(ctx, admission, candidate, observation, mergeErrorCode(mergeErr)); incidentErr != nil {
				return false, errors.Join(mergeErr, incidentErr)
			}
			return false, mergeErr
		}
		if !validSHA(result.MergeCommitSHA) {
			return false, e.recordIncident(ctx, admission, candidate, observation, "invalid_merge_result")
		}
		updated, err := e.store.CompleteDCPReviewLabAdmission(ctx, admission, strings.ToLower(result.MergeCommitSHA), e.clock())
		if err != nil {
			return false, err
		}
		if !updated {
			return false, errors.New("dcp admission: completed provider mutation could not be recorded")
		}
		return true, nil
	default:
		return false, errors.New("dcp admission: unknown disposition")
	}
}

type mergeCandidate struct {
	session domain.SessionRecord
	project domain.ProjectRecord
	pr      domain.PullRequest
	run     domain.ReviewRun
}

func (e *Engine) candidateForAdmission(ctx context.Context, admission domain.DCPReviewLabAdmission) (mergeCandidate, bool, error) {
	candidate, ok, err := e.candidate(ctx, admission.SessionID)
	if err != nil || !ok {
		return mergeCandidate{}, false, err
	}
	if candidate.run.ID != admission.ReviewRunID || candidate.run.ReviewID != admission.ReviewID ||
		candidate.pr.URL != admission.PRURL || int64(candidate.pr.Number) != admission.PRNumber ||
		!strings.EqualFold(candidate.run.TargetSHA, admission.TargetSHA) {
		return mergeCandidate{}, false, nil
	}
	return candidate, true, nil
}

func (e *Engine) candidate(ctx context.Context, id domain.SessionID) (mergeCandidate, bool, error) {
	session, ok, err := e.store.GetSession(ctx, id)
	if err != nil || !ok {
		return mergeCandidate{}, false, err
	}
	if session.ProjectID != domain.ProjectID(ProjectID) || session.Kind != domain.KindWorker || session.Harness != domain.HarnessCodex ||
		session.ReviewerHarness != "" || session.IssueID != "" || session.Activity.State != domain.ActivityIdle || session.IsTerminated ||
		session.TerminateOnPRMerge || session.Metadata.RuntimeLaunchID != "" || !validOptionalNativeBase(session.Metadata.DiffBaseSHA, session.Metadata.DiffBaseRef) ||
		!validTaskIdentity(session) {
		return mergeCandidate{}, false, nil
	}
	if session.ID == ArbiterSessionA || session.ID == ArbiterSessionB {
		if _, _, exact := arbiterTask(session); !exact {
			return mergeCandidate{}, false, nil
		}
	}
	expectedWorkspace := filepath.Join(e.dataDir, "worktrees", ProjectID, string(id))
	expectedBranch := "ao/" + string(id) + "/root"
	if !sameExactPath(session.Metadata.WorkspacePath, expectedWorkspace) || session.Metadata.Branch != expectedBranch {
		return mergeCandidate{}, false, nil
	}
	project, ok, err := e.store.GetProject(ctx, ProjectID)
	if err != nil || !ok {
		return mergeCandidate{}, false, err
	}
	expectedProjectPath := filepath.Join(filepath.Dir(e.dataDir), "targets", ProjectID)
	if !sameExactPath(project.Path, expectedProjectPath) || project.Kind.WithDefault() != domain.ProjectKindSingleRepo || project.RepoOriginURL != RepositoryURL ||
		project.Config.DefaultBranch != TargetBranch || project.Config.SessionPrefix != SessionPrefix ||
		project.Config.AgentRules != ProfileAgentRules || project.Config.AgentRulesFile != "" || project.Config.OrchestratorRules != "" ||
		!project.Config.AgentConfig.IsZero() || project.Config.Worker != (domain.RoleOverride{Harness: domain.HarnessCodex, AgentConfig: domain.AgentConfig{Permissions: domain.PermissionModeAcceptEdits, DCPReviewLabNetwork: true}}) ||
		project.Config.Orchestrator != (domain.RoleOverride{}) || project.Config.TrackerIntake != (domain.TrackerIntakeConfig{}) ||
		project.Config.ContainerReap != (domain.ContainerReapConfig{}) ||
		len(project.Config.Reviewers) != 1 || project.Config.Reviewers[0].Harness != domain.ReviewerCodex ||
		len(project.Config.Env) != 0 || len(project.Config.Symlinks) != 0 || len(project.Config.PostCreate) != 0 {
		return mergeCandidate{}, false, nil
	}
	prs, err := e.store.ListPRsBySession(ctx, id)
	if err != nil || len(prs) != 1 {
		return mergeCandidate{}, false, err
	}
	pr := prs[0]
	if pr.Provider != "github" || pr.Host != "github.com" || pr.Repo != RepositoryFullName || pr.TargetBranch != TargetBranch ||
		pr.SourceBranch != expectedBranch || pr.Author != "orenvlad-ai" || pr.HTMLURL != pr.URL ||
		!validPRURL(pr.URL, pr.Number) || !validSHA(pr.HeadSHA) || !validSHA(pr.BaseSHA) ||
		(session.Metadata.DiffBaseSHA != "" && !strings.EqualFold(pr.BaseSHA, session.Metadata.DiffBaseSHA)) {
		return mergeCandidate{}, false, nil
	}
	runs, err := e.store.ListReviewRunsBySession(ctx, id)
	if err != nil {
		return mergeCandidate{}, false, err
	}
	var exact []domain.ReviewRun
	for _, run := range runs {
		if run.PRURL == pr.URL && strings.EqualFold(run.TargetSHA, pr.HeadSHA) {
			exact = append(exact, run)
		}
	}
	if len(exact) != 1 {
		return mergeCandidate{}, false, nil
	}
	run := exact[0]
	if run.Status != domain.ReviewRunComplete || run.Verdict != domain.VerdictApproved || run.ResultChannel != structuredChannel ||
		run.Harness != domain.ReviewerCodex || run.ID == "" || run.ReviewID == "" || run.BatchID == "" || run.Body == "" || run.GithubReviewID != "" {
		return mergeCandidate{}, false, nil
	}
	switch run.TerminalMergeStatus {
	case "":
		if pr.Draft || pr.Merged || pr.Closed || pr.ProviderState != "OPEN" {
			return mergeCandidate{}, false, nil
		}
	case "running":
		if pr.ProviderState != "OPEN" && pr.ProviderState != "MERGED" && pr.ProviderState != "CLOSED" {
			return mergeCandidate{}, false, nil
		}
	case "succeeded", "failed":
	default:
		return mergeCandidate{}, false, nil
	}
	return mergeCandidate{session: session, project: project, pr: pr, run: run}, true, nil
}

func (e *Engine) fresh(ctx context.Context, pr domain.PullRequest) (ports.SCMObservation, ports.SCMReviewObservation, error) {
	ref := ports.SCMPRRef{
		Repo:   ports.SCMRepo{Provider: "github", Host: "github.com", Owner: "orenvlad-ai", Name: "dcp-review-lab", Repo: RepositoryFullName},
		Number: pr.Number, URL: pr.URL,
	}
	observations, err := e.scm.FetchPullRequests(ctx, []ports.SCMPRRef{ref})
	if err != nil {
		return ports.SCMObservation{}, ports.SCMReviewObservation{}, err
	}
	if len(observations) != 1 || !observations[0].Fetched {
		return ports.SCMObservation{}, ports.SCMReviewObservation{}, errors.New("dcp admission: exact PR could not be refreshed")
	}
	review, err := e.scm.FetchReviewThreads(ctx, ref)
	if err != nil {
		return ports.SCMObservation{}, ports.SCMReviewObservation{}, err
	}
	return observations[0], review, nil
}

func admissionFacts(candidate mergeCandidate, observation ports.SCMObservation, review ports.SCMReviewObservation) bool {
	if providerIdentityDrift(candidate, observation) || review.Partial || !knownNonBlockingReviewDecision(review.Decision) || hasBlockingReview(review) {
		return false
	}
	if len(observation.CI.Checks) == 0 || observation.CI.Summary != string(domain.CIPassing) || !strings.EqualFold(observation.CI.HeadSHA, candidate.run.TargetSHA) {
		return false
	}
	required := 0
	for _, check := range observation.CI.Checks {
		if check.Status != string(domain.PRCheckPassed) && check.Status != string(domain.PRCheckSkipped) {
			return false
		}
		if check.Name == RequiredCheckName {
			if check.Status != string(domain.PRCheckPassed) || check.Conclusion != "success" {
				return false
			}
			required++
		}
	}
	return required == 1
}

func providerIdentityDrift(candidate mergeCandidate, observation ports.SCMObservation) bool {
	pr := observation.PR
	return observation.Provider != "github" || observation.Host != "github.com" || observation.Repo != RepositoryFullName ||
		pr.Number != candidate.pr.Number || pr.URL != candidate.pr.URL || pr.HeadRepo != RepositoryFullName ||
		pr.SourceBranch != candidate.pr.SourceBranch || pr.TargetBranch != TargetBranch ||
		!strings.EqualFold(pr.HeadSHA, candidate.run.TargetSHA) || !validSHA(pr.BaseSHA) ||
		pr.State != string(domain.PRStateOpen) || pr.ProviderState != "OPEN" || pr.Author != "orenvlad-ai" || pr.HTMLURL != pr.URL ||
		pr.Draft || pr.Merged || pr.Closed
}

type disposition int

const (
	dispositionWait disposition = iota
	dispositionMerge
	dispositionRefresh
	dispositionIncident
)

func mergeDisposition(observation ports.SCMObservation) disposition {
	pr := observation.PR
	if pr.ProviderMergeable == "MERGEABLE" && pr.ProviderMergeStateStatus == "CLEAN" &&
		observation.Mergeability.State == string(domain.MergeMergeable) && observation.Mergeability.Mergeable && len(observation.Mergeability.Blockers) == 0 {
		return dispositionMerge
	}
	if pr.ProviderMergeable == "MERGEABLE" && pr.ProviderMergeStateStatus == "BEHIND" &&
		observation.Mergeability.State == string(domain.MergeMergeable) && observation.Mergeability.Mergeable {
		return dispositionRefresh
	}
	if pr.ProviderMergeable == "CONFLICTING" || pr.ProviderMergeStateStatus == "DIRTY" || observation.Mergeability.State == string(domain.MergeConflicting) {
		return dispositionIncident
	}
	return dispositionWait
}

func ready(candidate mergeCandidate, observation ports.SCMObservation, review ports.SCMReviewObservation) bool {
	return admissionFacts(candidate, observation, review) && mergeDisposition(observation) == dispositionMerge
}

func hasBlockingReview(review ports.SCMReviewObservation) bool {
	for _, thread := range review.Threads {
		if !thread.Resolved {
			return true
		}
	}
	return false
}

func knownNonBlockingReviewDecision(decision string) bool {
	return decision == string(domain.ReviewNone) || decision == string(domain.ReviewApproved)
}

func (e *Engine) syncCanonicalMain(ctx context.Context, candidate mergeCandidate, baseSHA string) (string, error) {
	if !validSHA(baseSHA) {
		return "", errors.New("dcp admission: provider base SHA is invalid")
	}
	projectPath := candidate.project.Path
	prechecks := []struct {
		args []string
		want string
	}{
		{[]string{"rev-parse", "--show-toplevel"}, projectPath},
		{[]string{"branch", "--show-current"}, TargetBranch},
		{[]string{"remote"}, "origin"},
		{[]string{"remote", "get-url", "origin"}, RepositoryURL},
		{[]string{"status", "--porcelain"}, ""},
	}
	for _, check := range prechecks {
		got, err := e.git(ctx, projectPath, check.args...)
		if err != nil || got != check.want {
			return "", errors.New("dcp admission: canonical repository identity is not exact and clean")
		}
	}
	if _, err := e.git(ctx, projectPath, "fetch", "--no-tags", "origin", TargetBranch); err != nil {
		return "", fmt.Errorf("dcp admission: fetch canonical main: %w", err)
	}
	originMain, err := e.git(ctx, projectPath, "rev-parse", "origin/main")
	if err != nil || !validSHA(originMain) {
		return "", errCanonicalBaseDrift
	}
	if !strings.EqualFold(originMain, baseSHA) {
		if _, err := e.git(ctx, projectPath, "merge-base", "--is-ancestor", baseSHA, originMain); err != nil {
			return "", errCanonicalBaseDrift
		}
	}
	head, err := e.git(ctx, projectPath, "rev-parse", "HEAD")
	if err != nil || !validSHA(head) {
		return "", errors.New("dcp admission: canonical main HEAD is invalid")
	}
	if !strings.EqualFold(head, originMain) {
		if _, err := e.git(ctx, projectPath, "merge-base", "--is-ancestor", head, originMain); err != nil {
			return "", errCanonicalDiverged
		}
		if _, err := e.git(ctx, projectPath, "merge", "--ff-only", "origin/main"); err != nil {
			return "", fmt.Errorf("%w: %v", errCanonicalDiverged, err)
		}
	}
	return strings.ToLower(originMain), nil
}

func (e *Engine) validateMergeCompatibility(ctx context.Context, candidate mergeCandidate, head, base string) error {
	if !validSHA(head) || !validSHA(base) {
		return errors.New("dcp admission: compatibility identity is invalid")
	}
	tree, err := e.git(ctx, candidate.project.Path, "merge-tree", "--write-tree", strings.ToLower(base), strings.ToLower(head))
	fields := strings.Fields(tree)
	if err != nil || len(fields) != 1 || !validSHA(fields[0]) {
		return errors.New("dcp admission: exact head is not proven compatible with current canonical main")
	}
	return nil
}

func (e *Engine) validateGit(ctx context.Context, candidate mergeCandidate, head, base string) error {
	projectPath := candidate.project.Path
	workspacePath := candidate.session.Metadata.WorkspacePath
	base = strings.ToLower(base)
	checks := []struct {
		path string
		args []string
		want string
	}{
		{projectPath, []string{"rev-parse", "--show-toplevel"}, projectPath},
		{projectPath, []string{"branch", "--show-current"}, TargetBranch},
		{projectPath, []string{"remote"}, "origin"},
		{projectPath, []string{"remote", "get-url", "origin"}, RepositoryURL},
		{projectPath, []string{"rev-parse", "origin/main"}, base},
		{projectPath, []string{"rev-parse", "HEAD"}, base},
		{projectPath, []string{"status", "--porcelain"}, ""},
		{workspacePath, []string{"rev-parse", "--show-toplevel"}, workspacePath},
		{workspacePath, []string{"branch", "--show-current"}, candidate.session.Metadata.Branch},
		{workspacePath, []string{"remote"}, "origin"},
		{workspacePath, []string{"remote", "get-url", "origin"}, RepositoryURL},
		{workspacePath, []string{"rev-parse", "HEAD"}, strings.ToLower(head)},
		{workspacePath, []string{"status", "--porcelain"}, ""},
	}
	for _, check := range checks {
		got, err := e.git(ctx, check.path, check.args...)
		if err != nil || got != check.want {
			return errors.New("dcp admission: local repository identity is not exact and clean")
		}
	}
	common, err := e.git(ctx, workspacePath, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || !sameExactPath(common, filepath.Join(projectPath, ".git")) {
		return errors.New("dcp admission: linked worktree common git directory is foreign")
	}
	private, err := e.git(ctx, workspacePath, "rev-parse", "--path-format=absolute", "--absolute-git-dir")
	if err != nil || !sameExactPath(private, filepath.Join(projectPath, ".git", "worktrees", string(candidate.session.ID))) {
		return errors.New("dcp admission: linked worktree private git directory is foreign")
	}
	return nil
}

func refreshPrompt(candidate mergeCandidate, admission domain.DCPReviewLabAdmission, baseSHA string) string {
	return fmt.Sprintf("DCP bounded admission refresh for %s: the approved head %s is behind exact origin/main %s. Fetch origin/main, rebase only the current branch %s onto that exact SHA, and abort the rebase without commit or push if any conflict or ambiguity appears. If clean, run the repository check, push the same branch to the existing PR with --force-with-lease bound to %s, create no new PR/branch/worktree, change no task scope, then stop.",
		candidate.session.DisplayName, admission.TargetSHA, baseSHA, candidate.session.Metadata.Branch, admission.TargetSHA)
}

type incidentPacket struct {
	SchemaVersion            string `json:"schemaVersion"`
	Reason                   string `json:"reason"`
	Repository               string `json:"repository"`
	AdmissionID              string `json:"admissionId"`
	LeaseID                  string `json:"leaseId"`
	Sequence                 int64  `json:"sequence"`
	SessionID                string `json:"sessionId"`
	TaskDisplayName          string `json:"taskDisplayName"`
	SourceBranch             string `json:"sourceBranch"`
	ReviewID                 string `json:"reviewId"`
	ReviewRunID              string `json:"reviewRunId"`
	PRURL                    string `json:"prUrl"`
	PRNumber                 int64  `json:"prNumber"`
	TargetSHA                string `json:"targetSha"`
	ReviewBaseSHA            string `json:"reviewBaseSha"`
	CurrentBaseSHA           string `json:"currentBaseSha"`
	ProviderMergeable        string `json:"providerMergeable"`
	ProviderMergeStateStatus string `json:"providerMergeStateStatus"`
	EvidenceDigest           string `json:"evidenceDigest"`
	RecordedAt               string `json:"recordedAt"`
}

func (e *Engine) recordIncident(ctx context.Context, admission domain.DCPReviewLabAdmission, candidate mergeCandidate, observation ports.SCMObservation, reason string) error {
	now := e.clock()
	leaseID := admission.LeaseID
	if leaseID == "" {
		leaseID = "dcp-incident-" + admission.ID
	}
	baseSHA := strings.ToLower(observation.PR.BaseSHA)
	if !validSHA(baseSHA) {
		baseSHA = admission.AdmittedBaseSHA
	}
	if !validSHA(baseSHA) {
		baseSHA = admission.ReviewBaseSHA
	}
	evidence := strings.Join([]string{RepositoryFullName, admission.ID, leaseID, string(admission.SessionID), candidate.session.DisplayName, candidate.pr.SourceBranch, admission.ReviewRunID,
		admission.PRURL, strconv.FormatInt(admission.PRNumber, 10), strings.ToLower(admission.TargetSHA), strings.ToLower(baseSHA),
		observation.PR.ProviderMergeable, observation.PR.ProviderMergeStateStatus, reason}, "\x00")
	digest := sha256.Sum256([]byte(evidence))
	packet, err := json.Marshal(incidentPacket{
		SchemaVersion: "dcp.review-lab.arbiter-needed/v1", Reason: reason, Repository: RepositoryFullName,
		AdmissionID: admission.ID, LeaseID: leaseID, Sequence: admission.Sequence, SessionID: string(admission.SessionID),
		TaskDisplayName: candidate.session.DisplayName, SourceBranch: candidate.pr.SourceBranch,
		ReviewID: admission.ReviewID, ReviewRunID: admission.ReviewRunID, PRURL: admission.PRURL, PRNumber: admission.PRNumber,
		TargetSHA: strings.ToLower(admission.TargetSHA), ReviewBaseSHA: strings.ToLower(admission.ReviewBaseSHA), CurrentBaseSHA: strings.ToLower(baseSHA),
		ProviderMergeable: observation.PR.ProviderMergeable, ProviderMergeStateStatus: observation.PR.ProviderMergeStateStatus,
		EvidenceDigest: fmt.Sprintf("%x", digest), RecordedAt: now.Format(time.RFC3339Nano),
	})
	if err != nil {
		return err
	}
	recorded, err := e.store.RecordDCPReviewLabIncident(ctx, admission, leaseID, baseSHA, reason, string(packet), now)
	if err != nil {
		return err
	}
	if !recorded {
		return errors.New("dcp admission: exact incident transition was rejected")
	}
	if reason == "merge_conflict_or_ambiguity" && (admission.SessionID == ArbiterSessionA || admission.SessionID == ArbiterSessionB) {
		return e.reconcileStage2Arbiter(ctx)
	}
	return nil
}

func gitOutput(ctx context.Context, repo string, args ...string) (string, error) {
	argv := append([]string{"-C", repo}, args...)
	out, err := exec.CommandContext(ctx, "git", argv...).Output()
	return strings.TrimSpace(string(out)), err
}

func eligibleSessionID(id domain.SessionID) bool {
	value := string(id)
	return value == HistoricalSession || value == AdmissionSessionA || value == AdmissionSessionB || value == ArbiterSessionA || value == ArbiterSessionB
}

func validPRURL(raw string, number int) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" && u.RawQuery == "" && u.Fragment == "" &&
		u.Path == "/"+RepositoryFullName+"/pull/"+strconv.Itoa(number)
}

func validSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range strings.ToLower(value) {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

func validOptionalNativeBase(sha, ref string) bool {
	return (sha == "" && ref == "") || (validSHA(sha) && ref == "origin/main")
}

func sameExactPath(a, b string) bool {
	if a == "" || b == "" || !filepath.IsAbs(a) || !filepath.IsAbs(b) || filepath.Clean(a) != a || filepath.Clean(b) != b || a != b {
		return false
	}
	resolvedA, errA := filepath.EvalSymlinks(a)
	resolvedB, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && resolvedA == resolvedB
}

func validTaskIdentity(session domain.SessionRecord) bool {
	if !strings.HasPrefix(session.DisplayName, TaskDisplayPrefix) {
		return false
	}
	taskID := strings.TrimPrefix(session.DisplayName, TaskDisplayPrefix)
	if !validTaskID(taskID) {
		return false
	}
	prefix := TaskPromptPrefix + taskID + ": "
	if !strings.HasPrefix(session.Metadata.Prompt, prefix) {
		return false
	}
	prompt := strings.TrimPrefix(session.Metadata.Prompt, prefix)
	if prompt == "" || len(prompt) > 512 || !utf8.ValidString(prompt) {
		return false
	}
	for _, r := range prompt {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validTaskID(value string) bool {
	if len(value) == 0 || len(value) > 16 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func mergeErrorCode(err error) string {
	switch {
	case errors.Is(err, ports.ErrSCMHeadChanged):
		return "head_changed"
	case errors.Is(err, ports.ErrSCMNotMergeable):
		return "not_mergeable"
	case errors.Is(err, ports.ErrSCMNotFound):
		return "not_found"
	default:
		return "provider_failed"
	}
}
