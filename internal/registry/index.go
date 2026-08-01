package registry

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// indexSchemaVersion gates the on-disk schema. index.sqlite3 is a fully
// rebuildable derived cache — never source of truth for anything — so a
// version bump or any corruption simply triggers a drop-and-rebuild-from-JSON
// rather than an in-place migration.
const indexSchemaVersion = "3"

// indexDB returns (opening and lazily migrating if needed) the cached
// index.sqlite3 connection for a project root.
func (s *Service) indexDB(root string) (*sql.DB, error) {
	s.dbMu.Lock()
	defer s.dbMu.Unlock()
	if s.dbs == nil {
		s.dbs = map[string]*sql.DB{}
	}
	if db, ok := s.dbs[root]; ok {
		return db, nil
	}
	db, err := openIndexDB(indexDBPath(root))
	if err != nil {
		return nil, err
	}
	rebuilt, err := ensureIndexSchema(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if rebuilt {
		if entries, loadErr := loadAllEntries(root); loadErr == nil && len(entries) > 0 {
			// Best-effort: if this fails, the index is simply thin until the
			// next Scan/Review call repopulates the affected rows — it is a
			// cache, never a source of truth, so a partial rebuild here is
			// not a correctness problem.
			_ = upsertEntries(db, entries)
		}
	}
	s.dbs[root] = db
	return db, nil
}

func openIndexDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create registry index directory: %w", err)
	}
	dsn := "file:" + filepath.ToSlash(path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open registry index: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA synchronous=NORMAL;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure registry index: %w", err)
	}
	return db, nil
}

// ensureIndexSchema creates the schema if absent, or drops and recreates it
// if the stored schema_version doesn't match — reports whether a (re)build
// happened so the caller can repopulate from records/*.json.
func ensureIndexSchema(db *sql.DB) (rebuilt bool, err error) {
	var version string
	scanErr := db.QueryRow(`SELECT value FROM meta WHERE key='schema_version'`).Scan(&version)
	if scanErr == nil && version == indexSchemaVersion {
		return false, nil
	}
	if scanErr != nil && !errors.Is(scanErr, sql.ErrNoRows) {
		// meta table likely doesn't exist yet (fresh file) — fall through to create.
		if !strings.Contains(scanErr.Error(), "no such table") {
			return false, fmt.Errorf("read registry index schema version: %w", scanErr)
		}
	}
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS meta`,
		`DROP TABLE IF EXISTS entries_index`,
		`DROP TABLE IF EXISTS fts_entries`,
		`DROP TABLE IF EXISTS embeddings`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return false, fmt.Errorf("reset registry index: %w", err)
		}
	}
	schemaStatements := []string{
		`CREATE TABLE meta(key TEXT PRIMARY KEY, value TEXT)`,
		`CREATE TABLE entries_index(
			entry_id TEXT PRIMARY KEY, path TEXT, category TEXT, module TEXT, area TEXT, role TEXT,
			language TEXT, kind TEXT, review_status TEXT, criticality TEXT, status TEXT,
			hash TEXT, last_scanned_at TEXT
		)`,
		`CREATE INDEX idx_ei_category ON entries_index(category)`,
		`CREATE INDEX idx_ei_module ON entries_index(module)`,
		`CREATE INDEX idx_ei_area ON entries_index(area)`,
		`CREATE INDEX idx_ei_language ON entries_index(language)`,
		`CREATE INDEX idx_ei_review ON entries_index(review_status)`,
		// Plain unicode61 (no porter stemming): stemming composes poorly
		// with the prefix ("term"*) queries search.go relies on for alias
		// expansion — a stemmed query prefix doesn't reliably line up with
		// stemmed index tokens. Literal substring-of-token matching is more
		// predictable for a code registry anyway (identifiers rarely need
		// stemming the way prose does).
		`CREATE VIRTUAL TABLE fts_entries USING fts5(
			entry_id UNINDEXED, path, symbols, description, responsibilities, module, category,
			tokenize='unicode61'
		)`,
		`CREATE TABLE embeddings(
			entry_id TEXT NOT NULL, model TEXT NOT NULL, hash TEXT NOT NULL, vector BLOB NOT NULL,
			PRIMARY KEY(entry_id, model)
		)`,
	}
	for _, stmt := range schemaStatements {
		if _, err := db.Exec(stmt); err != nil {
			return false, fmt.Errorf("create registry index schema: %w", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO meta(key, value) VALUES('schema_version', ?)`, indexSchemaVersion); err != nil {
		return false, fmt.Errorf("write registry index schema version: %w", err)
	}
	return true, nil
}

// upsertEntries writes/updates entries_index and fts_entries rows for a
// batch of entries in one transaction. Called with only the entries a Scan
// or Review actually changed — this is the "incremental sync" half of the
// performance requirement; a full rebuild only ever happens once, lazily,
// after the index file is missing/corrupt/schema-mismatched.
func upsertEntries(db *sql.DB, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin registry index sync: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, e := range entries {
		if err := upsertEntryTx(tx, e); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func upsertEntryTx(tx *sql.Tx, e Entry) error {
	_, err := tx.Exec(`
INSERT INTO entries_index(entry_id,path,category,module,area,role,language,kind,review_status,criticality,status,hash,last_scanned_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(entry_id) DO UPDATE SET
 path=excluded.path, category=excluded.category, module=excluded.module, area=excluded.area,
 role=excluded.role, language=excluded.language, kind=excluded.kind, review_status=excluded.review_status,
 criticality=excluded.criticality, status=excluded.status, hash=excluded.hash, last_scanned_at=excluded.last_scanned_at`,
		e.EntryID, e.Path, e.Category, e.Module, e.Area, e.Role, e.Language, string(e.Kind),
		string(e.ReviewStatus), string(e.Criticality), string(e.Status), e.Hash, e.LastScannedAt.Format(rfc3339))
	if err != nil {
		return fmt.Errorf("upsert registry index row: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM fts_entries WHERE entry_id = ?`, e.EntryID); err != nil {
		return fmt.Errorf("clear registry fts row: %w", err)
	}
	_, err = tx.Exec(`
INSERT INTO fts_entries(entry_id,path,symbols,description,responsibilities,module,category)
VALUES(?,?,?,?,?,?,?)`,
		e.EntryID, e.Path, flattenSymbolNames(e.Symbols), e.Description, strings.Join(e.Responsibilities, " "), e.Module, e.Category)
	if err != nil {
		return fmt.Errorf("insert registry fts row: %w", err)
	}
	return nil
}

const rfc3339 = "2006-01-02T15:04:05.999999999Z07:00"

// syncIndex is Scan/Review's write path into the derived cache — always
// called with only the entries that actually changed.
func (s *Service) syncIndex(root string, changed []Entry) error {
	if len(changed) == 0 {
		return nil
	}
	db, err := s.indexDB(root)
	if err != nil {
		return err
	}
	return upsertEntries(db, changed)
}
