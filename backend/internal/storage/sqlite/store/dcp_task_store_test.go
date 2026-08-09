package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	store "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func openDCPTaskStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.UpsertProject(context.Background(), domain.ProjectRecord{
		ID:           "dcp-lab",
		Path:         "/tmp/dcp-lab",
		DisplayName:  "DCP Lab",
		RegisteredAt: time.Unix(100, 0).UTC(),
		Kind:         domain.ProjectKindSingleRepo,
	}); err != nil {
		t.Fatalf("seed dcp-lab project: %v", err)
	}
	return s
}

func sampleDCPTask() (domain.DCPTask, domain.DCPTaskEvent) {
	now := time.Unix(200, 0).UTC()
	task := domain.DCPTask{
		ID:             "dcp-task-1",
		IdempotencyKey: "submit-1",
		ApprovedTask: domain.DCPApprovedTask{
			SchemaVersion: "dcp.task/v1",
			Title:         "Synthetic task",
			Description:   "Store only",
		},
		ApprovedScope: domain.DCPApprovedScope{
			SchemaVersion: "dcp.scope/v1",
			Statement:     "No execution",
		},
		ApprovedDigest: strings.Repeat("a", 64),
		Target: domain.DCPRepositoryIdentity{
			SchemaVersion:  "dcp.repository/v1",
			ProjectID:      "dcp-lab",
			Repository:     "dcp-lab",
			Path:           "/tmp/dcp-lab",
			HeadSHA:        strings.Repeat("b", 40),
			MarkerDigest:   strings.Repeat("c", 64),
			IdentityDigest: strings.Repeat("d", 64),
		},
		State:     domain.DCPTaskSubmitted,
		Revision:  1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	event := domain.DCPTaskEvent{
		TaskID:          task.ID,
		Sequence:        1,
		EventID:         "dcp-event-1",
		SchemaVersion:   "dcp.event/v1",
		EventType:       "task.submitted",
		SourceKind:      "daemon",
		SourceID:        "dcp-daemon",
		CorrelationID:   string(task.ID),
		IdempotencyKey:  task.IdempotencyKey,
		ToState:         task.State,
		TaskRevision:    task.Revision,
		OccurredAt:      now,
		RecordedAt:      now,
		Payload:         `{"schemaVersion":"dcp.event.payload/v1"}`,
		EvidenceDigest:  strings.Repeat("e", 64),
		IntegrityDigest: strings.Repeat("f", 64),
	}
	return task, event
}

func TestDCPTaskSubmitIsAtomicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	s := openDCPTaskStore(t)
	task, event := sampleDCPTask()

	first, err := s.SubmitDCPTask(ctx, task, event)
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if !first.Created || first.Task.ID != task.ID {
		t.Fatalf("first submit = %+v, want created task %s", first, task.ID)
	}

	duplicateTask := task
	duplicateTask.ID = "must-not-replace-stable-id"
	// The request's canonical payload is unchanged even if a later validation
	// observes another physical snapshot; idempotency returns the first durable
	// identity rather than rebinding the key or creating another event.
	duplicateTask.Target.HeadSHA = strings.Repeat("9", 40)
	duplicateTask.Target.IdentityDigest = strings.Repeat("8", 64)
	duplicateEvent := event
	duplicateEvent.TaskID = duplicateTask.ID
	duplicateEvent.EventID = "must-not-create-event"
	duplicate, err := s.SubmitDCPTask(ctx, duplicateTask, duplicateEvent)
	if err != nil {
		t.Fatalf("identical duplicate: %v", err)
	}
	if duplicate.Created || duplicate.Task.ID != task.ID {
		t.Fatalf("duplicate = %+v, want existing task %s", duplicate, task.ID)
	}
	events, err := s.ListDCPTaskEvents(ctx, task.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 || events[0].EventID != event.EventID {
		t.Fatalf("events = %+v, want exactly the original event", events)
	}

	conflict := task
	conflict.ApprovedTask.Description = "different canonical command"
	if _, err := s.SubmitDCPTask(ctx, conflict, event); !errors.Is(err, store.ErrDCPIdempotencyConflict) {
		t.Fatalf("conflicting duplicate err = %v, want idempotency conflict", err)
	}
	stored, err := s.GetDCPTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get after conflict: %v", err)
	}
	if stored.ApprovedTask.Description != task.ApprovedTask.Description {
		t.Fatalf("conflict mutated task: %+v", stored.ApprovedTask)
	}
}

