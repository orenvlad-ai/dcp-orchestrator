-- +goose Up
-- Preserve the exact first repo-only submit, its already-consumed worker and
-- its already-completed stock ReviewRun. The old CLI response projection made
-- the caller fail after creation, while the old project-name review gate did
-- not allocate the global reviewer action. This migration adds only truthful
-- accounting for that completed call and advances the same task to the normal
-- passive admission boundary. It launches no model, submit, push, admission,
-- lease, or merge.
CREATE TABLE dcp_real_target_submit_recovery_v1 (
    recovery_id              TEXT PRIMARY KEY CHECK (recovery_id = 'dcp-real-target-submit-recovery-efe6a81cfff28be89cc327bdc9e2380ca585fcc6b03064c0290b6aaf4c7b59fe'),
    contract_commit          TEXT NOT NULL CHECK (contract_commit = 'cf6c39fb46257da0c6dd7c856d52381fd5ca59ac'),
    predecessor_source       TEXT NOT NULL CHECK (predecessor_source = '9162d4c0eca9efd2a3d9fe1ad09d640c40738c47'),
    predecessor_tree         TEXT NOT NULL CHECK (predecessor_tree = 'ec8e4c6d613e5e503a2582955b40bb8f104f76ce'),
    predecessor_receipt      TEXT NOT NULL CHECK (predecessor_receipt = '5cb06d6edaeb70080999f531da76109936732a57bee8262d9c0cf0af1b7ce295'),
    task_id                  TEXT NOT NULL UNIQUE CHECK (task_id = 'price-arch-v1'),
    payload_digest           TEXT NOT NULL CHECK (payload_digest = 'efe6a81cfff28be89cc327bdc9e2380ca585fcc6b03064c0290b6aaf4c7b59fe'),
    session_id               TEXT NOT NULL UNIQUE CHECK (session_id = 'wb-price-extension-1'),
    card_number              INTEGER NOT NULL CHECK (card_number = 1),
    worktree_path            TEXT NOT NULL CHECK (worktree_path = '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/wb-price-extension/wb-price-extension-1'),
    source_branch            TEXT NOT NULL CHECK (source_branch = 'ao/wb-price-extension-1/root'),
    prior_state              TEXT NOT NULL CHECK (prior_state = 'ci_waiting'),
    prior_revision           INTEGER NOT NULL CHECK (prior_revision = 4),
    worker_action_sequence   INTEGER NOT NULL CHECK (worker_action_sequence = 59),
    worker_action_id         TEXT NOT NULL UNIQUE CHECK (worker_action_id = 'dcp-model-price-arch-v1-worker-1'),
    worker_launch_id         TEXT NOT NULL CHECK (worker_launch_id = 'b815e3d2-0dc2-48fe-b9bd-c2bb0b426bcd'),
    worker_token_count       INTEGER NOT NULL CHECK (worker_token_count = 27373),
    reviewer_action_sequence INTEGER NOT NULL CHECK (reviewer_action_sequence = 60),
    reviewer_action_id       TEXT NOT NULL UNIQUE CHECK (reviewer_action_id = 'dcp-model-price-arch-v1-review-1'),
    reviewer_handle_id       TEXT NOT NULL CHECK (reviewer_handle_id = 'review-wb-price-extension-1'),
    reviewer_token_count     INTEGER NOT NULL CHECK (reviewer_token_count = 20512),
    review_id                TEXT NOT NULL UNIQUE CHECK (review_id = 'f754d155-faad-4a6b-8a03-53a3b93b11b8'),
    review_run_id            TEXT NOT NULL UNIQUE CHECK (review_run_id = 'b0acfb9e-600c-4816-bb2f-02a67817ea05'),
    review_batch_id          TEXT NOT NULL UNIQUE CHECK (review_batch_id = '6b097406-b9bc-42e5-90fb-2b82180e9458'),
    review_summary_digest    TEXT NOT NULL CHECK (review_summary_digest = '2daeb2a36bfd934c75db878aba9b02d66a1d0cf44a2bd94bedb60e55796c76b4'),
    review_started_at        TIMESTAMP NOT NULL,
    review_completed_at      TIMESTAMP NOT NULL CHECK (review_completed_at >= review_started_at),
    pr_url                   TEXT NOT NULL UNIQUE CHECK (pr_url = 'https://github.com/orenvlad-ai/wb-price-extension/pull/1'),
    pr_number                INTEGER NOT NULL CHECK (pr_number = 1),
    pr_head                  TEXT NOT NULL CHECK (pr_head = 'afc748eba5ff05c0dc24d3002c690ec9f44984fb'),
    pr_base                  TEXT NOT NULL CHECK (pr_base = '9522cfb633f9b3f5a87298f4f1dcce902bb7ebfd'),
    check_url                TEXT NOT NULL CHECK (check_url = 'https://github.com/orenvlad-ai/wb-price-extension/actions/runs/31896051686/job/95039422914'),
    prior_updated_at         TIMESTAMP NOT NULL,
    recovered_at             TIMESTAMP NOT NULL CHECK (recovered_at >= prior_updated_at)
);

