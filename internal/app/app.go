package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jgcastro09/sessionhub/internal/automation"
	"github.com/jgcastro09/sessionhub/internal/config"
	contexthub "github.com/jgcastro09/sessionhub/internal/context"
	"github.com/jgcastro09/sessionhub/internal/domain"
	"github.com/jgcastro09/sessionhub/internal/executor"
	"github.com/jgcastro09/sessionhub/internal/metrics"
	"github.com/jgcastro09/sessionhub/internal/remote"
	"github.com/jgcastro09/sessionhub/internal/store"
	"github.com/jgcastro09/sessionhub/internal/terminal"
	"github.com/jgcastro09/sessionhub/internal/voice"
)

type App struct {
	Version             string
	Paths               config.Paths
	Store               *store.Store
	Terminals           *terminal.Manager
	Executors           *executor.Service
	Context             *contexthub.Service
	Metrics             *metrics.Calculator
	Automation          *automation.Engine
	AutomationScheduler *automation.Scheduler
	Voice               *voice.Manager
	ctx                 context.Context
	cancel              context.CancelFunc
	closeOnce           sync.Once
	remoteMu            sync.Mutex
	remoteHost          *remote.Host
	remoteDiscovery     *remote.Discovery
	networkSettings     config.NetworkSettings
	recovered           int64
}

func New(parent context.Context, paths config.Paths, version string) (*App, error) {
	ctx, cancel := context.WithCancel(parent)
	repository, err := store.Open(ctx, paths.Database)
	if err != nil {
		cancel()
		return nil, err
	}
	recovered, err := repository.Recover(ctx)
	if err != nil {
		_ = repository.Close()
		cancel()
		return nil, fmt.Errorf("recover interrupted work: %w", err)
	}
	terminals := terminal.NewManager(ctx, repository, 10_000)
	executors := executor.New(ctx, repository, terminals)
	automationEngine := automation.NewEngine(repository, executors, 4)
	automationScheduler, err := automation.NewScheduler(ctx, paths.Root, repository, executors)
	if err != nil {
		_ = repository.Close()
		cancel()
		return nil, fmt.Errorf("load automations: %w", err)
	}
	networkSettings, err := config.LoadNetworkSettings(paths.NetworkSettings)
	if err != nil {
		_ = repository.Close()
		cancel()
		return nil, err
	}
	a := &App{
		Version: version, Paths: paths, Store: repository,
		Terminals: terminals, Executors: executors,
		Context: contexthub.New(repository), Metrics: metrics.NewCalculator(),
		Automation: automationEngine, AutomationScheduler: automationScheduler,
		Voice:           voice.NewManager(paths.Tools),
		networkSettings: networkSettings,
		ctx:             ctx, cancel: cancel, recovered: recovered,
	}
	// Remote Mode is a peer feature: every open SessionHub hosts its own
	// terminal endpoint and advertises it only on the local LAN/tailnet.
	// Failure to bind discovery must not prevent the normal local app from
	// starting (for example, another process may temporarily own the UDP port).
	if err := a.StartAutomaticRemote(); err != nil {
		_ = repository.Log(context.Background(), domain.LogEntry{Level: "warn", Kind: "remote", Message: "automatic remote discovery unavailable: " + err.Error()})
	}
	go executors.Run()
	automationScheduler.Start()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if err := automationEngine.ProcessSchedules(ctx, now); err != nil {
					_ = repository.Log(context.Background(), domain.LogEntry{
						Level: "error", Kind: "scheduler", Message: err.Error(),
					})
				}
			}
		}
	}()
	return a, nil
}

func (a *App) RecoveredCount() int64 { return a.recovered }

func (a *App) Close() error {
	var result error
	a.closeOnce.Do(func() {
		a.AutomationScheduler.Close()
		a.cancel()
		a.Automation.Wait()
		result = errors.Join(a.StopRemoteHost(), a.Terminals.Close(), a.Voice.Close(), a.Store.Close())
	})
	return result
}
