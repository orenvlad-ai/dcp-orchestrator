package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRealTargetStartupQuarantineOnExactRecoveredLiveCopy(t *testing.T) {
	source := os.Getenv("DCP_AO_RECOVERED_LIVE_COPY_DB")
	if source == "" {
		t.Skip("DCP_AO_RECOVERED_LIVE_COPY_DB is not set")
	}
	before := openReadOnlyDB(t, source)
	beforeCounts := recoveredStartupAuthorityCounts(t, before)
	beforeIdentity := legacyRepoOnlyTerminalIdentity(t, before)
	var version int64
	if err := before.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&version); err != nil || version != 76 {
		t.Fatalf("recovered live-copy version=%d err=%v", version, err)
	}
	var target, profile, repository, policyVersion, sessionID, projectID string
	var cardNumber, sessionNumber int64
	if err := before.QueryRow(`
SELECT p.target, p.profile, p.repository, p.policy_version, p.session_id,
       p.card_number, s.project_id, s.num
FROM dcp_review_lab_policy_task p JOIN sessions s ON s.id=p.session_id
WHERE p.task_id='price-arch-v1'`).Scan(
		&target, &profile, &repository, &policyVersion, &sessionID,
		&cardNumber, &projectID, &sessionNumber,
	); err != nil {
		t.Fatal(err)
	}
	if target != "wb-price-extension" || profile != "repo-only" || repository != "orenvlad-ai/wb-price-extension" ||
		policyVersion != "dcp.repo-only.happy-path/v1" || sessionID != "wb-price-extension-1" ||
		cardNumber != 1 || projectID != "wb-price-extension" || sessionNumber != 1 {
		t.Fatalf("recovered startup fixture identity drifted: %s/%s/%s/%s session=%s/%d project=%s/%d",
			target, profile, repository, policyVersion, sessionID, cardNumber, projectID, sessionNumber)
	}
	if projectID == "dcp-review-lab" || cardNumber > 12 || sessionID == "dcp-review-lab-"+fmt.Sprint(cardNumber) {
		t.Fatal("recovered fixture no longer reproduces the synthetic-only predecessor rejection")
	}
	_ = before.Close()

	dir := t.TempDir()
	destination := filepath.Join(dir, "ao.db")
	copyRecoveryFixture(t, source, destination)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	quarantine, err := s.EstablishDCPGovernedStartupQuarantine(context.Background(), time.Now().UTC())
	if err != nil {
		_ = s.Close()
		t.Fatalf("exact recovered startup quarantine: %v", err)
	}
	if _, ok := quarantine["wb-price-extension-1"]; !ok {
		_ = s.Close()
		t.Fatal("exact repo-only session is absent from startup quarantine")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	after := openReadOnlyDB(t, destination)
	if counts := recoveredStartupAuthorityCounts(t, after); counts != beforeCounts {
		t.Fatalf("startup quarantine changed recovered authority: before=%v after=%v", beforeCounts, counts)
	}
	if identity := legacyRepoOnlyTerminalIdentity(t, after); identity != beforeIdentity {
		t.Fatalf("forward migration rewrote legacy terminal identity:\nbefore=%s\nafter=%s", beforeIdentity, identity)
	}
	var migratedVersion, mappings int64
	if err := after.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&migratedVersion); err != nil || migratedVersion != 77 {
		t.Fatalf("forward migration version=%d err=%v", migratedVersion, err)
	}
	if err := after.QueryRow(`SELECT count(*) FROM dcp_repo_only_target_forward_v1`).Scan(&mappings); err != nil || mappings != 1 {
		t.Fatalf("forward mapping rows=%d err=%v", mappings, err)
	}
	_ = after.Close()

	restarted, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	quarantine, err = restarted.EstablishDCPGovernedStartupQuarantine(context.Background(), time.Now().UTC())
	if err != nil {
		_ = restarted.Close()
		t.Fatalf("restarted exact legacy quarantine: %v", err)
	}
	if _, ok := quarantine["wb-price-extension-1"]; !ok {
		_ = restarted.Close()
		t.Fatal("restart lost exact legacy terminal session")
	}
	if err := restarted.Close(); err != nil {
		t.Fatal(err)
	}
	replayed := openReadOnlyDB(t, destination)
	defer replayed.Close()
	if err := replayed.QueryRow(`SELECT count(*) FROM dcp_repo_only_target_forward_v1`).Scan(&mappings); err != nil || mappings != 1 {
		t.Fatalf("restart duplicated forward mapping: rows=%d err=%v", mappings, err)
	}
}

func recoveredStartupAuthorityCounts(t *testing.T, db interface {
	QueryRow(query string, args ...any) *sql.Row
}) [5]int64 {
	t.Helper()
	var out [5]int64
	queries := []string{
		`SELECT count(*) FROM dcp_review_lab_policy_task WHERE task_id='price-arch-v1' AND state='merged' AND revision=7 AND merge_commit_sha='62853496837f64522bb08ba56169f60f3b0f9a2c'`,
		`SELECT count(*) FROM dcp_model_action WHERE task_id='price-arch-v1'`,
		`SELECT count(*) FROM dcp_real_target_submit_recovery_v1`,
		`SELECT count(*) FROM dcp_review_lab_admission WHERE session_id='wb-price-extension-1'`,
		`SELECT count(*) FROM dcp_model_action WHERE status IN ('claimed','running','queued')`,
	}
	for i, query := range queries {
		if err := db.QueryRow(query).Scan(&out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func legacyRepoOnlyTerminalIdentity(t *testing.T, db interface {
	QueryRow(query string, args ...any) *sql.Row
}) string {
	t.Helper()
	var identity string
	err := db.QueryRow(`
SELECT task_id || '|' || payload_digest || '|' || target || '|' || profile || '|' || repository || '|' ||
       policy_version || '|' || session_id || '|' || card_number || '|' || worktree_path || '|' || source_branch || '|' ||
       state || '|' || revision || '|' || repair_count || '|' || pr_url || '|' || pr_number || '|' || current_head_sha || '|' ||
       previous_head_sha || '|' || review_run_id || '|' || admission_id || '|' || merge_commit_sha || '|' || error_code || '|' || incident_packet
FROM dcp_review_lab_policy_task WHERE task_id='price-arch-v1'`).Scan(&identity)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}
