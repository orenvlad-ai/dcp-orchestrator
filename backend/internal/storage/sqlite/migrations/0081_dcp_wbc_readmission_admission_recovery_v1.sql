-- +goose Up
-- Preserve the exact first reviewed readmission admission false incident and
-- re-arm only its already-approved exact head for FIFO admission. The
-- migration creates no action, review, task, session, PR, branch or release
-- fact; the separate admission engine still owns every provider/release gate.
CREATE TABLE dcp_wbc_readmission_admission_recovery_v1 (
    recovery_id          TEXT PRIMARY KEY CHECK (recovery_id = 'wbc-canary-v1-readmission-admission-recovery'),
    contract_commit      TEXT NOT NULL CHECK (contract_commit = '4f7775f375a612a38e96496f09908ab48e3598c5'),
    task_id              TEXT NOT NULL UNIQUE REFERENCES dcp_review_lab_policy_task (task_id) ON DELETE RESTRICT,
    session_id           TEXT NOT NULL UNIQUE REFERENCES sessions (id) ON DELETE RESTRICT,
    generation_id        TEXT NOT NULL UNIQUE REFERENCES dcp_wbc_readmission_generation (generation_id) ON DELETE RESTRICT,
    review_action_id     TEXT NOT NULL UNIQUE REFERENCES dcp_model_action (id) ON DELETE RESTRICT,
    review_run_id        TEXT NOT NULL UNIQUE REFERENCES review_run (id) ON DELETE RESTRICT,
    pr_url               TEXT NOT NULL UNIQUE CHECK (pr_url = 'https://github.com/orenvlad-ai/wb-core/pull/987'),
    pr_number            INTEGER NOT NULL CHECK (pr_number = 987),
    head_sha             TEXT NOT NULL CHECK (head_sha = '26044c696651ce5873748ec3f920d40e77c5686c'),
    baseline_url         TEXT NOT NULL CHECK (baseline_url = 'https://github.com/orenvlad-ai/wb-core/actions/runs/32129475530/job/95687221649'),
    prior_task_state     TEXT NOT NULL CHECK (prior_task_state = 'incident'),
    prior_task_revision  INTEGER NOT NULL CHECK (prior_task_revision = 18),
    prior_error_code     TEXT NOT NULL CHECK (prior_error_code = 'admission_identity_drift'),
    prior_incident_packet TEXT NOT NULL CHECK (prior_incident_packet = '{"reason":"admission_identity_drift","recordedAt":"2026-08-18T13:30:26.357576Z","reviewRunId":"18c54338-df31-4471-a344-4db6648ff4e3","schemaVersion":"dcp.review-lab.policy-incident/v1","sessionId":"wb-core-1","targetSha":"26044c696651ce5873748ec3f920d40e77c5686c"}'),
    authority            TEXT NOT NULL CHECK (authority = 'resume_exact_reviewed_readmission_fifo_admission_zero_new_model_authority'),
    status               TEXT NOT NULL CHECK (status = 'applied'),
    created_at           TIMESTAMP NOT NULL
);

INSERT INTO dcp_wbc_readmission_admission_recovery_v1 (
    recovery_id, contract_commit, task_id, session_id, generation_id,
    review_action_id, review_run_id, pr_url, pr_number, head_sha,
    baseline_url, prior_task_state, prior_task_revision, prior_error_code,
    prior_incident_packet, authority, status, created_at
)
SELECT
    'wbc-canary-v1-readmission-admission-recovery',
    '4f7775f375a612a38e96496f09908ab48e3598c5',
    task.task_id, task.session_id, generation.generation_id,
    action.id, run.id, task.pr_url, task.pr_number, task.current_head_sha,
    baseline.url, task.state, task.revision, task.error_code,
    task.incident_packet,
    'resume_exact_reviewed_readmission_fifo_admission_zero_new_model_authority',
    'applied', CURRENT_TIMESTAMP
