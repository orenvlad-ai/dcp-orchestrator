package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

// TestMigrateAllowsEveryShippedHarness guards against the collapsed-migration
// silent-no-op concern: a hand-written replace() that fails to widen the
// sessions.harness CHECK (because the target substring drifted) leaves the
// schema accepting only the original harnesses while migrate() still reports
// success. This test opens a fresh DB, runs the migrations, and asserts the
// live sessions schema admits every harness the domain ships, building the
// expected set from the domain constants so it can't silently drift.
func TestMigrateAllowsEveryShippedHarness(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var schema string
	if err := db.QueryRow(
		"SELECT sql FROM sqlite_master WHERE type='table' AND name='sessions'",
	).Scan(&schema); err != nil {
		t.Fatalf("read sessions schema: %v", err)
	}

	harnesses := []domain.AgentHarness{
		domain.HarnessClaudeCode,
		domain.HarnessCodex,
		domain.HarnessAider,
		domain.HarnessOpenCode,
		domain.HarnessGrok,
		domain.HarnessDroid,
		domain.HarnessAmp,
		domain.HarnessAgy,
		domain.HarnessCrush,
		domain.HarnessCursor,
		domain.HarnessQwen,
		domain.HarnessCopilot,
		domain.HarnessGoose,
		domain.HarnessAuggie,
		domain.HarnessContinue,
		domain.HarnessDevin,
		domain.HarnessCline,
		domain.HarnessKimi,
		domain.HarnessKiro,
		domain.HarnessKilocode,
		domain.HarnessVibe,
		domain.HarnessPi,
		domain.HarnessAutohand,
	}

	for _, h := range harnesses {
		if !strings.Contains(schema, "'"+string(h)+"'") {
			t.Errorf("sessions.harness CHECK is missing harness %q — the migration that widens it silently no-opped; schema:\n%s", h, schema)
		}
	}
}

