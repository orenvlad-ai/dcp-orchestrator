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
