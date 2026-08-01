package registry

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Reasons is a justified rule set: every key must carry a non-empty
// justification string as its value. This is enforced structurally (the
// value position exists specifically to hold the justification) and
// checked by validate — a config with an empty justification anywhere never
// reaches disk (see Config.Validate, called from Service.SaveConfig).
type Reasons map[string]string

func (r Reasons) has(key string) bool {
	_, ok := r[key]
	return ok
}

func (r Reasons) validate(kind string) error {
	keys := make([]string, 0, len(r))
	for k := range r {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.TrimSpace(r[k]) == "" {
			return fmt.Errorf("%s %q has no justification", kind, k)
		}
	}
	return nil
}

// EligibilityPolicy replaces content-sniffing with a declarative whitelist:
// a file is only ever indexed if it matches ExtensionReasons,
// ExactFilenameReasons, or ExplicitIncludeReasons — and never if it matches
// an exclusion rule first, which always wins. There is deliberately no
// separate blacklist for media/binary types: since eligibility is
// whitelist-only, anything not whitelisted (images, video, audio, fonts,
// executables, archives, ...) is structurally excluded without needing its
// own rule.
type EligibilityPolicy struct {
	ExtensionReasons       Reasons `json:"extension_reasons"`
	ExactFilenameReasons   Reasons `json:"exact_filename_reasons"`
	ExplicitIncludeReasons Reasons `json:"explicit_include_reasons"`

	ExcludeDirectoryReasons  Reasons `json:"exclude_directory_reasons"`
	ExcludePathPrefixReasons Reasons `json:"exclude_path_prefix_reasons"`
	ExcludeFileReasons       Reasons `json:"exclude_file_reasons"`

	// MaxFileBytes skips files larger than this size (defaults to 2 MiB via
	// Config.maxFileBytes) so generated bundles/binaries never get hashed.
	MaxFileBytes int64 `json:"max_file_bytes,omitempty"`
}

func (p EligibilityPolicy) Validate() error {
	for kind, r := range map[string]Reasons{
		"extension":           p.ExtensionReasons,
		"exact filename":      p.ExactFilenameReasons,
		"explicit include":    p.ExplicitIncludeReasons,
		"exclude directory":   p.ExcludeDirectoryReasons,
		"exclude path prefix": p.ExcludePathPrefixReasons,
		"exclude file":        p.ExcludeFileReasons,
	} {
		if err := r.validate(kind); err != nil {
			return err
		}
	}
	return nil
}

// ExcludedDir reports whether a directory basename is excluded. Callers
// prune on a true result — the directory is never descended into,
// regardless of size or what it contains.
func (p EligibilityPolicy) ExcludedDir(name string) bool {
	return p.ExcludeDirectoryReasons.has(name)
}

// ExcludedPath reports whether relPath is excluded by an exact-file or
// path-prefix rule. Exclusion is checked before eligibility and always wins
// over it.
func (p EligibilityPolicy) ExcludedPath(relPath string) bool {
	if p.ExcludeFileReasons.has(relPath) {
		return true
	}
	for prefix := range p.ExcludePathPrefixReasons {
		if strings.HasPrefix(relPath, prefix) {
			return true
		}
	}
	return false
}

// Eligible reports whether relPath is on the declarative whitelist. It does
// not consult exclusion rules — callers must check ExcludedDir/ExcludedPath
// first, since exclusion always wins regardless of eligibility.
func (p EligibilityPolicy) Eligible(relPath string) bool {
	ext := strings.ToLower(filepath.Ext(relPath))
	if ext != "" && p.ExtensionReasons.has(ext) {
		return true
	}
	base := filepath.Base(relPath)
	if p.ExactFilenameReasons.has(base) {
		return true
	}
	return p.ExplicitIncludeReasons.has(relPath)
}

// defaultEligibilityPolicy seeds a new project with a generic, broad
// whitelist covering common source/config/doc file types and the usual
// dependency/build/cache directories — no project-specific assumptions.
func defaultEligibilityPolicy() EligibilityPolicy {
	return EligibilityPolicy{
		ExtensionReasons: Reasons{
			".go": "Go source", ".mod": "Go module manifest", ".sum": "Go module checksum",
			".ts": "TypeScript source", ".tsx": "TypeScript/React source",
			".js": "JavaScript source", ".jsx": "JavaScript/React source",
			".mjs": "JavaScript module source", ".cjs": "CommonJS source",
			".py":   "Python source",
			".html": "HTML source", ".htm": "HTML source",
			".css": "stylesheet", ".scss": "stylesheet", ".sass": "stylesheet", ".less": "stylesheet",
			".md": "documentation", ".mdx": "documentation",
			".json": "structured config/data", ".jsonc": "structured config/data",
			".yaml": "structured config/data", ".yml": "structured config/data",
			".toml": "structured config/data",
			".sql":  "database schema/query",
			".rs":   "Rust source",
			".c":    "C source", ".h": "C/C++ header",
			".cc": "C++ source", ".cpp": "C++ source", ".cxx": "C++ source",
			".hpp": "C++ header", ".hxx": "C++ header",
			".m": "Objective-C source", ".mm": "Objective-C++ source",
			".java": "Java source", ".kt": "Kotlin source", ".swift": "Swift source",
			".sh": "shell script", ".bash": "shell script", ".zsh": "shell script",
			".ps1": "PowerShell script", ".bat": "Windows batch script", ".cmd": "Windows batch script",
			".proto": "protobuf schema", ".graphql": "GraphQL schema",
			".glsl": "shader source", ".vert": "shader source", ".frag": "shader source",
		},
		ExactFilenameReasons: Reasons{
			"Makefile": "build orchestration", "Dockerfile": "container build definition",
			"Rakefile": "build orchestration", "Gemfile": "Ruby dependency manifest",
			"Procfile":        "process/deploy manifest",
			"CMakeLists.txt":  "build orchestration",
			".goreleaser.yml": "release build definition", ".goreleaser.yaml": "release build definition",
		},
		ExplicitIncludeReasons: Reasons{},
		ExcludeDirectoryReasons: Reasons{
			".git": "VCS metadata", ".shproject": "Session Hub project data",
			"node_modules": "vendored JS dependencies", "vendor": "vendored Go dependencies",
			"third_party": "vendored external code",
			"dist":        "build output", "build": "build output", "out": "build output",
			"bin": "build output", "obj": "build output", "generated": "generated output",
			"cache": "build/tool cache", ".cache": "build/tool cache",
			"__pycache__": "generated Python bytecode cache",
			".venv":       "Python virtualenv", "venv": "Python virtualenv",
			"target": "build output", ".next": "build output", ".turbo": "build cache",
			"coverage":      "generated coverage report",
			".pytest_cache": "generated test cache", ".mypy_cache": "generated type-check cache",
			".idea": "editor metadata", ".vscode": "editor metadata",
		},
		ExcludePathPrefixReasons: Reasons{},
		ExcludeFileReasons:       Reasons{},
	}
}