func TestArbiterPrelaunchConfigRecoveryPreservesAuditAndRearmsSameIncident(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
CREATE TABLE dcp_review_lab_arbiter_v1 (
    incident_id TEXT PRIMARY KEY,
    generation INTEGER NOT NULL,
    identity_digest TEXT NOT NULL,
    input_digest TEXT NOT NULL,
    status TEXT NOT NULL,
    model_call_count INTEGER NOT NULL,
    error_code TEXT NOT NULL,
    finished_at TIMESTAMP,
    model TEXT NOT NULL,
    reasoning TEXT NOT NULL,
    token_budget INTEGER NOT NULL,
    runtime_handle_id TEXT NOT NULL,
    decision_json TEXT NOT NULL,
    decision_digest TEXT NOT NULL,
    recovery_wake_count INTEGER NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
INSERT INTO dcp_review_lab_arbiter_v1 VALUES (
    'dcp-global-release-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    1,
    'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
    'failed', 1, 'child_failed', '2026-08-11 17:53:31',
    'gpt-5.6-sol', 'xhigh', 16384, 'dcp-global-release-arbiter-v1',
    '', '', 0, '2026-08-11 17:53:31'
);`); err != nil {
		t.Fatal(err)
	}
	migration, err := migrationsFS.ReadFile("migrations/0053_dcp_arbiter_prelaunch_config_recovery.sql")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(migration), "-- +goose Down")
	if len(parts) != 2 {
		t.Fatal("migration lacks one exact down boundary")
	}
	if _, err := db.Exec(parts[0]); err != nil {
		t.Fatalf("apply up: %v", err)
	}
	var status, errorCode string
	var callCount int
	if err := db.QueryRow(`SELECT status, model_call_count, error_code FROM dcp_review_lab_arbiter_v1`).Scan(&status, &callCount, &errorCode); err != nil {
		t.Fatal(err)
	}
	if status != "requested" || callCount != 0 || errorCode != "" {
		t.Fatalf("rearmed row = status:%s calls:%d error:%s", status, callCount, errorCode)
	}
	var auditCount int
	var priorFinished, reason, sourceSHA, contractSHA string
	if err := db.QueryRow(`
SELECT count(*), prior_finished_at, recovery_reason,
       failed_launcher_source_sha, correction_contract_sha
FROM dcp_review_lab_arbiter_v1_prelaunch_recovery
`).Scan(&auditCount, &priorFinished, &reason, &sourceSHA, &contractSHA); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 || priorFinished == "" || reason != "strict_config_top_level_rollout_budget_rejected" ||
		sourceSHA != "d5f9fd4b3459596fcb2d79efc0023bad4f7f0aa0" || contractSHA != "4d3e0736635579db053516813e2d5944f903f777" {
		t.Fatalf("recovery audit = count:%d finished:%s reason:%s source:%s contract:%s", auditCount, priorFinished, reason, sourceSHA, contractSHA)
	}
}

func TestArbiterResponseSchemaRecoveryPreservesExactPreInferenceAudit(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
CREATE TABLE dcp_review_lab_arbiter_v1 (
    incident_id TEXT PRIMARY KEY, generation INTEGER NOT NULL,
    identity_digest TEXT NOT NULL, input_digest TEXT NOT NULL,
    source_packet_digest TEXT NOT NULL, task_id TEXT NOT NULL,
    session_id TEXT NOT NULL, pr_number INTEGER NOT NULL, target_sha TEXT NOT NULL,
    status TEXT NOT NULL, model_call_count INTEGER NOT NULL, error_code TEXT NOT NULL,
    finished_at TIMESTAMP, model TEXT NOT NULL, reasoning TEXT NOT NULL,
    token_budget INTEGER NOT NULL, runtime_handle_id TEXT NOT NULL,
    decision_json TEXT NOT NULL, decision_digest TEXT NOT NULL,
    recovery_wake_count INTEGER NOT NULL, updated_at TIMESTAMP NOT NULL
);
CREATE TABLE dcp_review_lab_arbiter_v1_prelaunch_recovery (
    incident_id TEXT PRIMARY KEY, identity_digest TEXT NOT NULL, input_digest TEXT NOT NULL
);
INSERT INTO dcp_review_lab_arbiter_v1 VALUES (
    'dcp-global-release-2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60',
    1,
    '2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60',
    'f618fa8a46715acce0958b592384f0d42c071562e36988163e2b96f2c157fc49',
    'fab52d627d14a21ea7ab2a7fdadb4d6f53478d5cdc496858ca74c37e1dfda057',
    'i13-arbiter-b', 'dcp-review-lab-12', 9,
    'd4fcb68051ae113ed497d02151a759800ee85633',
    'failed', 1, 'child_failed', '2026-08-11 18:37:16',
    'gpt-5.6-sol', 'xhigh', 16384, 'dcp-global-release-arbiter-v1',
    '', '', 0, '2026-08-11 18:37:16'
);
INSERT INTO dcp_review_lab_arbiter_v1_prelaunch_recovery VALUES (
    'dcp-global-release-2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60',
    '2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60',
    'f618fa8a46715acce0958b592384f0d42c071562e36988163e2b96f2c157fc49'
);`); err != nil {
		t.Fatal(err)
	}
	migration, err := migrationsFS.ReadFile("migrations/0054_dcp_arbiter_response_schema_recovery.sql")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(migration), "-- +goose Down")
	if len(parts) != 2 {
		t.Fatal("migration lacks one exact down boundary")
	}
	if _, err := db.Exec(parts[0]); err != nil {
		t.Fatalf("apply up: %v", err)
	}
	var status, errorCode string
	var callCount int
	if err := db.QueryRow(`SELECT status, model_call_count, error_code FROM dcp_review_lab_arbiter_v1`).Scan(&status, &callCount, &errorCode); err != nil {
		t.Fatal(err)
	}
	if status != "requested" || callCount != 0 || errorCode != "" {
		t.Fatalf("rearmed row = status:%s calls:%d error:%s", status, callCount, errorCode)
	}
	var sessionID, providerCode, reason string
	var resultPresent, tokenPresent int
	if err := db.QueryRow(`
SELECT codex_session_id, provider_error_code, result_artifact_present,
       token_record_present, recovery_reason
FROM dcp_review_lab_arbiter_v1_schema_recovery
`).Scan(&sessionID, &providerCode, &resultPresent, &tokenPresent, &reason); err != nil {
		t.Fatal(err)
	}
	if sessionID != "019ff21d-4cde-72d1-b70d-49efd3cd1c17" || providerCode != "invalid_json_schema" ||
		resultPresent != 0 || tokenPresent != 0 || reason != "unsupported_root_oneof_rejected_before_inference" {
		t.Fatalf("schema recovery audit = session:%s provider:%s result:%d tokens:%d reason:%s", sessionID, providerCode, resultPresent, tokenPresent, reason)
	}
}

