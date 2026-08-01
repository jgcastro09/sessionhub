package registry

import (
	"regexp"
	"strings"
)

// Pure Go regex/line-scanning, deliberately not a real parser — same
// cross-compilation constraint as symbols.go (no tree-sitter/CGO). Good
// enough to build the relationship graph's includes/imports edges (graph.go)
// without needing per-language AST support.
var (
	goImportBlock  = regexp.MustCompile(`(?ms)^import\s*\(\s*(.*?)\s*\)`)
	goImportLine   = regexp.MustCompile(`(?m)^import\s+"([^"]+)"`)
	goQuotedImport = regexp.MustCompile(`"([^"]+)"`)

	jsImportFrom   = regexp.MustCompile(`(?m)^\s*import\s+(?:[\w*{}\s,]+\s+from\s+)?['"]([^'"]+)['"]`)
	jsRequireCall  = regexp.MustCompile(`require\(\s*['"]([^'"]+)['"]\s*\)`)
	jsExportNamed  = regexp.MustCompile(`(?m)^export\s+(?:default\s+)?(?:async\s+)?(?:function|class|const|let|var|interface|type)\s+([A-Za-z_]\w*)`)
	jsExportBraces = regexp.MustCompile(`(?m)^export\s*\{\s*([^}]+)\s*\}`)

	pyImportLine     = regexp.MustCompile(`(?m)^import\s+([\w.]+)`)
	pyFromImportLine = regexp.MustCompile(`(?m)^from\s+([\w.]+)\s+import`)
	pyAllAssignment  = regexp.MustCompile(`__all__\s*=\s*\[([^\]]*)\]`)
	pyQuotedItem     = regexp.MustCompile(`['"]([^'"]+)['"]`)

	cIncludeQuoted = regexp.MustCompile(`(?m)^\s*#include\s+"([^"]+)"`)
	cIncludeAngle  = regexp.MustCompile(`(?m)^\s*#include\s+<([^>]+)>`)

	shSourceLine = regexp.MustCompile(`(?m)^\s*(?:source|\.)\s+([^\s;]+)`)
)

// extractDependencies runs a per-language regex pass over content, returning
// raw (unresolved) include/import targets and exported names. Resolution
// against other registry entries happens later, at graph-build time
// (graph.go) — this function never touches the filesystem or other entries.
func extractDependencies(content, language string) (includes, imports, exports []string) {
	switch language {
	case "go":
		if m := goImportBlock.FindStringSubmatch(content); m != nil {
			imports = append(imports, extractGroup1(m[1], goQuotedImport)...)
		}
		imports = append(imports, extractGroup1(content, goImportLine)...)
	case "typescript", "javascript":
		imports = append(imports, extractGroup1(content, jsImportFrom)...)
		imports = append(imports, extractGroup1(content, jsRequireCall)...)
		exports = append(exports, extractGroup1(content, jsExportNamed)...)
		for _, m := range jsExportBraces.FindAllStringSubmatch(content, -1) {
			for _, name := range strings.Split(m[1], ",") {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				if idx := strings.Index(name, " as "); idx >= 0 {
					name = strings.TrimSpace(name[idx+len(" as "):])
				}
				exports = append(exports, name)
			}
		}
	case "python":
		imports = append(imports, extractGroup1(content, pyImportLine)...)
		imports = append(imports, extractGroup1(content, pyFromImportLine)...)
		if m := pyAllAssignment.FindStringSubmatch(content); m != nil {
			exports = append(exports, extractGroup1(m[1], pyQuotedItem)...)
		}
	case "c", "cpp", "objective-c":
		includes = append(includes, extractGroup1(content, cIncludeQuoted)...)
		includes = append(includes, extractGroup1(content, cIncludeAngle)...)
	case "shell":
		for _, raw := range extractGroup1(content, shSourceLine) {
			includes = append(includes, strings.Trim(raw, `"'`))
		}
	}
	return dedupeStrings(includes), dedupeStrings(imports), dedupeStrings(exports)
}

func extractGroup1(s string, re *regexp.Regexp) []string {
	var out []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		if len(m) > 1 {
			out = append(out, m[1])
		}
	}
	return out
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// union returns the deduplicated concatenation of includes and imports —
// Entry.Dependencies per the reference schema.
func union(includes, imports []string) []string {
	return dedupeStrings(append(append([]string{}, includes...), imports...))
}
