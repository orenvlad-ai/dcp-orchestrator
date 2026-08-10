package review

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const cancelInterruptDelay = 150 * time.Millisecond

const reviewerTaskMessagePrefix = "Read and follow the AO review task in `"

const reviewerSubmitBinaryName = "ao"

const reviewerResultDirectoryName = "reviewer-results"

// Launcher spawns, re-notifies, and probes a reviewer over a worker's worktree.
// It is the side of the engine that talks to the reviewer registry and runtime;
// the engine owns the orchestration and persistence.
type Launcher interface {
	// Preflight checks whether the reviewer for the given harness is available
	// to run (binary on PATH, etc.) without starting a runtime pane. It runs
	// only when a reviewer launch is actually required, after ReviewRun rows
	// have been created. On failure the engine's Trigger() calls failRuns() to
	// mark those rows as failed, matching the existing Spawn failure semantics.
	Preflight(ctx context.Context, harness domain.ReviewerHarness, workspacePath string) error
	// Spawn launches a fresh reviewer and returns the runtime handle id of the
	// live pane (stable per worker, reused across passes).
	Spawn(ctx context.Context, spec LaunchSpec) (handleID string, err error)
	// Notify asks an already-running reviewer pane to review a new commit.
	Notify(ctx context.Context, handleID string, spec LaunchSpec) error
	// Alive reports whether a reviewer pane is still running.
	Alive(ctx context.Context, handleID string) (bool, error)
	// Cancel interrupts a running reviewer pane while keeping the terminal alive.
	Cancel(ctx context.Context, handleID string, harness domain.ReviewerHarness) error
}

// LaunchSpec is the engine's request to (re)launch a reviewer for one pass.
type LaunchSpec struct {
	RunID         string
	BatchID       string
	WorkerID      domain.SessionID
	Harness       domain.ReviewerHarness
	WorkspacePath string
	PRURL         string
	TargetSHA     string
	ReviewQueue   []ports.ReviewTask
	ReviewIndex   int
}

// reviewerRuntime is the runtime surface the launcher needs: create a pane,
// inject a message into a running pane, and probe liveness. The tmux runtime
// satisfies it.
type reviewerRuntime interface {
	Create(ctx context.Context, cfg ports.RuntimeConfig) (ports.RuntimeHandle, error)
	Destroy(ctx context.Context, handle ports.RuntimeHandle) error
	Interrupt(ctx context.Context, handle ports.RuntimeHandle) error
	IsAlive(ctx context.Context, handle ports.RuntimeHandle) (bool, error)
	SendMessage(ctx context.Context, handle ports.RuntimeHandle, message string) error
}

// agentLauncher resolves a reviewer adapter from the registry and drives the
// runtime. The reviewer reuses the worker's worktree (a fresh session worktree
// would branch off the default branch and so would not contain the PR changes).
type agentLauncher struct {
	reviewers  ports.ReviewerResolver
	runtime    reviewerRuntime
	dataDir    string
	runFile    string
	executable func() (string, error)
}

type preLaunchReviewer interface {
	PreLaunch(ctx context.Context, inv ports.ReviewInvocation) error
}

// NewLauncher builds the production reviewer launcher.
func NewLauncher(reviewers ports.ReviewerResolver, runtime reviewerRuntime, dataDir string, runFiles ...string) Launcher {
	runFile := filepath.Join(dataDir, "run", "running.json")
	if len(runFiles) > 0 {
		runFile = runFiles[0]
	}
	return &agentLauncher{reviewers: reviewers, runtime: runtime, dataDir: dataDir, runFile: runFile, executable: os.Executable}
}

// Preflight checks whether the reviewer for the given harness can be launched
// without starting a runtime pane. It uses the same source of truth as Spawn:
// resolve the adapter, build the real ReviewCommand, and validate the
// executable. The only difference from Spawn is that Preflight stops before
// runtime.Create().
func (l *agentLauncher) Preflight(ctx context.Context, harness domain.ReviewerHarness, workspacePath string) error {
	reviewer, ok := l.reviewers.Reviewer(harness)
	if !ok {
		return fmt.Errorf("no reviewer adapter for harness %q", harness)
	}
	cmd, err := reviewer.ReviewCommand(ctx, ports.ReviewInvocation{WorkspacePath: workspacePath})
	if err != nil {
		return fmt.Errorf("reviewer command: %w", err)
	}
	if len(cmd.Argv) == 0 {
		return fmt.Errorf("reviewer produced empty command")
	}
	// Unwrap any leading env KEY=value ... prefix so the real binary is
	// validated. Mirrors launchBinary in the session manager, which already
	// skips the same prefix to validate the worker agent binary.
	bin := cmd.Argv[0]
	if filepath.Base(bin) == "env" {
		for _, arg := range cmd.Argv[1:] {
			if !strings.Contains(arg, "=") {
				bin = arg
				break
			}
		}
	}
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("reviewer binary %q not found: %w", bin, err)
	}
	return nil
}

