package httpd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
)

func TestTelemetryControlRoutesAreNotMounted(t *testing.T) {
	r := NewRouterWithControl(config.Config{DataDir: t.TempDir()}, discardLogger(), nil, APIDeps{Telemetry: &captureSink{}}, ControlDeps{})
	for _, route := range []string{"/internal/telemetry/cli-invoked", "/internal/telemetry/cli-usage-error"} {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1"+route, strings.NewReader(`{}`))
		req.Host = "127.0.0.1:43231"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", route, rec.Code)
		}
	}
}

func TestTelemetryEnvironmentCannotMountControlRoutes(t *testing.T) {
	t.Setenv("AO_TELEMETRY_EVENTS", "on")
	t.Setenv("AO_TELEMETRY_REMOTE", "posthog")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	r := NewRouterWithControl(cfg, discardLogger(), nil, APIDeps{Telemetry: &captureSink{}}, ControlDeps{})
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/internal/telemetry/cli-invoked", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
