-- +goose Up
-- Preserve the one successful guarded push and the subsequent exact post-push
-- provider-base validation failure. The existing action fence remains consumed;
-- this recovery can only inspect/adopt the already-remote candidate and cannot
-- execute another push.
CREATE TABLE dcp_card12_rebase_head_finalization_provider_base_recovery (
    correction_id                 TEXT PRIMARY KEY CHECK (correction_id = 'dcp-card12-rebase-head-finalization-provider-base-recovery-d140ac8daec5f311a278050c6e1e0b33011e28b0ee2ee9b52bb357f3b34ac923'),
    finalization_id               TEXT NOT NULL UNIQUE REFERENCES dcp_review_lab_card12_rebase_head_finalization (finalization_id) ON DELETE RESTRICT,
    finalization_generation       INTEGER NOT NULL CHECK (finalization_generation = 1),
    finalization_identity         TEXT NOT NULL CHECK (finalization_identity = 'a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b'),
    prior_status                  TEXT NOT NULL CHECK (prior_status = 'failed'),
    prior_error_code              TEXT NOT NULL CHECK (prior_error_code = 'provider_identity_drift'),
    prior_revision                INTEGER NOT NULL CHECK (prior_revision = 4),
    prior_worker_calls            INTEGER NOT NULL CHECK (prior_worker_calls = 0),
    prior_arbiter_calls           INTEGER NOT NULL CHECK (prior_arbiter_calls = 0),
    prior_action_count            INTEGER NOT NULL CHECK (prior_action_count = 1),
    prior_reviewer_calls          INTEGER NOT NULL CHECK (prior_reviewer_calls = 0),
    prior_provider_new_head       TEXT NOT NULL CHECK (prior_provider_new_head = ''),
    prior_review_run_id           TEXT NOT NULL CHECK (prior_review_run_id = ''),
    prior_merge_commit_sha        TEXT NOT NULL CHECK (prior_merge_commit_sha = ''),
    old_head                      TEXT NOT NULL CHECK (old_head = 'd4fcb68051ae113ed497d02151a759800ee85633'),
    candidate_head                TEXT NOT NULL CHECK (candidate_head = '4de6ff1a0b80223a9b32a05ba68cf0b665296081'),
    historical_provider_base      TEXT NOT NULL CHECK (historical_provider_base = 'dbaf01b05e85ffffa4c843a905e2fe5229eaf0da'),
    post_push_provider_base       TEXT NOT NULL CHECK (post_push_provider_base = 'b34b31b5443890e69128db2862726950a6bbac0d'),
    failed_source_sha             TEXT NOT NULL CHECK (failed_source_sha = '1f1e8cedf44d30773568f8801710f1371b14a47b'),
    failed_source_tree            TEXT NOT NULL CHECK (failed_source_tree = '4523bfacf690c15f75c155ccfc2f14831db7b2f2'),
    first_check_name              TEXT NOT NULL CHECK (first_check_name = 'dcp-review-lab'),
    first_check_id                TEXT NOT NULL CHECK (first_check_id = '94509683728'),
    first_check_status            TEXT NOT NULL CHECK (first_check_status = 'failed'),
    first_check_conclusion        TEXT NOT NULL CHECK (first_check_conclusion = 'failure'),
    observed_provider_mergeable   TEXT NOT NULL CHECK (observed_provider_mergeable = 'MERGEABLE'),
    observed_provider_merge_state TEXT NOT NULL CHECK (observed_provider_merge_state = 'UNSTABLE'),
    quarantine_rows               INTEGER NOT NULL CHECK (quarantine_rows = 2),
    quarantine_verifications      INTEGER NOT NULL CHECK (quarantine_verifications = 12),
    recovery_reason               TEXT NOT NULL CHECK (recovery_reason = 'post_push_provider_base_advanced_from_historical_base_to_current_main'),
    prior_updated_at              TIMESTAMP NOT NULL,
    prior_finished_at             TIMESTAMP NOT NULL,
    rearmed_at                    TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX idx_dcp_card12_one_rebase_head_finalization_provider_base_recovery
    ON dcp_card12_rebase_head_finalization_provider_base_recovery ((1));

INSERT INTO dcp_card12_rebase_head_finalization_provider_base_recovery (
    correction_id, finalization_id, finalization_generation,
    finalization_identity, prior_status, prior_error_code, prior_revision,
    prior_worker_calls, prior_arbiter_calls, prior_action_count,
    prior_reviewer_calls, prior_provider_new_head, prior_review_run_id,
    prior_merge_commit_sha, old_head, candidate_head,
    historical_provider_base, post_push_provider_base, failed_source_sha,
    failed_source_tree, first_check_name, first_check_id,
    first_check_status, first_check_conclusion, observed_provider_mergeable,
    observed_provider_merge_state, quarantine_rows,
    quarantine_verifications, recovery_reason, prior_updated_at,
    prior_finished_at, rearmed_at
)
SELECT
    'dcp-card12-rebase-head-finalization-provider-base-recovery-d140ac8daec5f311a278050c6e1e0b33011e28b0ee2ee9b52bb357f3b34ac923',
    finalization.finalization_id, finalization.generation,
    finalization.identity_digest, finalization.status,
    finalization.error_code, finalization.revision,
    finalization.worker_model_call_count,
    finalization.arbiter_model_call_count,
    finalization.model_free_action_count,
    finalization.reviewer_model_call_count,
    finalization.provider_new_head, finalization.review_run_id,
    finalization.merge_commit_sha, finalization.old_head,
    finalization.candidate_head, finalization.provider_base,
    finalization.current_main,
    '1f1e8cedf44d30773568f8801710f1371b14a47b',
    '4523bfacf690c15f75c155ccfc2f14831db7b2f2',
    checks.name, checks.details, checks.status, checks.conclusion,
    pr.provider_mergeable, pr.provider_merge_state_status,
    (SELECT count(*) FROM dcp_governed_startup_quarantine),
    (SELECT sum(verification_count) FROM dcp_governed_startup_quarantine),
    'post_push_provider_base_advanced_from_historical_base_to_current_main',
    finalization.updated_at, finalization.finished_at, CURRENT_TIMESTAMP
FROM dcp_review_lab_card12_rebase_head_finalization finalization
JOIN pr ON pr.url = finalization.pr_url AND pr.session_id = finalization.session_id
JOIN pr_checks checks ON checks.pr_url = finalization.pr_url
  AND checks.commit_hash = finalization.candidate_head
WHERE finalization.finalization_id = 'dcp-card12-rebase-head-finalization-a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b'
  AND finalization.generation = 1
  AND finalization.identity_digest = 'a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b'
  AND finalization.status = 'failed'
  AND finalization.error_code = 'provider_identity_drift'
  AND finalization.revision = 4
  AND finalization.worker_model_call_count = 0
  AND finalization.arbiter_model_call_count = 0
  AND finalization.model_free_action_count = 1
  AND finalization.reviewer_model_call_count = 0
  AND finalization.provider_new_head = ''
  AND finalization.review_run_id = ''
  AND finalization.review_id = ''
  AND finalization.review_batch_id = ''
  AND finalization.check_id = ''
  AND finalization.merge_commit_sha = ''
  AND finalization.finished_at IS NOT NULL
  AND pr.number = 9 AND pr.pr_state = 'open' AND pr.provider = 'github'
  AND pr.host = 'github.com' AND pr.repo = 'orenvlad-ai/dcp-review-lab'
  AND pr.source_branch = 'ao/dcp-review-lab-12/root'
  AND pr.target_branch = 'main' AND pr.head_sha = finalization.candidate_head
  AND pr.base_sha = finalization.current_main AND pr.author = 'orenvlad-ai'
  AND pr.is_draft = 0 AND pr.is_merged = 0 AND pr.is_closed = 0
  AND pr.provider_state = 'OPEN' AND pr.provider_mergeable = 'MERGEABLE'
  AND pr.provider_merge_state_status = 'UNSTABLE'
  AND checks.name = 'dcp-review-lab' AND checks.status = 'failed'
  AND checks.conclusion = 'failure' AND checks.details = '94509683728'
  AND (SELECT count(*) FROM dcp_governed_startup_quarantine q
       WHERE q.recovery_id = finalization.predecessor_recovery_id
         AND q.verification_count = 6
         AND ((q.session_id = 'dcp-review-lab-11' AND q.classification = 'governed_terminal')
           OR (q.session_id = 'dcp-review-lab-12' AND q.classification = 'governed_recovery'))) = 2
  AND EXISTS (
    SELECT 1 FROM dcp_card12_rebase_head_finalization_audit_recovery audit
    WHERE audit.finalization_id = finalization.finalization_id
      AND audit.correction_id = 'dcp-card12-rebase-head-finalization-audit-recovery-52490d8c01eccc8f02984ec4d863895c0215950590cfc5309d00a1525eb8f11b'
  );

CREATE TABLE dcp_card12_rebase_head_finalization_provider_base_up_guard (
    finalization_rows INTEGER NOT NULL CHECK (finalization_rows IN (0, 1)),
    correction_rows INTEGER NOT NULL CHECK (correction_rows = finalization_rows)
);
INSERT INTO dcp_card12_rebase_head_finalization_provider_base_up_guard
SELECT count(*),
       (SELECT count(*) FROM dcp_card12_rebase_head_finalization_provider_base_recovery)
FROM dcp_review_lab_card12_rebase_head_finalization;

UPDATE dcp_review_lab_card12_rebase_head_finalization
SET status = 'running', error_code = '', revision = revision + 1,
    updated_at = CURRENT_TIMESTAMP, finished_at = NULL
WHERE finalization_id = 'dcp-card12-rebase-head-finalization-a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b'
  AND status = 'failed' AND error_code = 'provider_identity_drift'
  AND revision = 4 AND worker_model_call_count = 0
  AND arbiter_model_call_count = 0 AND model_free_action_count = 1
  AND reviewer_model_call_count = 0 AND provider_new_head = ''
  AND review_run_id = '' AND merge_commit_sha = ''
  AND EXISTS (
    SELECT 1 FROM dcp_card12_rebase_head_finalization_provider_base_recovery audit
    WHERE audit.finalization_id = dcp_review_lab_card12_rebase_head_finalization.finalization_id
  );

CREATE TABLE dcp_card12_rebase_head_finalization_provider_base_rearm_guard (
    eligible_rows INTEGER NOT NULL CHECK (eligible_rows IN (0, 1)),
    correction_rows INTEGER NOT NULL CHECK (correction_rows = eligible_rows),
    running_rows INTEGER NOT NULL CHECK (running_rows = eligible_rows)
);
INSERT INTO dcp_card12_rebase_head_finalization_provider_base_rearm_guard
SELECT count(*),
       (SELECT count(*) FROM dcp_card12_rebase_head_finalization_provider_base_recovery),
       (SELECT count(*) FROM dcp_review_lab_card12_rebase_head_finalization
        WHERE status = 'running' AND revision = 5
          AND worker_model_call_count = 0 AND arbiter_model_call_count = 0
          AND model_free_action_count = 1 AND reviewer_model_call_count = 0
          AND provider_new_head = '' AND review_run_id = ''
          AND merge_commit_sha = '' AND error_code = '')
FROM dcp_review_lab_card12_rebase_head_finalization;
DROP TABLE dcp_card12_rebase_head_finalization_provider_base_rearm_guard;
DROP TABLE dcp_card12_rebase_head_finalization_provider_base_up_guard;

-- +goose StatementBegin
CREATE TRIGGER dcp_card12_rebase_head_finalization_provider_base_recovery_no_update
BEFORE UPDATE ON dcp_card12_rebase_head_finalization_provider_base_recovery
BEGIN
    SELECT RAISE(ABORT, 'card-12 REBASE_HEAD provider-base recovery is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_card12_rebase_head_finalization_provider_base_recovery_no_delete
BEFORE DELETE ON dcp_card12_rebase_head_finalization_provider_base_recovery
BEGIN
    SELECT RAISE(ABORT, 'card-12 REBASE_HEAD provider-base recovery is immutable');
END;
-- +goose StatementEnd

-- +goose Down
CREATE TABLE dcp_card12_rebase_head_finalization_provider_base_down_guard (
    finalization_rows INTEGER NOT NULL CHECK (finalization_rows IN (0, 1)),
    correction_rows INTEGER NOT NULL CHECK (correction_rows = finalization_rows),
    status TEXT NOT NULL CHECK (status IN ('', 'running')),
    revision INTEGER NOT NULL CHECK (revision IN (0, 5)),
    action_count INTEGER NOT NULL CHECK (action_count IN (0, 1)),
    reviewer_count INTEGER NOT NULL CHECK (reviewer_count = 0),
    provider_new_head TEXT NOT NULL CHECK (provider_new_head = ''),
    review_run_id TEXT NOT NULL CHECK (review_run_id = ''),
    merge_commit_sha TEXT NOT NULL CHECK (merge_commit_sha = '')
);
INSERT INTO dcp_card12_rebase_head_finalization_provider_base_down_guard
SELECT count(*),
       (SELECT count(*) FROM dcp_card12_rebase_head_finalization_provider_base_recovery),
       coalesce(max(status), ''), coalesce(max(revision), 0),
       coalesce(max(model_free_action_count), 0),
       coalesce(max(reviewer_model_call_count), 0),
       coalesce(max(provider_new_head), ''), coalesce(max(review_run_id), ''),
       coalesce(max(merge_commit_sha), '')
FROM dcp_review_lab_card12_rebase_head_finalization;

UPDATE dcp_review_lab_card12_rebase_head_finalization
SET status = 'failed', error_code = 'provider_identity_drift', revision = 4,
    updated_at = (SELECT prior_updated_at FROM dcp_card12_rebase_head_finalization_provider_base_recovery),
    finished_at = (SELECT prior_finished_at FROM dcp_card12_rebase_head_finalization_provider_base_recovery)
WHERE status = 'running' AND revision = 5
  AND worker_model_call_count = 0 AND arbiter_model_call_count = 0
  AND model_free_action_count = 1 AND reviewer_model_call_count = 0
  AND provider_new_head = '' AND review_run_id = '' AND merge_commit_sha = ''
  AND EXISTS (SELECT 1 FROM dcp_card12_rebase_head_finalization_provider_base_recovery);

DROP TRIGGER dcp_card12_rebase_head_finalization_provider_base_recovery_no_update;
DROP TRIGGER dcp_card12_rebase_head_finalization_provider_base_recovery_no_delete;
DROP INDEX idx_dcp_card12_one_rebase_head_finalization_provider_base_recovery;
DROP TABLE dcp_card12_rebase_head_finalization_provider_base_recovery;
DROP TABLE dcp_card12_rebase_head_finalization_provider_base_down_guard;