// reviewerHandleID is the stable runtime handle for a worker's reviewer pane, so
// one live reviewer is reused across passes.
func reviewerHandleID(workerID domain.SessionID) string {
	return "review-" + string(workerID)
}

func (l *agentLauncher) invocation(spec LaunchSpec, structured bool) ports.ReviewInvocation {
	prompt, systemPrompt := reviewTexts(spec)
	if structured {
		prompt, systemPrompt = structuredReviewTexts(spec)
	}
	return ports.ReviewInvocation{
		ReviewerID:      reviewerHandleID(spec.WorkerID),
		RunID:           spec.RunID,
		WorkerSessionID: spec.WorkerID,
		PRURL:           spec.PRURL,
		TargetSHA:       spec.TargetSHA,
		ReviewQueue:     spec.ReviewQueue,
		ReviewIndex:     spec.ReviewIndex,
		WorkspacePath:   spec.WorkspacePath,
		Prompt:          prompt,
		SystemPrompt:    systemPrompt,
	}
}

// prepareInvocation stores the full reviewer instructions outside the
// worktree, then replaces the terminal-visible prompt with a short file
// reference.
// Reviewer panes are shared by desktop, mobile, and direct runtime attaches,
// so keeping the full text out of the PTY is the only device-independent way
// to hide it.
func (l *agentLauncher) prepareInvocation(spec LaunchSpec, structured bool) (ports.ReviewInvocation, error) {
	inv := l.invocation(spec, structured)
	if strings.TrimSpace(l.dataDir) == "" {
		return ports.ReviewInvocation{}, fmt.Errorf("reviewer prompt data directory is required")
	}
	if strings.TrimSpace(spec.BatchID) == "" || strings.TrimSpace(spec.RunID) == "" {
		return ports.ReviewInvocation{}, fmt.Errorf("reviewer prompt batch and run ids are required")
	}
	promptRoot := filepath.Join(l.dataDir, "prompts", string(spec.WorkerID), "reviewer")
	requestDir := filepath.Join(promptRoot, "requests", spec.BatchID, spec.RunID)
	if err := os.MkdirAll(requestDir, 0o700); err != nil {
		return ports.ReviewInvocation{}, fmt.Errorf("create reviewer prompt directory: %w", err)
	}
	taskPath := filepath.Join(requestDir, "task.md")
	if err := os.WriteFile(taskPath, []byte(strings.TrimRight(inv.Prompt, "\n")+"\n"), 0o600); err != nil {
		return ports.ReviewInvocation{}, fmt.Errorf("write reviewer task prompt: %w", err)
	}
	systemPath := filepath.Join(promptRoot, "system.md")
	systemPrompt := strings.TrimRight(inv.SystemPrompt, "\n") + "\n\n" +
		"AO stores each review task in an immutable file. Whenever AO asks you to start a review task, " +
		"read the exact file path in that request first and follow it completely.\n"
	if err := os.WriteFile(systemPath, []byte(systemPrompt), 0o600); err != nil {
		return ports.ReviewInvocation{}, fmt.Errorf("write reviewer system prompt: %w", err)
	}
	inv.Prompt = reviewerTaskMessagePrefix + filepath.ToSlash(taskPath) + "`."
	inv.SystemPrompt = ""
	inv.SystemPromptFile = systemPath
	inv.TaskPromptFile = taskPath
	inv.TaskPromptRoot = promptRoot
	if structured {
		if len(spec.ReviewQueue) != 1 || spec.ReviewQueue[0].RunID != spec.RunID || spec.ReviewQueue[0].PRURL != spec.PRURL || spec.ReviewQueue[0].TargetSHA != spec.TargetSHA {
			return ports.ReviewInvocation{}, fmt.Errorf("structured reviewer requires exactly one matching exact-head task")
		}
		expected := StructuredResultExpected{
			WorkerSessionID:  string(spec.WorkerID),
			ReviewerHandleID: reviewerHandleID(spec.WorkerID),
			BatchID:          spec.BatchID,
			RunID:            spec.RunID,
			PRURL:            spec.PRURL,
			TargetSHA:        spec.TargetSHA,
		}
		schema, err := StructuredResultSchema(expected)
		if err != nil {
			return ports.ReviewInvocation{}, fmt.Errorf("structured reviewer identity: %w", err)
		}
		resultDir := filepath.Join(l.dataDir, "runtime", reviewerResultDirectoryName, string(spec.WorkerID), spec.BatchID, spec.RunID)
		if err := os.MkdirAll(resultDir, 0o700); err != nil {
			return ports.ReviewInvocation{}, fmt.Errorf("create structured reviewer result directory: %w", err)
		}
		info, err := os.Lstat(resultDir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ports.ReviewInvocation{}, fmt.Errorf("structured reviewer result directory is not an exact directory")
		}
		if err := os.Chmod(resultDir, 0o700); err != nil {
			return ports.ReviewInvocation{}, fmt.Errorf("restrict structured reviewer result directory: %w", err)
		}
		schemaPath := filepath.Join(resultDir, "schema.json")
		resultPath := filepath.Join(resultDir, "result.json")
		for _, artifact := range []string{schemaPath, resultPath} {
			if _, err := os.Lstat(artifact); err == nil {
				return ports.ReviewInvocation{}, fmt.Errorf("structured reviewer artifact already exists: %s", artifact)
			} else if !os.IsNotExist(err) {
				return ports.ReviewInvocation{}, fmt.Errorf("inspect structured reviewer artifact: %w", err)
			}
		}
		f, err := os.OpenFile(schemaPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return ports.ReviewInvocation{}, fmt.Errorf("create structured reviewer schema: %w", err)
		}
		if _, err := f.Write(append(schema, '\n')); err != nil {
			_ = f.Close()
			_ = os.Remove(schemaPath)
			return ports.ReviewInvocation{}, fmt.Errorf("write structured reviewer schema: %w", err)
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(schemaPath)
			return ports.ReviewInvocation{}, fmt.Errorf("close structured reviewer schema: %w", err)
		}
		inv.ResultSchemaFile = schemaPath
		inv.ResultFile = resultPath
	}
	return inv, nil
}

