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
	Path     string `json:"path"`
	Status   string `json:"status"`
	Conflict bool   `json:"conflict"`
}

type State struct {
	Repository string `json:"repository"`
	Branch     string `json:"branch"`
	Head       string `json:"head"`
	// Upstream, Ahead, and Behind are zero-valued when the branch has no
	// tracking upstream configured — that is not an error.
	Upstream   string `json:"upstream,omitempty"`
	Ahead      int    `json:"ahead"`
	Behind     int    `json:"behind"`
	Clean      bool   `json:"clean"`
	Conflicted bool   `json:"conflicted"`
	Files      []File `json:"files"`
}

// conflictCodes are the porcelain v1 XY pairs meaning "unresolved merge
// conflict," per git-status(1).
var conflictCodes = map[string]bool{
	"DD": true, "AU": true, "UD": true, "UA": true, "DU": true, "AA": true, "UU": true,
}

// Inspect reads Git's view of workspace: branch, upstream tracking,
// ahead/behind counts, and working-tree file status, including which files
// have an unresolved conflict. It only ever runs read-only plumbing
// commands (rev-parse, branch, status, rev-list) — never anything that
// mutates the worktree, index, or refs.
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
		conflict := conflictCodes[code]
		if conflict {
			state.Conflicted = true
		}
		state.Files = append(state.Files, File{Path: path, Status: code, Conflict: conflict})
	}

	if upstream, err := run(ctx, workspace, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err == nil {
		state.Upstream = strings.TrimSpace(upstream)
		if counts, err := run(ctx, workspace, "rev-list", "--left-right", "--count", state.Upstream+"...HEAD"); err == nil {
			var behind, ahead int
			if _, scanErr := fmt.Sscanf(strings.TrimSpace(counts), "%d\t%d", &behind, &ahead); scanErr == nil {
				state.Behind, state.Ahead = behind, ahead
			}
		}
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
