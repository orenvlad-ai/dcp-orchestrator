-- +goose Up
-- A stock SCM observation created the exact night-ui-b PR row before provider
-- enrichment completed. The old policy gate misclassified that partial row as
-- contradictory identity. Preserve the immutable failure once, then re-arm
-- only the same worker/session/branch/PR after the complete provider and named
-- check facts prove the original worker result. This recovery launches no
-- worker, reviewer, arbiter, push, repair, admission, or merge by itself.
CREATE TABLE dcp_policy_provider_pending_recovery (
    recovery_id          TEXT PRIMARY KEY CHECK (recovery_id = 'dcp-night-ui-b-provider-pending-recovery-0223c91fbdfd9d93ab47657b197fe1cc0356d0da4f15c1f832ef5c0b5b4722a8'),
    task_id              TEXT NOT NULL UNIQUE CHECK (task_id = 'night-ui-b'),
    payload_digest       TEXT NOT NULL CHECK (payload_digest = '0223c91fbdfd9d93ab47657b197fe1cc0356d0da4f15c1f832ef5c0b5b4722a8'),
    session_id           TEXT NOT NULL UNIQUE CHECK (session_id = 'dcp-review-lab-20'),
    card_number          INTEGER NOT NULL CHECK (card_number = 20),
    source_branch        TEXT NOT NULL CHECK (source_branch = 'ao/dcp-review-lab-20/root'),
    prior_state          TEXT NOT NULL CHECK (prior_state = 'incident'),
    prior_revision       INTEGER NOT NULL CHECK (prior_revision = 5),
    prior_error_code     TEXT NOT NULL CHECK (prior_error_code = 'provider_identity_drift'),
    prior_incident       TEXT NOT NULL CHECK (prior_incident = '{"detail":"policy PR provider identity is not exact","reason":"provider_identity_drift","schemaVersion":"dcp.review-lab.policy-incident/v1"}'),
    worker_action_id     TEXT NOT NULL UNIQUE CHECK (worker_action_id = 'dcp-model-night-ui-b-worker-1'),
    worker_launch_id     TEXT NOT NULL CHECK (worker_launch_id = '0d38b38c-f9cd-470b-b468-946d553a3e75'),
    pr_url               TEXT NOT NULL UNIQUE CHECK (pr_url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/17'),
    pr_number            INTEGER NOT NULL CHECK (pr_number = 17),
    pr_head              TEXT NOT NULL CHECK (pr_head = '6211c80a4b9e8b6ab30a38a64c4bca3ec38ef621'),
    pr_base              TEXT NOT NULL CHECK (pr_base = '2ef5c575b16705fb70f75d5dff47ec0f2cae21d2'),
    check_url            TEXT NOT NULL CHECK (check_url = 'https://github.com/orenvlad-ai/dcp-review-lab/actions/runs/31838388247/job/94889724858'),
    failed_source_sha    TEXT NOT NULL CHECK (failed_source_sha = '01d8905d98ddc7e1ace42c1e6440a4cb6a652e22'),
    failed_source_tree   TEXT NOT NULL CHECK (failed_source_tree = '3b4a01d924ea582bdc555f9b744ce502ed87ef0b'),
    prior_updated_at     TIMESTAMP NOT NULL,
    rearmed_at           TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX idx_dcp_policy_one_provider_pending_recovery
    ON dcp_policy_provider_pending_recovery ((1));

INSERT INTO dcp_policy_provider_pending_recovery (
    recovery_id, task_id, payload_digest, session_id, card_number,
    source_branch, prior_state, prior_revision, prior_error_code,
    prior_incident, worker_action_id, worker_launch_id, pr_url, pr_number,
    pr_head, pr_base, check_url, failed_source_sha, failed_source_tree,
    prior_updated_at, rearmed_at
)
SELECT
    'dcp-night-ui-b-provider-pending-recovery-0223c91fbdfd9d93ab47657b197fe1cc0356d0da4f15c1f832ef5c0b5b4722a8',
    task.task_id, task.payload_digest, task.session_id, task.card_number,
    task.source_branch, task.state, task.revision, task.error_code,
    task.incident_packet, action.id, action.launch_id, pr.url, pr.number,
    pr.head_sha, pr.base_sha, checks.url,
    '01d8905d98ddc7e1ace42c1e6440a4cb6a652e22',
    '3b4a01d924ea582bdc555f9b744ce502ed87ef0b',
    task.updated_at, CURRENT_TIMESTAMP
FROM dcp_review_lab_policy_task task
JOIN sessions session ON session.id = task.session_id
JOIN dcp_model_action action ON action.task_id = task.task_id
JOIN pr ON pr.session_id = task.session_id
JOIN pr_checks checks ON checks.pr_url = pr.url
WHERE task.task_id = 'night-ui-b'
  AND task.payload_digest = '0223c91fbdfd9d93ab47657b197fe1cc0356d0da4f15c1f832ef5c0b5b4722a8'
  AND task.target = 'dcp-review-lab' AND task.profile = 'synthetic-pr'
  AND task.repository = 'orenvlad-ai/dcp-review-lab'
  AND task.policy_version = 'dcp.review-lab.happy-path/v1'
  AND task.session_id = 'dcp-review-lab-20' AND task.card_number = 20
  AND task.worktree_path = '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/dcp-review-lab/dcp-review-lab-20'
  AND task.source_branch = 'ao/dcp-review-lab-20/root'
  AND task.state = 'incident' AND task.revision = 5 AND task.repair_count = 0
  AND task.pr_url = '' AND task.pr_number = 0
  AND task.current_head_sha = '' AND task.previous_head_sha = ''
  AND task.review_run_id = '' AND task.admission_id = ''
  AND task.merge_commit_sha = ''
  AND task.error_code = 'provider_identity_drift'
  AND task.incident_packet = '{"detail":"policy PR provider identity is not exact","reason":"provider_identity_drift","schemaVersion":"dcp.review-lab.policy-incident/v1"}'
  AND session.project_id = 'dcp-review-lab' AND session.num = 20
  AND session.kind = 'worker' AND session.harness = 'codex'
  AND session.activity_state = 'idle' AND session.is_terminated = 0
  AND session.branch = 'ao/dcp-review-lab-20/root'
  AND action.id = 'dcp-model-night-ui-b-worker-1'
  AND action.session_id = 'dcp-review-lab-20'
  AND action.kind = 'initial_worker' AND action.exact_head_sha = ''
  AND action.status = 'succeeded' AND action.slot = 0
  AND action.launch_id = '0d38b38c-f9cd-470b-b468-946d553a3e75'
  AND action.review_run_id = '' AND action.error_code = ''
  AND pr.url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/17'
  AND pr.number = 17 AND pr.pr_state = 'open' AND pr.review_decision = 'none'
  AND pr.ci_state = 'passing' AND pr.mergeability = 'mergeable'
  AND pr.provider = 'github' AND pr.host = 'github.com'
  AND pr.repo = 'orenvlad-ai/dcp-review-lab'
  AND pr.source_branch = 'ao/dcp-review-lab-20/root'
  AND pr.target_branch = 'main'
  AND pr.head_sha = '6211c80a4b9e8b6ab30a38a64c4bca3ec38ef621'
  AND pr.base_sha = '2ef5c575b16705fb70f75d5dff47ec0f2cae21d2'
  AND pr.author = 'orenvlad-ai' AND pr.is_draft = 0
  AND pr.is_merged = 0 AND pr.is_closed = 0 AND pr.provider_state = 'OPEN'
  AND pr.provider_mergeable = 'MERGEABLE'
  AND pr.provider_merge_state_status = 'CLEAN' AND pr.html_url = pr.url
  AND checks.name = 'dcp-review-lab'
  AND checks.commit_hash = '6211c80a4b9e8b6ab30a38a64c4bca3ec38ef621'
  AND checks.status = 'passed' AND checks.conclusion = 'success'
  AND checks.url = 'https://github.com/orenvlad-ai/dcp-review-lab/actions/runs/31838388247/job/94889724858'
  AND (SELECT count(*) FROM dcp_model_action a WHERE a.task_id = task.task_id) = 1
  AND (SELECT count(*) FROM dcp_model_action a WHERE a.status IN ('claimed', 'running')) = 0;

CREATE TABLE dcp_policy_provider_pending_up_guard (
    eligible_rows INTEGER NOT NULL CHECK (eligible_rows IN (0, 1)),
    recovery_rows INTEGER NOT NULL CHECK (recovery_rows = eligible_rows)
);
INSERT INTO dcp_policy_provider_pending_up_guard
SELECT
    (SELECT count(*) FROM dcp_review_lab_policy_task task
     WHERE task.task_id = 'night-ui-b'
       AND task.payload_digest = '0223c91fbdfd9d93ab47657b197fe1cc0356d0da4f15c1f832ef5c0b5b4722a8'
       AND task.session_id = 'dcp-review-lab-20' AND task.card_number = 20
       AND task.worktree_path = '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/dcp-review-lab/dcp-review-lab-20'
       AND task.source_branch = 'ao/dcp-review-lab-20/root'
       AND task.state = 'incident' AND task.revision = 5
       AND task.repair_count = 0 AND task.pr_url = '' AND task.pr_number = 0
       AND task.current_head_sha = '' AND task.review_run_id = ''
       AND task.admission_id = '' AND task.merge_commit_sha = ''
       AND task.error_code = 'provider_identity_drift'
       AND task.incident_packet = '{"detail":"policy PR provider identity is not exact","reason":"provider_identity_drift","schemaVersion":"dcp.review-lab.policy-incident/v1"}'),
    (SELECT count(*) FROM dcp_policy_provider_pending_recovery);

DROP TRIGGER dcp_review_lab_policy_task_immutable;
UPDATE dcp_review_lab_policy_task
SET state = 'ci_waiting', revision = 6, error_code = '', incident_packet = '',
    updated_at = CURRENT_TIMESTAMP
WHERE task_id = 'night-ui-b' AND state = 'incident' AND revision = 5
  AND error_code = 'provider_identity_drift'
  AND EXISTS (SELECT 1 FROM dcp_policy_provider_pending_recovery recovery
              WHERE recovery.task_id = dcp_review_lab_policy_task.task_id);

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
  OR OLD.state IN ('merged', 'failed', 'incident')
  OR NEW.repair_count < OLD.repair_count
  OR NEW.repair_count > OLD.repair_count + 1
  OR NEW.revision <> OLD.revision + 1
  OR NEW.updated_at < OLD.updated_at
BEGIN
    SELECT RAISE(ABORT, 'dcp review-lab policy immutable identity or revision violated');
END;
-- +goose StatementEnd

CREATE TABLE dcp_policy_provider_pending_rearm_guard (
    recovery_rows INTEGER NOT NULL CHECK (recovery_rows IN (0, 1)),
    rearmed_rows INTEGER NOT NULL CHECK (rearmed_rows = recovery_rows)
);
INSERT INTO dcp_policy_provider_pending_rearm_guard
SELECT count(*),
       (SELECT count(*) FROM dcp_review_lab_policy_task
        WHERE task_id = 'night-ui-b' AND state = 'ci_waiting' AND revision = 6
          AND repair_count = 0 AND pr_url = '' AND pr_number = 0
          AND current_head_sha = '' AND previous_head_sha = ''
          AND review_run_id = '' AND admission_id = ''
          AND merge_commit_sha = '' AND error_code = '' AND incident_packet = '')
FROM dcp_policy_provider_pending_recovery;
DROP TABLE dcp_policy_provider_pending_rearm_guard;
DROP TABLE dcp_policy_provider_pending_up_guard;

-- +goose StatementBegin
CREATE TRIGGER dcp_policy_provider_pending_recovery_no_update
BEFORE UPDATE ON dcp_policy_provider_pending_recovery
BEGIN
    SELECT RAISE(ABORT, 'DCP provider-pending recovery is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_policy_provider_pending_recovery_no_delete
BEFORE DELETE ON dcp_policy_provider_pending_recovery
BEGIN
    SELECT RAISE(ABORT, 'DCP provider-pending recovery is immutable');
END;
-- +goose StatementEnd

-- +goose Down
CREATE TABLE dcp_policy_provider_pending_down_guard (
    recovery_rows INTEGER NOT NULL CHECK (recovery_rows IN (0, 1)),
    rearmed_rows INTEGER NOT NULL CHECK (rearmed_rows = recovery_rows),
    action_rows INTEGER NOT NULL CHECK (action_rows = recovery_rows)
);
INSERT INTO dcp_policy_provider_pending_down_guard
SELECT count(*),
       (SELECT count(*) FROM dcp_review_lab_policy_task
        WHERE task_id = 'night-ui-b' AND state = 'ci_waiting' AND revision = 6
          AND repair_count = 0 AND pr_url = '' AND pr_number = 0
          AND current_head_sha = '' AND review_run_id = ''
          AND admission_id = '' AND merge_commit_sha = ''),
       (SELECT count(*) FROM dcp_model_action
        WHERE task_id = 'night-ui-b' AND kind = 'initial_worker'
          AND status = 'succeeded')
FROM dcp_policy_provider_pending_recovery;

DROP TRIGGER dcp_policy_provider_pending_recovery_no_update;
DROP TRIGGER dcp_policy_provider_pending_recovery_no_delete;
DROP TRIGGER dcp_review_lab_policy_task_immutable;
UPDATE dcp_review_lab_policy_task
SET state = 'incident', revision = 5, error_code = 'provider_identity_drift',
    incident_packet = (SELECT prior_incident FROM dcp_policy_provider_pending_recovery),
    updated_at = (SELECT prior_updated_at FROM dcp_policy_provider_pending_recovery)
WHERE task_id = 'night-ui-b' AND state = 'ci_waiting' AND revision = 6
  AND EXISTS (SELECT 1 FROM dcp_policy_provider_pending_recovery);

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
  OR OLD.state IN ('merged', 'failed', 'incident')
  OR NEW.repair_count < OLD.repair_count
  OR NEW.repair_count > OLD.repair_count + 1
  OR NEW.revision <> OLD.revision + 1
  OR NEW.updated_at < OLD.updated_at
BEGIN
    SELECT RAISE(ABORT, 'dcp review-lab policy immutable identity or revision violated');
END;
-- +goose StatementEnd

DROP INDEX idx_dcp_policy_one_provider_pending_recovery;
DROP TABLE dcp_policy_provider_pending_recovery;
DROP TABLE dcp_policy_provider_pending_down_guard;
