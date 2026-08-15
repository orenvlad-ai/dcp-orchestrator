package sqlite

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExactRepoOnlyMigrationPreservesHistoricalLiveCopy is opt-in because CI
// has no governed field database. The deterministic install preflight supplies
// DCP_AO_LIVE_COPY_DB and proves migration 0075 against a disposable byte copy;
// it never starts the daemon or creates a task/model action.
func TestExactRepoOnlyMigrationPreservesHistoricalLiveCopy(t *testing.T) {
	source := os.Getenv("DCP_AO_LIVE_COPY_DB")
	if source == "" {
		t.Skip("DCP_AO_LIVE_COPY_DB is not set")
	}
	before := openReadOnlyDB(t, source)
	beforeCounts := policyAuthorityCounts(t, before)
	beforeGate := onePolicyTaskProjection(t, before, "arb-c-right")
	_ = before.Close()

	dir := t.TempDir()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ao.db"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("migrate governed live copy: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	after := openReadOnlyDB(t, filepath.Join(dir, "ao.db"))
	defer after.Close()
	if got := policyAuthorityCounts(t, after); got != beforeCounts {
		t.Fatalf("durable authority counts drifted: before=%v after=%v", beforeCounts, got)
	}
	if got := onePolicyTaskProjection(t, after, "arb-c-right"); got != beforeGate {
		t.Fatalf("terminal Human Gate projection drifted: before=%q after=%q", beforeGate, got)
	}
	var integrity string
	if err := after.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity=%q err=%v", integrity, err)
	}
	var foreignViolations int
	if err := after.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&foreignViolations); err != nil || foreignViolations != 0 {
		t.Fatalf("foreign key violations=%d err=%v", foreignViolations, err)
	}
	var version int64
	if err := after.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&version); err != nil || version != 75 {
		t.Fatalf("migration version=%d err=%v", version, err)
	}
	var schema string
	if err := after.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='dcp_review_lab_policy_task'`).Scan(&schema); err != nil ||
		!strings.Contains(schema, "wb-price-extension") || !strings.Contains(schema, "dcp.repo-only.happy-path/v1") {
		t.Fatalf("repo-only table authority is absent: err=%v schema=%q", err, schema)
	}
}

func openReadOnlyDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func policyAuthorityCounts(t *testing.T, db *sql.DB) [4]int64 {
	t.Helper()
	var counts [4]int64
	queries := []string{
		`SELECT count(*) FROM dcp_review_lab_policy_task`,
		`SELECT count(*) FROM dcp_model_action`,
		`SELECT count(*) FROM review_run`,
		`SELECT count(*) FROM dcp_review_lab_admission`,
	}
	for index, query := range queries {
		if err := db.QueryRow(query).Scan(&counts[index]); err != nil {
			t.Fatal(err)
		}
	}
	return counts
}

func onePolicyTaskProjection(t *testing.T, db *sql.DB, taskID string) string {
	t.Helper()
	var state, packet string
	if err := db.QueryRow(`SELECT state, incident_packet FROM dcp_review_lab_policy_task WHERE task_id=?`, taskID).Scan(&state, &packet); err != nil {
		t.Fatal(err)
	}
	return state + "\x00" + packet
}