func (l *agentLauncher) Spawn(ctx context.Context, spec LaunchSpec) (string, error) {
	reviewer, ok := l.reviewers.Reviewer(spec.Harness)
	if !ok {
		return "", fmt.Errorf("no reviewer adapter for harness %q", spec.Harness)
	}
	handleID := reviewerHandleID(spec.WorkerID)
	structured := false
	if capable, ok := reviewer.(ports.StructuredResultReviewer); ok {
		structured = capable.RequiresStructuredResult()
	}
	inv, err := l.prepareInvocation(spec, structured)
	if err != nil {
		return "", err
	}
	if pl, ok := reviewer.(preLaunchReviewer); ok {
		if err := pl.PreLaunch(ctx, inv); err != nil {
			return "", fmt.Errorf("reviewer pre-launch: %w", err)
		}
	}
	cmd, err := reviewer.ReviewCommand(ctx, inv)
	if err != nil {
		return "", fmt.Errorf("reviewer command: %w", err)
	}
	argv, err := l.supervisedCommand(spec, inv, cmd.Argv)
	if err != nil {
		return "", err
	}
	// The reviewer handle is stable per worker, so a still-live pane from a
	// previous pass would otherwise block `tmux new-session` (duplicate name) or,
	// worse, keep serving under its old harness. Destroy any stale pane on this
	// handle first so the reviewer always (re)launches under spec.Harness's
	// sandbox/permissions/env — which are applied only here at Create, never by
	// Notify. Destroy is idempotent when no pane exists (first spawn / dead pane).
	if err := l.runtime.Destroy(ctx, ports.RuntimeHandle{ID: handleID}); err != nil {
		return "", fmt.Errorf("reviewer replace stale pane: %w", err)
	}
	env := copyReviewerEnv(cmd.Env)
	if !structured {
		env, err = l.reviewerEnv(cmd.Env, argv[0])
		if err != nil {
			return "", fmt.Errorf("reviewer submit channel: %w", err)
		}
	}
	handle, err := l.runtime.Create(ctx, ports.RuntimeConfig{
		SessionID:     domain.SessionID(handleID),
		WorkspacePath: spec.WorkspacePath,
		Argv:          argv,
		Env:           env,
	})
	if err != nil {
		return "", fmt.Errorf("reviewer runtime: %w", err)
	}
	return handle.ID, nil
}

