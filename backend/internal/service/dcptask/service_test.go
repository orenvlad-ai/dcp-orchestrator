package dcptask

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

type countingRepositoryValidator struct {
	calls    int
	identity domain.DCPRepositoryIdentity
	err      error
}

type policyRuntimeFixture struct {
	provisionCalls int
	launchCalls    int
	failProvision  bool
}

func (f *policyRuntimeFixture) ProvisionDCPReviewLabPolicySession(context.Context, domain.SessionID, ports.SpawnConfig) (domain.SessionRecord, int, int, error) {
	f.provisionCalls++
	if f.failProvision {
		f.failProvision = false
		return domain.SessionRecord{}, 0, 0, errors.New("fixture provision interruption")
	}
	return domain.SessionRecord{}, 1, 1, nil
}

func (f *policyRuntimeFixture) LaunchDCPReviewLabPolicyAction(_ context.Context, id domain.SessionID, _ string) (sessionmanager.RestoreResult, error) {
	f.launchCalls++
	return sessionmanager.RestoreResult{Session: domain.SessionRecord{ID: id, Metadata: domain.SessionMetadata{RuntimeLaunchID: fmt.Sprintf("fixture-launch-%d", f.launchCalls)}}}, nil
}

func (f *policyRuntimeFixture) DCPReviewLabPolicyActionAlive(context.Context, domain.SessionID, string) (bool, error) {
	return true, nil
}

func (v *countingRepositoryValidator) Validate(context.Context, domain.ProjectRecord) (domain.DCPRepositoryIdentity, error) {
	v.calls++
	return v.identity, v.err
}

func validSubmitInput() SubmitInput {
	return SubmitInput{
		IdempotencyKey: "i11-synthetic-1",
		Target:         TargetRepository,
		ApprovedTask: domain.DCPApprovedTask{
			SchemaVersion: ApprovedTaskSchema,
			Title:         "Synthetic durable task",
			Description:   "Persist only; do not execute",
		},
		ApprovedScope: domain.DCPApprovedScope{
			SchemaVersion: ApprovedScopeSchema,
			Statement:     "Model-free I11 storage proof",
		},
	}
}

func TestValidatePolicySubmitFailsClosedOutsideExactIdentity(t *testing.T) {
	valid := PolicySubmitInput{
		TaskID: "future-1", Target: PolicyTarget, Profile: PolicyProfile,
		Repository: PolicyRepositoryName, Prompt: "add one bounded synthetic fixture",
	}
	if _, err := validatePolicySubmit(valid); err != nil {
		t.Fatalf("valid policy input: %v", err)
	}
	repoOnly := PolicySubmitInput{TaskID: "brief-1", Target: RepoOnlyTarget, Profile: RepoOnlyProfile, Repository: RepoOnlyRepositoryName, Prompt: "refine the high-level architecture brief"}
	if spec, err := validatePolicySubmit(repoOnly); err != nil || spec.PolicyVersion != domain.DCPRepoOnlyPolicyVersion || spec.RequiredCheck != "baseline" {
		t.Fatalf("valid repo-only input: spec=%+v err=%v", spec, err)
	}
	wbc := PolicySubmitInput{TaskID: "wbc-canary-1", Target: WBCTarget, Profile: RepoOnlyProfile, Repository: WBCRepositoryName, Prompt: "add one bounded repo-only canary"}
	if spec, err := validatePolicySubmit(wbc); err != nil || spec.PolicyVersion != domain.DCPWBCRepoOnlyPolicyVersion ||
		spec.ProviderRepositoryID != 1201929580 || spec.ProviderOwnerID != 237411244 || !spec.UsesWBCReleaseTrain() || spec.CompatibilityMarker != "wb-core.dcp-release-handoff/v1" {
		t.Fatalf("valid WBC repo-only input: spec=%+v err=%v", spec, err)
	}
	legacy := repoOnly
	legacy.Target, legacy.Repository = "wb-price-extension", "orenvlad-ai/wb-price-extension"
	if _, err := validatePolicySubmit(legacy); err == nil {
		t.Fatal("legacy repo-only target accepted a future submit")
	}
	crossed := repoOnly
	crossed.Profile, crossed.Repository = PolicyProfile, PolicyRepositoryName
	if _, err := validatePolicySubmit(crossed); err == nil {
		t.Fatal("cross-target profile/repository identity was accepted")
	}
	for name, mutate := range map[string]func(*PolicySubmitInput){
		"foreign target":     func(in *PolicySubmitInput) { in.Target = "dcp-lab" },
		"foreign profile":    func(in *PolicySubmitInput) { in.Profile = "other" },
		"foreign repository": func(in *PolicySubmitInput) { in.Repository = "orenvlad-ai/other" },
		"ambiguous id":       func(in *PolicySubmitInput) { in.TaskID = "Future 1" },
		"multiline prompt":   func(in *PolicySubmitInput) { in.Prompt = "line one\nline two" },
	} {
		t.Run(name, func(t *testing.T) {
			input := valid
			mutate(&input)
			if _, err := validatePolicySubmit(input); err == nil {
				t.Fatalf("accepted out-of-policy input: %+v", input)
			}
		})
	}
}

