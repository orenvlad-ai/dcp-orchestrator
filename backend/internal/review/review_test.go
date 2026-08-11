package review

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// --- fakes ---

type fakeStore struct {
	review               *domain.Review
	runs                 []domain.ReviewRun
	listAllReviewRunHits int
	// insertErr, when set, makes the next InsertReviewRun model a concurrent
	// writer that already recorded a run for this commit: it records that
	// winner (so a follow-up GetReviewRunBySessionAndSHA finds it) and returns
	// insertErr instead of recording the caller's run.
	insertErr              error
	insertErrWinnerAtFront bool
}

func (f *fakeStore) UpsertReview(_ context.Context, r domain.Review) error {
	cp := r
	f.review = &cp
	return nil
}
func (f *fakeStore) GetReviewBySession(_ context.Context, _ domain.SessionID) (domain.Review, bool, error) {
	if f.review == nil {
		return domain.Review{}, false, nil
	}
	return *f.review, true, nil
}
func (f *fakeStore) InsertReviewRun(_ context.Context, r domain.ReviewRun) error {
	if f.insertErr != nil {
		winner := r
		winner.ID = "winner-" + r.ID
		if f.insertErrWinnerAtFront {
			f.runs = append([]domain.ReviewRun{winner}, f.runs...)
		} else {
			f.runs = append(f.runs, winner)
		}
		return f.insertErr
	}
	// Mirrors idx_review_run_session_pr_sha_harness. Harness is part of the key so
	// a second reviewer on the same commit is a distinct pass, not a duplicate.
	for _, existing := range f.runs {
		if existing.SessionID == r.SessionID &&
			existing.PRURL == r.PRURL &&
			existing.TargetSHA == r.TargetSHA &&
			existing.Harness == r.Harness &&
			existing.TargetSHA != "" &&
			existing.Status != domain.ReviewRunFailed &&
			existing.Status != domain.ReviewRunCancelled &&
			(existing.Status == domain.ReviewRunRunning ||
				(existing.Verdict != domain.VerdictNone && existing.Verdict != domain.VerdictChangesRequested)) {
			return domain.ErrDuplicateReviewRun
		}
	}
	f.runs = append(f.runs, r)
	return nil
}
func (f *fakeStore) UpdateReviewRunResult(_ context.Context, id string, status domain.ReviewRunStatus, verdict domain.ReviewVerdict, body, githubReviewID string) (bool, error) {
	for i := range f.runs {
		if f.runs[i].ID == id {
			if f.runs[i].Status != domain.ReviewRunRunning {
				return false, nil
			}
			f.runs[i].Status = status
			f.runs[i].Verdict = verdict
			f.runs[i].Body = body
			f.runs[i].GithubReviewID = githubReviewID
			return true, nil
		}
	}
	return false, nil
}
func (f *fakeStore) SupersedeStaleRunningReviewRuns(_ context.Context, sessionID domain.SessionID, prURL, targetSHA, body string) (int64, error) {
	var n int64
	for i := range f.runs {
		if f.runs[i].SessionID == sessionID && f.runs[i].PRURL == prURL && f.runs[i].TargetSHA != targetSHA && f.runs[i].Status == domain.ReviewRunRunning && f.runs[i].Verdict == domain.VerdictNone {
			f.runs[i].Status = domain.ReviewRunFailed
			f.runs[i].Body = body
			n++
		}
	}
	return n, nil
}
func (f *fakeStore) CancelRunningReviewRunsBySession(_ context.Context, sessionID domain.SessionID, body string) (int64, error) {
	var n int64
	for i := range f.runs {
		if f.runs[i].SessionID == sessionID && f.runs[i].Status == domain.ReviewRunRunning && f.runs[i].Verdict == domain.VerdictNone {
			f.runs[i].Status = domain.ReviewRunCancelled
			f.runs[i].Body = body
			n++
		}
	}
	return n, nil
}
func (f *fakeStore) GetReviewRun(_ context.Context, id string) (domain.ReviewRun, bool, error) {
	for _, r := range f.runs {
		if r.ID == id {
			return r, true, nil
		}
	}
	return domain.ReviewRun{}, false, nil
}
func (f *fakeStore) GetReviewRunBySessionPRAndSHA(_ context.Context, sessionID domain.SessionID, prURL, sha string) (domain.ReviewRun, bool, error) {
	for i := len(f.runs) - 1; i >= 0; i-- {
		if f.runs[i].SessionID == sessionID && f.runs[i].PRURL == prURL && f.runs[i].TargetSHA == sha {
			return f.runs[i], true, nil
		}
	}
	return domain.ReviewRun{}, false, nil
}
func (f *fakeStore) GetReviewRunBySessionPRSHAAndHarness(_ context.Context, sessionID domain.SessionID, prURL, sha string, harness domain.ReviewerHarness) (domain.ReviewRun, bool, error) {
	for i := len(f.runs) - 1; i >= 0; i-- {
		if f.runs[i].SessionID == sessionID && f.runs[i].PRURL == prURL && f.runs[i].TargetSHA == sha && f.runs[i].Harness == harness {
			return f.runs[i], true, nil
		}
	}
	return domain.ReviewRun{}, false, nil
}
func (f *fakeStore) ListReviewRunsBySession(_ context.Context, _ domain.SessionID) ([]domain.ReviewRun, error) {
	f.listAllReviewRunHits++
	return f.runs, nil
}
func (f *fakeStore) ListRunningReviewRunsBySession(_ context.Context, sessionID domain.SessionID) ([]domain.ReviewRun, error) {
	out := make([]domain.ReviewRun, 0)
	for _, run := range f.runs {
		if run.SessionID == sessionID && run.Status == domain.ReviewRunRunning && run.Verdict == domain.VerdictNone {
			out = append(out, run)
		}
	}
	return out, nil
}

type fakeSessions struct {
	rec     domain.SessionRecord
	ok      bool
	records []domain.SessionRecord
}

type fakeReviewWorkspacePreparer struct {
	rec       domain.SessionRecord
	err       error
	calls     int
	targetSHA string
}

func (f *fakeReviewWorkspacePreparer) PrepareReviewWorkspace(_ context.Context, _ domain.SessionID, targetSHA string) (domain.SessionRecord, error) {
	f.calls++
	f.targetSHA = targetSHA
	return f.rec, f.err
}

func (f fakeSessions) GetSession(_ context.Context, _ domain.SessionID) (domain.SessionRecord, bool, error) {
	return f.rec, f.ok, nil
}
func (f fakeSessions) ListAllSessions(context.Context) ([]domain.SessionRecord, error) {
	if f.records != nil {
		return f.records, nil
	}
	if f.ok {
		return []domain.SessionRecord{f.rec}, nil
	}
	return nil, nil
}

type fakePRs struct{ prs []domain.PullRequest }

func (f fakePRs) ListPRsBySession(_ context.Context, _ domain.SessionID) ([]domain.PullRequest, error) {
	return f.prs, nil
}

type fakeProjects struct {
	cfg domain.ProjectConfig
	rec *domain.ProjectRecord
}

func (f fakeProjects) GetProject(_ context.Context, id string) (domain.ProjectRecord, bool, error) {
	if f.rec != nil {
		return *f.rec, true, nil
	}
	return domain.ProjectRecord{ID: id, Config: f.cfg}, true, nil
}

type fakeLauncher struct {
	handle           string
	alive            bool
	spawnErr         error
	notifyErr        error
	spawned          bool
	spawnCount       int
	notified         bool
	cancelled        bool
	cancelErr        error
	aliveErr         error
	gotSpec          LaunchSpec
	gotHandle        string
	cancelledHandle  string
	cancelledHarness domain.ReviewerHarness
	specs            []LaunchSpec
	handles          []string
	preflightErr     error
	preflighted      bool
	processAlive     bool
	processAliveErr  error
}

