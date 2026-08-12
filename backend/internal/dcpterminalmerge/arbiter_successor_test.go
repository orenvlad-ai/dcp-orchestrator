package dcpterminalmerge

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func exactTestSuccessorState() (domain.DCPReleaseArbiterIncident, domain.DCPReleaseArbiterSuccessorAttempt) {
	incident := domain.DCPReleaseArbiterIncident{
		IncidentID: exactSuccessorIncidentID, Generation: 1, IdentityDigest: exactSuccessorIncidentDigest,
		AdmissionID: exactSuccessorAdmissionID, IncidentLeaseID: exactSuccessorIncidentLeaseID,
		SourcePacketDigest: exactSuccessorSourcePacketDigest,
		InputJSON:          `{"schemaVersion":"dcp.review-lab.global-release-arbiter-input/v1","allowedPaths":["same_worker_conflict_repair"]}`,
		InputDigest:        exactSuccessorIncidentInputDigest, TaskID: ArbiterTaskB, SessionID: ArbiterSessionB,
		SourceBranch: "ao/dcp-review-lab-12/root", PRURL: exactSuccessorPRURL, PRNumber: 9,
		TargetSHA: exactSuccessorTargetSHA, CurrentBaseSHA: exactSuccessorCurrentBaseSHA,
		ScopeDigest: strings.Repeat("1", 64), HistoryDigest: strings.Repeat("2", 64), DiffDigest: strings.Repeat("3", 64),
		CheckSetDigest: strings.Repeat("4", 64), ReviewSetDigest: strings.Repeat("5", 64),
		FrozenQueueDigest: strings.Repeat("6", 64), MechanicalDigest: strings.Repeat("7", 64),
		Model: ArbiterModel, Reasoning: ArbiterReasoning, TokenBudget: ArbiterTokenBudget,
		RuntimeHandleID: ArbiterRuntimeHandle, LaunchID: exactSuccessorIncidentID,
		Status: domain.DCPArbiterFailed, ModelCallCount: 1, ErrorCode: "submit_failed",
	}
	attempt := domain.DCPReleaseArbiterSuccessorAttempt{
		AttemptID: ArbiterSuccessorAttemptID, IncidentID: incident.IncidentID, IncidentGeneration: 1,
		AttemptGeneration: 2, AttemptIdentityDigest: ArbiterSuccessorAttemptDigest,
		IncidentIdentityDigest: incident.IdentityDigest, IncidentInputDigest: incident.InputDigest,
		OriginalInputArtifactDigest: exactSuccessorOriginalInputDigest, OriginalSchemaArtifactDigest: exactSuccessorOriginalSchemaDigest,
		OriginalResultArtifactDigest: exactSuccessorOriginalResultDigest, OriginalCodexSessionID: exactSuccessorOriginalCodexSession,
		OriginalTokenCount: exactSuccessorOriginalTokens, ContractCommit: ArbiterSuccessorContractCommit,
		Model: ArbiterModel, Reasoning: ArbiterReasoning, TokenBudget: ArbiterTokenBudget,
		PolicyMaxWorkerCalls: 1, PolicyMaxFreshReviews: 1,
		RuntimeHandleID: ArbiterSuccessorRuntimeHandle, LaunchID: ArbiterSuccessorAttemptID,
		Status: domain.DCPArbiterSuccessorAuthorized, AuthorizedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC(),
	}
	return incident, attempt
}

func validSuccessorAssignDecision(incident domain.DCPReleaseArbiterIncident, attempt domain.DCPReleaseArbiterSuccessorAttempt) ArbiterSuccessorDecision {
	return ArbiterSuccessorDecision{
		SchemaVersion: ArbiterSuccessorDecisionSchema,
		IncidentID:    incident.IncidentID, IncidentGeneration: 1, IncidentIdentityDigest: incident.IdentityDigest,
		IncidentInputDigest: incident.InputDigest, AttemptID: attempt.AttemptID, AttemptGeneration: 2,
		AttemptIdentityDigest: attempt.AttemptIdentityDigest, AttemptInputDigest: attempt.InputDigest,
		AdmissionID: incident.AdmissionID, TaskID: incident.TaskID, SessionID: string(incident.SessionID),
		Repository: RepositoryFullName, PRURL: incident.PRURL, PRNumber: incident.PRNumber,
		TargetSHA: incident.TargetSHA, CurrentBaseSHA: incident.CurrentBaseSHA,
		Verdict: "assign_recovery", RecoveryOwnerSessionID: string(incident.SessionID), RecoveryPath: "same_worker_conflict_repair",
		Summary:         "The exact same-worker conflict repair remains inside the frozen task.",
		EvidenceDigests: []string{attempt.InputDigest, incident.MechanicalDigest},
	}
}