CREATE UNIQUE INDEX idx_dcp_real_target_submit_recovery_v1_one
    ON dcp_real_target_submit_recovery_v1 ((1));

INSERT INTO dcp_real_target_submit_recovery_v1 (
    recovery_id, contract_commit, predecessor_source, predecessor_tree,
    predecessor_receipt, task_id, payload_digest, session_id, card_number,
    worktree_path, source_branch, prior_state, prior_revision,
    worker_action_sequence, worker_action_id, worker_launch_id,
    worker_token_count, reviewer_action_sequence, reviewer_action_id,
    reviewer_handle_id, reviewer_token_count, review_id, review_run_id,
    review_batch_id, review_summary_digest, review_started_at,
    review_completed_at, pr_url, pr_number, pr_head, pr_base, check_url,
    prior_updated_at, recovered_at
)
SELECT
    'dcp-real-target-submit-recovery-efe6a81cfff28be89cc327bdc9e2380ca585fcc6b03064c0290b6aaf4c7b59fe',
    'cf6c39fb46257da0c6dd7c856d52381fd5ca59ac',
    '9162d4c0eca9efd2a3d9fe1ad09d640c40738c47',
    'ec8e4c6d613e5e503a2582955b40bb8f104f76ce',
    '5cb06d6edaeb70080999f531da76109936732a57bee8262d9c0cf0af1b7ce295',
    task.task_id, task.payload_digest, task.session_id, task.card_number,
    task.worktree_path, task.source_branch, task.state, task.revision,
    action.sequence, action.id, action.launch_id, 27373, 60,
    'dcp-model-price-arch-v1-review-1', review.reviewer_handle_id, 20512,
    review.id, run.id, '6b097406-b9bc-42e5-90fb-2b82180e9458',
    '2daeb2a36bfd934c75db878aba9b02d66a1d0cf44a2bd94bedb60e55796c76b4',
    run.created_at, review.updated_at, pr.url, pr.number, pr.head_sha,
    pr.base_sha, checks.url, task.updated_at, CURRENT_TIMESTAMP
