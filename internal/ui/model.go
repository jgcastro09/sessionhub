package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/jgcastro09/sessionhub/internal/app"
	"github.com/jgcastro09/sessionhub/internal/automation"
	"github.com/jgcastro09/sessionhub/internal/domain"
	"github.com/jgcastro09/sessionhub/internal/executor"
	"github.com/jgcastro09/sessionhub/internal/gitstate"
	"github.com/jgcastro09/sessionhub/internal/id"
	"github.com/jgcastro09/sessionhub/internal/terminal"
	updatecheck "github.com/jgcastro09/sessionhub/internal/update"
)

var sections = []string{
	"Sessions", "Executors", "Queues", "Pipelines", "Automations",
	"Metrics", "Logs", "Remote", "Settings",
}

type dataMsg struct {
	sessions  []domain.Session
	executors []domain.ExecutorConfig
	instances []domain.Instance
	metrics   domain.Metric
	git       *gitstate.State
	err       error
}

type savedMsg struct {
	kind    string
	id      string
	deleted bool
	err     error
}

type startedMsg struct {
	session  *terminal.Session
	instance domain.Instance
	err      error
	// registerAfter carries the Executor config to save once this terminal
	// (an install run) reaches a terminal state, so "install a CLI" can
	// auto-register without a separate manual step.
	registerAfter *domain.ExecutorConfig
	// installDirs/installLine accompany registerAfter for an "add a CLI"
	// install run: once it finishes, Command is re-resolved against this
	// folder and a manifest.json is written there.
	installDirs *executor.InstallDirs
	installLine string
	// installExtraDirs adds a provider's own conventional install location
	// to the post-install lookup, for CLIs whose installer doesn't support
	// a custom directory (see executor.Provider.DefaultDirs).
	installExtraDirs []string
	// tabKey, when set, is the session|executor tab this instance backs, so
	// it can be recorded in Model.tabInstances for reattaching later.
	tabKey string
}

type tickMsg time.Time

// scannedMsg reports the result of scanning executors/ for CLIs installed
// before but missing from the database (e.g. after a reset), registering
// whichever ones are found and not already known.
type scannedMsg struct {
	found int
	added int
	err   error
}

type updateMsg struct {
	release updatecheck.Release
	newer   bool
	err     error
}

type formKind int

const (
	noForm formKind = iota
	sessionForm
	executorForm
	providerPickForm
	installForm
	queueForm
	remoteHostForm
	scheduleForm
	pipelineForm
	priceForm
)

type formModel struct {
	kind      formKind
	title     string
	labels    []string
	fields    []textinput.Model
	index     int
	err       string
	editingID string
	// executorChoices, when non-empty, adds one extra tab-stop after all
	// text fields: a checklist of registered Executors (sessionForm only).
	executorChoices []executorChoice
	choiceCursor    int
	// providerNames, when non-empty, means this form (kind ==
	// providerPickForm) is a single-select "pick a CLI" list; choiceCursor
	// indexes it. The last entry is always "Custom".
	providerNames []string
	// selectedProvider carries the catalog entry (if any) chosen before
	// this installForm was opened, so submitForm knows where an
	// unisolated provider's installer conventionally places its binary.
	selectedProvider *executor.Provider
}

type executorChoice struct {
	id       string
	name     string
	selected bool
}

// onChecklist reports whether the form's current tab-stop is the Executor
// checklist rather than one of the text fields.
func (f formModel) onChecklist() bool {
	return len(f.executorChoices) > 0 && f.index == len(f.fields)
}

// totalStops counts text fields plus the checklist (if this form has one).
func (f formModel) totalStops() int {
	if len(f.executorChoices) > 0 {
		return len(f.fields) + 1
	}
	return len(f.fields)
}

type Model struct {
	app            *app.App
	width          int
	height         int
	sidebar        bool
	focus          bool
	section        int
	selected       int
	sessions       []domain.Session
	executors      []domain.ExecutorConfig
	instances      []domain.Instance
	activeSession  int
	activeTerminal *terminal.Session
	activeInstance domain.Instance
	// scrollOffset is how many lines back into the active terminal's
	// scrollback the view currently shows (0 = live tail).
	scrollOffset    int
	metrics         domain.Metric
	git             *gitstate.State
	viewport        viewport.Model
	form            formModel
	confirm         *confirmRequest
	pendingRegister *pendingRegistration
	// tabInstances maps "sessionID|executorID" to the instanceID currently
	// backing that tab, so selecting the tab again reattaches to the same
	// running terminal instead of starting a duplicate.
	tabInstances map[string]string
	status       string
	lastErr      string

	// Terminal mouse selection state
	selecting    bool
	hasSelection bool
	selStartRow  int
	selStartCol  int
	selEndRow    int
	selEndCol    int

	// Toast popup notification state
	toastMessage string
	toastExpires time.Time
}

// pendingRegistration tracks an Executor config waiting to be saved once a
// specific install-run instance finishes successfully. It's scoped to
// instanceID (not just "whatever terminal is active") so switching to a
// different terminal before the install finishes can't cause it to fire
// against the wrong process.
type pendingRegistration struct {
	instanceID string
	cfg        domain.ExecutorConfig
	// installDirs/installLine, when set, mean cfg.Command still needs to be
	// re-resolved against the executor's own folder after install succeeds,
	// and a manifest.json written there.
	installDirs      *executor.InstallDirs
	installLine      string
	installExtraDirs []string
}

// confirmRequest gates a destructive action (deleting a session or
// executor) behind an explicit y/n prompt before it runs.
type confirmRequest struct {
	kind    string // "session" or "executor"
	id      string
	name    string
	message string
}

func New(application *app.App) Model {
	vp := viewport.New()
	return Model{
		app: application, sidebar: true, activeSession: -1,
		viewport:     vp,
		tabInstances: make(map[string]string),
		status:       "click or use keys • ctrl+g command mode • n new session • q quit",
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.reload(), tick())
}

// Redrawing the focused terminal every 50ms (20fps) could lag visibly
// behind fast/held arrow keys through a nested PTY, making navigation feel
// like it's a step behind (or "reversed", since you're seeing the previous
// position right as you register the next keypress). ~16ms (60fps) keeps
// the displayed cursor closer to real time.
func tick() tea.Cmd {
	return tea.Tick(16*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) reload() tea.Cmd {
	application := m.app
	sessionID := ""
	if m.activeSession >= 0 && m.activeSession < len(m.sessions) {
		sessionID = m.sessions[m.activeSession].ID
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		sessions, err := application.Store.ListSessions(ctx)
		if err != nil {
			return dataMsg{err: err}
		}
		executors, err := application.Store.ListExecutors(ctx)
		if err != nil {
			return dataMsg{err: err}
		}
		var instances []domain.Instance
		var metric domain.Metric
		var gitState *gitstate.State
		if sessionID == "" && len(sessions) > 0 {
			sessionID = sessions[0].ID
		}
		if sessionID != "" {
			instances, err = application.Store.ListInstances(ctx, sessionID)
			if err != nil {
				return dataMsg{err: err}
			}
			metric, _ = application.Store.AggregateMetrics(ctx, sessionID)
			for _, session := range sessions {
				if session.ID == sessionID {
					if state, gitErr := gitstate.Inspect(ctx, session.Workspace); gitErr == nil {
						gitState = &state
					}
					break
				}
			}
		}
		return dataMsg{
			sessions: sessions, executors: executors, instances: instances,
			metrics: metric, git: gitState,
		}
	}
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if m.confirm != nil {
		return m.updateConfirm(message)
	}
	if m.form.kind != noForm {
		return m.updateForm(message)
	}
	if click, ok := message.(tea.MouseClickMsg); ok {
		if handled, next, cmd := m.routeMouseClick(click); handled {
			return next, cmd
		}
	}
	if m.focus {
		if next, cmd, handled := m.updateTerminal(message); handled {
			return next, cmd
		}
	}
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
	case dataMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			return m, nil
		}
		currentID := ""
		if m.activeSession >= 0 && m.activeSession < len(m.sessions) {
			currentID = m.sessions[m.activeSession].ID
		}
		m.sessions, m.executors, m.instances = msg.sessions, msg.executors, msg.instances
		m.metrics, m.git = msg.metrics, msg.git
		m.activeSession = -1
		for i := range m.sessions {
			if currentID == "" || m.sessions[i].ID == currentID {
				m.activeSession = i
				break
			}
		}
		if m.tabInstances == nil {
			m.tabInstances = make(map[string]string)
		}
		for _, instance := range m.instances {
			if m.isInstanceLive(instance) {
				key := tabKeyFor(instance.SessionID, instance.ExecutorID)
				if _, exists := m.tabInstances[key]; !exists {
					m.tabInstances[key] = instance.ID
				}
			}
		}
	case savedMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			return m, nil
		}
		m.form = formModel{}
		if msg.deleted {
			m.status = msg.kind + " removed"
		} else {
			m.status = msg.kind + " saved"
		}
		return m, m.reload()
	case startedMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			m.status = fmt.Sprintf("Cannot start terminal: %s", msg.err.Error())
			m.toastMessage = fmt.Sprintf("Cannot start: %s", msg.err.Error())
			m.toastExpires = time.Now().Add(5 * time.Second)
			return m, nil
		}
		m.activeTerminal, m.activeInstance = msg.session, msg.instance
		m.focus = true
		m.scrollOffset = 0
		if msg.tabKey != "" {
			if m.tabInstances == nil {
				m.tabInstances = make(map[string]string)
			}
			m.tabInstances[msg.tabKey] = msg.instance.ID
		}
		if msg.registerAfter != nil {
			m.pendingRegister = &pendingRegistration{
				instanceID: msg.instance.ID, cfg: *msg.registerAfter,
				installDirs: msg.installDirs, installLine: msg.installLine,
				installExtraDirs: msg.installExtraDirs,
			}
			m.status = fmt.Sprintf("installing %q • esc / ctrl+g / ctrl+] returns to Hub (auto-registers when it finishes)", msg.registerAfter.Name)
		} else {
			m.status = "terminal focused • esc / ctrl+g / ctrl+] returns to Hub"
		}
		m.resize()
		return m, m.reload()
	case tickMsg:
		if m.activeTerminal != nil {
			_, height := m.terminalSize()
			m.viewport.SetContent(m.activeTerminal.SnapshotScrolled(m.scrollOffset, height))
			if cmd := m.checkPendingRegister(); cmd != nil {
				return m, tea.Batch(tick(), cmd)
			}
		}
		return m, tick()
	case updateMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
		} else if msg.newer {
			m.status = fmt.Sprintf("Update %s available • %s", msg.release.TagName, msg.release.HTMLURL)
		} else {
			m.status = "Session Hub is up to date"
		}
	case scannedMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			return m, nil
		}
		switch {
		case msg.found == 0:
			m.status = "scan found no CLIs installed in executors/"
		case msg.added == 0:
			m.status = fmt.Sprintf("scan found %d installed CLI(s), all already registered", msg.found)
		default:
			m.status = fmt.Sprintf("scan found %d installed CLI(s), registered %d new", msg.found, msg.added)
		}
		return m, m.reload()
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "ctrl+c":
			return m, tea.Quit
		case "ctrl+g", "ctrl+p":
			m.status = "Hub mode • tab sections • n new • enter activate"
		case "tab":
			m.section = (m.section + 1) % len(sections)
			m.selected = 0
			m.lastErr = ""
		case "shift+tab":
			m.section = (m.section - 1 + len(sections)) % len(sections)
			m.selected = 0
			m.lastErr = ""
		case "ctrl+b":
			m.sidebar = !m.sidebar
			m.resize()
		case "ctrl+f":
			m.sidebar = !m.sidebar
			m.resize()
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected+1 < m.sectionLength() {
				m.selected++
			}
		case "left":
			if m.activeSession > 0 {
				m.activeSession--
				return m, m.reload()
			}
		case "right":
			if m.activeSession+1 < len(m.sessions) {
				m.activeSession++
				return m, m.reload()
			}
		case "n":
			switch sections[m.section] {
			case "Sessions":
				m.form = newSessionForm(m.executors)
			case "Executors":
				m.form = newExecutorForm()
			case "Queues":
				m.form = newQueueForm(m.executors)
			case "Remote":
				m.form = newRemoteHostForm()
			case "Automations":
				m.form = newScheduleForm(m.executors)
			case "Pipelines":
				m.form = newPipelineForm()
			case "Metrics":
				m.form = newPriceForm()
			default:
				m.status = "Create and edit this item from its section action palette"
			}
		case "e":
			switch sections[m.section] {
			case "Executors":
				if m.selected < len(m.executors) {
					m.form = editExecutorForm(m.executors[m.selected])
				}
			case "Sessions":
				if m.selected < len(m.sessions) {
					m.form = editSessionForm(m.sessions[m.selected], m.executors)
				}
			}
		case "i":
			if sections[m.section] == "Executors" {
				m.form = newProviderPickForm()
			}
		case "s":
			if sections[m.section] == "Executors" {
				return m, m.scanInstalledExecutors()
			}
		case "d":
			switch sections[m.section] {
			case "Executors":
				if m.selected < len(m.executors) {
					cfg := m.executors[m.selected]
					m.confirm = &confirmRequest{
						kind: "executor", id: cfg.ID, name: cfg.Name,
						message: fmt.Sprintf("Delete Executor %q? This also clears its past run history (instances/queue items).", cfg.Name),
					}
				}
			case "Sessions":
				if m.selected < len(m.sessions) {
					session := m.sessions[m.selected]
					m.confirm = &confirmRequest{
						kind: "session", id: session.ID, name: session.Name,
						message: fmt.Sprintf("Delete session %q? This also clears its instances, checkpoints, queues, pipelines and schedules. The workspace files are untouched.", session.Name),
					}
				}
			}
		case "x":
			if sections[m.section] == "Executors" && m.selected < len(m.executors) {
				cfg := m.executors[m.selected]
				if cfg.InstallDir == "" {
					m.status = fmt.Sprintf("%q wasn't installed through SessionHub — nothing isolated to reset", cfg.Name)
				} else {
					m.confirm = &confirmRequest{
						kind: "executor-reset", id: cfg.ID, name: cfg.Name,
						message: fmt.Sprintf("Hard reset %q? This wipes its login/config (account) — the installed CLI itself stays, no reinstall needed.", cfg.Name),
					}
				}
			}
		case "enter":
			return m.activateSelected()
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			if m.activeSession >= 0 && m.activeSession < len(m.sessions) {
				session := m.sessions[m.activeSession]
				executors := m.sessionExecutors(session)
				idx := int(msg.String()[0] - '1')
				if idx >= 0 && idx < len(executors) {
					return m.selectTab(executors[idx])
				}
			}
		default:
			if idx, ok := parseAltShortcut(msg.String()); ok {
				if m.activeSession >= 0 && m.activeSession < len(m.sessions) {
					session := m.sessions[m.activeSession]
					executors := m.sessionExecutors(session)
					if idx >= 0 && idx < len(executors) {
						return m.selectTab(executors[idx])
					}
				}
			}
		case "r":
			if m.activeTerminal != nil {
				owner := terminal.Owner{Kind: "local", ID: "operator"}
				if err := m.activeTerminal.Release(owner); err != nil {
					m.lastErr = err.Error()
				} else {
					m.status = "terminal control released • automation or remote may request it"
				}
			}
		case "c":
			if m.activeSession >= 0 {
				sessionID := m.sessions[m.activeSession].ID
				appContext := m.app.Context
				return m, func() tea.Msg {
					_, err := appContext.Checkpoint(context.Background(), sessionID, "Manual checkpoint", false)
					return savedMsg{kind: "checkpoint", err: err}
				}
			}
		case "u":
			switch sections[m.section] {
			case "Settings":
				checker := updatecheck.NewChecker("jgcastro09", "sessionhub")
				current := m.app.Version
				return m, func() tea.Msg {
					release, err := checker.Latest(context.Background())
					return updateMsg{release: release, newer: updatecheck.IsNewer(current, release.TagName), err: err}
				}
			case "Executors":
				if m.selected < len(m.executors) {
					return m, m.updateExecutorCLI(m.executors[m.selected])
				}
			}
		}
	}
	return m, nil
}

