// Package procserver holds the small "run a local HTTP server as a child
// process on loopback" primitives shared by every SessionHub feature that
// wraps a self-installed local tool this way (internal/voice's
// whisper-server, internal/embedding's llama-server): find a free port, and
// poll until the process is actually accepting connections before using it.
package procserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// FreePort asks the OS for an unused TCP port on loopback.
func FreePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// WaitForHTTP polls path (typically "/" or "/health") until it accepts a
// connection or timeout elapses. Model loading is usually the slow part
// before a server starts listening at all.
func WaitForHTTP(ctx context.Context, baseURL, path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
		if err == nil {
			if response, err := http.DefaultClient.Do(request); err == nil {
				response.Body.Close()
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("timed out after %s", timeout)
}
