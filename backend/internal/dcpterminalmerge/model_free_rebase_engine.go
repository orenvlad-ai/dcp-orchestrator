package dcpterminalmerge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	ModelFreeRebaseContinuationDigest = "66eb630c1995f90b37429a2f6c57c57794dda9fc98a29149c88bdb2f01131060"
	ModelFreeRebaseContinuationID     = "dcp-card12-model-free-rebase-continuation-" + ModelFreeRebaseContinuationDigest
	modelFreeContractCommit           = "e17fa9080434b5642667392fb06db61cf35f19bd"
)

type ModelFreeRebaseStore interface {
	ValidateDCPCard12ModelFreeRebaseDurableCounts(context.Context) (bool, error)
	GetDCPCard12ModelFreeRebaseContinuation(context.Context, string) (domain.DCPCard12ModelFreeRebaseContinuation, bool, error)
	ListDCPCard12ModelFreeRebaseContinuations(context.Context) ([]domain.DCPCard12ModelFreeRebaseContinuation, error)
	StartDCPCard12ModelFreeRebaseContinuation(context.Context, domain.DCPCard12ModelFreeRebaseContinuation, time.Time) (bool, error)
	CompleteDCPCard12ModelFreeRebaseContinuation(context.Context, domain.DCPCard12ModelFreeRebaseContinuation, string, time.Time) (bool, error)
	FailDCPCard12ModelFreeRebaseContinuation(context.Context, string, string, time.Time) (bool, error)
	FailDCPCard12ModelFreeRebaseReview(context.Context, domain.DCPCard12ModelFreeRebaseContinuation, domain.ReviewRun, string, time.Time) (bool, error)
	RebindDCPAdmissionAfterCard12ModelFreeRebase(context.Context, domain.DCPReviewLabAdmission, domain.DCPCard12ModelFreeRebaseContinuation, domain.ReviewRun, string, string, time.Time) (bool, error)
}

func (e *Engine) SetModelFreeRebaseExecutor(executor ModelFreeRebaseExecutor) {
	e.modelFreeRebase = executor
}

func (e *Engine) SetModelFreeReviewTrigger(trigger func(context.Context, domain.SessionID) error) {
	e.modelFreeReviewTrigger = trigger
}

func (e *Engine) modelFreeRebaseStore() (ModelFreeRebaseStore, error) {
	store, ok := e.store.(ModelFreeRebaseStore)
	if !ok {
		return nil, errors.New("card-12 model-free rebase: durable store surface is unavailable")
	}
	return store, nil
}

func exactModelFreeRebaseContinuation(row domain.DCPCard12ModelFreeRebaseContinuation) bool {
	return row.ContinuationID == ModelFreeRebaseContinuationID && row.Generation == 1 &&
		row.IdentityDigest == ModelFreeRebaseContinuationDigest && row.ContractCommit == modelFreeContractCommit &&
		row.PredecessorRecoveryID == FreshWorkerRecoveryID && row.IncidentID == exactSuccessorIncidentID &&
		row.AdmissionID == "dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34" &&
		row.SessionID == ArbiterSessionB && row.TaskID == ArbiterTaskB && row.ProjectID == ProjectID &&
		row.Repository == RepositoryFullName && row.WorktreePath == "/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/dcp-review-lab/dcp-review-lab-12" &&
		row.SourceBranch == "ao/dcp-review-lab-12/root" && row.PRURL == "https://github.com/orenvlad-ai/dcp-review-lab/pull/9" && row.PRNumber == 9 &&
		row.OldHead == "d4fcb68051ae113ed497d02151a759800ee85633" && row.CurrentMain == "b34b31b5443890e69128db2862726950a6bbac0d" &&
		row.PredecessorInputDigest == "1b79923f68e0a53414579f059a1984fbcdae7aea4593d86c7fa4ae62027114bd" &&
		row.InputArtifactDigest == "131ab471a0509f4851f94e056998b3a620468a69bdd3b19435d2a225da01d393" &&
		row.ResultArtifactDigest == "e284aeb37d6fdd7ec86ee3ea6ad2272eee7d4856d5a39eb2894c89dd83d0836b" &&
		row.LogArtifactDigest == "8909c2cb81e96beb47414576fb6e1c54e9895fcf34e38e2865d87ca821b46a20" &&
		row.RebaseMetadataDigest == "db9933afbc18ffbd031818990e2b350845c766a5f0ae8ed37fae8f4e8a66f371" &&
		row.ResolvedBytesDigest == "2a5da25a78ff8bcd9aff4493f195eaefecbc70c3d4db8902dda468ccf69e5e46" &&
		row.PushRef == "refs/heads/ao/dcp-review-lab-12/root" && row.PushLeaseOldHead == row.OldHead
}

