package dcpterminalmerge

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	RebaseHeadFinalizationDigest = "a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b"
	RebaseHeadFinalizationID     = "dcp-card12-rebase-head-finalization-" + RebaseHeadFinalizationDigest
	rebaseHeadContractCommit     = "9465a84ec44f72f6b7c245ebddeac22d722108ae"
)

type RebaseHeadFinalizationStore interface {
	GetDCPCard12RebaseHeadFinalization(context.Context, string) (domain.DCPCard12RebaseHeadFinalization, bool, error)
	ListDCPCard12RebaseHeadFinalizations(context.Context) ([]domain.DCPCard12RebaseHeadFinalization, error)
	HasExactDCPFinalizationQuarantine(context.Context) (bool, error)
	StartDCPCard12RebaseHeadFinalization(context.Context, domain.DCPCard12RebaseHeadFinalization, time.Time) (bool, error)
	CompleteDCPCard12RebaseHeadFinalizationAction(context.Context, domain.DCPCard12RebaseHeadFinalization, time.Time) (bool, error)
	FailDCPCard12RebaseHeadFinalization(context.Context, string, string, time.Time) (bool, error)
	FailDCPCard12RebaseHeadFinalizationReview(context.Context, domain.DCPCard12RebaseHeadFinalization, domain.ReviewRun, string, time.Time) (bool, error)
	RebindDCPAdmissionAfterCard12RebaseHeadFinalization(context.Context, domain.DCPReviewLabAdmission, domain.DCPCard12RebaseHeadFinalization, domain.ReviewRun, string, string, time.Time) (bool, error)
}

type RebaseHeadFinalizationExecutor interface {
	Preflight(context.Context, domain.DCPCard12RebaseHeadFinalization) error
	Execute(context.Context, domain.DCPCard12RebaseHeadFinalization) error
	InspectCompleted(context.Context, domain.DCPCard12RebaseHeadFinalization) error
}

func (e *Engine) SetRebaseHeadFinalizationExecutor(executor RebaseHeadFinalizationExecutor) {
	e.rebaseHeadFinalization = executor
}

func exactRebaseHeadFinalization(row domain.DCPCard12RebaseHeadFinalization) bool {
	return row.FinalizationID == RebaseHeadFinalizationID && row.Generation == 1 &&
		row.IdentityDigest == RebaseHeadFinalizationDigest && row.ContractCommit == rebaseHeadContractCommit &&
		row.PredecessorRecoveryID == ColdStartRecoveryID && row.IncidentID == exactSuccessorIncidentID &&
		row.AdmissionID == "dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34" && row.SessionID == ArbiterSessionB &&
		row.TaskID == ArbiterTaskB && row.ProjectID == ProjectID && row.Repository == RepositoryFullName &&
		row.WorktreePath == "/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/dcp-review-lab/dcp-review-lab-12" &&
		row.SourceBranch == "ao/dcp-review-lab-12/root" && row.PRURL == "https://github.com/orenvlad-ai/dcp-review-lab/pull/9" && row.PRNumber == 9 &&
		row.OldHead == "d4fcb68051ae113ed497d02151a759800ee85633" && row.CandidateHead == "4de6ff1a0b80223a9b32a05ba68cf0b665296081" &&
		row.CurrentMain == "b34b31b5443890e69128db2862726950a6bbac0d" && row.ProviderBase == modelFreeProviderBaseSHA &&
		row.ConflictPath == arbiterConflictPath && row.ResolvedBytesDigest == "2a5da25a78ff8bcd9aff4493f195eaefecbc70c3d4db8902dda468ccf69e5e46" &&
		row.ResolvedBlob == modelFreeResolvedBlob && row.CandidateDiffDigest == "b415f3cc21e091afc82e8fbf5fa1a6f0e64ec42465ea8702efe4c681f47295f7" &&
		row.CleanStatusDigest == "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" &&
		row.RebaseHeadDigest == "657c15026f6e8f51e96e6ff6c2ae94a5d6f4031ec95f07030b52f6226cc4d810" && row.OrigHeadDigest == row.RebaseHeadDigest &&
		row.BackupPath == "/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/evidence/dcp-card12-cold-start-recovery/"+ColdStartRecoveryID &&
		row.BackupDigest == "82d0e5834375c380069e7d48a7fdb2066371670d92733ce59545718469a4f3dd" &&
		row.PushRef == "refs/heads/ao/dcp-review-lab-12/root" && row.PushLeaseOldHead == row.OldHead &&
		row.UnauthorizedWorkerTokens11 == 33238 && row.UnauthorizedWorkerTokens12 == 33573 &&
		row.WorkerModelCallCount == 0 && row.ArbiterModelCallCount == 0
}

