package store_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestDCPReviewLabPolicyFourTaskSlotsHeadsFIFOAndRestartDedupe(t *testing.T) {
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
		if created, createErr := s.CreateSession(ctx, rec); createErr != nil || created.ID != domain.SessionID(fmt.Sprintf("dcp-review-lab-%d", i)) {
			t.Fatalf("seed historical card %d = %s, %v", i, created.ID, createErr)
		}
	}

	root := filepath.Join(dir, "worktrees")
	tasks := make([]domain.DCPReviewLabPolicyTask, 0, 4)
	for i := 1; i <= 4; i++ {
		task := policyTaskFixture(fmt.Sprintf("future-%d", i), time.Unix(int64(100+i), 0).UTC())
		seed := domain.SessionRecord{
			ProjectID: "dcp-review-lab", Kind: domain.KindWorker, Harness: domain.HarnessCodex,
			DisplayName: "DCP:" + task.TaskID, Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: task.CreatedAt},
			CreatedAt: task.CreatedAt, UpdatedAt: task.CreatedAt,
		}
		reserved, reserveErr := s.ReserveDCPReviewLabPolicyTask(ctx, task, seed, root)
		if reserveErr != nil || !reserved.Created || reserved.Task.CardNumber != int64(12+i) {
			t.Fatalf("reserve task %d = %+v, %v", i, reserved, reserveErr)
		}
		if i == 1 {
			replayed, replayErr := s.ReserveDCPReviewLabPolicyTask(ctx, task, seed, root)
			if replayErr != nil || replayed.Created || replayed.Task.SessionID != reserved.Task.SessionID {
				t.Fatalf("equal replay = %+v, %v", replayed, replayErr)
			}
			conflict := task
			conflict.Prompt = "different immutable payload"
			if _, conflictErr := s.ReserveDCPReviewLabPolicyTask(ctx, conflict, seed, root); !errors.Is(conflictErr, sqlitestore.ErrDCPPolicyConflict) {
				t.Fatalf("conflicting replay error = %v", conflictErr)
			}
		}
		queued := reserved.Task
		queued.State, queued.UpdatedAt = domain.DCPPolicyWorkerQueued, task.CreatedAt.Add(time.Second)
		if updated, updateErr := s.UpdateDCPReviewLabPolicyTaskCAS(ctx, reserved.Task, queued); updateErr != nil || !updated {
			t.Fatalf("queue task %d = %v, %v", i, updated, updateErr)
		}
		if stale, staleErr := s.UpdateDCPReviewLabPolicyTaskCAS(ctx, reserved.Task, queued); staleErr != nil || stale {
			t.Fatalf("stale task revision %d was not rejected: %v, %v", i, stale, staleErr)
		}
		fresh, found, getErr := s.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
		if getErr != nil || !found {
			t.Fatalf("fresh task %d = %+v, %v, %v", i, fresh, found, getErr)
		}
		tasks = append(tasks, fresh)
	}

	for i := 0; i < 3; i++ {
		action, claimed, claimErr := s.ClaimNextDCPModelAction(ctx, time.Unix(int64(200+i), 0).UTC())
		if claimErr != nil || !claimed || action.TaskID != tasks[i].TaskID || action.Slot != int64(i+1) {
			t.Fatalf("claim %d = %+v, %v, %v", i, action, claimed, claimErr)
		}
		if started, startErr := s.StartDCPModelAction(ctx, action, fmt.Sprintf("worker-launch-%d", i+1), "", time.Unix(int64(205+i), 0).UTC()); startErr != nil || !started {
			t.Fatalf("start %d = %v, %v", i, started, startErr)
		}
	}
	if action, claimed, claimErr := s.ClaimNextDCPModelAction(ctx, time.Unix(203, 0).UTC()); claimErr != nil || claimed {
		t.Fatalf("fourth claim above cap = %+v, %v, %v", action, claimed, claimErr)
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
	active, err := s.ListActiveDCPModelActions(ctx)
	if err != nil || len(active) != 3 {
		t.Fatalf("active slots after restart = %d, %v", len(active), err)
	}

	finishPolicyWorker(t, s, active[0], domain.DCPPolicyCIWaiting, time.Unix(210, 0).UTC())
	fourth, claimed, err := s.ClaimNextDCPModelAction(ctx, time.Unix(211, 0).UTC())
	if err != nil || !claimed || fourth.TaskID != tasks[3].TaskID || fourth.Slot != 1 {
		t.Fatalf("fourth FIFO claim = %+v, %v, %v", fourth, claimed, err)
	}
	if started, startErr := s.StartDCPModelAction(ctx, fourth, "worker-launch-4", "", time.Unix(212, 0).UTC()); startErr != nil || !started {
		t.Fatalf("start fourth = %v, %v", started, startErr)
	}
	for _, action := range append(active[1:], fourth) {
		finishPolicyWorker(t, s, action, domain.DCPPolicyCIWaiting, time.Unix(212+action.Slot, 0).UTC())
	}

	first, _, _ := s.GetDCPReviewLabPolicyTaskByTaskID(ctx, tasks[0].TaskID)
	headA := strings.Repeat("a", 40)
	reviewA := domain.DCPModelAction{
		ID: "dcp-model-future-1-review-1", TaskID: first.TaskID, SessionID: first.SessionID,
		Kind: domain.DCPActionReviewer, ExactHeadSHA: headA, Status: domain.DCPActionQueued,
		CreatedAt: time.Unix(220, 0).UTC(), UpdatedAt: time.Unix(220, 0).UTC(),
	}
	next := first
	next.State, next.CurrentHeadSHA = domain.DCPPolicyReviewQueued, headA
	queuedReview, created, err := s.QueueDCPModelAction(ctx, first, next, reviewA)
	if err != nil || !created {
		t.Fatalf("queue first review = %+v, %v, %v", queuedReview, created, err)
	}
	first, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, first.TaskID)
	if duplicate, duplicateCreated, duplicateErr := s.QueueDCPModelAction(ctx, first, first, reviewA); duplicateErr != nil || duplicateCreated || duplicate.ID != reviewA.ID {
		t.Fatalf("duplicate review webhook = %+v, %v, %v", duplicate, duplicateCreated, duplicateErr)
	}
	claimedReview, ok, err := s.ClaimNextDCPModelAction(ctx, time.Unix(221, 0).UTC())
	if err != nil || !ok || claimedReview.ID != reviewA.ID {
		t.Fatalf("claim first review = %+v, %v, %v", claimedReview, ok, err)
	}
	if started, startErr := s.StartDCPModelAction(ctx, claimedReview, "review-handle-a", "review-run-a", time.Unix(222, 0).UTC()); startErr != nil || !started {
		t.Fatalf("start first review = %v, %v", started, startErr)
	}
	first, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, first.TaskID)
	first.RepairCount, first.State = 1, domain.DCPPolicyRepairQueued
	repair := domain.DCPModelAction{
		ID: "dcp-model-future-1-worker-2", TaskID: first.TaskID, SessionID: first.SessionID,
		Kind: domain.DCPActionRepairWorker, ExactHeadSHA: headA, Status: domain.DCPActionQueued,
		CreatedAt: time.Unix(223, 0).UTC(), UpdatedAt: time.Unix(223, 0).UTC(),
	}
	if finished, finishErr := s.FinishDCPModelActionAndQueue(ctx, claimedReview, first, repair, time.Unix(223, 0).UTC()); finishErr != nil || !finished {
		t.Fatalf("finish first review and queue bounded repair = %v, %v", finished, finishErr)
	}
	claimedRepair, ok, err := s.ClaimNextDCPModelAction(ctx, time.Unix(225, 0).UTC())
	if err != nil || !ok || claimedRepair.ID != repair.ID {
		t.Fatalf("claim repair = %+v, %v, %v", claimedRepair, ok, err)
	}
	if started, startErr := s.StartDCPModelAction(ctx, claimedRepair, "repair-launch", "", time.Unix(225, 0).UTC()); startErr != nil || !started {
		t.Fatalf("start repair = %v, %v", started, startErr)
	}
	first, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, first.TaskID)
	first.State = domain.DCPPolicyCIWaiting
	if finished, finishErr := s.FinishDCPModelAction(ctx, claimedRepair, first, domain.DCPActionSucceeded, "", time.Unix(226, 0).UTC()); finishErr != nil || !finished {
		t.Fatalf("finish repair = %v, %v", finished, finishErr)
	}
	first, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, first.TaskID)
	headB := strings.Repeat("b", 40)
	reviewB := domain.DCPModelAction{
		ID: "dcp-model-future-1-review-2", TaskID: first.TaskID, SessionID: first.SessionID,
		Kind: domain.DCPActionReviewer, ExactHeadSHA: headB, Status: domain.DCPActionQueued,
		CreatedAt: time.Unix(227, 0).UTC(), UpdatedAt: time.Unix(227, 0).UTC(),
	}
	next = first
	next.State, next.PreviousHeadSHA, next.CurrentHeadSHA = domain.DCPPolicyReviewQueued, headA, headB
	if _, created, err := s.QueueDCPModelAction(ctx, first, next, reviewB); err != nil || !created {
		t.Fatalf("queue fresh-head review = %v, %v", created, err)
	}
	actions, err := s.ListDCPModelActions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var reviewerHeads []string
	for _, action := range actions {
		if action.TaskID == first.TaskID && action.Kind == domain.DCPActionReviewer {
			reviewerHeads = append(reviewerHeads, action.ExactHeadSHA)
		}
	}
	if len(reviewerHeads) != 2 || reviewerHeads[0] != headA || reviewerHeads[1] != headB {
		t.Fatalf("fresh reviews per exact head = %v", reviewerHeads)
	}

	// Use two other future identities to prove the shared admission table is
	// FIFO and that policy terminal state plus merge lease completion survive a
	// restart without a second terminal mutation.
	admissions := make([]domain.DCPReviewLabAdmission, 0, 3)
	for i, source := range tasks[1:4] {
		task, _, _ := s.GetDCPReviewLabPolicyTaskByTaskID(ctx, source.TaskID)
		head := strings.Repeat(string(rune('c'+i)), 40)
		runID := fmt.Sprintf("policy-run-%d", i+1)
		requested := seedPolicyAdmissionFacts(t, s, task, runID, head, i+31)
		projection := task
		projection.State, projection.CurrentHeadSHA, projection.ReviewRunID = domain.DCPPolicyAdmissionWait, head, runID
		projection.UpdatedAt = time.Unix(int64(240+i), 0).UTC()
		if updated, updateErr := s.UpdateDCPReviewLabPolicyTaskCAS(ctx, task, projection); updateErr != nil || !updated {
			t.Fatalf("prepare policy admission %d = %v, %v", i, updated, updateErr)
		}
		task, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
		admission, created, enqueueErr := s.EnqueueDCPReviewLabPolicyAdmission(ctx, requested, task)
		if enqueueErr != nil || !created {
			t.Fatalf("atomically bind policy admission %d = %+v, %v, %v", i, admission, created, enqueueErr)
		}
		bound, _, _ := s.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
		if bound.AdmissionID != admission.ID {
			t.Fatalf("policy admission binding %d = %q, want %q", i, bound.AdmissionID, admission.ID)
		}
		admissions = append(admissions, admission)
	}
	if admissions[0].Sequence >= admissions[1].Sequence {
		t.Fatalf("policy FIFO sequence = %d, %d", admissions[0].Sequence, admissions[1].Sequence)
	}
	claimedAdmission, ok, err := s.GetNextWaitingDCPReviewLabAdmission(ctx)
	if err != nil || !ok || claimedAdmission.ID != admissions[0].ID {
		t.Fatalf("first policy admission = %+v, %v, %v", claimedAdmission, ok, err)
	}
	if claimed, claimErr := s.ClaimDCPReviewLabAdmission(ctx, claimedAdmission, "merge-"+claimedAdmission.ID, claimedAdmission.ReviewBaseSHA, time.Unix(300, 0).UTC()); claimErr != nil || !claimed {
		t.Fatalf("claim policy admission = %v, %v", claimed, claimErr)
	}
	claimedAdmission, _, _ = s.GetClaimedDCPReviewLabAdmission(ctx)
	terminalTask, _, _ := s.GetDCPReviewLabPolicyTaskByTaskID(ctx, tasks[1].TaskID)
	mergeSHA := strings.Repeat("f", 40)
	if completed, completeErr := s.CompleteDCPReviewLabPolicyAdmission(ctx, claimedAdmission, terminalTask, mergeSHA, time.Unix(301, 0).UTC()); completeErr != nil || !completed {
		t.Fatalf("complete policy admission = %v, %v", completed, completeErr)
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
	terminalTask, found, err := s.GetDCPReviewLabPolicyTaskByTaskID(ctx, tasks[1].TaskID)
	if err != nil || !found || terminalTask.State != domain.DCPPolicyMerged || terminalTask.MergeCommitSHA != mergeSHA {
		t.Fatalf("terminal task after restart = %+v, %v, %v", terminalTask, found, err)
	}
	terminalAdmission, found, err := s.GetDCPReviewLabAdmissionByID(ctx, admissions[0].ID)
	if err != nil || !found || terminalAdmission.Status != domain.DCPAdmissionSucceeded || terminalAdmission.MergeCommitSHA != mergeSHA {
		t.Fatalf("terminal admission after restart = %+v, %v, %v", terminalAdmission, found, err)
	}
	if completed, duplicateErr := s.CompleteDCPReviewLabPolicyAdmission(ctx, claimedAdmission, terminalTask, mergeSHA, time.Unix(302, 0).UTC()); duplicateErr == nil || completed {
		t.Fatalf("duplicate terminal completion = %v, %v", completed, duplicateErr)
	}
	nextAdmission, ok, err := s.GetNextWaitingDCPReviewLabAdmission(ctx)
	if err != nil || !ok || nextAdmission.ID != admissions[1].ID {
		t.Fatalf("next FIFO admission after restart = %+v, %v, %v", nextAdmission, ok, err)
	}
	incidentTask, _, _ := s.GetDCPReviewLabPolicyTaskByTaskID(ctx, tasks[2].TaskID)
	packet := `{"schemaVersion":"dcp.review-lab.arbiter-needed/v1","reason":"fixture_ambiguity"}`
	if recorded, recordErr := s.RecordDCPReviewLabPolicyIncident(ctx, nextAdmission, incidentTask, "dcp-incident-"+nextAdmission.ID, nextAdmission.ReviewBaseSHA, "fixture_ambiguity", packet, time.Unix(303, 0).UTC()); recordErr != nil || !recorded {
		t.Fatalf("atomic policy incident = %v, %v", recorded, recordErr)
	}
	incidentTask, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, tasks[2].TaskID)
	incidentAdmission, _, _ := s.GetDCPReviewLabAdmissionByID(ctx, nextAdmission.ID)
	if incidentTask.State != domain.DCPPolicyIncident || incidentTask.IncidentPacket != packet || incidentAdmission.Status != domain.DCPAdmissionIncident || incidentAdmission.IncidentPacket != packet {
		t.Fatalf("atomic incident projections = task %+v admission %+v", incidentTask, incidentAdmission)
	}
	afterIncident, ok, err := s.GetNextWaitingDCPReviewLabAdmission(ctx)
	if err != nil || !ok || afterIncident.ID != admissions[2].ID {
		t.Fatalf("terminal policy incident did not release later FIFO waiter = %+v, %v, %v", afterIncident, ok, err)
	}
}

