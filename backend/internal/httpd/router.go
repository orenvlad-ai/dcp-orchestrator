// Package httpd builds and runs the daemon's HTTP surface: middleware, health
// probes, daemon control, REST APIs, and terminal WebSocket routing.
package httpd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/daemonmeta"
	"github.com/aoagents/agent-orchestrator/backend/internal/dcpterminalmerge"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/telemetrymeta"
	"github.com/aoagents/agent-orchestrator/backend/internal/terminal"
)

// ControlDeps carries the daemon-control hooks the router exposes, such as the
// callback that requests a graceful shutdown.
type ControlDeps struct {
	RequestShutdown func()
}

// NewRouterWithControl builds the root router with the standard middleware
// stack, the API surface, and the daemon-control hooks wired from ControlDeps.
// Missing Managers in deps keep routes registered but return OpenAPI-backed 501
// responses.
//
// Middleware order (outermost first):
//
//	RequestID      → attach a request id for correlation
//	RealIP         → normalise client IP (loopback proxy from the dev server)
//	requestLogger  → slog-backed access log + 5xx telemetry, carries the request id
//	recoverer      → turn a handler panic into 500 instead of crashing the daemon
//	cors           → CORS allowlist for the Electron renderer / dev origins
//
// The per-request timeout is deliberately not global: it wraps only bounded
// REST routes, never long-lived terminal streams or health probes.
func NewRouterWithControl(cfg config.Config, log *slog.Logger, termMgr *terminal.Manager, deps APIDeps, control ControlDeps) chi.Router {
	log = loggerOrDefault(log)
	r := chi.NewRouter()
	api := NewAPI(cfg, deps)

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(requestLogger(log, deps.Telemetry))
	r.Use(recoverTelemetry(log, deps.Telemetry))
	r.Use(corsMiddleware(cfg.AllowedOrigins))
	r.Use(previewOriginMiddleware(api.sessions))

	// JSON envelopes for unmatched routes / methods — chi's defaults are
	// text/plain, which would break consumers that parse every response as
	// the locked APIError shape.
	r.NotFound(notFoundJSON)
	r.MethodNotAllowed(methodNotAllowedJSON)

	mountHealth(r, cfg)
	mountTerminalMux(r, termMgr, log)
	mountControl(r, control)
	mountTelemetry(r, cfg, deps.Telemetry)
	mountDCPV2Direct(r, deps.DCPV2Direct)
	mountDCPArbiter(r, deps.DCPArbiter)
	mountMobile(r, deps.Mobile)
	api.Register(r)

	return r
}

// DCPV2DirectService is the loopback-only completion wake used by the
// stateless direct model supervisor. The Action id is the only caller input;
// all Task/runtime/result bindings are read from DCP v2 durable authority.
type DCPV2DirectService interface {
	ReportDirectProcessExit(context.Context, string) error
}

type dcpV2DirectProcessExitRequest struct {
	ActionID string `json:"actionId"`
}

func mountDCPV2Direct(r chi.Router, service DCPV2DirectService) {
	if service == nil {
		return
	}
	r.Post("/internal/dcp/v2/model/process-exit", func(w http.ResponseWriter, req *http.Request) {
		if !localControlRequest(req) {
			envelope.WriteJSON(w, http.StatusForbidden, map[string]any{"status": "forbidden", "service": daemonmeta.ServiceName})
			return
		}
		var body dcpV2DirectProcessExitRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, req.Body, 4096))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil || body.ActionID == "" {
			envelope.WriteAPIError(w, req, http.StatusBadRequest, "bad_request", "INVALID_DCP_V2_PROCESS_EXIT", "invalid bounded DCP v2 process exit", nil)
			return
		}
		var trailing any
		if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
			envelope.WriteAPIError(w, req, http.StatusBadRequest, "bad_request", "INVALID_DCP_V2_PROCESS_EXIT", "invalid bounded DCP v2 process exit", nil)
			return
		}
		if err := service.ReportDirectProcessExit(req.Context(), body.ActionID); err != nil {
			envelope.WriteAPIError(w, req, http.StatusConflict, "conflict", "DCP_V2_PROCESS_EXIT_REJECTED", "DCP v2 process exit rejected fail-closed", nil)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
}

