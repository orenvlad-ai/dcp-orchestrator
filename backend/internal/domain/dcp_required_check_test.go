package domain

import "testing"

func TestEvaluateDCPRequiredCheckIgnoresUnconfiguredReleaseTrainJobs(t *testing.T) {
	const head = "e8cca45f3995b8181fe81ead154f7a933dbacbe8"
	baseline := DCPRequiredCheck{Name: "baseline", HeadSHA: head, Status: string(PRCheckPassed), Conclusion: "success", URL: "https://github.com/orenvlad-ai/wb-core/actions/runs/32048996893/job/95443534690"}
	currentTrainChecks := []DCPRequiredCheck{
		baseline,
		{Name: "Select queued PR", HeadSHA: head, Status: string(PRCheckSkipped), Conclusion: "skipped"},
		{Name: "Merge repo-only PR", HeadSHA: head, Status: string(PRCheckSkipped), Conclusion: "skipped"},
		{Name: "Merge and deploy live PR", HeadSHA: head, Status: string(PRCheckSkipped), Conclusion: "skipped"},
	}
	for name, checks := range map[string][]DCPRequiredCheck{"current complex train": currentTrainChecks, "future simplified train": {baseline}} {
		t.Run(name, func(t *testing.T) {
			gate, got, err := EvaluateDCPRequiredCheck("baseline", head, checks)
			if err != nil || gate != DCPRequiredCheckPassed || got != baseline {
				t.Fatalf("gate=%s check=%+v err=%v", gate, got, err)
			}
		})
	}
}

func TestEvaluateDCPRequiredCheckFailsClosed(t *testing.T) {
	const head = "e8cca45f3995b8181fe81ead154f7a933dbacbe8"
	passed := DCPRequiredCheck{Name: "baseline", HeadSHA: head, Status: string(PRCheckPassed), Conclusion: "success", URL: "https://github.com/orenvlad-ai/wb-core/actions/runs/1/job/2"}
	for _, tc := range []struct {
		name   string
		checks []DCPRequiredCheck
		gate   DCPRequiredCheckGate
	}{
		{name: "missing", gate: DCPRequiredCheckMissing},
		{name: "wrong head", checks: []DCPRequiredCheck{{Name: "baseline", HeadSHA: "0000000000000000000000000000000000000000", Status: string(PRCheckPassed), Conclusion: "success"}}, gate: DCPRequiredCheckMissing},
		{name: "pending", checks: []DCPRequiredCheck{{Name: "baseline", HeadSHA: head, Status: string(PRCheckInProgress)}}, gate: DCPRequiredCheckPending},
		{name: "duplicate", checks: []DCPRequiredCheck{passed, passed}, gate: DCPRequiredCheckRejected},
		{name: "failed", checks: []DCPRequiredCheck{{Name: "baseline", HeadSHA: head, Status: string(PRCheckFailed), Conclusion: "failure"}}, gate: DCPRequiredCheckRejected},
		{name: "cancelled", checks: []DCPRequiredCheck{{Name: "baseline", HeadSHA: head, Status: string(PRCheckCancelled), Conclusion: "cancelled"}}, gate: DCPRequiredCheckRejected},
		{name: "skipped", checks: []DCPRequiredCheck{{Name: "baseline", HeadSHA: head, Status: string(PRCheckSkipped), Conclusion: "skipped"}}, gate: DCPRequiredCheckRejected},
		{name: "malformed conclusion", checks: []DCPRequiredCheck{{Name: "baseline", HeadSHA: head, Status: string(PRCheckPassed), Conclusion: "neutral"}}, gate: DCPRequiredCheckRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gate, _, _ := EvaluateDCPRequiredCheck("baseline", head, tc.checks)
			if gate != tc.gate {
				t.Fatalf("gate=%s, want %s", gate, tc.gate)
			}
		})
	}
}
