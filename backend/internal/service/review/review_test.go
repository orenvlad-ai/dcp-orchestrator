package review

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/lifecycle"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
)

type fakeStore struct {
	run            domain.ReviewRun
	ok             bool
	batchRuns      []domain.ReviewRun
	prs            []domain.PullRequest
	reviewerHandle string

	updateCalls int
	markCalls   int
	markedIDs   []string
}

func (f *fakeStore) UpdateBoundReviewRunResult(_ context.Context, expected reviewcore.StructuredResultExpected, verdict domain.ReviewVerdict, body string) (bool, error) {
	if !f.ok || f.run.Status != domain.ReviewRunRunning || f.run.ID != expected.RunID || string(f.run.SessionID) != expected.WorkerSessionID ||
		f.run.BatchID != expected.BatchID || f.run.PRURL != expected.PRURL || f.run.TargetSHA != expected.TargetSHA || f.reviewerHandle != expected.ReviewerHandleID {
		return false, nil
	}
	current := false
	for _, pr := range f.prs {
		if pr.URL == expected.PRURL && pr.SessionID == f.run.SessionID && pr.HeadSHA == expected.TargetSHA && !pr.Draft && !pr.Merged && !pr.Closed {
			current = true
			break
		}
	}
	if !current {
		return false, nil
	}
	f.updateCalls++
	f.run.Status = domain.ReviewRunComplete
	f.run.Verdict = verdict
	f.run.Body = body
	return true, nil
}

func (f *fakeStore) GetReviewRun(_ context.Context, id string) (domain.ReviewRun, bool, error) {
	for _, run := range f.batchRuns {
		if run.ID == id {
			return run, true, nil
		}
	}
	if f.ok && f.run.ID == id {
		return f.run, true, nil
	}
	return domain.ReviewRun{}, false, nil
}

func (f *fakeStore) UpdateReviewRunResult(_ context.Context, id string, status domain.ReviewRunStatus, verdict domain.ReviewVerdict, body, githubReviewID string) (bool, error) {
	for i := range f.batchRuns {
		if f.batchRuns[i].ID == id {
			if f.batchRuns[i].Status != domain.ReviewRunRunning {
				return false, nil
			}
			f.updateCalls++
			f.batchRuns[i].Status = status
			f.batchRuns[i].Verdict = verdict
			f.batchRuns[i].Body = body
			f.batchRuns[i].GithubReviewID = githubReviewID
			if f.run.ID == id {
				f.run = f.batchRuns[i]
			}
			return true, nil
		}
	}
	if f.run.Status != domain.ReviewRunRunning {
		return false, nil
	}
	f.updateCalls++
	f.run.Status = status
	f.run.Verdict = verdict
	f.run.Body = body
	f.run.GithubReviewID = githubReviewID
	return true, nil
}

func (f *fakeStore) MarkReviewRunDelivered(_ context.Context, id string, deliveredAt time.Time) (bool, error) {
	f.markCalls++
	f.markedIDs = append(f.markedIDs, id)
	if f.run.ID == id && f.run.Status == domain.ReviewRunComplete && f.run.DeliveredAt == nil {
		f.run.Status = domain.ReviewRunDelivered
		f.run.DeliveredAt = &deliveredAt
	}
	for i := range f.batchRuns {
		if f.batchRuns[i].ID == id && f.batchRuns[i].Status == domain.ReviewRunComplete && f.batchRuns[i].DeliveredAt == nil {
			f.batchRuns[i].Status = domain.ReviewRunDelivered
			f.batchRuns[i].DeliveredAt = &deliveredAt
			return true, nil
		}
	}
	if f.run.ID != id || f.run.Status != domain.ReviewRunDelivered {
		return false, nil
	}
	return true, nil
}

func (f *fakeStore) ListReviewRunsByBatch(context.Context, domain.SessionID, string) ([]domain.ReviewRun, error) {
	out := append([]domain.ReviewRun(nil), f.batchRuns...)
	return out, nil
}

func (f *fakeStore) ListPRsBySession(context.Context, domain.SessionID) ([]domain.PullRequest, error) {
	out := append([]domain.PullRequest(nil), f.prs...)
	return out, nil
}

type fakeReducer struct {
	outcome    lifecycle.ReviewDeliveryOutcome
	err        error
	batchCalls int
	gotBatchID string
	gotBatch   []lifecycle.ReviewResult
}

