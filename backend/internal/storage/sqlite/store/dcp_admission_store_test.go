package store_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestDCPAdmissionFIFOLeaseSurvivesRestartAndSerializesClaims(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertProject(ctx, domain.ProjectRecord{ID: "lab", Path: filepath.Join(dir, "lab"), RegisteredAt: time.Unix(1, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	first := seedAdmissionCandidate(t, s, "lab", 1)
	second := seedAdmissionCandidate(t, s, "lab", 2)
	if first.Sequence >= second.Sequence {
		t.Fatalf("FIFO sequences = %d, %d", first.Sequence, second.Sequence)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	next, ok, err := s.GetNextWaitingDCPReviewLabAdmission(ctx)
	if err != nil || !ok || next.ID != first.ID {
		t.Fatalf("next after restart = %+v ok=%v err=%v", next, ok, err)
	}

	var wg sync.WaitGroup
	results := make(chan string, 2)
	for _, a := range []domain.DCPReviewLabAdmission{first, second} {
		wg.Add(1)
		go func(a domain.DCPReviewLabAdmission) {
			defer wg.Done()
			claimed, claimErr := s.ClaimDCPReviewLabAdmission(ctx, a, "lease-"+a.ID, a.ReviewBaseSHA, time.Unix(10, 0).UTC())
			if claimErr != nil {
				results <- "error:" + claimErr.Error()
				return
			}
			if claimed {
				results <- a.ID
			}
		}(a)
	}
	wg.Wait()
	close(results)
	var winners []string
	for result := range results {
		winners = append(winners, result)
	}
	if len(winners) != 1 || winners[0] != first.ID {
		t.Fatalf("claim winners = %v, want only %s", winners, first.ID)
	}
	claimed, ok, err := s.GetClaimedDCPReviewLabAdmission(ctx)
	if err != nil || !ok || claimed.ID != first.ID {
		t.Fatalf("claimed row = %+v ok=%v err=%v", claimed, ok, err)
	}
	if completed, err := s.CompleteDCPReviewLabAdmission(ctx, claimed, strings.Repeat("f", 40), time.Unix(11, 0).UTC()); err != nil || !completed {
		t.Fatalf("complete first = %v, %v", completed, err)
	}
	if claimed, err := s.ClaimDCPReviewLabAdmission(ctx, second, "lease-"+second.ID, second.ReviewBaseSHA, time.Unix(12, 0).UTC()); err != nil || !claimed {
		t.Fatalf("claim second = %v, %v", claimed, err)
	}
}

func TestDCPAdmissionRefreshRebindsSameFIFOIdentityToOneNewReviewHead(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "lab")
	a := seedAdmissionCandidate(t, s, "lab", 1)
	started, err := s.StartDCPReviewLabRefresh(ctx, a, "refresh-lease", a.ReviewBaseSHA, time.Unix(10, 0).UTC())
	if err != nil || !started {
		t.Fatalf("start refresh = %v, %v", started, err)
	}
	a, ok, err := s.GetRefreshingDCPReviewLabAdmissionBySession(ctx, a.SessionID)
	if err != nil || !ok {
		t.Fatalf("refreshing = %+v ok=%v err=%v", a, ok, err)
	}
	newHead, newBase := strings.Repeat("e", 40), strings.Repeat("d", 40)
	now := time.Unix(11, 0).UTC()
	if err := s.WriteSCMObservation(ctx, domain.PullRequest{
		URL: a.PRURL, SessionID: a.SessionID, Number: int(a.PRNumber), Provider: "github", Host: "github.com",
		Repo: "orenvlad-ai/dcp-review-lab", HeadSHA: newHead, BaseSHA: newBase, SourceBranch: "branch", TargetBranch: "main",
		ProviderState: "OPEN", UpdatedAt: now, ObservedAt: now,
	}, nil, nil, nil, nil, ports.ReviewWritePreserve); err != nil {
		t.Fatal(err)
	}
	run := domain.ReviewRun{
		ID: "run-1-refresh", ReviewID: a.ReviewID, SessionID: a.SessionID, BatchID: "batch-1-refresh",
		Harness: domain.ReviewerCodex, PRURL: a.PRURL, TargetSHA: newHead, Status: domain.ReviewRunRunning, CreatedAt: now,
	}
	if err := s.InsertReviewRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdateBoundReviewRunResult(ctx, reviewcore.StructuredResultExpected{
		WorkerSessionID: string(a.SessionID), ReviewerHandleID: "reviewer-1", BatchID: run.BatchID,
		RunID: run.ID, PRURL: run.PRURL, TargetSHA: run.TargetSHA,
	}, domain.VerdictApproved, "approved refreshed head")
	if err != nil || !updated {
		t.Fatalf("complete refreshed review = %v, %v", updated, err)
	}
	resumed, err := s.ResumeDCPReviewLabAdmissionAfterRefresh(ctx, a, run, newBase, time.Unix(12, 0).UTC())
	if err != nil || !resumed {
		t.Fatalf("resume admission = %v, %v", resumed, err)
	}
	got, ok, err := s.GetDCPReviewLabAdmissionByRun(ctx, run.ID)
	if err != nil || !ok {
		t.Fatalf("rebound admission = %+v ok=%v err=%v", got, ok, err)
	}
	if got.ID != a.ID || got.Sequence != a.Sequence || got.Status != domain.DCPAdmissionWaiting || got.TargetSHA != newHead || got.RefreshWakeCount != 1 || got.LeaseID != "" {
		t.Fatalf("rebound admission = %+v", got)
	}
	if _, oldExists, err := s.GetDCPReviewLabAdmissionByRun(ctx, a.ReviewRunID); err != nil || oldExists {
		t.Fatalf("old run still owns admission: exists=%v err=%v", oldExists, err)
	}
}

func TestDCPAdmissionRefreshingHeadPhysicallyBlocksLaterClaim(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "lab")
	first := seedAdmissionCandidate(t, s, "lab", 1)
	second := seedAdmissionCandidate(t, s, "lab", 2)
	if started, err := s.StartDCPReviewLabRefresh(ctx, first, "refresh-first", first.ReviewBaseSHA, time.Unix(10, 0).UTC()); err != nil || !started {
		t.Fatalf("start first refresh = %v, %v", started, err)
	}
	if claimed, err := s.ClaimDCPReviewLabAdmission(ctx, second, "merge-second", second.ReviewBaseSHA, time.Unix(11, 0).UTC()); err != nil || claimed {
		t.Fatalf("later claim while first refreshes = %v, %v", claimed, err)
	}
	if _, claimed, err := s.GetClaimedDCPReviewLabAdmission(ctx); err != nil || claimed {
		t.Fatalf("claimed row exists=%v err=%v", claimed, err)
	}
}

func TestDCPAdmissionCanonicalBaseRecoveryPreservesIncidentPacketOnce(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "lab")
	a := seedAdmissionCandidate(t, s, "lab", 1)
	packetBytes, err := json.Marshal(map[string]any{
		"schemaVersion": "dcp.review-lab.arbiter-needed/v1",
		"reason":        "canonical_main_diverged",
		"admissionId":   a.ID,
		"sessionId":     string(a.SessionID),
		"reviewRunId":   a.ReviewRunID,
		"targetSha":     a.TargetSHA,
	})
	if err != nil {
		t.Fatal(err)
	}
	packet := string(packetBytes)
	recorded, err := s.RecordDCPReviewLabIncident(ctx, a, "dcp-incident-"+a.ID, a.ReviewBaseSHA, "canonical_main_diverged", packet, time.Unix(10, 0).UTC())
	if err != nil || !recorded {
		t.Fatalf("record incident = %v, %v", recorded, err)
	}
	a, ok, err := s.GetDCPReviewLabAdmissionByID(ctx, a.ID)
	if err != nil || !ok {
		t.Fatalf("incident = %+v ok=%v err=%v", a, ok, err)
	}
	recovered, err := s.RecoverDCPReviewLabCanonicalBaseIncident(ctx, a, time.Unix(11, 0).UTC())
	if err != nil || !recovered {
		t.Fatalf("recover = %v, %v", recovered, err)
	}
	got, ok, err := s.GetDCPReviewLabAdmissionByID(ctx, a.ID)
	if err != nil || !ok {
		t.Fatalf("recovered row = %+v ok=%v err=%v", got, ok, err)
	}
	if got.Status != domain.DCPAdmissionWaiting || got.LeaseID != "" || got.ErrorCode != "" || got.IncidentPacket != "" || got.RecoveredIncidentPacket != packet {
		t.Fatalf("recovered row = %+v", got)
	}
	if recovered, err := s.RecoverDCPReviewLabCanonicalBaseIncident(ctx, got, time.Unix(12, 0).UTC()); err != nil || recovered {
		t.Fatalf("duplicate recover = %v, %v", recovered, err)
	}
}

func seedAdmissionCandidate(t *testing.T, s *sqlite.Store, project string, ordinal int) domain.DCPReviewLabAdmission {
	t.Helper()
	ctx := context.Background()
	now := time.Unix(int64(ordinal+1), 0).UTC()
	rec := sampleRecord(project)
	rec.CreatedAt, rec.UpdatedAt = now, now
	rec, err := s.CreateSession(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}
	reviewID := "review-" + string(rune('0'+ordinal))
	reviewerHandleID := "reviewer-" + string(rune('0'+ordinal))
	if err := s.UpsertReview(ctx, domain.Review{ID: reviewID, SessionID: rec.ID, ProjectID: rec.ProjectID, Harness: domain.ReviewerCodex, ReviewerHandleID: reviewerHandleID, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	head := strings.Repeat(string(rune('a'+ordinal-1)), 40)
	base := strings.Repeat("c", 40)
	prURL := "https://github.com/orenvlad-ai/dcp-review-lab/pull/" + string(rune('0'+ordinal))
	pr := domain.PullRequest{
		URL: prURL, SessionID: rec.ID, Number: ordinal, Provider: "github", Host: "github.com",
		Repo: "orenvlad-ai/dcp-review-lab", HeadSHA: head, BaseSHA: base, SourceBranch: "branch", TargetBranch: "main",
		ProviderState: "OPEN", UpdatedAt: now, ObservedAt: now,
	}
	if err := s.WriteSCMObservation(ctx, pr, nil, nil, nil, nil, ports.ReviewWritePreserve); err != nil {
		t.Fatal(err)
	}
	run := domain.ReviewRun{
		ID: "run-" + string(rune('0'+ordinal)), ReviewID: reviewID, SessionID: rec.ID, BatchID: "batch-" + string(rune('0'+ordinal)),
		Harness: domain.ReviewerCodex, PRURL: prURL, TargetSHA: head, Status: domain.ReviewRunRunning,
		Verdict: domain.VerdictNone, CreatedAt: now,
	}
	if err := s.InsertReviewRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdateBoundReviewRunResult(ctx, reviewcore.StructuredResultExpected{
		WorkerSessionID: string(rec.ID), ReviewerHandleID: reviewerHandleID, BatchID: run.BatchID,
		RunID: run.ID, PRURL: run.PRURL, TargetSHA: run.TargetSHA,
	}, domain.VerdictApproved, "approved")
	if err != nil || !updated {
		t.Fatalf("complete structured review = %v, %v", updated, err)
	}
	a, created, err := s.EnqueueDCPReviewLabAdmission(ctx, domain.DCPReviewLabAdmission{
		ID: "admission-" + run.ID, ReviewRunID: run.ID, ReviewID: reviewID, SessionID: rec.ID,
		PRURL: prURL, PRNumber: int64(ordinal), TargetSHA: head, ReviewBaseSHA: base, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || !created {
		t.Fatalf("enqueue = %+v created=%v err=%v", a, created, err)
	}
	return a
}
