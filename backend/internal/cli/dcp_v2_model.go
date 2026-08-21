package cli

import (
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

const directModelOutputLimit = 64 * 1024

type directModelSuperviseOptions struct {
	action, runtime, fence, role     string
	messageFile, resultFile          string
	supervisorDataDir, supervisorRun string
}

type directModelSupervisorResult struct {
	ActionID     string    `json:"actionId"`
	RuntimeID    string    `json:"runtimeId"`
	LaunchFence  string    `json:"launchFence"`
	Started      bool      `json:"started"`
	ExitCode     int       `json:"exitCode"`
	OutputJSON   string    `json:"outputJson"`
	OutputDigest string    `json:"outputDigest"`
	CompletedAt  time.Time `json:"completedAt"`
}

type directModelProcessExitRequest struct {
	ActionID string `json:"actionId"`
}

func newDCPV2ModelCommand(ctx *commandContext) *cobra.Command {
	root := &cobra.Command{Use: "dcp-v2-model", Short: "DCP v2 direct model transport internals", Hidden: true}
	root.AddCommand(newDCPV2ModelSuperviseCommand(ctx))
	return root
}

func newDCPV2ModelSuperviseCommand(ctx *commandContext) *cobra.Command {
	var opts directModelSuperviseOptions
	cmd := &cobra.Command{
		Use: "supervise --action <id> --runtime <id> -- <command> [args...]", Hidden: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageError{errors.New("DCP v2 direct model command is required")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateDirectModelSupervisorOptions(opts); err != nil {
				return usageError{err}
			}
			restore, err := applySupervisorConnection(opts.supervisorDataDir, opts.supervisorRun)
			if err != nil {
				return usageError{err}
			}
			defer restore()
			return ctx.runDirectModelSupervisor(cmd.Context(), opts, args)
		},
	}
	cmd.Flags().StringVar(&opts.action, "action", "", "Exact DCP v2 Action id")
	cmd.Flags().StringVar(&opts.runtime, "runtime", "", "Exact DCP v2 runtime id")
	cmd.Flags().StringVar(&opts.fence, "fence", "", "Exact DCP v2 launch fence")
	cmd.Flags().StringVar(&opts.role, "role", "", "Exact DCP v2 model role")
	cmd.Flags().StringVar(&opts.messageFile, "message-file", "", "AO-owned final-message path")
	cmd.Flags().StringVar(&opts.resultFile, "result-file", "", "AO-owned terminal-result path")
	cmd.Flags().StringVar(&opts.supervisorDataDir, "supervisor-data-dir", "", "AO data directory used only by the supervisor")
	cmd.Flags().StringVar(&opts.supervisorRun, "supervisor-run-file", "", "AO run-file used only by the supervisor")
	return cmd
}

func validateDirectModelSupervisorOptions(opts directModelSuperviseOptions) error {
	if !directV2ID(opts.action) || !directV2ID(opts.runtime) ||
		opts.fence != "model:"+opts.action+":"+opts.runtime ||
		(opts.role != "worker" && opts.role != "reviewer" && opts.role != "repair" && opts.role != "arbiter") ||
		!filepath.IsAbs(opts.supervisorDataDir) || filepath.Clean(opts.supervisorDataDir) != opts.supervisorDataDir ||
		!filepath.IsAbs(opts.supervisorRun) || filepath.Clean(opts.supervisorRun) != opts.supervisorRun {
		return errors.New("invalid bounded DCP v2 supervisor identity")
	}
	root := filepath.Join(opts.supervisorDataDir, "runtime", "dcp-v2", opts.action, opts.runtime)
	if filepath.Clean(opts.messageFile) != filepath.Join(root, "last-message.json") ||
		filepath.Clean(opts.resultFile) != filepath.Join(root, "terminal.json") || opts.messageFile == opts.resultFile {
		return errors.New("DCP v2 supervisor artifacts are outside the exact runtime root")
	}
	if _, err := os.Lstat(opts.resultFile); err == nil {
		return errors.New("DCP v2 terminal result exists before process start")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func directV2ID(value string) bool {
	if !strings.HasPrefix(value, "v2-") || len(value) != 43 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "v2-"))
	return err == nil
}

func (c *commandContext) runDirectModelSupervisor(ctx context.Context, opts directModelSuperviseOptions, argv []string) error {
	child := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // exact daemon-constructed argv.
	child.Env = directModelChildEnv(os.Environ())
	child.Stdin, child.Stdout, child.Stderr = c.deps.In, c.deps.Out, c.deps.Err
	started := false
	exitCode := -1
	startErr := child.Start()
	var waitErr error
	if startErr == nil {
		started = true
		waitErr = child.Wait()
		exitCode = 0
		if waitErr != nil {
			exitCode = 1
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) {
				exitCode = exitErr.ExitCode()
			}
		}
	} else {
		waitErr = startErr
	}

	output := json.RawMessage(`{}`)
	if started && exitCode == 0 {
		bounded, err := readDirectModelOutput(opts.messageFile, opts.role)
		if err != nil {
			exitCode, waitErr = 1, err
		} else {
			output = bounded
		}
	}
	report := directModelSupervisorResult{ActionID: opts.action, RuntimeID: opts.runtime, LaunchFence: opts.fence,
		Started: started, ExitCode: exitCode, OutputJSON: string(output), OutputDigest: directModelOutputDigest(output), CompletedAt: c.deps.Now().UTC()}
	if err := writeDirectModelResult(opts.resultFile, report); err != nil {
		return err
	}
	c.reportDirectModelProcessExit(opts.action)
	return waitErr
}