func TestProcessExitFailsOnlyStillRunningExactRuns(t *testing.T) {
	st := &fakeStore{batchRuns: []domain.ReviewRun{
		{ID: "run-1", SessionID: "mer-1", Status: domain.ReviewRunRunning},
		{ID: "run-2", SessionID: "mer-1", Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved},
	}}
	svc := New(nil, st)
	runs, err := svc.ProcessExit(context.Background(), "mer-1", ProcessExitReport{RunIDs: []string{"run-1", "run-2"}, Started: true, ExitCode: 2})
	if err != nil {
		t.Fatalf("ProcessExit: %v", err)
	}
	if len(runs) != 2 || runs[0].Status != domain.ReviewRunFailed || !strings.Contains(runs[0].Body, "code 2") {
		t.Fatalf("runs = %+v", runs)
	}
	if runs[1].Status != domain.ReviewRunComplete || st.updateCalls != 1 {
		t.Fatalf("terminal result was overwritten: runs=%+v updates=%d", runs, st.updateCalls)
	}
	if _, err := svc.ProcessExit(context.Background(), "mer-1", ProcessExitReport{RunIDs: []string{"run-1"}, Started: true, ExitCode: 2}); err != nil {
		t.Fatalf("idempotent ProcessExit: %v", err)
	}
	if st.updateCalls != 1 {
		t.Fatalf("idempotent report updated twice: %d", st.updateCalls)
	}
}

func structuredServiceResult() reviewcore.StructuredResult {
	return reviewcore.StructuredResult{
		Version:          reviewcore.StructuredResultVersion,
		WorkerSessionID:  "mer-1",
		ReviewerHandleID: "review-mer-1",
		BatchID:          "batch-1",
		RunID:            "run-1",
		PRURL:            "https://github.com/o/r/pull/1",
		TargetSHA:        strings.Repeat("1", 40),
		Verdict:          "approved",
		Summary:          "No blocking findings.",
		Findings:         []reviewcore.StructuredFinding{},
	}
}

func structuredServiceStore() *fakeStore {
	result := structuredServiceResult()
	return &fakeStore{
		ok:             true,
		reviewerHandle: result.ReviewerHandleID,
		run: domain.ReviewRun{
			ID: result.RunID, SessionID: "mer-1", BatchID: result.BatchID,
			PRURL: result.PRURL, TargetSHA: result.TargetSHA, Status: domain.ReviewRunRunning,
		},
		prs: []domain.PullRequest{{URL: result.PRURL, SessionID: "mer-1", HeadSHA: result.TargetSHA}},
	}
}

func TestSubmitStructuredPersistsOnceAndRejectsDuplicateOrLate(t *testing.T) {
	st := structuredServiceStore()
	svc := New(nil, st)
	approvedCalls := 0
	svc.SetApprovedStructuredHandler(func(_ context.Context, id domain.SessionID) {
		if id != "mer-1" {
			t.Fatalf("approved handler id = %q", id)
		}
		approvedCalls++
	})
	result := structuredServiceResult()

	run, err := svc.SubmitStructured(context.Background(), "mer-1", result)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != domain.ReviewRunComplete || run.Verdict != domain.VerdictApproved || run.Body != result.Summary || st.updateCalls != 1 || approvedCalls != 1 {
		t.Fatalf("run=%+v updates=%d", run, st.updateCalls)
	}
	if _, err := svc.SubmitStructured(context.Background(), "mer-1", result); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate error = %v, want ErrInvalid", err)
	}
	if st.updateCalls != 1 {
		t.Fatalf("duplicate result updated twice: %d", st.updateCalls)
	}
	if approvedCalls != 1 {
		t.Fatalf("duplicate result signalled approval twice: %d", approvedCalls)
	}

	lateStore := structuredServiceStore()
	lateStore.run.Status = domain.ReviewRunFailed
	if _, err := New(nil, lateStore).SubmitStructured(context.Background(), "mer-1", result); !errors.Is(err, ErrInvalid) {
		t.Fatalf("late result error = %v, want ErrInvalid", err)
	}
	if lateStore.updateCalls != 0 {
		t.Fatalf("late result mutated storage: %d", lateStore.updateCalls)
	}
}

