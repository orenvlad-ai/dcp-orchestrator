package dcpterminalmerge

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func futureProtocolIncident(t *testing.T) domain.DCPFutureArbiterIncident {
	t.Helper()
	cohort, err := json.Marshal([]futureArbiterCohortMember{
		{TaskID: "night-arb-a", TaskState: string(domain.DCPPolicyMerged), AdmissionSequence: 21},
		{TaskID: "night-arb-b", TaskState: string(domain.DCPPolicyIncident), AdmissionSequence: 22},
	})
	if err != nil {
		t.Fatal(err)
	}
	paths, err := json.Marshal([]string{"shared.txt"})
	if err != nil {
		t.Fatal(err)
	}
	return domain.DCPFutureArbiterIncident{
		IncidentID:         "dcp-future-arbiter-" + strings.Repeat("a", 64),
		Generation:         1,
		IdentityDigest:     strings.Repeat("a", 64),
		InputDigest:        strings.Repeat("b", 64),
		SourcePacketDigest: strings.Repeat("c", 64),
		CohortDigest:       strings.Repeat("d", 64),
		EvidenceDigest:     strings.Repeat("e", 64),
		TaskID:             "night-arb-b",
		CohortJSON:         string(cohort),
		AffectedPathsJSON:  string(paths),
	}
}

func TestFutureArbiterDecisionSchemaIsStrictAndNonCompositional(t *testing.T) {
	schema, err := FutureArbiterDecisionJSONSchema(futureProtocolIncident(t))
	if err != nil {
		t.Fatal(err)
	}
	text := string(schema)
	for _, forbidden := range []string{`"oneOf"`, `"anyOf"`, `"allOf"`, `"not"`, `"const"`, `"uniqueItems"`, `"$schema"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("schema contains provider-unsupported composition %s", forbidden)
		}
	}
	for _, required := range []string{`"additionalProperties":false`, `"enum":["dcp.review-lab.future-arbiter-decision/v1"]`, `"successor_repair"`, `"human_gate"`} {
		if !strings.Contains(text, required) {
			t.Fatalf("schema omitted %s", required)
		}
	}
}

func TestFutureArbiterResponseSchemaCompatibilityFenceRejectsNestedUnsupportedKeyword(t *testing.T) {
	bad := []byte(`{"type":"object","properties":{"affectedPaths":{"type":"array","uniqueItems":true}}}`)
	if err := validateFutureArbiterResponseSchema(bad); err == nil || !strings.Contains(err.Error(), `"uniqueItems"`) {
		t.Fatalf("unsupported nested keyword crossed compatibility fence: %v", err)
	}
}

func TestParseFutureArbiterDecisionAcceptsExactRepairAndRejectsReplayDrift(t *testing.T) {
	incident := futureProtocolIncident(t)
	decision := FutureArbiterDecision{
		SchemaVersion: futureArbiterDecisionSchema,
		IncidentID:    incident.IncidentID, Generation: incident.Generation,
		IdentityDigest: incident.IdentityDigest, InputDigest: incident.InputDigest,
		Verdict: string(domain.DCPFutureVerdictRepair), Order: []string{"night-arb-a", "night-arb-b"},
		RepairTaskID: incident.TaskID, RepairObjective: "Rebase the second compatible line onto current main.",
		AffectedPaths: []string{"shared.txt"}, Summary: "The intents are compatible after ordered repair.",
		EvidenceDigests: []string{incident.SourcePacketDigest, incident.CohortDigest, incident.EvidenceDigest},
	}
	data, err := json.Marshal(decision)
	if err != nil {
		t.Fatal(err)
	}
	parsed, canonical, err := ParseFutureArbiterDecision(data, incident)
	if err != nil || parsed.Verdict != string(domain.DCPFutureVerdictRepair) || !json.Valid(canonical) {
		t.Fatalf("exact repair rejected: parsed=%+v err=%v", parsed, err)
	}
	if _, _, err := ParseFutureArbiterDecision(append(data, []byte(` {}`)...), incident); err == nil {
		t.Fatal("trailing object was accepted")
	}
	decision.InputDigest = strings.Repeat("f", 64)
	drifted, _ := json.Marshal(decision)
	if _, _, err := ParseFutureArbiterDecision(drifted, incident); err == nil {
		t.Fatal("foreign input digest was accepted")
	}
}

func TestParseFutureArbiterDecisionRequiresHumanQuestionAndNoMutation(t *testing.T) {
	incident := futureProtocolIncident(t)
	decision := FutureArbiterDecision{
		SchemaVersion: futureArbiterDecisionSchema,
		IncidentID:    incident.IncidentID, Generation: incident.Generation,
		IdentityDigest: incident.IdentityDigest, InputDigest: incident.InputDigest,
		Verdict: string(domain.DCPFutureVerdictHumanGate), Order: []string{"night-arb-a", "night-arb-b"},
		HumanQuestion: "Should shared.txt keep intent A or intent B?", Summary: "The two requested final values are mutually exclusive.",
		EvidenceDigests: []string{incident.SourcePacketDigest, incident.CohortDigest, incident.EvidenceDigest},
	}
	data, _ := json.Marshal(decision)
	if _, _, err := ParseFutureArbiterDecision(data, incident); err != nil {
		t.Fatalf("exact HumanGate rejected: %v", err)
	}
	decision.AffectedPaths = []string{"shared.txt"}
	data, _ = json.Marshal(decision)
	if _, _, err := ParseFutureArbiterDecision(data, incident); err == nil {
		t.Fatal("HumanGate with mutation authority was accepted")
	}
}

func TestParseFutureArbiterDecisionAllowsOnlyARealSiblingOrderHold(t *testing.T) {
	incident := futureProtocolIncident(t)
	cohort, _ := json.Marshal([]futureArbiterCohortMember{
		{TaskID: "night-arb-a", TaskState: string(domain.DCPPolicyIncident), AdmissionSequence: 21},
		{TaskID: "night-arb-b", TaskState: string(domain.DCPPolicyAdmissionWait), AdmissionSequence: 22},
	})
	incident.TaskID = "night-arb-a"
	incident.CohortJSON = string(cohort)
	decision := FutureArbiterDecision{
		SchemaVersion: futureArbiterDecisionSchema,
		IncidentID:    incident.IncidentID, Generation: incident.Generation,
		IdentityDigest: incident.IdentityDigest, InputDigest: incident.InputDigest,
		Verdict: string(domain.DCPFutureVerdictOrderHold), Order: []string{"night-arb-b", "night-arb-a"},
		Summary:         "The exact sibling must advance first; this incident can wait without mutation.",
		EvidenceDigests: []string{incident.SourcePacketDigest, incident.CohortDigest, incident.EvidenceDigest},
	}
	data, _ := json.Marshal(decision)
	if _, _, err := ParseFutureArbiterDecision(data, incident); err != nil {
		t.Fatalf("real sibling hold rejected: %v", err)
	}
	decision.Order = []string{"night-arb-a", "night-arb-b"}
	data, _ = json.Marshal(decision)
	if _, _, err := ParseFutureArbiterDecision(data, incident); err == nil {
		t.Fatal("self-deadlocking hold was accepted")
	}
}
