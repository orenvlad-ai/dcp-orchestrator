-- +goose Up
-- Durable boot quarantine for the two governed I13 sessions. The daemon must
-- establish and validate these rows before constructing any runtime/session
-- restoration component. They are classifications, not a scheduler.
CREATE TABLE dcp_governed_startup_quarantine (
    session_id          TEXT PRIMARY KEY CHECK (session_id IN ('dcp-review-lab-11', 'dcp-review-lab-12')),
    recovery_id         TEXT NOT NULL CHECK (recovery_id = 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'),
    classification      TEXT NOT NULL CHECK (
        (session_id = 'dcp-review-lab-11' AND classification = 'governed_terminal') OR
        (session_id = 'dcp-review-lab-12' AND classification = 'governed_recovery')
    ),
    contract_commit     TEXT NOT NULL CHECK (contract_commit = '623c3896a50d410e5b305ed08cf29abdc40b5b23'),
    verification_count INTEGER NOT NULL DEFAULT 0 CHECK (verification_count >= 0),
    established_at      TIMESTAMP NOT NULL,
    last_verified_at    TIMESTAMP NOT NULL CHECK (last_verified_at >= established_at)
);

-- A new row, not a re-arm or mutation of the terminal identity_drift row.
CREATE TABLE dcp_review_lab_card12_cold_start_recovery (
    recovery_id                    TEXT PRIMARY KEY CHECK (recovery_id = 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'),
    generation                     INTEGER NOT NULL CHECK (generation = 1),
    identity_digest                TEXT NOT NULL UNIQUE CHECK (identity_digest = '087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'),
    contract_commit                TEXT NOT NULL CHECK (contract_commit = '623c3896a50d410e5b305ed08cf29abdc40b5b23'),
    predecessor_continuation_id    TEXT NOT NULL UNIQUE REFERENCES dcp_review_lab_card12_model_free_rebase_continuation (continuation_id) ON DELETE RESTRICT,
    incident_id                    TEXT NOT NULL UNIQUE CHECK (incident_id = 'dcp-global-release-2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60'),
    admission_id                   TEXT NOT NULL UNIQUE CHECK (admission_id = 'dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34'),
    session_id                     TEXT NOT NULL UNIQUE CHECK (session_id = 'dcp-review-lab-12'),
    task_id                        TEXT NOT NULL CHECK (task_id = 'i13-arbiter-b'),
    project_id                     TEXT NOT NULL CHECK (project_id = 'dcp-review-lab'),
    repository                     TEXT NOT NULL CHECK (repository = 'orenvlad-ai/dcp-review-lab'),
    worktree_path                  TEXT NOT NULL CHECK (worktree_path = '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/dcp-review-lab/dcp-review-lab-12'),
    source_branch                  TEXT NOT NULL CHECK (source_branch = 'ao/dcp-review-lab-12/root'),
    pr_url                         TEXT NOT NULL CHECK (pr_url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/9'),
    pr_number                      INTEGER NOT NULL CHECK (pr_number = 9),
    old_head                       TEXT NOT NULL CHECK (old_head = 'd4fcb68051ae113ed497d02151a759800ee85633'),
    current_main                   TEXT NOT NULL CHECK (current_main = 'b34b31b5443890e69128db2862726950a6bbac0d'),
    provider_base                  TEXT NOT NULL CHECK (provider_base = 'dbaf01b05e85ffffa4c843a905e2fe5229eaf0da'),
    conflict_path                  TEXT NOT NULL CHECK (conflict_path = 'canary/i13-arbiter-conflict.txt'),
    marker_digest                  TEXT NOT NULL CHECK (marker_digest = '5850bba009db75bf47ff88aef2d2cecbdba89c68967f51a8cdb60f48e968dc1a'),
    status_digest                  TEXT NOT NULL CHECK (status_digest = 'fd7d8ff8f4918e9960e5e46e01c70a877d4218b3fa1e884ecc1723935b1c9886'),
    stage1_blob                    TEXT NOT NULL CHECK (stage1_blob = 'ed237ce2dd2684371797e22634480ffb28dc9e77'),
    stage2_blob                    TEXT NOT NULL CHECK (stage2_blob = 'a4c945ba7328504f2efea44f076a1407c6aa7b47'),
    stage3_blob                    TEXT NOT NULL CHECK (stage3_blob = '80a658c4cfc3ffda5786da316bc0bd10ffb1834f'),
    resolved_bytes_digest          TEXT NOT NULL CHECK (resolved_bytes_digest = '2a5da25a78ff8bcd9aff4493f195eaefecbc70c3d4db8902dda468ccf69e5e46'),
    resolved_blob                  TEXT NOT NULL CHECK (resolved_blob = '80a658c4cfc3ffda5786da316bc0bd10ffb1834f'),
    push_ref                       TEXT NOT NULL CHECK (push_ref = 'refs/heads/ao/dcp-review-lab-12/root'),
    push_lease_old_head            TEXT NOT NULL CHECK (push_lease_old_head = old_head),
    unauthorized_worker_thread_11  TEXT NOT NULL CHECK (unauthorized_worker_thread_11 = '019ff9f3-cad3-73c1-bcee-293efe857349'),
    unauthorized_worker_thread_12  TEXT NOT NULL CHECK (unauthorized_worker_thread_12 = '019ff9f3-cbe6-71e2-8636-ea6259a7e7d1'),
    unauthorized_worker_tokens_11  INTEGER NOT NULL CHECK (unauthorized_worker_tokens_11 = 33238),
    unauthorized_worker_tokens_12  INTEGER NOT NULL CHECK (unauthorized_worker_tokens_12 = 33573),
    worker_model_call_count         INTEGER NOT NULL DEFAULT 0 CHECK (worker_model_call_count = 0),
    arbiter_model_call_count        INTEGER NOT NULL DEFAULT 0 CHECK (arbiter_model_call_count = 0),
    model_free_action_count         INTEGER NOT NULL DEFAULT 0 CHECK (model_free_action_count IN (0, 1)),
    reviewer_model_call_count       INTEGER NOT NULL DEFAULT 0 CHECK (reviewer_model_call_count IN (0, 1)),
    backup_path                     TEXT NOT NULL DEFAULT '' CHECK (backup_path = '' OR backup_path = '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/evidence/dcp-card12-cold-start-recovery/dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'),
    backup_digest                   TEXT NOT NULL DEFAULT '' CHECK (backup_digest = '' OR length(backup_digest) = 64),
    local_ref_before                TEXT NOT NULL DEFAULT '',
    local_ref_after                 TEXT NOT NULL DEFAULT '' CHECK (local_ref_after = '' OR length(local_ref_after) = 40),
    new_head                        TEXT NOT NULL DEFAULT '' CHECK (new_head = '' OR length(new_head) = 40),
    new_commit                      TEXT NOT NULL DEFAULT '' CHECK (new_commit = '' OR length(new_commit) = 40),
    provider_new_head               TEXT NOT NULL DEFAULT '' CHECK (provider_new_head = '' OR length(provider_new_head) = 40),
    recovery_review_run_id          TEXT NOT NULL DEFAULT '',
    recovery_review_id              TEXT NOT NULL DEFAULT '',
    recovery_review_batch_id        TEXT NOT NULL DEFAULT '',
    recovery_check_id               TEXT NOT NULL DEFAULT '',
    merge_commit_sha                TEXT NOT NULL DEFAULT '' CHECK (merge_commit_sha = '' OR length(merge_commit_sha) = 40),
    status                          TEXT NOT NULL CHECK (status IN ('authorized', 'backed_up', 'running', 'candidate_ready', 'review_running', 'recovery_reviewed', 'succeeded', 'failed')),
    revision                        INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    error_code                      TEXT NOT NULL DEFAULT '',
    authorized_at                   TIMESTAMP NOT NULL,
    updated_at                      TIMESTAMP NOT NULL CHECK (updated_at >= authorized_at),
    finished_at                     TIMESTAMP,
    CHECK ((backup_path = '') = (backup_digest = '')),
    CHECK (status IN ('authorized', 'failed') OR (backup_path <> '' AND backup_digest <> '')),
    CHECK (status IN ('authorized', 'backed_up', 'failed') OR model_free_action_count = 1),
    CHECK (status NOT IN ('candidate_ready', 'review_running', 'recovery_reviewed', 'succeeded') OR (new_head <> '' AND new_commit = new_head AND provider_new_head = new_head AND local_ref_after = new_head)),
    CHECK (status NOT IN ('review_running', 'recovery_reviewed', 'succeeded') OR (reviewer_model_call_count = 1 AND recovery_review_run_id <> '')),
    CHECK ((status IN ('succeeded', 'failed')) = (finished_at IS NOT NULL))
);

CREATE UNIQUE INDEX idx_dcp_card12_one_cold_start_recovery
    ON dcp_review_lab_card12_cold_start_recovery ((1));

-- Exact live-state bootstrap is intentionally performed by the daemon in the
-- same transaction that establishes the pre-restoration fence. This migration
-- must also remain compatible with ordinary/burned histories that do not have
-- the bounded I13 predecessor tables.

-- +goose Down
CREATE TABLE dcp_card12_cold_start_recovery_rollback_guard (
    recovery_rows INTEGER NOT NULL CHECK (recovery_rows IN (0, 1)),
    quarantine_rows INTEGER NOT NULL CHECK (quarantine_rows IN (0, 2)),
    verification_count INTEGER NOT NULL CHECK (verification_count = 0),
    action_count INTEGER NOT NULL CHECK (action_count = 0),
    reviewer_calls INTEGER NOT NULL CHECK (reviewer_calls = 0),
    backup_path TEXT NOT NULL CHECK (backup_path = ''),
    status TEXT NOT NULL CHECK (status IN ('', 'authorized'))
);
INSERT INTO dcp_card12_cold_start_recovery_rollback_guard
SELECT (SELECT count(*) FROM dcp_review_lab_card12_cold_start_recovery),
       (SELECT count(*) FROM dcp_governed_startup_quarantine),
       coalesce((SELECT max(verification_count) FROM dcp_governed_startup_quarantine), 0),
       coalesce((SELECT max(model_free_action_count) FROM dcp_review_lab_card12_cold_start_recovery), 0),
       coalesce((SELECT max(reviewer_model_call_count) FROM dcp_review_lab_card12_cold_start_recovery), 0),
       coalesce((SELECT max(backup_path) FROM dcp_review_lab_card12_cold_start_recovery), ''),
       coalesce((SELECT max(status) FROM dcp_review_lab_card12_cold_start_recovery), '');
DROP INDEX idx_dcp_card12_one_cold_start_recovery;
DROP TABLE dcp_governed_startup_quarantine;
DROP TABLE dcp_review_lab_card12_cold_start_recovery;
DROP TABLE dcp_card12_cold_start_recovery_rollback_guard;
