package store_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
	storepkg "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestInsertReviewRunDuplicatePRSHAMapsToSentinel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "mer")
	rec, err := s.CreateSession(ctx, sampleRecord("mer"))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.UpsertReview(ctx, domain.Review{
		ID: "rev-1", SessionID: rec.ID, ProjectID: rec.ProjectID,
		Harness: domain.ReviewerClaudeCode, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert review: %v", err)
	}
	run := domain.ReviewRun{
		ID: "run-1", ReviewID: "rev-1", SessionID: rec.ID, Harness: domain.ReviewerClaudeCode,
		PRURL: "https://example/pr/1", TargetSHA: "sha1", Status: domain.ReviewRunRunning, Verdict: domain.VerdictNone, CreatedAt: now,
	}
	if err := s.InsertReviewRun(ctx, run); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	// A second run for the same (session_id, pr_url, target_sha, harness) hits the
	// partial unique index (migration 0041) and must surface as the sentinel so
	// the engine can fall back to the existing run.
	dup := run
	dup.ID = "run-2"
	if err := s.InsertReviewRun(ctx, dup); !errors.Is(err, domain.ErrDuplicateReviewRun) {
		t.Fatalf("duplicate insert err = %v, want ErrDuplicateReviewRun", err)
	}

	otherPR := run
	otherPR.ID = "run-other-pr"
	otherPR.PRURL = "https://example/pr/2"
	if err := s.InsertReviewRun(ctx, otherPR); err != nil {
		t.Fatalf("same sha on different PR should insert: %v", err)
	}

	if ok, err := s.UpdateReviewRunResult(ctx, "run-1", domain.ReviewRunFailed, domain.VerdictNone, "claude: not found", ""); err != nil {
		t.Fatalf("mark failed: %v", err)
	} else if !ok {
		t.Fatal("mark failed: got ok=false")
	}
	if err := s.InsertReviewRun(ctx, dup); err != nil {
		t.Fatalf("retry after failed insert: %v", err)
	}

	// An empty target_sha is excluded from the index, so two are allowed.
	for _, id := range []string{"run-empty-1", "run-empty-2"} {
		r := run
		r.ID, r.TargetSHA = id, ""
		if err := s.InsertReviewRun(ctx, r); err != nil {
			t.Fatalf("empty-sha insert %s: %v", id, err)
		}
	}
}

// Harness is part of the idempotency key, so a different reviewer on the same
// commit is a second opinion rather than a duplicate. Without this the reviewer
// picker is inert on an already-reviewed commit.
func TestInsertReviewRunAllowsADifferentHarnessForTheSameCommit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "mer")
	rec, err := s.CreateSession(ctx, sampleRecord("mer"))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.UpsertReview(ctx, domain.Review{
		ID: "rev-1", SessionID: rec.ID, ProjectID: rec.ProjectID,
		Harness: domain.ReviewerClaudeCode, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert review: %v", err)
	}
	first := domain.ReviewRun{
		ID: "run-1", ReviewID: "rev-1", SessionID: rec.ID, Harness: domain.ReviewerClaudeCode,
		PRURL: "https://example/pr/1", TargetSHA: "sha1", Status: domain.ReviewRunRunning, Verdict: domain.VerdictNone, CreatedAt: now,
	}
	if err := s.InsertReviewRun(ctx, first); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	other := first
	other.ID = "run-other-harness"
	other.Harness = domain.ReviewerCodex
	if err := s.InsertReviewRun(ctx, other); err != nil {
		t.Fatalf("a different harness on the same commit should insert: %v", err)
	}

	// ...but the same harness twice is still a duplicate.
	same := first
	same.ID = "run-same-harness"
	if err := s.InsertReviewRun(ctx, same); !errors.Is(err, domain.ErrDuplicateReviewRun) {
		t.Fatalf("same harness duplicate err = %v, want ErrDuplicateReviewRun", err)
	}
}

