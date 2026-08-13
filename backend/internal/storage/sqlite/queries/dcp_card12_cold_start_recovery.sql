-- name: GetDCPCard12ColdStartRecovery :one
SELECT * FROM dcp_review_lab_card12_cold_start_recovery
WHERE recovery_id = ?;

-- name: ListDCPCard12ColdStartRecoveries :many
SELECT * FROM dcp_review_lab_card12_cold_start_recovery ORDER BY authorized_at;

-- name: CountExactDCPCard12ColdStartToolPathRecovery :one
SELECT count(*)
FROM dcp_card12_cold_start_tool_path_recovery audit
JOIN dcp_review_lab_card12_cold_start_recovery recovery
  ON recovery.recovery_id = audit.recovery_id
WHERE audit.correction_id = 'dcp-card12-cold-start-tool-path-recovery-a10a121ce3cf41afeeeda32396a190d6de725592570ae02d0d136f1d1cbba9e1'
  AND audit.recovery_id = 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
  AND audit.recovery_generation = 1
  AND audit.recovery_identity_digest = '087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
  AND audit.prior_status = 'failed'
  AND audit.prior_error_code = 'preflight_or_backup_failed'
  AND audit.prior_revision = 1
  AND audit.prior_worker_calls = 0 AND audit.prior_arbiter_calls = 0
  AND audit.prior_action_count = 0 AND audit.prior_reviewer_calls = 0
  AND audit.prior_backup_path = '' AND audit.prior_backup_digest = ''
  AND audit.prior_new_head = '' AND audit.prior_review_run_id = ''
  AND audit.prior_merge_commit_sha = ''
  AND audit.failed_source_sha = '032e16aa3025858eeddecc1a25e87d4ec8ea4f18'
  AND audit.failed_source_tree = 'cc519e93923e02d59463bbe14dd77192a237ce95'
  AND audit.rejected_tool_path = '/opt/homebrew/bin/gh'
  AND audit.physical_tool_path = '/opt/homebrew/Cellar/gh/2.87.2/bin/gh'
  AND audit.physical_tool_digest = 'f392d9ad8d2260c671566936b127f5436772ce16e25b091cf1fa7b301987f27e'
  AND audit.quarantine_rows = 2 AND audit.quarantine_verifications = 2
  AND audit.recovery_reason = 'trusted_gh_constant_named_symlink_not_physical_regular_file'
  AND recovery.status = 'authorized' AND recovery.revision = 2
  AND recovery.worker_model_call_count = 0
  AND recovery.arbiter_model_call_count = 0
  AND recovery.model_free_action_count = 0
  AND recovery.reviewer_model_call_count = 0
  AND recovery.backup_path = '' AND recovery.backup_digest = ''
  AND recovery.local_ref_after = '' AND recovery.new_head = ''
  AND recovery.recovery_review_run_id = ''
  AND recovery.merge_commit_sha = '' AND recovery.error_code = '';

-- name: CountExactDCPCard12ColdStartAutoMergeRecovery :one
SELECT count(*)
FROM dcp_card12_cold_start_auto_merge_recovery audit
JOIN dcp_card12_cold_start_tool_path_recovery tool
  ON tool.recovery_id = audit.recovery_id
JOIN dcp_review_lab_card12_cold_start_recovery recovery
  ON recovery.recovery_id = audit.recovery_id