func (f *fakeLauncher) Spawn(_ context.Context, spec LaunchSpec) (string, error) {
	f.spawned = true
	f.spawnCount++
	f.gotSpec = spec
	f.specs = append(f.specs, spec)
	if f.spawnErr != nil {
		return "", f.spawnErr
	}
	return f.handle, nil
}
func (f *fakeLauncher) Notify(_ context.Context, handleID string, spec LaunchSpec) error {
	f.notified = true
	f.gotHandle = handleID
	f.gotSpec = spec
	f.handles = append(f.handles, handleID)
	f.specs = append(f.specs, spec)
	return f.notifyErr
}
func (f *fakeLauncher) Alive(_ context.Context, _ string) (bool, error) {
	return f.alive || f.spawned, f.aliveErr
}
func (f *fakeLauncher) Cancel(_ context.Context, handleID string, harness domain.ReviewerHarness) error {
	f.cancelled = true
	f.cancelledHandle = handleID
	f.cancelledHarness = harness
	return f.cancelErr
}
func (f *fakeLauncher) Preflight(_ context.Context, _ domain.ReviewerHarness, _ string) error {
	f.preflighted = true
	return f.preflightErr
}
func (f *fakeLauncher) ReviewerProcessAlive(_ context.Context, _, _ string) (bool, error) {
	return f.processAlive || f.spawned, f.processAliveErr
}

func liveWorker() domain.SessionRecord {
	return domain.SessionRecord{
		ID:        "mer-1",
		ProjectID: "mer",
		Harness:   domain.HarnessClaudeCode,
		Metadata:  domain.SessionMetadata{WorkspacePath: "/ws/mer-1"},
	}
}

func newEngineForTest(store Store, sessions Sessions, prs PRs, projects Projects, launcher Launcher) *Engine {
	return newEngineForTestWithPreparer(store, sessions, prs, projects, launcher, nil)
}

func newEngineForTestWithPreparer(store Store, sessions Sessions, prs PRs, projects Projects, launcher Launcher, preparer WorkspacePreparer) *Engine {
	ids := 0
	return New(Deps{
		Store: store, Sessions: sessions, PRs: prs, Projects: projects, Launcher: launcher, WorkspacePreparer: preparer,
		Clock: func() time.Time { return time.Unix(0, 0).UTC() },
		NewID: func() string { ids++; return "id-" + string(rune('0'+ids)) },
	})
}

func prAt(sha string) fakePRs {
	return fakePRs{prs: []domain.PullRequest{{URL: "https://github.com/o/r/pull/1", Number: 1, HeadSHA: sha}}}
}

func newReviewGitRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	runReviewGit(t, dir, "init")
	runReviewGit(t, dir, "config", "user.email", "review@example.com")
	runReviewGit(t, dir, "config", "user.name", "Review Tests")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("review\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runReviewGit(t, dir, "add", ".")
	runReviewGit(t, dir, "commit", "-m", "initial")
	runReviewGit(t, dir, "branch", "-M", "main")
	return dir, strings.TrimSpace(runReviewGit(t, dir, "rev-parse", "HEAD"))
}

func runReviewGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func idleWorker() domain.SessionRecord {
	rec := liveWorker()
	rec.Activity.State = domain.ActivityIdle
	return rec
}

func TestWorkspacePreparerRestoresExactCleanHeadWithoutWorkerLaunch(t *testing.T) {
	repo, head := newReviewGitRepo(t)
	worker := idleWorker()
	worker.IsTerminated = true
	worker.Activity.State = domain.ActivityExited
	worker.Metadata.WorkspacePath = repo
	worker.Metadata.Branch = "main"
	project := domain.ProjectRecord{ID: string(worker.ProjectID), Kind: domain.ProjectKindSingleRepo}
	var got ports.WorkspaceConfig
	preparer := NewWorkspacePreparer(fakeSessions{rec: worker, ok: true}, fakeProjects{rec: &project}, func(_ context.Context, cfg ports.WorkspaceConfig) (ports.WorkspaceInfo, error) {
		got = cfg
		return ports.WorkspaceInfo{Path: repo, Branch: "main", RepoPath: repo, SessionID: worker.ID, ProjectID: worker.ProjectID}, nil
	})

	rec, err := preparer.PrepareReviewWorkspace(context.Background(), worker.ID, head)
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != worker.ID || got.Path != repo || got.Branch != "main" {
		t.Fatalf("restore config = %+v", got)
	}
	if !rec.IsTerminated || rec.Activity.State != domain.ActivityExited || rec.Metadata.RuntimeLaunchID != "" || rec.Metadata.WorkspacePath != repo {
		t.Fatalf("prepared record changed worker lifecycle: %+v", rec)
	}
}

func TestWorkspacePreparerFailsClosedOnHeadCleanlinessAndProjectKind(t *testing.T) {
	for _, tc := range []struct {
		name        string
		projectKind domain.ProjectKind
		target      func(string) string
		dirty       bool
		wantErr     string
	}{
		{name: "wrong exact head", projectKind: domain.ProjectKindSingleRepo, target: func(string) string { return strings.Repeat("0", 40) }, wantErr: "does not match exact review target"},
		{name: "dirty worktree", projectKind: domain.ProjectKindSingleRepo, dirty: true, wantErr: "not clean"},
		{name: "workspace project", projectKind: domain.ProjectKindWorkspace, wantErr: "single-repo project"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, head := newReviewGitRepo(t)
			if tc.dirty {
				if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			worker := idleWorker()
			worker.IsTerminated = true
			worker.Activity.State = domain.ActivityExited
			worker.Metadata.WorkspacePath = repo
			worker.Metadata.Branch = "main"
			project := domain.ProjectRecord{ID: string(worker.ProjectID), Kind: tc.projectKind}
			restoreCalls := 0
			preparer := NewWorkspacePreparer(fakeSessions{rec: worker, ok: true}, fakeProjects{rec: &project}, func(_ context.Context, _ ports.WorkspaceConfig) (ports.WorkspaceInfo, error) {
				restoreCalls++
				return ports.WorkspaceInfo{Path: repo, Branch: "main", SessionID: worker.ID, ProjectID: worker.ProjectID}, nil
			})
			target := head
			if tc.target != nil {
				target = tc.target(head)
			}
			if _, err := preparer.PrepareReviewWorkspace(context.Background(), worker.ID, target); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
			if tc.projectKind == domain.ProjectKindWorkspace && restoreCalls != 0 {
				t.Fatalf("workspace project restore calls = %d", restoreCalls)
			}
		})
	}
}

// --- tests ---

func TestAutoTriggerIsExactHeadSingleFlightAndDoesNotRetryFailure(t *testing.T) {
	store := &fakeStore{}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	prs := &fakePRs{prs: prAt("sha1").prs}
	eng := newEngineForTest(store, fakeSessions{rec: idleWorker(), ok: true}, prs, fakeProjects{}, launcher)

	first, err := eng.AutoTrigger(context.Background(), "mer-1")
	if err != nil || !first.Created {
		t.Fatalf("first AutoTrigger = %+v, %v", first, err)
	}
	second, err := eng.AutoTrigger(context.Background(), "mer-1")
	if err != nil || second.Created || launcher.spawnCount != 1 {
		t.Fatalf("idempotent AutoTrigger = %+v, err=%v spawns=%d", second, err, launcher.spawnCount)
	}
	if _, err := store.UpdateReviewRunResult(context.Background(), first.Run.ID, domain.ReviewRunFailed, domain.VerdictNone, "technical failure", ""); err != nil {
		t.Fatal(err)
	}
	third, err := eng.AutoTrigger(context.Background(), "mer-1")
	if err != nil || third.Created || launcher.spawnCount != 1 {
		t.Fatalf("failed current head retried automatically: %+v err=%v spawns=%d", third, err, launcher.spawnCount)
	}
	prs.prs = prAt("sha2").prs
	fourth, err := eng.AutoTrigger(context.Background(), "mer-1")
	if err != nil || !fourth.Created || fourth.Run.TargetSHA != "sha2" || launcher.spawnCount != 2 {
		t.Fatalf("new exact head = %+v err=%v spawns=%d", fourth, err, launcher.spawnCount)
	}
}

