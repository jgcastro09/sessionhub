package gitstate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestInspectTracksUpstreamAheadBehind(t *testing.T) {
	remote := t.TempDir()
	runGit(t, remote, "init", "-q", "--bare")

	work := t.TempDir()
	runGit(t, work, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "a.txt")
	runGit(t, work, "commit", "-q", "-m", "initial")
	runGit(t, work, "remote", "add", "origin", remote)
	runGit(t, work, "push", "-q", "-u", "origin", "main")

	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "commit", "-q", "-am", "local change")

	state, err := Inspect(context.Background(), work)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if state.Branch != "main" {
		t.Fatalf("expected branch main, got %q", state.Branch)
	}
	if state.Upstream != "origin/main" {
		t.Fatalf("expected upstream origin/main, got %q", state.Upstream)
	}
	if state.Ahead != 1 || state.Behind != 0 {
		t.Fatalf("expected 1 ahead / 0 behind, got %d/%d", state.Ahead, state.Behind)
	}
	if !state.Clean {
		t.Fatalf("expected clean working tree after commit")
	}
}

func TestInspectDetectsConflict(t *testing.T) {
	work := t.TempDir()
	runGit(t, work, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "a.txt")
	runGit(t, work, "commit", "-q", "-m", "base")
	runGit(t, work, "checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("feature"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "commit", "-q", "-am", "feature change")
	runGit(t, work, "checkout", "-q", "main")
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("main"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "commit", "-q", "-am", "main change")

	mergeCmd := exec.Command("git", "merge", "feature")
	mergeCmd.Dir = work
	_ = mergeCmd.Run() // expected to fail with a conflict

	state, err := Inspect(context.Background(), work)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !state.Conflicted {
		t.Fatalf("expected a conflict to be detected, got %+v", state.Files)
	}
	found := false
	for _, f := range state.Files {
		if f.Path == "a.txt" && f.Conflict {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a.txt to be flagged as conflicted, got %+v", state.Files)
	}
}