func (e *Engine) rebaseHeadStore() (RebaseHeadFinalizationStore, error) {
	store, ok := e.store.(RebaseHeadFinalizationStore)
	if !ok {
		return nil, errors.New("card-12 REBASE_HEAD finalization: store unavailable")
	}
	return store, nil
}

func (e *Engine) reconcileCard12RebaseHeadFinalization(ctx context.Context) error {
	store, ok := e.store.(RebaseHeadFinalizationStore)
	if !ok {
		return nil
	}
	rows, err := store.ListDCPCard12RebaseHeadFinalizations(ctx)
	if err != nil || len(rows) == 0 {
		return err
	}
	if len(rows) != 1 || !exactRebaseHeadFinalization(rows[0]) {
		return errors.New("card-12 REBASE_HEAD finalization: row count or immutable identity drifted")
	}
	return e.advanceCard12RebaseHeadFinalizationLocked(ctx, rows[0])
}

func (e *Engine) advanceCard12RebaseHeadFinalizationLocked(ctx context.Context, row domain.DCPCard12RebaseHeadFinalization) error {
	store, err := e.rebaseHeadStore()
	if err != nil {
		return err
	}
	fail := func(code string, cause error) error {
		_, persistErr := store.FailDCPCard12RebaseHeadFinalization(ctx, row.FinalizationID, code, e.clock())
		return errors.Join(cause, persistErr)
	}
	switch row.Status {
	case domain.DCPRebaseHeadFinalizationAuthorized:
		if e.rebaseHeadFinalization == nil {
			return fail("executor_unavailable", errors.New("card-12 REBASE_HEAD finalization: executor unavailable"))
		}
		if err := e.validateRebaseHeadFinalizationPredecessor(ctx, row); err != nil {
			return fail("identity_drift", err)
		}
		if err := e.rebaseHeadFinalization.Preflight(ctx, row); err != nil {
			return fail("preflight_failed", err)
		}
		if err := e.validateRebaseHeadFinalizationProvider(ctx, row, row.OldHead, true); err != nil {
			return fail("provider_identity_drift", err)
		}
		started, err := store.StartDCPCard12RebaseHeadFinalization(ctx, row, e.clock())
		if err != nil || !started {
			return errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: one-action fence unavailable"))
		}
		fenced, found, err := store.GetDCPCard12RebaseHeadFinalization(ctx, row.FinalizationID)
		if err != nil || !found || fenced.Status != domain.DCPRebaseHeadFinalizationRunning || fenced.ModelFreeActionCount != 1 {
			return errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: fenced row unavailable"))
		}
		if err := e.rebaseHeadFinalization.Execute(ctx, fenced); err != nil {
			_, failErr := store.FailDCPCard12RebaseHeadFinalization(ctx, fenced.FinalizationID, "model_free_action_failed", e.clock())
			return errors.Join(err, failErr)
		}
		if err := e.validateRebaseHeadFinalizationProvider(ctx, fenced, fenced.CandidateHead, false); err != nil {
			_, failErr := store.FailDCPCard12RebaseHeadFinalization(ctx, fenced.FinalizationID, "provider_identity_drift", e.clock())
			return errors.Join(err, failErr)
		}
		completed, err := store.CompleteDCPCard12RebaseHeadFinalizationAction(ctx, fenced, e.clock())
		if err != nil || !completed {
			return errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: candidate transition unavailable"))
		}
		reloaded, found, err := store.GetDCPCard12RebaseHeadFinalization(ctx, row.FinalizationID)
		if err != nil || !found {
			return errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: candidate row unavailable"))
		}
		return e.advanceCard12RebaseHeadFinalizationLocked(ctx, reloaded)
	case domain.DCPRebaseHeadFinalizationRunning:
		if e.rebaseHeadFinalization == nil {
			return fail("executor_unavailable", errors.New("card-12 REBASE_HEAD finalization: executor unavailable"))
		}
		if err := e.rebaseHeadFinalization.InspectCompleted(ctx, row); err != nil {
			return fail("incomplete_action", err)
		}
		if err := e.validateRebaseHeadFinalizationProvider(ctx, row, row.CandidateHead, false); err != nil {
			return fail("provider_identity_drift", err)
		}
		completed, err := store.CompleteDCPCard12RebaseHeadFinalizationAction(ctx, row, e.clock())
		if err != nil || !completed {
			return errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: reconciled candidate transition unavailable"))
		}
		reloaded, found, err := store.GetDCPCard12RebaseHeadFinalization(ctx, row.FinalizationID)
		if err != nil || !found {
			return errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: reconciled candidate row unavailable"))
		}
		return e.advanceCard12RebaseHeadFinalizationLocked(ctx, reloaded)
	case domain.DCPRebaseHeadFinalizationCandidateReady:
		if e.modelFreeReviewTrigger == nil {
			return errors.New("card-12 REBASE_HEAD finalization: reviewer trigger unavailable")
		}
		return e.modelFreeReviewTrigger(ctx, row.SessionID)
	case domain.DCPRebaseHeadFinalizationReviewRunning, domain.DCPRebaseHeadFinalizationRecoveryReviewed,
		domain.DCPRebaseHeadFinalizationSucceeded, domain.DCPRebaseHeadFinalizationFailed:
		return nil
	default:
		return errors.New("card-12 REBASE_HEAD finalization: unknown durable status")
	}
}