WHERE audit.correction_id = 'dcp-card12-cold-start-auto-merge-recovery-e29a07a0b1aaddee25324e025ec23ab53b63007f78d76155ea79cef1bda52e79'
  AND audit.recovery_id = 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
  AND audit.recovery_generation = 1
  AND audit.recovery_identity_digest = '087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
  AND audit.prior_status = 'failed'
  AND audit.prior_error_code = 'preflight_or_backup_failed'
  AND audit.prior_revision = 3
  AND audit.prior_worker_calls = 0 AND audit.prior_arbiter_calls = 0
  AND audit.prior_action_count = 0 AND audit.prior_reviewer_calls = 0
  AND audit.prior_backup_path = '' AND audit.prior_backup_digest = ''
  AND audit.prior_local_ref_after = '' AND audit.prior_new_head = ''
  AND audit.prior_review_run_id = '' AND audit.prior_merge_commit_sha = ''
  AND audit.failed_source_sha = '798e9bfb8f75846d846f2ec2d4dfc9ec0076573b'
  AND audit.failed_source_tree = 'e5668c51fbc3c7aae872cafbe4759fc405fa0677'
  AND audit.residue_path = 'AUTO_MERGE'
  AND audit.auto_merge_tree = '3eba7b0dec18c759875b2b33a8d7d2379caaa6a1'
  AND audit.auto_merge_file_digest = 'dac6e5a895aed94e8cd5a0f1a39b1c23f0201393e621c635ed228070710c13ed'
  AND audit.auto_merge_conflict_blob = '1af18aad20e3aab90ea7f1c617d330abc3b08de9'
  AND audit.marker_digest = '5850bba009db75bf47ff88aef2d2cecbdba89c68967f51a8cdb60f48e968dc1a'
  AND audit.quarantine_rows = 2 AND audit.quarantine_verifications = 4
  AND audit.recovery_reason = 'exact_preserved_git_auto_merge_tree_was_misclassified_as_active_mutator'
  AND tool.correction_id = 'dcp-card12-cold-start-tool-path-recovery-a10a121ce3cf41afeeeda32396a190d6de725592570ae02d0d136f1d1cbba9e1'
  AND tool.prior_revision = 1
  AND tool.physical_tool_path = '/opt/homebrew/Cellar/gh/2.87.2/bin/gh'
  AND recovery.status = 'authorized' AND recovery.revision = 4
  AND recovery.worker_model_call_count = 0
  AND recovery.arbiter_model_call_count = 0
  AND recovery.model_free_action_count = 0
  AND recovery.reviewer_model_call_count = 0
  AND recovery.backup_path = '' AND recovery.backup_digest = ''
  AND recovery.local_ref_after = '' AND recovery.new_head = ''
  AND recovery.recovery_review_run_id = ''
  AND recovery.merge_commit_sha = '' AND recovery.error_code = '';

-- name: BootstrapDCPCard12ColdStartRecovery :execrows
INSERT INTO dcp_review_lab_card12_cold_start_recovery (
    recovery_id, generation, identity_digest, contract_commit,
    predecessor_continuation_id, incident_id, admission_id, session_id,
    task_id, project_id, repository, worktree_path, source_branch, pr_url,
    pr_number, old_head, current_main, provider_base, conflict_path,
    marker_digest, status_digest, stage1_blob, stage2_blob, stage3_blob,
    resolved_bytes_digest, resolved_blob, push_ref, push_lease_old_head,
    unauthorized_worker_thread_11, unauthorized_worker_thread_12,
    unauthorized_worker_tokens_11, unauthorized_worker_tokens_12,
    status, authorized_at, updated_at
)
SELECT
    'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f',
    1, '087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f',
    '623c3896a50d410e5b305ed08cf29abdc40b5b23', continuation.continuation_id,
    'dcp-global-release-2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60',
    admission.id, admission.session_id, 'i13-arbiter-b', 'dcp-review-lab',
    'orenvlad-ai/dcp-review-lab', session.workspace_path, session.branch,
    admission.pr_url, admission.pr_number, admission.target_sha,
    'b34b31b5443890e69128db2862726950a6bbac0d', admission.review_base_sha,
    'canary/i13-arbiter-conflict.txt',
    '5850bba009db75bf47ff88aef2d2cecbdba89c68967f51a8cdb60f48e968dc1a',
    'fd7d8ff8f4918e9960e5e46e01c70a877d4218b3fa1e884ecc1723935b1c9886',
    'ed237ce2dd2684371797e22634480ffb28dc9e77',
    'a4c945ba7328504f2efea44f076a1407c6aa7b47',
    '80a658c4cfc3ffda5786da316bc0bd10ffb1834f',
    '2a5da25a78ff8bcd9aff4493f195eaefecbc70c3d4db8902dda468ccf69e5e46',
    '80a658c4cfc3ffda5786da316bc0bd10ffb1834f',
    'refs/heads/ao/dcp-review-lab-12/root', admission.target_sha,
    '019ff9f3-cad3-73c1-bcee-293efe857349',
    '019ff9f3-cbe6-71e2-8636-ea6259a7e7d1', 33238, 33573,
    'authorized', sqlc.arg(now), sqlc.arg(now)
