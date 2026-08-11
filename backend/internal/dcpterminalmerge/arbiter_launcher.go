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

type ArbiterLauncher interface {
	Preflight(context.Context, domain.DCPReleaseArbiterIncident) error
	Launch(context.Context, domain.DCPReleaseArbiterIncident) error
	ProcessAlive(context.Context, domain.DCPReleaseArbiterIncident) (bool, error)
	ResultPath(domain.DCPReleaseArbiterIncident) (string, error)
}

type arbiterRuntime interface {
	Create(context.Context, ports.RuntimeConfig) (ports.RuntimeHandle, error)
	Destroy(context.Context, ports.RuntimeHandle) error
	IsAlive(context.Context, ports.RuntimeHandle) (bool, error)
}

type arbiterLauncher struct {
	runtime    arbiterRuntime
	dataDir    string
	runFile    string
	executable func() (string, error)
	lookPath   func(string) (string, error)
	command    func(context.Context, string, ...string) ([]byte, error)
}

func NewArbiterLauncher(runtime arbiterRuntime, dataDir, runFile string) ArbiterLauncher {
	return &arbiterLauncher{
		runtime: runtime, dataDir: filepath.Clean(dataDir), runFile: filepath.Clean(runFile),
		executable: os.Executable, lookPath: exec.LookPath,
		command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		},
	}
}

type arbiterArtifacts struct {
	directory string
	input     string
	schema    string
	result    string
}

func (l *arbiterLauncher) artifacts(incident domain.DCPReleaseArbiterIncident) (arbiterArtifacts, error) {
	if l == nil || !filepath.IsAbs(l.dataDir) || filepath.Clean(l.dataDir) != l.dataDir ||
		!filepath.IsAbs(l.runFile) || filepath.Clean(l.runFile) != l.runFile ||
		incident.IncidentID == "" || incident.RuntimeHandleID != ArbiterRuntimeHandle || incident.LaunchID != incident.IncidentID {
		return arbiterArtifacts{}, errors.New("dcp arbiter: launcher identity is invalid")
	}
	directory := filepath.Join(l.dataDir, "runtime", "dcp-arbiter", incident.IncidentID)
	return arbiterArtifacts{directory: directory, input: filepath.Join(directory, "input.json"), schema: filepath.Join(directory, "schema.json"), result: filepath.Join(directory, "result.json")}, nil
}

