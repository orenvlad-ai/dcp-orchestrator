-- +goose Up
-- Preserve the exact first wb-core canary CI false incident and queue only its
-- already-authorized fresh reviewer. The predecessor incorrectly evaluated
-- unrelated skipped Release Train jobs before selecting the configured
-- baseline check. No task/session/PR/worker is recreated and no model runs in
-- this migration.
CREATE TABLE dcp_wbc_ci_truth_recovery_v1 (
    recovery_id             TEXT PRIMARY KEY CHECK (recovery_id = 'wbc-canary-v1-ci-truth-recovery'),
    contract_commit         TEXT NOT NULL CHECK (contract_commit = '1ca282408bec53a1d696cb58d247e33285209ee9'),
    task_id                 TEXT NOT NULL UNIQUE REFERENCES dcp_review_lab_policy_task (task_id) ON DELETE RESTRICT,
    session_id              TEXT NOT NULL UNIQUE REFERENCES sessions (id) ON DELETE RESTRICT,
    worker_action_id        TEXT NOT NULL UNIQUE REFERENCES dcp_model_action (id) ON DELETE RESTRICT,
    reviewer_action_id      TEXT NOT NULL UNIQUE CHECK (reviewer_action_id = 'dcp-model-wbc-canary-v1-review-1'),
    pr_url                  TEXT NOT NULL UNIQUE CHECK (pr_url = 'https://github.com/orenvlad-ai/wb-core/pull/987'),
    pr_number               INTEGER NOT NULL CHECK (pr_number = 987),
    head_sha                TEXT NOT NULL CHECK (head_sha = 'e8cca45f3995b8181fe81ead154f7a933dbacbe8'),
    base_sha                TEXT NOT NULL CHECK (base_sha = '45efaf76065f4364d815cb44fc15396fdf6d1f7d'),
    baseline_url            TEXT NOT NULL CHECK (baseline_url = 'https://github.com/orenvlad-ai/wb-core/actions/runs/32048996893/job/95443534690'),
    skipped_observation_count INTEGER NOT NULL CHECK (skipped_observation_count = 9),
    notification_id         TEXT NOT NULL UNIQUE REFERENCES notifications (id) ON DELETE RESTRICT,
    prior_task_state        TEXT NOT NULL CHECK (prior_task_state = 'incident'),
    prior_task_revision     INTEGER NOT NULL CHECK (prior_task_revision = 5),
    prior_error_code        TEXT NOT NULL CHECK (prior_error_code = 'ci_identity_failed'),
    prior_incident_packet   TEXT NOT NULL CHECK (prior_incident_packet = '{"detail":"named CI is not successful","reason":"ci_identity_failed","schemaVersion":"dcp.review-lab.policy-incident/v1"}'),
    authority               TEXT NOT NULL CHECK (authority = 'bind_exact_required_baseline_and_queue_one_fresh_reviewer'),
    status                  TEXT NOT NULL CHECK (status = 'applied'),
    created_at              TIMESTAMP NOT NULL
);

INSERT INTO dcp_wbc_ci_truth_recovery_v1 (
    recovery_id, contract_commit, task_id, session_id, worker_action_id,
    reviewer_action_id, pr_url, pr_number, head_sha, base_sha, baseline_url,
    skipped_observation_count, notification_id, prior_task_state,
    prior_task_revision, prior_error_code, prior_incident_packet, authority,
    status, created_at
)
SELECT
    'wbc-canary-v1-ci-truth-recovery',
    '1ca282408bec53a1d696cb58d247e33285209ee9',
    task.task_id, task.session_id, worker.id,
    'dcp-model-wbc-canary-v1-review-1', pr.url, pr.number, pr.head_sha,
    pr.base_sha, baseline.url,
    (SELECT COUNT(*) FROM pr_checks extra
     WHERE extra.pr_url = pr.url AND extra.commit_hash = pr.head_sha
       AND extra.name <> 'baseline' AND extra.status = 'skipped'
       AND extra.conclusion = 'skipped'),
    notification.id, task.state, task.revision, task.error_code,
    task.incident_packet,
    'bind_exact_required_baseline_and_queue_one_fresh_reviewer',
    'applied', CURRENT_TIMESTAMP
