package dcpterminalmerge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	testHead  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testBase  = "cccccccccccccccccccccccccccccccccccccccc"
	testMerge = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type fakeStore struct {
	session domain.SessionRecord
	project domain.ProjectRecord
	pr      domain.PullRequest
	run     domain.ReviewRun
	claims  int
}

func (f *fakeStore) GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error) {
	return f.session, true, nil
}
func (f *fakeStore) ListAllSessions(context.Context) ([]domain.SessionRecord, error) {
	return []domain.SessionRecord{f.session}, nil
}
func (f *fakeStore) GetProject(context.Context, string) (domain.ProjectRecord, bool, error) {
	return f.project, true, nil
}
func (f *fakeStore) ListPRsBySession(context.Context, domain.SessionID) ([]domain.PullRequest, error) {
	return []domain.PullRequest{f.pr}, nil
}
func (f *fakeStore) ListReviewRunsBySession(context.Context, domain.SessionID) ([]domain.ReviewRun, error) {
	return []domain.ReviewRun{f.run}, nil
}
func (f *fakeStore) ClaimDCPReviewLabTerminalMerge(_ context.Context, run domain.ReviewRun) (bool, error) {
	if f.run.TerminalMergeStatus != "" || run.ID != f.run.ID {
		return false, nil
	}
	f.claims++
	f.run.TerminalMergeStatus = "running"
	return true, nil
}
func (f *fakeStore) CompleteDCPReviewLabTerminalMerge(_ context.Context, runID, sha string) (bool, error) {
	if runID != f.run.ID || f.run.TerminalMergeStatus != "running" {
		return false, nil
	}
	f.run.TerminalMergeStatus = "succeeded"
	f.run.TerminalMergeCommitSHA = sha
	return true, nil
}
func (f *fakeStore) FailDCPReviewLabTerminalMerge(_ context.Context, runID, code string) (bool, error) {
	if runID != f.run.ID || f.run.TerminalMergeStatus != "running" {
		return false, nil
	}
	f.run.TerminalMergeStatus = "failed"
	f.run.TerminalMergeError = code
	return true, nil
}

type fakeSCM struct {
	observation ports.SCMObservation
	review      ports.SCMReviewObservation
	mergeErr    error
	mergeCalls  int
}

func (f *fakeSCM) FetchPullRequests(context.Context, []ports.SCMPRRef) ([]ports.SCMObservation, error) {
	return []ports.SCMObservation{f.observation}, nil
}
func (f *fakeSCM) FetchReviewThreads(context.Context, ports.SCMPRRef) (ports.SCMReviewObservation, error) {
	return f.review, nil
}
func (f *fakeSCM) MergePullRequest(_ context.Context, request ports.SCMMergeRequest) (ports.SCMMergeResult, error) {
	f.mergeCalls++
	if request.ExpectedHeadSHA != testHead || request.Method != ports.SCMMergeSquash || request.PR.Repo.Repo != RepositoryFullName {
		return ports.SCMMergeResult{}, errors.New("unexpected merge request")
	}
	if f.mergeErr != nil {
		return ports.SCMMergeResult{}, f.mergeErr
	}
	return ports.SCMMergeResult{MergeCommitSHA: testMerge}, nil
}