func policyTaskFixture(taskID string, now time.Time) domain.DCPReviewLabPolicyTask {
	return domain.DCPReviewLabPolicyTask{
		TaskID: taskID, PayloadJSON: fmt.Sprintf(`{"schemaVersion":"%s","taskId":"%s"}`, domain.DCPReviewLabPolicyVersion, taskID),
		PayloadDigest: strings.Repeat("a", 64), Target: "dcp-review-lab", Profile: "synthetic-pr",
		Repository: "orenvlad-ai/dcp-review-lab", PolicyVersion: domain.DCPReviewLabPolicyVersion,
		Prompt: "add one bounded synthetic fixture", State: domain.DCPPolicyReserved, Revision: 1,
		CreatedAt: now, UpdatedAt: now,
	}
}

func finishPolicyWorker(t *testing.T, s *sqlite.Store, action domain.DCPModelAction, state domain.DCPReviewLabPolicyState, now time.Time) {
	t.Helper()
	task, found, err := s.GetDCPReviewLabPolicyTaskByTaskID(context.Background(), action.TaskID)
	if err != nil || !found {
		t.Fatalf("worker task = %+v, %v, %v", task, found, err)
	}
	task.State = state
	if finished, finishErr := s.FinishDCPModelAction(context.Background(), action, task, domain.DCPActionSucceeded, "", now); finishErr != nil || !finished {
		t.Fatalf("finish worker %s = %v, %v", action.ID, finished, finishErr)
	}
}