func (e *Engine) reconcileCard12ModelFreeRebase(ctx context.Context) error {
	store, ok := e.store.(ModelFreeRebaseStore)
	if !ok {
		return nil
	}
	rows, err := store.ListDCPCard12ModelFreeRebaseContinuations(ctx)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		if freshStore, freshOK := e.store.(FreshWorkerStore); freshOK {
			fresh, found, freshErr := freshStore.GetDCPCard12FreshWorkerRecovery(ctx, FreshWorkerRecoveryID)
			if freshErr != nil {
				return freshErr
			}
			if found && fresh.Status == domain.DCPFreshWorkerFailed && fresh.ErrorCode == "worker_process_failed" {
				return errors.New("card-12 model-free rebase: exact terminal predecessor lacks its authorized continuation row")
			}
		}
		return nil
	}
	if len(rows) != 1 || !exactModelFreeRebaseContinuation(rows[0]) {
		return errors.New("card-12 model-free rebase: row count or immutable identity drifted")
	}
	return e.advanceCard12ModelFreeRebaseLocked(ctx, rows[0])
}

func (e *Engine) advanceCard12ModelFreeRebaseLocked(ctx context.Context, row domain.DCPCard12ModelFreeRebaseContinuation) error {
	store, err := e.modelFreeRebaseStore()
	if err != nil {
		return err
	}
	switch row.Status {
	case domain.DCPModelFreeRebaseAuthorized:
		fail := func(code string, cause error) error {
			_, persistErr := store.FailDCPCard12ModelFreeRebaseContinuation(ctx, row.ContinuationID, code, e.clock())
			return errors.Join(cause, persistErr)
		}
		if e.modelFreeRebase == nil {
			return fail("executor_unavailable", errors.New("card-12 model-free rebase: executor is unavailable"))
		}
		if err := e.validateModelFreeRebasePredecessor(ctx, row); err != nil {
			return fail("identity_drift", err)
		}
		if err := e.modelFreeRebase.Preflight(ctx, row); err != nil {
			return fail("preflight_failed", err)
		}
		if err := e.validateModelFreeProvider(ctx, row, row.OldHead, true); err != nil {
			return fail("provider_identity_drift", err)
		}
		started, err := store.StartDCPCard12ModelFreeRebaseContinuation(ctx, row, e.clock())
		if err != nil || !started {
			return errors.Join(err, errors.New("card-12 model-free rebase: one-action fence was unavailable"))
		}
		fenced, found, err := store.GetDCPCard12ModelFreeRebaseContinuation(ctx, row.ContinuationID)
		if err != nil || !found || fenced.Status != domain.DCPModelFreeRebaseRunning || fenced.ModelFreeActionCount != 1 {
			return errors.Join(err, errors.New("card-12 model-free rebase: fenced row could not be reloaded"))
		}
		newHead, executeErr := e.modelFreeRebase.Execute(ctx, fenced)
		if executeErr != nil {
			// No Git command is retried in this process. The next controlled
			// startup may only inspect an already completed exact remote result.
			return executeErr
		}
		if err := e.validateModelFreeProvider(ctx, fenced, newHead, false); err != nil {
			return err
		}
		completed, err := store.CompleteDCPCard12ModelFreeRebaseContinuation(ctx, fenced, strings.ToLower(newHead), e.clock())
		if err != nil || !completed {
			return errors.Join(err, errors.New("card-12 model-free rebase: exact candidate transition was unavailable"))
		}
		reloaded, found, err := store.GetDCPCard12ModelFreeRebaseContinuation(ctx, row.ContinuationID)
		if err != nil || !found {
			return errors.Join(err, errors.New("card-12 model-free rebase: candidate row could not be reloaded"))
		}
		return e.advanceCard12ModelFreeRebaseLocked(ctx, reloaded)
	case domain.DCPModelFreeRebaseRunning:
		if e.modelFreeRebase == nil {
			_, failErr := store.FailDCPCard12ModelFreeRebaseContinuation(ctx, row.ContinuationID, "executor_unavailable", e.clock())
			return failErr
		}
		newHead, inspectErr := e.modelFreeRebase.InspectCompleted(ctx, row)
		if inspectErr != nil {
			_, failErr := store.FailDCPCard12ModelFreeRebaseContinuation(ctx, row.ContinuationID, "incomplete_action", e.clock())
			return errors.Join(inspectErr, failErr)
		}
		if err := e.validateModelFreeProvider(ctx, row, newHead, false); err != nil {
			_, failErr := store.FailDCPCard12ModelFreeRebaseContinuation(ctx, row.ContinuationID, "provider_identity_drift", e.clock())
			return errors.Join(err, failErr)
		}
		completed, err := store.CompleteDCPCard12ModelFreeRebaseContinuation(ctx, row, strings.ToLower(newHead), e.clock())
		if err != nil || !completed {
			return errors.Join(err, errors.New("card-12 model-free rebase: reconciled candidate transition was unavailable"))
		}
		reloaded, found, err := store.GetDCPCard12ModelFreeRebaseContinuation(ctx, row.ContinuationID)
		if err != nil || !found {
			return errors.Join(err, errors.New("card-12 model-free rebase: reconciled candidate row could not be reloaded"))
		}
		return e.advanceCard12ModelFreeRebaseLocked(ctx, reloaded)
	case domain.DCPModelFreeRebaseCandidateReady:
		if e.modelFreeReviewTrigger == nil {
			return errors.New("card-12 model-free rebase: exact reviewer trigger is unavailable")
		}
		return e.modelFreeReviewTrigger(ctx, row.SessionID)
	case domain.DCPModelFreeRebaseReviewRunning,
		domain.DCPModelFreeRebaseRecoveryReviewed, domain.DCPModelFreeRebaseSucceeded,
		domain.DCPModelFreeRebaseFailed:
		return nil
	default:
		return errors.New("card-12 model-free rebase: unknown durable state")
	}
}

