-- +goose Up
-- Preserve Scenario A's exact pre-launch continuation-target false failure,
-- then re-arm only its already-authorized repair action. The action never
-- crossed the model-call fence (empty launch id, slot released), so this adds
-- no repair generation, arbiter generation, or model authority.
CREATE TABLE dcp_future_card_repair_target_validation_recovery_v1 (
    recovery_id              TEXT PRIMARY KEY CHECK (recovery_id = 'dcp-future-repair-target-recovery-arb-a-second'),
    incident_id              TEXT NOT NULL UNIQUE REFERENCES dcp_future_card_arbiter_v1 (incident_id) ON DELETE RESTRICT,
    task_id                  TEXT NOT NULL UNIQUE REFERENCES dcp_review_lab_policy_task (task_id) ON DELETE RESTRICT,
    model_action_id          TEXT NOT NULL UNIQUE REFERENCES dcp_model_action (id) ON DELETE RESTRICT,
    prior_task_state         TEXT NOT NULL CHECK (prior_task_state = 'failed'),
    prior_task_revision      INTEGER NOT NULL CHECK (prior_task_revision = 13),
    prior_task_error_code    TEXT NOT NULL CHECK (prior_task_error_code = 'worker_target_invalid'),
    prior_action_status      TEXT NOT NULL CHECK (prior_action_status = 'failed'),
    prior_action_error_code  TEXT NOT NULL CHECK (prior_action_error_code = 'worker_target_invalid'),
    prior_action_finished_at TIMESTAMP NOT NULL CHECK (prior_action_finished_at = '2026-08-15 00:11:24.373742 +0000 UTC'),
    local_main_before        TEXT NOT NULL CHECK (local_main_before = 'b1b58cb92f5a07413bf0077418519727cf93a1fd'),
    origin_main_before       TEXT NOT NULL CHECK (origin_main_before = '55e0c64b67560dc075d12a3dbc45a3d0674f405c'),
    authority               TEXT NOT NULL CHECK (authority = 'rearm_same_prelaunch_repair_action_zero_new_model_authority'),
    status                  TEXT NOT NULL CHECK (status = 'applied'),
    created_at              TIMESTAMP NOT NULL
);

INSERT INTO dcp_future_card_repair_target_validation_recovery_v1 (
    recovery_id, incident_id, task_id, model_action_id,
    prior_task_state, prior_task_revision, prior_task_error_code,
    prior_action_status, prior_action_error_code, prior_action_finished_at,
    local_main_before, origin_main_before, authority, status, created_at
)
SELECT
    'dcp-future-repair-target-recovery-arb-a-second', arb.incident_id,
    task.task_id, action.id, task.state, task.revision, task.error_code,
    action.status, action.error_code, action.updated_at,
    'b1b58cb92f5a07413bf0077418519727cf93a1fd',
    '55e0c64b67560dc075d12a3dbc45a3d0674f405c',
    'rearm_same_prelaunch_repair_action_zero_new_model_authority',
    'applied', CURRENT_TIMESTAMP
FROM dcp_future_card_arbiter_v1 arb
JOIN dcp_review_lab_policy_task task ON task.task_id = arb.task_id
JOIN dcp_model_action action ON action.id = arb.repair_action_id
JOIN dcp_future_card_arbiter_result_validation_recovery_v1 result_recovery
  ON result_recovery.incident_id = arb.incident_id
WHERE arb.incident_id = 'dcp-future-arbiter-9e94bbd542bafa1c1d3fd37ca4c1429dcf0aed444b71f07a6645655155cbcd10'
  AND arb.generation = 2
  AND arb.task_id = 'arb-a-second' AND arb.session_id = 'dcp-review-lab-22'
  AND arb.admission_id = 'dcp-admission-f3e6b021-dc04-494e-acae-57d0a8b76404'
  AND arb.status = 'repair_queued' AND arb.verdict = 'successor_repair'
  AND arb.model_call_count = 1
  AND arb.decision_digest = 'a44e3c9ba44f71bf21a9a3f862747b43ca5ad3c5f893bdc621ce510e40dbaf71'
  AND arb.repair_action_id = 'dcp-model-arb-a-second-worker-2'
  AND arb.recovery_review_run_id = '' AND arb.recovery_head_sha = ''
  AND arb.merge_commit_sha = '' AND arb.error_code = '' AND arb.finished_at IS NULL
  AND result_recovery.status = 'applied' AND result_recovery.error_code = ''
  AND result_recovery.finished_at = '2026-08-15 00:11:24.317427 +0000 UTC'
  AND task.state = 'failed' AND task.revision = 13 AND task.repair_count = 1
  AND task.current_head_sha = '8b3f601ae7b82b68bfd3f3810069c7a91774ca72'
  AND task.error_code = 'worker_target_invalid' AND task.merge_commit_sha = ''
  AND action.task_id = task.task_id AND action.session_id = task.session_id
  AND action.kind = 'repair_worker'
  AND action.exact_head_sha = task.current_head_sha
  AND action.incident_id = arb.incident_id
  AND action.status = 'failed' AND action.slot = 0 AND action.launch_id = ''
  AND action.review_run_id = '' AND action.error_code = 'worker_target_invalid'
  AND action.updated_at = '2026-08-15 00:11:24.373742 +0000 UTC';

