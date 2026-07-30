package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	pty "github.com/aymanbagabas/go-pty"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/jgcastro09/sessionhub/internal/domain"
)

var (
	ErrControlBusy = errors.New("terminal control is owned by another actor")
	ErrNotRunning  = errors.New("terminal is not running")
)

type HistorySink interface {
	AppendTerminal(context.Context, string, string, []byte) error
}

type EventKind string

const (
	EventOutput EventKind = "output"
	EventState  EventKind = "state"
	EventError  EventKind = "error"
)

type Event struct {
	InstanceID string
	Kind       EventKind
	State      domain.State
	Err        error
	Data       []byte
	ExitCode   *int
}

type Owner struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func (o Owner) Equal(other Owner) bool { return o.Kind == other.Kind && o.ID == other.ID }
func (o Owner) Empty() bool            { return o.Kind == "" && o.ID == "" }

type historyRecord struct {
	direction string
	data      []byte
}

type Session struct {
	id            string
	cfg           domain.ExecutorConfig
	ctx           context.Context
	cancel        context.CancelFunc
	pty           pty.Pty
	cmd           *pty.Cmd
	emulator      *vt.SafeEmulator
	sink          HistorySink
	events        chan<- Event
	historyMu     sync.Mutex
	historyQueue  []historyRecord
	historyWake   chan struct{}
	historyClosed bool

	mu        sync.RWMutex
	state     domain.State
	owner     Owner
	width     int
	height    int
	exitCode  *int
	lastErr   error
	waitDone  chan struct{}
	closeOnce sync.Once
	ioWG      sync.WaitGroup
	historyWG sync.WaitGroup
}

func Start(
	parent context.Context,
	instanceID string,
	cfg domain.ExecutorConfig,
	width, height, scrollback int,
	sink HistorySink,
	events chan<- Event,
) (*Session, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if width < 2 || height < 2 {
		return nil, fmt.Errorf("terminal dimensions must be at least 2x2")
	}
	if cfg.WorkingDir != "" {
		info, err := os.Stat(cfg.WorkingDir)
		if err != nil {
			return nil, fmt.Errorf("open executor working directory %q: %w", cfg.WorkingDir, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("executor working directory %q is not a directory", cfg.WorkingDir)
		}
	}
	command, args := commandLine(cfg)
	resolvedCommand, err := exec.LookPath(command)
	if err != nil {
		return nil, fmt.Errorf("find executor command %q: %w", command, err)
	}
	command = resolvedCommand
	p, err := pty.New()
	if err != nil {
		return nil, fmt.Errorf("create pseudoterminal: %w", err)
	}
	if err := p.Resize(width, height); err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("size pseudoterminal to %dx%d: %w", width, height, err)
	}
	ctx, cancel := context.WithCancel(parent)
	// Process lifetime is owned explicitly by Session.Close. Using the PTY
	// library's CommandContext starts a second waiter on Windows ConPTY, which
	// races its public Wait path. A single owner also makes graceful interrupt,
	// forced termination, and persisted final state deterministic.
	cmd := p.Command(command, args...)
	cmd.Dir = cfg.WorkingDir
	// Every executor installed through SessionHub (cfg.InstallDir set) runs
	// with HOME/USERPROFILE redirected to its own config/ folder. Most CLIs
	// resolve their login/session state relative to the home directory
	// (some, like Codex, also expose a dedicated var such as CODEX_HOME —
	// set that too, in the executor's own Environment field, for extra
	// precision); this is the one mechanism that isolates login state for
	// any CLI regardless of whether it has a dedicated override variable.
	// Manually-registered executors (no InstallDir) are left alone, since
	// those deliberately point at something already installed globally.
	isolatedHome := ""
	if cfg.InstallDir != "" {
		isolatedHome = filepath.Join(cfg.InstallDir, "config")
		if err := os.MkdirAll(isolatedHome, 0o700); err != nil {
			cancel()
			_ = p.Close()
			return nil, fmt.Errorf("prepare isolated config dir for %q: %w", cfg.Name, err)
		}
	}
	cmd.Env = mergedEnvironment(cfg.Environment, isolatedHome)

	if err := cmd.Start(); err != nil {
		cancel()
		_ = p.Close()
		return nil, fmt.Errorf("start executor %q in pseudoterminal: %w", cfg.Name, err)
	}

	emu := vt.NewSafeEmulator(width, height)
	emu.SetScrollbackSize(scrollback)
	s := &Session{
		id: instanceID, cfg: cfg, ctx: ctx, cancel: cancel, pty: p, cmd: cmd,
		emulator: emu, sink: sink, events: events,
		historyWake: make(chan struct{}, 1),
		state:       domain.StateRunning, owner: Owner{Kind: "local", ID: "operator"},
		width: width, height: height, waitDone: make(chan struct{}),
	}
	s.historyWG.Add(1)
	go func() {
		defer s.historyWG.Done()
		s.persistHistory()
	}()
	s.ioWG.Add(2)
	go func() {
		defer s.ioWG.Done()
		s.readOutput()
	}()
	go func() {
		defer s.ioWG.Done()
		s.copyTerminalReplies()
	}()
	go s.wait()
	s.emit(Event{InstanceID: instanceID, Kind: EventState, State: domain.StateRunning})
	return s, nil
}