FROM dcp_review_lab_policy_task task
JOIN sessions session ON session.id = task.session_id
JOIN dcp_wbc_readmission_generation generation ON generation.task_id = task.task_id
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
  AND task.state = 'incident' AND task.revision = 18 AND task.repair_count = 0
  AND task.pr_url = 'https://github.com/orenvlad-ai/wb-core/pull/987' AND task.pr_number = 987
  AND task.current_head_sha = '26044c696651ce5873748ec3f920d40e77c5686c'
  AND task.previous_head_sha = 'e8cca45f3995b8181fe81ead154f7a933dbacbe8'
  AND task.review_run_id = '18c54338-df31-4471-a344-4db6648ff4e3' AND task.admission_id = ''
  AND task.release_phase = '' AND task.merge_commit_sha = ''
  AND task.error_code = 'admission_identity_drift'
  AND task.incident_packet = '{"reason":"admission_identity_drift","recordedAt":"2026-08-18T13:30:26.357576Z","reviewRunId":"18c54338-df31-4471-a344-4db6648ff4e3","schemaVersion":"dcp.review-lab.policy-incident/v1","sessionId":"wb-core-1","targetSha":"26044c696651ce5873748ec3f920d40e77c5686c"}'
  AND task.created_at = '2026-08-17 17:06:51.814478 +0000 UTC'
  AND task.updated_at = '2026-08-18 13:30:26.357584 +0000 UTC'
  AND session.project_id = 'wb-core' AND session.num = 1
  AND session.kind = 'worker' AND session.harness = 'codex'
  AND session.display_name = 'DCP:wbc-canary-v1'
  AND session.activity_state = 'exited' AND session.is_terminated = 1
  AND session.branch = task.source_branch AND session.workspace_path = task.worktree_path
  AND session.runtime_handle_id = 'wb-core-1'
  AND generation.sequence = 1
  AND generation.generation_id = 'dcp-wbc-readmission-wbc-canary-v1-5319010312'
  AND generation.marker_version = 'wb-core.dcp-release-handoff/v1'
  AND generation.task_id = task.task_id AND generation.session_id = task.session_id
  AND generation.pr_url = task.pr_url AND generation.pr_number = task.pr_number
  AND generation.repository = task.repository AND generation.base_branch = 'main'
  AND generation.scope = task.profile AND generation.head_ref = task.source_branch
  AND generation.session_number = task.card_number
  AND generation.admitted_head_sha = task.previous_head_sha
  AND generation.new_head_sha = task.current_head_sha
  AND generation.status = 'reviewed'
  AND generation.lease_id = 'dcp-wbc-readmission-lease-1'
  AND generation.review_action_id = 'dcp-model-wbc-canary-v1-readmission-1-review-1'
  AND generation.review_run_id = task.review_run_id AND generation.admission_id = ''
  AND generation.error_code = ''
  AND action.sequence = 73 AND action.task_id = task.task_id
  AND action.session_id = task.session_id AND action.kind = 'reviewer'
  AND action.exact_head_sha = task.current_head_sha
  AND action.status = 'succeeded' AND action.slot = 0
  AND action.review_run_id = task.review_run_id AND action.error_code = ''
  AND run.session_id = task.session_id AND run.pr_url = task.pr_url
  AND run.target_sha = task.current_head_sha AND run.harness = 'codex'
  AND run.status = 'complete' AND run.verdict = 'approved'
  AND run.body <> '' AND run.github_review_id = ''
  AND pr.number = task.pr_number AND pr.pr_state = 'open'
  AND pr.provider = 'github' AND pr.host = 'github.com' AND pr.repo = task.repository
  AND pr.source_branch = task.source_branch AND pr.target_branch = 'main'
  AND pr.head_sha = task.current_head_sha AND pr.author = 'orenvlad-ai'
  AND pr.provider_state = 'OPEN' AND pr.provider_mergeable = 'MERGEABLE'
  AND pr.html_url = pr.url AND pr.is_draft = 0 AND pr.is_merged = 0 AND pr.is_closed = 0
  AND baseline.status = 'passed' AND baseline.conclusion = 'success'
  AND baseline.url = 'https://github.com/orenvlad-ai/wb-core/actions/runs/32129475530/job/95687221649'
  AND (SELECT COUNT(*) FROM dcp_review_lab_policy_task exact_task WHERE exact_task.task_id = task.task_id) = 1
  AND (SELECT COUNT(*) FROM sessions exact_session WHERE exact_session.id = task.session_id) = 1
  AND (SELECT COUNT(*) FROM dcp_model_action exact_action WHERE exact_action.task_id = task.task_id) = 3
  AND (SELECT COUNT(*) FROM dcp_model_action initial_worker WHERE initial_worker.task_id = task.task_id AND initial_worker.kind = 'initial_worker') = 1
  AND (SELECT COUNT(*) FROM review_run exact_run WHERE exact_run.session_id = task.session_id AND exact_run.target_sha = task.current_head_sha) = 1
  AND (SELECT COUNT(*) FROM dcp_wbc_readmission_generation exact_generation WHERE exact_generation.task_id = task.task_id) = 1
  AND (SELECT COUNT(*) FROM pr exact_pr WHERE exact_pr.session_id = task.session_id) = 1
  AND (SELECT COUNT(*) FROM pr_checks exact_baseline WHERE exact_baseline.pr_url = pr.url AND exact_baseline.commit_hash = task.current_head_sha AND exact_baseline.name = 'baseline') = 1;

CREATE TABLE dcp_wbc_readmission_admission_recovery_v1_guard (
    existing_task_rows INTEGER NOT NULL,
    recovery_rows      INTEGER NOT NULL,
    CHECK (existing_task_rows = 0 OR (existing_task_rows = 1 AND recovery_rows = 1))
);
INSERT INTO dcp_wbc_readmission_admission_recovery_v1_guard
SELECT
    (SELECT COUNT(*) FROM dcp_review_lab_policy_task WHERE task_id = 'wbc-canary-v1'),
    (SELECT COUNT(*) FROM dcp_wbc_readmission_admission_recovery_v1);
DROP TABLE dcp_wbc_readmission_admission_recovery_v1_guard;

DROP TRIGGER dcp_review_lab_policy_task_immutable;

UPDATE dcp_review_lab_policy_task
SET state = 'admission_waiting', revision = revision + 1,
    error_code = '', incident_packet = '', updated_at = CURRENT_TIMESTAMP
WHERE task_id = 'wbc-canary-v1' AND state = 'incident' AND revision = 18
  AND EXISTS (
    SELECT 1 FROM dcp_wbc_readmission_admission_recovery_v1 recovery
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
CREATE TRIGGER dcp_wbc_readmission_admission_recovery_v1_no_update
BEFORE UPDATE ON dcp_wbc_readmission_admission_recovery_v1
BEGIN
    SELECT RAISE(ABORT, 'DCP WBC readmission admission recovery is immutable evidence');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_wbc_readmission_admission_recovery_v1_no_delete
BEFORE DELETE ON dcp_wbc_readmission_admission_recovery_v1
BEGIN
    SELECT RAISE(ABORT, 'DCP WBC readmission admission recovery cannot be deleted');
END;
-- +goose StatementEnd

-- +goose Down
SELECT RAISE(ABORT, '0081 DCP WBC readmission admission recovery is immutable evidence');
