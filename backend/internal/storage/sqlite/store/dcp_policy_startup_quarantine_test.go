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
		Target: "wb-browser-extension", Profile: "repo-only", Repository: "orenvlad-ai/wb-browser-extension",
		PolicyVersion: "dcp.repo-only.happy-path/v1", SessionID: "wb-browser-extension-1", CardNumber: 1,
	}
	repoOnlySession := gen.Session{ID: "wb-browser-extension-1", ProjectID: "wb-browser-extension", Num: 1}
	wbc := gen.DcpReviewLabPolicyTask{
		Target: "wb-core", Profile: "repo-only", Repository: "orenvlad-ai/wb-core",
		PolicyVersion: "dcp.wb-core.repo-only.release-train/v1", SessionID: "wb-core-1", CardNumber: 1,
	}
	wbcSession := gen.Session{ID: "wb-core-1", ProjectID: "wb-core", Num: 1}
	legacy := gen.DcpReviewLabPolicyTask{
		TaskID: "price-arch-v1", PayloadDigest: "efe6a81cfff28be89cc327bdc9e2380ca585fcc6b03064c0290b6aaf4c7b59fe",
		Target: "wb-price-extension", Profile: "repo-only", Repository: "orenvlad-ai/wb-price-extension",
		PolicyVersion: "dcp.repo-only.happy-path/v1", SessionID: "wb-price-extension-1", CardNumber: 1,
		WorktreePath: "/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/wb-price-extension/wb-price-extension-1",
		SourceBranch: "ao/wb-price-extension-1/root", State: "merged", Revision: 7,
		PRURL: "https://github.com/orenvlad-ai/wb-price-extension/pull/1", PRNumber: 1,
		CurrentHeadSha: "afc748eba5ff05c0dc24d3002c690ec9f44984fb",
		ReviewRunID:    "b0acfb9e-600c-4816-bb2f-02a67817ea05",
		AdmissionID:    "dcp-admission-b0acfb9e-600c-4816-bb2f-02a67817ea05",
		MergeCommitSha: "62853496837f64522bb08ba56169f60f3b0f9a2c",
	}
	legacySession := gen.Session{ID: "wb-price-extension-1", ProjectID: "wb-price-extension", Num: 1}

	if !isExactDCPPolicyStartupQuarantineSession(synthetic, syntheticSession) {
		t.Fatal("exact synthetic policy session was rejected")
	}
	if !isExactDCPPolicyStartupQuarantineSession(repoOnly, repoOnlySession) {
		t.Fatal("exact repo-only policy session was rejected")
	}
	if !isExactDCPPolicyStartupQuarantineSession(wbc, wbcSession) {
		t.Fatal("exact WBC repo-only policy session was rejected")
	}
	if !isExactDCPPolicyStartupQuarantineSession(legacy, legacySession) {
		t.Fatal("exact terminal legacy policy session was rejected")
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
			v.SessionID = "wb-browser-extension-0"
			return v
		}(), session: gen.Session{ID: "wb-browser-extension-0", ProjectID: "wb-browser-extension", Num: 0}},
		{name: "legacy nonterminal", task: func() gen.DcpReviewLabPolicyTask {
			v := legacy
			v.State = "ci_waiting"
			v.MergeCommitSha = ""
			return v
		}(), session: legacySession},
		{name: "legacy different task", task: func() gen.DcpReviewLabPolicyTask { v := legacy; v.TaskID = "mv3-shell-v1"; return v }(), session: legacySession},
		{name: "legacy wrong merge", task: func() gen.DcpReviewLabPolicyTask {
			v := legacy
			v.MergeCommitSha = "1111111111111111111111111111111111111111"
			return v
		}(), session: legacySession},
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
		{name: "foreign project", task: repoOnly, session: gen.Session{ID: "wb-browser-extension-1", ProjectID: "dcp-review-lab", Num: 1}},
		{name: "wrong session id", task: repoOnly, session: gen.Session{ID: "wb-browser-extension-2", ProjectID: "wb-browser-extension", Num: 1}},
		{name: "wrong session number", task: repoOnly, session: gen.Session{ID: "wb-browser-extension-1", ProjectID: "wb-browser-extension", Num: 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if isExactDCPPolicyStartupQuarantineSession(tc.task, tc.session) {
				t.Fatal("foreign or crossed policy session was accepted")
			}
		})
	}
}