func (e *Engine) validateRebaseHeadFinalizationPredecessor(ctx context.Context, row domain.DCPCard12RebaseHeadFinalization) error {
	if !exactRebaseHeadFinalization(row) || row.Status != domain.DCPRebaseHeadFinalizationAuthorized || row.Revision != 0 ||
		row.ModelFreeActionCount != 0 || row.ReviewerModelCallCount != 0 || row.ProviderNewHead != "" || row.ReviewRunID != "" || row.MergeCommitSHA != "" {
		return errors.New("card-12 REBASE_HEAD finalization: authorization row is not pristine")
	}
	quarantine, err := e.rebaseHeadStore()
	if err != nil {
		return err
	}
	exactQuarantine, err := quarantine.HasExactDCPFinalizationQuarantine(ctx)
	if err != nil || !exactQuarantine {
		return errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: startup quarantine drifted"))
	}
	coldStore, ok := e.store.(ColdStartRecoveryStore)
	if !ok {
		return errors.New("card-12 REBASE_HEAD finalization: predecessor store unavailable")
	}
	predecessor, found, err := coldStore.GetDCPCard12ColdStartRecovery(ctx, row.PredecessorRecoveryID)
	if err != nil || !found || !exactColdStartRecovery(predecessor) || predecessor.Status != domain.DCPColdStartRecoveryFailed ||
		predecessor.ErrorCode != "model_free_action_failed" || predecessor.Revision != 7 || predecessor.WorkerModelCallCount != 0 ||
		predecessor.ArbiterModelCallCount != 0 || predecessor.ModelFreeActionCount != 1 || predecessor.ReviewerModelCallCount != 0 ||
		predecessor.BackupPath != row.BackupPath || predecessor.BackupDigest != row.BackupDigest || predecessor.LocalRefBefore != row.OldHead ||
		predecessor.LocalRefAfter != "" || predecessor.NewHead != "" || predecessor.ProviderNewHead != "" ||
		predecessor.RecoveryReviewRunID != "" || predecessor.MergeCommitSHA != "" || predecessor.FinishedAt == nil {
		return errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: terminal predecessor drifted"))
	}
	if exact, err := coldStore.HasExactDCPCard12ColdStartToolPathRecovery(ctx); err != nil || !exact {
		return errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: tool-path audit drifted"))
	}
	if exact, err := coldStore.HasExactDCPCard12ColdStartAutoMergeRecovery(ctx); err != nil || !exact {
		return errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: AUTO_MERGE audit drifted"))
	}
	session, found, err := e.store.GetSession(ctx, row.SessionID)
	taskID, taskText, taskExact := arbiterTask(session)
	if err != nil || !found || !taskExact || taskID != row.TaskID || taskText != "Create canary/i13-arbiter-conflict.txt with exactly one line: arbiter intent B. Commit, push, and open the ready PR required by the profile." ||
		string(session.ProjectID) != row.ProjectID || session.Activity.State != domain.ActivityIdle || session.IsTerminated ||
		session.Harness != domain.HarnessCodex || session.Kind != domain.KindWorker || session.DisplayName != "DCP:i13-arbiter-b" ||
		session.Metadata.WorkspacePath != row.WorktreePath || session.Metadata.Branch != row.SourceBranch ||
		session.Metadata.RuntimeHandleID != "dcp-review-lab-12" || session.Metadata.AgentSessionID != "" || session.Metadata.RuntimeLaunchID != "" {
		return errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: native session evidence drifted"))
	}
	arbiterStore, err := e.arbiterStore()
	if err != nil {
		return err
	}
	admission, found, err := arbiterStore.GetDCPReviewLabAdmissionByID(ctx, row.AdmissionID)
	if err != nil || !found || admission.Sequence != 4 || admission.Status != domain.DCPAdmissionIncident ||
		admission.SessionID != row.SessionID || admission.TargetSHA != row.OldHead || admission.ReviewBaseSHA != row.ProviderBase ||
		admission.ErrorCode != "merge_conflict_or_ambiguity" || admission.LeaseID != exactSuccessorIncidentLeaseID ||
		admission.MergeCommitSHA != "" || admission.RefreshWakeCount != 0 {
		return errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: admission evidence drifted"))
	}
	incident, found, err := arbiterStore.GetDCPReleaseArbiterIncidentByID(ctx, row.IncidentID)
	if err != nil || !found || !exactSuccessorOriginal(incident) {
		return errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: incident evidence drifted"))
	}
	return nil
}

