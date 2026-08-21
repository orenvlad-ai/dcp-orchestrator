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
	dcpv2svc "github.com/aoagents/agent-orchestrator/backend/internal/service/dcpv2"
)

// DCPTaskService is the controller-facing model-free I11 task contract.
type DCPTaskService interface {
	Submit(ctx context.Context, in dcptasksvc.SubmitInput) (dcptasksvc.SubmitResult, error)
	Get(ctx context.Context, taskID domain.DCPTaskID) (domain.DCPTask, error)
	List(ctx context.Context, projectID string) ([]domain.DCPTask, error)
	Events(ctx context.Context, taskID domain.DCPTaskID) ([]domain.DCPTaskEvent, error)
}

type dcpPolicyTaskService interface {
	SubmitPolicy(ctx context.Context, in dcptasksvc.PolicySubmitInput) (dcptasksvc.PolicySubmitResult, error)
}

type dcpV2TwinTaskService interface {
	SubmitTwin(context.Context, dcpv2svc.TwinSubmitInput) (dcpv2svc.TwinSubmitResult, error)
	Snapshot(context.Context, string) (dcpv2svc.TwinSnapshot, error)
	WakeChecks(context.Context, string, string, int64, string) (dcpv2svc.TwinSnapshot, error)
	WakeRelease(context.Context, string, string, int64, string) (dcpv2svc.TwinSnapshot, error)
}

type DCPTasksController struct {
	Svc DCPTaskService
	V2  dcpV2TwinTaskService
}

func (c *DCPTasksController) Register(r chi.Router) {
	r.Post("/dcp/tasks", c.submit)
	r.Post("/dcp/tasks/policy", c.submitPolicy)
	r.Post("/dcp/v2/tasks", c.submitV2Twin)
	r.Get("/dcp/v2/tasks/{taskId}", c.getV2Twin)
	r.Post("/dcp/v2/tasks/{taskId}/check-event", c.wakeV2TwinChecks)
	r.Post("/dcp/v2/tasks/{taskId}/release-event", c.wakeV2TwinRelease)
	r.Get("/dcp/tasks", c.list)
	r.Get("/dcp/tasks/{taskId}", c.get)
	r.Get("/dcp/tasks/{taskId}/events", c.events)
}

func (c *DCPTasksController) wakeV2TwinChecks(w http.ResponseWriter, r *http.Request) {
	if c.V2 == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/dcp/v2/tasks/{taskId}/check-event")
		return
	}
	var req WakeDCPV2TwinChecksRequest
	if err := decodeDCPTaskRequest(r, &req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Request body must be one JSON object with no unknown fields", nil)
		return
	}
	result, err := c.V2.WakeChecks(r.Context(), chi.URLParam(r, "taskId"), req.DeliveryID, req.RunID, req.PayloadDigest)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}

func (c *DCPTasksController) submitV2Twin(w http.ResponseWriter, r *http.Request) {
	if c.V2 == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/dcp/v2/tasks")
		return
	}
	var req SubmitDCPV2TwinTaskRequest
	if err := decodeDCPTaskRequest(r, &req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Request body must be one JSON object with no unknown fields", nil)
		return
	}
	result, err := c.V2.SubmitTwin(r.Context(), dcpv2svc.TwinSubmitInput{TaskID: req.TaskID, Prompt: req.Prompt})
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	status := http.StatusCreated
	if result.Duplicate {
		status = http.StatusOK
	}
	envelope.WriteJSON(w, status, result)
}

func (c *DCPTasksController) getV2Twin(w http.ResponseWriter, r *http.Request) {
	if c.V2 == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/dcp/v2/tasks/{taskId}")
		return
	}
	result, err := c.V2.Snapshot(r.Context(), chi.URLParam(r, "taskId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}

func (c *DCPTasksController) wakeV2TwinRelease(w http.ResponseWriter, r *http.Request) {
	if c.V2 == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/dcp/v2/tasks/{taskId}/release-event")
		return
	}
	var req WakeDCPV2TwinReleaseRequest
	if err := decodeDCPTaskRequest(r, &req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Request body must be one JSON object with no unknown fields", nil)
		return
	}
	result, err := c.V2.WakeRelease(r.Context(), chi.URLParam(r, "taskId"), req.DeliveryID, req.RunID, req.PayloadDigest)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}

func (c *DCPTasksController) submitPolicy(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.Svc.(dcpPolicyTaskService)
	if c.Svc == nil || !ok {
		apispec.NotImplemented(w, r, "POST", "/api/v1/dcp/tasks/policy")
		return
	}
	var req SubmitDCPPolicyTaskRequest
	if err := decodeDCPTaskRequest(r, &req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Request body must be one JSON object with no unknown fields", nil)
		return
	}
	result, err := svc.SubmitPolicy(r.Context(), dcptasksvc.PolicySubmitInput{
		TaskID: req.TaskID, Target: req.Target, Profile: req.Profile,
		Repository: req.Repository, Prompt: req.Prompt,
	})
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	status := http.StatusCreated
	if result.Duplicate {
		status = http.StatusOK
	}
	envelope.WriteJSON(w, status, DCPPolicyTaskResponse{Task: result.Task, Duplicate: result.Duplicate})
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

func decodeDCPTaskRequest(r *http.Request, out any) error {
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
