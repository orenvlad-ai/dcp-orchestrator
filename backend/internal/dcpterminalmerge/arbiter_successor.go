package dcpterminalmerge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

const (
	ArbiterSuccessorAttemptSchema   = "dcp.review-lab.global-release-arbiter-successor-attempt/v1"
	ArbiterSuccessorInputSchema     = "dcp.review-lab.global-release-arbiter-successor-input/v1"
	ArbiterSuccessorDecisionSchema  = "dcp.review-lab.global-release-arbiter-successor-decision/v1"
	ArbiterSuccessorContractVersion = "dcp-i13-stage2-arbiter-successor-v1"
	ArbiterSuccessorAttemptDigest   = "3c62ea80b56ef94165519d4f01e4c449c320bff22d16b902dd68d4a1a355ea7d"
	ArbiterSuccessorAttemptID       = "dcp-arbiter-successor-" + ArbiterSuccessorAttemptDigest
	ArbiterSuccessorRuntimeHandle   = "dcp-global-release-arbiter-v1-successor"
	ArbiterSuccessorContractCommit  = "4dfff558ac425080d62bd6fe2fb13b573ef50661"

	exactSuccessorIncidentID           = "dcp-global-release-2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60"
	exactSuccessorIncidentDigest       = "2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60"
	exactSuccessorIncidentInputDigest  = "f618fa8a46715acce0958b592384f0d42c071562e36988163e2b96f2c157fc49"
	exactSuccessorSourcePacketDigest   = "fab52d627d14a21ea7ab2a7fdadb4d6f53478d5cdc496858ca74c37e1dfda057"
	exactSuccessorAdmissionID          = "dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34"
	exactSuccessorIncidentLeaseID      = "dcp-incident-dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34"
	exactSuccessorPRURL                = "https://github.com/orenvlad-ai/dcp-review-lab/pull/9"
	exactSuccessorTargetSHA            = "d4fcb68051ae113ed497d02151a759800ee85633"
	exactSuccessorCurrentBaseSHA       = "b34b31b5443890e69128db2862726950a6bbac0d"
	exactSuccessorOriginalInputDigest  = "355a00609c8ded920bd87b215cea74d3c50213fa4ed8f0b484ea577f73bdbd7d"
	exactSuccessorOriginalSchemaDigest = "8314793a7dbc3f0fc654c28e5936687138883b6e134460fc7204a025102b805f"
	exactSuccessorOriginalResultDigest = "d121d012a0b3042f02886fdc0c2aca806f34be64f9e5a3d15e1edf444ff3ae2d"
	exactSuccessorOriginalCodexSession = "019ff23c-7cbf-7ee1-9567-30c6693f95fe"
	exactSuccessorOriginalTokens       = int64(11583)
)

type arbiterSuccessorInput struct {
	SchemaVersion          string          `json:"schemaVersion"`
	IncidentID             string          `json:"incidentId"`
	IncidentGeneration     int64           `json:"incidentGeneration"`
	IncidentIdentityDigest string          `json:"incidentIdentityDigest"`
	IncidentInputDigest    string          `json:"incidentInputDigest"`
	AttemptID              string          `json:"attemptId"`
	AttemptGeneration      int64           `json:"attemptGeneration"`
	AttemptIdentityDigest  string          `json:"attemptIdentityDigest"`
	FrozenIncident         json.RawMessage `json:"frozenIncident"`
	AllowedVerdicts        []string        `json:"allowedVerdicts"`
	AllowedRecoveryOwner   string          `json:"allowedRecoveryOwner"`
	AllowedRecoveryPath    string          `json:"allowedRecoveryPath"`
	AllowedSafeStops       []string        `json:"allowedSafeStops"`
}