// DCPArbiterService is the loopback-only callback surface used by the exact
// one-shot arbiter supervisor. It is deliberately absent from the public API
// and OpenAPI document.
type DCPArbiterService interface {
	SubmitArbiterDecision(context.Context, string, []byte) error
	ReportArbiterProcessExit(context.Context, string, dcpterminalmerge.ArbiterProcessExitReport) error
	ProcessFreshWorkerExit(context.Context, string, dcpterminalmerge.FreshWorkerExitReport) error
}

type dcpArbiterDecisionRequest struct {
	IncidentID string          `json:"incidentId"`
	Decision   json.RawMessage `json:"decision"`
}

type dcpArbiterExitRequest struct {
	IncidentID    string `json:"incidentId"`
	Started       bool   `json:"started"`
	ExitCode      int    `json:"exitCode"`
	ResultFailure string `json:"resultFailure,omitempty"`
}

type dcpFreshWorkerExitRequest struct {
	RecoveryID string `json:"recoveryId"`
	Started    bool   `json:"started"`
	ExitCode   int    `json:"exitCode"`
}

func mountDCPArbiter(r chi.Router, service DCPArbiterService) {
	if service == nil {
		return
	}
	local := func(w http.ResponseWriter, req *http.Request) bool {
		if localControlRequest(req) {
			return true
		}
		envelope.WriteJSON(w, http.StatusForbidden, map[string]any{"status": "forbidden", "service": daemonmeta.ServiceName})
		return false
	}
	r.Post("/internal/dcp/review-lab/arbiter/decision", func(w http.ResponseWriter, req *http.Request) {
		if !local(w, req) {
			return
		}
		var body dcpArbiterDecisionRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, req.Body, 20*1024))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil || body.IncidentID == "" || len(body.Decision) == 0 {
			envelope.WriteAPIError(w, req, http.StatusBadRequest, "bad_request", "INVALID_ARBITER_RESULT", "invalid bounded arbiter result", nil)
			return
		}
		if err := service.SubmitArbiterDecision(req.Context(), body.IncidentID, body.Decision); err != nil {
			envelope.WriteAPIError(w, req, http.StatusConflict, "conflict", "ARBITER_RESULT_REJECTED", "arbiter result rejected fail-closed", nil)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	r.Post("/internal/dcp/review-lab/arbiter/process-exit", func(w http.ResponseWriter, req *http.Request) {
		if !local(w, req) {
			return
		}
		var body dcpArbiterExitRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, req.Body, 4096))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil || body.IncidentID == "" {
			envelope.WriteAPIError(w, req, http.StatusBadRequest, "bad_request", "INVALID_ARBITER_EXIT", "invalid bounded arbiter exit", nil)
			return
		}
		report := dcpterminalmerge.ArbiterProcessExitReport{Started: body.Started, ExitCode: body.ExitCode, ResultFailure: body.ResultFailure}
		if err := service.ReportArbiterProcessExit(req.Context(), body.IncidentID, report); err != nil {
			envelope.WriteAPIError(w, req, http.StatusConflict, "conflict", "ARBITER_EXIT_REJECTED", "arbiter exit rejected fail-closed", nil)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	r.Post("/internal/dcp/review-lab/card12-recovery/process-exit", func(w http.ResponseWriter, req *http.Request) {
		if !local(w, req) {
			return
		}
		var body dcpFreshWorkerExitRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, req.Body, 4096))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil || body.RecoveryID == "" {
			envelope.WriteAPIError(w, req, http.StatusBadRequest, "bad_request", "INVALID_RECOVERY_EXIT", "invalid bounded recovery exit", nil)
			return
		}
		if err := service.ProcessFreshWorkerExit(req.Context(), body.RecoveryID, dcpterminalmerge.FreshWorkerExitReport{Started: body.Started, ExitCode: body.ExitCode}); err != nil {
			envelope.WriteAPIError(w, req, http.StatusConflict, "conflict", "RECOVERY_EXIT_REJECTED", "recovery exit rejected fail-closed", nil)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
}

func previewOriginMiddleware(sessions *controllers.SessionsController) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if sessions != nil && sessions.PreviewOrigin(w, r) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// mountHealth registers the liveness and readiness probes the Electron
// supervisor polls before letting the renderer connect.
func mountHealth(r chi.Router, cfg config.Config) {
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		envelope.WriteJSON(w, http.StatusOK, daemonProbePayload("ok", cfg))
	})
	r.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		envelope.WriteJSON(w, http.StatusOK, daemonProbePayload("ready", cfg))
	})
}

