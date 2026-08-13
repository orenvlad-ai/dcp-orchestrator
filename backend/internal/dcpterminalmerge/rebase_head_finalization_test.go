package dcpterminalmerge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func exactTestRebaseHeadFinalization() domain.DCPCard12RebaseHeadFinalization {
	return domain.DCPCard12RebaseHeadFinalization{
		FinalizationID: RebaseHeadFinalizationID, Generation: 1, IdentityDigest: RebaseHeadFinalizationDigest,
		ContractCommit: rebaseHeadContractCommit, PredecessorRecoveryID: ColdStartRecoveryID,
		IncidentID: exactSuccessorIncidentID, AdmissionID: "dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34",
		SessionID: ArbiterSessionB, TaskID: ArbiterTaskB, ProjectID: ProjectID, Repository: RepositoryFullName,
		WorktreePath: "/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/dcp-review-lab/dcp-review-lab-12",
		SourceBranch: "ao/dcp-review-lab-12/root", PRURL: "https://github.com/orenvlad-ai/dcp-review-lab/pull/9", PRNumber: 9,
		OldHead: "d4fcb68051ae113ed497d02151a759800ee85633", CandidateHead: "4de6ff1a0b80223a9b32a05ba68cf0b665296081",
		CurrentMain: "b34b31b5443890e69128db2862726950a6bbac0d", ProviderBase: modelFreeProviderBaseSHA,
		ConflictPath: arbiterConflictPath, ResolvedBytesDigest: "2a5da25a78ff8bcd9aff4493f195eaefecbc70c3d4db8902dda468ccf69e5e46",
		ResolvedBlob: modelFreeResolvedBlob, CandidateDiffDigest: "b415f3cc21e091afc82e8fbf5fa1a6f0e64ec42465ea8702efe4c681f47295f7",
		CleanStatusDigest: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		RebaseHeadDigest:  "657c15026f6e8f51e96e6ff6c2ae94a5d6f4031ec95f07030b52f6226cc4d810",
		OrigHeadDigest:    "657c15026f6e8f51e96e6ff6c2ae94a5d6f4031ec95f07030b52f6226cc4d810",
		BackupPath:        "/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/evidence/dcp-card12-cold-start-recovery/" + ColdStartRecoveryID,
		BackupDigest:      "82d0e5834375c380069e7d48a7fdb2066371670d92733ce59545718469a4f3dd",
		PushRef:           "refs/heads/ao/dcp-review-lab-12/root", PushLeaseOldHead: "d4fcb68051ae113ed497d02151a759800ee85633",
		UnauthorizedWorkerTokens11: 33238, UnauthorizedWorkerTokens12: 33573,
		Status: domain.DCPRebaseHeadFinalizationAuthorized, Revision: 2,
	}
}

func TestExactRebaseHeadFinalizationRejectsIdentityAndBudgetDrift(t *testing.T) {
	row := exactTestRebaseHeadFinalization()
	if !exactRebaseHeadFinalization(row) {
		t.Fatal("exact finalization rejected")
	}
	for name, mutate := range map[string]func(*domain.DCPCard12RebaseHeadFinalization){
		"foreign contract":   func(r *domain.DCPCard12RebaseHeadFinalization) { r.ContractCommit = strings.Repeat("0", 40) },
		"candidate drift":    func(r *domain.DCPCard12RebaseHeadFinalization) { r.CandidateHead = strings.Repeat("0", 40) },
		"pseudoref drift":    func(r *domain.DCPCard12RebaseHeadFinalization) { r.RebaseHeadDigest = strings.Repeat("0", 64) },
		"backup drift":       func(r *domain.DCPCard12RebaseHeadFinalization) { r.BackupDigest = strings.Repeat("0", 64) },
		"worker call":        func(r *domain.DCPCard12RebaseHeadFinalization) { r.WorkerModelCallCount = 1 },
		"arbiter call":       func(r *domain.DCPCard12RebaseHeadFinalization) { r.ArbiterModelCallCount = 1 },
		"token erasure":      func(r *domain.DCPCard12RebaseHeadFinalization) { r.UnauthorizedWorkerTokens12 = 0 },
		"replacement branch": func(r *domain.DCPCard12RebaseHeadFinalization) { r.SourceBranch += "-other" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := row
			mutate(&bad)
			if exactRebaseHeadFinalization(bad) {
				t.Fatal("drifted finalization accepted")
			}
		})
	}
	if row.UnauthorizedWorkerTokens11+row.UnauthorizedWorkerTokens12 != 66811 {
		t.Fatal("immutable unauthorized token accounting drifted")
	}
}

func TestRebaseHeadFinalizationAcceptsOnlyExactRegularInertPseudorefs(t *testing.T) {
	row := exactTestRebaseHeadFinalization()
	gitDir, commonDir := t.TempDir(), t.TempDir()
	want := []byte(row.OldHead + "\n")
	for _, name := range []string{"REBASE_HEAD", "ORIG_HEAD"} {
		if err := os.WriteFile(filepath.Join(gitDir, name), want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	x := &rebaseHeadFinalizationExecutor{}
	if err := x.validatePseudorefs(row, gitDir, commonDir); err != nil {
		t.Fatalf("exact inert pseudorefs rejected: %v", err)
	}
	t.Run("changed bytes", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(gitDir, "REBASE_HEAD"), []byte(strings.Repeat("0", 40)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := x.validatePseudorefs(row, gitDir, commonDir); err == nil {
			t.Fatal("drifted REBASE_HEAD accepted")
		}
		if err := os.WriteFile(filepath.Join(gitDir, "REBASE_HEAD"), want, 0o644); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("active rebase directory", func(t *testing.T) {
		if err := os.Mkdir(filepath.Join(gitDir, "rebase-merge"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := x.validatePseudorefs(row, gitDir, commonDir); err == nil {
			t.Fatal("active rebase directory accepted")
		}
	})
}
