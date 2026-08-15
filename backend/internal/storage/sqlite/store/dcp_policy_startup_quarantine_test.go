package store

import (
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

func TestExactDCPPolicyStartupQuarantineSessionAllowlist(t *testing.T) {
	synthetic := gen.DcpReviewLabPolicyTask{
		Target: "dcp-review-lab", Profile: "synthetic-pr", Repository: "orenvlad-ai/dcp-review-lab",
		PolicyVersion: "dcp.review-lab.happy-path/v1", SessionID: "dcp-review-lab-13", CardNumber: 13,
	}
	syntheticSession := gen.Session{ID: "dcp-review-lab-13", ProjectID: "dcp-review-lab", Num: 13}
	repoOnly := gen.DcpReviewLabPolicyTask{
		Target: "wb-price-extension", Profile: "repo-only", Repository: "orenvlad-ai/wb-price-extension",
		PolicyVersion: "dcp.repo-only.happy-path/v1", SessionID: "wb-price-extension-1", CardNumber: 1,
	}
	repoOnlySession := gen.Session{ID: "wb-price-extension-1", ProjectID: "wb-price-extension", Num: 1}

	if !isExactDCPPolicyStartupQuarantineSession(synthetic, syntheticSession) {
		t.Fatal("exact synthetic policy session was rejected")
	}
	if !isExactDCPPolicyStartupQuarantineSession(repoOnly, repoOnlySession) {
		t.Fatal("exact repo-only policy session was rejected")
	}

	cases := []struct {
		name    string
		task    gen.DcpReviewLabPolicyTask
		session gen.Session
	}{
		{name: "synthetic historical card", task: func() gen.DcpReviewLabPolicyTask {
			v := synthetic
			v.CardNumber = 12
			v.SessionID = "dcp-review-lab-12"
			return v
		}(), session: gen.Session{ID: "dcp-review-lab-12", ProjectID: "dcp-review-lab", Num: 12}},
		{name: "repo zero card", task: func() gen.DcpReviewLabPolicyTask {
			v := repoOnly
			v.CardNumber = 0
			v.SessionID = "wb-price-extension-0"
			return v
		}(), session: gen.Session{ID: "wb-price-extension-0", ProjectID: "wb-price-extension", Num: 0}},
		{name: "crossed profile", task: func() gen.DcpReviewLabPolicyTask { v := repoOnly; v.Profile = "synthetic-pr"; return v }(), session: repoOnlySession},
		{name: "crossed repository", task: func() gen.DcpReviewLabPolicyTask {
			v := repoOnly
			v.Repository = "orenvlad-ai/dcp-review-lab"
			return v
		}(), session: repoOnlySession},
		{name: "crossed policy", task: func() gen.DcpReviewLabPolicyTask {
			v := repoOnly
			v.PolicyVersion = "dcp.review-lab.happy-path/v1"
			return v
		}(), session: repoOnlySession},
		{name: "foreign target", task: func() gen.DcpReviewLabPolicyTask { v := repoOnly; v.Target = "foreign"; return v }(), session: repoOnlySession},
		{name: "foreign project", task: repoOnly, session: gen.Session{ID: "wb-price-extension-1", ProjectID: "dcp-review-lab", Num: 1}},
		{name: "wrong session id", task: repoOnly, session: gen.Session{ID: "wb-price-extension-2", ProjectID: "wb-price-extension", Num: 1}},
		{name: "wrong session number", task: repoOnly, session: gen.Session{ID: "wb-price-extension-1", ProjectID: "wb-price-extension", Num: 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if isExactDCPPolicyStartupQuarantineSession(tc.task, tc.session) {
				t.Fatal("foreign or crossed policy session was accepted")
			}
		})
	}
}
