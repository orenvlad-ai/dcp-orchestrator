package dcpterminalmerge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	FreshWorkerRecoveryDigest = "d2b7142bc9e5844ba165abe24d3222b3e1a94c3577fba5f6f8d97ec3dbad151b"
	FreshWorkerRecoveryID     = "dcp-card12-fresh-worker-recovery-" + FreshWorkerRecoveryDigest
	freshWorkerResultSchema   = "dcp.review-lab.card12-fresh-worker-result/v1"
	freshWorkerInputSchema    = "dcp.review-lab.card12-fresh-worker-input/v1"
	freshWorkerExpectedBytes  = "arbiter intent A\narbiter intent B\n"
)

var codexSessionIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type FreshWorkerStore interface {
	GetDCPCard12FreshWorkerRecovery(context.Context, string) (domain.DCPCard12FreshWorkerRecovery, bool, error)
	ListDCPCard12FreshWorkerRecoveries(context.Context) ([]domain.DCPCard12FreshWorkerRecovery, error)
	PrepareDCPCard12FreshWorkerRecovery(context.Context, domain.DCPCard12FreshWorkerRecovery, time.Time) (bool, error)
	StartDCPCard12FreshWorkerRecovery(context.Context, domain.DCPCard12FreshWorkerRecovery, time.Time) (bool, error)
	FailDCPCard12FreshWorkerPreflight(context.Context, string, string, time.Time) (bool, error)
	FailDCPCard12FreshWorkerCall(context.Context, string, string, time.Time) (bool, error)
	CompleteDCPCard12FreshWorkerCall(context.Context, domain.DCPCard12FreshWorkerRecovery, time.Time) (bool, error)
	FailDCPCard12FreshRecoveryReview(context.Context, domain.DCPCard12FreshWorkerRecovery, domain.ReviewRun, string, time.Time) (bool, error)
	RebindDCPAdmissionAfterCard12FreshWorkerRecovery(context.Context, domain.DCPReviewLabAdmission, domain.DCPCard12FreshWorkerRecovery, domain.ReviewRun, string, string, time.Time) (bool, error)
}

type FreshWorkerExitReport struct {
	Started  bool
	ExitCode int
}

func (e *Engine) SetFreshWorkerLauncher(launcher FreshWorkerLauncher) { e.freshWorker = launcher }

func (e *Engine) freshWorkerStore() (FreshWorkerStore, error) {
	store, ok := e.store.(FreshWorkerStore)
	if !ok {
		return nil, errors.New("card-12 fresh worker: durable store surface is unavailable")
	}
	return store, nil
}

func exactFreshWorkerRecovery(r domain.DCPCard12FreshWorkerRecovery) bool {
	return r.RecoveryID == FreshWorkerRecoveryID && r.RecoveryGeneration == 1 && r.RecoveryIdentityDigest == FreshWorkerRecoveryDigest &&
		r.IncidentID == exactSuccessorIncidentID && r.IncidentGeneration == 1 &&
		r.SuccessorAttemptID == ArbiterSuccessorAttemptID && r.SuccessorAttemptGeneration == 2 &&
		r.SuccessorIdentityDigest == ArbiterSuccessorAttemptDigest && r.AcceptedDecisionDigest == "237472879b22a8db65c5a3a0715510dc17aee1de93c45eaab45dde538cefb939" &&
		r.AdmissionID == "dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34" && r.SessionID == ArbiterSessionB &&
		r.TaskID == ArbiterTaskB && r.ProjectID == ProjectID && r.Repository == RepositoryFullName &&
		r.SourceBranch == "ao/dcp-review-lab-12/root" && r.PRURL == "https://github.com/orenvlad-ai/dcp-review-lab/pull/9" && r.PRNumber == 9 &&
		r.OldHead == "d4fcb68051ae113ed497d02151a759800ee85633" && r.CurrentMain == "b34b31b5443890e69128db2862726950a6bbac0d" &&
		r.PredecessorStatus == "failed" && r.PredecessorError == "repair_launch_failed" &&
		r.OldRuntimeHandleID == "dcp-review-lab-12" && r.OldAgentSessionID == "" && r.OldRuntimeLaunchID == "" &&
		r.ContractCommit == "2a174899ae72bf1db548c3b2f172d963488191f1" && r.Model == ArbiterModel && r.Reasoning == ArbiterReasoning && r.TokenBudget == 16384 &&
		r.RuntimeActionID == FreshWorkerRecoveryID && r.RuntimeHandleID == "dcp-card12-fresh-worker-recovery"
}

