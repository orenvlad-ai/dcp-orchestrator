package sqlite

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

const dcpV2Schema83CopyEnv = "DCP_V2_CORE_SCHEMA83_DB"

func migrateDCPV2TestTo(t *testing.T, db *sql.DB, version int64) {
	t.Helper()
	gooseMu.Lock()
	defer gooseMu.Unlock()
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(db, "migrations", version, goose.WithAllowMissing()); err != nil {
		t.Fatalf("migrate to %d: %v", version, err)
	}
}

func TestDCPV2CoreMigrationIsAdditiveAndStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ao.db")
	db, err := sql.Open("sqlite", "file:"+path+pragmas)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migrateDCPV2TestTo(t, db, 83)
	before := dcpV2PredecessorSnapshot(t, db)
	migrateDCPV2TestTo(t, db, 84)
	after := dcpV2PredecessorSnapshot(t, db)
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatalf("schema-83 predecessor rows changed\nbefore=%v\nafter=%v", before, after)
	}
	var version, tasks, actions, admissions, incidents, authority, adapter, installed int64
	if err := db.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	for query, out := range map[string]*int64{
		`SELECT count(*) FROM dcp_v2_task`:                                         &tasks,
		`SELECT count(*) FROM dcp_v2_action`:                                       &actions,
		`SELECT count(*) FROM dcp_v2_admission`:                                    &admissions,
		`SELECT count(*) FROM dcp_v2_incident`:                                     &incidents,
		`SELECT count(*), adapter_activated, installed FROM dcp_v2_core_authority`: &authority,
	} {
		if strings.Contains(query, "core_authority") {
			if err := db.QueryRow(query).Scan(&authority, &adapter, &installed); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := db.QueryRow(query).Scan(out); err != nil {
			t.Fatal(err)
		}
	}
	if version != 84 || tasks != 0 || actions != 0 || admissions != 0 || incidents != 0 || authority != 1 || adapter != 0 || installed != 0 {
		t.Fatalf("stage4 migration version=%d rows=%d/%d/%d/%d authority=%d adapter=%d installed=%d", version, tasks, actions, admissions, incidents, authority, adapter, installed)
	}
	var controlPlane, architecture string
	if err := db.QueryRow(`SELECT control_plane_commit, architecture_version FROM dcp_v2_core_authority`).Scan(&controlPlane, &architecture); err != nil {
		t.Fatal(err)
	}
	if controlPlane != "8be08577673722edc9ae036dedea46c88ceac129" || architecture != "dcp.wbc-integration-twin/v2" {
		t.Fatalf("authority=%s/%s", controlPlane, architecture)
	}
}

// TestDCPV2CoreMigrationOnExactSchema83Copy is opt-in and model-free. The
// source is opened read-only and copied into a disposable directory; live
// SQLite is never opened writable by this test.
func TestDCPV2CoreMigrationOnExactSchema83Copy(t *testing.T) {
	source := os.Getenv(dcpV2Schema83CopyEnv)
	if source == "" {
		t.Skip(dcpV2Schema83CopyEnv + " is not set")
	}
	beforeDB := openReadOnlyDB(t, source)
	var version int64
	if err := beforeDB.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 83 {
		t.Fatalf("exact-copy source schema=%d want=83", version)
	}
	before := dcpV2PredecessorSnapshot(t, beforeDB)
	if err := beforeDB.Close(); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	destination := filepath.Join(dir, "ao.db")
	copyRecoveryFixture(t, source, destination)
	db, err := sql.Open("sqlite", "file:"+destination+pragmas)
	if err != nil {
		t.Fatal(err)
	}
	migrateDCPV2TestTo(t, db, 84)
	after := dcpV2PredecessorSnapshot(t, db)
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatalf("exact schema-83 copy changed predecessor rows\nbefore=%v\nafter=%v", before, after)
	}
	var taskRows, actionRows, admissionRows, incidentRows int64
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM dcp_v2_task), (SELECT count(*) FROM dcp_v2_action), (SELECT count(*) FROM dcp_v2_admission), (SELECT count(*) FROM dcp_v2_incident)`).Scan(&taskRows, &actionRows, &admissionRows, &incidentRows); err != nil {
		t.Fatal(err)
	}
	if taskRows != 0 || actionRows != 0 || admissionRows != 0 || incidentRows != 0 {
		t.Fatalf("migration activated DCP v2 rows=%d/%d/%d/%d", taskRows, actionRows, admissionRows, incidentRows)
	}
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity=%q err=%v", integrity, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func dcpV2PredecessorSnapshot(t *testing.T, db interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}) map[string]string {
	t.Helper()
	tables := []string{
		"projects", "sessions", "dcp_task", "dcp_task_event", "dcp_review_lab_policy_task",
		"dcp_model_action", "review_run", "dcp_review_lab_admission", "dcp_wbc_readmission_generation",
		"dcp_task_first_native_lifecycle_recovery_v1",
	}
	snapshot := make(map[string]string, len(tables))
	for _, table := range tables {
		var exists int
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists == 0 {
			continue
		}
		columns, err := db.Query(`PRAGMA table_info(` + quoteDCPV2Identifier(table) + `)`)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for columns.Next() {
			var cid, notNull, pk int
			var name, kind string
			var defaultValue sql.NullString
			if err := columns.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
				t.Fatal(err)
			}
			names = append(names, name)
		}
		if err := columns.Close(); err != nil {
			t.Fatal(err)
		}
		sort.Strings(names)
		expressions := make([]string, 0, len(names))
		for _, name := range names {
			expressions = append(expressions, `quote(`+quoteDCPV2Identifier(name)+`)`)
		}
		query := `SELECT ` + strings.Join(expressions, `,`) + ` FROM ` + quoteDCPV2Identifier(table)
		if len(names) > 0 {
			positions := make([]string, len(names))
			for i := range names {
				positions[i] = fmt.Sprint(i + 1)
			}
			query += ` ORDER BY ` + strings.Join(positions, `,`)
		}
		rows, err := db.Query(query)
		if err != nil {
			t.Fatalf("snapshot %s: %v", table, err)
		}
		hash := sha256.New()
		_, _ = hash.Write([]byte(strings.Join(names, "\x1f")))
		for rows.Next() {
			values := make([]string, len(names))
			pointers := make([]any, len(names))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			_, _ = hash.Write([]byte("\x1e" + strings.Join(values, "\x1f")))
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		snapshot[table] = hex.EncodeToString(hash.Sum(nil))
	}
	return snapshot
}

func quoteDCPV2Identifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
