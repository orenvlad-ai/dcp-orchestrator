package cli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	card12RecoveryDigest = "d2b7142bc9e5844ba165abe24d3222b3e1a94c3577fba5f6f8d97ec3dbad151b"
	card12RecoveryID     = "dcp-card12-fresh-worker-recovery-" + card12RecoveryDigest
	card12RecoveryMaxLog = 2 << 20
)

type recoverySuperviseOptions struct {
	recovery, identityDigest, inputDigest string
	resultFile, logFile                   string
	supervisorDataDir, supervisorRunFile  string
}

type recoveryResult struct {
	SchemaVersion  string `json:"schemaVersion"`
	RecoveryID     string `json:"recoveryId"`
	IdentityDigest string `json:"identityDigest"`
	InputDigest    string `json:"inputDigest"`
	CodexSessionID string `json:"codexSessionId"`
	TokenCount     int64  `json:"tokenCount"`
	Started        bool   `json:"started"`
	ExitCode       int    `json:"exitCode"`
	LogDigest      string `json:"logDigest"`
	LogOverflow    bool   `json:"logOverflow"`
}

type recoveryProcessExitRequest struct {
	RecoveryID string `json:"recoveryId"`
	Started    bool   `json:"started"`
	ExitCode   int    `json:"exitCode"`
}

func newRecoveryCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{Use: "recovery", Short: "DCP card-12 recovery internals", Hidden: true}
	cmd.AddCommand(newRecoverySuperviseCommand(ctx))
	return cmd
}

func newRecoverySuperviseCommand(ctx *commandContext) *cobra.Command {
	var opts recoverySuperviseOptions
	cmd := &cobra.Command{
		Use: "supervise --recovery <id> -- <command> [args...]", Hidden: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageError{errors.New("recovery command is required")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateRecoverySupervisorOptions(opts); err != nil {
				return usageError{err}
			}
			restore, err := applySupervisorConnection(opts.supervisorDataDir, opts.supervisorRunFile)
			if err != nil {
				return usageError{err}
			}
			defer restore()
			return ctx.runSupervisedRecovery(cmd.Context(), opts, args)
		},
	}
	cmd.Flags().StringVar(&opts.recovery, "recovery", "", "Exact recovery id")
	cmd.Flags().StringVar(&opts.identityDigest, "identity-digest", "", "Exact recovery identity digest")
	cmd.Flags().StringVar(&opts.inputDigest, "input-digest", "", "Exact worker input digest")
	cmd.Flags().StringVar(&opts.resultFile, "result-file", "", "AO-owned worker result path")
	cmd.Flags().StringVar(&opts.logFile, "log-file", "", "AO-owned worker event log path")
	cmd.Flags().StringVar(&opts.supervisorDataDir, "supervisor-data-dir", "", "AO data directory used only by the supervisor")
	cmd.Flags().StringVar(&opts.supervisorRunFile, "supervisor-run-file", "", "AO run-file used only by the supervisor")
	return cmd
}

