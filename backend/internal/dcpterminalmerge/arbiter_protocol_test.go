package dcpterminalmerge

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func exactTestArbiterIncident(t *testing.T) domain.DCPReleaseArbiterIncident {
	t.Helper()
	digest := func(char string) string { return strings.Repeat(char, 64) }
	return domain.DCPReleaseArbiterIncident{
		IncidentID: "dcp-global-release-" + digest("a"), Generation: 1, IdentityDigest: digest("a"),
		AdmissionID: "admission-11", IncidentLeaseID: "lease-11", SourcePacketDigest: digest("b"),
		InputJSON:   `{"schemaVersion":"dcp.review-lab.global-release-arbiter-input/v1"}`,
		InputDigest: digest("c"), TaskID: ArbiterTaskA, SessionID: ArbiterSessionA,
		WorktreePath: filepath.Join(t.TempDir(), "worktree"), SourceBranch: "ao/dcp-review-lab-11/root",
		PRURL: "https://github.com/orenvlad-ai/dcp-review-lab/pull/11", PRNumber: 11,
		TargetSHA: strings.Repeat("d", 40), ReviewedBaseSHA: strings.Repeat("e", 40), CurrentBaseSHA: strings.Repeat("f", 40),
		ReviewID: "review-11", ReviewRunID: "run-11", BatchID: "batch-11",
		ScopeDigest: digest("1"), HistoryDigest: digest("2"), DiffDigest: digest("3"), CheckSetDigest: digest("4"),
		ReviewSetDigest: digest("5"), FrozenQueueDigest: digest("6"), MechanicalDigest: digest("7"),
		Model: ArbiterModel, Reasoning: ArbiterReasoning, TokenBudget: ArbiterTokenBudget,
		RuntimeHandleID: ArbiterRuntimeHandle, LaunchID: "dcp-global-release-" + digest("a"), Status: domain.DCPArbiterRunning,
	}
}

func validAssignDecision(incident domain.DCPReleaseArbiterIncident) ArbiterDecision {
	return ArbiterDecision{
		SchemaVersion: ArbiterDecisionSchema, IncidentID: incident.IncidentID, Generation: 1,
		IdentityDigest: incident.IdentityDigest, InputDigest: incident.InputDigest, AdmissionID: incident.AdmissionID,
		TaskID: incident.TaskID, SessionID: string(incident.SessionID), Repository: RepositoryFullName,
		PRURL: incident.PRURL, PRNumber: incident.PRNumber, TargetSHA: incident.TargetSHA, CurrentBaseSHA: incident.CurrentBaseSHA,
		Verdict: "assign_recovery", RecoveryOwner: &ArbiterRecoveryOwner{Kind: "same_worker", SessionID: string(incident.SessionID)},
		RecoveryPath: &ArbiterRecoveryPath{Kind: "same_worker_conflict_repair", MaxWorkerCalls: 1, MaxFreshReviews: 1},
		Summary:      "The one exact same-worker conflict repair remains bounded.", EvidenceDigests: []string{incident.ScopeDigest, incident.MechanicalDigest},
	}
}

func TestArbiterDecisionAcceptsOnlyExactOwnerPathAndReferencedEvidence(t *testing.T) {
	incident := exactTestArbiterIncident(t)
	decision := validAssignDecision(incident)
	data, err := json.Marshal(decision)
	if err != nil {
		t.Fatal(err)
	}
	got, canonical, err := ParseArbiterDecision(data, incident)
	if err != nil || got.Verdict != "assign_recovery" || !json.Valid(canonical) {
		t.Fatalf("valid decision = %+v canonical=%s err=%v", got, canonical, err)
	}
	for name, mutate := range map[string]func(*ArbiterDecision){
		"foreign owner":      func(d *ArbiterDecision) { d.RecoveryOwner.SessionID = ArbiterSessionB },
		"second worker call": func(d *ArbiterDecision) { d.RecoveryPath.MaxWorkerCalls = 2 },
		"foreign evidence":   func(d *ArbiterDecision) { d.EvidenceDigests = []string{strings.Repeat("9", 64)} },
		"stale head":         func(d *ArbiterDecision) { d.TargetSHA = strings.Repeat("0", 40) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := validAssignDecision(incident)
			mutate(&bad)
			data, _ := json.Marshal(bad)
			if _, _, err := ParseArbiterDecision(data, incident); err == nil {
				t.Fatal("malformed or foreign decision was accepted")
			}
		})
	}
	nullOwner := strings.Replace(string(data), `"recoveryOwner":{"kind":"same_worker","sessionId":"dcp-review-lab-11"}`, `"recoveryOwner":null`, 1)
	if _, _, err := ParseArbiterDecision([]byte(nullOwner), incident); err == nil {
		t.Fatal("explicit null recovery owner was accepted")
	}
}

func TestArbiterDecisionSchemaPinsEveryMutationIdentity(t *testing.T) {
	incident := exactTestArbiterIncident(t)
	schema, err := ArbiterDecisionJSONSchema(incident)
	if err != nil || !json.Valid(schema) {
		t.Fatalf("schema err=%v body=%s", err, schema)
	}
	for _, exact := range []string{incident.IncidentID, incident.IdentityDigest, incident.InputDigest, incident.AdmissionID, incident.TaskID, string(incident.SessionID), incident.PRURL, incident.TargetSHA, incident.CurrentBaseSHA, "same_worker_conflict_repair"} {
		if !strings.Contains(string(schema), exact) {
			t.Fatalf("schema lacks exact constant %q", exact)
		}
	}
}

