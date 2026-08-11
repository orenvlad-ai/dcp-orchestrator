// Package review holds the core code-review logic: triggering a reviewer over a
// worker's worktree, recording review runs, and accepting submitted results.
//
// It is independent of any transport. The daemon's HTTP service
// (internal/service/review) is a thin boundary over this engine today, and the
// same engine can back an in-process CLI trigger later without going through the
// API. Transport-specific concerns (DTOs, error→status mapping) stay in the
// service/controller layers; the orchestration and run-id generation live here.
package review

import (
	stdctx "context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	aoprocess "github.com/aoagents/agent-orchestrator/backend/internal/process"
)

// ErrInvalid and ErrNotFound let the transport layer map failures to 422/404.
var (
	ErrInvalid  = errors.New("review: invalid input")
	ErrNotFound = errors.New("review: not found")
)

// Store is the persistence surface the engine needs. *sqlite.Store satisfies it
// in production; tests use a fake.
type Store interface {
	UpsertReview(ctx stdctx.Context, r domain.Review) error
	GetReviewBySession(ctx stdctx.Context, id domain.SessionID) (domain.Review, bool, error)
	InsertReviewRun(ctx stdctx.Context, r domain.ReviewRun) error
	UpdateReviewRunResult(ctx stdctx.Context, id string, status domain.ReviewRunStatus, verdict domain.ReviewVerdict, body, githubReviewID string) (bool, error)
	SupersedeStaleRunningReviewRuns(ctx stdctx.Context, sessionID domain.SessionID, prURL, targetSHA, body string) (int64, error)
	CancelRunningReviewRunsBySession(ctx stdctx.Context, sessionID domain.SessionID, body string) (int64, error)
	GetReviewRun(ctx stdctx.Context, id string) (domain.ReviewRun, bool, error)
	GetReviewRunBySessionPRAndSHA(ctx stdctx.Context, id domain.SessionID, prURL, targetSHA string) (domain.ReviewRun, bool, error)
	GetReviewRunBySessionPRSHAAndHarness(ctx stdctx.Context, id domain.SessionID, prURL, targetSHA string, harness domain.ReviewerHarness) (domain.ReviewRun, bool, error)
	ListReviewRunsBySession(ctx stdctx.Context, id domain.SessionID) ([]domain.ReviewRun, error)
	ListRunningReviewRunsBySession(ctx stdctx.Context, id domain.SessionID) ([]domain.ReviewRun, error)
}

// Sessions resolves the worker session under review.
type Sessions interface {
	GetSession(ctx stdctx.Context, id domain.SessionID) (domain.SessionRecord, bool, error)
}

// PRs resolves the PR a worker owns.
type PRs interface {
	ListPRsBySession(ctx stdctx.Context, id domain.SessionID) ([]domain.PullRequest, error)
}

// Projects resolves the per-project reviewer config.
type Projects interface {
	GetProject(ctx stdctx.Context, id string) (domain.ProjectRecord, bool, error)
}

type sessionLister interface {
	ListAllSessions(ctx stdctx.Context) ([]domain.SessionRecord, error)
}

type reviewerProcessInspector interface {
	ReviewerProcessAlive(ctx stdctx.Context, handleID, runID string) (bool, error)
}

// WorkspacePreparer restores and verifies the preserved worker worktree
// used by a reviewer. It is intentionally narrower than session restore: the
// worker process stays terminated and no model is launched by this operation.
type WorkspacePreparer interface {
	PrepareReviewWorkspace(ctx stdctx.Context, id domain.SessionID, targetSHA string) (domain.SessionRecord, error)
}

type preservedWorkspacePreparer struct {
	sessions Sessions
	projects Projects
	restore  func(stdctx.Context, ports.WorkspaceConfig) (ports.WorkspaceInfo, error)
}

// NewWorkspacePreparer builds the model-free preserved-worktree boundary used
// by automatic reviewer recovery. The caller supplies only the existing stock
// workspace Restore method; this helper cannot spawn or resume a worker.
func NewWorkspacePreparer(sessions Sessions, projects Projects, restore func(stdctx.Context, ports.WorkspaceConfig) (ports.WorkspaceInfo, error)) WorkspacePreparer {
	return &preservedWorkspacePreparer{sessions: sessions, projects: projects, restore: restore}
}

