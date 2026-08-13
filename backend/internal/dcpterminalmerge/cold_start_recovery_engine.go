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
	ColdStartRecoveryDigest = "087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f"
	ColdStartRecoveryID     = "dcp-card12-cold-start-recovery-" + ColdStartRecoveryDigest
	coldStartContractCommit = "623c3896a50d410e5b305ed08cf29abdc40b5b23"
)

type ColdStartRecoveryStore interface {
	GetDCPCard12ColdStartRecovery(context.Context, string) (domain.DCPCard12ColdStartRecovery, bool, error)
	ListDCPCard12ColdStartRecoveries(context.Context) ([]domain.DCPCard12ColdStartRecovery, error)
	PersistDCPCard12ColdStartBackup(context.Context, domain.DCPCard12ColdStartRecovery, string, string, time.Time) (bool, error)
	StartDCPCard12ColdStartRecovery(context.Context, domain.DCPCard12ColdStartRecovery, time.Time) (bool, error)
	CompleteDCPCard12ColdStartRecoveryAction(context.Context, domain.DCPCard12ColdStartRecovery, string, time.Time) (bool, error)
	FailDCPCard12ColdStartRecovery(context.Context, string, string, time.Time) (bool, error)
	FailDCPCard12ColdStartRecoveryReview(context.Context, domain.DCPCard12ColdStartRecovery, domain.ReviewRun, string, time.Time) (bool, error)
	RebindDCPAdmissionAfterCard12ColdStartRecovery(context.Context, domain.DCPReviewLabAdmission, domain.DCPCard12ColdStartRecovery, domain.ReviewRun, string, string, time.Time) (bool, error)
}

type ColdStartRecoveryExecutor interface {
	PrepareBackup(context.Context, domain.DCPCard12ColdStartRecovery) (string, string, error)
	Execute(context.Context, domain.DCPCard12ColdStartRecovery) (string, error)
	InspectCompleted(context.Context, domain.DCPCard12ColdStartRecovery) (string, error)
}

func (e *Engine) SetColdStartRecoveryExecutor(executor ColdStartRecoveryExecutor) {
	e.coldStartRecovery = executor
}

func exactColdStartRecovery(row domain.DCPCard12ColdStartRecovery) bool {
	return row.RecoveryID == ColdStartRecoveryID && row.Generation == 1 && row.IdentityDigest == ColdStartRecoveryDigest &&
		row.ContractCommit == coldStartContractCommit && row.PredecessorContinuationID == ModelFreeRebaseContinuationID &&
		row.IncidentID == exactSuccessorIncidentID && row.AdmissionID == "dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34" &&
		row.SessionID == ArbiterSessionB && row.TaskID == ArbiterTaskB && row.ProjectID == ProjectID && row.Repository == RepositoryFullName &&
		row.WorktreePath == "/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/dcp-review-lab/dcp-review-lab-12" &&
		row.SourceBranch == "ao/dcp-review-lab-12/root" && row.PRURL == "https://github.com/orenvlad-ai/dcp-review-lab/pull/9" && row.PRNumber == 9 &&
		row.OldHead == "d4fcb68051ae113ed497d02151a759800ee85633" && row.CurrentMain == "b34b31b5443890e69128db2862726950a6bbac0d" &&
		row.ProviderBase == modelFreeProviderBaseSHA && row.ConflictPath == arbiterConflictPath &&
		row.MarkerDigest == "5850bba009db75bf47ff88aef2d2cecbdba89c68967f51a8cdb60f48e968dc1a" &&
		row.StatusDigest == "fd7d8ff8f4918e9960e5e46e01c70a877d4218b3fa1e884ecc1723935b1c9886" &&
		row.Stage1Blob == "ed237ce2dd2684371797e22634480ffb28dc9e77" && row.Stage2Blob == "a4c945ba7328504f2efea44f076a1407c6aa7b47" &&
		row.Stage3Blob == modelFreeResolvedBlob && row.ResolvedBytesDigest == "2a5da25a78ff8bcd9aff4493f195eaefecbc70c3d4db8902dda468ccf69e5e46" &&
		row.ResolvedBlob == modelFreeResolvedBlob && row.PushRef == "refs/heads/ao/dcp-review-lab-12/root" && row.PushLeaseOldHead == row.OldHead &&
		row.UnauthorizedWorkerThread11 == "019ff9f3-cad3-73c1-bcee-293efe857349" && row.UnauthorizedWorkerTokens11 == 33238 &&
		row.UnauthorizedWorkerThread12 == "019ff9f3-cbe6-71e2-8636-ea6259a7e7d1" && row.UnauthorizedWorkerTokens12 == 33573 &&
		row.WorkerModelCallCount == 0 && row.ArbiterModelCallCount == 0
}

