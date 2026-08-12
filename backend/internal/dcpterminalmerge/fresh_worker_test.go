package dcpterminalmerge

import (
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func exactTestFreshWorkerRecovery() domain.DCPCard12FreshWorkerRecovery {
	return domain.DCPCard12FreshWorkerRecovery{
		RecoveryID: FreshWorkerRecoveryID, RecoveryGeneration: 1, RecoveryIdentityDigest: FreshWorkerRecoveryDigest,
		IncidentID: exactSuccessorIncidentID, IncidentGeneration: 1,
		SuccessorAttemptID: ArbiterSuccessorAttemptID, SuccessorAttemptGeneration: 2,
		SuccessorIdentityDigest: ArbiterSuccessorAttemptDigest,
		AcceptedDecisionDigest:  "237472879b22a8db65c5a3a0715510dc17aee1de93c45eaab45dde538cefb939",
		AdmissionID:             "dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34", SessionID: ArbiterSessionB,
		TaskID: ArbiterTaskB, ProjectID: ProjectID, Repository: RepositoryFullName,
		SourceBranch: "ao/dcp-review-lab-12/root", PRURL: "https://github.com/orenvlad-ai/dcp-review-lab/pull/9", PRNumber: 9,
		OldHead: "d4fcb68051ae113ed497d02151a759800ee85633", CurrentMain: "b34b31b5443890e69128db2862726950a6bbac0d",
		PredecessorStatus: "failed", PredecessorError: "repair_launch_failed", OldRuntimeHandleID: "dcp-review-lab-12",
		ContractCommit: "2a174899ae72bf1db548c3b2f172d963488191f1", Model: ArbiterModel, Reasoning: ArbiterReasoning,
		TokenBudget: 16384, RuntimeActionID: FreshWorkerRecoveryID, RuntimeHandleID: "dcp-card12-fresh-worker-recovery",
	}
}

func TestExactFreshWorkerRecoveryPinsEveryPredecessorIdentity(t *testing.T) {
	recovery := exactTestFreshWorkerRecovery()
	if !exactFreshWorkerRecovery(recovery) {
		t.Fatal("exact recovery identity was rejected")
	}
	for name, mutate := range map[string]func(*domain.DCPCard12FreshWorkerRecovery){
		"foreign PR":       func(r *domain.DCPCard12FreshWorkerRecovery) { r.PRNumber = 10 },
		"new native id":    func(r *domain.DCPCard12FreshWorkerRecovery) { r.OldAgentSessionID = "foreign" },
		"changed decision": func(r *domain.DCPCard12FreshWorkerRecovery) { r.AcceptedDecisionDigest = strings.Repeat("0", 64) },
		"larger budget":    func(r *domain.DCPCard12FreshWorkerRecovery) { r.TokenBudget = 32768 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := recovery
			mutate(&bad)
			if exactFreshWorkerRecovery(bad) {
				t.Fatal("foreign recovery identity was accepted")
			}
		})
	}
}

func TestFreshWorkerCommandLogRequiresOneExactGuardedPush(t *testing.T) {
	recovery := exactTestFreshWorkerRecovery()
	exact := `{"type":"item.completed","item":{"type":"command_execution","command":"git push --force-with-lease=refs/heads/ao/dcp-review-lab-12/root:d4fcb68051ae113ed497d02151a759800ee85633 origin HEAD:refs/heads/ao/dcp-review-lab-12/root"}}`
	if err := validateFreshWorkerCommandLog([]byte(exact), recovery); err != nil {
		t.Fatalf("exact guarded push rejected: %v", err)
	}
	for name, data := range map[string]string{
		"unleased":    `{"command":"git push origin HEAD:refs/heads/ao/dcp-review-lab-12/root"}`,
		"foreign ref": `{"command":"git push --force-with-lease=refs/heads/foreign:d4fcb68051ae113ed497d02151a759800ee85633 origin HEAD:refs/heads/foreign"}`,
		"duplicate":   exact + "\n" + `{"command":"git push --force-with-lease=refs/heads/ao/dcp-review-lab-12/root:d4fcb68051ae113ed497d02151a759800ee85633 origin HEAD:refs/heads/ao/dcp-review-lab-12/root --porcelain"}`,
		"new PR":      exact + "\n" + `{"command":"gh pr create --title foreign"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateFreshWorkerCommandLog([]byte(data), recovery); err == nil {
				t.Fatal("unsafe command log was accepted")
			}
		})
	}
}
