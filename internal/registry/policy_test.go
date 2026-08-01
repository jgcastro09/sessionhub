package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func writeBinary(root, rel string, data []byte) error {
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func TestEligibilityPolicyExclusionWinsOverInclusion(t *testing.T) {
	p := EligibilityPolicy{
		ExtensionReasons:         Reasons{".go": "source"},
		ExcludePathPrefixReasons: Reasons{"vendor/": "vendored code"},
	}
	if !p.Eligible("vendor/pkg/main.go") {
		t.Fatalf("Eligible should not itself consult exclusion rules")
	}
	if !p.ExcludedPath("vendor/pkg/main.go") {
		t.Fatalf("expected vendor/pkg/main.go to be excluded by path prefix")
	}
}

func TestEligibilityPolicyValidateRejectsEmptyJustification(t *testing.T) {
	p := defaultEligibilityPolicy()
	p.ExtensionReasons[".xyz"] = ""
	if err := p.Validate(); err == nil {
		t.Fatalf("expected Validate to reject an empty justification")
	}
}

func TestEligibilityWhitelistExtensionExactFilenameAndExplicitInclude(t *testing.T) {
	p := EligibilityPolicy{
		ExtensionReasons:       Reasons{".go": "source"},
		ExactFilenameReasons:   Reasons{"Makefile": "build"},
		ExplicitIncludeReasons: Reasons{"scripts/deploy": "no extension, release-critical"},
	}
	cases := map[string]bool{
		"main.go":         true,
		"Makefile":        true,
		"scripts/deploy":  true,
		"scripts/deploy2": false,
		"README.md":       false,
	}
	for path, want := range cases {
		if got := p.Eligible(path); got != want {
			t.Errorf("Eligible(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestExcludedDirPrunesWithoutInspectingContents(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "src/main.go", "package main\n")
	writeSource(t, root, "vendor/pkg/lib.go", "package pkg\n")

	cfg := defaultConfig()
	cfg.Eligibility.ExcludeDirectoryReasons["vendor"] = "vendored dependencies"
	result, err := scan(root, cfg)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, f := range result.Files {
		if f.RelPath == "vendor/pkg/lib.go" {
			t.Fatalf("expected vendor/ to be pruned entirely, found %s", f.RelPath)
		}
	}
	if len(result.Files) != 1 || result.Files[0].RelPath != "src/main.go" {
		t.Fatalf("unexpected scan result: %+v", result.Files)
	}
}

func TestUnrecognizedTextFileSurfacesAsPendingNotIndexed(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "notes.xyz", "just some plain text notes\n")
	cfg := defaultConfig()
	result, err := scan(root, cfg)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(result.Files) != 0 {
		t.Fatalf("expected the unwhitelisted file to not be indexed, got %+v", result.Files)
	}
	if len(result.Pending) != 1 || result.Pending[0].Path != "notes.xyz" {
		t.Fatalf("expected notes.xyz to surface as pending, got %+v", result.Pending)
	}
}

func TestBinaryUnrecognizedFileIsNeitherEligibleNorPending(t *testing.T) {
	root := t.TempDir()
	data := []byte{0x00, 0x01, 0x02, 'P', 'N', 'G', 0x00, 0x00}
	if err := writeBinary(root, "image.xyz", data); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	cfg := defaultConfig()
	result, err := scan(root, cfg)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(result.Files) != 0 || len(result.Pending) != 0 {
		t.Fatalf("expected the binary file to be silently skipped, got files=%+v pending=%+v", result.Files, result.Pending)
	}
}