func commandLine(cfg domain.ExecutorConfig) (string, []string) {
	if strings.TrimSpace(cfg.Shell) == "" {
		return cfg.Command, append([]string(nil), cfg.Args...)
	}
	line := quoteCommand(cfg.Command, cfg.Args)
	base := strings.ToLower(filepath.Base(cfg.Shell))
	switch base {
	case "cmd", "cmd.exe":
		return cfg.Shell, []string{"/d", "/s", "/c", line}
	case "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		return cfg.Shell, []string{"-NoLogo", "-NoProfile", "-Command", line}
	default:
		return cfg.Shell, []string{"-lc", line}
	}
}

func quoteCommand(command string, args []string) string {
	all := append([]string{command}, args...)
	for i, value := range all {
		if runtime.GOOS == "windows" {
			all[i] = `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
		} else {
			all[i] = "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
		}
	}
	return strings.Join(all, " ")
}

// mergedEnvironment builds the child process's environment: the OS
// environment, then (if isolatedHome is set) HOME/USERPROFILE redirected to
// the executor's own isolated config folder, then the executor's explicit
// Environment entries — each layer able to override the one before it.
func mergedEnvironment(configured []domain.SecretEnv, isolatedHome string) []string {
	values := make(map[string]string, len(os.Environ())+len(configured)+2)
	keys := make(map[string]string, len(values))
	for _, item := range os.Environ() {
		name, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		canonical := name
		if runtime.GOOS == "windows" {
			canonical = strings.ToUpper(name)
		}
		keys[canonical] = name
		values[canonical] = value
	}
	if isolatedHome != "" {
		for _, name := range []string{"HOME", "USERPROFILE"} {
			keys[name] = name
			values[name] = isolatedHome
		}
	}
	for _, item := range configured {
		canonical := item.Name
		if runtime.GOOS == "windows" {
			canonical = strings.ToUpper(item.Name)
		}
		keys[canonical] = item.Name
		values[canonical] = item.Value
	}
	if _, ok := values["TERM"]; !ok {
		keys["TERM"] = "TERM"
		values["TERM"] = "xterm-256color"
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, canonical := range names {
		out = append(out, keys[canonical]+"="+values[canonical])
	}
	return out
}

func (s *Session) persistHistory() {
	for {
		s.historyMu.Lock()
		batch := s.historyQueue
		s.historyQueue = nil
		closed := s.historyClosed
		s.historyMu.Unlock()
		for _, record := range batch {
			if s.sink == nil {
				continue
			}
			if err := s.sink.AppendTerminal(context.Background(), s.id, record.direction, record.data); err != nil {
				s.emit(Event{InstanceID: s.id, Kind: EventError, Err: err})
			}
		}
		if closed && len(batch) == 0 {
			return
		}
		<-s.historyWake
	}
}

func (s *Session) record(direction string, data []byte) {
	copyData := append([]byte(nil), data...)
	s.historyMu.Lock()
	if s.historyClosed {
		s.historyMu.Unlock()
		return
	}
	s.historyQueue = append(s.historyQueue, historyRecord{direction: direction, data: copyData})
	s.historyMu.Unlock()
	select {
	case s.historyWake <- struct{}{}:
	default:
	}
}

func (s *Session) readOutput() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			_, _ = s.emulator.Write(chunk)
			s.record("output", chunk)
			s.emit(Event{InstanceID: s.id, Kind: EventOutput, Data: chunk})
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) && s.ctx.Err() == nil {
				s.emit(Event{InstanceID: s.id, Kind: EventError, Err: fmt.Errorf("read PTY output: %w", err)})
			}
			return
		}
	}
}

func (s *Session) copyTerminalReplies() {
	_, err := io.Copy(s.pty, s.emulator)
	if err != nil && !errors.Is(err, os.ErrClosed) && s.ctx.Err() == nil {
		s.emit(Event{InstanceID: s.id, Kind: EventError, Err: fmt.Errorf("forward terminal input: %w", err)})
	}
}

func (s *Session) wait() {
	err := s.cmd.Wait()
	s.mu.Lock()
	nowState := domain.StateFinished
	if err != nil && s.ctx.Err() == nil {
		nowState = domain.StateError
		s.lastErr = err
	}
	if s.cmd.ProcessState != nil {
		code := s.cmd.ProcessState.ExitCode()
		s.exitCode = &code
		if code != 0 && s.ctx.Err() == nil {
			nowState = domain.StateError
		}
	}
	if s.ctx.Err() != nil {
		nowState = domain.StateInterrupted
	}
	s.state = nowState
	s.mu.Unlock()
	s.emit(Event{InstanceID: s.id, Kind: EventState, State: nowState, Err: err, ExitCode: s.exitCode})
	close(s.waitDone)
}

func (s *Session) emit(event Event) {
	if s.events == nil {
		return
	}
	select {
	case s.events <- event:
	default:
	}
}

func (s *Session) ID() string { return s.id }

func (s *Session) State() domain.State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *Session) Owner() Owner {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.owner
}

func (s *Session) Acquire(owner Owner) error {
	if owner.Empty() {
		return fmt.Errorf("control owner is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.owner.Empty() && !s.owner.Equal(owner) {
		return fmt.Errorf("%w: %s/%s", ErrControlBusy, s.owner.Kind, s.owner.ID)
	}
	s.owner = owner
	return nil
}

func (s *Session) Transfer(from, to Owner) error {
	if to.Empty() {
		return fmt.Errorf("new control owner is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.owner.Equal(from) {
		return fmt.Errorf("%w: %s/%s", ErrControlBusy, s.owner.Kind, s.owner.ID)
	}
	s.owner = to
	return nil
}

func (s *Session) Release(owner Owner) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.owner.Equal(owner) {
		return fmt.Errorf("%w: %s/%s", ErrControlBusy, s.owner.Kind, s.owner.ID)
	}
	s.owner = Owner{}
	return nil
}

func (s *Session) checkWrite(owner Owner) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state != domain.StateRunning && s.state != domain.StateWaiting {
		return ErrNotRunning
	}
	if !s.owner.Equal(owner) {
		return fmt.Errorf("%w: %s/%s", ErrControlBusy, s.owner.Kind, s.owner.ID)
	}
	return nil
}

func (s *Session) SendKey(owner Owner, key uv.KeyEvent) error {
	if err := s.checkWrite(owner); err != nil {
		return err
	}
	s.emulator.SendKey(key)
	return nil
}

func (s *Session) SendMouse(owner Owner, mouse uv.MouseEvent) error {
	if err := s.checkWrite(owner); err != nil {
		return err
	}
	s.emulator.SendMouse(mouse)
	return nil
}

func (s *Session) Paste(owner Owner, text string) error {
	if err := s.checkWrite(owner); err != nil {
		return err
	}
	s.emulator.Paste(text)
	s.record("input", []byte(text))
	return nil
}

func (s *Session) Write(owner Owner, data []byte) error {
	if err := s.checkWrite(owner); err != nil {
		return err
	}
	if _, err := s.pty.Write(data); err != nil {
		return fmt.Errorf("write terminal input: %w", err)
	}
	s.record("input", data)
	return nil
}

func (s *Session) SendPrompt(owner Owner, prompt string) error {
	if err := s.checkWrite(owner); err != nil {
		return err
	}
	text := prompt + s.cfg.PromptSuffix
	s.emulator.SendText(text)
	s.record("input", []byte(text))
	return nil
}

func (s *Session) Resize(width, height int) error {
	if width < 2 || height < 2 {
		return nil
	}
	s.mu.Lock()
	s.width, s.height = width, height
	s.mu.Unlock()
	s.emulator.Resize(width, height)
	if err := s.pty.Resize(width, height); err != nil {
		return fmt.Errorf("resize PTY to %dx%d: %w", width, height, err)
	}
	return nil
}

func (s *Session) Snapshot() string   { return s.emulator.Render() }
func (s *Session) IsAltScreen() bool  { return s.emulator.IsAltScreen() }
func (s *Session) ScrollbackLen() int { return s.emulator.ScrollbackLen() }

// SnapshotScrolled renders a height-row window ending offset lines back
// from the live tail. offset<=0 is identical to Snapshot(); the caller is
// expected to clamp offset to [0, ScrollbackLen()]. Lines below the
// scrollback boundary come from the live screen, so the result always
// tails into whatever's currently on screen once scrolled back far enough.
func (s *Session) SnapshotScrolled(offset, height int) string {
	if offset <= 0 {
		return s.emulator.Render()
	}
	screenHeight := s.emulator.Height()
	if height <= 0 {
		height = screenHeight
	}
	sb := s.emulator.Scrollback()
	sbLen := sb.Len()
	total := sbLen + screenHeight
	end := total - offset
	if end > total {
		end = total
	}
	if end < 0 {
		end = 0
	}
	start := end - height
	if start < 0 {
		start = 0
	}
	lines := make([]string, 0, end-start)
	sbEnd := end
	if sbEnd > sbLen {
		sbEnd = sbLen
	}
	for i := start; i < sbEnd; i++ {
		lines = append(lines, sb.Line(i).Render())
	}
	if end > sbLen {
		screenLines := strings.Split(s.emulator.Render(), "\n")
		screenStart := start - sbLen
		if screenStart < 0 {
			screenStart = 0
		}
		screenEnd := end - sbLen
		for i := screenStart; i < screenEnd && i < len(screenLines); i++ {
			lines = append(lines, screenLines[i])
		}
	}
	return strings.Join(lines, "\n")
}

func (s *Session) Close(grace time.Duration) error {
	var closeErr error
	s.closeOnce.Do(func() {
		_, _ = s.pty.Write([]byte{0x03})
		s.cancel()
		select {
		case <-s.waitDone:
		case <-time.After(grace):
			if s.cmd.Process != nil {
				_ = s.cmd.Process.Kill()
			}
			<-s.waitDone
		}
		if err := s.pty.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			closeErr = errors.Join(closeErr, err)
		}
		// The VT input bridge can be blocked reading an encoded key or terminal
		// reply. Once the PTY is closed, one sentinel wakes that read; its write
		// to the closed PTY fails and the bridge exits. Waiting for both I/O
		// bridges before Emulator.Close avoids racing the upstream pipe fields.
		s.emulator.SendText("\x00")
		s.ioWG.Wait()
		if err := s.emulator.Close(); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
		s.historyMu.Lock()
		s.historyClosed = true
		s.historyMu.Unlock()
		select {
		case s.historyWake <- struct{}{}:
		default:
		}
		s.historyWG.Wait()
	})
	return closeErr
}