// ArbiterSuccessorDecision is the only model-owned successor artifact. Worker
// and review maxima are intentionally absent; those are persisted daemon
// policy on DCPReleaseArbiterSuccessorAttempt.
type ArbiterSuccessorDecision struct {
	SchemaVersion          string   `json:"schemaVersion"`
	IncidentID             string   `json:"incidentId"`
	IncidentGeneration     int64    `json:"incidentGeneration"`
	IncidentIdentityDigest string   `json:"incidentIdentityDigest"`
	IncidentInputDigest    string   `json:"incidentInputDigest"`
	AttemptID              string   `json:"attemptId"`
	AttemptGeneration      int64    `json:"attemptGeneration"`
	AttemptIdentityDigest  string   `json:"attemptIdentityDigest"`
	AttemptInputDigest     string   `json:"attemptInputDigest"`
	AdmissionID            string   `json:"admissionId"`
	TaskID                 string   `json:"taskId"`
	SessionID              string   `json:"sessionId"`
	Repository             string   `json:"repository"`
	PRURL                  string   `json:"prUrl"`
	PRNumber               int64    `json:"prNumber"`
	TargetSHA              string   `json:"targetSha"`
	CurrentBaseSHA         string   `json:"currentBaseSha"`
	Verdict                string   `json:"verdict"`
	RecoveryOwnerSessionID string   `json:"recoveryOwnerSessionId"`
	RecoveryPath           string   `json:"recoveryPath"`
	SafeStopCode           string   `json:"safeStopCode"`
	Summary                string   `json:"summary"`
	EvidenceDigests        []string `json:"evidenceDigests"`
}

func exactSuccessorOriginal(incident domain.DCPReleaseArbiterIncident) bool {
	return incident.IncidentID == exactSuccessorIncidentID && incident.Generation == 1 &&
		incident.IdentityDigest == exactSuccessorIncidentDigest && incident.InputDigest == exactSuccessorIncidentInputDigest &&
		incident.SourcePacketDigest == exactSuccessorSourcePacketDigest && incident.AdmissionID == exactSuccessorAdmissionID &&
		incident.IncidentLeaseID == exactSuccessorIncidentLeaseID && incident.TaskID == ArbiterTaskB && incident.SessionID == ArbiterSessionB &&
		incident.PRURL == exactSuccessorPRURL && incident.PRNumber == 9 && incident.TargetSHA == exactSuccessorTargetSHA &&
		incident.CurrentBaseSHA == exactSuccessorCurrentBaseSHA && incident.Model == ArbiterModel && incident.Reasoning == ArbiterReasoning &&
		incident.TokenBudget == ArbiterTokenBudget && incident.Status == domain.DCPArbiterFailed && incident.ModelCallCount == 1 &&
		incident.DecisionJSON == "" && incident.DecisionDigest == "" && incident.RecoveryOwnerSessionID == "" &&
		incident.RecoveryPath == "" && incident.RecoveryWakeCount == 0 && incident.RecoveryReviewRunID == "" &&
		incident.RecoveryTargetSHA == "" && incident.ErrorCode == "submit_failed"
}

func exactSuccessorAuthorization(attempt domain.DCPReleaseArbiterSuccessorAttempt, incident domain.DCPReleaseArbiterIncident) bool {
	return exactSuccessorOriginal(incident) && attempt.AttemptID == ArbiterSuccessorAttemptID &&
		attempt.IncidentID == incident.IncidentID && attempt.IncidentGeneration == 1 && attempt.AttemptGeneration == 2 &&
		attempt.AttemptIdentityDigest == ArbiterSuccessorAttemptDigest && attempt.IncidentIdentityDigest == incident.IdentityDigest &&
		attempt.IncidentInputDigest == incident.InputDigest && attempt.OriginalInputArtifactDigest == exactSuccessorOriginalInputDigest &&
		attempt.OriginalSchemaArtifactDigest == exactSuccessorOriginalSchemaDigest && attempt.OriginalResultArtifactDigest == exactSuccessorOriginalResultDigest &&
		attempt.OriginalCodexSessionID == exactSuccessorOriginalCodexSession && attempt.OriginalTokenCount == exactSuccessorOriginalTokens &&
		attempt.ContractCommit == ArbiterSuccessorContractCommit && attempt.Model == ArbiterModel && attempt.Reasoning == ArbiterReasoning &&
		attempt.TokenBudget == ArbiterTokenBudget && attempt.PolicyMaxWorkerCalls == 1 && attempt.PolicyMaxFreshReviews == 1 &&
		attempt.RuntimeHandleID == ArbiterSuccessorRuntimeHandle && attempt.LaunchID == attempt.AttemptID
}