// mountControl registers the loopback daemon-control endpoints. /shutdown is
// unauthenticated and state-changing, so it is gated by localControlRequest to
// keep a browser the user happens to have open (CSRF / DNS-rebinding) or a
// remote client from being able to kill the daemon.
func mountControl(r chi.Router, deps ControlDeps) {
	if deps.RequestShutdown == nil {
		return
	}
	r.Post("/shutdown", func(w http.ResponseWriter, req *http.Request) {
		if !localControlRequest(req) {
			envelope.WriteJSON(w, http.StatusForbidden, map[string]any{
				"status":  "forbidden",
				"service": daemonmeta.ServiceName,
			})
			return
		}
		envelope.WriteJSON(w, http.StatusAccepted, map[string]any{
			"status":  "shutting_down",
			"service": daemonmeta.ServiceName,
			"pid":     os.Getpid(),
		})
		deps.RequestShutdown()
	})
}

// mountMobile registers the Connect Mobile control routes: status, enable,
// disable, and regenerate. These toggle the LAN bridge that lets a phone reach
// the daemon. They must be reachable from the desktop renderer — a browser
// context that always sends an Origin header — so they are NOT gated by
// localControlRequest (which rejects any Origin-bearing request and is meant for
// the CLI). The "phone must never toggle its own access" invariant is enforced
// on the LAN listener instead, by lanControlBlock, which 404s /api/v1/mobile on
// the 0.0.0.0 socket the phone reaches — a transport-based check that cannot be
// spoofed with a forged Host header. On the loopback listener these routes are
// protected by the same CORS allowlist as every other app route.
func mountMobile(r chi.Router, c *controllers.MobileController) {
	if c == nil {
		return
	}
	r.Get("/api/v1/mobile/status", c.Status)
	r.Post("/api/v1/mobile/enable", c.Enable)
	r.Post("/api/v1/mobile/disable", c.Disable)
	r.Post("/api/v1/mobile/regenerate", c.Regenerate)
}

type cliInvokedRequest struct {
	Command     string `json:"command"`
	CommandPath string `json:"commandPath"`
	ActorType   string `json:"actorType"`
}

type cliUsageErrorRequest struct {
	Command     string `json:"command"`
	CommandPath string `json:"commandPath"`
	Error       string `json:"error"`
}

