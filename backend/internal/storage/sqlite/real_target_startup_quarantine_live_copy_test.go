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
	defer after.Close()
	if counts := recoveredStartupAuthorityCounts(t, after); counts != beforeCounts {
		t.Fatalf("startup quarantine changed recovered authority: before=%v after=%v", beforeCounts, counts)
	}
}

func recoveredStartupAuthorityCounts(t *testing.T, db interface {
	QueryRow(query string, args ...any) *sql.Row
}) [5]int64 {
	t.Helper()
	var out [5]int64
	queries := []string{
		`SELECT count(*) FROM dcp_review_lab_policy_task WHERE task_id='price-arch-v1' AND state='admission_waiting' AND revision=5`,
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
