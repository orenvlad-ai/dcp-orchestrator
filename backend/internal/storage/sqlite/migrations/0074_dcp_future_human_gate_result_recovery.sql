-- +goose Up
-- Preserve the exact Scenario-C validation failure before one model-free
-- acceptance of its unchanged HumanGate result. This grants zero model calls,
-- repair actions, reviews, admission transitions, or merges.
CREATE TABLE dcp_future_card_arbiter_human_gate_result_recovery_v1 (
    recovery_id              TEXT PRIMARY KEY CHECK (recovery_id = 'dcp-future-arbiter-human-gate-recovery-98e4d77336bdfc1539aa44932eacc45514adcbbf8600ff0483d0fb1fb1ed499a'),
    incident_id              TEXT NOT NULL UNIQUE REFERENCES dcp_future_card_arbiter_v1 (incident_id) ON DELETE RESTRICT,
    identity_digest          TEXT NOT NULL CHECK (identity_digest = '98e4d77336bdfc1539aa44932eacc45514adcbbf8600ff0483d0fb1fb1ed499a'),
    input_digest             TEXT NOT NULL CHECK (input_digest = '316d2739b62268d4f4c05b92ac21016e409297af5b7a9dd77aacd8207cfaa43b'),
    model_action_id          TEXT NOT NULL UNIQUE REFERENCES dcp_model_action (id) ON DELETE RESTRICT,
    prior_status             TEXT NOT NULL CHECK (prior_status = 'failed'),
    prior_error_code         TEXT NOT NULL CHECK (prior_error_code = 'submit_failed'),
    prior_finished_at        TIMESTAMP NOT NULL,
    prior_model_call_count   INTEGER NOT NULL CHECK (prior_model_call_count = 1),
    prior_decision_digest    TEXT NOT NULL CHECK (prior_decision_digest = ''),
    runtime_handle_id        TEXT NOT NULL CHECK (runtime_handle_id = incident_id),
    physical_runtime_handle  TEXT NOT NULL CHECK (physical_runtime_handle = 'dcp-future-arbiter-98e4d77336bdf-f4e0fd68'),
    input_artifact_digest    TEXT NOT NULL CHECK (input_artifact_digest = '566e59d251276599932d6d319210ae4b52412f66c48d864cf791163a408936be'),
    input_artifact_size      INTEGER NOT NULL CHECK (input_artifact_size = 3686),
    schema_artifact_digest   TEXT NOT NULL CHECK (schema_artifact_digest = '6ecfe4f335f927a7e76be9c755b52001537ac16d844164dd75fe86cfbaf920e4'),
    schema_artifact_size     INTEGER NOT NULL CHECK (schema_artifact_size = 1572),
    result_artifact_digest   TEXT NOT NULL CHECK (result_artifact_digest = '46c44780cee9a9f198e854890448400c9c208d920233f1acba399cadb5afdf5e'),
    result_artifact_size     INTEGER NOT NULL CHECK (result_artifact_size = 988),
    codex_session_id         TEXT NOT NULL CHECK (codex_session_id = '01a00318-9df3-7721-aa41-fcdc3a0ad00d'),
    inference_tokens         INTEGER NOT NULL CHECK (inference_tokens = 10430),
    contract_commit          TEXT NOT NULL CHECK (contract_commit = 'd8660bb43f15e95d1850cd26a553e30438062c1f'),
    status                   TEXT NOT NULL CHECK (status IN ('pending', 'applied', 'failed')),
    error_code               TEXT NOT NULL DEFAULT '',
    created_at               TIMESTAMP NOT NULL,
    finished_at              TIMESTAMP,
    CHECK ((status = 'pending') = (finished_at IS NULL)),
    CHECK ((status IN ('pending', 'applied')) = (error_code = ''))
);

INSERT INTO dcp_future_card_arbiter_human_gate_result_recovery_v1 (
    recovery_id, incident_id, identity_digest, input_digest, model_action_id,
    prior_status, prior_error_code, prior_finished_at, prior_model_call_count,
    prior_decision_digest, runtime_handle_id, physical_runtime_handle,
    input_artifact_digest, input_artifact_size,
    schema_artifact_digest, schema_artifact_size,
    result_artifact_digest, result_artifact_size,
    codex_session_id, inference_tokens, contract_commit, status, created_at
)
SELECT
    'dcp-future-arbiter-human-gate-recovery-98e4d77336bdfc1539aa44932eacc45514adcbbf8600ff0483d0fb1fb1ed499a',
    arb.incident_id, arb.identity_digest, arb.input_digest, arb.model_action_id,
    arb.status, arb.error_code, arb.finished_at, arb.model_call_count,
    arb.decision_digest, arb.runtime_handle_id,
    'dcp-future-arbiter-98e4d77336bdf-f4e0fd68',
    '566e59d251276599932d6d319210ae4b52412f66c48d864cf791163a408936be', 3686,
    '6ecfe4f335f927a7e76be9c755b52001537ac16d844164dd75fe86cfbaf920e4', 1572,
    '46c44780cee9a9f198e854890448400c9c208d920233f1acba399cadb5afdf5e', 988,
    '01a00318-9df3-7721-aa41-fcdc3a0ad00d', 10430,
    'd8660bb43f15e95d1850cd26a553e30438062c1f', 'pending', CURRENT_TIMESTAMP