func TestDCPReviewLabAllowsTwoDistinctHeadsWithoutManualOrDuplicateReview(t *testing.T) {
	worker := idleWorker()
	worker.ID, worker.ProjectID, worker.Harness = "dcp-review-lab-8", "dcp-review-lab", domain.HarnessCodex
	store := &fakeStore{}
	launcher := &fakeLauncher{handle: "review-dcp-review-lab-8"}
	prs := &fakePRs{prs: prAt("sha1").prs}
	eng := newEngineForTest(store, fakeSessions{rec: worker, ok: true}, prs, fakeProjects{}, launcher)

	if res, err := eng.Trigger(context.Background(), worker.ID, ""); err == nil || res.Created || !errors.Is(err, ErrInvalid) {
		t.Fatalf("manual trigger = %+v err=%v", res, err)
	}
	first, err := eng.AutoTrigger(context.Background(), worker.ID)
	if err != nil || !first.Created {
		t.Fatalf("first trigger = %+v err=%v", first, err)
	}
	if _, err := store.UpdateReviewRunResult(context.Background(), first.Run.ID, domain.ReviewRunComplete, domain.VerdictApproved, "approved", ""); err != nil {
		t.Fatal(err)
	}
	prs.prs = prAt("sha2").prs
	second, err := eng.AutoTrigger(context.Background(), worker.ID)
	if err != nil || !second.Created || launcher.spawnCount != 2 {
		t.Fatalf("second exact head = %+v err=%v spawns=%d", second, err, launcher.spawnCount)
	}
	if _, err := store.UpdateReviewRunResult(context.Background(), second.Run.ID, domain.ReviewRunFailed, domain.VerdictNone, "failed", ""); err != nil {
		t.Fatal(err)
	}
	prs.prs = prAt("sha3").prs
	third, err := eng.AutoTrigger(context.Background(), worker.ID)
	if err != nil || third.Created || launcher.spawnCount != 2 {
		t.Fatalf("third head exceeded budget: %+v err=%v spawns=%d", third, err, launcher.spawnCount)
	}
}

func TestDCPReviewLabRejectsFutureCardAutomatically(t *testing.T) {
	worker := idleWorker()
	worker.ID, worker.ProjectID, worker.Harness = "dcp-review-lab-10", "dcp-review-lab", domain.HarnessCodex
	launcher := &fakeLauncher{handle: "review-dcp-review-lab-10"}
	eng := newEngineForTest(&fakeStore{}, fakeSessions{rec: worker, ok: true}, prAt("sha1"), fakeProjects{}, launcher)
	res, err := eng.AutoTrigger(context.Background(), worker.ID)
	if err != nil || res.Created || launcher.spawnCount != 0 {
		t.Fatalf("future card trigger = %+v err=%v spawns=%d", res, err, launcher.spawnCount)
	}
}

func TestAutoTriggerRequiresIdleWorkerAndEligiblePR(t *testing.T) {
	for _, tc := range []struct {
		name   string
		worker domain.SessionRecord
		pr     domain.PullRequest
	}{
		{name: "active", worker: func() domain.SessionRecord { r := idleWorker(); r.Activity.State = domain.ActivityActive; return r }(), pr: prAt("sha1").prs[0]},
		{name: "active launch", worker: func() domain.SessionRecord { r := idleWorker(); r.Metadata.RuntimeLaunchID = "launch-1"; return r }(), pr: prAt("sha1").prs[0]},
		{name: "terminated without proven workspace failure", worker: func() domain.SessionRecord {
			r := idleWorker()
			r.IsTerminated = true
			r.Activity.State = domain.ActivityExited
			return r
		}(), pr: prAt("sha1").prs[0]},
		{name: "draft", worker: idleWorker(), pr: func() domain.PullRequest { p := prAt("sha1").prs[0]; p.Draft = true; return p }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			launcher := &fakeLauncher{handle: "review-mer-1"}
			eng := newEngineForTest(&fakeStore{}, fakeSessions{rec: tc.worker, ok: true}, fakePRs{prs: []domain.PullRequest{tc.pr}}, fakeProjects{}, launcher)
			res, err := eng.AutoTrigger(context.Background(), "mer-1")
			if err != nil || res.Created || launcher.spawnCount != 0 {
				t.Fatalf("AutoTrigger = %+v err=%v spawns=%d", res, err, launcher.spawnCount)
			}
		})
	}
}

func TestReconcileStartupRecoversProvenStaleRunOnce(t *testing.T) {
	worker := idleWorker()
	worker.IsTerminated = true
	worker.Activity.State = domain.ActivityExited
	old := domain.ReviewRun{ID: "old-run", ReviewID: "review-1", SessionID: worker.ID, Harness: domain.ReviewerCodex, PRURL: prAt("sha1").prs[0].URL, TargetSHA: "sha1", Status: domain.ReviewRunRunning, CreatedAt: time.Unix(1, 0)}
	store := &fakeStore{review: &domain.Review{ID: "review-1", SessionID: worker.ID, ReviewerHandleID: "review-mer-1", Harness: domain.ReviewerCodex}, runs: []domain.ReviewRun{old}}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	preparer := &fakeReviewWorkspacePreparer{rec: worker}
	eng := newEngineForTestWithPreparer(store, fakeSessions{rec: worker, ok: true}, prAt("sha1"), fakeProjects{}, launcher, preparer)

	if err := eng.ReconcileStartup(context.Background()); err != nil {
		t.Fatalf("ReconcileStartup: %v", err)
	}
	if store.runs[0].Status != domain.ReviewRunFailed || launcher.spawnCount != 1 || len(store.runs) != 2 || store.runs[1].TargetSHA != "sha1" {
		t.Fatalf("reconciled runs=%+v spawns=%d", store.runs, launcher.spawnCount)
	}
	if preparer.calls != 1 || preparer.targetSHA != "sha1" {
		t.Fatalf("workspace preparer calls=%d target=%q", preparer.calls, preparer.targetSHA)
	}
	if err := eng.ReconcileStartup(context.Background()); err != nil {
		t.Fatalf("second ReconcileStartup: %v", err)
	}
	if launcher.spawnCount != 1 || len(store.runs) != 2 {
		t.Fatalf("recovery duplicated: runs=%d spawns=%d", len(store.runs), launcher.spawnCount)
	}
}