FROM dcp_review_lab_card12_model_free_rebase_continuation continuation
JOIN dcp_review_lab_admission admission ON admission.sequence = 4
JOIN sessions session ON session.id = admission.session_id
WHERE continuation.continuation_id = 'dcp-card12-model-free-rebase-continuation-66eb630c1995f90b37429a2f6c57c57794dda9fc98a29149c88bdb2f01131060'
  AND continuation.status = 'failed' AND continuation.error_code = 'identity_drift'
  AND continuation.revision = 1 AND continuation.model_free_action_count = 0
  AND continuation.reviewer_model_call_count = 0 AND continuation.new_head = ''
  AND continuation.merge_commit_sha = '' AND continuation.finished_at IS NOT NULL
  AND admission.id = 'dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34'
  AND admission.session_id = 'dcp-review-lab-12' AND admission.status = 'incident'
  AND admission.target_sha = 'd4fcb68051ae113ed497d02151a759800ee85633'
  AND admission.review_base_sha = 'dbaf01b05e85ffffa4c843a905e2fe5229eaf0da'
  AND admission.lease_id = 'dcp-incident-dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34'
  AND admission.error_code = 'merge_conflict_or_ambiguity'
  AND session.project_id = 'dcp-review-lab' AND session.num = 12
  AND session.kind = 'worker' AND session.harness = 'codex'
  AND session.activity_state = 'idle' AND session.is_terminated = 0
  AND session.branch = 'ao/dcp-review-lab-12/root'
  AND session.workspace_path = '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/dcp-review-lab/dcp-review-lab-12'
  AND session.runtime_handle_id = 'dcp-review-lab-12'
  AND session.agent_session_id = ''
  AND NOT EXISTS (SELECT 1 FROM dcp_review_lab_card12_cold_start_recovery);

-- name: BootstrapDCPCard11StartupQuarantine :execrows
INSERT INTO dcp_governed_startup_quarantine (
    session_id, recovery_id, classification, contract_commit,
    established_at, last_verified_at
)
SELECT 'dcp-review-lab-11', recovery.recovery_id, 'governed_terminal',
       recovery.contract_commit, sqlc.arg(now), sqlc.arg(now)
FROM dcp_review_lab_card12_cold_start_recovery recovery
JOIN dcp_review_lab_admission admission ON admission.sequence = 3
JOIN sessions session ON session.id = admission.session_id
WHERE admission.id = 'dcp-admission-841c6c1e-3dcd-4ffb-875e-c42dfa358919'
  AND admission.session_id = 'dcp-review-lab-11' AND admission.status = 'succeeded'
  AND admission.merge_commit_sha = 'b34b31b5443890e69128db2862726950a6bbac0d'
  AND session.project_id = 'dcp-review-lab' AND session.num = 11
  AND session.kind = 'worker' AND session.harness = 'codex'
  AND session.activity_state = 'idle' AND session.is_terminated = 0
  AND session.branch = 'ao/dcp-review-lab-11/root'
  AND session.workspace_path = '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/dcp-review-lab/dcp-review-lab-11'
  AND session.runtime_handle_id = 'dcp-review-lab-11'
  AND session.agent_session_id = ''
  AND NOT EXISTS (SELECT 1 FROM dcp_governed_startup_quarantine WHERE session_id = 'dcp-review-lab-11');

-- name: BootstrapDCPCard12StartupQuarantine :execrows
INSERT INTO dcp_governed_startup_quarantine (
    session_id, recovery_id, classification, contract_commit,
    established_at, last_verified_at
)
SELECT 'dcp-review-lab-12', recovery.recovery_id, 'governed_recovery',
       recovery.contract_commit, sqlc.arg(now), sqlc.arg(now)
FROM dcp_review_lab_card12_cold_start_recovery recovery
WHERE NOT EXISTS (SELECT 1 FROM dcp_governed_startup_quarantine WHERE session_id = 'dcp-review-lab-12');