func deriveArbiterSuccessorAttempt(incident domain.DCPReleaseArbiterIncident, attempt domain.DCPReleaseArbiterSuccessorAttempt) (domain.DCPReleaseArbiterSuccessorAttempt, error) {
	if !exactSuccessorAuthorization(attempt, incident) || attempt.Status != domain.DCPArbiterSuccessorAuthorized ||
		attempt.ModelCallCount != 0 || attempt.InputJSON != "" || attempt.InputDigest != "" || attempt.DecisionJSON != "" ||
		attempt.DecisionDigest != "" || attempt.RecoveryWakeCount != 0 {
		return domain.DCPReleaseArbiterSuccessorAttempt{}, errors.New("dcp arbiter successor: authorization is not exact")
	}
	identityParts := []string{
		ArbiterSuccessorAttemptSchema, incident.IncidentID, strconv.FormatInt(incident.Generation, 10), strconv.FormatInt(attempt.AttemptGeneration, 10),
		incident.IdentityDigest, incident.InputDigest, incident.AdmissionID, string(incident.SessionID), incident.PRURL,
		incident.TargetSHA, incident.CurrentBaseSHA, attempt.OriginalCodexSessionID, attempt.OriginalResultArtifactDigest,
		strconv.FormatInt(attempt.OriginalTokenCount, 10), ArbiterSuccessorContractVersion,
	}
	if digestString(strings.Join(identityParts, "\x00")) != attempt.AttemptIdentityDigest {
		return domain.DCPReleaseArbiterSuccessorAttempt{}, errors.New("dcp arbiter successor: attempt identity digest is invalid")
	}
	if !json.Valid([]byte(incident.InputJSON)) || len(incident.InputJSON) == 0 {
		return domain.DCPReleaseArbiterSuccessorAttempt{}, errors.New("dcp arbiter successor: frozen incident input is invalid")
	}
	input := arbiterSuccessorInput{
		SchemaVersion: ArbiterSuccessorInputSchema, IncidentID: incident.IncidentID, IncidentGeneration: incident.Generation,
		IncidentIdentityDigest: incident.IdentityDigest, IncidentInputDigest: incident.InputDigest,
		AttemptID: attempt.AttemptID, AttemptGeneration: attempt.AttemptGeneration, AttemptIdentityDigest: attempt.AttemptIdentityDigest,
		FrozenIncident: json.RawMessage(incident.InputJSON), AllowedVerdicts: []string{"assign_recovery", "safe_stop"},
		AllowedRecoveryOwner: string(incident.SessionID), AllowedRecoveryPath: "same_worker_conflict_repair",
		AllowedSafeStops: []string{"scope_not_proven", "identity_ambiguous", "evidence_incomplete", "no_safe_bounded_path"},
	}
	inputDigest, inputJSON, err := canonicalDigest(input)
	if err != nil {
		return domain.DCPReleaseArbiterSuccessorAttempt{}, err
	}
	if len(inputJSON) > arbiterMaxInputBytes {
		return domain.DCPReleaseArbiterSuccessorAttempt{}, errors.New("dcp arbiter successor: frozen input exceeds 16384 bytes")
	}
	attempt.InputJSON, attempt.InputDigest = string(inputJSON), inputDigest
	attempt.Status = domain.DCPArbiterSuccessorRequested
	return attempt, nil
}