func validateRecoverySupervisorOptions(opts recoverySuperviseOptions) error {
	if opts.recovery != card12RecoveryID || opts.identityDigest != card12RecoveryDigest ||
		len(opts.inputDigest) != 64 || !lowerHex(opts.inputDigest) ||
		!filepath.IsAbs(opts.supervisorDataDir) || filepath.Clean(opts.supervisorDataDir) != opts.supervisorDataDir ||
		!filepath.IsAbs(opts.supervisorRunFile) || filepath.Clean(opts.supervisorRunFile) != opts.supervisorRunFile {
		return errors.New("invalid bounded card-12 recovery supervisor identity")
	}
	root := filepath.Join(opts.supervisorDataDir, "runtime", "dcp-card12-fresh-worker-recovery", opts.recovery)
	if filepath.Clean(opts.resultFile) != filepath.Join(root, "worker-result.json") ||
		filepath.Clean(opts.logFile) != filepath.Join(root, "worker-events.jsonl") || opts.resultFile == opts.logFile {
		return errors.New("card-12 recovery artifacts are outside the exact root")
	}
	for _, path := range []string{opts.resultFile, opts.logFile} {
		if _, err := os.Lstat(path); err == nil {
			return errors.New("card-12 recovery output exists before process start")
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

type boundedRecoveryLog struct {
	file     *os.File
	written  int64
	overflow bool
}

func (w *boundedRecoveryLog) Write(data []byte) (int, error) {
	remaining := int64(card12RecoveryMaxLog) - w.written
	if remaining > 0 {
		part := data
		if int64(len(part)) > remaining {
			part = part[:remaining]
			w.overflow = true
		}
		n, err := w.file.Write(part)
		w.written += int64(n)
		if err != nil {
			return n, err
		}
	}
	if int64(len(data)) > remaining {
		w.overflow = true
	}
	return len(data), nil
}

func (c *commandContext) runSupervisedRecovery(ctx context.Context, opts recoverySuperviseOptions, argv []string) error {
	logFile, err := os.OpenFile(opts.logFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	writer := &boundedRecoveryLog{file: logFile}
	child := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // trusted daemon constructed exact argv.
	child.Env = supervisedRecoveryChildEnv(os.Environ())
	child.Stdin, child.Stdout, child.Stderr = c.deps.In, io.MultiWriter(c.deps.Out, writer), c.deps.Err
	started := false
	exitCode := -1
	if err := child.Start(); err != nil {
		_ = logFile.Close()
		c.reportRecoveryProcessExit(opts.recovery, false, exitCode)
		return err
	}
	started = true
	waitErr := child.Wait()
	exitCode = 0
	if waitErr != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	if err := logFile.Sync(); err != nil && waitErr == nil {
		waitErr = err
	}
	if err := logFile.Close(); err != nil && waitErr == nil {
		waitErr = err
	}
	sessionID, tokens := parseRecoveryCodexEvents(opts.logFile)
	logBytes, readErr := os.ReadFile(opts.logFile)
	if readErr != nil {
		return readErr
	}
	report := recoveryResult{
		SchemaVersion: "dcp.review-lab.card12-fresh-worker-result/v1", RecoveryID: opts.recovery,
		IdentityDigest: opts.identityDigest, InputDigest: opts.inputDigest,
		CodexSessionID: sessionID, TokenCount: tokens, Started: started, ExitCode: exitCode,
		LogDigest: digestRecoveryBytes(logBytes), LogOverflow: writer.overflow,
	}
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	result, err := os.OpenFile(opts.resultFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := result.Write(append(data, '\n')); err != nil {
		_ = result.Close()
		return err
	}
	if err := result.Sync(); err != nil {
		_ = result.Close()
		return err
	}
	if err := result.Close(); err != nil {
		return err
	}
	c.reportRecoveryProcessExit(opts.recovery, started, exitCode)
	return waitErr
}

func parseRecoveryCodexEvents(path string) (string, int64) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0
	}
	defer func() { _ = file.Close() }()
	var sessionID string
	var tokens int64
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
			Usage    struct {
				InputTokens  int64 `json:"input_tokens"`
				OutputTokens int64 `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		if event.Type == "thread.started" && sessionID == "" {
			sessionID = event.ThreadID
		}
		if event.Type == "turn.completed" {
			tokens = event.Usage.InputTokens + event.Usage.OutputTokens
		}
	}
	return sessionID, tokens
}

func supervisedRecoveryChildEnv(environ []string) []string {
	filtered := make([]string, 0, len(environ))
	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "AO_") || strings.HasPrefix(key, "DCP_") || key == "GH_TOKEN" || key == "GITHUB_TOKEN" {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func (c *commandContext) reportRecoveryProcessExit(recovery string, started bool, exitCode int) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.postLoopbackJSON(ctx, "/internal/dcp/review-lab/card12-recovery/process-exit", recoveryProcessExitRequest{RecoveryID: recovery, Started: started, ExitCode: exitCode}); err != nil {
		c.reportHookFailure("recovery", "process-exited", recovery, err)
	}
}

func digestRecoveryBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