func TestValidatePolicySubmitPreservesExactRealTargetPromptByteLimit(t *testing.T) {
	const prompt = "Write only docs/ARCHITECTURE.md for Chrome MV3 MVP: authenticated owner changes own WB product price on-page. Cover scope/non-goals; components; user-confirmed Draft->Validate->Confirm->Queue->Apply; permissions/security; typed WB API adapter boundary, no invented endpoints; no token storage/embedding; offline/server-unavailable; failure/rollback; tests; acceptance. No code, deps, credentials, live calls, deploy, or server work. Run baseline; open one ready PR to main; stop for DCP review/admission/merge."
	if len([]byte(prompt)) != 510 {
		t.Fatalf("exact product prompt bytes = %d, want 510", len([]byte(prompt)))
	}
	exact := PolicySubmitInput{
		TaskID: "price-arch-v1", Target: RepoOnlyTarget, Profile: RepoOnlyProfile,
		Repository: RepoOnlyRepositoryName, Prompt: prompt,
	}
	if _, err := validatePolicySubmit(exact); err != nil {
		t.Fatalf("exact 510-byte product prompt was rejected: %v", err)
	}
	exact.Prompt = strings.Repeat("x", 513)
	if _, err := validatePolicySubmit(exact); err == nil {
		t.Fatal("513-byte prompt crossed the immutable policy limit")
	}
}

func TestLegacyRepoOnlyTargetIsRestoreOnlyAndNeverSubmitAuthority(t *testing.T) {
	if _, ok := domain.DCPPolicyTarget("wb-price-extension", RepoOnlyProfile); ok {
		t.Fatal("legacy target remains an active policy allowlist entry")
	}
	legacy := domain.DCPReviewLabPolicyTask{
		TaskID: "price-arch-v1", PayloadDigest: "efe6a81cfff28be89cc327bdc9e2380ca585fcc6b03064c0290b6aaf4c7b59fe",
		Target: "wb-price-extension", Profile: RepoOnlyProfile, Repository: "orenvlad-ai/wb-price-extension",
		PolicyVersion: domain.DCPRepoOnlyPolicyVersion, SessionID: "wb-price-extension-1", CardNumber: 1,
		WorktreePath: "/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/wb-price-extension/wb-price-extension-1",
		SourceBranch: "ao/wb-price-extension-1/root", State: domain.DCPPolicyMerged, Revision: 7,
		PRURL: "https://github.com/orenvlad-ai/wb-price-extension/pull/1", PRNumber: 1,
		CurrentHeadSHA: "afc748eba5ff05c0dc24d3002c690ec9f44984fb",
		ReviewRunID:    "b0acfb9e-600c-4816-bb2f-02a67817ea05",
		AdmissionID:    "dcp-admission-b0acfb9e-600c-4816-bb2f-02a67817ea05",
		MergeCommitSHA: "62853496837f64522bb08ba56169f60f3b0f9a2c",
	}
	if spec, ok := domain.DCPPolicyTargetForTask(legacy); !ok || spec.Target != "wb-price-extension" {
		t.Fatalf("exact terminal legacy task cannot restore: spec=%+v ok=%v", spec, ok)
	}
	legacy.State, legacy.MergeCommitSHA = domain.DCPPolicyCIWaiting, ""
	if _, ok := domain.DCPPolicyTargetForTask(legacy); ok {
		t.Fatal("nonterminal legacy task crossed the restore-only gate")
	}
}

