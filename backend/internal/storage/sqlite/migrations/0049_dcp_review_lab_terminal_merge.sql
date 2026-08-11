-- +goose Up
-- DCP records the single authorized synthetic terminal merge on the
-- existing exact-head ReviewRun. This is not a general merge queue: blank is
-- the default for every ordinary AO review and only the bounded DCP contour
-- may claim it.
ALTER TABLE review_run ADD COLUMN result_channel TEXT NOT NULL DEFAULT '';
ALTER TABLE review_run ADD COLUMN terminal_merge_status TEXT NOT NULL DEFAULT '';
ALTER TABLE review_run ADD COLUMN terminal_merge_commit_sha TEXT NOT NULL DEFAULT '';
ALTER TABLE review_run ADD COLUMN terminal_merge_error TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE review_run DROP COLUMN terminal_merge_error;
ALTER TABLE review_run DROP COLUMN terminal_merge_commit_sha;
ALTER TABLE review_run DROP COLUMN terminal_merge_status;
ALTER TABLE review_run DROP COLUMN result_channel;
