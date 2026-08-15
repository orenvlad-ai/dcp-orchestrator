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
