package store_test

import (
	"context"
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

func TestDCPWBCReadmissionTwoSequentialMainAdvancesPreserveFIFOAndOneInitialWorker(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "wb-core")
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	spec, ok := domain.DCPPolicyTarget("wb-core", "repo-only")
	if !ok {
		t.Fatal("wb-core repo-only spec unavailable")
	}
	task := domain.DCPReviewLabPolicyTask{
		TaskID: "wbc-store-v1", PayloadJSON: `{"schemaVersion":"dcp.wb-core.repo-only.release-train/v1","taskId":"wbc-store-v1"}`,
		PayloadDigest: strings.Repeat("a", 64), Target: spec.Target, Profile: spec.Profile,
		Repository: spec.Repository, PolicyVersion: spec.PolicyVersion, Prompt: "add inert store fixture",
		State: domain.DCPPolicyReserved, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	seed := domain.SessionRecord{
		ProjectID: "wb-core", Kind: domain.KindWorker, Harness: domain.HarnessCodex,
		DisplayName: "DCP:wbc-store-v1", Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: now},
		CreatedAt: now, UpdatedAt: now,
	}
	reserved, err := s.ReserveDCPReviewLabPolicyTask(ctx, task, seed, filepath.Join(t.TempDir(), "worktrees"))
	if err != nil || !reserved.Created || reserved.Task.SessionID != "wb-core-1" {
		t.Fatalf("reserve=%+v err=%v", reserved, err)
	}
	task = reserved.Task
	queued := task
	queued.State, queued.UpdatedAt = domain.DCPPolicyWorkerQueued, now.Add(time.Second)
	if changed, updateErr := s.UpdateDCPReviewLabPolicyTaskCAS(ctx, task, queued); updateErr != nil || !changed {
		t.Fatalf("queue initial worker=%v %v", changed, updateErr)
	}
	worker, claimed, err := s.ClaimNextDCPModelAction(ctx, now.Add(2*time.Second))
	if err != nil || !claimed || worker.Kind != domain.DCPActionInitialWorker {
		t.Fatalf("claim initial worker=%+v %v %v", worker, claimed, err)
	}
	if started, startErr := s.StartDCPModelAction(ctx, worker, "worker-once", "", now.Add(3*time.Second)); startErr != nil || !started {
		t.Fatalf("start initial worker=%v %v", started, startErr)
	}
	task, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
	workerDone := task
	workerDone.State = domain.DCPPolicyCIWaiting
	if finished, finishErr := s.FinishDCPModelAction(ctx, worker, workerDone, domain.DCPActionSucceeded, "", now.Add(4*time.Second)); finishErr != nil || !finished {
		t.Fatalf("finish initial worker=%v %v", finished, finishErr)
	}

	firstHead := strings.Repeat("1", 40)
	firstAdmission := approveWBCStoreHead(t, s, task.TaskID, "first", firstHead, now.Add(10*time.Second), nil)
	firstIncident := incidentWBCStoreAdmission(t, s, task.TaskID, firstAdmission, now.Add(20*time.Second))
	firstMain := strings.Repeat("2", 40)
	firstPreparedMain := strings.Repeat("8", 40)
	firstGeneration := observeAndAdvanceWBCStoreGeneration(t, s, firstIncident, firstAdmission, firstMain, firstPreparedMain, strings.Repeat("3", 40), strings.Repeat("4", 40), 1001, now.Add(30*time.Second))
	if firstGeneration.CurrentMainSHA != firstPreparedMain {
		t.Fatalf("provider main advance was not bound at prepare: %+v", firstGeneration)
	}
	secondAdmission := approveWBCStoreHead(t, s, task.TaskID, "second", firstGeneration.NewHeadSHA, now.Add(40*time.Second), &firstGeneration)
	secondIncident := incidentWBCStoreAdmission(t, s, task.TaskID, secondAdmission, now.Add(50*time.Second))

	secondMain := strings.Repeat("5", 40)
	secondGeneration := observeAndAdvanceWBCStoreGeneration(t, s, secondIncident, secondAdmission, secondMain, secondMain, strings.Repeat("6", 40), strings.Repeat("7", 40), 1002, now.Add(60*time.Second))
	thirdAdmission := approveWBCStoreHead(t, s, task.TaskID, "third", secondGeneration.NewHeadSHA, now.Add(70*time.Second), &secondGeneration)
	task, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
	claimed, err = s.ClaimDCPReleaseTrainAdmission(ctx, thirdAdmission, task, "release-third", thirdAdmission.ReviewBaseSHA, now.Add(80*time.Second))
	if err != nil || !claimed {
		t.Fatalf("third admission was blocked by superseded incident evidence: claimed=%v err=%v", claimed, err)
	}

	generations, err := s.ListDCPWBCReadmissionGenerations(ctx)
	if err != nil || len(generations) != 2 {
		t.Fatalf("generations=%+v err=%v", generations, err)
	}
	if generations[0].Status != domain.DCPWBCReadmissionFailed || generations[0].ErrorCode != "superseded_by_readmission" ||
		generations[0].AdmissionID != secondAdmission.ID || generations[1].Status != domain.DCPWBCReadmissionReleaseWait ||
		generations[1].AdmissionID != thirdAdmission.ID {
		t.Fatalf("sequential generation states=%+v", generations)
	}
	actions, err := s.ListDCPModelActions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	workers, reviewers := 0, 0
	for _, action := range actions {
		if action.TaskID != task.TaskID {
			continue
		}
		switch action.Kind {
		case domain.DCPActionInitialWorker:
			workers++
		case domain.DCPActionReviewer:
			reviewers++
		}
	}
	if workers != 1 || reviewers != 3 {
		t.Fatalf("action continuity workers=%d reviewers=%d actions=%+v", workers, reviewers, actions)
	}
}

