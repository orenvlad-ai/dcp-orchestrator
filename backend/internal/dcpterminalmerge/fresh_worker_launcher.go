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

type FreshWorkerLauncher interface {
	PreflightFreshWorker(context.Context, domain.DCPCard12FreshWorkerRecovery) error
	LaunchFreshWorker(context.Context, domain.DCPCard12FreshWorkerRecovery) error
	FreshWorkerProcessAlive(context.Context, domain.DCPCard12FreshWorkerRecovery) (bool, error)
}

type freshWorkerLauncher struct {
	runtime    arbiterRuntime
	agent      ports.Agent
	dataDir    string
	runFile    string
	executable func() (string, error)
	command    func(context.Context, string, ...string) ([]byte, error)
}

func NewFreshWorkerLauncher(runtime arbiterRuntime, agent ports.Agent, dataDir, runFile string) FreshWorkerLauncher {
	return &freshWorkerLauncher{
		runtime: runtime, agent: agent, dataDir: filepath.Clean(dataDir), runFile: filepath.Clean(runFile),
		executable: os.Executable,
		command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		},
	}
}

func (l *freshWorkerLauncher) exactCommand(ctx context.Context, recovery domain.DCPCard12FreshWorkerRecovery) ([]string, error) {
	if l.agent == nil {
		return nil, errors.New("card-12 fresh worker: Codex adapter is unavailable")
	}
	pushCommand := "git push --force-with-lease=refs/heads/" + recovery.SourceBranch + ":" + recovery.OldHead + " origin HEAD:refs/heads/" + recovery.SourceBranch
	system := "You are the one bounded stateless DCP card-12 conflict-repair worker. Treat the supplied JSON as the complete authority. Modify only its single allowed path, keep the exact existing branch and PR, make exactly one repaired commit whose parent is the named current main, then execute exactly this one push command and stop: " + pushCommand + ". Do not execute any other push. Do not create any card, task, worktree, branch, PR, incident, review, merge, label, service or retry. Do not inspect AO state, prior transcripts, arbiter artifacts or unrelated files."
	prompt := "Execute exactly this bounded recovery envelope and stop after its guarded push:\n" + recovery.InputJSON
	argv, err := l.agent.GetLaunchCommand(ctx, ports.LaunchConfig{
		Config:  ports.AgentConfig{Model: recovery.Model, DCPReviewLabNetwork: true},
		DataDir: l.dataDir, Kind: domain.KindWorker, Permissions: ports.PermissionModeAcceptEdits,
		Prompt: prompt, SessionID: string(recovery.SessionID), SystemPrompt: system,
		WorkspacePath: recovery.WorktreePath,
	})
	if err != nil {
		return nil, err
	}
	marker := -1
	for i, arg := range argv {
		if arg == "--" {
			marker = i
			break
		}
	}
	if marker < 2 {
		return nil, errors.New("card-12 fresh worker: generated Codex argv is not exact")
	}
	extra := []string{"--json", "-c", `model_reasoning_effort="xhigh"`, "-c", arbiterRolloutBudgetConfig}
	argv = append(argv[:marker], append(extra, argv[marker:]...)...)
	return argv, nil
}

