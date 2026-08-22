-- +goose NO TRANSACTION
-- +goose Up
-- DCP v2 immutable provider binding and terminal artifact-source provenance.
-- The live predecessor is stopped at schema 86. Existing Revisions and Results
-- retain their identities; publication may only add a provider_bound successor.
PRAGMA foreign_keys = OFF;
BEGIN IMMEDIATE;

DROP TRIGGER dcp_v2_revision_no_update;
DROP TRIGGER dcp_v2_revision_no_delete;

CREATE TABLE dcp_v2_revision_v87 (
    revision_id             TEXT PRIMARY KEY CHECK (length(revision_id) > 0),
    task_id                 TEXT NOT NULL REFERENCES dcp_v2_task (task_id) ON DELETE RESTRICT,
    sequence                INTEGER NOT NULL CHECK (sequence >= 1),
    kind                    TEXT NOT NULL CHECK (kind IN ('work_input','worker_output','repair_output','provider_bound','readmission_output')),
    repository              TEXT NOT NULL CHECK (length(repository) > 0),
    base_ref                TEXT NOT NULL CHECK (length(base_ref) > 0),
    base_sha                TEXT NOT NULL CHECK (length(base_sha) = 40),
    head_ref                TEXT NOT NULL CHECK (length(head_ref) > 0),
    head_sha                TEXT NOT NULL CHECK (length(head_sha) = 40),
    tree_sha                TEXT NOT NULL DEFAULT '' CHECK (tree_sha = '' OR length(tree_sha) = 40),
    predecessor_revision_id TEXT NOT NULL DEFAULT '',
    cause_command_id        TEXT NOT NULL DEFAULT '',
    pr_number               INTEGER NOT NULL DEFAULT 0 CHECK (pr_number >= 0),
    evidence_digest         TEXT NOT NULL CHECK (length(evidence_digest) = 64),
    created_at              TIMESTAMP NOT NULL,
    UNIQUE (task_id, sequence),
    UNIQUE (task_id, head_sha, kind),
    UNIQUE (revision_id, task_id),
    CHECK ((sequence = 1) = (kind = 'work_input')),
    CHECK ((sequence = 1) = (predecessor_revision_id = '')),
    CHECK ((sequence = 1) = (cause_command_id = '')),
    CHECK (sequence <> 1 OR base_sha = head_sha),
    CHECK (kind = 'work_input' OR length(tree_sha) = 40),
    CHECK ((kind IN ('provider_bound','readmission_output')) = (pr_number > 0))
);

INSERT INTO dcp_v2_revision_v87 (
    revision_id, task_id, sequence, kind, repository, base_ref, base_sha,
    head_ref, head_sha, tree_sha, predecessor_revision_id, cause_command_id, pr_number,
    evidence_digest, created_at
) SELECT revision_id, task_id, sequence, kind, repository, base_ref, base_sha,
         head_ref, head_sha, '', predecessor_revision_id, cause_command_id, pr_number,
         evidence_digest, created_at
    FROM dcp_v2_revision;

DROP TABLE dcp_v2_revision;
ALTER TABLE dcp_v2_revision_v87 RENAME TO dcp_v2_revision;

