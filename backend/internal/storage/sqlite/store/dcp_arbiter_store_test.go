package store_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestDCPReleaseArbiterSingleFlightSurvivesRestartAndRejectsReplay(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertProject(ctx, domain.ProjectRecord{ID: "dcp-review-lab", Path: filepath.Join(dir, "target"), RegisteredAt: time.Unix(1, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		rec := sampleRecord("dcp-review-lab")
		if _, err := s.CreateSession(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	admission := seedAdmissionCandidate(t, s, "dcp-review-lab", 11)
	if admission.SessionID != "dcp-review-lab-11" {
		t.Fatalf("session = %s, want exact card 11", admission.SessionID)
	}
	packetBytes, err := json.Marshal(map[string]any{
		"schemaVersion": "dcp.review-lab.arbiter-needed/v1", "reason": "merge_conflict_or_ambiguity",
		"admissionId": admission.ID, "sequence": admission.Sequence, "leaseId": "dcp-incident-" + admission.ID,
		"sessionId": string(admission.SessionID), "reviewRunId": admission.ReviewRunID, "targetSha": admission.TargetSHA,
	})
	if err != nil {
		t.Fatal(err)
	}
	packet := string(packetBytes)
	if recorded, err := s.RecordDCPReviewLabIncident(ctx, admission, "dcp-incident-"+admission.ID, admission.ReviewBaseSHA, "merge_conflict_or_ambiguity", packet, time.Unix(20, 0).UTC()); err != nil || !recorded {
		t.Fatalf("record source incident = %v, %v", recorded, err)
	}
	admission, ok, err := s.GetDCPReviewLabAdmissionByID(ctx, admission.ID)
	if err != nil || !ok {
		t.Fatal("reload source incident")
	}
	digest := strings.Repeat("a", 64)
	incident := domain.DCPReleaseArbiterIncident{
		IncidentID: "dcp-global-release-" + digest, Generation: 1, IdentityDigest: digest,
		AdmissionID: admission.ID, IncidentLeaseID: admission.LeaseID, SourcePacketJSON: packet,
		SourcePacketDigest: strings.Repeat("b", 64), InputJSON: `{"schemaVersion":"dcp.review-lab.global-release-arbiter-input/v1"}`,
		InputDigest: strings.Repeat("c", 64), TaskID: "i13-arbiter-a", SessionID: admission.SessionID,
		WorktreePath: filepath.Join(dir, "worktree"), SourceBranch: "ao/dcp-review-lab-11/root",
		PRURL: admission.PRURL, PRNumber: admission.PRNumber, TargetSHA: admission.TargetSHA,
		ReviewedBaseSHA: admission.ReviewBaseSHA, CurrentBaseSHA: strings.Repeat("d", 40),
		ReviewID: admission.ReviewID, ReviewRunID: admission.ReviewRunID, BatchID: "batch",
		ScopeDigest: strings.Repeat("1", 64), HistoryDigest: strings.Repeat("2", 64), DiffDigest: strings.Repeat("3", 64),
		CheckSetDigest: strings.Repeat("4", 64), ReviewSetDigest: strings.Repeat("5", 64), FrozenQueueDigest: strings.Repeat("6", 64), MechanicalDigest: strings.Repeat("7", 64),
		Model: "gpt-5.6-sol", Reasoning: "xhigh", TokenBudget: 16384,
		RuntimeHandleID: "dcp-global-release-arbiter-v1", LaunchID: "dcp-global-release-" + digest,
		Status: domain.DCPArbiterRequested, CreatedAt: time.Unix(21, 0).UTC(), UpdatedAt: time.Unix(21, 0).UTC(),
	}
	created, inserted, err := s.OpenDCPReleaseArbiterIncident(ctx, admission, incident)
	if err != nil || !inserted || created.IncidentID != incident.IncidentID {
		t.Fatalf("open arbiter = %+v inserted=%v err=%v", created, inserted, err)
	}
	if _, inserted, err := s.OpenDCPReleaseArbiterIncident(ctx, admission, incident); err != nil || inserted {
		t.Fatalf("equal replay = inserted=%v err=%v", inserted, err)
	}
	drifted := incident
	drifted.InputDigest = strings.Repeat("e", 64)
	if _, _, err := s.OpenDCPReleaseArbiterIncident(ctx, admission, drifted); err == nil {
		t.Fatal("changed replay was accepted")
	}
	if started, err := s.StartDCPReleaseArbiterCall(ctx, created, time.Unix(22, 0).UTC()); err != nil || !started {
		t.Fatalf("start call = %v, %v", started, err)
	}
	if started, err := s.StartDCPReleaseArbiterCall(ctx, created, time.Unix(23, 0).UTC()); err != nil || started {
		t.Fatalf("duplicate call = %v, %v", started, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	restarted, ok, err := s.GetDCPReleaseArbiterIncidentByID(ctx, incident.IncidentID)
	if err != nil || !ok || restarted.Status != domain.DCPArbiterRunning || restarted.ModelCallCount != 1 {
		t.Fatalf("restart row = %+v ok=%v err=%v", restarted, ok, err)
	}
	decision := `{"schemaVersion":"dcp.review-lab.global-release-arbiter-decision/v1"}`
	if accepted, err := s.RecordDCPReleaseArbiterDecision(ctx, restarted, decision, strings.Repeat("f", 64), false, "", time.Unix(24, 0).UTC()); err != nil || !accepted {
		t.Fatalf("record decision = %v, %v", accepted, err)
	}
	restarted, _, _ = s.GetDCPReleaseArbiterIncidentByID(ctx, incident.IncidentID)
	if consumed, err := s.ConsumeDCPReleaseArbiterRepair(ctx, restarted, time.Unix(25, 0).UTC()); err != nil || !consumed {
		t.Fatalf("consume repair = %v, %v", consumed, err)
	}
	if consumed, err := s.ConsumeDCPReleaseArbiterRepair(ctx, restarted, time.Unix(26, 0).UTC()); err != nil || consumed {
		t.Fatalf("duplicate repair = %v, %v", consumed, err)
	}
	final, _, _ := s.GetDCPReleaseArbiterIncidentByID(ctx, incident.IncidentID)
	if final.Status != domain.DCPArbiterRepairing || final.ModelCallCount != 1 || final.RecoveryWakeCount != 1 || final.RecoveryOwnerSessionID != admission.SessionID {
		t.Fatalf("repair row = %+v", final)
	}
	newHead, now := strings.Repeat("e", 40), time.Unix(27, 0).UTC()
	if err := s.WriteSCMObservation(ctx, domain.PullRequest{
		URL: admission.PRURL, SessionID: admission.SessionID, Number: int(admission.PRNumber), Provider: "github", Host: "github.com",
		Repo: "orenvlad-ai/dcp-review-lab", HeadSHA: newHead, BaseSHA: incident.CurrentBaseSHA,
		SourceBranch: incident.SourceBranch, TargetBranch: "main", ProviderState: "OPEN", UpdatedAt: now, ObservedAt: now,
	}, nil, nil, nil, nil, ports.ReviewWritePreserve); err != nil {
		t.Fatal(err)
	}
	recoveryRun := domain.ReviewRun{
		ID: "run-11-recovery", ReviewID: admission.ReviewID, SessionID: admission.SessionID, BatchID: "batch-11-recovery",
		Harness: domain.ReviewerCodex, PRURL: admission.PRURL, TargetSHA: newHead, Status: domain.ReviewRunRunning, CreatedAt: now,
	}
	if err := s.InsertReviewRun(ctx, recoveryRun); err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdateBoundReviewRunResult(ctx, reviewcore.StructuredResultExpected{
		WorkerSessionID: string(admission.SessionID), ReviewerHandleID: "reviewer-;", BatchID: recoveryRun.BatchID,
		RunID: recoveryRun.ID, PRURL: recoveryRun.PRURL, TargetSHA: recoveryRun.TargetSHA,
	}, domain.VerdictApproved, "approved repaired head")
	if err != nil || !updated {
		t.Fatalf("complete recovery review = %v, %v", updated, err)
	}
	if rebound, err := s.RebindDCPAdmissionAfterArbiterRepair(ctx, admission, final, recoveryRun, incident.CurrentBaseSHA, time.Unix(28, 0).UTC()); err != nil || !rebound {
		t.Fatalf("rebind recovery = %v, %v", rebound, err)
	}
	rebound, ok, err := s.GetDCPReviewLabAdmissionByRun(ctx, recoveryRun.ID)
	if err != nil || !ok || rebound.ID != admission.ID || rebound.Sequence != admission.Sequence || rebound.Status != domain.DCPAdmissionWaiting || rebound.RecoveredIncidentPacket != packet {
		t.Fatalf("rebound admission = %+v ok=%v err=%v", rebound, ok, err)
	}
	if claimed, err := s.ClaimDCPReviewLabAdmission(ctx, rebound, "merge-recovery", incident.CurrentBaseSHA, time.Unix(29, 0).UTC()); err != nil || !claimed {
		t.Fatalf("claim repaired admission = %v, %v", claimed, err)
	}
	rebound, _, _ = s.GetDCPReviewLabAdmissionByRun(ctx, recoveryRun.ID)
	if completed, err := s.CompleteDCPReviewLabAdmission(ctx, rebound, strings.Repeat("9", 40), time.Unix(30, 0).UTC()); err != nil || !completed {
		t.Fatalf("complete repaired admission = %v, %v", completed, err)
	}
	completedArbiter, _, _ := s.GetDCPReleaseArbiterIncidentByID(ctx, incident.IncidentID)
	if completedArbiter.Status != domain.DCPArbiterSucceeded || completedArbiter.RecoveryReviewRunID != recoveryRun.ID || completedArbiter.RecoveryTargetSHA != newHead || completedArbiter.ModelCallCount != 1 {
		t.Fatalf("completed arbiter = %+v", completedArbiter)
	}
}