func TestAutoTriggerContinuesProvenMissingWorkspaceOnOneNewExactHead(t *testing.T) {
	worker := idleWorker()
	worker.IsTerminated = true
	worker.Activity.State = domain.ActivityExited
	failed := domain.ReviewRun{
		ID: "failed-run", ReviewID: "review-1", SessionID: worker.ID, Harness: domain.ReviewerCodex,
		PRURL: prAt("sha1").prs[0].URL, TargetSHA: "sha1", Status: domain.ReviewRunFailed,
		Body:      "launch reviewer: reviewer runtime: runtime: session working directory mismatch: session review-mer-1 started in empty cwd, want /ws/mer-1",
		CreatedAt: time.Unix(-1, 0),
	}
	store := &fakeStore{review: &domain.Review{ID: "review-1", SessionID: worker.ID, ReviewerHandleID: "review-mer-1", Harness: domain.ReviewerCodex}, runs: []domain.ReviewRun{failed}}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	restored := worker
	restored.Metadata.WorkspacePath = "/restored/mer-1"
	preparer := &fakeReviewWorkspacePreparer{rec: restored}
	prs := &fakePRs{prs: prAt("sha2").prs}
	eng := newEngineForTestWithPreparer(store, fakeSessions{rec: worker, ok: true}, prs, fakeProjects{}, launcher, preparer)

	first, err := eng.AutoTrigger(context.Background(), worker.ID)
	if err != nil || !first.Created || first.Run.TargetSHA != "sha2" {
		t.Fatalf("continued AutoTrigger = %+v, err=%v", first, err)
	}
	if preparer.calls != 1 || preparer.targetSHA != "sha2" || launcher.spawnCount != 1 || launcher.gotSpec.WorkspacePath != restored.Metadata.WorkspacePath {
		t.Fatalf("preparer=%+v launcher=%+v", preparer, launcher)
	}
	second, err := eng.AutoTrigger(context.Background(), worker.ID)
	if err != nil || second.Created || launcher.spawnCount != 1 || preparer.calls != 1 {
		t.Fatalf("same-head continuation duplicated: %+v err=%v preparer=%d spawns=%d", second, err, preparer.calls, launcher.spawnCount)
	}
	if _, err := store.UpdateReviewRunResult(context.Background(), first.Run.ID, domain.ReviewRunComplete, domain.VerdictApproved, "approved", "review-2"); err != nil {
		t.Fatal(err)
	}
	prs.prs = prAt("sha3").prs
	third, err := eng.AutoTrigger(context.Background(), worker.ID)
	if err != nil || third.Created || launcher.spawnCount != 1 || preparer.calls != 1 {
		t.Fatalf("consumed continuation retried on newer head: %+v err=%v preparer=%d spawns=%d", third, err, preparer.calls, launcher.spawnCount)
	}
}

func TestPreservedReviewContinuationEligibilityIsSingleUse(t *testing.T) {
	marker := "launch reviewer: session working directory mismatch: empty cwd"
	failed := domain.ReviewRun{
		ID: "failed-1", Status: domain.ReviewRunFailed, Verdict: domain.VerdictNone,
		Body: marker, CreatedAt: time.Unix(1, 0),
	}
	if !PreservedReviewContinuationEligible(true, []domain.ReviewRun{failed}) {
		t.Fatal("one latest proven missing-worktree failure should be eligible")
	}
	if PreservedReviewContinuationEligible(false, []domain.ReviewRun{failed}) {
		t.Fatal("missing review ownership should be ineligible")
	}
	failed.Verdict = domain.VerdictChangesRequested
	if PreservedReviewContinuationEligible(true, []domain.ReviewRun{failed}) {
		t.Fatal("a persisted verdict should consume eligibility")
	}
	failed.Verdict = domain.VerdictNone
	second := failed
	second.ID = "failed-2"
	second.CreatedAt = time.Unix(2, 0)
	if PreservedReviewContinuationEligible(true, []domain.ReviewRun{failed, second}) {
		t.Fatal("a second missing-worktree failure must consume the continuation")
	}
	other := second
	other.Body = "reviewer preflight failed"
	if PreservedReviewContinuationEligible(true, []domain.ReviewRun{failed, other}) {
		t.Fatal("a newer non-proven failure must consume the continuation")
	}
}

func TestAutoTriggerFailsClosedWhenPreservedWorkspaceCannotBePrepared(t *testing.T) {
	worker := idleWorker()
	worker.IsTerminated = true
	worker.Activity.State = domain.ActivityExited
	failed := domain.ReviewRun{
		ID: "failed-run", ReviewID: "review-1", SessionID: worker.ID, Harness: domain.ReviewerCodex,
		PRURL: prAt("sha1").prs[0].URL, TargetSHA: "sha1", Status: domain.ReviewRunFailed,
		Body: "session working directory mismatch:", CreatedAt: time.Unix(-1, 0),
	}
	store := &fakeStore{review: &domain.Review{ID: "review-1", SessionID: worker.ID, Harness: domain.ReviewerCodex}, runs: []domain.ReviewRun{failed}}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	preparer := &fakeReviewWorkspacePreparer{err: errors.New("exact head mismatch")}
	prs := &fakePRs{prs: prAt("sha2").prs}
	eng := newEngineForTestWithPreparer(store, fakeSessions{rec: worker, ok: true}, prs, fakeProjects{}, launcher, preparer)

	if res, err := eng.AutoTrigger(context.Background(), worker.ID); err == nil || res.Created || !strings.Contains(err.Error(), "exact head mismatch") {
		t.Fatalf("AutoTrigger = %+v err=%v", res, err)
	}
	if len(store.runs) != 2 || store.runs[1].Status != domain.ReviewRunFailed || launcher.spawnCount != 0 || preparer.calls != 1 {
		t.Fatalf("runs=%+v preparer=%d spawns=%d", store.runs, preparer.calls, launcher.spawnCount)
	}
	prs.prs = prAt("sha3").prs
	if res, err := eng.AutoTrigger(context.Background(), worker.ID); err != nil || res.Created || preparer.calls != 1 {
		t.Fatalf("failed preparation retried automatically: %+v err=%v preparer=%d", res, err, preparer.calls)
	}
}

