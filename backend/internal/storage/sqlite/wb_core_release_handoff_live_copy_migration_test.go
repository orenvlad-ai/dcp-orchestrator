package sqlite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWBCReleaseHandoffMigrationPreservesHistoricalLiveCopy is model-free and
// opt-in. The deterministic install preflight supplies a disposable copy of
// the governed database; this test never starts the daemon or creates a task.
func TestWBCReleaseHandoffMigrationPreservesHistoricalLiveCopy(t *testing.T) {
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
	var version int64
	if err := after.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&version); err != nil || version != 78 {
		t.Fatalf("migration version=%d err=%v", version, err)
	}
	var schema string
	if err := after.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='dcp_review_lab_policy_task'`).Scan(&schema); err != nil ||
		!strings.Contains(schema, "orenvlad-ai/wb-core") || !strings.Contains(schema, "release_waiting") ||
		!strings.Contains(schema, "dcp.wb-core.repo-only.release-train/v1") {
		t.Fatalf("WBC policy table authority is absent: err=%v schema=%q", err, schema)
	}
	var targetRows, authorityRows int64
	if err := after.QueryRow(`SELECT count(*) FROM dcp_review_lab_policy_task WHERE target='wb-core'`).Scan(&targetRows); err != nil || targetRows != 0 {
		t.Fatalf("wb-core task rows=%d err=%v", targetRows, err)
	}
	if err := after.QueryRow(`SELECT count(*) FROM dcp_wb_core_release_handoff_v1 WHERE authority_id='wb-core-release-train-handoff-v1'`).Scan(&authorityRows); err != nil || authorityRows != 1 {
		t.Fatalf("release handoff authority rows=%d err=%v", authorityRows, err)
	}
}
