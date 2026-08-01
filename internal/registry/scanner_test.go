package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectSymbolsRecordsLineNumbers(t *testing.T) {
	content := "package foo\n\nfunc First() {}\n\nfunc Second() {}\n"
	symbols := detectSymbols(content, "go")
	functions := symbols["functions"]
	if len(functions) != 2 {
		t.Fatalf("expected 2 functions, got %+v", functions)
	}
	want := map[string]int{"First": 3, "Second": 5}
	for _, s := range functions {
		if want[s.Name] != s.Line {
			t.Errorf("symbol %s: got line %d, want %d", s.Name, s.Line, want[s.Name])
		}
	}
}

func TestExtractDependenciesPerLanguage(t *testing.T) {
	cases := []struct {
		name, language, content                string
		wantIncludes, wantImports, wantExports []string
	}{
		{
			name: "go", language: "go",
			content:     "package foo\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n",
			wantImports: []string{"fmt", "os"},
		},
		{
			name: "typescript", language: "typescript",
			content:     "import { useState } from 'react'\nexport function App() {}\n",
			wantImports: []string{"react"},
			wantExports: []string{"App"},
		},
		{
			name: "python", language: "python",
			content:     "import os\nfrom collections import OrderedDict\n\n__all__ = [\"foo\", \"bar\"]\n",
			wantImports: []string{"os", "collections"},
			wantExports: []string{"foo", "bar"},
		},
		{
			name: "c", language: "c",
			content:      "#include \"local.h\"\n#include <stdio.h>\n",
			wantIncludes: []string{"local.h", "stdio.h"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			includes, imports, exports := extractDependencies(c.content, c.language)
			if !equalStringSlices(sortedCopy(includes), sortedCopy(c.wantIncludes)) {
				t.Errorf("includes = %v, want %v", includes, c.wantIncludes)
			}
			if !equalStringSlices(sortedCopy(imports), sortedCopy(c.wantImports)) {
				t.Errorf("imports = %v, want %v", imports, c.wantImports)
			}
			if !equalStringSlices(sortedCopy(exports), sortedCopy(c.wantExports)) {
				t.Errorf("exports = %v, want %v", exports, c.wantExports)
			}
		})
	}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func TestNoOpRescanTouchesNothingOnDisk(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\nfunc A() {}\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	recordPath := filepath.Join(root, ".shproject", "registry", "records", "go.json")
	before, err := os.Stat(recordPath)
	if err != nil {
		t.Fatalf("stat records file: %v", err)
	}

	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	after, err := os.Stat(recordPath)
	if err != nil {
		t.Fatalf("stat records file after rescan: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("expected a no-op rescan to leave records/go.json untouched: before=%v after=%v", before.ModTime(), after.ModTime())
	}
}

// TestScanSkipsSymlinkEscapingRoot is a regression test covering the
// project-root symlink-containment rule directly at the scan() level: a
// symlink pointing outside the resolved project root must never be
// followed, and must never fail the whole scan.
func TestScanSkipsSymlinkEscapingRoot(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "a.go", "package a\n")

	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.go")
	if err := os.WriteFile(outsideFile, []byte("package secret\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(root, "escape.go")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	result, err := scan(root, defaultConfig(), nil, false)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, d := range result.Files {
		if d.RelPath == "escape.go" {
			t.Fatalf("expected the root-escaping symlink to be skipped, got it scanned: %+v", d)
		}
	}
	if len(result.Files) != 1 || result.Files[0].RelPath != "a.go" {
		t.Fatalf("expected only a.go to be scanned, got %+v", result.Files)
	}
}

// TestScanFollowsSymlinkWithinRoot complements the escaping-symlink test: a
// symlink whose target resolves inside the project root is a normal,
// scannable file.
func TestScanFollowsSymlinkWithinRoot(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "real/target.go", "package real\nfunc Target(){}\n")
	if err := os.Symlink(filepath.Join(root, "real", "target.go"), filepath.Join(root, "linked.go")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	result, err := scan(root, defaultConfig(), nil, false)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	found := map[string]bool{}
	for _, d := range result.Files {
		found[d.RelPath] = true
	}
	if !found["real/target.go"] || !found["linked.go"] {
		t.Fatalf("expected both the real file and the in-root symlink to be scanned, got %+v", result.Files)
	}
}

// TestIncrementalScanReusesUnchangedFingerprint is the regression test for
// the Fase 1 promise that a rescan without any file change must not re-read
// or re-hash content. It exercises Service.Scan (not the bare scan() func)
// since the fingerprint cache lives in scanstate.json, persisted between
// Service.Scan calls.
func TestIncrementalScanReusesUnchangedFingerprint(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\nfunc A(){}\n")
	writeSource(t, root, "b.go", "package a\nfunc B(){}\n")

	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("first Scan: %v", err)
	}
	firstMetrics, err := svc.ScanMetrics(projectID)
	if err != nil {
		t.Fatalf("ScanMetrics: %v", err)
	}
	if firstMetrics.FilesReanalyzed != 2 || firstMetrics.FilesReused != 0 {
		t.Fatalf("expected the first scan to analyze both new files, got %+v", firstMetrics)
	}

	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	secondMetrics, err := svc.ScanMetrics(projectID)
	if err != nil {
		t.Fatalf("ScanMetrics: %v", err)
	}
	if secondMetrics.FilesReused != 2 || secondMetrics.FilesReanalyzed != 0 || secondMetrics.BytesRead != 0 {
		t.Fatalf("expected an unchanged rescan to reuse every fingerprint and read zero bytes, got %+v", secondMetrics)
	}

	// Editing one file must cause exactly that one to be reanalyzed, the
	// other to still be reused from cache.
	writeSource(t, root, "a.go", "package a\nfunc A(){}\nfunc A2(){}\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("third Scan: %v", err)
	}
	thirdMetrics, err := svc.ScanMetrics(projectID)
	if err != nil {
		t.Fatalf("ScanMetrics: %v", err)
	}
	if thirdMetrics.FilesReused != 1 || thirdMetrics.FilesReanalyzed != 1 {
		t.Fatalf("expected exactly one file to be reused and one reanalyzed after a single edit, got %+v", thirdMetrics)
	}

	// ScanFull must bypass the cache entirely, even though nothing changed
	// since the previous scan.
	if _, err := svc.ScanFull(projectID); err != nil {
		t.Fatalf("ScanFull: %v", err)
	}
	fullMetrics, err := svc.ScanMetrics(projectID)
	if err != nil {
		t.Fatalf("ScanMetrics: %v", err)
	}
	if !fullMetrics.Full || fullMetrics.FilesReused != 0 || fullMetrics.FilesReanalyzed != 2 {
		t.Fatalf("expected ScanFull to re-read every file regardless of fingerprint match, got %+v", fullMetrics)
	}
}
