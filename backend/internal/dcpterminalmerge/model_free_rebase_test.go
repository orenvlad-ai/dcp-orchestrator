package dcpterminalmerge

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func exactTestModelFreeRebaseContinuation() domain.DCPCard12ModelFreeRebaseContinuation {
	return domain.DCPCard12ModelFreeRebaseContinuation{
		ContinuationID: ModelFreeRebaseContinuationID, Generation: 1,
		IdentityDigest: ModelFreeRebaseContinuationDigest, ContractCommit: modelFreeContractCommit,
		PredecessorRecoveryID: FreshWorkerRecoveryID, IncidentID: exactSuccessorIncidentID,
		AdmissionID: "dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34", SessionID: ArbiterSessionB,
		TaskID: ArbiterTaskB, ProjectID: ProjectID, Repository: RepositoryFullName,
		WorktreePath: "/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/dcp-review-lab/dcp-review-lab-12",
		SourceBranch: "ao/dcp-review-lab-12/root", PRURL: "https://github.com/orenvlad-ai/dcp-review-lab/pull/9", PRNumber: 9,
		OldHead: "d4fcb68051ae113ed497d02151a759800ee85633", CurrentMain: "b34b31b5443890e69128db2862726950a6bbac0d",
		PredecessorInputDigest: "1b79923f68e0a53414579f059a1984fbcdae7aea4593d86c7fa4ae62027114bd",
		InputArtifactDigest:    "131ab471a0509f4851f94e056998b3a620468a69bdd3b19435d2a225da01d393",
		ResultArtifactDigest:   "e284aeb37d6fdd7ec86ee3ea6ad2272eee7d4856d5a39eb2894c89dd83d0836b",
		LogArtifactDigest:      "8909c2cb81e96beb47414576fb6e1c54e9895fcf34e38e2865d87ca821b46a20",
		RebaseMetadataDigest:   "db9933afbc18ffbd031818990e2b350845c766a5f0ae8ed37fae8f4e8a66f371",
		ResolvedBytesDigest:    "2a5da25a78ff8bcd9aff4493f195eaefecbc70c3d4db8902dda468ccf69e5e46",
		PushRef:                "refs/heads/ao/dcp-review-lab-12/root", PushLeaseOldHead: "d4fcb68051ae113ed497d02151a759800ee85633",
		Status: domain.DCPModelFreeRebaseAuthorized,
	}
}

func TestExactModelFreeRebaseContinuationRejectsIdentityDrift(t *testing.T) {
	row := exactTestModelFreeRebaseContinuation()
	if !exactModelFreeRebaseContinuation(row) {
		t.Fatal("exact model-free continuation was rejected")
	}
	for name, mutate := range map[string]func(*domain.DCPCard12ModelFreeRebaseContinuation){
		"foreign contract": func(r *domain.DCPCard12ModelFreeRebaseContinuation) { r.ContractCommit = strings.Repeat("0", 40) },
		"foreign worktree": func(r *domain.DCPCard12ModelFreeRebaseContinuation) { r.WorktreePath += "-other" },
		"moved main":       func(r *domain.DCPCard12ModelFreeRebaseContinuation) { r.CurrentMain = strings.Repeat("0", 40) },
		"foreign lease":    func(r *domain.DCPCard12ModelFreeRebaseContinuation) { r.PushLeaseOldHead = strings.Repeat("0", 40) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := row
			mutate(&bad)
			if exactModelFreeRebaseContinuation(bad) {
				t.Fatal("drifted model-free continuation was accepted")
			}
		})
	}
}

func TestExactPreservedRebasePreflight(t *testing.T) {
	if os.Getenv("DCP_EXACT_CARD12_PREFLIGHT") != "1" {
		t.Skip("exact retained card-12 state is only available in the bounded lab")
	}
	executor := &modelFreeRebaseExecutor{}
	if err := executor.validatePreservedRebase(context.Background(), exactTestModelFreeRebaseContinuation()); err != nil {
		t.Fatalf("exact preserved rebase preflight: %v", err)
	}
}