// routeMouseClick handles a click that lands in the sidebar or tab bar,
// which are drawn outside the terminal viewport and stay visible even while
// a terminal is focused. A click anywhere else (the content/terminal area)
// is left unhandled so it falls through to the focused terminal, if any.
func (m Model) routeMouseClick(msg tea.MouseClickMsg) (bool, tea.Model, tea.Cmd) {
	mouse := msg.Mouse()
	x, y := mouse.X, mouse.Y
	if m.activeSession >= 0 && y == 1 {
		if cfg, ok := m.tabAt(x, y); ok {
			next, cmd := m.selectTab(cfg)
			return true, next, cmd
		}
		return true, m, nil
	}
	if m.sidebar && x < 26 {
		if row, ok := m.sidebarRowAt(y); ok {
			next, cmd := m.clickSidebar(row)
			return true, next, cmd
		}
		m.focus = false
		return true, m, nil
	}
	return false, m, nil
}

func (m Model) terminalRelativeCoords(mouseX, mouseY int) (int, int, bool) {
	leftOffset := 0
	if m.sidebar {
		leftOffset = 26
	}
	topOffset := 1
	if m.activeSession >= 0 {
		topOffset = 2
	}
	termWidth, termHeight := m.terminalSize()
	col := mouseX - leftOffset
	row := mouseY - topOffset
	inBounds := col >= 0 && col < termWidth && row >= 0 && row < termHeight
	return col, row, inBounds
}

func (m *Model) clearSelection() {
	m.selecting = false
	m.hasSelection = false
	m.selStartRow = 0
	m.selStartCol = 0
	m.selEndRow = 0
	m.selEndCol = 0
}

func (m Model) extractSelectedText() string {
	if m.activeTerminal == nil {
		return ""
	}
	_, height := m.terminalSize()
	snapshot := m.activeTerminal.SnapshotScrolled(m.scrollOffset, height)
	lines := strings.Split(snapshot, "\n")
	if len(lines) == 0 {
		return ""
	}

	r1, c1 := m.selStartRow, m.selStartCol
	r2, c2 := m.selEndRow, m.selEndCol
	if r1 > r2 || (r1 == r2 && c1 > c2) {
		r1, c1, r2, c2 = r2, c2, r1, c1
	}

	var extracted []string
	for r := r1; r <= r2; r++ {
		if r < 0 || r >= len(lines) {
			continue
		}
		plain := ansi.Strip(lines[r])
		runes := []rune(plain)
		if len(runes) == 0 {
			extracted = append(extracted, "")
			continue
		}

		startC := 0
		if r == r1 {
			startC = c1
		}
		endC := len(runes) - 1
		if r == r2 {
			endC = c2
		}

		if startC < 0 {
			startC = 0
		}
		if startC > len(runes) {
			startC = len(runes)
		}
		if endC < 0 {
			endC = 0
		}
		if endC >= len(runes) {
			endC = len(runes) - 1
		}

		if startC <= endC && startC < len(runes) {
			slice := string(runes[startC : endC+1])
			extracted = append(extracted, strings.TrimRight(slice, " "))
		} else {
			extracted = append(extracted, "")
		}
	}
	return strings.Join(extracted, "\n")
}

func (m Model) applySelectionHighlight(content string, termWidth, termHeight int) string {
	if !m.hasSelection {
		return content
	}
	r1, c1 := m.selStartRow, m.selStartCol
	r2, c2 := m.selEndRow, m.selEndCol
	if r1 > r2 || (r1 == r2 && c1 > c2) {
		r1, c1, r2, c2 = r2, c2, r1, c1
	}

	lines := strings.Split(content, "\n")
	for r := 0; r < len(lines); r++ {
		if r < r1 || r > r2 {
			continue
		}
		startC := 0
		if r == r1 {
			startC = c1
		}
		endC := termWidth - 1
		if r == r2 {
			endC = c2
		}
		lines[r] = highlightANSILine(lines[r], startC, endC)
	}
	return strings.Join(lines, "\n")
}

func highlightANSILine(line string, startCol, endCol int) string {
	if startCol > endCol {
		return line
	}
	var sb strings.Builder
	sb.Grow(len(line) + 32)

	col := 0
	inEsc := false
	escBuf := make([]byte, 0, 32)
	highlighted := false

	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if inEsc {
			escBuf = append(escBuf, byte(r))
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '~' {
				inEsc = false
				sb.WriteString(string(escBuf))
				escBuf = escBuf[:0]
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			escBuf = append(escBuf, '\x1b')
			continue
		}

		if col == startCol && !highlighted {
			sb.WriteString("\x1b[7m")
			highlighted = true
		}

		sb.WriteRune(r)

		if col == endCol && highlighted {
			sb.WriteString("\x1b[27m")
			highlighted = false
		}

		col++
	}

	if inEsc {
		sb.WriteString(string(escBuf))
	}
	if highlighted {
		sb.WriteString("\x1b[27m")
	}

	return sb.String()
}

