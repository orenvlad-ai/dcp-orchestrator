package sqlite

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestCard12RebaseHeadFinalizationPreservesTerminalPredecessorAndRefusesConsumedRollback(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
CREATE TABLE dcp_review_lab_card12_cold_start_recovery (
  recovery_id TEXT PRIMARY KEY, generation INTEGER NOT NULL,
  identity_digest TEXT NOT NULL, contract_commit TEXT NOT NULL,
  incident_id TEXT NOT NULL, admission_id TEXT NOT NULL, session_id TEXT NOT NULL,
  task_id TEXT NOT NULL, project_id TEXT NOT NULL, repository TEXT NOT NULL,
  worktree_path TEXT NOT NULL, source_branch TEXT NOT NULL, pr_url TEXT NOT NULL,
  pr_number INTEGER NOT NULL, old_head TEXT NOT NULL, current_main TEXT NOT NULL,
  provider_base TEXT NOT NULL, conflict_path TEXT NOT NULL,
  resolved_bytes_digest TEXT NOT NULL, resolved_blob TEXT NOT NULL,
  backup_path TEXT NOT NULL, backup_digest TEXT NOT NULL, push_ref TEXT NOT NULL,
  push_lease_old_head TEXT NOT NULL, unauthorized_worker_tokens_11 INTEGER NOT NULL,
  unauthorized_worker_tokens_12 INTEGER NOT NULL, status TEXT NOT NULL,
  error_code TEXT NOT NULL, revision INTEGER NOT NULL,
  worker_model_call_count INTEGER NOT NULL, arbiter_model_call_count INTEGER NOT NULL,
  model_free_action_count INTEGER NOT NULL, reviewer_model_call_count INTEGER NOT NULL,
  local_ref_before TEXT NOT NULL, local_ref_after TEXT NOT NULL,
  new_head TEXT NOT NULL, new_commit TEXT NOT NULL, provider_new_head TEXT NOT NULL,
  recovery_review_run_id TEXT NOT NULL, merge_commit_sha TEXT NOT NULL,
  finished_at TIMESTAMP
);
CREATE TABLE dcp_governed_startup_quarantine (
  session_id TEXT PRIMARY KEY, recovery_id TEXT NOT NULL,
  classification TEXT NOT NULL, contract_commit TEXT NOT NULL,
  verification_count INTEGER NOT NULL
);
CREATE TABLE dcp_card12_cold_start_tool_path_recovery (recovery_id TEXT PRIMARY KEY);
CREATE TABLE dcp_card12_cold_start_auto_merge_recovery (recovery_id TEXT PRIMARY KEY);
INSERT INTO dcp_review_lab_card12_cold_start_recovery VALUES (
  'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f',
  1, '087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f',
  '623c3896a50d410e5b305ed08cf29abdc40b5b23',
  'dcp-global-release-2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60',
  'dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34', 'dcp-review-lab-12',
  'i13-arbiter-b', 'dcp-review-lab', 'orenvlad-ai/dcp-review-lab',
  '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/dcp-review-lab/dcp-review-lab-12',
  'ao/dcp-review-lab-12/root', 'https://github.com/orenvlad-ai/dcp-review-lab/pull/9', 9,
  'd4fcb68051ae113ed497d02151a759800ee85633',
  'b34b31b5443890e69128db2862726950a6bbac0d',
  'dbaf01b05e85ffffa4c843a905e2fe5229eaf0da',
  'canary/i13-arbiter-conflict.txt',
  '2a5da25a78ff8bcd9aff4493f195eaefecbc70c3d4db8902dda468ccf69e5e46',
  '80a658c4cfc3ffda5786da316bc0bd10ffb1834f',
  '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/evidence/dcp-card12-cold-start-recovery/dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f',
  '82d0e5834375c380069e7d48a7fdb2066371670d92733ce59545718469a4f3dd',
  'refs/heads/ao/dcp-review-lab-12/root', 'd4fcb68051ae113ed497d02151a759800ee85633',
  33238, 33573, 'failed', 'model_free_action_failed', 7, 0, 0, 1, 0,
  'd4fcb68051ae113ed497d02151a759800ee85633', '', '', '', '', '', '',
  '2026-08-13 14:00:00'
);
INSERT INTO dcp_governed_startup_quarantine VALUES
  ('dcp-review-lab-11',
   'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f',
   'governed_terminal', '623c3896a50d410e5b305ed08cf29abdc40b5b23', 4),
  ('dcp-review-lab-12',
   'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f',
   'governed_recovery', '623c3896a50d410e5b305ed08cf29abdc40b5b23', 4);
INSERT INTO dcp_card12_cold_start_tool_path_recovery VALUES
  ('dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f');
INSERT INTO dcp_card12_cold_start_auto_merge_recovery VALUES
  ('dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f');
`); err != nil {
		t.Fatal(err)
	}
	migration, err := migrationsFS.ReadFile("migrations/0064_dcp_card12_rebase_head_finalization.sql")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(migration), "-- +goose Down")
	if len(parts) != 2 {
		t.Fatal("migration lacks one exact down boundary")
	}
	if _, err := db.Exec(parts[0]); err != nil {
		t.Fatalf("apply up: %v", err)
	}
	var status, predecessorID, backupDigest, candidate string
	var revision, workers, arbiters, actions, reviewers int
	if err := db.QueryRow(`
SELECT status, predecessor_recovery_id, backup_digest, candidate_head,
       revision, worker_model_call_count, arbiter_model_call_count,
       model_free_action_count, reviewer_model_call_count
FROM dcp_review_lab_card12_rebase_head_finalization
`).Scan(&status, &predecessorID, &backupDigest, &candidate, &revision,
		&workers, &arbiters, &actions, &reviewers); err != nil {
		t.Fatal(err)
	}
	if status != "authorized" || predecessorID == "" ||
		backupDigest != "82d0e5834375c380069e7d48a7fdb2066371670d92733ce59545718469a4f3dd" ||
		candidate != "4de6ff1a0b80223a9b32a05ba68cf0b665296081" || revision != 0 ||
		workers != 0 || arbiters != 0 || actions != 0 || reviewers != 0 {
		t.Fatalf("finalization row drifted: %s rev=%d counters=%d/%d/%d/%d", status, revision, workers, arbiters, actions, reviewers)
	}
	var predecessorStatus, predecessorError string
	var predecessorRevision, predecessorActions int
	if err := db.QueryRow(`
SELECT status, error_code, revision, model_free_action_count
FROM dcp_review_lab_card12_cold_start_recovery
`).Scan(&predecessorStatus, &predecessorError, &predecessorRevision, &predecessorActions); err != nil {
		t.Fatal(err)
	}
	if predecessorStatus != "failed" || predecessorError != "model_free_action_failed" || predecessorRevision != 7 || predecessorActions != 1 {
		t.Fatal("terminal predecessor was rewritten")
	}
	if _, err := db.Exec(`UPDATE dcp_review_lab_card12_rebase_head_finalization
SET status='running', model_free_action_count=1, revision=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(parts[1]); err == nil {
		t.Fatal("rollback accepted a consumed finalization action fence")
	}
}