func (e *Engine) validateRebaseHeadFinalizationProvider(ctx context.Context, row domain.DCPCard12RebaseHeadFinalization, wantHead string, pre bool) error {
	prs, err := e.store.ListPRsBySession(ctx, row.SessionID)
	if err != nil || len(prs) != 1 {
		return errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: stored PR identity drifted"))
	}
	stored := prs[0]
	if stored.URL != row.PRURL || stored.Number != int(row.PRNumber) || stored.Repo != row.Repository || stored.SourceBranch != row.SourceBranch ||
		stored.TargetBranch != TargetBranch || stored.Author != "orenvlad-ai" || !strings.EqualFold(stored.BaseSHA, row.ProviderBase) {
		return errors.New("card-12 REBASE_HEAD finalization: stored PR is foreign")
	}
	ref := ports.SCMPRRef{Repo: ports.SCMRepo{Provider: "github", Host: "github.com", Owner: "orenvlad-ai", Name: "dcp-review-lab", Repo: RepositoryFullName}, Number: int(row.PRNumber), URL: row.PRURL}
	observations, err := e.scm.FetchPullRequests(ctx, []ports.SCMPRRef{ref})
	if err != nil || len(observations) != 1 || !observations[0].Fetched {
		return errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: provider PR refresh failed"))
	}
	observation, pr := observations[0], observations[0].PR
	if observation.Provider != "github" || observation.Host != "github.com" || observation.Repo != row.Repository || pr.Number != int(row.PRNumber) ||
		pr.URL != row.PRURL || pr.HeadRepo != row.Repository || pr.SourceBranch != row.SourceBranch || pr.TargetBranch != TargetBranch ||
		!strings.EqualFold(pr.HeadSHA, wantHead) || !strings.EqualFold(pr.BaseSHA, row.ProviderBase) || pr.State != string(domain.PRStateOpen) ||
		pr.ProviderState != "OPEN" || pr.Author != "orenvlad-ai" || pr.HTMLURL != pr.URL || pr.Draft || pr.Merged || pr.Closed {
		return errors.New("card-12 REBASE_HEAD finalization: provider identity/head drifted")
	}
	if pre && (pr.ProviderMergeable != "CONFLICTING" || pr.ProviderMergeStateStatus != "DIRTY" || observation.Mergeability.State != string(domain.MergeConflicting)) {
		return errors.New("card-12 REBASE_HEAD finalization: provider conflict state drifted")
	}
	return nil
}

