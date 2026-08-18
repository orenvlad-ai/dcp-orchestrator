package sqlite

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

const taskFirstLifecycleLiveCopyEnv = "DCP_TASK_FIRST_LIFECYCLE_SCHEMA82_DB"

// TestTaskFirstNativeLifecycleRecoveryOnExactSchema82Copy is model-free and
// opt-in. The source must be a disposable immutable copy, never the live DB.
func TestTaskFirstNativeLifecycleRecoveryOnExactSchema82Copy(t *testing.T) {
	source := os.Getenv(taskFirstLifecycleLiveCopyEnv)
	if source == "" {
		t.Skip(taskFirstLifecycleLiveCopyEnv + " is not set")
	}
	before := openReadOnlyDB(t, source)
	assertTaskFirstLifecycleSchema82(t, before)
	var tasks, sessions, actions, reviews, admissions, generations int64
	countQueries := []struct {
		query string
		out   *int64
	}{
		{`SELECT count(*) FROM dcp_review_lab_policy_task`, &tasks},
		{`SELECT count(*) FROM sessions`, &sessions},
		{`SELECT count(*) FROM dcp_model_action`, &actions},
		{`SELECT count(*) FROM review_run`, &reviews},
		{`SELECT count(*) FROM dcp_review_lab_admission`, &admissions},
		{`SELECT count(*) FROM dcp_wbc_readmission_generation`, &generations},
	}
	for _, item := range countQueries {
		if err := before.QueryRow(item.query).Scan(item.out); err != nil {
			t.Fatal(err)
		}
	}
	_ = before.Close()

	dir := t.TempDir()
	destination := filepath.Join(dir, "ao.db")
	copyRecoveryFixture(t, source, destination)
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("migrate exact schema-82 task-first copy: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	after := openReadOnlyDB(t, destination)
	defer after.Close()
	var state, sessionID, admissionID, recoveryStatus, authority string
	var revision, sequence, version, evidence int64
	if err := after.QueryRow(`SELECT state, revision, session_id, admission_id FROM dcp_review_lab_policy_task WHERE task_id='wbc-canary-v1'`).Scan(&state, &revision, &sessionID, &admissionID); err != nil {
		t.Fatal(err)
	}
	if err := after.QueryRow(`SELECT sequence FROM dcp_review_lab_admission WHERE id=? AND status='waiting' AND lease_id=''`, admissionID).Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	if err := after.QueryRow(`SELECT count(*), status, authority FROM dcp_task_first_native_lifecycle_recovery_v1 WHERE task_id='wbc-canary-v1'`).Scan(&evidence, &recoveryStatus, &authority); err != nil {
		t.Fatal(err)
	}
	if err := after.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if state != "admission_waiting" || revision != 23 || sessionID != "wb-core-1" || sequence != 32 || evidence != 1 ||
		recoveryStatus != "applied" || authority != "rearm_exact_archived_task_for_common_non_model_admission_continuation" || version != 83 {
		t.Fatalf("task-first recovery drifted state=%s revision=%d session=%s admission=%s sequence=%d evidence=%d/%s authority=%s version=%d",
			state, revision, sessionID, admissionID, sequence, evidence, recoveryStatus, authority, version)
	}
	for _, item := range countQueries {
		var got int64
		if err := after.QueryRow(item.query).Scan(&got); err != nil || got != *item.out {
			t.Fatalf("domain-row count drift for %q: got=%d want=%d err=%v", item.query, got, *item.out, err)
		}
	}
	var active, preserved82 int64
	if err := after.QueryRow(`SELECT count(*) FROM dcp_model_action WHERE status IN ('claimed','running')`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := after.QueryRow(`SELECT count(*) FROM dcp_wbc_readmission_waiting_recovery_v1 WHERE prior_error_code='waiting_identity_drift' AND length(prior_incident_packet)>0`).Scan(&preserved82); err != nil {
		t.Fatal(err)
	}
	if active != 0 || preserved82 != 1 {
		t.Fatalf("model/incident preservation drifted active=%d preserved82=%d", active, preserved82)
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
	var replayRevision, replayEvidence int64
	if err := replayed.QueryRow(`SELECT revision FROM dcp_review_lab_policy_task WHERE task_id='wbc-canary-v1'`).Scan(&replayRevision); err != nil {
		t.Fatal(err)
	}
	if err := replayed.QueryRow(`SELECT count(*) FROM dcp_task_first_native_lifecycle_recovery_v1`).Scan(&replayEvidence); err != nil {
		t.Fatal(err)
	}
	if replayRevision != 23 || replayEvidence != 1 {
		t.Fatalf("restart duplicated task-first recovery revision=%d evidence=%d", replayRevision, replayEvidence)
	}
}

func TestTaskFirstNativeLifecycleRecoveryRejectsIdentityDrift(t *testing.T) {
	source := os.Getenv(taskFirstLifecycleLiveCopyEnv)
	if source == "" {
		t.Skip(taskFirstLifecycleLiveCopyEnv + " is not set")
	}
	for name, mutate := range map[string]string{
		"native shell": `UPDATE sessions SET activity_state='idle' WHERE id='wb-core-1'`,
		"task head":    `UPDATE dcp_review_lab_policy_task SET current_head_sha='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', revision=23 WHERE task_id='wbc-canary-v1'`,
		"admission":    `UPDATE dcp_review_lab_admission SET status='claimed', lease_id='foreign' WHERE sequence=32`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			destination := filepath.Join(dir, "ao.db")
			copyRecoveryFixture(t, source, destination)
			db, err := sql.Open("sqlite", "file:"+destination+pragmas)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(mutate); err != nil {
				_ = db.Close()
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if migrated, err := Open(dir); err == nil {
				_ = migrated.Close()
				t.Fatal("schema-83 migration accepted drifted exact identity")
			}
			unchanged := openReadOnlyDB(t, destination)
			defer unchanged.Close()
			var version, table int64
			if err := unchanged.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&version); err != nil {
				t.Fatal(err)
			}
			if err := unchanged.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='dcp_task_first_native_lifecycle_recovery_v1'`).Scan(&table); err != nil {
				t.Fatal(err)
			}
			if version != 82 || table != 0 {
				t.Fatalf("rejected schema-83 migration was not atomic: version=%d table=%d", version, table)
			}
		})
	}
}

func assertTaskFirstLifecycleSchema82(t *testing.T, db interface {
	QueryRow(query string, args ...any) *sql.Row
}) {
	t.Helper()
	var state, activity, admissionStatus, generationStatus string
	var revision, terminated, actions, active, reviews, admissions, generations, version int64
	queries := []struct {
		query string
		args  []any
		scan  []any
	}{
		{`SELECT state, revision FROM dcp_review_lab_policy_task WHERE task_id='wbc-canary-v1'`, nil, []any{&state, &revision}},
		{`SELECT activity_state, is_terminated FROM sessions WHERE id='wb-core-1'`, nil, []any{&activity, &terminated}},
		{`SELECT status FROM dcp_review_lab_admission WHERE sequence=32`, nil, []any{&admissionStatus}},
		{`SELECT status FROM dcp_wbc_readmission_generation WHERE task_id='wbc-canary-v1'`, nil, []any{&generationStatus}},
		{`SELECT count(*), sum(status IN ('claimed','running')) FROM dcp_model_action`, nil, []any{&actions, &active}},
		{`SELECT count(*) FROM review_run`, nil, []any{&reviews}},
		{`SELECT count(*) FROM dcp_review_lab_admission`, nil, []any{&admissions}},
		{`SELECT count(*) FROM dcp_wbc_readmission_generation`, nil, []any{&generations}},
		{`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`, nil, []any{&version}},
	}
	for _, item := range queries {
		if err := db.QueryRow(item.query, item.args...).Scan(item.scan...); err != nil {
			t.Fatal(err)
		}
	}
	if state != "admission_waiting" || revision != 22 || activity != "exited" || terminated != 1 ||
		admissionStatus != "waiting" || generationStatus != "admitted" || actions != 73 || active != 0 ||
		reviews != 46 || admissions != 32 || generations != 1 || version != 82 {
		t.Fatalf("schema-82 predecessor drifted state=%s/%d shell=%s/%d admission=%s generation=%s actions=%d/%d reviews=%d admissions=%d generations=%d version=%d",
			state, revision, activity, terminated, admissionStatus, generationStatus, actions, active, reviews, admissions, generations, version)
	}
}