func TestPolicyPromptUsesTheExactAllowlistedProfile(t *testing.T) {
	repoOnly := domain.DCPReviewLabPolicyTask{TaskID: "brief-1", Profile: RepoOnlyProfile, Prompt: "refine the architecture brief"}
	if got, want := policyPrompt(repoOnly), "DCP repo-only task brief-1: refine the architecture brief"; got != want {
		t.Fatalf("repo-only prompt = %q, want %q", got, want)
	}
	initial, err := (&Service{}).workerActionPrompt(context.Background(), repoOnly, domain.DCPModelAction{Kind: domain.DCPActionInitialWorker})
	if err != nil || initial != policyPrompt(repoOnly) {
		t.Fatalf("repo-only initial action prompt = %q, err=%v", initial, err)
	}

	synthetic := domain.DCPReviewLabPolicyTask{TaskID: "fixture-1", Profile: PolicyProfile, Prompt: "add one fixture"}
	if got, want := policyPrompt(synthetic), "DCP synthetic task fixture-1: add one fixture"; got != want {
		t.Fatalf("synthetic prompt = %q, want %q", got, want)
	}
}

func TestValidateExactPolicyPRWaitsForEnrichmentAndRejectsContradiction(t *testing.T) {
	task := domain.DCPReviewLabPolicyTask{Target: PolicyTarget, Profile: PolicyProfile, Repository: PolicyRepositoryName,
		PolicyVersion: domain.DCPReviewLabPolicyVersion, SourceBranch: "ao/dcp-review-lab-20/root"}
	url := "https://github.com/orenvlad-ai/dcp-review-lab/pull/17"
	head := "6211c80a4b9e8b6ab30a38a64c4bca3ec38ef621"
	partial := domain.PullRequest{URL: url, Number: 17, HeadSHA: head}
	if err := validateExactPolicyPR(task, partial, url, head); !errors.Is(err, errPolicyPRFactsPending) {
		t.Fatalf("partial provider snapshot = %v, want pending", err)
	}

	exact := domain.PullRequest{
		URL: url, HTMLURL: url, Number: 17, Provider: "github", Host: "github.com",
		Repo: PolicyRepositoryName, SourceBranch: task.SourceBranch, TargetBranch: "main",
		Author: "orenvlad-ai", ProviderState: "OPEN", HeadSHA: head,
	}
	if err := validateExactPolicyPR(task, exact, url, head); err != nil {
		t.Fatalf("exact enriched provider snapshot: %v", err)
	}

	contradictory := exact
	contradictory.SourceBranch = "ao/foreign/root"
	if err := validateExactPolicyPR(task, contradictory, url, head); err == nil || errors.Is(err, errPolicyPRFactsPending) {
		t.Fatalf("contradictory provider snapshot = %v, want terminal drift", err)
	}

	closed := exact
	closed.Closed, closed.ProviderState = true, "CLOSED"
	if err := validateExactPolicyPR(task, closed, url, head); err == nil || errors.Is(err, errPolicyPRFactsPending) {
		t.Fatalf("closed provider snapshot = %v, want terminal drift", err)
	}
}

func TestValidateExactPolicyNamedCIIgnoresHistoricalHeadChecks(t *testing.T) {
	head := "931a69637be0b14d9ca145909d0f6060ad81c2fc"
	oldHead := "8b3f601ae7b82b68bfd3f3810069c7a91774ca72"
	pr := domain.PullRequest{CI: domain.CIPassing, Repo: PolicyRepositoryName}
	checks := []domain.PullRequestCheck{
		{Name: "dcp-review-lab", CommitHash: oldHead, Status: domain.PRCheckPassed, Conclusion: "success", URL: "https://github.com/orenvlad-ai/dcp-review-lab/actions/runs/31847795164/job/94917645974"},
		{Name: "dcp-review-lab", CommitHash: head, Status: domain.PRCheckPassed, Conclusion: "success", URL: "https://github.com/orenvlad-ai/dcp-review-lab/actions/runs/31854288545/job/94935989369"},
	}
	ready, terminal, err := validateExactPolicyNamedCI(pr, checks, head)
	if err != nil || !ready || terminal {
		t.Fatalf("historical/current check snapshot = ready %v terminal %v err %v", ready, terminal, err)
	}

	ready, terminal, err = validateExactPolicyNamedCI(pr, checks[:1], head)
	if err != nil || ready || terminal {
		t.Fatalf("historical-only snapshot = ready %v terminal %v err %v, want model-free wait", ready, terminal, err)
	}

	checks[1].Status, checks[1].Conclusion = domain.PRCheckFailed, "failure"
	ready, terminal, err = validateExactPolicyNamedCI(pr, checks, head)
	if err == nil || ready || !terminal {
		t.Fatalf("failed current-head check = ready %v terminal %v err %v", ready, terminal, err)
	}
}