func (p *preservedWorkspacePreparer) PrepareReviewWorkspace(ctx stdctx.Context, id domain.SessionID, targetSHA string) (domain.SessionRecord, error) {
	if id == "" || strings.TrimSpace(targetSHA) == "" {
		return domain.SessionRecord{}, errors.New("review workspace requires session id and exact target sha")
	}
	rec, ok, err := p.sessions.GetSession(ctx, id)
	if err != nil {
		return domain.SessionRecord{}, fmt.Errorf("load review session %s: %w", id, err)
	}
	if !ok {
		return domain.SessionRecord{}, fmt.Errorf("%w: review session %s", ErrNotFound, id)
	}
	if !rec.IsTerminated || rec.Activity.State != domain.ActivityExited || rec.Metadata.RuntimeLaunchID != "" {
		return domain.SessionRecord{}, fmt.Errorf("review session %s is not a preserved terminated worker", id)
	}
	if rec.Metadata.WorkspacePath == "" || rec.Metadata.Branch == "" {
		return domain.SessionRecord{}, fmt.Errorf("review session %s has incomplete workspace metadata", id)
	}
	project, ok, err := p.projects.GetProject(ctx, string(rec.ProjectID))
	if err != nil {
		return domain.SessionRecord{}, fmt.Errorf("load review project: %w", err)
	}
	if !ok {
		return domain.SessionRecord{}, fmt.Errorf("review project %s is not registered", rec.ProjectID)
	}
	if project.Kind.WithDefault() != domain.ProjectKindSingleRepo {
		return domain.SessionRecord{}, errors.New("review workspace recovery requires a single-repo project")
	}
	if p.restore == nil {
		return domain.SessionRecord{}, errors.New("review workspace restore is unavailable")
	}
	ws, err := p.restore(ctx, ports.WorkspaceConfig{
		ProjectID: rec.ProjectID,
		SessionID: rec.ID,
		Kind:      rec.Kind,
		Branch:    rec.Metadata.Branch,
		Path:      rec.Metadata.WorkspacePath,
	})
	if err != nil {
		return domain.SessionRecord{}, fmt.Errorf("restore preserved worktree: %w", err)
	}
	if filepath.Clean(ws.Path) != filepath.Clean(rec.Metadata.WorkspacePath) || ws.Branch != rec.Metadata.Branch {
		return domain.SessionRecord{}, fmt.Errorf("restored worktree identity mismatch: path %q branch %q", ws.Path, ws.Branch)
	}
	headCmd := aoprocess.CommandContext(ctx, "git", "-C", ws.Path, "rev-parse", "HEAD")
	headOut, err := headCmd.Output()
	if err != nil {
		return domain.SessionRecord{}, fmt.Errorf("verify restored worktree head: %w", err)
	}
	head := strings.TrimSpace(string(headOut))
	if head != targetSHA {
		return domain.SessionRecord{}, fmt.Errorf("restored worktree head %s does not match exact review target %s", head, targetSHA)
	}
	statusCmd := aoprocess.CommandContext(ctx, "git", "-C", ws.Path, "status", "--porcelain")
	statusOut, err := statusCmd.Output()
	if err != nil {
		return domain.SessionRecord{}, fmt.Errorf("verify restored worktree cleanliness: %w", err)
	}
	if strings.TrimSpace(string(statusOut)) != "" {
		return domain.SessionRecord{}, errors.New("restored worktree is not clean")
	}
	rec.Metadata.WorkspacePath = ws.Path
	rec.Metadata.Branch = ws.Branch
	rec.Metadata.WorkspaceRepoPath = ws.RepoPath
	return rec, nil
}

type triggerMode int

const (
	triggerManual triggerMode = iota
	triggerAutomatic
	triggerRecovery
	triggerPreserved
)

const missingWorkspaceFailureMarker = "session working directory mismatch:"

// Deps wires the engine.
type Deps struct {
	Store    Store
	Sessions Sessions
	PRs      PRs
	Projects Projects
	Launcher Launcher
	// WorkspacePreparer is used only for a preserved terminated worker whose
	// reviewer worktree must be restored model-free before a recovery launch.
	WorkspacePreparer WorkspacePreparer

	// Clock and NewID are injectable for deterministic tests.
	Clock func() time.Time
	NewID func() string
}

