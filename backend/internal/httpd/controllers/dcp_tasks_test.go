package controllers_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	dcptasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/dcptask"
)

type fakeDCPTaskService struct {
	task   domain.DCPTask
	events []domain.DCPTaskEvent
	seen   bool
}

func newFakeDCPTaskService() *fakeDCPTaskService {
	now := time.Unix(600, 0).UTC()
	task := domain.DCPTask{
		ID:             "dcp_task_stable",
		IdempotencyKey: "i11-api-1",
		ApprovedTask: domain.DCPApprovedTask{
			SchemaVersion: dcptasksvc.ApprovedTaskSchema,
			Title:         "Synthetic API task",
			Description:   "Store only",
		},
		ApprovedScope: domain.DCPApprovedScope{
			SchemaVersion: dcptasksvc.ApprovedScopeSchema,
			Statement:     "No model action",
		},
		ApprovedDigest: strings.Repeat("a", 64),
		Target: domain.DCPRepositoryIdentity{
			SchemaVersion:  dcptasksvc.RepositorySchema,
			ProjectID:      dcptasksvc.TargetProjectID,
			Repository:     dcptasksvc.TargetRepository,
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
		EventID:         "dcp_event_stable",
		SchemaVersion:   dcptasksvc.EventSchema,
		EventType:       "task.submitted",
		SourceKind:      "daemon",
		SourceID:        dcptasksvc.SourceID,
		CorrelationID:   string(task.ID),
		IdempotencyKey:  task.IdempotencyKey,
		ToState:         task.State,
		TaskRevision:    1,
		OccurredAt:      now,
		RecordedAt:      now,
		Payload:         `{"schemaVersion":"dcp.task-submitted/v1"}`,
		EvidenceDigest:  strings.Repeat("e", 64),
		IntegrityDigest: strings.Repeat("f", 64),
	}
	return &fakeDCPTaskService{task: task, events: []domain.DCPTaskEvent{event}}
}

func (f *fakeDCPTaskService) Submit(_ context.Context, in dcptasksvc.SubmitInput) (dcptasksvc.SubmitResult, error) {
	if in.IdempotencyKey == f.task.IdempotencyKey && in.ApprovedTask.Description != f.task.ApprovedTask.Description {
		return dcptasksvc.SubmitResult{}, apierr.Conflict("DCP_IDEMPOTENCY_CONFLICT", "conflict", nil)
	}
	if f.seen {
		return dcptasksvc.SubmitResult{Task: f.task, Duplicate: true}, nil
	}
	f.seen = true
	return dcptasksvc.SubmitResult{Task: f.task}, nil
}

func (f *fakeDCPTaskService) Get(_ context.Context, id domain.DCPTaskID) (domain.DCPTask, error) {
	if id != f.task.ID {
		return domain.DCPTask{}, apierr.NotFound("DCP_TASK_NOT_FOUND", "unknown task")
	}
	return f.task, nil
}

func (f *fakeDCPTaskService) List(_ context.Context, projectID string) ([]domain.DCPTask, error) {
	if projectID != "" && projectID != dcptasksvc.TargetProjectID {
		return nil, apierr.Invalid("DCP_TARGET_INVALID", "invalid project", nil)
	}
	if !f.seen {
		return []domain.DCPTask{}, nil
	}
	return []domain.DCPTask{f.task}, nil
}

func (f *fakeDCPTaskService) Events(_ context.Context, id domain.DCPTaskID) ([]domain.DCPTaskEvent, error) {
	if id != f.task.ID {
		return nil, apierr.NotFound("DCP_TASK_NOT_FOUND", "unknown task")
	}
	return f.events, nil
}

func newDCPTaskTestServer(t *testing.T, svc *fakeDCPTaskService) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{DCPTasks: svc}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	return srv
}

const dcpSubmitBody = `{"idempotencyKey":"i11-api-1","target":"dcp-lab","approvedTask":{"schemaVersion":"dcp.task/v1","title":"Synthetic API task","description":"Store only"},"approvedScope":{"schemaVersion":"dcp.scope/v1","statement":"No model action"}}`

