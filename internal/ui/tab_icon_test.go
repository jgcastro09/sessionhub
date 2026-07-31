package ui

import (
	"testing"

	"github.com/jgcastro09/sessionhub/internal/domain"
)

func TestExecutorIconInference(t *testing.T) {
	tests := []struct {
		cfg  domain.ExecutorConfig
		want string
	}{
		{cfg: domain.ExecutorConfig{Name: "Zsh (Terminal)", Command: "zsh"}, want: ""},
		{cfg: domain.ExecutorConfig{Name: "Bash", Command: "bash"}, want: "🐚"},
		{cfg: domain.ExecutorConfig{Name: "Fish", Command: "fish"}, want: "🐟"},
		{cfg: domain.ExecutorConfig{Name: "PowerShell", Command: "pwsh"}, want: "⚡"},
		{cfg: domain.ExecutorConfig{Name: "CMD", Command: "cmd.exe"}, want: "🖥️"},
		{cfg: domain.ExecutorConfig{Name: "WSL", Command: "wsl.exe"}, want: "🐧"},
		{cfg: domain.ExecutorConfig{Name: "Claude Code", Command: "claude"}, want: "✳️"},
		{cfg: domain.ExecutorConfig{Name: "Codex", Command: "codex"}, want: "🤖"},
		{cfg: domain.ExecutorConfig{Name: "OpenCode", Command: "opencode"}, want: "🚀"},
		{cfg: domain.ExecutorConfig{Name: "Antigravity", Command: "agy"}, want: "🌌"},
		{cfg: domain.ExecutorConfig{Name: "My Custom CLI", Command: "custom", Icon: "🔥"}, want: "🔥"},
	}

	for _, tt := range tests {
		got := executorIcon(tt.cfg)
		if got != tt.want {
			t.Errorf("executorIcon(%+v) = %q, want %q", tt.cfg, got, tt.want)
		}
	}
}