func (e *Engine) handleRebaseHeadFinalizationReview(ctx context.Context, workerID domain.SessionID, run domain.ReviewRun) (bool, error) {
	if workerID != ArbiterSessionB {
		return false, nil
	}
	store, ok := e.store.(RebaseHeadFinalizationStore)
	if !ok {
		return false, nil
	}
	row, found, err := store.GetDCPCard12RebaseHeadFinalization(ctx, RebaseHeadFinalizationID)
	if err != nil || !found {
		return false, err
	}
	if row.Status == domain.DCPRebaseHeadFinalizationFailed && row.ReviewRunID == run.ID && row.CandidateHead == run.TargetSHA {
		return true, nil
	}
	if row.Status != domain.DCPRebaseHeadFinalizationReviewRunning || row.ReviewRunID != run.ID || row.CandidateHead != run.TargetSHA {
		return false, nil
	}
	if run.Status != domain.ReviewRunComplete || run.ResultChannel != structuredChannel {
		_, failErr := store.FailDCPCard12RebaseHeadFinalizationReview(ctx, row, run, "review_result_malformed", e.clock())
		return true, failErr
	}
	if run.Verdict != domain.VerdictApproved {
		_, failErr := store.FailDCPCard12RebaseHeadFinalizationReview(ctx, row, run, "review_changes_requested", e.clock())
		return true, failErr
	}
	if err := e.enrol(ctx, workerID); err != nil {
		return true, err
	}
	return true, e.drain(ctx)
}

func (e *Engine) tryRebindRebaseHeadFinalization(ctx context.Context, candidate mergeCandidate, observation ports.SCMObservation, now time.Time) (bool, error) {
	store, ok := e.store.(RebaseHeadFinalizationStore)
	if !ok {
		return false, nil
	}
	row, found, err := store.GetDCPCard12RebaseHeadFinalization(ctx, RebaseHeadFinalizationID)
	if err != nil || !found {
		return false, err
	}
	if row.Status != domain.DCPRebaseHeadFinalizationReviewRunning || candidate.session.ID != row.SessionID ||
		candidate.run.ID != row.ReviewRunID || candidate.run.TargetSHA != row.CandidateHead {
		return false, nil
	}
	if providerIdentityDrift(candidate, observation) {
		_, failErr := store.FailDCPCard12RebaseHeadFinalizationReview(ctx, row, candidate.run, "review_provider_identity_drift", now)
		return true, failErr
	}
	_, review, err := e.fresh(ctx, candidate.pr)
	if err != nil {
		return true, err
	}
	if !admissionFacts(candidate, observation, review) || mergeDisposition(observation) == dispositionWait {
		return true, nil
	}
	if mergeDisposition(observation) != dispositionMerge {
		_, failErr := store.FailDCPCard12RebaseHeadFinalizationReview(ctx, row, candidate.run, "review_not_mergeable", now)
		return true, failErr
	}
	arbiterStore, err := e.arbiterStore()
	if err != nil {
		return true, err
	}
	incident, found, err := arbiterStore.GetDCPReleaseArbiterIncidentByID(ctx, row.IncidentID)
	if err != nil || !found {
		return true, errors.New("card-12 REBASE_HEAD finalization: original incident disappeared")
	}
	if err := e.validateArbiterRecoveryCandidate(ctx, candidate, incident); err != nil {
		_, failErr := store.FailDCPCard12RebaseHeadFinalizationReview(ctx, row, candidate.run, "review_scope_drift", now)
		return true, errors.Join(err, failErr)
	}
	admission, found, err := arbiterStore.GetDCPReviewLabAdmissionByID(ctx, row.AdmissionID)
	if err != nil || !found {
		return true, errors.New("card-12 REBASE_HEAD finalization: original admission disappeared")
	}
	checkID := ""
	for _, check := range observation.CI.Checks {
		if check.Name == RequiredCheckName && check.Status == string(domain.PRCheckPassed) && check.Conclusion == "success" {
			checkID = check.ProviderID
		}
	}
	if checkID == "" {
		return true, nil
	}
	rebound, err := store.RebindDCPAdmissionAfterCard12RebaseHeadFinalization(ctx, admission, row, candidate.run, strings.ToLower(observation.PR.BaseSHA), checkID, now)
	if err != nil || !rebound {
		return true, errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: reviewed admission rebind rejected"))
	}
	return true, nil
}
