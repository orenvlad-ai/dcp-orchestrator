package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var (
	// ErrDCPIdempotencyConflict means an existing key is bound to different
	// canonical approved content or logical target.
	ErrDCPIdempotencyConflict = errors.New("dcp task idempotency key conflict")
	// ErrDCPStaleRevision means the expected state/revision no longer matches.
	ErrDCPStaleRevision = errors.New("dcp task stale revision")
	// ErrDCPTaskNotFound means no durable DCP task has the requested id.
	ErrDCPTaskNotFound = errors.New("dcp task not found")
)

// DCPSubmitResult reports whether SubmitDCPTask created the task or returned
// the existing task for an identical idempotent command.
type DCPSubmitResult struct {
	Task    domain.DCPTask
	Created bool
}

// SubmitDCPTask atomically persists the task and its first event. The caller
// supplies already validated canonical content; this layer owns concurrency,
// idempotency, and transactionality.
func (s *Store) SubmitDCPTask(ctx context.Context, task domain.DCPTask, event domain.DCPTaskEvent) (DCPSubmitResult, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	var result DCPSubmitResult
	err := s.inTx(ctx, "submit dcp task", func(q *gen.Queries) error {
		existing, err := q.GetDCPTaskByIdempotencyKey(ctx, task.IdempotencyKey)
		switch {
		case err == nil:
			if !sameDCPTaskCommand(existing, task) {
				return ErrDCPIdempotencyConflict
			}
			mapped, err := dcpTaskFromGen(existing)
			if err != nil {
				return err
			}
			result = DCPSubmitResult{Task: mapped, Created: false}
			return nil
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("read idempotency key: %w", err)
		}

		approvedTaskJSON, err := json.Marshal(task.ApprovedTask)
		if err != nil {
			return fmt.Errorf("encode approved task: %w", err)
		}
		approvedScopeJSON, err := json.Marshal(task.ApprovedScope)
		if err != nil {
			return fmt.Errorf("encode approved scope: %w", err)
		}
		if err := q.InsertDCPTask(ctx, gen.InsertDCPTaskParams{
			TaskID:               task.ID,
			IdempotencyKey:       task.IdempotencyKey,
			ApprovedTaskJson:     string(approvedTaskJSON),
			ApprovedScopeJson:    string(approvedScopeJSON),
			ApprovedDigest:       task.ApprovedDigest,
			TargetProjectID:      task.Target.ProjectID,
			TargetRepository:     task.Target.Repository,
			TargetPath:           task.Target.Path,
			TargetHeadSha:        task.Target.HeadSHA,
			TargetMarkerDigest:   task.Target.MarkerDigest,
			TargetIdentityDigest: task.Target.IdentityDigest,
			State:                task.State,
			Revision:             task.Revision,
			CreatedAt:            task.CreatedAt,
			UpdatedAt:            task.UpdatedAt,
		}); err != nil {
			return fmt.Errorf("insert task: %w", err)
		}
		if err := q.InsertDCPTaskEvent(ctx, dcpEventParams(event)); err != nil {
			return fmt.Errorf("insert submitted event: %w", err)
		}
		result = DCPSubmitResult{Task: task, Created: true}
		return nil
	})
	if err != nil {
		return DCPSubmitResult{}, err
	}
	return result, nil
}

// TransitionDCPTaskCAS is the bounded storage primitive that guarantees a
// state/revision compare-and-set and event append commit together. I11 exposes
// no controller or service action that calls it; the only valid physical state
// remains SUBMITTED. Keeping the primitive here lets migration/store tests prove
// stale rejection without activating a worker or future role.
func (s *Store) TransitionDCPTaskCAS(
	ctx context.Context,
	taskID domain.DCPTaskID,
	expectedState domain.DCPTaskState,
	expectedRevision int64,
	event domain.DCPTaskEvent,
) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "transition dcp task", func(q *gen.Queries) error {
		rows, err := q.UpdateDCPTaskCAS(ctx, gen.UpdateDCPTaskCASParams{
			State:     event.ToState,
			UpdatedAt: event.RecordedAt,
			TaskID:    taskID,
			State_2:   expectedState,
			Revision:  expectedRevision,
		})
		if err != nil {
			return fmt.Errorf("compare-and-set task: %w", err)
		}
		if rows != 1 {
			return ErrDCPStaleRevision
		}
		sequence, err := q.NextDCPTaskEventSequence(ctx, taskID)
		if err != nil {
			return fmt.Errorf("next event sequence: %w", err)
		}
		event.TaskID = taskID
		event.Sequence = sequence
		event.FromState = &expectedState
		event.TaskRevision = expectedRevision + 1
		if err := q.InsertDCPTaskEvent(ctx, dcpEventParams(event)); err != nil {
			return fmt.Errorf("insert transition event: %w", err)
		}
		return nil
	})
}

func (s *Store) GetDCPTask(ctx context.Context, taskID domain.DCPTaskID) (domain.DCPTask, error) {
	row, err := s.qr.GetDCPTaskByID(ctx, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPTask{}, ErrDCPTaskNotFound
	}
	if err != nil {
		return domain.DCPTask{}, fmt.Errorf("get dcp task %s: %w", taskID, err)
	}
	return dcpTaskFromGen(row)
}

func (s *Store) ListDCPTasks(ctx context.Context, projectID string) ([]domain.DCPTask, error) {
	rows, err := s.qr.ListDCPTasks(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list dcp tasks: %w", err)
	}
	out := make([]domain.DCPTask, 0, len(rows))
	for _, row := range rows {
		task, err := dcpTaskFromGen(row)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, nil
}

func (s *Store) ListDCPTaskEvents(ctx context.Context, taskID domain.DCPTaskID) ([]domain.DCPTaskEvent, error) {
	if _, err := s.GetDCPTask(ctx, taskID); err != nil {
		return nil, err
	}
	rows, err := s.qr.ListDCPTaskEvents(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("list dcp task events %s: %w", taskID, err)
	}
	out := make([]domain.DCPTaskEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpEventFromGen(row))
	}
	return out, nil
}

