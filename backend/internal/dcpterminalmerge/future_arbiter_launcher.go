package dcpterminalmerge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// FutureArbiterLauncher owns the isolated one-shot process for an exact incident.
type FutureArbiterLauncher interface {
	PreflightFuture(context.Context, domain.DCPFutureArbiterIncident) error
	LaunchFuture(context.Context, domain.DCPFutureArbiterIncident) error
	FutureProcessAlive(context.Context, domain.DCPFutureArbiterIncident) (bool, error)
	FutureResultPath(domain.DCPFutureArbiterIncident) (string, error)
}

type futureArbiterLauncher struct {
	runtime    arbiterRuntime
	dataDir    string
	runFile    string
	executable func() (string, error)
	lookPath   func(string) (string, error)
	command    func(context.Context, string, ...string) ([]byte, error)
}

// NewFutureArbiterLauncher builds the ordinary-card arbiter launcher.
func NewFutureArbiterLauncher(runtime arbiterRuntime, dataDir, runFile string) FutureArbiterLauncher {
	return &futureArbiterLauncher{
		runtime: runtime, dataDir: filepath.Clean(dataDir), runFile: filepath.Clean(runFile),
		executable: os.Executable, lookPath: exec.LookPath,
		command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		},
	}
}

func (l *futureArbiterLauncher) artifacts(incident domain.DCPFutureArbiterIncident) (arbiterArtifacts, error) {
	if l == nil || !filepath.IsAbs(l.dataDir) || filepath.Clean(l.dataDir) != l.dataDir ||
		!filepath.IsAbs(l.runFile) || filepath.Clean(l.runFile) != l.runFile ||
		!strings.HasPrefix(incident.IncidentID, "dcp-future-arbiter-") || len(incident.IncidentID) != 83 ||
		incident.RuntimeHandleID != incident.IncidentID || incident.IdentityDigest != strings.TrimPrefix(incident.IncidentID, "dcp-future-arbiter-") {
		return arbiterArtifacts{}, errors.New("DCP future arbiter launcher identity is invalid")
	}
	directory := filepath.Join(l.dataDir, "runtime", "dcp-future-arbiter", incident.IncidentID)
	return arbiterArtifacts{directory: directory, input: filepath.Join(directory, "input.json"), schema: filepath.Join(directory, "schema.json"), result: filepath.Join(directory, "result.json")}, nil
}

func (l *futureArbiterLauncher) PreflightFuture(ctx context.Context, incident domain.DCPFutureArbiterIncident) error {
	if l.runtime == nil || l.lookPath == nil || l.command == nil || l.executable == nil {
		return errors.New("DCP future arbiter launcher dependencies are unavailable")
	}
	artifacts, err := l.artifacts(incident)
	if err != nil {
		return err
	}
	if err := ensureExactDirectory(artifacts.directory); err != nil {
		return err
	}
	schema, err := FutureArbiterDecisionJSONSchema(incident)
	if err != nil {
		return err
	}
	if err := ensureExactArtifact(artifacts.input, append([]byte(incident.InputJSON), '\n')); err != nil {
		return fmt.Errorf("DCP future arbiter input artifact: %w", err)
	}
	if err := ensureExactArtifact(artifacts.schema, append(schema, '\n')); err != nil {
		return fmt.Errorf("DCP future arbiter schema artifact: %w", err)
	}
	if _, err := os.Lstat(artifacts.result); err == nil {
		return errors.New("DCP future arbiter result exists before call fence")
	} else if !os.IsNotExist(err) {
		return err
	}
	codex, err := l.lookPath("codex")
	if err != nil || !filepath.IsAbs(codex) {
		return errors.New("DCP future arbiter Codex binary is unavailable")
	}
	version, err := l.command(ctx, codex, "--version")
	if err != nil || strings.TrimSpace(string(version)) != arbiterCodexVersion {
		return fmt.Errorf("DCP future arbiter Codex version is not exact: %s", strings.TrimSpace(string(version)))
	}
	missing := filepath.Join(artifacts.directory, "preflight-schema-must-not-exist.json")
	probe := append(arbiterCodexBaseArgs(), "--output-schema", missing, "--", "DCP model-free strict future arbiter configuration preflight")
	output, probeErr := l.command(ctx, codex, probe...)
	if probeErr == nil || !strings.Contains(string(output), "Failed to read output schema file "+missing+":") {
		return fmt.Errorf("DCP future arbiter strict configuration preflight failed: %w: %s", probeErr, strings.TrimSpace(string(output)))
	}
	return nil
}

