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
