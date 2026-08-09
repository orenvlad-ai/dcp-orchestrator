package dcptask

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

type countingRepositoryValidator struct {
	calls    int
	identity domain.DCPRepositoryIdentity
	err      error
}

func (v *countingRepositoryValidator) Validate(context.Context, domain.ProjectRecord) (domain.DCPRepositoryIdentity, error) {
	v.calls++
	return v.identity, v.err
}

func validSubmitInput() SubmitInput {
	return SubmitInput{
		IdempotencyKey: "i11-synthetic-1",
		Target:         TargetRepository,
		ApprovedTask: domain.DCPApprovedTask{
			SchemaVersion: ApprovedTaskSchema,
			Title:         "Synthetic durable task",
			Description:   "Persist only; do not execute",
		},
		ApprovedScope: domain.DCPApprovedScope{
			SchemaVersion: ApprovedScopeSchema,
			Statement:     "Model-free I11 storage proof",
		},
	}
}

func requireAPIError(t *testing.T, err error, kind apierr.Kind, code string) {
	t.Helper()
	var got *apierr.Error
	if !errors.As(err, &got) {
		t.Fatalf("err = %v, want *apierr.Error", err)
	}
	if got.Kind != kind || got.Code != code {
		t.Fatalf("api error = %+v, want kind=%v code=%s", got, kind, code)
	}
}

func TestSubmitRejectsMalformedOrOutOfScopeBeforeMutation(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.UpsertProject(ctx, testProject(t.TempDir())); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	validator := &countingRepositoryValidator{}
	svc := New(Deps{Store: store, Repository: validator})

	outOfScope := validSubmitInput()
	outOfScope.Target = "real-repository"
	_, err = svc.Submit(ctx, outOfScope)
	requireAPIError(t, err, apierr.KindInvalid, "DCP_TARGET_INVALID")
	if validator.calls != 0 {
		t.Fatalf("repository validator calls = %d, want 0", validator.calls)
	}

	malformed := validSubmitInput()
	malformed.ApprovedTask.Title = "   "
	_, err = svc.Submit(ctx, malformed)
	requireAPIError(t, err, apierr.KindInvalid, "DCP_TASK_INVALID")
	if validator.calls != 0 {
		t.Fatalf("repository validator calls after malformed input = %d, want 0", validator.calls)
	}
	tasks, err := store.ListDCPTasks(ctx, "")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("invalid requests mutated storage: %+v", tasks)
	}
}

func TestModelFreeSubmitIdempotencyAndRestartPersistence(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repo := createDCPTestRepository(t)
	dataDir := filepath.Join(root, "data")
	worktreeRoot := filepath.Join(dataDir, "worktrees")
	store, err := sqlite.Open(dataDir)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	now := time.Unix(500, 0).UTC()
	if err := store.UpsertProject(ctx, testProject(repo)); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	session, err := store.CreateSession(ctx, domain.SessionRecord{
		ProjectID: domain.ProjectID(TargetProjectID),
		Kind:      domain.KindWorker,
		Harness:   domain.HarnessCodex,
		Activity: domain.Activity{
			State:          domain.ActivityIdle,
			LastActivityAt: now,
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("seed existing session: %v", err)
	}

	nextID := 0
	svc := New(Deps{
		Store: store,
		Repository: GitRepositoryValidator{
			TargetPath:          repo,
			AllowedWorktreeRoot: worktreeRoot,
		},
		Now: func() time.Time { return now },
		NewID: func(prefix string) string {
			nextID++
			return fmt.Sprintf("%s%02d", prefix, nextID)
		},
	})
	input := validSubmitInput()
	first, err := svc.Submit(ctx, input)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if first.Duplicate || first.Task.State != domain.DCPTaskSubmitted || first.Task.Revision != 1 {
		t.Fatalf("first submit = %+v", first)
	}
	duplicate, err := svc.Submit(ctx, input)
	if err != nil {
		t.Fatalf("duplicate submit: %v", err)
	}
	if !duplicate.Duplicate || duplicate.Task.ID != first.Task.ID {
		t.Fatalf("duplicate = %+v, want stable task %s", duplicate, first.Task.ID)
	}
	events, err := svc.Events(ctx, first.Task.ID)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(events) != 1 || events[0].EventType != "task.submitted" || events[0].Sequence != 1 {
		t.Fatalf("events = %+v", events)
	}

	conflict := input
	conflict.ApprovedScope.Statement = "different canonical scope"
	_, err = svc.Submit(ctx, conflict)
	requireAPIError(t, err, apierr.KindConflict, "DCP_IDEMPOTENCY_CONFLICT")
	events, err = svc.Events(ctx, first.Task.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("conflict mutated events: len=%d err=%v", len(events), err)
	}

	// Closing and reopening the only database models a daemon/app restart. The
	// service has no executor/model dependency and startup validation is read-only.
	if err := store.Close(); err != nil {
		t.Fatalf("close before restart: %v", err)
	}
	restarted, err := sqlite.Open(dataDir)
	if err != nil {
		t.Fatalf("reopen after restart: %v", err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	restartedSvc := New(Deps{
		Store: restarted,
		Repository: GitRepositoryValidator{
			TargetPath:          repo,
			AllowedWorktreeRoot: worktreeRoot,
		},
	})
	if err := restartedSvc.ValidateSchema(ctx); err != nil {
		t.Fatalf("startup schema validation: %v", err)
	}
	persisted, err := restartedSvc.Get(ctx, first.Task.ID)
	if err != nil {
		t.Fatalf("get after restart: %v", err)
	}
	persistedEvents, err := restartedSvc.Events(ctx, first.Task.ID)
	if err != nil {
		t.Fatalf("events after restart: %v", err)
	}
	preservedSession, ok, err := restarted.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("existing session after restart: %v", err)
	}
	if persisted.State != domain.DCPTaskSubmitted || persisted.Revision != 1 || len(persistedEvents) != 1 {
		t.Fatalf("task changed during restart: task=%+v events=%+v", persisted, persistedEvents)
	}
	if !ok || preservedSession.ID != session.ID || preservedSession.Harness != domain.HarnessCodex {
		t.Fatalf("existing session not preserved: %+v ok=%v", preservedSession, ok)
	}
}