FROM dcp_review_lab_policy_task task
JOIN sessions session ON session.id = task.session_id
JOIN dcp_model_action worker ON worker.task_id = task.task_id
JOIN pr ON pr.session_id = task.session_id
JOIN pr_checks baseline ON baseline.pr_url = pr.url AND baseline.commit_hash = pr.head_sha
JOIN notifications notification ON notification.session_id = task.session_id AND notification.pr_url = pr.url
WHERE task.task_id = 'wbc-canary-v1'
  AND task.payload_digest = '3124b0ac5e50843ae2cec4ad8500ee70666cd7a65ff16554fd7fd5d204cba901'
  AND task.target = 'wb-core' AND task.profile = 'repo-only'
  AND task.repository = 'orenvlad-ai/wb-core'
  AND task.policy_version = 'dcp.wb-core.repo-only.release-train/v1'
  AND task.session_id = 'wb-core-1' AND task.card_number = 1
  AND task.worktree_path = '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/wb-core/wb-core-1'
  AND task.source_branch = 'ao/wb-core-1/root'
  AND task.state = 'incident' AND task.revision = 5 AND task.repair_count = 0
  AND task.pr_url = '' AND task.pr_number = 0 AND task.current_head_sha = ''
  AND task.previous_head_sha = '' AND task.review_run_id = '' AND task.admission_id = ''
  AND task.merge_commit_sha = '' AND task.error_code = 'ci_identity_failed'
  AND task.incident_packet = '{"detail":"named CI is not successful","reason":"ci_identity_failed","schemaVersion":"dcp.review-lab.policy-incident/v1"}'
  AND task.created_at = '2026-08-17 17:06:51.814478 +0000 UTC'
  AND task.updated_at = '2026-08-17 17:21:31.255762 +0000 UTC'
  AND session.project_id = 'wb-core' AND session.num = 1 AND session.kind = 'worker'
  AND session.harness = 'codex' AND session.display_name = 'DCP:wbc-canary-v1'
  AND session.activity_state = 'idle' AND session.is_terminated = 0
  AND session.branch = task.source_branch AND session.workspace_path = task.worktree_path
  AND session.runtime_handle_id = 'wb-core-1'
  AND worker.id = 'dcp-model-wbc-canary-v1-worker-1'
  AND worker.sequence = 71 AND worker.session_id = task.session_id
  AND worker.kind = 'initial_worker' AND worker.exact_head_sha = ''
  AND worker.status = 'succeeded' AND worker.slot = 0
  AND worker.launch_id = 'ea35056c-1a59-4c4d-94e4-bf6910573cd4'
  AND worker.review_run_id = '' AND worker.incident_id = '' AND worker.error_code = ''
  AND pr.url = 'https://github.com/orenvlad-ai/wb-core/pull/987'
  AND pr.number = 987 AND pr.pr_state = 'open' AND pr.review_decision = 'none'
  AND pr.ci_state = 'passing' AND pr.mergeability = 'mergeable'
  AND pr.provider = 'github' AND pr.host = 'github.com' AND pr.repo = task.repository
  AND pr.source_branch = task.source_branch AND pr.target_branch = 'main'
  AND pr.head_sha = 'e8cca45f3995b8181fe81ead154f7a933dbacbe8'
  AND pr.base_sha = '45efaf76065f4364d815cb44fc15396fdf6d1f7d'
  AND pr.author = 'orenvlad-ai' AND pr.provider_state = 'OPEN'
  AND pr.provider_mergeable = 'MERGEABLE' AND pr.provider_merge_state_status = 'CLEAN'
  AND pr.html_url = pr.url AND pr.is_draft = 0 AND pr.is_merged = 0 AND pr.is_closed = 0
  AND baseline.name = 'baseline' AND baseline.status = 'passed'
  AND baseline.conclusion = 'success'
  AND baseline.url = 'https://github.com/orenvlad-ai/wb-core/actions/runs/32048996893/job/95443534690'
  AND notification.id = 'ntf_2c602ac8-9c4c-4db3-8c70-0c6e02a85537'
  AND notification.type = 'ready_to_merge' AND notification.status = 'read'
  AND notification.title = 'docs: add DCP-to-WBC qualification canary · PR #987'
  AND notification.body = 'PR from session DCP:wbc-canary-v1 is ready to merge. CI passed with no blocking review feedback.'
  AND (SELECT COUNT(*) FROM dcp_review_lab_policy_task exact_task WHERE exact_task.task_id = task.task_id) = 1
  AND (SELECT COUNT(*) FROM sessions exact_session WHERE exact_session.id = task.session_id) = 1
  AND (SELECT COUNT(*) FROM dcp_model_action action WHERE action.task_id = task.task_id) = 1
  AND (SELECT COUNT(*) FROM review_run run WHERE run.session_id = task.session_id) = 0
  AND (SELECT COUNT(*) FROM pr exact_pr WHERE exact_pr.session_id = task.session_id) = 1
  AND (SELECT COUNT(*) FROM pr_checks exact_baseline
       WHERE exact_baseline.pr_url = pr.url AND exact_baseline.commit_hash = pr.head_sha
         AND exact_baseline.name = 'baseline') = 1
  AND (SELECT COUNT(*) FROM pr_checks exact_checks
       WHERE exact_checks.pr_url = pr.url AND exact_checks.commit_hash = pr.head_sha) = 10
  AND (SELECT COUNT(*) FROM notifications exact_notification
       WHERE exact_notification.session_id = task.session_id
         AND exact_notification.type = 'ready_to_merge') = 1
  AND NOT EXISTS (SELECT 1 FROM dcp_model_action existing
                  WHERE existing.id = 'dcp-model-wbc-canary-v1-review-1');

