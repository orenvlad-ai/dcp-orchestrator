package httpd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
)

type captureDCPV2DirectExit struct {
	action string
	err    error
}

func (c *captureDCPV2DirectExit) ReportDirectProcessExit(_ context.Context, action string) error {
	c.action = action
	return c.err
}

func TestDCPV2DirectProcessExitIsLoopbackOnlyAndBounded(t *testing.T) {
	capture := &captureDCPV2DirectExit{}
	router := NewRouterWithControl(config.Config{}, discardLogger(), nil, APIDeps{DCPV2Direct: capture}, ControlDeps{})
	req := httptest.NewRequest(http.MethodPost, "/internal/dcp/v2/model/process-exit", strings.NewReader(`{"actionId":"v2-action"}`))
	req.RemoteAddr = "127.0.0.1:43210"
	req.Host = "127.0.0.1:3001"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusAccepted || capture.action != "v2-action" {
		t.Fatalf("loopback response=%d action=%q body=%s", response.Code, capture.action, response.Body.String())
	}

	foreign := httptest.NewRequest(http.MethodPost, "/internal/dcp/v2/model/process-exit", strings.NewReader(`{"actionId":"v2-action"}`))
	foreign.RemoteAddr = "192.0.2.10:43210"
	foreign.Host = "192.0.2.10:3001"
	response = httptest.NewRecorder()
	router.ServeHTTP(response, foreign)
	if response.Code != http.StatusForbidden {
		t.Fatalf("foreign callback response=%d body=%s", response.Code, response.Body.String())
	}

	unknown := httptest.NewRequest(http.MethodPost, "/internal/dcp/v2/model/process-exit", strings.NewReader(`{"actionId":"v2-action","taskId":"foreign"}`))
	unknown.RemoteAddr = "127.0.0.1:43210"
	unknown.Host = "127.0.0.1:3001"
	response = httptest.NewRecorder()
	router.ServeHTTP(response, unknown)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unbounded callback response=%d body=%s", response.Code, response.Body.String())
	}

	trailing := httptest.NewRequest(http.MethodPost, "/internal/dcp/v2/model/process-exit", strings.NewReader(`{"actionId":"v2-action"} {"actionId":"foreign"}`))
	trailing.RemoteAddr = "127.0.0.1:43210"
	trailing.Host = "127.0.0.1:3001"
	response = httptest.NewRecorder()
	router.ServeHTTP(response, trailing)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("trailing callback response=%d body=%s", response.Code, response.Body.String())
	}
}
