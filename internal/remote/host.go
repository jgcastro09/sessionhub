package remote

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/jgcastro09/sessionhub/internal/domain"
	"github.com/jgcastro09/sessionhub/internal/terminal"
)

type Backend interface {
	RemoteProjects(context.Context) ([]domain.Project, error)
	RemoteExecutors(context.Context) ([]domain.ExecutorConfig, error)
	RemoteExecutorStatuses(context.Context) ([]ExecutorStatus, error)
	RemoteStartTerminal(context.Context, string, string, int, int) (domain.Instance, error)
	RemoteTerminal(string) (*terminal.Session, bool)
	RemoteMetrics(context.Context, string) (domain.Metric, error)
	RemoteLogs(context.Context, string, int) ([]domain.LogEntry, error)
	RemoteCheckpoint(context.Context, string, string) (domain.Checkpoint, error)
	RemoteRunQueue(context.Context, string) error
	RemoteDecideApproval(context.Context, string, string, string) (domain.Approval, error)
	RemoteCancelWork(context.Context, string) error
}

type Host struct {
	listener net.Listener
	backend  Backend
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	info     Device
	mu       sync.RWMutex
	active   *peer
	view     ViewState
}

func Listen(ctx context.Context, address string, backend Backend, info Device) (*Host, error) {
	if backend == nil {
		return nil, fmt.Errorf("remote backend is required")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen for Remote Mode on %s: %w", address, err)
	}
	hostCtx, cancel := context.WithCancel(ctx)
	host := &Host{listener: listener, backend: backend, ctx: hostCtx, cancel: cancel, info: info}
	host.wg.Add(1)
	go host.accept()
	return host, nil
}

func (h *Host) Address() string { return h.listener.Addr().String() }

func (h *Host) accept() {
	defer h.wg.Done()
	for {
		connection, err := h.listener.Accept()
		if err != nil {
			if h.ctx.Err() != nil {
				return
			}
			continue
		}
		remote, err := netip.ParseAddrPort(connection.RemoteAddr().String())
		if err != nil || !isTrustedRemoteAddress(remote.Addr().Unmap()) {
			_ = connection.Close()
			continue
		}
		peerCtx, peerCancel := context.WithCancel(h.ctx)
		candidate := &peer{id: connection.RemoteAddr().String(), conn: connection, backend: h.backend, ctx: peerCtx, cancel: peerCancel, host: h}
		if !h.claim(candidate) {
			_ = WriteFrame(connection, Frame{Type: "busy", Error: "this SessionHub is already being controlled by another device"})
			peerCancel()
			_ = connection.Close()
			continue
		}
		h.wg.Add(1)
		go func(p *peer) {
			defer h.wg.Done()
			h.serve(p)
		}(candidate)
	}
}

type peer struct {
	id         string
	conn       net.Conn
	writerMu   sync.Mutex
	backend    Backend
	observed   string
	sequence   uint64
	ctx        context.Context
	cancel     context.CancelFunc
	host       *Host
	name       string
	compatible bool
}

func (h *Host) serve(peer *peer) {
	defer func() {
		peer.cancel()
		peer.releaseObservedControl()
		h.release(peer)
		_ = peer.conn.Close()
	}()
	if err := peer.write(Frame{
		Type: "hello", Payload: payload(map[string]any{
			"protocol": protocolVersion, "host": h.info,
		}),
	}); err != nil {
		return
	}
	go peer.stream()
	reader := bufio.NewReader(peer.conn)
	for {
		frame, err := ReadFrame(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				_ = peer.write(Frame{Type: "error", Error: err.Error()})
			}
			return
		}
		response := peer.handle(frame)
		response.ID = frame.ID
		if err := peer.write(response); err != nil {
			return
		}
	}
}

func (p *peer) write(frame Frame) error {
	p.writerMu.Lock()
	defer p.writerMu.Unlock()
	_ = p.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := WriteFrame(p.conn, frame)
	_ = p.conn.SetWriteDeadline(time.Time{})
	return err
}