func TestValidateExactPolicyNamedCIReproducesAndCorrectsWBCCanaryIncident(t *testing.T) {
	const head = "e8cca45f3995b8181fe81ead154f7a933dbacbe8"
	pr := domain.PullRequest{CI: domain.CIPassing, Repo: WBCRepositoryName}
	baseline := domain.PullRequestCheck{
		Name: "baseline", CommitHash: head, Status: domain.PRCheckPassed, Conclusion: "success",
		URL: "https://github.com/orenvlad-ai/wb-core/actions/runs/32048996893/job/95443534690",
	}
	currentTrainChecks := []domain.PullRequestCheck{
		baseline,
		{Name: "Select queued PR", CommitHash: head, Status: domain.PRCheckSkipped, Conclusion: "skipped", URL: "https://github.com/orenvlad-ai/wb-core/actions/runs/32049021805/job/95443626637"},
		{Name: "Merge repo-only PR", CommitHash: head, Status: domain.PRCheckSkipped, Conclusion: "skipped", URL: "https://github.com/orenvlad-ai/wb-core/actions/runs/32049021805/job/95443627412"},
	}
	// This is the exact predecessor defect: the old loop rejected the snapshot
	// before it selected spec.RequiredCheck.
	for _, check := range currentTrainChecks {
		if check.CommitHash == head && check.Status != domain.PRCheckPassed {
			goto reproduced
		}
	}
	t.Fatal("fixture no longer reproduces the old all-visible-check rejection")

reproduced:
	for name, checks := range map[string][]domain.PullRequestCheck{
		"current complex Release Train":   currentTrainChecks,
		"future simplified Release Train": {baseline},
	} {
		t.Run(name, func(t *testing.T) {
			ready, terminal, err := validateExactPolicyNamedCI(pr, checks, head)
			if err != nil || !ready || terminal {
				t.Fatalf("ready=%v terminal=%v err=%v", ready, terminal, err)
			}
		})
	}
}

func TestValidateExactPolicyNamedCIFailsClosedForRequiredBaselineOnly(t *testing.T) {
	const head = "e8cca45f3995b8181fe81ead154f7a933dbacbe8"
	pr := domain.PullRequest{CI: domain.CIPassing, Repo: WBCRepositoryName}
	valid := domain.PullRequestCheck{Name: "baseline", CommitHash: head, Status: domain.PRCheckPassed, Conclusion: "success", URL: "https://github.com/orenvlad-ai/wb-core/actions/runs/1/job/2"}
	for _, tc := range []struct {
		name     string
		checks   []domain.PullRequestCheck
		terminal bool
	}{
		{name: "missing", terminal: false},
		{name: "wrong head", checks: []domain.PullRequestCheck{{Name: "baseline", CommitHash: strings.Repeat("0", 40), Status: domain.PRCheckPassed, Conclusion: "success"}}, terminal: false},
		{name: "pending", checks: []domain.PullRequestCheck{{Name: "baseline", CommitHash: head, Status: domain.PRCheckInProgress}}, terminal: false},
		{name: "duplicate", checks: []domain.PullRequestCheck{valid, valid}, terminal: true},
		{name: "failed", checks: []domain.PullRequestCheck{{Name: "baseline", CommitHash: head, Status: domain.PRCheckFailed, Conclusion: "failure"}}, terminal: true},
		{name: "skipped", checks: []domain.PullRequestCheck{{Name: "baseline", CommitHash: head, Status: domain.PRCheckSkipped, Conclusion: "skipped"}}, terminal: true},
		{name: "malformed", checks: []domain.PullRequestCheck{{Name: "baseline", CommitHash: head, Status: domain.PRCheckPassed, Conclusion: "neutral"}}, terminal: true},
		{name: "foreign provider URL", checks: []domain.PullRequestCheck{{Name: "baseline", CommitHash: head, Status: domain.PRCheckPassed, Conclusion: "success", URL: "https://example.com/job/2"}}, terminal: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ready, terminal, err := validateExactPolicyNamedCI(pr, tc.checks, head)
			if ready || terminal != tc.terminal || (terminal && err == nil) || (!terminal && err != nil) {
				t.Fatalf("ready=%v terminal=%v err=%v", ready, terminal, err)
			}
		})
	}
}

