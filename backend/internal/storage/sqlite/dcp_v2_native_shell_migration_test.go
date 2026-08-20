package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

const dcpV2Stage6Schema84CopyEnv = "DCP_V2_STAGE6_SCHEMA84_DB"

func TestDCPV2TwinNativeShellMigrationOpensOnlyExactIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ao.db")
	db, err := sql.Open("sqlite", "file:"+path+pragmas)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migrateDCPV2TestTo(t, db, 84)
	before := dcpV2PredecessorSnapshot(t, db)
	migrateDCPV2TestTo(t, db, 85)
	after := dcpV2PredecessorSnapshot(t, db)
	if len(before) != len(after) {
		t.Fatalf("predecessor snapshot table count changed: before=%d after=%d", len(before), len(after))
	}
	for table, digest := range before {
		if after[table] != digest {
			t.Fatalf("predecessor %s changed: before=%s after=%s", table, digest, after[table])
		}
	}
	var version int64
	var schema string
	if err := db.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='dcp_review_lab_policy_task'`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if version != 85 || !strings.Contains(schema, "task_id = 'dcp-v2-twin-canary-v1'") ||
		!strings.Contains(schema, "target = 'dcp-wbc-integration-lab'") {
		t.Fatalf("schema=%d does not contain the exact twin native-shell identity", version)
	}
	store := sqlitestore.NewStore(db, db)
	now := time.Date(2026, 8, 20, 17, 30, 0, 0, time.UTC)
	if err := store.UpsertProject(t.Context(), domain.ProjectRecord{ID: "dcp-wbc-integration-lab",
		Path: filepath.Join(t.TempDir(), "dcp-wbc-integration-lab"), RepoOriginURL: "https://github.com/orenvlad-ai/dcp-wbc-integration-lab.git",
		DisplayName: "dcp-wbc-integration-lab", RegisteredAt: now, Kind: domain.ProjectKindSingleRepo}); err != nil {
		t.Fatal(err)
	}
	seed := domain.SessionRecord{ProjectID: "dcp-wbc-integration-lab", Kind: domain.KindWorker, Harness: domain.HarnessCodex,
		DisplayName: "DCP:dcp-v2-twin-canary-v1", Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: now},
		CreatedAt: now, UpdatedAt: now}
	task := domain.DCPReviewLabPolicyTask{TaskID: "dcp-v2-twin-canary-v1", PayloadJSON: `{}`,
		PayloadDigest: strings.Repeat("a", 64), Target: "dcp-wbc-integration-lab", Profile: "live-runtime",
		Repository: "orenvlad-ai/dcp-wbc-integration-lab", PolicyVersion: domain.DCPWBCIntegrationTwinPolicyVersion,
		Prompt: "exact twin", State: domain.DCPPolicyReserved, Revision: 1, CreatedAt: now, UpdatedAt: now}
	reserved, err := store.ReserveDCPReviewLabPolicyTask(t.Context(), task, seed, filepath.Join(t.TempDir(), "worktrees"))
	if err != nil || !reserved.Created || reserved.Task.SessionID != "dcp-wbc-integration-lab-1" {
		t.Fatalf("reserve exact twin shell: reserved=%+v err=%v", reserved, err)
	}
}

func TestDCPV2TwinNativeShellMigrationRejectsLongOrForeignIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ao.db")
	db, err := sql.Open("sqlite", "file:"+path+pragmas)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migrateDCPV2TestTo(t, db, 85)
	store := sqlitestore.NewStore(db, db)
	now := time.Date(2026, 8, 20, 17, 30, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "worktrees")
	for _, project := range []domain.ProjectRecord{
		{ID: "dcp-wbc-integration-lab", Path: filepath.Join(t.TempDir(), "dcp-wbc-integration-lab"), RepoOriginURL: "https://github.com/orenvlad-ai/dcp-wbc-integration-lab.git", DisplayName: "twin", RegisteredAt: now, Kind: domain.ProjectKindSingleRepo},
		{ID: "wb-core", Path: filepath.Join(t.TempDir(), "wb-core"), RepoOriginURL: "https://github.com/orenvlad-ai/wb-core.git", DisplayName: "wbc", RegisteredAt: now, Kind: domain.ProjectKindSingleRepo},
	} {
		if err := store.UpsertProject(t.Context(), project); err != nil {
			t.Fatal(err)
		}
	}
	for _, task := range []domain.DCPReviewLabPolicyTask{
		{TaskID: "foreign-long-canary-v1", PayloadJSON: `{}`, PayloadDigest: strings.Repeat("b", 64),
			Target: "dcp-wbc-integration-lab", Profile: "live-runtime", Repository: "orenvlad-ai/dcp-wbc-integration-lab",
			PolicyVersion: domain.DCPWBCIntegrationTwinPolicyVersion, Prompt: "foreign long", State: domain.DCPPolicyReserved,
			Revision: 1, CreatedAt: now, UpdatedAt: now},
		{TaskID: "dcp-v2-twin-canary-v1", PayloadJSON: `{}`, PayloadDigest: strings.Repeat("c", 64),
			Target: "wb-core", Profile: "repo-only", Repository: "orenvlad-ai/wb-core",
			PolicyVersion: domain.DCPWBCRepoOnlyPolicyVersion, Prompt: "foreign tuple", State: domain.DCPPolicyReserved,
			Revision: 1, CreatedAt: now, UpdatedAt: now},
	} {
		seed := domain.SessionRecord{ProjectID: domain.ProjectID(task.Target), Kind: domain.KindWorker, Harness: domain.HarnessCodex,
			DisplayName: "DCP:" + task.TaskID, Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: now},
			CreatedAt: now, UpdatedAt: now}
		if reserved, err := store.ReserveDCPReviewLabPolicyTask(t.Context(), task, seed, root); err == nil || reserved.Created {
			t.Fatalf("forbidden identity reserved=%+v err=%v", reserved, err)
		}
	}
	var sessions, tasks, actions int64
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM sessions), (SELECT count(*) FROM dcp_review_lab_policy_task), (SELECT count(*) FROM dcp_model_action)`).Scan(&sessions, &tasks, &actions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 || tasks != 0 || actions != 0 {
		t.Fatalf("failed identity reservation was not atomic: sessions/tasks/actions=%d/%d/%d", sessions, tasks, actions)
	}
}