func (p *peer) handle(frame Frame) Frame {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()
	if frame.Type != "identify" && !p.compatible {
		return Frame{Type: "error", Error: "remote SessionHub must identify with the exact same version before it can control this host"}
	}
	switch frame.Type {
	case "identify":
		var request struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		_ = json.Unmarshal(frame.Payload, &request)
		if !SameVersion(p.host.info.Version, request.Version) {
			return Frame{Type: "error", Error: fmt.Sprintf("remote SessionHub version mismatch: this host is v%s and the controller is v%s", p.host.info.Version, request.Version)}
		}
		p.compatible = true
		if request.Name != "" {
			p.name = request.Name
			p.host.setControllerName(p, request.Name)
		}
		return Frame{Type: "identified"}
	case "ui_navigation":
		var view ViewState
		if err := json.Unmarshal(frame.Payload, &view); err != nil {
			return Frame{Type: "error", Error: err.Error()}
		}
		p.host.setView(p, view)
		return Frame{Type: "ui_navigation_ack"}
	case "list_projects":
		items, err := p.backend.RemoteProjects(ctx)
		return response("projects", items, err)
	case "list_executors":
		items, err := p.backend.RemoteExecutors(ctx)
		for i := range items {
			items[i] = items[i].Redacted()
		}
		return response("executors", items, err)
	case "executor_status":
		items, err := p.backend.RemoteExecutorStatuses(ctx)
		return response("executor_status", items, err)
	case "start_terminal":
		var request struct {
			ProjectID  string `json:"project_id"`
			ExecutorID string `json:"executor_id"`
			Width      int    `json:"width"`
			Height     int    `json:"height"`
		}
		if err := json.Unmarshal(frame.Payload, &request); err != nil {
			return Frame{Type: "error", Error: err.Error()}
		}
		if request.Width < 2 {
			request.Width = 80
		}
		if request.Height < 2 {
			request.Height = 24
		}
		instance, err := p.backend.RemoteStartTerminal(ctx, request.ProjectID, request.ExecutorID, request.Width, request.Height)
		if err != nil {
			return Frame{Type: "error", Error: err.Error()}
		}
		p.observed = instance.ID
		return Frame{Type: "terminal_started", InstanceID: instance.ID, Payload: payload(instance)}
	case "open_terminal":
		if _, ok := p.backend.RemoteTerminal(frame.InstanceID); !ok {
			return Frame{Type: "error", Error: "terminal is not active"}
		}
		p.observed = frame.InstanceID
		return Frame{Type: "terminal_opened", InstanceID: frame.InstanceID}
	case "request_control":
		project, ok := p.backend.RemoteTerminal(frame.InstanceID)
		if !ok {
			return Frame{Type: "error", Error: "terminal is not active"}
		}
		owner := terminal.Owner{Kind: "remote", ID: p.id}
		err := project.Acquire(owner)
		// A person may have left a local tab focused before walking away. A
		// Remote Mode connection explicitly transfers that local lease, but it
		// never steals an automation or another remote controller's lease.
		if err != nil && project.Owner().Kind == "local" {
			err = project.Transfer(project.Owner(), owner)
		}
		if err != nil {
			return Frame{Type: "control_denied", InstanceID: frame.InstanceID, Error: err.Error()}
		}
		p.observed = frame.InstanceID
		return Frame{Type: "control_granted", InstanceID: frame.InstanceID}
	case "release_control":
		project, ok := p.backend.RemoteTerminal(frame.InstanceID)
		if !ok {
			return Frame{Type: "error", Error: "terminal is not active"}
		}
		err := project.Release(terminal.Owner{Kind: "remote", ID: p.id})
		return response("control_released", nil, err)
	case "terminal_input":
		project, ok := p.backend.RemoteTerminal(frame.InstanceID)
		if !ok {
			return Frame{Type: "error", Error: "terminal is not active"}
		}
		var input struct {
			Data string `json:"data"`
		}
		if err := json.Unmarshal(frame.Payload, &input); err != nil {
			return Frame{Type: "error", Error: err.Error()}
		}
		data, err := base64.StdEncoding.DecodeString(input.Data)
		if err == nil {
			err = project.Write(terminal.Owner{Kind: "remote", ID: p.id}, data)
		}
		if err != nil {
			return Frame{Type: "input_rejected", Sequence: frame.Sequence, Error: err.Error()}
		}
		return Frame{Type: "input_ack", InstanceID: frame.InstanceID, Sequence: frame.Sequence}
	case "terminal_key":
		project, ok := p.backend.RemoteTerminal(frame.InstanceID)
		if !ok {
			return Frame{Type: "error", Error: "terminal is not active"}
		}
		var request struct {
			Key     uv.Key `json:"key"`
			Release bool   `json:"release"`
		}
		if err := json.Unmarshal(frame.Payload, &request); err != nil {
			return Frame{Type: "error", Error: err.Error()}
		}
		var err error
		if request.Release {
			err = project.SendKey(terminal.Owner{Kind: "remote", ID: p.id}, uv.KeyReleaseEvent(request.Key))
		} else {
			err = project.SendKey(terminal.Owner{Kind: "remote", ID: p.id}, uv.KeyPressEvent(request.Key))
		}
		return response("key_ack", nil, err)
	case "terminal_paste":
		project, ok := p.backend.RemoteTerminal(frame.InstanceID)
		if !ok {
			return Frame{Type: "error", Error: "terminal is not active"}
		}
		var request struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(frame.Payload, &request); err != nil {
			return Frame{Type: "error", Error: err.Error()}
		}
		return response("paste_ack", nil, project.Paste(terminal.Owner{Kind: "remote", ID: p.id}, request.Text))
	case "resize":
		project, ok := p.backend.RemoteTerminal(frame.InstanceID)
		if !ok {
			return Frame{Type: "error", Error: "terminal is not active"}
		}
		if !project.Owner().Equal(terminal.Owner{Kind: "remote", ID: p.id}) {
			return Frame{Type: "error", Error: "remote peer does not control this terminal"}
		}
		var size struct {
			Width, Height int
		}
		if err := json.Unmarshal(frame.Payload, &size); err != nil {
			return Frame{Type: "error", Error: err.Error()}
		}
		return response("resized", nil, project.Resize(size.Width, size.Height))
	case "metrics":
		var request struct {
			ProjectID string `json:"project_id"`
		}
		_ = json.Unmarshal(frame.Payload, &request)
		value, err := p.backend.RemoteMetrics(ctx, request.ProjectID)
		return response("metrics", value, err)
	case "logs":
		var request struct {
			ProjectID string `json:"project_id"`
			Limit     int    `json:"limit"`
		}
		_ = json.Unmarshal(frame.Payload, &request)
		value, err := p.backend.RemoteLogs(ctx, request.ProjectID, request.Limit)
		return response("logs", value, err)
	case "checkpoint":
		var request struct {
			ProjectID string `json:"project_id"`
			Name      string `json:"name"`
		}
		if err := json.Unmarshal(frame.Payload, &request); err != nil {
			return Frame{Type: "error", Error: err.Error()}
		}
		value, err := p.backend.RemoteCheckpoint(ctx, request.ProjectID, request.Name)
		return response("checkpoint_created", value, err)
	case "run_queue":
		var request struct {
			ProjectID string `json:"project_id"`
		}
		if err := json.Unmarshal(frame.Payload, &request); err != nil {
			return Frame{Type: "error", Error: err.Error()}
		}
		return response("queue_started", nil, p.backend.RemoteRunQueue(ctx, request.ProjectID))
	case "decide_approval":
		var request struct {
			ApprovalID string `json:"approval_id"`
			Decision   string `json:"decision"`
		}
		if err := json.Unmarshal(frame.Payload, &request); err != nil {
			return Frame{Type: "error", Error: err.Error()}
		}
		value, err := p.backend.RemoteDecideApproval(ctx, request.ApprovalID, p.id, request.Decision)
		return response("approval_decided", value, err)
	case "cancel_work":
		var request struct {
			WorkID string `json:"work_id"`
		}
		if err := json.Unmarshal(frame.Payload, &request); err != nil {
			return Frame{Type: "error", Error: err.Error()}
		}
		return response("work_canceled", nil, p.backend.RemoteCancelWork(ctx, request.WorkID))
	default:
		return Frame{Type: "error", Error: fmt.Sprintf("unsupported remote request %q", frame.Type)}
	}
}