func approveWBCStoreHead(t *testing.T, s *sqlite.Store, taskID, suffix, head string, now time.Time, generation *domain.DCPWBCReadmissionGeneration) domain.DCPReviewLabAdmission {
	t.Helper()
	ctx := context.Background()
	task, found, err := s.GetDCPReviewLabPolicyTaskByTaskID(ctx, taskID)
	if err != nil || !found || task.State != domain.DCPPolicyCIWaiting {
		t.Fatalf("review source task=%+v found=%v err=%v", task, found, err)
	}
	action := domain.DCPModelAction{
		ID: "dcp-model-" + taskID + "-review-" + suffix, TaskID: taskID, SessionID: task.SessionID,
		Kind: domain.DCPActionReviewer, ExactHeadSHA: head, Status: domain.DCPActionQueued, CreatedAt: now, UpdatedAt: now,
	}
	prURL := "https://github.com/orenvlad-ai/wb-core/pull/987"
	next := task
	next.State, next.PreviousHeadSHA, next.CurrentHeadSHA = domain.DCPPolicyReviewQueued, task.CurrentHeadSHA, head
	next.PRURL, next.PRNumber = prURL, 987
	if generation == nil {
		if _, created, queueErr := s.QueueDCPModelAction(ctx, task, next, action); queueErr != nil || !created {
			t.Fatalf("queue initial review=%v %v", created, queueErr)
		}
	} else if _, created, queueErr := s.QueueDCPWBCReadmissionReview(ctx, task, next, action, *generation); queueErr != nil || !created {
		t.Fatalf("queue readmission review=%v %v", created, queueErr)
	}

	reviewID, runID, handleID := "review-wbc-store", "run-"+suffix, "reviewer-wbc-store"
	if err := s.UpsertReview(ctx, domain.Review{
		ID: reviewID, SessionID: task.SessionID, ProjectID: "wb-core", Harness: domain.ReviewerCodex,
		ReviewerHandleID: handleID, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteSCMObservation(ctx, domain.PullRequest{
		URL: prURL, SessionID: task.SessionID, Number: 987, Provider: "github", Host: "github.com", Repo: "orenvlad-ai/wb-core",
		HeadSHA: head, BaseSHA: strings.Repeat("9", 40), SourceBranch: task.SourceBranch, TargetBranch: "main",
		ProviderState: "OPEN", UpdatedAt: now, ObservedAt: now,
	}, nil, nil, nil, nil, ports.ReviewWritePreserve); err != nil {
		t.Fatal(err)
	}
	run := domain.ReviewRun{
		ID: runID, ReviewID: reviewID, SessionID: task.SessionID, BatchID: "batch-" + suffix,
		Harness: domain.ReviewerCodex, PRURL: prURL, TargetSHA: head, Status: domain.ReviewRunRunning, CreatedAt: now,
	}
	if err := s.InsertReviewRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := s.ClaimNextDCPModelAction(ctx, now.Add(time.Second))
	if err != nil || !ok || claimed.ID != action.ID {
		t.Fatalf("claim review=%+v %v %v", claimed, ok, err)
	}
	if started, startErr := s.StartDCPModelAction(ctx, claimed, handleID, runID, now.Add(2*time.Second)); startErr != nil || !started {
		t.Fatalf("start review=%v %v", started, startErr)
	}
	task, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, taskID)
	running := task
	running.State, running.ReviewRunID = domain.DCPPolicyReviewRunning, runID
	if changed, updateErr := s.UpdateDCPReviewLabPolicyTaskCAS(ctx, task, running); updateErr != nil || !changed {
		t.Fatalf("mark review running=%v %v", changed, updateErr)
	}
	if updated, updateErr := s.UpdateBoundReviewRunResult(ctx, reviewcore.StructuredResultExpected{
		WorkerSessionID: string(task.SessionID), ReviewerHandleID: handleID, BatchID: run.BatchID,
		RunID: run.ID, PRURL: run.PRURL, TargetSHA: run.TargetSHA,
	}, domain.VerdictApproved, "approved"); updateErr != nil || !updated {
		t.Fatalf("approve review=%v %v", updated, updateErr)
	}
	task, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, taskID)
	approved := task
	approved.State = domain.DCPPolicyAdmissionWait
	claimed.ReviewRunID = runID
	if finished, finishErr := s.FinishDCPModelAction(ctx, claimed, approved, domain.DCPActionSucceeded, "", now.Add(3*time.Second)); finishErr != nil || !finished {
		t.Fatalf("finish review=%v %v", finished, finishErr)
	}
	task, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, taskID)
	admission, created, err := s.EnqueueDCPReviewLabPolicyAdmission(ctx, domain.DCPReviewLabAdmission{
		ID: "admission-" + runID, ReviewRunID: runID, ReviewID: reviewID, SessionID: task.SessionID,
		PRURL: prURL, PRNumber: 987, TargetSHA: head, ReviewBaseSHA: strings.Repeat("9", 40), CreatedAt: now.Add(4 * time.Second), UpdatedAt: now.Add(4 * time.Second),
	}, task)
	if err != nil || !created {
		t.Fatalf("enqueue admission=%+v %v %v", admission, created, err)
	}
	return admission
}

