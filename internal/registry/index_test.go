package registry

import (
	"os"
	"testing"
)

func TestIndexRebuildsFromRecordsWhenMissing(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\n")
	writeSource(t, root, "b.go", "package a\nfunc B(){}\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := os.Remove(indexDBPath(root)); err != nil {
		t.Fatalf("remove index: %v", err)
	}

	db, err := svc.indexDB(root)
	if err != nil {
		t.Fatalf("indexDB: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entries_index`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected the index to rebuild from records/*.json with 2 rows, got %d", count)
	}
}

func TestIndexSchemaMismatchTriggersRebuild(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	db, err := svc.indexDB(root)
	if err != nil {
		t.Fatalf("indexDB: %v", err)
	}
	if _, err := db.Exec(`UPDATE meta SET value='0' WHERE key='schema_version'`); err != nil {
		t.Fatalf("corrupt schema version: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db2, err := svc.indexDB(root)
	if err != nil {
		t.Fatalf("indexDB after mismatch: %v", err)
	}
	var version string
	if err := db2.QueryRow(`SELECT value FROM meta WHERE key='schema_version'`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != indexSchemaVersion {
		t.Fatalf("expected schema to self-heal to %q, got %q", indexSchemaVersion, version)
	}
	var count int
	if err := db2.QueryRow(`SELECT COUNT(*) FROM entries_index`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected the rebuilt index to repopulate from records/*.json, got %d rows", count)
	}
}
