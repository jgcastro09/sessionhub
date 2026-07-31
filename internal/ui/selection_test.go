package ui

import (
	"strings"
	"testing"

	"github.com/jgcastro09/sessionhub/internal/domain"
)

func TestHighlightANSILine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		startCol int
		endCol   int
		want     string
	}{
		{
			name:     "plain text single line",
			line:     "Hello World",
			startCol: 0,
			endCol:   4,
			want:     "\x1b[7mHello\x1b[27m World",
		},
		{
			name:     "styled text line",
			line:     "\x1b[31mHello World\x1b[0m",
			startCol: 6,
			endCol:   10,
			want:     "\x1b[31mHello \x1b[7mWorld\x1b[27m\x1b[0m",
		},
		{
			name:     "out of bounds end column",
			line:     "Short",
			startCol: 2,
			endCol:   10,
			want:     "Sh\x1b[7mort\x1b[27m",
		},
		{
			name:     "invalid column range",
			line:     "Test",
			startCol: 5,
			endCol:   2,
			want:     "Test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := highlightANSILine(tt.line, tt.startCol, tt.endCol)
			if got != tt.want {
				t.Errorf("highlightANSILine(%q, %d, %d) = %q, want %q", tt.line, tt.startCol, tt.endCol, got, tt.want)
			}
		})
	}
}

func TestTerminalRelativeCoords(t *testing.T) {
	m := Model{
		width:         100,
		height:        30,
		sidebar:       true,
		activeProject: 0,
	}

	// Sidebar = 26 cols, top bar + tab bar = 2 rows
	col, row, inBounds := m.terminalRelativeCoords(30, 5)
	if !inBounds || col != 4 || row != 3 {
		t.Errorf("terminalRelativeCoords(30, 5) = col:%d, row:%d, inBounds:%v; want 4, 3, true", col, row, inBounds)
	}

	// Outside left (in sidebar)
	col, row, inBounds = m.terminalRelativeCoords(10, 5)
	if inBounds {
		t.Errorf("expected out of bounds for sidebar click (10, 5), got col:%d, row:%d", col, row)
	}

	// Outside top (in header/tab bar)
	col, row, inBounds = m.terminalRelativeCoords(30, 1)
	if inBounds {
		t.Errorf("expected out of bounds for top bar click (30, 1), got col:%d, row:%d", col, row)
	}
}

func TestApplySelectionHighlight(t *testing.T) {
	m := Model{
		hasSelection: true,
		selStartRow:  0,
		selStartCol:  0,
		selEndRow:    1,
		selEndCol:    3,
	}

	input := "Line One\nLine Two\nLine Three"
	got := m.applySelectionHighlight(input, 20, 10)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "\x1b[7m") {
		t.Errorf("expected selection highlight on line 0, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "\x1b[7m") {
		t.Errorf("expected selection highlight on line 1, got %q", lines[1])
	}
	if strings.Contains(lines[2], "\x1b[7m") {
		t.Errorf("did not expect selection highlight on line 2, got %q", lines[2])
	}
}

func TestOverlayToast(t *testing.T) {
	base := "Header Line\nRow 1 Content Here\nRow 2 Content Here\nRow 3 Content Here"
	toast := "[ COPIED 25 CHARS ]"
	got := overlayToast(base, toast, 40, 10)
	if !strings.Contains(got, "[ COPIED 25 CHARS ]") {
		t.Errorf("expected overlayToast output to contain toast string, got %q", got)
	}
}

func TestTabLabelWidth(t *testing.T) {
	w1 := tabLabelWidth(0, "Claude Code")
	w2 := tabLabelWidth(1, "Codex")
	if w1 <= 0 || w2 <= 0 {
		t.Errorf("expected positive tab widths, got w1=%d, w2=%d", w1, w2)
	}
}

// TestRenderTopStaysSingleLine guards against a regression where a long
// project/workspace/branch combo made lipgloss word-wrap the top bar onto a
// second line. Every row below it (crucially the tab bar, whose click
// detection hardcodes y==1) assumes renderTop() is exactly one line, so a
// wrap there silently broke tab switching once real (non-placeholder)
// project names were configured.
func TestRenderTopStaysSingleLine(t *testing.T) {
	m := Model{
		width:         80,
		activeProject: 0,
		projects: []domain.Project{
			{ID: "s1", Name: "a-fairly-long-project-name-for-testing", Root: "/some/long/workspace/path/here"},
		},
	}
	out := m.renderTop()
	if lines := strings.Split(out, "\n"); len(lines) != 1 {
		t.Fatalf("renderTop() produced %d lines, want 1: %q", len(lines), out)
	}
}

func TestRenderBottomStaysSingleLine(t *testing.T) {
	m := Model{
		width:  60,
		status: "Cannot start terminal: a very long error message that would otherwise wrap across multiple lines of the status bar",
	}
	out := m.renderBottom()
	if lines := strings.Split(out, "\n"); len(lines) != 1 {
		t.Fatalf("renderBottom() produced %d lines, want 1: %q", len(lines), out)
	}

	m.lastErr = "some very long underlying error text that keeps going and going past the width of the terminal window"
	out = m.renderBottom()
	if lines := strings.Split(out, "\n"); len(lines) != 1 {
		t.Fatalf("renderBottom() with lastErr produced %d lines, want 1: %q", len(lines), out)
	}
}

func TestParseAltShortcut(t *testing.T) {
	cases := []struct {
		input   string
		wantIdx int
		wantOk  bool
	}{
		{"1", 0, false},
		{"2", 0, false},
		{"9", 0, false},
		{"alt+1", 0, true},
		{"alt+2", 1, true},
		{"esc+1", 0, true},
		{"meta+1", 0, true},
		{"ctrl+1", 0, true},
		{"\x1b1", 0, true},
		{"\x1b2", 1, true},
		{"a", 0, false},
		{"ctrl+]", 0, false},
	}
	for _, tc := range cases {
		idx, ok := parseAltShortcut(tc.input)
		if idx != tc.wantIdx || ok != tc.wantOk {
			t.Errorf("parseAltShortcut(%q) = (%d, %v), want (%d, %v)", tc.input, idx, ok, tc.wantIdx, tc.wantOk)
		}
	}
}
