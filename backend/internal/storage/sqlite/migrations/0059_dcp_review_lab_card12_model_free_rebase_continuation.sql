-- +goose Up
-- One exact, model-free continuation of the byte-preserved card-12 rebase.
-- The predecessor fresh-worker row remains immutable and terminal. This row
-- is not a worker retry or a general Git-operation/reviewer table.
CREATE TABLE dcp_review_lab_card12_model_free_rebase_continuation (
    continuation_id               TEXT PRIMARY KEY CHECK (continuation_id = 'dcp-card12-model-free-rebase-continuation-66eb630c1995f90b37429a2f6c57c57794dda9fc98a29149c88bdb2f01131060'),
    generation                    INTEGER NOT NULL CHECK (generation = 1),
    identity_digest               TEXT NOT NULL UNIQUE CHECK (identity_digest = '66eb630c1995f90b37429a2f6c57c57794dda9fc98a29149c88bdb2f01131060'),
    contract_commit               TEXT NOT NULL CHECK (contract_commit = 'e17fa9080434b5642667392fb06db61cf35f19bd'),
    predecessor_recovery_id       TEXT NOT NULL UNIQUE REFERENCES dcp_review_lab_card12_fresh_worker_recovery (recovery_id) ON DELETE RESTRICT,
    incident_id                   TEXT NOT NULL UNIQUE CHECK (incident_id = 'dcp-global-release-2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60'),
    admission_id                  TEXT NOT NULL UNIQUE CHECK (admission_id = 'dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34'),
    session_id                    TEXT NOT NULL UNIQUE CHECK (session_id = 'dcp-review-lab-12'),
    task_id                       TEXT NOT NULL CHECK (task_id = 'i13-arbiter-b'),
    project_id                    TEXT NOT NULL CHECK (project_id = 'dcp-review-lab'),
    repository                    TEXT NOT NULL CHECK (repository = 'orenvlad-ai/dcp-review-lab'),
    worktree_path                 TEXT NOT NULL CHECK (worktree_path = '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/dcp-review-lab/dcp-review-lab-12'),
    source_branch                 TEXT NOT NULL CHECK (source_branch = 'ao/dcp-review-lab-12/root'),
    pr_url                        TEXT NOT NULL CHECK (pr_url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/9'),
    pr_number                     INTEGER NOT NULL CHECK (pr_number = 9),
    old_head                      TEXT NOT NULL CHECK (old_head = 'd4fcb68051ae113ed497d02151a759800ee85633'),
    current_main                  TEXT NOT NULL CHECK (current_main = 'b34b31b5443890e69128db2862726950a6bbac0d'),
    predecessor_status            TEXT NOT NULL CHECK (predecessor_status = 'failed'),
    predecessor_error             TEXT NOT NULL CHECK (predecessor_error = 'worker_process_failed'),
    predecessor_revision          INTEGER NOT NULL CHECK (predecessor_revision = 5),
    predecessor_worker_calls      INTEGER NOT NULL CHECK (predecessor_worker_calls = 1),
    predecessor_reviewer_calls    INTEGER NOT NULL CHECK (predecessor_reviewer_calls = 0),
    predecessor_input_digest      TEXT NOT NULL CHECK (predecessor_input_digest = '1b79923f68e0a53414579f059a1984fbcdae7aea4593d86c7fa4ae62027114bd'),
    input_artifact_digest         TEXT NOT NULL CHECK (input_artifact_digest = '131ab471a0509f4851f94e056998b3a620468a69bdd3b19435d2a225da01d393'),
    result_artifact_digest        TEXT NOT NULL CHECK (result_artifact_digest = 'e284aeb37d6fdd7ec86ee3ea6ad2272eee7d4856d5a39eb2894c89dd83d0836b'),
    log_artifact_digest           TEXT NOT NULL CHECK (log_artifact_digest = '8909c2cb81e96beb47414576fb6e1c54e9895fcf34e38e2865d87ca821b46a20'),
    rebase_metadata_digest        TEXT NOT NULL CHECK (rebase_metadata_digest = 'db9933afbc18ffbd031818990e2b350845c766a5f0ae8ed37fae8f4e8a66f371'),
    resolved_bytes_digest         TEXT NOT NULL CHECK (resolved_bytes_digest = '2a5da25a78ff8bcd9aff4493f195eaefecbc70c3d4db8902dda468ccf69e5e46'),
    worker_model_call_count       INTEGER NOT NULL DEFAULT 0 CHECK (worker_model_call_count = 0),
    arbiter_model_call_count      INTEGER NOT NULL DEFAULT 0 CHECK (arbiter_model_call_count = 0),
    model_free_action_count       INTEGER NOT NULL DEFAULT 0 CHECK (model_free_action_count IN (0, 1)),
    reviewer_model_call_count     INTEGER NOT NULL DEFAULT 0 CHECK (reviewer_model_call_count IN (0, 1)),
    local_ref_before              TEXT NOT NULL DEFAULT '',
    local_ref_after               TEXT NOT NULL DEFAULT '' CHECK (local_ref_after = '' OR length(local_ref_after) = 40),
    push_ref                      TEXT NOT NULL CHECK (push_ref = 'refs/heads/ao/dcp-review-lab-12/root'),
    push_lease_old_head           TEXT NOT NULL CHECK (push_lease_old_head = old_head),
    new_head                      TEXT NOT NULL DEFAULT '' CHECK (new_head = '' OR length(new_head) = 40),
    new_commit                    TEXT NOT NULL DEFAULT '' CHECK (new_commit = '' OR length(new_commit) = 40),
    provider_new_head             TEXT NOT NULL DEFAULT '' CHECK (provider_new_head = '' OR length(provider_new_head) = 40),
    recovery_review_run_id        TEXT NOT NULL DEFAULT '',
    recovery_review_id            TEXT NOT NULL DEFAULT '',
    recovery_review_batch_id      TEXT NOT NULL DEFAULT '',
    recovery_check_id             TEXT NOT NULL DEFAULT '',
    merge_commit_sha              TEXT NOT NULL DEFAULT '' CHECK (merge_commit_sha = '' OR length(merge_commit_sha) = 40),
    status                        TEXT NOT NULL CHECK (status IN ('authorized', 'running', 'candidate_ready', 'review_running', 'recovery_reviewed', 'succeeded', 'failed')),
    revision                      INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    error_code                    TEXT NOT NULL DEFAULT '',
    authorized_at                 TIMESTAMP NOT NULL,
    updated_at                    TIMESTAMP NOT NULL CHECK (updated_at >= authorized_at),
    finished_at                   TIMESTAMP,
    CHECK ((model_free_action_count = 0 AND local_ref_before = '' AND new_head = '') OR model_free_action_count = 1),
    CHECK (status IN ('authorized', 'failed') OR model_free_action_count = 1),
    CHECK (status NOT IN ('candidate_ready', 'review_running', 'recovery_reviewed', 'succeeded') OR (new_head <> '' AND new_commit = new_head AND provider_new_head = new_head AND local_ref_after = new_head)),
    CHECK (status NOT IN ('review_running', 'recovery_reviewed', 'succeeded') OR (reviewer_model_call_count = 1 AND recovery_review_run_id <> '')),
    CHECK ((status IN ('succeeded', 'failed')) = (finished_at IS NOT NULL))
);

CREATE UNIQUE INDEX idx_dcp_card12_one_model_free_rebase_continuation
    ON dcp_review_lab_card12_model_free_rebase_continuation ((1));

INSERT INTO dcp_review_lab_card12_model_free_rebase_continuation (
    continuation_id, generation, identity_digest, contract_commit,
    predecessor_recovery_id, incident_id, admission_id, session_id, task_id,
    project_id, repository, worktree_path, source_branch, pr_url, pr_number,
    old_head, current_main, predecessor_status, predecessor_error,
    predecessor_revision, predecessor_worker_calls, predecessor_reviewer_calls,
    predecessor_input_digest, input_artifact_digest, result_artifact_digest,
    log_artifact_digest, rebase_metadata_digest, resolved_bytes_digest,
    push_ref, push_lease_old_head, status, authorized_at, updated_at
)
SELECT
    'dcp-card12-model-free-rebase-continuation-66eb630c1995f90b37429a2f6c57c57794dda9fc98a29149c88bdb2f01131060',
    1, '66eb630c1995f90b37429a2f6c57c57794dda9fc98a29149c88bdb2f01131060',
    'e17fa9080434b5642667392fb06db61cf35f19bd', worker.recovery_id,
    worker.incident_id, worker.admission_id, worker.session_id, worker.task_id,
    worker.project_id, worker.repository, worker.worktree_path,
    worker.source_branch, worker.pr_url, worker.pr_number, worker.old_head,
    worker.current_main, worker.status, worker.error_code, worker.revision,
    worker.worker_model_call_count, worker.reviewer_model_call_count,
    worker.input_digest,
    '131ab471a0509f4851f94e056998b3a620468a69bdd3b19435d2a225da01d393',
    'e284aeb37d6fdd7ec86ee3ea6ad2272eee7d4856d5a39eb2894c89dd83d0836b',
    '8909c2cb81e96beb47414576fb6e1c54e9895fcf34e38e2865d87ca821b46a20',
    'db9933afbc18ffbd031818990e2b350845c766a5f0ae8ed37fae8f4e8a66f371',
    '2a5da25a78ff8bcd9aff4493f195eaefecbc70c3d4db8902dda468ccf69e5e46',
    'refs/heads/ao/dcp-review-lab-12/root', worker.old_head, 'authorized',
    CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM dcp_review_lab_card12_fresh_worker_recovery worker
JOIN dcp_card12_fresh_worker_preflight_recovery preflight
  ON preflight.recovery_id = worker.recovery_id
WHERE worker.recovery_id = 'dcp-card12-fresh-worker-recovery-d2b7142bc9e5844ba165abe24d3222b3e1a94c3577fba5f6f8d97ec3dbad151b'
  AND worker.status = 'failed' AND worker.error_code = 'worker_process_failed'
  AND worker.revision = 5 AND worker.worker_model_call_count = 1
  AND worker.reviewer_model_call_count = 0
  AND worker.input_digest = '1b79923f68e0a53414579f059a1984fbcdae7aea4593d86c7fa4ae62027114bd'
  AND worker.launch_id = worker.recovery_id AND worker.runtime_action_id = worker.recovery_id
  AND worker.worker_codex_session_id = '' AND worker.worker_token_count = 0
  AND worker.worker_result_digest = '' AND worker.worker_log_digest = ''
  AND worker.new_head = '' AND worker.new_commit = ''
  AND worker.recovery_review_run_id = '' AND worker.merge_commit_sha = ''
  AND worker.finished_at IS NOT NULL
  AND preflight.failed_source_sha = 'fbcf4929f9192f7cce9c5097b0bc6a449d28e663'
  AND preflight.prior_worker_calls = 0 AND preflight.prior_reviewer_calls = 0
  AND (SELECT count(*) FROM dcp_review_lab_card12_fresh_worker_recovery) = 1
  AND (SELECT count(*) FROM dcp_card12_fresh_worker_preflight_recovery) = 1;

-- +goose Down
CREATE TABLE dcp_card12_model_free_rebase_rollback_guard (
    eligible_rows INTEGER NOT NULL CHECK (eligible_rows IN (0, 1)),
    action_count INTEGER NOT NULL CHECK (action_count = 0),
    reviewer_calls INTEGER NOT NULL CHECK (reviewer_calls = 0),
    status TEXT NOT NULL CHECK (status IN ('', 'authorized')),
    new_head TEXT NOT NULL CHECK (new_head = '')
);
INSERT INTO dcp_card12_model_free_rebase_rollback_guard
SELECT count(*), coalesce(max(model_free_action_count), 0),
       coalesce(max(reviewer_model_call_count), 0), coalesce(max(status), ''),
       coalesce(max(new_head), '')
FROM dcp_review_lab_card12_model_free_rebase_continuation;
DROP INDEX idx_dcp_card12_one_model_free_rebase_continuation;
DROP TABLE dcp_review_lab_card12_model_free_rebase_continuation;
DROP TABLE dcp_card12_model_free_rebase_rollback_guard;
