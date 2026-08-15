package dcpterminalmerge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

const (
	futureArbiterInputSchema    = "dcp.review-lab.future-arbiter-input/v1"
	futureArbiterDecisionSchema = "dcp.review-lab.future-arbiter-decision/v1"
	futureArbiterMaxInputBytes  = 16384
	futureArbiterMaxResultBytes = 16384
)

type futureArbiterCohortMember struct {
	TaskID            string   `json:"taskId"`
	SessionID         string   `json:"sessionId"`
	Intent            string   `json:"intent"`
	TaskDigest        string   `json:"taskDigest"`
	TaskState         string   `json:"taskState"`
	AdmissionID       string   `json:"admissionId"`
	AdmissionSequence int64    `json:"admissionSequence"`
	AdmissionStatus   string   `json:"admissionStatus"`
	PRURL             string   `json:"prUrl"`
	CandidateHead     string   `json:"candidateHead"`
	ReviewedBase      string   `json:"reviewedBase"`
	ReviewRunID       string   `json:"reviewRunId"`
	AffectedPaths     []string `json:"affectedPaths"`
	Diff              string   `json:"diff"`
	DiffDigest        string   `json:"diffDigest"`
}

type futureArbiterEvidence struct {
	Repository       string   `json:"repository"`
	TargetBranch     string   `json:"targetBranch"`
	IncidentKind     string   `json:"incidentKind"`
	CurrentMain      string   `json:"currentMain"`
	ProviderHead     string   `json:"providerHead"`
	ProviderBase     string   `json:"providerBase"`
	Mergeable        string   `json:"mergeable"`
	MergeState       string   `json:"mergeState"`
	NormalizedState  string   `json:"normalizedState"`
	NamedCheck       string   `json:"namedCheck"`
	ReviewRunID      string   `json:"reviewRunId"`
	ReviewVerdict    string   `json:"reviewVerdict"`
	ReviewBodyDigest string   `json:"reviewBodyDigest"`
	MergeTreeClean   bool     `json:"mergeTreeClean"`
	ConflictPaths    []string `json:"conflictPaths"`
	MergeTreeDigest  string   `json:"mergeTreeDigest"`
	FIFOOwner        string   `json:"fifoOwner"`
}

type futureArbiterInput struct {
	SchemaVersion      string                      `json:"schemaVersion"`
	IncidentID         string                      `json:"incidentId"`
	Generation         int64                       `json:"generation"`
	IdentityDigest     string                      `json:"identityDigest"`
	SourcePacketDigest string                      `json:"sourcePacketDigest"`
	AnchorTaskID       string                      `json:"anchorTaskId"`
	AnchorSessionID    string                      `json:"anchorSessionId"`
	AffectedPaths      []string                    `json:"affectedPaths"`
	Cohort             []futureArbiterCohortMember `json:"cohort"`
	CohortDigest       string                      `json:"cohortDigest"`
	Evidence           futureArbiterEvidence       `json:"evidence"`
	EvidenceDigest     string                      `json:"evidenceDigest"`
	AllowedVerdicts    []string                    `json:"allowedVerdicts"`
}

// FutureArbiterDecision is the sole context-free model artifact. Conditional
// verdict semantics are validated after strict non-compositional schema parse.
type FutureArbiterDecision struct {
	SchemaVersion   string   `json:"schemaVersion"`
	IncidentID      string   `json:"incidentId"`
	Generation      int64    `json:"generation"`
	IdentityDigest  string   `json:"identityDigest"`
	InputDigest     string   `json:"inputDigest"`
	Verdict         string   `json:"verdict"`
	Order           []string `json:"order"`
	RepairTaskID    string   `json:"repairTaskId"`
	RepairObjective string   `json:"repairObjective"`
	AffectedPaths   []string `json:"affectedPaths"`
	HumanQuestion   string   `json:"humanQuestion"`
	Summary         string   `json:"summary"`
	EvidenceDigests []string `json:"evidenceDigests"`
}