// Engine is the core code-review engine.
type Engine struct {
	store             Store
	sessions          Sessions
	prs               PRs
	projects          Projects
	launcher          Launcher
	workspacePreparer WorkspacePreparer
	clock             func() time.Time
	newID             func() string

	// triggerMu guards triggerLocks; triggerLocks holds one mutex per worker
	// session so concurrent Trigger calls for the same worker serialise (see
	// lockWorker). Distinct workers never contend.
	triggerMu    sync.Mutex
	triggerLocks map[domain.SessionID]*sync.Mutex
}

// New wires an Engine from its dependencies, defaulting the clock and id source.
func New(d Deps) *Engine {
	clock := d.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	newID := d.NewID
	if newID == nil {
		newID = uuid.NewString
	}
	return &Engine{
		store:             d.Store,
		sessions:          d.Sessions,
		prs:               d.PRs,
		projects:          d.Projects,
		launcher:          d.Launcher,
		workspacePreparer: d.WorkspacePreparer,
		clock:             clock,
		newID:             newID,
		triggerLocks:      make(map[domain.SessionID]*sync.Mutex),
	}
}

// lockWorker serialises Trigger calls for a single worker session and returns
// the unlock func. Without it, two concurrent triggers for the same worker can
// both pass the per-commit idempotency check and each spawn a reviewer against
// the same deterministic handle, leaving two running runs for one commit (#242).
//
// The per-worker mutex is created on first use and kept for the lifetime of the
// engine; the entry is a single pointer, so the unbounded-by-session-count map
// is a negligible, bounded-in-practice cost.
func (e *Engine) lockWorker(id domain.SessionID) func() {
	e.triggerMu.Lock()
	mu, ok := e.triggerLocks[id]
	if !ok {
		mu = &sync.Mutex{}
		e.triggerLocks[id] = mu
	}
	e.triggerMu.Unlock()
	mu.Lock()
	return mu.Unlock
}

// TriggerResult is the outcome of a trigger: the (new or existing) run, the live
// reviewer pane's handle so the UI can attach its terminal, and whether a new
// pass was started (false when an existing run for the same commit was reused).
type TriggerResult struct {
	Run              domain.ReviewRun
	ReviewerHandleID string
	Created          bool
	Reviews          []PRReviewState
	CreatedRuns      []domain.ReviewRun
}

// SessionReviews is a worker's review state: the live reviewer handle plus its
// recorded passes, newest first.
type SessionReviews struct {
	ReviewerHandleID string
	Runs             []domain.ReviewRun
	Reviews          []PRReviewState
}

// CancelResult is the review state after a reviewer pane cancellation.
type CancelResult struct {
	ReviewerHandleID string
	Reviews          []PRReviewState
	CancelledRuns    []domain.ReviewRun
}

// Trigger starts reviews for every PR on the worker session that needs review.
// It reuses running/up-to-date runs, retries failed/current changes-requested
// heads, and uses one reviewer pane for every new run in the batch.
//
// An empty override keeps the project's configured reviewer. A known one runs
// this pass under it without editing project config, so picking a reviewer for
// one session cannot change what any other session in the project runs. The
// harness-change path below already handles the swap by respawning the pane.
func (e *Engine) Trigger(ctx stdctx.Context, workerID domain.SessionID, override domain.ReviewerHarness) (TriggerResult, error) {
	return e.trigger(ctx, workerID, override, triggerManual, nil)
}

// AutoTrigger starts a review for an idle, non-terminated worker and an eligible
// exact PR head that has never had an AO review attempt. It also permits one
// narrow continuation for a preserved terminated worker when the latest run
// proves that the reviewer failed only because its worktree was missing. The
// replacement head is still single-flight; any outcome consumes the
// continuation and cannot form an automatic retry loop.
func (e *Engine) AutoTrigger(ctx stdctx.Context, workerID domain.SessionID) (TriggerResult, error) {
	return e.trigger(ctx, workerID, "", triggerAutomatic, nil)
}