func TestReconcileStartupFailsAmbiguousRunWithoutModelCall(t *testing.T) {
	worker := idleWorker()
	old := domain.ReviewRun{ID: "old-run", ReviewID: "review-1", SessionID: worker.ID, Harness: domain.ReviewerCodex, PRURL: prAt("sha1").prs[0].URL, TargetSHA: "sha1", Status: domain.ReviewRunRunning}
	store := &fakeStore{review: &domain.Review{ID: "review-1", SessionID: worker.ID, ReviewerHandleID: "review-mer-1"}, runs: []domain.ReviewRun{old}}
	launcher := &fakeLauncher{handle: "review-mer-1", processAliveErr: errors.New("probe unavailable")}
	eng := newEngineForTest(store, fakeSessions{rec: worker, ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	if err := eng.ReconcileStartup(context.Background()); err != nil {
		t.Fatalf("ReconcileStartup: %v", err)
	}
	if store.runs[0].Status != domain.ReviewRunFailed || launcher.spawnCount != 0 || !strings.Contains(store.runs[0].Body, "could not be verified") {
		t.Fatalf("ambiguous reconcile runs=%+v spawns=%d", store.runs, launcher.spawnCount)
	}
}

func TestReconcileStartupPreservesActiveReview(t *testing.T) {
	worker := idleWorker()
	old := domain.ReviewRun{ID: "old-run", ReviewID: "review-1", SessionID: worker.ID, Harness: domain.ReviewerCodex, PRURL: prAt("sha1").prs[0].URL, TargetSHA: "sha1", Status: domain.ReviewRunRunning}
	store := &fakeStore{review: &domain.Review{ID: "review-1", SessionID: worker.ID, ReviewerHandleID: "review-mer-1"}, runs: []domain.ReviewRun{old}}
	launcher := &fakeLauncher{handle: "review-mer-1", processAlive: true}
	eng := newEngineForTest(store, fakeSessions{rec: worker, ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	if err := eng.ReconcileStartup(context.Background()); err != nil {
		t.Fatalf("ReconcileStartup: %v", err)
	}
	if store.runs[0].Status != domain.ReviewRunRunning || launcher.spawnCount != 0 {
		t.Fatalf("active review changed: run=%+v spawns=%d", store.runs[0], launcher.spawnCount)
	}
}

func TestReconcileStartupPreservesStructuredVerdictWithoutSecondReviewer(t *testing.T) {
	worker := idleWorker()
	approved := domain.ReviewRun{
		ID: "run-structured", ReviewID: "review-1", SessionID: worker.ID, Harness: domain.ReviewerCodex,
		PRURL: prAt("sha1").prs[0].URL, TargetSHA: "sha1", Status: domain.ReviewRunComplete,
		Verdict: domain.VerdictApproved, Body: "No blocking findings.", CreatedAt: time.Unix(1, 0),
	}
	store := &fakeStore{
		review: &domain.Review{ID: "review-1", SessionID: worker.ID, ReviewerHandleID: "review-mer-1", Harness: domain.ReviewerCodex},
		runs:   []domain.ReviewRun{approved},
	}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	eng := newEngineForTest(store, fakeSessions{rec: worker, ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	for i := 0; i < 2; i++ {
		if err := eng.ReconcileStartup(context.Background()); err != nil {
			t.Fatalf("ReconcileStartup %d: %v", i+1, err)
		}
	}
	if launcher.spawnCount != 0 || len(store.runs) != 1 || store.runs[0].Verdict != domain.VerdictApproved {
		t.Fatalf("restart duplicated or changed verdict: runs=%+v spawns=%d", store.runs, launcher.spawnCount)
	}
}

func TestTriggerSpawnsNewReviewerAndRecordsRunAfterLaunch(t *testing.T) {
	store := &fakeStore{}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if !res.Created || res.ReviewerHandleID != "review-mer-1" {
		t.Fatalf("result = %+v", res)
	}
	if !launcher.spawned || launcher.notified {
		t.Fatalf("expected spawn (no live reviewer): %+v", launcher)
	}
	if res.Run.TargetSHA != "sha1" || res.Run.Status != domain.ReviewRunRunning || res.Run.Harness != domain.ReviewerClaudeCode {
		t.Fatalf("run = %+v", res.Run)
	}
	if launcher.gotSpec.RunID != res.Run.ID || launcher.gotSpec.BatchID != res.Run.BatchID {
		t.Fatalf("launch spec ids = batch %q run %q, want batch %q run %q", launcher.gotSpec.BatchID, launcher.gotSpec.RunID, res.Run.BatchID, res.Run.ID)
	}
	if len(store.runs) != 1 || store.review == nil || store.review.ReviewerHandleID != "review-mer-1" {
		t.Fatalf("persisted review=%+v runs=%+v", store.review, store.runs)
	}
}

func TestCancelInterruptsReviewerAndCancelsRunningRuns(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerCodex, ReviewerHandleID: "review-mer-1"},
		runs: []domain.ReviewRun{
			{ID: "run-1", ReviewID: "rev-1", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1", Status: domain.ReviewRunRunning},
			{ID: "run-2", ReviewID: "rev-1", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/2", TargetSHA: "sha2", Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved},
		},
	}
	launcher := &fakeLauncher{}
	prs := fakePRs{prs: []domain.PullRequest{
		{URL: "https://github.com/o/r/pull/1", Number: 1, HeadSHA: "sha1"},
		{URL: "https://github.com/o/r/pull/2", Number: 2, HeadSHA: "sha2"},
	}}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prs, fakeProjects{}, launcher)

	res, err := eng.Cancel(context.Background(), "mer-1")
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if !launcher.cancelled || launcher.cancelledHandle != "review-mer-1" {
		t.Fatalf("launcher cancel = %v handle=%q", launcher.cancelled, launcher.cancelledHandle)
	}
	if launcher.cancelledHarness != domain.ReviewerCodex {
		t.Fatalf("cancel harness = %q, want codex", launcher.cancelledHarness)
	}
	if len(res.CancelledRuns) != 1 || res.CancelledRuns[0].ID != "run-1" {
		t.Fatalf("cancelled runs = %+v", res.CancelledRuns)
	}
	if store.runs[0].Status != domain.ReviewRunCancelled || !strings.Contains(store.runs[0].Body, "cancelled") {
		t.Fatalf("run not marked cancelled: %+v", store.runs[0])
	}
	if store.runs[1].Status != domain.ReviewRunComplete {
		t.Fatalf("non-running run was changed: %+v", store.runs[1])
	}
	if store.listAllReviewRunHits != 1 {
		t.Fatalf("full review run list calls = %d, want 1 for final plan refresh only", store.listAllReviewRunHits)
	}
	if res.Reviews[0].Status == ReviewStateRunning {
		t.Fatalf("review state still running: %+v", res.Reviews[0])
	}
}

func TestCancelMarksRunsCancelledWhenReviewerHandleIsGone(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerCodex, ReviewerHandleID: "review-mer-1"},
		runs: []domain.ReviewRun{{
			ID: "run-1", ReviewID: "rev-1", SessionID: "mer-1",
			PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1", Status: domain.ReviewRunRunning,
		}},
	}
	launcher := &fakeLauncher{cancelErr: errors.New("runtime: session not found")}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.Cancel(context.Background(), "mer-1")
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if !launcher.cancelled {
		t.Fatal("expected launcher cancellation to be attempted")
	}
	if got := store.runs[0]; got.Status != domain.ReviewRunCancelled {
		t.Fatalf("run not marked cancelled after stale handle: %+v", got)
	}
	if len(res.CancelledRuns) != 1 || res.CancelledRuns[0].ID != "run-1" {
		t.Fatalf("cancelled runs = %+v", res.CancelledRuns)
	}
}

func TestCancelKeepsRunsRunningWhenReviewerCancelFailsAndHandleIsAlive(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerCodex, ReviewerHandleID: "review-mer-1"},
		runs: []domain.ReviewRun{{
			ID: "run-1", ReviewID: "rev-1", SessionID: "mer-1",
			PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1", Status: domain.ReviewRunRunning,
		}},
	}
	launcher := &fakeLauncher{alive: true, cancelErr: errors.New("interrupt failed")}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	if _, err := eng.Cancel(context.Background(), "mer-1"); err == nil {
		t.Fatal("Cancel err = nil, want interrupt failure")
	}
	if got := store.runs[0]; got.Status != domain.ReviewRunRunning {
		t.Fatalf("run should remain running when reviewer is still alive: %+v", got)
	}
}

func TestTriggerConcurrentSameWorkerSpawnsOnce(t *testing.T) {
	store := &fakeStore{}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	const n = 8
	var wg sync.WaitGroup
	results := make([]TriggerResult, n)
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = eng.Trigger(context.Background(), "mer-1", "")
		}(i)
	}
	wg.Wait()

	created := 0
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("Trigger[%d]: %v", i, errs[i])
		}
		if results[i].Created {
			created++
		}
	}
	if created != 1 {
		t.Errorf("Created=true count = %d, want exactly 1", created)
	}
	if launcher.spawnCount != 1 {
		t.Errorf("reviewer spawn count = %d, want 1", launcher.spawnCount)
	}
	if len(store.runs) != 1 {
		t.Errorf("recorded review runs = %d, want 1", len(store.runs))
	}
}

func TestTriggerFallsBackToExistingRunOnUniqueConflict(t *testing.T) {
	// The idempotency check passes (no run yet), but the insert loses to a
	// concurrent writer the unique index already accepted.
	store := &fakeStore{insertErr: domain.ErrDuplicateReviewRun}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if res.Created {
		t.Fatalf("expected Created=false on unique conflict: %+v", res)
	}
	if res.Run.TargetSHA != "sha1" || !strings.HasPrefix(res.Run.ID, "winner-") {
		t.Fatalf("expected the recorded winner run, got %+v", res.Run)
	}
	if launcher.spawnCount != 0 {
		t.Fatalf("reviewer should not launch after unique conflict: %+v", launcher)
	}
}

