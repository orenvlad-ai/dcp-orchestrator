package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestCard13CreationBaseRepairRequiresExactPassiveIdentity(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
CREATE TABLE sessions (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL, num INTEGER NOT NULL,
  kind TEXT NOT NULL, harness TEXT NOT NULL, display_name TEXT NOT NULL,
  activity_state TEXT NOT NULL, is_terminated INTEGER NOT NULL,
  branch TEXT NOT NULL, workspace_path TEXT NOT NULL,
  runtime_launch_id TEXT NOT NULL, reviewer_harness TEXT NOT NULL,
  terminate_on_pr_merge INTEGER NOT NULL, diff_base_sha TEXT NOT NULL,
  diff_base_ref TEXT NOT NULL
);
CREATE TABLE dcp_review_lab_policy_task (
  task_id TEXT PRIMARY KEY, session_id TEXT NOT NULL, card_number INTEGER NOT NULL,
  target TEXT NOT NULL, profile TEXT NOT NULL, repository TEXT NOT NULL,
  policy_version TEXT NOT NULL, worktree_path TEXT NOT NULL,
  source_branch TEXT NOT NULL, state TEXT NOT NULL, revision INTEGER NOT NULL,
  repair_count INTEGER NOT NULL, pr_url TEXT NOT NULL, pr_number INTEGER NOT NULL,
  current_head_sha TEXT NOT NULL, review_run_id TEXT NOT NULL,
  admission_id TEXT NOT NULL, merge_commit_sha TEXT NOT NULL,
  error_code TEXT NOT NULL, incident_packet TEXT NOT NULL
);
CREATE TABLE pr (
  url TEXT PRIMARY KEY, session_id TEXT NOT NULL, number INTEGER NOT NULL,
  pr_state TEXT NOT NULL, provider TEXT NOT NULL, host TEXT NOT NULL,
  repo TEXT NOT NULL, source_branch TEXT NOT NULL, target_branch TEXT NOT NULL,
  head_sha TEXT NOT NULL, base_sha TEXT NOT NULL, author TEXT NOT NULL,
  is_draft INTEGER NOT NULL, is_merged INTEGER NOT NULL, is_closed INTEGER NOT NULL,
  provider_state TEXT NOT NULL, provider_mergeable TEXT NOT NULL,
  provider_merge_state_status TEXT NOT NULL
);
CREATE TABLE review_run (
  id TEXT PRIMARY KEY, review_id TEXT NOT NULL, batch_id TEXT NOT NULL,
  session_id TEXT NOT NULL, harness TEXT NOT NULL, pr_url TEXT NOT NULL,
  target_sha TEXT NOT NULL, status TEXT NOT NULL, verdict TEXT NOT NULL,
  result_channel TEXT NOT NULL, github_review_id TEXT NOT NULL,
  terminal_merge_status TEXT NOT NULL, terminal_merge_commit_sha TEXT NOT NULL,
  terminal_merge_error TEXT NOT NULL
);
CREATE TABLE dcp_review_lab_admission (
  id TEXT PRIMARY KEY, sequence INTEGER NOT NULL, review_run_id TEXT NOT NULL,
  session_id TEXT NOT NULL, pr_url TEXT NOT NULL, pr_number INTEGER NOT NULL,
  target_sha TEXT NOT NULL, review_base_sha TEXT NOT NULL,
  admitted_base_sha TEXT NOT NULL, status TEXT NOT NULL, lease_id TEXT NOT NULL,
  merge_commit_sha TEXT NOT NULL, error_code TEXT NOT NULL,
  incident_packet TEXT NOT NULL, refresh_wake_count INTEGER NOT NULL
);
CREATE TABLE dcp_model_action (
  id TEXT PRIMARY KEY, task_id TEXT NOT NULL, session_id TEXT NOT NULL,
  kind TEXT NOT NULL, exact_head_sha TEXT NOT NULL, status TEXT NOT NULL,
  slot INTEGER NOT NULL, review_run_id TEXT NOT NULL, error_code TEXT NOT NULL
);
CREATE TABLE pr_checks (
  pr_url TEXT NOT NULL, name TEXT NOT NULL, commit_hash TEXT NOT NULL,
  status TEXT NOT NULL, conclusion TEXT NOT NULL
);
INSERT INTO sessions VALUES (
  'dcp-review-lab-13', 'dcp-review-lab', 13, 'worker', 'codex', 'DCP:chat-probe-b',
  'idle', 0, 'ao/dcp-review-lab-13/root', '/managed/dcp-review-lab-13',
  '', '', 0, '', ''
);
INSERT INTO dcp_review_lab_policy_task VALUES (
  'chat-probe-b', 'dcp-review-lab-13', 13, 'dcp-review-lab', 'synthetic-pr',
  'orenvlad-ai/dcp-review-lab', 'dcp.review-lab.happy-path/v1',
  '/managed/dcp-review-lab-13', 'ao/dcp-review-lab-13/root',
  'admission_waiting', 9, 0,
  'https://github.com/orenvlad-ai/dcp-review-lab/pull/10', 10,
  'e467d1a44668294d59cca15a756c6cef18e4b247',
  '152048c0-6720-4397-9430-df975a453807',
  'dcp-admission-152048c0-6720-4397-9430-df975a453807', '', '', ''
);
INSERT INTO pr VALUES (
  'https://github.com/orenvlad-ai/dcp-review-lab/pull/10', 'dcp-review-lab-13', 10,
  'open', 'github', 'github.com', 'orenvlad-ai/dcp-review-lab',
  'ao/dcp-review-lab-13/root', 'main',
  'e467d1a44668294d59cca15a756c6cef18e4b247',
  '5bfd20d3b3f5b7d9d9ccb02500b742a917e6ea01', 'orenvlad-ai',
  0, 0, 0, 'OPEN', 'MERGEABLE', 'CLEAN'
);
INSERT INTO review_run VALUES (
  '152048c0-6720-4397-9430-df975a453807',
  '3ad4fe55-b014-4590-bf85-a9038b0d29d6',
  'c7989a9e-cca9-40fc-9659-5246a68590eb', 'dcp-review-lab-13', 'codex',
  'https://github.com/orenvlad-ai/dcp-review-lab/pull/10',
  'e467d1a44668294d59cca15a756c6cef18e4b247',
  'complete', 'approved', 'structured_dcp_v1', '', '', '', ''
);
INSERT INTO dcp_review_lab_admission VALUES (
  'dcp-admission-152048c0-6720-4397-9430-df975a453807', 5,
  '152048c0-6720-4397-9430-df975a453807', 'dcp-review-lab-13',
  'https://github.com/orenvlad-ai/dcp-review-lab/pull/10', 10,
  'e467d1a44668294d59cca15a756c6cef18e4b247',
  '5bfd20d3b3f5b7d9d9ccb02500b742a917e6ea01',
  '', 'waiting', '', '', '', '', 0
);
INSERT INTO dcp_model_action VALUES
  ('dcp-model-chat-probe-b-worker-1', 'chat-probe-b', 'dcp-review-lab-13',
   'initial_worker', '', 'succeeded', 0, '', ''),
  ('dcp-model-chat-probe-b-review-1', 'chat-probe-b', 'dcp-review-lab-13',
   'reviewer', 'e467d1a44668294d59cca15a756c6cef18e4b247', 'succeeded', 0,
   '152048c0-6720-4397-9430-df975a453807', '');
INSERT INTO pr_checks VALUES (
  'https://github.com/orenvlad-ai/dcp-review-lab/pull/10', 'dcp-review-lab',
  'e467d1a44668294d59cca15a756c6cef18e4b247', 'passed', 'success'
);`); err != nil {
		t.Fatal(err)
	}

	if err := repairDCPReviewLabCard13CreationBase(db); err != nil {
		t.Fatalf("apply exact repair: %v", err)
	}
	var sha, ref string
	if err := db.QueryRow(`SELECT diff_base_sha, diff_base_ref FROM sessions WHERE id='dcp-review-lab-13'`).Scan(&sha, &ref); err != nil {
		t.Fatal(err)
	}
	if sha != "5bfd20d3b3f5b7d9d9ccb02500b742a917e6ea01" || ref != "origin/main" {
		t.Fatalf("creation base not repaired exactly: %s %s", sha, ref)
	}

	if _, err := db.Exec(`UPDATE sessions SET diff_base_sha='', diff_base_ref='' WHERE id='dcp-review-lab-13'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE dcp_model_action SET status='running', slot=1 WHERE id='dcp-model-chat-probe-b-review-1'`); err != nil {
		t.Fatal(err)
	}
	if err := repairDCPReviewLabCard13CreationBase(db); err != nil {
		t.Fatalf("replay drifted repair: %v", err)
	}
	if err := db.QueryRow(`SELECT diff_base_sha, diff_base_ref FROM sessions WHERE id='dcp-review-lab-13'`).Scan(&sha, &ref); err != nil {
		t.Fatal(err)
	}
	if sha != "" || ref != "" {
		t.Fatalf("active model fact did not fail closed: %s %s", sha, ref)
	}
}
