package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os/exec"
	"sync"
	"time"
)

// Manager owns one lazily-started, long-lived whisper-server.exe process:
// starting it once (rather than reloading the model for every dictation)
// keeps repeated transcriptions fast, matching how the user already runs
// their own local Ollama/llama.cpp servers.
type Manager struct {
	toolsRoot string

	mu        sync.Mutex
	cmd       *exec.Cmd
	baseURL   string
	starting  bool
	installed Installed
}

// NewManager returns a Manager that hasn't downloaded or started anything
// yet — that only happens on the first Ensure call.
func NewManager(toolsRoot string) *Manager {
	return &Manager{toolsRoot: toolsRoot}
}

// Ensure installs whisper.cpp if needed and starts its server if not already
// running. Safe to call every time dictation is invoked — it's a no-op once
// the server is up.
func (m *Manager) Ensure(ctx context.Context) error {
	return m.EnsureWithProgress(ctx, nil)
}

// EnsureWithProgress is Ensure with optional, UI-friendly progress updates
// for the one-time local tools and model download.
func (m *Manager) EnsureWithProgress(ctx context.Context, report ProgressReporter) error {
	m.mu.Lock()
	if m.cmd != nil {
		m.mu.Unlock()
		return nil
	}
	if m.starting {
		m.mu.Unlock()
		return fmt.Errorf("voice transcription is still starting up")
	}
	m.starting = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.starting = false
		m.mu.Unlock()
	}()

	reportProgress(report, Progress{Stage: "Checking local Whisper files"})
	installed, err := EnsureInstalled(ctx, m.toolsRoot, report)
	if err != nil {
		return err
	}

	port, err := freePort()
	if err != nil {
		return fmt.Errorf("find a free port for whisper-server: %w", err)
	}

	reportProgress(report, Progress{Stage: "Starting local Whisper server"})
	cmd := exec.Command(installed.ServerExe,
		"--host", "127.0.0.1",
		"--port", fmt.Sprintf("%d", port),
		"--model", installed.ModelPath,
		"--language", "auto",
		"--no-timestamps",
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start whisper-server: %w", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := waitForServer(ctx, baseURL, 30*time.Second); err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("whisper-server didn't come up: %w (output: %s)", err, output.String())
	}

	m.mu.Lock()
	m.cmd = cmd
	m.baseURL = baseURL
	m.installed = installed
	m.mu.Unlock()
	reportProgress(report, Progress{Stage: "Voice transcription ready"})
	return nil
}

// RecorderExe returns the path to the platform's native recording helper
// resolved by the last successful Ensure call (empty until then, and always
// empty on platforms that record in-process instead — e.g. Windows/WASAPI).
func (m *Manager) RecorderExe() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.installed.RecorderExe
}

// Transcribe posts a WAV recording to the running server and returns its
// best-effort transcript. Call Ensure first.
func (m *Manager) Transcribe(ctx context.Context, wav []byte) (string, error) {
	m.mu.Lock()
	baseURL := m.baseURL
	m.mu.Unlock()
	if baseURL == "" {
		return "", fmt.Errorf("voice transcription server isn't running")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", err
	}
	if _, err := part.Write(wav); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/inference", &body)
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("call whisper-server: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return "", fmt.Errorf("whisper-server returned %s: %s", response.Status, payload)
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode whisper-server response: %w", err)
	}
	return result.Text, nil
}

// Close stops the server process, if one is running. Safe to call even if
// Ensure was never called.
func (m *Manager) Close() error {
	m.mu.Lock()
	cmd := m.cmd
	m.cmd = nil
	m.baseURL = ""
	m.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// waitForServer polls until whisper-server's HTTP port accepts a connection
// (model loading is the slow part before it starts listening).
func waitForServer(ctx context.Context, baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/", nil)
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
