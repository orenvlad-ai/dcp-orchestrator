package sqlite

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestCard12FinalizationProviderBaseRecoveryPreservesPushFence(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
CREATE TABLE dcp_review_lab_card12_rebase_head_finalization (
  finalization_id TEXT PRIMARY KEY, generation INTEGER NOT NULL,
  identity_digest TEXT NOT NULL, predecessor_recovery_id TEXT NOT NULL,
  pr_url TEXT NOT NULL, session_id TEXT NOT NULL, old_head TEXT NOT NULL,
  candidate_head TEXT NOT NULL, provider_base TEXT NOT NULL,
  current_main TEXT NOT NULL, status TEXT NOT NULL, error_code TEXT NOT NULL,
  revision INTEGER NOT NULL, worker_model_call_count INTEGER NOT NULL,
  arbiter_model_call_count INTEGER NOT NULL,
  model_free_action_count INTEGER NOT NULL,
  reviewer_model_call_count INTEGER NOT NULL, provider_new_head TEXT NOT NULL,
  review_run_id TEXT NOT NULL, review_id TEXT NOT NULL,
  review_batch_id TEXT NOT NULL, check_id TEXT NOT NULL,
  merge_commit_sha TEXT NOT NULL, updated_at TIMESTAMP NOT NULL,
  finished_at TIMESTAMP
);
CREATE TABLE dcp_card12_rebase_head_finalization_audit_recovery (
  correction_id TEXT PRIMARY KEY, finalization_id TEXT NOT NULL UNIQUE
);
CREATE TABLE dcp_governed_startup_quarantine (
  session_id TEXT PRIMARY KEY, recovery_id TEXT NOT NULL,
  classification TEXT NOT NULL, verification_count INTEGER NOT NULL
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
CREATE TABLE pr_checks (
  pr_url TEXT NOT NULL, name TEXT NOT NULL, commit_hash TEXT NOT NULL,
  status TEXT NOT NULL, conclusion TEXT NOT NULL, details TEXT NOT NULL,
  PRIMARY KEY (pr_url, name, commit_hash)
);
INSERT INTO dcp_review_lab_card12_rebase_head_finalization VALUES (
  'dcp-card12-rebase-head-finalization-a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b',
  1, 'a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b',
  'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f',
  'https://github.com/orenvlad-ai/dcp-review-lab/pull/9', 'dcp-review-lab-12',
  'd4fcb68051ae113ed497d02151a759800ee85633',
  '4de6ff1a0b80223a9b32a05ba68cf0b665296081',
  'dbaf01b05e85ffffa4c843a905e2fe5229eaf0da',
  'b34b31b5443890e69128db2862726950a6bbac0d',
  'failed', 'provider_identity_drift', 4, 0, 0, 1, 0, '', '', '', '', '', '',
  '2026-08-13 16:02:34', '2026-08-13 16:02:34'
);
INSERT INTO dcp_card12_rebase_head_finalization_audit_recovery VALUES (
  'dcp-card12-rebase-head-finalization-audit-recovery-52490d8c01eccc8f02984ec4d863895c0215950590cfc5309d00a1525eb8f11b',
  'dcp-card12-rebase-head-finalization-a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b'
);
INSERT INTO dcp_governed_startup_quarantine VALUES
  ('dcp-review-lab-11', 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f', 'governed_terminal', 6),
  ('dcp-review-lab-12', 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f', 'governed_recovery', 6);
INSERT INTO pr VALUES (
  'https://github.com/orenvlad-ai/dcp-review-lab/pull/9', 'dcp-review-lab-12', 9,
  'open', 'github', 'github.com', 'orenvlad-ai/dcp-review-lab',
  'ao/dcp-review-lab-12/root', 'main',
  '4de6ff1a0b80223a9b32a05ba68cf0b665296081',
  'b34b31b5443890e69128db2862726950a6bbac0d', 'orenvlad-ai',
  0, 0, 0, 'OPEN', 'MERGEABLE', 'UNSTABLE'
);
INSERT INTO pr_checks VALUES (
  'https://github.com/orenvlad-ai/dcp-review-lab/pull/9', 'dcp-review-lab',
  '4de6ff1a0b80223a9b32a05ba68cf0b665296081',
  'failed', 'failure', '94509683728'
);`); err != nil {
		t.Fatal(err)
	}
	migration, err := migrationsFS.ReadFile("migrations/0066_dcp_card12_rebase_head_finalization_provider_base_recovery.sql")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(migration), "-- +goose Down")
	if len(parts) != 2 {
		t.Fatal("provider-base recovery lacks one exact down boundary")
	}
	if _, err := db.Exec(parts[0]); err != nil {
		t.Fatalf("apply provider-base recovery: %v", err)
	}
	var status, code string
	var revision, workers, arbiters, actions, reviewers, auditRows int
	if err := db.QueryRow(`
SELECT status, error_code, revision, worker_model_call_count,
       arbiter_model_call_count, model_free_action_count,
       reviewer_model_call_count
FROM dcp_review_lab_card12_rebase_head_finalization
`).Scan(&status, &code, &revision, &workers, &arbiters, &actions, &reviewers); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM dcp_card12_rebase_head_finalization_provider_base_recovery`).Scan(&auditRows); err != nil {
		t.Fatal(err)
	}
	if auditRows != 1 || status != "running" || code != "" || revision != 5 ||
		workers != 0 || arbiters != 0 || actions != 1 || reviewers != 0 {
		t.Fatalf("recovery drifted: audit=%d status=%s code=%s rev=%d counters=%d/%d/%d/%d", auditRows, status, code, revision, workers, arbiters, actions, reviewers)
	}
	if _, err := db.Exec(`UPDATE dcp_card12_rebase_head_finalization_provider_base_recovery SET recovery_reason='changed'`); err == nil {
		t.Fatal("immutable provider-base recovery accepted update")
	}
	if _, err := db.Exec(parts[1]); err != nil {
		t.Fatalf("rollback before adoption: %v", err)
	}
	if err := db.QueryRow(`SELECT status, error_code, revision, model_free_action_count FROM dcp_review_lab_card12_rebase_head_finalization`).Scan(&status, &code, &revision, &actions); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || code != "provider_identity_drift" || revision != 4 || actions != 1 {
		t.Fatalf("rollback rewrote push fence: status=%s code=%s rev=%d actions=%d", status, code, revision, actions)
	}
}
