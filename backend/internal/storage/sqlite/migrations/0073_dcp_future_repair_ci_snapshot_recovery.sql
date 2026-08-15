-- +goose Up
-- Preserve Scenario A's exact post-repair false CI failure. Stock SCM keeps
-- immutable check rows for earlier PR heads; the old successful check was
-- incorrectly treated as current-head identity drift even though the fresh
-- exact head and its one named check were already durably green. Bind those
-- persisted facts and queue only the already-authorized fresh reviewer.
CREATE TABLE dcp_future_card_repair_ci_snapshot_recovery_v1 (
    recovery_id              TEXT PRIMARY KEY CHECK (recovery_id = 'dcp-future-repair-ci-recovery-arb-a-second'),
    incident_id              TEXT NOT NULL UNIQUE REFERENCES dcp_future_card_arbiter_v1 (incident_id) ON DELETE RESTRICT,
    task_id                  TEXT NOT NULL UNIQUE REFERENCES dcp_review_lab_policy_task (task_id) ON DELETE RESTRICT,
    repair_action_id         TEXT NOT NULL UNIQUE REFERENCES dcp_model_action (id) ON DELETE RESTRICT,
    review_action_id         TEXT NOT NULL UNIQUE CHECK (review_action_id = 'dcp-model-arb-a-second-review-2'),
    prior_task_state         TEXT NOT NULL CHECK (prior_task_state = 'incident'),
    prior_task_revision      INTEGER NOT NULL CHECK (prior_task_revision = 17),
    prior_task_error_code    TEXT NOT NULL CHECK (prior_task_error_code = 'ci_identity_failed'),
    prior_task_updated_at    TIMESTAMP NOT NULL CHECK (prior_task_updated_at = '2026-08-15 00:40:00.421923 +0000 UTC'),
    repair_launch_id         TEXT NOT NULL CHECK (repair_launch_id = 'c488b498-a758-4d46-a81e-f9041e6df4df'),
    repair_finished_at       TIMESTAMP NOT NULL CHECK (repair_finished_at = '2026-08-15 00:39:59.569017 +0000 UTC'),
    prior_head_sha           TEXT NOT NULL CHECK (prior_head_sha = '8b3f601ae7b82b68bfd3f3810069c7a91774ca72'),
    recovery_head_sha        TEXT NOT NULL CHECK (recovery_head_sha = '931a69637be0b14d9ca145909d0f6060ad81c2fc'),
    current_main_sha         TEXT NOT NULL CHECK (current_main_sha = '55e0c64b67560dc075d12a3dbc45a3d0674f405c'),
    historical_check_url     TEXT NOT NULL CHECK (historical_check_url = 'https://github.com/orenvlad-ai/dcp-review-lab/actions/runs/31847795164/job/94917645974'),
    recovery_check_url       TEXT NOT NULL CHECK (recovery_check_url = 'https://github.com/orenvlad-ai/dcp-review-lab/actions/runs/31854288545/job/94935989369'),
    authority                TEXT NOT NULL CHECK (authority = 'bind_exact_green_repair_head_and_queue_one_fresh_reviewer'),
    status                   TEXT NOT NULL CHECK (status = 'applied'),
    created_at               TIMESTAMP NOT NULL
);

INSERT INTO dcp_future_card_repair_ci_snapshot_recovery_v1 (
    recovery_id, incident_id, task_id, repair_action_id, review_action_id,
    prior_task_state, prior_task_revision, prior_task_error_code, prior_task_updated_at,
    repair_launch_id, repair_finished_at, prior_head_sha, recovery_head_sha,
    current_main_sha, historical_check_url, recovery_check_url, authority, status, created_at
)
SELECT
    'dcp-future-repair-ci-recovery-arb-a-second', arb.incident_id, task.task_id,
    repair.id, 'dcp-model-arb-a-second-review-2', task.state, task.revision,
    task.error_code, task.updated_at, repair.launch_id, repair.updated_at,
    task.current_head_sha, pr.head_sha, pr.base_sha, old_check.url, new_check.url,
    'bind_exact_green_repair_head_and_queue_one_fresh_reviewer', 'applied', CURRENT_TIMESTAMP
FROM dcp_future_card_arbiter_v1 arb
JOIN dcp_review_lab_policy_task task ON task.task_id = arb.task_id
JOIN dcp_model_action repair ON repair.id = arb.repair_action_id
JOIN dcp_future_card_repair_target_validation_recovery_v1 target_recovery
  ON target_recovery.incident_id = arb.incident_id
