-- +goose Up
-- The owner-authorized I13 Stage 2 correction adds one successor attempt for
-- the exact frozen card-12 incident. The rejected v1 row and its artifacts
-- remain immutable; this is not a general attempt or retry table.
CREATE TABLE dcp_review_lab_arbiter_v1_successor_attempt (
    attempt_id                       TEXT PRIMARY KEY CHECK (attempt_id = 'dcp-arbiter-successor-3c62ea80b56ef94165519d4f01e4c449c320bff22d16b902dd68d4a1a355ea7d'),
    incident_id                      TEXT NOT NULL UNIQUE REFERENCES dcp_review_lab_arbiter_v1 (incident_id) ON DELETE RESTRICT,
    incident_generation              INTEGER NOT NULL CHECK (incident_generation = 1),
    attempt_generation               INTEGER NOT NULL CHECK (attempt_generation = 2),
    attempt_identity_digest          TEXT NOT NULL UNIQUE CHECK (attempt_identity_digest = '3c62ea80b56ef94165519d4f01e4c449c320bff22d16b902dd68d4a1a355ea7d'),
    incident_identity_digest         TEXT NOT NULL CHECK (incident_identity_digest = '2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60'),
    incident_input_digest            TEXT NOT NULL CHECK (incident_input_digest = 'f618fa8a46715acce0958b592384f0d42c071562e36988163e2b96f2c157fc49'),
    original_input_artifact_digest   TEXT NOT NULL CHECK (original_input_artifact_digest = '355a00609c8ded920bd87b215cea74d3c50213fa4ed8f0b484ea577f73bdbd7d'),
    original_schema_artifact_digest  TEXT NOT NULL CHECK (original_schema_artifact_digest = '8314793a7dbc3f0fc654c28e5936687138883b6e134460fc7204a025102b805f'),
    original_result_artifact_digest  TEXT NOT NULL CHECK (original_result_artifact_digest = 'd121d012a0b3042f02886fdc0c2aca806f34be64f9e5a3d15e1edf444ff3ae2d'),
    original_codex_session_id        TEXT NOT NULL CHECK (original_codex_session_id = '019ff23c-7cbf-7ee1-9567-30c6693f95fe'),
    original_token_count             INTEGER NOT NULL CHECK (original_token_count = 11583),
    contract_commit                  TEXT NOT NULL CHECK (contract_commit = '4dfff558ac425080d62bd6fe2fb13b573ef50661'),
    input_json                       TEXT NOT NULL DEFAULT '' CHECK (input_json = '' OR (length(CAST(input_json AS BLOB)) <= 16384 AND json_valid(input_json) AND json_extract(input_json, '$.schemaVersion') = 'dcp.review-lab.global-release-arbiter-successor-input/v1')),
    input_digest                     TEXT NOT NULL DEFAULT '' CHECK (input_digest = '' OR length(input_digest) = 64),
    model                            TEXT NOT NULL CHECK (model = 'gpt-5.6-sol'),
    reasoning                        TEXT NOT NULL CHECK (reasoning = 'xhigh'),
    token_budget                     INTEGER NOT NULL CHECK (token_budget = 16384),
    policy_max_worker_calls          INTEGER NOT NULL CHECK (policy_max_worker_calls = 1),
    policy_max_fresh_reviews         INTEGER NOT NULL CHECK (policy_max_fresh_reviews = 1),
    runtime_handle_id                TEXT NOT NULL CHECK (runtime_handle_id = 'dcp-global-release-arbiter-v1-successor'),
    launch_id                        TEXT NOT NULL CHECK (launch_id = attempt_id),
    status                           TEXT NOT NULL CHECK (status IN ('authorized', 'requested', 'preflight_failed', 'running', 'decided', 'safe_stopped', 'repairing', 'recovery_reviewed', 'succeeded', 'failed')),
    model_call_count                 INTEGER NOT NULL DEFAULT 0 CHECK (model_call_count IN (0, 1)),
    decision_json                    TEXT NOT NULL DEFAULT '' CHECK (decision_json = '' OR (json_valid(decision_json) AND json_extract(decision_json, '$.schemaVersion') = 'dcp.review-lab.global-release-arbiter-successor-decision/v1')),
    decision_digest                  TEXT NOT NULL DEFAULT '' CHECK (decision_digest = '' OR length(decision_digest) = 64),
    recovery_owner_session_id        TEXT NOT NULL DEFAULT '',
    recovery_path                    TEXT NOT NULL DEFAULT '' CHECK (recovery_path IN ('', 'same_worker_conflict_repair')),
    recovery_wake_count              INTEGER NOT NULL DEFAULT 0 CHECK (recovery_wake_count IN (0, 1)),
    recovery_review_run_id           TEXT NOT NULL DEFAULT '',
    recovery_target_sha              TEXT NOT NULL DEFAULT '' CHECK (recovery_target_sha = '' OR length(recovery_target_sha) = 40),
    error_code                       TEXT NOT NULL DEFAULT '',
    authorized_at                    TIMESTAMP NOT NULL,
    updated_at                       TIMESTAMP NOT NULL CHECK (updated_at >= authorized_at),
    decision_at                      TIMESTAMP,
    finished_at                      TIMESTAMP,
    CHECK ((input_json = '') = (input_digest = '')),
    CHECK ((status = 'authorized' AND input_json = '') OR (status <> 'authorized' AND input_json <> '')),
    CHECK ((status IN ('authorized', 'requested', 'preflight_failed') AND model_call_count = 0) OR (status NOT IN ('authorized', 'requested', 'preflight_failed') AND model_call_count = 1)),
    CHECK ((decision_json = '') = (decision_digest = '')),
    CHECK (status NOT IN ('authorized', 'requested', 'preflight_failed', 'running') OR decision_json = ''),
    CHECK (status NOT IN ('decided', 'safe_stopped', 'repairing', 'recovery_reviewed', 'succeeded') OR decision_json <> ''),
    CHECK (status NOT IN ('repairing', 'recovery_reviewed', 'succeeded') OR recovery_wake_count = 1),
    CHECK (status NOT IN ('authorized', 'requested', 'preflight_failed', 'running', 'decided', 'safe_stopped') OR recovery_wake_count = 0),
    CHECK ((recovery_wake_count = 0 AND recovery_owner_session_id = '' AND recovery_path = '') OR
           (recovery_wake_count = 1 AND recovery_owner_session_id = 'dcp-review-lab-12' AND recovery_path = 'same_worker_conflict_repair')),
    CHECK ((status IN ('preflight_failed', 'safe_stopped', 'succeeded', 'failed')) = (finished_at IS NOT NULL))
);

