package update

import (
	"strings"
	"testing"
)

func TestVersionComparison(t *testing.T) {
	if !IsNewer("0.1.0", "v0.2.0") {
		t.Fatal("expected newer version")
	}
	if IsNewer("1.2.3", "v1.2.3") || IsNewer("2.0.0", "v1.9.9") {
		t.Fatal("unexpected newer version")
	}
}

func TestChecksumLookup(t *testing.T) {
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	got, err := checksumFor([]byte(hash+"  artifact.tar.gz\n"), "artifact.tar.gz")
	if err != nil || got != hash {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := checksumFor([]byte(hash+" other\n"), "missing"); err == nil {
		t.Fatal("expected missing checksum error")
	}
}

func TestWindowsReplacementCommandWaitsAndEscapesPaths(t *testing.T) {
	command := windowsReplacementCommand(42, `C:\O'Brien\sessionhub.exe.sessionhub-new`, `C:\O'Brien\sessionhub.exe`)
	if !strings.Contains(command, "Wait-Process -Id 42") || !strings.Contains(command, "O''Brien") || !strings.Contains(command, "Move-Item") {
		t.Fatalf("unexpected replacement command: %s", command)
	}
}