func incidentWBCStoreAdmission(t *testing.T, s *sqlite.Store, taskID string, admission domain.DCPReviewLabAdmission, now time.Time) domain.DCPReviewLabPolicyTask {
	t.Helper()
	ctx := context.Background()
	task, _, _ := s.GetDCPReviewLabPolicyTaskByTaskID(ctx, taskID)
	lease := "release-" + admission.ID
	claimed, err := s.ClaimDCPReleaseTrainAdmission(ctx, admission, task, lease, admission.ReviewBaseSHA, now)
	if err != nil || !claimed {
		t.Fatalf("claim release admission=%v %v", claimed, err)
	}
	admission, _, _ = s.GetDCPReviewLabAdmissionByID(ctx, admission.ID)
	task, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, taskID)
	packet := fmt.Sprintf(`{"schemaVersion":"dcp.review-lab.arbiter-needed/v1","admissionId":%q,"error":"release_state_drift"}`, admission.ID)
	recorded, err := s.RecordDCPReviewLabPolicyIncident(ctx, admission, task, lease, admission.ReviewBaseSHA, "release_state_drift", packet, now.Add(time.Second))
	if err != nil || !recorded {
		t.Fatalf("record release incident=%v %v", recorded, err)
	}
	task, _, _ = s.GetDCPReviewLabPolicyTaskByTaskID(ctx, taskID)
	return task
}

