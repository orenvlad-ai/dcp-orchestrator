-- +goose Up
-- Source 0064 created the exact finalization row and held the cold-start
-- quarantine with zero worker/arbiter/action/reviewer calls. Its predecessor
-- validation failed because it reused two historical audit queries whose
-- predicates intentionally describe the old rev2/rev4 authorized recovery
-- states, not the immutable terminal rev7 predecessor. Preserve that exact
-- pre-action failure and re-arm only the same finalization generation.
CREATE TABLE dcp_card12_rebase_head_finalization_audit_recovery (
    correction_id               TEXT PRIMARY KEY CHECK (correction_id = 'dcp-card12-rebase-head-finalization-audit-recovery-52490d8c01eccc8f02984ec4d863895c0215950590cfc5309d00a1525eb8f11b'),
    finalization_id             TEXT NOT NULL UNIQUE REFERENCES dcp_review_lab_card12_rebase_head_finalization (finalization_id) ON DELETE RESTRICT,
    finalization_generation     INTEGER NOT NULL CHECK (finalization_generation = 1),
    finalization_identity       TEXT NOT NULL CHECK (finalization_identity = 'a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b'),
    predecessor_recovery_id     TEXT NOT NULL CHECK (predecessor_recovery_id = 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'),
    prior_status                TEXT NOT NULL CHECK (prior_status = 'failed'),
    prior_error_code            TEXT NOT NULL CHECK (prior_error_code = 'identity_drift'),
    prior_revision              INTEGER NOT NULL CHECK (prior_revision = 1),
    prior_worker_calls          INTEGER NOT NULL CHECK (prior_worker_calls = 0),
    prior_arbiter_calls         INTEGER NOT NULL CHECK (prior_arbiter_calls = 0),
    prior_action_count          INTEGER NOT NULL CHECK (prior_action_count = 0),
    prior_reviewer_calls        INTEGER NOT NULL CHECK (prior_reviewer_calls = 0),
    prior_provider_new_head     TEXT NOT NULL CHECK (prior_provider_new_head = ''),
    prior_review_run_id         TEXT NOT NULL CHECK (prior_review_run_id = ''),
    prior_merge_commit_sha      TEXT NOT NULL CHECK (prior_merge_commit_sha = ''),
    prior_updated_at            TIMESTAMP NOT NULL,
    prior_finished_at           TIMESTAMP NOT NULL,
    failed_source_sha           TEXT NOT NULL CHECK (failed_source_sha = '6f53f74f456b869c98bb82d928f671b54672808a'),
    failed_source_tree          TEXT NOT NULL CHECK (failed_source_tree = '0fab2ee443d8bf20a0efcc524851e8c9589e6dd9'),
    tool_audit_correction_id    TEXT NOT NULL CHECK (tool_audit_correction_id = 'dcp-card12-cold-start-tool-path-recovery-a10a121ce3cf41afeeeda32396a190d6de725592570ae02d0d136f1d1cbba9e1'),
    auto_audit_correction_id    TEXT NOT NULL CHECK (auto_audit_correction_id = 'dcp-card12-cold-start-auto-merge-recovery-e29a07a0b1aaddee25324e025ec23ab53b63007f78d76155ea79cef1bda52e79'),
    tool_audit_rows             INTEGER NOT NULL CHECK (tool_audit_rows = 1),
    auto_audit_rows             INTEGER NOT NULL CHECK (auto_audit_rows = 1),
    stale_tool_query_matches    INTEGER NOT NULL CHECK (stale_tool_query_matches = 0),
    stale_auto_query_matches    INTEGER NOT NULL CHECK (stale_auto_query_matches = 0),
    quarantine_rows             INTEGER NOT NULL CHECK (quarantine_rows = 2),
    quarantine_verifications   INTEGER NOT NULL CHECK (quarantine_verifications = 10),
    recovery_reason             TEXT NOT NULL CHECK (recovery_reason = 'terminal_predecessor_was_checked_with_historical_authorized_state_audit_queries'),
    rearmed_at                  TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX idx_dcp_card12_one_rebase_head_finalization_audit_recovery
    ON dcp_card12_rebase_head_finalization_audit_recovery ((1));

INSERT INTO dcp_card12_rebase_head_finalization_audit_recovery (
    correction_id, finalization_id, finalization_generation,
    finalization_identity, predecessor_recovery_id, prior_status,
    prior_error_code, prior_revision, prior_worker_calls, prior_arbiter_calls,
    prior_action_count, prior_reviewer_calls, prior_provider_new_head,
    prior_review_run_id, prior_merge_commit_sha, prior_updated_at,
    prior_finished_at, failed_source_sha, failed_source_tree,
    tool_audit_correction_id, auto_audit_correction_id, tool_audit_rows,
    auto_audit_rows, stale_tool_query_matches, stale_auto_query_matches,
    quarantine_rows, quarantine_verifications, recovery_reason, rearmed_at
)
SELECT
    'dcp-card12-rebase-head-finalization-audit-recovery-52490d8c01eccc8f02984ec4d863895c0215950590cfc5309d00a1525eb8f11b',
    finalization.finalization_id, finalization.generation,
    finalization.identity_digest, finalization.predecessor_recovery_id,
    finalization.status, finalization.error_code, finalization.revision,
    finalization.worker_model_call_count,
    finalization.arbiter_model_call_count,
    finalization.model_free_action_count,
    finalization.reviewer_model_call_count,
    finalization.provider_new_head, finalization.review_run_id,
    finalization.merge_commit_sha, finalization.updated_at,
    finalization.finished_at,
    '6f53f74f456b869c98bb82d928f671b54672808a',
    '0fab2ee443d8bf20a0efcc524851e8c9589e6dd9',
    'dcp-card12-cold-start-tool-path-recovery-a10a121ce3cf41afeeeda32396a190d6de725592570ae02d0d136f1d1cbba9e1',
    'dcp-card12-cold-start-auto-merge-recovery-e29a07a0b1aaddee25324e025ec23ab53b63007f78d76155ea79cef1bda52e79',
    (SELECT count(*) FROM dcp_card12_cold_start_tool_path_recovery tool
     WHERE tool.correction_id = 'dcp-card12-cold-start-tool-path-recovery-a10a121ce3cf41afeeeda32396a190d6de725592570ae02d0d136f1d1cbba9e1'
       AND tool.recovery_id = finalization.predecessor_recovery_id),
    (SELECT count(*) FROM dcp_card12_cold_start_auto_merge_recovery auto
     WHERE auto.correction_id = 'dcp-card12-cold-start-auto-merge-recovery-e29a07a0b1aaddee25324e025ec23ab53b63007f78d76155ea79cef1bda52e79'
       AND auto.recovery_id = finalization.predecessor_recovery_id),
    (SELECT count(*) FROM dcp_card12_cold_start_tool_path_recovery tool
     JOIN dcp_review_lab_card12_cold_start_recovery recovery
       ON recovery.recovery_id = tool.recovery_id
     WHERE recovery.status = 'authorized' AND recovery.revision = 2),
    (SELECT count(*) FROM dcp_card12_cold_start_auto_merge_recovery auto
     JOIN dcp_review_lab_card12_cold_start_recovery recovery
       ON recovery.recovery_id = auto.recovery_id
     WHERE recovery.status = 'authorized' AND recovery.revision = 4),
    (SELECT count(*) FROM dcp_governed_startup_quarantine),
    (SELECT sum(verification_count) FROM dcp_governed_startup_quarantine),
    'terminal_predecessor_was_checked_with_historical_authorized_state_audit_queries',
    CURRENT_TIMESTAMP
FROM dcp_review_lab_card12_rebase_head_finalization finalization
JOIN dcp_review_lab_card12_cold_start_recovery predecessor
  ON predecessor.recovery_id = finalization.predecessor_recovery_id
WHERE finalization.finalization_id = 'dcp-card12-rebase-head-finalization-a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b'
  AND finalization.generation = 1
  AND finalization.identity_digest = 'a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b'
  AND finalization.contract_commit = '9465a84ec44f72f6b7c245ebddeac22d722108ae'
  AND finalization.status = 'failed' AND finalization.error_code = 'identity_drift'
  AND finalization.revision = 1
  AND finalization.worker_model_call_count = 0
  AND finalization.arbiter_model_call_count = 0
  AND finalization.model_free_action_count = 0
  AND finalization.reviewer_model_call_count = 0
  AND finalization.provider_new_head = '' AND finalization.review_run_id = ''
  AND finalization.review_id = '' AND finalization.review_batch_id = ''
  AND finalization.check_id = '' AND finalization.merge_commit_sha = ''
  AND finalization.finished_at IS NOT NULL
  AND predecessor.status = 'failed'
  AND predecessor.error_code = 'model_free_action_failed'
  AND predecessor.revision = 7
  AND predecessor.worker_model_call_count = 0
  AND predecessor.arbiter_model_call_count = 0
  AND predecessor.model_free_action_count = 1
  AND predecessor.reviewer_model_call_count = 0
  AND predecessor.backup_digest = finalization.backup_digest
  AND predecessor.local_ref_before = finalization.old_head
  AND predecessor.local_ref_after = '' AND predecessor.new_head = ''
  AND predecessor.provider_new_head = ''
  AND predecessor.recovery_review_run_id = ''
  AND predecessor.merge_commit_sha = '' AND predecessor.finished_at IS NOT NULL
  AND (SELECT count(*) FROM dcp_governed_startup_quarantine q
       WHERE q.recovery_id = predecessor.recovery_id
         AND q.contract_commit = predecessor.contract_commit
         AND q.verification_count = 5
         AND ((q.session_id = 'dcp-review-lab-11' AND q.classification = 'governed_terminal')
           OR (q.session_id = 'dcp-review-lab-12' AND q.classification = 'governed_recovery'))) = 2;

CREATE TABLE dcp_card12_rebase_head_finalization_audit_up_guard (
    finalization_rows INTEGER NOT NULL CHECK (finalization_rows IN (0, 1)),
    correction_rows INTEGER NOT NULL CHECK (correction_rows = finalization_rows)
);
INSERT INTO dcp_card12_rebase_head_finalization_audit_up_guard
SELECT count(*),
       (SELECT count(*) FROM dcp_card12_rebase_head_finalization_audit_recovery)
FROM dcp_review_lab_card12_rebase_head_finalization;

UPDATE dcp_review_lab_card12_rebase_head_finalization
SET status = 'authorized', error_code = '', revision = revision + 1,
    updated_at = CURRENT_TIMESTAMP, finished_at = NULL
WHERE finalization_id = 'dcp-card12-rebase-head-finalization-a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b'
  AND status = 'failed' AND error_code = 'identity_drift' AND revision = 1
  AND worker_model_call_count = 0 AND arbiter_model_call_count = 0
  AND model_free_action_count = 0 AND reviewer_model_call_count = 0
  AND provider_new_head = '' AND review_run_id = '' AND merge_commit_sha = ''
  AND EXISTS (
    SELECT 1 FROM dcp_card12_rebase_head_finalization_audit_recovery audit
    WHERE audit.finalization_id = dcp_review_lab_card12_rebase_head_finalization.finalization_id
  );

CREATE TABLE dcp_card12_rebase_head_finalization_audit_rearm_guard (
    eligible_rows INTEGER NOT NULL CHECK (eligible_rows IN (0, 1)),
    correction_rows INTEGER NOT NULL CHECK (correction_rows = eligible_rows),
    authorized_rows INTEGER NOT NULL CHECK (authorized_rows = eligible_rows)
);
INSERT INTO dcp_card12_rebase_head_finalization_audit_rearm_guard
SELECT count(*),
       (SELECT count(*) FROM dcp_card12_rebase_head_finalization_audit_recovery),
       (SELECT count(*) FROM dcp_review_lab_card12_rebase_head_finalization
        WHERE status = 'authorized' AND revision = 2
          AND worker_model_call_count = 0 AND arbiter_model_call_count = 0
          AND model_free_action_count = 0 AND reviewer_model_call_count = 0
          AND provider_new_head = '' AND review_run_id = ''
          AND merge_commit_sha = '' AND error_code = '')
FROM dcp_review_lab_card12_rebase_head_finalization;
DROP TABLE dcp_card12_rebase_head_finalization_audit_rearm_guard;
DROP TABLE dcp_card12_rebase_head_finalization_audit_up_guard;

-- +goose StatementBegin
CREATE TRIGGER dcp_card12_rebase_head_finalization_audit_recovery_no_update
BEFORE UPDATE ON dcp_card12_rebase_head_finalization_audit_recovery
BEGIN
    SELECT RAISE(ABORT, 'card-12 REBASE_HEAD finalization audit recovery is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_card12_rebase_head_finalization_audit_recovery_no_delete
BEFORE DELETE ON dcp_card12_rebase_head_finalization_audit_recovery
BEGIN
    SELECT RAISE(ABORT, 'card-12 REBASE_HEAD finalization audit recovery is immutable');
END;
-- +goose StatementEnd

-- +goose Down
CREATE TABLE dcp_card12_rebase_head_finalization_audit_down_guard (
    finalization_rows INTEGER NOT NULL CHECK (finalization_rows IN (0, 1)),
    correction_rows INTEGER NOT NULL CHECK (correction_rows = finalization_rows),
    status TEXT NOT NULL CHECK (status IN ('', 'authorized')),
    revision INTEGER NOT NULL CHECK (revision IN (0, 2)),
    action_count INTEGER NOT NULL CHECK (action_count = 0),
    reviewer_count INTEGER NOT NULL CHECK (reviewer_count = 0),
    provider_new_head TEXT NOT NULL CHECK (provider_new_head = ''),
    review_run_id TEXT NOT NULL CHECK (review_run_id = ''),
    merge_commit_sha TEXT NOT NULL CHECK (merge_commit_sha = '')
);
INSERT INTO dcp_card12_rebase_head_finalization_audit_down_guard
SELECT count(*),
       (SELECT count(*) FROM dcp_card12_rebase_head_finalization_audit_recovery),
       coalesce(max(status), ''), coalesce(max(revision), 0),
       coalesce(max(model_free_action_count), 0),
       coalesce(max(reviewer_model_call_count), 0),
       coalesce(max(provider_new_head), ''), coalesce(max(review_run_id), ''),
       coalesce(max(merge_commit_sha), '')
FROM dcp_review_lab_card12_rebase_head_finalization;

UPDATE dcp_review_lab_card12_rebase_head_finalization
SET status = 'failed', error_code = 'identity_drift', revision = 1,
    updated_at = (SELECT prior_updated_at FROM dcp_card12_rebase_head_finalization_audit_recovery),
    finished_at = (SELECT prior_finished_at FROM dcp_card12_rebase_head_finalization_audit_recovery)
WHERE status = 'authorized' AND revision = 2
  AND worker_model_call_count = 0 AND arbiter_model_call_count = 0
  AND model_free_action_count = 0 AND reviewer_model_call_count = 0
  AND provider_new_head = '' AND review_run_id = '' AND merge_commit_sha = ''
  AND EXISTS (SELECT 1 FROM dcp_card12_rebase_head_finalization_audit_recovery);

DROP TRIGGER dcp_card12_rebase_head_finalization_audit_recovery_no_update;
DROP TRIGGER dcp_card12_rebase_head_finalization_audit_recovery_no_delete;
DROP INDEX idx_dcp_card12_one_rebase_head_finalization_audit_recovery;
DROP TABLE dcp_card12_rebase_head_finalization_audit_recovery;
DROP TABLE dcp_card12_rebase_head_finalization_audit_down_guard;