func TestArbiterSuccessorMigrationPreservesRejectedRowAndAddsOneExactAttempt(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
CREATE TABLE dcp_review_lab_admission (
    id TEXT PRIMARY KEY, sequence INTEGER NOT NULL, session_id TEXT NOT NULL,
    review_run_id TEXT NOT NULL, pr_url TEXT NOT NULL, pr_number INTEGER NOT NULL,
    target_sha TEXT NOT NULL, status TEXT NOT NULL, lease_id TEXT NOT NULL,
    error_code TEXT NOT NULL
);
CREATE TABLE dcp_review_lab_arbiter_v1 (
    incident_id TEXT PRIMARY KEY, generation INTEGER NOT NULL,
    identity_digest TEXT NOT NULL, input_digest TEXT NOT NULL,
    admission_id TEXT NOT NULL, incident_lease_id TEXT NOT NULL,
    source_packet_digest TEXT NOT NULL, task_id TEXT NOT NULL,
    session_id TEXT NOT NULL, pr_url TEXT NOT NULL, pr_number INTEGER NOT NULL,
    target_sha TEXT NOT NULL, current_base_sha TEXT NOT NULL,
    review_run_id TEXT NOT NULL, status TEXT NOT NULL,
    model_call_count INTEGER NOT NULL, decision_json TEXT NOT NULL,
    decision_digest TEXT NOT NULL, recovery_wake_count INTEGER NOT NULL,
    error_code TEXT NOT NULL, finished_at TIMESTAMP,
    model TEXT NOT NULL, reasoning TEXT NOT NULL, token_budget INTEGER NOT NULL
);
CREATE TABLE dcp_review_lab_arbiter_v1_prelaunch_recovery (incident_id TEXT PRIMARY KEY);
CREATE TABLE dcp_review_lab_arbiter_v1_schema_recovery (incident_id TEXT PRIMARY KEY);
INSERT INTO dcp_review_lab_admission VALUES (
    'dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34', 4,
    'dcp-review-lab-12', 'ecb500ad-f9f0-443b-9d73-2c8a6350ce34',
    'https://github.com/orenvlad-ai/dcp-review-lab/pull/9', 9,
    'd4fcb68051ae113ed497d02151a759800ee85633', 'incident',
    'dcp-incident-dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34',
    'merge_conflict_or_ambiguity'
);
INSERT INTO dcp_review_lab_arbiter_v1 VALUES (
    'dcp-global-release-2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60', 1,
    '2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60',
    'f618fa8a46715acce0958b592384f0d42c071562e36988163e2b96f2c157fc49',
    'dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34',
    'dcp-incident-dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34',
    'fab52d627d14a21ea7ab2a7fdadb4d6f53478d5cdc496858ca74c37e1dfda057',
    'i13-arbiter-b', 'dcp-review-lab-12',
    'https://github.com/orenvlad-ai/dcp-review-lab/pull/9', 9,
    'd4fcb68051ae113ed497d02151a759800ee85633',
    'b34b31b5443890e69128db2862726950a6bbac0d',
    'ecb500ad-f9f0-443b-9d73-2c8a6350ce34',
    'failed', 1, '', '', 0, 'submit_failed', '2026-08-11 19:11:47',
    'gpt-5.6-sol', 'xhigh', 16384
);
INSERT INTO dcp_review_lab_arbiter_v1_prelaunch_recovery VALUES (
    'dcp-global-release-2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60'
);
INSERT INTO dcp_review_lab_arbiter_v1_schema_recovery VALUES (
    'dcp-global-release-2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60'
);`); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := db.QueryRow(`SELECT status || ':' || model_call_count || ':' || error_code || ':' || decision_json || ':' || recovery_wake_count FROM dcp_review_lab_arbiter_v1`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	migration, err := migrationsFS.ReadFile("migrations/0055_dcp_arbiter_successor_attempt.sql")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(migration), "-- +goose Down")
	if len(parts) != 2 {
		t.Fatal("migration lacks one exact down boundary")
	}
	if _, err := db.Exec(parts[0]); err != nil {
		t.Fatalf("apply up: %v", err)
	}
	var after string
	if err := db.QueryRow(`SELECT status || ':' || model_call_count || ':' || error_code || ':' || decision_json || ':' || recovery_wake_count FROM dcp_review_lab_arbiter_v1`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before || after != "failed:1:submit_failed::0" {
		t.Fatalf("original row changed: before=%q after=%q", before, after)
	}
	var count, attemptGeneration, calls, maxWorkers, maxReviews int
	var attemptID, status, contract string
	if err := db.QueryRow(`
SELECT count(*), attempt_id, attempt_generation, status, model_call_count,
       policy_max_worker_calls, policy_max_fresh_reviews, contract_commit
FROM dcp_review_lab_arbiter_v1_successor_attempt
`).Scan(&count, &attemptID, &attemptGeneration, &status, &calls, &maxWorkers, &maxReviews, &contract); err != nil {
		t.Fatal(err)
	}
	if count != 1 || attemptID != "dcp-arbiter-successor-3c62ea80b56ef94165519d4f01e4c449c320bff22d16b902dd68d4a1a355ea7d" ||
		attemptGeneration != 2 || status != "authorized" || calls != 0 || maxWorkers != 1 || maxReviews != 1 ||
		contract != "4dfff558ac425080d62bd6fe2fb13b573ef50661" {
		t.Fatalf("successor row = count:%d id:%s generation:%d status:%s calls:%d policy:%d/%d contract:%s", count, attemptID, attemptGeneration, status, calls, maxWorkers, maxReviews, contract)
	}
	store := sqlitestore.NewStore(db, db)
	attempt, found, err := store.GetDCPReleaseArbiterSuccessorAttemptByID(context.Background(), attemptID)
	if err != nil || !found {
		t.Fatalf("get successor attempt: found=%v err=%v", found, err)
	}
	attempt.InputJSON = `{"schemaVersion":"dcp.review-lab.global-release-arbiter-successor-input/v1"}`
	attempt.InputDigest = strings.Repeat("a", 64)
	prepared, err := store.PrepareDCPReleaseArbiterSuccessorAttempt(context.Background(), attempt, time.Now())
	if err != nil || !prepared {
		t.Fatalf("prepare successor attempt: prepared=%v err=%v", prepared, err)
	}
	if duplicate, err := store.PrepareDCPReleaseArbiterSuccessorAttempt(context.Background(), attempt, time.Now()); err != nil || duplicate {
		t.Fatalf("duplicate prepare was not inert: updated=%v err=%v", duplicate, err)
	}
	started, err := store.StartDCPReleaseArbiterSuccessorCall(context.Background(), attempt, time.Now())
	if err != nil || !started {
		t.Fatalf("start successor call: started=%v err=%v", started, err)
	}
	if duplicate, err := store.StartDCPReleaseArbiterSuccessorCall(context.Background(), attempt, time.Now()); err != nil || duplicate {
		t.Fatalf("duplicate call fence was not inert: updated=%v err=%v", duplicate, err)
	}
	decisionJSON := `{"schemaVersion":"dcp.review-lab.global-release-arbiter-successor-decision/v1"}`
	decisionDigest := strings.Repeat("b", 64)
	recorded, err := store.RecordDCPReleaseArbiterSuccessorDecision(context.Background(), attempt, decisionJSON, decisionDigest, false, "", time.Now())
	if err != nil || !recorded {
		t.Fatalf("record successor decision: recorded=%v err=%v", recorded, err)
	}
	if duplicate, err := store.RecordDCPReleaseArbiterSuccessorDecision(context.Background(), attempt, decisionJSON, decisionDigest, false, "", time.Now()); err != nil || duplicate {
		t.Fatalf("duplicate decision was not inert: updated=%v err=%v", duplicate, err)
	}
	attempt.DecisionDigest = decisionDigest
	consumed, err := store.ConsumeDCPReleaseArbiterSuccessorRepair(context.Background(), attempt, time.Now())
	if err != nil || !consumed {
		t.Fatalf("consume successor recovery: consumed=%v err=%v", consumed, err)
	}
	if duplicate, err := store.ConsumeDCPReleaseArbiterSuccessorRepair(context.Background(), attempt, time.Now()); err != nil || duplicate {
		t.Fatalf("duplicate recovery wake was not inert: updated=%v err=%v", duplicate, err)
	}
	if _, err := db.Exec(parts[1]); err == nil {
		t.Fatal("rollback erased a fenced successor attempt")
	}
	if err := db.QueryRow(`SELECT count(*) FROM dcp_review_lab_arbiter_v1_successor_attempt`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("successor evidence did not survive refused rollback: count=%d err=%v", count, err)
	}
	if _, err := db.Exec(`
UPDATE dcp_review_lab_arbiter_v1_successor_attempt
SET status = 'failed',
    input_json = '{"schemaVersion":"dcp.review-lab.global-release-arbiter-successor-input/v1"}',
    input_digest = 'aa44c625c940048d5e0266dac23dd4835a1afcf7648116a056758093b67160e6',
    decision_json = '', decision_digest = '', decision_at = NULL,
    recovery_owner_session_id = '', recovery_path = '', recovery_wake_count = 0,
    error_code = 'submit_failed', finished_at = '2099-08-12 01:41:41',
    updated_at = '2099-08-12 01:41:41'
`); err != nil {
		t.Fatal(err)
	}
	validationMigration, err := migrationsFS.ReadFile("migrations/0056_dcp_arbiter_successor_result_validation_recovery.sql")
	if err != nil {
		t.Fatal(err)
	}
	validationParts := strings.Split(string(validationMigration), "-- +goose Down")
	if len(validationParts) != 2 {
		t.Fatal("validation migration lacks one exact down boundary")
	}
	if _, err := db.Exec(validationParts[0]); err != nil {
		t.Fatalf("apply validation recovery: %v", err)
	}
	recovery, found, err := store.GetDCPArbiterSuccessorValidationRecovery(context.Background(), attemptID)
	if err != nil || !found || recovery.Status != "pending" || recovery.PriorErrorCode != "submit_failed" ||
		recovery.ResultArtifactDigest != "9b5ff7847db2533e56bdbbc424114e5bea8e5e3c352ad1d029a99deaba05c172" ||
		recovery.CodexSessionID != "019ff3a1-7f0e-79e2-baa5-cbaa1cc6fc37" || recovery.TokenCount != 12271 {
		t.Fatalf("exact validation audit = found:%v err:%v row:%+v", found, err, recovery)
	}
	attempt, found, err = store.GetDCPReleaseArbiterSuccessorAttemptByID(context.Background(), attemptID)
	if err != nil || !found {
		t.Fatalf("reload failed successor: found=%v err=%v", found, err)
	}
	recovered, err := store.RecoverDCPArbiterSuccessorExactDecision(context.Background(), attempt, decisionJSON, decisionDigest, time.Now())
	if err != nil || !recovered {
		t.Fatalf("recover exact decision: recovered=%v err=%v", recovered, err)
	}
	if duplicate, err := store.RecoverDCPArbiterSuccessorExactDecision(context.Background(), attempt, decisionJSON, decisionDigest, time.Now()); err != nil || duplicate {
		t.Fatalf("duplicate exact result recovery was not inert: recovered=%v err=%v", duplicate, err)
	}
	if _, err := db.Exec(validationParts[1]); err == nil {
		t.Fatal("rollback erased a consumed validation recovery audit")
	}
}

func TestOpenReadOnlyDoesNotCreateDatabase(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "missing")
	if _, err := OpenReadOnly(context.Background(), dataDir); err == nil {
		t.Fatal("OpenReadOnly succeeded for missing database")
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("data dir stat err = %v, want not exist", err)
	}
}

func TestOpenReadOnlyDoesNotMigrate(t *testing.T) {
	dataDir := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dataDir, "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    path TEXT NOT NULL,
    repo_origin_url TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    registered_at TIMESTAMP NOT NULL,
    archived_at TIMESTAMP
);
INSERT INTO projects (id, path, registered_at) VALUES ('alpha', '/repos/alpha', ?);
`, time.Unix(100, 0).UTC()); err != nil {
		_ = db.Close()
		t.Fatalf("seed old schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	store, err := OpenReadOnly(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.ListProjects(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no such column") {
		t.Fatalf("ListProjects err = %v, want old-schema column failure", err)
	}

	checkDB, err := sql.Open("sqlite", "file:"+filepath.Join(dataDir, "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open check db: %v", err)
	}
	defer func() { _ = checkDB.Close() }()

	var schema string
	if err := checkDB.QueryRow(
		"SELECT sql FROM sqlite_master WHERE type='table' AND name='projects'",
	).Scan(&schema); err != nil {
		t.Fatalf("read projects schema: %v", err)
	}
	if strings.Contains(schema, "config") || strings.Contains(schema, "kind") {
		t.Fatalf("OpenReadOnly migrated projects schema:\n%s", schema)
	}
}