func (e *Engine) validateModelFreeRebasePredecessor(ctx context.Context, row domain.DCPCard12ModelFreeRebaseContinuation) error {
	if !exactModelFreeRebaseContinuation(row) || row.ModelFreeActionCount != 0 || row.ReviewerModelCallCount != 0 || row.Revision != 0 {
		return errors.New("card-12 model-free rebase: authorization row is not pristine")
	}
	store, err := e.modelFreeRebaseStore()
	if err != nil {
		return err
	}
	countsExact, err := store.ValidateDCPCard12ModelFreeRebaseDurableCounts(ctx)
	if err != nil || !countsExact {
		return errors.Join(err, errors.New("card-12 model-free rebase: durable row counts drifted"))
	}
	freshStore, ok := e.store.(FreshWorkerStore)
	if !ok {
		return errors.New("card-12 model-free rebase: predecessor store is unavailable")
	}
	fresh, found, err := freshStore.GetDCPCard12FreshWorkerRecovery(ctx, row.PredecessorRecoveryID)
	if err != nil || !found || !exactFreshWorkerRecovery(fresh) || fresh.Status != domain.DCPFreshWorkerFailed ||
		fresh.ErrorCode != "worker_process_failed" || fresh.Revision != 5 || fresh.WorkerModelCallCount != 1 || fresh.ReviewerModelCallCount != 0 ||
		fresh.InputDigest != row.PredecessorInputDigest || fresh.LaunchID != fresh.RecoveryID || fresh.WorkerCodexSessionID != "" ||
		fresh.WorkerTokenCount != 0 || fresh.WorkerResultDigest != "" || fresh.WorkerLogDigest != "" || fresh.NewHead != "" || fresh.NewCommit != "" ||
		fresh.RecoveryReviewRunID != "" || fresh.MergeCommitSHA != "" || fresh.FinishedAt == nil {
		return errors.Join(err, errors.New("card-12 model-free rebase: immutable fresh-worker predecessor drifted"))
	}
	for _, artifact := range []struct {
		path   string
		size   int64
		digest string
	}{
		{fresh.InputPath, 3143, row.InputArtifactDigest},
		{fresh.ResultPath, 541, row.ResultArtifactDigest},
		{fresh.LogPath, 3802, row.LogArtifactDigest},
	} {
		info, statErr := os.Lstat(artifact.path)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || info.Size() != artifact.size {
			return errors.Join(statErr, errors.New("card-12 model-free rebase: predecessor artifact identity drifted"))
		}
		if err := requireRegularDigest(artifact.path, artifact.digest); err != nil {
			return err
		}
	}
	expectedArtifactRoot := filepath.Join(e.dataDir, "runtime", "dcp-card12-fresh-worker-recovery", row.PredecessorRecoveryID)
	if fresh.InputPath != filepath.Join(expectedArtifactRoot, "input.json") ||
		fresh.ResultPath != filepath.Join(expectedArtifactRoot, "worker-result.json") ||
		fresh.LogPath != filepath.Join(expectedArtifactRoot, "worker-events.jsonl") {
		return errors.New("card-12 model-free rebase: predecessor artifact paths drifted")
	}
	session, found, err := e.store.GetSession(ctx, row.SessionID)
	taskID, taskText, taskExact := arbiterTask(session)
	if err != nil || !found || !taskExact || taskID != row.TaskID || taskText != "Create canary/i13-arbiter-conflict.txt with exactly one line: arbiter intent B. Commit, push, and open the ready PR required by the profile." ||
		string(session.ProjectID) != row.ProjectID || session.Activity.State != domain.ActivityIdle || session.IsTerminated ||
		session.Harness != domain.HarnessCodex || session.Kind != domain.KindWorker || session.DisplayName != "DCP:i13-arbiter-b" ||
		session.Metadata.WorkspacePath != row.WorktreePath || session.Metadata.Branch != row.SourceBranch ||
		session.Metadata.RuntimeHandleID != "dcp-review-lab-12" || session.Metadata.AgentSessionID != "" || session.Metadata.RuntimeLaunchID != "" {
		return errors.Join(err, errors.New("card-12 model-free rebase: native session evidence drifted"))
	}
	arbiterStore, err := e.arbiterStore()
	if err != nil {
		return err
	}
	incident, found, err := arbiterStore.GetDCPReleaseArbiterIncidentByID(ctx, row.IncidentID)
	if err != nil || !found || incident.Status != domain.DCPArbiterFailed || incident.ErrorCode != "submit_failed" ||
		incident.ModelCallCount != 1 || incident.RecoveryWakeCount != 0 || incident.DecisionDigest != "" {
		return errors.Join(err, errors.New("card-12 model-free rebase: original incident drifted"))
	}
	admission, found, err := arbiterStore.GetDCPReviewLabAdmissionByID(ctx, row.AdmissionID)
	if err != nil || !found || admission.Sequence != 4 || admission.Status != domain.DCPAdmissionIncident ||
		admission.SessionID != row.SessionID || admission.TargetSHA != row.OldHead || admission.ErrorCode != "merge_conflict_or_ambiguity" ||
		admission.LeaseID != "dcp-incident-dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34" || admission.MergeCommitSHA != "" || admission.RefreshWakeCount != 0 {
		return errors.Join(err, errors.New("card-12 model-free rebase: original admission drifted"))
	}
	return nil
}