func TestTriggerDuplicateFallbackUsesRequestedHarness(t *testing.T) {
	store := &fakeStore{
		insertErr:              domain.ErrDuplicateReviewRun,
		insertErrWinnerAtFront: true,
		runs: []domain.ReviewRun{{
			ID: "other-harness-run", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1",
			Harness: domain.ReviewerClaudeCode,
			Status:  domain.ReviewRunComplete, Verdict: domain.VerdictApproved,
		}},
	}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", domain.ReviewerCodex)
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if res.Created {
		t.Fatalf("expected Created=false on unique conflict: %+v", res)
	}
	if res.Run.Harness != domain.ReviewerCodex || !strings.HasPrefix(res.Run.ID, "winner-") {
		t.Fatalf("expected duplicate fallback to return the codex winner, got %+v", res.Run)
	}
	if launcher.spawnCount != 0 {
		t.Fatalf("reviewer should not launch after unique conflict: %+v", launcher)
	}
}

func TestTriggerIsIdempotentForSameCommit(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerClaudeCode, ReviewerHandleID: "review-mer-1"},
		runs: []domain.ReviewRun{{
			ID: "run-1", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1",
			Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved,
		}},
	}
	launcher := &fakeLauncher{alive: true}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if res.Created || res.Run.ID != "run-1" || res.ReviewerHandleID != "review-mer-1" {
		t.Fatalf("expected reuse of existing run: %+v", res)
	}
	if launcher.spawned || launcher.notified {
		t.Fatalf("should not launch for an already-reviewed commit: %+v", launcher)
	}
	if len(store.runs) != 1 {
		t.Fatalf("should not insert another run: %+v", store.runs)
	}
}

// Choosing a different reviewer is a request for a second opinion on this exact
// commit. Before this, an approved commit was skipped before the harness was
// even consulted, so the picker looked broken precisely when a user would reach
// for it: pick another agent, nothing happens.
func TestTriggerRunsAnotherHarnessOnAnAlreadyApprovedCommit(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerClaudeCode, ReviewerHandleID: "review-mer-1"},
		runs: []domain.ReviewRun{{
			ID: "run-1", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1",
			Harness: domain.ReviewerClaudeCode,
			Status:  domain.ReviewRunComplete, Verdict: domain.VerdictApproved,
		}},
	}
	launcher := &fakeLauncher{alive: true}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", domain.ReviewerCodex)
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if !res.Created {
		t.Fatalf("a different harness should start a new pass: %+v", res)
	}
	if len(store.runs) != 2 {
		t.Fatalf("expected a second run for the other harness, got %d: %+v", len(store.runs), store.runs)
	}
	if res.Run.Harness != domain.ReviewerCodex {
		t.Fatalf("new run should record the requested harness, got %q", res.Run.Harness)
	}
}

// The project default must not re-review an approved commit on every trigger.
// Only an explicit pick counts as asking for a second opinion.
func TestTriggerWithoutOverrideStillSkipsAnApprovedCommit(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerClaudeCode, ReviewerHandleID: "review-mer-1"},
		runs: []domain.ReviewRun{{
			ID: "run-1", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1",
			Harness: domain.ReviewerClaudeCode,
			Status:  domain.ReviewRunComplete, Verdict: domain.VerdictApproved,
		}},
	}
	launcher := &fakeLauncher{alive: true}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if res.Created || len(store.runs) != 1 {
		t.Fatalf("no override should still reuse the existing pass: created=%v runs=%+v", res.Created, store.runs)
	}
}

// Re-picking the harness that already reviewed this commit is not a second
// opinion, so it must still reuse rather than run the same agent twice.
func TestTriggerWithSameHarnessOverrideStillReuses(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerClaudeCode, ReviewerHandleID: "review-mer-1"},
		runs: []domain.ReviewRun{{
			ID: "run-1", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1",
			Harness: domain.ReviewerClaudeCode,
			Status:  domain.ReviewRunComplete, Verdict: domain.VerdictApproved,
		}},
	}
	launcher := &fakeLauncher{alive: true}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", domain.ReviewerClaudeCode)
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if res.Created || len(store.runs) != 1 {
		t.Fatalf("same harness should reuse: created=%v runs=%+v", res.Created, store.runs)
	}
}

func TestTriggerReusesRunningRowWithNoVerdict(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerClaudeCode, ReviewerHandleID: "review-mer-1"},
		runs:   []domain.ReviewRun{{ID: "run-1", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1", Status: domain.ReviewRunRunning}},
	}
	launcher := &fakeLauncher{alive: false, handle: "review-mer-2"}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if res.Created || res.Run.ID != "run-1" {
		t.Fatalf("expected reuse of the running review for the same commit: %+v", res)
	}
	if launcher.spawned || launcher.notified {
		t.Fatalf("running same-commit review should not relaunch: %+v", launcher)
	}
	if got := store.runs[0]; got.Status != domain.ReviewRunRunning {
		t.Fatalf("running row should remain running, got %+v", got)
	}
}

func TestTriggerRetriesTerminalRowWithNoVerdict(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerClaudeCode, ReviewerHandleID: "review-mer-1"},
		runs: []domain.ReviewRun{{
			ID: "run-empty-verdict", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1",
			Status: domain.ReviewRunComplete, Verdict: domain.VerdictNone,
		}},
	}
	launcher := &fakeLauncher{handle: "review-mer-2"}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if !res.Created || res.Run.ID == "run-empty-verdict" {
		t.Fatalf("expected retry to create a new run, got %+v", res)
	}
	if len(store.runs) != 2 || !launcher.spawned {
		t.Fatalf("expected new launch/run after terminal empty-verdict row: launched=%v runs=%+v", launcher.spawned, store.runs)
	}
}

func TestTriggerReplacesReviewerOnNewCommit(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerClaudeCode, ReviewerHandleID: "review-mer-1"},
		runs:   []domain.ReviewRun{{ID: "run-0", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha0", Status: domain.ReviewRunComplete}},
	}
	launcher := &fakeLauncher{alive: true}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if !launcher.spawned || launcher.notified {
		t.Fatalf("expected fresh reviewer process: %+v", launcher)
	}
	if !launcher.preflighted {
		t.Fatal("expected fresh reviewer process to be preflighted")
	}
	if !res.Created || res.Run.TargetSHA != "sha1" || len(store.runs) != 2 {
		t.Fatalf("expected a new run for sha1: res=%+v runs=%+v", res, store.runs)
	}
}

func TestTriggerSupersedesOlderRunningRunOnNewCommit(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerClaudeCode, ReviewerHandleID: "review-mer-1"},
		runs:   []domain.ReviewRun{{ID: "run-old", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha0", Status: domain.ReviewRunRunning}},
	}
	launcher := &fakeLauncher{alive: true, handle: "review-mer-1"}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if !res.Created || res.Run.TargetSHA != "sha1" {
		t.Fatalf("expected new run for new commit, got %+v", res)
	}
	if old := store.runs[0]; old.ID != "run-old" || old.Status != domain.ReviewRunFailed {
		t.Fatalf("expected older running run to be failed, got %+v", old)
	}
	if !launcher.spawned || launcher.notified {
		t.Fatalf("expected reviewer process replaced for new commit: %+v", launcher)
	}
}

func TestTriggerSpawnsWhenReviewerDead(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerClaudeCode, ReviewerHandleID: "review-mer-1"},
		runs:   []domain.ReviewRun{{ID: "run-0", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha0", Status: domain.ReviewRunComplete}},
	}
	launcher := &fakeLauncher{alive: false, handle: "review-mer-1"}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	if _, err := eng.Trigger(context.Background(), "mer-1", ""); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if !launcher.spawned || launcher.notified {
		t.Fatalf("expected spawn when reviewer dead: %+v", launcher)
	}
}