-- +goose StatementBegin
CREATE TRIGGER dcp_v2_revision_no_update BEFORE UPDATE ON dcp_v2_revision
BEGIN SELECT RAISE(ABORT, 'DCP v2 Revision is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_revision_no_delete BEFORE DELETE ON dcp_v2_revision
BEGIN SELECT RAISE(ABORT, 'DCP v2 Revision cannot be deleted'); END;
-- +goose StatementEnd

DROP TRIGGER dcp_v2_result_no_update;
DROP TRIGGER dcp_v2_result_no_delete;

CREATE TABLE dcp_v2_result_v87 (
    result_id           TEXT PRIMARY KEY CHECK (length(result_id) > 0),
    task_id             TEXT NOT NULL REFERENCES dcp_v2_task (task_id) ON DELETE RESTRICT,
    revision_id         TEXT NOT NULL REFERENCES dcp_v2_revision (revision_id) ON DELETE RESTRICT,
    admission_id        TEXT REFERENCES dcp_v2_admission (admission_id) ON DELETE RESTRICT,
    command_id          TEXT NOT NULL REFERENCES dcp_v2_command (command_id) ON DELETE RESTRICT,
    kind                TEXT NOT NULL CHECK (kind IN ('release','deployment','failure')),
    provider            TEXT NOT NULL DEFAULT '',
    proof_id            TEXT NOT NULL DEFAULT '',
    run_id              TEXT NOT NULL DEFAULT '',
    actor               TEXT NOT NULL DEFAULT '',
    manifest_digest     TEXT NOT NULL DEFAULT '' CHECK (manifest_digest = '' OR length(manifest_digest) = 64),
    proof_digest        TEXT NOT NULL UNIQUE CHECK (length(proof_digest) = 64),
    merge_sha           TEXT NOT NULL DEFAULT '' CHECK (merge_sha = '' OR length(merge_sha) = 40),
    artifact_source_sha TEXT NOT NULL DEFAULT '' CHECK (artifact_source_sha = '' OR length(artifact_source_sha) = 40),
    artifact_digest     TEXT NOT NULL DEFAULT '' CHECK (artifact_digest = '' OR length(artifact_digest) = 64),
    deployed_sha        TEXT NOT NULL DEFAULT '' CHECK (deployed_sha = '' OR length(deployed_sha) = 40),
    environment         TEXT NOT NULL DEFAULT '',
    service             TEXT NOT NULL DEFAULT '',
    probe_digest        TEXT NOT NULL DEFAULT '' CHECK (probe_digest = '' OR length(probe_digest) = 64),
    verified            INTEGER NOT NULL CHECK (verified IN (0, 1)),
    error_code          TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMP NOT NULL,
    CHECK (kind <> 'deployment' OR verified = 0 OR (
        merge_sha <> '' AND artifact_source_sha = merge_sha AND artifact_digest <> '' AND deployed_sha = merge_sha
        AND environment <> '' AND service <> '' AND probe_digest <> ''
    )),
    FOREIGN KEY (command_id, task_id, revision_id) REFERENCES dcp_v2_command (command_id, task_id, revision_id) ON DELETE RESTRICT,
    FOREIGN KEY (admission_id, task_id, revision_id) REFERENCES dcp_v2_admission (admission_id, task_id, revision_id) ON DELETE RESTRICT,
    CHECK (kind <> 'release' OR verified = 0 OR (merge_sha <> '' AND artifact_source_sha = merge_sha AND artifact_digest <> '')),
    CHECK (verified = 0 OR (provider <> '' AND proof_id <> '' AND run_id <> '' AND actor <> '' AND manifest_digest <> '')),
    CHECK (verified = 0 OR error_code = '')
);

INSERT INTO dcp_v2_result_v87 (
    result_id, task_id, revision_id, admission_id, command_id, kind, provider,
    proof_id, run_id, actor, manifest_digest, proof_digest, merge_sha,
    artifact_source_sha, artifact_digest, deployed_sha, environment, service,
    probe_digest, verified, error_code, created_at
) SELECT result_id, task_id, revision_id, admission_id, command_id, kind, provider,
         proof_id, run_id, actor, manifest_digest, proof_digest, merge_sha,
         CASE WHEN verified = 1 AND kind IN ('release','deployment') THEN merge_sha ELSE '' END,
         artifact_digest, deployed_sha, environment, service, probe_digest,
         verified, error_code, created_at
    FROM dcp_v2_result;

DROP TABLE dcp_v2_result;
ALTER TABLE dcp_v2_result_v87 RENAME TO dcp_v2_result;

-- +goose StatementBegin
CREATE TRIGGER dcp_v2_result_no_update BEFORE UPDATE ON dcp_v2_result
BEGIN SELECT RAISE(ABORT, 'DCP v2 Result is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_result_no_delete BEFORE DELETE ON dcp_v2_result
BEGIN SELECT RAISE(ABORT, 'DCP v2 Result cannot be deleted'); END;
-- +goose StatementEnd

COMMIT;
PRAGMA foreign_keys = ON;

-- +goose Down
SELECT RAISE(ABORT, '0087 DCP v2 provider-bound Revision is forward-only');