func (l *agentLauncher) supervisedCommand(spec LaunchSpec, inv ports.ReviewInvocation, child []string) ([]string, error) {
	if len(child) == 0 {
		return nil, fmt.Errorf("reviewer produced empty command")
	}
	dataDir := filepath.Clean(l.dataDir)
	runFile := filepath.Clean(l.runFile)
	if !filepath.IsAbs(dataDir) || !filepath.IsAbs(runFile) || dataDir != l.dataDir || runFile != l.runFile {
		return nil, fmt.Errorf("reviewer supervisor requires exact absolute data-dir and run-file paths")
	}
	exe, err := l.executablePath()
	if err != nil {
		return nil, fmt.Errorf("resolve reviewer supervisor executable: %w", err)
	}
	argv := []string{exe, "review", "supervise", "--session", string(spec.WorkerID), "--run", spec.RunID}
	for _, task := range spec.ReviewQueue {
		if task.RunID != "" && task.RunID != spec.RunID {
			argv = append(argv, "--run", task.RunID)
		}
	}
	if inv.ResultFile != "" || inv.ResultSchemaFile != "" {
		if inv.ResultFile == "" || inv.ResultSchemaFile == "" {
			return nil, fmt.Errorf("structured reviewer requires both result and schema paths")
		}
		argv = append(argv,
			"--reviewer-handle", reviewerHandleID(spec.WorkerID),
			"--batch", spec.BatchID,
			"--pr-url", spec.PRURL,
			"--target-sha", spec.TargetSHA,
			"--result-file", inv.ResultFile,
			"--result-schema", inv.ResultSchemaFile,
		)
	}
	argv = append(argv,
		"--supervisor-data-dir", dataDir,
		"--supervisor-run-file", runFile,
		"--",
	)
	return append(argv, child...), nil
}

func copyReviewerEnv(base map[string]string) map[string]string {
	env := make(map[string]string, len(base))
	for key, value := range base {
		env[key] = value
	}
	return env
}

// ReviewerProcessAlive reports whether the exact supervised review generation
// is still running. A preserved terminal shell is not an active reviewer.
func (l *agentLauncher) ReviewerProcessAlive(ctx context.Context, handleID, runID string) (bool, error) {
	inspector, ok := l.runtime.(ports.SupervisedProcessInspector)
	if !ok {
		return false, fmt.Errorf("reviewer process inspection is unavailable")
	}
	return inspector.IsSupervisedProcessAlive(ctx, ports.RuntimeHandle{ID: handleID}, ports.SupervisedProcessRef{
		SessionID: domain.SessionID(handleID),
		LaunchID:  runID,
	})
}

// executablePath resolves the exact binary that owns both reviewer supervision
// and the local verdict callback. Keeping one resolver for both uses prevents a
// packaged/renamed daemon from supervising with one executable while exposing
// another CLI to the reviewer.
func (l *agentLauncher) executablePath() (string, error) {
	resolve := l.executable
	if resolve == nil {
		resolve = os.Executable
	}
	exe, err := resolve()
	if err != nil {
		return "", err
	}
	if _, err := exactExecutableInfo(exe); err != nil {
		return "", err
	}
	return exe, nil
}