func TestArbiterSuccessorInputExcludesRejectedResultAndPinsAttemptTwo(t *testing.T) {
	incident, attempt := exactTestSuccessorState()
	prepared, err := deriveArbiterSuccessorAttempt(incident, attempt)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Status != domain.DCPArbiterSuccessorRequested || !validDigest(prepared.InputDigest) || prepared.InputJSON == "" {
		t.Fatalf("prepared attempt = %+v", prepared)
	}
	for _, forbidden := range []string{exactSuccessorOriginalResultDigest, exactSuccessorOriginalCodexSession, "maxFreshReviews", "maxWorkerCalls"} {
		if strings.Contains(prepared.InputJSON, forbidden) {
			t.Fatalf("successor input leaked prior/model-policy field %q: %s", forbidden, prepared.InputJSON)
		}
	}
	for _, exact := range []string{ArbiterSuccessorAttemptID, ArbiterSuccessorAttemptDigest, exactSuccessorIncidentID, `"attemptGeneration":2`} {
		if !strings.Contains(prepared.InputJSON, exact) {
			t.Fatalf("successor input lacks %q: %s", exact, prepared.InputJSON)
		}
	}
}

func TestArbiterSuccessorDecisionRemovesModelOwnedPolicyAndBindsEveryGeneration(t *testing.T) {
	incident, authorized := exactTestSuccessorState()
	attempt, err := deriveArbiterSuccessorAttempt(incident, authorized)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := ArbiterSuccessorDecisionJSONSchema(incident, attempt)
	if err != nil || !json.Valid(schema) {
		t.Fatalf("schema err=%v body=%s", err, schema)
	}
	for _, forbidden := range []string{"maxFreshReviews", "maxWorkerCalls", `"oneOf"`, `"anyOf"`, `"not"`} {
		if strings.Contains(string(schema), forbidden) {
			t.Fatalf("successor schema contains forbidden model authority/composition %q: %s", forbidden, schema)
		}
	}
	for _, exact := range []string{incident.IncidentID, incident.IdentityDigest, incident.InputDigest, attempt.AttemptID, attempt.AttemptIdentityDigest, attempt.InputDigest} {
		if !strings.Contains(string(schema), exact) {
			t.Fatalf("successor schema lacks exact binding %q", exact)
		}
	}
	decision := validSuccessorAssignDecision(incident, attempt)
	data, _ := json.Marshal(decision)
	if _, _, err := ParseArbiterSuccessorDecision(data, incident, attempt); err != nil {
		t.Fatalf("valid successor decision rejected: %v", err)
	}
	var withModelPolicy map[string]any
	if err := json.Unmarshal(data, &withModelPolicy); err != nil {
		t.Fatal(err)
	}
	withModelPolicy["maxFreshReviews"] = 0
	data, _ = json.Marshal(withModelPolicy)
	if _, _, err := ParseArbiterSuccessorDecision(data, incident, attempt); err == nil {
		t.Fatal("successor accepted model-owned review policy")
	}
	stale := decision
	stale.AttemptGeneration = 1
	data, _ = json.Marshal(stale)
	if _, _, err := ParseArbiterSuccessorDecision(data, incident, attempt); err == nil {
		t.Fatal("successor accepted a stale attempt generation")
	}
	foreign := decision
	foreign.RecoveryOwnerSessionID = ArbiterSessionA
	data, _ = json.Marshal(foreign)
	if _, _, err := ParseArbiterSuccessorDecision(data, incident, attempt); err == nil {
		t.Fatal("successor accepted a foreign recovery owner")
	}
}

func TestArbiterSuccessorSafeStopHasNoRecoveryAuthority(t *testing.T) {
	incident, authorized := exactTestSuccessorState()
	attempt, err := deriveArbiterSuccessorAttempt(incident, authorized)
	if err != nil {
		t.Fatal(err)
	}
	decision := validSuccessorAssignDecision(incident, attempt)
	decision.Verdict, decision.RecoveryOwnerSessionID, decision.RecoveryPath = "safe_stop", "", ""
	decision.SafeStopCode = "no_safe_bounded_path"
	data, _ := json.Marshal(decision)
	if _, _, err := ParseArbiterSuccessorDecision(data, incident, attempt); err != nil {
		t.Fatalf("valid safe stop rejected: %v", err)
	}
	decision.RecoveryPath = "same_worker_conflict_repair"
	data, _ = json.Marshal(decision)
	if _, _, err := ParseArbiterSuccessorDecision(data, incident, attempt); err == nil {
		t.Fatal("safe stop retained recovery authority")
	}
}
