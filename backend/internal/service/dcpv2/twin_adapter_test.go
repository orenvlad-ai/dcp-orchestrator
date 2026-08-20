package dcpv2

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestTwinAdapterObservesOneExactPRAndNamedCheck(t *testing.T) {
	const base = "1111111111111111111111111111111111111111"
	const head = "2222222222222222222222222222222222222222"
	run := func(_ context.Context, _ []byte, args ...string) ([]byte, error) {
		path := strings.Join(args, " ")
		switch {
		case strings.Contains(path, "/git/ref/heads/main"):
			return []byte(`{"object":{"sha":"` + base + `"}}`), nil
		case strings.Contains(path, "/pulls?"):
			return []byte(`[{"number":7,"state":"open","draft":false,"merged":false,"base":{"ref":"main","sha":"` + base + `"},"head":{"ref":"ao/twin-1/root","sha":"` + head + `","repo":{"full_name":"orenvlad-ai/dcp-wbc-integration-lab"}}}]`), nil
		case strings.Contains(path, "/pulls/7"):
			return []byte(`{"number":7,"state":"open","draft":false,"merged":false,"mergeable":true,"mergeable_state":"clean","base":{"ref":"main","sha":"` + base + `"},"head":{"ref":"ao/twin-1/root","sha":"` + head + `","repo":{"full_name":"orenvlad-ai/dcp-wbc-integration-lab"}}}`), nil
		case strings.Contains(path, "/check-runs"):
			return []byte(`{"check_runs":[{"id":91,"name":"baseline","status":"completed","conclusion":"success","details_url":"https://github.com/orenvlad-ai/dcp-wbc-integration-lab/actions/runs/88","head_sha":"` + head + `"}]}`), nil
		default:
			t.Fatalf("unexpected gh call %q", path)
			return nil, nil
		}
	}
	facts, err := newTwinGitHubAdapterForTest(run).ObserveChecks(context.Background(), "ao/twin-1/root")
	if err != nil {
		t.Fatal(err)
	}
	if facts.PRNumber != 7 || facts.BaseSHA != base || facts.MainSHA != base || facts.HeadSHA != head ||
		facts.CheckRunID != 91 || !facts.CheckPassed || len(facts.EvidenceHash) != 64 {
		t.Fatalf("facts=%+v", facts)
	}
}

func TestTwinManifestAndDispatchAreExactAndIdempotencyBound(t *testing.T) {
	now := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	task, revision, admission := twinManifestFixture(now)
	manifest, err := BuildTwinManifest(task, revision, admission)
	if err != nil {
		t.Fatal(err)
	}
	admission.ManifestDigest = manifest.ManifestDigest
	var captured map[string]any
	adapter := newTwinGitHubAdapterForTest(func(_ context.Context, input []byte, args ...string) ([]byte, error) {
		if strings.Join(args, " ") != "--method POST repos/orenvlad-ai/dcp-wbc-integration-lab/dispatches --input -" {
			t.Fatalf("unexpected dispatch args %q", strings.Join(args, " "))
		}
		if err := json.Unmarshal(input, &captured); err != nil {
			t.Fatal(err)
		}
		return []byte(`{}`), nil
	})
	receipt, err := adapter.DispatchAdmission(context.Background(), task, revision, admission, admission.ManifestDigest)
	if err != nil {
		t.Fatal(err)
	}
	if captured["event_type"] != TwinDispatchEvent || receipt.ManifestDigest != admission.ManifestDigest ||
		receipt.Actor != TwinIssuerActor || len(receipt.EvidenceDigest) != 64 {
		t.Fatalf("payload=%+v receipt=%+v", captured, receipt)
	}
	if _, err := adapter.DispatchAdmission(context.Background(), task, revision, admission, strings.Repeat("0", 64)); err == nil {
		t.Fatal("wrong dispatch fence was accepted")
	}
}