JOIN pr ON pr.url = task.pr_url AND pr.session_id = task.session_id
JOIN pr_checks old_check ON old_check.pr_url = pr.url AND old_check.commit_hash = task.current_head_sha
JOIN pr_checks new_check ON new_check.pr_url = pr.url AND new_check.commit_hash = pr.head_sha
WHERE arb.incident_id = 'dcp-future-arbiter-9e94bbd542bafa1c1d3fd37ca4c1429dcf0aed444b71f07a6645655155cbcd10'
  AND arb.generation = 2 AND arb.status = 'repair_queued' AND arb.verdict = 'successor_repair'
  AND arb.model_call_count = 1 AND arb.repair_task_id = 'arb-a-second'
  AND arb.repair_action_id = 'dcp-model-arb-a-second-worker-2'
  AND arb.recovery_review_run_id = '' AND arb.recovery_head_sha = ''
  AND arb.merge_commit_sha = '' AND arb.error_code = '' AND arb.finished_at IS NULL
  AND target_recovery.status = 'applied'
  AND task.task_id = 'arb-a-second' AND task.session_id = 'dcp-review-lab-22'
  AND task.state = 'incident' AND task.revision = 17 AND task.repair_count = 1
  AND task.current_head_sha = '8b3f601ae7b82b68bfd3f3810069c7a91774ca72'
  AND task.previous_head_sha = '' AND task.error_code = 'ci_identity_failed'
  AND task.updated_at = '2026-08-15 00:40:00.421923 +0000 UTC'
  AND json_extract(task.incident_packet, '$.schemaVersion') = 'dcp.review-lab.policy-incident/v1'
  AND json_extract(task.incident_packet, '$.reason') = 'ci_identity_failed'
  AND json_extract(task.incident_packet, '$.detail') = 'named CI exact head drifted'
  AND repair.task_id = task.task_id AND repair.session_id = task.session_id
  AND repair.kind = 'repair_worker' AND repair.exact_head_sha = task.current_head_sha
  AND repair.incident_id = arb.incident_id AND repair.status = 'succeeded' AND repair.slot = 0
  AND repair.launch_id = 'c488b498-a758-4d46-a81e-f9041e6df4df'
  AND repair.review_run_id = '' AND repair.error_code = ''
  AND repair.updated_at = '2026-08-15 00:39:59.569017 +0000 UTC'
  AND pr.number = 19 AND pr.head_sha = '931a69637be0b14d9ca145909d0f6060ad81c2fc'
  AND pr.base_sha = '55e0c64b67560dc075d12a3dbc45a3d0674f405c'
  AND pr.pr_state = 'open' AND pr.ci_state = 'passing' AND pr.mergeability = 'mergeable'
  AND pr.provider = 'github' AND pr.host = 'github.com' AND pr.repo = 'orenvlad-ai/dcp-review-lab'
  AND pr.source_branch = task.source_branch AND pr.target_branch = 'main'
  AND pr.author = 'orenvlad-ai' AND pr.provider_state = 'OPEN'
  AND pr.provider_mergeable = 'MERGEABLE' AND pr.provider_merge_state_status = 'CLEAN'
  AND pr.is_draft = 0 AND pr.is_merged = 0 AND pr.is_closed = 0
  AND old_check.name = 'dcp-review-lab' AND old_check.status = 'passed'
  AND old_check.conclusion = 'success'
  AND old_check.url = 'https://github.com/orenvlad-ai/dcp-review-lab/actions/runs/31847795164/job/94917645974'
  AND new_check.name = 'dcp-review-lab' AND new_check.status = 'passed'
  AND new_check.conclusion = 'success'
  AND new_check.url = 'https://github.com/orenvlad-ai/dcp-review-lab/actions/runs/31854288545/job/94935989369'
  AND (SELECT COUNT(*) FROM pr_checks current_check
       WHERE current_check.pr_url = pr.url AND current_check.commit_hash = pr.head_sha) = 1
  AND NOT EXISTS (SELECT 1 FROM dcp_model_action existing
                  WHERE existing.id = 'dcp-model-arb-a-second-review-2');

-- The shipped task guard correctly makes incident rows terminal except for
-- the original arbiter repair edge. Remove it only inside this transaction,
-- bind the exact already-persisted head/check, and restore it before commit.
DROP TRIGGER dcp_review_lab_policy_task_immutable;

UPDATE dcp_review_lab_policy_task
SET state = 'review_queued', revision = revision + 1,
    previous_head_sha = current_head_sha,
    current_head_sha = '931a69637be0b14d9ca145909d0f6060ad81c2fc',
    review_run_id = '', error_code = '', incident_packet = '', updated_at = CURRENT_TIMESTAMP
WHERE task_id = 'arb-a-second' AND state = 'incident' AND revision = 17
  AND repair_count = 1 AND error_code = 'ci_identity_failed'
  AND EXISTS (
    SELECT 1 FROM dcp_future_card_repair_ci_snapshot_recovery_v1 recovery
    WHERE recovery.task_id = dcp_review_lab_policy_task.task_id AND recovery.status = 'applied'
  );

INSERT INTO dcp_model_action (
    id, task_id, session_id, kind, exact_head_sha, status, slot, launch_id,
    review_run_id, incident_id, error_code, created_at, updated_at
)
SELECT recovery.review_action_id, recovery.task_id, task.session_id, 'reviewer',
       recovery.recovery_head_sha, 'queued', 0, '', '', '', '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM dcp_future_card_repair_ci_snapshot_recovery_v1 recovery
JOIN dcp_review_lab_policy_task task ON task.task_id = recovery.task_id
WHERE recovery.status = 'applied' AND task.state = 'review_queued'
  AND task.current_head_sha = recovery.recovery_head_sha;

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
CREATE TRIGGER dcp_future_card_repair_ci_snapshot_recovery_immutable
BEFORE UPDATE ON dcp_future_card_repair_ci_snapshot_recovery_v1
BEGIN
    SELECT RAISE(ABORT, 'DCP future repair CI snapshot recovery is immutable evidence');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_future_card_repair_ci_snapshot_recovery_no_delete
BEFORE DELETE ON dcp_future_card_repair_ci_snapshot_recovery_v1
BEGIN
    SELECT RAISE(ABORT, 'DCP future repair CI snapshot recovery cannot be deleted');
END;
-- +goose StatementEnd

-- +goose Down
SELECT RAISE(ABORT, '0073 future-card repair CI snapshot recovery is immutable evidence');