func mountTelemetry(r chi.Router, cfg config.Config, sink ports.EventSink) {
	if sink == nil || !cfg.Telemetry.Events {
		return
	}
	// CLI telemetry is capped to bounded uniques: ao.app.active once per UTC
	// six-hour slot for user-context CLI activity (matching the renderer
	// heartbeat) and ao.cli.invoked once per actor type + command path per UTC
	// day. Scripts and agent sessions invoke read-only commands (status, ls,
	// get) in polling loops, so raw invocation counts measure automation, not
	// usage; bounded uniques keep the "which commands, how many users" signal
	// without the firehose. The reservation state is persisted under DataDir so
	// daemon restarts cannot turn polling loops back into raw event volume.
	cliTelemetry := newCLITelemetryReservoir(cfg.DataDir)
	r.Post("/internal/telemetry/cli-invoked", func(w http.ResponseWriter, req *http.Request) {
		if !localControlRequest(req) {
			envelope.WriteJSON(w, http.StatusForbidden, map[string]any{
				"status":  "forbidden",
				"service": daemonmeta.ServiceName,
			})
			return
		}

		var body cliInvokedRequest
		dec := json.NewDecoder(req.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			envelope.WriteAPIError(w, req, http.StatusBadRequest, "bad_request", "INVALID_JSON", "request body must be valid JSON", nil)
			return
		}
		if body.CommandPath == "" {
			envelope.WriteAPIError(w, req, http.StatusBadRequest, "bad_request", "COMMAND_PATH_REQUIRED", "commandPath is required", nil)
			return
		}
		commandPath := telemetrymeta.NormalizeCommandPath(body.CommandPath)
		actorType := telemetrymeta.CLIActorType(body.ActorType, commandPath)
		if actorType == "system" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if telemetrymeta.IsRoutineInternalCLICommand(commandPath) {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		if now := time.Now(); cliTelemetry.reserveInvoked(now, actorType, commandPath) {
			sink.Emit(req.Context(), ports.TelemetryEvent{
				Name:       "ao.cli.invoked",
				Source:     "cli",
				OccurredAt: now.UTC(),
				Level:      ports.TelemetryLevelInfo,
				RequestID:  middleware.GetReqID(req.Context()),
				Payload: map[string]any{
					"command":      body.Command,
					"command_path": commandPath,
					"actor_type":   actorType,
				},
			})
		}
		if actorType == "user" {
			if now := time.Now(); cliTelemetry.reserveActive(now) {
				sink.Emit(req.Context(), ports.TelemetryEvent{
					Name:       "ao.app.active",
					Source:     "cli",
					OccurredAt: now.UTC(),
					Level:      ports.TelemetryLevelInfo,
					RequestID:  middleware.GetReqID(req.Context()),
					Payload: map[string]any{
						"channel":      "cli",
						"command":      body.Command,
						"command_path": commandPath,
						"actor_type":   actorType,
					},
				})
			}
		}
		w.WriteHeader(http.StatusAccepted)
	})
	r.Post("/internal/telemetry/cli-usage-error", func(w http.ResponseWriter, req *http.Request) {
		if !localControlRequest(req) {
			envelope.WriteJSON(w, http.StatusForbidden, map[string]any{
				"status":  "forbidden",
				"service": daemonmeta.ServiceName,
			})
			return
		}

		var body cliUsageErrorRequest
		dec := json.NewDecoder(req.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			envelope.WriteAPIError(w, req, http.StatusBadRequest, "bad_request", "INVALID_JSON", "request body must be valid JSON", nil)
			return
		}
		if body.CommandPath == "" {
			envelope.WriteAPIError(w, req, http.StatusBadRequest, "bad_request", "COMMAND_PATH_REQUIRED", "commandPath is required", nil)
			return
		}

		sink.Emit(req.Context(), ports.TelemetryEvent{
			Name:       "ao.cli.usage_errors",
			Source:     "cli",
			OccurredAt: time.Now().UTC(),
			Level:      ports.TelemetryLevelWarn,
			RequestID:  middleware.GetReqID(req.Context()),
			Payload: map[string]any{
				"component":    "cli",
				"operation":    "command_parse",
				"command":      body.Command,
				"command_path": body.CommandPath,
				"error_kind":   "usage",
				"fingerprint":  telemetrymeta.Fingerprint("cli", "command_parse", body.CommandPath, "usage"),
			},
		})
		w.WriteHeader(http.StatusAccepted)
	})
}

// localControlRequest reports whether a control request is a trusted local
// caller. The Go CLI client addresses the daemon by its loopback host and
// never sets an Origin header; a cross-site browser fetch always carries an
// Origin, and a DNS-rebinding attempt resolves a non-loopback Host. Rejecting
// either closes the CSRF/rebinding vector while leaving the CLI unaffected.
func localControlRequest(r *http.Request) bool {
	if r.Header.Get("Origin") != "" {
		return false
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// daemonProbePayload is shared by /healthz and /readyz. Dependency
// initialization happens before the server is constructed, so a listening
// daemon is ready to answer requests.
func daemonProbePayload(status string, cfg config.Config) map[string]any {
	payload := map[string]any{
		"status":  status,
		"service": daemonmeta.ServiceName,
		"pid":     os.Getpid(),
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		payload["executablePath"] = exe
	}
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		payload["workingDirectory"] = cwd
	}
	if cfg.StartupWorkingDirectory != "" {
		payload["startupWorkingDirectory"] = cfg.StartupWorkingDirectory
	}
	return payload
}