// A live reviewer pane launched under a previous harness must be respawned under
// the newly-resolved harness, not reused via Notify: the pane's sandbox/
// permissions/env are fixed at Spawn, so reusing a codex pane to serve a
// claude-code review (or vice versa) would run under the wrong profile.
func TestTriggerRespawnsWhenReviewerHarnessChanged(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerCodex, ReviewerHandleID: "review-mer-1"},
		runs:   []domain.ReviewRun{{ID: "run-0", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha0", Status: domain.ReviewRunComplete}},
	}
	// Live pane exists (alive), but the worker/project now resolves to claude-code
	// while the pane was launched under codex.
	launcher := &fakeLauncher{alive: true, handle: "review-mer-1"}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if !launcher.spawned || launcher.notified {
		t.Fatalf("expected respawn under the new harness, not reuse via notify: %+v", launcher)
	}
	if launcher.gotSpec.Harness != domain.ReviewerClaudeCode {
		t.Fatalf("respawn harness = %q, want claude-code", launcher.gotSpec.Harness)
	}
	if res.Run.Harness != domain.ReviewerClaudeCode {
		t.Fatalf("run harness = %q, want claude-code", res.Run.Harness)
	}
}

// A harness switch observed on a trigger that creates no run (the current
// commit is already reviewed) must NOT advance the recorded harness. The live
// pane keeps running under the previous harness, so recording the new harness
// on this no-created path would make the next trigger read it back as
// prevHarness, match the resolved harness, and reuse (Notify) the stale pane.
func TestTriggerKeepsHarnessWhenNothingCreated(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerCodex, ReviewerHandleID: "review-mer-1"},
		runs: []domain.ReviewRun{{
			ID: "run-1", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1",
			Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved,
		}},
	}
	// Live codex pane; the worker/project now resolves to claude-code.
	launcher := &fakeLauncher{alive: true, handle: "review-mer-1"}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if res.Created || launcher.spawned || launcher.notified {
		t.Fatalf("already-reviewed commit: expected no launch, got res=%+v launcher=%+v", res, launcher)
	}
	if store.review.Harness != domain.ReviewerCodex {
		t.Fatalf("recorded harness = %q, want codex preserved (no respawn happened)", store.review.Harness)
	}
}

// End-to-end of the blocker: a harness switch seen on a no-run trigger must not
// defeat respawn on the next commit. Before the fix, the eager upsert recorded
// the new harness on the no-created trigger, so the following commit saw
// prevHarness == harness and reused (Notify) the stale old-harness pane.
func TestTriggerRespawnsOnNextCommitAfterHarnessSwitchWithNoRun(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerCodex, ReviewerHandleID: "review-mer-1"},
		runs: []domain.ReviewRun{{
			ID: "run-1", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1",
			Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved,
		}},
	}
	// Trigger 1: current commit sha1 is already reviewed → no run created, while
	// the worker now resolves to claude-code but the live pane is still codex.
	l1 := &fakeLauncher{alive: true, handle: "review-mer-1"}
	eng1 := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, l1)
	if _, err := eng1.Trigger(context.Background(), "mer-1", ""); err != nil {
		t.Fatalf("trigger 1: %v", err)
	}
	if l1.spawned || l1.notified {
		t.Fatalf("trigger 1 (nothing new) should not launch: %+v", l1)
	}

	// Trigger 2: a new commit arrives → a run is created. The reviewer must
	// respawn under claude-code, not Notify the stale codex pane.
	l2 := &fakeLauncher{alive: true, handle: "review-mer-1"}
	eng2 := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha2"), fakeProjects{}, l2)
	res, err := eng2.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("trigger 2: %v", err)
	}
	if !res.Created || !l2.spawned || l2.notified {
		t.Fatalf("trigger 2 must respawn under the new harness, not reuse the stale pane: res=%+v launcher=%+v", res, l2)
	}
	if l2.gotSpec.Harness != domain.ReviewerClaudeCode {
		t.Fatalf("respawn harness = %q, want claude-code", l2.gotSpec.Harness)
	}
}

func TestTriggerLaunchFailureRecordsFailedRun(t *testing.T) {
	store := &fakeStore{}
	launcher := &fakeLauncher{spawnErr: fmt.Errorf("claude: %w", ports.ErrAgentBinaryNotFound)}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	if _, err := eng.Trigger(context.Background(), "mer-1", ""); !errors.Is(err, ports.ErrAgentBinaryNotFound) {
		t.Fatalf("err = %v, want ports.ErrAgentBinaryNotFound", err)
	}
	if store.review == nil || len(store.runs) != 1 {
		t.Fatalf("expected persisted failed review/run: review=%+v runs=%+v", store.review, store.runs)
	}
	run := store.runs[0]
	if run.Status != domain.ReviewRunFailed || run.Verdict != domain.VerdictNone {
		t.Fatalf("run = %+v, want failed with no verdict", run)
	}
	if !strings.Contains(run.Body, "claude") || !strings.Contains(run.Body, ports.ErrAgentBinaryNotFound.Error()) {
		t.Fatalf("run body = %q, want launch cause", run.Body)
	}
}

func TestTriggerRetriesAfterFailedRunForSameCommit(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerClaudeCode, ReviewerHandleID: "review-mer-1"},
		runs:   []domain.ReviewRun{{ID: "run-failed", ReviewID: "rev-1", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1", Status: domain.ReviewRunFailed}},
	}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if !res.Created || res.Run.ID == "run-failed" {
		t.Fatalf("expected retry to create a new run, got %+v", res)
	}
	if len(store.runs) != 2 || !launcher.spawned {
		t.Fatalf("expected new launch/run after failed pass: launched=%v runs=%+v", launcher.spawned, store.runs)
	}
}

func TestTriggerRetriesAfterCancelledRunForSameCommit(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerClaudeCode, ReviewerHandleID: "review-mer-1"},
		runs:   []domain.ReviewRun{{ID: "run-cancelled", ReviewID: "rev-1", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1", Status: domain.ReviewRunCancelled}},
	}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if !res.Created || res.Run.ID == "run-cancelled" {
		t.Fatalf("expected retry to create a new run, got %+v", res)
	}
	if len(store.runs) != 2 || !launcher.spawned {
		t.Fatalf("expected new launch/run after cancelled pass: launched=%v runs=%+v", launcher.spawned, store.runs)
	}
}

func TestTriggerCreatesRunsForMultipleEligiblePRsWithOneReviewer(t *testing.T) {
	store := &fakeStore{}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	prs := fakePRs{prs: []domain.PullRequest{
		{URL: "https://github.com/o/r/pull/1", Number: 1, HeadSHA: "sha1"},
		{URL: "https://github.com/o/r/pull/2", Number: 2, HeadSHA: "sha2"},
	}}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prs, fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if !res.Created || len(res.CreatedRuns) != 2 || len(store.runs) != 2 {
		t.Fatalf("created batch = %+v runs=%+v", res, store.runs)
	}
	if res.CreatedRuns[0].BatchID == "" || res.CreatedRuns[0].BatchID != res.CreatedRuns[1].BatchID {
		t.Fatalf("created runs should share one batch id: %+v", res.CreatedRuns)
	}
	if launcher.spawnCount != 1 || len(launcher.handles) != 0 {
		t.Fatalf("expected one spawn and no extra notify, launcher=%+v", launcher)
	}
	if len(launcher.specs) != 1 {
		t.Fatalf("launch specs = %d, want 1: %+v", len(launcher.specs), launcher.specs)
	}
	spec := launcher.specs[0]
	if spec.BatchID != res.CreatedRuns[0].BatchID {
		t.Fatalf("launch spec batch id %q != created batch %q", spec.BatchID, res.CreatedRuns[0].BatchID)
	}
	if spec.ReviewIndex != 0 || len(spec.ReviewQueue) != 2 {
		t.Fatalf("spec queue context = index %d queue %+v", spec.ReviewIndex, spec.ReviewQueue)
	}
	if spec.ReviewQueue[0].PRURL != "https://github.com/o/r/pull/1" || spec.ReviewQueue[1].PRURL != "https://github.com/o/r/pull/2" {
		t.Fatalf("spec queue URLs = %+v", spec.ReviewQueue)
	}
	if store.review == nil || store.review.ReviewerHandleID != "review-mer-1" || store.review.PRURL != "" {
		t.Fatalf("review row = %+v, want shared handle and no behavioral pr_url", store.review)
	}
}

