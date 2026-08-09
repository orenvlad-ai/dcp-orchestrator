package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	dcptasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/dcptask"
)

// DCPTaskService is the controller-facing model-free I11 task contract.
type DCPTaskService interface {
	Submit(ctx context.Context, in dcptasksvc.SubmitInput) (dcptasksvc.SubmitResult, error)
	Get(ctx context.Context, taskID domain.DCPTaskID) (domain.DCPTask, error)
	List(ctx context.Context, projectID string) ([]domain.DCPTask, error)
	Events(ctx context.Context, taskID domain.DCPTaskID) ([]domain.DCPTaskEvent, error)
}

type DCPTasksController struct{ Svc DCPTaskService }

func (c *DCPTasksController) Register(r chi.Router) {
	r.Post("/dcp/tasks", c.submit)
	r.Get("/dcp/tasks", c.list)
	r.Get("/dcp/tasks/{taskId}", c.get)
	r.Get("/dcp/tasks/{taskId}/events", c.events)
}

func (c *DCPTasksController) submit(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/dcp/tasks")
		return
	}
	var req SubmitDCPTaskRequest
	if err := decodeDCPTaskRequest(r, &req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Request body must be one JSON object with no unknown fields", nil)
		return
	}
	result, err := c.Svc.Submit(r.Context(), dcptasksvc.SubmitInput{
		IdempotencyKey: req.IdempotencyKey,
		Target:         req.Target,
		ApprovedTask:   req.ApprovedTask,
		ApprovedScope:  req.ApprovedScope,
	})
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	status := http.StatusCreated
	if result.Duplicate {
		status = http.StatusOK
	}
	envelope.WriteJSON(w, status, DCPTaskResponse{Task: result.Task, Duplicate: result.Duplicate})
}

func (c *DCPTasksController) list(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/dcp/tasks")
		return
	}
	tasks, err := c.Svc.List(r.Context(), r.URL.Query().Get("project"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	if tasks == nil {
		tasks = []domain.DCPTask{}
	}
	envelope.WriteJSON(w, http.StatusOK, ListDCPTasksResponse{Tasks: tasks})
}

func (c *DCPTasksController) get(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/dcp/tasks/{taskId}")
		return
	}
	task, err := c.Svc.Get(r.Context(), dcpTaskID(r))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, DCPTaskResponse{Task: task, Duplicate: false})
}

func (c *DCPTasksController) events(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/dcp/tasks/{taskId}/events")
		return
	}
	events, err := c.Svc.Events(r.Context(), dcpTaskID(r))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	responses := make([]DCPTaskEventResponse, 0, len(events))
	for _, event := range events {
		responses = append(responses, newDCPTaskEventResponse(event))
	}
	envelope.WriteJSON(w, http.StatusOK, ListDCPTaskEventsResponse{Events: responses})
}

func dcpTaskID(r *http.Request) domain.DCPTaskID {
	return domain.DCPTaskID(chi.URLParam(r, "taskId"))
}

func decodeDCPTaskRequest(r *http.Request, out *SubmitDCPTaskRequest) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON value")
	}
	return nil
}