func (m Model) updateTerminal(message tea.Msg) (Model, tea.Cmd, bool) {
	if m.activeTerminal == nil {
		m.focus = false
		m.clearSelection()
		return m, nil, false
	}
	owner := terminal.Owner{Kind: "local", ID: "operator"}
	if m.activeTerminal.Owner().Empty() {
		_ = m.activeTerminal.Acquire(owner)
	}
	switch msg := message.(type) {
	case tea.KeyPressMsg:
		m.clearSelection()
		keyStr := msg.String()
		if idx, ok := parseAltShortcut(keyStr); ok {
			if m.activeSession >= 0 && m.activeSession < len(m.sessions) {
				executors := m.sessionExecutors(m.sessions[m.activeSession])
				if idx >= 0 && idx < len(executors) {
					next, cmd := m.selectTab(executors[idx])
					return next.(Model), cmd, true
				}
			}
		}
		switch keyStr {
		case "ctrl+]", "ctrl+g", "ctrl+p", "ctrl+b", "ctrl+q", "esc", "escape":
			m.focus = false
			m.lastErr = ""
			m.status = "Hub mode • enter returns to terminal"
			return m, nil, true
		case "pgup":
			_, height := m.terminalSize()
			m.scrollBy(height)
			return m, nil, true
		case "pgdown":
			_, height := m.terminalSize()
			m.scrollBy(-height)
			return m, nil, true
		}
		// Any other key snaps back to the live tail, like a real terminal.
		m.scrollOffset = 0
		key := uv.KeyPressEvent(uv.Key(msg.Key()))
		if err := m.activeTerminal.SendKey(owner, key); err != nil {
			m.noteTerminalWriteErr(err)
		}
		return m, nil, true
	case tea.KeyReleaseMsg:
		key := uv.KeyReleaseEvent(uv.Key(msg.Key()))
		if err := m.activeTerminal.SendKey(owner, key); err != nil {
			m.noteTerminalWriteErr(err)
		}
		return m, nil, true
	case tea.PasteMsg:
		m.clearSelection()
		if err := m.activeTerminal.Paste(owner, msg.Content); err != nil {
			m.noteTerminalWriteErr(err)
		}
		return m, nil, true
	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		col, row, inBounds := m.terminalRelativeCoords(mouse.X, mouse.Y)
		if inBounds {
			if mouse.Button == tea.MouseLeft {
				m.selecting = true
				m.hasSelection = true
				m.selStartRow, m.selStartCol = row, col
				m.selEndRow, m.selEndCol = row, col
			}
			mEv := uv.Mouse(mouse)
			mEv.X = col
			mEv.Y = row
			_ = m.activeTerminal.SendMouse(owner, uv.MouseClickEvent(mEv))
		}
		return m, nil, true
	case tea.MouseReleaseMsg:
		mouse := msg.Mouse()
		col, row, inBounds := m.terminalRelativeCoords(mouse.X, mouse.Y)
		if m.selecting {
			m.selecting = false
			termWidth, termHeight := m.terminalSize()
			cCol, cRow := col, row
			if cCol < 0 {
				cCol = 0
			}
			if cCol >= termWidth {
				cCol = termWidth - 1
			}
			if cRow < 0 {
				cRow = 0
			}
			if cRow >= termHeight {
				cRow = termHeight - 1
			}
			m.selEndRow, m.selEndCol = cRow, cCol
			if m.selStartRow != m.selEndRow || m.selStartCol != m.selEndCol {
				text := m.extractSelectedText()
				if text != "" {
					_ = clipboard.WriteAll(text)
					count := len([]rune(text))
					if count == 1 {
						m.toastMessage = "Copied 1 character"
						m.status = "Copied 1 character to clipboard"
					} else {
						m.toastMessage = fmt.Sprintf("Copied %d characters", count)
						m.status = fmt.Sprintf("Copied %d characters to clipboard", count)
					}
					m.toastExpires = time.Now().Add(3 * time.Second)
				}
			} else {
				m.hasSelection = false
			}
		}
		if inBounds {
			mEv := uv.Mouse(mouse)
			mEv.X = col
			mEv.Y = row
			_ = m.activeTerminal.SendMouse(owner, uv.MouseReleaseEvent(mEv))
		}
		return m, nil, true
	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		switch mouse.Button {
		case tea.MouseWheelUp:
			m.scrollBy(3)
		case tea.MouseWheelDown:
			m.scrollBy(-3)
		default:
			col, row, inBounds := m.terminalRelativeCoords(mouse.X, mouse.Y)
			if inBounds {
				mEv := uv.Mouse(mouse)
				mEv.X = col
				mEv.Y = row
				_ = m.activeTerminal.SendMouse(owner, uv.MouseWheelEvent(mEv))
			}
		}
		return m, nil, true
	case tea.MouseMotionMsg:
		mouse := msg.Mouse()
		col, row, inBounds := m.terminalRelativeCoords(mouse.X, mouse.Y)
		if m.selecting {
			termWidth, termHeight := m.terminalSize()
			cCol, cRow := col, row
			if cCol < 0 {
				cCol = 0
			}
			if cCol >= termWidth {
				cCol = termWidth - 1
			}
			if cRow < 0 {
				cRow = 0
			}
			if cRow >= termHeight {
				cRow = termHeight - 1
			}
			m.selEndRow, m.selEndCol = cRow, cCol
			return m, nil, true
		} else if inBounds {
			mEv := uv.Mouse(mouse)
			mEv.X = col
			mEv.Y = row
			_ = m.activeTerminal.SendMouse(owner, uv.MouseMotionEvent(mEv))
		}
		return m, nil, true
	}
	return m, nil, false
}

// scrollBy moves the scrollback view by delta lines (positive = further
// back into history, negative = toward the live tail), clamped to what the
// terminal's scrollback actually holds.
func (m *Model) scrollBy(delta int) {
	if m.activeTerminal == nil {
		return
	}
	m.scrollOffset += delta
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
	if max := m.activeTerminal.ScrollbackLen(); m.scrollOffset > max {
		m.scrollOffset = max
	}
}

// scanInstalledExecutors walks executors/ for CLIs already installed on
// disk (from a previous run, or copied in manually alongside their
// manifest.json) that aren't yet registered, and registers them — so a
// fresh or wiped database recovers without reinstalling anything.
func (m Model) scanInstalledExecutors() tea.Cmd {
	executorsRoot := m.app.Paths.Executors
	registered := make(map[string]bool, len(m.executors))
	for _, cfg := range m.executors {
		if cfg.Command != "" {
			registered[cfg.Command] = true
		}
	}
	appStore := m.app.Store
	return func() tea.Msg {
		manifests, err := executor.ScanInstalled(executorsRoot)
		if err != nil {
			return scannedMsg{err: err}
		}
		added := 0
		for _, manifest := range manifests {
			if registered[manifest.Command] {
				continue
			}
			cfg := domain.ExecutorConfig{
				ID: id.New("exec"), Name: manifest.Name, Command: manifest.Command, BinaryName: manifest.BinaryName,
				InstallDir: filepath.Join(executorsRoot, manifest.Slug), PromptSuffix: "\r",
			}
			if err := appStore.SaveExecutor(context.Background(), cfg); err == nil {
				added++
			}
		}
		return scannedMsg{found: len(manifests), added: added}
	}
}

// updateExecutorCLI re-runs the original install command recorded in the
// executor's manifest.json, in a real PTY, to fetch the latest version.
// Login/config is untouched (that's isolated separately in config/), and
// the SAME executor ID is kept (SaveExecutor upserts), so every session/tab
// referencing it keeps working — only Command gets re-resolved afterward.
func (m Model) updateExecutorCLI(cfg domain.ExecutorConfig) tea.Cmd {
	if cfg.InstallDir == "" {
		return func() tea.Msg {
			return savedMsg{kind: "executor update", id: cfg.ID,
				err: fmt.Errorf("%q wasn't installed through SessionHub — nothing to update automatically", cfg.Name)}
		}
	}
	manifest, err := executor.ReadManifest(cfg.InstallDir)
	if err != nil || strings.TrimSpace(manifest.InstallCmd) == "" {
		return func() tea.Msg {
			return savedMsg{kind: "executor update", id: cfg.ID,
				err: fmt.Errorf("no install command recorded for %q — nothing to re-run", cfg.Name)}
		}
	}
	dirs := executor.InstallDirs{
		Root: cfg.InstallDir, Bin: filepath.Join(cfg.InstallDir, "bin"),
		Config: filepath.Join(cfg.InstallDir, "config"), Runtime: filepath.Join(cfg.InstallDir, "runtime"),
	}
	installCommand, installArgs := shellCommandLine(manifest.InstallCmd)
	installCfg := domain.ExecutorConfig{
		ID: id.New("update"), Name: "Update " + cfg.Name, Command: installCommand, Args: installArgs,
		WorkingDir: dirs.Root, PromptSuffix: "\r",
	}
	width, height := m.terminalSize()
	instanceID := id.New("update")
	manager := m.app.Terminals
	registerAfter := cfg
	return func() tea.Msg {
		session, err := manager.StartEphemeral(instanceID, installCfg, width, height)
		return startedMsg{
			session: session,
			instance: domain.Instance{
				ID: instanceID, ExecutorID: installCfg.ID, State: domain.StateRunning,
			},
			err:           err,
			registerAfter: &registerAfter,
			installDirs:   &dirs,
			installLine:   manifest.InstallCmd,
		}
	}
}

// noteTerminalWriteErr distinguishes a benign "process already exited" write
// (expected once an install/test/executor run finishes) from a real error,
// so a stray keypress after completion doesn't paint a scary red banner over
// the ctrl+] hint.
func (m *Model) noteTerminalWriteErr(err error) {
	if errors.Is(err, terminal.ErrNotRunning) {
		m.status = "process finished • ctrl+] returns to Hub"
		return
	}
	m.lastErr = err.Error()
}

// checkPendingRegister looks at the currently displayed terminal: if it's
// the install run a pendingRegister is waiting on and it has now exited, it
// either fires the save (on success) or reports the failure, then clears
// pendingRegister either way so it can't fire twice or against a later,
// unrelated terminal.
func (m *Model) checkPendingRegister() tea.Cmd {
	if m.pendingRegister == nil || m.pendingRegister.instanceID != m.activeInstance.ID {
		return nil
	}
	state := m.activeTerminal.State()
	if state == domain.StateRunning || state == domain.StateWaiting {
		return nil
	}
	pending := *m.pendingRegister
	m.pendingRegister = nil
	if state != domain.StateFinished {
		m.lastErr = fmt.Sprintf("install for %q ended with state %q — it was not registered", pending.cfg.Name, state)
		return nil
	}
	if pending.installDirs != nil {
		searchName := pending.cfg.BinaryName
		if searchName == "" {
			searchName = pending.cfg.Command
		}
		resolved, found := terminal.FindInExecutorDir(searchName, pending.installDirs.Root, pending.installExtraDirs...)
		if !found {
			m.lastErr = fmt.Sprintf(
				"install for %q finished but %q wasn't found in %s or %s — the install command likely placed it somewhere else (check the terminal output above, or that it doesn't ignore --prefix)",
				pending.cfg.Name, searchName,
				filepath.Join(pending.installDirs.Root, "bin"), pending.installDirs.Root)
			return nil
		}
		pending.cfg.Command = resolved
		pending.cfg.BinaryName = searchName
		pending.cfg.InstallDir = pending.installDirs.Root
		_ = executor.WriteManifest(*pending.installDirs, executor.Manifest{
			ID: pending.cfg.ID, Name: pending.cfg.Name, Slug: filepath.Base(pending.installDirs.Root),
			Command: resolved, BinaryName: searchName, InstallCmd: pending.installLine, InstalledAt: time.Now().UTC(),
		})
	}
	m.status = fmt.Sprintf("install finished, registering %q…", pending.cfg.Name)
	return func() tea.Msg {
		err := m.app.Store.SaveExecutor(context.Background(), pending.cfg)
		return savedMsg{kind: "executor", id: pending.cfg.ID, err: err}
	}
}