func (e *Engine) validateModelFreeProvider(ctx context.Context, row domain.DCPCard12ModelFreeRebaseContinuation, wantHead string, pre bool) error {
	prs, err := e.store.ListPRsBySession(ctx, row.SessionID)
	if err != nil || len(prs) != 1 {
		return errors.Join(err, errors.New("card-12 model-free rebase: stored PR identity drifted"))
	}
	stored := prs[0]
	if stored.URL != row.PRURL || stored.Number != int(row.PRNumber) || stored.Repo != row.Repository || stored.SourceBranch != row.SourceBranch ||
		stored.TargetBranch != TargetBranch || stored.Author != "orenvlad-ai" {
		return errors.New("card-12 model-free rebase: stored PR is foreign")
	}
	if pre && !strings.EqualFold(stored.HeadSHA, row.OldHead) {
		return errors.New("card-12 model-free rebase: stored old head drifted")
	}
	ref := ports.SCMPRRef{Repo: ports.SCMRepo{Provider: "github", Host: "github.com", Owner: "orenvlad-ai", Name: "dcp-review-lab", Repo: RepositoryFullName}, Number: int(row.PRNumber), URL: row.PRURL}
	observations, err := e.scm.FetchPullRequests(ctx, []ports.SCMPRRef{ref})
	if err != nil || len(observations) != 1 || !observations[0].Fetched {
		return errors.Join(err, errors.New("card-12 model-free rebase: provider PR could not be refreshed"))
	}
	observation, pr := observations[0], observations[0].PR
	if observation.Provider != "github" || observation.Host != "github.com" || observation.Repo != row.Repository ||
		pr.Number != int(row.PRNumber) || pr.URL != row.PRURL || pr.HeadRepo != row.Repository || pr.SourceBranch != row.SourceBranch ||
		pr.TargetBranch != TargetBranch || !strings.EqualFold(pr.HeadSHA, wantHead) || !strings.EqualFold(pr.BaseSHA, row.CurrentMain) ||
		pr.State != string(domain.PRStateOpen) || pr.ProviderState != "OPEN" || pr.Author != "orenvlad-ai" || pr.HTMLURL != pr.URL ||
		pr.Draft || pr.Merged || pr.Closed {
		return errors.New("card-12 model-free rebase: provider identity/head drifted")
	}
	if pre && (pr.ProviderMergeable != "CONFLICTING" || pr.ProviderMergeStateStatus != "DIRTY" || observation.Mergeability.State != string(domain.MergeConflicting)) {
		return errors.New("card-12 model-free rebase: preserved provider conflict state drifted")
	}
	if pre {
		required := 0
		for _, check := range observation.CI.Checks {
			if check.Name == RequiredCheckName && check.Status == string(domain.PRCheckPassed) && check.Conclusion == "success" {
				required++
			}
		}
		if required != 1 || !strings.EqualFold(observation.CI.HeadSHA, row.OldHead) {
			return errors.New("card-12 model-free rebase: old exact-head check evidence drifted")
		}
	}
	return nil
}

