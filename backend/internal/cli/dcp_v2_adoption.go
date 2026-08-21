package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/runfile"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/dcpv2"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

type dcpV2Stage6AdoptOptions struct {
	sourceCommit, sourceTree, installReceiptSHA string
	json                                        bool
}

type dcpV2Stage6AdoptResponse struct {
	SchemaVersion         string                           `json:"schemaVersion"`
	InstalledSourceCommit string                           `json:"installedSourceCommit"`
	InstalledSourceTree   string                           `json:"installedSourceTree"`
	InstallReceiptSHA     string                           `json:"installReceiptSha"`
	Adoption              domain.DCPV2Stage6WorkerAdoption `json:"adoption"`
	Applied               bool                             `json:"applied"`
}

func newDCPV2Stage6AdoptCommand(ctx *commandContext) *cobra.Command {
	var opts dcpV2Stage6AdoptOptions
	cmd := &cobra.Command{
		Use:    "stage6-direct-adopt",
		Short:  "Consume the exact frozen Stage 6 Worker receipt once (internal)",
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
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if live, err := runfile.CheckStale(cfg.RunFilePath); err != nil {
				return fmt.Errorf("inspect run-file: %w", err)
			} else if live != nil {
				return usageError{fmt.Errorf("the DCP daemon is running (pid %d); stop it before Stage 6 adoption", live.PID)}
			}
			store, err := sqlite.Open(cfg.DataDir)
			if err != nil {
				return fmt.Errorf("open stopped Stage 6 store: %w", err)
			}
			defer func() { _ = store.Close() }()
			adoption, applied, err := dcpv2.AdoptStage6WorkerExact(cmd.Context(), store, dcpv2.NewTwinGitHubAdapter(), ctx.deps.Now)
			if err != nil {
				return fmt.Errorf("adopt exact Stage 6 Worker receipt: %w", err)
			}
			response := dcpV2Stage6AdoptResponse{SchemaVersion: "dcp.v2.stage6-direct-adoption/v1",
				InstalledSourceCommit: opts.sourceCommit, InstalledSourceTree: opts.sourceTree,
				InstallReceiptSHA: opts.installReceiptSHA, Adoption: adoption, Applied: applied}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), response)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "task_id=%s\nrevision_id=%s\ncommand_id=%s\naction_id=%s\nruntime_id=%s\nreceipt_id=%s\ncommit_sha=%s\ntree_sha=%s\napplied=%t\n",
				adoption.TaskID, adoption.RevisionID, adoption.CommandID, adoption.ActionID, adoption.RuntimeID,
				adoption.ReceiptID, adoption.CommitSHA, adoption.TreeSHA, applied)
			return err
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.sourceCommit, "source-commit", "", "Exact reviewed managed-source merge commit")
	flags.StringVar(&opts.sourceTree, "source-tree", "", "Exact reviewed managed-source merge tree")
	flags.StringVar(&opts.installReceiptSHA, "install-receipt-sha", "", "SHA-256 of the exact install receipt")
	flags.BoolVar(&opts.json, "json", false, "Print JSON")
	return cmd
}
