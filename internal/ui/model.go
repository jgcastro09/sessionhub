package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/nodestage/sessionhub/internal/app"
	"github.com/nodestage/sessionhub/internal/automation"
	"github.com/nodestage/sessionhub/internal/domain"
	"github.com/nodestage/sessionhub/internal/gitstate"
	"github.com/nodestage/sessionhub/internal/id"
	"github.com/nodestage/sessionhub/internal/terminal"
	updatecheck "github.com/nodestage/sessionhub/internal/update"
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
	kind string
	id   string
	err  error
}

type startedMsg struct {
	session  *terminal.Session
	instance domain.Instance
	err      error
}

type tickMsg time.Time

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
	queueForm
	remoteHostForm
	scheduleForm
	pipelineForm
	priceForm
)

type formModel struct {
	kind   formKind
	title  string
	labels []string
	fields []textinput.Model
	index  int
	err    string
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
	metrics        domain.Metric
	git            *gitstate.State
	viewport       viewport.Model
	form           formModel
	status         string
	lastErr        string
}

func New(application *app.App) Model {
	vp := viewport.New()
	return Model{
		app: application, sidebar: true, activeSession: -1,
		viewport: vp,
		status:   "ctrl+g command mode • n new session • q quit",
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.reload(), tick())
}

func tick() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
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
	if m.form.kind != noForm {
		return m.updateForm(message)
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
	case savedMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			return m, nil
		}
		m.form = formModel{}
		m.status = msg.kind + " saved"
		return m, m.reload()
	case startedMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			return m, nil
		}
		m.activeTerminal, m.activeInstance = msg.session, msg.instance
		m.focus = true
		m.status = "terminal focused • ctrl+] returns to Hub"
		m.resize()
		return m, m.reload()
	case tickMsg:
		if m.activeTerminal != nil {
			m.viewport.SetContent(m.activeTerminal.Snapshot())
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
		case "shift+tab":
			m.section = (m.section - 1 + len(sections)) % len(sections)
			m.selected = 0
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
				m.form = newSessionForm()
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
			m.form = newExecutorForm()
		case "enter":
			return m.activateSelected()
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
			if sections[m.section] == "Settings" {
				checker := updatecheck.NewChecker("nodestage", "sessionhub")
				current := m.app.Version
				return m, func() tea.Msg {
					release, err := checker.Latest(context.Background())
					return updateMsg{release: release, newer: updatecheck.IsNewer(current, release.TagName), err: err}
				}
			}
		}
	}
	return m, nil
}

func (m Model) updateTerminal(message tea.Msg) (Model, tea.Cmd, bool) {
	if m.activeTerminal == nil {
		m.focus = false
		return m, nil, false
	}
	owner := terminal.Owner{Kind: "local", ID: "operator"}
	switch msg := message.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+]" {
			m.focus = false
			m.status = "Hub mode • enter returns to terminal"
			return m, nil, true
		}
		key := uv.KeyPressEvent(uv.Key(msg.Key()))
		if err := m.activeTerminal.SendKey(owner, key); err != nil {
			m.lastErr = err.Error()
		}
		return m, nil, true
	case tea.KeyReleaseMsg:
		key := uv.KeyReleaseEvent(uv.Key(msg.Key()))
		if err := m.activeTerminal.SendKey(owner, key); err != nil {
			m.lastErr = err.Error()
		}
		return m, nil, true
	case tea.PasteMsg:
		if err := m.activeTerminal.Paste(owner, msg.Content); err != nil {
			m.lastErr = err.Error()
		}
		return m, nil, true
	case tea.MouseClickMsg:
		_ = m.activeTerminal.SendMouse(owner, uv.MouseClickEvent(uv.Mouse(msg.Mouse())))
		return m, nil, true
	case tea.MouseReleaseMsg:
		_ = m.activeTerminal.SendMouse(owner, uv.MouseReleaseEvent(uv.Mouse(msg.Mouse())))
		return m, nil, true
	case tea.MouseWheelMsg:
		_ = m.activeTerminal.SendMouse(owner, uv.MouseWheelEvent(uv.Mouse(msg.Mouse())))
		return m, nil, true
	case tea.MouseMotionMsg:
		_ = m.activeTerminal.SendMouse(owner, uv.MouseMotionEvent(uv.Mouse(msg.Mouse())))
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) activateSelected() (tea.Model, tea.Cmd) {
	switch sections[m.section] {
	case "Sessions":
		if m.selected < len(m.sessions) {
			m.activeSession = m.selected
			return m, m.reload()
		}
	case "Executors":
		if m.activeSession < 0 {
			m.lastErr = "create or select a session first"
			return m, nil
		}
		if m.selected < len(m.executors) {
			cfg := m.executors[m.selected]
			sessionID := m.sessions[m.activeSession].ID
			width, height := m.terminalSize()
			service := m.app.Executors
			return m, func() tea.Msg {
				session, instance, err := service.Start(
					context.Background(), sessionID, cfg.ID, width, height)
				return startedMsg{session: session, instance: instance, err: err}
			}
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
			m.status = "terminal focused • ctrl+] returns to Hub"
		}
	}
	return m, nil
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
	if m.sidebar && !m.focus {
		sidebarWidth = 26
	}
	width := m.width - sidebarWidth
	height := m.height - 2
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
	if m.focus {
		view.MouseMode = tea.MouseModeAllMotion
		view.KeyboardEnhancements.ReportEventTypes = true
	}
	return view
}