func TestExactPolicyNativeIdentityAcceptsOnlyTerminalArchivedShell(t *testing.T) {
	task := domain.DCPReviewLabPolicyTask{
		TaskID: "archived-1", Target: PolicyTarget, Profile: PolicyProfile,
		Repository: PolicyRepositoryName, PolicyVersion: domain.DCPReviewLabPolicyVersion,
		SessionID: "dcp-review-lab-21", CardNumber: 21,
		WorktreePath: "/lab/data/worktrees/dcp-review-lab/dcp-review-lab-21",
		SourceBranch: "ao/dcp-review-lab-21/root", Prompt: "add one fixture",
		State: domain.DCPPolicyMerged,
	}
	session := domain.SessionRecord{
		ID: task.SessionID, ProjectID: PolicyTarget, Kind: domain.KindWorker,
		Harness: domain.HarnessCodex, DisplayName: "DCP:" + task.TaskID,
		IsTerminated: true, Activity: domain.Activity{State: domain.ActivityExited},
		Metadata: domain.SessionMetadata{
			Branch: task.SourceBranch, WorkspacePath: task.WorktreePath,
			Prompt: "DCP synthetic task " + task.TaskID + ": " + task.Prompt,
		},
	}
	if !exactPolicyNativeIdentity(task, session) {
		t.Fatal("exact terminal archived shell was rejected")
	}

	nonterminal := task
	nonterminal.State = domain.DCPPolicyReviewQueued
	if exactPolicyNativeIdentity(nonterminal, session) {
		t.Fatal("terminated nonterminal shell was accepted")
	}

	notExited := session
	notExited.Activity.State = domain.ActivityIdle
	if exactPolicyNativeIdentity(task, notExited) {
		t.Fatal("terminated terminal shell without exited activity was accepted")
	}

	drifted := session
	drifted.Metadata.Branch = "ao/foreign/root"
	if exactPolicyNativeIdentity(task, drifted) {
		t.Fatal("archived shell with metadata drift was accepted")
	}
}

func TestSubmitPolicyReplayCompletesOnlyTheReservedNativeIdentity(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	project := domain.ProjectRecord{ID: PolicyTarget, Path: t.TempDir(), Kind: domain.ProjectKindSingleRepo, RepoOriginURL: reviewLabOrigin, RegisteredAt: time.Unix(1, 0).UTC()}
	if err := store.UpsertProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 12; i++ {
		rec := domain.SessionRecord{ProjectID: PolicyTarget, Kind: domain.KindWorker, Harness: domain.HarnessCodex, DisplayName: fmt.Sprintf("history-%d", i), Activity: domain.Activity{State: domain.ActivityIdle}, CreatedAt: time.Unix(int64(i), 0).UTC(), UpdatedAt: time.Unix(int64(i), 0).UTC()}
		if created, createErr := store.CreateSession(ctx, rec); createErr != nil || created.ID != domain.SessionID(fmt.Sprintf("dcp-review-lab-%d", i)) {
			t.Fatalf("seed card %d = %s, %v", i, created.ID, createErr)
		}
	}
	repository := &countingRepositoryValidator{identity: domain.DCPRepositoryIdentity{ProjectID: PolicyTarget, Repository: PolicyRepositoryName}}
	runtime := &policyRuntimeFixture{failProvision: true}
	svc := New(Deps{Store: store, PolicyRepository: repository, PolicyWorktreeRoot: filepath.Join(t.TempDir(), "worktrees"), Now: func() time.Time { return time.Unix(100, 0).UTC() }})
	svc.SetPolicyRuntime(runtime, nil)
	input := PolicySubmitInput{TaskID: "future-replay", Target: PolicyTarget, Profile: PolicyProfile, Repository: PolicyRepositoryName, Prompt: "add one bounded fixture"}
	if _, err := svc.SubmitPolicy(ctx, input); err == nil {
		t.Fatal("interrupted provision unexpectedly succeeded")
	}
	reserved, found, err := store.GetDCPReviewLabPolicyTaskByTaskID(ctx, input.TaskID)
	if err != nil || !found || reserved.State != domain.DCPPolicyReserved || reserved.CardNumber != 13 {
		t.Fatalf("durable interrupted reservation = %+v, %v, %v", reserved, found, err)
	}
	replayed, err := svc.SubmitPolicy(ctx, input)
	if err != nil || !replayed.Duplicate || replayed.Task.SessionID != reserved.SessionID || replayed.Task.CardNumber != reserved.CardNumber || runtime.launchCalls != 1 {
		t.Fatalf("reserved replay = %+v, launches=%d, err=%v", replayed, runtime.launchCalls, err)
	}
	again, err := svc.SubmitPolicy(ctx, input)
	if err != nil || !again.Duplicate || again.Task.SessionID != reserved.SessionID || runtime.launchCalls != 1 {
		t.Fatalf("stable replay = %+v, launches=%d, err=%v", again, runtime.launchCalls, err)
	}
	conflict := input
	conflict.Prompt = "different payload"
	if _, err := svc.SubmitPolicy(ctx, conflict); err == nil {
		t.Fatal("conflicting replay was accepted")
	}
	sessions, err := store.ListSessions(ctx, PolicyTarget)
	if err != nil || len(sessions) != 13 {
		t.Fatalf("replay allocated replacement card: sessions=%d err=%v", len(sessions), err)
	}
}