FROM dcp_future_card_arbiter_v1 arb
JOIN dcp_model_action action ON action.id = arb.model_action_id
JOIN dcp_review_lab_policy_task task ON task.task_id = arb.task_id
WHERE arb.incident_id = 'dcp-future-arbiter-98e4d77336bdfc1539aa44932eacc45514adcbbf8600ff0483d0fb1fb1ed499a'
  AND arb.generation = 1
  AND arb.identity_digest = '98e4d77336bdfc1539aa44932eacc45514adcbbf8600ff0483d0fb1fb1ed499a'
  AND arb.task_id = 'arb-c-right' AND arb.session_id = 'dcp-review-lab-27'
  AND arb.admission_id = 'dcp-admission-21708ae5-acf7-4d33-9482-64b802a88868'
  AND arb.admission_sequence = 19
  AND arb.incident_lease_id = 'dcp-incident-dcp-admission-21708ae5-acf7-4d33-9482-64b802a88868'
  AND arb.incident_kind = 'merge_conflict_or_ambiguity'
  AND arb.source_packet_digest = '6a333bcdd2ece1b8700603874a8bdfe4b5cd7402d80ec22bf75225390ebd0094'
  AND arb.pr_url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/24' AND arb.pr_number = 24
  AND arb.candidate_head_sha = '58adc8c6abe1d2fee90cd1bfa9addd149cede1a8'
  AND arb.reviewed_base_sha = '7da2d78cb4ff6ab23538983a31d5d2196b32c470'
  AND arb.current_main_sha = 'e7056f5f0328e041f9f81aa420ab22f713acecdf'
  AND arb.review_run_id = '21708ae5-acf7-4d33-9482-64b802a88868'
  AND arb.affected_paths_json = '["qualification/arbiter-c.txt"]'
  AND arb.cohort_digest = '3429609c555b93d09edb459f0372d784afc92536f6268540b4664c801e9b5af7'
  AND arb.evidence_digest = 'd3353f9c90df2f2e9364d5302c5b3ab5676e5ec87c19aeb5d0fdedfc609e0432'
  AND arb.input_digest = '316d2739b62268d4f4c05b92ac21016e409297af5b7a9dd77aacd8207cfaa43b'
  AND arb.model_action_id = 'dcp-model-arb-c-right-arbiter-1'
  AND arb.runtime_handle_id = arb.incident_id
  AND arb.status = 'failed' AND arb.error_code = 'submit_failed'
  AND arb.model_call_count = 1 AND arb.decision_json = '' AND arb.decision_digest = ''
  AND arb.finished_at = '2026-08-15 01:45:55.723769 +0000 UTC'
  AND action.sequence = 41 AND action.task_id = arb.task_id AND action.session_id = arb.session_id
  AND action.kind = 'arbiter' AND action.exact_head_sha = arb.candidate_head_sha
  AND action.incident_id = arb.incident_id AND action.status = 'failed'
  AND action.error_code = 'submit_failed' AND action.slot = 0 AND action.launch_id = arb.incident_id
  AND task.payload_digest = '848076c4f163388464fe0af5e56b601aa679f71da2eaa363548d578e2501d01c'
  AND task.state = 'incident' AND task.revision = 10 AND task.repair_count = 0
  AND task.current_head_sha = arb.candidate_head_sha AND task.review_run_id = arb.review_run_id
  AND task.admission_id = arb.admission_id AND task.error_code = arb.incident_kind;

-- +goose StatementBegin
CREATE TRIGGER dcp_future_card_arbiter_human_gate_result_recovery_immutable
BEFORE UPDATE ON dcp_future_card_arbiter_human_gate_result_recovery_v1
WHEN OLD.recovery_id <> NEW.recovery_id
  OR OLD.incident_id <> NEW.incident_id
  OR OLD.identity_digest <> NEW.identity_digest
  OR OLD.input_digest <> NEW.input_digest
  OR OLD.model_action_id <> NEW.model_action_id
  OR OLD.prior_status <> NEW.prior_status
  OR OLD.prior_error_code <> NEW.prior_error_code
  OR OLD.prior_finished_at <> NEW.prior_finished_at
  OR OLD.prior_model_call_count <> NEW.prior_model_call_count
  OR OLD.prior_decision_digest <> NEW.prior_decision_digest
  OR OLD.runtime_handle_id <> NEW.runtime_handle_id
  OR OLD.physical_runtime_handle <> NEW.physical_runtime_handle
  OR OLD.input_artifact_digest <> NEW.input_artifact_digest
  OR OLD.input_artifact_size <> NEW.input_artifact_size
  OR OLD.schema_artifact_digest <> NEW.schema_artifact_digest
  OR OLD.schema_artifact_size <> NEW.schema_artifact_size
  OR OLD.result_artifact_digest <> NEW.result_artifact_digest
  OR OLD.result_artifact_size <> NEW.result_artifact_size
  OR OLD.codex_session_id <> NEW.codex_session_id
  OR OLD.inference_tokens <> NEW.inference_tokens
  OR OLD.contract_commit <> NEW.contract_commit
  OR OLD.created_at <> NEW.created_at
  OR NOT (OLD.status = 'pending' AND NEW.status IN ('applied', 'failed'))
BEGIN
    SELECT RAISE(ABORT, 'DCP future arbiter HumanGate result recovery is immutable');
END;
-- +goose StatementEnd

-- +goose Down
SELECT RAISE(ABORT, '0074 future-card arbiter HumanGate result recovery is immutable evidence');
