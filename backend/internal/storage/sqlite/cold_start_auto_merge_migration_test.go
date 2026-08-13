package sqlite

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestCard12ColdStartAutoMergeRecoveryPreservesFailureAndRearmsSameRow(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
CREATE TABLE dcp_review_lab_card12_cold_start_recovery (
  recovery_id TEXT PRIMARY KEY, generation INTEGER NOT NULL,
  identity_digest TEXT NOT NULL, contract_commit TEXT NOT NULL,
  status TEXT NOT NULL, error_code TEXT NOT NULL, revision INTEGER NOT NULL,
  worker_model_call_count INTEGER NOT NULL, arbiter_model_call_count INTEGER NOT NULL,
  model_free_action_count INTEGER NOT NULL, reviewer_model_call_count INTEGER NOT NULL,
  backup_path TEXT NOT NULL, backup_digest TEXT NOT NULL,
  local_ref_before TEXT NOT NULL, local_ref_after TEXT NOT NULL,
  new_head TEXT NOT NULL, new_commit TEXT NOT NULL, provider_new_head TEXT NOT NULL,
  recovery_review_run_id TEXT NOT NULL, merge_commit_sha TEXT NOT NULL,
  updated_at TIMESTAMP NOT NULL, finished_at TIMESTAMP
);
CREATE TABLE dcp_governed_startup_quarantine (
  session_id TEXT PRIMARY KEY, recovery_id TEXT NOT NULL,
  classification TEXT NOT NULL, contract_commit TEXT NOT NULL,
  verification_count INTEGER NOT NULL
);
CREATE TABLE dcp_card12_cold_start_tool_path_recovery (
  recovery_id TEXT PRIMARY KEY, prior_revision INTEGER NOT NULL,
  failed_source_sha TEXT NOT NULL, physical_tool_path TEXT NOT NULL
);
INSERT INTO dcp_review_lab_card12_cold_start_recovery VALUES (
  'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f',
  1, '087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f',
  '623c3896a50d410e5b305ed08cf29abdc40b5b23',
  'failed', 'preflight_or_backup_failed', 3, 0, 0, 0, 0,
  '', '', '', '', '', '', '', '', '',
  '2026-08-13 11:59:00', '2026-08-13 11:59:00'
);
INSERT INTO dcp_governed_startup_quarantine VALUES
  ('dcp-review-lab-11',
   'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f',
   'governed_terminal', '623c3896a50d410e5b305ed08cf29abdc40b5b23', 2),
  ('dcp-review-lab-12',
   'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f',
   'governed_recovery', '623c3896a50d410e5b305ed08cf29abdc40b5b23', 2);
INSERT INTO dcp_card12_cold_start_tool_path_recovery VALUES (
  'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f',
  1, '032e16aa3025858eeddecc1a25e87d4ec8ea4f18',
  '/opt/homebrew/Cellar/gh/2.87.2/bin/gh'
);
`); err != nil {
		t.Fatal(err)
	}
	migration, err := migrationsFS.ReadFile("migrations/0063_dcp_card12_cold_start_auto_merge_recovery.sql")
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
	var status, errorCode string
	var revision, workers, arbiters, actions, reviewers int
	if err := db.QueryRow(`
SELECT status, error_code, revision, worker_model_call_count,
       arbiter_model_call_count, model_free_action_count,
       reviewer_model_call_count
FROM dcp_review_lab_card12_cold_start_recovery
`).Scan(&status, &errorCode, &revision, &workers, &arbiters, &actions, &reviewers); err != nil {
		t.Fatal(err)
	}
	if status != "authorized" || errorCode != "" || revision != 4 || workers != 0 || arbiters != 0 || actions != 0 || reviewers != 0 {
		t.Fatalf("rearmed row = %s/%s rev=%d calls=%d/%d/%d/%d", status, errorCode, revision, workers, arbiters, actions, reviewers)
	}
	var auditCount, quarantineRows, quarantineVerifications, priorRevision int
	var tree, fileDigest, blob, marker, reason string
	if err := db.QueryRow(`
SELECT count(*), quarantine_rows, quarantine_verifications, prior_revision,
       auto_merge_tree, auto_merge_file_digest, auto_merge_conflict_blob,
       marker_digest, recovery_reason
FROM dcp_card12_cold_start_auto_merge_recovery
`).Scan(&auditCount, &quarantineRows, &quarantineVerifications, &priorRevision,
		&tree, &fileDigest, &blob, &marker, &reason); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 || quarantineRows != 2 || quarantineVerifications != 4 || priorRevision != 3 ||
		tree != "3eba7b0dec18c759875b2b33a8d7d2379caaa6a1" ||
		fileDigest != "dac6e5a895aed94e8cd5a0f1a39b1c23f0201393e621c635ed228070710c13ed" ||
		blob != "1af18aad20e3aab90ea7f1c617d330abc3b08de9" ||
		marker != "5850bba009db75bf47ff88aef2d2cecbdba89c68967f51a8cdb60f48e968dc1a" ||
		reason != "exact_preserved_git_auto_merge_tree_was_misclassified_as_active_mutator" {
		t.Fatalf("audit drifted: count=%d quarantine=%d/%d prior=%d", auditCount, quarantineRows, quarantineVerifications, priorRevision)
	}
	if _, err := db.Exec(`UPDATE dcp_card12_cold_start_auto_merge_recovery SET recovery_reason = 'drift'`); err == nil {
		t.Fatal("immutable audit accepted update")
	}
	if _, err := db.Exec(parts[1]); err != nil {
		t.Fatalf("safe down: %v", err)
	}
	if err := db.QueryRow(`SELECT status, error_code, revision FROM dcp_review_lab_card12_cold_start_recovery`).Scan(&status, &errorCode, &revision); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || errorCode != "preflight_or_backup_failed" || revision != 3 {
		t.Fatalf("down did not restore failure: %s/%s rev=%d", status, errorCode, revision)
	}
}
