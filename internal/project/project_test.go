package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInitDiscoverAndStableID(t *testing.T) {
	root := t.TempDir()
	created, err := Init(root, "one")
	if err != nil {
		t.Fatal(err)
	}
	if created.Root != root || created.ID == "" {
		t.Fatalf("unexpected project: %#v", created)
	}
	again, err := Init(root, "different")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != created.ID || again.Name != "one" {
		t.Fatalf("init overwrote manifest: %#v", again)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	found, err := Discover(nested)
	if err != nil || found.ID != created.ID {
		t.Fatalf("discover: %#v, %v", found, err)
	}
}

func TestResolvePathRejectsEscapeAndSymlink(t *testing.T) {
	root := t.TempDir()
	if _, err := ResolvePath(root, "../secret"); err == nil {
		t.Fatal("accepted traversal")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolvePath(root, "escape/file"); err == nil {
		t.Fatal("accepted escaping symlink")
	}
	if _, err := ResolvePath(root, "context/brief.md"); err == nil {
		t.Fatal("nonexistent parent should fail")
	}
}

func TestLoadRejectsInvalidManifest(t *testing.T) {
	root := t.TempDir()
	if _, err := Load(root); !errors.Is(err, ErrNotProject) {
		t.Fatalf("got %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, Directory), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, Directory, ManifestFile), []byte(`{"schema_version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("accepted invalid manifest")
	}
}

func TestCatalogDetachDoesNotTouchProjectFiles(t *testing.T) {
	root := t.TempDir()
	created, err := Init(root, "catalogued")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := OpenCatalog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Attach(root); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Detach(created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, Directory, ManifestFile)); err != nil {
		t.Fatalf("detach removed manifest: %v", err)
	}
}