func TestSubmitPolicyRejectsLockedWBCTargetBeforeDurableOrModelMutation(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	spec, _ := domain.DCPPolicyTarget(WBCTarget, RepoOnlyProfile)
	project := domain.ProjectRecord{
		ID: spec.Target, Path: t.TempDir(), Kind: domain.ProjectKindSingleRepo,
		RepoOriginURL: spec.OriginURL, RegisteredAt: time.Unix(1, 0).UTC(),
		Config: domain.ProjectConfig{DefaultBranch: spec.DefaultBranch, SessionPrefix: spec.SessionPrefix, AgentRules: spec.AgentRules},
	}
	if err := store.UpsertProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	repository := &countingRepositoryValidator{err: errors.New("wb-core Release Train compatibility marker is unavailable")}
	runtime := &policyRuntimeFixture{}
	svc := New(Deps{Store: store, PolicyRepository: repository, PolicyWorktreeRoot: filepath.Join(t.TempDir(), "worktrees")})
	svc.SetPolicyRuntime(runtime, nil)
	input := PolicySubmitInput{TaskID: "wbc-canary-1", Target: WBCTarget, Profile: RepoOnlyProfile, Repository: WBCRepositoryName, Prompt: "add one bounded repo-only canary"}
	if _, err := svc.SubmitPolicy(ctx, input); err == nil {
		t.Fatal("locked WBC target accepted a substantive submit")
	}
	if repository.calls != 1 || runtime.provisionCalls != 0 || runtime.launchCalls != 0 {
		t.Fatalf("locked WBC submit crossed mutation boundary: validator=%d provision=%d launch=%d", repository.calls, runtime.provisionCalls, runtime.launchCalls)
	}
	if tasks, listErr := store.ListDCPReviewLabPolicyTasks(ctx); listErr != nil || len(tasks) != 0 {
		t.Fatalf("locked WBC submit created policy rows: tasks=%+v err=%v", tasks, listErr)
	}
	if sessions, listErr := store.ListSessions(ctx, domain.ProjectID(spec.Target)); listErr != nil || len(sessions) != 0 {
		t.Fatalf("locked WBC submit created native sessions: sessions=%+v err=%v", sessions, listErr)
	}
	if actions, listErr := store.ListDCPModelActions(ctx); listErr != nil || len(actions) != 0 {
		t.Fatalf("locked WBC submit created model actions: actions=%+v err=%v", actions, listErr)
	}
}

func requireAPIError(t *testing.T, err error, kind apierr.Kind, code string) {
	t.Helper()
	var got *apierr.Error
	if !errors.As(err, &got) {
		t.Fatalf("err = %v, want *apierr.Error", err)
	}
	if got.Kind != kind || got.Code != code {
		t.Fatalf("api error = %+v, want kind=%v code=%s", got, kind, code)
	}
}

