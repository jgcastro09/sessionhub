package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/jgcastro09/sessionhub/internal/app"
	"github.com/jgcastro09/sessionhub/internal/config"
	"github.com/jgcastro09/sessionhub/internal/ui"
)

var (
	version   = "0.3.33"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sessionhub:", err)
		os.Exit(1)
	}
}

func run() error {
	showVersion := flag.Bool("version", false, "print version and build metadata")
	dataDir := flag.String("data-dir", "", "override the Session Hub data directory")
	remoteHost := flag.String("remote-host", "", "deprecated: Remote Mode starts and discovers peers automatically")
	flag.Parse()
	if *showVersion {
		fmt.Printf("sessionhub %s (%s, %s)\n", version, commit, buildDate)
		return nil
	}
	if *dataDir != "" {
		if err := os.Setenv("SESSIONHUB_DATA_DIR", *dataDir); err != nil {
			return err
		}
	}
	paths, err := config.ResolvePaths()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	application, err := app.New(ctx, paths, version)
	if err != nil {
		return err
	}
	if *remoteHost != "" {
		fmt.Fprintln(os.Stderr, "sessionhub: --remote-host is no longer needed; Remote Mode starts automatically")
	}
	enableConsoleMouseInput()
	program := tea.NewProgram(ui.New(application))
	_, runErr := program.Run()
	closeErr := application.Close()
	if runErr != nil && !errors.Is(runErr, tea.ErrInterrupted) {
		return errors.Join(runErr, closeErr)
	}
	return closeErr
}