func TestArbiterRecoveryCandidateAllowsOnlyOneDirectTwoLineCanaryCommit(t *testing.T) {
	incident := exactTestArbiterIncident(t)
	newHead := strings.Repeat("9", 40)
	candidate := mergeCandidate{project: domain.ProjectRecord{Path: "/exact/project"}, run: domain.ReviewRun{TargetSHA: newHead}}
	engine := &Engine{}
	engine.git = func(_ context.Context, repo string, args ...string) (string, error) {
		if repo != candidate.project.Path {
			return "", errors.New("foreign repo")
		}
		switch strings.Join(args, " ") {
		case "merge-base --is-ancestor " + incident.CurrentBaseSHA + " " + newHead:
			return "", nil
		case "show -s --format=%P " + newHead:
			return incident.CurrentBaseSHA, nil
		case "diff --name-status " + incident.CurrentBaseSHA + ".." + newHead:
			return "M\t" + arbiterConflictPath, nil
		case "show " + incident.TargetSHA + ":" + arbiterConflictPath:
			return "arbiter-a", nil
		case "show " + incident.CurrentBaseSHA + ":" + arbiterConflictPath:
			return "arbiter-b", nil
		case "show " + newHead + ":" + arbiterConflictPath:
			return "arbiter-b\narbiter-a", nil
		}
		return "", errors.New("unexpected git command")
	}
	if err := engine.validateArbiterRecoveryCandidate(context.Background(), candidate, incident); err != nil {
		t.Fatal(err)
	}
	originalGit := engine.git
	engine.git = func(ctx context.Context, repo string, args ...string) (string, error) {
		if strings.Join(args, " ") == "diff --name-status "+incident.CurrentBaseSHA+".."+newHead {
			return "M\t" + arbiterConflictPath + "\nM\tforeign.txt", nil
		}
		return originalGit(ctx, repo, args...)
	}
	if err := engine.validateArbiterRecoveryCandidate(context.Background(), candidate, incident); err == nil {
		t.Fatal("recovery with an extra file was accepted")
	}
}

type fakeArbiterRuntime struct {
	destroyed int
	config    ports.RuntimeConfig
}

func (f *fakeArbiterRuntime) Create(_ context.Context, cfg ports.RuntimeConfig) (ports.RuntimeHandle, error) {
	f.config = cfg
	return ports.RuntimeHandle{ID: string(cfg.SessionID)}, nil
}
func (f *fakeArbiterRuntime) Destroy(context.Context, ports.RuntimeHandle) error {
	f.destroyed++
	return nil
}
func (f *fakeArbiterRuntime) IsAlive(context.Context, ports.RuntimeHandle) (bool, error) {
	return true, nil
}

func TestArbiterLauncherPreflightsHardBudgetAndCreatesOneStableSupervisor(t *testing.T) {
	incident := exactTestArbiterIncident(t)
	dataDir := t.TempDir()
	incident.WorktreePath = filepath.Join(dataDir, "unused-worker")
	runtime := &fakeArbiterRuntime{}
	launcher := NewArbiterLauncher(runtime, dataDir, filepath.Join(dataDir, "run", "running.json")).(*arbiterLauncher)
	executable := filepath.Join(dataDir, "ao")
	if err := os.WriteFile(executable, []byte("test"), 0o700); err != nil {
		t.Fatal(err)
	}
	launcher.lookPath = func(name string) (string, error) {
		if name != "codex" {
			return "", errors.New("foreign binary")
		}
		return "/opt/codex", nil
	}
	launcher.executable = func() (string, error) { return executable, nil }
	var probe []string
	launcher.command = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "/opt/codex" {
			t.Fatalf("probe binary = %s", name)
		}
		probe = append([]string(nil), args...)
		return nil, nil
	}
	if err := launcher.Preflight(context.Background(), incident); err != nil {
		t.Fatal(err)
	}
	if !containsArgSequence(probe, []string{"--enable", "rollout_budget"}) ||
		!containsArgSequence(probe, []string{"-c", "rollout_budget.limit_tokens=16384"}) ||
		!containsArgSequence(probe, []string{"--sandbox", "read-only"}) ||
		!containsArgSequence(probe, []string{"--model", ArbiterModel}) || probe[len(probe)-1] != "--help" {
		t.Fatalf("preflight argv = %#v", probe)
	}
	if err := launcher.Launch(context.Background(), incident); err != nil {
		t.Fatal(err)
	}
	if runtime.destroyed != 1 || runtime.config.SessionID != ArbiterRuntimeHandle || runtime.config.WorkspacePath == incident.WorktreePath {
		t.Fatalf("runtime destroyed=%d config=%+v", runtime.destroyed, runtime.config)
	}
	joined := strings.Join(runtime.config.Argv, "\x00")
	for _, exact := range []string{"arbiter\x00supervise", "--incident\x00" + incident.IncidentID, "--identity-digest\x00" + incident.IdentityDigest, "rollout_budget.limit_tokens=16384", "--output-schema", "--output-last-message"} {
		if !strings.Contains(joined, exact) {
			t.Fatalf("launch argv lacks %q: %#v", exact, runtime.config.Argv)
		}
	}
	if strings.Contains(joined, "dangerously-bypass") || reflect.DeepEqual(runtime.config.Env, map[string]string{}) {
		t.Fatalf("unsafe launch config: %+v", runtime.config)
	}
	resultPath, _ := launcher.ResultPath(incident)
	if _, err := os.Stat(filepath.Join(filepath.Dir(resultPath), "input.json")); err != nil {
		t.Fatal(err)
	}
}

func containsArgSequence(values, want []string) bool {
	for i := 0; i+len(want) <= len(values); i++ {
		if reflect.DeepEqual(values[i:i+len(want)], want) {
			return true
		}
	}
	return false
}