func seedPolicyAdmissionFacts(t *testing.T, s *sqlite.Store, task domain.DCPReviewLabPolicyTask, runID, head string, number int) domain.DCPReviewLabAdmission {
	t.Helper()
	ctx := context.Background()
	now := time.Unix(int64(230+number), 0).UTC()
	reviewID := "review-" + runID
	handleID := "handle-" + runID
	if err := s.UpsertReview(ctx, domain.Review{ID: reviewID, SessionID: task.SessionID, ProjectID: "dcp-review-lab", Harness: domain.ReviewerCodex, ReviewerHandleID: handleID, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	prURL := fmt.Sprintf("https://github.com/orenvlad-ai/dcp-review-lab/pull/%d", number)
	if err := s.WriteSCMObservation(ctx, domain.PullRequest{
		URL: prURL, SessionID: task.SessionID, Number: number, Provider: "github", Host: "github.com",
		Repo: "orenvlad-ai/dcp-review-lab", HeadSHA: head, BaseSHA: strings.Repeat("9", 40),
		SourceBranch: task.SourceBranch, TargetBranch: "main", ProviderState: "OPEN", UpdatedAt: now, ObservedAt: now,
	}, nil, nil, nil, nil, ports.ReviewWritePreserve); err != nil {
		t.Fatal(err)
	}
	run := domain.ReviewRun{
		ID: runID, ReviewID: reviewID, SessionID: task.SessionID, BatchID: "batch-" + runID,
		Harness: domain.ReviewerCodex, PRURL: prURL, TargetSHA: head, Status: domain.ReviewRunRunning, CreatedAt: now,
	}
	if err := s.InsertReviewRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdateBoundReviewRunResult(ctx, reviewcore.StructuredResultExpected{
		WorkerSessionID: string(task.SessionID), ReviewerHandleID: handleID, BatchID: run.BatchID,
		RunID: run.ID, PRURL: run.PRURL, TargetSHA: run.TargetSHA,
	}, domain.VerdictApproved, "approved")
	if err != nil || !updated {
		t.Fatalf("complete policy review = %v, %v", updated, err)
	}
	return domain.DCPReviewLabAdmission{
		ID: "admission-" + runID, ReviewRunID: runID, ReviewID: reviewID, SessionID: task.SessionID,
		PRURL: prURL, PRNumber: int64(number), TargetSHA: head, ReviewBaseSHA: strings.Repeat("9", 40),
		CreatedAt: now, UpdatedAt: now,
	}
}