func (m Model) render() string {
	top := m.renderTop()
	center := m.renderCenter()
	bottom := m.renderBottom()
	content := lipgloss.JoinVertical(lipgloss.Left, top, center, bottom)
	if m.form.kind != noForm {
		content = m.renderForm()
	}
	return content
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
	text := fmt.Sprintf(" SESSION HUB  %s  %s  %s  %s  %s ", session, workspace, executor, branch, state)
	return topStyle.Width(max(0, m.width)).Render(text)
}

func (m Model) renderCenter() string {
	width, height := m.terminalSize()
	if m.activeTerminal != nil {
		m.viewport.SetWidth(width)
		m.viewport.SetHeight(height)
		m.viewport.SetContent(m.activeTerminal.Snapshot())
	} else {
		m.viewport.SetWidth(width)
		m.viewport.SetHeight(height)
		m.viewport.SetContent(m.emptyContent(width, height))
	}
	center := m.viewport.View()
	if m.sidebar && !m.focus {
		return lipgloss.JoinHorizontal(lipgloss.Top, m.renderSidebar(), center)
	}
	return center
}

func (m Model) renderSidebar() string {
	var b strings.Builder
	for i, section := range sections {
		style := sideItemStyle
		prefix := "  "
		if i == m.section {
			style, prefix = sideActiveStyle, "› "
		}
		b.WriteString(style.Render(prefix + section))
		b.WriteByte('\n')
	}
	b.WriteString("\n" + mutedStyle.Render("Sessions") + "\n")
	for i, session := range m.sessions {
		prefix := "  "
		if i == m.activeSession {
			prefix = "● "
		}
		b.WriteString(sideItemStyle.Render(prefix+truncate(session.Name, 20)) + "\n")
	}
	b.WriteString("\n" + mutedStyle.Render("Live terminals") + "\n")
	for _, instance := range m.instances {
		marker := "○ "
		if _, ok := m.app.Terminals.Get(instance.ID); ok {
			marker = "● "
		}
		b.WriteString(sideItemStyle.Render(marker+truncate(m.executorName(instance.ExecutorID), 18)) + "\n")
	}
	return sidebarStyle.Width(25).Height(max(0, m.height-2)).Render(b.String())
}