// FutureArbiterDecisionJSONSchema returns the strict non-compositional exact-incident schema.
func FutureArbiterDecisionJSONSchema(incident domain.DCPFutureArbiterIncident) ([]byte, error) {
	var cohort []futureArbiterCohortMember
	var paths []string
	if err := json.Unmarshal([]byte(incident.CohortJSON), &cohort); err != nil || len(cohort) == 0 ||
		json.Unmarshal([]byte(incident.AffectedPathsJSON), &paths) != nil || len(paths) == 0 {
		return nil, errors.New("future arbiter schema identity is incomplete")
	}
	tasks := make([]string, 0, len(cohort))
	for _, member := range cohort {
		tasks = append(tasks, member.TaskID)
	}
	schema := map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"schemaVersion", "incidentId", "generation", "identityDigest", "inputDigest", "verdict", "order", "repairTaskId", "repairObjective", "affectedPaths", "humanQuestion", "summary", "evidenceDigests"},
		"properties": map[string]any{
			"schemaVersion":   map[string]any{"type": "string", "enum": []string{futureArbiterDecisionSchema}},
			"incidentId":      map[string]any{"type": "string", "enum": []string{incident.IncidentID}},
			"generation":      map[string]any{"type": "integer", "enum": []int64{incident.Generation}},
			"identityDigest":  map[string]any{"type": "string", "enum": []string{incident.IdentityDigest}},
			"inputDigest":     map[string]any{"type": "string", "enum": []string{incident.InputDigest}},
			"verdict":         map[string]any{"type": "string", "enum": []string{string(domain.DCPFutureVerdictOrderHold), string(domain.DCPFutureVerdictRepair), string(domain.DCPFutureVerdictHumanGate)}},
			"order":           map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": tasks}, "minItems": len(tasks), "maxItems": len(tasks)},
			"repairTaskId":    map[string]any{"type": "string", "enum": []string{"", incident.TaskID}},
			"repairObjective": map[string]any{"type": "string", "maxLength": 1024},
			"affectedPaths":   map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": paths}, "maxItems": len(paths)},
			"humanQuestion":   map[string]any{"type": "string", "maxLength": 512},
			"summary":         map[string]any{"type": "string", "minLength": 1, "maxLength": 1024},
			"evidenceDigests": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{incident.SourcePacketDigest, incident.CohortDigest, incident.EvidenceDigest}}, "minItems": 3, "maxItems": 3},
		},
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	if err := validateFutureArbiterResponseSchema(encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}