func fixture(t *testing.T) (*Engine, *fakeStore, *fakeSCM) {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	id := domain.SessionID(SessionPrefix + "-6")
	workspace := filepath.Join(dataDir, "worktrees", ProjectID, string(id))
	projectPath := filepath.Join(root, "targets", ProjectID)
	privateGitDir := filepath.Join(projectPath, ".git", "worktrees", string(id))
	for _, path := range []string{workspace, privateGitDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	branch := "ao/" + string(id) + "/root"
	prURL := "https://github.com/orenvlad-ai/dcp-review-lab/pull/6"
	taskID := "i7-terminal-merge"
	store := &fakeStore{
		session: domain.SessionRecord{
			ID: id, ProjectID: ProjectID, Kind: domain.KindWorker, Harness: domain.HarnessCodex,
			DisplayName: TaskDisplayPrefix + taskID,
			Activity:    domain.Activity{State: domain.ActivityIdle},
			Metadata: domain.SessionMetadata{
				WorkspacePath: workspace, Branch: branch, DiffBaseSHA: testBase, DiffBaseRef: "origin/main",
				Prompt: TaskPromptPrefix + taskID + ": Add the exact synthetic canary workflow.",
			},
		},
		project: domain.ProjectRecord{
			ID: ProjectID, Path: projectPath, RepoOriginURL: RepositoryURL, Kind: domain.ProjectKindSingleRepo,
			Config: domain.ProjectConfig{
				DefaultBranch: TargetBranch, SessionPrefix: SessionPrefix, AgentRules: ProfileAgentRules,
				Worker:    domain.RoleOverride{Harness: domain.HarnessCodex, AgentConfig: domain.AgentConfig{Permissions: domain.PermissionModeAcceptEdits}},
				Reviewers: []domain.ReviewerConfig{{Harness: domain.ReviewerCodex}},
			},
		},
		pr: domain.PullRequest{
			URL: prURL, SessionID: id, Number: 6, Provider: "github", Host: "github.com", Repo: RepositoryFullName,
			SourceBranch: branch, TargetBranch: TargetBranch, HeadSHA: testHead, BaseSHA: testBase,
			Author: "orenvlad-ai", ProviderState: "OPEN", HTMLURL: prURL,
		},
		run: domain.ReviewRun{
			ID: "run-6", ReviewID: "review-record-6", BatchID: "batch-6", SessionID: id, Harness: domain.ReviewerCodex,
			PRURL: prURL, TargetSHA: testHead, Body: "No blocking findings.",
			Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved, ResultChannel: structuredChannel,
		},
	}
	scm := &fakeSCM{observation: ports.SCMObservation{
		Fetched: true, Provider: "github", Host: "github.com", Repo: RepositoryFullName,
		PR: ports.SCMPRObservation{
			URL: prURL, Number: 6, HeadRepo: RepositoryFullName, SourceBranch: branch, TargetBranch: TargetBranch,
			HeadSHA: testHead, BaseSHA: testBase, State: string(domain.PRStateOpen), ProviderState: "OPEN",
			Author: "orenvlad-ai", HTMLURL: prURL, ProviderMergeable: "MERGEABLE", ProviderMergeStateStatus: "CLEAN",
		},
		CI: ports.SCMCIObservation{Summary: string(domain.CIPassing), HeadSHA: testHead, Checks: []ports.SCMCheckObservation{{
			Name: RequiredCheckName, Status: string(domain.PRCheckPassed), Conclusion: "success",
		}}},
		Mergeability: ports.SCMMergeabilityObservation{State: string(domain.MergeMergeable), Mergeable: true},
	}}
	engine := New(store, scm, dataDir)
	engine.git = func(_ context.Context, path string, args ...string) (string, error) {
		cmd := strings.Join(args, " ")
		if cmd == "status --porcelain" {
			return "", nil
		}
		if cmd == "remote" {
			return "origin", nil
		}
		if cmd == "remote get-url origin" {
			return RepositoryURL, nil
		}
		if path == store.project.Path {
			switch cmd {
			case "rev-parse --show-toplevel":
				return path, nil
			case "branch --show-current":
				return TargetBranch, nil
			case "rev-parse origin/main", "rev-parse HEAD":
				return testBase, nil
			}
		}
		if path == workspace {
			switch cmd {
			case "rev-parse --show-toplevel":
				return path, nil
			case "branch --show-current":
				return branch, nil
			case "rev-parse HEAD":
				return testHead, nil
			case "rev-parse --path-format=absolute --git-common-dir":
				return filepath.Join(store.project.Path, ".git"), nil
			case "rev-parse --path-format=absolute --absolute-git-dir":
				return privateGitDir, nil
			}
		}
		return "", errors.New("unexpected git command")
	}
	return engine, store, scm
}

func TestTryMergesExactCleanApprovedHeadOnce(t *testing.T) {
	engine, store, scm := fixture(t)
	candidate, ok, err := engine.candidate(context.Background(), store.session.ID)
	if err != nil || !ok {
		t.Fatalf("candidate ok=%v err=%v", ok, err)
	}
	observation, review, err := engine.fresh(context.Background(), candidate.pr)
	if err != nil || !ready(candidate, observation, review) {
		t.Fatalf("ready=false err=%v observation=%+v review=%+v", err, observation, review)
	}
	if err := engine.validateGit(context.Background(), candidate, observation.PR.HeadSHA); err != nil {
		t.Fatal(err)
	}
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if store.claims != 1 || scm.mergeCalls != 1 || store.run.TerminalMergeStatus != "succeeded" || store.run.TerminalMergeCommitSHA != testMerge {
		t.Fatalf("claims=%d merges=%d run=%+v", store.claims, scm.mergeCalls, store.run)
	}
}

func TestTryRejectsOldSessionAndNonCleanProviderFacts(t *testing.T) {
	engine, store, scm := fixture(t)
	oldID := domain.SessionID("dcp-review-lab-5")
	store.session.ID = oldID
	if err := engine.Try(context.Background(), oldID); err != nil {
		t.Fatal(err)
	}
	if scm.mergeCalls != 0 {
		t.Fatal("old session reached merge")
	}
	engine, store, scm = fixture(t)
	scm.observation.PR.ProviderMergeStateStatus = "BLOCKED"
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if store.claims != 0 || scm.mergeCalls != 0 {
		t.Fatal("non-clean PR reached merge")
	}
}

func TestTryRequiresEveryVisibleCheckToPass(t *testing.T) {
	engine, store, scm := fixture(t)
	scm.observation.CI = ports.SCMCIObservation{
		Summary: string(domain.CIPending), HeadSHA: testHead,
		Checks: []ports.SCMCheckObservation{{Name: RequiredCheckName, Status: string(domain.PRCheckInProgress)}},
	}
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if store.claims != 0 || scm.mergeCalls != 0 {
		t.Fatal("pending check reached merge")
	}
}

func TestTryRequiresOneSuccessfulNamedCheck(t *testing.T) {
	for _, mutate := range []func(*fakeSCM){
		func(scm *fakeSCM) { scm.observation.CI.Checks = nil },
		func(scm *fakeSCM) { scm.observation.CI.Checks[0].Name = "foreign" },
		func(scm *fakeSCM) { scm.observation.CI.Checks[0].Status = string(domain.PRCheckSkipped) },
	} {
		engine, store, scm := fixture(t)
		mutate(scm)
		if err := engine.Try(context.Background(), store.session.ID); err != nil {
			t.Fatal(err)
		}
		if store.claims != 0 || scm.mergeCalls != 0 {
			t.Fatal("missing, foreign, or skipped required check reached merge")
		}
	}
}

func TestTryRejectsForeignTaskBaseAndProfile(t *testing.T) {
	for _, mutate := range []func(*fakeStore, *fakeSCM){
		func(store *fakeStore, _ *fakeSCM) { store.session.DisplayName = TaskDisplayPrefix + "FOREIGN" },
		func(store *fakeStore, _ *fakeSCM) { store.session.Metadata.Prompt = "unbound prompt" },
		func(store *fakeStore, _ *fakeSCM) { store.project.Config.AgentRules += " malicious override" },
		func(store *fakeStore, _ *fakeSCM) { store.project.Config.AgentRulesFile = "AGENTS.md" },
		func(store *fakeStore, _ *fakeSCM) { store.project.Config.TrackerIntake.Enabled = true },
		func(_ *fakeStore, scm *fakeSCM) { scm.observation.PR.BaseSHA = testHead },
	} {
		engine, store, scm := fixture(t)
		mutate(store, scm)
		if err := engine.Try(context.Background(), store.session.ID); err != nil {
			t.Fatal(err)
		}
		if store.claims != 0 || scm.mergeCalls != 0 {
			t.Fatal("foreign task, base, or profile reached merge")
		}
	}
}

func TestTryRejectsAnyUnresolvedReviewThread(t *testing.T) {
	engine, store, scm := fixture(t)
	scm.review.Threads = []ports.SCMReviewThreadObservation{{ID: "bot-thread", IsBot: true}}
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if store.claims != 0 || scm.mergeCalls != 0 {
		t.Fatal("unresolved review thread reached merge")
	}
}

func TestTryRecordsFailureWithoutRetry(t *testing.T) {
	engine, store, scm := fixture(t)
	scm.mergeErr = ports.ErrSCMNotMergeable
	if err := engine.Try(context.Background(), store.session.ID); !errors.Is(err, ports.ErrSCMNotMergeable) {
		t.Fatalf("error=%v", err)
	}
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if scm.mergeCalls != 1 || store.run.TerminalMergeStatus != "failed" || store.run.TerminalMergeError != "not_mergeable" {
		t.Fatalf("merges=%d run=%+v", scm.mergeCalls, store.run)
	}
}

func TestReconcileRunningUsesFreshMergedFactWithoutSecondMutation(t *testing.T) {
	engine, store, scm := fixture(t)
	store.run.TerminalMergeStatus = "running"
	store.pr.Merged = true
	scm.observation.PR.Merged = true
	scm.observation.PR.MergeCommitSHA = testMerge
	if err := engine.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scm.mergeCalls != 0 || store.run.TerminalMergeStatus != "succeeded" || store.run.TerminalMergeCommitSHA != testMerge {
		t.Fatalf("merges=%d run=%+v", scm.mergeCalls, store.run)
	}
}
