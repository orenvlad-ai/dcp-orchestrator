// Package dcptask implements the bounded model-free I11 task surface.
package dcptask

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

const (
	ApprovedTaskSchema  = "dcp.task/v1"
	ApprovedScopeSchema = "dcp.scope/v1"
	RepositorySchema    = "dcp.repository/v1"
	EventSchema         = "dcp.event/v1"
	TargetRepository    = "dcp-lab"
	TargetProjectID     = "dcp-lab"
	SourceID            = "internal-lab"
)

const (
	maxTitleBytes       = 160
	maxDescriptionBytes = 4096
	maxScopeBytes       = 4096
)

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// Store is the existing SQLite authority used by the I11 service.
type Store interface {
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
	SubmitDCPTask(ctx context.Context, task domain.DCPTask, event domain.DCPTaskEvent) (sqlitestore.DCPSubmitResult, error)
	GetDCPTask(ctx context.Context, taskID domain.DCPTaskID) (domain.DCPTask, error)
	ListDCPTasks(ctx context.Context, projectID string) ([]domain.DCPTask, error)
	ListDCPTaskEvents(ctx context.Context, taskID domain.DCPTaskID) ([]domain.DCPTaskEvent, error)
	ValidateDCPTaskSchema(ctx context.Context) error
}

// RepositoryValidator proves the one exact disposable target without mutation.
type RepositoryValidator interface {
	Validate(ctx context.Context, project domain.ProjectRecord) (domain.DCPRepositoryIdentity, error)
}

type Deps struct {
	Store      Store
	Repository RepositoryValidator
	Now        func() time.Time
	NewID      func(prefix string) string
}

type Service struct {
	store      Store
	repository RepositoryValidator
	now        func() time.Time
	newID      func(prefix string) string
}

func New(deps Deps) *Service {
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	newID := deps.NewID
	if newID == nil {
		newID = func(prefix string) string { return prefix + uuid.NewString() }
	}
	return &Service{store: deps.Store, repository: deps.Repository, now: now, newID: newID}
}

// SubmitInput is the semantic command accepted from the internal loopback lab
// surface. Target is a fixed slug, never a caller-controlled path.
type SubmitInput struct {
	IdempotencyKey string
	Target         string
	ApprovedTask   domain.DCPApprovedTask
	ApprovedScope  domain.DCPApprovedScope
}

type SubmitResult struct {
	Task      domain.DCPTask
	Duplicate bool
}

