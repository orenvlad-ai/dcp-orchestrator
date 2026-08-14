package sqlite

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestPolicyProviderPendingRecoveryPreservesWorkerAndRearmsSameTask(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
CREATE TABLE dcp_review_lab_policy_task (
  task_id TEXT PRIMARY KEY, payload_json TEXT NOT NULL, payload_digest TEXT NOT NULL,
  target TEXT NOT NULL, profile TEXT NOT NULL, repository TEXT NOT NULL,
  policy_version TEXT NOT NULL, session_id TEXT NOT NULL, card_number INTEGER NOT NULL,
  worktree_path TEXT NOT NULL, source_branch TEXT NOT NULL, prompt TEXT NOT NULL,
  state TEXT NOT NULL, revision INTEGER NOT NULL, repair_count INTEGER NOT NULL,
  pr_url TEXT NOT NULL, pr_number INTEGER NOT NULL, current_head_sha TEXT NOT NULL,
  previous_head_sha TEXT NOT NULL, review_run_id TEXT NOT NULL,
  admission_id TEXT NOT NULL, merge_commit_sha TEXT NOT NULL,
  error_code TEXT NOT NULL, incident_packet TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL
);
CREATE TRIGGER dcp_review_lab_policy_task_immutable BEFORE UPDATE ON dcp_review_lab_policy_task
WHEN OLD.state IN ('merged', 'failed', 'incident') OR NEW.revision <> OLD.revision + 1
BEGIN SELECT RAISE(ABORT, 'dcp review-lab policy immutable identity or revision violated'); END;
CREATE TABLE sessions (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL, num INTEGER NOT NULL,
  kind TEXT NOT NULL, harness TEXT NOT NULL, activity_state TEXT NOT NULL,
  is_terminated INTEGER NOT NULL, branch TEXT NOT NULL, diff_base_sha TEXT NOT NULL,
  diff_base_ref TEXT NOT NULL
);
CREATE TABLE dcp_model_action (
  id TEXT PRIMARY KEY, task_id TEXT NOT NULL, session_id TEXT NOT NULL,
  kind TEXT NOT NULL, exact_head_sha TEXT NOT NULL, status TEXT NOT NULL,
  slot INTEGER NOT NULL, launch_id TEXT NOT NULL, review_run_id TEXT NOT NULL,
  error_code TEXT NOT NULL
);
CREATE TABLE pr (
  url TEXT PRIMARY KEY, session_id TEXT NOT NULL, number INTEGER NOT NULL,
  pr_state TEXT NOT NULL, review_decision TEXT NOT NULL, ci_state TEXT NOT NULL,
  mergeability TEXT NOT NULL, provider TEXT NOT NULL, host TEXT NOT NULL,
  repo TEXT NOT NULL, source_branch TEXT NOT NULL, target_branch TEXT NOT NULL,
  head_sha TEXT NOT NULL, base_sha TEXT NOT NULL, author TEXT NOT NULL,
  is_draft INTEGER NOT NULL, is_merged INTEGER NOT NULL, is_closed INTEGER NOT NULL,
  provider_state TEXT NOT NULL, provider_mergeable TEXT NOT NULL,
  provider_merge_state_status TEXT NOT NULL, html_url TEXT NOT NULL
);
CREATE TABLE pr_checks (
  pr_url TEXT NOT NULL, name TEXT NOT NULL, commit_hash TEXT NOT NULL,
  status TEXT NOT NULL, conclusion TEXT NOT NULL, url TEXT NOT NULL
);
INSERT INTO dcp_review_lab_policy_task VALUES (
  'night-ui-b', '{}', '0223c91fbdfd9d93ab47657b197fe1cc0356d0da4f15c1f832ef5c0b5b4722a8',
  'dcp-review-lab', 'synthetic-pr', 'orenvlad-ai/dcp-review-lab',
  'dcp.review-lab.happy-path/v1', 'dcp-review-lab-20', 20,
  '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/dcp-review-lab/dcp-review-lab-20',
  'ao/dcp-review-lab-20/root', 'fixture', 'incident', 5, 0, '', 0, '', '', '', '', '',
  'provider_identity_drift',
  '{"detail":"policy PR provider identity is not exact","reason":"provider_identity_drift","schemaVersion":"dcp.review-lab.policy-incident/v1"}',
  '2026-08-14 20:31:53', '2026-08-14 20:32:52'
);
INSERT INTO sessions VALUES (
  'dcp-review-lab-20', 'dcp-review-lab', 20, 'worker', 'codex', 'idle', 0,
  'ao/dcp-review-lab-20/root', '2ef5c575b16705fb70f75d5dff47ec0f2cae21d2', 'origin/main'
);
INSERT INTO dcp_model_action VALUES (
  'dcp-model-night-ui-b-worker-1', 'night-ui-b', 'dcp-review-lab-20',
  'initial_worker', '', 'succeeded', 0, '0d38b38c-f9cd-470b-b468-946d553a3e75', '', ''
);
INSERT INTO pr VALUES (
  'https://github.com/orenvlad-ai/dcp-review-lab/pull/17', 'dcp-review-lab-20', 17,
  'open', 'none', 'passing', 'mergeable', 'github', 'github.com',
  'orenvlad-ai/dcp-review-lab', 'ao/dcp-review-lab-20/root', 'main',
  '6211c80a4b9e8b6ab30a38a64c4bca3ec38ef621',
  '2ef5c575b16705fb70f75d5dff47ec0f2cae21d2', 'orenvlad-ai', 0, 0, 0,
  'OPEN', 'MERGEABLE', 'CLEAN', 'https://github.com/orenvlad-ai/dcp-review-lab/pull/17'
);
INSERT INTO pr_checks VALUES (
  'https://github.com/orenvlad-ai/dcp-review-lab/pull/17', 'dcp-review-lab',
  '6211c80a4b9e8b6ab30a38a64c4bca3ec38ef621', 'passed', 'success',
  'https://github.com/orenvlad-ai/dcp-review-lab/actions/runs/31838388247/job/94889724858'
);`); err != nil {
		t.Fatal(err)
	}

	migration, err := migrationsFS.ReadFile("migrations/0068_dcp_policy_provider_pending_recovery.sql")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(migration), "-- +goose Down")
	if len(parts) != 2 {
		t.Fatal("provider-pending recovery lacks one exact down boundary")
	}
	if _, err := db.Exec(parts[0]); err != nil {
		t.Fatalf("apply provider-pending recovery: %v", err)
	}

	var state, code, packet string
	var revision, auditRows, actionRows int
	if err := db.QueryRow(`SELECT state, revision, error_code, incident_packet FROM dcp_review_lab_policy_task WHERE task_id='night-ui-b'`).Scan(&state, &revision, &code, &packet); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM dcp_policy_provider_pending_recovery`).Scan(&auditRows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM dcp_model_action WHERE task_id='night-ui-b'`).Scan(&actionRows); err != nil {
		t.Fatal(err)
	}
	if state != "ci_waiting" || revision != 6 || code != "" || packet != "" || auditRows != 1 || actionRows != 1 {
		t.Fatalf("recovery drifted: state=%s rev=%d code=%q packet=%q audit=%d actions=%d", state, revision, code, packet, auditRows, actionRows)
	}
	if _, err := db.Exec(`UPDATE dcp_policy_provider_pending_recovery SET prior_state='changed'`); err == nil {
		t.Fatal("immutable provider-pending recovery accepted update")
	}

	if _, err := db.Exec(parts[1]); err != nil {
		t.Fatalf("rollback before review: %v", err)
	}
	if err := db.QueryRow(`SELECT state, revision, error_code, incident_packet FROM dcp_review_lab_policy_task WHERE task_id='night-ui-b'`).Scan(&state, &revision, &code, &packet); err != nil {
		t.Fatal(err)
	}
	if state != "incident" || revision != 5 || code != "provider_identity_drift" || packet == "" {
		t.Fatalf("rollback did not preserve incident: state=%s rev=%d code=%q packet=%q", state, revision, code, packet)
	}
}