func (e *Engine) coldStartStore() (ColdStartRecoveryStore, error) {
	store, ok := e.store.(ColdStartRecoveryStore)
	if !ok {
		return nil, errors.New("card-12 cold-start recovery: store is unavailable")
	}
	return store, nil
}

func (e *Engine) reconcileCard12ColdStartRecovery(ctx context.Context) error {
	store, ok := e.store.(ColdStartRecoveryStore)
	if !ok {
		return nil
	}
	rows, err := store.ListDCPCard12ColdStartRecoveries(ctx)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	if len(rows) != 1 || !exactColdStartRecovery(rows[0]) {
		return errors.New("card-12 cold-start recovery: row count or immutable identity drifted")
	}
	return e.advanceCard12ColdStartRecoveryLocked(ctx, rows[0])
}

func (e *Engine) advanceCard12ColdStartRecoveryLocked(ctx context.Context, row domain.DCPCard12ColdStartRecovery) error {
	store, err := e.coldStartStore()
	if err != nil {
		return err
	}
	fail := func(code string, cause error) error {
		_, persistErr := store.FailDCPCard12ColdStartRecovery(ctx, row.RecoveryID, code, e.clock())
		return errors.Join(cause, persistErr)
	}
	switch row.Status {
	case domain.DCPColdStartRecoveryAuthorized:
		if e.coldStartRecovery == nil {
			return fail("executor_unavailable", errors.New("card-12 cold-start recovery: executor unavailable"))
		}
		if err := e.validateColdStartPredecessor(ctx, row); err != nil {
			return fail("identity_drift", err)
		}
		if err := e.validateColdStartProvider(ctx, row, row.OldHead, true); err != nil {
			return fail("provider_identity_drift", err)
		}
		backupPath, backupDigest, err := e.coldStartRecovery.PrepareBackup(ctx, row)
		if err != nil {
			return fail("preflight_or_backup_failed", err)
		}
		persisted, err := store.PersistDCPCard12ColdStartBackup(ctx, row, backupPath, backupDigest, e.clock())
		if err != nil || !persisted {
			return errors.Join(err, errors.New("card-12 cold-start recovery: backup fence unavailable"))
		}
		reloaded, found, err := store.GetDCPCard12ColdStartRecovery(ctx, row.RecoveryID)
		if err != nil || !found {
			return errors.Join(err, errors.New("card-12 cold-start recovery: backed-up row unavailable"))
		}
		return e.advanceCard12ColdStartRecoveryLocked(ctx, reloaded)
	case domain.DCPColdStartRecoveryBackedUp:
		started, err := store.StartDCPCard12ColdStartRecovery(ctx, row, e.clock())
		if err != nil || !started {
			return errors.Join(err, errors.New("card-12 cold-start recovery: one-action fence unavailable"))
		}
		fenced, found, err := store.GetDCPCard12ColdStartRecovery(ctx, row.RecoveryID)
		if err != nil || !found || fenced.Status != domain.DCPColdStartRecoveryRunning || fenced.ModelFreeActionCount != 1 {
			return errors.Join(err, errors.New("card-12 cold-start recovery: fenced row unavailable"))
		}
		newHead, executeErr := e.coldStartRecovery.Execute(ctx, fenced)
		if executeErr != nil {
			_, failErr := store.FailDCPCard12ColdStartRecovery(ctx, fenced.RecoveryID, "model_free_action_failed", e.clock())
			return errors.Join(executeErr, failErr)
		}
		if err := e.validateColdStartProvider(ctx, fenced, newHead, false); err != nil {
			_, failErr := store.FailDCPCard12ColdStartRecovery(ctx, fenced.RecoveryID, "provider_identity_drift", e.clock())
			return errors.Join(err, failErr)
		}
		completed, err := store.CompleteDCPCard12ColdStartRecoveryAction(ctx, fenced, strings.ToLower(newHead), e.clock())
		if err != nil || !completed {
			return errors.Join(err, errors.New("card-12 cold-start recovery: candidate transition unavailable"))
		}
		reloaded, found, err := store.GetDCPCard12ColdStartRecovery(ctx, row.RecoveryID)
		if err != nil || !found {
			return errors.Join(err, errors.New("card-12 cold-start recovery: candidate row unavailable"))
		}
		return e.advanceCard12ColdStartRecoveryLocked(ctx, reloaded)
	case domain.DCPColdStartRecoveryRunning:
		if e.coldStartRecovery == nil {
			return fail("executor_unavailable", errors.New("card-12 cold-start recovery: executor unavailable"))
		}
		newHead, err := e.coldStartRecovery.InspectCompleted(ctx, row)
		if err != nil {
			return fail("incomplete_action", err)
		}
		if err := e.validateColdStartProvider(ctx, row, newHead, false); err != nil {
			return fail("provider_identity_drift", err)
		}
		completed, err := store.CompleteDCPCard12ColdStartRecoveryAction(ctx, row, strings.ToLower(newHead), e.clock())
		if err != nil || !completed {
			return errors.Join(err, errors.New("card-12 cold-start recovery: reconciled candidate transition unavailable"))
		}
		reloaded, found, err := store.GetDCPCard12ColdStartRecovery(ctx, row.RecoveryID)
		if err != nil || !found {
			return errors.Join(err, errors.New("card-12 cold-start recovery: reconciled candidate row unavailable"))
		}
		return e.advanceCard12ColdStartRecoveryLocked(ctx, reloaded)
	case domain.DCPColdStartRecoveryCandidateReady:
		if e.modelFreeReviewTrigger == nil {
			return errors.New("card-12 cold-start recovery: reviewer trigger unavailable")
		}
		return e.modelFreeReviewTrigger(ctx, row.SessionID)
	case domain.DCPColdStartRecoveryReviewRunning, domain.DCPColdStartRecoveryRecoveryReviewed,
		domain.DCPColdStartRecoverySucceeded, domain.DCPColdStartRecoveryFailed:
		return nil
	default:
		return errors.New("card-12 cold-start recovery: unknown durable status")
	}
}

