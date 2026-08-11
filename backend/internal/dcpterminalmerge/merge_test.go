package dcpterminalmerge

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	testHead  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testBase  = "cccccccccccccccccccccccccccccccccccccccc"
	testMerge = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type fakeStore struct {
	session           domain.SessionRecord
	project           domain.ProjectRecord
	pr                domain.PullRequest
	run               domain.ReviewRun
	claims            int
	admission         *domain.DCPReviewLabAdmission
	includeCohortPeer bool
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
func (f *fakeStore) EnqueueDCPReviewLabAdmission(_ context.Context, admission domain.DCPReviewLabAdmission) (domain.DCPReviewLabAdmission, bool, error) {
	if f.admission != nil {
		return *f.admission, false, nil
	}
	admission.Sequence = 1
	f.admission = &admission
	return admission, true, nil
}
func (f *fakeStore) GetDCPReviewLabAdmissionByRun(_ context.Context, runID string) (domain.DCPReviewLabAdmission, bool, error) {
	if f.admission == nil || f.admission.ReviewRunID != runID {
		return domain.DCPReviewLabAdmission{}, false, nil
	}
	return *f.admission, true, nil
}
func (f *fakeStore) GetClaimedDCPReviewLabAdmission(context.Context) (domain.DCPReviewLabAdmission, bool, error) {
	if f.admission == nil || f.admission.Status != domain.DCPAdmissionClaimed {
		return domain.DCPReviewLabAdmission{}, false, nil
	}
	return *f.admission, true, nil
}
func (f *fakeStore) GetNextWaitingDCPReviewLabAdmission(context.Context) (domain.DCPReviewLabAdmission, bool, error) {
	if f.admission == nil || f.admission.Status != domain.DCPAdmissionWaiting {
		return domain.DCPReviewLabAdmission{}, false, nil
	}
	return *f.admission, true, nil
}
func (f *fakeStore) ListDCPReviewLabAdmissions(context.Context) ([]domain.DCPReviewLabAdmission, error) {
	var rows []domain.DCPReviewLabAdmission
	if f.includeCohortPeer {
		rows = append(rows, domain.DCPReviewLabAdmission{
			Sequence: 99, ID: "cohort-peer", ReviewRunID: "cohort-peer-run", ReviewID: "cohort-peer-review",
			SessionID: AdmissionSessionB, PRURL: "https://github.com/orenvlad-ai/dcp-review-lab/pull/99",
			PRNumber: 99, TargetSHA: testHead, ReviewBaseSHA: testBase, AdmittedBaseSHA: testBase,
			Status: domain.DCPAdmissionSucceeded, LeaseID: "peer-lease", MergeCommitSHA: testMerge,
		})
	}
	if f.admission != nil {
		rows = append(rows, *f.admission)
	}
	return rows, nil
}
func (f *fakeStore) GetRefreshingDCPReviewLabAdmissionBySession(_ context.Context, sessionID domain.SessionID) (domain.DCPReviewLabAdmission, bool, error) {
	if f.admission == nil || f.admission.SessionID != sessionID || f.admission.Status != domain.DCPAdmissionRefreshing {
		return domain.DCPReviewLabAdmission{}, false, nil
	}
	return *f.admission, true, nil
}
func (f *fakeStore) RecoverDCPReviewLabCanonicalBaseIncident(_ context.Context, admission domain.DCPReviewLabAdmission, now time.Time) (bool, error) {
	if f.admission == nil || f.admission.ID != admission.ID || f.admission.Status != domain.DCPAdmissionIncident ||
		f.admission.ErrorCode != "canonical_main_diverged" || f.admission.RefreshWakeCount != 0 || f.admission.RecoveredIncidentPacket != "" {
		return false, nil
	}
	f.admission.Status, f.admission.LeaseID, f.admission.AdmittedBaseSHA = domain.DCPAdmissionWaiting, "", ""
	f.admission.ErrorCode, f.admission.RecoveredIncidentPacket, f.admission.IncidentPacket = "", f.admission.IncidentPacket, ""
	f.admission.UpdatedAt = now
	return true, nil
}
func (f *fakeStore) ResumeDCPReviewLabAdmissionAfterRefresh(_ context.Context, admission domain.DCPReviewLabAdmission, run domain.ReviewRun, baseSHA string, now time.Time) (bool, error) {
	if f.admission == nil || f.admission.ID != admission.ID || f.admission.Status != domain.DCPAdmissionRefreshing || f.admission.RefreshWakeCount != 1 || strings.EqualFold(f.admission.TargetSHA, run.TargetSHA) {
		return false, nil
	}
	f.admission.ReviewRunID, f.admission.ReviewID, f.admission.TargetSHA = run.ID, run.ReviewID, run.TargetSHA
	f.admission.ReviewBaseSHA, f.admission.Status, f.admission.LeaseID, f.admission.UpdatedAt = baseSHA, domain.DCPAdmissionWaiting, "", now
	return true, nil
}
func (f *fakeStore) ClaimDCPReviewLabAdmission(_ context.Context, admission domain.DCPReviewLabAdmission, leaseID, baseSHA string, now time.Time) (bool, error) {
	if f.admission == nil || f.admission.ID != admission.ID || f.admission.Status != domain.DCPAdmissionWaiting || f.run.TerminalMergeStatus != "" {
		return false, nil
	}
	f.claims++
	f.admission.Status, f.admission.LeaseID, f.admission.AdmittedBaseSHA, f.admission.UpdatedAt = domain.DCPAdmissionClaimed, leaseID, baseSHA, now
	f.run.TerminalMergeStatus = "running"
	return true, nil
}
func (f *fakeStore) CompleteDCPReviewLabAdmission(_ context.Context, admission domain.DCPReviewLabAdmission, sha string, now time.Time) (bool, error) {
	if f.admission == nil || f.admission.ID != admission.ID || f.admission.Status != domain.DCPAdmissionClaimed || f.run.TerminalMergeStatus != "running" {
		return false, nil
	}
	f.admission.Status, f.admission.MergeCommitSHA, f.admission.UpdatedAt = domain.DCPAdmissionSucceeded, sha, now
	f.run.TerminalMergeStatus, f.run.TerminalMergeCommitSHA = "succeeded", sha
	return true, nil
}
func (f *fakeStore) FailDCPReviewLabAdmission(_ context.Context, admission domain.DCPReviewLabAdmission, code string, now time.Time) (bool, error) {
	if f.admission == nil || f.admission.ID != admission.ID || f.admission.Status != domain.DCPAdmissionClaimed || f.run.TerminalMergeStatus != "running" {
		return false, nil
	}
	f.admission.Status, f.admission.ErrorCode, f.admission.UpdatedAt = domain.DCPAdmissionFailed, code, now
	f.run.TerminalMergeStatus, f.run.TerminalMergeError = "failed", code
	return true, nil
}
func (f *fakeStore) StartDCPReviewLabRefresh(_ context.Context, admission domain.DCPReviewLabAdmission, leaseID, baseSHA string, now time.Time) (bool, error) {
	if f.admission == nil || f.admission.ID != admission.ID || f.admission.Status != domain.DCPAdmissionWaiting || f.admission.RefreshWakeCount != 0 {
		return false, nil
	}
	f.admission.Status, f.admission.LeaseID, f.admission.AdmittedBaseSHA = domain.DCPAdmissionRefreshing, leaseID, baseSHA
	f.admission.RefreshWakeCount, f.admission.UpdatedAt = 1, now
	return true, nil
}
func (f *fakeStore) RecordDCPReviewLabIncident(_ context.Context, admission domain.DCPReviewLabAdmission, leaseID, baseSHA, code, packet string, now time.Time) (bool, error) {
	if f.admission == nil || f.admission.ID != admission.ID || f.admission.Status == domain.DCPAdmissionSucceeded || f.admission.Status == domain.DCPAdmissionIncident {
		return false, nil
	}
	if f.admission.Status == domain.DCPAdmissionClaimed {
		f.run.TerminalMergeStatus, f.run.TerminalMergeError = "failed", code
	}
	f.admission.Status, f.admission.LeaseID, f.admission.AdmittedBaseSHA = domain.DCPAdmissionIncident, leaseID, baseSHA
	f.admission.ErrorCode, f.admission.IncidentPacket, f.admission.UpdatedAt = code, packet, now
	return true, nil
}

type fakeSCM struct {
	observation  ports.SCMObservation
	review       ports.SCMReviewObservation
	mergeErr     error
	mergeCalls   int
	expectedHead string
	mergeSHA     string
}

func (f *fakeSCM) FetchPullRequests(context.Context, []ports.SCMPRRef) ([]ports.SCMObservation, error) {
	return []ports.SCMObservation{f.observation}, nil
}
func (f *fakeSCM) FetchReviewThreads(context.Context, ports.SCMPRRef) (ports.SCMReviewObservation, error) {
	return f.review, nil
}
func (f *fakeSCM) MergePullRequest(_ context.Context, request ports.SCMMergeRequest) (ports.SCMMergeResult, error) {
	f.mergeCalls++
	if request.ExpectedHeadSHA != f.expectedHead || request.Method != ports.SCMMergeSquash || request.PR.Repo.Repo != RepositoryFullName {
		return ports.SCMMergeResult{}, errors.New("unexpected merge request")
	}
	if f.mergeErr != nil {
		return ports.SCMMergeResult{}, f.mergeErr
	}
	return ports.SCMMergeResult{MergeCommitSHA: f.mergeSHA}, nil
}

func fixture(t *testing.T) (*Engine, *fakeStore, *fakeSCM) {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	id := domain.SessionID(AdmissionSessionA)
	workspace := filepath.Join(dataDir, "worktrees", ProjectID, string(id))
	projectPath := filepath.Join(root, "targets", ProjectID)
	privateGitDir := filepath.Join(projectPath, ".git", "worktrees", string(id))
	for _, path := range []string{workspace, privateGitDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	branch := "ao/" + string(id) + "/root"
	prURL := "https://github.com/orenvlad-ai/dcp-review-lab/pull/4"
	taskID := "i7-terminal"
	if len(TaskDisplayPrefix+taskID) > 20 {
		t.Fatal("exact task identity must fit the stock spawn display-name limit")
	}
	if !strings.Contains(ProfileAgentRules, "additional pull requests") || !strings.Contains(ProfileAgentRules, "open one ready pull request") {
		t.Fatal("exact profile must allow one ready PR while rejecting extras")
	}
	store := &fakeStore{
		includeCohortPeer: true,
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
				Worker:    domain.RoleOverride{Harness: domain.HarnessCodex, AgentConfig: domain.AgentConfig{Permissions: domain.PermissionModeAcceptEdits, DCPReviewLabNetwork: true}},
				Reviewers: []domain.ReviewerConfig{{Harness: domain.ReviewerCodex}},
			},
		},
		pr: domain.PullRequest{
			URL: prURL, SessionID: id, Number: 4, Provider: "github", Host: "github.com", Repo: RepositoryFullName,
			SourceBranch: branch, TargetBranch: TargetBranch, HeadSHA: testHead, BaseSHA: testBase,
			Author: "orenvlad-ai", ProviderState: "OPEN", HTMLURL: prURL,
		},
		run: domain.ReviewRun{
			ID: "run-7", ReviewID: "review-record-7", BatchID: "batch-7", SessionID: id, Harness: domain.ReviewerCodex,
			PRURL: prURL, TargetSHA: testHead, Body: "No blocking findings.",
			Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved, ResultChannel: structuredChannel,
		},
	}
	scm := &fakeSCM{expectedHead: testHead, mergeSHA: testMerge, review: ports.SCMReviewObservation{Decision: string(domain.ReviewNone)}, observation: ports.SCMObservation{
		Fetched: true, Provider: "github", Host: "github.com", Repo: RepositoryFullName,
		PR: ports.SCMPRObservation{
			URL: prURL, Number: 4, HeadRepo: RepositoryFullName, SourceBranch: branch, TargetBranch: TargetBranch,
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
		if cmd == "fetch --no-tags origin main" {
			return "", nil
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
				return strings.ToLower(store.pr.HeadSHA), nil
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
	scm.review.Decision = string(domain.ReviewApproved)
	candidate, ok, err := engine.candidate(context.Background(), store.session.ID)
	if err != nil || !ok {
		t.Fatalf("candidate ok=%v err=%v", ok, err)
	}
	observation, review, err := engine.fresh(context.Background(), candidate.pr)
	if err != nil || !ready(candidate, observation, review) {
		t.Fatalf("ready=false err=%v observation=%+v review=%+v", err, observation, review)
	}
	if err := engine.validateGit(context.Background(), candidate, observation.PR.HeadSHA, observation.PR.BaseSHA); err != nil {
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

func TestTryMergesWithStockNativeMissingDiffBaseMetadata(t *testing.T) {
	engine, store, scm := fixture(t)
	store.session.Metadata.DiffBaseSHA = ""
	store.session.Metadata.DiffBaseRef = ""
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if store.claims != 1 || scm.mergeCalls != 1 || store.run.TerminalMergeStatus != "succeeded" {
		t.Fatalf("claims=%d merges=%d run=%+v", store.claims, scm.mergeCalls, store.run)
	}
}

func TestTryRejectsOldSessionAndNonCleanProviderFacts(t *testing.T) {
	for _, rejectedID := range []domain.SessionID{"dcp-review-lab-6", "dcp-review-lab-8", "dcp-review-lab-11"} {
		engine, store, scm := fixture(t)
		store.session.ID = rejectedID
		if err := engine.Try(context.Background(), rejectedID); err != nil {
			t.Fatal(err)
		}
		if scm.mergeCalls != 0 || store.admission != nil {
			t.Fatalf("rejected session %s reached admission or merge", rejectedID)
		}
	}
	engine, store, scm := fixture(t)
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
		func(store *fakeStore, _ *fakeSCM) { store.session.Metadata.DiffBaseRef = "" },
		func(store *fakeStore, _ *fakeSCM) { store.pr.BaseSHA = "" },
		func(store *fakeStore, _ *fakeSCM) { store.project.Config.AgentRules += " malicious override" },
		func(store *fakeStore, _ *fakeSCM) { store.project.Config.AgentRulesFile = "AGENTS.md" },
		func(store *fakeStore, _ *fakeSCM) {
			store.project.Config.Worker.AgentConfig.DCPReviewLabNetwork = false
		},
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

func TestTryRejectsUnknownOrBlockingProviderReviewDecision(t *testing.T) {
	for _, decision := range []string{"", string(domain.ReviewRequired), string(domain.ReviewChangesRequest), "foreign"} {
		engine, store, scm := fixture(t)
		scm.review.Decision = decision
		if err := engine.Try(context.Background(), store.session.ID); err != nil {
			t.Fatal(err)
		}
		if store.claims != 0 || scm.mergeCalls != 0 {
			t.Fatalf("decision %q reached merge", decision)
		}
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
	if scm.mergeCalls != 1 || store.run.TerminalMergeStatus != "failed" || store.run.TerminalMergeError != "not_mergeable" || store.admission.Status != domain.DCPAdmissionIncident {
		t.Fatalf("merges=%d run=%+v", scm.mergeCalls, store.run)
	}
}

func TestReconcileRunningUsesFreshMergedFactWithoutSecondMutation(t *testing.T) {
	engine, store, scm := fixture(t)
	store.run.TerminalMergeStatus = "running"
	store.admission = &domain.DCPReviewLabAdmission{
		Sequence: 1, ID: "dcp-admission-" + store.run.ID, ReviewRunID: store.run.ID, ReviewID: store.run.ReviewID,
		SessionID: store.session.ID, PRURL: store.pr.URL, PRNumber: int64(store.pr.Number), TargetSHA: testHead,
		ReviewBaseSHA: testBase, AdmittedBaseSHA: testBase, Status: domain.DCPAdmissionClaimed, LeaseID: "dcp-merge-dcp-admission-" + store.run.ID,
	}
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

func TestBehindHeadGetsOneBoundedWakeThenReusesSameFIFOAdmission(t *testing.T) {
	engine, store, scm := fixture(t)
	scm.observation.PR.ProviderMergeStateStatus = "BEHIND"
	wakes := 0
	engine.SetRefreshWaker(func(_ context.Context, id domain.SessionID, prompt string) error {
		wakes++
		if id != store.session.ID || !strings.Contains(prompt, "force-with-lease") || !strings.Contains(prompt, testHead) {
			t.Fatalf("wake id=%s prompt=%q", id, prompt)
		}
		store.session.Metadata.RuntimeLaunchID = "refresh-launch"
		return nil
	})
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if wakes != 1 || store.admission == nil || store.admission.Status != domain.DCPAdmissionRefreshing || store.admission.RefreshWakeCount != 1 || scm.mergeCalls != 0 {
		t.Fatalf("wake=%d admission=%+v merges=%d", wakes, store.admission, scm.mergeCalls)
	}
	sequence := store.admission.Sequence
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if wakes != 1 {
		t.Fatalf("active refresh duplicated wake: %d", wakes)
	}

	newHead := "dddddddddddddddddddddddddddddddddddddddd"
	store.session.Metadata.RuntimeLaunchID = ""
	store.pr.HeadSHA = newHead
	store.run.ID, store.run.ReviewID, store.run.BatchID, store.run.TargetSHA = "run-8-refresh", "review-record-8-refresh", "batch-8-refresh", newHead
	scm.expectedHead = newHead
	scm.observation.PR.HeadSHA = newHead
	scm.observation.CI.HeadSHA = newHead
	scm.observation.PR.ProviderMergeStateStatus = "CLEAN"
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if wakes != 1 || scm.mergeCalls != 1 || store.admission.Sequence != sequence || store.admission.ReviewRunID != store.run.ID ||
		store.admission.TargetSHA != newHead || store.admission.Status != domain.DCPAdmissionSucceeded || store.admission.RefreshWakeCount != 1 {
		t.Fatalf("wake=%d merges=%d admission=%+v run=%+v", wakes, scm.mergeCalls, store.admission, store.run)
	}
}

func TestConflictCreatesStructuredIncidentWithoutWakeOrMerge(t *testing.T) {
	engine, store, scm := fixture(t)
	scm.observation.PR.ProviderMergeable = "CONFLICTING"
	scm.observation.PR.ProviderMergeStateStatus = "DIRTY"
	scm.observation.Mergeability.State = string(domain.MergeConflicting)
	scm.observation.Mergeability.Mergeable = false
	wakes := 0
	engine.SetRefreshWaker(func(context.Context, domain.SessionID, string) error { wakes++; return nil })
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if wakes != 0 || scm.mergeCalls != 0 || store.admission == nil || store.admission.Status != domain.DCPAdmissionIncident || store.admission.ErrorCode != "merge_conflict_or_ambiguity" {
		t.Fatalf("wake=%d merges=%d admission=%+v", wakes, scm.mergeCalls, store.admission)
	}
	var packet incidentPacket
	if err := json.Unmarshal([]byte(store.admission.IncidentPacket), &packet); err != nil {
		t.Fatal(err)
	}
	if packet.SchemaVersion != "dcp.review-lab.arbiter-needed/v1" || packet.SessionID != string(store.session.ID) || packet.TargetSHA != testHead || packet.EvidenceDigest == "" {
		t.Fatalf("packet=%+v", packet)
	}
}

func TestPassiveWaitingConsumesNoWakeOrMerge(t *testing.T) {
	engine, store, scm := fixture(t)
	scm.observation.PR.ProviderMergeable = "UNKNOWN"
	scm.observation.PR.ProviderMergeStateStatus = "UNKNOWN"
	scm.observation.Mergeability = ports.SCMMergeabilityObservation{}
	wakes := 0
	engine.SetRefreshWaker(func(context.Context, domain.SessionID, string) error { wakes++; return nil })
	for range 3 {
		if err := engine.Try(context.Background(), store.session.ID); err != nil {
			t.Fatal(err)
		}
	}
	if wakes != 0 || scm.mergeCalls != 0 || store.admission == nil || store.admission.Status != domain.DCPAdmissionWaiting {
		t.Fatalf("wake=%d merges=%d admission=%+v", wakes, scm.mergeCalls, store.admission)
	}
}

func TestCohortBarrierIsPassiveAndSurvivesStartupReconciliation(t *testing.T) {
	engine, store, scm := fixture(t)
	store.includeCohortPeer = false
	wakes := 0
	engine.SetRefreshWaker(func(context.Context, domain.SessionID, string) error { wakes++; return nil })
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if err := engine.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if wakes != 0 || store.claims != 0 || scm.mergeCalls != 0 || store.admission == nil || store.admission.Status != domain.DCPAdmissionWaiting {
		t.Fatalf("before peer: wakes=%d claims=%d merges=%d admission=%+v", wakes, store.claims, scm.mergeCalls, store.admission)
	}
	store.includeCohortPeer = true
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if wakes != 0 || store.claims != 1 || scm.mergeCalls != 1 || store.admission.Status != domain.DCPAdmissionSucceeded {
		t.Fatalf("after peer: wakes=%d claims=%d merges=%d admission=%+v", wakes, store.claims, scm.mergeCalls, store.admission)
	}
}

func TestStartupRecoversAuditedCanonicalAdvanceAndMergesCompatibleHead(t *testing.T) {
	engine, store, scm := fixture(t)
	newBase := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	packet, err := json.Marshal(incidentPacket{
		SchemaVersion: "dcp.review-lab.arbiter-needed/v1", Reason: "canonical_main_diverged",
		AdmissionID: "dcp-admission-" + store.run.ID, SessionID: string(store.session.ID),
		ReviewRunID: store.run.ID, TargetSHA: testHead,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.admission = &domain.DCPReviewLabAdmission{
		Sequence: 1, ID: "dcp-admission-" + store.run.ID, ReviewRunID: store.run.ID, ReviewID: store.run.ReviewID,
		SessionID: store.session.ID, PRURL: store.pr.URL, PRNumber: int64(store.pr.Number), TargetSHA: testHead,
		ReviewBaseSHA: testBase, AdmittedBaseSHA: testBase, Status: domain.DCPAdmissionIncident,
		LeaseID: "dcp-incident-dcp-admission-" + store.run.ID, ErrorCode: "canonical_main_diverged", IncidentPacket: string(packet),
	}
	canonicalHead := testBase
	engine.git = func(_ context.Context, path string, args ...string) (string, error) {
		cmd := strings.Join(args, " ")
		switch cmd {
		case "status --porcelain":
			return "", nil
		case "remote":
			return "origin", nil
		case "remote get-url origin":
			return RepositoryURL, nil
		case "fetch --no-tags origin main":
			return "", nil
		}
		if path == store.project.Path {
			switch cmd {
			case "rev-parse --show-toplevel":
				return path, nil
			case "branch --show-current":
				return TargetBranch, nil
			case "rev-parse origin/main":
				return newBase, nil
			case "rev-parse HEAD":
				return canonicalHead, nil
			case "merge-base --is-ancestor " + testBase + " " + newBase:
				return "", nil
			case "merge --ff-only origin/main":
				canonicalHead = newBase
				return "", nil
			case "merge-tree --write-tree " + newBase + " " + testHead:
				return testMerge, nil
			}
		}
		if path == store.session.Metadata.WorkspacePath {
			switch cmd {
			case "rev-parse --show-toplevel":
				return path, nil
			case "branch --show-current":
				return store.session.Metadata.Branch, nil
			case "rev-parse HEAD":
				return testHead, nil
			case "rev-parse --path-format=absolute --git-common-dir":
				return filepath.Join(store.project.Path, ".git"), nil
			case "rev-parse --path-format=absolute --absolute-git-dir":
				return filepath.Join(store.project.Path, ".git", "worktrees", string(store.session.ID)), nil
			}
		}
		return "", errors.New("unexpected git command: " + cmd)
	}

	if err := engine.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scm.mergeCalls != 1 || store.admission.Status != domain.DCPAdmissionSucceeded ||
		store.admission.AdmittedBaseSHA != newBase || store.admission.RecoveredIncidentPacket != string(packet) ||
		store.admission.IncidentPacket != "" || store.admission.ErrorCode != "" {
		t.Fatalf("merges=%d admission=%+v", scm.mergeCalls, store.admission)
	}
}
