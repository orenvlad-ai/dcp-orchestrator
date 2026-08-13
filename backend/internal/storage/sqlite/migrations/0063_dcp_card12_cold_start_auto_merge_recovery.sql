-- +goose Up
-- The source-0062 live start again proved the startup quarantine and launched
-- no governed worker. It failed before backup/action because the exact
-- branch-attached conflict includes Git's normal AUTO_MERGE tree ref from the
-- preserved conflict, while source-0061 incorrectly classified every such ref
-- as an active mutator. Preserve that exact zero-call failure and re-arm only
-- the same recovery row for exact AUTO_MERGE-aware backup/reconstruction.
CREATE TABLE dcp_card12_cold_start_auto_merge_recovery (
    correction_id              TEXT PRIMARY KEY CHECK (correction_id = 'dcp-card12-cold-start-auto-merge-recovery-e29a07a0b1aaddee25324e025ec23ab53b63007f78d76155ea79cef1bda52e79'),
    recovery_id                TEXT NOT NULL UNIQUE REFERENCES dcp_review_lab_card12_cold_start_recovery (recovery_id) ON DELETE RESTRICT,
    recovery_generation        INTEGER NOT NULL CHECK (recovery_generation = 1),
    recovery_identity_digest   TEXT NOT NULL CHECK (recovery_identity_digest = '087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'),
    prior_status               TEXT NOT NULL CHECK (prior_status = 'failed'),
    prior_error_code           TEXT NOT NULL CHECK (prior_error_code = 'preflight_or_backup_failed'),
    prior_revision             INTEGER NOT NULL CHECK (prior_revision = 3),
    prior_worker_calls         INTEGER NOT NULL CHECK (prior_worker_calls = 0),
    prior_arbiter_calls        INTEGER NOT NULL CHECK (prior_arbiter_calls = 0),
    prior_action_count         INTEGER NOT NULL CHECK (prior_action_count = 0),
    prior_reviewer_calls       INTEGER NOT NULL CHECK (prior_reviewer_calls = 0),
    prior_backup_path          TEXT NOT NULL CHECK (prior_backup_path = ''),
    prior_backup_digest        TEXT NOT NULL CHECK (prior_backup_digest = ''),
    prior_local_ref_after      TEXT NOT NULL CHECK (prior_local_ref_after = ''),
    prior_new_head             TEXT NOT NULL CHECK (prior_new_head = ''),
    prior_review_run_id        TEXT NOT NULL CHECK (prior_review_run_id = ''),
    prior_merge_commit_sha     TEXT NOT NULL CHECK (prior_merge_commit_sha = ''),
    prior_updated_at           TIMESTAMP NOT NULL,
    prior_finished_at          TIMESTAMP NOT NULL,
    failed_source_sha          TEXT NOT NULL CHECK (failed_source_sha = '798e9bfb8f75846d846f2ec2d4dfc9ec0076573b'),
    failed_source_tree         TEXT NOT NULL CHECK (failed_source_tree = 'e5668c51fbc3c7aae872cafbe4759fc405fa0677'),
    residue_path               TEXT NOT NULL CHECK (residue_path = 'AUTO_MERGE'),
    auto_merge_tree            TEXT NOT NULL CHECK (auto_merge_tree = '3eba7b0dec18c759875b2b33a8d7d2379caaa6a1'),
    auto_merge_file_digest     TEXT NOT NULL CHECK (auto_merge_file_digest = 'dac6e5a895aed94e8cd5a0f1a39b1c23f0201393e621c635ed228070710c13ed'),
    auto_merge_conflict_blob   TEXT NOT NULL CHECK (auto_merge_conflict_blob = '1af18aad20e3aab90ea7f1c617d330abc3b08de9'),
    marker_digest              TEXT NOT NULL CHECK (marker_digest = '5850bba009db75bf47ff88aef2d2cecbdba89c68967f51a8cdb60f48e968dc1a'),
    quarantine_rows            INTEGER NOT NULL CHECK (quarantine_rows = 2),
    quarantine_verifications   INTEGER NOT NULL CHECK (quarantine_verifications = 4),
    recovery_reason            TEXT NOT NULL CHECK (recovery_reason = 'exact_preserved_git_auto_merge_tree_was_misclassified_as_active_mutator'),
    rearmed_at                 TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX idx_dcp_card12_one_cold_start_auto_merge_recovery
    ON dcp_card12_cold_start_auto_merge_recovery ((1));

INSERT INTO dcp_card12_cold_start_auto_merge_recovery (
    correction_id, recovery_id, recovery_generation,
    recovery_identity_digest, prior_status, prior_error_code,
    prior_revision, prior_worker_calls, prior_arbiter_calls,
    prior_action_count, prior_reviewer_calls, prior_backup_path,
    prior_backup_digest, prior_local_ref_after, prior_new_head,
    prior_review_run_id, prior_merge_commit_sha, prior_updated_at,
    prior_finished_at, failed_source_sha, failed_source_tree,
    residue_path, auto_merge_tree, auto_merge_file_digest,
    auto_merge_conflict_blob, marker_digest, quarantine_rows,
    quarantine_verifications, recovery_reason, rearmed_at
)
SELECT
    'dcp-card12-cold-start-auto-merge-recovery-e29a07a0b1aaddee25324e025ec23ab53b63007f78d76155ea79cef1bda52e79',
    recovery.recovery_id, recovery.generation, recovery.identity_digest,
    recovery.status, recovery.error_code, recovery.revision,
    recovery.worker_model_call_count, recovery.arbiter_model_call_count,
    recovery.model_free_action_count, recovery.reviewer_model_call_count,
    recovery.backup_path, recovery.backup_digest, recovery.local_ref_after,
    recovery.new_head, recovery.recovery_review_run_id,
    recovery.merge_commit_sha, recovery.updated_at, recovery.finished_at,
    '798e9bfb8f75846d846f2ec2d4dfc9ec0076573b',
    'e5668c51fbc3c7aae872cafbe4759fc405fa0677',
    'AUTO_MERGE', '3eba7b0dec18c759875b2b33a8d7d2379caaa6a1',
    'dac6e5a895aed94e8cd5a0f1a39b1c23f0201393e621c635ed228070710c13ed',
    '1af18aad20e3aab90ea7f1c617d330abc3b08de9',
    '5850bba009db75bf47ff88aef2d2cecbdba89c68967f51a8cdb60f48e968dc1a',
    (SELECT count(*) FROM dcp_governed_startup_quarantine),
    (SELECT sum(verification_count) FROM dcp_governed_startup_quarantine),
    'exact_preserved_git_auto_merge_tree_was_misclassified_as_active_mutator',
    CURRENT_TIMESTAMP
FROM dcp_review_lab_card12_cold_start_recovery recovery
WHERE recovery.recovery_id = 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
  AND recovery.generation = 1
  AND recovery.identity_digest = '087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
  AND recovery.contract_commit = '623c3896a50d410e5b305ed08cf29abdc40b5b23'
  AND recovery.status = 'failed'
  AND recovery.error_code = 'preflight_or_backup_failed'
  AND recovery.revision = 3
  AND recovery.worker_model_call_count = 0
  AND recovery.arbiter_model_call_count = 0
  AND recovery.model_free_action_count = 0
  AND recovery.reviewer_model_call_count = 0
  AND recovery.backup_path = '' AND recovery.backup_digest = ''
  AND recovery.local_ref_before = '' AND recovery.local_ref_after = ''
  AND recovery.new_head = '' AND recovery.new_commit = ''
  AND recovery.provider_new_head = ''
  AND recovery.recovery_review_run_id = ''
  AND recovery.merge_commit_sha = '' AND recovery.finished_at IS NOT NULL
  AND (SELECT count(*) FROM dcp_card12_cold_start_tool_path_recovery tool
       WHERE tool.recovery_id = recovery.recovery_id
         AND tool.prior_revision = 1
         AND tool.failed_source_sha = '032e16aa3025858eeddecc1a25e87d4ec8ea4f18'
         AND tool.physical_tool_path = '/opt/homebrew/Cellar/gh/2.87.2/bin/gh') = 1
  AND (SELECT count(*) FROM dcp_governed_startup_quarantine q
       WHERE q.recovery_id = recovery.recovery_id
         AND q.contract_commit = recovery.contract_commit
         AND q.verification_count = 2
         AND ((q.session_id = 'dcp-review-lab-11' AND q.classification = 'governed_terminal')
           OR (q.session_id = 'dcp-review-lab-12' AND q.classification = 'governed_recovery'))) = 2;

CREATE TABLE dcp_card12_cold_start_auto_merge_up_guard (
    recovery_rows INTEGER NOT NULL,
    quarantine_rows INTEGER NOT NULL,
    audit_rows INTEGER NOT NULL,
    CHECK ((recovery_rows = 0 AND quarantine_rows = 0 AND audit_rows = 0) OR
           (recovery_rows = 1 AND quarantine_rows = 2 AND audit_rows = 1))
);
INSERT INTO dcp_card12_cold_start_auto_merge_up_guard
SELECT (SELECT count(*) FROM dcp_review_lab_card12_cold_start_recovery),
       (SELECT count(*) FROM dcp_governed_startup_quarantine),
       (SELECT count(*) FROM dcp_card12_cold_start_auto_merge_recovery);

UPDATE dcp_review_lab_card12_cold_start_recovery
SET status = 'authorized', error_code = '', revision = revision + 1,
    updated_at = CURRENT_TIMESTAMP, finished_at = NULL
WHERE recovery_id = 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
  AND status = 'failed' AND error_code = 'preflight_or_backup_failed'
  AND revision = 3 AND worker_model_call_count = 0
  AND arbiter_model_call_count = 0 AND model_free_action_count = 0
  AND reviewer_model_call_count = 0 AND backup_path = ''
  AND backup_digest = '' AND new_head = '' AND merge_commit_sha = ''
  AND EXISTS (
    SELECT 1 FROM dcp_card12_cold_start_auto_merge_recovery audit
    WHERE audit.recovery_id = dcp_review_lab_card12_cold_start_recovery.recovery_id
  );

CREATE TABLE dcp_card12_cold_start_auto_merge_rearm_guard (
    eligible_rows INTEGER NOT NULL CHECK (eligible_rows IN (0, 1)),
    audit_rows INTEGER NOT NULL CHECK (audit_rows = eligible_rows),
    authorized_rows INTEGER NOT NULL CHECK (authorized_rows = eligible_rows)
);
INSERT INTO dcp_card12_cold_start_auto_merge_rearm_guard
SELECT count(*),
       (SELECT count(*) FROM dcp_card12_cold_start_auto_merge_recovery),
       (SELECT count(*) FROM dcp_review_lab_card12_cold_start_recovery
        WHERE status = 'authorized' AND revision = 4
          AND worker_model_call_count = 0 AND arbiter_model_call_count = 0
          AND model_free_action_count = 0 AND reviewer_model_call_count = 0
          AND backup_path = '' AND backup_digest = '' AND error_code = '')
FROM dcp_review_lab_card12_cold_start_recovery;
DROP TABLE dcp_card12_cold_start_auto_merge_rearm_guard;
DROP TABLE dcp_card12_cold_start_auto_merge_up_guard;

-- +goose StatementBegin
CREATE TRIGGER dcp_card12_cold_start_auto_merge_recovery_no_update
BEFORE UPDATE ON dcp_card12_cold_start_auto_merge_recovery
BEGIN
    SELECT RAISE(ABORT, 'card-12 cold-start AUTO_MERGE recovery audit is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_card12_cold_start_auto_merge_recovery_no_delete
BEFORE DELETE ON dcp_card12_cold_start_auto_merge_recovery
BEGIN
    SELECT RAISE(ABORT, 'card-12 cold-start AUTO_MERGE recovery audit is immutable');
END;
-- +goose StatementEnd

-- +goose Down
CREATE TABLE dcp_card12_cold_start_auto_merge_down_guard (
    recovery_rows INTEGER NOT NULL CHECK (recovery_rows IN (0, 1)),
    audit_rows INTEGER NOT NULL CHECK (audit_rows = recovery_rows),
    status TEXT NOT NULL CHECK (status IN ('', 'authorized')),
    revision INTEGER NOT NULL CHECK (revision IN (0, 4)),
    action_count INTEGER NOT NULL CHECK (action_count = 0),
    reviewer_count INTEGER NOT NULL CHECK (reviewer_count = 0),
    backup_path TEXT NOT NULL CHECK (backup_path = ''),
    new_head TEXT NOT NULL CHECK (new_head = '')
);
INSERT INTO dcp_card12_cold_start_auto_merge_down_guard
SELECT count(*),
       (SELECT count(*) FROM dcp_card12_cold_start_auto_merge_recovery),
       coalesce(max(status), ''), coalesce(max(revision), 0),
       coalesce(max(model_free_action_count), 0),
       coalesce(max(reviewer_model_call_count), 0),
       coalesce(max(backup_path), ''), coalesce(max(new_head), '')
FROM dcp_review_lab_card12_cold_start_recovery;

UPDATE dcp_review_lab_card12_cold_start_recovery
SET status = 'failed', error_code = 'preflight_or_backup_failed',
    revision = 3,
    updated_at = (SELECT prior_updated_at FROM dcp_card12_cold_start_auto_merge_recovery),
    finished_at = (SELECT prior_finished_at FROM dcp_card12_cold_start_auto_merge_recovery)
WHERE status = 'authorized' AND revision = 4
  AND worker_model_call_count = 0 AND arbiter_model_call_count = 0
  AND model_free_action_count = 0 AND reviewer_model_call_count = 0
  AND backup_path = '' AND backup_digest = '' AND new_head = ''
  AND EXISTS (SELECT 1 FROM dcp_card12_cold_start_auto_merge_recovery);

DROP TRIGGER dcp_card12_cold_start_auto_merge_recovery_no_update;
DROP TRIGGER dcp_card12_cold_start_auto_merge_recovery_no_delete;
DROP INDEX idx_dcp_card12_one_cold_start_auto_merge_recovery;
DROP TABLE dcp_card12_cold_start_auto_merge_recovery;
DROP TABLE dcp_card12_cold_start_auto_merge_down_guard;