func (e *Engine) reconcileCard12FreshWorker(ctx context.Context) error {
	store, ok := e.store.(FreshWorkerStore)
	if !ok {
		return nil
	}
	rows, err := store.ListDCPCard12FreshWorkerRecoveries(ctx)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		_, successor, found, stateErr := e.exactSuccessorState(ctx)
		if stateErr != nil {
			return stateErr
		}
		if found && successor.Status == domain.DCPArbiterSuccessorFailed && successor.ErrorCode == "repair_launch_failed" {
			return errors.New("card-12 fresh worker: exact terminal predecessor lacks its authorized recovery row")
		}
		return nil
	}
	if len(rows) != 1 || !exactFreshWorkerRecovery(rows[0]) {
		return errors.New("card-12 fresh worker: recovery row count or immutable identity drifted")
	}
	return e.advanceCard12FreshWorkerLocked(ctx, rows[0])
}

func (e *Engine) advanceCard12FreshWorkerLocked(ctx context.Context, recovery domain.DCPCard12FreshWorkerRecovery) error {
	store, err := e.freshWorkerStore()
	if err != nil {
		return err
	}
	switch recovery.Status {
	case domain.DCPFreshWorkerAuthorized:
		incident, _, err := e.revalidateFreshWorkerPredecessor(ctx, recovery)
		if err != nil {
			_, persistErr := store.FailDCPCard12FreshWorkerPreflight(ctx, recovery.RecoveryID, "identity_drift", e.clock())
			return errors.Join(err, persistErr)
		}
		prepared, err := e.deriveFreshWorkerInput(ctx, recovery, incident)
		if err != nil {
			_, persistErr := store.FailDCPCard12FreshWorkerPreflight(ctx, recovery.RecoveryID, "input_derivation_failed", e.clock())
			return errors.Join(err, persistErr)
		}
		updated, err := store.PrepareDCPCard12FreshWorkerRecovery(ctx, prepared, e.clock())
		if err != nil || !updated {
			return errors.Join(err, errors.New("card-12 fresh worker: exact input preparation was unavailable"))
		}
		reloaded, ok, err := store.GetDCPCard12FreshWorkerRecovery(ctx, recovery.RecoveryID)
		if err != nil || !ok {
			return errors.Join(err, errors.New("card-12 fresh worker: prepared row could not be reloaded"))
		}
		return e.advanceCard12FreshWorkerLocked(ctx, reloaded)
	case domain.DCPFreshWorkerRequested:
		if e.freshWorker == nil {
			_, failErr := store.FailDCPCard12FreshWorkerPreflight(ctx, recovery.RecoveryID, "launcher_unavailable", e.clock())
			return failErr
		}
		if _, _, err := e.revalidateFreshWorkerPredecessor(ctx, recovery); err != nil {
			_, persistErr := store.FailDCPCard12FreshWorkerPreflight(ctx, recovery.RecoveryID, "identity_drift", e.clock())
			return errors.Join(err, persistErr)
		}
		if err := e.freshWorker.PreflightFreshWorker(ctx, recovery); err != nil {
			_, persistErr := store.FailDCPCard12FreshWorkerPreflight(ctx, recovery.RecoveryID, "preflight_failed", e.clock())
			return errors.Join(err, persistErr)
		}
		started, err := store.StartDCPCard12FreshWorkerRecovery(ctx, recovery, e.clock())
		if err != nil || !started {
			return errors.Join(err, errors.New("card-12 fresh worker: one-call fence was unavailable"))
		}
		fenced, ok, err := store.GetDCPCard12FreshWorkerRecovery(ctx, recovery.RecoveryID)
		if err != nil || !ok || fenced.Status != domain.DCPFreshWorkerRunning || fenced.WorkerModelCallCount != 1 {
			return errors.New("card-12 fresh worker: one-call fence could not be reloaded")
		}
		if err := e.freshWorker.LaunchFreshWorker(ctx, fenced); err != nil {
			_, persistErr := store.FailDCPCard12FreshWorkerCall(ctx, recovery.RecoveryID, "launch_failed", e.clock())
			return errors.Join(err, persistErr)
		}
		return nil
	case domain.DCPFreshWorkerRunning:
		if _, err := os.Lstat(recovery.ResultPath); err == nil {
			return e.consumeFreshWorkerResultLocked(ctx, recovery, FreshWorkerExitReport{Started: true, ExitCode: 0})
		} else if !os.IsNotExist(err) {
			return err
		}
		if e.freshWorker == nil {
			_, failErr := store.FailDCPCard12FreshWorkerCall(ctx, recovery.RecoveryID, "launcher_unavailable", e.clock())
			return failErr
		}
		alive, err := e.freshWorker.FreshWorkerProcessAlive(ctx, recovery)
		if err != nil {
			return err
		}
		if !alive {
			_, err = store.FailDCPCard12FreshWorkerCall(ctx, recovery.RecoveryID, "missing_result", e.clock())
		}
		return err
	case domain.DCPFreshWorkerPreflightFailed, domain.DCPFreshWorkerSucceeded,
		domain.DCPFreshReviewerRunning, domain.DCPFreshWorkerRecoveryReviewed,
		domain.DCPFreshWorkerComplete, domain.DCPFreshWorkerFailed:
		return nil
	default:
		return errors.New("card-12 fresh worker: unknown durable state")
	}
}

