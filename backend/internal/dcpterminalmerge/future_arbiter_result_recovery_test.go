package dcpterminalmerge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type mappedFutureRuntime struct {
	physical  string
	returned  string
	alive     bool
	destroyed ports.RuntimeHandle
	inspected ports.RuntimeHandle
	config    ports.RuntimeConfig
}

func (f *mappedFutureRuntime) ResolveSessionHandle(domain.SessionID) (ports.RuntimeHandle, error) {
	if f.physical == "" {
		return ports.RuntimeHandle{}, errors.New("missing physical handle")
	}
	return ports.RuntimeHandle{ID: f.physical}, nil
}

func (f *mappedFutureRuntime) Create(_ context.Context, cfg ports.RuntimeConfig) (ports.RuntimeHandle, error) {
	f.config = cfg
	if f.returned != "" {
		return ports.RuntimeHandle{ID: f.returned}, nil
	}
	return ports.RuntimeHandle{ID: f.physical}, nil
}

func (f *mappedFutureRuntime) Destroy(_ context.Context, handle ports.RuntimeHandle) error {
	f.destroyed = handle
	return nil
}

func (f *mappedFutureRuntime) IsAlive(context.Context, ports.RuntimeHandle) (bool, error) {
	return true, nil
}

func (f *mappedFutureRuntime) IsSupervisedProcessAlive(_ context.Context, handle ports.RuntimeHandle, _ ports.SupervisedProcessRef) (bool, error) {
	f.inspected = handle
	return f.alive, nil
}

func TestFutureArbiterLauncherUsesOpaquePhysicalHandle(t *testing.T) {
	incident := futureProtocolIncident(t)
	incident.RuntimeHandleID = incident.IncidentID
	runtime := &mappedFutureRuntime{physical: "dcp-future-arbiter-short-deadbeef"}
	dataDir := t.TempDir()
	launcher := NewFutureArbiterLauncher(runtime, dataDir, filepath.Join(dataDir, "run.json")).(*futureArbiterLauncher)
	executable := filepath.Join(dataDir, "daemon")
	if err := os.WriteFile(executable, []byte("test"), 0o700); err != nil {
		t.Fatal(err)
	}
	launcher.executable = func() (string, error) { return executable, nil }
	launcher.lookPath = func(string) (string, error) { return "/opt/codex", nil }
	if err := launcher.LaunchFuture(context.Background(), incident); err != nil {
		t.Fatal(err)
	}
	if runtime.destroyed.ID != runtime.physical || runtime.config.SessionID != domain.SessionID(incident.RuntimeHandleID) {
		t.Fatalf("physical launch mapping = destroyed=%+v config=%+v", runtime.destroyed, runtime.config)
	}
	if _, err := launcher.FutureProcessAlive(context.Background(), incident); err != nil {
		t.Fatal(err)
	}
	if runtime.inspected.ID != runtime.physical {
		t.Fatalf("inspected handle = %q, want %q", runtime.inspected.ID, runtime.physical)
	}
	runtime.returned = "foreign-handle"
	if err := launcher.LaunchFuture(context.Background(), incident); err == nil {
		t.Fatal("foreign physical runtime handle was accepted")
	}
}

func TestFutureArbiterResultRecoveryPreflightIsModelFreeAndByteExact(t *testing.T) {
	incident := futureProtocolIncident(t)
	incident.RuntimeHandleID = incident.IncidentID
	incident.Status, incident.ModelCallCount, incident.ErrorCode = domain.DCPFutureArbiterFailed, 1, "launch_failed"
	finished := time.Unix(499, 0).UTC()
	incident.FinishedAt = &finished
	runtime := &mappedFutureRuntime{physical: "dcp-future-arbiter-short-deadbeef"}
	dataDir := t.TempDir()
	launcher := NewFutureArbiterLauncher(runtime, dataDir, filepath.Join(dataDir, "run.json")).(*futureArbiterLauncher)
	artifacts, err := launcher.artifacts(incident)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(artifacts.directory, 0o750); err != nil {
		t.Fatal(err)
	}
	input, schema, result := []byte("input\n"), []byte("schema\n"), []byte("result\n")
	for path, data := range map[string][]byte{artifacts.input: input, artifacts.schema: schema, artifacts.result: result} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	recovery := domain.DCPFutureArbiterResultRecovery{
		IncidentID: incident.IncidentID, IdentityDigest: incident.IdentityDigest, InputDigest: incident.InputDigest,
		ModelActionID: incident.ModelActionID, PriorStatus: string(domain.DCPFutureArbiterFailed), PriorErrorCode: "launch_failed",
		PriorFinishedAt: finished, PriorModelCallCount: 1, RuntimeHandleID: incident.RuntimeHandleID,
		PhysicalRuntimeHandle: runtime.physical, InputArtifactDigest: digestBytes(input), InputArtifactSize: int64(len(input)),
		SchemaArtifactDigest: digestBytes(schema), SchemaArtifactSize: int64(len(schema)), ResultArtifactDigest: digestBytes(result),
		ResultArtifactSize: int64(len(result)), Status: "pending",
	}
	if err := launcher.PreflightResultRecovery(context.Background(), incident, recovery); err != nil {
		t.Fatal(err)
	}
	if runtime.inspected.ID != runtime.physical || runtime.config.SessionID != "" {
		t.Fatalf("recovery mutated runtime or inspected foreign handle: %+v", runtime)
	}
	if err := os.WriteFile(artifacts.result, []byte("drift\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := launcher.PreflightResultRecovery(context.Background(), incident, recovery); err == nil {
		t.Fatal("drifted result artifact was accepted")
	}
}
