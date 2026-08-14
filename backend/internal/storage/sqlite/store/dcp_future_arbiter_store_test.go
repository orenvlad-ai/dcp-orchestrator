package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestDCPFutureArbiterOneCallRepairFreshReviewMergeAndRestartDedupe(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = s.Close()
		}
	})
	seedProject(t, s, "dcp-review-lab")
	for i := 1; i <= 12; i++ {
		rec := sampleRecord("dcp-review-lab")
		rec.Harness = domain.HarnessCodex
		if _, err := s.CreateSession(ctx, rec); err != nil {
			t.Fatalf("seed historical session %d: %v", i, err)
		}
	}

	now := time.Unix(307, 0).UTC()
	seed := domain.SessionRecord{ProjectID: "dcp-review-lab", Kind: domain.KindWorker, Harness: domain.HarnessCodex,
		DisplayName: "DCP:arb-store-a", Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: now}, CreatedAt: now, UpdatedAt: now}
	reserved, err := s.ReserveDCPReviewLabPolicyTask(ctx, policyTaskFixture("arb-store-a", now), seed, filepath.Join(dir, "worktrees"))
	if err != nil || !reserved.Created {
		t.Fatalf("reserve = %+v, %v", reserved, err)
	}
	queued := reserved.Task
	queued.State = domain.DCPPolicyWorkerQueued
	if ok, err := s.UpdateDCPReviewLabPolicyTaskCAS(ctx, reserved.Task, queued); err != nil || !ok {
		t.Fatalf("queue worker = %v, %v", ok, err)
	}
	worker, ok, err := s.ClaimNextDCPModelAction(ctx, now.Add(time.Second))
	if err != nil || !ok || worker.Kind != domain.DCPActionInitialWorker {
		t.Fatalf("claim worker = %+v, %v, %v", worker, ok, err)
	}
	if ok, err := s.StartDCPModelAction(ctx, worker, "worker-generation", "", now.Add(2*time.Second)); err != nil || !ok {
		t.Fatalf("start worker = %v, %v", ok, err)
	}
	task, _, _ := s.GetDCPReviewLabPolicyTaskByTaskID(ctx, reserved.Task.TaskID)
	task.State = domain.DCPPolicyCIWaiting
	if ok, err := s.FinishDCPModelAction(ctx, worker, task, domain.DCPActionSucceeded, "", now.Add(3*time.Second)); err != nil || !ok {
		t.Fatalf("finish worker = %v, %v", ok, err)
	}

	oldHead := strings.Repeat("a", 40)
	request := seedPolicyAdmissionFacts(t, s, task, "arb-store-run-1", oldHead, 81)
	task, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
	prepared := task
	prepared.State, prepared.CurrentHeadSHA, prepared.ReviewRunID = domain.DCPPolicyAdmissionWait, oldHead, request.ReviewRunID
	prepared.PRURL, prepared.PRNumber = request.PRURL, request.PRNumber
	if ok, err := s.UpdateDCPReviewLabPolicyTaskCAS(ctx, task, prepared); err != nil || !ok {
		t.Fatalf("prepare admission = %v, %v", ok, err)
	}
	prepared, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
	admission, created, err := s.EnqueueDCPReviewLabPolicyAdmission(ctx, request, prepared)
	if err != nil || !created {
		t.Fatalf("enqueue admission = %+v, %v, %v", admission, created, err)
	}
	prepared, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
	packet := fmt.Sprintf(`{"schemaVersion":"dcp.review-lab.arbiter-needed/v1","reason":"merge_conflict_or_ambiguity","admissionId":%q}`, admission.ID)
	lease := "dcp-incident-" + admission.ID
	if ok, err := s.RecordDCPReviewLabPolicyIncident(ctx, admission, prepared, lease, request.ReviewBaseSHA, "merge_conflict_or_ambiguity", packet, now.Add(4*time.Second)); err != nil || !ok {
		t.Fatalf("record source incident = %v, %v", ok, err)
	}
	incidentTask, _, _ := s.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
	incidentAdmission, _, _ := s.GetDCPReviewLabAdmissionByID(ctx, admission.ID)

	digest := func(r byte) string { return strings.Repeat(string(r), 64) }
	incidentID := "dcp-future-arbiter-" + digest('1')
	incident := domain.DCPFutureArbiterIncident{
		IncidentID: incidentID, Generation: 1, IdentityDigest: digest('1'), TaskID: task.TaskID,
		SessionID: task.SessionID, AdmissionID: admission.ID, AdmissionSequence: admission.Sequence, IncidentLeaseID: lease,
		IncidentKind: "merge_conflict_or_ambiguity", SourcePacketJSON: packet, SourcePacketDigest: digest('2'),
		PRURL: admission.PRURL, PRNumber: admission.PRNumber, CandidateHeadSHA: oldHead, ReviewedBaseSHA: admission.ReviewBaseSHA,
		CurrentMainSHA: strings.Repeat("b", 40), ReviewRunID: admission.ReviewRunID,
		AffectedPathsJSON: `["shared.txt"]`, CohortJSON: `[{"taskId":"arb-store-a"}]`, CohortDigest: digest('3'),
		EvidenceJSON: `{}`, EvidenceDigest: digest('4'), InputJSON: `{"schemaVersion":"dcp.review-lab.future-arbiter-input/v1"}`, InputDigest: digest('5'),
		ModelActionID: "dcp-model-arb-store-a-arbiter-1", RuntimeHandleID: incidentID,
		Status: domain.DCPFutureArbiterRequested, CreatedAt: now.Add(5 * time.Second), UpdatedAt: now.Add(5 * time.Second),
	}
	action := domain.DCPModelAction{ID: incident.ModelActionID, TaskID: task.TaskID, SessionID: task.SessionID, Kind: domain.DCPActionArbiter,
		ExactHeadSHA: oldHead, IncidentID: incidentID, Status: domain.DCPActionQueued, CreatedAt: incident.CreatedAt, UpdatedAt: incident.UpdatedAt}
	opened, created, err := s.OpenDCPFutureArbiterIncident(ctx, incident, action)
	if err != nil || !created || opened.IncidentID != incidentID {
		t.Fatalf("open arbiter = %+v, %v, %v", opened, created, err)
	}
	if replay, created, err := s.OpenDCPFutureArbiterIncident(ctx, incident, action); err != nil || created || replay.IncidentID != incidentID {
		t.Fatalf("restart/replay open = %+v, %v, %v", replay, created, err)
	}
	conflict := incident
	conflict.InputDigest = digest('9')
	if _, _, err := s.OpenDCPFutureArbiterIncident(ctx, conflict, action); err == nil {
		t.Fatal("conflicting incident-generation replay was accepted")
	}
	claimed, ok, err := s.ClaimNextDCPModelAction(ctx, now.Add(6*time.Second))
	if err != nil || !ok || claimed.Kind != domain.DCPActionArbiter || claimed.IncidentID != incidentID {
		t.Fatalf("claim arbiter = %+v, %v, %v", claimed, ok, err)
	}
	unchanged, _, _ := s.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
	if unchanged.State != domain.DCPPolicyIncident || unchanged.Revision != incidentTask.Revision {
		t.Fatalf("arbiter visually mutated incident task = %+v", unchanged)
	}
	if ok, err := s.StartDCPModelAction(ctx, claimed, incidentID, "", now.Add(7*time.Second)); err != nil || !ok {
		t.Fatalf("start arbiter = %v, %v", ok, err)
	}
	claimed.Status, claimed.LaunchID = domain.DCPActionRunning, incidentID
	if ok, err := s.FailDCPFutureArbiterIncident(ctx, incident, claimed, "launch_failed", now.Add(8*time.Second)); err != nil || !ok {
		t.Fatalf("preserve provider-rejected generation = %v, %v", ok, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	providerError := `{"type":"invalid_request_error","code":"invalid_json_schema","message":"uniqueItems is not permitted","status":400}`
	if _, err := raw.Exec(`
INSERT INTO dcp_future_card_arbiter_schema_recovery_v1 (
  recovery_id, predecessor_incident_id, predecessor_identity_digest,
  predecessor_input_digest, predecessor_model_action_id, predecessor_schema_digest,
  provider_error_json, provider_error_digest, provider_inference_tokens,
  successor_generation, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, 2, 'authorized', ?, ?)`,
		"dcp-future-arbiter-schema-recovery-"+digest('1'), incident.IncidentID, incident.IdentityDigest,
		incident.InputDigest, incident.ModelActionID, digest('7'), providerError, digest('8'), now.Add(8*time.Second), now.Add(8*time.Second)); err != nil {
		_ = raw.Close()
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	closed = false
	recovery, found, err := s.GetDCPFutureArbiterSchemaRecoveryByPredecessor(ctx, incident.IncidentID)
	if err != nil || !found || recovery.Status != "authorized" {
		t.Fatalf("schema recovery grant = %+v, %v, %v", recovery, found, err)
	}
	predecessor, found, err := s.GetDCPFutureArbiterIncidentByID(ctx, incident.IncidentID)
	if err != nil || !found || predecessor.Status != domain.DCPFutureArbiterFailed {
		t.Fatalf("immutable predecessor = %+v, %v, %v", predecessor, found, err)
	}
	successor := incident
	successor.Generation, successor.IdentityDigest = 2, digest('9')
	successor.IncidentID = "dcp-future-arbiter-" + successor.IdentityDigest
	successor.InputDigest, successor.ModelActionID, successor.RuntimeHandleID = digest('a'), "dcp-model-arb-store-a-arbiter-2", successor.IncidentID
	successor.Status, successor.ModelCallCount, successor.ErrorCode = domain.DCPFutureArbiterRequested, 0, ""
	successor.CreatedAt, successor.UpdatedAt = now.Add(9*time.Second), now.Add(9*time.Second)
	successorAction := action
	successorAction.ID, successorAction.IncidentID = successor.ModelActionID, successor.IncidentID
	successorAction.Status, successorAction.CreatedAt, successorAction.UpdatedAt = domain.DCPActionQueued, successor.CreatedAt, successor.UpdatedAt
	opened, created, err = s.OpenDCPFutureArbiterSchemaRecovery(ctx, predecessor, recovery, successor, successorAction)
	if err != nil || !created || opened.IncidentID != successor.IncidentID {
		t.Fatalf("open schema recovery = %+v, %v, %v", opened, created, err)
	}
	if _, created, err := s.OpenDCPFutureArbiterSchemaRecovery(ctx, predecessor, recovery, successor, successorAction); err == nil || created {
		t.Fatalf("schema recovery replay was accepted: created=%v err=%v", created, err)
	}
	recovery, found, err = s.GetDCPFutureArbiterSchemaRecoveryByPredecessor(ctx, incident.IncidentID)
	if err != nil || !found || recovery.Status != "consumed" || recovery.SuccessorIncidentID != successor.IncidentID {
		t.Fatalf("consumed schema recovery = %+v, %v, %v", recovery, found, err)
	}
	incident = successor
	incidentID = successor.IncidentID
	claimed, ok, err = s.ClaimNextDCPModelAction(ctx, now.Add(10*time.Second))
	if err != nil || !ok || claimed.ID != successor.ModelActionID {
		t.Fatalf("claim successor arbiter = %+v, %v, %v", claimed, ok, err)
	}
	if ok, err := s.StartDCPModelAction(ctx, claimed, incidentID, "", now.Add(11*time.Second)); err != nil || !ok {
		t.Fatalf("start successor arbiter = %v, %v", ok, err)
	}
	claimed.Status, claimed.LaunchID = domain.DCPActionRunning, incidentID
	decisionJSON := `{"schemaVersion":"dcp.review-lab.future-arbiter-decision/v1"}`
	if ok, err := s.RecordDCPFutureArbiterDecision(ctx, incident, claimed, decisionJSON, digest('6'), domain.DCPFutureVerdictRepair,
		`["arb-store-a"]`, "Rebase the compatible intent onto exact current main.", `["shared.txt"]`, "", now.Add(12*time.Second)); err != nil || !ok {
		t.Fatalf("record repair decision = %v, %v", ok, err)
	}
	repair, ok, err := s.ClaimNextDCPModelAction(ctx, now.Add(13*time.Second))
	if err != nil || !ok || repair.Kind != domain.DCPActionRepairWorker || repair.IncidentID != incidentID {
		t.Fatalf("claim exact repair = %+v, %v, %v", repair, ok, err)
	}
	if ok, err := s.StartDCPModelAction(ctx, repair, "repair-generation", "", now.Add(14*time.Second)); err != nil || !ok {
		t.Fatalf("start repair = %v, %v", ok, err)
	}
	task, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
	task.State = domain.DCPPolicyCIWaiting
	if ok, err := s.FinishDCPModelAction(ctx, repair, task, domain.DCPActionSucceeded, "", now.Add(15*time.Second)); err != nil || !ok {
		t.Fatalf("finish repair = %v, %v", ok, err)
	}

	newHead := strings.Repeat("c", 40)
	newBase := incident.CurrentMainSHA
	newRun := seedArbiterRecoveryReview(t, s, incidentAdmission, newHead, newBase, now.Add(16*time.Second))
	task, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
	reviewAction := domain.DCPModelAction{ID: "dcp-model-arb-store-a-review-2", TaskID: task.TaskID, SessionID: task.SessionID,
		Kind: domain.DCPActionReviewer, ExactHeadSHA: newHead, Status: domain.DCPActionQueued, CreatedAt: now.Add(17 * time.Second), UpdatedAt: now.Add(17 * time.Second)}
	reviewQueued := task
	reviewQueued.State, reviewQueued.PreviousHeadSHA, reviewQueued.CurrentHeadSHA = domain.DCPPolicyReviewQueued, oldHead, newHead
	if _, created, err := s.QueueDCPModelAction(ctx, task, reviewQueued, reviewAction); err != nil || !created {
		t.Fatalf("queue fresh review = %v, %v", created, err)
	}
	review, ok, err := s.ClaimNextDCPModelAction(ctx, now.Add(18*time.Second))
	if err != nil || !ok || review.ID != reviewAction.ID {
		t.Fatalf("claim fresh review = %+v, %v, %v", review, ok, err)
	}
	if ok, err := s.StartDCPModelAction(ctx, review, "fresh-review-handle", newRun.ID, now.Add(19*time.Second)); err != nil || !ok {
		t.Fatalf("start fresh review = %v, %v", ok, err)
	}
	reviewTask, _, _ := s.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
	bound := reviewTask
	bound.ReviewRunID = newRun.ID
	if ok, err := s.UpdateDCPReviewLabPolicyTaskCAS(ctx, reviewTask, bound); err != nil || !ok {
		t.Fatalf("bind fresh run = %v, %v", ok, err)
	}
	bound, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
	bound.State = domain.DCPPolicyAdmissionWait
	if ok, err := s.FinishDCPModelAction(ctx, review, bound, domain.DCPActionSucceeded, "", now.Add(20*time.Second)); err != nil || !ok {
		t.Fatalf("finish fresh review = %v, %v", ok, err)
	}
	bound, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
	if ok, err := s.RebindDCPFutureArbiterAdmission(ctx, incident, bound, newRun, newBase, now.Add(21*time.Second)); err != nil || !ok {
		t.Fatalf("rebind admission = %v, %v", ok, err)
	}
	rebound, _, _ := s.GetDCPReviewLabAdmissionByID(ctx, admission.ID)
	if rebound.Status != domain.DCPAdmissionWaiting || rebound.TargetSHA != newHead || rebound.Sequence != admission.Sequence {
		t.Fatalf("rebound FIFO identity = %+v", rebound)
	}
	if ok, err := s.ClaimDCPReviewLabAdmission(ctx, rebound, "merge-"+admission.ID, newBase, now.Add(22*time.Second)); err != nil || !ok {
		t.Fatalf("claim rebound = %v, %v", ok, err)
	}
	rebound, _, _ = s.GetClaimedDCPReviewLabAdmission(ctx)
	bound, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
	mergeSHA := strings.Repeat("d", 40)
	if ok, err := s.CompleteDCPReviewLabPolicyAdmission(ctx, rebound, bound, mergeSHA, now.Add(23*time.Second)); err != nil || !ok {
		t.Fatalf("complete repaired merge = %v, %v", ok, err)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true
	s, err = sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	closed = false
	finalIncident, found, err := s.GetDCPFutureArbiterIncidentByID(ctx, incidentID)
	if err != nil || !found || finalIncident.Status != domain.DCPFutureArbiterSucceeded || finalIncident.ModelCallCount != 1 || finalIncident.MergeCommitSHA != mergeSHA {
		t.Fatalf("terminal incident after restart = %+v, %v, %v", finalIncident, found, err)
	}
	actions, err := s.ListDCPModelActions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[domain.DCPModelActionKind]int{}
	for _, got := range actions {
		counts[got.Kind]++
	}
	if counts[domain.DCPActionArbiter] != 2 || counts[domain.DCPActionRepairWorker] != 1 || counts[domain.DCPActionReviewer] != 1 {
		t.Fatalf("deduped action counts = %v", counts)
	}
}

func seedArbiterRecoveryReview(t *testing.T, s *sqlite.Store, admission domain.DCPReviewLabAdmission, head, base string, now time.Time) domain.ReviewRun {
	t.Helper()
	ctx := context.Background()
	if err := s.WriteSCMObservation(ctx, domain.PullRequest{
		URL: admission.PRURL, SessionID: admission.SessionID, Number: int(admission.PRNumber), Provider: "github", Host: "github.com",
		Repo: "orenvlad-ai/dcp-review-lab", HeadSHA: head, BaseSHA: base, SourceBranch: "dcp/arb-store-a", TargetBranch: "main",
		ProviderState: "OPEN", UpdatedAt: now, ObservedAt: now,
	}, nil, nil, nil, nil, ports.ReviewWritePreserve); err != nil {
		t.Fatal(err)
	}
	review, found, err := s.GetReviewBySession(ctx, admission.SessionID)
	if err != nil || !found {
		t.Fatalf("get review = %+v, %v, %v", review, found, err)
	}
	run := domain.ReviewRun{ID: "arb-store-run-2", ReviewID: review.ID, SessionID: admission.SessionID, BatchID: "batch-arb-store-run-2",
		Harness: domain.ReviewerCodex, PRURL: admission.PRURL, TargetSHA: head, Status: domain.ReviewRunRunning, CreatedAt: now}
	if err := s.InsertReviewRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdateBoundReviewRunResult(ctx, reviewcore.StructuredResultExpected{WorkerSessionID: string(admission.SessionID), ReviewerHandleID: review.ReviewerHandleID,
		BatchID: run.BatchID, RunID: run.ID, PRURL: run.PRURL, TargetSHA: run.TargetSHA}, domain.VerdictApproved, "approved recovery")
	if err != nil || !updated {
		t.Fatalf("approve recovery review = %v, %v", updated, err)
	}
	run.Status, run.Verdict, run.Body = domain.ReviewRunComplete, domain.VerdictApproved, "approved recovery"
	return run
}