func TestDCPTasksAPIModelFreeSubmitReadAndEvents(t *testing.T) {
	svc := newFakeDCPTaskService()
	srv := newDCPTaskTestServer(t, svc)

	body, status, headers := doRequest(t, srv, http.MethodPost, "/api/v1/dcp/tasks", dcpSubmitBody)
	assertJSON(t, headers)
	if status != http.StatusCreated || !strings.Contains(string(body), `"taskId":"dcp_task_stable"`) || !strings.Contains(string(body), `"state":"SUBMITTED"`) {
		t.Fatalf("first submit status=%d body=%s", status, body)
	}
	body, status, _ = doRequest(t, srv, http.MethodPost, "/api/v1/dcp/tasks", dcpSubmitBody)
	if status != http.StatusOK || !strings.Contains(string(body), `"duplicate":true`) || !strings.Contains(string(body), `"taskId":"dcp_task_stable"`) {
		t.Fatalf("duplicate status=%d body=%s", status, body)
	}

	body, status, _ = doRequest(t, srv, http.MethodGet, "/api/v1/dcp/tasks?project=dcp-lab", "")
	if status != http.StatusOK || strings.Count(string(body), `"taskId":"dcp_task_stable"`) != 1 {
		t.Fatalf("list status=%d body=%s", status, body)
	}
	body, status, _ = doRequest(t, srv, http.MethodGet, "/api/v1/dcp/tasks/dcp_task_stable", "")
	if status != http.StatusOK || !strings.Contains(string(body), `"revision":1`) {
		t.Fatalf("get status=%d body=%s", status, body)
	}
	body, status, _ = doRequest(t, srv, http.MethodGet, "/api/v1/dcp/tasks/dcp_task_stable/events", "")
	if status != http.StatusOK || !strings.Contains(string(body), `"sequence":1`) || !strings.Contains(string(body), `"payload":{"schemaVersion":"dcp.task-submitted/v1"}`) {
		t.Fatalf("events status=%d body=%s", status, body)
	}
}

func TestDCPTasksAPIRejectsConflictAndMalformedJSON(t *testing.T) {
	svc := newFakeDCPTaskService()
	srv := newDCPTaskTestServer(t, svc)
	_, status, _ := doRequest(t, srv, http.MethodPost, "/api/v1/dcp/tasks", dcpSubmitBody)
	if status != http.StatusCreated {
		t.Fatalf("seed status = %d", status)
	}

	conflict := strings.Replace(dcpSubmitBody, `"description":"Store only"`, `"description":"different"`, 1)
	body, status, _ := doRequest(t, srv, http.MethodPost, "/api/v1/dcp/tasks", conflict)
	assertErrorCode(t, body, status, http.StatusConflict, "DCP_IDEMPOTENCY_CONFLICT")

	unknown := strings.TrimSuffix(dcpSubmitBody, "}") + `,"unexpected":true}`
	body, status, _ = doRequest(t, srv, http.MethodPost, "/api/v1/dcp/tasks", unknown)
	assertErrorCode(t, body, status, http.StatusBadRequest, "INVALID_JSON")
	body, status, _ = doRequest(t, srv, http.MethodPost, "/api/v1/dcp/tasks", dcpSubmitBody+` {}`)
	assertErrorCode(t, body, status, http.StatusBadRequest, "INVALID_JSON")

	body, status, _ = doRequest(t, srv, http.MethodGet, "/api/v1/dcp/tasks?project=real-repo", "")
	assertErrorCode(t, body, status, http.StatusBadRequest, "DCP_TARGET_INVALID")
}

func TestDCPTasksRoutesDefaultToModelFreeStubs(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	body, status, _ := doRequest(t, srv, http.MethodPost, "/api/v1/dcp/tasks", dcpSubmitBody)
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")
}
