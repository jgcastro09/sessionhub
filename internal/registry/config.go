package registry

import (
	"fmt"
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
	if err := c.Eligibility.Validate(); err != nil {
		return fmt.Errorf("eligibility policy: %w", err)
	}
	if err := c.Classification.Validate(); err != nil {
		return fmt.Errorf("classification policy: %w", err)
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