func (e *Engine) trigger(ctx stdctx.Context, workerID domain.SessionID, override domain.ReviewerHarness, mode triggerMode, recovery map[string]struct{}) (TriggerResult, error) {
	if workerID == "" {
		return TriggerResult{}, fmt.Errorf("%w: worker session id is required", ErrInvalid)
	}
	if override != "" && !override.IsKnown() {
		return TriggerResult{}, fmt.Errorf("%w: unknown reviewer harness %q", ErrInvalid, override)
	}

	// Serialise concurrent triggers for this worker so the idempotency check
	// below (and the reviewer spawn that follows it) can't be raced into a
	// double-spawn. Held across the spawn deliberately: the loser then re-reads
	// the freshly-recorded run and short-circuits to Created:false.
	unlock := e.lockWorker(workerID)
	defer unlock()
	return e.triggerLocked(ctx, workerID, override, mode, recovery)
}

func (e *Engine) triggerLocked(ctx stdctx.Context, workerID domain.SessionID, override domain.ReviewerHarness, mode triggerMode, recovery map[string]struct{}) (TriggerResult, error) {

	worker, ok, err := e.sessions.GetSession(ctx, workerID)
	if err != nil {
		return TriggerResult{}, err
	}
	if !ok {
		return TriggerResult{}, fmt.Errorf("%w: worker session %q", ErrNotFound, workerID)
	}
	reviewLab := worker.ProjectID == "dcp-review-lab"
	if reviewLab {
		if mode == triggerManual {
			return TriggerResult{}, fmt.Errorf("%w: DCP review-lab reviews are automatic only", ErrInvalid)
		}
		if !eligibleDCPReviewLabWorker(workerID) {
			return TriggerResult{}, nil
		}
	}
	if mode == triggerManual && worker.IsTerminated {
		return TriggerResult{}, fmt.Errorf("%w: worker session %q is terminated", ErrInvalid, workerID)
	}
	if mode == triggerAutomatic {
		preservedTerminated := worker.Activity.State == domain.ActivityExited && worker.IsTerminated
		if preservedTerminated && worker.Metadata.RuntimeLaunchID == "" {
			mode = triggerPreserved
		} else if worker.IsTerminated || worker.Activity.State != domain.ActivityIdle || worker.Metadata.RuntimeLaunchID != "" {
			return TriggerResult{}, nil
		}
	}
	if mode == triggerRecovery {
		safelyStopped := worker.Activity.State == domain.ActivityIdle && !worker.IsTerminated
		preservedTerminated := worker.Activity.State == domain.ActivityExited && worker.IsTerminated
		if (!safelyStopped && !preservedTerminated) || worker.Metadata.RuntimeLaunchID != "" {
			return TriggerResult{}, nil
		}
	}
	if worker.Metadata.WorkspacePath == "" {
		return TriggerResult{}, fmt.Errorf("%w: worker session %q has no workspace to review", ErrInvalid, workerID)
	}

	prs, err := e.prs.ListPRsBySession(ctx, workerID)
	if err != nil {
		return TriggerResult{}, err
	}
	if len(prs) == 0 {
		if mode != triggerManual {
			return TriggerResult{}, nil
		}
		return TriggerResult{}, fmt.Errorf("%w: worker %q has no PR to review", ErrInvalid, workerID)
	}
	runs, err := e.store.ListReviewRunsBySession(ctx, workerID)
	if err != nil {
		return TriggerResult{}, err
	}
	if reviewLab && !dcpReviewLabRunBudget(workerID, mode, runs) {
		return TriggerResult{}, nil
	}
	reviews := Plan(prs, runs)

	reviewRow, hasReview, err := e.store.GetReviewBySession(ctx, workerID)
	if err != nil {
		return TriggerResult{}, err
	}
	if mode == triggerPreserved && !PreservedReviewContinuationEligible(hasReview, runs) {
		return TriggerResult{}, nil
	}

	harness, err := e.reviewerHarness(ctx, worker)
	if err != nil {
		return TriggerResult{}, err
	}
	if override != "" {
		harness = override
	}

	// Preserve the last harness until a newly-created pass actually launches.
	prevHarness := reviewRow.Harness

	now := e.clock()
	// This eager upsert only needs the review row to exist so the runs below can
	// reference it; it must NOT advance the recorded harness past what the live
	// pane actually ran. Preserve an existing row's harness; only the post-spawn
	// upsert records the harness launched for a real pass.
	eagerHarness := harness
	if hasReview {
		eagerHarness = prevHarness
	}
	reviewRow, err = e.upsertReview(ctx, worker, eagerHarness, reviewRow.ReviewerHandleID, now)
	if err != nil {
		return TriggerResult{}, err
	}

	var created []domain.ReviewRun
	batchID := ""
	for _, reviewState := range reviews {
		// A PR that is already up to date has nothing due — unless the caller asked
		// for a different reviewer than the one that produced that verdict. Picking
		// another agent is precisely a request for a second opinion on this commit,
		// so refusing it makes the reviewer choice inert exactly when it is most
		// useful. Ineligible PRs stay excluded: nothing can review those.
		eligible := reviewState.Status == ReviewStateNeedsReview || reviewState.Status == ReviewStateChangesRequested
		switch mode {
		case triggerAutomatic, triggerPreserved:
			eligible = reviewState.Status == ReviewStateNeedsReview && reviewState.LatestRun == nil
		case triggerRecovery:
			_, explicitlyRecovered := recovery[reviewKey(reviewState.PRURL, reviewState.TargetSHA)]
			eligible = reviewState.Status == ReviewStateNeedsReview && (reviewState.LatestRun == nil || explicitlyRecovered)
		}
		if !eligible && (mode != triggerManual || !secondOpinionWanted(reviewState, override, harness)) {
			continue
		}
		if _, err := e.store.SupersedeStaleRunningReviewRuns(ctx, workerID, reviewState.PRURL, reviewState.TargetSHA, "superseded by a review trigger for a newer commit"); err != nil {
			return TriggerResult{}, err
		}
		if batchID == "" {
			batchID = e.newID()
		}
		run := domain.ReviewRun{
			ID:        e.newID(),
			ReviewID:  reviewRow.ID,
			SessionID: workerID,
			BatchID:   batchID,
			Harness:   harness,
			PRURL:     reviewState.PRURL,
			TargetSHA: reviewState.TargetSHA,
			Status:    domain.ReviewRunRunning,
			Verdict:   domain.VerdictNone,
			CreatedAt: now,
		}
		if err := e.store.InsertReviewRun(ctx, run); err != nil {
			if errors.Is(err, domain.ErrDuplicateReviewRun) {
				if existing, ok, getErr := e.store.GetReviewRunBySessionPRSHAAndHarness(ctx, workerID, reviewState.PRURL, reviewState.TargetSHA, harness); getErr != nil {
					return TriggerResult{}, getErr
				} else if ok {
					reviews = replaceReviewLatestRun(reviews, reviewState.PRURL, reviewState.TargetSHA, existing)
					continue
				}
			}
			return TriggerResult{}, err
		}
		created = append(created, run)
		reviews = replaceReviewLatestRun(reviews, reviewState.PRURL, reviewState.TargetSHA, run)
	}
	if len(created) == 0 {
		return TriggerResult{Run: firstReusableRun(reviews), ReviewerHandleID: reviewRow.ReviewerHandleID, Created: false, Reviews: reviews}, nil
	}

	failRuns := func(start int, err error) error {
		for _, run := range created[start:] {
			if _, updateErr := e.store.UpdateReviewRunResult(ctx, run.ID, domain.ReviewRunFailed, domain.VerdictNone, err.Error(), ""); updateErr != nil {
				return updateErr
			}
		}
		return err
	}

	queue := reviewQueue(created)
	if (mode == triggerRecovery && worker.IsTerminated) || mode == triggerPreserved {
		if len(created) != 1 {
			return TriggerResult{}, failRuns(0, fmt.Errorf("prepare reviewer workspace: preserved review requires exactly one exact-head run, got %d", len(created)))
		}
		if e.workspacePreparer == nil {
			return TriggerResult{}, failRuns(0, errors.New("prepare reviewer workspace: preparer is unavailable"))
		}
		worker, err = e.workspacePreparer.PrepareReviewWorkspace(ctx, workerID, created[0].TargetSHA)
		if err != nil {
			return TriggerResult{}, failRuns(0, fmt.Errorf("prepare reviewer workspace: %w", err))
		}
	}
	// Each pass gets a fresh reviewer process on the same stable terminal
	// handle. Runtime panes intentionally preserve a shell after their command
	// exits, so pane liveness cannot prove the reviewer agent is still present;
	// sending a task to such a pane executes it as shell input. Spawn replaces
	// the old pane atomically and also applies the selected harness's current
	// permissions and environment.
	if err := e.launcher.Preflight(ctx, harness, worker.Metadata.WorkspacePath); err != nil {
		return TriggerResult{}, failRuns(0, fmt.Errorf("reviewer preflight: %w", err))
	}
	handleID, err := e.launcher.Spawn(ctx, reviewLaunchSpec(worker, harness, created[0], queue, 0))
	if err != nil {
		return TriggerResult{}, failRuns(0, fmt.Errorf("launch reviewer: %w", err))
	}
	reviewRow, err = e.upsertReview(ctx, worker, harness, handleID, now)
	if err != nil {
		return TriggerResult{}, err
	}
	for i := range created {
		created[i].ReviewID = reviewRow.ID
	}
	return TriggerResult{Run: created[0], ReviewerHandleID: handleID, Created: true, Reviews: reviews, CreatedRuns: created}, nil
}