func (e *Engine) handleModelFreeRebaseReview(ctx context.Context, workerID domain.SessionID, run domain.ReviewRun) (bool, error) {
	if workerID != ArbiterSessionB {
		return false, nil
	}
	store, ok := e.store.(ModelFreeRebaseStore)
	if !ok {
		return false, nil
	}
	row, found, err := store.GetDCPCard12ModelFreeRebaseContinuation(ctx, ModelFreeRebaseContinuationID)
	if err != nil || !found {
		return false, err
	}
	if row.Status == domain.DCPModelFreeRebaseFailed && row.RecoveryReviewRunID == run.ID && row.NewHead == run.TargetSHA {
		return true, nil
	}
	if row.Status != domain.DCPModelFreeRebaseReviewRunning || row.RecoveryReviewRunID != run.ID || row.NewHead != run.TargetSHA {
		return false, nil
	}
	if run.Status != domain.ReviewRunComplete || run.ResultChannel != structuredChannel {
		_, failErr := store.FailDCPCard12ModelFreeRebaseReview(ctx, row, run, "review_result_malformed", e.clock())
		return true, failErr
	}
	if run.Verdict != domain.VerdictApproved {
		_, failErr := store.FailDCPCard12ModelFreeRebaseReview(ctx, row, run, "review_changes_requested", e.clock())
		return true, failErr
	}
	if err := e.enrol(ctx, workerID); err != nil {
		return true, err
	}
	return true, e.drain(ctx)
}

func (e *Engine) tryRebindModelFreeRebase(ctx context.Context, candidate mergeCandidate, observation ports.SCMObservation, now time.Time) (bool, error) {
	store, ok := e.store.(ModelFreeRebaseStore)
	if !ok {
		return false, nil
	}
	row, found, err := store.GetDCPCard12ModelFreeRebaseContinuation(ctx, ModelFreeRebaseContinuationID)
	if err != nil || !found {
		return false, err
	}
	if row.Status != domain.DCPModelFreeRebaseReviewRunning || candidate.session.ID != row.SessionID ||
		candidate.run.ID != row.RecoveryReviewRunID || candidate.run.TargetSHA != row.NewHead {
		return false, nil
	}
	if providerIdentityDrift(candidate, observation) {
		_, failErr := store.FailDCPCard12ModelFreeRebaseReview(ctx, row, candidate.run, "review_provider_identity_drift", now)
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
		_, failErr := store.FailDCPCard12ModelFreeRebaseReview(ctx, row, candidate.run, "review_not_mergeable", now)
		return true, failErr
	}
	arbiterStore, err := e.arbiterStore()
	if err != nil {
		return true, err
	}
	incident, found, err := arbiterStore.GetDCPReleaseArbiterIncidentByID(ctx, row.IncidentID)
	if err != nil || !found {
		return true, errors.New("card-12 model-free rebase: original incident disappeared")
	}
	if err := e.validateArbiterRecoveryCandidate(ctx, candidate, incident); err != nil {
		_, failErr := store.FailDCPCard12ModelFreeRebaseReview(ctx, row, candidate.run, "review_scope_drift", now)
		return true, errors.Join(err, failErr)
	}
	admission, found, err := arbiterStore.GetDCPReviewLabAdmissionByID(ctx, row.AdmissionID)
	if err != nil || !found {
		return true, errors.New("card-12 model-free rebase: original admission disappeared")
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
	rebound, err := store.RebindDCPAdmissionAfterCard12ModelFreeRebase(ctx, admission, row, candidate.run, strings.ToLower(observation.PR.BaseSHA), checkID, now)
	if err != nil || !rebound {
		return true, errors.Join(err, errors.New("card-12 model-free rebase: reviewed admission rebind was rejected"))
	}
	return true, nil
}
