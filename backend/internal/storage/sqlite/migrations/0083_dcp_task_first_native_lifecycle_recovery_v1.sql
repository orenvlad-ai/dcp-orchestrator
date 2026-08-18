-- +goose Up
-- Activate the common task-first native lifecycle only for the exact stopped
-- schema-82 wbc-canary-v1 continuation. The existing admission keeps its FIFO
-- sequence; no task/session/action/review/admission/generation/release row is
-- created, and every predecessor incident remains immutable evidence.
CREATE TABLE dcp_task_first_native_lifecycle_recovery_v1 (
    recovery_id             TEXT PRIMARY KEY CHECK (recovery_id = 'wbc-canary-v1-task-first-native-lifecycle'),
    contract_commit         TEXT NOT NULL CHECK (contract_commit = '5075235780b9c38d95faa9657a70265069d3a5c5'),
    predecessor_source      TEXT NOT NULL CHECK (predecessor_source = '3fdc3976edc6bad591bca4cf4e254b479a905fb3'),
    task_id                 TEXT NOT NULL UNIQUE REFERENCES dcp_review_lab_policy_task (task_id) ON DELETE RESTRICT,
    session_id              TEXT NOT NULL UNIQUE REFERENCES sessions (id) ON DELETE RESTRICT,
    generation_id           TEXT NOT NULL UNIQUE REFERENCES dcp_wbc_readmission_generation (generation_id) ON DELETE RESTRICT,
    admission_id            TEXT NOT NULL UNIQUE REFERENCES dcp_review_lab_admission (id) ON DELETE RESTRICT,
    review_action_id        TEXT NOT NULL UNIQUE REFERENCES dcp_model_action (id) ON DELETE RESTRICT,
    review_run_id           TEXT NOT NULL UNIQUE REFERENCES review_run (id) ON DELETE RESTRICT,
    prior_task_revision     INTEGER NOT NULL CHECK (prior_task_revision = 22),
    admission_sequence      INTEGER NOT NULL CHECK (admission_sequence = 32),
    preserved_incident_size INTEGER NOT NULL CHECK (preserved_incident_size > 0),
    authority               TEXT NOT NULL CHECK (authority = 'rearm_exact_archived_task_for_common_non_model_admission_continuation'),
    status                  TEXT NOT NULL CHECK (status = 'applied'),
    created_at              TIMESTAMP NOT NULL
);

INSERT INTO dcp_task_first_native_lifecycle_recovery_v1 (
    recovery_id, contract_commit, predecessor_source, task_id, session_id,
    generation_id, admission_id, review_action_id, review_run_id,
    prior_task_revision, admission_sequence, preserved_incident_size,
    authority, status, created_at
)
SELECT
    'wbc-canary-v1-task-first-native-lifecycle',
    '5075235780b9c38d95faa9657a70265069d3a5c5',
    '3fdc3976edc6bad591bca4cf4e254b479a905fb3',
    task.task_id, task.session_id, generation.generation_id, admission.id,
    action.id, run.id, task.revision, admission.sequence,
    length(admission.recovered_incident_packet),
    'rearm_exact_archived_task_for_common_non_model_admission_continuation',
    'applied', CURRENT_TIMESTAMP
FROM dcp_review_lab_policy_task task
JOIN sessions session ON session.id = task.session_id
JOIN dcp_review_lab_admission admission ON admission.id = task.admission_id
JOIN dcp_wbc_readmission_generation generation
  ON generation.task_id = task.task_id AND generation.admission_id = admission.id
JOIN dcp_model_action action ON action.id = generation.review_action_id
JOIN review_run run ON run.id = generation.review_run_id
JOIN dcp_wbc_readmission_waiting_recovery_v1 prior
  ON prior.task_id = task.task_id AND prior.admission_id = admission.id