-- The shipped transition guards correctly make failed rows terminal. Remove
-- them only inside this migration transaction, perform the one audited exact
-- re-arm, and restore the same strict guards before commit.
DROP TRIGGER dcp_model_action_immutable;
DROP TRIGGER dcp_review_lab_policy_task_immutable;

UPDATE dcp_model_action
SET status = 'queued', slot = 0, launch_id = '', review_run_id = '',
    error_code = '', updated_at = CURRENT_TIMESTAMP
WHERE id = 'dcp-model-arb-a-second-worker-2'
  AND status = 'failed' AND slot = 0 AND launch_id = ''
  AND error_code = 'worker_target_invalid'
  AND EXISTS (
    SELECT 1 FROM dcp_future_card_repair_target_validation_recovery_v1 recovery
    WHERE recovery.model_action_id = dcp_model_action.id AND recovery.status = 'applied'
  );

UPDATE dcp_review_lab_policy_task
SET state = 'repair_queued', revision = revision + 1, error_code = '',
    updated_at = CURRENT_TIMESTAMP
WHERE task_id = 'arb-a-second' AND state = 'failed' AND revision = 13
  AND repair_count = 1 AND error_code = 'worker_target_invalid'
  AND EXISTS (
    SELECT 1
    FROM dcp_future_card_repair_target_validation_recovery_v1 recovery
    JOIN dcp_model_action action ON action.id = recovery.model_action_id
    WHERE recovery.task_id = dcp_review_lab_policy_task.task_id
      AND recovery.status = 'applied' AND action.status = 'queued'
  );

-- +goose StatementBegin
CREATE TRIGGER dcp_model_action_immutable
BEFORE UPDATE ON dcp_model_action
WHEN OLD.sequence <> NEW.sequence
  OR OLD.id <> NEW.id
  OR OLD.task_id <> NEW.task_id
  OR OLD.session_id <> NEW.session_id
  OR OLD.kind <> NEW.kind
  OR OLD.exact_head_sha <> NEW.exact_head_sha
  OR OLD.incident_id <> NEW.incident_id
  OR OLD.created_at <> NEW.created_at
  OR NEW.updated_at < OLD.updated_at
  OR NOT (
      (OLD.status = 'queued' AND NEW.status = 'claimed')
      OR (OLD.status = 'claimed' AND NEW.status IN ('running', 'failed'))
      OR (OLD.status = 'running' AND NEW.status IN ('succeeded', 'failed'))
  )
BEGIN
    SELECT RAISE(ABORT, 'dcp model action immutable identity or transition violated');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_review_lab_policy_task_immutable
BEFORE UPDATE ON dcp_review_lab_policy_task
WHEN OLD.task_id <> NEW.task_id
  OR OLD.payload_json <> NEW.payload_json
  OR OLD.payload_digest <> NEW.payload_digest
  OR OLD.target <> NEW.target
  OR OLD.profile <> NEW.profile
  OR OLD.repository <> NEW.repository
  OR OLD.policy_version <> NEW.policy_version
  OR OLD.session_id <> NEW.session_id
  OR OLD.card_number <> NEW.card_number
  OR OLD.worktree_path <> NEW.worktree_path
  OR OLD.source_branch <> NEW.source_branch
  OR OLD.prompt <> NEW.prompt
  OR OLD.created_at <> NEW.created_at
  OR OLD.state IN ('merged', 'failed')
  OR (OLD.state = 'incident' AND NOT (
      NEW.state = 'repair_queued'
      AND NEW.repair_count = OLD.repair_count + 1
      AND EXISTS (
        SELECT 1 FROM dcp_future_card_arbiter_v1 arb
        WHERE arb.task_id = OLD.task_id
          AND arb.source_packet_json = OLD.incident_packet
          AND arb.status = 'repair_queued'
          AND arb.repair_task_id = OLD.task_id
      )
  ))
  OR NEW.repair_count < OLD.repair_count
  OR NEW.repair_count > OLD.repair_count + 1
  OR NEW.revision <> OLD.revision + 1
  OR NEW.updated_at < OLD.updated_at
BEGIN
    SELECT RAISE(ABORT, 'dcp review-lab policy immutable identity or revision violated');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_future_card_repair_target_validation_recovery_immutable
BEFORE UPDATE ON dcp_future_card_repair_target_validation_recovery_v1
BEGIN
    SELECT RAISE(ABORT, 'DCP future repair target recovery is immutable evidence');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_future_card_repair_target_validation_recovery_no_delete
BEFORE DELETE ON dcp_future_card_repair_target_validation_recovery_v1
BEGIN
    SELECT RAISE(ABORT, 'DCP future repair target recovery cannot be deleted');
END;
-- +goose StatementEnd

-- +goose Down
SELECT RAISE(ABORT, '0072 future-card repair target recovery is immutable evidence');
