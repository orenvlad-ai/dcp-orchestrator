package dcpterminalmerge

import (
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func exactTestColdStartRecovery() domain.DCPCard12ColdStartRecovery {
	return domain.DCPCard12ColdStartRecovery{
		RecoveryID: ColdStartRecoveryID, Generation: 1, IdentityDigest: ColdStartRecoveryDigest, ContractCommit: coldStartContractCommit,
		PredecessorContinuationID: ModelFreeRebaseContinuationID, IncidentID: exactSuccessorIncidentID,
		AdmissionID: "dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34", SessionID: ArbiterSessionB,
		TaskID: ArbiterTaskB, ProjectID: ProjectID, Repository: RepositoryFullName,
		WorktreePath: "/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/dcp-review-lab/dcp-review-lab-12",
		SourceBranch: "ao/dcp-review-lab-12/root", PRURL: "https://github.com/orenvlad-ai/dcp-review-lab/pull/9", PRNumber: 9,
		OldHead: "d4fcb68051ae113ed497d02151a759800ee85633", CurrentMain: "b34b31b5443890e69128db2862726950a6bbac0d",
		ProviderBase: modelFreeProviderBaseSHA, ConflictPath: arbiterConflictPath,
		MarkerDigest: "5850bba009db75bf47ff88aef2d2cecbdba89c68967f51a8cdb60f48e968dc1a",
		StatusDigest: "fd7d8ff8f4918e9960e5e46e01c70a877d4218b3fa1e884ecc1723935b1c9886",
		Stage1Blob:   "ed237ce2dd2684371797e22634480ffb28dc9e77", Stage2Blob: "a4c945ba7328504f2efea44f076a1407c6aa7b47",
		Stage3Blob: modelFreeResolvedBlob, ResolvedBytesDigest: "2a5da25a78ff8bcd9aff4493f195eaefecbc70c3d4db8902dda468ccf69e5e46",
		ResolvedBlob: modelFreeResolvedBlob, PushRef: "refs/heads/ao/dcp-review-lab-12/root",
		PushLeaseOldHead:           "d4fcb68051ae113ed497d02151a759800ee85633",
		UnauthorizedWorkerThread11: "019ff9f3-cad3-73c1-bcee-293efe857349", UnauthorizedWorkerTokens11: 33238,
		UnauthorizedWorkerThread12: "019ff9f3-cbe6-71e2-8636-ea6259a7e7d1", UnauthorizedWorkerTokens12: 33573,
		Status: domain.DCPColdStartRecoveryAuthorized,
	}
}

func TestExactColdStartRecoveryRejectsIdentityAndBudgetDrift(t *testing.T) {
	row := exactTestColdStartRecovery()
	if !exactColdStartRecovery(row) {
		t.Fatal("exact cold-start recovery rejected")
	}
	for name, mutate := range map[string]func(*domain.DCPCard12ColdStartRecovery){
		"foreign contract": func(r *domain.DCPCard12ColdStartRecovery) { r.ContractCommit = strings.Repeat("0", 40) },
		"foreign worktree": func(r *domain.DCPCard12ColdStartRecovery) { r.WorktreePath += "-other" },
		"moved main":       func(r *domain.DCPCard12ColdStartRecovery) { r.CurrentMain = strings.Repeat("0", 40) },
		"marker drift":     func(r *domain.DCPCard12ColdStartRecovery) { r.MarkerDigest = strings.Repeat("0", 64) },
		"worker call":      func(r *domain.DCPCard12ColdStartRecovery) { r.WorkerModelCallCount = 1 },
		"arbiter call":     func(r *domain.DCPCard12ColdStartRecovery) { r.ArbiterModelCallCount = 1 },
		"token erasure":    func(r *domain.DCPCard12ColdStartRecovery) { r.UnauthorizedWorkerTokens12 = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := row
			mutate(&bad)
			if exactColdStartRecovery(bad) {
				t.Fatal("drifted recovery accepted")
			}
		})
	}
	if row.UnauthorizedWorkerTokens11+row.UnauthorizedWorkerTokens12 != 66811 {
		t.Fatal("immutable unauthorized token accounting drifted")
	}
}

func TestColdStartBackupManifestIsDeterministicAndBindsBytes(t *testing.T) {
	files := map[string][]byte{"git/HEAD": []byte("ref: refs/heads/exact\n"), "worktree/conflict.txt": []byte("exact")}
	first := backupManifest(files)
	second := backupManifest(map[string][]byte{"worktree/conflict.txt": []byte("exact"), "git/HEAD": []byte("ref: refs/heads/exact\n")})
	if string(first) != string(second) {
		t.Fatal("manifest depends on map iteration order")
	}
	files["worktree/conflict.txt"] = []byte("drift")
	if digestBytes(backupManifest(files)) == digestBytes(first) {
		t.Fatal("manifest did not bind backup bytes")
	}
}
