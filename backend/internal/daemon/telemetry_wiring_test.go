package daemon

import (
	"context"
	"log/slog"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestNewTelemetrySinkIsAlwaysDisabled(t *testing.T) {
	sink := newTelemetrySink(config.Config{Telemetry: config.TelemetryConfig{
		Events:      true,
		Metrics:     true,
		Remote:      "posthog",
		PostHogKey:  "must-not-be-used",
		PostHogHost: "https://example.invalid",
	}}, nil, slog.Default())

	sink.Emit(context.Background(), ports.TelemetryEvent{Name: "must.not.persist"})
	if err := sink.Close(context.Background()); err != nil {
		t.Fatalf("disabled sink close: %v", err)
	}
	if _, ok := sink.(disabledEventSink); !ok {
		t.Fatalf("sink type = %T, want disabledEventSink", sink)
	}
}