// ValidateDCPTaskSchema checks the I11 state/event invariant at daemon startup.
// It is read-only and starts no process, timer, model, or reconciliation loop.
func (s *Store) ValidateDCPTaskSchema(ctx context.Context) error {
	tasks, err := s.ListDCPTasks(ctx, "")
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if task.State != domain.DCPTaskSubmitted || task.Revision < 1 {
			return fmt.Errorf("dcp task %s has unsupported state/revision", task.ID)
		}
		events, err := s.ListDCPTaskEvents(ctx, task.ID)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			return fmt.Errorf("dcp task %s has no event", task.ID)
		}
		for i, event := range events {
			if event.Sequence != int64(i+1) {
				return fmt.Errorf("dcp task %s event sequence is not contiguous", task.ID)
			}
			if event.TaskRevision != event.Sequence {
				return fmt.Errorf("dcp task %s event revision does not match sequence", task.ID)
			}
			if (event.Sequence == 1 && event.FromState != nil) || (event.Sequence > 1 && event.FromState == nil) {
				return fmt.Errorf("dcp task %s event state chain is invalid", task.ID)
			}
			if i > 0 && *event.FromState != events[i-1].ToState {
				return fmt.Errorf("dcp task %s event state chain is discontinuous", task.ID)
			}
		}
		last := events[len(events)-1]
		if last.ToState != task.State || last.TaskRevision != task.Revision {
			return fmt.Errorf("dcp task %s event head does not match task revision", task.ID)
		}
	}
	return nil
}

func sameDCPTaskCommand(row gen.DcpTask, task domain.DCPTask) bool {
	taskJSON, err := json.Marshal(task.ApprovedTask)
	if err != nil {
		return false
	}
	scopeJSON, err := json.Marshal(task.ApprovedScope)
	if err != nil {
		return false
	}
	return row.ApprovedDigest == task.ApprovedDigest &&
		row.ApprovedTaskJson == string(taskJSON) &&
		row.ApprovedScopeJson == string(scopeJSON) &&
		row.TargetProjectID == task.Target.ProjectID &&
		row.TargetRepository == task.Target.Repository
}

func dcpTaskFromGen(row gen.DcpTask) (domain.DCPTask, error) {
	var approvedTask domain.DCPApprovedTask
	if err := json.Unmarshal([]byte(row.ApprovedTaskJson), &approvedTask); err != nil {
		return domain.DCPTask{}, fmt.Errorf("decode dcp task %s approved task: %w", row.TaskID, err)
	}
	var approvedScope domain.DCPApprovedScope
	if err := json.Unmarshal([]byte(row.ApprovedScopeJson), &approvedScope); err != nil {
		return domain.DCPTask{}, fmt.Errorf("decode dcp task %s approved scope: %w", row.TaskID, err)
	}
	return domain.DCPTask{
		ID:             row.TaskID,
		IdempotencyKey: row.IdempotencyKey,
		ApprovedTask:   approvedTask,
		ApprovedScope:  approvedScope,
		ApprovedDigest: row.ApprovedDigest,
		Target: domain.DCPRepositoryIdentity{
			SchemaVersion:  "dcp.repository/v1",
			ProjectID:      row.TargetProjectID,
			Repository:     row.TargetRepository,
			Path:           row.TargetPath,
			HeadSHA:        row.TargetHeadSha,
			MarkerDigest:   row.TargetMarkerDigest,
			IdentityDigest: row.TargetIdentityDigest,
		},
		State:     row.State,
		Revision:  row.Revision,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}, nil
}

func dcpEventParams(event domain.DCPTaskEvent) gen.InsertDCPTaskEventParams {
	return gen.InsertDCPTaskEventParams{
		TaskID:          event.TaskID,
		Sequence:        event.Sequence,
		EventID:         event.EventID,
		SchemaVersion:   event.SchemaVersion,
		EventType:       event.EventType,
		SourceKind:      event.SourceKind,
		SourceID:        event.SourceID,
		CorrelationID:   event.CorrelationID,
		CausationID:     sql.NullString{String: event.CausationID, Valid: event.CausationID != ""},
		IdempotencyKey:  event.IdempotencyKey,
		FromState:       event.FromState,
		ToState:         event.ToState,
		TaskRevision:    event.TaskRevision,
		OccurredAt:      event.OccurredAt,
		RecordedAt:      event.RecordedAt,
		PayloadJson:     event.Payload,
		EvidenceDigest:  event.EvidenceDigest,
		IntegrityDigest: event.IntegrityDigest,
	}
}

func dcpEventFromGen(row gen.DcpTaskEvent) domain.DCPTaskEvent {
	return domain.DCPTaskEvent{
		TaskID:          row.TaskID,
		Sequence:        row.Sequence,
		EventID:         row.EventID,
		SchemaVersion:   row.SchemaVersion,
		EventType:       row.EventType,
		SourceKind:      row.SourceKind,
		SourceID:        row.SourceID,
		CorrelationID:   row.CorrelationID,
		CausationID:     row.CausationID.String,
		IdempotencyKey:  row.IdempotencyKey,
		FromState:       row.FromState,
		ToState:         row.ToState,
		TaskRevision:    row.TaskRevision,
		OccurredAt:      row.OccurredAt,
		RecordedAt:      row.RecordedAt,
		Payload:         row.PayloadJson,
		EvidenceDigest:  row.EvidenceDigest,
		IntegrityDigest: row.IntegrityDigest,
	}
}
