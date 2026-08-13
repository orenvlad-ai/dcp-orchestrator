-- +goose Up
-- One immutable correction for the exact distinction between PR #9's
-- historical provider-base snapshot and the current target-branch ref.
CREATE TABLE dcp_review_lab_card12_model_free_provider_base_correction (
    correction_id             TEXT PRIMARY KEY CHECK (correction_id = 'dcp-card12-model-free-provider-base-correction-25663a5a551fce7ec0d6d9055588b4c4d1d1294fd926e2c7c2347cacd799ab59'),
    generation                INTEGER NOT NULL CHECK (generation = 1),
    identity_digest           TEXT NOT NULL UNIQUE CHECK (identity_digest = '25663a5a551fce7ec0d6d9055588b4c4d1d1294fd926e2c7c2347cacd799ab59'),
    contract_commit           TEXT NOT NULL CHECK (contract_commit = '9610bf1a8fa41f631ca5ed336d0d9b0313d7d73f'),
    continuation_id           TEXT NOT NULL UNIQUE CHECK (continuation_id = 'dcp-card12-model-free-rebase-continuation-66eb630c1995f90b37429a2f6c57c57794dda9fc98a29149c88bdb2f01131060') REFERENCES dcp_review_lab_card12_model_free_rebase_continuation (continuation_id) ON DELETE RESTRICT,
    original_contract_commit  TEXT NOT NULL CHECK (original_contract_commit = 'e17fa9080434b5642667392fb06db61cf35f19bd'),
    reviewed_source_commit    TEXT NOT NULL CHECK (reviewed_source_commit = 'a7b5476fb886bcbb6bbd91aa89da17966547b3b8'),
    provider_base_sha         TEXT NOT NULL CHECK (provider_base_sha = 'dbaf01b05e85ffffa4c843a905e2fe5229eaf0da'),
    current_main_sha          TEXT NOT NULL CHECK (current_main_sha = 'b34b31b5443890e69128db2862726950a6bbac0d'),
    authorized_at             TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX idx_dcp_card12_one_model_free_provider_base_correction
    ON dcp_review_lab_card12_model_free_provider_base_correction ((1));

INSERT INTO dcp_review_lab_card12_model_free_provider_base_correction (
    correction_id, generation, identity_digest, contract_commit,
    continuation_id, original_contract_commit, reviewed_source_commit,
    provider_base_sha, current_main_sha, authorized_at
)
SELECT
    'dcp-card12-model-free-provider-base-correction-25663a5a551fce7ec0d6d9055588b4c4d1d1294fd926e2c7c2347cacd799ab59',
    1, '25663a5a551fce7ec0d6d9055588b4c4d1d1294fd926e2c7c2347cacd799ab59',
    '9610bf1a8fa41f631ca5ed336d0d9b0313d7d73f', continuation.continuation_id,
    'e17fa9080434b5642667392fb06db61cf35f19bd',
    'a7b5476fb886bcbb6bbd91aa89da17966547b3b8',
    'dbaf01b05e85ffffa4c843a905e2fe5229eaf0da', continuation.current_main,
    CURRENT_TIMESTAMP
FROM dcp_review_lab_card12_model_free_rebase_continuation continuation
WHERE continuation.continuation_id = 'dcp-card12-model-free-rebase-continuation-66eb630c1995f90b37429a2f6c57c57794dda9fc98a29149c88bdb2f01131060'
  AND continuation.contract_commit = 'e17fa9080434b5642667392fb06db61cf35f19bd'
  AND continuation.current_main = 'b34b31b5443890e69128db2862726950a6bbac0d'
  AND continuation.status = 'authorized' AND continuation.revision = 0
  AND continuation.model_free_action_count = 0
  AND continuation.reviewer_model_call_count = 0
  AND continuation.new_head = '' AND continuation.recovery_review_run_id = '';

-- +goose StatementBegin
CREATE TRIGGER dcp_card12_model_free_provider_base_correction_no_update
BEFORE UPDATE ON dcp_review_lab_card12_model_free_provider_base_correction
BEGIN
    SELECT RAISE(ABORT, 'card-12 model-free provider-base correction is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_card12_model_free_provider_base_correction_no_delete
BEFORE DELETE ON dcp_review_lab_card12_model_free_provider_base_correction
BEGIN
    SELECT RAISE(ABORT, 'card-12 model-free provider-base correction is immutable');
END;
-- +goose StatementEnd

-- +goose Down
CREATE TABLE dcp_card12_model_free_provider_base_correction_rollback_guard (
    correction_count INTEGER NOT NULL CHECK (correction_count = 1),
    status TEXT NOT NULL CHECK (status = 'authorized'),
    action_count INTEGER NOT NULL CHECK (action_count = 0),
    reviewer_count INTEGER NOT NULL CHECK (reviewer_count = 0),
    new_head TEXT NOT NULL CHECK (new_head = '')
);
INSERT INTO dcp_card12_model_free_provider_base_correction_rollback_guard
SELECT count(*), continuation.status, continuation.model_free_action_count,
       continuation.reviewer_model_call_count, continuation.new_head
FROM dcp_review_lab_card12_model_free_provider_base_correction correction
JOIN dcp_review_lab_card12_model_free_rebase_continuation continuation
  ON continuation.continuation_id = correction.continuation_id;
DROP TRIGGER dcp_card12_model_free_provider_base_correction_no_update;
DROP TRIGGER dcp_card12_model_free_provider_base_correction_no_delete;
DROP INDEX idx_dcp_card12_one_model_free_provider_base_correction;
DROP TABLE dcp_review_lab_card12_model_free_provider_base_correction;
DROP TABLE dcp_card12_model_free_provider_base_correction_rollback_guard;