func (e *Engine) revalidateFreshWorkerPredecessor(ctx context.Context, recovery domain.DCPCard12FreshWorkerRecovery) (domain.DCPReleaseArbiterIncident, domain.DCPReleaseArbiterSuccessorAttempt, error) {
	if !exactFreshWorkerRecovery(recovery) || recovery.WorkerModelCallCount != 0 || recovery.ReviewerModelCallCount != 0 {
		return domain.DCPReleaseArbiterIncident{}, domain.DCPReleaseArbiterSuccessorAttempt{}, errors.New("card-12 fresh worker: recovery predecessor identity is invalid")
	}
	incident, successor, ok, err := e.exactSuccessorState(ctx)
	if err != nil || !ok || successor.Status != domain.DCPArbiterSuccessorFailed || successor.ErrorCode != "repair_launch_failed" ||
		successor.DecisionDigest != recovery.AcceptedDecisionDigest || successor.RecoveryOwnerSessionID != recovery.SessionID ||
		successor.RecoveryPath != "same_worker_conflict_repair" || successor.RecoveryWakeCount != 1 ||
		successor.RecoveryReviewRunID != "" || successor.RecoveryTargetSHA != "" {
		return domain.DCPReleaseArbiterIncident{}, domain.DCPReleaseArbiterSuccessorAttempt{}, errors.Join(err, errors.New("card-12 fresh worker: terminal successor predecessor drifted"))
	}
	if err := e.revalidateArbiterSuccessor(ctx, incident, successor); err != nil {
		return domain.DCPReleaseArbiterIncident{}, domain.DCPReleaseArbiterSuccessorAttempt{}, err
	}
	session, found, err := e.store.GetSession(ctx, recovery.SessionID)
	if err != nil || !found || string(session.ProjectID) != recovery.ProjectID || session.Activity.State != domain.ActivityIdle || session.IsTerminated ||
		session.Harness != domain.HarnessCodex || session.Metadata.WorkspacePath != recovery.WorktreePath ||
		session.Metadata.RuntimeHandleID != recovery.OldRuntimeHandleID || session.Metadata.AgentSessionID != "" || session.Metadata.RuntimeLaunchID != "" {
		return domain.DCPReleaseArbiterIncident{}, domain.DCPReleaseArbiterSuccessorAttempt{}, errors.New("card-12 fresh worker: native session evidence drifted")
	}
	if err := e.validateFreshWorkerGit(ctx, recovery, false); err != nil {
		return domain.DCPReleaseArbiterIncident{}, domain.DCPReleaseArbiterSuccessorAttempt{}, err
	}
	return incident, successor, nil
}