func eligibleDCPReviewLabWorker(id domain.SessionID) bool {
	return id == "dcp-review-lab-7" || id == "dcp-review-lab-8" || id == "dcp-review-lab-9"
}

func dcpReviewLabRunBudget(id domain.SessionID, mode triggerMode, runs []domain.ReviewRun) bool {
	// I13 never replaces a reviewer on the same head after restart: ambiguity is
	// persisted as failure and consumes that head. A branch-refresh continuation
	// may create one fresh reviewer only because it produces a distinct head.
	if mode == triggerRecovery || mode == triggerPreserved {
		return false
	}
	limit := 1
	if id == "dcp-review-lab-8" || id == "dcp-review-lab-9" {
		limit = 2
	}
	return len(runs) < limit
}

func reviewKey(prURL, targetSHA string) string { return prURL + "\x00" + targetSHA }

// PreservedReviewContinuationEligible identifies the one durable failure that
// permits a terminated worker to stay visible to the stock SCM observer for a
// replacement exact head. Requiring exactly one matching failure makes the
// contour consumable: if the bounded continuation reaches the same failure (or
// any other outcome), no later head can form an automatic retry loop.
func PreservedReviewContinuationEligible(hasReview bool, runs []domain.ReviewRun) bool {
	if !hasReview || len(runs) == 0 {
		return false
	}
	latest := runs[0]
	missingWorkspaceFailures := 0
	if missingWorkspaceReviewFailure(latest) {
		missingWorkspaceFailures++
	}
	for i := 1; i < len(runs); i++ {
		if missingWorkspaceReviewFailure(runs[i]) {
			missingWorkspaceFailures++
		}
		if !runs[i].CreatedAt.Before(latest.CreatedAt) {
			latest = runs[i]
		}
	}
	return missingWorkspaceFailures == 1 && missingWorkspaceReviewFailure(latest)
}