func TestDCPTaskTransitionRejectsStaleRevisionAndRollsBackEventFailure(t *testing.T) {
	ctx := context.Background()
	s := openDCPTaskStore(t)
	task, event := sampleDCPTask()
	if _, err := s.SubmitDCPTask(ctx, task, event); err != nil {
		t.Fatalf("submit: %v", err)
	}

	nextAt := task.UpdatedAt.Add(time.Second)
	next := domain.DCPTaskEvent{
		EventID:         "dcp-event-2",
		SchemaVersion:   "dcp.event/v1",
		EventType:       "system.reconciled",
		SourceKind:      "daemon",
		SourceID:        "dcp-daemon-startup",
		CorrelationID:   string(task.ID),
		CausationID:     event.EventID,
		IdempotencyKey:  "reconcile-2",
		ToState:         domain.DCPTaskSubmitted,
		OccurredAt:      nextAt,
		RecordedAt:      nextAt,
		Payload:         `{"schemaVersion":"dcp.event.payload/v1"}`,
		EvidenceDigest:  strings.Repeat("1", 64),
		IntegrityDigest: strings.Repeat("2", 64),
	}
	if err := s.TransitionDCPTaskCAS(ctx, task.ID, task.State, 1, next); err != nil {
		t.Fatalf("valid CAS: %v", err)
	}
	stored, err := s.GetDCPTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get after CAS: %v", err)
	}
	if stored.Revision != 2 || !stored.UpdatedAt.Equal(nextAt) {
		t.Fatalf("task after CAS = %+v, want revision 2", stored)
	}
	events, err := s.ListDCPTaskEvents(ctx, task.ID)
	if err != nil {
		t.Fatalf("events after CAS: %v", err)
	}
	if len(events) != 2 || events[1].Sequence != 2 || events[1].TaskRevision != 2 {
		t.Fatalf("events after CAS = %+v", events)
	}

	stale := next
	stale.EventID = "stale-event"
	stale.IdempotencyKey = "stale-event"
	if err := s.TransitionDCPTaskCAS(ctx, task.ID, task.State, 1, stale); !errors.Is(err, store.ErrDCPStaleRevision) {
		t.Fatalf("stale CAS err = %v, want stale revision", err)
	}

	invalidEvent := next
	invalidEvent.EventID = "invalid-event"
	invalidEvent.IdempotencyKey = "invalid-event"
	invalidEvent.Payload = "not-json"
	invalidEvent.RecordedAt = nextAt.Add(time.Second)
	invalidEvent.OccurredAt = invalidEvent.RecordedAt
	if err := s.TransitionDCPTaskCAS(ctx, task.ID, task.State, 2, invalidEvent); err == nil {
		t.Fatal("CAS with invalid event unexpectedly succeeded")
	}
	stored, err = s.GetDCPTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get after rollback: %v", err)
	}
	events, err = s.ListDCPTaskEvents(ctx, task.ID)
	if err != nil {
		t.Fatalf("events after rollback: %v", err)
	}
	if stored.Revision != 2 || len(events) != 2 {
		t.Fatalf("failed event was not atomic: revision=%d events=%d", stored.Revision, len(events))
	}
	if err := s.ValidateDCPTaskSchema(ctx); err != nil {
		t.Fatalf("validate schema: %v", err)
	}
}
