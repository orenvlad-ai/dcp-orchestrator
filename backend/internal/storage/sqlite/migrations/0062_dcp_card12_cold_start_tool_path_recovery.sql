-- +goose Up
-- The first exact source-0061 recovery start established the two-row startup
-- quarantine before runtime construction and launched no governed worker, but
-- failed before backup/action because the trusted gh constant named a Homebrew
-- symlink while the verifier intentionally accepts only a physical regular
-- file. Preserve that exact zero-call failure, then re-arm only the same row.
CREATE TABLE dcp_card12_cold_start_tool_path_recovery (
    correction_id              TEXT PRIMARY KEY CHECK (correction_id = 'dcp-card12-cold-start-tool-path-recovery-a10a121ce3cf41afeeeda32396a190d6de725592570ae02d0d136f1d1cbba9e1'),
    recovery_id                TEXT NOT NULL UNIQUE REFERENCES dcp_review_lab_card12_cold_start_recovery (recovery_id) ON DELETE RESTRICT,
    recovery_generation        INTEGER NOT NULL CHECK (recovery_generation = 1),
    recovery_identity_digest   TEXT NOT NULL CHECK (recovery_identity_digest = '087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'),
    prior_status               TEXT NOT NULL CHECK (prior_status = 'failed'),
    prior_error_code           TEXT NOT NULL CHECK (prior_error_code = 'preflight_or_backup_failed'),
    prior_revision             INTEGER NOT NULL CHECK (prior_revision = 1),
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
    failed_source_sha          TEXT NOT NULL CHECK (failed_source_sha = '032e16aa3025858eeddecc1a25e87d4ec8ea4f18'),
    failed_source_tree         TEXT NOT NULL CHECK (failed_source_tree = 'cc519e93923e02d59463bbe14dd77192a237ce95'),
    rejected_tool_path         TEXT NOT NULL CHECK (rejected_tool_path = '/opt/homebrew/bin/gh'),
    physical_tool_path         TEXT NOT NULL CHECK (physical_tool_path = '/opt/homebrew/Cellar/gh/2.87.2/bin/gh'),
    physical_tool_digest       TEXT NOT NULL CHECK (physical_tool_digest = 'f392d9ad8d2260c671566936b127f5436772ce16e25b091cf1fa7b301987f27e'),
    quarantine_rows            INTEGER NOT NULL CHECK (quarantine_rows = 2),
    quarantine_verifications   INTEGER NOT NULL CHECK (quarantine_verifications = 2),
    recovery_reason            TEXT NOT NULL CHECK (recovery_reason = 'trusted_gh_constant_named_symlink_not_physical_regular_file'),
    rearmed_at                 TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX idx_dcp_card12_one_cold_start_tool_path_recovery
    ON dcp_card12_cold_start_tool_path_recovery ((1));

INSERT INTO dcp_card12_cold_start_tool_path_recovery (
    correction_id, recovery_id, recovery_generation,
    recovery_identity_digest, prior_status, prior_error_code,
    prior_revision, prior_worker_calls, prior_arbiter_calls,
    prior_action_count, prior_reviewer_calls, prior_backup_path,
    prior_backup_digest, prior_local_ref_after, prior_new_head,
    prior_review_run_id, prior_merge_commit_sha, prior_updated_at,
    prior_finished_at, failed_source_sha, failed_source_tree,
    rejected_tool_path, physical_tool_path, physical_tool_digest,
    quarantine_rows, quarantine_verifications, recovery_reason, rearmed_at
)
SELECT
    'dcp-card12-cold-start-tool-path-recovery-a10a121ce3cf41afeeeda32396a190d6de725592570ae02d0d136f1d1cbba9e1',
    recovery.recovery_id, recovery.generation, recovery.identity_digest,
    recovery.status, recovery.error_code, recovery.revision,
    recovery.worker_model_call_count, recovery.arbiter_model_call_count,
    recovery.model_free_action_count, recovery.reviewer_model_call_count,
    recovery.backup_path, recovery.backup_digest, recovery.local_ref_after,
    recovery.new_head, recovery.recovery_review_run_id,
    recovery.merge_commit_sha, recovery.updated_at, recovery.finished_at,
    '032e16aa3025858eeddecc1a25e87d4ec8ea4f18',
    'cc519e93923e02d59463bbe14dd77192a237ce95',
    '/opt/homebrew/bin/gh', '/opt/homebrew/Cellar/gh/2.87.2/bin/gh',
    'f392d9ad8d2260c671566936b127f5436772ce16e25b091cf1fa7b301987f27e',
    (SELECT count(*) FROM dcp_governed_startup_quarantine),
    (SELECT sum(verification_count) FROM dcp_governed_startup_quarantine),
    'trusted_gh_constant_named_symlink_not_physical_regular_file',
    CURRENT_TIMESTAMP
FROM dcp_review_lab_card12_cold_start_recovery recovery
WHERE recovery.recovery_id = 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
  AND recovery.generation = 1
  AND recovery.identity_digest = '087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
  AND recovery.contract_commit = '623c3896a50d410e5b305ed08cf29abdc40b5b23'
  AND recovery.status = 'failed'
  AND recovery.error_code = 'preflight_or_backup_failed'
  AND recovery.revision = 1
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
  AND (SELECT count(*) FROM dcp_governed_startup_quarantine q
       WHERE q.recovery_id = recovery.recovery_id
         AND q.contract_commit = recovery.contract_commit
         AND q.verification_count = 1
         AND ((q.session_id = 'dcp-review-lab-11' AND q.classification = 'governed_terminal')
           OR (q.session_id = 'dcp-review-lab-12' AND q.classification = 'governed_recovery'))) = 2;

-- Ordinary/burned histories have neither exact row and remain compatible;
-- an exact live contour must produce exactly one audit row.
CREATE TABLE dcp_card12_cold_start_tool_path_up_guard (
    recovery_rows INTEGER NOT NULL,
    quarantine_rows INTEGER NOT NULL,
    audit_rows INTEGER NOT NULL,
    CHECK ((recovery_rows = 0 AND quarantine_rows = 0 AND audit_rows = 0) OR
           (recovery_rows = 1 AND quarantine_rows = 2 AND audit_rows = 1))
);
INSERT INTO dcp_card12_cold_start_tool_path_up_guard
SELECT (SELECT count(*) FROM dcp_review_lab_card12_cold_start_recovery),
       (SELECT count(*) FROM dcp_governed_startup_quarantine),
       (SELECT count(*) FROM dcp_card12_cold_start_tool_path_recovery);

UPDATE dcp_review_lab_card12_cold_start_recovery
SET status = 'authorized', error_code = '', revision = revision + 1,
    updated_at = CURRENT_TIMESTAMP, finished_at = NULL
WHERE recovery_id = 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
  AND status = 'failed' AND error_code = 'preflight_or_backup_failed'
  AND revision = 1 AND worker_model_call_count = 0
  AND arbiter_model_call_count = 0 AND model_free_action_count = 0
  AND reviewer_model_call_count = 0 AND backup_path = ''
  AND backup_digest = '' AND new_head = '' AND merge_commit_sha = ''
  AND EXISTS (
    SELECT 1 FROM dcp_card12_cold_start_tool_path_recovery audit
    WHERE audit.recovery_id = dcp_review_lab_card12_cold_start_recovery.recovery_id
  );

CREATE TABLE dcp_card12_cold_start_tool_path_rearm_guard (
    eligible_rows INTEGER NOT NULL CHECK (eligible_rows IN (0, 1)),
    audit_rows INTEGER NOT NULL CHECK (audit_rows = eligible_rows),
    authorized_rows INTEGER NOT NULL CHECK (authorized_rows = eligible_rows)
);
INSERT INTO dcp_card12_cold_start_tool_path_rearm_guard
SELECT count(*),
       (SELECT count(*) FROM dcp_card12_cold_start_tool_path_recovery),
       (SELECT count(*) FROM dcp_review_lab_card12_cold_start_recovery
        WHERE status = 'authorized' AND revision = 2
          AND worker_model_call_count = 0 AND arbiter_model_call_count = 0
          AND model_free_action_count = 0 AND reviewer_model_call_count = 0
          AND backup_path = '' AND backup_digest = '' AND error_code = '')
FROM dcp_review_lab_card12_cold_start_recovery;
DROP TABLE dcp_card12_cold_start_tool_path_rearm_guard;
DROP TABLE dcp_card12_cold_start_tool_path_up_guard;

-- +goose StatementBegin
CREATE TRIGGER dcp_card12_cold_start_tool_path_recovery_no_update
BEFORE UPDATE ON dcp_card12_cold_start_tool_path_recovery
BEGIN
    SELECT RAISE(ABORT, 'card-12 cold-start tool-path recovery audit is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_card12_cold_start_tool_path_recovery_no_delete
BEFORE DELETE ON dcp_card12_cold_start_tool_path_recovery
BEGIN
    SELECT RAISE(ABORT, 'card-12 cold-start tool-path recovery audit is immutable');
END;
-- +goose StatementEnd

-- +goose Down
CREATE TABLE dcp_card12_cold_start_tool_path_down_guard (
    recovery_rows INTEGER NOT NULL CHECK (recovery_rows IN (0, 1)),
    audit_rows INTEGER NOT NULL CHECK (audit_rows = recovery_rows),
    status TEXT NOT NULL CHECK (status IN ('', 'authorized')),
    revision INTEGER NOT NULL CHECK (revision IN (0, 2)),
    action_count INTEGER NOT NULL CHECK (action_count = 0),
    reviewer_count INTEGER NOT NULL CHECK (reviewer_count = 0),
    backup_path TEXT NOT NULL CHECK (backup_path = ''),
    new_head TEXT NOT NULL CHECK (new_head = '')
);
INSERT INTO dcp_card12_cold_start_tool_path_down_guard
SELECT count(*),
       (SELECT count(*) FROM dcp_card12_cold_start_tool_path_recovery),
       coalesce(max(status), ''), coalesce(max(revision), 0),
       coalesce(max(model_free_action_count), 0),
       coalesce(max(reviewer_model_call_count), 0),
       coalesce(max(backup_path), ''), coalesce(max(new_head), '')
FROM dcp_review_lab_card12_cold_start_recovery;

UPDATE dcp_review_lab_card12_cold_start_recovery
SET status = 'failed', error_code = 'preflight_or_backup_failed',
    revision = 1,
    updated_at = (SELECT prior_updated_at FROM dcp_card12_cold_start_tool_path_recovery),
    finished_at = (SELECT prior_finished_at FROM dcp_card12_cold_start_tool_path_recovery)
WHERE status = 'authorized' AND revision = 2
  AND worker_model_call_count = 0 AND arbiter_model_call_count = 0
  AND model_free_action_count = 0 AND reviewer_model_call_count = 0
  AND backup_path = '' AND backup_digest = '' AND new_head = ''
  AND EXISTS (SELECT 1 FROM dcp_card12_cold_start_tool_path_recovery);

DROP TRIGGER dcp_card12_cold_start_tool_path_recovery_no_update;
DROP TRIGGER dcp_card12_cold_start_tool_path_recovery_no_delete;
DROP INDEX idx_dcp_card12_one_cold_start_tool_path_recovery;
DROP TABLE dcp_card12_cold_start_tool_path_recovery;
DROP TABLE dcp_card12_cold_start_tool_path_down_guard;