// validateFutureArbiterResponseSchema is a model-free provider-compatibility
// fence. Exact identity and set semantics remain enforced by the trusted
// parser; unsupported response-schema keywords must never cross the one-call
// fence merely because the generated JSON is syntactically valid.
func validateFutureArbiterResponseSchema(encoded []byte) error {
	var document any
	if json.Unmarshal(encoded, &document) != nil {
		return errors.New("future arbiter response schema is malformed")
	}
	forbidden := map[string]bool{
		"$schema": true, "oneOf": true, "anyOf": true, "allOf": true,
		"not": true, "const": true, "uniqueItems": true,
	}
	var walk func(any) error
	walk = func(value any) error {
		switch typed := value.(type) {
		case map[string]any:
			for key, child := range typed {
				if forbidden[key] {
					return fmt.Errorf("future arbiter response schema uses unsupported keyword %q", key)
				}
				if err := walk(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range typed {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(document)
}

// ParseFutureArbiterDecision validates one structured exact-incident verdict.
func ParseFutureArbiterDecision(data []byte, incident domain.DCPFutureArbiterIncident) (FutureArbiterDecision, []byte, error) {
	if len(data) == 0 || len(data) > futureArbiterMaxResultBytes || !json.Valid(data) {
		return FutureArbiterDecision{}, nil, errors.New("future arbiter decision is malformed or unbounded")
	}
	var decision FutureArbiterDecision
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&decision); err != nil {
		return FutureArbiterDecision{}, nil, errors.Join(err, errors.New("future arbiter decision is not one strict object"))
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return FutureArbiterDecision{}, nil, errors.New("future arbiter decision contains trailing data")
	}
	if decision.SchemaVersion != futureArbiterDecisionSchema || decision.IncidentID != incident.IncidentID ||
		decision.Generation != incident.Generation || decision.IdentityDigest != incident.IdentityDigest || decision.InputDigest != incident.InputDigest {
		return FutureArbiterDecision{}, nil, errors.New("future arbiter decision identity drifted")
	}
	var cohort []futureArbiterCohortMember
	var allowedPaths []string
	if json.Unmarshal([]byte(incident.CohortJSON), &cohort) != nil || json.Unmarshal([]byte(incident.AffectedPathsJSON), &allowedPaths) != nil {
		return FutureArbiterDecision{}, nil, errors.New("future arbiter frozen evidence is unavailable")
	}
	wantTasks := make([]string, 0, len(cohort))
	states := make(map[string]string, len(cohort))
	for _, member := range cohort {
		wantTasks = append(wantTasks, member.TaskID)
		states[member.TaskID] = member.TaskState
	}
	if len(decision.Order) != len(wantTasks) || !sameStringSet(decision.Order, wantTasks) ||
		!sameStringSet(decision.EvidenceDigests, []string{incident.SourcePacketDigest, incident.CohortDigest, incident.EvidenceDigest}) ||
		!boundedPlainText(decision.Summary, 1024) {
		return FutureArbiterDecision{}, nil, errors.New("future arbiter decision omitted exact cohort or evidence")
	}
	verdict := domain.DCPFutureArbiterVerdict(decision.Verdict)
	nextTask := ""
	for _, taskID := range decision.Order {
		if states[taskID] != string(domain.DCPPolicyMerged) && states[taskID] != string(domain.DCPPolicyFailed) {
			nextTask = taskID
			break
		}
	}
	switch verdict {
	case domain.DCPFutureVerdictRepair:
		if decision.RepairTaskID != incident.TaskID || !boundedPlainText(decision.RepairObjective, 1024) ||
			len(decision.AffectedPaths) == 0 || !stringSubset(decision.AffectedPaths, allowedPaths) || decision.HumanQuestion != "" || nextTask != incident.TaskID {
			return FutureArbiterDecision{}, nil, errors.New("future arbiter repair authority is invalid")
		}
	case domain.DCPFutureVerdictHumanGate:
		if decision.RepairTaskID != "" || decision.RepairObjective != "" ||
			!stringSubset(decision.AffectedPaths, allowedPaths) || !boundedPlainText(decision.HumanQuestion, 512) {
			return FutureArbiterDecision{}, nil, errors.New("future arbiter HumanGate is invalid")
		}
	case domain.DCPFutureVerdictOrderHold:
		if decision.RepairTaskID != "" || decision.RepairObjective != "" || len(decision.AffectedPaths) != 0 || decision.HumanQuestion != "" ||
			nextTask == "" || nextTask == incident.TaskID {
			return FutureArbiterDecision{}, nil, errors.New("future arbiter hold authority is invalid")
		}
	default:
		return FutureArbiterDecision{}, nil, fmt.Errorf("unknown future arbiter verdict %q", decision.Verdict)
	}
	canonical, err := json.Marshal(decision)
	return decision, canonical, err
}

func boundedPlainText(value string, limit int) bool {
	if value == "" || len(value) > limit || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return false
		}
	}
	return true
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aa, bb := append([]string(nil), a...), append([]string(nil), b...)
	slices.Sort(aa)
	slices.Sort(bb)
	return slices.Equal(aa, bb)
}

func stringSubset(values, allowed []string) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value] {
			return false
		}
		seen[value] = true
		if !slices.Contains(allowed, value) {
			return false
		}
	}
	return true
}