func observeAndAdvanceWBCStoreGeneration(t *testing.T, s *sqlite.Store, task domain.DCPReviewLabPolicyTask, admission domain.DCPReviewLabAdmission, main, preparedMain, tree, head string, commentID int64, now time.Time) domain.DCPWBCReadmissionGeneration {
	t.Helper()
	ctx := context.Background()
	admission, _, _ = s.GetDCPReviewLabAdmissionByID(ctx, admission.ID)
	row := domain.DCPWBCReadmissionGeneration{
		GenerationID: fmt.Sprintf("dcp-wbc-readmission-%s-%d", task.TaskID, commentID), MarkerDigest: strings.Repeat(fmt.Sprintf("%d", commentID%10), 64),
		MarkerVersion: "wb-core.dcp-release-handoff/v2", MarkerCommentID: commentID, MarkerAuthor: "github-actions[bot]",
		MarkerCreatedAt: now, MarkerUpdatedAt: now, MarkerMainSHA: main, TaskID: task.TaskID, SessionID: task.SessionID, OldAdmissionID: admission.ID,
		PRURL: task.PRURL, PRNumber: task.PRNumber, Repository: task.Repository, BaseBranch: "main", Scope: task.Profile,
		HeadRef: task.SourceBranch, SessionNumber: task.CardNumber, AdmittedHeadSHA: admission.TargetSHA,
		AdmittedBaseSHA: admission.AdmittedBaseSHA, ObservedHeadSHA: admission.TargetSHA, CurrentMainSHA: main,
		ReadyEventID: commentID + 1, AdmissionCheckID: commentID + 2, HandoffProofID: commentID + 3,
		Reason: "base-behind-after-admission", Status: domain.DCPWBCReadmissionObserved, CreatedAt: now, UpdatedAt: now,
	}
	observed, created, err := s.ObserveDCPWBCReadmissionGeneration(ctx, row, task, admission)
	if err != nil || !created {
		t.Fatalf("observe generation=%+v %v %v", observed, created, err)
	}
	lease := fmt.Sprintf("readmission-lease-%d", commentID)
	if claimed, claimErr := s.ClaimDCPWBCReadmissionGeneration(ctx, observed, lease, now.Add(time.Second)); claimErr != nil || !claimed {
		t.Fatalf("claim generation=%v %v", claimed, claimErr)
	}
	observed.Status, observed.LeaseID = domain.DCPWBCReadmissionClaimed, lease
	if prepared, prepareErr := s.PrepareDCPWBCReadmissionGeneration(ctx, observed, tree, head, preparedMain, now.Add(2*time.Second)); prepareErr != nil || !prepared {
		t.Fatalf("prepare generation=%v %v", prepared, prepareErr)
	}
	observed.Status, observed.MergeTreeSHA, observed.NewHeadSHA, observed.CurrentMainSHA = domain.DCPWBCReadmissionPrepared, tree, head, preparedMain
	if advanced, advanceErr := s.AdvanceDCPWBCReadmissionHead(ctx, observed, task, now.Add(3*time.Second)); advanceErr != nil || !advanced {
		t.Fatalf("advance generation=%v %v", advanced, advanceErr)
	}
	observed.Status = domain.DCPWBCReadmissionHeadPushed
	return observed
}
