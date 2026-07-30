package ui

import (
	"strings"
	"testing"

	"github.com/jgcastro09/sessionhub/internal/automation"
)

func TestAutomationDetailsKeepsTerminalOutputInsideModalAsPlainText(t *testing.T) {
	form := automationDetailsForm(automation.SimpleAutomation{
		Name:       "Build site",
		LiveOutput: "\x1b[48;5;0mOpenCode answer\x1b[0m",
		LastRun: automation.LastRun{
			Status: automation.StatusRunning,
		},
	})
	if form.title != "Automation History: Build site" {
		t.Fatalf("unexpected details title: %q", form.title)
	}
	if !strings.Contains(form.details, "Live terminal output:\nOpenCode answer") {
		t.Fatalf("live output missing from modal: %q", form.details)
	}
	if strings.Contains(form.details, "\x1b") {
		t.Fatalf("terminal escape sequence leaked into modal: %q", form.details)
	}
}
