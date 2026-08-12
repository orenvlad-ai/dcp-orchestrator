-- +goose Up
-- One owner-authorized fresh stateless worker for the already-failed card-12
-- native resume. This row is subordinate to the immutable generation-2
-- successor; it is not a general worker retry/session table.
CREATE TABLE dcp_review_lab_card12_fresh_worker_recovery (
    recovery_id                    TEXT PRIMARY KEY CHECK (recovery_id = 'dcp-card12-fresh-worker-recovery-d2b7142bc9e5844ba165abe24d3222b3e1a94c3577fba5f6f8d97ec3dbad151b'),
    recovery_generation            INTEGER NOT NULL CHECK (recovery_generation = 1),
    recovery_identity_digest       TEXT NOT NULL UNIQUE CHECK (recovery_identity_digest = 'd2b7142bc9e5844ba165abe24d3222b3e1a94c3577fba5f6f8d97ec3dbad151b'),
    incident_id                    TEXT NOT NULL UNIQUE CHECK (incident_id = 'dcp-global-release-2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60'),
    incident_generation            INTEGER NOT NULL CHECK (incident_generation = 1),
    successor_attempt_id           TEXT NOT NULL UNIQUE CHECK (successor_attempt_id = 'dcp-arbiter-successor-3c62ea80b56ef94165519d4f01e4c449c320bff22d16b902dd68d4a1a355ea7d'),
    successor_attempt_generation   INTEGER NOT NULL CHECK (successor_attempt_generation = 2),
    successor_identity_digest      TEXT NOT NULL CHECK (successor_identity_digest = '3c62ea80b56ef94165519d4f01e4c449c320bff22d16b902dd68d4a1a355ea7d'),
    accepted_decision_digest       TEXT NOT NULL CHECK (accepted_decision_digest = '237472879b22a8db65c5a3a0715510dc17aee1de93c45eaab45dde538cefb939'),
    admission_id                   TEXT NOT NULL UNIQUE CHECK (admission_id = 'dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34'),
    session_id                     TEXT NOT NULL UNIQUE CHECK (session_id = 'dcp-review-lab-12'),
    task_id                        TEXT NOT NULL CHECK (task_id = 'i13-arbiter-b'),
    project_id                     TEXT NOT NULL CHECK (project_id = 'dcp-review-lab'),
    repository                     TEXT NOT NULL CHECK (repository = 'orenvlad-ai/dcp-review-lab'),
    worktree_path                  TEXT NOT NULL,
    source_branch                  TEXT NOT NULL CHECK (source_branch = 'ao/dcp-review-lab-12/root'),
    pr_url                         TEXT NOT NULL CHECK (pr_url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/9'),
    pr_number                      INTEGER NOT NULL CHECK (pr_number = 9),
    old_head                       TEXT NOT NULL CHECK (old_head = 'd4fcb68051ae113ed497d02151a759800ee85633'),
    current_main                   TEXT NOT NULL CHECK (current_main = 'b34b31b5443890e69128db2862726950a6bbac0d'),
    predecessor_status             TEXT NOT NULL CHECK (predecessor_status = 'failed'),
    predecessor_error              TEXT NOT NULL CHECK (predecessor_error = 'repair_launch_failed'),
    old_runtime_handle_id          TEXT NOT NULL CHECK (old_runtime_handle_id = 'dcp-review-lab-12'),
    old_agent_session_id           TEXT NOT NULL CHECK (old_agent_session_id = ''),
    old_runtime_launch_id          TEXT NOT NULL CHECK (old_runtime_launch_id = ''),
    contract_commit                TEXT NOT NULL CHECK (contract_commit = '2a174899ae72bf1db548c3b2f172d963488191f1'),
    model                          TEXT NOT NULL CHECK (model = 'gpt-5.6-sol'),
    reasoning                      TEXT NOT NULL CHECK (reasoning = 'xhigh'),
    token_budget                   INTEGER NOT NULL CHECK (token_budget = 16384),
    worker_model_call_count        INTEGER NOT NULL DEFAULT 0 CHECK (worker_model_call_count IN (0, 1)),
    reviewer_model_call_count      INTEGER NOT NULL DEFAULT 0 CHECK (reviewer_model_call_count IN (0, 1)),
    runtime_action_id              TEXT NOT NULL CHECK (runtime_action_id = recovery_id),
    runtime_handle_id              TEXT NOT NULL CHECK (runtime_handle_id = 'dcp-card12-fresh-worker-recovery'),
    launch_id                      TEXT NOT NULL DEFAULT '',
    input_json                     TEXT NOT NULL DEFAULT '' CHECK (input_json = '' OR (length(CAST(input_json AS BLOB)) <= 8192 AND json_valid(input_json) AND json_extract(input_json, '$.schemaVersion') = 'dcp.review-lab.card12-fresh-worker-input/v1')),
    input_digest                   TEXT NOT NULL DEFAULT '' CHECK (input_digest = '' OR length(input_digest) = 64),
    input_path                     TEXT NOT NULL DEFAULT '',
    result_path                    TEXT NOT NULL DEFAULT '',
    log_path                       TEXT NOT NULL DEFAULT '',
    worker_codex_session_id        TEXT NOT NULL DEFAULT '',
    worker_token_count             INTEGER NOT NULL DEFAULT 0 CHECK (worker_token_count >= 0 AND worker_token_count <= token_budget),
    worker_result_digest           TEXT NOT NULL DEFAULT '' CHECK (worker_result_digest = '' OR length(worker_result_digest) = 64),
    worker_log_digest              TEXT NOT NULL DEFAULT '' CHECK (worker_log_digest = '' OR length(worker_log_digest) = 64),
    new_head                       TEXT NOT NULL DEFAULT '' CHECK (new_head = '' OR length(new_head) = 40),
    new_commit                     TEXT NOT NULL DEFAULT '' CHECK (new_commit = '' OR length(new_commit) = 40),
    recovery_review_run_id         TEXT NOT NULL DEFAULT '',
    recovery_review_id             TEXT NOT NULL DEFAULT '',
    recovery_review_batch_id       TEXT NOT NULL DEFAULT '',
    recovery_check_id              TEXT NOT NULL DEFAULT '',
    merge_commit_sha               TEXT NOT NULL DEFAULT '' CHECK (merge_commit_sha = '' OR length(merge_commit_sha) = 40),
    status                         TEXT NOT NULL CHECK (status IN ('authorized', 'requested', 'preflight_failed', 'running', 'worker_succeeded', 'review_running', 'recovery_reviewed', 'succeeded', 'failed')),
    revision                       INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    error_code                     TEXT NOT NULL DEFAULT '',
    authorized_at                  TIMESTAMP NOT NULL,
    updated_at                     TIMESTAMP NOT NULL CHECK (updated_at >= authorized_at),
    finished_at                    TIMESTAMP,
    CHECK ((input_json = '') = (input_digest = '')),
    CHECK ((status = 'authorized' AND input_json = '') OR status = 'preflight_failed' OR (status NOT IN ('authorized', 'preflight_failed') AND input_json <> '')),
    CHECK ((worker_model_call_count = 0 AND launch_id = '' AND worker_codex_session_id = '' AND new_head = '') OR worker_model_call_count = 1),
    CHECK (status IN ('authorized', 'requested', 'preflight_failed') OR worker_model_call_count = 1),
    CHECK (status NOT IN ('review_running', 'recovery_reviewed', 'succeeded') OR (reviewer_model_call_count = 1 AND recovery_review_run_id <> '' AND new_head <> '')),
    CHECK ((status IN ('preflight_failed', 'succeeded', 'failed')) = (finished_at IS NOT NULL))
);

CREATE UNIQUE INDEX idx_dcp_card12_one_fresh_worker_recovery
    ON dcp_review_lab_card12_fresh_worker_recovery ((1));

INSERT INTO dcp_review_lab_card12_fresh_worker_recovery (
    recovery_id, recovery_generation, recovery_identity_digest,
    incident_id, incident_generation, successor_attempt_id,
    successor_attempt_generation, successor_identity_digest,
    accepted_decision_digest, admission_id, session_id, task_id, project_id,
    repository, worktree_path, source_branch, pr_url, pr_number, old_head,
    current_main, predecessor_status, predecessor_error,
    old_runtime_handle_id, old_agent_session_id, old_runtime_launch_id,
    contract_commit, model, reasoning, token_budget, runtime_action_id,
    runtime_handle_id, status, authorized_at, updated_at
)
SELECT
    'dcp-card12-fresh-worker-recovery-d2b7142bc9e5844ba165abe24d3222b3e1a94c3577fba5f6f8d97ec3dbad151b',
    1, 'd2b7142bc9e5844ba165abe24d3222b3e1a94c3577fba5f6f8d97ec3dbad151b',
    successor.incident_id, 1, successor.attempt_id, 2,
    successor.attempt_identity_digest, successor.decision_digest,
    incident.admission_id, incident.session_id, incident.task_id, 'dcp-review-lab',
    'orenvlad-ai/dcp-review-lab', incident.worktree_path, incident.source_branch,
    incident.pr_url, incident.pr_number, incident.target_sha,
    'b34b31b5443890e69128db2862726950a6bbac0d', successor.status,
    successor.error_code, 'dcp-review-lab-12', '', '',
    '2a174899ae72bf1db548c3b2f172d963488191f1',
    'gpt-5.6-sol', 'xhigh', 16384,
    'dcp-card12-fresh-worker-recovery-d2b7142bc9e5844ba165abe24d3222b3e1a94c3577fba5f6f8d97ec3dbad151b',
    'dcp-card12-fresh-worker-recovery', 'authorized', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM dcp_review_lab_arbiter_v1_successor_attempt successor
JOIN dcp_review_lab_arbiter_v1 incident ON incident.incident_id = successor.incident_id
JOIN dcp_arbiter_successor_result_validation_recovery validation ON validation.attempt_id = successor.attempt_id
WHERE successor.attempt_id = 'dcp-arbiter-successor-3c62ea80b56ef94165519d4f01e4c449c320bff22d16b902dd68d4a1a355ea7d'
  AND successor.status = 'failed' AND successor.error_code = 'repair_launch_failed'
  AND successor.model_call_count = 1 AND successor.recovery_wake_count = 1
  AND successor.decision_digest = '237472879b22a8db65c5a3a0715510dc17aee1de93c45eaab45dde538cefb939'
  AND successor.recovery_owner_session_id = 'dcp-review-lab-12'
  AND successor.recovery_path = 'same_worker_conflict_repair'
  AND successor.recovery_review_run_id = '' AND successor.recovery_target_sha = ''
  AND successor.input_digest = 'aa44c625c940048d5e0266dac23dd4835a1afcf7648116a056758093b67160e6'
  AND successor.model = 'gpt-5.6-sol' AND successor.reasoning = 'xhigh'
  AND successor.token_budget = 16384 AND successor.policy_max_worker_calls = 1
  AND successor.policy_max_fresh_reviews = 1 AND successor.finished_at IS NOT NULL
  AND incident.incident_id = 'dcp-global-release-2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60'
  AND incident.generation = 1
  AND incident.identity_digest = '2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60'
  AND incident.input_digest = 'f618fa8a46715acce0958b592384f0d42c071562e36988163e2b96f2c157fc49'
  AND incident.status = 'failed' AND incident.error_code = 'submit_failed'
  AND incident.admission_id = 'dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34'
  AND incident.session_id = 'dcp-review-lab-12' AND incident.task_id = 'i13-arbiter-b'
  AND incident.source_branch = 'ao/dcp-review-lab-12/root'
  AND incident.pr_url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/9'
  AND incident.pr_number = 9 AND incident.target_sha = 'd4fcb68051ae113ed497d02151a759800ee85633'
  AND incident.current_base_sha = 'b34b31b5443890e69128db2862726950a6bbac0d'
  AND validation.status = 'applied' AND validation.finished_at IS NOT NULL;

-- +goose Down
-- Once any fence or artifact identity exists the recovery evidence is
-- irreversible. A fresh database with no row and an unused authorization may
-- still roll back during development.
CREATE TABLE dcp_card12_fresh_worker_rollback_guard (
    worker_calls INTEGER NOT NULL CHECK (worker_calls = 0),
    reviewer_calls INTEGER NOT NULL CHECK (reviewer_calls = 0),
    launch_id TEXT NOT NULL CHECK (launch_id = ''),
    new_head TEXT NOT NULL CHECK (new_head = '')
);
INSERT INTO dcp_card12_fresh_worker_rollback_guard
SELECT worker_model_call_count, reviewer_model_call_count, launch_id, new_head
FROM dcp_review_lab_card12_fresh_worker_recovery;
DROP INDEX idx_dcp_card12_one_fresh_worker_recovery;
DROP TABLE dcp_review_lab_card12_fresh_worker_recovery;
DROP TABLE dcp_card12_fresh_worker_rollback_guard;
