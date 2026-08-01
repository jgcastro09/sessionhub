package registry

import (
	"path"
	"strings"
)

// Entry.Path is always forward-slash (normalized in scanner.go via
// filepath.ToSlash), so every helper here uses the "path" package, not
// "path/filepath" — the latter would reformat with backslashes on Windows
// and silently break every map lookup keyed by Entry.Path.
var testStemSuffixes = []string{"_test", ".test", ".spec"}

// stemKey returns a (dir, normalized-stem) grouping key so foo.go and
// foo_test.go, or Button.tsx and Button.test.tsx, land in the same group —
// the same-stem heuristic behind ProbableRelatedFiles.
func stemKey(relPath string) string {
	dir := path.Dir(relPath)
	base := path.Base(relPath)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	lower := strings.ToLower(stem)
	for _, suffix := range testStemSuffixes {
		if strings.HasSuffix(lower, suffix) {
			stem = stem[:len(stem)-len(suffix)]
			break
		}
	}
	return dir + "\x00" + strings.ToLower(stem)
}

// computeProbableRelatedFiles groups active entries by same-directory,
// same-stem (test-suffix-stripped) and returns each entry's group-mates by
// entry_id. It is a pure function of the current entry set, recomputed on
// every scan — ProbableRelatedFiles is derived data, never human-owned, so
// nothing preserves a stale value across scans.
func computeProbableRelatedFiles(entries []Entry) map[string][]string {
	groups := map[string][]string{}
	for _, e := range entries {
		if e.Status != StatusActive {
			continue
		}
		key := stemKey(e.Path)
		groups[key] = append(groups[key], e.EntryID)
	}
	out := map[string][]string{}
	for _, ids := range groups {
		if len(ids) < 2 {
			continue
		}
		for _, self := range ids {
			var mates []string
			for _, other := range ids {
				if other != self {
					mates = append(mates, other)
				}
			}
			out[self] = mates
		}
	}
	return out
}
