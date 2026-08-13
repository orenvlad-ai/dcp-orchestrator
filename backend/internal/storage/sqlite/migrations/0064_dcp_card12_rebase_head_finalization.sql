-- +goose Up
-- One additive successor for the exact clean candidate retained after the
-- terminal cold-start recovery. The predecessor and its artifacts are never
-- updated or re-armed.
CREATE TABLE dcp_review_lab_card12_rebase_head_finalization (
    finalization_id                 TEXT PRIMARY KEY CHECK (finalization_id = 'dcp-card12-rebase-head-finalization-a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b'),
    generation                      INTEGER NOT NULL CHECK (generation = 1),
    identity_digest                 TEXT NOT NULL UNIQUE CHECK (identity_digest = 'a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b'),
    contract_commit                 TEXT NOT NULL CHECK (contract_commit = '9465a84ec44f72f6b7c245ebddeac22d722108ae'),
    predecessor_recovery_id         TEXT NOT NULL UNIQUE REFERENCES dcp_review_lab_card12_cold_start_recovery (recovery_id) ON DELETE RESTRICT,
    incident_id                     TEXT NOT NULL UNIQUE CHECK (incident_id = 'dcp-global-release-2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60'),
    admission_id                    TEXT NOT NULL UNIQUE CHECK (admission_id = 'dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34'),
    session_id                      TEXT NOT NULL UNIQUE CHECK (session_id = 'dcp-review-lab-12'),
    task_id                         TEXT NOT NULL CHECK (task_id = 'i13-arbiter-b'),
    project_id                      TEXT NOT NULL CHECK (project_id = 'dcp-review-lab'),
    repository                      TEXT NOT NULL CHECK (repository = 'orenvlad-ai/dcp-review-lab'),
    worktree_path                   TEXT NOT NULL CHECK (worktree_path = '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/dcp-review-lab/dcp-review-lab-12'),
    source_branch                   TEXT NOT NULL CHECK (source_branch = 'ao/dcp-review-lab-12/root'),
    pr_url                          TEXT NOT NULL CHECK (pr_url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/9'),
    pr_number                       INTEGER NOT NULL CHECK (pr_number = 9),
    old_head                        TEXT NOT NULL CHECK (old_head = 'd4fcb68051ae113ed497d02151a759800ee85633'),
    candidate_head                  TEXT NOT NULL CHECK (candidate_head = '4de6ff1a0b80223a9b32a05ba68cf0b665296081'),
    current_main                    TEXT NOT NULL CHECK (current_main = 'b34b31b5443890e69128db2862726950a6bbac0d'),
    provider_base                   TEXT NOT NULL CHECK (provider_base = 'dbaf01b05e85ffffa4c843a905e2fe5229eaf0da'),
    conflict_path                   TEXT NOT NULL CHECK (conflict_path = 'canary/i13-arbiter-conflict.txt'),
    resolved_bytes_digest           TEXT NOT NULL CHECK (resolved_bytes_digest = '2a5da25a78ff8bcd9aff4493f195eaefecbc70c3d4db8902dda468ccf69e5e46'),
    resolved_blob                   TEXT NOT NULL CHECK (resolved_blob = '80a658c4cfc3ffda5786da316bc0bd10ffb1834f'),
    candidate_diff_digest           TEXT NOT NULL CHECK (candidate_diff_digest = 'b415f3cc21e091afc82e8fbf5fa1a6f0e64ec42465ea8702efe4c681f47295f7'),
    clean_status_digest             TEXT NOT NULL CHECK (clean_status_digest = 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'),
    rebase_head_digest              TEXT NOT NULL CHECK (rebase_head_digest = '657c15026f6e8f51e96e6ff6c2ae94a5d6f4031ec95f07030b52f6226cc4d810'),
    orig_head_digest                TEXT NOT NULL CHECK (orig_head_digest = '657c15026f6e8f51e96e6ff6c2ae94a5d6f4031ec95f07030b52f6226cc4d810'),
    backup_path                     TEXT NOT NULL CHECK (backup_path = '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/evidence/dcp-card12-cold-start-recovery/dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'),
    backup_digest                   TEXT NOT NULL CHECK (backup_digest = '82d0e5834375c380069e7d48a7fdb2066371670d92733ce59545718469a4f3dd'),
    push_ref                        TEXT NOT NULL CHECK (push_ref = 'refs/heads/ao/dcp-review-lab-12/root'),
    push_lease_old_head             TEXT NOT NULL CHECK (push_lease_old_head = old_head),
    provider_new_head               TEXT NOT NULL DEFAULT '' CHECK (provider_new_head = '' OR provider_new_head = candidate_head),
    unauthorized_worker_tokens_11   INTEGER NOT NULL CHECK (unauthorized_worker_tokens_11 = 33238),
    unauthorized_worker_tokens_12   INTEGER NOT NULL CHECK (unauthorized_worker_tokens_12 = 33573),
    worker_model_call_count          INTEGER NOT NULL DEFAULT 0 CHECK (worker_model_call_count = 0),
    arbiter_model_call_count         INTEGER NOT NULL DEFAULT 0 CHECK (arbiter_model_call_count = 0),
    model_free_action_count          INTEGER NOT NULL DEFAULT 0 CHECK (model_free_action_count IN (0, 1)),
    reviewer_model_call_count        INTEGER NOT NULL DEFAULT 0 CHECK (reviewer_model_call_count IN (0, 1)),
    review_run_id                    TEXT NOT NULL DEFAULT '',
    review_id                        TEXT NOT NULL DEFAULT '',
    review_batch_id                  TEXT NOT NULL DEFAULT '',
    check_id                         TEXT NOT NULL DEFAULT '',
    merge_commit_sha                 TEXT NOT NULL DEFAULT '' CHECK (merge_commit_sha = '' OR length(merge_commit_sha) = 40),
    status                           TEXT NOT NULL CHECK (status IN ('authorized', 'running', 'candidate_ready', 'review_running', 'recovery_reviewed', 'succeeded', 'failed')),
    revision                         INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    error_code                       TEXT NOT NULL DEFAULT '',
    authorized_at                    TIMESTAMP NOT NULL,
    updated_at                       TIMESTAMP NOT NULL CHECK (updated_at >= authorized_at),
    finished_at                      TIMESTAMP,
    CHECK (status IN ('authorized', 'failed') OR model_free_action_count = 1),
    CHECK (status NOT IN ('candidate_ready', 'review_running', 'recovery_reviewed', 'succeeded') OR provider_new_head = candidate_head),
    CHECK (status NOT IN ('review_running', 'recovery_reviewed', 'succeeded') OR (reviewer_model_call_count = 1 AND review_run_id <> '')),
    CHECK ((status IN ('succeeded', 'failed')) = (finished_at IS NOT NULL))
);

CREATE UNIQUE INDEX idx_dcp_card12_one_rebase_head_finalization
    ON dcp_review_lab_card12_rebase_head_finalization ((1));

INSERT INTO dcp_review_lab_card12_rebase_head_finalization (
    finalization_id, generation, identity_digest, contract_commit,
    predecessor_recovery_id, incident_id, admission_id, session_id, task_id,
    project_id, repository, worktree_path, source_branch, pr_url, pr_number,
    old_head, candidate_head, current_main, provider_base, conflict_path,
    resolved_bytes_digest, resolved_blob, candidate_diff_digest,
    clean_status_digest, rebase_head_digest, orig_head_digest, backup_path,
    backup_digest, push_ref, push_lease_old_head,
    unauthorized_worker_tokens_11, unauthorized_worker_tokens_12,
    status, authorized_at, updated_at
)
SELECT
    'dcp-card12-rebase-head-finalization-a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b',
    1, 'a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b',
    '9465a84ec44f72f6b7c245ebddeac22d722108ae', recovery.recovery_id,
    recovery.incident_id, recovery.admission_id, recovery.session_id,
    recovery.task_id, recovery.project_id, recovery.repository,
    recovery.worktree_path, recovery.source_branch, recovery.pr_url,
    recovery.pr_number, recovery.old_head,
    '4de6ff1a0b80223a9b32a05ba68cf0b665296081', recovery.current_main,
    recovery.provider_base, recovery.conflict_path, recovery.resolved_bytes_digest,
    recovery.resolved_blob,
    'b415f3cc21e091afc82e8fbf5fa1a6f0e64ec42465ea8702efe4c681f47295f7',
    'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
    '657c15026f6e8f51e96e6ff6c2ae94a5d6f4031ec95f07030b52f6226cc4d810',
    '657c15026f6e8f51e96e6ff6c2ae94a5d6f4031ec95f07030b52f6226cc4d810',
    recovery.backup_path, recovery.backup_digest, recovery.push_ref,
    recovery.push_lease_old_head, recovery.unauthorized_worker_tokens_11,
    recovery.unauthorized_worker_tokens_12, 'authorized', CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
FROM dcp_review_lab_card12_cold_start_recovery recovery
WHERE recovery.recovery_id = 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
  AND recovery.generation = 1
  AND recovery.identity_digest = '087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
  AND recovery.contract_commit = '623c3896a50d410e5b305ed08cf29abdc40b5b23'
  AND recovery.status = 'failed' AND recovery.error_code = 'model_free_action_failed'
  AND recovery.revision = 7 AND recovery.worker_model_call_count = 0
  AND recovery.arbiter_model_call_count = 0 AND recovery.model_free_action_count = 1
  AND recovery.reviewer_model_call_count = 0
  AND recovery.backup_path = '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/evidence/dcp-card12-cold-start-recovery/dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
  AND recovery.backup_digest = '82d0e5834375c380069e7d48a7fdb2066371670d92733ce59545718469a4f3dd'
  AND recovery.local_ref_before = recovery.old_head
  AND recovery.local_ref_after = '' AND recovery.new_head = ''
  AND recovery.new_commit = '' AND recovery.provider_new_head = ''
  AND recovery.recovery_review_run_id = '' AND recovery.merge_commit_sha = ''
  AND recovery.finished_at IS NOT NULL
  AND (SELECT count(*) FROM dcp_card12_cold_start_tool_path_recovery tool
       WHERE tool.recovery_id = recovery.recovery_id) = 1
  AND (SELECT count(*) FROM dcp_card12_cold_start_auto_merge_recovery auto
       WHERE auto.recovery_id = recovery.recovery_id) = 1
  AND (SELECT count(*) FROM dcp_governed_startup_quarantine q
       WHERE q.recovery_id = recovery.recovery_id
         AND q.contract_commit = recovery.contract_commit
         AND q.verification_count = 4
         AND ((q.session_id = 'dcp-review-lab-11' AND q.classification = 'governed_terminal')
           OR (q.session_id = 'dcp-review-lab-12' AND q.classification = 'governed_recovery'))) = 2;

-- Exact live eligibility and the inserted successor must agree; ordinary and
-- already-burned histories remain compatible with zero rows.
CREATE TABLE dcp_card12_rebase_head_finalization_up_guard (
    eligible_rows INTEGER NOT NULL CHECK (eligible_rows IN (0, 1)),
    finalization_rows INTEGER NOT NULL CHECK (finalization_rows = eligible_rows)
);
INSERT INTO dcp_card12_rebase_head_finalization_up_guard
SELECT count(*),
       (SELECT count(*) FROM dcp_review_lab_card12_rebase_head_finalization)
FROM dcp_review_lab_card12_cold_start_recovery recovery
WHERE recovery.status = 'failed' AND recovery.error_code = 'model_free_action_failed'
  AND recovery.revision = 7 AND recovery.model_free_action_count = 1
  AND recovery.reviewer_model_call_count = 0
  AND recovery.backup_digest = '82d0e5834375c380069e7d48a7fdb2066371670d92733ce59545718469a4f3dd'
  AND (SELECT count(*) FROM dcp_governed_startup_quarantine q
       WHERE q.recovery_id = recovery.recovery_id AND q.verification_count = 4) = 2;
DROP TABLE dcp_card12_rebase_head_finalization_up_guard;

-- +goose Down
CREATE TABLE dcp_card12_rebase_head_finalization_down_guard (
    row_count INTEGER NOT NULL CHECK (row_count IN (0, 1)),
    status TEXT NOT NULL CHECK (status IN ('', 'authorized')),
    revision INTEGER NOT NULL CHECK (revision = 0),
    action_count INTEGER NOT NULL CHECK (action_count = 0),
    reviewer_count INTEGER NOT NULL CHECK (reviewer_count = 0),
    provider_new_head TEXT NOT NULL CHECK (provider_new_head = ''),
    review_run_id TEXT NOT NULL CHECK (review_run_id = ''),
    merge_commit_sha TEXT NOT NULL CHECK (merge_commit_sha = '')
);
INSERT INTO dcp_card12_rebase_head_finalization_down_guard
SELECT count(*), coalesce(max(status), ''), coalesce(max(revision), 0),
       coalesce(max(model_free_action_count), 0),
       coalesce(max(reviewer_model_call_count), 0),
       coalesce(max(provider_new_head), ''), coalesce(max(review_run_id), ''),
       coalesce(max(merge_commit_sha), '')
FROM dcp_review_lab_card12_rebase_head_finalization;
DROP INDEX idx_dcp_card12_one_rebase_head_finalization;
DROP TABLE dcp_review_lab_card12_rebase_head_finalization;
DROP TABLE dcp_card12_rebase_head_finalization_down_guard;