func (m Model) updateConfirm(message tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := message.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "y", "Y":
		req := *m.confirm
		m.confirm = nil
		switch req.kind {
		case "session":
			return m, func() tea.Msg {
				err := m.app.Store.DeleteSession(context.Background(), req.id)
				return savedMsg{kind: "session", id: req.id, deleted: true, err: err}
			}
		case "executor":
			return m, func() tea.Msg {
				err := m.app.Store.DeleteExecutor(context.Background(), req.id)
				return savedMsg{kind: "executor", id: req.id, deleted: true, err: err}
			}
		case "executor-reset":
			return m, func() tea.Msg {
				cfg, err := m.app.Store.GetExecutor(context.Background(), req.id)
				if err != nil {
					return savedMsg{kind: "executor reset", id: req.id, err: err}
				}
				if err := executor.ResetAccount(cfg.InstallDir); err != nil {
					return savedMsg{kind: "executor reset", id: req.id, err: err}
				}
				return savedMsg{kind: "executor reset", id: req.id}
			}
		}
		return m, nil
	case "esc", "n", "N":
		m.confirm = nil
		m.status = "canceled"
		return m, nil
	}
	return m, nil
}

func (m Model) activateSelected() (tea.Model, tea.Cmd) {
	switch sections[m.section] {
	case "Sessions":
		if m.selected < len(m.sessions) {
			m.activeSession = m.selected
			return m, m.reload()
		}
	case "Executors":
		// Executors is a registry only: register, edit, validate (ctrl+t),
		// install, delete. Actually running one for work happens through a
		// session's tab bar, not from here.
		if m.selected < len(m.executors) {
			m.status = fmt.Sprintf(
				"%q is registered. Add it to a session (Sessions: n or e) to open it as a tab, or ctrl+t here to validate it.",
				m.executors[m.selected].Name)
		}
	default:
		if m.activeTerminal != nil {
			owner := terminal.Owner{Kind: "local", ID: "operator"}
			if m.activeTerminal.Owner().Empty() {
				if err := m.activeTerminal.Acquire(owner); err != nil {
					m.lastErr = err.Error()
					return m, nil
				}
			}
			m.focus = true
			m.scrollOffset = 0
			m.status = "terminal focused • ctrl+] returns to Hub"
		}
	}
	return m, nil
}

// sessionExecutors resolves a session's ExecutorIDs into the actual
// registered configs (in assigned order), skipping any that were since
// deleted.
func (m Model) sessionExecutors(session domain.Session) []domain.ExecutorConfig {
	var out []domain.ExecutorConfig
	for _, execID := range session.ExecutorIDs() {
		for _, cfg := range m.executors {
			if cfg.ID == execID {
				out = append(out, cfg)
				break
			}
		}
	}
	return out
}

func tabKeyFor(sessionID, executorID string) string {
	return sessionID + "|" + executorID
}

// selectTab focuses the tab for cfg within the active session: reattaching
// to its already-running instance if one exists, or lazily starting a new
// one on first selection.
func (m Model) selectTab(cfg domain.ExecutorConfig) (tea.Model, tea.Cmd) {
	if m.activeSession < 0 || m.activeSession >= len(m.sessions) {
		return m, nil
	}
	session := m.sessions[m.activeSession]
	key := tabKeyFor(session.ID, cfg.ID)
	if instanceID, ok := m.tabInstances[key]; ok {
		if live, ok := m.app.Terminals.Get(instanceID); ok && live.State() == domain.StateRunning {
			localOwner := terminal.Owner{Kind: "local", ID: "operator"}
			if live.Owner().Empty() {
				_ = live.Acquire(localOwner)
			}
			m.activeTerminal = live
			m.activeInstance = domain.Instance{
				ID: instanceID, SessionID: session.ID, ExecutorID: cfg.ID, State: domain.StateRunning,
			}
			m.focus = true
			m.scrollOffset = 0
			m.status = fmt.Sprintf("%q focused • esc / ctrl+g / ctrl+] returns to Hub", cfg.Name)
			m.resize()
			return m, nil
		}
	}
	width, height := m.terminalSize()
	service := m.app.Executors
	sessionID := session.ID
	executorID := cfg.ID
	return m, func() tea.Msg {
		live, instance, err := service.Start(context.Background(), sessionID, executorID, width, height)
		return startedMsg{session: live, instance: instance, err: err, tabKey: key}
	}
}

func (m *Model) resize() {
	width, height := m.terminalSize()
	m.viewport.SetWidth(width)
	m.viewport.SetHeight(height)
	if m.activeTerminal != nil {
		if err := m.activeTerminal.Resize(width, height); err != nil {
			m.lastErr = err.Error()
		}
	}
}

func (m Model) terminalSize() (int, int) {
	sidebarWidth := 0
	if m.sidebar {
		sidebarWidth = 26
	}
	width := m.width - sidebarWidth
	height := m.height - 2
	if m.activeSession >= 0 {
		height-- // tab bar row
	}
	if width < 2 {
		width = 2
	}
	if height < 2 {
		height = 2
	}
	return width, height
}

func (m Model) sectionLength() int {
	switch sections[m.section] {
	case "Sessions":
		return len(m.sessions)
	case "Executors":
		return len(m.executors)
	default:
		return 1
	}
}

func (m Model) View() tea.View {
	content := m.render()
	view := tea.NewView(content)
	view.AltScreen = true
	// Mouse is captured at all times (not just while a terminal is focused)
	// so the sidebar and tab bar are clickable from anywhere.
	view.MouseMode = tea.MouseModeAllMotion
	if m.focus {
		view.KeyboardEnhancements.ReportEventTypes = true
	}
	return view
}

func (m Model) render() string {
	rows := []string{m.renderTop()}
	if m.activeSession >= 0 {
		rows = append(rows, m.renderTabs())
	}
	rows = append(rows, m.renderCenter(), m.renderBottom())
	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	if m.form.kind != noForm {
		content = m.renderForm()
	}
	if m.confirm != nil {
		content = m.renderConfirm()
	}
	if toast := m.renderToast(); toast != "" {
		content = overlayToast(content, toast, m.width, m.height)
	}
	return content
}

func (m Model) renderToast() string {
	if m.toastMessage == "" || time.Now().After(m.toastExpires) {
		return ""
	}
	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#5F5FD7")).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#8787FF")).
		Padding(0, 1)

	return style.Render(m.toastMessage)
}

func overlayToast(base string, toast string, width, height int) string {
	if toast == "" {
		return base
	}
	baseLines := strings.Split(base, "\n")
	toastLines := strings.Split(toast, "\n")
	if len(baseLines) == 0 || len(toastLines) == 0 {
		return base
	}

	startY := 1
	if len(baseLines) > 2 {
		startY = 2
	}

	toastWidth := 0
	for _, l := range toastLines {
		if w := ansi.StringWidth(l); w > toastWidth {
			toastWidth = w
		}
	}
	startX := width - toastWidth - 4
	if startX < 0 {
		startX = 0
	}

	for i, tLine := range toastLines {
		y := startY + i
		if y >= len(baseLines) {
			break
		}
		baseLines[y] = overlayLine(baseLines[y], tLine, startX, width)
	}

	return strings.Join(baseLines, "\n")
}

func overlayLine(baseLine, overlayStr string, startX, totalWidth int) string {
	overlayWidth := ansi.StringWidth(overlayStr)
	endX := startX + overlayWidth

	var sb strings.Builder
	sb.Grow(len(baseLine) + len(overlayStr))

	col := 0
	inEsc := false
	escBuf := make([]byte, 0, 32)
	overlayInserted := false

	runes := []rune(baseLine)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if inEsc {
			escBuf = append(escBuf, byte(r))
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '~' {
				inEsc = false
				if col < startX || col >= endX {
					sb.WriteString(string(escBuf))
				}
				escBuf = escBuf[:0]
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			escBuf = append(escBuf, '\x1b')
			continue
		}

		if col == startX && !overlayInserted {
			sb.WriteString(overlayStr)
			overlayInserted = true
		}

		if col < startX || col >= endX {
			sb.WriteRune(r)
		}

		col++
	}

	if !overlayInserted {
		if col < startX {
			sb.WriteString(strings.Repeat(" ", startX-col))
		}
		sb.WriteString(overlayStr)
	}

	return sb.String()
}

// renderTabs shows one tab per Executor grouped under the active session —
// click one to focus it (starting it the first time). ● marks a tab with a
// live process; ○ one not started yet.
func (m Model) renderTabs() string {
	session := m.sessions[m.activeSession]
	executors := m.sessionExecutors(session)
	if len(executors) == 0 {
		return tabBarStyle.Width(max(0, m.width)).Render(mutedStyle.Render(" no Executors in this session — press e on Sessions to add some "))
	}
	var parts []string
	for i, cfg := range executors {
		marker := "○"
		key := tabKeyFor(session.ID, cfg.ID)
		running := false
		if instanceID, ok := m.tabInstances[key]; ok {
			if _, alive := m.app.Terminals.Get(instanceID); alive {
				marker = "●"
				running = true
			}
		}
		label := tabLabel(i, marker, cfg.Name)
		style := tabStyle
		if m.focus && running && m.tabInstances[key] == m.activeInstance.ID {
			style = tabActiveStyle
		}
		parts = append(parts, style.Render(label))
	}
	return tabBarStyle.Width(max(0, m.width)).Render(strings.Join(parts, ""))
}

func parseAltShortcut(keyStr string) (int, bool) {
	keyStr = strings.ToLower(keyStr)
	var digitChar byte
	for _, prefix := range []string{"alt+", "esc+", "meta+", "ctrl+"} {
		if strings.HasPrefix(keyStr, prefix) {
			rest := keyStr[len(prefix):]
			if len(rest) > 0 {
				digitChar = rest[len(rest)-1]
			}
			break
		}
	}
	if digitChar == 0 && len(keyStr) == 2 && keyStr[0] == '\x1b' {
		digitChar = keyStr[1]
	}

	if digitChar >= '1' && digitChar <= '9' {
		return int(digitChar - '1'), true
	}
	return 0, false
}

// tabLabel renders one tab: a 1-based digit shortcut for the first nine
// tabs (pressing that number opens/focuses it, no mouse required), a ●/○
// running marker, and the Executor's name.
func tabLabel(index int, marker, name string) string {
	if index < 9 {
		return fmt.Sprintf(" %d%s %s ", index+1, marker, name)
	}
	return fmt.Sprintf(" %s %s ", marker, name)
}

// tabLabelWidth mirrors tabLabel, so tabAt can compute the same column
// ranges without re-rendering.
func tabLabelWidth(index int, name string) int {
	return lipgloss.Width(tabLabel(index, "○", name))
}

// tabAt returns the Executor whose tab occupies screen column x, when y is
// the tab bar's row and a session is active.
func (m Model) tabAt(x, y int) (domain.ExecutorConfig, bool) {
	if m.activeSession < 0 || y != 1 {
		return domain.ExecutorConfig{}, false
	}
	session := m.sessions[m.activeSession]
	cursor := 0
	for i, cfg := range m.sessionExecutors(session) {
		width := tabLabelWidth(i, cfg.Name)
		if x >= cursor && x < cursor+width {
			return cfg, true
		}
		cursor += width
	}
	return domain.ExecutorConfig{}, false
}