type freshWorkerInput struct {
	SchemaVersion string `json:"schemaVersion"`
	Recovery      struct {
		ID                     string `json:"id"`
		Generation             int64  `json:"generation"`
		IdentityDigest         string `json:"identityDigest"`
		IncidentID             string `json:"incidentId"`
		SuccessorAttemptID     string `json:"successorAttemptId"`
		AcceptedDecisionDigest string `json:"acceptedDecisionDigest"`
	} `json:"recovery"`
	Scope struct {
		TaskID                string `json:"taskId"`
		TaskText              string `json:"taskText"`
		ScopeDigest           string `json:"scopeDigest"`
		FixedSyntheticProfile string `json:"fixedSyntheticProfile"`
	} `json:"scope"`
	Identity struct {
		ProjectID    string `json:"projectId"`
		SessionID    string `json:"sessionId"`
		WorktreePath string `json:"worktreePath"`
		GitDir       string `json:"gitDir"`
		GitCommonDir string `json:"gitCommonDir"`
		Branch       string `json:"branch"`
		Repository   string `json:"repository"`
		PRURL        string `json:"prUrl"`
		PRNumber     int64  `json:"prNumber"`
		OldHead      string `json:"oldHead"`
		CurrentMain  string `json:"currentMain"`
	} `json:"identity"`
	Conflict struct {
		Paths            []string `json:"paths"`
		CurrentMainBytes string   `json:"currentMainBytes"`
		CandidateBytes   string   `json:"candidateBytes"`
		RequiredBytes    string   `json:"requiredBytes"`
	} `json:"conflict"`
	PushLease struct {
		Ref             string `json:"ref"`
		ExpectedOldHead string `json:"expectedOldHead"`
	} `json:"pushLease"`
	Policy struct {
		MaxWorkerCalls  int64    `json:"maxWorkerCalls"`
		MaxFreshReviews int64    `json:"maxFreshReviews"`
		MaxTokens       int64    `json:"maxTokens"`
		SameBranchOnly  bool     `json:"sameBranchOnly"`
		Prohibitions    []string `json:"prohibitions"`
	} `json:"policy"`
}

func (e *Engine) deriveFreshWorkerInput(ctx context.Context, recovery domain.DCPCard12FreshWorkerRecovery, incident domain.DCPReleaseArbiterIncident) (domain.DCPCard12FreshWorkerRecovery, error) {
	gitDir, err := e.git(ctx, recovery.WorktreePath, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return recovery, err
	}
	commonDir, err := e.git(ctx, recovery.WorktreePath, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return recovery, err
	}
	var input freshWorkerInput
	input.SchemaVersion = freshWorkerInputSchema
	input.Recovery.ID, input.Recovery.Generation, input.Recovery.IdentityDigest = recovery.RecoveryID, 1, recovery.RecoveryIdentityDigest
	input.Recovery.IncidentID, input.Recovery.SuccessorAttemptID, input.Recovery.AcceptedDecisionDigest = recovery.IncidentID, recovery.SuccessorAttemptID, recovery.AcceptedDecisionDigest
	input.Scope.TaskID, input.Scope.TaskText, input.Scope.ScopeDigest, input.Scope.FixedSyntheticProfile = recovery.TaskID,
		"Create canary/i13-arbiter-conflict.txt with exactly one line: arbiter intent B. Commit, push, and open the ready PR required by the profile.", incident.ScopeDigest, ProfileAgentRules
	input.Identity.ProjectID, input.Identity.SessionID, input.Identity.WorktreePath = recovery.ProjectID, string(recovery.SessionID), recovery.WorktreePath
	input.Identity.GitDir, input.Identity.GitCommonDir, input.Identity.Branch = gitDir, commonDir, recovery.SourceBranch
	input.Identity.Repository, input.Identity.PRURL, input.Identity.PRNumber = recovery.Repository, recovery.PRURL, recovery.PRNumber
	input.Identity.OldHead, input.Identity.CurrentMain = recovery.OldHead, recovery.CurrentMain
	input.Conflict.Paths = []string{arbiterConflictPath}
	input.Conflict.CurrentMainBytes, input.Conflict.CandidateBytes, input.Conflict.RequiredBytes = "arbiter intent A\n", "arbiter intent B\n", freshWorkerExpectedBytes
	input.PushLease.Ref, input.PushLease.ExpectedOldHead = "refs/heads/"+recovery.SourceBranch, recovery.OldHead
	input.Policy.MaxWorkerCalls, input.Policy.MaxFreshReviews, input.Policy.MaxTokens, input.Policy.SameBranchOnly = 1, 1, recovery.TokenBudget, true
	input.Policy.Prohibitions = []string{"other paths", "other repositories", "new card/task/worktree/branch/PR/incident", "arbiter call or decision", "review or merge", "retry or transcript replay"}
	digest, canonical, err := canonicalDigest(input)
	if err != nil || len(canonical) > 8192 {
		return recovery, errors.Join(err, errors.New("card-12 fresh worker: bounded input is invalid"))
	}
	root := filepath.Join(e.dataDir, "runtime", "dcp-card12-fresh-worker-recovery", recovery.RecoveryID)
	recovery.InputJSON, recovery.InputDigest = string(canonical), digest
	recovery.InputPath, recovery.ResultPath, recovery.LogPath = filepath.Join(root, "input.json"), filepath.Join(root, "worker-result.json"), filepath.Join(root, "worker-events.jsonl")
	return recovery, nil
}