// TestDCPV2TwinNativeShellMigrationOnExactSchema84Copy is opt-in and
// model-free. The source remains read-only; VACUUM INTO creates the only
// writable disposable copy used by migration 0085.
func TestDCPV2TwinNativeShellMigrationOnExactSchema84Copy(t *testing.T) {
	source := os.Getenv(dcpV2Stage6Schema84CopyEnv)
	if source == "" {
		t.Skip(dcpV2Stage6Schema84CopyEnv + " is not set")
	}
	beforeDB := openReadOnlyDB(t, source)
	var version int64
	if err := beforeDB.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&version); err != nil || version != 84 {
		t.Fatalf("exact-copy source schema=%d want=84 err=%v", version, err)
	}
	beforeRows := dcpV2PredecessorSnapshot(t, beforeDB)
	beforeFence := dcpV2Stage6RawFence(t, beforeDB)
	destination := filepath.Join(t.TempDir(), "ao.db")
	if _, err := beforeDB.Exec(`VACUUM INTO ?`, destination); err != nil {
		t.Fatalf("create disposable exact schema-84 copy: %v", err)
	}
	if err := beforeDB.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+destination+pragmas)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migrateDCPV2TestTo(t, db, 85)
	afterRows := dcpV2PredecessorSnapshot(t, db)
	if fmt.Sprint(beforeRows) != fmt.Sprint(afterRows) {
		t.Fatalf("schema-85 changed predecessor rows\nbefore=%v\nafter=%v", beforeRows, afterRows)
	}
	if afterFence := dcpV2Stage6RawFence(t, db); afterFence != beforeFence {
		t.Fatalf("schema-85 changed durable Stage 6 fence\nbefore=%s\nafter=%s", beforeFence, afterFence)
	}
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity=%q err=%v", integrity, err)
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("schema-85 exact copy has a foreign-key violation")
	}
}

func dcpV2Stage6RawFence(t *testing.T, db interface {
	QueryRow(query string, args ...any) *sql.Row
}) string {
	t.Helper()
	var taskID, revisionID, commandID, commandStatus, actionID, actionStatus, nativeTaskID string
	var tasks, revisions, commands, actions, admissions, results, incidents, events, native, sessions int64
	if err := db.QueryRow(`
SELECT
  (SELECT count(*) FROM dcp_v2_task),
  (SELECT count(*) FROM dcp_v2_revision),
  (SELECT count(*) FROM dcp_v2_command),
  (SELECT count(*) FROM dcp_v2_action),
  (SELECT count(*) FROM dcp_v2_admission),
  (SELECT count(*) FROM dcp_v2_result),
  (SELECT count(*) FROM dcp_v2_incident),
  (SELECT count(*) FROM dcp_v2_external_event),
  (SELECT count(*) FROM dcp_review_lab_policy_task WHERE task_id='dcp-v2-twin-canary-v1'),
  (SELECT count(*) FROM sessions WHERE project_id='dcp-wbc-integration-lab'),
  (SELECT task_id FROM dcp_v2_task LIMIT 1),
  (SELECT revision_id FROM dcp_v2_revision LIMIT 1),
  (SELECT command_id FROM dcp_v2_command LIMIT 1),
  (SELECT status FROM dcp_v2_command LIMIT 1),
  (SELECT action_id FROM dcp_v2_action LIMIT 1),
  (SELECT status FROM dcp_v2_action LIMIT 1),
  COALESCE((SELECT task_id FROM dcp_review_lab_policy_task WHERE task_id='dcp-v2-twin-canary-v1'), '')
`).Scan(&tasks, &revisions, &commands, &actions, &admissions, &results, &incidents, &events, &native, &sessions,
		&taskID, &revisionID, &commandID, &commandStatus, &actionID, &actionStatus, &nativeTaskID); err != nil {
		t.Fatal(err)
	}
	if tasks != 1 || revisions != 1 || commands != 1 || actions != 1 || admissions != 0 || results != 0 || incidents != 0 || events != 0 || native != 0 || sessions != 0 ||
		taskID != "dcp-v2-twin-canary-v1" || revisionID != "v2-13f81f321f99d1117dc931419e0bea3945ee35a5" ||
		commandID != "v2-e028f779a18417e990911057f7db7c666f7487ca" || commandStatus != "leased" ||
		actionID != "v2-40f87d048813533daa1108b4316c09139acf0a8f" || actionStatus != "launching" || nativeTaskID != "" {
		t.Fatalf("unexpected exact Stage 6 fence counts=%d/%d/%d/%d/%d/%d/%d/%d native/session=%d/%d ids=%s/%s/%s/%s/%s/%s/%s",
			tasks, revisions, commands, actions, admissions, results, incidents, events, native, sessions,
			taskID, revisionID, commandID, commandStatus, actionID, actionStatus, nativeTaskID)
	}
	return fmt.Sprintf("%d/%d/%d/%d/%d/%d/%d/%d/%d/%d:%s:%s:%s:%s:%s:%s:%s", tasks, revisions, commands, actions,
		admissions, results, incidents, events, native, sessions, taskID, revisionID, commandID, commandStatus, actionID, actionStatus, nativeTaskID)
}