func ArbiterSuccessorDecisionJSONSchema(incident domain.DCPReleaseArbiterIncident, attempt domain.DCPReleaseArbiterSuccessorAttempt) ([]byte, error) {
	if !exactSuccessorAuthorization(attempt, incident) || !validDigest(attempt.InputDigest) || attempt.InputJSON == "" {
		return nil, errors.New("dcp arbiter successor: decision schema identity is invalid")
	}
	constValue := func(value any) map[string]any { return map[string]any{"enum": []any{value}} }
	properties := map[string]any{
		"schemaVersion": constValue(ArbiterSuccessorDecisionSchema), "incidentId": constValue(incident.IncidentID),
		"incidentGeneration": constValue(int64(1)), "incidentIdentityDigest": constValue(incident.IdentityDigest),
		"incidentInputDigest": constValue(incident.InputDigest), "attemptId": constValue(attempt.AttemptID),
		"attemptGeneration": constValue(int64(2)), "attemptIdentityDigest": constValue(attempt.AttemptIdentityDigest),
		"attemptInputDigest": constValue(attempt.InputDigest), "admissionId": constValue(incident.AdmissionID),
		"taskId": constValue(incident.TaskID), "sessionId": constValue(string(incident.SessionID)),
		"repository": constValue(RepositoryFullName), "prUrl": constValue(incident.PRURL), "prNumber": constValue(incident.PRNumber),
		"targetSha": constValue(incident.TargetSHA), "currentBaseSha": constValue(incident.CurrentBaseSHA),
		"verdict":                map[string]any{"enum": []string{"assign_recovery", "safe_stop"}},
		"recoveryOwnerSessionId": map[string]any{"enum": []string{"", string(incident.SessionID)}},
		"recoveryPath":           map[string]any{"enum": []string{"", "same_worker_conflict_repair"}},
		"safeStopCode":           map[string]any{"enum": []string{"", "scope_not_proven", "identity_ambiguous", "evidence_incomplete", "no_safe_bounded_path"}},
		"summary":                map[string]any{"type": "string"},
		"evidenceDigests":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	}
	schema := map[string]any{
		"type": "object", "additionalProperties": false,
		"required":   []string{"schemaVersion", "incidentId", "incidentGeneration", "incidentIdentityDigest", "incidentInputDigest", "attemptId", "attemptGeneration", "attemptIdentityDigest", "attemptInputDigest", "admissionId", "taskId", "sessionId", "repository", "prUrl", "prNumber", "targetSha", "currentBaseSha", "verdict", "recoveryOwnerSessionId", "recoveryPath", "safeStopCode", "summary", "evidenceDigests"},
		"properties": properties,
	}
	return json.Marshal(schema)
}

func ParseArbiterSuccessorDecision(data []byte, incident domain.DCPReleaseArbiterIncident, attempt domain.DCPReleaseArbiterSuccessorAttempt) (ArbiterSuccessorDecision, []byte, error) {
	var decision ArbiterSuccessorDecision
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&decision); err != nil {
		return ArbiterSuccessorDecision{}, nil, fmt.Errorf("dcp arbiter successor: malformed decision: %w", err)
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return ArbiterSuccessorDecision{}, nil, errors.New("dcp arbiter successor: decision has trailing JSON")
	}
	if err := validateArbiterSuccessorDecision(decision, incident, attempt); err != nil {
		return ArbiterSuccessorDecision{}, nil, err
	}
	canonical, err := json.Marshal(decision)
	if err != nil {
		return ArbiterSuccessorDecision{}, nil, err
	}
	return decision, canonical, nil
}

func ReadArbiterSuccessorDecision(path string, incident domain.DCPReleaseArbiterIncident, attempt domain.DCPReleaseArbiterSuccessorAttempt) (ArbiterSuccessorDecision, []byte, error) {
	path = filepath.Clean(path)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > arbiterMaxResultBytes || info.Mode().Perm()&0o022 != 0 {
		return ArbiterSuccessorDecision{}, nil, errors.New("dcp arbiter successor: result is not an exact owner-controlled bounded file")
	}
	file, err := os.Open(path)
	if err != nil {
		return ArbiterSuccessorDecision{}, nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, arbiterMaxResultBytes+1))
	if err != nil || len(data) > arbiterMaxResultBytes {
		return ArbiterSuccessorDecision{}, nil, errors.New("dcp arbiter successor: result exceeds its bound")
	}
	return ParseArbiterSuccessorDecision(data, incident, attempt)
}