func (m Model) renderConfirm() string {
	var b strings.Builder
	b.WriteString(errorStyle.Render("Confirm delete") + "\n\n")
	b.WriteString(m.confirm.message + "\n\n")
	b.WriteString(mutedStyle.Render("y confirms • esc/n cancels"))
	panel := modalStyle.Width(min(72, max(40, m.width-8))).Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

func (m Model) renderTop() string {
	session, workspace, executor, state := "No session", "", "No Executor", "idle"
	if m.activeSession >= 0 && m.activeSession < len(m.sessions) {
		session = m.sessions[m.activeSession].Name
		workspace = filepath.Base(m.sessions[m.activeSession].Workspace)
	}
	if m.activeTerminal != nil {
		executor, state = m.executorName(m.activeInstance.ExecutorID), string(m.activeTerminal.State())
	}
	branch := "no git"
	if m.git != nil {
		branch = m.git.Branch
		if !m.git.Clean {
			branch += " *"
		}
	}
	ver := ""
	if m.app != nil {
		ver = m.app.Version
	}
	if ver != "" && !strings.HasPrefix(ver, "v") {
		ver = "v" + ver
	}
	text := fmt.Sprintf(" SESSION HUB %s  %s  %s  %s  %s  %s ", ver, session, workspace, executor, branch, state)
	// Style.Width() word-wraps content that's too long rather than
	// truncating it — left unbounded, a long session/workspace/branch combo
	// would wrap the top bar onto a second line and silently shift every
	// row below it (including the tab bar's hardcoded y==1 click target).
	text = truncate(text, max(0, m.width))
	return topStyle.Width(max(0, m.width)).Render(text)
}

func (m Model) renderCenter() string {
	width, height := m.terminalSize()
	// Only show the terminal snapshot while actually focused on it — once
	// you leave with ctrl+], the Hub's normal section content should be
	// back, even though the (possibly finished) process is still tracked
	// in m.activeTerminal for the status bar/top bar.
	if m.focus && m.activeTerminal != nil {
		m.viewport.SetWidth(width)
		m.viewport.SetHeight(height)
		content := m.activeTerminal.SnapshotScrolled(m.scrollOffset, height)
		if m.hasSelection {
			content = m.applySelectionHighlight(content, width, height)
		}
		m.viewport.SetContent(content)
	} else {
		m.viewport.SetWidth(width)
		m.viewport.SetHeight(height)
		m.viewport.SetContent(m.emptyContent(width, height))
	}
	center := m.viewport.View()
	if m.sidebar {
		return lipgloss.JoinHorizontal(lipgloss.Top, m.renderSidebar(), center)
	}
	return center
}

type sidebarRowKind int

const (
	sidebarSection sidebarRowKind = iota
	sidebarSpacer
	sidebarHeader
	sidebarSessionRow
	sidebarExecutorRow
	sidebarInstanceRow
)

type sidebarRow struct {
	kind  sidebarRowKind
	index int
	label string // only meaningful for sidebarHeader
}

func (m Model) isInstanceLive(instance domain.Instance) bool {
	if term, ok := m.app.Terminals.Get(instance.ID); ok {
		return term.State() == domain.StateRunning
	}
	return false
}

// sidebarLayout is the single source of truth for what appears in the
// sidebar and in what order — renderSidebar draws it, sidebarRowAt maps a
// clicked screen row back into it, so the two can never drift apart.
func (m Model) sidebarLayout() []sidebarRow {
	rows := make([]sidebarRow, 0, len(sections)+len(m.sessions)+len(m.executors)+len(m.instances)+6)
	for i := range sections {
		rows = append(rows, sidebarRow{kind: sidebarSection, index: i})
	}
	rows = append(rows, sidebarRow{kind: sidebarSpacer}, sidebarRow{kind: sidebarHeader, label: "Sessions"})
	for i := range m.sessions {
		rows = append(rows, sidebarRow{kind: sidebarSessionRow, index: i})
	}
	rows = append(rows, sidebarRow{kind: sidebarSpacer}, sidebarRow{kind: sidebarHeader, label: "Executors"})
	for i := range m.executors {
		rows = append(rows, sidebarRow{kind: sidebarExecutorRow, index: i})
	}
	rows = append(rows, sidebarRow{kind: sidebarSpacer}, sidebarRow{kind: sidebarHeader, label: "Live terminals"})
	for i, instance := range m.instances {
		if m.isInstanceLive(instance) {
			rows = append(rows, sidebarRow{kind: sidebarInstanceRow, index: i})
		}
	}
	return rows
}

func (m Model) renderSidebar() string {
	var b strings.Builder
	for _, row := range m.sidebarLayout() {
		switch row.kind {
		case sidebarSection:
			style, prefix := sideItemStyle, "  "
			if row.index == m.section {
				style, prefix = sideActiveStyle, "› "
			}
			b.WriteString(style.Render(prefix+sections[row.index]) + "\n")
		case sidebarSpacer:
			b.WriteString("\n")
		case sidebarHeader:
			b.WriteString(mutedStyle.Render(row.label) + "\n")
		case sidebarSessionRow:
			prefix := "  "
			if row.index == m.activeSession {
				prefix = "● "
			}
			b.WriteString(sideItemStyle.Render(prefix+truncate(m.sessions[row.index].Name, 20)) + "\n")
		case sidebarExecutorRow:
			prefix := "  "
			if sections[m.section] == "Executors" && row.index == m.selected {
				prefix = "› "
			}
			cfg := m.executors[row.index]
			marker := ""
			if cfg.InstallDir != "" {
				marker = "○ "
				if executorActivated(cfg) {
					marker = "● "
				}
			}
			b.WriteString(sideItemStyle.Render(prefix+marker+truncate(cfg.Name, 18)) + "\n")
		case sidebarInstanceRow:
			instance := m.instances[row.index]
			marker := "● "
			b.WriteString(sideItemStyle.Render(marker+truncate(m.executorName(instance.ExecutorID), 18)) + "\n")
		}
	}
	return sidebarStyle.Width(25).Height(max(0, m.height-2)).Render(b.String())
}

// sidebarRowAt maps a clicked screen row to the sidebar entry it lands on,
// accounting for the top bar, the tab bar (only when a session is active),
// and the sidebar box's own top padding.
func (m Model) sidebarRowAt(y int) (sidebarRow, bool) {
	boxTop := 1
	if m.activeSession >= 0 {
		boxTop = 2
	}
	idx := y - boxTop - 1 // -1 for sidebarStyle's Padding(1,1) top line
	rows := m.sidebarLayout()
	if idx < 0 || idx >= len(rows) {
		return sidebarRow{}, false
	}
	return rows[idx], true
}

// clickSidebar applies whatever was clicked in the sidebar.
func (m Model) clickSidebar(row sidebarRow) (tea.Model, tea.Cmd) {
	switch row.kind {
	case sidebarSection:
		m.focus = false
		m.section = row.index
		m.selected = 0
		m.lastErr = ""
	case sidebarSessionRow:
		m.focus = false
		if row.index < len(m.sessions) {
			m.activeSession = row.index
			return m, m.reload()
		}
	case sidebarExecutorRow:
		if row.index < len(m.executors) {
			cfg := m.executors[row.index]
			for i, name := range sections {
				if name == "Executors" {
					m.section = i
					break
				}
			}
			m.selected = row.index
			if m.activeSession >= 0 {
				return m.selectTab(cfg)
			}
		}
	case sidebarInstanceRow:
		if row.index < len(m.instances) {
			instance := m.instances[row.index]
			if live, ok := m.app.Terminals.Get(instance.ID); ok && live.State() == domain.StateRunning {
				localOwner := terminal.Owner{Kind: "local", ID: "operator"}
				if live.Owner().Empty() {
					_ = live.Acquire(localOwner)
				}
				m.activeTerminal, m.activeInstance = live, instance
				m.focus = true
				m.scrollOffset = 0
				m.status = fmt.Sprintf("%q focused • esc / ctrl+g / ctrl+] returns to Hub", m.executorName(instance.ExecutorID))
				m.resize()
			} else if m.activeSession >= 0 {
				for _, cfg := range m.executors {
					if cfg.ID == instance.ExecutorID {
						return m.selectTab(cfg)
					}
				}
			}
		}
	}
	return m, nil
}

func (m Model) emptyContent(width, height int) string {
	var body strings.Builder
	body.WriteString(titleStyle.Render(sections[m.section]) + "\n\n")
	switch sections[m.section] {
	case "Sessions":
		if len(m.sessions) == 0 {
			body.WriteString("No sessions yet.\n\nPress n to create one: a workspace plus which Executors (CLIs) it groups together as tabs.")
		} else {
			for i, session := range m.sessions {
				prefix := "  "
				if i == m.selected {
					prefix = "› "
				}
				names := executorNamesForIDs(session.ExecutorIDs(), m.executors)
				executorList := "no Executors yet"
				if len(names) > 0 {
					executorList = strings.Join(names, ", ")
				}
				body.WriteString(fmt.Sprintf("%s%s\n    %s\n    %s\n", prefix, session.Name, session.Workspace, executorList))
			}
			body.WriteString("\nEnter activates the selected session (shows its CLI tabs above — press 1-9 or click a tab to open it). e edits which Executors it groups, d deletes it (asks to confirm).")
		}
	case "Executors":
		if len(m.executors) == 0 {
			body.WriteString("No Executors configured.\n\nPress i to add a CLI: pick Codex, Claude Code, OpenCode, Antigravity or Custom. Checks if it's already installed first, only runs the install command if missing, then registers it automatically. Press s to scan executors/ for CLIs already installed on disk but missing from this list.")
		} else {
			for i, cfg := range m.executors {
				prefix := "  "
				if i == m.selected {
					prefix = "› "
				}
				status := executorStatusLabel(cfg)
				if status != "" {
					status = "  " + status
				}
				body.WriteString(fmt.Sprintf("%s%s%s\n    %s %s\n", prefix, cfg.Name, status, cfg.Command, strings.Join(cfg.Args, " ")))
			}
			body.WriteString("\nThis is a registry, not where you work: e edits, d deletes (asks to confirm, also clears its past run history), i adds another CLI (checks before reinstalling — pick the same provider twice with different names for multiple accounts), u updates it (re-runs the install command, keeps the same login), x hard-resets its account/login (asks to confirm, keeps the CLI installed), s scans executors/ for ones already installed but not registered, n registers one manually, ctrl+t validates unsaved changes in a real PTY. To actually use a CLI, add it to a session (Sessions tab).")
		}
	case "Queues":
		body.WriteString("Prompt queues are persisted and idempotent.\n\nPress n to add an item. Completion requires an Executor rule or manual confirmation.")
	case "Pipelines":
		body.WriteString("Pipelines support prompt, deterministic command, approval, condition, parallel, and consolidation steps.\n\nDependencies, retries, workspace locks, and budgets are enforced by the engine.")
	case "Automations":
		body.WriteString("Schedules and watchers run only while this TUI is open.\n\nMissed occurrences use the configured skip, run-once, or ask policy.")
	case "Metrics":
		body.WriteString(fmt.Sprintf("Input Tokens     %d\nOutput Tokens    %d\nTotal Tokens     %d\nCache Read       %d\nCache Write      %d\nDuration         %s\n\nCusto equivalente estimado em API: US$ %.6f",
			m.metrics.InputTokens, m.metrics.OutputTokens, m.metrics.TotalTokens(),
			m.metrics.CacheRead, m.metrics.CacheWrite,
			time.Duration(m.metrics.Duration)*time.Millisecond,
			float64(m.metrics.CostMicrosUSD)/1_000_000))
	case "Logs":
		body.WriteString("Audit logs retain state transitions, terminal events, automation effects, approvals, errors, and metrics.\n\nSecret environment values are redacted.")
	case "Remote":
		address := m.app.RemoteHostAddress()
		if address == "" {
			address = "stopped"
		}
		body.WriteString("Remote Mode binds only to an explicitly selected Tailscale address.\n\nOne remote controller may own a PTY; all other clients observe or request control.\n\nHost: " + address + "\n\nPress n to start the Host.")
	case "Settings":
		body.WriteString(fmt.Sprintf("Session Hub %s\n\nData: %s\nDatabase: %s\nRecovered records on startup: %d\n\nNo provider accounts or Executors are bundled.",
			m.app.Version, m.app.Paths.Root, m.app.Paths.Database, m.app.RecoveredCount()))
	}
	return contentStyle.Width(width).Height(height).Render(body.String())
}

func (m Model) renderBottom() string {
	message := m.status
	if m.lastErr != "" {
		message = "Error: " + m.lastErr
	} else {
		message += fmt.Sprintf("  •  tokens %d  •  US$ %.4f",
			m.metrics.TotalTokens(), float64(m.metrics.CostMicrosUSD)/1_000_000)
	}
	// Style.Width() word-wraps content that's too long rather than
	// truncating it — left unbounded, a long status/error message would wrap
	// the bottom bar onto extra lines and push it past the screen height.
	message = truncate(message, max(0, m.width))
	if m.lastErr != "" {
		message = errorStyle.Render(message)
	}
	return statusStyle.Width(max(0, m.width)).Render(message)
}

func (m Model) executorName(executorID string) string {
	for _, cfg := range m.executors {
		if cfg.ID == executorID {
			return cfg.Name
		}
	}
	return "unknown"
}

var sessionLabels = []string{"Name", "Workspace"}

// newSessionForm creates a session: a workspace plus which registered
// Executors (CLIs) it groups together as tabs, picked from an actual
// checklist (space toggles, ↑/↓ moves) rather than typed free text. The
// session itself has no conversation "context" of its own.
func newSessionForm(executors []domain.ExecutorConfig) formModel {
	form := makeForm(sessionForm, "New session", sessionLabels,
		[]string{"Project work", "Absolute or relative directory"})
	form.executorChoices = executorChoicesFrom(executors, nil)
	return form
}

func editSessionForm(session domain.Session, executors []domain.ExecutorConfig) formModel {
	form := makeForm(sessionForm, "Edit session: "+session.Name, sessionLabels,
		[]string{"Project work", "Absolute or relative directory"})
	form.editingID = session.ID
	form.fields[0].SetValue(session.Name)
	form.fields[1].SetValue(session.Workspace)
	form.executorChoices = executorChoicesFrom(executors, session.ExecutorIDs())
	return form
}

func executorChoicesFrom(executors []domain.ExecutorConfig, selectedIDs []string) []executorChoice {
	selected := make(map[string]bool, len(selectedIDs))
	for _, id := range selectedIDs {
		selected[id] = true
	}
	choices := make([]executorChoice, len(executors))
	for i, cfg := range executors {
		choices[i] = executorChoice{id: cfg.ID, name: cfg.Name, selected: selected[cfg.ID]}
	}
	return choices
}

func executorNamesForIDs(ids []string, executors []domain.ExecutorConfig) []string {
	names := make([]string, 0, len(ids))
	for _, execID := range ids {
		for _, cfg := range executors {
			if cfg.ID == execID {
				names = append(names, cfg.Name)
				break
			}
		}
	}
	return names
}

// executorActivated reports whether cfg has a login/state present in its
// own isolated config folder — "activated" meaning logged in, "deactivated"
// meaning not. Only meaningful for executors installed through SessionHub
// (InstallDir set); manually-registered ones return false, unknown.
func executorActivated(cfg domain.ExecutorConfig) bool {
	if cfg.InstallDir == "" {
		return false
	}
	entries, err := os.ReadDir(filepath.Join(cfg.InstallDir, "config"))
	return err == nil && len(entries) > 0
}

// executorStatusLabel renders the activated/deactivated marker for the
// Executors list, or "" for executors with no isolated config folder to
// check (manually registered).
func executorStatusLabel(cfg domain.ExecutorConfig) string {
	if cfg.InstallDir == "" {
		return ""
	}
	if executorActivated(cfg) {
		return "● activated"
	}
	return "○ deactivated"
}

var executorCoreLabels = []string{
	"Display name", "Command", "Arguments (JSON array)", "Working directory",
}

var executorCorePlaceholders = []string{
	"My CLI", "executable or absolute path", `["arg","value"]`, "defaults to session workspace",
}

var executorAdvancedLabels = []string{
	"Environment (JSON array)", "Shell (optional)", "Resume command",
	"Resume args (JSON array)", "Recognition rules (JSON array)",
	"Timeout", "Prompt suffix", "Roles (JSON array)", "Model label",
	"Tokenizer", "Price ID",
}

var executorAdvancedPlaceholders = []string{
	`[{"name":"TOKEN","value":"...","secret":true}]`, "shell executable", "optional executable",
	`[]`, `[]`, "30m or 0", `\r`, `[]`, "metrics only", "unicode_words", "optional",
}

// newExecutorForm only asks what is required to launch the process. OAuth
// executors need nothing more: login happens interactively inside the PTY on
// first run and is persisted by the CLI itself. Automation-only fields
// (recognition rules, resume, roles, cost metadata) are opt-in via ctrl+a.
func newExecutorForm() formModel {
	return makeForm(executorForm, "Register Executor", executorCoreLabels, executorCorePlaceholders)
}

// newInstallForm is a single "Add a CLI" step: it checks whether Command is
// already installed (in the shared bin folder or on PATH) and, only if not,
// runs the install command in a real PTY. Either way the executor ends up
// registered without a separate manual step.
var installFormLabels = []string{
	"Display name", "Command", "Arguments (JSON array)", "Install command (only runs if not found)",
}

var installFormPlaceholders = []string{
	"Codex", "codex", `["--yolo"]`, `npm install @openai/codex`,
}

// newInstallForm opens the "Custom" (blank) variant of the add-a-CLI form.
func newInstallForm() formModel {
	return makeForm(installForm, "Add a CLI (checks first, installs only if missing)",
		installFormLabels, installFormPlaceholders)
}

// newInstallFormForProvider pre-fills the same form from a verified catalog
// entry, so the user only needs to confirm (or tweak, e.g. for a second
// account of the same CLI) rather than typing everything by hand.
func newInstallFormForProvider(p executor.Provider) formModel {
	form := makeForm(installForm, "Add a CLI — "+p.Name, installFormLabels, installFormPlaceholders)
	form.fields[0].SetValue(p.Name)
	form.fields[1].SetValue(p.Command)
	if len(p.Args) > 0 {
		if data, err := json.Marshal(p.Args); err == nil {
			form.fields[2].SetValue(string(data))
		}
	}
	form.fields[3].SetValue(p.InstallCmd())
	provider := p
	form.selectedProvider = &provider
	return form
}

// newProviderPickForm lists the verified catalog plus "Custom" as a
// single-select pick list (↑↓ moves, enter confirms) shown before the
// install form itself.
func newProviderPickForm() formModel {
	names := make([]string, 0, len(executor.Catalog)+1)
	for _, p := range executor.Catalog {
		names = append(names, p.Name)
	}
	names = append(names, "Custom")
	return formModel{kind: providerPickForm, title: "Add a CLI — pick one", providerNames: names}
}

// editExecutorForm opens the full field set (core + advanced) prefilled from
// an existing config, so saving never silently drops fields that aren't
// shown in the short create form.
func editExecutorForm(cfg domain.ExecutorConfig) formModel {
	labels := append(append([]string{}, executorCoreLabels...), executorAdvancedLabels...)
	placeholders := append(append([]string{}, executorCorePlaceholders...), executorAdvancedPlaceholders...)
	form := makeForm(executorForm, "Edit Executor: "+cfg.Name, labels, placeholders)
	form.editingID = cfg.ID
	values := executorToValues(cfg)
	for i, label := range labels {
		if value, ok := values[label]; ok && value != "" {
			form.fields[i].SetValue(value)
		}
	}
	return form
}

func executorToValues(cfg domain.ExecutorConfig) map[string]string {
	values := map[string]string{
		"Display name":      cfg.Name,
		"Command":           cfg.Command,
		"Working directory": cfg.WorkingDir,
		"Shell (optional)":  cfg.Shell,
		"Resume command":    cfg.ResumeCommand,
		"Prompt suffix":     strings.NewReplacer("\r", `\r`, "\n", `\n`, "\t", `\t`).Replace(cfg.PromptSuffix),
		"Model label":       cfg.Model,
		"Tokenizer":         cfg.Tokenizer,
		"Price ID":          cfg.PriceID,
	}
	if cfg.Timeout > 0 {
		values["Timeout"] = cfg.Timeout.String()
	}
	if len(cfg.Args) > 0 {
		values["Arguments (JSON array)"] = string(payloadJSON(cfg.Args))
	}
	if len(cfg.Environment) > 0 {
		values["Environment (JSON array)"] = string(payloadJSON(cfg.Environment))
	}
	if len(cfg.ResumeArgs) > 0 {
		values["Resume args (JSON array)"] = string(payloadJSON(cfg.ResumeArgs))
	}
	if len(cfg.Rules) > 0 {
		values["Recognition rules (JSON array)"] = string(payloadJSON(cfg.Rules))
	}
	if len(cfg.Roles) > 0 {
		values["Roles (JSON array)"] = string(payloadJSON(cfg.Roles))
	}
	return values
}

func newQueueForm(executors []domain.ExecutorConfig) formModel {
	hint := "Executor ID"
	if len(executors) > 0 {
		hint = executors[0].ID
	}
	return makeForm(queueForm, "Add prompt queue item",
		[]string{"Prompt", "Executor ID", "Priority", "Timeout", "Max attempts"},
		[]string{"Work to send through the PTY", hint, "0", "30m", "1"})
}

func newRemoteHostForm() formModel {
	return makeForm(remoteHostForm, "Start Tailscale Remote Host",
		[]string{"Tailscale listen address"},
		[]string{"100.x.y.z:8765"})
}

func newScheduleForm(executors []domain.ExecutorConfig) formModel {
	executor := "Executor ID"
	if len(executors) > 0 {
		executor = executors[0].ID
	}
	return makeForm(scheduleForm, "New schedule",
		[]string{"Name", "Kind", "Spec", "Timezone", "Executor ID", "Prompt", "Missed policy"},
		[]string{"Daily task", "once|daily|weekdays|interval", "13:30", "America/Sao_Paulo", executor, "Prompt sent by PTY", "skip|run_once|ask"})
}

func newPipelineForm() formModel {
	return makeForm(pipelineForm, "New pipeline",
		[]string{"Name", "Original request", "Steps (JSON array)", "Budget (JSON object)", "Save as template"},
		[]string{
			"Implement and review", "Request and acceptance criteria",
			`[{"name":"test","type":"command","config":{"command":"go","args":["test","./..."]},"max_attempts":1,"read_only":true}]`,
			`{"attempts":3,"duration":3600000000000}`, "false",
		})
}

func newPriceForm() formModel {
	return makeForm(priceForm, "New local price record",
		[]string{"Model label", "Version/date", "Input micros/million", "Output micros/million", "Cache read micros/million", "Cache write micros/million", "Explicit zero cost"},
		[]string{"manual model label", "2026-07-30", "0", "0", "0", "0", "false"})
}

func makeForm(kind formKind, title string, labels, placeholders []string) formModel {
	fields := make([]textinput.Model, len(labels))
	for i := range fields {
		fields[i] = textinput.New()
		fields[i].Placeholder = placeholders[i]
		fields[i].SetWidth(70)
	}
	if len(fields) > 0 {
		fields[0].Focus()
	}
	return formModel{kind: kind, title: title, labels: labels, fields: fields}
}

// updateProviderPick handles the single-select "pick a CLI" list: ↑↓ moves,
// enter opens the install form pre-filled from the chosen catalog entry (or
// blank for "Custom", the last option), esc cancels.
func (m Model) updateProviderPick(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.form = formModel{}
	case "up", "k":
		if m.form.choiceCursor > 0 {
			m.form.choiceCursor--
		}
	case "down", "j":
		if m.form.choiceCursor+1 < len(m.form.providerNames) {
			m.form.choiceCursor++
		}
	case "enter":
		if m.form.choiceCursor < len(executor.Catalog) {
			m.form = newInstallFormForProvider(executor.Catalog[m.form.choiceCursor])
		} else {
			m.form = newInstallForm()
		}
	}
	return m, nil
}

