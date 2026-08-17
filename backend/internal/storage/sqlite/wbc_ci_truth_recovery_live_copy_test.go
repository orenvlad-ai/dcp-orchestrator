package sqlite

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// TestWBCCITruthRecoveryOnExactLiveCopy is model-free and opt-in. Governed
// stopped preflight supplies a checkpointed schema-78 database. The test
// migrates only a disposable copy and cannot start the daemon or a model.
func TestWBCCITruthRecoveryOnExactLiveCopy(t *testing.T) {
	source := os.Getenv("DCP_AO_LIVE_COPY_DB")
	if source == "" {
		t.Skip("DCP_AO_LIVE_COPY_DB is not set")
	}
	before := openReadOnlyDB(t, source)
	assertWBCCITruthPredecessor(t, before)
	var beforeActions, beforeNotifications int64
	if err := before.QueryRow(`SELECT count(*) FROM dcp_model_action`).Scan(&beforeActions); err != nil {
		t.Fatal(err)
	}
	if err := before.QueryRow(`SELECT count(*) FROM notifications`).Scan(&beforeNotifications); err != nil {
		t.Fatal(err)
	}
	_ = before.Close()

	dir := t.TempDir()
	destination := filepath.Join(dir, "ao.db")
	copyRecoveryFixture(t, source, destination)
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("migrate exact WBC canary copy: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	after := openReadOnlyDB(t, destination)
	var state, prURL, head, errorCode, incidentPacket string
	var revision, prNumber int64
	if err := after.QueryRow(`
SELECT state, revision, pr_url, pr_number, current_head_sha, error_code, incident_packet
FROM dcp_review_lab_policy_task WHERE task_id='wbc-canary-v1'`).Scan(
		&state, &revision, &prURL, &prNumber, &head, &errorCode, &incidentPacket,
	); err != nil {
		t.Fatal(err)
	}
	if state != "review_queued" || revision != 6 || prURL != "https://github.com/orenvlad-ai/wb-core/pull/987" ||
		prNumber != 987 || head != "e8cca45f3995b8181fe81ead154f7a933dbacbe8" || errorCode != "" || incidentPacket != "" {
		t.Fatalf("recovered task drifted: state=%s rev=%d pr=%s/%d head=%s error=%q packet=%q", state, revision, prURL, prNumber, head, errorCode, incidentPacket)
	}
	var actions, reviewers, reviews, admissions, evidence, notificationPreserved, version int64
	queries := []struct {
		query string
		out   *int64
	}{
		{`SELECT count(*) FROM dcp_model_action`, &actions},
		{`SELECT count(*) FROM dcp_model_action WHERE id='dcp-model-wbc-canary-v1-review-1' AND task_id='wbc-canary-v1' AND session_id='wb-core-1' AND kind='reviewer' AND exact_head_sha='e8cca45f3995b8181fe81ead154f7a933dbacbe8' AND status='queued'`, &reviewers},
		{`SELECT count(*) FROM review_run WHERE session_id='wb-core-1'`, &reviews},
		{`SELECT count(*) FROM dcp_review_lab_admission WHERE session_id='wb-core-1'`, &admissions},
		{`SELECT count(*) FROM dcp_wbc_ci_truth_recovery_v1 WHERE recovery_id='wbc-canary-v1-ci-truth-recovery' AND prior_task_state='incident' AND prior_task_revision=5 AND prior_error_code='ci_identity_failed'`, &evidence},
		{`SELECT count(*) FROM notifications WHERE id='ntf_2c602ac8-9c4c-4db3-8c70-0c6e02a85537' AND resolved_at IS NULL`, &notificationPreserved},
		{`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`, &version},
	}
	for _, item := range queries {
		if err := after.QueryRow(item.query).Scan(item.out); err != nil {
			t.Fatal(err)
		}
	}
	if actions != beforeActions+1 || reviewers != 1 || reviews != 0 || admissions != 0 || evidence != 1 ||
		notificationPreserved != 1 || version != 79 {
		t.Fatalf("recovery counts actions=%d/%d reviewers=%d reviews=%d admissions=%d evidence=%d notification=%d version=%d",
			actions, beforeActions, reviewers, reviews, admissions, evidence, notificationPreserved, version)
	}
	var notifications int64
	if err := after.QueryRow(`SELECT count(*) FROM notifications`).Scan(&notifications); err != nil || notifications != beforeNotifications {
		t.Fatalf("notification history changed: before=%d after=%d err=%v", beforeNotifications, notifications, err)
	}
	var integrity string
	if err := after.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity=%q err=%v", integrity, err)
	}
	var foreignViolations int64
	if err := after.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&foreignViolations); err != nil || foreignViolations != 0 {
		t.Fatalf("foreign key violations=%d err=%v", foreignViolations, err)
	}

	// Goose makes the recovery one-shot across restart/open.
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
	if err := replayed.QueryRow(`SELECT count(*) FROM dcp_wbc_ci_truth_recovery_v1`).Scan(&replayEvidence); err != nil {
		t.Fatal(err)
	}
	if replayActions != actions || replayEvidence != 1 {
		t.Fatalf("restart duplicated recovery: actions=%d/%d evidence=%d", replayActions, actions, replayEvidence)
	}
}

func TestWBCCITruthRecoveryRejectsExactBaselineDriftOnLiveCopy(t *testing.T) {
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
	if _, err := db.Exec(`UPDATE pr_checks SET conclusion='neutral' WHERE pr_url='https://github.com/orenvlad-ai/wb-core/pull/987' AND name='baseline' AND commit_hash='e8cca45f3995b8181fe81ead154f7a933dbacbe8'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if store, err := Open(dir); err == nil {
		_ = store.Close()
		t.Fatal("migration accepted drifted required baseline evidence")
	}
	unchanged := openReadOnlyDB(t, destination)
	defer unchanged.Close()
	assertWBCCITruthPredecessor(t, unchanged)
}

func assertWBCCITruthPredecessor(t *testing.T, db interface {
	QueryRow(query string, args ...any) *sql.Row
}) {
	t.Helper()
	var state, errorCode, packet string
	var revision, actions, reviews, admissions, version int64
	if err := db.QueryRow(`SELECT state, revision, error_code, incident_packet FROM dcp_review_lab_policy_task WHERE task_id='wbc-canary-v1'`).Scan(&state, &revision, &errorCode, &packet); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM dcp_model_action WHERE task_id='wbc-canary-v1'`).Scan(&actions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM review_run WHERE session_id='wb-core-1'`).Scan(&reviews); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM dcp_review_lab_admission WHERE session_id='wb-core-1'`).Scan(&admissions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if state != "incident" || revision != 5 || errorCode != "ci_identity_failed" || packet == "" ||
		actions != 1 || reviews != 0 || admissions != 0 || version != 78 {
		t.Fatalf("predecessor drifted state=%s rev=%d error=%s packet=%q actions=%d reviews=%d admissions=%d version=%d",
			state, revision, errorCode, packet, actions, reviews, admissions, version)
	}
}
