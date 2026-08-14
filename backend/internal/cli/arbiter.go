package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type arbiterSuperviseOptions struct {
	handle            string
	incident          string
	identityDigest    string
	inputDigest       string
	resultFile        string
	resultSchema      string
	supervisorDataDir string
	supervisorRunFile string
}

const (
	dcpArbiterSuccessorHandle = "dcp-global-release-arbiter-v1-successor"
	dcpArbiterSuccessorDigest = "3c62ea80b56ef94165519d4f01e4c449c320bff22d16b902dd68d4a1a355ea7d"
	dcpArbiterSuccessorID     = "dcp-arbiter-successor-" + dcpArbiterSuccessorDigest
)

type arbiterDecisionRequest struct {
	IncidentID string          `json:"incidentId"`
	Decision   json.RawMessage `json:"decision"`
}

type arbiterProcessExitRequest struct {
	IncidentID    string `json:"incidentId"`
	Started       bool   `json:"started"`
	ExitCode      int    `json:"exitCode"`
	ResultFailure string `json:"resultFailure,omitempty"`
}

func newArbiterCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{Use: "arbiter", Short: "DCP release arbiter internals", Hidden: true}
	cmd.AddCommand(newArbiterSuperviseCommand(ctx))
	return cmd
}

func newArbiterSuperviseCommand(ctx *commandContext) *cobra.Command {
	var opts arbiterSuperviseOptions
	cmd := &cobra.Command{
		Use: "supervise --incident <id> -- <command> [args...]", Short: "Supervise the one DCP arbiter process (internal)", Hidden: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageError{errors.New("arbiter command is required")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateArbiterSupervisorOptions(opts); err != nil {
				return usageError{err}
			}
			restore, err := applySupervisorConnection(opts.supervisorDataDir, opts.supervisorRunFile)
			if err != nil {
				return usageError{err}
			}
			defer restore()
			return ctx.runSupervisedArbiter(cmd.Context(), opts, args)
		},
	}
	cmd.Flags().StringVar(&opts.handle, "handle", "", "Exact arbiter runtime handle (internal)")
	cmd.Flags().StringVar(&opts.incident, "incident", "", "Exact DCP arbiter incident id")
	cmd.Flags().StringVar(&opts.identityDigest, "identity-digest", "", "Exact incident identity digest")
	cmd.Flags().StringVar(&opts.inputDigest, "input-digest", "", "Exact frozen input digest")
	cmd.Flags().StringVar(&opts.resultFile, "result-file", "", "AO-owned arbiter result path")
	cmd.Flags().StringVar(&opts.resultSchema, "result-schema", "", "AO-owned arbiter schema path")
	cmd.Flags().StringVar(&opts.supervisorDataDir, "supervisor-data-dir", "", "AO data directory used only by the supervisor")
	cmd.Flags().StringVar(&opts.supervisorRunFile, "supervisor-run-file", "", "AO run-file used only by the supervisor")
	return cmd
}

func validateArbiterSupervisorOptions(opts arbiterSuperviseOptions) error {
	original := opts.handle == "dcp-global-release-arbiter-v1" && strings.HasPrefix(opts.incident, "dcp-global-release-") && len(opts.incident) == 83
	successor := opts.handle == dcpArbiterSuccessorHandle && opts.incident == dcpArbiterSuccessorID && opts.identityDigest == dcpArbiterSuccessorDigest
	future := opts.handle == opts.incident && strings.HasPrefix(opts.incident, "dcp-future-arbiter-") && len(opts.incident) == 83 &&
		strings.TrimPrefix(opts.incident, "dcp-future-arbiter-") == opts.identityDigest
	if (!original && !successor && !future) || len(opts.identityDigest) != 64 || len(opts.inputDigest) != 64 || !lowerHex(opts.identityDigest) || !lowerHex(opts.inputDigest) ||
		!filepath.IsAbs(opts.supervisorDataDir) || filepath.Clean(opts.supervisorDataDir) != opts.supervisorDataDir ||
		!filepath.IsAbs(opts.supervisorRunFile) || filepath.Clean(opts.supervisorRunFile) != opts.supervisorRunFile {
		return errors.New("invalid bounded arbiter supervisor identity")
	}
	rootName := "dcp-arbiter"
	if successor {
		rootName = "dcp-arbiter-successor"
	} else if future {
		rootName = "dcp-future-arbiter"
	}
	root := filepath.Join(opts.supervisorDataDir, "runtime", rootName, opts.incident)
	result, schema := filepath.Clean(opts.resultFile), filepath.Clean(opts.resultSchema)
	if result != filepath.Join(root, "result.json") || schema != filepath.Join(root, "schema.json") || result == schema {
		return errors.New("arbiter artifacts are outside the exact incident root")
	}
	info, err := os.Lstat(schema)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("arbiter schema is not an owner-controlled regular file")
	}
	if _, err := os.Lstat(result); err == nil {
		return errors.New("arbiter result exists before process start")
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (c *commandContext) runSupervisedArbiter(ctx context.Context, opts arbiterSuperviseOptions, argv []string) error {
	child := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // trusted daemon constructed exact argv.
	child.Env = supervisedArbiterChildEnv(os.Environ())
	child.Stdin, child.Stdout, child.Stderr = c.deps.In, c.deps.Out, c.deps.Err
	if err := child.Start(); err != nil {
		c.reportArbiterProcessExit(opts.incident, false, -1, "child_not_started")
		return err
	}
	waitErr := child.Wait()
	exitCode := 0
	if waitErr != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	resultFailure := ""
	if waitErr == nil {
		result, err := readBoundedArbiterResult(opts.resultFile)
		if err != nil {
			resultFailure = "malformed_result"
			waitErr = err
		} else if err := c.postLoopbackJSON(ctx, "/internal/dcp/review-lab/arbiter/decision", arbiterDecisionRequest{IncidentID: opts.incident, Decision: result}); err != nil {
			resultFailure = "submit_failed"
			waitErr = fmt.Errorf("record DCP arbiter result: %w", err)
		}
	}
	// The single successor's input/schema/result artifacts are immutable audit
	// evidence after its fenced call. Durable state makes any restart replay
	// inert; preserving the files cannot authorize another call.
	if opts.handle == "dcp-global-release-arbiter-v1" && resultFailure != "submit_failed" {
		_ = os.Remove(opts.resultFile)
		_ = os.Remove(opts.resultSchema)
	}
	c.reportArbiterProcessExit(opts.incident, true, exitCode, resultFailure)
	return waitErr
}

func readBoundedArbiterResult(path string) (json.RawMessage, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > 16384 || info.Mode().Perm()&0o022 != 0 {
		return nil, errors.New("arbiter result is not an exact owner-controlled bounded file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil || len(data) > 16384 || !json.Valid(data) {
		return nil, errors.New("arbiter result is malformed or unbounded")
	}
	return json.RawMessage(data), nil
}

func supervisedArbiterChildEnv(environ []string) []string {
	filtered := make([]string, 0, len(environ))
	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "AO_") || strings.HasPrefix(key, "DCP_ARBITER_") || key == "GH_TOKEN" || key == "GITHUB_TOKEN" {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func (c *commandContext) reportArbiterProcessExit(incident string, started bool, exitCode int, resultFailure string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req := arbiterProcessExitRequest{IncidentID: incident, Started: started, ExitCode: exitCode, ResultFailure: resultFailure}
	if err := c.postLoopbackJSON(ctx, "/internal/dcp/review-lab/arbiter/process-exit", req); err != nil {
		c.reportHookFailure("arbiter", "process-exited", incident, err)
	}
}

func lowerHex(value string) bool {
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return value != ""
}
