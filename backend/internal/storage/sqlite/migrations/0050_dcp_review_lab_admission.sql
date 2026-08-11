-- +goose Up
-- I13 Stage 1 serializes only the two bounded synthetic review-lab terminal
-- candidates. Each row is subordinate to an existing exact-head ReviewRun;
-- it is not a task/card registry or a general release queue.
CREATE TABLE dcp_review_lab_admission (
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
    CHECK ((status = 'waiting' AND lease_id = '') OR (status <> 'waiting' AND length(lease_id) > 0)),
    CHECK ((status = 'incident') = (incident_packet <> '')),
    CHECK ((status = 'succeeded') = (merge_commit_sha <> ''))
);

-- Every claimed row has the same indexed value. The partial unique index is
-- therefore a physical global single-owner guard even if process locking is
-- bypassed or two daemon callbacks race.
CREATE UNIQUE INDEX idx_dcp_review_lab_admission_one_claim
    ON dcp_review_lab_admission (status) WHERE status = 'claimed';
CREATE UNIQUE INDEX idx_dcp_review_lab_admission_one_active_per_session
    ON dcp_review_lab_admission (session_id)
    WHERE status IN ('waiting', 'claimed', 'refreshing', 'incident');
CREATE INDEX idx_dcp_review_lab_admission_fifo
    ON dcp_review_lab_admission (status, sequence);
CREATE INDEX idx_dcp_review_lab_admission_session
    ON dcp_review_lab_admission (session_id, sequence);

-- +goose Down
DROP TABLE dcp_review_lab_admission;