FROM dcp_review_lab_policy_task task
JOIN sessions session ON session.id = task.session_id
JOIN dcp_model_action action ON action.task_id = task.task_id
JOIN pr ON pr.session_id = task.session_id
JOIN pr_checks checks ON checks.pr_url = pr.url
JOIN review ON review.session_id = task.session_id
JOIN review_run run ON run.session_id = task.session_id AND run.review_id = review.id
WHERE task.task_id = 'price-arch-v1'
  AND task.payload_digest = 'efe6a81cfff28be89cc327bdc9e2380ca585fcc6b03064c0290b6aaf4c7b59fe'
  AND task.target = 'wb-price-extension' AND task.profile = 'repo-only'
  AND task.repository = 'orenvlad-ai/wb-price-extension'
  AND task.policy_version = 'dcp.repo-only.happy-path/v1'
  AND task.session_id = 'wb-price-extension-1' AND task.card_number = 1
  AND task.worktree_path = '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/wb-price-extension/wb-price-extension-1'
  AND task.source_branch = 'ao/wb-price-extension-1/root'
  AND length(CAST(task.prompt AS BLOB)) = 510
  AND json_extract(task.payload_json, '$.schemaVersion') = task.policy_version
  AND json_extract(task.payload_json, '$.taskId') = task.task_id
  AND json_extract(task.payload_json, '$.target') = task.target
  AND json_extract(task.payload_json, '$.profile') = task.profile
  AND json_extract(task.payload_json, '$.repository') = task.repository
  AND json_extract(task.payload_json, '$.prompt') = task.prompt
  AND task.state = 'ci_waiting' AND task.revision = 4 AND task.repair_count = 0
  AND task.pr_url = '' AND task.pr_number = 0
  AND task.current_head_sha = '' AND task.previous_head_sha = ''
  AND task.review_run_id = '' AND task.admission_id = ''
  AND task.merge_commit_sha = '' AND task.error_code = ''
  AND task.incident_packet = ''
  AND session.project_id = 'wb-price-extension' AND session.num = 1
  AND session.kind = 'worker' AND session.harness = 'codex'
  AND session.activity_state = 'idle' AND session.is_terminated = 0
  AND session.branch = task.source_branch
  AND session.workspace_path = task.worktree_path
  AND session.prompt = 'DCP repo-only task price-arch-v1: ' || task.prompt
  AND session.display_name = 'DCP:price-arch-v1'
  AND session.runtime_launch_id = '' AND session.agent_session_id = ''
  AND action.sequence = 59
  AND action.id = 'dcp-model-price-arch-v1-worker-1'
  AND action.session_id = task.session_id AND action.kind = 'initial_worker'
  AND action.exact_head_sha = '' AND action.status = 'succeeded'
  AND action.slot = 0
  AND action.launch_id = 'b815e3d2-0dc2-48fe-b9bd-c2bb0b426bcd'
  AND action.review_run_id = '' AND action.incident_id = ''
  AND action.error_code = ''
  AND pr.url = 'https://github.com/orenvlad-ai/wb-price-extension/pull/1'
  AND pr.number = 1 AND pr.pr_state = 'open'
  AND pr.review_decision = 'none' AND pr.ci_state = 'passing'
  AND pr.mergeability = 'mergeable' AND pr.provider = 'github'
  AND pr.host = 'github.com' AND pr.repo = task.repository
  AND pr.source_branch = task.source_branch AND pr.target_branch = 'main'
  AND pr.head_sha = 'afc748eba5ff05c0dc24d3002c690ec9f44984fb'
  AND pr.base_sha = '9522cfb633f9b3f5a87298f4f1dcce902bb7ebfd'
  AND pr.author = 'orenvlad-ai' AND pr.is_draft = 0
  AND pr.is_merged = 0 AND pr.is_closed = 0
  AND pr.provider_state = 'OPEN' AND pr.provider_mergeable = 'MERGEABLE'
  AND pr.provider_merge_state_status = 'CLEAN' AND pr.html_url = pr.url
  AND checks.name = 'baseline' AND checks.commit_hash = pr.head_sha
  AND checks.status = 'passed' AND checks.conclusion = 'success'
  AND checks.url = 'https://github.com/orenvlad-ai/wb-price-extension/actions/runs/31896051686/job/95039422914'
  AND checks.details = '95039422914'
  AND review.id = 'f754d155-faad-4a6b-8a03-53a3b93b11b8'
  AND review.project_id = task.target AND review.harness = 'codex'
  AND review.pr_url = ''
  AND review.reviewer_handle_id = 'review-wb-price-extension-1'
  AND run.id = 'b0acfb9e-600c-4816-bb2f-02a67817ea05'
  AND run.harness = 'codex' AND run.pr_url = pr.url
  AND run.target_sha = pr.head_sha AND run.status = 'complete'
  AND run.verdict = 'approved'
  AND run.body = 'Exact head confirmed. The architecture documentation defines coherent MV3 trust boundaries, confirmation and recovery behavior, security constraints, and a model-free test strategy. The baseline check passes.'
  AND run.github_review_id = '' AND run.delivered_at IS NULL
  AND (SELECT count(*) FROM dcp_review_lab_policy_task t WHERE t.target = 'wb-price-extension') = 1
  AND (SELECT count(*) FROM dcp_model_action a WHERE a.task_id = task.task_id) = 1
  AND (SELECT max(a.sequence) FROM dcp_model_action a) = 59
  AND (SELECT count(*) FROM dcp_model_action a WHERE a.status IN ('claimed', 'running')) = 0
  AND (SELECT count(*) FROM pr p WHERE p.session_id = task.session_id) = 1
  AND (SELECT count(*) FROM review_run r WHERE r.session_id = task.session_id) = 1;

