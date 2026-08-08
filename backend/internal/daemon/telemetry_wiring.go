package daemon

import (
	"context"
	"log/slog"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

// disabledEventSink is deliberately local to the daemon package so the DCP
// runtime binary does not link the upstream analytics adapter package at all.
// It captures, persists, and exports nothing.
type disabledEventSink struct{}

func (disabledEventSink) Emit(context.Context, ports.TelemetryEvent) {}
func (disabledEventSink) Close(context.Context) error                { return nil }

func newTelemetrySink(config.Config, *sqlite.Store, *slog.Logger) ports.EventSink {
	return disabledEventSink{}
}