-- name: CountExactDCPGovernedStartupQuarantine :one
SELECT count(*) FROM dcp_governed_startup_quarantine q
WHERE q.recovery_id = 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
  AND q.contract_commit = '623c3896a50d410e5b305ed08cf29abdc40b5b23'
  AND ((q.session_id = 'dcp-review-lab-11' AND q.classification = 'governed_terminal')
    OR (q.session_id = 'dcp-review-lab-12' AND q.classification = 'governed_recovery'))
  AND EXISTS (
    SELECT 1 FROM sessions s WHERE s.id = q.session_id
      AND s.project_id = 'dcp-review-lab' AND s.kind = 'worker'
      AND s.harness = 'codex' AND s.agent_session_id = ''
      AND s.activity_state = 'idle' AND s.is_terminated = 0
      AND s.runtime_handle_id = s.id
      AND s.branch = CASE q.session_id
        WHEN 'dcp-review-lab-11' THEN 'ao/dcp-review-lab-11/root'
        ELSE 'ao/dcp-review-lab-12/root' END
      AND s.workspace_path = CASE q.session_id
        WHEN 'dcp-review-lab-11' THEN '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/dcp-review-lab/dcp-review-lab-11'
        ELSE '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/dcp-review-lab/dcp-review-lab-12' END
      AND s.display_name = CASE q.session_id
        WHEN 'dcp-review-lab-11' THEN 'DCP:i13-arbiter-a'
        ELSE 'DCP:i13-arbiter-b' END
  )
  AND (q.session_id = 'dcp-review-lab-12' OR EXISTS (
    SELECT 1 FROM dcp_review_lab_admission terminal
    WHERE terminal.sequence = 3
      AND terminal.id = 'dcp-admission-841c6c1e-3dcd-4ffb-875e-c42dfa358919'
      AND terminal.session_id = 'dcp-review-lab-11'
      AND terminal.status = 'succeeded'
      AND terminal.merge_commit_sha = 'b34b31b5443890e69128db2862726950a6bbac0d'
  ))
  AND EXISTS (
    SELECT 1 FROM dcp_review_lab_card12_cold_start_recovery recovery
    WHERE recovery.recovery_id = q.recovery_id
      AND recovery.identity_digest = '087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
      AND recovery.worker_model_call_count = 0
      AND recovery.arbiter_model_call_count = 0
      AND recovery.unauthorized_worker_tokens_11 = 33238
      AND recovery.unauthorized_worker_tokens_12 = 33573
      AND recovery.model_free_action_count IN (0, 1)
      AND recovery.reviewer_model_call_count IN (0, 1)
      AND EXISTS (
        SELECT 1 FROM dcp_review_lab_card12_model_free_rebase_continuation predecessor
        WHERE predecessor.continuation_id = recovery.predecessor_continuation_id
          AND predecessor.status = 'failed' AND predecessor.error_code = 'identity_drift'
          AND predecessor.revision = 1 AND predecessor.model_free_action_count = 0
          AND predecessor.reviewer_model_call_count = 0
          AND predecessor.new_head = '' AND predecessor.merge_commit_sha = ''
      )
      AND EXISTS (
        SELECT 1 FROM dcp_review_lab_admission a
        WHERE a.sequence = 4 AND a.id = recovery.admission_id
          AND a.session_id = recovery.session_id AND a.pr_number = recovery.pr_number
          AND a.pr_url = recovery.pr_url AND a.merge_commit_sha = recovery.merge_commit_sha
      )
  );

-- name: TouchDCPGovernedStartupQuarantine :execrows
UPDATE dcp_governed_startup_quarantine
SET verification_count = verification_count + 1,
    last_verified_at = sqlc.arg(last_verified_at)
WHERE recovery_id = 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
  AND contract_commit = '623c3896a50d410e5b305ed08cf29abdc40b5b23';

-- name: ListDCPGovernedStartupQuarantine :many
SELECT session_id FROM dcp_governed_startup_quarantine
WHERE recovery_id = 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
ORDER BY session_id;

-- name: PersistDCPCard12ColdStartBackup :execrows
UPDATE dcp_review_lab_card12_cold_start_recovery
SET status = 'backed_up', backup_path = sqlc.arg(backup_path),
    backup_digest = sqlc.arg(backup_digest), revision = revision + 1,
    updated_at = sqlc.arg(updated_at)
