package registry

import (
	"errors"
	"path"
	"strings"
)

// relPath is always forward-slash (scanner.go normalizes with
// filepath.ToSlash), so this file uses "path", not "path/filepath" — the
// latter reformats with backslashes on Windows and would break every
// prefix/glob match here.

// CategoryRule assigns a Category to any file matching at least one of its
// non-empty match fields (PathPrefixes/Extensions/ExactFilenames are OR'd
// together), unless the file's extension is in ExcludeExtensions. Rules are
// evaluated in order; the first match wins. The last rule in a policy must
// have Fallback:true, so classification is total — every eligible file gets
// a category.
type CategoryRule struct {
	PathPrefixes      []string `json:"path_prefixes,omitempty"`
	Extensions        []string `json:"extensions,omitempty"`
	ExactFilenames    []string `json:"exact_filenames,omitempty"`
	ExcludeExtensions []string `json:"exclude_extensions,omitempty"`
	Fallback          bool     `json:"fallback,omitempty"`
	Category          string   `json:"category"`
}

func (r CategoryRule) matches(relPath, ext, base string) bool {
	if r.Fallback {
		return true
	}
	if len(r.ExcludeExtensions) > 0 && contains(r.ExcludeExtensions, ext) {
		return false
	}
	if len(r.PathPrefixes) > 0 && hasAnyPrefix(relPath, r.PathPrefixes) {
		return true
	}
	if len(r.Extensions) > 0 && contains(r.Extensions, ext) {
		return true
	}
	if len(r.ExactFilenames) > 0 && contains(r.ExactFilenames, base) {
		return true
	}
	return false
}

// KindRule assigns a structural Kind the same way CategoryRule assigns a
// category — ordered, first match wins, last rule must be Fallback:true.
type KindRule struct {
	PathGlobs      []string `json:"path_globs,omitempty"`
	PathPrefixes   []string `json:"path_prefixes,omitempty"`
	Extensions     []string `json:"extensions,omitempty"`
	ExactFilenames []string `json:"exact_filenames,omitempty"`
	Fallback       bool     `json:"fallback,omitempty"`
	Kind           Kind     `json:"kind"`
}

func (r KindRule) matches(relPath, ext, base string) bool {
	if r.Fallback {
		return true
	}
	if len(r.PathPrefixes) > 0 && hasAnyPrefix(relPath, r.PathPrefixes) {
		return true
	}
	if len(r.Extensions) > 0 && contains(r.Extensions, ext) {
		return true
	}
	if len(r.ExactFilenames) > 0 && contains(r.ExactFilenames, base) {
		return true
	}
	for _, glob := range r.PathGlobs {
		if ok, _ := path.Match(glob, relPath); ok {
			return true
		}
		if ok, _ := path.Match(glob, base); ok {
			return true
		}
	}
	return false
}

// ClassificationPolicy is the declarative, ordered rule set that replaces
// static extension-to-category maps: category/kind are a pure function of
// (path, extension, basename), never of file content, so classification is
// deterministic and can never depend on something a scan would need to read
// the file for.
type ClassificationPolicy struct {
	CategoryRules []CategoryRule `json:"category_rules"`
	KindRules     []KindRule     `json:"kind_rules"`
}

func (p ClassificationPolicy) Validate() error {
	if len(p.CategoryRules) == 0 || !p.CategoryRules[len(p.CategoryRules)-1].Fallback {
		return errors.New("category_rules must end with a fallback:true rule")
	}
	if len(p.KindRules) == 0 || !p.KindRules[len(p.KindRules)-1].Fallback {
		return errors.New("kind_rules must end with a fallback:true rule")
	}
	return nil
}

// Classify assigns category and kind for relPath. Total: the required
// trailing fallback rules guarantee a non-empty result for any input.
func (p ClassificationPolicy) Classify(relPath string) (category string, kind Kind) {
	ext := strings.ToLower(path.Ext(relPath))
	base := path.Base(relPath)
	category = "uncategorized"
	for _, r := range p.CategoryRules {
		if r.matches(relPath, ext, base) {
			category = r.Category
			break
		}
	}
	kind = KindOther
	for _, r := range p.KindRules {
		if r.matches(relPath, ext, base) {
			kind = r.Kind
			break
		}
	}
	return category, kind
}

var defaultTestGlobs = []string{
	"*_test.go", "*.test.ts", "*.test.tsx", "*.test.js", "*.test.jsx",
	"*.spec.ts", "*.spec.tsx", "*.spec.js", "*.spec.jsx",
	"test_*.py", "*_test.py",
}

var defaultBuildFilenames = []string{
	"Makefile", "Dockerfile", "Rakefile", "Gemfile", "Procfile", "CMakeLists.txt",
	".goreleaser.yml", ".goreleaser.yaml",
}

// defaultClassificationPolicy seeds a new project with generic, ordered
// category/kind rules — no project-specific names or layout assumptions.
func defaultClassificationPolicy() ClassificationPolicy {
	return ClassificationPolicy{
		CategoryRules: []CategoryRule{
			{Extensions: []string{".py"}, Category: "python-tools"},
			{Extensions: []string{".glsl", ".vert", ".frag"}, Category: "shaders"},
			{PathPrefixes: []string{"scripts/", "tools/", "installer/", ".github/"}, Category: "build-scripts"},
			{ExactFilenames: defaultBuildFilenames, Category: "build-scripts"},
			{Extensions: []string{".sh", ".bash", ".zsh", ".ps1", ".bat", ".cmd"}, Category: "build-scripts"},
			{Extensions: []string{".md", ".mdx"}, Category: "docs"},
			{Extensions: []string{".json", ".jsonc", ".yaml", ".yml", ".toml"}, Category: "config"},
			{Extensions: []string{".go", ".mod", ".sum"}, Category: "go"},
			{Extensions: []string{".rs"}, Category: "rust"},
			{Extensions: []string{".c", ".h", ".cc", ".cpp", ".cxx", ".hpp", ".hxx"}, Category: "cpp"},
			{Extensions: []string{".m", ".mm", ".swift"}, Category: "platform"},
			{Extensions: []string{".java", ".kt"}, Category: "java"},
			{Extensions: []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".html", ".htm", ".css", ".scss", ".sass", ".less"}, Category: "web"},
			{Fallback: true, Category: "uncategorized"},
		},
		KindRules: []KindRule{
			{PathGlobs: defaultTestGlobs, Kind: KindTest},
			{Extensions: []string{".h", ".hpp", ".hxx"}, Kind: KindHeader},
			{PathPrefixes: []string{"cmd/"}, Kind: KindTool},
			{Extensions: []string{".tsx", ".jsx"}, Kind: KindComponent},
			{Extensions: []string{".json", ".jsonc", ".yaml", ".yml", ".toml"}, Kind: KindConfig},
			{Extensions: []string{".md", ".mdx"}, Kind: KindDocs},
			{Extensions: []string{".sh", ".bash", ".zsh", ".ps1", ".bat", ".cmd"}, ExactFilenames: defaultBuildFilenames, Kind: KindScript},
			{Extensions: []string{
				".go", ".py", ".ts", ".js", ".mjs", ".cjs", ".rs", ".c", ".cc", ".cpp", ".cxx",
				".m", ".mm", ".java", ".kt", ".swift", ".html", ".htm", ".css", ".scss", ".sass", ".less",
			}, Kind: KindImplementation},
			{Fallback: true, Kind: KindOther},
		},
	}
}