func (l *freshWorkerLauncher) PreflightFreshWorker(ctx context.Context, recovery domain.DCPCard12FreshWorkerRecovery) error {
	if l == nil || l.runtime == nil || l.command == nil || l.executable == nil ||
		!exactFreshWorkerRecovery(recovery) || recovery.Status != domain.DCPFreshWorkerRequested ||
		recovery.WorkerModelCallCount != 0 || recovery.ReviewerModelCallCount != 0 {
		return errors.New("card-12 fresh worker: requested launcher identity is invalid")
	}
	inspector, ok := l.runtime.(ports.RuntimeQuiescenceInspector)
	if !ok {
		return errors.New("card-12 fresh worker: runtime quiescence inspection is unavailable")
	}
	for _, handle := range []string{
		"dcp-review-lab-7", "dcp-review-lab-9", "dcp-review-lab-10", "dcp-review-lab-11", "dcp-review-lab-12",
		"review-dcp-review-lab-7", "review-dcp-review-lab-9", "review-dcp-review-lab-10", "review-dcp-review-lab-11", "review-dcp-review-lab-12",
		ArbiterRuntimeHandle, ArbiterSuccessorRuntimeHandle,
	} {
		quiescent, err := inspector.IsRuntimeQuiescent(ctx, ports.RuntimeHandle{ID: handle})
		if err != nil || !quiescent {
			return errors.Join(err, fmt.Errorf("card-12 fresh worker: runtime %s is not provably quiescent", handle))
		}
	}
	if alive, err := l.runtime.IsAlive(ctx, ports.RuntimeHandle{ID: recovery.RuntimeHandleID}); err != nil || alive {
		return errors.Join(err, errors.New("card-12 fresh worker: fresh runtime handle already exists"))
	}
	if err := ensureExactDirectory(filepath.Dir(recovery.InputPath)); err != nil {
		return err
	}
	if err := ensureExactArtifact(recovery.InputPath, append([]byte(recovery.InputJSON), '\n')); err != nil {
		return fmt.Errorf("card-12 fresh worker: input artifact: %w", err)
	}
	for _, path := range []string{recovery.ResultPath, recovery.LogPath} {
		if _, err := os.Lstat(path); err == nil {
			return errors.New("card-12 fresh worker: output artifact exists before the call fence")
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	argv, err := l.exactCommand(ctx, recovery)
	if err != nil {
		return err
	}
	version, err := l.command(ctx, argv[0], "--version")
	if err != nil || strings.TrimSpace(string(version)) != arbiterCodexVersion {
		return errors.New("card-12 fresh worker: installed Codex version is not exact")
	}
	missing := filepath.Join(filepath.Dir(recovery.InputPath), "preflight-schema-must-not-exist.json")
	if _, err := os.Lstat(missing); err == nil {
		return errors.New("card-12 fresh worker: strict-config preflight sentinel exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	marker := -1
	for i, arg := range argv {
		if arg == "--" {
			marker = i
			break
		}
	}
	probe := append([]string{}, argv[1:marker]...)
	probe = append(probe, "--output-schema", missing)
	probe = append(probe, argv[marker:]...)
	output, probeErr := l.command(ctx, argv[0], probe...)
	expected := "Failed to read output schema file " + missing + ":"
	if probeErr == nil || !strings.Contains(string(output), expected) {
		return fmt.Errorf("card-12 fresh worker: installed Codex cannot parse exact bounded argv: %w: %s", probeErr, strings.TrimSpace(string(output)))
	}
	return nil
}

func (l *freshWorkerLauncher) LaunchFreshWorker(ctx context.Context, recovery domain.DCPCard12FreshWorkerRecovery) error {
	if !exactFreshWorkerRecovery(recovery) || recovery.Status != domain.DCPFreshWorkerRunning ||
		recovery.WorkerModelCallCount != 1 || recovery.LaunchID != recovery.RuntimeActionID {
		return errors.New("card-12 fresh worker: fenced launch identity is invalid")
	}
	child, err := l.exactCommand(ctx, recovery)
	if err != nil {
		return err
	}
	executable, err := l.executable()
	if err != nil || !filepath.IsAbs(executable) {
		return errors.New("card-12 fresh worker: supervisor executable is unavailable")
	}
	argv := []string{executable, "recovery", "supervise",
		"--recovery", recovery.RecoveryID, "--identity-digest", recovery.RecoveryIdentityDigest,
		"--input-digest", recovery.InputDigest, "--result-file", recovery.ResultPath,
		"--log-file", recovery.LogPath, "--supervisor-data-dir", l.dataDir,
		"--supervisor-run-file", l.runFile, "--"}
	argv = append(argv, child...)
	created, err := l.runtime.Create(ctx, ports.RuntimeConfig{
		SessionID: domain.SessionID(recovery.RuntimeHandleID), WorkspacePath: recovery.WorktreePath,
		Argv: argv, Env: map[string]string{"DCP_CARD12_RECOVERY_ID": recovery.RecoveryID},
	})
	if err != nil {
		return fmt.Errorf("card-12 fresh worker: runtime create: %w", err)
	}
	if created.ID != recovery.RuntimeHandleID {
		return errors.New("card-12 fresh worker: runtime returned a foreign handle")
	}
	return nil
}

func (l *freshWorkerLauncher) FreshWorkerProcessAlive(ctx context.Context, recovery domain.DCPCard12FreshWorkerRecovery) (bool, error) {
	inspector, ok := l.runtime.(ports.SupervisedProcessInspector)
	if !ok {
		return false, errors.New("card-12 fresh worker: process inspection is unavailable")
	}
	return inspector.IsSupervisedProcessAlive(ctx, ports.RuntimeHandle{ID: recovery.RuntimeHandleID}, ports.SupervisedProcessRef{
		SessionID: domain.SessionID(recovery.RuntimeHandleID), LaunchID: recovery.LaunchID,
	})
}