func (l *arbiterLauncher) Preflight(ctx context.Context, incident domain.DCPReleaseArbiterIncident) error {
	if l.runtime == nil || l.lookPath == nil || l.command == nil || l.executable == nil {
		return errors.New("dcp arbiter: launcher dependencies are unavailable")
	}
	artifacts, err := l.artifacts(incident)
	if err != nil {
		return err
	}
	if err := ensureExactDirectory(artifacts.directory); err != nil {
		return err
	}
	schema, err := ArbiterDecisionJSONSchema(incident)
	if err != nil {
		return err
	}
	if err := ensureExactArtifact(artifacts.input, append([]byte(incident.InputJSON), '\n')); err != nil {
		return fmt.Errorf("dcp arbiter: input artifact: %w", err)
	}
	if err := ensureExactArtifact(artifacts.schema, append(schema, '\n')); err != nil {
		return fmt.Errorf("dcp arbiter: schema artifact: %w", err)
	}
	if _, err := os.Lstat(artifacts.result); err == nil {
		return errors.New("dcp arbiter: result exists before the call fence")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("dcp arbiter: inspect result artifact: %w", err)
	}
	codex, err := l.lookPath("codex")
	if err != nil || !filepath.IsAbs(codex) {
		return errors.New("dcp arbiter: exact installed Codex binary is unavailable")
	}
	probe := append(arbiterCodexBaseArgs(), "--help")
	if output, err := l.command(ctx, codex, probe...); err != nil {
		return fmt.Errorf("dcp arbiter: installed Codex cannot enforce exact argv: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (l *arbiterLauncher) Launch(ctx context.Context, incident domain.DCPReleaseArbiterIncident) error {
	artifacts, err := l.artifacts(incident)
	if err != nil {
		return err
	}
	codex, err := l.lookPath("codex")
	if err != nil || !filepath.IsAbs(codex) {
		return errors.New("dcp arbiter: exact installed Codex binary disappeared after preflight")
	}
	executable, err := l.executable()
	if err != nil || !filepath.IsAbs(executable) {
		return errors.New("dcp arbiter: exact supervisor executable is unavailable")
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return errors.New("dcp arbiter: supervisor executable is not an executable regular file")
	}
	prompt := "You are the bounded DCP global release arbiter v1. The following JSON is the complete frozen authoritative input. Return exactly one JSON object matching the supplied schema. Select assign_recovery only when the exact evidence proves the sole same-worker conflict-repair path remains inside the approved task; otherwise select one truthful safe_stop. Do not propose or perform mutations, merges, labels, scope changes, risk acceptance, HumanGate, or owner acceptance. Frozen input:\n" + incident.InputJSON
	child := append(arbiterCodexBaseArgs(),
		"--cd", artifacts.directory, "--output-schema", artifacts.schema,
		"--output-last-message", artifacts.result, "--", prompt)
	argv := []string{executable, "arbiter", "supervise",
		"--handle", ArbiterRuntimeHandle,
		"--incident", incident.IncidentID, "--identity-digest", incident.IdentityDigest,
		"--input-digest", incident.InputDigest, "--result-file", artifacts.result,
		"--result-schema", artifacts.schema, "--supervisor-data-dir", l.dataDir,
		"--supervisor-run-file", l.runFile, "--"}
	argv = append(argv, codex)
	argv = append(argv, child...)
	handle := ports.RuntimeHandle{ID: ArbiterRuntimeHandle}
	if err := l.runtime.Destroy(ctx, handle); err != nil {
		return fmt.Errorf("dcp arbiter: replace foreign or stale terminal: %w", err)
	}
	created, err := l.runtime.Create(ctx, ports.RuntimeConfig{
		SessionID: domain.SessionID(ArbiterRuntimeHandle), WorkspacePath: artifacts.directory, Argv: argv,
		Env: map[string]string{"DCP_ARBITER_INCIDENT_ID": incident.IncidentID},
	})
	if err != nil {
		return fmt.Errorf("dcp arbiter: runtime create: %w", err)
	}
	if created.ID != ArbiterRuntimeHandle {
		return errors.New("dcp arbiter: runtime returned a foreign stable handle")
	}
	return nil
}

func (l *arbiterLauncher) ProcessAlive(ctx context.Context, incident domain.DCPReleaseArbiterIncident) (bool, error) {
	inspector, ok := l.runtime.(ports.SupervisedProcessInspector)
	if !ok {
		return false, errors.New("dcp arbiter: exact process inspection is unavailable")
	}
	return inspector.IsSupervisedProcessAlive(ctx, ports.RuntimeHandle{ID: incident.RuntimeHandleID}, ports.SupervisedProcessRef{
		SessionID: domain.SessionID(incident.RuntimeHandleID), LaunchID: incident.LaunchID,
	})
}

func (l *arbiterLauncher) ResultPath(incident domain.DCPReleaseArbiterIncident) (string, error) {
	artifacts, err := l.artifacts(incident)
	if err != nil {
		return "", err
	}
	return artifacts.result, nil
}

func arbiterCodexBaseArgs() []string {
	args := []string{"exec", "--ignore-user-config", "--ephemeral", "--strict-config"}
	for _, feature := range []string{"hooks", "apps", "plugins", "multi_agent", "shell_tool", "unified_exec", "browser_use", "computer_use", "image_generation", "code_mode"} {
		args = append(args, "--disable", feature)
	}
	args = append(args,
		"--enable", "rollout_budget",
		"-c", `approval_policy="never"`, "-c", `web_search="disabled"`,
		"-c", `model_reasoning_effort="xhigh"`,
		"-c", "rollout_budget.limit_tokens=16384",
		"-c", "rollout_budget.reminder_at_remaining_tokens=2048",
		"-c", "rollout_budget.sampling_token_weight=1",
		"-c", "rollout_budget.prefill_token_weight=1",
		"-c", "check_for_update_on_startup=false",
		"--sandbox", "read-only", "--model", ArbiterModel, "--skip-git-repo-check")
	return args
}

func ensureExactDirectory(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("dcp arbiter: artifact directory is not exact")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("dcp arbiter: artifact root is not an exact directory")
	}
	return os.Chmod(path, 0o700)
}

func ensureExactArtifact(path string, expected []byte) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 || info.Size() != int64(len(expected)) {
			return errors.New("existing artifact identity is unsafe")
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != string(expected) {
			return errors.New("existing artifact bytes drifted")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(expected); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
