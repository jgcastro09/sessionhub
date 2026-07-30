package remote

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"sync"
	"time"
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
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.nextID++
	if frame.ID == "" {
		frame.ID = fmt.Sprintf("request-%d", c.nextID)
	}
	return WriteFrame(c.conn, frame)
}

func (c *Client) Receive() (Frame, error) { return ReadFrame(c.reader) }
func (c *Client) Close() error            { return c.conn.Close() }
