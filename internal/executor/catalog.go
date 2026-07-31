package executor

import (
	"os"
	"path/filepath"
	"runtime"
)

// Provider is a known CLI with a verified install command, offered as a
// pick-list entry in the "Add a CLI" flow instead of typing everything by
// hand. Only providers with a confirmed, official install method are
// listed here — guessing a package name wrong wastes an install attempt
// and, worse, could point at an unofficial/impersonating package.
type Category string

const (
	CategoryAI    Category = "AI CLI"
	CategoryShell Category = "System Terminal"
)

// Provider is a known CLI or system shell offered as a pick-list entry in the
// "Add a Terminal / CLI" flow.
type Provider struct {
	Name        string
	Command     string
	Args        []string
	Category    Category
	UseHostHome bool
	// SupportedOS lists OS names ("darwin", "windows", "linux") this provider is valid for.
	// Empty means supported on all operating systems.
	SupportedOS []string
	// InstallCmd returns the raw shell line to run (WorkingDir is already
	// the executor's own folder), branching on GOOS where needed.
	InstallCmd func() string
	// Isolated reports whether InstallCmd places the binary inside the
	// executor's own managed folder. false means the official installer
	// doesn't support a custom directory, so the binary lands wherever it
	// always does — login/config are still isolated via HOME, just not
	// the binary's location.
	Isolated bool
	// ConfigEnvVar, when set, is a documented variable this CLI reads for
	// its own config/login directory (e.g. Codex's CODEX_HOME). Native
	// (non-Node) binaries especially may not respect a HOME/USERPROFILE
	// override the way most Node-based CLIs do, so the documented,
	// CLI-specific variable is set as a belt-and-suspenders measure
	// alongside the universal HOME override.
	ConfigEnvVar string
	// DefaultDirs, only set when Isolated is false, lists the installer's
	// own conventional install location(s) so SessionHub can still find
	// the binary afterward.
	DefaultDirs func() []string
}

// Catalog lists providers with verified install commands and system shell options.
var Catalog = []Provider{
	// System Terminals (Shells)
	{
		Name:        "Zsh (Terminal)",
		Command:     "zsh",
		Args:        []string{"-l"},
		Category:    CategoryShell,
		UseHostHome: true,
		SupportedOS: []string{"darwin", "linux"},
	},
	{
		Name:        "Bash (Shell)",
		Command:     "bash",
		Args:        []string{"-l"},
		Category:    CategoryShell,
		UseHostHome: true,
		SupportedOS: []string{"darwin", "linux"},
	},
	{
		Name:        "Fish (Shell)",
		Command:     "fish",
		Category:    CategoryShell,
		UseHostHome: true,
		SupportedOS: []string{"darwin", "linux"},
	},
	{
		Name: "PowerShell",
		Command: func() string {
			if runtime.GOOS == "windows" {
				return "powershell.exe"
			}
			return "pwsh"
		}(),
		Args:        []string{"-NoLogo"},
		Category:    CategoryShell,
		UseHostHome: true,
		SupportedOS: []string{"windows"},
	},
	{
		Name:        "CMD (Command Prompt)",
		Command:     "cmd.exe",
		Category:    CategoryShell,
		UseHostHome: true,
		SupportedOS: []string{"windows"},
	},
	{
		Name:        "WSL (Linux)",
		Command:     "wsl.exe",
		Category:    CategoryShell,
		UseHostHome: true,
		SupportedOS: []string{"windows"},
	},
	// AI CLIs
	{
		Name:         "Codex",
		Command:      "codex",
		Args:         []string{"--yolo"},
		Category:     CategoryAI,
		InstallCmd:   func() string { return "npm install @openai/codex" },
		Isolated:     true,
		ConfigEnvVar: "CODEX_HOME",
	},
	{
		Name:       "Claude Code",
		Command:    "claude",
		Args:       []string{"--dangerously-skip-permissions"},
		Category:   CategoryAI,
		InstallCmd: func() string { return "npm install @anthropic-ai/claude-code" },
		Isolated:   true,
	},
	{
		Name:       "OpenCode",
		Command:    "opencode",
		Category:   CategoryAI,
		InstallCmd: func() string { return "npm install opencode-ai" },
		Isolated:   true,
	},
	{
		Name:     "Antigravity",
		Command:  "agy",
		Category: CategoryAI,
		InstallCmd: func() string {
			if runtime.GOOS == "windows" {
				return `powershell -NoProfile -Command "irm https://antigravity.google/cli/install.ps1 | iex"`
			}
			return "curl -fsSL https://antigravity.google/cli/install.sh | bash"
		},
		Isolated: false,
		DefaultDirs: func() []string {
			if runtime.GOOS == "windows" {
				if appData := os.Getenv("LOCALAPPDATA"); appData != "" {
					return []string{filepath.Join(appData, "agy", "bin")}
				}
				return nil
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return nil
			}
			return []string{filepath.Join(home, ".local", "bin")}
		},
	},
}

// CatalogForOS returns catalog providers valid for the current OS (runtime.GOOS).
func CatalogForOS() []Provider {
	goos := runtime.GOOS
	filtered := make([]Provider, 0, len(Catalog))
	for _, p := range Catalog {
		if len(p.SupportedOS) > 0 {
			matched := false
			for _, osName := range p.SupportedOS {
				if osName == goos {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		filtered = append(filtered, p)
	}
	return filtered
}