func TestSubmitRejectsMalformedOrOutOfScopeBeforeMutation(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.UpsertProject(ctx, testProject(t.TempDir())); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	validator := &countingRepositoryValidator{}
	svc := New(Deps{Store: store, Repository: validator})

	outOfScope := validSubmitInput()
	outOfScope.Target = "real-repository"
	_, err = svc.Submit(ctx, outOfScope)
	requireAPIError(t, err, apierr.KindInvalid, "DCP_TARGET_INVALID")
	if validator.calls != 0 {
		t.Fatalf("repository validator calls = %d, want 0", validator.calls)
	}

	malformed := validSubmitInput()
	malformed.ApprovedTask.Title = "   "
	_, err = svc.Submit(ctx, malformed)
	requireAPIError(t, err, apierr.KindInvalid, "DCP_TASK_INVALID")
	if validator.calls != 0 {
		t.Fatalf("repository validator calls after malformed input = %d, want 0", validator.calls)
	}
	tasks, err := store.ListDCPTasks(ctx, "")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("invalid requests mutated storage: %+v", tasks)
	}
}

func TestModelFreeSubmitIdempotencyAndRestartPersistence(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repo := createDCPTestRepository(t)
	dataDir := filepath.Join(root, "data")
	worktreeRoot := filepath.Join(dataDir, "worktrees")
	store, err := sqlite.Open(dataDir)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	now := time.Unix(500, 0).UTC()
	if err := store.UpsertProject(ctx, testProject(repo)); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	session, err := store.CreateSession(ctx, domain.SessionRecord{
		ProjectID: domain.ProjectID(TargetProjectID),
		Kind:      domain.KindWorker,
		Harness:   domain.HarnessCodex,
		Activity: domain.Activity{
			State:          domain.ActivityIdle,
			LastActivityAt: now,
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("seed existing session: %v", err)
	}

	nextID := 0
	svc := New(Deps{
		Store: store,
		Repository: GitRepositoryValidator{
			TargetPath:          repo,
			AllowedWorktreeRoot: worktreeRoot,
		},
		Now: func() time.Time { return now },
		NewID: func(prefix string) string {
			nextID++
			return fmt.Sprintf("%s%02d", prefix, nextID)
		},
	})
	input := validSubmitInput()
	first, err := svc.Submit(ctx, input)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if first.Duplicate || first.Task.State != domain.DCPTaskSubmitted || first.Task.Revision != 1 {
		t.Fatalf("first submit = %+v", first)
	}
	duplicate, err := svc.Submit(ctx, input)
	if err != nil {
		t.Fatalf("duplicate submit: %v", err)
	}
	if !duplicate.Duplicate || duplicate.Task.ID != first.Task.ID {
		t.Fatalf("duplicate = %+v, want stable task %s", duplicate, first.Task.ID)
	}
	events, err := svc.Events(ctx, first.Task.ID)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(events) != 1 || events[0].EventType != "task.submitted" || events[0].Sequence != 1 {
		t.Fatalf("events = %+v", events)
	}

	conflict := input
	conflict.ApprovedScope.Statement = "different canonical scope"
	_, err = svc.Submit(ctx, conflict)
	requireAPIError(t, err, apierr.KindConflict, "DCP_IDEMPOTENCY_CONFLICT")
	events, err = svc.Events(ctx, first.Task.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("conflict mutated events: len=%d err=%v", len(events), err)
	}

	// Closing and reopening the only database models a daemon/app restart. The
	// service has no executor/model dependency and startup validation is read-only.
	if err := store.Close(); err != nil {
		t.Fatalf("close before restart: %v", err)
	}
	restarted, err := sqlite.Open(dataDir)
	if err != nil {
		t.Fatalf("reopen after restart: %v", err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	restartedSvc := New(Deps{
		Store: restarted,
		Repository: GitRepositoryValidator{
			TargetPath:          repo,
			AllowedWorktreeRoot: worktreeRoot,
		},
	})
	if err := restartedSvc.ValidateSchema(ctx); err != nil {
		t.Fatalf("startup schema validation: %v", err)
	}
	persisted, err := restartedSvc.Get(ctx, first.Task.ID)
	if err != nil {
		t.Fatalf("get after restart: %v", err)
	}
	persistedEvents, err := restartedSvc.Events(ctx, first.Task.ID)
	if err != nil {
		t.Fatalf("events after restart: %v", err)
	}
	preservedSession, ok, err := restarted.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("existing session after restart: %v", err)
	}
	if persisted.State != domain.DCPTaskSubmitted || persisted.Revision != 1 || len(persistedEvents) != 1 {
		t.Fatalf("task changed during restart: task=%+v events=%+v", persisted, persistedEvents)
	}
	if !ok || preservedSession.ID != session.ID || preservedSession.Harness != domain.HarnessCodex {
		t.Fatalf("existing session not preserved: %+v ok=%v", preservedSession, ok)
	}
}