func (e *Engine) validateFreshWorkerGit(ctx context.Context, recovery domain.DCPCard12FreshWorkerRecovery, post bool) error {
	repo := recovery.WorktreePath
	if !filepath.IsAbs(repo) || filepath.Clean(repo) != repo {
		return errors.New("card-12 fresh worker: worktree path is not exact")
	}
	checks := []struct {
		args []string
		want string
	}{
		{[]string{"rev-parse", "--show-toplevel"}, repo}, {[]string{"branch", "--show-current"}, recovery.SourceBranch},
		{[]string{"remote"}, "origin"}, {[]string{"remote", "get-url", "origin"}, RepositoryURL},
		{[]string{"remote", "get-url", "--push", "origin"}, RepositoryURL}, {[]string{"status", "--porcelain=v1", "--untracked-files=all"}, ""},
	}
	for _, check := range checks {
		got, err := e.git(ctx, repo, check.args...)
		if err != nil || got != check.want {
			return errors.New("card-12 fresh worker: worktree/Git identity is not exact and clean")
		}
	}
	for _, residue := range []string{"MERGE_HEAD", "rebase-apply", "rebase-merge", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG", "index.lock"} {
		path, err := e.git(ctx, repo, "rev-parse", "--git-path", residue)
		if err != nil {
			return err
		}
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("card-12 fresh worker: Git residue %s exists", residue)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if _, err := e.git(ctx, repo, "fetch", "--no-tags", "origin",
		"+refs/heads/main:refs/remotes/origin/main",
		"+refs/heads/"+recovery.SourceBranch+":refs/remotes/origin/"+recovery.SourceBranch); err != nil {
		return err
	}
	mainSHA, err := e.git(ctx, repo, "rev-parse", "refs/remotes/origin/main")
	if err != nil || mainSHA != recovery.CurrentMain {
		return errors.New("card-12 fresh worker: origin/main drifted")
	}
	head, err := e.git(ctx, repo, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	remoteHead, err := e.git(ctx, repo, "rev-parse", "refs/remotes/origin/"+recovery.SourceBranch)
	if err != nil || remoteHead != head {
		return errors.New("card-12 fresh worker: guarded remote branch outcome is not exact")
	}
	if !post {
		if head != recovery.OldHead {
			return errors.New("card-12 fresh worker: old worktree head drifted")
		}
		status, err := e.git(ctx, repo, "diff", "--name-status", recovery.CurrentMain+".."+recovery.OldHead)
		if err != nil || status != "A\t"+arbiterConflictPath {
			return errors.New("card-12 fresh worker: old candidate diff drifted")
		}
		current, err := e.git(ctx, repo, "show", recovery.CurrentMain+":"+arbiterConflictPath)
		if err != nil || current != "arbiter intent A" {
			return errors.New("card-12 fresh worker: current-main bytes drifted")
		}
		candidate, err := e.git(ctx, repo, "show", recovery.OldHead+":"+arbiterConflictPath)
		if err != nil || candidate != "arbiter intent B" {
			return errors.New("card-12 fresh worker: candidate bytes drifted")
		}
		return nil
	}
	if head == recovery.OldHead || !validSHA(head) {
		return errors.New("card-12 fresh worker: no exact new head was produced")
	}
	parents, err := e.git(ctx, repo, "show", "-s", "--format=%P", head)
	if err != nil || parents != recovery.CurrentMain {
		return errors.New("card-12 fresh worker: new head is not one direct commit on current main")
	}
	status, err := e.git(ctx, repo, "diff", "--name-status", recovery.CurrentMain+".."+head)
	if err != nil || status != "M\t"+arbiterConflictPath {
		return errors.New("card-12 fresh worker: new head changed foreign scope")
	}
	content, err := e.git(ctx, repo, "show", head+":"+arbiterConflictPath)
	if err != nil || content != strings.TrimSuffix(freshWorkerExpectedBytes, "\n") {
		return errors.New("card-12 fresh worker: repaired bytes are not exact")
	}
	return nil
}

type freshWorkerResult struct {
	SchemaVersion  string `json:"schemaVersion"`
	RecoveryID     string `json:"recoveryId"`
	IdentityDigest string `json:"identityDigest"`
	InputDigest    string `json:"inputDigest"`
	CodexSessionID string `json:"codexSessionId"`
	TokenCount     int64  `json:"tokenCount"`
	Started        bool   `json:"started"`
	ExitCode       int    `json:"exitCode"`
	LogDigest      string `json:"logDigest"`
	LogOverflow    bool   `json:"logOverflow"`
}

func (e *Engine) ProcessFreshWorkerExit(ctx context.Context, recoveryID string, report FreshWorkerExitReport) error {
	if err := e.configured(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	store, err := e.freshWorkerStore()
	if err != nil {
		return err
	}
	recovery, ok, err := store.GetDCPCard12FreshWorkerRecovery(ctx, strings.TrimSpace(recoveryID))
	if err != nil || !ok {
		return errors.New("card-12 fresh worker: exact recovery was not found")
	}
	if recovery.Status != domain.DCPFreshWorkerRunning {
		return nil
	}
	return e.consumeFreshWorkerResultLocked(ctx, recovery, report)
}

func (e *Engine) consumeFreshWorkerResultLocked(ctx context.Context, recovery domain.DCPCard12FreshWorkerRecovery, exit FreshWorkerExitReport) error {
	store, _ := e.freshWorkerStore()
	fail := func(code string, cause error) error {
		_, err := store.FailDCPCard12FreshWorkerCall(ctx, recovery.RecoveryID, code, e.clock())
		return errors.Join(cause, err)
	}
	if !exit.Started || exit.ExitCode != 0 {
		return fail("worker_process_failed", errors.New("card-12 fresh worker: worker process did not exit successfully"))
	}
	resultInfo, err := os.Lstat(recovery.ResultPath)
	if err != nil || !resultInfo.Mode().IsRegular() || resultInfo.Mode()&os.ModeSymlink != 0 || resultInfo.Mode().Perm()&0o022 != 0 || resultInfo.Size() <= 0 || resultInfo.Size() > 4096 {
		return fail("malformed_result", err)
	}
	resultBytes, err := os.ReadFile(recovery.ResultPath)
	if err != nil {
		return fail("malformed_result", err)
	}
	logInfo, err := os.Lstat(recovery.LogPath)
	if err != nil || !logInfo.Mode().IsRegular() || logInfo.Mode()&os.ModeSymlink != 0 || logInfo.Mode().Perm()&0o022 != 0 || logInfo.Size() <= 0 || logInfo.Size() > 2<<20 {
		return fail("malformed_log", err)
	}
	logBytes, err := os.ReadFile(recovery.LogPath)
	if err != nil {
		return fail("malformed_log", err)
	}
	var result freshWorkerResult
	if json.Unmarshal(resultBytes, &result) != nil || result.SchemaVersion != freshWorkerResultSchema || result.RecoveryID != recovery.RecoveryID ||
		result.IdentityDigest != recovery.RecoveryIdentityDigest || result.InputDigest != recovery.InputDigest || !result.Started || result.ExitCode != 0 || result.LogOverflow ||
		!codexSessionIDPattern.MatchString(result.CodexSessionID) || result.TokenCount <= 0 || result.TokenCount > recovery.TokenBudget || result.LogDigest != digestBytes(logBytes) {
		return fail("malformed_result", errors.New("card-12 fresh worker: sealed result identity is invalid"))
	}
	if err := validateFreshWorkerCommandLog(logBytes, recovery); err != nil {
		return fail("guarded_push_not_proven", err)
	}
	if err := e.validateFreshWorkerGit(ctx, recovery, true); err != nil {
		return fail("worker_postcondition_failed", err)
	}
	head, _ := e.git(ctx, recovery.WorktreePath, "rev-parse", "HEAD")
	prs, err := e.store.ListPRsBySession(ctx, recovery.SessionID)
	if err != nil || len(prs) != 1 {
		return fail("provider_identity_drift", err)
	}
	ref := ports.SCMPRRef{Repo: ports.SCMRepo{Provider: "github", Host: "github.com", Owner: "orenvlad-ai", Name: "dcp-review-lab", Repo: RepositoryFullName}, Number: int(recovery.PRNumber), URL: recovery.PRURL}
	observations, err := e.scm.FetchPullRequests(ctx, []ports.SCMPRRef{ref})
	if err != nil || len(observations) != 1 || !observations[0].Fetched {
		return fail("provider_identity_drift", err)
	}
	pr := observations[0].PR
	if observations[0].Provider != "github" || observations[0].Repo != recovery.Repository || pr.Number != int(recovery.PRNumber) || pr.URL != recovery.PRURL ||
		pr.SourceBranch != recovery.SourceBranch || pr.TargetBranch != TargetBranch || !strings.EqualFold(pr.HeadSHA, head) || !strings.EqualFold(pr.BaseSHA, recovery.CurrentMain) ||
		pr.State != string(domain.PRStateOpen) || pr.ProviderState != "OPEN" || pr.Draft || pr.Merged || pr.Closed || pr.Author != "orenvlad-ai" {
		return fail("provider_identity_drift", errors.New("card-12 fresh worker: provider did not prove the exact same PR/new head"))
	}
	recovery.WorkerCodexSessionID, recovery.WorkerTokenCount = result.CodexSessionID, result.TokenCount
	recovery.WorkerResultDigest, recovery.WorkerLogDigest = digestBytes(resultBytes), result.LogDigest
	recovery.NewHead, recovery.NewCommit = strings.ToLower(head), strings.ToLower(head)
	updated, err := store.CompleteDCPCard12FreshWorkerCall(ctx, recovery, e.clock())
	if err != nil || !updated {
		return errors.Join(err, errors.New("card-12 fresh worker: trusted new-head transition was unavailable"))
	}
	return nil
}

func validateFreshWorkerCommandLog(data []byte, recovery domain.DCPCard12FreshWorkerRecovery) error {
	var commands []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var value any
		if json.Unmarshal([]byte(line), &value) != nil {
			continue
		}
		collectFreshWorkerCommands(value, &commands)
	}
	pushes := make([]string, 0, 1)
	for _, command := range commands {
		lower := strings.ToLower(command)
		if strings.Contains(lower, "git push") || strings.Contains(lower, "git -c") && strings.Contains(lower, " push ") {
			if strings.Count(lower, " push ") > 1 {
				return errors.New("card-12 fresh worker: one command contains multiple push operations")
			}
			pushes = append(pushes, command)
		}
		for _, forbidden := range []string{"gh pr create", "git worktree add", "git checkout -b", "git switch -c"} {
			if strings.Contains(lower, forbidden) {
				return errors.New("card-12 fresh worker: event log contains a forbidden identity mutation")
			}
		}
	}
	lease := "--force-with-lease=refs/heads/" + recovery.SourceBranch + ":" + recovery.OldHead
	target := "HEAD:refs/heads/" + recovery.SourceBranch
	if len(pushes) != 1 || !strings.Contains(pushes[0], lease) || !strings.Contains(pushes[0], " origin ") || !strings.Contains(pushes[0], target) {
		return errors.New("card-12 fresh worker: event log does not prove exactly one exact guarded push")
	}
	return nil
}

func collectFreshWorkerCommands(value any, commands *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "command" {
				if command, ok := child.(string); ok && strings.TrimSpace(command) != "" {
					*commands = append(*commands, command)
				}
			}
			collectFreshWorkerCommands(child, commands)
		}
	case []any:
		for _, child := range typed {
			collectFreshWorkerCommands(child, commands)
		}
	}
}

