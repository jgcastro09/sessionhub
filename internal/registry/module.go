package registry

import (
	"path"
	"sort"
	"strings"
)

// relPath is always forward-slash (scanner.go normalizes with
// filepath.ToSlash before anything else touches it), so this file uses the
// "path" package throughout — "path/filepath" would reformat with
// backslashes on Windows and break every prefix/segment comparison here.

// modulePrefix pairs a relative-path prefix with the module name assigned
// to anything under it.
type modulePrefix struct {
	prefix string
	module string
}

// builtinModulePrefixes is a generic, longest-prefix-first table covering
// common repository layout conventions (Go's cmd//internal/, a web/
// frontend, docs/scripts/tools/tests directories). It carries no
// project-specific names — it is a fact about widely-used conventions, not
// about any particular project.
var builtinModulePrefixes = sortModulePrefixes([]modulePrefix{
	{"cmd/", "Entrypoints"},
	{"internal/", "Backend"},
	{"pkg/", "Backend"},
	{"web/src/", "Web"},
	{"web/", "Web"},
	{"docs/", "Documentation"},
	{"scripts/", "Automation"},
	{"tools/", "Automation"},
	{"tests/", "Tests"},
	{"test/", "Tests"},
})

func sortModulePrefixes(in []modulePrefix) []modulePrefix {
	out := append([]modulePrefix(nil), in...)
	sort.Slice(out, func(i, j int) bool { return len(out[i].prefix) > len(out[j].prefix) })
	return out
}

// moduleForPath assigns a module owner string for relPath: project-level
// overrides win first (longest prefix), then the generic built-in table,
// then a humanized version of the file's top-level directory, then "Root"
// for files at the project root.
func moduleForPath(relPath string, overrides map[string]string) string {
	if len(overrides) > 0 {
		prefixes := make([]modulePrefix, 0, len(overrides))
		for prefix, module := range overrides {
			prefixes = append(prefixes, modulePrefix{prefix, module})
		}
		prefixes = sortModulePrefixes(prefixes)
		for _, p := range prefixes {
			if strings.HasPrefix(relPath, p.prefix) {
				return p.module
			}
		}
	}
	for _, p := range builtinModulePrefixes {
		if strings.HasPrefix(relPath, p.prefix) {
			return p.module
		}
	}
	dir := path.Dir(relPath)
	if dir == "." {
		return "Root"
	}
	segments := strings.Split(dir, "/")
	return titleSegment(segments[0])
}

// titleSegment turns a directory segment like "code_registry" into a
// display-friendly "Code Registry".
func titleSegment(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	words := strings.Fields(s)
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
