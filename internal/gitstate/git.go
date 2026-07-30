package gitstate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var ErrNotRepository = errors.New("workspace is not a Git repository")

type File struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

type State struct {
	Repository string `json:"repository"`
	Branch     string `json:"branch"`
	Head       string `json:"head"`
	Clean      bool   `json:"clean"`
	Files      []File `json:"files"`
}

func Inspect(ctx context.Context, workspace string) (State, error) {
	root, err := run(ctx, workspace, "rev-parse", "--show-toplevel")
	if err != nil {
		return State{}, ErrNotRepository
	}
	branch, _ := run(ctx, workspace, "branch", "--show-current")
	head, err := run(ctx, workspace, "rev-parse", "HEAD")
	if err != nil {
		head = ""
	}
	status, err := runRaw(ctx, workspace, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return State{}, fmt.Errorf("read Git status: %w", err)
	}
	state := State{
		Repository: strings.TrimSpace(root),
		Branch:     strings.TrimSpace(branch),
		Head:       strings.TrimSpace(head),
		Clean:      len(status) == 0,
	}
	records := bytes.Split(status, []byte{0})
	for i := 0; i < len(records); i++ {
		record := records[i]
		if len(record) < 4 {
			continue
		}
		code := string(record[:2])
		path := string(record[3:])
		if (code[0] == 'R' || code[0] == 'C') && i+1 < len(records) {
			i++
			path = string(records[i])
		}
		state.Files = append(state.Files, File{Path: path, Status: code})
	}
	return state, nil
}

func Diff(ctx context.Context, workspace string) (string, error) {
	data, err := runRaw(ctx, workspace, "diff", "--no-ext-diff", "--binary")
	if err != nil {
		return "", fmt.Errorf("read Git diff: %w", err)
	}
	return string(data), nil
}

func run(ctx context.Context, dir string, args ...string) (string, error) {
	data, err := runRaw(ctx, dir, args...)
	return string(data), err
}

func runRaw(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "color.ui=false"}, args...)...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return data, nil
}