func (e *Engine) validateColdStartPredecessor(ctx context.Context, row domain.DCPCard12ColdStartRecovery) error {
	if !exactColdStartRecovery(row) || row.Status != domain.DCPColdStartRecoveryAuthorized || row.Revision != 0 ||
		row.ModelFreeActionCount != 0 || row.ReviewerModelCallCount != 0 || row.BackupPath != "" || row.BackupDigest != "" {
		return errors.New("card-12 cold-start recovery: authorization row is not pristine")
	}
	oldStore, ok := e.store.(ModelFreeRebaseStore)
	if !ok {
		return errors.New("card-12 cold-start recovery: predecessor store unavailable")
	}
	old, found, err := oldStore.GetDCPCard12ModelFreeRebaseContinuation(ctx, row.PredecessorContinuationID)
	if err != nil || !found || !exactModelFreeRebaseContinuation(old) || old.Status != domain.DCPModelFreeRebaseFailed ||
		old.ErrorCode != "identity_drift" || old.Revision != 1 || old.ModelFreeActionCount != 0 || old.ReviewerModelCallCount != 0 ||
		old.NewHead != "" || old.MergeCommitSHA != "" || old.FinishedAt == nil {
		return errors.Join(err, errors.New("card-12 cold-start recovery: terminal predecessor drifted"))
	}
	session, found, err := e.store.GetSession(ctx, row.SessionID)
	taskID, taskText, taskExact := arbiterTask(session)
	if err != nil || !found || !taskExact || taskID != row.TaskID || taskText != "Create canary/i13-arbiter-conflict.txt with exactly one line: arbiter intent B. Commit, push, and open the ready PR required by the profile." ||
		string(session.ProjectID) != row.ProjectID || session.Activity.State != domain.ActivityIdle || session.IsTerminated ||
		session.Harness != domain.HarnessCodex || session.Kind != domain.KindWorker || session.DisplayName != "DCP:i13-arbiter-b" ||
		session.Metadata.WorkspacePath != row.WorktreePath || session.Metadata.Branch != row.SourceBranch ||
		session.Metadata.RuntimeHandleID != "dcp-review-lab-12" || session.Metadata.AgentSessionID != "" || session.Metadata.RuntimeLaunchID != "" {
		return errors.Join(err, errors.New("card-12 cold-start recovery: native session evidence drifted"))
	}
	arbiterStore, err := e.arbiterStore()
	if err != nil {
		return err
	}
	admission, found, err := arbiterStore.GetDCPReviewLabAdmissionByID(ctx, row.AdmissionID)
	if err != nil || !found || admission.Sequence != 4 || admission.Status != domain.DCPAdmissionIncident ||
		admission.SessionID != row.SessionID || admission.TargetSHA != row.OldHead || admission.ReviewBaseSHA != row.ProviderBase ||
		admission.ErrorCode != "merge_conflict_or_ambiguity" || admission.LeaseID != "dcp-incident-dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34" ||
		admission.MergeCommitSHA != "" || admission.RefreshWakeCount != 0 {
		return errors.Join(err, errors.New("card-12 cold-start recovery: admission evidence drifted"))
	}
	return nil
}

