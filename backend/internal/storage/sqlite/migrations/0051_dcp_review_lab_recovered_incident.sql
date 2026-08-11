-- +goose Up
-- Preserve a false-positive incident packet when the exact admission is
-- model-free recovered after proving canonical main only advanced by fast
-- forward. The active incident field remains reserved for terminal ambiguity.
ALTER TABLE dcp_review_lab_admission
ADD COLUMN recovered_incident_packet TEXT NOT NULL DEFAULT '' CHECK (
    recovered_incident_packet = '' OR (
        json_valid(recovered_incident_packet)
        AND json_type(recovered_incident_packet) = 'object'
        AND json_extract(recovered_incident_packet, '$.schemaVersion') = 'dcp.review-lab.arbiter-needed/v1'
    )
);

-- +goose Down
ALTER TABLE dcp_review_lab_admission DROP COLUMN recovered_incident_packet;