func TestSubmitStructuredRejectsEveryForeignOrStaleBinding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*fakeStore, *reviewcore.StructuredResult)
	}{
		{name: "foreign worker", mutate: func(_ *fakeStore, result *reviewcore.StructuredResult) {
			result.WorkerSessionID = "mer-2"
			result.ReviewerHandleID = "review-mer-2"
		}},
		{name: "foreign terminal", mutate: func(store *fakeStore, _ *reviewcore.StructuredResult) { store.reviewerHandle = "review-mer-foreign" }},
		{name: "foreign batch", mutate: func(_ *fakeStore, result *reviewcore.StructuredResult) { result.BatchID = "batch-foreign" }},
		{name: "foreign run", mutate: func(_ *fakeStore, result *reviewcore.StructuredResult) { result.RunID = "run-foreign" }},
		{name: "foreign PR", mutate: func(_ *fakeStore, result *reviewcore.StructuredResult) {
			result.PRURL = "https://github.com/o/r/pull/2"
		}},
		{name: "foreign target", mutate: func(_ *fakeStore, result *reviewcore.StructuredResult) { result.TargetSHA = strings.Repeat("2", 40) }},
		{name: "stale current head", mutate: func(store *fakeStore, _ *reviewcore.StructuredResult) { store.prs[0].HeadSHA = strings.Repeat("2", 40) }},
		{name: "draft head", mutate: func(store *fakeStore, _ *reviewcore.StructuredResult) { store.prs[0].Draft = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := structuredServiceStore()
			result := structuredServiceResult()
			tc.mutate(store, &result)
			if _, err := New(nil, store).SubmitStructured(context.Background(), "mer-1", result); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
			if store.updateCalls != 0 || store.run.Status != domain.ReviewRunRunning {
				t.Fatalf("foreign result mutated run: %+v updates=%d", store.run, store.updateCalls)
			}
		})
	}
}

