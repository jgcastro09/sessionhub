package automation

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func TestRunCommandUsesRealExitCode(t *testing.T) {
	var config CommandConfig
	if runtime.GOOS == "windows" {
		config = CommandConfig{Command: "cmd.exe", Args: []string{"/c", "echo ok"}}
	} else {
		config = CommandConfig{Command: "sh", Args: []string{"-c", "printf ok"}}
	}
	result, err := RunCommand(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.Stdout == "" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestRunCommandTimeout(t *testing.T) {
	var config CommandConfig
	if runtime.GOOS == "windows" {
		config = CommandConfig{Command: "powershell.exe", Args: []string{"-NoProfile", "-Command", "Start-Sleep -Seconds 2"}, Timeout: 50 * time.Millisecond}
	} else {
		config = CommandConfig{Command: "sh", Args: []string{"-c", "sleep 2"}, Timeout: 50 * time.Millisecond}
	}
	result, err := RunCommand(context.Background(), config)
	if err == nil || !result.TimedOut {
		t.Fatalf("expected timeout, got %#v %v", result, err)
	}
}