func (m Model) emptyContent(width, height int) string {
	var body strings.Builder
	body.WriteString(titleStyle.Render(sections[m.section]) + "\n\n")
	switch sections[m.section] {
	case "Sessions":
		if len(m.sessions) == 0 {
			body.WriteString("No sessions yet.\n\nPress n to create a global session and choose its workspace.")
		} else {
			for i, session := range m.sessions {
				prefix := "  "
				if i == m.selected {
					prefix = "› "
				}
				body.WriteString(fmt.Sprintf("%s%s\n    %s\n", prefix, session.Name, session.Workspace))
			}
		}
	case "Executors":
		if len(m.executors) == 0 {
			body.WriteString("No Executors configured.\n\nPress n to register an external terminal program manually.")
		} else {
			for i, cfg := range m.executors {
				prefix := "  "
				if i == m.selected {
					prefix = "› "
				}
				body.WriteString(fmt.Sprintf("%s%s\n    %s %s\n", prefix, cfg.Name, cfg.Command, strings.Join(cfg.Args, " ")))
			}
			body.WriteString("\nEnter starts the selected Executor in a real PTY.")
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
		message = errorStyle.Render("Error: " + truncate(m.lastErr, max(10, m.width-10)))
	} else {
		message += fmt.Sprintf("  •  tokens %d  •  US$ %.4f",
			m.metrics.TotalTokens(), float64(m.metrics.CostMicrosUSD)/1_000_000)
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

func newSessionForm() formModel {
	return makeForm(sessionForm, "New session",
		[]string{"Name", "Workspace"},
		[]string{"Project work", "Absolute or relative directory"})
}

func newExecutorForm() formModel {
	return makeForm(executorForm, "Register Executor manually",
		[]string{
			"Display name", "Command", "Arguments (JSON array)", "Working directory",
			"Environment (JSON array)", "Shell (optional)", "Resume command",
			"Resume args (JSON array)", "Recognition rules (JSON array)",
			"Timeout", "Prompt suffix", "Roles (JSON array)", "Model label",
			"Tokenizer", "Price ID",
		},
		[]string{
			"My CLI", "executable or absolute path", `["arg","value"]`, "defaults to session workspace",
			`[{"name":"TOKEN","value":"...","secret":true}]`, "shell executable", "optional executable",
			`[]`, `[]`, "30m or 0", `\r`, `[]`, "metrics only", "unicode_words", "optional",
		})
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
		m.status = "test terminal focused • ctrl+] returns to Hub"
		m.resize()
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.form = formModel{}
			return m, nil
		case "tab", "down":
			m.form.fields[m.form.index].Blur()
			m.form.index = (m.form.index + 1) % len(m.form.fields)
			return m, m.form.fields[m.form.index].Focus()
		case "shift+tab", "up":
			m.form.fields[m.form.index].Blur()
			m.form.index = (m.form.index - 1 + len(m.form.fields)) % len(m.form.fields)
			return m, m.form.fields[m.form.index].Focus()
		case "ctrl+s":
			return m.submitForm()
		case "ctrl+t":
			if m.form.kind == executorForm {
				values := make([]string, len(m.form.fields))
				for i := range values {
					values[i] = m.form.fields[i].Value()
				}
				cfg, err := executorFromValues(values, m)
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
		session := domain.Session{Name: name, Workspace: absolute}
		return m, func() tea.Msg {
			saved, err := m.app.Store.SaveSession(context.Background(), session)
			return savedMsg{kind: "session", id: saved.ID, err: err}
		}
	case executorForm:
		cfg, err := executorFromValues(values, m)
		if err != nil {
			m.form.err = err.Error()
			return m, nil
		}
		return m, func() tea.Msg {
			err := m.app.Store.SaveExecutor(context.Background(), cfg)
			return savedMsg{kind: "executor", id: cfg.ID, err: err}
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

func executorFromValues(values []string, m Model) (domain.ExecutorConfig, error) {
	var args, resumeArgs, roles []string
	var environment []domain.SecretEnv
	var rules []domain.RecognitionRule
	for value, target := range map[string]any{
		values[2]: &args, values[4]: &environment, values[7]: &resumeArgs,
		values[8]: &rules, values[11]: &roles,
	} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if err := json.Unmarshal([]byte(value), target); err != nil {
			return domain.ExecutorConfig{}, fmt.Errorf("invalid JSON field: %w", err)
		}
	}
	timeout := time.Duration(0)
	if strings.TrimSpace(values[9]) != "" {
		var err error
		timeout, err = time.ParseDuration(values[9])
		if err != nil {
			return domain.ExecutorConfig{}, fmt.Errorf("invalid timeout: %w", err)
		}
	}
	workingDir := strings.TrimSpace(values[3])
	if workingDir == "" && m.activeSession >= 0 {
		workingDir = m.sessions[m.activeSession].Workspace
	}
	suffix := strings.NewReplacer(`\r`, "\r", `\n`, "\n", `\t`, "\t").Replace(values[10])
	if suffix == "" {
		suffix = "\r"
	}
	cfg := domain.ExecutorConfig{
		ID: id.New("exec"), Name: values[0], Command: values[1], Args: args,
		WorkingDir: workingDir, Environment: environment, Shell: values[5],
		ResumeCommand: values[6], ResumeArgs: resumeArgs, Rules: rules,
		Timeout: timeout, PromptSuffix: suffix, Roles: roles, Model: values[12],
		Tokenizer: values[13], PriceID: values[14],
	}
	return cfg, cfg.Validate()
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

func (m Model) renderForm() string {
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
	if m.form.err != "" {
		b.WriteString("\n" + errorStyle.Render(m.form.err))
	}
	hint := "tab next • shift+tab previous • ctrl+s save • esc cancel"
	if m.form.kind == executorForm {
		hint = "tab next • ctrl+t test unsaved config in real PTY • ctrl+s save • esc cancel"
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
)
