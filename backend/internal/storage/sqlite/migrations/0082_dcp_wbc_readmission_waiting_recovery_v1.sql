-- +goose Up
-- Preserve the exact post-enqueue waiting_identity_drift incident and re-arm
-- the same admission/readmission binding. This migration creates no task,
-- action, review, admission, generation, release or provider fact.
--
-- A historical foreign field profile records migrations 0050/0051 as applied
-- without their physical admission table. Re-materialize only that absent
-- final schema before the recovery SELECT names it. Healthy databases are a
-- no-op here, and the compatibility profile receives no admission row.
CREATE TABLE IF NOT EXISTS dcp_review_lab_admission (
    sequence             INTEGER PRIMARY KEY AUTOINCREMENT,
    id                   TEXT NOT NULL UNIQUE CHECK (length(id) > 0),
    review_run_id        TEXT NOT NULL UNIQUE REFERENCES review_run (id) ON DELETE RESTRICT,
    review_id            TEXT NOT NULL CHECK (length(review_id) > 0),
    session_id           TEXT NOT NULL REFERENCES sessions (id) ON DELETE RESTRICT,
    pr_url               TEXT NOT NULL CHECK (length(pr_url) > 0),
    pr_number            INTEGER NOT NULL CHECK (pr_number > 0),
    target_sha           TEXT NOT NULL CHECK (length(target_sha) = 40),
    review_base_sha      TEXT NOT NULL CHECK (length(review_base_sha) = 40),
    admitted_base_sha    TEXT NOT NULL DEFAULT '' CHECK (admitted_base_sha = '' OR length(admitted_base_sha) = 40),
    status               TEXT NOT NULL CHECK (status IN ('waiting', 'claimed', 'refreshing', 'succeeded', 'failed', 'incident')),
    lease_id             TEXT NOT NULL DEFAULT '',
    merge_commit_sha     TEXT NOT NULL DEFAULT '' CHECK (merge_commit_sha = '' OR length(merge_commit_sha) = 40),
    error_code           TEXT NOT NULL DEFAULT '',
    incident_packet      TEXT NOT NULL DEFAULT '' CHECK (
        incident_packet = '' OR (
            json_valid(incident_packet)
            AND json_type(incident_packet) = 'object'
            AND json_extract(incident_packet, '$.schemaVersion') = 'dcp.review-lab.arbiter-needed/v1'
        )
    ),
    refresh_wake_count   INTEGER NOT NULL DEFAULT 0 CHECK (refresh_wake_count IN (0, 1)),
    created_at           TIMESTAMP NOT NULL,
    updated_at           TIMESTAMP NOT NULL CHECK (updated_at >= created_at),
    recovered_incident_packet TEXT NOT NULL DEFAULT '' CHECK (
        recovered_incident_packet = '' OR (
            json_valid(recovered_incident_packet)
            AND json_type(recovered_incident_packet) = 'object'
            AND json_extract(recovered_incident_packet, '$.schemaVersion') = 'dcp.review-lab.arbiter-needed/v1'
        )
    ),
    CHECK ((status = 'waiting' AND lease_id = '') OR (status <> 'waiting' AND length(lease_id) > 0)),
    CHECK ((status = 'incident') = (incident_packet <> '')),
    CHECK ((status = 'succeeded') = (merge_commit_sha <> ''))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_dcp_review_lab_admission_one_claim
    ON dcp_review_lab_admission (status) WHERE status = 'claimed';
CREATE UNIQUE INDEX IF NOT EXISTS idx_dcp_review_lab_admission_one_active_per_session
    ON dcp_review_lab_admission (session_id)
    WHERE status IN ('waiting', 'claimed', 'refreshing');
CREATE INDEX IF NOT EXISTS idx_dcp_review_lab_admission_fifo
    ON dcp_review_lab_admission (status, sequence);
CREATE INDEX IF NOT EXISTS idx_dcp_review_lab_admission_session
    ON dcp_review_lab_admission (session_id, sequence);

CREATE TABLE dcp_wbc_readmission_waiting_recovery_v1 (
    recovery_id             TEXT PRIMARY KEY CHECK (recovery_id = 'wbc-canary-v1-readmission-waiting-recovery'),
    contract_commit         TEXT NOT NULL CHECK (contract_commit = '4f7775f375a612a38e96496f09908ab48e3598c5'),
    source_commit           TEXT NOT NULL CHECK (source_commit = '2accc566f19a2ab0d1f99e70ba9e4cfa01fd0925'),
    task_id                 TEXT NOT NULL UNIQUE REFERENCES dcp_review_lab_policy_task (task_id) ON DELETE RESTRICT,
    session_id              TEXT NOT NULL UNIQUE REFERENCES sessions (id) ON DELETE RESTRICT,
    generation_id           TEXT NOT NULL UNIQUE REFERENCES dcp_wbc_readmission_generation (generation_id) ON DELETE RESTRICT,
    admission_id            TEXT NOT NULL UNIQUE REFERENCES dcp_review_lab_admission (id) ON DELETE RESTRICT,
    review_action_id        TEXT NOT NULL UNIQUE REFERENCES dcp_model_action (id) ON DELETE RESTRICT,
    review_run_id           TEXT NOT NULL UNIQUE REFERENCES review_run (id) ON DELETE RESTRICT,
    prior_task_revision     INTEGER NOT NULL CHECK (prior_task_revision = 21),
    prior_admission_sequence INTEGER NOT NULL CHECK (prior_admission_sequence = 32),
    prior_error_code        TEXT NOT NULL CHECK (prior_error_code = 'waiting_identity_drift'),
    prior_incident_packet   TEXT NOT NULL CHECK (prior_incident_packet = '{"schemaVersion":"dcp.review-lab.arbiter-needed/v1","reason":"waiting_identity_drift","repository":"orenvlad-ai/wb-core","admissionId":"dcp-admission-18c54338-df31-4471-a344-4db6648ff4e3","leaseId":"dcp-incident-dcp-admission-18c54338-df31-4471-a344-4db6648ff4e3","sequence":32,"sessionId":"wb-core-1","taskDisplayName":"DCP:wbc-canary-v1","sourceBranch":"ao/wb-core-1/root","reviewId":"6080d386-5582-443b-a848-afe804acf41c","reviewRunId":"18c54338-df31-4471-a344-4db6648ff4e3","prUrl":"https://github.com/orenvlad-ai/wb-core/pull/987","prNumber":987,"targetSha":"26044c696651ce5873748ec3f920d40e77c5686c","reviewBaseSha":"f731a7b9d51907314ce435def3709c0e104c4b60","currentBaseSha":"f731a7b9d51907314ce435def3709c0e104c4b60","providerMergeable":"","providerMergeStateStatus":"","evidenceDigest":"d5b12a729b8fa7ab2dea03cbc72401416c501177d5fc005bef7437e028ccb39c","recordedAt":"2026-08-18T14:44:49.590085Z"}'),
    authority               TEXT NOT NULL CHECK (authority = 'resume_exact_bound_wbc_readmission_admission_zero_new_model_or_release_authority'),
    status                  TEXT NOT NULL CHECK (status = 'applied'),
    created_at              TIMESTAMP NOT NULL
);

INSERT INTO dcp_wbc_readmission_waiting_recovery_v1 (
    recovery_id, contract_commit, source_commit, task_id, session_id,
    generation_id, admission_id, review_action_id, review_run_id,
    prior_task_revision, prior_admission_sequence, prior_error_code,
    prior_incident_packet, authority, status, created_at
)
SELECT
    'wbc-canary-v1-readmission-waiting-recovery',
    '4f7775f375a612a38e96496f09908ab48e3598c5',
    '2accc566f19a2ab0d1f99e70ba9e4cfa01fd0925',
    task.task_id, task.session_id, generation.generation_id, admission.id,
    action.id, run.id, task.revision, admission.sequence, task.error_code,
    task.incident_packet,
    'resume_exact_bound_wbc_readmission_admission_zero_new_model_or_release_authority',
    'applied', CURRENT_TIMESTAMP
FROM dcp_review_lab_policy_task task
JOIN sessions session ON session.id = task.session_id
JOIN dcp_review_lab_admission admission ON admission.id = task.admission_id
JOIN dcp_wbc_readmission_generation generation
  ON generation.task_id = task.task_id AND generation.admission_id = admission.id
JOIN dcp_model_action action ON action.id = generation.review_action_id
JOIN review_run run ON run.id = generation.review_run_id
JOIN pr ON pr.session_id = task.session_id AND pr.url = task.pr_url
JOIN pr_checks baseline ON baseline.pr_url = pr.url
  AND baseline.commit_hash = task.current_head_sha AND baseline.name = 'baseline'
WHERE task.task_id = 'wbc-canary-v1'
  AND task.payload_digest = '3124b0ac5e50843ae2cec4ad8500ee70666cd7a65ff16554fd7fd5d204cba901'
  AND task.target = 'wb-core' AND task.profile = 'repo-only'
  AND task.repository = 'orenvlad-ai/wb-core'
  AND task.policy_version = 'dcp.wb-core.repo-only.release-train/v1'
  AND task.session_id = 'wb-core-1' AND task.card_number = 1
  AND task.worktree_path = '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/wb-core/wb-core-1'
  AND task.source_branch = 'ao/wb-core-1/root'
  AND task.state = 'incident' AND task.revision = 21 AND task.repair_count = 0
  AND task.pr_url = 'https://github.com/orenvlad-ai/wb-core/pull/987' AND task.pr_number = 987
  AND task.current_head_sha = '26044c696651ce5873748ec3f920d40e77c5686c'
  AND task.previous_head_sha = 'e8cca45f3995b8181fe81ead154f7a933dbacbe8'
  AND task.review_run_id = '18c54338-df31-4471-a344-4db6648ff4e3'
  AND task.admission_id = 'dcp-admission-18c54338-df31-4471-a344-4db6648ff4e3'
  AND task.release_phase = '' AND task.merge_commit_sha = ''
  AND task.error_code = 'waiting_identity_drift'
  AND task.incident_packet = admission.incident_packet
  AND task.created_at = '2026-08-17 17:06:51.814478 +0000 UTC'
  AND task.updated_at = '2026-08-18 14:44:49.590085 +0000 UTC'
  AND session.project_id = 'wb-core' AND session.num = 1
  AND session.kind = 'worker' AND session.harness = 'codex'
  AND session.display_name = 'DCP:wbc-canary-v1'
  AND session.activity_state = 'exited' AND session.is_terminated = 1
  AND session.branch = task.source_branch AND session.workspace_path = task.worktree_path
  AND session.runtime_handle_id = 'wb-core-1'
  AND generation.sequence = 1
  AND generation.generation_id = 'dcp-wbc-readmission-wbc-canary-v1-5319010312'
  AND generation.marker_version = 'wb-core.dcp-release-handoff/v1'
  AND generation.repository = task.repository AND generation.base_branch = 'main'
  AND generation.scope = task.profile AND generation.head_ref = task.source_branch
  AND generation.session_id = task.session_id AND generation.session_number = task.card_number
  AND generation.pr_url = task.pr_url AND generation.pr_number = task.pr_number
  AND generation.admitted_head_sha = task.previous_head_sha
  AND generation.new_head_sha = task.current_head_sha
  AND generation.status = 'admitted'
  AND generation.lease_id = 'dcp-wbc-readmission-lease-1'
  AND generation.review_action_id = 'dcp-model-wbc-canary-v1-readmission-1-review-1'
  AND generation.review_run_id = task.review_run_id
  AND generation.admission_id = task.admission_id AND generation.error_code = ''
  AND admission.sequence = 32 AND admission.review_run_id = task.review_run_id
  AND admission.review_id = '6080d386-5582-443b-a848-afe804acf41c'
  AND admission.session_id = task.session_id AND admission.pr_url = task.pr_url
  AND admission.pr_number = task.pr_number AND admission.target_sha = task.current_head_sha
  AND admission.review_base_sha = 'f731a7b9d51907314ce435def3709c0e104c4b60'
  AND admission.admitted_base_sha = admission.review_base_sha
  AND admission.status = 'incident'
  AND admission.lease_id = 'dcp-incident-dcp-admission-18c54338-df31-4471-a344-4db6648ff4e3'
  AND admission.merge_commit_sha = '' AND admission.error_code = task.error_code
  AND admission.incident_packet = task.incident_packet
  AND admission.recovered_incident_packet = '' AND admission.refresh_wake_count = 0
  AND admission.created_at = '2026-08-18 14:44:49.582541 +0000 UTC'
  AND admission.updated_at = task.updated_at
  AND action.sequence = 73 AND action.task_id = task.task_id
  AND action.session_id = task.session_id AND action.kind = 'reviewer'
  AND action.exact_head_sha = task.current_head_sha
  AND action.status = 'succeeded' AND action.slot = 0
  AND action.review_run_id = task.review_run_id AND action.error_code = ''
  AND run.session_id = task.session_id AND run.pr_url = task.pr_url
  AND run.target_sha = task.current_head_sha AND run.harness = 'codex'
  AND run.batch_id = '6576e119-6470-46e0-95cf-183fe60c607c'
  AND run.status = 'complete' AND run.verdict = 'approved'
  AND run.body <> '' AND run.github_review_id = ''
  AND pr.number = task.pr_number AND pr.pr_state = 'open'
  AND pr.provider = 'github' AND pr.host = 'github.com' AND pr.repo = task.repository
  AND pr.source_branch = task.source_branch AND pr.target_branch = 'main'
  AND pr.head_sha = task.current_head_sha AND pr.author = 'orenvlad-ai'
  AND pr.base_sha = admission.review_base_sha
  AND pr.provider_state = 'OPEN' AND pr.provider_mergeable = 'MERGEABLE'
  AND pr.provider_merge_state_status = 'BEHIND'
  AND pr.html_url = pr.url AND pr.is_draft = 0 AND pr.is_merged = 0 AND pr.is_closed = 0
  AND baseline.status = 'passed' AND baseline.conclusion = 'success'
  AND baseline.url = 'https://github.com/orenvlad-ai/wb-core/actions/runs/32129475530/job/95687221649'
  AND EXISTS (
    SELECT 1 FROM dcp_wbc_readmission_admission_recovery_v1 prior
    WHERE prior.task_id = task.task_id AND prior.generation_id = generation.generation_id
      AND prior.review_action_id = action.id AND prior.review_run_id = run.id
      AND prior.status = 'applied'
  )
  AND (SELECT COUNT(*) FROM dcp_review_lab_policy_task exact_task WHERE exact_task.task_id = task.task_id) = 1
  AND (SELECT COUNT(*) FROM sessions exact_session WHERE exact_session.id = task.session_id) = 1
  AND (SELECT COUNT(*) FROM dcp_model_action exact_action WHERE exact_action.task_id = task.task_id) = 3
  AND (SELECT COUNT(*) FROM dcp_model_action initial_worker WHERE initial_worker.task_id = task.task_id AND initial_worker.kind = 'initial_worker') = 1
  AND (SELECT COUNT(*) FROM review_run exact_run WHERE exact_run.session_id = task.session_id AND exact_run.target_sha = task.current_head_sha) = 1
  AND (SELECT COUNT(*) FROM dcp_review_lab_admission exact_admission WHERE exact_admission.session_id = task.session_id AND exact_admission.id = task.admission_id) = 1
  AND (SELECT COUNT(*) FROM dcp_wbc_readmission_generation exact_generation WHERE exact_generation.task_id = task.task_id) = 1
  AND (SELECT COUNT(*) FROM pr exact_pr WHERE exact_pr.session_id = task.session_id) = 1
  AND (SELECT COUNT(*) FROM pr_checks exact_baseline WHERE exact_baseline.pr_url = pr.url AND exact_baseline.commit_hash = task.current_head_sha AND exact_baseline.name = 'baseline') = 1;

CREATE TABLE dcp_wbc_readmission_waiting_recovery_v1_guard (
    existing_task_rows INTEGER NOT NULL,
    recovery_rows      INTEGER NOT NULL,
    CHECK (existing_task_rows = 0 OR (existing_task_rows = 1 AND recovery_rows = 1))
);
INSERT INTO dcp_wbc_readmission_waiting_recovery_v1_guard
SELECT
    (SELECT COUNT(*) FROM dcp_review_lab_policy_task WHERE task_id = 'wbc-canary-v1'),
    (SELECT COUNT(*) FROM dcp_wbc_readmission_waiting_recovery_v1);
DROP TABLE dcp_wbc_readmission_waiting_recovery_v1_guard;

UPDATE dcp_review_lab_admission
SET status = 'waiting', lease_id = '', admitted_base_sha = '',
    error_code = '', recovered_incident_packet = incident_packet,
    incident_packet = '', updated_at = CURRENT_TIMESTAMP
WHERE id = 'dcp-admission-18c54338-df31-4471-a344-4db6648ff4e3'
  AND status = 'incident' AND error_code = 'waiting_identity_drift'
  AND EXISTS (
    SELECT 1 FROM dcp_wbc_readmission_waiting_recovery_v1 recovery
    WHERE recovery.admission_id = dcp_review_lab_admission.id
      AND recovery.status = 'applied'
  );

DROP TRIGGER dcp_review_lab_policy_task_immutable;

UPDATE dcp_review_lab_policy_task
SET state = 'admission_waiting', revision = revision + 1,
    error_code = '', incident_packet = '', updated_at = CURRENT_TIMESTAMP
WHERE task_id = 'wbc-canary-v1' AND state = 'incident' AND revision = 21
  AND EXISTS (
    SELECT 1 FROM dcp_wbc_readmission_waiting_recovery_v1 recovery
    WHERE recovery.task_id = dcp_review_lab_policy_task.task_id
      AND recovery.status = 'applied'
  );

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
      (NEW.state = 'repair_queued'
       AND NEW.repair_count = OLD.repair_count + 1
       AND EXISTS (
         SELECT 1 FROM dcp_future_card_arbiter_v1 arb
         WHERE arb.task_id = OLD.task_id
           AND arb.source_packet_json = OLD.incident_packet
           AND arb.status = 'repair_queued'
           AND arb.repair_task_id = OLD.task_id
       )) OR
      (NEW.state = 'ci_waiting'
       AND NEW.repair_count = OLD.repair_count
       AND NEW.admission_id = '' AND NEW.review_run_id = ''
       AND NEW.previous_head_sha = OLD.current_head_sha
       AND NEW.current_head_sha <> OLD.current_head_sha
       AND NEW.error_code = '' AND NEW.incident_packet = ''
       AND EXISTS (
         SELECT 1 FROM dcp_wbc_readmission_generation generation
         WHERE generation.task_id = OLD.task_id
           AND generation.session_id = OLD.session_id
           AND generation.old_admission_id = OLD.admission_id
           AND generation.admitted_head_sha = OLD.current_head_sha
           AND generation.new_head_sha = NEW.current_head_sha
           AND generation.status = 'head_pushed'
       ))
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
CREATE TRIGGER dcp_wbc_readmission_waiting_recovery_v1_no_update
BEFORE UPDATE ON dcp_wbc_readmission_waiting_recovery_v1
BEGIN
    SELECT RAISE(ABORT, 'DCP WBC readmission waiting recovery is immutable evidence');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_wbc_readmission_waiting_recovery_v1_no_delete
BEFORE DELETE ON dcp_wbc_readmission_waiting_recovery_v1
BEGIN
    SELECT RAISE(ABORT, 'DCP WBC readmission waiting recovery cannot be deleted');
END;
-- +goose StatementEnd

-- +goose Down
SELECT RAISE(ABORT, '0082 DCP WBC readmission waiting recovery is immutable evidence');