func TestTriggerAllowsTwoPRsWithSameHeadSHA(t *testing.T) {
	store := &fakeStore{}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	prs := fakePRs{prs: []domain.PullRequest{
		{URL: "https://github.com/o/r/pull/1", Number: 1, HeadSHA: "same"},
		{URL: "https://github.com/o/r/pull/2", Number: 2, HeadSHA: "same"},
	}}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prs, fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if len(res.CreatedRuns) != 2 {
		t.Fatalf("created runs = %d, want 2: %+v", len(res.CreatedRuns), res.CreatedRuns)
	}
	if store.runs[0].PRURL == store.runs[1].PRURL || store.runs[0].TargetSHA != store.runs[1].TargetSHA {
		t.Fatalf("runs should differ by PR only: %+v", store.runs)
	}
}

func TestTriggerSkipsApprovedAndRunningCurrentHead(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerClaudeCode, ReviewerHandleID: "review-mer-1"},
		runs: []domain.ReviewRun{
			{ID: "approved", ReviewID: "rev-1", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1", Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved, CreatedAt: time.Unix(1, 0)},
			{ID: "running", ReviewID: "rev-1", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/2", TargetSHA: "sha2", Status: domain.ReviewRunRunning, CreatedAt: time.Unix(2, 0)},
		},
	}
	launcher := &fakeLauncher{alive: true}
	prs := fakePRs{prs: []domain.PullRequest{
		{URL: "https://github.com/o/r/pull/1", Number: 1, HeadSHA: "sha1"},
		{URL: "https://github.com/o/r/pull/2", Number: 2, HeadSHA: "sha2"},
	}}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prs, fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if res.Created || len(res.CreatedRuns) != 0 || launcher.spawned || launcher.notified {
		t.Fatalf("expected no new work: res=%+v launcher=%+v", res, launcher)
	}
	if launcher.preflighted {
		t.Fatal("expected preflight not to run")
	}
	if len(res.Reviews) != 2 || res.Reviews[0].Status != ReviewStateUpToDate || res.Reviews[1].Status != ReviewStateRunning {
		t.Fatalf("review states = %+v", res.Reviews)
	}
}

func TestTriggerCreatesRunForChangesRequestedCurrentHead(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerClaudeCode, ReviewerHandleID: "review-mer-1"},
		runs: []domain.ReviewRun{{
			ID: "changes", ReviewID: "rev-1", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1",
			Status: domain.ReviewRunComplete, Verdict: domain.VerdictChangesRequested, CreatedAt: time.Unix(1, 0),
		}},
	}
	launcher := &fakeLauncher{alive: true, handle: "review-mer-1"}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if !res.Created || len(res.CreatedRuns) != 1 || !launcher.spawned || launcher.notified {
		t.Fatalf("expected rerun on changes_requested current head: res=%+v launcher=%+v", res, launcher)
	}
}

func TestTriggerUsesConfiguredReviewerHarness(t *testing.T) {
	store := &fakeStore{}
	projects := fakeProjects{cfg: domain.ProjectConfig{Reviewers: []domain.ReviewerConfig{{Harness: domain.ReviewerHarness("greptile")}}}}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), projects, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if res.Run.Harness != domain.ReviewerHarness("greptile") || launcher.gotSpec.Harness != domain.ReviewerHarness("greptile") {
		t.Fatalf("harness not used: run=%+v spec=%+v", res.Run, launcher.gotSpec)
	}
}

func TestTriggerUsesSessionReviewerHarnessBeforeProjectDefault(t *testing.T) {
	store := &fakeStore{}
	projects := fakeProjects{cfg: domain.ProjectConfig{Reviewers: []domain.ReviewerConfig{{Harness: domain.ReviewerCodex}}}}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	worker := liveWorker()
	worker.ReviewerHarness = domain.ReviewerOpenCode
	eng := newEngineForTest(store, fakeSessions{rec: worker, ok: true}, prAt("sha1"), projects, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if res.Run.Harness != domain.ReviewerOpenCode || launcher.gotSpec.Harness != domain.ReviewerOpenCode {
		t.Fatalf("session harness not used: run=%+v spec=%+v", res.Run, launcher.gotSpec)
	}
}

func TestTriggerRejectsBadWorkerState(t *testing.T) {
	t.Run("unknown worker", func(t *testing.T) {
		eng := newEngineForTest(&fakeStore{}, fakeSessions{ok: false}, prAt("sha1"), fakeProjects{}, &fakeLauncher{})
		if _, err := eng.Trigger(context.Background(), "mer-1", ""); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
	t.Run("no pr", func(t *testing.T) {
		eng := newEngineForTest(&fakeStore{}, fakeSessions{rec: liveWorker(), ok: true}, fakePRs{}, fakeProjects{}, &fakeLauncher{})
		if _, err := eng.Trigger(context.Background(), "mer-1", ""); !errors.Is(err, ErrInvalid) {
			t.Fatalf("err = %v, want ErrInvalid", err)
		}
	})
}

func TestListReturnsHandleAndRuns(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerClaudeCode, ReviewerHandleID: "review-mer-1"},
		runs:   []domain.ReviewRun{{ID: "run-1", SessionID: "mer-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1"}},
	}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, &fakeLauncher{})
	got, err := eng.List(context.Background(), "mer-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got.ReviewerHandleID != "review-mer-1" || len(got.Runs) != 1 {
		t.Fatalf("list = %+v", got)
	}
}

func TestTriggerPreflightFailureRecordsFailedRun(t *testing.T) {
	store := &fakeStore{}
	launcher := &fakeLauncher{preflightErr: fmt.Errorf("codex: %w", ports.ErrAgentBinaryNotFound)}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	_, err := eng.Trigger(context.Background(), "mer-1", "")
	if err == nil {
		t.Fatal("expected error from preflight, got nil")
	}
	if !errors.Is(err, ports.ErrAgentBinaryNotFound) {
		t.Fatalf("err = %v, want wrapped ErrAgentBinaryNotFound", err)
	}
	if !launcher.preflighted {
		t.Fatal("expected Preflight to be called")
	}
	if launcher.spawned {
		t.Fatal("expected no spawn attempt when preflight fails")
	}
	if len(store.runs) != 1 {
		t.Fatalf("expected 1 review run (failed), got %d", len(store.runs))
	}
	run := store.runs[0]
	if run.Status != domain.ReviewRunFailed || run.Verdict != domain.VerdictNone {
		t.Fatalf("run = %+v, want failed with no verdict", run)
	}
	if !strings.Contains(run.Body, "codex") || !strings.Contains(run.Body, ports.ErrAgentBinaryNotFound.Error()) {
		t.Fatalf("run body = %q, want preflight cause", run.Body)
	}
}

func TestTriggerProceedsNormallyAfterSuccessfulPreflight(t *testing.T) {
	store := &fakeStore{}
	launcher := &fakeLauncher{handle: "review-mer-1"}
	eng := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.Trigger(context.Background(), "mer-1", "")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if !launcher.preflighted {
		t.Fatal("expected Preflight to be called")
	}
	if !res.Created || res.ReviewerHandleID != "review-mer-1" {
		t.Fatalf("result = %+v", res)
	}
	if !launcher.spawned {
		t.Fatal("expected spawn after successful preflight")
	}
	if len(store.runs) != 1 {
		t.Fatalf("expected 1 review run, got %d", len(store.runs))
	}
}