func TestProcessExitRecordsBoundedStructuredFailureWithoutVerdict(t *testing.T) {
	st := structuredServiceStore()
	svc := New(nil, st)
	runs, err := svc.ProcessExit(context.Background(), "mer-1", ProcessExitReport{
		RunIDs: []string{"run-1"}, Started: true, ExitCode: 0, ResultFailure: string(reviewcore.StructuredResultForeign),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != domain.ReviewRunFailed || runs[0].Verdict != domain.VerdictNone || !strings.Contains(runs[0].Body, "foreign identity") {
		t.Fatalf("runs = %+v", runs)
	}
}

func (f *fakeReducer) ApplyReviewBatch(_ context.Context, _ domain.SessionID, batchID string, results []lifecycle.ReviewResult) (lifecycle.ReviewDeliveryOutcome, error) {
	f.batchCalls++
	f.gotBatchID = batchID
	f.gotBatch = append([]lifecycle.ReviewResult(nil), results...)
	return f.outcome, f.err
}

func TestSubmitPersistsThenAppliesThenStampsDelivered(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	st := &fakeStore{
		ok:  true,
		run: domain.ReviewRun{ID: "run-1", SessionID: "mer-1", BatchID: "batch-1", PRURL: "pr1", TargetSHA: "sha1", Status: domain.ReviewRunRunning},
		prs: []domain.PullRequest{{URL: "pr1", HeadSHA: "sha1"}},
	}
	reducer := &fakeReducer{outcome: lifecycle.ReviewDeliverySent}
	svc := New(nil, st, WithLifecycleReducer(reducer), WithClock(func() time.Time { return now }))

	run, err := svc.Submit(context.Background(), "mer-1", "run-1", domain.VerdictChangesRequested, "fix it", "987")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if st.updateCalls != 1 || reducer.batchCalls != 1 || st.markCalls != 1 {
		t.Fatalf("calls update/reducer/mark = %d/%d/%d", st.updateCalls, reducer.batchCalls, st.markCalls)
	}
	if reducer.gotBatch[0].Verdict != domain.VerdictChangesRequested || reducer.gotBatch[0].Body != "fix it" || reducer.gotBatch[0].GithubReviewID != "987" {
		t.Fatalf("reducer saw wrong result: %+v", reducer.gotBatch)
	}
	if run.Status != domain.ReviewRunDelivered || run.DeliveredAt == nil || !run.DeliveredAt.Equal(now) {
		t.Fatalf("run not stamped delivered: %+v", run)
	}
}

func TestSubmitBatchRunDoesNotWaitForOtherRunningRuns(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	st := &fakeStore{
		ok:  true,
		run: domain.ReviewRun{ID: "run-1", SessionID: "mer-1", BatchID: "batch-1", PRURL: "pr1", TargetSHA: "sha1", Status: domain.ReviewRunRunning},
		batchRuns: []domain.ReviewRun{
			{ID: "run-1", SessionID: "mer-1", BatchID: "batch-1", PRURL: "pr1", TargetSHA: "sha1", Status: domain.ReviewRunRunning},
			{ID: "run-2", SessionID: "mer-1", BatchID: "batch-1", PRURL: "pr2", TargetSHA: "sha2", Status: domain.ReviewRunRunning},
		},
		prs: []domain.PullRequest{{URL: "pr1", HeadSHA: "sha1"}, {URL: "pr2", HeadSHA: "sha2"}},
	}
	reducer := &fakeReducer{outcome: lifecycle.ReviewDeliverySent}
	svc := New(nil, st, WithLifecycleReducer(reducer), WithClock(func() time.Time { return now }))

	run, err := svc.Submit(context.Background(), "mer-1", "run-1", domain.VerdictChangesRequested, "fix pr1", "101")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if run.Status != domain.ReviewRunDelivered || run.DeliveredAt == nil || !run.DeliveredAt.Equal(now) {
		t.Fatalf("first submit status = %+v, want delivered", run)
	}
	if reducer.batchCalls != 1 || len(reducer.gotBatch) != 1 || reducer.gotBatch[0].RunID != "run-1" || st.markCalls != 1 {
		t.Fatalf("submitted run should deliver independently: batchCalls=%d got=%+v markCalls=%d", reducer.batchCalls, reducer.gotBatch, st.markCalls)
	}
}

func TestSubmitManySendsCombinedChangesRequested(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	st := &fakeStore{
		ok: true,
		batchRuns: []domain.ReviewRun{
			{ID: "run-1", SessionID: "mer-1", BatchID: "batch-1", PRURL: "pr1", TargetSHA: "sha1", Status: domain.ReviewRunRunning},
			{ID: "run-2", SessionID: "mer-1", BatchID: "batch-1", PRURL: "pr2", TargetSHA: "sha2", Status: domain.ReviewRunRunning},
			{ID: "run-3", SessionID: "mer-1", BatchID: "batch-1", PRURL: "pr3", TargetSHA: "sha3", Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved},
			{ID: "run-4", SessionID: "mer-1", BatchID: "batch-1", PRURL: "pr4", TargetSHA: "old", Status: domain.ReviewRunComplete, Verdict: domain.VerdictChangesRequested, Body: "stale"},
			{ID: "run-5", SessionID: "mer-1", BatchID: "batch-1", PRURL: "pr5", TargetSHA: "sha5", Status: domain.ReviewRunFailed},
		},
		prs: []domain.PullRequest{
			{URL: "pr1", HeadSHA: "sha1"},
			{URL: "pr2", HeadSHA: "sha2"},
			{URL: "pr3", HeadSHA: "sha3"},
			{URL: "pr4", HeadSHA: "new"},
			{URL: "pr5", HeadSHA: "sha5"},
		},
	}
	reducer := &fakeReducer{outcome: lifecycle.ReviewDeliverySent}
	svc := New(nil, st, WithLifecycleReducer(reducer), WithClock(func() time.Time { return now }))

	runs, err := svc.SubmitMany(context.Background(), "mer-1", []SubmittedReview{
		{RunID: "run-1", Verdict: domain.VerdictChangesRequested, Body: "fix pr1", GithubReviewID: "101"},
		{RunID: "run-2", Verdict: domain.VerdictChangesRequested, Body: "fix pr2", GithubReviewID: "102"},
		{RunID: "run-3", Verdict: domain.VerdictApproved},
	})
	if err != nil {
		t.Fatalf("SubmitMany: %v", err)
	}
	if reducer.batchCalls != 1 || reducer.gotBatchID != "batch-1" {
		t.Fatalf("batch delivery calls/id = %d/%q", reducer.batchCalls, reducer.gotBatchID)
	}
	if len(reducer.gotBatch) != 2 || reducer.gotBatch[0].RunID != "run-1" || reducer.gotBatch[1].RunID != "run-2" {
		t.Fatalf("delivered batch = %+v, want run-1 and run-2 only", reducer.gotBatch)
	}
	if st.markCalls != 2 {
		t.Fatalf("markCalls = %d, want 2", st.markCalls)
	}
	if runs[0].Status != domain.ReviewRunDelivered || runs[0].DeliveredAt == nil || !runs[0].DeliveredAt.Equal(now) ||
		runs[1].Status != domain.ReviewRunDelivered || runs[1].DeliveredAt == nil || !runs[1].DeliveredAt.Equal(now) {
		t.Fatalf("submitted runs not stamped delivered: %+v", runs)
	}
}

func TestSubmitBatchApprovedOnlySendsNothing(t *testing.T) {
	st := &fakeStore{
		ok:  true,
		run: domain.ReviewRun{ID: "run-2", SessionID: "mer-1", BatchID: "batch-1", PRURL: "pr2", TargetSHA: "sha2", Status: domain.ReviewRunRunning},
		batchRuns: []domain.ReviewRun{
			{ID: "run-1", SessionID: "mer-1", BatchID: "batch-1", PRURL: "pr1", TargetSHA: "sha1", Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved},
			{ID: "run-2", SessionID: "mer-1", BatchID: "batch-1", PRURL: "pr2", TargetSHA: "sha2", Status: domain.ReviewRunRunning},
		},
		prs: []domain.PullRequest{{URL: "pr1", HeadSHA: "sha1"}, {URL: "pr2", HeadSHA: "sha2"}},
	}
	reducer := &fakeReducer{outcome: lifecycle.ReviewDeliverySent}
	svc := New(nil, st, WithLifecycleReducer(reducer))

	if _, err := svc.Submit(context.Background(), "mer-1", "run-2", domain.VerdictApproved, "", "102"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if reducer.batchCalls != 0 || st.markCalls != 0 {
		t.Fatalf("approved-only batch should not deliver: batchCalls=%d markCalls=%d", reducer.batchCalls, st.markCalls)
	}
}

func TestSubmitDeliveryFailureLeavesCompletedUndeliveredForRetry(t *testing.T) {
	sendErr := errors.New("dead pane")
	st := &fakeStore{
		ok:  true,
		run: domain.ReviewRun{ID: "run-1", SessionID: "mer-1", BatchID: "batch-1", PRURL: "pr1", TargetSHA: "sha1", Status: domain.ReviewRunRunning},
		prs: []domain.PullRequest{{URL: "pr1", HeadSHA: "sha1"}},
	}
	reducer := &fakeReducer{err: sendErr}
	svc := New(nil, st, WithLifecycleReducer(reducer))

	if _, err := svc.Submit(context.Background(), "mer-1", "run-1", domain.VerdictChangesRequested, "fix it", "987"); !errors.Is(err, sendErr) {
		t.Fatalf("err = %v, want sendErr", err)
	}
	if st.run.Status != domain.ReviewRunComplete || st.run.DeliveredAt != nil || st.markCalls != 0 {
		t.Fatalf("failed delivery should leave completed/undelivered without stamp: %+v markCalls=%d", st.run, st.markCalls)
	}

	reducer.err = nil
	reducer.outcome = lifecycle.ReviewDeliverySent
	if _, err := svc.Submit(context.Background(), "mer-1", "run-1", domain.VerdictChangesRequested, "fix it", "987"); err != nil {
		t.Fatalf("retry Submit: %v", err)
	}
	if st.updateCalls != 1 || reducer.batchCalls != 2 || st.run.Status != domain.ReviewRunDelivered || st.run.DeliveredAt == nil {
		t.Fatalf("retry should not rewrite result and should stamp delivery: update=%d reducer=%d run=%+v", st.updateCalls, reducer.batchCalls, st.run)
	}
}

func TestSubmitCompletedRetryRejectsDifferentRecordedFields(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		githubReviewID string
	}{
		{name: "different body", body: "different", githubReviewID: "987"},
		{name: "different review id", body: "fix it", githubReviewID: "654"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &fakeStore{ok: true, run: domain.ReviewRun{
				ID: "run-1", SessionID: "mer-1", PRURL: "pr1", TargetSHA: "sha1",
				Status: domain.ReviewRunComplete, Verdict: domain.VerdictChangesRequested,
				Body: "fix it", GithubReviewID: "987",
			}}
			reducer := &fakeReducer{outcome: lifecycle.ReviewDeliverySent}
			svc := New(nil, st, WithLifecycleReducer(reducer))

			if _, err := svc.Submit(context.Background(), "mer-1", "run-1", domain.VerdictChangesRequested, tt.body, tt.githubReviewID); !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
			if st.updateCalls != 0 || st.markCalls != 0 || reducer.batchCalls != 0 {
				t.Fatalf("mismatched retry should not rewrite or deliver: update=%d mark=%d reducer=%d", st.updateCalls, st.markCalls, reducer.batchCalls)
			}
		})
	}
}