CREATE UNIQUE INDEX idx_dcp_review_lab_arbiter_v1_one_successor_attempt
    ON dcp_review_lab_arbiter_v1_successor_attempt ((1));

INSERT INTO dcp_review_lab_arbiter_v1_successor_attempt (
    attempt_id, incident_id, incident_generation, attempt_generation,
    attempt_identity_digest, incident_identity_digest, incident_input_digest,
    original_input_artifact_digest, original_schema_artifact_digest,
    original_result_artifact_digest, original_codex_session_id,
    original_token_count, contract_commit, model, reasoning, token_budget,
    policy_max_worker_calls, policy_max_fresh_reviews, runtime_handle_id,
    launch_id, status, authorized_at, updated_at
)
SELECT
    'dcp-arbiter-successor-3c62ea80b56ef94165519d4f01e4c449c320bff22d16b902dd68d4a1a355ea7d',
    arb.incident_id, arb.generation, 2,
    '3c62ea80b56ef94165519d4f01e4c449c320bff22d16b902dd68d4a1a355ea7d',
    arb.identity_digest, arb.input_digest,
    '355a00609c8ded920bd87b215cea74d3c50213fa4ed8f0b484ea577f73bdbd7d',
    '8314793a7dbc3f0fc654c28e5936687138883b6e134460fc7204a025102b805f',
    'd121d012a0b3042f02886fdc0c2aca806f34be64f9e5a3d15e1edf444ff3ae2d',
    '019ff23c-7cbf-7ee1-9567-30c6693f95fe', 11583,
    '4dfff558ac425080d62bd6fe2fb13b573ef50661',
    'gpt-5.6-sol', 'xhigh', 16384, 1, 1,
    'dcp-global-release-arbiter-v1-successor',
    'dcp-arbiter-successor-3c62ea80b56ef94165519d4f01e4c449c320bff22d16b902dd68d4a1a355ea7d',
    'authorized', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM dcp_review_lab_arbiter_v1 arb
WHERE arb.incident_id = 'dcp-global-release-2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60'
  AND arb.generation = 1
  AND arb.identity_digest = '2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60'
  AND arb.input_digest = 'f618fa8a46715acce0958b592384f0d42c071562e36988163e2b96f2c157fc49'
  AND arb.admission_id = 'dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34'
  AND arb.incident_lease_id = 'dcp-incident-dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34'
  AND arb.source_packet_digest = 'fab52d627d14a21ea7ab2a7fdadb4d6f53478d5cdc496858ca74c37e1dfda057'
  AND arb.task_id = 'i13-arbiter-b'
  AND arb.session_id = 'dcp-review-lab-12'
  AND arb.pr_url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/9'
  AND arb.pr_number = 9
  AND arb.target_sha = 'd4fcb68051ae113ed497d02151a759800ee85633'
  AND arb.current_base_sha = 'b34b31b5443890e69128db2862726950a6bbac0d'
  AND arb.status = 'failed'
  AND arb.model_call_count = 1
  AND arb.decision_json = ''
  AND arb.decision_digest = ''
  AND arb.recovery_wake_count = 0
  AND arb.error_code = 'submit_failed'
  AND arb.finished_at IS NOT NULL
  AND arb.model = 'gpt-5.6-sol'
  AND arb.reasoning = 'xhigh'
  AND arb.token_budget = 16384
  AND EXISTS (
    SELECT 1 FROM dcp_review_lab_arbiter_v1_prelaunch_recovery pre
    WHERE pre.incident_id = arb.incident_id
  )
  AND EXISTS (
    SELECT 1 FROM dcp_review_lab_arbiter_v1_schema_recovery schema_recovery
    WHERE schema_recovery.incident_id = arb.incident_id
  );

-- +goose Down
-- Refuse to erase a successor fence or result. Only an unused authorization
-- row (or no row in a fresh database) is reversible.
CREATE TABLE dcp_arbiter_successor_rollback_guard (
    model_call_count INTEGER NOT NULL CHECK (model_call_count = 0)
);
INSERT INTO dcp_arbiter_successor_rollback_guard (model_call_count)
SELECT model_call_count FROM dcp_review_lab_arbiter_v1_successor_attempt;
DROP INDEX idx_dcp_review_lab_arbiter_v1_one_successor_attempt;
DROP TABLE dcp_review_lab_arbiter_v1_successor_attempt;
DROP TABLE dcp_arbiter_successor_rollback_guard;