func (l *futureArbiterLauncher) LaunchFuture(ctx context.Context, incident domain.DCPFutureArbiterIncident) error {
	artifacts, err := l.artifacts(incident)
	if err != nil {
		return err
	}
	codex, err := l.lookPath("codex")
	if err != nil || !filepath.IsAbs(codex) {
		return errors.New("DCP future arbiter Codex binary disappeared")
	}
	executable, err := l.executable()
	if err != nil || !filepath.IsAbs(executable) {
		return errors.New("DCP future arbiter supervisor is unavailable")
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return errors.New("DCP future arbiter supervisor is not executable")
	}
	prompt := "You are the bounded DCP ordinary-card release arbiter. The supplied JSON is the complete authoritative context-free evidence envelope for one exact incident generation and its full relevant cohort. Return exactly one JSON object matching the schema. Choose successor_repair only when one bounded same-task repair can preserve every compatible cohort intent on current main. Choose deterministic_order_hold only when exact predecessor order alone can resolve the incident without code mutation. If intents are mutually exclusive or evidence is insufficient, choose human_gate and ask one short specific owner question. Never edit, push, review, admit, merge, accept risk, guess owner intent, or claim owner acceptance. Evidence:\n" + incident.InputJSON
	child := append(arbiterCodexBaseArgs(), "--cd", artifacts.directory, "--output-schema", artifacts.schema,
		"--output-last-message", artifacts.result, "--", prompt)
	fixed := []string{executable, "arbiter", "supervise", "--handle", incident.RuntimeHandleID,
		"--incident", incident.IncidentID, "--identity-digest", incident.IdentityDigest,
		"--input-digest", incident.InputDigest, "--result-file", artifacts.result,
		"--result-schema", artifacts.schema, "--supervisor-data-dir", l.dataDir,
		"--supervisor-run-file", l.runFile, "--", codex}
	argv := make([]string, len(fixed)+len(child))
	copy(argv, fixed)
	copy(argv[len(fixed):], child)
	handle := ports.RuntimeHandle{ID: incident.RuntimeHandleID}
	if err := l.runtime.Destroy(ctx, handle); err != nil {
		return fmt.Errorf("DCP future arbiter stale runtime: %w", err)
	}
	created, err := l.runtime.Create(ctx, ports.RuntimeConfig{SessionID: domain.SessionID(incident.RuntimeHandleID), WorkspacePath: artifacts.directory, Argv: argv,
		Env: map[string]string{"DCP_ARBITER_INCIDENT_ID": incident.IncidentID}})
	if err != nil {
		return fmt.Errorf("DCP future arbiter runtime create: %w", err)
	}
	if created.ID != incident.RuntimeHandleID {
		return errors.New("DCP future arbiter runtime returned foreign handle")
	}
	return nil
}

func (l *futureArbiterLauncher) FutureProcessAlive(ctx context.Context, incident domain.DCPFutureArbiterIncident) (bool, error) {
	inspector, ok := l.runtime.(ports.SupervisedProcessInspector)
	if !ok {
		return false, errors.New("DCP future arbiter process inspection is unavailable")
	}
	return inspector.IsSupervisedProcessAlive(ctx, ports.RuntimeHandle{ID: incident.RuntimeHandleID}, ports.SupervisedProcessRef{SessionID: domain.SessionID(incident.RuntimeHandleID), LaunchID: incident.IncidentID})
}

func (l *futureArbiterLauncher) FutureResultPath(incident domain.DCPFutureArbiterIncident) (string, error) {
	artifacts, err := l.artifacts(incident)
	if err != nil {
		return "", err
	}
	return artifacts.result, nil
}
