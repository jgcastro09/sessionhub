package remote

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/jgcastro09/sessionhub/internal/domain"
)

type Client struct {
	conn    net.Conn
	reader  *bufio.Reader
	writeMu sync.Mutex
	nextID  uint64
}

func Connect(ctx context.Context, address string) (*Client, Frame, error) {
	validated, err := ValidateRemoteAddress(address)
	if err != nil {
		return nil, Frame{}, err
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", validated.String())
	if err != nil {
		return nil, Frame{}, fmt.Errorf("connect to Session Hub host: %w", err)
	}
	client := &Client{conn: connection, reader: bufio.NewReader(connection)}
	hello, err := ReadFrame(client.reader)
	if err != nil {
		_ = connection.Close()
		return nil, Frame{}, fmt.Errorf("read Session Hub handshake: %w", err)
	}
	if hello.Type != "hello" {
		_ = connection.Close()
		return nil, Frame{}, fmt.Errorf("unexpected Session Hub handshake %q", hello.Type)
	}
	return client, hello, nil
}

func (c *Client) Send(frame Frame) error {
	_, err := c.SendRequest(frame)
	return err
}

func (c *Client) SendRequest(frame Frame) (string, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.nextID++
	if frame.ID == "" {
		frame.ID = fmt.Sprintf("request-%d", c.nextID)
	}
	return frame.ID, WriteFrame(c.conn, frame)
}

func (c *Client) Receive() (Frame, error) { return ReadFrame(c.reader) }
func (c *Client) Close() error            { return c.conn.Close() }

// Controller serializes request/response frames while terminal snapshots
// arrive asynchronously. It is the client-side counterpart to one active
// Remote Mode connection (one controller and one controlled SessionHub).
type Controller struct {
	client  *Client
	device  Device
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.RWMutex
	pending map[string]chan Frame
	screens map[string]string
	states  map[string]domain.State
	err     error
	wg      sync.WaitGroup
}

func ConnectController(ctx context.Context, device Device, controllerName, controllerVersion string) (*Controller, error) {
	client, hello, err := Connect(ctx, device.Endpoint())
	if err != nil {
		return nil, err
	}
	if hello.Type == "busy" {
		_ = client.Close()
		return nil, errors.New(hello.Error)
	}
	// ctx limits dialing and the initial handshake only. A controller is a
	// live terminal stream and must remain connected after that short setup
	// deadline expires; it is closed explicitly when the user returns local.
	controllerCtx, cancel := context.WithCancel(context.Background())
	c := &Controller{client: client, device: device, ctx: controllerCtx, cancel: cancel, pending: make(map[string]chan Frame), screens: make(map[string]string), states: make(map[string]domain.State)}
	if len(hello.Payload) > 0 {
		var info struct {
			Host Device `json:"host"`
		}
		if json.Unmarshal(hello.Payload, &info) == nil && info.Host.Name != "" {
			c.device.Name, c.device.Version = info.Host.Name, info.Host.Version
		}
	}
	if !SameVersion(controllerVersion, c.device.Version) {
		_ = client.Close()
		cancel()
		return nil, fmt.Errorf("remote SessionHub version mismatch: this device is v%s, %s is v%s", controllerVersion, c.device.Name, c.device.Version)
	}
	c.wg.Add(1)
	go c.readLoop()
	if _, err := c.request(ctx, Frame{Type: "identify", Payload: payload(map[string]string{"name": controllerName, "version": controllerVersion})}); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Controller) Device() Device { return c.device }

func (c *Controller) readLoop() {
	defer c.wg.Done()
	for {
		frame, err := c.client.Receive()
		if err != nil {
			c.mu.Lock()
			if c.err == nil && c.ctx.Err() == nil {
				c.err = err
			}
			for id, waiter := range c.pending {
				delete(c.pending, id)
				close(waiter)
			}
			c.mu.Unlock()
			return
		}
		if frame.Type == "terminal_snapshot" {
			var snapshot struct {
				Screen string       `json:"screen"`
				State  domain.State `json:"state"`
			}
			if json.Unmarshal(frame.Payload, &snapshot) == nil {
				c.mu.Lock()
				c.screens[frame.InstanceID] = snapshot.Screen
				c.states[frame.InstanceID] = snapshot.State
				c.mu.Unlock()
			}
			continue
		}
		c.mu.Lock()
		waiter := c.pending[frame.ID]
		if waiter != nil {
			delete(c.pending, frame.ID)
		}
		c.mu.Unlock()
		if waiter != nil {
			waiter <- frame
			close(waiter)
		}
	}
}

func (c *Controller) request(ctx context.Context, frame Frame) (Frame, error) {
	if c == nil {
		return Frame{}, errors.New("remote controller is not connected")
	}
	waiter := make(chan Frame, 1)
	// Client allocates the request ID under its writer lock. Give the frame a
	// stable ID here so the waiter is registered before bytes leave the socket.
	c.client.writeMu.Lock()
	c.client.nextID++
	frame.ID = fmt.Sprintf("request-%d", c.client.nextID)
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		c.client.writeMu.Unlock()
		return Frame{}, err
	}
	c.pending[frame.ID] = waiter
	c.mu.Unlock()
	err := WriteFrame(c.client.conn, frame)
	c.client.writeMu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, frame.ID)
		c.mu.Unlock()
		return Frame{}, err
	}
	select {
	case response, ok := <-waiter:
		if !ok {
			c.mu.RLock()
			err := c.err
			c.mu.RUnlock()
			if err == nil {
				err = errors.New("remote connection closed")
			}
			return Frame{}, err
		}
		if response.Type == "error" || response.Type == "control_denied" || response.Type == "busy" {
			return response, errors.New(response.Error)
		}
		return response, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, frame.ID)
		c.mu.Unlock()
		return Frame{}, ctx.Err()
	case <-c.ctx.Done():
		return Frame{}, errors.New("remote connection closed")
	}
}

