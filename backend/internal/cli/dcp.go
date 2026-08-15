package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type dcpPolicySubmitOptions struct {
	taskID     string
	target     string
	profile    string
	repository string
	prompt     string
	json       bool
}

type dcpPolicySubmitRequest struct {
	TaskID     string `json:"taskId"`
	Target     string `json:"target"`
	Profile    string `json:"profile"`
	Repository string `json:"repository"`
	Prompt     string `json:"prompt"`
}

type dcpPolicySubmitResponse struct {
	Task struct {
		TaskID       string `json:"taskId"`
		Target       string `json:"target"`
		Profile      string `json:"profile"`
		Repository   string `json:"repository"`
		SessionID    string `json:"sessionId"`
		CardNumber   int64  `json:"cardNumber"`
		WorktreePath string `json:"worktreePath"`
		SourceBranch string `json:"sourceBranch"`
		State        string `json:"state"`
		Revision     int64  `json:"revision"`
	} `json:"task"`
	Duplicate bool `json:"duplicate"`
}

func newDCPCommand(ctx *commandContext) *cobra.Command {
	root := &cobra.Command{Use: "dcp", Short: "DCP internal lab commands", Hidden: true}
	root.AddCommand(newDCPPolicySubmitCommand(ctx))
	return root
}

func newDCPPolicySubmitCommand(ctx *commandContext) *cobra.Command {
	var opts dcpPolicySubmitOptions
	cmd := &cobra.Command{
		Use:    "submit",
		Short:  "Submit one exact DCP review-lab policy task (internal)",
		Hidden: true,
		Args:   noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			for name, value := range map[string]string{
				"task-id": opts.taskID, "target": opts.target, "profile": opts.profile,
				"repository": opts.repository, "prompt": opts.prompt,
			} {
				if strings.TrimSpace(value) == "" {
					return usageError{fmt.Errorf("--%s is required", name)}
				}
			}
			var response dcpPolicySubmitResponse
			err := ctx.postJSON(cmd.Context(), "dcp/tasks/policy", dcpPolicySubmitRequest{
				TaskID: opts.taskID, Target: opts.target, Profile: opts.profile,
				Repository: opts.repository, Prompt: opts.prompt,
			}, &response)
			if err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), response)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "task_id=%s\nsession_id=%s\ncard_number=%d\nworktree=%s\nbranch=%s\nstate=%s\nrevision=%d\nduplicate=%t\n",
				response.Task.TaskID, response.Task.SessionID, response.Task.CardNumber,
				response.Task.WorktreePath, response.Task.SourceBranch, response.Task.State,
				response.Task.Revision, response.Duplicate)
			return err
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.taskID, "task-id", "", "Exact future DCP task id")
	f.StringVar(&opts.target, "target", "", "Exact DCP target")
	f.StringVar(&opts.profile, "profile", "", "Exact DCP profile")
	f.StringVar(&opts.repository, "repository", "", "Exact public synthetic repository")
	f.StringVar(&opts.prompt, "prompt", "", "Bounded one-line task prompt")
	f.BoolVar(&opts.json, "json", false, "Print JSON")
	return cmd
}