CREATE TABLE dcp_real_target_submit_recovery_v1_up_guard (
    predecessor_rows INTEGER NOT NULL CHECK (predecessor_rows IN (0, 1)),
    recovery_rows    INTEGER NOT NULL CHECK (recovery_rows = predecessor_rows)
);
INSERT INTO dcp_real_target_submit_recovery_v1_up_guard
SELECT
    (SELECT count(*) FROM dcp_review_lab_policy_task
     WHERE task_id = 'price-arch-v1'
       AND payload_digest = 'efe6a81cfff28be89cc327bdc9e2380ca585fcc6b03064c0290b6aaf4c7b59fe'
       AND session_id = 'wb-price-extension-1' AND card_number = 1
       AND state = 'ci_waiting' AND revision = 4),
    (SELECT count(*) FROM dcp_real_target_submit_recovery_v1);

INSERT INTO dcp_model_action (
    sequence, id, task_id, session_id, kind, exact_head_sha, status, slot,
    launch_id, review_run_id, incident_id, error_code, created_at, updated_at
)
SELECT
    reviewer_action_sequence, reviewer_action_id, task_id, session_id,
    'reviewer', pr_head, 'succeeded', 0, reviewer_handle_id, review_run_id,
    '', '', review_started_at, review_completed_at
FROM dcp_real_target_submit_recovery_v1;

UPDATE dcp_review_lab_policy_task
SET state = 'admission_waiting', revision = 5,
    pr_url = (SELECT pr_url FROM dcp_real_target_submit_recovery_v1),
    pr_number = (SELECT pr_number FROM dcp_real_target_submit_recovery_v1),
    current_head_sha = (SELECT pr_head FROM dcp_real_target_submit_recovery_v1),
    review_run_id = (SELECT review_run_id FROM dcp_real_target_submit_recovery_v1),
    updated_at = CURRENT_TIMESTAMP
WHERE task_id = 'price-arch-v1' AND state = 'ci_waiting' AND revision = 4
  AND EXISTS (SELECT 1 FROM dcp_real_target_submit_recovery_v1);

CREATE TABLE dcp_real_target_submit_recovery_v1_result_guard (
    recovery_rows INTEGER NOT NULL CHECK (recovery_rows IN (0, 1)),
    reviewer_rows INTEGER NOT NULL CHECK (reviewer_rows = recovery_rows),
    recovered_rows INTEGER NOT NULL CHECK (recovered_rows = recovery_rows)
);
INSERT INTO dcp_real_target_submit_recovery_v1_result_guard
SELECT count(*),
       (SELECT count(*) FROM dcp_model_action
        WHERE sequence = 60
          AND id = 'dcp-model-price-arch-v1-review-1'
          AND task_id = 'price-arch-v1'
          AND session_id = 'wb-price-extension-1'
          AND kind = 'reviewer'
          AND exact_head_sha = 'afc748eba5ff05c0dc24d3002c690ec9f44984fb'
          AND status = 'succeeded' AND slot = 0
          AND launch_id = 'review-wb-price-extension-1'
          AND review_run_id = 'b0acfb9e-600c-4816-bb2f-02a67817ea05'
          AND incident_id = '' AND error_code = ''),
       (SELECT count(*) FROM dcp_review_lab_policy_task
        WHERE task_id = 'price-arch-v1' AND state = 'admission_waiting'
          AND revision = 5 AND repair_count = 0
          AND pr_url = 'https://github.com/orenvlad-ai/wb-price-extension/pull/1'
          AND pr_number = 1
          AND current_head_sha = 'afc748eba5ff05c0dc24d3002c690ec9f44984fb'
          AND previous_head_sha = ''
          AND review_run_id = 'b0acfb9e-600c-4816-bb2f-02a67817ea05'
          AND admission_id = '' AND merge_commit_sha = ''
          AND error_code = '' AND incident_packet = '')
FROM dcp_real_target_submit_recovery_v1;
DROP TABLE dcp_real_target_submit_recovery_v1_result_guard;
DROP TABLE dcp_real_target_submit_recovery_v1_up_guard;

-- +goose StatementBegin
CREATE TRIGGER dcp_real_target_submit_recovery_v1_no_update
BEFORE UPDATE ON dcp_real_target_submit_recovery_v1
BEGIN
    SELECT RAISE(ABORT, 'DCP real-target submit recovery is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_real_target_submit_recovery_v1_no_delete
BEFORE DELETE ON dcp_real_target_submit_recovery_v1
BEGIN
    SELECT RAISE(ABORT, 'DCP real-target submit recovery is immutable');
END;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