-- Existing exact task drift is a hard migration error. A fresh installation
-- with no canary remains a clean zero-row no-op.
CREATE TABLE dcp_wbc_ci_truth_recovery_v1_guard (
    existing_task_rows INTEGER NOT NULL,
    recovery_rows      INTEGER NOT NULL,
    CHECK (existing_task_rows = 0 OR (existing_task_rows = 1 AND recovery_rows = 1))
);
INSERT INTO dcp_wbc_ci_truth_recovery_v1_guard
SELECT
    (SELECT COUNT(*) FROM dcp_review_lab_policy_task WHERE task_id = 'wbc-canary-v1'),
    (SELECT COUNT(*) FROM dcp_wbc_ci_truth_recovery_v1);
DROP TABLE dcp_wbc_ci_truth_recovery_v1_guard;

DROP TRIGGER dcp_review_lab_policy_task_immutable;

UPDATE dcp_review_lab_policy_task
SET state = 'review_queued', revision = revision + 1,
    pr_url = 'https://github.com/orenvlad-ai/wb-core/pull/987', pr_number = 987,
    current_head_sha = 'e8cca45f3995b8181fe81ead154f7a933dbacbe8',
    previous_head_sha = '', review_run_id = '', admission_id = '',
    error_code = '', incident_packet = '', updated_at = CURRENT_TIMESTAMP
WHERE task_id = 'wbc-canary-v1' AND state = 'incident' AND revision = 5
  AND EXISTS (
    SELECT 1 FROM dcp_wbc_ci_truth_recovery_v1 recovery
    WHERE recovery.task_id = dcp_review_lab_policy_task.task_id AND recovery.status = 'applied'
  );

INSERT INTO dcp_model_action (
    id, task_id, session_id, kind, exact_head_sha, status, slot, launch_id,
    review_run_id, incident_id, error_code, created_at, updated_at
)
SELECT reviewer_action_id, task_id, session_id, 'reviewer', head_sha, 'queued',
       0, '', '', '', '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM dcp_wbc_ci_truth_recovery_v1
WHERE status = 'applied';

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
CREATE TRIGGER dcp_wbc_ci_truth_recovery_v1_no_update
BEFORE UPDATE ON dcp_wbc_ci_truth_recovery_v1
BEGIN
    SELECT RAISE(ABORT, 'DCP WBC CI truth recovery is immutable evidence');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_wbc_ci_truth_recovery_v1_no_delete
BEFORE DELETE ON dcp_wbc_ci_truth_recovery_v1
BEGIN
    SELECT RAISE(ABORT, 'DCP WBC CI truth recovery cannot be deleted');
END;
-- +goose StatementEnd

-- +goose Down
SELECT RAISE(ABORT, '0079 WBC CI truth recovery is immutable evidence');