WHERE recovery_id = sqlc.arg(recovery_id) AND status = 'authorized'
  AND revision = sqlc.arg(revision) AND worker_model_call_count = 0
  AND arbiter_model_call_count = 0 AND model_free_action_count = 0
  AND reviewer_model_call_count = 0 AND backup_path = '' AND backup_digest = ''
  AND error_code = '';

-- name: StartDCPCard12ColdStartRecovery :execrows
UPDATE dcp_review_lab_card12_cold_start_recovery
SET status = 'running', model_free_action_count = 1,
    local_ref_before = old_head, revision = revision + 1,
    updated_at = sqlc.arg(updated_at)
WHERE recovery_id = sqlc.arg(recovery_id) AND status = 'backed_up'
  AND revision = sqlc.arg(revision) AND worker_model_call_count = 0
  AND arbiter_model_call_count = 0 AND model_free_action_count = 0
  AND reviewer_model_call_count = 0 AND backup_path <> '' AND backup_digest <> ''
  AND local_ref_before = '' AND local_ref_after = '' AND new_head = ''
  AND recovery_review_run_id = '' AND merge_commit_sha = '' AND error_code = '';

-- name: CompleteDCPCard12ColdStartRecoveryAction :execrows
UPDATE dcp_review_lab_card12_cold_start_recovery
SET status = 'candidate_ready', local_ref_after = sqlc.arg(new_head),
    new_head = sqlc.arg(new_head), new_commit = sqlc.arg(new_head),
    provider_new_head = sqlc.arg(new_head), revision = revision + 1,
    updated_at = sqlc.arg(updated_at)
WHERE recovery_id = sqlc.arg(recovery_id) AND status = 'running'
  AND revision = sqlc.arg(revision) AND model_free_action_count = 1
  AND reviewer_model_call_count = 0 AND local_ref_before = old_head
  AND local_ref_after = '' AND new_head = '' AND recovery_review_run_id = ''
  AND merge_commit_sha = '' AND error_code = ''
  AND sqlc.arg(new_head) <> old_head AND length(sqlc.arg(new_head)) = 40;

-- name: FailDCPCard12ColdStartRecovery :execrows
UPDATE dcp_review_lab_card12_cold_start_recovery
SET status = 'failed', error_code = sqlc.arg(error_code),
    revision = revision + 1, updated_at = sqlc.arg(updated_at),
    finished_at = sqlc.arg(finished_at)
WHERE recovery_id = sqlc.arg(recovery_id)
  AND status IN ('authorized', 'backed_up', 'running')
  AND reviewer_model_call_count = 0 AND recovery_review_run_id = ''
  AND merge_commit_sha = '' AND error_code = '';

-- name: FenceDCPCard12ColdStartRecoveryReview :execrows
UPDATE dcp_review_lab_card12_cold_start_recovery
SET status = 'review_running', reviewer_model_call_count = 1,
    recovery_review_run_id = sqlc.arg(review_run_id),
    recovery_review_id = sqlc.arg(review_id),
    recovery_review_batch_id = sqlc.arg(batch_id),
    revision = revision + 1, updated_at = sqlc.arg(updated_at)
WHERE recovery_id = 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
  AND status = 'candidate_ready' AND model_free_action_count = 1
  AND reviewer_model_call_count = 0 AND session_id = sqlc.arg(session_id)
  AND pr_url = sqlc.arg(pr_url) AND new_head = sqlc.arg(target_sha)
  AND provider_new_head = sqlc.arg(target_sha)
  AND recovery_review_run_id = '' AND error_code = '';

-- name: FailDCPCard12ColdStartRecoveryReview :execrows
UPDATE dcp_review_lab_card12_cold_start_recovery
SET status = 'failed', error_code = sqlc.arg(error_code),
    revision = revision + 1, updated_at = sqlc.arg(updated_at),
    finished_at = sqlc.arg(finished_at)
WHERE recovery_id = sqlc.arg(recovery_id) AND status = 'review_running'
  AND model_free_action_count = 1 AND reviewer_model_call_count = 1
  AND recovery_review_run_id = sqlc.arg(review_run_id)
  AND new_head = sqlc.arg(target_sha) AND merge_commit_sha = ''
  AND error_code = '';