func (s *Service) Submit(ctx context.Context, in SubmitInput) (SubmitResult, error) {
	if s == nil || s.store == nil || s.repository == nil {
		return SubmitResult{}, apierr.Internal("DCP_TASK_SERVICE_UNAVAILABLE", "DCP task service is unavailable")
	}
	if err := validateSubmitInput(in); err != nil {
		return SubmitResult{}, err
	}

	project, ok, err := s.store.GetProject(ctx, TargetProjectID)
	if err != nil {
		return SubmitResult{}, apierr.Internal("DCP_TARGET_LOAD_FAILED", "DCP target could not be loaded")
	}
	if !ok || !project.ArchivedAt.IsZero() {
		return SubmitResult{}, apierr.Invalid("DCP_TARGET_INVALID", "The disposable dcp-lab target is not registered", nil)
	}
	target, err := s.repository.Validate(ctx, project)
	if err != nil {
		var validation *TargetValidationError
		if errors.As(err, &validation) {
			return SubmitResult{}, apierr.Invalid("DCP_TARGET_INVALID", validation.Error(), nil)
		}
		return SubmitResult{}, apierr.Internal("DCP_TARGET_VALIDATION_FAILED", "DCP target validation failed")
	}

	approvedDigest, err := digestJSON(struct {
		Task   domain.DCPApprovedTask  `json:"task"`
		Scope  domain.DCPApprovedScope `json:"scope"`
		Target string                  `json:"target"`
	}{Task: in.ApprovedTask, Scope: in.ApprovedScope, Target: TargetRepository})
	if err != nil {
		return SubmitResult{}, apierr.Internal("DCP_TASK_CANONICALIZATION_FAILED", "DCP task could not be canonicalized")
	}

	now := s.now().UTC()
	taskID := domain.DCPTaskID(s.newID("dcp_task_"))
	eventID := s.newID("dcp_event_")
	task := domain.DCPTask{
		ID:             taskID,
		IdempotencyKey: in.IdempotencyKey,
		ApprovedTask:   in.ApprovedTask,
		ApprovedScope:  in.ApprovedScope,
		ApprovedDigest: approvedDigest,
		Target:         target,
		State:          domain.DCPTaskSubmitted,
		Revision:       1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	payloadBytes, err := json.Marshal(struct {
		SchemaVersion        string `json:"schemaVersion"`
		ApprovedDigest       string `json:"approvedDigest"`
		TargetIdentityDigest string `json:"targetIdentityDigest"`
	}{
		SchemaVersion:        "dcp.task-submitted/v1",
		ApprovedDigest:       approvedDigest,
		TargetIdentityDigest: target.IdentityDigest,
	})
	if err != nil {
		return SubmitResult{}, apierr.Internal("DCP_EVENT_CANONICALIZATION_FAILED", "DCP event could not be canonicalized")
	}
	event := domain.DCPTaskEvent{
		TaskID:         taskID,
		Sequence:       1,
		EventID:        eventID,
		SchemaVersion:  EventSchema,
		EventType:      "task.submitted",
		SourceKind:     "daemon",
		SourceID:       SourceID,
		CorrelationID:  string(taskID),
		IdempotencyKey: in.IdempotencyKey,
		ToState:        domain.DCPTaskSubmitted,
		TaskRevision:   1,
		OccurredAt:     now,
		RecordedAt:     now,
		Payload:        string(payloadBytes),
		EvidenceDigest: target.IdentityDigest,
	}
	event.IntegrityDigest, err = eventDigest(event)
	if err != nil {
		return SubmitResult{}, apierr.Internal("DCP_EVENT_CANONICALIZATION_FAILED", "DCP event could not be canonicalized")
	}

	stored, err := s.store.SubmitDCPTask(ctx, task, event)
	if errors.Is(err, sqlitestore.ErrDCPIdempotencyConflict) {
		return SubmitResult{}, apierr.Conflict(
			"DCP_IDEMPOTENCY_CONFLICT",
			"The idempotency key is already bound to a different canonical DCP task",
			map[string]any{"idempotencyKey": in.IdempotencyKey},
		)
	}
	if err != nil {
		return SubmitResult{}, apierr.Internal("DCP_TASK_SUBMIT_FAILED", "DCP task could not be submitted")
	}
	return SubmitResult{Task: stored.Task, Duplicate: !stored.Created}, nil
}

func (s *Service) Get(ctx context.Context, taskID domain.DCPTaskID) (domain.DCPTask, error) {
	if strings.TrimSpace(string(taskID)) == "" {
		return domain.DCPTask{}, apierr.Invalid("DCP_TASK_ID_REQUIRED", "DCP task id is required", nil)
	}
	task, err := s.store.GetDCPTask(ctx, taskID)
	if errors.Is(err, sqlitestore.ErrDCPTaskNotFound) {
		return domain.DCPTask{}, apierr.NotFound("DCP_TASK_NOT_FOUND", "Unknown DCP task")
	}
	if err != nil {
		return domain.DCPTask{}, apierr.Internal("DCP_TASK_LOAD_FAILED", "DCP task could not be loaded")
	}
	return task, nil
}

func (s *Service) List(ctx context.Context, projectID string) ([]domain.DCPTask, error) {
	if projectID != "" && projectID != TargetProjectID {
		return nil, apierr.Invalid("DCP_TARGET_INVALID", "Only dcp-lab DCP tasks may be listed", nil)
	}
	tasks, err := s.store.ListDCPTasks(ctx, projectID)
	if err != nil {
		return nil, apierr.Internal("DCP_TASK_LIST_FAILED", "DCP tasks could not be loaded")
	}
	return tasks, nil
}

func (s *Service) Events(ctx context.Context, taskID domain.DCPTaskID) ([]domain.DCPTaskEvent, error) {
	if _, err := s.Get(ctx, taskID); err != nil {
		return nil, err
	}
	events, err := s.store.ListDCPTaskEvents(ctx, taskID)
	if err != nil {
		return nil, apierr.Internal("DCP_TASK_EVENTS_LOAD_FAILED", "DCP task events could not be loaded")
	}
	return events, nil
}

func (s *Service) ValidateSchema(ctx context.Context) error {
	if s == nil || s.store == nil {
		return errors.New("dcp task store is unavailable")
	}
	return s.store.ValidateDCPTaskSchema(ctx)
}

func validateSubmitInput(in SubmitInput) error {
	if !idempotencyKeyPattern.MatchString(in.IdempotencyKey) {
		return apierr.Invalid("DCP_IDEMPOTENCY_KEY_INVALID", "Idempotency key must be 1-128 safe characters", nil)
	}
	if in.Target != TargetRepository {
		return apierr.Invalid("DCP_TARGET_INVALID", "Only the disposable dcp-lab target is allowed", nil)
	}
	if in.ApprovedTask.SchemaVersion != ApprovedTaskSchema {
		return apierr.Invalid("DCP_TASK_SCHEMA_INVALID", "approvedTask.schemaVersion must be dcp.task/v1", nil)
	}
	if err := validateText("approved task title", in.ApprovedTask.Title, maxTitleBytes); err != nil {
		return apierr.Invalid("DCP_TASK_INVALID", err.Error(), nil)
	}
	if err := validateText("approved task description", in.ApprovedTask.Description, maxDescriptionBytes); err != nil {
		return apierr.Invalid("DCP_TASK_INVALID", err.Error(), nil)
	}
	if in.ApprovedScope.SchemaVersion != ApprovedScopeSchema {
		return apierr.Invalid("DCP_SCOPE_SCHEMA_INVALID", "approvedScope.schemaVersion must be dcp.scope/v1", nil)
	}
	if err := validateText("approved scope statement", in.ApprovedScope.Statement, maxScopeBytes); err != nil {
		return apierr.Invalid("DCP_SCOPE_INVALID", err.Error(), nil)
	}
	return nil
}

func validateText(label, value string, maxBytes int) error {
	if value == "" || strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", label)
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s contains a NUL byte", label)
	}
	if len([]byte(value)) > maxBytes {
		return fmt.Errorf("%s exceeds %d UTF-8 bytes", label, maxBytes)
	}
	return nil
}

func digestJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func eventDigest(event domain.DCPTaskEvent) (string, error) {
	return digestJSON(struct {
		TaskID         domain.DCPTaskID     `json:"taskId"`
		Sequence       int64                `json:"sequence"`
		EventID        string               `json:"eventId"`
		SchemaVersion  string               `json:"schemaVersion"`
		EventType      string               `json:"eventType"`
		SourceKind     string               `json:"sourceKind"`
		SourceID       string               `json:"sourceId"`
		CorrelationID  string               `json:"correlationId"`
		CausationID    string               `json:"causationId,omitempty"`
		IdempotencyKey string               `json:"idempotencyKey"`
		FromState      *domain.DCPTaskState `json:"fromState,omitempty"`
		ToState        domain.DCPTaskState  `json:"toState"`
		TaskRevision   int64                `json:"taskRevision"`
		OccurredAt     time.Time            `json:"occurredAt"`
		RecordedAt     time.Time            `json:"recordedAt"`
		Payload        string               `json:"payload"`
		EvidenceDigest string               `json:"evidenceDigest"`
	}{
		TaskID: event.TaskID, Sequence: event.Sequence, EventID: event.EventID,
		SchemaVersion: event.SchemaVersion, EventType: event.EventType,
		SourceKind: event.SourceKind, SourceID: event.SourceID,
		CorrelationID: event.CorrelationID, CausationID: event.CausationID,
		IdempotencyKey: event.IdempotencyKey, FromState: event.FromState,
		ToState: event.ToState, TaskRevision: event.TaskRevision,
		OccurredAt: event.OccurredAt, RecordedAt: event.RecordedAt,
		Payload: event.Payload, EvidenceDigest: event.EvidenceDigest,
	})
}