func exactExecutableInfo(exe string) (os.FileInfo, error) {
	if !filepath.IsAbs(exe) || filepath.Clean(exe) != exe {
		return nil, fmt.Errorf("daemon executable must be an exact absolute path: %q", exe)
	}
	info, err := os.Stat(exe)
	if err != nil {
		return nil, fmt.Errorf("stat daemon executable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return nil, fmt.Errorf("daemon executable is not an executable regular file: %s", exe)
	}
	return info, nil
}

// reviewerEnv exposes the stock `ao review submit` command through a private,
// pane-local alias bound to the exact executable that supervises this reviewer.
// DCP's packaged daemon is intentionally named dcp-orchestratord, so merely
// prepending its directory cannot make the stock bare `ao` callback resolve.
// The alias lives below the daemon's own data directory and is atomically
// replaced before every fresh reviewer launch. It never changes the user's
// global PATH and always wins over any inherited or retired `ao` binary.
func (l *agentLauncher) reviewerEnv(base map[string]string, supervisorExecutable string) (map[string]string, error) {
	exeInfo, err := exactExecutableInfo(supervisorExecutable)
	if err != nil {
		return nil, err
	}
	exe := supervisorExecutable
	dataDir := filepath.Clean(l.dataDir)
	if !filepath.IsAbs(dataDir) || dataDir != l.dataDir {
		return nil, fmt.Errorf("reviewer CLI alias requires an exact absolute data directory")
	}
	binDir := filepath.Join(dataDir, "runtime", "reviewer-cli")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		return nil, fmt.Errorf("create reviewer CLI directory: %w", err)
	}
	dirInfo, err := os.Lstat(binDir)
	if err != nil {
		return nil, fmt.Errorf("inspect reviewer CLI directory: %w", err)
	}
	if !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("reviewer CLI directory is not an exact directory: %s", binDir)
	}
	if err := os.Chmod(binDir, 0o700); err != nil {
		return nil, fmt.Errorf("restrict reviewer CLI directory: %w", err)
	}

	alias := filepath.Join(binDir, reviewerSubmitBinaryName)
	tmp, err := os.CreateTemp(binDir, ".ao-link-")
	if err != nil {
		return nil, fmt.Errorf("reserve reviewer CLI alias: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("close reviewer CLI alias reservation: %w", err)
	}
	if err := os.Remove(tmpPath); err != nil {
		return nil, fmt.Errorf("prepare reviewer CLI alias: %w", err)
	}
	defer func() { _ = os.Remove(tmpPath) }()
	if err := os.Symlink(exe, tmpPath); err != nil {
		return nil, fmt.Errorf("create reviewer CLI alias: %w", err)
	}
	if err := os.Rename(tmpPath, alias); err != nil {
		return nil, fmt.Errorf("activate reviewer CLI alias: %w", err)
	}
	link, err := os.Readlink(alias)
	if err != nil {
		return nil, fmt.Errorf("read reviewer CLI alias: %w", err)
	}
	if link != exe {
		return nil, fmt.Errorf("reviewer CLI alias target mismatch: got %q, want %q", link, exe)
	}
	aliasInfo, err := os.Stat(alias)
	if err != nil {
		return nil, fmt.Errorf("stat reviewer CLI alias target: %w", err)
	}
	if !os.SameFile(aliasInfo, exeInfo) {
		return nil, fmt.Errorf("reviewer CLI alias is not bound to the supervisor executable")
	}

	env := copyReviewerEnv(base)
	path := base["PATH"]
	if path == "" {
		path = os.Getenv("PATH")
	}
	if path == "" {
		env["PATH"] = binDir
	} else {
		env["PATH"] = binDir + string(os.PathListSeparator) + path
	}
	return env, nil
}

func (l *agentLauncher) Notify(ctx context.Context, handleID string, spec LaunchSpec) error {
	reviewer, ok := l.reviewers.Reviewer(spec.Harness)
	if !ok {
		return fmt.Errorf("no reviewer adapter for harness %q", spec.Harness)
	}
	structured := false
	if capable, ok := reviewer.(ports.StructuredResultReviewer); ok {
		structured = capable.RequiresStructuredResult()
	}
	if structured {
		return fmt.Errorf("structured reviewer requires a fresh supervised process")
	}
	inv, err := l.prepareInvocation(spec, structured)
	if err != nil {
		return err
	}
	msg, err := reviewer.ReviewMessage(ctx, inv)
	if err != nil {
		return fmt.Errorf("reviewer message: %w", err)
	}
	if err := l.runtime.SendMessage(ctx, ports.RuntimeHandle{ID: handleID}, msg); err != nil {
		return fmt.Errorf("notify reviewer: %w", err)
	}
	return nil
}

func (l *agentLauncher) Alive(ctx context.Context, handleID string) (bool, error) {
	if handleID == "" {
		return false, nil
	}
	return l.runtime.IsAlive(ctx, ports.RuntimeHandle{ID: handleID})
}

func (l *agentLauncher) Cancel(ctx context.Context, handleID string, harness domain.ReviewerHarness) error {
	if handleID == "" {
		return nil
	}
	reviewer, ok := l.reviewers.Reviewer(harness)
	if !ok {
		return fmt.Errorf("no reviewer adapter for harness %q", harness)
	}
	canceller, ok := reviewer.(ports.ReviewerCanceller)
	if !ok {
		return fmt.Errorf("reviewer adapter %q does not support cancellation", harness)
	}
	spec, err := canceller.ReviewCancel(ctx)
	if err != nil {
		return fmt.Errorf("reviewer cancel: %w", err)
	}
	switch spec.Mode {
	case ports.ReviewCancelInterrupt:
		interrupts := spec.Interrupts
		if interrupts <= 0 {
			interrupts = 1
		}
		for i := 0; i < interrupts; i++ {
			if err := l.runtime.Interrupt(ctx, ports.RuntimeHandle{ID: handleID}); err != nil {
				return err
			}
			if i < interrupts-1 {
				timer := time.NewTimer(cancelInterruptDelay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("reviewer adapter %q returned unsupported cancel mode %q", harness, spec.Mode)
	}
}
