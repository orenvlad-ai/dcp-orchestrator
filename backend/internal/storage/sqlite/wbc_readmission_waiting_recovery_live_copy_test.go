package sqlite

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// TestWBCReadmissionWaitingRecoveryOnExactLiveCopy is model-free and opt-in.
// The supplied schema-81 database must be a disposable checkpointed copy; this
// test never opens or mutates the canonical runtime database.
func TestWBCReadmissionWaitingRecoveryOnExactLiveCopy(t *testing.T) {
	source := os.Getenv("DCP_WBC_READMISSION_WAITING_LIVE_DB")
	if source == "" {
		t.Skip("DCP_WBC_READMISSION_WAITING_LIVE_DB is not set")
	}
	before := openReadOnlyDB(t, source)
	assertWBCReadmissionWaitingPredecessor(t, before)
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
		t.Fatalf("migrate exact WBC readmission waiting copy: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	after := openReadOnlyDB(t, destination)
	defer after.Close()
	var state, taskError, taskPacket, reviewRunID, admissionID string
	var revision int64
	if err := after.QueryRow(`
SELECT state, revision, review_run_id, admission_id, error_code, incident_packet
FROM dcp_review_lab_policy_task WHERE task_id='wbc-canary-v1'`).Scan(
		&state, &revision, &reviewRunID, &admissionID, &taskError, &taskPacket,
	); err != nil {
		t.Fatal(err)
	}
	if state != "admission_waiting" || revision != 22 || reviewRunID != "18c54338-df31-4471-a344-4db6648ff4e3" ||
		admissionID != "dcp-admission-18c54338-df31-4471-a344-4db6648ff4e3" || taskError != "" || taskPacket != "" {
		t.Fatalf("recovered task drifted: state=%s rev=%d review=%s admission=%s error=%q packet=%q",
			state, revision, reviewRunID, admissionID, taskError, taskPacket)
	}
	var admissionStatus, leaseID, admittedBase, admissionError, incidentPacket, recoveredPacket string
	if err := after.QueryRow(`
SELECT status, lease_id, admitted_base_sha, error_code, incident_packet, recovered_incident_packet
FROM dcp_review_lab_admission WHERE id=?`, admissionID).Scan(
		&admissionStatus, &leaseID, &admittedBase, &admissionError, &incidentPacket, &recoveredPacket,
	); err != nil {
		t.Fatal(err)
	}
	if admissionStatus != "waiting" || leaseID != "" || admittedBase != "" || admissionError != "" || incidentPacket != "" || recoveredPacket == "" {
		t.Fatalf("recovered admission drifted: status=%s lease=%q base=%q error=%q packet=%q recovered=%q",
			admissionStatus, leaseID, admittedBase, admissionError, incidentPacket, recoveredPacket)
	}

	var actions, admissions, initialWorkers, evidence, boundGeneration, version int64
	queries := []struct {
		query string
		out   *int64
	}{
		{`SELECT count(*) FROM dcp_model_action`, &actions},
		{`SELECT count(*) FROM dcp_review_lab_admission`, &admissions},
		{`SELECT count(*) FROM dcp_model_action WHERE task_id='wbc-canary-v1' AND kind='initial_worker'`, &initialWorkers},
		{`SELECT count(*) FROM dcp_wbc_readmission_waiting_recovery_v1 WHERE recovery_id='wbc-canary-v1-readmission-waiting-recovery' AND prior_task_revision=21 AND prior_admission_sequence=32 AND prior_error_code='waiting_identity_drift'`, &evidence},
		{`SELECT count(*) FROM dcp_wbc_readmission_generation WHERE task_id='wbc-canary-v1' AND status='admitted' AND new_head_sha='26044c696651ce5873748ec3f920d40e77c5686c' AND review_run_id='18c54338-df31-4471-a344-4db6648ff4e3' AND admission_id='dcp-admission-18c54338-df31-4471-a344-4db6648ff4e3'`, &boundGeneration},
		{`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`, &version},
	}
	for _, item := range queries {
		if err := after.QueryRow(item.query).Scan(item.out); err != nil {
			t.Fatal(err)
		}
	}
	if actions != beforeActions || admissions != beforeAdmissions || initialWorkers != 1 || evidence != 1 || boundGeneration != 1 || version != 82 {
		t.Fatalf("recovery counts actions=%d/%d admissions=%d/%d workers=%d evidence=%d generation=%d version=%d",
			actions, beforeActions, admissions, beforeAdmissions, initialWorkers, evidence, boundGeneration, version)
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
	var replayActions, replayAdmissions, replayEvidence int64
	if err := replayed.QueryRow(`SELECT count(*) FROM dcp_model_action`).Scan(&replayActions); err != nil {
		t.Fatal(err)
	}
	if err := replayed.QueryRow(`SELECT count(*) FROM dcp_review_lab_admission`).Scan(&replayAdmissions); err != nil {
		t.Fatal(err)
	}
	if err := replayed.QueryRow(`SELECT count(*) FROM dcp_wbc_readmission_waiting_recovery_v1`).Scan(&replayEvidence); err != nil {
		t.Fatal(err)
	}
	if replayActions != actions || replayAdmissions != admissions || replayEvidence != 1 {
		t.Fatalf("restart duplicated recovery: actions=%d/%d admissions=%d/%d evidence=%d",
			replayActions, actions, replayAdmissions, admissions, replayEvidence)
	}
}

func TestWBCReadmissionWaitingRecoveryRejectsShellDriftOnLiveCopy(t *testing.T) {
	source := os.Getenv("DCP_WBC_READMISSION_WAITING_LIVE_DB")
	if source == "" {
		t.Skip("DCP_WBC_READMISSION_WAITING_LIVE_DB is not set")
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
	var state, errorCode, admissionStatus string
	var revision, evidence int64
	if err := unchanged.QueryRow(`SELECT state, revision, error_code FROM dcp_review_lab_policy_task WHERE task_id='wbc-canary-v1'`).Scan(&state, &revision, &errorCode); err != nil {
		t.Fatal(err)
	}
	if err := unchanged.QueryRow(`SELECT status FROM dcp_review_lab_admission WHERE id='dcp-admission-18c54338-df31-4471-a344-4db6648ff4e3'`).Scan(&admissionStatus); err != nil {
		t.Fatal(err)
	}
	if err := unchanged.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='dcp_wbc_readmission_waiting_recovery_v1'`).Scan(&evidence); err != nil {
		t.Fatal(err)
	}
	if state != "incident" || revision != 21 || errorCode != "waiting_identity_drift" || admissionStatus != "incident" || evidence != 0 {
		t.Fatalf("rejected migration mutated state: state=%s rev=%d error=%s admission=%s evidence=%d",
			state, revision, errorCode, admissionStatus, evidence)
	}
}

func assertWBCReadmissionWaitingPredecessor(t *testing.T, db interface {
	QueryRow(query string, args ...any) *sql.Row
}) {
	t.Helper()
	var state, errorCode, generationStatus, generationAdmission, admissionStatus, admissionError string
	var revision, actions, initialWorkers, activeActions, version int64
	if err := db.QueryRow(`SELECT state, revision, error_code FROM dcp_review_lab_policy_task WHERE task_id='wbc-canary-v1'`).Scan(&state, &revision, &errorCode); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status, admission_id FROM dcp_wbc_readmission_generation WHERE task_id='wbc-canary-v1'`).Scan(&generationStatus, &generationAdmission); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status, error_code FROM dcp_review_lab_admission WHERE id=?`, generationAdmission).Scan(&admissionStatus, &admissionError); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*), sum(kind='initial_worker'), sum(status IN ('queued','claimed','running')) FROM dcp_model_action WHERE task_id='wbc-canary-v1'`).Scan(&actions, &initialWorkers, &activeActions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if state != "incident" || revision != 21 || errorCode != "waiting_identity_drift" ||
		generationStatus != "admitted" || generationAdmission != "dcp-admission-18c54338-df31-4471-a344-4db6648ff4e3" ||
		admissionStatus != "incident" || admissionError != "waiting_identity_drift" ||
		actions != 3 || initialWorkers != 1 || activeActions != 0 || version != 81 {
		t.Fatalf("predecessor drifted state=%s rev=%d error=%s generation=%s/%s admission=%s/%s actions=%d workers=%d active=%d version=%d",
			state, revision, errorCode, generationStatus, generationAdmission, admissionStatus, admissionError,
			actions, initialWorkers, activeActions, version)
	}
}
