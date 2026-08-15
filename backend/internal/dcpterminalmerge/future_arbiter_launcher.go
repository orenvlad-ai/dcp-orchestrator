package dcpterminalmerge

import (
	"bytes"
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
	PreflightSchemaRecovery(context.Context, domain.DCPFutureArbiterIncident, domain.DCPFutureArbiterSchemaRecovery) error
	PreflightResultRecovery(context.Context, domain.DCPFutureArbiterIncident, domain.DCPFutureArbiterResultRecovery) error
	LaunchFuture(context.Context, domain.DCPFutureArbiterIncident) error
	FutureProcessAlive(context.Context, domain.DCPFutureArbiterIncident) (bool, error)
	FutureResultPath(domain.DCPFutureArbiterIncident) (string, error)
}

func (l *futureArbiterLauncher) physicalHandle(logical string) (ports.RuntimeHandle, error) {
	if resolver, ok := l.runtime.(ports.RuntimeSessionHandleResolver); ok {
		return resolver.ResolveSessionHandle(domain.SessionID(logical))
	}
	if strings.TrimSpace(logical) == "" {
		return ports.RuntimeHandle{}, errors.New("DCP future arbiter runtime handle is empty")
	}
	return ports.RuntimeHandle{ID: logical}, nil
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

// PreflightSchemaRecovery proves the immutable predecessor artifacts and the
// pre-inference provider rejection before a separately authorized successor
// generation can be persisted. It launches no process and consumes no slot.
func (l *futureArbiterLauncher) PreflightSchemaRecovery(_ context.Context, predecessor domain.DCPFutureArbiterIncident, recovery domain.DCPFutureArbiterSchemaRecovery) error {
	if recovery.Status != "authorized" || recovery.PredecessorIncidentID != predecessor.IncidentID ||
		recovery.PredecessorIdentityDigest != predecessor.IdentityDigest || recovery.PredecessorInputDigest != predecessor.InputDigest ||
		recovery.PredecessorModelActionID != predecessor.ModelActionID || recovery.ProviderInferenceTokens != 0 ||
		recovery.SuccessorGeneration != predecessor.Generation+1 || predecessor.Status != domain.DCPFutureArbiterFailed ||
		predecessor.ErrorCode != "launch_failed" || predecessor.ModelCallCount != 1 || predecessor.DecisionJSON != "" {
		return errors.New("DCP future arbiter schema recovery identity is invalid")
	}
	if digestString(recovery.ProviderErrorJSON) != recovery.ProviderErrorDigest ||
		!strings.Contains(recovery.ProviderErrorJSON, `"code":"invalid_json_schema"`) ||
		!strings.Contains(recovery.ProviderErrorJSON, `"status":400`) ||
		!strings.Contains(recovery.ProviderErrorJSON, "uniqueItems") {
		return errors.New("DCP future arbiter schema recovery provider rejection is invalid")
	}
	artifacts, err := l.artifacts(predecessor)
	if err != nil {
		return err
	}
	input, err := os.ReadFile(artifacts.input)
	if err != nil || !bytes.Equal(input, append([]byte(predecessor.InputJSON), '\n')) {
		return errors.Join(err, errors.New("DCP future arbiter schema recovery input artifact drifted"))
	}
	schema, err := os.ReadFile(artifacts.schema)
	if err != nil || digestBytes(schema) != recovery.PredecessorSchemaDigest || !bytes.Contains(schema, []byte(`"uniqueItems":true`)) {
		return errors.Join(err, errors.New("DCP future arbiter schema recovery rejected schema drifted"))
	}
	if _, err := os.Lstat(artifacts.result); err == nil {
		return errors.New("DCP future arbiter schema recovery predecessor produced a result")
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

// PreflightResultRecovery proves that the one generation-2 process is gone
// and that all three frozen artifacts still have their byte-exact identities.
// It never starts a runtime or invokes Codex.
func (l *futureArbiterLauncher) PreflightResultRecovery(ctx context.Context, incident domain.DCPFutureArbiterIncident, recovery domain.DCPFutureArbiterResultRecovery) error {
	if incident.FinishedAt == nil || recovery.Status != "pending" ||
		recovery.IncidentID != incident.IncidentID || recovery.IdentityDigest != incident.IdentityDigest ||
		recovery.InputDigest != incident.InputDigest || recovery.ModelActionID != incident.ModelActionID ||
		recovery.PriorStatus != string(domain.DCPFutureArbiterFailed) ||
		(recovery.PriorErrorCode != "launch_failed" && recovery.PriorErrorCode != "submit_failed") ||
		!recovery.PriorFinishedAt.Equal(*incident.FinishedAt) || recovery.PriorModelCallCount != 1 ||
		recovery.PriorDecisionDigest != "" || incident.Status != domain.DCPFutureArbiterFailed ||
		incident.ErrorCode != recovery.PriorErrorCode || incident.ModelCallCount != 1 || incident.DecisionJSON != "" ||
		incident.DecisionDigest != "" || recovery.RuntimeHandleID != incident.RuntimeHandleID {
		return errors.New("DCP future arbiter result recovery identity is invalid")
	}
	handle, err := l.physicalHandle(incident.RuntimeHandleID)
	if err != nil || handle.ID != recovery.PhysicalRuntimeHandle {
		return errors.Join(err, errors.New("DCP future arbiter result recovery physical handle drifted"))
	}
	alive, err := l.FutureProcessAlive(ctx, incident)
	if err != nil {
		return err
	}
	if alive {
		return errors.New("DCP future arbiter result recovery process is still active")
	}
	artifacts, err := l.artifacts(incident)
	if err != nil {
		return err
	}
	type expectedArtifact struct {
		path   string
		digest string
		size   int64
	}
	for _, artifact := range []expectedArtifact{
		{artifacts.input, recovery.InputArtifactDigest, recovery.InputArtifactSize},
		{artifacts.schema, recovery.SchemaArtifactDigest, recovery.SchemaArtifactSize},
		{artifacts.result, recovery.ResultArtifactDigest, recovery.ResultArtifactSize},
	} {
		info, statErr := os.Lstat(artifact.path)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
			info.Mode().Perm()&0o022 != 0 || info.Size() != artifact.size {
			return errors.Join(statErr, errors.New("DCP future arbiter result recovery artifact identity drifted"))
		}
		data, readErr := os.ReadFile(artifact.path)
		if readErr != nil || digestBytes(data) != artifact.digest {
			return errors.Join(readErr, errors.New("DCP future arbiter result recovery artifact digest drifted"))
		}
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
	prompt := "You are the bounded DCP ordinary-card release arbiter. The supplied JSON is the complete authoritative context-free evidence envelope for one exact incident generation and its full relevant cohort. Return exactly one JSON object matching the schema. Choose successor_repair only when one bounded same-task repair can preserve every compatible cohort intent on current main. Choose deterministic_order_hold only when exact predecessor order alone can resolve the incident without code mutation. If intents are mutually exclusive or evidence is insufficient, choose human_gate and ask one short specific owner question. For human_gate, repairTaskId and repairObjective must be empty; affectedPaths may only identify frozen incident paths for diagnosis and grants no mutation authority. Never edit, push, review, admit, merge, accept risk, guess owner intent, or claim owner acceptance. Evidence:\n" + incident.InputJSON
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
	handle, err := l.physicalHandle(incident.RuntimeHandleID)
	if err != nil {
		return err
	}
	if err := l.runtime.Destroy(ctx, handle); err != nil {
		return fmt.Errorf("DCP future arbiter stale runtime: %w", err)
	}
	created, err := l.runtime.Create(ctx, ports.RuntimeConfig{SessionID: domain.SessionID(incident.RuntimeHandleID), WorkspacePath: artifacts.directory, Argv: argv,
		Env: map[string]string{"DCP_ARBITER_INCIDENT_ID": incident.IncidentID}})
	if err != nil {
		return fmt.Errorf("DCP future arbiter runtime create: %w", err)
	}
	if created.ID != handle.ID {
		return errors.New("DCP future arbiter runtime returned foreign handle")
	}
	return nil
}

func (l *futureArbiterLauncher) FutureProcessAlive(ctx context.Context, incident domain.DCPFutureArbiterIncident) (bool, error) {
	inspector, ok := l.runtime.(ports.SupervisedProcessInspector)
	if !ok {
		return false, errors.New("DCP future arbiter process inspection is unavailable")
	}
	handle, err := l.physicalHandle(incident.RuntimeHandleID)
	if err != nil {
		return false, err
	}
	return inspector.IsSupervisedProcessAlive(ctx, handle, ports.SupervisedProcessRef{SessionID: domain.SessionID(incident.RuntimeHandleID), LaunchID: incident.IncidentID})
}

func (l *futureArbiterLauncher) FutureResultPath(incident domain.DCPFutureArbiterIncident) (string, error) {
	artifacts, err := l.artifacts(incident)
	if err != nil {
		return "", err
	}
	return artifacts.result, nil
}
