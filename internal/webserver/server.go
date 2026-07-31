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
	"github.com/jgcastro09/sessionhub/internal/remote"
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
