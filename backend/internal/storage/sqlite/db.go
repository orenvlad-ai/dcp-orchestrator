// Package sqlite owns SQLite connection setup and goose-managed schema
// migrations. Typed CRUD lives in the store subpackage; this package keeps the
// public Open entrypoint and compatibility aliases for callers.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pressly/goose/v3"

	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"

	// modernc.org/sqlite is the pure-Go (CGO-free) SQLite driver — chosen so the
	// daemon cross-compiles and ships as a static binary with no libsqlite/CGO
	// toolchain dependency, at the cost of some raw throughput vs a C-backed driver.
	_ "modernc.org/sqlite"
)

// Store is the SQLite-backed persistence layer.
type Store = sqlitestore.Store

//go:embed migrations/*.sql
var migrationsFS embed.FS

// pragmas are applied on every connection open. WAL + NORMAL lets readers run
// concurrently with the writer; busy_timeout absorbs brief writer contention;
// foreign_keys enforces the cascades and the CDC triggers' lookups.
const pragmas = "?_pragma=journal_mode(WAL)" +
	"&_pragma=busy_timeout(5000)" +
	"&_pragma=foreign_keys(ON)" +
	"&_pragma=synchronous(NORMAL)"

const readOnlyPragmas = "?mode=ro" +
	"&_pragma=busy_timeout(5000)" +
	"&_pragma=foreign_keys(ON)"

// maxReaders caps the reader pool. WAL allows many concurrent readers.
const maxReaders = 8

// Open opens (creating if absent) the SQLite database under dataDir and returns
// a Store. It uses TWO pools against the same file:
//
//   - a single WRITER connection (writeDB, MaxOpenConns=1): every write goes
//     here, so a write and the CDC triggers' subqueries it fires always see the
//     prior writes on the same connection (read-your-writes). This is required
//     because the pr/pr_checks triggers SELECT from sessions/pr to fill in the
//     event's project_id; a pooled writer could land that read on a connection
//     that hasn't caught up to the commit and read NULL.
//   - a READER pool (readDB, MaxOpenConns=maxReaders): all reads scale across
//     it; WAL readers see the latest committed snapshot.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dsn := "file:" + filepath.Join(dataDir, "ao.db") + pragmas

	writeDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite writer: %w", err)
	}
	writeDB.SetMaxOpenConns(1)
	writeDB.SetMaxIdleConns(1)
	if err := migrate(writeDB); err != nil {
		_ = writeDB.Close()
		return nil, err
	}

	readDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("open sqlite reader: %w", err)
	}
	readDB.SetMaxOpenConns(maxReaders)
	readDB.SetMaxIdleConns(maxReaders)

	return sqlitestore.NewStore(writeDB, readDB), nil
}

// OpenReadOnly opens an existing SQLite database under dataDir without creating
// the directory, opening a writable connection, or running migrations.
func OpenReadOnly(ctx context.Context, dataDir string) (*Store, error) {
	dsn := "file:" + filepath.Join(dataDir, "ao.db") + readOnlyPragmas

	writeDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite read-only writer: %w", err)
	}
	writeDB.SetMaxOpenConns(1)
	writeDB.SetMaxIdleConns(1)
	if err := writeDB.PingContext(ctx); err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("open sqlite read-only writer: %w", err)
	}

	readDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("open sqlite read-only reader: %w", err)
	}
	readDB.SetMaxOpenConns(maxReaders)
	readDB.SetMaxIdleConns(maxReaders)
	if err := readDB.PingContext(ctx); err != nil {
		_ = readDB.Close()
		_ = writeDB.Close()
		return nil, fmt.Errorf("open sqlite read-only reader: %w", err)
	}

	return sqlitestore.NewStore(writeDB, readDB), nil
}

// gooseMu serialises calls into goose. goose v3 keeps its baseFS / logger /
// dialect as package-level globals (goose.SetBaseFS, goose.SetLogger,
// goose.SetDialect), so two concurrent Open() calls — uncommon in production
// but normal in -race test runs — race on those writes. The cost of holding the
// mutex is one process-startup migration; readers and writers afterwards never
// touch goose.
var gooseMu sync.Mutex

