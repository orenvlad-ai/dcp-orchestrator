package sqlite

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// TestRealTargetSubmitRecoveryOnExactLiveCopy is opt-in because CI has no
// governed field database. Stopped prepare/install preflight supplies a
// transactionally copied version-75 database through DCP_AO_LIVE_COPY_DB. The
// test applies migration 0076 only to another disposable copy and cannot start
// a daemon, submit a task, or launch a model.
func TestRealTargetSubmitRecoveryOnExactLiveCopy(t *testing.T) {
	source := os.Getenv("DCP_AO_LIVE_COPY_DB")
	if source == "" {
		t.Skip("DCP_AO_LIVE_COPY_DB is not set")
	}
	before := openReadOnlyDB(t, source)
	beforeCounts := policyAuthorityCounts(t, before)
	var predecessorState, predecessorBatch, predecessorChannel string
	var predecessorRevision, predecessorAdmissionRows int64
	if err := before.QueryRow(`SELECT state, revision FROM dcp_review_lab_policy_task WHERE task_id='price-arch-v1'`).Scan(&predecessorState, &predecessorRevision); err != nil {
		t.Fatal(err)
	}
	if err := before.QueryRow(`
SELECT batch_id, result_channel FROM review_run
WHERE id='b0acfb9e-600c-4816-bb2f-02a67817ea05'
  AND review_id='f754d155-faad-4a6b-8a03-53a3b93b11b8'
  AND session_id='wb-price-extension-1'
  AND pr_url='https://github.com/orenvlad-ai/wb-price-extension/pull/1'
  AND target_sha='afc748eba5ff05c0dc24d3002c690ec9f44984fb'
  AND status='complete' AND verdict='approved'
  AND terminal_merge_status='' AND terminal_merge_commit_sha=''
  AND terminal_merge_error=''`).Scan(&predecessorBatch, &predecessorChannel); err != nil {
		t.Fatal(err)
	}
	if err := before.QueryRow(`SELECT count(*) FROM dcp_review_lab_admission WHERE session_id='wb-price-extension-1'`).Scan(&predecessorAdmissionRows); err != nil {
		t.Fatal(err)
	}
	if predecessorState != "ci_waiting" || predecessorRevision != 4 ||
		predecessorBatch != "6b097406-b9bc-42e5-90fb-2b82180e9458" || predecessorChannel != "structured_dcp_v1" || predecessorAdmissionRows != 0 {
		t.Fatalf("predecessor drifted: state=%s rev=%d batch=%s channel=%s admissions=%d", predecessorState, predecessorRevision, predecessorBatch, predecessorChannel, predecessorAdmissionRows)
	}
	var beforeVersion int64
	if err := before.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&beforeVersion); err != nil || beforeVersion != 75 {
		t.Fatalf("predecessor migration version=%d err=%v", beforeVersion, err)
	}
	_ = before.Close()

	dir := t.TempDir()
	destination := filepath.Join(dir, "ao.db")
	copyRecoveryFixture(t, source, destination)
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("migrate exact real-target copy: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	after := openReadOnlyDB(t, destination)
	var state, prURL, head, reviewRunID string
	var revision, prNumber int64
	if err := after.QueryRow(`
SELECT state, revision, pr_url, pr_number, current_head_sha, review_run_id
FROM dcp_review_lab_policy_task WHERE task_id='price-arch-v1'`).Scan(
		&state, &revision, &prURL, &prNumber, &head, &reviewRunID,
	); err != nil {
		t.Fatal(err)
	}
	if state != "admission_waiting" || revision != 5 || prURL != "https://github.com/orenvlad-ai/wb-price-extension/pull/1" ||
		prNumber != 1 || head != "afc748eba5ff05c0dc24d3002c690ec9f44984fb" || reviewRunID != "b0acfb9e-600c-4816-bb2f-02a67817ea05" {
		t.Fatalf("recovered task drifted: state=%s rev=%d pr=%s/%d head=%s run=%s", state, revision, prURL, prNumber, head, reviewRunID)
	}
	var sequence, workerTokens, reviewerTokens int64
	var actionID, actionStatus, actionRunID string
	if err := after.QueryRow(`
SELECT sequence, id, status, review_run_id
FROM dcp_model_action WHERE task_id='price-arch-v1' AND kind='reviewer'`).Scan(
		&sequence, &actionID, &actionStatus, &actionRunID,
	); err != nil {
		t.Fatal(err)
	}
	if sequence != 60 || actionID != "dcp-model-price-arch-v1-review-1" || actionStatus != "succeeded" || actionRunID != reviewRunID {
		t.Fatalf("reviewer accounting drifted: seq=%d id=%s status=%s run=%s", sequence, actionID, actionStatus, actionRunID)
	}
	if err := after.QueryRow(`
SELECT worker_token_count, reviewer_token_count
FROM dcp_real_target_submit_recovery_v1`).Scan(&workerTokens, &reviewerTokens); err != nil {
		t.Fatal(err)
	}
	if workerTokens != 27373 || reviewerTokens != 20512 {
		t.Fatalf("token accounting drifted: worker=%d reviewer=%d", workerTokens, reviewerTokens)
	}
	afterCounts := policyAuthorityCounts(t, after)
	if afterCounts != [4]int64{beforeCounts[0], beforeCounts[1] + 1, beforeCounts[2], beforeCounts[3]} {
		t.Fatalf("authority counts drifted: before=%v after=%v", beforeCounts, afterCounts)
	}
	var version int64
	if err := after.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&version); err != nil || version != 76 {
		t.Fatalf("migration version=%d err=%v", version, err)
	}
	var integrity string
	if err := after.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity=%q err=%v", integrity, err)
	}
	var foreignViolations int64
	if err := after.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&foreignViolations); err != nil || foreignViolations != 0 {
		t.Fatalf("foreign key violations=%d err=%v", foreignViolations, err)
	}
	_ = after.Close()

	// Goose versioning makes restart replay inert; opening the migrated copy a
	// second time must not add another action or alter the recovered identity.
	store, err = Open(dir)
	if err != nil {
		t.Fatalf("reopen recovered copy: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	replayed := openReadOnlyDB(t, destination)
	defer replayed.Close()
	if counts := policyAuthorityCounts(t, replayed); counts != afterCounts {
		t.Fatalf("restart replay duplicated authority: once=%v replay=%v", afterCounts, counts)
	}
	var auditRows, reviewerRows, admissionRows int64
	if err := replayed.QueryRow(`SELECT count(*) FROM dcp_real_target_submit_recovery_v1`).Scan(&auditRows); err != nil {
		t.Fatal(err)
	}
	if err := replayed.QueryRow(`SELECT count(*) FROM dcp_model_action WHERE id='dcp-model-price-arch-v1-review-1'`).Scan(&reviewerRows); err != nil {
		t.Fatal(err)
	}
	if err := replayed.QueryRow(`SELECT count(*) FROM dcp_review_lab_admission WHERE session_id='wb-price-extension-1'`).Scan(&admissionRows); err != nil {
		t.Fatal(err)
	}
	if auditRows != 1 || reviewerRows != 1 || admissionRows != 0 {
		t.Fatalf("replay/audit side effects: audit=%d reviewer=%d admission=%d", auditRows, reviewerRows, admissionRows)
	}
}

func TestRealTargetSubmitRecoveryRejectsExactHeadDriftOnLiveCopy(t *testing.T) {
	source := os.Getenv("DCP_AO_LIVE_COPY_DB")
	if source == "" {
		t.Skip("DCP_AO_LIVE_COPY_DB is not set")
	}
	dir := t.TempDir()
	destination := filepath.Join(dir, "ao.db")
	copyRecoveryFixture(t, source, destination)
	db, err := sql.Open("sqlite", "file:"+destination+pragmas)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE review_run SET target_sha='0000000000000000000000000000000000000000' WHERE id='b0acfb9e-600c-4816-bb2f-02a67817ea05'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if store, err := Open(dir); err == nil {
		_ = store.Close()
		t.Fatal("migration accepted a stale reviewer head")
	}
	unchanged := openReadOnlyDB(t, destination)
	defer unchanged.Close()
	var state string
	var revision, reviewerRows, admissionRows int64
	if err := unchanged.QueryRow(`SELECT state, revision FROM dcp_review_lab_policy_task WHERE task_id='price-arch-v1'`).Scan(&state, &revision); err != nil {
		t.Fatal(err)
	}
	if err := unchanged.QueryRow(`SELECT count(*) FROM dcp_model_action WHERE task_id='price-arch-v1' AND kind='reviewer'`).Scan(&reviewerRows); err != nil {
		t.Fatal(err)
	}
	if err := unchanged.QueryRow(`SELECT count(*) FROM dcp_review_lab_admission WHERE session_id='wb-price-extension-1'`).Scan(&admissionRows); err != nil {
		t.Fatal(err)
	}
	if state != "ci_waiting" || revision != 4 || reviewerRows != 0 || admissionRows != 0 {
		t.Fatalf("rejected migration mutated state: state=%s rev=%d reviewer=%d admission=%d", state, revision, reviewerRows, admissionRows)
	}
}

func copyRecoveryFixture(t *testing.T, source, destination string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
