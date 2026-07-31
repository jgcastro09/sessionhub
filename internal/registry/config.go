package registry

import (
	"path/filepath"
	"strings"
)

// Config is the generic scan configuration stored at
// .shproject/registry/config.json. It never contains project-specific
// names, paths, or assumptions — the Project's Setup step proposes one from
// what it detects on disk, and the user can edit it there (plan 5.1).
type Config struct {
	// Roots are scan roots, relative to the project root. Defaults to ["."].
	Roots []string `json:"roots,omitempty"`
	// Extensions, when non-empty, is an allowlist (with leading dot,
	// lowercase) restricting the scan to only these file extensions. Empty
	// means "every file that looks like text," decided by content sniffing.
	Extensions []string `json:"extensions,omitempty"`
	// Include, when non-empty, restricts the scan to files matching at least
	// one of these glob patterns (evaluated against the path relative to the
	// project root), in addition to the default/",Exclude" rules.
	Include []string `json:"include,omitempty"`
	// Exclude adds glob patterns on top of the built-in default exclusions
	// (dependency, build, and .shproject directories).
	Exclude []string `json:"exclude,omitempty"`
	// Categories maps a file extension (with leading dot, lowercase) to a
	// category name, overriding the built-in defaults in classify.go.
	Categories map[string]string `json:"categories,omitempty"`
	// MaxFileBytes skips files larger than this size (defaults to 2 MiB) so
	// generated bundles/binaries never end up hashed line-by-line.
	MaxFileBytes int64 `json:"max_file_bytes,omitempty"`
}

const defaultMaxFileBytes = 2 << 20

// defaultExcludeDirs never need to be listed by a user; they are excluded
// unconditionally in addition to Config.Exclude.
var defaultExcludeDirs = []string{
	".git", ".shproject", "node_modules", "vendor", "dist", "build",
	"__pycache__", ".venv", "venv", "target", ".next", ".turbo",
	"coverage", ".pytest_cache", ".mypy_cache", ".DS_Store", ".idea", ".vscode",
}

func (c Config) roots() []string {
	if len(c.Roots) == 0 {
		return []string{"."}
	}
	return c.Roots
}

func (c Config) allowsExtension(ext string) bool {
	if len(c.Extensions) == 0 {
		return true
	}
	for _, allowed := range c.Extensions {
		if strings.EqualFold(allowed, ext) {
			return true
		}
	}
	return false
}

func (c Config) maxFileBytes() int64 {
	if c.MaxFileBytes > 0 {
		return c.MaxFileBytes
	}
	return defaultMaxFileBytes
}

// isExcludedDir reports whether a directory name (not a path) is one of the
// built-in default exclusions.
func isExcludedDir(name string) bool {
	for _, d := range defaultExcludeDirs {
		if name == d {
			return true
		}
	}
	return strings.HasPrefix(name, ".")
}

// matchesAny reports whether relPath matches any of the given glob patterns.
// Patterns are matched both against the full relative path and its base
// name, so "*.min.js" and "generated/*.json" both work as expected.
func matchesAny(patterns []string, relPath string) bool {
	base := filepath.Base(relPath)
	for _, pattern := range patterns {
		if ok, _ := filepath.Match(pattern, relPath); ok {
			return true
		}
		if ok, _ := filepath.Match(pattern, base); ok {
			return true
		}
	}
	return false
}

func defaultConfig() Config {
	return Config{Roots: []string{"."}}
}