// Status is intentionally small: it is rendered by the controlled host so
// the person at that machine can see that a remote peer currently owns it.
type Status struct {
	Active     bool
	Controller string
	View       ViewState
}

// ViewState is the small amount of UI state shared with the controlled
// SessionHub. It keeps both people looking at the same section, project and
// terminal without giving the controlled screen any local input while the
// remote lease is active.
type ViewState struct {
	Section         string `json:"section"`
	ProjectID       string `json:"project_id,omitempty"`
	ExecutorID      string `json:"executor_id,omitempty"`
	TerminalFocused bool   `json:"terminal_focused"`
}

func (h *Host) Status() Status {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.active == nil {
		return Status{}
	}
	name := h.active.name
	if name == "" {
		name = h.active.id
	}
	return Status{Active: true, Controller: name, View: h.view}
}

func (h *Host) claim(candidate *peer) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active != nil {
		return false
	}
	h.active = candidate
	return true
}

func (h *Host) release(candidate *peer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active == candidate {
		h.active = nil
		h.view = ViewState{}
	}
}

func (h *Host) setView(candidate *peer, view ViewState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active == candidate {
		h.view = view
	}
}

// Revoke immediately ends the active remote-control connection. The peer's
// normal cleanup path releases any terminal lease and clears host status.
func (h *Host) Revoke() bool {
	h.mu.RLock()
	active := h.active
	h.mu.RUnlock()
	if active == nil {
		return false
	}
	_ = active.conn.Close()
	return true
}