func TestTwinAdapterVerifiesImmutableReleaseAndDeploymentProof(t *testing.T) {
	now := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	task, revision, admission := twinManifestFixture(now)
	manifest, _ := BuildTwinManifest(task, revision, admission)
	admission.ManifestDigest = manifest.ManifestDigest
	proof := twinProof{Protocol: "dcp-deployment-proof/v1", TargetSpec: TwinTargetSpec,
		TaskID: task.TaskID, RevisionID: revision.RevisionID, AdmissionID: admission.AdmissionID,
		AdmissionSequence: admission.Sequence, AdmissionDigest: admission.ManifestDigest,
		Repository: TwinRepository, RepositoryID: TwinRepositoryID, Base: TwinBase, PRNumber: admission.PRNumber,
		AdmittedHead: admission.HeadSHA, CheckRunID: 91, ReviewID: 55, ReviewDigest: strings.Repeat("a", 64),
		MergeSHA: "3333333333333333333333333333333333333333", MergeActor: "github-actions[bot]",
		ArtifactDigest: strings.Repeat("b", 64), DeployedSHA: "3333333333333333333333333333333333333333",
		Environment: TwinEnvironment, Service: TwinServiceName, RunID: "700", DispatchActor: TwinIssuerActor}
	for _, item := range [][2]string{{"healthz", "loopback"}, {"provenance", "loopback"}, {"post_job_readback", "forced-ssh-probe"}} {
		proof.Probes = append(proof.Probes, struct {
			Name           string `json:"name"`
			Target         string `json:"target"`
			Result         string `json:"result"`
			EvidenceDigest string `json:"evidence_digest"`
		}{item[0], item[1], "success", strings.Repeat("c", 64)})
	}
	proof.ProofDigest = digestWithoutField(proof, "proof_digest")
	manifestJSON, _ := json.Marshal(manifest)
	proofJSON, _ := json.Marshal(proof)
	archive := proofArchive(t, manifestJSON, proofJSON)
	adapter := newTwinGitHubAdapterForTest(func(_ context.Context, _ []byte, args ...string) ([]byte, error) {
		path := strings.Join(args, " ")
		if strings.Contains(path, "/actions/artifacts?name=deploy-proof-") {
			return []byte(`{"artifacts":[{"id":99,"expired":false,"workflow_run":{"id":700}}]}`), nil
		}
		if strings.Contains(path, "/actions/artifacts/99/zip") {
			return archive, nil
		}
		t.Fatalf("unexpected proof gh call %q", path)
		return nil, nil
	})
	release, err := adapter.ObserveRelease(context.Background(), task, revision, admission)
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := adapter.ObserveDeployment(context.Background(), task, revision, admission, proof.MergeSHA)
	if err != nil {
		t.Fatal(err)
	}
	if release.ProofID != proof.ProofDigest || release.RunID != "700" || deployment.DeployedSHA != proof.MergeSHA ||
		deployment.ProbeDigest != strings.Repeat("c", 64) || !deployment.Succeeded {
		t.Fatalf("release=%+v deployment=%+v", release, deployment)
	}
	proof.ReviewID++
	proof.ProofDigest = digestWithoutField(proof, "proof_digest")
	proofJSON, _ = json.Marshal(proof)
	archive = proofArchive(t, manifestJSON, proofJSON)
	if _, err := adapter.ObserveRelease(context.Background(), task, revision, admission); err == nil {
		t.Fatal("proof with a foreign review identity was accepted")
	}
}

func twinManifestFixture(now time.Time) (domain.DCPV2Task, domain.DCPV2Revision, domain.DCPV2Admission) {
	head := "2222222222222222222222222222222222222222"
	base := "1111111111111111111111111111111111111111"
	task := domain.DCPV2Task{TaskID: TwinCanaryTaskID, TargetSpecVersion: TwinTargetSpec, Repository: TwinRepository,
		RepositoryID: TwinRepositoryID, OwnerID: TwinOwnerID, BaseRef: TwinBase, Profile: TwinProfile}
	revision := domain.DCPV2Revision{RevisionID: "revision-2", TaskID: task.TaskID, Sequence: 2,
		Kind: domain.DCPV2RevisionWorker, Repository: TwinRepository, BaseRef: TwinBase, BaseSHA: base,
		HeadRef: "ao/twin-1/root", HeadSHA: head, PRNumber: 7, CreatedAt: now}
	admission := domain.DCPV2Admission{Sequence: 1, AdmissionID: "admission-1", TaskID: task.TaskID,
		RevisionID: revision.RevisionID, PRNumber: 7, HeadSHA: head, BaseSHA: base, MainSHA: base,
		RequiredCheckID: "91", ReviewID: "55:" + strings.Repeat("a", 64)}
	return task, revision, admission
}

func proofArchive(t *testing.T, manifest, proof []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string][]byte{"manifest.json": manifest, "deploy-proof.json": proof} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestStableIDsAndManifestDigestsAreDeterministic(t *testing.T) {
	a := stableID("task", "command", "1")
	if a != stableID("task", "command", "1") || a == stableID("task", "command", "2") {
		t.Fatal("stable identity is not deterministic")
	}
	if _, err := strconv.ParseInt("91", 10, 64); err != nil {
		t.Fatal(err)
	}
	external := strings.Repeat("e", 64)
	release := twinResultProofDigest(domain.DCPV2ResultRelease, external, "")
	deployment := twinResultProofDigest(domain.DCPV2ResultDeployment, external, strings.Repeat("p", 64))
	if release == deployment || len(release) != 64 || len(deployment) != 64 {
		t.Fatalf("typed result proof digests are not distinct: release=%s deployment=%s", release, deployment)
	}
}
