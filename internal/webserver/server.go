// Package webserver exposes a read-only HTTP view of a running SessionHub —
// projects, executors, metrics, logs and automation status — for the
// companion web panel. It deliberately mirrors internal/remote's Backend
// pattern instead of talking to internal/store directly, so the same App
// methods back both the TCP remote-control protocol and this HTTP surface.
package webserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/jgcastro09/sessionhub/internal/config"
	"github.com/jgcastro09/sessionhub/internal/domain"
	"github.com/jgcastro09/sessionhub/internal/events"
	"github.com/jgcastro09/sessionhub/internal/registry"
	"github.com/jgcastro09/sessionhub/internal/remote"
	"github.com/jgcastro09/sessionhub/internal/tasks"
)

// Backend is the read-only subset of data a monitoring dashboard needs. Its
// first five methods intentionally share signatures with internal/remote's
// Backend interface (RemoteProjects, RemoteExecutors, ...) so *app.App
// satisfies both without duplicate glue code.
type Backend interface {
	RemoteProjects(context.Context) ([]domain.Project, error)
	RemoteExecutors(context.Context) ([]domain.ExecutorConfig, error)
	RemoteExecutorStatuses(context.Context) ([]remote.ExecutorStatus, error)
	RemoteMetrics(context.Context, string) (domain.Metric, error)
	RemoteLogs(context.Context, string, int) ([]domain.LogEntry, error)
	WebQueue(context.Context, string) ([]domain.QueueItem, error)
	WebSchedules(context.Context, string) ([]domain.Schedule, error)
	WebPipelines(context.Context, string) ([]domain.Pipeline, error)

	WebTasksList(ctx context.Context, projectID string, filter tasks.Filter) ([]tasks.Card, error)
	WebTasksGet(ctx context.Context, projectID, taskID string) (tasks.Card, error)
	WebTasksCreate(ctx context.Context, projectID string, input tasks.CreateInput) (tasks.Card, error)
	WebTasksUpdate(ctx context.Context, projectID, taskID string, patch tasks.Patch) (tasks.Card, error)
	WebTasksSetStatus(ctx context.Context, projectID, taskID string, status tasks.Status) (tasks.Card, error)
	WebTasksAudit(ctx context.Context, projectID, taskID string) (tasks.AuditReport, error)
	WebTasksClaims(ctx context.Context, projectID string) ([]tasks.Claim, error)
	WebTasksClaim(ctx context.Context, projectID, taskID, executorID, terminalID string) ([]tasks.Claim, error)
	WebTasksReleaseClaim(ctx context.Context, projectID, terminalID string) ([]tasks.Claim, error)

	WebRegistryList(ctx context.Context, projectID string) ([]registry.Entry, error)
	WebRegistryGet(ctx context.Context, projectID, entryID string) (registry.Entry, error)
	WebRegistrySearch(ctx context.Context, projectID, query string, limit int) ([]registry.SearchResult, error)
	WebRegistrySemanticSearch(ctx context.Context, projectID, query string, limit int) ([]registry.SearchResult, error)
	WebRegistryScan(ctx context.Context, projectID string) ([]registry.Entry, error)
	WebRegistryHealth(ctx context.Context, projectID string) (registry.CoverageReport, error)
	WebRegistryReview(ctx context.Context, projectID, entryID string, input registry.ReviewInput) (registry.Entry, error)
	WebRegistrySource(ctx context.Context, projectID, entryID string) (string, error)
	WebRegistryGitStatus(ctx context.Context, projectID string) (registry.GitCorrelation, error)

	// Subscribe opens a live feed of Task/Registry events for one project,
	// backing handleEvents. The returned cancel func must be called once the
	// SSE connection ends.
	Subscribe(projectID string) (<-chan events.Event, func())
}

// Server hosts the web panel's HTTP API (and, once embedded, its static
// frontend build) on a single listener.
type Server struct {
	httpServer *http.Server
	listener   net.Listener
	backend    Backend
	bindMode   config.WebBindMode
	pairing    *pairing
	ctx        context.Context
	cancel     context.CancelFunc
}

// Listen starts the web panel HTTP server on address (e.g. "0.0.0.0:0" for
// an ephemeral port reachable on the LAN/tailnet). bindMode controls which
// requests requireTrusted lets through: see config.WebBindMode.
func Listen(ctx context.Context, address string, backend Backend, bindMode config.WebBindMode) (*Server, error) {
	if backend == nil {
		return nil, fmt.Errorf("web panel backend is required")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen for web panel on %s: %w", address, err)
	}
	serverCtx, cancel := context.WithCancel(ctx)
	server := &Server{listener: listener, backend: backend, bindMode: bindMode, pairing: newPairing(), ctx: serverCtx, cancel: cancel}
	mux := http.NewServeMux()
	server.routes(mux)
	server.httpServer = &http.Server{Handler: mux, BaseContext: func(net.Listener) context.Context { return serverCtx }}
	go func() {
		_ = server.httpServer.Serve(listener)
	}()
	return server, nil
}

// Address returns the actual host:port the server is listening on, useful
// when Listen was given an ephemeral ":0" port.
func (s *Server) Address() string { return s.listener.Addr().String() }

// PairingCode is the current code a device must send to /api/pair to get a
// project cookie under WebBindLocal/WebBindBoth. Rendered in the TUI.
func (s *Server) PairingCode() string { return s.pairing.Code() }

// RegeneratePairingCode invalidates the current pairing code (already-paired
// devices keep their cookies) and returns the new one.
func (s *Server) RegeneratePairingCode() string { return s.pairing.Regenerate() }

// Close stops accepting new connections and gives in-flight requests a
// short grace period before forcing the listener closed.
func (s *Server) Close() error {
	s.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}