func (e *Engine) HandleFreshRecoveryReview(ctx context.Context, workerID domain.SessionID, run domain.ReviewRun) (bool, error) {
	if workerID != ArbiterSessionB {
		return false, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	store, err := e.freshWorkerStore()
	if err != nil {
		return false, err
	}
	recovery, ok, err := store.GetDCPCard12FreshWorkerRecovery(ctx, FreshWorkerRecoveryID)
	if err != nil || !ok {
		return false, err
	}
	if recovery.Status == domain.DCPFreshWorkerFailed && recovery.RecoveryReviewRunID == run.ID &&
		recovery.NewHead == run.TargetSHA && recovery.ErrorCode == "review_changes_requested" {
		return true, nil
	}
	if recovery.Status != domain.DCPFreshReviewerRunning || recovery.RecoveryReviewRunID != run.ID || recovery.NewHead != run.TargetSHA {
		return false, nil
	}
	if run.Status != domain.ReviewRunComplete || run.ResultChannel != structuredChannel {
		_, failErr := store.FailDCPCard12FreshRecoveryReview(ctx, recovery, run, "review_result_malformed", e.clock())
		return true, failErr
	}
	if run.Verdict != domain.VerdictApproved {
		_, failErr := store.FailDCPCard12FreshRecoveryReview(ctx, recovery, run, "review_changes_requested", e.clock())
		return true, failErr
	}
	if err := e.enrol(ctx, workerID); err != nil {
		return true, err
	}
	return true, e.drain(ctx)
}

func (e *Engine) tryRebindFreshRecovery(ctx context.Context, candidate mergeCandidate, observation ports.SCMObservation, now time.Time) (bool, error) {
	store, ok := e.store.(FreshWorkerStore)
	if !ok {
		return false, nil
	}
	recovery, found, err := store.GetDCPCard12FreshWorkerRecovery(ctx, FreshWorkerRecoveryID)
	if err != nil || !found {
		return false, err
	}
	if recovery.Status != domain.DCPFreshReviewerRunning || candidate.session.ID != recovery.SessionID || candidate.run.ID != recovery.RecoveryReviewRunID || candidate.run.TargetSHA != recovery.NewHead {
		return false, nil
	}
	if providerIdentityDrift(candidate, observation) {
		_, failErr := store.FailDCPCard12FreshRecoveryReview(ctx, recovery, candidate.run, "review_provider_identity_drift", now)
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
		_, failErr := store.FailDCPCard12FreshRecoveryReview(ctx, recovery, candidate.run, "review_not_mergeable", now)
		return true, failErr
	}
	arbiterStore, err := e.arbiterStore()
	if err != nil {
		return true, err
	}
	incident, found, err := arbiterStore.GetDCPReleaseArbiterIncidentByID(ctx, recovery.IncidentID)
	if err != nil || !found {
		return true, errors.New("card-12 fresh worker: original incident disappeared")
	}
	if err := e.validateArbiterRecoveryCandidate(ctx, candidate, incident); err != nil {
		_, failErr := store.FailDCPCard12FreshRecoveryReview(ctx, recovery, candidate.run, "review_scope_drift", now)
		return true, errors.Join(err, failErr)
	}
	admission, found, err := arbiterStore.GetDCPReviewLabAdmissionByID(ctx, recovery.AdmissionID)
	if err != nil || !found {
		return true, errors.New("card-12 fresh worker: original admission disappeared")
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
	rebound, err := store.RebindDCPAdmissionAfterCard12FreshWorkerRecovery(ctx, admission, recovery, candidate.run, strings.ToLower(observation.PR.BaseSHA), checkID, now)
	if err != nil || !rebound {
		return true, errors.Join(err, errors.New("card-12 fresh worker: reviewed admission rebind was rejected"))
	}
	return true, nil
}
