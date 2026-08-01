package registry

import (
	"path/filepath"
	"strings"
)

// languageByExt is deliberately generic — no project-specific names, per
// the config-file convention documented in Config. exactLanguage overrides
// it for filenames with no useful extension.
var languageByExt = map[string]string{
	".go": "go", ".mod": "go", ".sum": "go",
	".ts": "typescript", ".tsx": "typescript",
	".js": "javascript", ".jsx": "javascript", ".mjs": "javascript", ".cjs": "javascript",
	".py":   "python",
	".html": "html", ".htm": "html",
	".css": "css", ".scss": "css", ".sass": "css", ".less": "css",
	".md": "markdown", ".mdx": "markdown",
	".json": "json", ".jsonc": "json",
	".yaml": "yaml", ".yml": "yaml",
	".toml": "toml",
	".sql":  "sql",
	".rs":   "rust",
	".c":    "c", ".h": "c",
	".cc": "cpp", ".cpp": "cpp", ".cxx": "cpp", ".hpp": "cpp", ".hxx": "cpp",
	".m": "objective-c", ".mm": "objective-c",
	".java": "java", ".kt": "kotlin", ".swift": "swift",
	".sh": "shell", ".bash": "shell", ".zsh": "shell",
	".ps1": "powershell", ".bat": "batch", ".cmd": "batch",
	".proto": "protobuf", ".graphql": "graphql",
	".glsl": "glsl", ".vert": "glsl", ".frag": "glsl",
}

var exactLanguage = map[string]string{
	"Makefile": "make", "Dockerfile": "dockerfile", "Rakefile": "ruby", "Gemfile": "ruby",
	"CMakeLists.txt":  "cmake",
	".goreleaser.yml": "yaml", ".goreleaser.yaml": "yaml",
}

// languageForPath resolves a display language for relPath, favoring an
// exact-filename override (e.g. Makefile) over the extension table.
func languageForPath(relPath string) string {
	base := filepath.Base(relPath)
	if lang, ok := exactLanguage[base]; ok {
		return lang
	}
	ext := strings.ToLower(filepath.Ext(relPath))
	if lang, ok := languageByExt[ext]; ok {
		return lang
	}
	return "unknown"
}
