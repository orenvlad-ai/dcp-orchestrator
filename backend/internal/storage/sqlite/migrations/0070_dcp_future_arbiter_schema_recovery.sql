-- +goose Up
-- Preserve the exact Scenario-A generation-1 provider schema rejection and
-- authorize one additive generation-2 attempt only. The predecessor incident
-- and model action stay terminal and immutable; clean databases receive no
-- authorization row.
CREATE TABLE dcp_future_card_arbiter_schema_recovery_v1 (
    recovery_id                  TEXT PRIMARY KEY CHECK (recovery_id GLOB 'dcp-future-arbiter-schema-recovery-[0-9a-f]*'),
    predecessor_incident_id      TEXT NOT NULL UNIQUE REFERENCES dcp_future_card_arbiter_v1 (incident_id) ON DELETE RESTRICT,
    predecessor_identity_digest  TEXT NOT NULL CHECK (length(predecessor_identity_digest) = 64),
    predecessor_input_digest     TEXT NOT NULL CHECK (length(predecessor_input_digest) = 64),
    predecessor_model_action_id  TEXT NOT NULL UNIQUE REFERENCES dcp_model_action (id) ON DELETE RESTRICT,
    predecessor_schema_digest    TEXT NOT NULL CHECK (length(predecessor_schema_digest) = 64),
    provider_error_json          TEXT NOT NULL CHECK (
        json_valid(provider_error_json)
        AND json_extract(provider_error_json, '$.type') = 'invalid_request_error'
        AND json_extract(provider_error_json, '$.code') = 'invalid_json_schema'
        AND json_extract(provider_error_json, '$.status') = 400
    ),
    provider_error_digest        TEXT NOT NULL CHECK (length(provider_error_digest) = 64),
    provider_inference_tokens    INTEGER NOT NULL CHECK (provider_inference_tokens = 0),
    successor_generation         INTEGER NOT NULL CHECK (successor_generation = 2),
    status                       TEXT NOT NULL CHECK (status IN ('authorized', 'consumed')),
    successor_incident_id        TEXT NOT NULL DEFAULT '' CHECK (successor_incident_id = '' OR length(successor_incident_id) = 83),
    created_at                   TIMESTAMP NOT NULL,
    updated_at                   TIMESTAMP NOT NULL CHECK (updated_at >= created_at),
    consumed_at                  TIMESTAMP,
    CHECK ((status = 'authorized') = (successor_incident_id = '' AND consumed_at IS NULL)),
    CHECK ((status = 'consumed') = (successor_incident_id <> '' AND consumed_at IS NOT NULL))
);

INSERT INTO dcp_future_card_arbiter_schema_recovery_v1 (
    recovery_id, predecessor_incident_id, predecessor_identity_digest,
    predecessor_input_digest, predecessor_model_action_id,
    predecessor_schema_digest, provider_error_json, provider_error_digest,
    provider_inference_tokens, successor_generation, status, created_at, updated_at
)
SELECT
    'dcp-future-arbiter-schema-recovery-141e3d64af9568aea9ea1fb6835045060dfd566bc3b21d50ff6f3f90f3f67a52',
    arb.incident_id, arb.identity_digest, arb.input_digest, arb.model_action_id,
    'efd3508976954dde038b545b53e1240ab41dee70ef3fc1d64f777ebb68585f79',
    '{"type":"invalid_request_error","code":"invalid_json_schema","message":"Invalid schema for response_format ''codex_output_schema'': In context=(''properties'', ''affectedPaths''), ''uniqueItems'' is not permitted.","status":400}',
    'e4532d43b347f85107a31562ab21fc6c1a7baedf8b768bef4fc8eb90e927c008',
    0, 2, 'authorized', arb.finished_at, arb.finished_at
FROM dcp_future_card_arbiter_v1 arb
JOIN dcp_model_action action ON action.id = arb.model_action_id
WHERE arb.incident_id = 'dcp-future-arbiter-141e3d64af9568aea9ea1fb6835045060dfd566bc3b21d50ff6f3f90f3f67a52'
  AND arb.generation = 1
  AND arb.identity_digest = '141e3d64af9568aea9ea1fb6835045060dfd566bc3b21d50ff6f3f90f3f67a52'
  AND arb.input_digest = 'af059227f4c8890257a57d95f55128ac23a80ad1fe092193782de91196574d2d'
  AND arb.task_id = 'arb-a-second'
  AND arb.session_id = 'dcp-review-lab-22'
  AND arb.model_action_id = 'dcp-model-arb-a-second-arbiter-1'
  AND arb.status = 'failed'
  AND arb.model_call_count = 1
  AND arb.error_code = 'launch_failed'
  AND arb.decision_json = ''
  AND arb.decision_digest = ''
  AND arb.finished_at = '2026-08-14 23:12:10.563532 +0000 UTC'
  AND action.task_id = arb.task_id
  AND action.session_id = arb.session_id
  AND action.kind = 'arbiter'
  AND action.incident_id = arb.incident_id
  AND action.status = 'failed'
  AND action.error_code = 'launch_failed';

-- +goose StatementBegin
CREATE TRIGGER dcp_future_card_arbiter_schema_recovery_immutable
BEFORE UPDATE ON dcp_future_card_arbiter_schema_recovery_v1
WHEN OLD.recovery_id <> NEW.recovery_id
  OR OLD.predecessor_incident_id <> NEW.predecessor_incident_id
  OR OLD.predecessor_identity_digest <> NEW.predecessor_identity_digest
  OR OLD.predecessor_input_digest <> NEW.predecessor_input_digest
  OR OLD.predecessor_model_action_id <> NEW.predecessor_model_action_id
  OR OLD.predecessor_schema_digest <> NEW.predecessor_schema_digest
  OR OLD.provider_error_json <> NEW.provider_error_json
  OR OLD.provider_error_digest <> NEW.provider_error_digest
  OR OLD.provider_inference_tokens <> NEW.provider_inference_tokens
  OR OLD.successor_generation <> NEW.successor_generation
  OR OLD.created_at <> NEW.created_at
  OR NEW.updated_at < OLD.updated_at
  OR NOT (OLD.status = 'authorized' AND NEW.status = 'consumed')
BEGIN
    SELECT RAISE(ABORT, 'DCP future arbiter schema recovery is immutable');
END;
-- +goose StatementEnd

-- +goose Down
SELECT RAISE(ABORT, '0070 future-card arbiter schema recovery is immutable evidence');