func migrate(db *sql.DB) error {
	gooseMu.Lock()
	defer gooseMu.Unlock()
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	// Builds can advance a database past a migration that is added or
	// renumbered later (notably across fast-moving Nightly releases). Apply
	// those embedded migrations instead of permanently wedging daemon startup
	// on goose's out-of-order-history guard.
	if err := goose.Up(db, "migrations", goose.WithAllowMissing()); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	if err := reconcileSchema(db); err != nil {
		return err
	}
	if err := reconcileDCPWBCAdmissionIndex(db); err != nil {
		return err
	}
	return repairDCPReviewLabCard13CreationBase(db)
}

// reconcileDCPWBCAdmissionIndex relaxes only the physical one-active-row
// guard needed for exact same-session readmission generations. Historical
// incident rows remain immutable FIFO blockers in ClaimDCPReviewLabAdmission;
// only waiting/claimed/refreshing rows remain mutually exclusive here.
//
// The table-existence guard preserves the historical burned-migration profile
// whose foreign 0050 ledger entry omitted the DCP admission table entirely.
// That profile can still serve its non-DCP sessions; a healthy DCP database
// receives this exact index immediately after migration 0080 creates the
// readmission authority table.
func reconcileDCPWBCAdmissionIndex(db *sql.DB) error {
	var admissionTable, readmissionTable int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'dcp_review_lab_admission'`).Scan(&admissionTable); err != nil {
		return fmt.Errorf("schema verification: inspect DCP admission table: %w", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'dcp_wbc_readmission_generation'`).Scan(&readmissionTable); err != nil {
		return fmt.Errorf("schema verification: inspect WBC readmission table: %w", err)
	}
	if admissionTable == 0 || readmissionTable == 0 {
		return nil
	}
	var indexSQL sql.NullString
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'idx_dcp_review_lab_admission_one_active_per_session'`).Scan(&indexSQL)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("schema verification: inspect DCP admission active index: %w", err)
	}
	if err == nil && indexSQL.Valid && strings.Contains(indexSQL.String, "WHERE status IN ('waiting', 'claimed', 'refreshing')") {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("schema repair: begin WBC readmission admission-index update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DROP INDEX IF EXISTS idx_dcp_review_lab_admission_one_active_per_session`); err != nil {
		return fmt.Errorf("schema repair: drop predecessor DCP admission active index: %w", err)
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX idx_dcp_review_lab_admission_one_active_per_session
    ON dcp_review_lab_admission (session_id)
    WHERE status IN ('waiting', 'claimed', 'refreshing')`); err != nil {
		return fmt.Errorf("schema repair: create WBC readmission admission active index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("schema repair: commit WBC readmission admission-index update: %w", err)
	}
	return nil
}

// repairDCPReviewLabCard13CreationBase heals the one already-provisioned live
// policy card created before lifecycle metadata merging retained DiffBaseSHA
// and DiffBaseRef. It runs once at stopped startup, after burned-profile schema
// reconciliation, and changes no task/action/review/admission identity. Every
// material local fact is exact; any drift makes the UPDATE a no-op.
func repairDCPReviewLabCard13CreationBase(db *sql.DB) error {
	for _, required := range [][2]string{
		{"sessions", "diff_base_ref"},
		{"dcp_review_lab_policy_task", "policy_version"},
		{"pr", "provider_merge_state_status"},
		{"review_run", "result_channel"},
		{"review_run", "batch_id"},
		{"dcp_review_lab_admission", "review_base_sha"},
		{"dcp_model_action", "status"},
		{"pr_checks", "conclusion"},
	} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, required[0], required[1]).Scan(&count); err != nil {
			return fmt.Errorf("repair card-13 creation base schema %s.%s: %w", required[0], required[1], err)
		}
		if count != 1 {
			// Historical foreign/burned profiles may legitimately lack the DCP
			// tables. Their schema repair must complete without guessing facts.
			return nil
		}
	}
	result, err := db.Exec(`
UPDATE sessions
SET diff_base_sha = '5bfd20d3b3f5b7d9d9ccb02500b742a917e6ea01',
    diff_base_ref = 'origin/main'
WHERE id = 'dcp-review-lab-13'
  AND project_id = 'dcp-review-lab'
  AND num = 13
  AND kind = 'worker'
  AND harness = 'codex'
  AND display_name = 'DCP:chat-probe-b'
  AND activity_state = 'idle'
  AND is_terminated = 0
  AND branch = 'ao/dcp-review-lab-13/root'
  AND runtime_launch_id = ''
  AND reviewer_harness = ''
  AND terminate_on_pr_merge = 0
  AND diff_base_sha = ''
  AND diff_base_ref = ''
  AND EXISTS (
      SELECT 1
      FROM dcp_review_lab_policy_task t
      WHERE t.task_id = 'chat-probe-b'
        AND t.session_id = sessions.id
        AND t.card_number = 13
        AND t.target = 'dcp-review-lab'
        AND t.profile = 'synthetic-pr'
        AND t.repository = 'orenvlad-ai/dcp-review-lab'
        AND t.policy_version = 'dcp.review-lab.happy-path/v1'
        AND t.worktree_path = sessions.workspace_path
        AND t.source_branch = sessions.branch
        AND t.state = 'admission_waiting'
        AND t.revision = 9
        AND t.repair_count = 0
        AND t.pr_url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/10'
        AND t.pr_number = 10
        AND t.current_head_sha = 'e467d1a44668294d59cca15a756c6cef18e4b247'
        AND t.review_run_id = '152048c0-6720-4397-9430-df975a453807'
        AND t.admission_id = 'dcp-admission-152048c0-6720-4397-9430-df975a453807'
        AND t.merge_commit_sha = ''
        AND t.error_code = ''
        AND t.incident_packet = ''
  )
  AND EXISTS (
      SELECT 1
      FROM pr p
      WHERE p.url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/10'
        AND p.session_id = sessions.id
        AND p.number = 10
        AND p.pr_state = 'open'
        AND p.provider = 'github'
        AND p.host = 'github.com'
        AND p.repo = 'orenvlad-ai/dcp-review-lab'
        AND p.source_branch = sessions.branch
        AND p.target_branch = 'main'
        AND p.head_sha = 'e467d1a44668294d59cca15a756c6cef18e4b247'
        AND p.base_sha = '5bfd20d3b3f5b7d9d9ccb02500b742a917e6ea01'
        AND p.author = 'orenvlad-ai'
        AND p.is_draft = 0
        AND p.is_merged = 0
        AND p.is_closed = 0
        AND p.provider_state = 'OPEN'
        AND p.provider_mergeable = 'MERGEABLE'
        AND p.provider_merge_state_status = 'CLEAN'
  )
  AND EXISTS (
      SELECT 1
      FROM review_run r
      WHERE r.id = '152048c0-6720-4397-9430-df975a453807'
        AND r.review_id = '3ad4fe55-b014-4590-bf85-a9038b0d29d6'
        AND r.batch_id = 'c7989a9e-cca9-40fc-9659-5246a68590eb'
        AND r.session_id = sessions.id
        AND r.harness = 'codex'
        AND r.pr_url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/10'
        AND r.target_sha = 'e467d1a44668294d59cca15a756c6cef18e4b247'
        AND r.status = 'complete'
        AND r.verdict = 'approved'
        AND r.result_channel = 'structured_dcp_v1'
        AND r.github_review_id = ''
        AND r.terminal_merge_status = ''
        AND r.terminal_merge_commit_sha = ''
        AND r.terminal_merge_error = ''
  )
  AND EXISTS (
      SELECT 1
      FROM dcp_review_lab_admission a
      WHERE a.id = 'dcp-admission-152048c0-6720-4397-9430-df975a453807'
        AND a.sequence = 5
        AND a.review_run_id = '152048c0-6720-4397-9430-df975a453807'
        AND a.session_id = sessions.id
        AND a.pr_url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/10'
        AND a.pr_number = 10
        AND a.target_sha = 'e467d1a44668294d59cca15a756c6cef18e4b247'
        AND a.review_base_sha = '5bfd20d3b3f5b7d9d9ccb02500b742a917e6ea01'
        AND a.admitted_base_sha = ''
        AND a.status = 'waiting'
        AND a.lease_id = ''
        AND a.merge_commit_sha = ''
        AND a.error_code = ''
        AND a.incident_packet = ''
        AND a.refresh_wake_count = 0
  )
  AND 2 = (
      SELECT count(*)
      FROM dcp_model_action m
      WHERE m.task_id = 'chat-probe-b'
        AND m.session_id = sessions.id
        AND m.status = 'succeeded'
  )
  AND 1 = (
      SELECT count(*)
      FROM dcp_model_action m
      WHERE m.id = 'dcp-model-chat-probe-b-worker-1'
        AND m.task_id = 'chat-probe-b'
        AND m.session_id = sessions.id
        AND m.kind = 'initial_worker'
        AND m.exact_head_sha = ''
        AND m.status = 'succeeded'
        AND m.slot = 0
        AND m.error_code = ''
  )
  AND 1 = (
      SELECT count(*)
      FROM dcp_model_action m
      WHERE m.id = 'dcp-model-chat-probe-b-review-1'
        AND m.task_id = 'chat-probe-b'
        AND m.session_id = sessions.id
        AND m.kind = 'reviewer'
        AND m.exact_head_sha = 'e467d1a44668294d59cca15a756c6cef18e4b247'
        AND m.status = 'succeeded'
        AND m.slot = 0
        AND m.review_run_id = '152048c0-6720-4397-9430-df975a453807'
        AND m.error_code = ''
  )
  AND 0 = (
      SELECT count(*)
      FROM dcp_model_action m
      WHERE m.status IN ('claimed', 'running')
  )
  AND 1 = (
      SELECT count(*)
      FROM pr_checks c
      WHERE c.pr_url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/10'
        AND c.name = 'dcp-review-lab'
        AND c.commit_hash = 'e467d1a44668294d59cca15a756c6cef18e4b247'
        AND c.status = 'passed'
        AND lower(c.conclusion) = 'success'
  )`)
	if err != nil {
		return fmt.Errorf("repair card-13 creation base: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("repair card-13 creation base rows: %w", err)
	}
	if rows > 1 {
		return fmt.Errorf("repair card-13 creation base affected %d rows", rows)
	}
	return nil
}

// schemaRepairs lists the column-level effects of migrations that real
// installs are known to skip. Issue #3475/#3476: profiles exist whose
// goose_db_version already records versions 40 through 46 (written by a
// foreign build), so goose silently skips the real migrations carrying those
// numbers and the generated queries then fail with "no such column" — every
// session list 500s while /healthz stays green. A versioned repair migration
// cannot fix this class, because a burned version number is exactly what
// caused it; instead the physical schema is verified on every startup.
//
// Each entry keys on one column. postAdd statements replay the rest of the
// skipped migration's effects (backfills, index swaps) and run ONLY when the
// column was just added, so healthy databases — where those statements would
// clobber live data — are never touched.
//
// Any new migration numbered up to 0046 whose schema the generated queries
// depend on MUST add an entry here, or the burned field profiles skip it and
// regress to the 500s this exists to prevent.
var schemaRepairs = []struct {
	table   string
	column  string
	addDDL  string
	postAdd []string
}{
	// 0040_add_session_diff_base.sql
	{table: "sessions", column: "diff_base_sha",
		addDDL: `ALTER TABLE sessions ADD COLUMN diff_base_sha TEXT NOT NULL DEFAULT ''`},
	{table: "sessions", column: "diff_base_ref",
		addDDL: `ALTER TABLE sessions ADD COLUMN diff_base_ref TEXT NOT NULL DEFAULT ''`},
	// 0041_notification_resolution.sql
	{table: "notifications", column: "resolved_at",
		addDDL: `ALTER TABLE notifications ADD COLUMN resolved_at TIMESTAMP`,
		postAdd: []string{
			`UPDATE notifications SET resolved_at = created_at WHERE status = 'read'`,
			`DROP INDEX IF EXISTS idx_notifications_unread_dedupe`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_notifications_open_dedupe
    ON notifications(session_id, type, pr_url)
    WHERE status = 'unread' OR resolved_at IS NULL`,
			`CREATE INDEX IF NOT EXISTS idx_notifications_unresolved
    ON notifications(resolved_at, created_at DESC, id DESC)`,
		}},
	// 0042_review_run_unique_per_harness.sql
	{table: "sessions", column: "reviewer_harness",
		addDDL: `ALTER TABLE sessions ADD COLUMN reviewer_harness TEXT NOT NULL DEFAULT ''`,
		postAdd: []string{
			`DROP INDEX IF EXISTS idx_review_run_session_pr_sha`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_review_run_session_pr_sha_harness
    ON review_run (session_id, pr_url, target_sha, harness)
    WHERE target_sha != ''
        AND status NOT IN ('failed', 'cancelled')
        AND (status = 'running' OR verdict NOT IN ('', 'changes_requested'))`,
		}},
	// 0043_add_session_pinned.sql. The trigger replay hangs off pinned_at, the
	// second of the two columns: it references both, and SQLite resolves a
	// trigger body at CREATE time, so it cannot run until both exist.
	{table: "sessions", column: "is_pinned",
		addDDL: `ALTER TABLE sessions ADD COLUMN is_pinned BOOLEAN NOT NULL DEFAULT 0`},
	{table: "sessions", column: "pinned_at",
		addDDL: `ALTER TABLE sessions ADD COLUMN pinned_at DATETIME`,
		postAdd: []string{
			`DROP TRIGGER IF EXISTS sessions_cdc_update`,
			`CREATE TRIGGER sessions_cdc_update
AFTER UPDATE ON sessions
WHEN OLD.activity_state <> NEW.activity_state
    OR OLD.is_terminated <> NEW.is_terminated
    OR (OLD.first_signal_at IS NULL AND NEW.first_signal_at IS NOT NULL)
    OR OLD.preview_url <> NEW.preview_url
    OR OLD.preview_revision <> NEW.preview_revision
    OR OLD.display_name <> NEW.display_name
    OR OLD.terminate_on_pr_merge <> NEW.terminate_on_pr_merge
    OR OLD.is_pinned <> NEW.is_pinned
    OR OLD.pinned_at <> NEW.pinned_at
    OR (OLD.pinned_at IS NULL AND NEW.pinned_at IS NOT NULL)
    OR (OLD.pinned_at IS NOT NULL AND NEW.pinned_at IS NULL)
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (NEW.project_id, NEW.id, 'session_updated',
        json_object(
            'id', NEW.id,
            'activity', NEW.activity_state,
            'isTerminated', json(CASE WHEN NEW.is_terminated THEN 'true' ELSE 'false' END),
            'terminateOnPrMerge', json(CASE WHEN NEW.terminate_on_pr_merge THEN 'true' ELSE 'false' END),
            'previewUrl', NEW.preview_url,
            'previewRevision', NEW.preview_revision,
            'isPinned', json(CASE WHEN NEW.is_pinned THEN 'true' ELSE 'false' END)
        ),
        NEW.updated_at);
END`,
		}},
}

// reconcileSchema verifies that the columns in schemaRepairs physically exist
// and replays the skipped migration's effects for any that are missing. It is
// idempotent: a healthy database (migrations applied normally, or one already
// repaired by hand or a previous startup) is left untouched. Failures surface
// as a specific, actionable startup error instead of an opaque INTERNAL_ERROR
// on the first session list.
func reconcileSchema(db *sql.DB) error {
	for _, rc := range schemaRepairs {
		var count int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, rc.table, rc.column,
		).Scan(&count); err != nil {
			return fmt.Errorf("schema verification: inspect %s.%s: %w", rc.table, rc.column, err)
		}
		if count > 0 {
			continue
		}
		if _, err := db.Exec(rc.addDDL); err != nil {
			return fmt.Errorf(
				"schema repair: %s.%s is missing (a burned goose version skipped the migration that adds it, see #3475) and could not be added: %w",
				rc.table, rc.column, err,
			)
		}
		for _, stmt := range rc.postAdd {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("schema repair: replay skipped migration effects for %s.%s: %w", rc.table, rc.column, err)
			}
		}
	}
	return nil
}