WHERE task.task_id = 'wbc-canary-v1'
  AND task.payload_digest = '3124b0ac5e50843ae2cec4ad8500ee70666cd7a65ff16554fd7fd5d204cba901'
  AND task.target = 'wb-core' AND task.profile = 'repo-only'
  AND task.repository = 'orenvlad-ai/wb-core'
  AND task.policy_version = 'dcp.wb-core.repo-only.release-train/v1'
  AND task.session_id = 'wb-core-1' AND task.card_number = 1
  AND task.worktree_path = '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/wb-core/wb-core-1'
  AND task.source_branch = 'ao/wb-core-1/root'
  AND task.state = 'admission_waiting' AND task.revision = 22 AND task.repair_count = 0
  AND task.pr_url = 'https://github.com/orenvlad-ai/wb-core/pull/987' AND task.pr_number = 987
  AND task.current_head_sha = '26044c696651ce5873748ec3f920d40e77c5686c'
  AND task.previous_head_sha = 'e8cca45f3995b8181fe81ead154f7a933dbacbe8'
  AND task.review_run_id = '18c54338-df31-4471-a344-4db6648ff4e3'
  AND task.admission_id = 'dcp-admission-18c54338-df31-4471-a344-4db6648ff4e3'
  AND task.release_phase = '' AND task.merge_commit_sha = ''
  AND task.error_code = '' AND task.incident_packet = ''
  AND session.project_id = task.target AND session.num = task.card_number
  AND session.kind = 'worker' AND session.harness = 'codex'
  AND session.display_name = 'DCP:wbc-canary-v1'
  AND session.activity_state = 'exited' AND session.is_terminated = 1
  AND session.runtime_launch_id = '' AND session.branch = task.source_branch
  AND session.workspace_path = task.worktree_path
  AND session.prompt = 'DCP repo-only task ' || task.task_id || ': ' || task.prompt
  AND generation.sequence = 1
  AND generation.generation_id = 'dcp-wbc-readmission-wbc-canary-v1-5319010312'
  AND generation.marker_version = 'wb-core.dcp-release-handoff/v1'
  AND generation.repository = task.repository AND generation.base_branch = 'main'
  AND generation.scope = task.profile AND generation.head_ref = task.source_branch
  AND generation.session_id = task.session_id AND generation.session_number = task.card_number
  AND generation.pr_url = task.pr_url AND generation.pr_number = task.pr_number
  AND generation.admitted_head_sha = task.previous_head_sha
  AND generation.new_head_sha = task.current_head_sha
  AND generation.status = 'admitted' AND generation.error_code = ''
  AND generation.lease_id = 'dcp-wbc-readmission-lease-1'
  AND generation.review_action_id = 'dcp-model-wbc-canary-v1-readmission-1-review-1'
  AND generation.review_run_id = task.review_run_id
  AND generation.admission_id = task.admission_id
  AND admission.sequence = 32 AND admission.status = 'waiting'
  AND admission.review_run_id = task.review_run_id AND admission.session_id = task.session_id
  AND admission.pr_url = task.pr_url AND admission.pr_number = task.pr_number
  AND admission.target_sha = task.current_head_sha
  AND admission.lease_id = '' AND admission.admitted_base_sha = ''
  AND admission.error_code = '' AND admission.incident_packet = ''
  AND admission.recovered_incident_packet = prior.prior_incident_packet
  AND action.sequence = 73 AND action.task_id = task.task_id
  AND action.session_id = task.session_id AND action.kind = 'reviewer'
  AND action.exact_head_sha = task.current_head_sha
  AND action.status = 'succeeded' AND action.slot = 0
  AND action.review_run_id = task.review_run_id AND action.error_code = ''
  AND run.session_id = task.session_id AND run.pr_url = task.pr_url
  AND run.target_sha = task.current_head_sha AND run.harness = 'codex'
  AND run.status = 'complete' AND run.verdict = 'approved'
  AND run.body <> '' AND run.github_review_id = ''
  AND prior.recovery_id = 'wbc-canary-v1-readmission-waiting-recovery'
  AND prior.prior_task_revision = 21 AND prior.prior_admission_sequence = 32
  AND prior.prior_error_code = 'waiting_identity_drift' AND prior.status = 'applied'
  AND (SELECT COUNT(*) FROM dcp_review_lab_policy_task) = 27
  AND (SELECT COUNT(*) FROM sessions) = 44
  AND (SELECT COUNT(*) FROM dcp_model_action) = 73
  AND (SELECT COUNT(*) FROM dcp_model_action WHERE status IN ('claimed','running')) = 0
  AND (SELECT COUNT(*) FROM review_run) = 46
  AND (SELECT COUNT(*) FROM dcp_review_lab_admission) = 32
  AND (SELECT COUNT(*) FROM dcp_wbc_readmission_generation) = 1
  AND (SELECT COUNT(*) FROM dcp_model_action WHERE task_id = task.task_id) = 3
  AND (SELECT COUNT(*) FROM review_run WHERE session_id = task.session_id) = 2
  AND (SELECT COUNT(*) FROM dcp_review_lab_admission WHERE session_id = task.session_id) = 2;

CREATE TABLE dcp_task_first_native_lifecycle_recovery_v1_guard (
    existing_task_rows INTEGER NOT NULL,
    recovery_rows      INTEGER NOT NULL,
    CHECK (existing_task_rows = 0 OR (existing_task_rows = 1 AND recovery_rows = 1))
);
INSERT INTO dcp_task_first_native_lifecycle_recovery_v1_guard
SELECT
    (SELECT COUNT(*) FROM dcp_review_lab_policy_task WHERE task_id = 'wbc-canary-v1'),
    (SELECT COUNT(*) FROM dcp_task_first_native_lifecycle_recovery_v1);
DROP TABLE dcp_task_first_native_lifecycle_recovery_v1_guard;

UPDATE dcp_review_lab_admission
SET updated_at = CURRENT_TIMESTAMP
WHERE id = 'dcp-admission-18c54338-df31-4471-a344-4db6648ff4e3'
  AND status = 'waiting' AND lease_id = ''
  AND EXISTS (
    SELECT 1 FROM dcp_task_first_native_lifecycle_recovery_v1 recovery
    WHERE recovery.admission_id = dcp_review_lab_admission.id
      AND recovery.status = 'applied'
  );

UPDATE dcp_review_lab_policy_task
SET revision = revision + 1, updated_at = CURRENT_TIMESTAMP
WHERE task_id = 'wbc-canary-v1' AND state = 'admission_waiting' AND revision = 22
  AND EXISTS (
    SELECT 1 FROM dcp_task_first_native_lifecycle_recovery_v1 recovery
    WHERE recovery.task_id = dcp_review_lab_policy_task.task_id
      AND recovery.status = 'applied'
  );

-- +goose StatementBegin
CREATE TRIGGER dcp_task_first_native_lifecycle_recovery_v1_no_update
BEFORE UPDATE ON dcp_task_first_native_lifecycle_recovery_v1
BEGIN
    SELECT RAISE(ABORT, 'DCP task-first lifecycle recovery is immutable evidence');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_task_first_native_lifecycle_recovery_v1_no_delete
BEFORE DELETE ON dcp_task_first_native_lifecycle_recovery_v1
BEGIN
    SELECT RAISE(ABORT, 'DCP task-first lifecycle recovery cannot be deleted');
END;
-- +goose StatementEnd

-- +goose Down
SELECT RAISE(ABORT, '0083 DCP task-first native lifecycle recovery is immutable evidence');