func (m Model) updateForm(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tickMsg:
		return m, tick()
	case savedMsg:
		if msg.err != nil {
			m.form.err = msg.err.Error()
			return m, nil
		}
		m.form = formModel{}
		m.status = msg.kind + " saved"
		return m, m.reload()
	case startedMsg:
		if msg.err != nil {
			m.form.err = msg.err.Error()
			return m, nil
		}
		m.form = formModel{}
		m.activeTerminal, m.activeInstance = msg.session, msg.instance
		m.focus = true
		m.scrollOffset = 0
		if msg.registerAfter != nil {
			m.pendingRegister = &pendingRegistration{
				instanceID: msg.instance.ID, cfg: *msg.registerAfter,
				installDirs: msg.installDirs, installLine: msg.installLine,
				installExtraDirs: msg.installExtraDirs,
			}
			m.status = fmt.Sprintf("installing %q • ctrl+] returns to Hub (auto-registers when it finishes)", msg.registerAfter.Name)
		} else {
			m.status = "test terminal focused • ctrl+] returns to Hub"
		}
		m.resize()
		return m, nil
	case tea.KeyPressMsg:
		if m.form.kind == providerPickForm {
			return m.updateProviderPick(msg)
		}
		switch msg.String() {
		case "esc":
			m.form = formModel{}
			return m, nil
		case "tab", "down":
			if m.form.onChecklist() {
				if msg.String() == "down" {
					if m.form.choiceCursor+1 < len(m.form.executorChoices) {
						m.form.choiceCursor++
					}
					return m, nil
				}
			} else if m.form.index < len(m.form.fields) {
				m.form.fields[m.form.index].Blur()
			}
			m.form.index = (m.form.index + 1) % m.form.totalStops()
			if m.form.index < len(m.form.fields) {
				return m, m.form.fields[m.form.index].Focus()
			}
			m.form.choiceCursor = 0
			return m, nil
		case "shift+tab", "up":
			if m.form.onChecklist() {
				if msg.String() == "up" {
					if m.form.choiceCursor > 0 {
						m.form.choiceCursor--
					}
					return m, nil
				}
			} else if m.form.index < len(m.form.fields) {
				m.form.fields[m.form.index].Blur()
			}
			m.form.index = (m.form.index - 1 + m.form.totalStops()) % m.form.totalStops()
			if m.form.index < len(m.form.fields) {
				return m, m.form.fields[m.form.index].Focus()
			}
			m.form.choiceCursor = len(m.form.executorChoices) - 1
			return m, nil
		case " ", "enter":
			if m.form.onChecklist() {
				m.form.executorChoices[m.form.choiceCursor].selected = !m.form.executorChoices[m.form.choiceCursor].selected
				return m, nil
			}
		case "ctrl+s":
			return m.submitForm()
		case "ctrl+a":
			if m.form.kind == executorForm && len(m.form.fields) == len(executorCoreLabels) {
				m.form.labels = append(m.form.labels, executorAdvancedLabels...)
				for _, placeholder := range executorAdvancedPlaceholders {
					field := textinput.New()
					field.Placeholder = placeholder
					field.SetWidth(70)
					m.form.fields = append(m.form.fields, field)
				}
				m.status = "advanced fields added"
			}
			return m, nil
		case "ctrl+t":
			if m.form.kind == executorForm {
				values := make([]string, len(m.form.fields))
				for i := range values {
					values[i] = m.form.fields[i].Value()
				}
				cfg, err := executorFromValues(m.form.labels, values, m)
				if err != nil {
					m.form.err = err.Error()
					return m, nil
				}
				width, height := m.terminalSize()
				instanceID := id.New("test")
				manager := m.app.Terminals
				return m, func() tea.Msg {
					session, err := manager.StartEphemeral(instanceID, cfg, width, height)
					return startedMsg{
						session: session,
						instance: domain.Instance{
							ID: instanceID, ExecutorID: cfg.ID, State: domain.StateRunning,
						},
						err: err,
					}
				}
			}
		}
	}
	if m.form.index >= len(m.form.fields) {
		return m, nil
	}
	field, cmd := m.form.fields[m.form.index].Update(message)
	m.form.fields[m.form.index] = field
	return m, cmd
}

