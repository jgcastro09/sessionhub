package terminal

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFindExecutableInExecutorDir(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	dummyCmd := "testcli"
	if runtime.GOOS == "windows" {
		dummyCmd = "testcli.exe"
	}
	binaryPath := filepath.Join(binDir, dummyCmd)
	if err := os.WriteFile(binaryPath, []byte("echo hi"), 0o755); err != nil {
		t.Fatalf("write file: %v", err)
	}

	resolved, found := FindExecutable("testcli", tmpDir)
	if !found {
		t.Fatalf("expected to find testcli in executor bin dir")
	}
	if resolved != binaryPath {
		t.Errorf("got %q, want %q", resolved, binaryPath)
	}
}

func TestFindExecutableDirectPath(t *testing.T) {
	tmpDir := t.TempDir()
	dummyCmd := "customcli"
	if runtime.GOOS == "windows" {
		dummyCmd = "customcli.exe"
	}
	binaryPath := filepath.Join(tmpDir, dummyCmd)
	if err := os.WriteFile(binaryPath, []byte("echo hi"), 0o755); err != nil {
		t.Fatalf("write file: %v", err)
	}

	resolved, found := FindExecutable(binaryPath, t.TempDir())
	if !found {
		t.Fatalf("expected to find custom binary via direct path")
	}
	if resolved != binaryPath {
		t.Errorf("got %q, want %q", resolved, binaryPath)
	}
}

func TestFindExecutableSystemGlobalDirs(t *testing.T) {
	tmpDir := t.TempDir()
	dummyCmd := "globalcli"
	if runtime.GOOS == "windows" {
		dummyCmd = "globalcli.exe"
	}
	binaryPath := filepath.Join(tmpDir, dummyCmd)
	if err := os.WriteFile(binaryPath, []byte("echo hi"), 0o755); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Pass tmpDir as an extraDir to test fallback global/extra dir resolution
	resolved, found := FindExecutable("globalcli", t.TempDir(), tmpDir)
	if !found {
		t.Fatalf("expected to find globalcli via extraDirs/global dirs")
	}
	if resolved != binaryPath {
		t.Errorf("got %q, want %q", resolved, binaryPath)
	}
}