func validateArbiterSuccessorDecision(d ArbiterSuccessorDecision, incident domain.DCPReleaseArbiterIncident, attempt domain.DCPReleaseArbiterSuccessorAttempt) error {
	if !exactSuccessorAuthorization(attempt, incident) || d.SchemaVersion != ArbiterSuccessorDecisionSchema ||
		d.IncidentID != incident.IncidentID || d.IncidentGeneration != 1 || d.IncidentIdentityDigest != incident.IdentityDigest ||
		d.IncidentInputDigest != incident.InputDigest || d.AttemptID != attempt.AttemptID || d.AttemptGeneration != 2 ||
		d.AttemptIdentityDigest != attempt.AttemptIdentityDigest || d.AttemptInputDigest != attempt.InputDigest ||
		d.AdmissionID != incident.AdmissionID || d.TaskID != incident.TaskID || d.SessionID != string(incident.SessionID) ||
		d.Repository != RepositoryFullName || d.PRURL != incident.PRURL || d.PRNumber != incident.PRNumber ||
		!strings.EqualFold(d.TargetSHA, incident.TargetSHA) || !strings.EqualFold(d.CurrentBaseSHA, incident.CurrentBaseSHA) ||
		len(d.Summary) == 0 || len(d.Summary) > 512 || strings.TrimSpace(d.Summary) != d.Summary ||
		len(d.EvidenceDigests) < 1 || len(d.EvidenceDigests) > 8 {
		return errors.New("dcp arbiter successor: decision identity or bounds are invalid")
	}
	allowedEvidence := map[string]bool{
		incident.SourcePacketDigest: true, incident.ScopeDigest: true, incident.HistoryDigest: true,
		incident.DiffDigest: true, incident.CheckSetDigest: true, incident.ReviewSetDigest: true,
		incident.FrozenQueueDigest: true, incident.MechanicalDigest: true, incident.InputDigest: true,
		attempt.InputDigest: true, attempt.AttemptIdentityDigest: true,
	}
	seen := map[string]bool{}
	for _, digest := range d.EvidenceDigests {
		if !validDigest(digest) || !allowedEvidence[digest] || seen[digest] {
			return errors.New("dcp arbiter successor: decision cites foreign or duplicate evidence")
		}
		seen[digest] = true
	}
	switch d.Verdict {
	case "assign_recovery":
		if d.RecoveryOwnerSessionID != string(incident.SessionID) || d.RecoveryPath != "same_worker_conflict_repair" || d.SafeStopCode != "" {
			return errors.New("dcp arbiter successor: recovery owner or path is outside the allowlist")
		}
	case "safe_stop":
		if d.RecoveryOwnerSessionID != "" || d.RecoveryPath != "" || !allowedSafeStop(d.SafeStopCode) {
			return errors.New("dcp arbiter successor: safe-stop decision is malformed")
		}
	default:
		return errors.New("dcp arbiter successor: verdict is not allowed")
	}
	return nil
}

func sameArbiterSuccessorImmutable(a, b domain.DCPReleaseArbiterSuccessorAttempt) bool {
	return a.AttemptID == b.AttemptID && a.IncidentID == b.IncidentID && a.IncidentGeneration == b.IncidentGeneration &&
		a.AttemptGeneration == b.AttemptGeneration && a.AttemptIdentityDigest == b.AttemptIdentityDigest &&
		a.IncidentIdentityDigest == b.IncidentIdentityDigest && a.IncidentInputDigest == b.IncidentInputDigest &&
		a.OriginalInputArtifactDigest == b.OriginalInputArtifactDigest && a.OriginalSchemaArtifactDigest == b.OriginalSchemaArtifactDigest &&
		a.OriginalResultArtifactDigest == b.OriginalResultArtifactDigest && a.OriginalCodexSessionID == b.OriginalCodexSessionID &&
		a.OriginalTokenCount == b.OriginalTokenCount && a.ContractCommit == b.ContractCommit && a.InputJSON == b.InputJSON &&
		a.InputDigest == b.InputDigest && a.Model == b.Model && a.Reasoning == b.Reasoning && a.TokenBudget == b.TokenBudget &&
		a.PolicyMaxWorkerCalls == b.PolicyMaxWorkerCalls && a.PolicyMaxFreshReviews == b.PolicyMaxFreshReviews &&
		a.RuntimeHandleID == b.RuntimeHandleID && a.LaunchID == b.LaunchID
}