func (m Model) submitForm() (tea.Model, tea.Cmd) {
	values := make([]string, len(m.form.fields))
	for i := range values {
		values[i] = m.form.fields[i].Value()
	}
	switch m.form.kind {
	case sessionForm:
		name, workspace := strings.TrimSpace(values[0]), strings.TrimSpace(values[1])
		if workspace == "" {
			workspace, _ = os.Getwd()
		}
		absolute, err := filepath.Abs(workspace)
		if err != nil {
			m.form.err = err.Error()
			return m, nil
		}
		info, err := os.Stat(absolute)
		if err != nil || !info.IsDir() {
			m.form.err = "workspace must be an existing directory"
			return m, nil
		}
		var executorIDs []string
		for _, choice := range m.form.executorChoices {
			if choice.selected {
				executorIDs = append(executorIDs, choice.id)
			}
		}
		session := domain.Session{ID: m.form.editingID, Name: name, Workspace: absolute}
		session.SetExecutorIDs(executorIDs)
		return m, func() tea.Msg {
			saved, err := m.app.Store.SaveSession(context.Background(), session)
			return savedMsg{kind: "session", id: saved.ID, err: err}
		}
	case executorForm:
		cfg, err := executorFromValues(m.form.labels, values, m)
		if err != nil {
			m.form.err = err.Error()
			return m, nil
		}
		if m.form.editingID != "" {
			cfg.ID = m.form.editingID
		}
		return m, func() tea.Msg {
			err := m.app.Store.SaveExecutor(context.Background(), cfg)
			return savedMsg{kind: "executor", id: cfg.ID, err: err}
		}
	case installForm:
		name := strings.TrimSpace(values[0])
		command := strings.TrimSpace(values[1])
		if name == "" || command == "" {
			m.form.err = "display name and command are required"
			return m, nil
		}
		var args []string
		if raw := strings.TrimSpace(values[2]); raw != "" {
			if err := json.Unmarshal([]byte(raw), &args); err != nil {
				m.form.err = "invalid arguments JSON: " + err.Error()
				return m, nil
			}
		}
		workingDir := ""
		if m.activeSession >= 0 {
			workingDir = m.sessions[m.activeSession].Workspace
		}
		slug := executor.Slug(name)
		dirs, err := executor.EnsureInstallDirs(m.app.Paths.Executors, slug)
		if err != nil {
			m.form.err = err.Error()
			return m, nil
		}
		installLine := strings.TrimSpace(values[3])
		cfg := domain.ExecutorConfig{
			ID: id.New("exec"), Name: name, Command: command, BinaryName: command, Args: args,
			WorkingDir: workingDir, PromptSuffix: "\r", InstallDir: dirs.Root,
		}
		if provider := m.form.selectedProvider; provider != nil && provider.ConfigEnvVar != "" {
			// Belt-and-suspenders alongside the universal HOME override:
			// some CLIs (especially native, non-Node binaries) only
			// reliably honor their own documented config-dir variable.
			cfg.Environment = append(cfg.Environment, domain.SecretEnv{
				Name: provider.ConfigEnvVar, Value: filepath.Join(dirs.Root, "config"),
			})
		}
		var extraDirs []string
		if provider := m.form.selectedProvider; provider != nil && !provider.Isolated && provider.DefaultDirs != nil {
			// This provider's own installer doesn't support a custom
			// directory, so it always lands in its conventional location —
			// check there too instead of only the executor's own folder.
			extraDirs = provider.DefaultDirs()
		}
		if resolved, found := terminal.FindInExecutorDir(command, dirs.Root, extraDirs...); found {
			// Already installed in this executor's own folder: register
			// directly, no reinstall, no PTY needed.
			cfg.Command = resolved
			_ = executor.WriteManifest(dirs, executor.Manifest{
				ID: cfg.ID, Name: name, Slug: slug, Command: resolved, BinaryName: command,
				InstallCmd: installLine, InstalledAt: time.Now().UTC(), AlreadyPresent: true,
			})
			return m, func() tea.Msg {
				err := m.app.Store.SaveExecutor(context.Background(), cfg)
				return savedMsg{kind: "executor", id: cfg.ID, err: err}
			}
		}
		if installLine == "" {
			m.form.err = fmt.Sprintf("%q was not found in executors/%s and no install command was given", command, slug)
			return m, nil
		}
		installCommand, installArgs := shellCommandLine(installLine)
		installCfg := domain.ExecutorConfig{
			ID: id.New("install"), Name: "Install " + name, Command: installCommand, Args: installArgs,
			WorkingDir: dirs.Root, PromptSuffix: "\r",
		}
		width, height := m.terminalSize()
		instanceID := id.New("install")
		manager := m.app.Terminals
		return m, func() tea.Msg {
			session, err := manager.StartEphemeral(instanceID, installCfg, width, height)
			return startedMsg{
				session: session,
				instance: domain.Instance{
					ID: instanceID, ExecutorID: installCfg.ID, State: domain.StateRunning,
				},
				err:              err,
				registerAfter:    &cfg,
				installDirs:      &dirs,
				installLine:      installLine,
				installExtraDirs: extraDirs,
			}
		}
	case queueForm:
		if m.activeSession < 0 {
			m.form.err = "select a session first"
			return m, nil
		}
		priority, err := strconv.Atoi(defaultString(values[2], "0"))
		if err != nil {
			m.form.err = "priority must be an integer"
			return m, nil
		}
		timeout, err := time.ParseDuration(defaultString(values[3], "30m"))
		if err != nil {
			m.form.err = "timeout must be a duration"
			return m, nil
		}
		attempts, err := strconv.Atoi(defaultString(values[4], "1"))
		if err != nil || attempts < 1 {
			m.form.err = "max attempts must be at least 1"
			return m, nil
		}
		item := domain.QueueItem{
			SessionID: m.sessions[m.activeSession].ID, ExecutorID: values[1],
			Prompt: values[0], Priority: priority, Timeout: timeout,
			MaxAttempts: attempts,
		}
		return m, func() tea.Msg {
			saved, err := m.app.Store.Enqueue(context.Background(), item)
			return savedMsg{kind: "queue item", id: saved.ID, err: err}
		}
	case remoteHostForm:
		address := strings.TrimSpace(values[0])
		return m, func() tea.Msg {
			err := m.app.StartRemoteHost(address)
			return savedMsg{kind: "remote host", id: address, err: err}
		}
	case scheduleForm:
		if m.activeSession < 0 {
			m.form.err = "select a session first"
			return m, nil
		}
		schedule := domain.Schedule{
			SessionID: m.sessions[m.activeSession].ID, Name: values[0],
			Kind: domain.ScheduleKind(values[1]), Spec: values[2],
			Timezone: defaultString(values[3], "UTC"), TargetType: "prompt",
			Target:  payloadJSON(map[string]string{"executor_id": values[4], "prompt": values[5]}),
			Enabled: true, MissedPolicy: domain.MissedPolicy(defaultString(values[6], "ask")),
			CreatedAt: time.Now().UTC(),
		}
		next, err := automation.NextOccurrence(schedule, time.Now().UTC())
		if err != nil {
			m.form.err = err.Error()
			return m, nil
		}
		if next.IsZero() {
			m.form.err = "schedule has no future occurrence"
			return m, nil
		}
		schedule.NextRun = &next
		return m, func() tea.Msg {
			saved, err := m.app.Store.SaveSchedule(context.Background(), schedule)
			return savedMsg{kind: "schedule", id: saved.ID, err: err}
		}
	case pipelineForm:
		if m.activeSession < 0 {
			m.form.err = "select a session first"
			return m, nil
		}
		var steps []domain.PipelineStep
		if err := json.Unmarshal([]byte(values[2]), &steps); err != nil {
			m.form.err = "invalid steps JSON: " + err.Error()
			return m, nil
		}
		var budget domain.Budget
		if strings.TrimSpace(values[3]) != "" {
			if err := json.Unmarshal([]byte(values[3]), &budget); err != nil {
				m.form.err = "invalid budget JSON: " + err.Error()
				return m, nil
			}
		}
		template, _ := strconv.ParseBool(defaultString(values[4], "false"))
		pipeline := domain.Pipeline{
			SessionID: m.sessions[m.activeSession].ID, Name: values[0],
			Request: values[1], Budget: budget, Template: template,
		}
		return m, func() tea.Msg {
			saved, _, err := m.app.Store.SavePipeline(context.Background(), pipeline, steps)
			return savedMsg{kind: "pipeline", id: saved.ID, err: err}
		}
	case priceForm:
		rates := make([]int64, 4)
		for i := range rates {
			value, err := strconv.ParseInt(defaultString(values[i+2], "0"), 10, 64)
			if err != nil || value < 0 {
				m.form.err = "price fields must be non-negative integers"
				return m, nil
			}
			rates[i] = value
		}
		zero, _ := strconv.ParseBool(defaultString(values[6], "false"))
		price := domain.Price{
			Model: values[0], Version: values[1], InputMicros: rates[0],
			OutputMicros: rates[1], CacheReadMicros: rates[2],
			CacheWriteMicros: rates[3], ZeroCostExplicit: zero,
		}
		return m, func() tea.Msg {
			saved, err := m.app.Store.SavePrice(context.Background(), price)
			return savedMsg{kind: "price", id: saved.ID, err: err}
		}
	}
	return m, nil
}

