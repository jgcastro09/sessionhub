package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jgcastro09/sessionhub/internal/safego"
)

type CommandConfig struct {
	Command     string        `json:"command"`
	Args        []string      `json:"args,omitempty"`
	WorkingDir  string        `json:"working_dir,omitempty"`
	Environment []string      `json:"environment,omitempty"`
	Timeout     time.Duration `json:"timeout,omitempty"`
	Sensitive   bool          `json:"sensitive,omitempty"`
}

func (c *CommandConfig) UnmarshalJSON(data []byte) error {
	type rawConfig struct {
		Command     string   `json:"command"`
		Args        []string `json:"args"`
		WorkingDir  string   `json:"working_dir"`
		Environment []string `json:"environment"`
		Timeout     string   `json:"timeout"`
		Sensitive   bool     `json:"sensitive"`
	}
	var raw rawConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.Command, c.Args, c.WorkingDir = raw.Command, raw.Args, raw.WorkingDir
	c.Environment, c.Sensitive = raw.Environment, raw.Sensitive
	if raw.Timeout != "" {
		value, err := time.ParseDuration(raw.Timeout)
		if err != nil {
			return fmt.Errorf("parse command timeout: %w", err)
		}
		c.Timeout = value
	}
	return nil
}

type CommandResult struct {
	Command    string        `json:"command"`
	Args       []string      `json:"args"`
	WorkingDir string        `json:"working_dir"`
	Stdout     string        `json:"stdout"`
	Stderr     string        `json:"stderr"`
	ExitCode   int           `json:"exit_code"`
	Duration   time.Duration `json:"duration"`
	TimedOut   bool          `json:"timed_out"`
}

func RunCommand(ctx context.Context, config CommandConfig) (CommandResult, error) {
	if strings.TrimSpace(config.Command) == "" {
		return CommandResult{}, fmt.Errorf("deterministic command is required")
	}
	if _, err := exec.LookPath(config.Command); err != nil {
		return CommandResult{}, fmt.Errorf("find deterministic command %q: %w", config.Command, err)
	}
	commandContext := ctx
	cancel := func() {}
	if config.Timeout > 0 {
		commandContext, cancel = context.WithTimeout(ctx, config.Timeout)
	}
	defer cancel()
	cmd := exec.CommandContext(commandContext, config.Command, config.Args...)
	cmd.Dir = config.WorkingDir
	if len(config.Environment) > 0 {
		cmd.Env = append(cmd.Environ(), config.Environment...)
	}
	prepareProcess(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	started := time.Now()
	if err := cmd.Start(); err != nil {
		return CommandResult{}, fmt.Errorf("start deterministic command: %w", err)
	}
	done := make(chan error, 1)
	go func() { safego.Run("automation.command.wait", func() { done <- cmd.Wait() }) }()
	var runErr error
	select {
	case runErr = <-done:
	case <-commandContext.Done():
		terminateProcessTree(cmd)
		runErr = <-done
	}
	result := CommandResult{
		Command: config.Command, Args: append([]string(nil), config.Args...),
		WorkingDir: config.WorkingDir, Stdout: stdout.String(), Stderr: stderr.String(),
		ExitCode: -1, Duration: time.Since(started),
		TimedOut: errors.Is(commandContext.Err(), context.DeadlineExceeded),
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if result.TimedOut {
		return result, fmt.Errorf("deterministic command timed out after %s", config.Timeout)
	}
	if runErr != nil {
		return result, fmt.Errorf("deterministic command exited with code %d: %w", result.ExitCode, runErr)
	}
	return result, nil
}
