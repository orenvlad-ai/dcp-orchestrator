package gen

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestCountExactDCPGovernedStartupQuarantineAcceptsOnlyIdleOrTerminalPairs(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
CREATE TABLE dcp_governed_startup_quarantine (
  session_id TEXT, recovery_id TEXT, classification TEXT, contract_commit TEXT
);
CREATE TABLE sessions (
  id TEXT, project_id TEXT, kind TEXT, harness TEXT, agent_session_id TEXT,
  activity_state TEXT, is_terminated INTEGER, runtime_handle_id TEXT,
  branch TEXT, workspace_path TEXT, display_name TEXT
);
CREATE TABLE dcp_review_lab_admission (
  sequence INTEGER, id TEXT, session_id TEXT, status TEXT, merge_commit_sha TEXT,
  pr_number INTEGER, pr_url TEXT
);
CREATE TABLE dcp_review_lab_card12_cold_start_recovery (
  recovery_id TEXT, identity_digest TEXT, worker_model_call_count INTEGER,
  arbiter_model_call_count INTEGER, unauthorized_worker_tokens_11 INTEGER,
  unauthorized_worker_tokens_12 INTEGER, model_free_action_count INTEGER,
  reviewer_model_call_count INTEGER, predecessor_continuation_id TEXT,
  admission_id TEXT, session_id TEXT, pr_number INTEGER, pr_url TEXT,
  merge_commit_sha TEXT
);
CREATE TABLE dcp_review_lab_card12_model_free_rebase_continuation (
  continuation_id TEXT, status TEXT, error_code TEXT, revision INTEGER,
  model_free_action_count INTEGER, reviewer_model_call_count INTEGER,
  new_head TEXT, merge_commit_sha TEXT
);
CREATE TABLE dcp_review_lab_card12_rebase_head_finalization (
  predecessor_recovery_id TEXT, admission_id TEXT, status TEXT,
  merge_commit_sha TEXT, model_free_action_count INTEGER,
  reviewer_model_call_count INTEGER, worker_model_call_count INTEGER,
  arbiter_model_call_count INTEGER
);

INSERT INTO dcp_governed_startup_quarantine VALUES
  ('dcp-review-lab-11', 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f', 'governed_terminal', '623c3896a50d410e5b305ed08cf29abdc40b5b23'),
  ('dcp-review-lab-12', 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f', 'governed_recovery', '623c3896a50d410e5b305ed08cf29abdc40b5b23');
INSERT INTO sessions VALUES
  ('dcp-review-lab-11', 'dcp-review-lab', 'worker', 'codex', '', 'idle', 0, 'dcp-review-lab-11', 'ao/dcp-review-lab-11/root', '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/dcp-review-lab/dcp-review-lab-11', 'DCP:i13-arbiter-a'),
  ('dcp-review-lab-12', 'dcp-review-lab', 'worker', 'codex', '', 'idle', 0, 'dcp-review-lab-12', 'ao/dcp-review-lab-12/root', '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/dcp-review-lab/dcp-review-lab-12', 'DCP:i13-arbiter-b');
INSERT INTO dcp_review_lab_admission VALUES
  (3, 'dcp-admission-841c6c1e-3dcd-4ffb-875e-c42dfa358919', 'dcp-review-lab-11', 'succeeded', 'b34b31b5443890e69128db2862726950a6bbac0d', 8, 'https://github.com/orenvlad-ai/dcp-review-lab/pull/8'),
  (4, 'dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34', 'dcp-review-lab-12', 'succeeded', '5bfd20d3b3f5b7d9d9ccb02500b742a917e6ea01', 9, 'https://github.com/orenvlad-ai/dcp-review-lab/pull/9');
INSERT INTO dcp_review_lab_card12_model_free_rebase_continuation VALUES
  ('dcp-card12-model-free-rebase-continuation-66eb630c1995f90b37429a2f6c57c57794dda9fc98a29149c88bdb2f01131060', 'failed', 'identity_drift', 1, 0, 0, '', '');
INSERT INTO dcp_review_lab_card12_cold_start_recovery VALUES
  ('dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f',
   '087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f',
   0, 0, 33238, 33573, 1, 1,
   'dcp-card12-model-free-rebase-continuation-66eb630c1995f90b37429a2f6c57c57794dda9fc98a29149c88bdb2f01131060',
   'dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34', 'dcp-review-lab-12', 9,
   'https://github.com/orenvlad-ai/dcp-review-lab/pull/9', '5bfd20d3b3f5b7d9d9ccb02500b742a917e6ea01');
`); err != nil {
		t.Fatal(err)
	}

	q := New(db)
	assertCount := func(want int64) {
		t.Helper()
		got, queryErr := q.CountExactDCPGovernedStartupQuarantine(context.Background())
		if queryErr != nil || got != want {
			t.Fatalf("quarantine count = %d, %v; want %d", got, queryErr, want)
		}
	}
	assertCount(2)

	if _, err := db.Exec(`UPDATE sessions SET activity_state='exited', is_terminated=1`); err != nil {
		t.Fatal(err)
	}
	assertCount(2)

	if _, err := db.Exec(`UPDATE sessions SET is_terminated=0 WHERE id='dcp-review-lab-12'`); err != nil {
		t.Fatal(err)
	}
	assertCount(1)

	if _, err := db.Exec(`UPDATE sessions SET activity_state='active', is_terminated=1 WHERE id='dcp-review-lab-12'`); err != nil {
		t.Fatal(err)
	}
	assertCount(1)
}
