package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const supervisedExitReportTimeout = 5 * time.Second

func newAgentProcessCommand(ctx *commandContext) *cobra.Command {
	root := &cobra.Command{
		Use:    "agent-process",
		Short:  "Run an AO-managed agent process (internal)",
		Hidden: true,
	}
	root.AddCommand(newAgentProcessSuperviseCommand(ctx))
	return root
}

func newAgentProcessSuperviseCommand(ctx *commandContext) *cobra.Command {
	var sessionID string
	var launchID string
	var idleOnSuccess bool
	cmd := &cobra.Command{
		Use:    "supervise --session <id> --launch <id> -- <command> [args...]",
		Short:  "Supervise one managed agent process (internal)",
		Hidden: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageError{fmt.Errorf("agent command is required")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID = strings.TrimSpace(sessionID)
			launchID = strings.TrimSpace(launchID)
			if !sessionIDPattern.MatchString(sessionID) {
				return usageError{fmt.Errorf("invalid session id")}
			}
			if !sessionIDPattern.MatchString(launchID) {
				return usageError{fmt.Errorf("invalid launch id")}
			}
			ctx.runSupervisedProcess(cmd.Context(), sessionID, launchID, idleOnSuccess, args)
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", "AO session id")
	cmd.Flags().StringVar(&launchID, "launch", "", "AO process launch id")
	cmd.Flags().BoolVar(&idleOnSuccess, "idle-on-success", false, "report a zero process exit as idle")
	return cmd
}

func (c *commandContext) runSupervisedProcess(ctx context.Context, sessionID, launchID string, idleOnSuccess bool, argv []string) {
	child := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // argv is constructed by the selected agent adapter.
	child.Stdin = c.deps.In
	child.Stdout = c.deps.Out
	child.Stderr = c.deps.Err

	if err := child.Start(); err != nil {
		_, _ = fmt.Fprintf(c.deps.Err, "ao: start managed agent: %v\n", err)
		c.reportSupervisedActivity(sessionID, launchID, "exited", "process-exited")
		return
	}
	if idleOnSuccess {
		// Starting the child is the exact machine fact that the bounded one-shot
		// workload is active. Interactive supervisors preserve their upstream
		// signal behavior by leaving idleOnSuccess disabled.
		c.reportSupervisedActivity(sessionID, launchID, "active", "process-started")
	}

	// The child shares the terminal foreground process group and therefore
	// receives Ctrl-C directly. Consume the supervisor's copy so it remains
	// alive long enough to reap the child and publish the exit observation.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	waitErr := child.Wait()
	signal.Stop(interrupts)

	state := "exited"
	if idleOnSuccess && waitErr == nil {
		state = "idle"
	}
	c.reportSupervisedActivity(sessionID, launchID, state, "process-exited")
}

func (c *commandContext) reportSupervisedActivity(sessionID, launchID, state, event string) {
	ctx, cancel := context.WithTimeout(context.Background(), supervisedExitReportTimeout)
	defer cancel()
	path := "sessions/" + sessionID + "/activity"
	req := setActivityAPIRequest{State: state, Event: event, LaunchID: launchID}
	if err := c.postJSON(ctx, path, req, nil); err != nil {
		// Workload-death reconciliation stays fail-closed as exited when exact
		// outcome delivery fails. Keep the delivery failure visible without
		// preventing the terminal's shell.
		c.reportHookFailure("agent-process", event, sessionID, err)
	}
}
