package registry

import (
	"path/filepath"
	"strings"
)

// languageByExt and defaultCategoryByExt are deliberately generic — no
// NodeStage-specific or otherwise project-specific names, per plan 5.1.
var languageByExt = map[string]string{
	".go":    "go",
	".ts":    "typescript",
	".tsx":   "typescript",
	".js":    "javascript",
	".jsx":   "javascript",
	".mjs":   "javascript",
	".py":    "python",
	".html":  "html",
	".htm":   "html",
	".css":   "css",
	".scss":  "css",
	".sh":    "shell",
	".bash":  "shell",
	".zsh":   "shell",
	".md":    "markdown",
	".json":  "json",
	".yaml":  "yaml",
	".yml":   "yaml",
	".toml":  "toml",
	".sql":   "sql",
	".rs":    "rust",
	".c":     "c",
	".h":     "c",
	".cpp":   "cpp",
	".hpp":   "cpp",
	".mm":    "objective-c",
	".m":     "objective-c",
	".java":  "java",
	".kt":    "kotlin",
	".swift": "swift",
}

var defaultCategoryByExt = map[string]string{
	".go":    "go",
	".ts":    "web",
	".tsx":   "web",
	".js":    "web",
	".jsx":   "web",
	".mjs":   "web",
	".html":  "web",
	".css":   "web",
	".scss":  "web",
	".py":    "python-tools",
	".sh":    "build-scripts",
	".bash":  "build-scripts",
	".zsh":   "build-scripts",
	".md":    "docs",
	".json":  "config",
	".yaml":  "config",
	".yml":   "config",
	".toml":  "config",
	".rs":    "rust",
	".c":     "cpp",
	".h":     "cpp",
	".cpp":   "cpp",
	".hpp":   "cpp",
	".mm":    "platform",
	".m":     "platform",
	".java":  "java",
	".kt":    "java",
	".swift": "platform",
}

var buildFileNames = map[string]bool{
	"Makefile": true, "makefile": true, "Dockerfile": true, "Rakefile": true,
	"Gemfile": true, ".goreleaser.yml": true, ".goreleaser.yaml": true,
}

func classify(relPath string, categories map[string]string) (language, category string) {
	base := filepath.Base(relPath)
	if buildFileNames[base] {
		return "build", "build-scripts"
	}
	ext := strings.ToLower(filepath.Ext(relPath))
	language = languageByExt[ext]
	if language == "" {
		language = "unknown"
	}
	if override, ok := categories[ext]; ok {
		return language, override
	}
	if cat, ok := defaultCategoryByExt[ext]; ok {
		return language, cat
	}
	return language, "uncategorized"
}