func TestMigrateI11FromI8SchemaPreservesSessions(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)

	// Version 47 is the exact pre-I11 physical boundary. Seed the existing AO
	// registry/session through its typed store, then apply the additive slice.
	upTo(t, db, 47)
	ctx := context.Background()
	store := sqlitestore.NewStore(db, db)
	now := time.Unix(300, 0).UTC()
	if err := store.UpsertProject(ctx, domain.ProjectRecord{
		ID:           "dcp-lab",
		Path:         "/tmp/dcp-lab",
		DisplayName:  "DCP Lab",
		RegisteredAt: now,
		Kind:         domain.ProjectKindSingleRepo,
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	session, err := store.CreateSession(ctx, domain.SessionRecord{
		ProjectID: domain.ProjectID("dcp-lab"),
		Kind:      domain.KindWorker,
		Harness:   domain.HarnessCodex,
		Activity: domain.Activity{
			State:          domain.ActivityIdle,
			LastActivityAt: now,
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}

	if err := migrate(db); err != nil {
		t.Fatalf("migrate I11: %v", err)
	}
	preserved, ok, err := store.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("get preserved session: %v", err)
	}
	if !ok || preserved.ID != session.ID || preserved.Harness != domain.HarnessCodex {
		t.Fatalf("session after I11 migration = %+v ok=%v", preserved, ok)
	}
	for _, table := range []string{"dcp_tasks", "dcp_task_events"} {
		var count int
		if err := db.QueryRow(
			"SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&count); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("table %s count = %d, want 1", table, count)
		}
	}
}