func (e *Engine) validateColdStartProvider(ctx context.Context, row domain.DCPCard12ColdStartRecovery, wantHead string, pre bool) error {
	prs, err := e.store.ListPRsBySession(ctx, row.SessionID)
	if err != nil || len(prs) != 1 {
		return errors.Join(err, errors.New("card-12 cold-start recovery: stored PR identity drifted"))
	}
	stored := prs[0]
	if stored.URL != row.PRURL || stored.Number != int(row.PRNumber) || stored.Repo != row.Repository || stored.SourceBranch != row.SourceBranch ||
		stored.TargetBranch != TargetBranch || stored.Author != "orenvlad-ai" || !strings.EqualFold(stored.BaseSHA, row.ProviderBase) {
		return errors.New("card-12 cold-start recovery: stored PR is foreign")
	}
	ref := ports.SCMPRRef{Repo: ports.SCMRepo{Provider: "github", Host: "github.com", Owner: "orenvlad-ai", Name: "dcp-review-lab", Repo: RepositoryFullName}, Number: int(row.PRNumber), URL: row.PRURL}
	observations, err := e.scm.FetchPullRequests(ctx, []ports.SCMPRRef{ref})
	if err != nil || len(observations) != 1 || !observations[0].Fetched {
		return errors.Join(err, errors.New("card-12 cold-start recovery: provider PR refresh failed"))
	}
	observation, pr := observations[0], observations[0].PR
	if observation.Provider != "github" || observation.Host != "github.com" || observation.Repo != row.Repository || pr.Number != int(row.PRNumber) ||
		pr.URL != row.PRURL || pr.HeadRepo != row.Repository || pr.SourceBranch != row.SourceBranch || pr.TargetBranch != TargetBranch ||
		!strings.EqualFold(pr.HeadSHA, wantHead) || !strings.EqualFold(pr.BaseSHA, row.ProviderBase) || pr.State != string(domain.PRStateOpen) ||
		pr.ProviderState != "OPEN" || pr.Author != "orenvlad-ai" || pr.HTMLURL != pr.URL || pr.Draft || pr.Merged || pr.Closed {
		return errors.New("card-12 cold-start recovery: provider identity/head drifted")
	}
	if pre && (pr.ProviderMergeable != "CONFLICTING" || pr.ProviderMergeStateStatus != "DIRTY" || observation.Mergeability.State != string(domain.MergeConflicting)) {
		return errors.New("card-12 cold-start recovery: provider conflict state drifted")
	}
	return nil
}

