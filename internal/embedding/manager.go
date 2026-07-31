package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/jgcastro09/sessionhub/internal/procserver"
)

// Manager owns one lazily-started, long-lived llama-server process running
// in --embedding mode, bound to loopback only — never reachable from the
// network, and never a dependency the rest of SessionHub needs running
// (search always has the lexical fallback). This mirrors internal/voice's
// Manager for whisper-server exactly: same install-on-first-use, same
// keep-it-running-once-started shape.
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

// Ensure installs the embedding engine if needed and starts its server if
// not already running. Safe to call before every embedding request — it's a
// no-op once the server is up.
func (m *Manager) Ensure(ctx context.Context) error {
	return m.EnsureWithProgress(ctx, nil)
}

// EnsureWithProgress is Ensure with optional, UI-friendly progress updates
// for the one-time local engine and model download.
func (m *Manager) EnsureWithProgress(ctx context.Context, report ProgressReporter) error {
	m.mu.Lock()
	if m.cmd != nil {
		m.mu.Unlock()
		return nil
	}
	if m.starting {
		m.mu.Unlock()
		return fmt.Errorf("semantic search engine is still starting up")
	}
	m.starting = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.starting = false
		m.mu.Unlock()
	}()

	reportProgress(report, Progress{Stage: "Checking local embedding engine"})
	installed, err := EnsureInstalled(ctx, m.toolsRoot, report)
	if err != nil {
		return err
	}

	port, err := procserver.FreePort()
	if err != nil {
		return fmt.Errorf("find a free port for llama-server: %w", err)
	}

	reportProgress(report, Progress{Stage: "Starting local embedding engine"})
	cmd := exec.Command(installed.ServerExe,
		"--host", "127.0.0.1",
		"--port", fmt.Sprintf("%d", port),
		"--model", installed.ModelPath,
		"--embedding",
		"--pooling", "mean",
		"--ctx-size", "512",
		"--no-warmup",
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start llama-server: %w", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := procserver.WaitForHTTP(ctx, baseURL, "/health", 30*time.Second); err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("llama-server didn't come up: %w (output: %s)", err, output.String())
	}

	m.mu.Lock()
	m.cmd = cmd
	m.baseURL = baseURL
	m.installed = installed
	m.mu.Unlock()
	reportProgress(report, Progress{Stage: "Semantic search ready"})
	return nil
}

type embeddingRequest struct {
	Input string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed returns the vector for text. Call Ensure first; Embed itself never
// installs or starts anything, so a caller that wants to keep the engine
// warm across many calls (indexing a whole Registry) only pays the startup
// cost once.
func (m *Manager) Embed(ctx context.Context, text string) ([]float32, error) {
	m.mu.Lock()
	baseURL := m.baseURL
	m.mu.Unlock()
	if baseURL == "" {
		return nil, fmt.Errorf("semantic search engine isn't running")
	}

	body, err := json.Marshal(embeddingRequest{Input: text})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call llama-server: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llama-server returned %s", response.Status)
	}

	var result embeddingResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode llama-server response: %w", err)
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("llama-server returned no embedding")
	}
	return result.Data[0].Embedding, nil
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