func missingWorkspaceReviewFailure(run domain.ReviewRun) bool {
	return run.Status == domain.ReviewRunFailed &&
		run.Verdict == domain.VerdictNone &&
		strings.Contains(run.Body, missingWorkspaceFailureMarker)
}

// ReconcileStartup folds persisted running rows against the exact supervised
// reviewer process before any automatic trigger. It never calls a model when
// the process is active or its state is ambiguous. A proven stale run is failed
// durably, then its current exact head receives at most one recovery launch.
func (e *Engine) ReconcileStartup(ctx stdctx.Context) error {
	lister, ok := e.sessions.(sessionLister)
	if !ok {
		return fmt.Errorf("review startup reconciliation requires session listing")
	}
	sessions, err := lister.ListAllSessions(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, session := range sessions {
		if err := e.reconcileSession(ctx, session.ID); err != nil {
			errs = append(errs, fmt.Errorf("session %s: %w", session.ID, err))
		}
	}
	return errors.Join(errs...)
}

func (e *Engine) reconcileSession(ctx stdctx.Context, workerID domain.SessionID) error {
	unlock := e.lockWorker(workerID)
	defer unlock()
	running, err := e.store.ListRunningReviewRunsBySession(ctx, workerID)
	if err != nil {
		return err
	}
	if len(running) == 0 {
		_, err := e.triggerLocked(ctx, workerID, "", triggerAutomatic, nil)
		return err
	}
	reviewRow, ok, err := e.store.GetReviewBySession(ctx, workerID)
	if err != nil {
		return err
	}
	inspector, inspectable := e.launcher.(reviewerProcessInspector)
	ambiguous := !ok || reviewRow.ReviewerHandleID == "" || !inspectable
	alive := false
	if !ambiguous {
		for _, run := range running {
			runAlive, inspectErr := inspector.ReviewerProcessAlive(ctx, reviewRow.ReviewerHandleID, run.ID)
			if inspectErr != nil {
				ambiguous = true
				break
			}
			if runAlive {
				alive = true
				break
			}
		}
	}
	if alive {
		return nil
	}
	reason := "reviewer process state could not be verified after restart; no automatic retry was started"
	if !ambiguous {
		reason = "reviewer was not running after restart; the stale review was reconciled before one automatic recovery attempt"
	}
	recovered := make(map[string]struct{}, len(running))
	for _, run := range running {
		updated, updateErr := e.store.UpdateReviewRunResult(ctx, run.ID, domain.ReviewRunFailed, domain.VerdictNone, reason, "")
		if updateErr != nil {
			return updateErr
		}
		if updated {
			recovered[reviewKey(run.PRURL, run.TargetSHA)] = struct{}{}
		}
	}
	if ambiguous || len(recovered) == 0 {
		return nil
	}
	_, err = e.triggerLocked(ctx, workerID, "", triggerRecovery, recovered)
	return err
}

func reviewLaunchSpec(worker domain.SessionRecord, harness domain.ReviewerHarness, run domain.ReviewRun, queue []ports.ReviewTask, index int) LaunchSpec {
	return LaunchSpec{
		RunID:         run.ID,
		BatchID:       run.BatchID,
		WorkerID:      worker.ID,
		Harness:       harness,
		WorkspacePath: worker.Metadata.WorkspacePath,
		PRURL:         run.PRURL,
		TargetSHA:     run.TargetSHA,
		ReviewQueue:   queue,
		ReviewIndex:   index,
	}
}

func reviewQueue(runs []domain.ReviewRun) []ports.ReviewTask {
	queue := make([]ports.ReviewTask, 0, len(runs))
	for _, run := range runs {
		queue = append(queue, ports.ReviewTask{
			RunID:     run.ID,
			PRURL:     run.PRURL,
			TargetSHA: run.TargetSHA,
		})
	}
	return queue
}

// secondOpinionWanted reports whether an explicitly requested harness differs
// from the one that already reviewed this commit, which makes an otherwise
// up-to-date PR worth running again. Only an explicit override counts: falling
// back to the project default must not re-review a commit on every trigger.
func secondOpinionWanted(state PRReviewState, override, harness domain.ReviewerHarness) bool {
	if override == "" || state.Status == ReviewStateIneligible || state.Status == ReviewStateRunning {
		return false
	}
	if state.LatestRun == nil {
		return false
	}
	return state.LatestRun.Harness != harness
}

func replaceReviewLatestRun(reviews []PRReviewState, prURL, targetSHA string, run domain.ReviewRun) []PRReviewState {
	for i := range reviews {
		if reviews[i].PRURL == prURL && reviews[i].TargetSHA == targetSHA {
			reviews[i].LatestRun = &run
			if run.Status == domain.ReviewRunRunning {
				reviews[i].Status = ReviewStateRunning
			}
			break
		}
	}
	return reviews
}

func firstReusableRun(reviews []PRReviewState) domain.ReviewRun {
	// Legacy compatibility only: in the multi-PR model the authoritative state
	// is Reviews. When no run is created, this field is just a best-effort
	// non-empty run for older clients.
	for _, review := range reviews {
		if review.LatestRun != nil {
			return *review.LatestRun
		}
	}
	return domain.ReviewRun{}
}

// List returns a worker's review state: the live reviewer handle and its passes.
func (e *Engine) List(ctx stdctx.Context, workerID domain.SessionID) (SessionReviews, error) {
	if workerID == "" {
		return SessionReviews{}, fmt.Errorf("%w: worker session id is required", ErrInvalid)
	}
	runs, err := e.store.ListReviewRunsBySession(ctx, workerID)
	if err != nil {
		return SessionReviews{}, err
	}
	var handle string
	if review, ok, err := e.store.GetReviewBySession(ctx, workerID); err != nil {
		return SessionReviews{}, err
	} else if ok {
		handle = review.ReviewerHandleID
	}
	prs, err := e.prs.ListPRsBySession(ctx, workerID)
	if err != nil {
		return SessionReviews{}, err
	}
	return SessionReviews{ReviewerHandleID: handle, Runs: runs, Reviews: Plan(prs, runs)}, nil
}

// Cancel interrupts the live reviewer pane for a worker and marks running
// review runs as cancelled so they no longer block a fresh trigger.
func (e *Engine) Cancel(ctx stdctx.Context, workerID domain.SessionID) (CancelResult, error) {
	if workerID == "" {
		return CancelResult{}, fmt.Errorf("%w: worker session id is required", ErrInvalid)
	}
	review, ok, err := e.store.GetReviewBySession(ctx, workerID)
	if err != nil {
		return CancelResult{}, err
	}
	if !ok || review.ReviewerHandleID == "" {
		return CancelResult{}, fmt.Errorf("%w: reviewer for worker session %q", ErrNotFound, workerID)
	}
	running, err := e.store.ListRunningReviewRunsBySession(ctx, workerID)
	if err != nil {
		return CancelResult{}, err
	}
	if err := e.launcher.Cancel(ctx, review.ReviewerHandleID, review.Harness); err != nil {
		alive, aliveErr := e.launcher.Alive(ctx, review.ReviewerHandleID)
		if aliveErr != nil {
			return CancelResult{}, err
		}
		if alive {
			return CancelResult{}, err
		}
	}
	if _, err := e.store.CancelRunningReviewRunsBySession(ctx, workerID, "cancelled by user"); err != nil {
		return CancelResult{}, err
	}
	cancelled := make([]domain.ReviewRun, 0, len(running))
	for _, run := range running {
		run.Status = domain.ReviewRunCancelled
		run.Verdict = domain.VerdictNone
		run.Body = "cancelled by user"
		run.GithubReviewID = ""
		cancelled = append(cancelled, run)
	}
	prs, err := e.prs.ListPRsBySession(ctx, workerID)
	if err != nil {
		return CancelResult{}, err
	}
	runs, err := e.store.ListReviewRunsBySession(ctx, workerID)
	if err != nil {
		return CancelResult{}, err
	}
	return CancelResult{ReviewerHandleID: review.ReviewerHandleID, Reviews: Plan(prs, runs), CancelledRuns: cancelled}, nil
}

// reviewerHarness resolves which harness reviews the worker's PR: a persisted
// session preference wins, then project configuration, then the worker's own
// harness when supported, otherwise claude-code.
func (e *Engine) reviewerHarness(ctx stdctx.Context, worker domain.SessionRecord) (domain.ReviewerHarness, error) {
	if worker.ReviewerHarness != "" {
		return worker.ReviewerHarness, nil
	}
	var cfg domain.ProjectConfig
	if e.projects != nil {
		if proj, ok, err := e.projects.GetProject(ctx, string(worker.ProjectID)); err != nil {
			return "", err
		} else if ok {
			cfg = proj.Config
		}
	}
	return cfg.ResolveReviewerHarness(worker.Harness), nil
}

func (e *Engine) upsertReview(ctx stdctx.Context, worker domain.SessionRecord, harness domain.ReviewerHarness, handleID string, now time.Time) (domain.Review, error) {
	existing, ok, err := e.store.GetReviewBySession(ctx, worker.ID)
	if err != nil {
		return domain.Review{}, err
	}
	review := domain.Review{
		ID:               e.newID(),
		SessionID:        worker.ID,
		ProjectID:        worker.ProjectID,
		Harness:          harness,
		PRURL:            "",
		ReviewerHandleID: handleID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if ok {
		// Reuse the existing row's identity and creation time; UpsertReview
		// refreshes harness/pr_url/reviewer_handle_id/updated_at.
		review.ID = existing.ID
		review.CreatedAt = existing.CreatedAt
	}
	if err := e.store.UpsertReview(ctx, review); err != nil {
		return domain.Review{}, err
	}
	return review, nil
}