func (h *Host) setControllerName(candidate *peer, name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active == candidate {
		h.active.name = name
	}
}

func response(kind string, value any, err error) Frame {
	if err != nil {
		return Frame{Type: "error", Error: err.Error()}
	}
	return Frame{Type: kind, Payload: payload(value)}
}

func (p *peer) stream() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var previous string
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			if p.observed == "" {
				continue
			}
			project, ok := p.backend.RemoteTerminal(p.observed)
			if !ok {
				continue
			}
			snapshot := project.Snapshot()
			if snapshot == previous {
				continue
			}
			previous = snapshot
			p.sequence++
			if err := p.write(Frame{
				Type: "terminal_snapshot", InstanceID: p.observed,
				Sequence: p.sequence, Payload: payload(map[string]any{
					"screen": snapshot, "state": project.State(),
					"owner": project.Owner(), "alternate_screen": project.IsAltScreen(),
				}),
			}); err != nil {
				return
			}
		}
	}
}

func (p *peer) releaseObservedControl() {
	if p.observed == "" {
		return
	}
	if project, ok := p.backend.RemoteTerminal(p.observed); ok {
		owner := terminal.Owner{Kind: "remote", ID: p.id}
		if project.Owner().Equal(owner) {
			_ = project.Release(owner)
		}
	}
}

func (h *Host) Close() error {
	h.cancel()
	// Canceling a context does not interrupt a blocking TCP ReadFrame. Close
	// the sole active controller socket as well so SessionHub can always exit
	// promptly while a device is remotely connected.
	h.mu.RLock()
	active := h.active
	h.mu.RUnlock()
	if active != nil {
		_ = active.conn.Close()
	}
	err := h.listener.Close()
	h.wg.Wait()
	return err
}
