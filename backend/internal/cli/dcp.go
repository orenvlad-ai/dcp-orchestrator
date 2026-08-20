package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/runfile"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

const dcpV2Stage5AuthorityCommit = "4143982eb054a40537d963356c209bfe8447ba31"

type dcpV2Stage5ActivateOptions struct {
	sourceCommit      string
	sourceTree        string
	installReceiptSHA string
	targetPath        string
	json              bool
}

type dcpV2Stage5ActivateResponse struct {
	Activation     domain.DCPV2Stage5Activation `json:"activation"`
	ProjectID      string                       `json:"projectId"`
	ProjectPath    string                       `json:"projectPath"`
	Created        bool                         `json:"created"`
	ProjectCreated bool                         `json:"projectCreated"`
}

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
	root.AddCommand(newDCPV2Stage5ActivateCommand(ctx))
	return root
}

func newDCPV2Stage5ActivateCommand(ctx *commandContext) *cobra.Command {
	var opts dcpV2Stage5ActivateOptions
	cmd := &cobra.Command{
		Use:    "stage5-activate",
		Short:  "Apply the exact stopped DCP v2 twin activation (internal)",
		Hidden: true,
		Args:   noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			for name, value := range map[string]string{
				"source-commit": opts.sourceCommit, "source-tree": opts.sourceTree,
				"install-receipt-sha": opts.installReceiptSHA,
			} {
				if !isLowerHex(value, map[string]int{"source-commit": 40, "source-tree": 40, "install-receipt-sha": 64}[name]) {
					return usageError{fmt.Errorf("--%s must be exact lowercase hexadecimal identity", name)}
				}
			}
			if Commit != "" && Commit != opts.sourceCommit {
				return usageError{fmt.Errorf("--source-commit does not match the installed binary commit")}
			}
			targetPath := filepath.Clean(strings.TrimSpace(opts.targetPath))
			if targetPath == "." || !filepath.IsAbs(targetPath) || filepath.Base(targetPath) != "dcp-wbc-integration-lab" || filepath.Base(filepath.Dir(targetPath)) != "targets" {
				return usageError{fmt.Errorf("--target-path must be the absolute exact targets/dcp-wbc-integration-lab path")}
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if live, err := runfile.CheckStale(cfg.RunFilePath); err != nil {
				return fmt.Errorf("inspect run-file: %w", err)
			} else if live != nil {
				return usageError{fmt.Errorf("the AO daemon is running (pid %d); stop it before Stage 5 activation", live.PID)}
			}
			activation := domain.DCPV2Stage5Activation{
				ActivationID: "dcp-v2-twin-stage5", AuthorityCommit: dcpV2Stage5AuthorityCommit,
				SourceCommit: opts.sourceCommit, SourceTree: opts.sourceTree,
				InstallReceiptSHA:  opts.installReceiptSHA,
				TargetSpecVersion:  "dcp-wbc-integration-lab/v2",
				TargetPolicyDigest: domain.DCPWBCIntegrationTwinPolicyDigest(),
				Repository:         "orenvlad-ai/dcp-wbc-integration-lab", RepositoryID: 1340359100, OwnerID: 237411244,
				BaseRef: "main", RequiredCheck: "baseline", IssuerKind: "dcp/v2", IssuerActor: "orenvlad-ai",
				IssuerEvent: "repository_dispatch", IssuerEventType: "dcp-admission-v2", WorkflowID: 338377713,
				Environment: "dcp-wbc-integration-lab-selectel", Service: "dcp-wbc-integration-lab",
				Adapter: "selectel-systemd/v1", ActivatedAt: ctx.deps.Now().UTC(),
			}
			spec, ok := domain.DCPPolicyTarget("dcp-wbc-integration-lab", "live-runtime")
			if !ok {
				return fmt.Errorf("exact DCP v2 twin target is absent from the managed-source allowlist")
			}
			project := domain.ProjectRecord{
				ID: spec.Target, Path: targetPath, RepoOriginURL: spec.OriginURL, DisplayName: spec.Target,
				RegisteredAt: activation.ActivatedAt, Kind: domain.ProjectKindSingleRepo,
				Config: domain.ProjectConfig{
					DefaultBranch: spec.DefaultBranch, SessionPrefix: spec.SessionPrefix, AgentRules: spec.AgentRules,
					Worker: domain.RoleOverride{Harness: domain.HarnessCodex, AgentConfig: domain.AgentConfig{
						Permissions: domain.PermissionModeAcceptEdits, DCPReviewLabNetwork: true,
					}},
					Reviewers: []domain.ReviewerConfig{{Harness: domain.ReviewerCodex}},
				},
			}
			store, err := sqlite.Open(cfg.DataDir)
			if err != nil {
				return fmt.Errorf("open stopped Stage 5 store: %w", err)
			}
			defer func() { _ = store.Close() }()
			created, projectCreated, err := store.ActivateDCPV2Stage5WithProject(cmd.Context(), activation, project)
			if err != nil {
				return fmt.Errorf("activate DCP v2 Stage 5: %w", err)
			}
			stored, err := store.GetDCPV2Stage5Activation(cmd.Context())
			if err != nil {
				return fmt.Errorf("read back DCP v2 Stage 5 activation: %w", err)
			}
			storedProject, found, err := store.GetProject(cmd.Context(), project.ID)
			if err != nil || !found {
				return fmt.Errorf("read back exact DCP v2 twin project: found=%t: %w", found, err)
			}
			response := dcpV2Stage5ActivateResponse{Activation: stored, ProjectID: storedProject.ID, ProjectPath: storedProject.Path, Created: created, ProjectCreated: projectCreated}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), response)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "activation_id=%s\nsource_commit=%s\nsource_tree=%s\ninstall_receipt_sha=%s\ntarget_policy_digest=%s\nproject_id=%s\nproject_path=%s\ncreated=%t\nproject_created=%t\n",
				stored.ActivationID, stored.SourceCommit, stored.SourceTree, stored.InstallReceiptSHA,
				stored.TargetPolicyDigest, storedProject.ID, storedProject.Path, created, projectCreated)
			return err
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.sourceCommit, "source-commit", "", "Exact reviewed managed-source merge commit")
	f.StringVar(&opts.sourceTree, "source-tree", "", "Exact reviewed managed-source merge tree")
	f.StringVar(&opts.installReceiptSHA, "install-receipt-sha", "", "SHA-256 of the exact install receipt")
	f.StringVar(&opts.targetPath, "target-path", "", "Absolute exact local integration-twin repository path")
	f.BoolVar(&opts.json, "json", false, "Print JSON")
	return cmd
}

func isLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
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
