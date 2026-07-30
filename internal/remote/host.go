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

	"github.com/nodestage/sessionhub/internal/domain"
	"github.com/nodestage/sessionhub/internal/terminal"
)

type Backend interface {
	RemoteSessions(context.Context) ([]domain.Session, error)
	RemoteExecutors(context.Context) ([]domain.ExecutorConfig, error)
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
}

func Listen(ctx context.Context, address string, backend Backend) (*Host, error) {
	if backend == nil {
		return nil, fmt.Errorf("remote backend is required")
	}
	validated, err := ValidateListenAddress(address)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", validated.String())
	if err != nil {
		return nil, fmt.Errorf("listen on Tailscale address %s: %w", validated, err)
	}
	hostCtx, cancel := context.WithCancel(ctx)
	host := &Host{listener: listener, backend: backend, ctx: hostCtx, cancel: cancel}
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
		if err != nil || !IsTailscaleIP(remote.Addr().Unmap()) {
			_ = connection.Close()
			continue
		}
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			h.serve(connection)
		}()
	}
}

type peer struct {
	id       string
	conn     net.Conn
	writerMu sync.Mutex
	backend  Backend
	observed string
	sequence uint64
	ctx      context.Context
	cancel   context.CancelFunc
}

func (h *Host) serve(connection net.Conn) {
	ctx, cancel := context.WithCancel(h.ctx)
	peer := &peer{
		id: connection.RemoteAddr().String(), conn: connection,
		backend: h.backend, ctx: ctx, cancel: cancel,
	}
	defer func() {
		cancel()
		peer.releaseObservedControl()
		_ = connection.Close()
	}()
	if err := peer.write(Frame{
		Type: "hello", Payload: payload(map[string]any{
			"protocol": protocolVersion, "controller": peer.id,
		}),
	}); err != nil {
		return
	}
	go peer.stream()
	reader := bufio.NewReader(connection)
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
	switch frame.Type {
	case "list_sessions":
		items, err := p.backend.RemoteSessions(ctx)
		return response("sessions", items, err)
	case "list_executors":
		items, err := p.backend.RemoteExecutors(ctx)
		for i := range items {
			items[i] = items[i].Redacted()
		}
		return response("executors", items, err)
	case "open_terminal":
		if _, ok := p.backend.RemoteTerminal(frame.InstanceID); !ok {
			return Frame{Type: "error", Error: "terminal is not active"}
		}
		p.observed = frame.InstanceID
		return Frame{Type: "terminal_opened", InstanceID: frame.InstanceID}
	case "request_control":
		session, ok := p.backend.RemoteTerminal(frame.InstanceID)
		if !ok {
			return Frame{Type: "error", Error: "terminal is not active"}
		}
		if err := session.Acquire(terminal.Owner{Kind: "remote", ID: p.id}); err != nil {
			return Frame{Type: "control_denied", InstanceID: frame.InstanceID, Error: err.Error()}
		}
		p.observed = frame.InstanceID
		return Frame{Type: "control_granted", InstanceID: frame.InstanceID}
	case "release_control":
		session, ok := p.backend.RemoteTerminal(frame.InstanceID)
		if !ok {
			return Frame{Type: "error", Error: "terminal is not active"}
		}
		err := session.Release(terminal.Owner{Kind: "remote", ID: p.id})
		return response("control_released", nil, err)
	case "terminal_input":
		session, ok := p.backend.RemoteTerminal(frame.InstanceID)
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
			err = session.Write(terminal.Owner{Kind: "remote", ID: p.id}, data)
		}
		if err != nil {
			return Frame{Type: "input_rejected", Sequence: frame.Sequence, Error: err.Error()}
		}
		return Frame{Type: "input_ack", InstanceID: frame.InstanceID, Sequence: frame.Sequence}
	case "resize":
		session, ok := p.backend.RemoteTerminal(frame.InstanceID)
		if !ok {
			return Frame{Type: "error", Error: "terminal is not active"}
		}
		if !session.Owner().Equal(terminal.Owner{Kind: "remote", ID: p.id}) {
			return Frame{Type: "error", Error: "remote peer does not control this terminal"}
		}
		var size struct {
			Width, Height int
		}
		if err := json.Unmarshal(frame.Payload, &size); err != nil {
			return Frame{Type: "error", Error: err.Error()}
		}
		return response("resized", nil, session.Resize(size.Width, size.Height))
	case "metrics":
		var request struct {
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(frame.Payload, &request)
		value, err := p.backend.RemoteMetrics(ctx, request.SessionID)
		return response("metrics", value, err)
	case "logs":
		var request struct {
			SessionID string `json:"session_id"`
			Limit     int    `json:"limit"`
		}
		_ = json.Unmarshal(frame.Payload, &request)
		value, err := p.backend.RemoteLogs(ctx, request.SessionID, request.Limit)
		return response("logs", value, err)
	case "checkpoint":
		var request struct {
			SessionID string `json:"session_id"`
			Name      string `json:"name"`
		}
		if err := json.Unmarshal(frame.Payload, &request); err != nil {
			return Frame{Type: "error", Error: err.Error()}
		}
		value, err := p.backend.RemoteCheckpoint(ctx, request.SessionID, request.Name)
		return response("checkpoint_created", value, err)
	case "run_queue":
		var request struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(frame.Payload, &request); err != nil {
			return Frame{Type: "error", Error: err.Error()}
		}
		return response("queue_started", nil, p.backend.RemoteRunQueue(ctx, request.SessionID))
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
			session, ok := p.backend.RemoteTerminal(p.observed)
			if !ok {
				continue
			}
			snapshot := session.Snapshot()
			if snapshot == previous {
				continue
			}
			previous = snapshot
			p.sequence++
			if err := p.write(Frame{
				Type: "terminal_snapshot", InstanceID: p.observed,
				Sequence: p.sequence, Payload: payload(map[string]any{
					"screen": snapshot, "state": session.State(),
					"owner": session.Owner(), "alternate_screen": session.IsAltScreen(),
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
	if session, ok := p.backend.RemoteTerminal(p.observed); ok {
		owner := terminal.Owner{Kind: "remote", ID: p.id}
		if session.Owner().Equal(owner) {
			_ = session.Release(owner)
		}
	}
}

func (h *Host) Close() error {
	h.cancel()
	err := h.listener.Close()
	h.wg.Wait()
	return err
}
