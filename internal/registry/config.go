package registry

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Config is the generic scan configuration stored at
// .shproject/registry/config.json. It never contains project-specific
// names, paths, or assumptions — the Project's Setup step proposes one from
// what it detects on disk, and the user can edit it from there (or from the
// Web Panel's Config view).
type Config struct {
	// Roots are scan roots, relative to the project root. Defaults to ["."].
	Roots []string `json:"roots,omitempty"`

	Eligibility    EligibilityPolicy    `json:"eligibility"`
	Classification ClassificationPolicy `json:"classification"`

	// ModuleOverrides maps a path prefix to a module name, checked before
	// the built-in longest-prefix table in module.go.
	ModuleOverrides map[string]string `json:"module_overrides,omitempty"`

	// SearchAliases expands a query term into a set of related terms before
	// full-text matching (Phase 7), e.g. "db": ["database","sqlite","storage"].
	SearchAliases map[string][]string `json:"search_aliases,omitempty"`

	// Watch is the Fase 1.5 continuous-freshness policy: opt-in (disabled by
	// default) and per-project, never assumed.
	Watch WatchPolicy `json:"watch,omitempty"`
}

// WatchPolicy controls the optional filesystem watcher (watcher.go). It is
// never enabled implicitly — a person or Setup step must turn it on.
type WatchPolicy struct {
	Enabled bool `json:"enabled"`
	// DebounceMS is how long the watcher waits for the filesystem to go
	// quiet before scanning; 0 uses defaultWatchDebounce (1500ms).
	DebounceMS int `json:"debounce_ms,omitempty"`
}

const defaultMaxFileBytes = 2 << 20

func (c Config) roots() []string {
	if len(c.Roots) == 0 {
		return []string{"."}
	}
	return c.Roots
}

func (c Config) maxFileBytes() int64 {
	if c.Eligibility.MaxFileBytes > 0 {
		return c.Eligibility.MaxFileBytes
	}
	return defaultMaxFileBytes
}

// Validate rejects a config with any unjustified eligibility/exclusion rule
// or a classification policy missing a required fallback rule. SaveConfig
// calls this before ever writing to disk, so an unjustified rule can never
// reach a project's config.json.
func (c Config) Validate() error {
	if err := c.validateRoots(); err != nil {
		return err
	}
	if err := c.Eligibility.Validate(); err != nil {
		return fmt.Errorf("eligibility policy: %w", err)
	}
	if err := c.Classification.Validate(); err != nil {
		return fmt.Errorf("classification policy: %w", err)
	}
	return nil
}

// validateRoots rejects an absolute root, a root that escapes the project
// root via "..", and two roots where one is a prefix of the other (a scan
// under an overlapping pair would double-count and double-write every file
// under the inner root).
func (c Config) validateRoots() error {
	seen := make([]string, 0, len(c.Roots))
	for _, r := range c.Roots {
		if strings.TrimSpace(r) == "" {
			return fmt.Errorf("root %q must not be empty", r)
		}
		if filepath.IsAbs(r) {
			return fmt.Errorf("root %q must be relative to the project root", r)
		}
		clean := filepath.ToSlash(filepath.Clean(r))
		if clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("root %q escapes the project root", r)
		}
		for _, prev := range seen {
			if clean == prev || strings.HasPrefix(clean+"/", prev+"/") || strings.HasPrefix(prev+"/", clean+"/") {
				return fmt.Errorf("root %q overlaps root %q", r, prev)
			}
		}
		seen = append(seen, clean)
	}
	return nil
}

// defaultConfig seeds a new project with a generic, project-agnostic policy
// — no Session-Hub-specific names or assumptions about the project's own
// language or layout.
func defaultConfig() Config {
	return Config{
		Roots:          []string{"."},
		Eligibility:    defaultEligibilityPolicy(),
		Classification: defaultClassificationPolicy(),
	}
}