func readDirectModelOutput(path, role string) (json.RawMessage, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > directModelOutputLimit || info.Mode().Perm()&0o022 != 0 {
		return nil, errors.New("DCP v2 model output is not an exact owner-controlled bounded file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, directModelOutputLimit+1))
	if err != nil || len(data) > directModelOutputLimit {
		return nil, errors.New("DCP v2 model output is unreadable or unbounded")
	}
	var value any
	if json.Valid(data) {
		if err := rejectDuplicateDirectModelJSONKeys(data); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(data, &value)
		canonical, _ := json.Marshal(value)
		return canonical, nil
	}
	if role != "worker" && role != "repair" {
		return nil, errors.New("DCP v2 decision output is not structured JSON")
	}
	canonical, _ := json.Marshal(map[string]string{"message": strings.TrimSpace(string(data))})
	return canonical, nil
}

func rejectDuplicateDirectModelJSONKeys(data []byte) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var value func() error
	value = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok || seen[key] {
					return errors.New("DCP v2 model output contains a duplicate object key")
				}
				seen[key] = true
				if err := value(); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return errors.New("DCP v2 model output object is unterminated")
			}
		case '[':
			for decoder.More() {
				if err := value(); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return errors.New("DCP v2 model output array is unterminated")
			}
		default:
			return errors.New("DCP v2 model output contains an invalid delimiter")
		}
		return nil
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("DCP v2 model output contains trailing data")
	}
	return nil
}

func directModelOutputDigest(output json.RawMessage) string {
	sum := sha256.Sum256(output)
	return hex.EncodeToString(sum[:])
}

func writeDirectModelResult(path string, report directModelSupervisorResult) error {
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func directModelChildEnv(environ []string) []string {
	allowed := map[string]bool{
		"CODEX_HOME": true, "COLORTERM": true, "COMSPEC": true,
		"HOME": true, "LANG": true, "LOGNAME": true,
		"PATH": true, "PATHEXT": true, "SHELL": true, "SSL_CERT_DIR": true,
		"SSL_CERT_FILE": true, "SYSTEMROOT": true, "TEMP": true, "TERM": true,
		"TMP": true, "TMPDIR": true, "USER": true, "USERPROFILE": true,
	}
	filtered := make([]string, 0, len(environ))
	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		if allowed[key] || strings.HasPrefix(key, "LC_") {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func (c *commandContext) reportDirectModelProcessExit(actionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.postLoopbackJSON(ctx, "/internal/dcp/v2/model/process-exit", directModelProcessExitRequest{ActionID: actionID}); err != nil {
		c.reportHookFailure("dcp-v2-model", "process-exited", actionID, err)
	}
}