// executorFromValues looks fields up by label rather than fixed index, since
// the executor form may be the short (core-only) or expanded (ctrl+a,
// automation fields included) variant.
func executorFromValues(labels, values []string, m Model) (domain.ExecutorConfig, error) {
	field := func(name string) string {
		for i, label := range labels {
			if label == name {
				return values[i]
			}
		}
		return ""
	}

	var args, resumeArgs, roles []string
	var environment []domain.SecretEnv
	var rules []domain.RecognitionRule
	for value, target := range map[string]any{
		field("Arguments (JSON array)"):         &args,
		field("Environment (JSON array)"):       &environment,
		field("Resume args (JSON array)"):       &resumeArgs,
		field("Recognition rules (JSON array)"): &rules,
		field("Roles (JSON array)"):             &roles,
	} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if err := json.Unmarshal([]byte(value), target); err != nil {
			return domain.ExecutorConfig{}, fmt.Errorf("invalid JSON field: %w", err)
		}
	}
	timeout := time.Duration(0)
	if raw := strings.TrimSpace(field("Timeout")); raw != "" {
		var err error
		timeout, err = time.ParseDuration(raw)
		if err != nil {
			return domain.ExecutorConfig{}, fmt.Errorf("invalid timeout: %w", err)
		}
	}
	workingDir := strings.TrimSpace(field("Working directory"))
	if workingDir == "" && m.activeSession >= 0 {
		workingDir = m.sessions[m.activeSession].Workspace
	}
	suffix := strings.NewReplacer(`\r`, "\r", `\n`, "\n", `\t`, "\t").Replace(field("Prompt suffix"))
	if suffix == "" {
		suffix = "\r"
	}
	cfg := domain.ExecutorConfig{
		ID: id.New("exec"), Name: field("Display name"), Command: field("Command"), Args: args,
		WorkingDir: workingDir, Environment: environment, Shell: field("Shell (optional)"),
		ResumeCommand: field("Resume command"), ResumeArgs: resumeArgs, Rules: rules,
		Timeout: timeout, PromptSuffix: suffix, Roles: roles, Model: field("Model label"),
		Tokenizer: field("Tokenizer"), PriceID: field("Price ID"),
	}
	return cfg, cfg.Validate()
}

// shellCommandLine wraps a raw shell command line (e.g. an install command
// typed by the user) so it runs through the OS default shell inside a PTY.
func shellCommandLine(line string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd.exe", []string{"/d", "/s", "/c", line}
	}
	return "/bin/sh", []string{"-lc", line}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func payloadJSON(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}

func (m Model) renderProviderPick() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.form.title) + "\n\n")
	for i, name := range m.form.providerNames {
		prefix, style := "  ", sideItemStyle
		if i == m.form.choiceCursor {
			prefix, style = "› ", sideActiveStyle
		}
		b.WriteString(style.Render(prefix+name) + "\n")
	}
	b.WriteString("\n" + mutedStyle.Render("↑↓ moves • enter picks • esc cancels"))
	panel := modalStyle.Width(min(60, max(30, m.width-8))).Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

func (m Model) renderForm() string {
	if m.form.kind == providerPickForm {
		return m.renderProviderPick()
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.form.title) + "\n\n")
	start := 0
	if m.form.index > 6 {
		start = m.form.index - 6
	}
	end := min(len(m.form.fields), start+8)
	for i := start; i < end; i++ {
		label := m.form.labels[i]
		if i == m.form.index {
			label = "› " + label
		} else {
			label = "  " + label
		}
		b.WriteString(mutedStyle.Render(label) + "\n")
		b.WriteString(m.form.fields[i].View() + "\n")
	}
	if m.form.kind == sessionForm {
		label := "Executors (space toggles, ↑↓ moves)"
		if m.form.onChecklist() {
			label = "› " + label
		} else {
			label = "  " + label
		}
		b.WriteString(mutedStyle.Render(label) + "\n")
		if len(m.form.executorChoices) == 0 {
			b.WriteString(mutedStyle.Render("    (no Executors registered yet — register one in Executors first)") + "\n")
		}
		for i, choice := range m.form.executorChoices {
			box := "[ ]"
			if choice.selected {
				box = "[x]"
			}
			prefix := "  "
			style := sideItemStyle
			if m.form.onChecklist() && i == m.form.choiceCursor {
				prefix, style = "› ", sideActiveStyle
			}
			b.WriteString(style.Render(fmt.Sprintf("%s%s %s", prefix, box, choice.name)) + "\n")
		}
	}
	if m.form.err != "" {
		b.WriteString("\n" + errorStyle.Render(m.form.err))
	}
	hint := "tab next • shift+tab previous • ctrl+s save • esc cancel"
	switch m.form.kind {
	case executorForm:
		hint = "tab next • ctrl+t test unsaved config in real PTY • ctrl+s save • esc cancel"
		if len(m.form.fields) == len(executorCoreLabels) {
			hint = "tab next • ctrl+a more fields (recognition rules, resume, roles...) • " + hint
		}
	case installForm:
		hint = "ctrl+s checks, installs only if missing, then registers • esc cancel"
	case sessionForm:
		hint = "tab next • space toggles Executor, ↑↓ moves within the list • ctrl+s save • esc cancel"
	}
	b.WriteString("\n" + mutedStyle.Render(hint))
	panel := modalStyle.Width(min(86, max(40, m.width-8))).Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

var (
	accent   = lipgloss.Color("#7C6AF2")
	topStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F4F2FF")).
			Background(lipgloss.Color("#27233A")).
			Bold(true)
	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A8A3BA")).
			Background(lipgloss.Color("#191720"))
	sidebarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#C8C3D9")).
			Background(lipgloss.Color("#211E2B")).
			Padding(1, 1)
	sideItemStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#A8A3BA"))
	sideActiveStyle = lipgloss.NewStyle().Foreground(accent).Bold(true)
	titleStyle      = lipgloss.NewStyle().Foreground(accent).Bold(true)
	mutedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#777286"))
	errorStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B7A"))
	contentStyle    = lipgloss.NewStyle().Padding(2, 3)
	modalStyle      = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Background(lipgloss.Color("#191720")).
			Padding(1, 2)
	tabBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#211E2B"))
	tabStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A8A3BA")).
			Background(lipgloss.Color("#211E2B"))
	tabActiveStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F4F2FF")).
			Background(accent).
			Bold(true)
)