func (e *Engine) handleColdStartRecoveryReview(ctx context.Context, workerID domain.SessionID, run domain.ReviewRun) (bool, error) {
	if workerID != ArbiterSessionB {
		return false, nil
	}
	store, ok := e.store.(ColdStartRecoveryStore)
	if !ok {
		return false, nil
	}
	row, found, err := store.GetDCPCard12ColdStartRecovery(ctx, ColdStartRecoveryID)
	if err != nil || !found {
		return false, err
	}
	if row.Status == domain.DCPColdStartRecoveryFailed && row.RecoveryReviewRunID == run.ID && row.NewHead == run.TargetSHA {
		return true, nil
	}
	if row.Status != domain.DCPColdStartRecoveryReviewRunning || row.RecoveryReviewRunID != run.ID || row.NewHead != run.TargetSHA {
		return false, nil
	}
	if run.Status != domain.ReviewRunComplete || run.ResultChannel != structuredChannel {
		_, failErr := store.FailDCPCard12ColdStartRecoveryReview(ctx, row, run, "review_result_malformed", e.clock())
		return true, failErr
	}
	if run.Verdict != domain.VerdictApproved {
		_, failErr := store.FailDCPCard12ColdStartRecoveryReview(ctx, row, run, "review_changes_requested", e.clock())
		return true, failErr
	}
	if err := e.enrol(ctx, workerID); err != nil {
		return true, err
	}
	return true, e.drain(ctx)
}

func (e *Engine) tryRebindColdStartRecovery(ctx context.Context, candidate mergeCandidate, observation ports.SCMObservation, now time.Time) (bool, error) {
	store, ok := e.store.(ColdStartRecoveryStore)
	if !ok {
		return false, nil
	}
	row, found, err := store.GetDCPCard12ColdStartRecovery(ctx, ColdStartRecoveryID)
	if err != nil || !found {
		return false, err
	}
	if row.Status != domain.DCPColdStartRecoveryReviewRunning || candidate.session.ID != row.SessionID ||
		candidate.run.ID != row.RecoveryReviewRunID || candidate.run.TargetSHA != row.NewHead {
		return false, nil
	}
	if providerIdentityDrift(candidate, observation) {
		_, failErr := store.FailDCPCard12ColdStartRecoveryReview(ctx, row, candidate.run, "review_provider_identity_drift", now)
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
		_, failErr := store.FailDCPCard12ColdStartRecoveryReview(ctx, row, candidate.run, "review_not_mergeable", now)
		return true, failErr
	}
	arbiterStore, err := e.arbiterStore()
	if err != nil {
		return true, err
	}
	incident, found, err := arbiterStore.GetDCPReleaseArbiterIncidentByID(ctx, row.IncidentID)
	if err != nil || !found {
		return true, errors.New("card-12 cold-start recovery: original incident disappeared")
	}
	if err := e.validateArbiterRecoveryCandidate(ctx, candidate, incident); err != nil {
		_, failErr := store.FailDCPCard12ColdStartRecoveryReview(ctx, row, candidate.run, "review_scope_drift", now)
		return true, errors.Join(err, failErr)
	}
	admission, found, err := arbiterStore.GetDCPReviewLabAdmissionByID(ctx, row.AdmissionID)
	if err != nil || !found {
		return true, errors.New("card-12 cold-start recovery: original admission disappeared")
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
	rebound, err := store.RebindDCPAdmissionAfterCard12ColdStartRecovery(ctx, admission, row, candidate.run, strings.ToLower(observation.PR.BaseSHA), checkID, now)
	if err != nil || !rebound {
		return true, errors.Join(err, errors.New("card-12 cold-start recovery: reviewed admission rebind rejected"))
	}
	return true, nil
}