func (c *Controller) Sessions(ctx context.Context) ([]domain.Session, error) {
	frame, err := c.request(ctx, Frame{Type: "list_sessions"})
	if err != nil {
		return nil, err
	}
	var value []domain.Session
	return value, json.Unmarshal(frame.Payload, &value)
}

func (c *Controller) Executors(ctx context.Context) ([]domain.ExecutorConfig, error) {
	frame, err := c.request(ctx, Frame{Type: "list_executors"})
	if err != nil {
		return nil, err
	}
	var value []domain.ExecutorConfig
	return value, json.Unmarshal(frame.Payload, &value)
}

func (c *Controller) ExecutorStatuses(ctx context.Context) ([]ExecutorStatus, error) {
	frame, err := c.request(ctx, Frame{Type: "executor_status"})
	if err != nil {
		return nil, err
	}
	var value []ExecutorStatus
	return value, json.Unmarshal(frame.Payload, &value)
}

func (c *Controller) StartTerminal(ctx context.Context, sessionID, executorID string, width, height int) (domain.Instance, error) {
	frame, err := c.request(ctx, Frame{Type: "start_terminal", Payload: payload(map[string]any{"session_id": sessionID, "executor_id": executorID, "width": width, "height": height})})
	if err != nil {
		return domain.Instance{}, err
	}
	var instance domain.Instance
	if err := json.Unmarshal(frame.Payload, &instance); err != nil {
		return domain.Instance{}, err
	}
	if _, err := c.request(ctx, Frame{Type: "request_control", InstanceID: instance.ID}); err != nil {
		return domain.Instance{}, err
	}
	return instance, nil
}

func (c *Controller) SendKey(ctx context.Context, instanceID string, key uv.Key, release bool) error {
	_, err := c.request(ctx, Frame{Type: "terminal_key", InstanceID: instanceID, Payload: payload(map[string]any{"key": key, "release": release})})
	return err
}

func (c *Controller) Paste(ctx context.Context, instanceID, text string) error {
	_, err := c.request(ctx, Frame{Type: "terminal_paste", InstanceID: instanceID, Payload: payload(map[string]string{"text": text})})
	return err
}

func (c *Controller) Resize(ctx context.Context, instanceID string, width, height int) error {
	_, err := c.request(ctx, Frame{Type: "resize", InstanceID: instanceID, Payload: payload(map[string]int{"Width": width, "Height": height})})
	return err
}

func (c *Controller) Release(ctx context.Context, instanceID string) error {
	_, err := c.request(ctx, Frame{Type: "release_control", InstanceID: instanceID})
	return err
}

// Navigate mirrors the controller's current UI selection on the controlled
// SessionHub. Terminal content still streams independently.
func (c *Controller) Navigate(ctx context.Context, view ViewState) error {
	_, err := c.request(ctx, Frame{Type: "ui_navigation", Payload: payload(view)})
	return err
}

func (c *Controller) Snapshot(instanceID string) (string, domain.State) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.screens[instanceID], c.states[instanceID]
}

func (c *Controller) Err() error { c.mu.RLock(); defer c.mu.RUnlock(); return c.err }

func (c *Controller) Close() error {
	if c == nil {
		return nil
	}
	c.cancel()
	err := c.client.Close()
	c.wg.Wait()
	return err
}