-- name: RebindDCPAdmissionAfterCard12ColdStartRecovery :execrows
UPDATE dcp_review_lab_admission
SET review_run_id = sqlc.arg(new_review_run_id), review_id = sqlc.arg(new_review_id),
    target_sha = sqlc.arg(new_target_sha), review_base_sha = sqlc.arg(new_review_base_sha),
    status = 'waiting', lease_id = '', admitted_base_sha = '', error_code = '',
    updated_at = sqlc.arg(updated_at)
WHERE dcp_review_lab_admission.id = sqlc.arg(admission_id)
  AND dcp_review_lab_admission.sequence = 4
  AND dcp_review_lab_admission.review_run_id = sqlc.arg(old_review_run_id)
  AND dcp_review_lab_admission.session_id = 'dcp-review-lab-12'
  AND dcp_review_lab_admission.pr_url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/9'
  AND dcp_review_lab_admission.target_sha = 'd4fcb68051ae113ed497d02151a759800ee85633'
  AND dcp_review_lab_admission.status = 'incident'
  AND dcp_review_lab_admission.lease_id = 'dcp-incident-dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34'
  AND dcp_review_lab_admission.error_code = 'merge_conflict_or_ambiguity'
  AND EXISTS (SELECT 1 FROM review_run rr
    WHERE rr.id = sqlc.arg(new_review_run_id) AND rr.review_id = sqlc.arg(new_review_id)
      AND rr.session_id = 'dcp-review-lab-12'
      AND rr.pr_url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/9'
      AND rr.target_sha = sqlc.arg(new_target_sha) AND rr.status = 'complete'
      AND rr.verdict = 'approved' AND rr.result_channel = 'structured_dcp_v1'
      AND rr.terminal_merge_status = '')
  AND EXISTS (SELECT 1 FROM dcp_review_lab_card12_cold_start_recovery recovery
    WHERE recovery.recovery_id = sqlc.arg(recovery_id)
      AND recovery.status = 'review_running'
      AND recovery.recovery_review_run_id = sqlc.arg(new_review_run_id)
      AND recovery.new_head = sqlc.arg(new_target_sha)
      AND recovery.model_free_action_count = 1
      AND recovery.reviewer_model_call_count = 1
      AND recovery.worker_model_call_count = 0
      AND recovery.arbiter_model_call_count = 0);

-- name: MarkDCPCard12ColdStartRecoveryReviewed :execrows
UPDATE dcp_review_lab_card12_cold_start_recovery
SET status = 'recovery_reviewed', recovery_check_id = sqlc.arg(check_id),
    revision = revision + 1, updated_at = sqlc.arg(updated_at)
WHERE recovery_id = sqlc.arg(recovery_id) AND status = 'review_running'
  AND recovery_review_run_id = sqlc.arg(review_run_id)
  AND new_head = sqlc.arg(target_sha) AND model_free_action_count = 1
  AND reviewer_model_call_count = 1 AND error_code = '';

-- name: CompleteDCPCard12ColdStartRecovery :execrows
UPDATE dcp_review_lab_card12_cold_start_recovery
SET status = 'succeeded', merge_commit_sha = sqlc.arg(merge_commit_sha),
    revision = revision + 1, updated_at = sqlc.arg(updated_at),
    finished_at = sqlc.arg(finished_at)
WHERE recovery_id = sqlc.arg(recovery_id) AND status = 'recovery_reviewed'
  AND recovery_review_run_id = sqlc.arg(review_run_id)
  AND new_head = sqlc.arg(target_sha) AND model_free_action_count = 1
  AND reviewer_model_call_count = 1 AND worker_model_call_count = 0
  AND arbiter_model_call_count = 0 AND merge_commit_sha = '' AND error_code = '';

-- name: FailDCPCard12ColdStartRecoveryTerminal :execrows
UPDATE dcp_review_lab_card12_cold_start_recovery
SET status = 'failed', error_code = sqlc.arg(error_code),
    revision = revision + 1, updated_at = sqlc.arg(updated_at),
    finished_at = sqlc.arg(finished_at)
WHERE recovery_id = 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
  AND status = 'recovery_reviewed'
  AND recovery_review_run_id = sqlc.arg(review_run_id)
  AND new_head = sqlc.arg(target_sha) AND model_free_action_count = 1
  AND reviewer_model_call_count = 1 AND merge_commit_sha = '' AND error_code = '';
