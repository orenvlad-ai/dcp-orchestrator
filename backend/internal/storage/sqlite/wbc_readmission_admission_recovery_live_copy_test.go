package sqlite

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// TestWBCReadmissionAdmissionRecoveryOnExactLiveCopy is model-free and
// opt-in. The supplied schema-80 database must be a disposable checkpointed
// copy; this test never opens or mutates the canonical runtime database.
func TestWBCReadmissionAdmissionRecoveryOnExactLiveCopy(t *testing.T) {
	source := os.Getenv("DCP_WBC_READMISSION_ADMISSION_LIVE_DB")
	if source == "" {
		t.Skip("DCP_WBC_READMISSION_ADMISSION_LIVE_DB is not set")
	}
	before := openReadOnlyDB(t, source)
	assertWBCReadmissionAdmissionPredecessor(t, before)
	var beforeActions, beforeAdmissions int64
	if err := before.QueryRow(`SELECT count(*) FROM dcp_model_action`).Scan(&beforeActions); err != nil {
		t.Fatal(err)
	}
	if err := before.QueryRow(`SELECT count(*) FROM dcp_review_lab_admission`).Scan(&beforeAdmissions); err != nil {
		t.Fatal(err)
	}
	_ = before.Close()

	dir := t.TempDir()
	destination := filepath.Join(dir, "ao.db")
	copyRecoveryFixture(t, source, destination)
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("migrate exact WBC readmission admission copy: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	after := openReadOnlyDB(t, destination)
	defer after.Close()
	var state, errorCode, packet, reviewRunID string
	var revision int64
	if err := after.QueryRow(`
SELECT state, revision, review_run_id, error_code, incident_packet
FROM dcp_review_lab_policy_task WHERE task_id='wbc-canary-v1'`).Scan(
		&state, &revision, &reviewRunID, &errorCode, &packet,
	); err != nil {
		t.Fatal(err)
	}
	if state != "admission_waiting" || revision != 19 || reviewRunID != "18c54338-df31-4471-a344-4db6648ff4e3" || errorCode != "" || packet != "" {
		t.Fatalf("recovered task drifted: state=%s rev=%d review=%s error=%q packet=%q", state, revision, reviewRunID, errorCode, packet)
	}
	var actions, admissions, initialWorkers, evidence, reviewedGeneration, version int64
	queries := []struct {
		query string
		out   *int64
	}{
		{`SELECT count(*) FROM dcp_model_action`, &actions},
		{`SELECT count(*) FROM dcp_review_lab_admission`, &admissions},
		{`SELECT count(*) FROM dcp_model_action WHERE task_id='wbc-canary-v1' AND kind='initial_worker'`, &initialWorkers},
		{`SELECT count(*) FROM dcp_wbc_readmission_admission_recovery_v1 WHERE recovery_id='wbc-canary-v1-readmission-admission-recovery' AND prior_task_state='incident' AND prior_task_revision=18 AND prior_error_code='admission_identity_drift'`, &evidence},
		{`SELECT count(*) FROM dcp_wbc_readmission_generation WHERE task_id='wbc-canary-v1' AND status='reviewed' AND new_head_sha='26044c696651ce5873748ec3f920d40e77c5686c' AND review_run_id='18c54338-df31-4471-a344-4db6648ff4e3' AND admission_id=''`, &reviewedGeneration},
		{`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`, &version},
	}
	for _, item := range queries {
		if err := after.QueryRow(item.query).Scan(item.out); err != nil {
			t.Fatal(err)
		}
	}
	if actions != beforeActions || admissions != beforeAdmissions || initialWorkers != 1 || evidence != 1 || reviewedGeneration != 1 || version != 81 {
		t.Fatalf("recovery counts actions=%d/%d admissions=%d/%d workers=%d evidence=%d generation=%d version=%d",
			actions, beforeActions, admissions, beforeAdmissions, initialWorkers, evidence, reviewedGeneration, version)
	}
	var integrity string
	if err := after.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity=%q err=%v", integrity, err)
	}
	var foreignViolations int64
	if err := after.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&foreignViolations); err != nil || foreignViolations != 0 {
		t.Fatalf("foreign key violations=%d err=%v", foreignViolations, err)
	}

	if err := after.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	replayed := openReadOnlyDB(t, destination)
	defer replayed.Close()
	var replayActions, replayEvidence int64
	if err := replayed.QueryRow(`SELECT count(*) FROM dcp_model_action`).Scan(&replayActions); err != nil {
		t.Fatal(err)
	}
	if err := replayed.QueryRow(`SELECT count(*) FROM dcp_wbc_readmission_admission_recovery_v1`).Scan(&replayEvidence); err != nil {
		t.Fatal(err)
	}
	if replayActions != actions || replayEvidence != 1 {
		t.Fatalf("restart duplicated recovery: actions=%d/%d evidence=%d", replayActions, actions, replayEvidence)
	}
}

func TestWBCReadmissionAdmissionRecoveryRejectsShellDriftOnLiveCopy(t *testing.T) {
	source := os.Getenv("DCP_WBC_READMISSION_ADMISSION_LIVE_DB")
	if source == "" {
		t.Skip("DCP_WBC_READMISSION_ADMISSION_LIVE_DB is not set")
	}
	dir := t.TempDir()
	destination := filepath.Join(dir, "ao.db")
	copyRecoveryFixture(t, source, destination)
	db, err := sql.Open("sqlite", "file:"+destination+pragmas)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE sessions SET activity_state='idle' WHERE id='wb-core-1'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if store, err := Open(dir); err == nil {
		_ = store.Close()
		t.Fatal("migration accepted drifted preserved session shell")
	}
	unchanged := openReadOnlyDB(t, destination)
	defer unchanged.Close()
	var state, errorCode string
	var revision, evidence int64
	if err := unchanged.QueryRow(`SELECT state, revision, error_code FROM dcp_review_lab_policy_task WHERE task_id='wbc-canary-v1'`).Scan(&state, &revision, &errorCode); err != nil {
		t.Fatal(err)
	}
	if err := unchanged.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='dcp_wbc_readmission_admission_recovery_v1'`).Scan(&evidence); err != nil {
		t.Fatal(err)
	}
	if state != "incident" || revision != 18 || errorCode != "admission_identity_drift" || evidence != 0 {
		t.Fatalf("rejected migration mutated state: state=%s rev=%d error=%s evidence=%d", state, revision, errorCode, evidence)
	}
}

func assertWBCReadmissionAdmissionPredecessor(t *testing.T, db interface {
	QueryRow(query string, args ...any) *sql.Row
}) {
	t.Helper()
	var state, errorCode, generationStatus string
	var revision, actions, initialWorkers, activeActions, version int64
	if err := db.QueryRow(`SELECT state, revision, error_code FROM dcp_review_lab_policy_task WHERE task_id='wbc-canary-v1'`).Scan(&state, &revision, &errorCode); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM dcp_wbc_readmission_generation WHERE task_id='wbc-canary-v1'`).Scan(&generationStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*), sum(kind='initial_worker'), sum(status IN ('queued','claimed','running')) FROM dcp_model_action WHERE task_id='wbc-canary-v1'`).Scan(&actions, &initialWorkers, &activeActions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if state != "incident" || revision != 18 || errorCode != "admission_identity_drift" || generationStatus != "reviewed" ||
		actions != 3 || initialWorkers != 1 || activeActions != 0 || version != 80 {
		t.Fatalf("predecessor drifted state=%s rev=%d error=%s generation=%s actions=%d workers=%d active=%d version=%d",
			state, revision, errorCode, generationStatus, actions, initialWorkers, activeActions, version)
	}
}