func TestInsertReviewRunAllowsRerunAfterChangesRequested(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "mer")
	rec, err := s.CreateSession(ctx, sampleRecord("mer"))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.UpsertReview(ctx, domain.Review{
		ID: "rev-1", SessionID: rec.ID, ProjectID: rec.ProjectID,
		Harness: domain.ReviewerClaudeCode, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert review: %v", err)
	}
	run := domain.ReviewRun{
		ID: "run-1", ReviewID: "rev-1", SessionID: rec.ID, Harness: domain.ReviewerClaudeCode,
		PRURL: "https://example/pr/1", TargetSHA: "sha1", Status: domain.ReviewRunRunning, Verdict: domain.VerdictNone, CreatedAt: now,
	}
	if err := s.InsertReviewRun(ctx, run); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if ok, err := s.UpdateReviewRunResult(ctx, "run-1", domain.ReviewRunComplete, domain.VerdictChangesRequested, "please fix", "rev-1"); err != nil {
		t.Fatalf("mark changes requested: %v", err)
	} else if !ok {
		t.Fatal("mark changes requested: got ok=false")
	}

	rerun := run
	rerun.ID = "run-2"
	rerun.CreatedAt = now.Add(time.Second)
	if err := s.InsertReviewRun(ctx, rerun); err != nil {
		t.Fatalf("rerun after changes_requested insert: %v", err)
	}
}

func TestInsertReviewRunAllowsRerunAfterTerminalEmptyVerdict(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "mer")
	rec, err := s.CreateSession(ctx, sampleRecord("mer"))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.UpsertReview(ctx, domain.Review{
		ID: "rev-1", SessionID: rec.ID, ProjectID: rec.ProjectID,
		Harness: domain.ReviewerClaudeCode, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert review: %v", err)
	}
	run := domain.ReviewRun{
		ID: "run-1", ReviewID: "rev-1", SessionID: rec.ID, Harness: domain.ReviewerClaudeCode,
		PRURL: "https://example/pr/1", TargetSHA: "sha1", Status: domain.ReviewRunComplete, Verdict: domain.VerdictNone, CreatedAt: now,
	}
	if err := s.InsertReviewRun(ctx, run); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	rerun := run
	rerun.ID = "run-2"
	rerun.Status = domain.ReviewRunRunning
	rerun.CreatedAt = now.Add(time.Second)
	if err := s.InsertReviewRun(ctx, rerun); err != nil {
		t.Fatalf("rerun after terminal empty-verdict insert: %v", err)
	}
}

func TestReviewUpsertReusesRowAndRunRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "mer")
	rec, err := s.CreateSession(ctx, sampleRecord("mer"))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)

	// First upsert creates the review row.
	if err := s.UpsertReview(ctx, domain.Review{
		ID: "rev-1", SessionID: rec.ID, ProjectID: rec.ProjectID,
		Harness: domain.ReviewerClaudeCode, PRURL: "https://example/pr/1",
		ReviewerHandleID: "review-mer-1",
		CreatedAt:        now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert review: %v", err)
	}
	// Second upsert with the same session reuses the row (session_id UNIQUE),
	// refreshing harness/pr_url/reviewer_handle_id but keeping the original id.
	if err := s.UpsertReview(ctx, domain.Review{
		ID: "rev-2", SessionID: rec.ID, ProjectID: rec.ProjectID,
		Harness: domain.ReviewerHarness("greptile"), PRURL: "https://example/pr/2",
		ReviewerHandleID: "review-mer-1b",
		CreatedAt:        now, UpdatedAt: now.Add(time.Second),
	}); err != nil {
		t.Fatalf("upsert review (reuse): %v", err)
	}
	got, ok, err := s.GetReviewBySession(ctx, rec.ID)
	if err != nil || !ok {
		t.Fatalf("get review: ok=%v err=%v", ok, err)
	}
	if got.ID != "rev-1" {
		t.Fatalf("upsert created a new row, want reuse: id=%q", got.ID)
	}
	if got.Harness != domain.ReviewerHarness("greptile") || got.PRURL != "https://example/pr/2" || got.ReviewerHandleID != "review-mer-1b" {
		t.Fatalf("upsert did not refresh fields: %+v", got)
	}

	// A run inserts running and updates to complete/changes_requested.
	if err := s.InsertReviewRun(ctx, domain.ReviewRun{
		ID: "run-1", ReviewID: got.ID, SessionID: rec.ID, BatchID: "batch-1", Harness: domain.ReviewerHarness("greptile"),
		PRURL: got.PRURL, TargetSHA: "sha1", Status: domain.ReviewRunRunning, Verdict: domain.VerdictNone,
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if ok, err := s.UpdateReviewRunResult(ctx, "run-1", domain.ReviewRunComplete, domain.VerdictChangesRequested, "please fix", "rev-987"); err != nil {
		t.Fatalf("update run: %v", err)
	} else if !ok {
		t.Fatal("update run: got ok=false")
	}

	gotRun, ok, err := s.GetReviewRun(ctx, "run-1")
	if err != nil || !ok {
		t.Fatalf("get run: ok=%v err=%v", ok, err)
	}
	if gotRun.ID != "run-1" || gotRun.SessionID != rec.ID || gotRun.BatchID != "batch-1" || gotRun.TargetSHA != "sha1" {
		t.Fatalf("get run = %+v", gotRun)
	}

	bySHA, ok, err := s.GetReviewRunBySessionPRAndSHA(ctx, rec.ID, got.PRURL, "sha1")
	if err != nil || !ok {
		t.Fatalf("by sha: ok=%v err=%v", ok, err)
	}
	if bySHA.Status != domain.ReviewRunComplete || bySHA.Verdict != domain.VerdictChangesRequested || bySHA.Body != "please fix" || bySHA.GithubReviewID != "rev-987" {
		t.Fatalf("run result not persisted: %+v", bySHA)
	}
	byHarness, ok, err := s.GetReviewRunBySessionPRSHAAndHarness(ctx, rec.ID, got.PRURL, "sha1", domain.ReviewerHarness("greptile"))
	if err != nil || !ok {
		t.Fatalf("by harness: ok=%v err=%v", ok, err)
	}
	if byHarness.ID != "run-1" {
		t.Fatalf("by harness = %+v, want run-1", byHarness)
	}
	if _, ok, _ := s.GetReviewRunBySessionPRSHAAndHarness(ctx, rec.ID, got.PRURL, "sha1", domain.ReviewerCodex); ok {
		t.Fatal("unexpected run for a different harness")
	}
	if _, ok, _ := s.GetReviewRunBySessionPRAndSHA(ctx, rec.ID, got.PRURL, "other"); ok {
		t.Fatal("unexpected run for a different sha")
	}

	runs, err := s.ListReviewRunsBySession(ctx, rec.ID)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "run-1" {
		t.Fatalf("list runs = %+v", runs)
	}
	batchRuns, err := s.ListReviewRunsByBatch(ctx, rec.ID, "batch-1")
	if err != nil {
		t.Fatalf("list batch runs: %v", err)
	}
	if len(batchRuns) != 1 || batchRuns[0].ID != "run-1" || batchRuns[0].BatchID != "batch-1" {
		t.Fatalf("batch runs = %+v", batchRuns)
	}

	if ok, err := s.UpdateReviewRunResult(ctx, "run-1", domain.ReviewRunComplete, domain.VerdictApproved, "again", ""); err != nil {
		t.Fatalf("second update: %v", err)
	} else if ok {
		t.Fatal("second update completed an already-complete run")
	}
}

func TestUpdateBoundReviewRunResultIsAtomicAndExactHeadBound(t *testing.T) {
	sha := "1111111111111111111111111111111111111111"
	prURL := "https://github.com/o/r/pull/1"
	setup := func(t *testing.T) (*storepkg.Store, reviewcore.StructuredResultExpected) {
		t.Helper()
		s := newTestStore(t)
		ctx := context.Background()
		seedProject(t, s, "mer")
		rec, err := s.CreateSession(ctx, sampleRecord("mer"))
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC().Truncate(time.Second)
		if err := s.WriteSCMObservation(ctx, domain.PullRequest{URL: prURL, SessionID: rec.ID, Number: 1, HeadSHA: sha, UpdatedAt: now}, nil, nil, nil, nil, ports.ReviewWriteReplace); err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertReview(ctx, domain.Review{
			ID: "rev-1", SessionID: rec.ID, ProjectID: rec.ProjectID, Harness: domain.ReviewerCodex,
			ReviewerHandleID: "review-" + string(rec.ID), CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		if err := s.InsertReviewRun(ctx, domain.ReviewRun{
			ID: "run-1", ReviewID: "rev-1", SessionID: rec.ID, BatchID: "batch-1", Harness: domain.ReviewerCodex,
			PRURL: prURL, TargetSHA: sha, Status: domain.ReviewRunRunning, CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		return s, reviewcore.StructuredResultExpected{
			WorkerSessionID: string(rec.ID), ReviewerHandleID: "review-" + string(rec.ID), BatchID: "batch-1", RunID: "run-1", PRURL: prURL, TargetSHA: sha,
		}
	}

	t.Run("one winner under concurrent duplicate submits", func(t *testing.T) {
		s, expected := setup(t)
		var winners atomic.Int32
		errs := make(chan error, 16)
		var wg sync.WaitGroup
		for range 16 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ok, err := s.UpdateBoundReviewRunResult(context.Background(), expected, domain.VerdictApproved, "approved")
				if err != nil {
					errs <- err
					return
				}
				if ok {
					winners.Add(1)
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("concurrent update: %v", err)
		}
		if got := winners.Load(); got != 1 {
			t.Fatalf("successful updates = %d, want exactly one", got)
		}
		run, _, _ := s.GetReviewRun(context.Background(), expected.RunID)
		if run.Status != domain.ReviewRunComplete || run.Verdict != domain.VerdictApproved || run.Body != "approved" || run.ResultChannel != "structured_dcp_v1" {
			t.Fatalf("run = %+v", run)
		}
		claimed, err := s.ClaimDCPReviewLabTerminalMerge(context.Background(), run)
		if err != nil || !claimed {
			t.Fatalf("claim=%v err=%v", claimed, err)
		}
		if claimed, err := s.ClaimDCPReviewLabTerminalMerge(context.Background(), run); err != nil || claimed {
			t.Fatalf("duplicate claim=%v err=%v", claimed, err)
		}
		mergeSHA := "3333333333333333333333333333333333333333"
		if completed, err := s.CompleteDCPReviewLabTerminalMerge(context.Background(), run.ID, mergeSHA); err != nil || !completed {
			t.Fatalf("complete=%v err=%v", completed, err)
		}
		run, _, _ = s.GetReviewRun(context.Background(), expected.RunID)
		if run.TerminalMergeStatus != "succeeded" || run.TerminalMergeCommitSHA != mergeSHA || run.TerminalMergeError != "" {
			t.Fatalf("terminal merge state = %+v", run)
		}
	})

	for _, tc := range []struct {
		name   string
		mutate func(*reviewcore.StructuredResultExpected)
	}{
		{name: "session", mutate: func(e *reviewcore.StructuredResultExpected) { e.WorkerSessionID = "mer-foreign" }},
		{name: "terminal", mutate: func(e *reviewcore.StructuredResultExpected) { e.ReviewerHandleID = "review-mer-foreign" }},
		{name: "batch", mutate: func(e *reviewcore.StructuredResultExpected) { e.BatchID = "batch-foreign" }},
		{name: "run", mutate: func(e *reviewcore.StructuredResultExpected) { e.RunID = "run-foreign" }},
		{name: "pr", mutate: func(e *reviewcore.StructuredResultExpected) { e.PRURL = "https://github.com/o/r/pull/2" }},
		{name: "target", mutate: func(e *reviewcore.StructuredResultExpected) { e.TargetSHA = "2222222222222222222222222222222222222222" }},
	} {
		t.Run("reject "+tc.name, func(t *testing.T) {
			s, expected := setup(t)
			tc.mutate(&expected)
			ok, err := s.UpdateBoundReviewRunResult(context.Background(), expected, domain.VerdictApproved, "foreign")
			if err != nil || ok {
				t.Fatalf("update ok=%v err=%v", ok, err)
			}
			run, _, _ := s.GetReviewRun(context.Background(), "run-1")
			if run.Status != domain.ReviewRunRunning || run.Verdict != domain.VerdictNone {
				t.Fatalf("foreign binding mutated run: %+v", run)
			}
		})
	}

	t.Run("reject stale observed head", func(t *testing.T) {
		s, expected := setup(t)
		pr, ok, err := s.GetPR(context.Background(), expected.PRURL)
		if err != nil || !ok {
			t.Fatal(err)
		}
		pr.HeadSHA = "2222222222222222222222222222222222222222"
		pr.UpdatedAt = pr.UpdatedAt.Add(time.Second)
		if err := s.WriteSCMObservation(context.Background(), pr, nil, nil, nil, nil, ports.ReviewWriteReplace); err != nil {
			t.Fatal(err)
		}
		updated, err := s.UpdateBoundReviewRunResult(context.Background(), expected, domain.VerdictApproved, "stale")
		if err != nil || updated {
			t.Fatalf("stale head update=%v err=%v", updated, err)
		}
	})
}

func TestCancelRunningReviewRunsBySession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "mer")
	rec, err := s.CreateSession(ctx, sampleRecord("mer"))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.UpsertReview(ctx, domain.Review{
		ID: "rev-1", SessionID: rec.ID, ProjectID: rec.ProjectID,
		Harness: domain.ReviewerCodex, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert review: %v", err)
	}
	for _, run := range []domain.ReviewRun{
		{ID: "run-1", ReviewID: "rev-1", SessionID: rec.ID, Harness: domain.ReviewerCodex, PRURL: "https://example/pr/1", TargetSHA: "sha1", Status: domain.ReviewRunRunning, CreatedAt: now},
		{ID: "run-2", ReviewID: "rev-1", SessionID: rec.ID, Harness: domain.ReviewerCodex, PRURL: "https://example/pr/2", TargetSHA: "sha2", Status: domain.ReviewRunRunning, CreatedAt: now.Add(time.Second)},
		{ID: "run-3", ReviewID: "rev-1", SessionID: rec.ID, Harness: domain.ReviewerCodex, PRURL: "https://example/pr/3", TargetSHA: "sha3", Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved, CreatedAt: now.Add(2 * time.Second)},
	} {
		if err := s.InsertReviewRun(ctx, run); err != nil {
			t.Fatalf("insert %s: %v", run.ID, err)
		}
	}
	running, err := s.ListRunningReviewRunsBySession(ctx, rec.ID)
	if err != nil {
		t.Fatalf("list running: %v", err)
	}
	if len(running) != 2 || running[0].ID != "run-2" || running[1].ID != "run-1" {
		t.Fatalf("running = %+v", running)
	}
	n, err := s.CancelRunningReviewRunsBySession(ctx, rec.ID, "cancelled by user")
	if err != nil {
		t.Fatalf("cancel running: %v", err)
	}
	if n != 2 {
		t.Fatalf("cancelled rows = %d, want 2", n)
	}
	runs, err := s.ListReviewRunsBySession(ctx, rec.ID)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	byID := map[string]domain.ReviewRun{}
	for _, run := range runs {
		byID[run.ID] = run
	}
	if byID["run-1"].Status != domain.ReviewRunCancelled || byID["run-2"].Status != domain.ReviewRunCancelled {
		t.Fatalf("running runs not cancelled: %+v", byID)
	}
	if byID["run-3"].Status != domain.ReviewRunComplete {
		t.Fatalf("complete run changed: %+v", byID["run-3"])
	}
}

func TestReviewGettersMissing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, ok, err := s.GetReviewBySession(ctx, "mer-1"); err != nil || ok {
		t.Fatalf("missing review: ok=%v err=%v", ok, err)
	}
	if _, ok, err := s.GetReviewRunBySessionPRAndSHA(ctx, "mer-1", "pr1", "sha1"); err != nil || ok {
		t.Fatalf("missing run: ok=%v err=%v", ok, err)
	}
	if _, ok, err := s.GetReviewRun(ctx, "run-missing"); err != nil || ok {
		t.Fatalf("missing run by id: ok=%v err=%v", ok, err)
	}
}
