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
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/jgcastro09/sessionhub/internal/app"
	"github.com/jgcastro09/sessionhub/internal/automation"
	"github.com/jgcastro09/sessionhub/internal/domain"
	"github.com/jgcastro09/sessionhub/internal/executor"
	"github.com/jgcastro09/sessionhub/internal/gitstate"
	"github.com/jgcastro09/sessionhub/internal/id"
	"github.com/jgcastro09/sessionhub/internal/remote"
	"github.com/jgcastro09/sessionhub/internal/terminal"
	updatecheck "github.com/jgcastro09/sessionhub/internal/update"
	"github.com/jgcastro09/sessionhub/internal/voice"
)

var sections = []string{
	"Sessions", "Executors", "Queues", "Pipelines", "Automations",
	"Metrics", "Logs", "Remote", "Settings",
}

const voicePartialInterval = 2 * time.Second

type dataMsg struct {
	sessions  []domain.Session
	executors []domain.ExecutorConfig
	instances []domain.Instance
	metrics   domain.Metric
	git       *gitstate.State
	err       error
}

type remoteConnectedMsg struct {
	controller *remote.Controller
	device     remote.Device
	sessions   []domain.Session
	executors  []domain.ExecutorConfig
	statuses   []remote.ExecutorStatus
	err        error
}

type remoteStartedMsg struct {
	instance domain.Instance
	cfg      domain.ExecutorConfig
	err      error
}

type remoteInputMsg struct{ err error }

type remoteNavigationMsg struct{ err error }

type remoteRevokedMsg struct {
	revoked bool
}

type networkSettingsMsg struct {
	action  string
	enabled bool
	err     error
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

// voiceReadyMsg reports whether the local Whisper transcription server is
// installed and running (see toggleDictation) — the async result of
// voice.Manager.Ensure, which may include a one-time multi-minute download
// the first time dictation is ever used.
type voiceReadyMsg struct {
	err error
}

// voiceProgressMsg carries a best-effort update from the one-time local
// Whisper installation. updates is retained so Update can wait for the next
// update without blocking Bubble Tea's event loop.
type voiceProgressMsg struct {
	progress voice.Progress
	updates  <-chan voice.Progress
}

// voiceTranscribedMsg carries a finished transcription (see
// toggleDictation) back to whichever terminal was focused when recording
// started (target), even if focus moved on in the meantime.
type voiceTranscribedMsg struct {
	text     string
	previous string
	target   *terminal.Session
	err      error
}

// voicePartialMsg is one incremental, best-effort pass over the audio
// captured so far. Its recordingID prevents a slow request from a previous
// dictation being pasted into a later one.
type voicePartialMsg struct {
	recordingID uint64
	text        string
	err         error
}

// factoryResetDoneMsg reports the outcome of wiping the entire data
// directory (see submitForm's factoryResetForm case). A nil err means the
// app is shutting down cleanly — the next launch starts from a fresh
// install since ResolvePaths/store.Open recreate everything from scratch.
type factoryResetDoneMsg struct {
	err error
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
	automationForm
	automationDetailsView
	pipelineForm
	priceForm
	factoryResetForm
)

// factoryResetPhrase is the exact text the operator must type to actually
// trigger a factory reset — a typo-proof last gate behind the y/n confirm,
// since this action is unrecoverable (see submitForm's factoryResetForm
// case).
const factoryResetPhrase = "DELETE EVERYTHING"

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
	// originalExecutor, set only by editExecutorForm, is the config being
	// edited. editExecutorForm shows only the core fields (same short form
	// as "new executor"); advanced fields (Environment, Resume command,
	// Recognition rules, Roles, Shell, Timeout, Prompt suffix, Model label,
	// Tokenizer, Price ID) are carried through from here untouched unless
	// ctrl+a expands them into the form, so collapsing the form never
	// silently drops data the operator can't see.
	originalExecutor *domain.ExecutorConfig
	// selectedProvider carries the catalog entry (if any) chosen before
	// this installForm was opened, so submitForm knows where an
	// unisolated provider's installer conventionally places its binary.
	selectedProvider *executor.Provider
	details          string
	// Automation uses a purpose-built, selection-first editor: only the
	// prompt and time are typed; sessions, executors, schedule type and
	// weekdays are chosen directly in the UI.
	automationSessions  []automationChoice
	automationExecutors []automationChoice
	automationSchedule  int
	automationDays      [7]bool
	automationFocus     int
	automationCursor    int
}

type executorChoice struct {
	id       string
	name     string
	selected bool
}

type automationChoice struct {
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
	// remoteController is present only while this UI is controlling another
	// SessionHub. Local state is reloaded from the store immediately after
	// disconnecting, so Remote Mode never mutates or replaces local records.
	remoteController     *remote.Controller
	remoteDevice         remote.Device
	remoteTabInstances   map[string]string
	remoteExecutorStatus map[string]remote.ExecutorStatus
	remoteLastNavigation remote.ViewState
	activeInstance       domain.Instance
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

	// Voice dictation state (see toggleDictation): recording captures from
	// the mic into recorder; recordingTerminal is fixed at the moment
	// recording starts, so the transcript still lands in the right tab even
	// if focus moves on before transcription finishes. While recording, a
	// small WAV snapshot is transcribed every voicePartialInterval and only
	// its new words are pasted into the terminal.
	recording         bool
	voiceBusy         bool
	partialInFlight   bool
	recordingID       uint64
	dictatedText      string
	recorder          *voice.Recorder
	recordingTerminal *terminal.Session

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

	// Auto-updater state
	availableUpdate  *updatecheck.Release
	isUpdating       bool
	isCheckingUpdate bool
	lastUpdateCheck  time.Time
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
		viewport:             vp,
		tabInstances:         make(map[string]string),
		remoteTabInstances:   make(map[string]string),
		remoteExecutorStatus: make(map[string]remote.ExecutorStatus),
		status:               "click or use keys • ctrl+g command mode • n new session • q quit",
	}
}

type selfUpdateResultMsg struct {
	version string
	err     error
}

func (m Model) checkUpdateCmd() tea.Cmd {
	if m.app == nil || m.app.Version == "" {
		return nil
	}
	current := m.app.Version
	return func() tea.Msg {
		checker := updatecheck.NewChecker("jgcastro09", "sessionhub")
		release, err := checker.Latest(context.Background())
		if err != nil {
			return updateMsg{err: err}
		}
		return updateMsg{
			release: release,
			newer:   updatecheck.IsNewer(current, release.TagName),
		}
	}
}

func (m Model) performSelfUpdateCmd(release updatecheck.Release) tea.Cmd {
	return func() tea.Msg {
		checker := updatecheck.NewChecker("jgcastro09", "sessionhub")
		err := checker.ApplySelfUpdate(context.Background(), release)
		return selfUpdateResultMsg{version: release.TagName, err: err}
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.reload(), tick(), m.checkUpdateCmd())
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
	controller := m.remoteController
	sessionID := ""
	if m.activeSession >= 0 && m.activeSession < len(m.sessions) {
		sessionID = m.sessions[m.activeSession].ID
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if controller != nil {
			sessions, err := controller.Sessions(ctx)
			if err != nil {
				return dataMsg{err: err}
			}
			executors, err := controller.Executors(ctx)
			if err != nil {
				return dataMsg{err: err}
			}
			return dataMsg{sessions: sessions, executors: executors}
		}
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
	// A SessionHub being controlled remotely is deliberately read-only. Its
	// background mirrors the controller's navigation, while this modal is the
	// only local interaction and can revoke the current connection.
	if m.isRemotelyControlled() {
		return m.updateRemoteControlled(message)
	}
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
	if m.focus && m.remoteController != nil {
		if next, cmd, handled := m.updateRemoteTerminal(message); handled {
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
	case remoteConnectedMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			m.status = "Couldn't connect remote device: " + msg.err.Error()
			return m, nil
		}
		m.remoteController, m.remoteDevice = msg.controller, msg.device
		m.remoteTabInstances = make(map[string]string)
		m.remoteLastNavigation = remote.ViewState{}
		m.remoteExecutorStatus = make(map[string]remote.ExecutorStatus, len(msg.statuses))
		for _, status := range msg.statuses {
			m.remoteExecutorStatus[status.ExecutorID] = status
		}
		m.sessions, m.executors, m.instances = msg.sessions, msg.executors, nil
		m.activeSession, m.activeTerminal, m.focus = -1, nil, false
		if len(m.sessions) > 0 {
			m.activeSession = 0
		}
		m.status = fmt.Sprintf("REMOTE CONTROL • connected to %s • select a CLI tab", msg.device.Name)
		m.toastMessage = "Remote control active — press d in Remote to return locally"
		m.toastExpires = time.Now().Add(8 * time.Second)
		m.resize()
	case remoteStartedMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			m.status = "Couldn't open remote terminal: " + msg.err.Error()
			return m, nil
		}
		if m.remoteTabInstances == nil {
			m.remoteTabInstances = make(map[string]string)
		}
		m.remoteTabInstances[tabKeyFor(msg.instance.SessionID, msg.instance.ExecutorID)] = msg.instance.ID
		m.activeTerminal = nil
		m.activeInstance = msg.instance
		m.focus, m.scrollOffset = true, 0
		m.status = fmt.Sprintf("REMOTE CONTROL • %s on %s • f12 returns to remote hub", msg.cfg.Name, m.remoteDevice.Name)
		m.resize()
	case remoteInputMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			m.status = "Remote terminal input failed: " + msg.err.Error()
		}
	case remoteNavigationMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
		}
	case networkSettingsMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			m.status = "Couldn't update network settings: " + msg.err.Error()
			return m, nil
		}
		switch msg.action {
		case "remote":
			if msg.enabled {
				m.status = "Remote Mode enabled • host and discovery are online"
			} else {
				m.status = "Remote Mode disabled • no SessionHub is exposed on the network"
			}
		case "restart":
			m.status = "Remote Mode restarted • LAN/Tailscale discovery re-announced"
		default:
			if msg.enabled {
				m.status = "Tailscale discovery enabled • LAN and Tailscale are both available"
			} else {
				m.status = "Tailscale discovery disabled • LAN discovery remains available"
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
			m.status = fmt.Sprintf("installing %q • f12 returns to Hub (auto-registers when it finishes)", msg.registerAfter.Name)
		} else {
			m.status = "terminal focused • f12 returns to Hub"
		}
		m.resize()
		return m, m.reload()
	case tickMsg:
		if m.remoteController != nil {
			if err := m.remoteController.Err(); err != nil {
				controller := m.remoteController
				m.remoteController = nil
				m.remoteTabInstances = make(map[string]string)
				m.remoteExecutorStatus = make(map[string]remote.ExecutorStatus)
				m.remoteLastNavigation = remote.ViewState{}
				m.activeTerminal, m.focus = nil, false
				m.status = "Remote control ended by the controlled SessionHub • returned to local environment"
				return m, tea.Batch(tick(), func() tea.Msg { _ = controller.Close(); return savedMsg{kind: "local environment"} }, m.reload())
			}
			if cmd := m.syncRemoteNavigation(); cmd != nil {
				return m, tea.Batch(tick(), cmd)
			}
		}
		if time.Since(m.lastUpdateCheck) > 5*time.Minute {
			m.lastUpdateCheck = time.Now()
			if cmd := m.checkUpdateCmd(); cmd != nil {
				return m, tea.Batch(tick(), cmd)
			}
		}
		if m.activeTerminal != nil {
			_, height := m.terminalSize()
			m.viewport.SetContent(m.activeTerminal.SnapshotScrolled(m.scrollOffset, height))
			if cmd := m.checkPendingRegister(); cmd != nil {
				return m, tea.Batch(tick(), cmd)
			}
		}
		return m, tick()
	case updateMsg:
		m.lastUpdateCheck = time.Now()
		m.isCheckingUpdate = false
		if msg.err != nil {
			m.lastErr = fmt.Sprintf("Update check failed: %v", msg.err)
			m.status = "Update check failed"
		} else if msg.newer {
			m.availableUpdate = &msg.release
			m.toastMessage = fmt.Sprintf("✨ Update %s is available! Press 'u' to update", msg.release.TagName)
			m.toastExpires = time.Now().Add(12 * time.Second)
			m.status = fmt.Sprintf("Update %s available • Press 'u' to update now • %s", msg.release.TagName, msg.release.HTMLURL)
		} else {
			m.availableUpdate = nil
			m.status = "Session Hub is up to date"
		}
	case selfUpdateResultMsg:
		m.isUpdating = false
		if msg.err != nil {
			m.lastErr = fmt.Sprintf("Update failed: %v", msg.err)
			m.status = "Update failed"
			m.toastMessage = fmt.Sprintf("❌ Update failed: %v", msg.err)
			m.toastExpires = time.Now().Add(10 * time.Second)
		} else {
			m.availableUpdate = nil
			m.toastMessage = fmt.Sprintf("🎉 Session Hub updated to %s! Restart app to apply.", msg.version)
			m.toastExpires = time.Now().Add(15 * time.Second)
			m.status = fmt.Sprintf("Session Hub updated to %s • Restart app to apply", msg.version)
		}
	case voiceReadyMsg:
		m.voiceBusy = false
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			m.status = "Voice setup failed: " + msg.err.Error()
			return m, nil
		}
		if m.activeTerminal == nil {
			m.status = "Voice transcription ready, but no CLI tab is focused"
			return m, nil
		}
		recorder := voice.NewRecorder(m.app.Voice.RecorderExe())
		if err := recorder.Start(); err != nil {
			m.lastErr = err.Error()
			m.status = "Microphone error: " + err.Error()
			return m, nil
		}
		m.recorder = recorder
		m.recording = true
		m.recordingID++
		m.partialInFlight = true
		m.dictatedText = ""
		m.recordingTerminal = m.activeTerminal
		m.status = "🎤 Recording... text will appear as you speak • F9 stops"
		return m, m.transcribePartialCmd(m.recordingID, recorder)
	case voiceProgressMsg:
		if !m.voiceBusy {
			return m, nil
		}
		m.status = formatVoiceProgress(msg.progress)
		return m, waitVoiceProgressCmd(msg.updates)
	case voicePartialMsg:
		if !m.recording || msg.recordingID != m.recordingID {
			return m, nil
		}
		m.partialInFlight = false
		if msg.err == nil {
			text := strings.TrimSpace(msg.text)
			if delta := voiceTranscriptDelta(m.dictatedText, text); delta != "" && m.recordingTerminal != nil {
				owner := terminal.Owner{Kind: "local", ID: "operator"}
				if err := m.recordingTerminal.Paste(owner, delta); err != nil {
					m.lastErr = err.Error()
					m.status = "Couldn't paste live transcription: " + err.Error()
				} else {
					m.status = fmt.Sprintf("🎤 Recording... pasted %d live characters • F9 stops", len([]rune(delta)))
				}
			}
			if text != "" {
				m.dictatedText = text
			}
		}
		m.partialInFlight = true
		return m, m.transcribePartialCmd(m.recordingID, m.recorder)
	case voiceTranscribedMsg:
		m.voiceBusy = false
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			m.status = "Transcription failed: " + msg.err.Error()
			return m, nil
		}
		delta := voiceTranscriptDelta(msg.previous, strings.TrimSpace(msg.text))
		if delta == "" {
			m.status = "Live voice dictation complete"
			return m, nil
		}
		if msg.target != nil {
			owner := terminal.Owner{Kind: "local", ID: "operator"}
			if err := msg.target.Paste(owner, delta); err != nil {
				m.lastErr = err.Error()
				m.status = "Couldn't paste transcription: " + err.Error()
				return m, nil
			}
		}
		m.status = fmt.Sprintf("Live voice dictation complete • pasted final %d characters", len([]rune(delta)))
		return m, nil
	case factoryResetDoneMsg:
		if msg.err != nil {
			m.lastErr = fmt.Sprintf("Factory reset failed: %v", msg.err)
			m.status = "Factory reset failed — some files may be left over"
			return m, nil
		}
		return m, tea.Quit
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
		// A connected remote hub is view/control-only in this first version:
		// terminal tabs use the remote PTY, while local configuration actions
		// stay local and cannot accidentally edit the other computer's store.
		if m.remoteController != nil && strings.Contains("n e i s x c r", msg.String()) && sections[m.section] != "Remote" {
			m.status = "Remote control exposes sessions and CLI terminals only • return locally in Remote (d) to edit configuration"
			return m, nil
		}
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
				m.status = "Remote discovery is automatic — select an online device and press Enter to connect"
			case "Automations":
				m.form = newAutomationForm(m.sessions, m.executors, m.activeSession)
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
			case "Automations":
				if item, ok := m.selectedAutomation(); ok {
					m.form = editAutomationForm(item, m.sessions, m.executors)
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
			case "Remote":
				if m.remoteController != nil {
					controller := m.remoteController
					m.remoteController = nil
					m.remoteTabInstances = make(map[string]string)
					m.remoteExecutorStatus = make(map[string]remote.ExecutorStatus)
					m.activeTerminal, m.focus = nil, false
					m.status = "Returned to local SessionHub environment"
					return m, func() tea.Msg { _ = controller.Close(); return savedMsg{kind: "local environment"} }
				}
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
			case "Automations":
				if item, ok := m.selectedAutomation(); ok {
					m.confirm = &confirmRequest{kind: "automation", id: item.ID, name: item.Name,
						message: fmt.Sprintf("Delete automation %q? Its saved last-run summary will also be removed.", item.Name)}
				}
			}
		case "l":
			if sections[m.section] == "Automations" {
				if item, ok := m.selectedAutomation(); ok {
					m.form = automationDetailsForm(item)
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
			if sections[m.section] == "Settings" {
				enabled := !m.app.RemoteNetworkStatus().RemoteEnabled
				application := m.app
				m.status = "Updating Remote Mode..."
				return m, func() tea.Msg {
					return networkSettingsMsg{action: "remote", enabled: enabled, err: application.SetRemoteEnabled(enabled)}
				}
			}
			if sections[m.section] == "Automations" {
				if item, ok := m.selectedAutomation(); ok {
					if err := m.app.AutomationScheduler.RunNow(item.ID); err != nil {
						m.lastErr = err.Error()
					} else {
						m.status = fmt.Sprintf("Automation %q started manually", item.Name)
					}
				}
				return m, nil
			}
			if m.activeTerminal != nil {
				owner := terminal.Owner{Kind: "local", ID: "operator"}
				if err := m.activeTerminal.Release(owner); err != nil {
					m.lastErr = err.Error()
				} else {
					m.status = "terminal control released • automation or remote may request it"
				}
			}
		case "c":
			if sections[m.section] == "Automations" {
				if item, ok := m.selectedAutomation(); ok {
					if err := m.app.AutomationScheduler.Cancel(item.ID); err != nil {
						m.lastErr = err.Error()
					} else {
						m.status = fmt.Sprintf("Canceling automation %q", item.Name)
					}
				}
				return m, nil
			}
			if m.activeSession >= 0 {
				sessionID := m.sessions[m.activeSession].ID
				appContext := m.app.Context
				return m, func() tea.Msg {
					_, err := appContext.Checkpoint(context.Background(), sessionID, "Manual checkpoint", false)
					return savedMsg{kind: "checkpoint", err: err}
				}
			}
		case "u":
			if m.availableUpdate != nil {
				currentVer := "unknown"
				if m.app != nil {
					currentVer = m.app.Version
				}
				m.confirm = &confirmRequest{
					kind:    "update",
					name:    m.availableUpdate.TagName,
					message: fmt.Sprintf("Update Session Hub from %s to %s?", currentVer, m.availableUpdate.TagName),
				}
				return m, nil
			}
			if sections[m.section] == "Executors" {
				if m.selected < len(m.executors) {
					return m, m.updateExecutorCLI(m.executors[m.selected])
				}
			} else {
				m.isCheckingUpdate = true
				m.status = "Checking for updates..."
				m.toastMessage = "Checking for updates..."
				m.toastExpires = time.Now().Add(5 * time.Second)
				return m, m.checkUpdateCmd()
			}
		case "ctrl+r":
			if sections[m.section] == "Settings" {
				m.confirm = &confirmRequest{
					kind: "factory-reset-warn",
					message: fmt.Sprintf(
						"step 1 of 3 done (ctrl+r) — step 2 of 3: this permanently deletes every "+
							"session, executor, login, log, and downloaded file, wiping %s back to a "+
							"clean install. This cannot be undone.\n\nContinue to the final confirmation?",
						m.app.Paths.Root),
				}
				return m, nil
			}
		case "t":
			if sections[m.section] == "Settings" {
				enabled := !m.app.RemoteNetworkStatus().TailscaleEnabled
				application := m.app
				return m, func() tea.Msg {
					return networkSettingsMsg{action: "tailscale", enabled: enabled, err: application.SetTailscaleEnabled(enabled)}
				}
			}
		case "p":
			if sections[m.section] == "Settings" {
				application := m.app
				m.status = "Restarting Remote Mode..."
				return m, func() tea.Msg {
					return networkSettingsMsg{action: "restart", err: application.RestartRemote()}
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
	if m.voiceButtonAt(x, y) {
		next, cmd := m.toggleDictation()
		return true, next, cmd
	}
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

// voiceButtonLabel is deliberately short: it is rendered in the top bar so
// it stays visible even while a full-screen CLI owns the terminal viewport.
func (m Model) voiceButtonLabel() string {
	if m.recording {
		return " ■ PARAR "
	}
	if m.voiceBusy {
		return " ⏳ PREPARANDO "
	}
	return " 🎙 MICROFONE "
}

func (m Model) voiceButtonBounds() (start, end int, ok bool) {
	buttonWidth := ansi.StringWidth(m.voiceButtonLabel())
	// Keep enough room for the Session Hub identity in narrow terminals. The
	// keyboard shortcut remains available when there is no room for a button.
	if m.width < buttonWidth+20 {
		return 0, 0, false
	}
	return m.width - buttonWidth, m.width, true
}

func (m Model) voiceButtonAt(x, y int) bool {
	start, end, ok := m.voiceButtonBounds()
	return ok && y == 0 && x >= start && x < end
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
		case "f12":
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
		case "f9":
			next, cmd := m.toggleDictation()
			return next, cmd, true
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
		// A full-screen app (alt-screen — opencode, vim, htop, less...) owns
		// scrolling itself and expects the wheel forwarded like any other
		// mouse event; only a plain shell prompt (no alt-screen) benefits
		// from the Hub instead panning its own scrollback.
		if !m.activeTerminal.IsAltScreen() {
			switch mouse.Button {
			case tea.MouseWheelUp:
				m.scrollBy(3)
				return m, nil, true
			case tea.MouseWheelDown:
				m.scrollBy(-3)
				return m, nil, true
			}
		}
		col, row, inBounds := m.terminalRelativeCoords(mouse.X, mouse.Y)
		if inBounds {
			mEv := uv.Mouse(mouse)
			mEv.X = col
			mEv.Y = row
			_ = m.activeTerminal.SendMouse(owner, uv.MouseWheelEvent(mEv))
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

// toggleDictation is F9's handler: first press installs/starts the local
// Whisper server if needed (async — the very first call may be a
// multi-minute download), starts recording, and begins incremental local
// transcription. The second press stops recording and performs one final
// pass for words that arrived after the most recent live snapshot.
func (m Model) toggleDictation() (Model, tea.Cmd) {
	if m.voiceBusy && !m.recording {
		m.status = "voice dictation is still working..."
		return m, nil
	}
	if m.recording {
		m.recording = false
		m.recordingID++ // invalidate an in-flight live-transcription request
		m.partialInFlight = false
		wav, err := m.recorder.Stop()
		m.recorder = nil
		if err != nil {
			m.lastErr = err.Error()
			m.status = "Recording failed: " + err.Error()
			m.recordingTerminal = nil
			return m, nil
		}
		m.voiceBusy = true
		m.status = "Transcribing..."
		target := m.recordingTerminal
		previous := m.dictatedText
		m.recordingTerminal = nil
		manager := m.app.Voice
		return m, func() tea.Msg {
			text, err := manager.Transcribe(context.Background(), wav)
			return voiceTranscribedMsg{text: text, previous: previous, target: target, err: err}
		}
	}
	m.voiceBusy = true
	m.status = "Preparing local voice transcription..."
	manager := m.app.Voice
	updates := make(chan voice.Progress, 1)
	return m, tea.Batch(voiceSetupCmd(manager, updates), waitVoiceProgressCmd(updates))
}

func voiceSetupCmd(manager *voice.Manager, updates chan voice.Progress) tea.Cmd {
	return func() tea.Msg {
		defer close(updates)
		err := manager.EnsureWithProgress(context.Background(), func(progress voice.Progress) {
			// Download readers must never wait on UI rendering. Retain the most
			// recent update and drop a stale one when the event loop is busy.
			select {
			case updates <- progress:
			default:
				select {
				case <-updates:
				default:
				}
				select {
				case updates <- progress:
				default:
				}
			}
		})
		return voiceReadyMsg{err: err}
	}
}

func waitVoiceProgressCmd(updates <-chan voice.Progress) tea.Cmd {
	return func() tea.Msg {
		progress, ok := <-updates
		if !ok {
			return nil
		}
		return voiceProgressMsg{progress: progress, updates: updates}
	}
}

func formatVoiceProgress(progress voice.Progress) string {
	if progress.Total <= 0 {
		return progress.Stage
	}
	percent := progress.Current * 100 / progress.Total
	if percent > 100 {
		percent = 100
	}
	return fmt.Sprintf("%s: %d%% (%.1f / %.1f MB)", progress.Stage, percent,
		float64(progress.Current)/(1024*1024), float64(progress.Total)/(1024*1024))
}

// transcribePartialCmd waits just long enough to collect a useful phrase,
// then sends the growing WAV to the already-running local whisper-server.
// Requests are chained from voicePartialMsg so only one inference runs at a
// time; this avoids competing model requests and preserves transcript order.
func (m Model) transcribePartialCmd(recordingID uint64, recorder *voice.Recorder) tea.Cmd {
	manager := m.app.Voice
	return func() tea.Msg {
		timer := time.NewTimer(voicePartialInterval)
		defer timer.Stop()
		<-timer.C
		wav, err := recorder.Snapshot()
		if err != nil {
			return voicePartialMsg{recordingID: recordingID, err: err}
		}
		text, err := manager.Transcribe(context.Background(), wav)
		return voicePartialMsg{recordingID: recordingID, text: text, err: err}
	}
}

// voiceTranscriptDelta returns only the words beyond the previous full
// inference. Whisper can revise punctuation or spelling in an earlier live
// pass; those already-pasted characters cannot safely be edited inside an
// arbitrary CLI, so use word position to avoid duplicate text and append
// only the newly heard tail.
func voiceTranscriptDelta(previous, current string) string {
	previousWords := strings.Fields(strings.TrimSpace(previous))
	currentWords := strings.Fields(strings.TrimSpace(current))
	if len(currentWords) <= len(previousWords) {
		return ""
	}
	delta := strings.Join(currentWords[len(previousWords):], " ")
	if len(previousWords) > 0 {
		return " " + delta
	}
	return delta
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
// the f12 hint.
func (m *Model) noteTerminalWriteErr(err error) {
	if errors.Is(err, terminal.ErrNotRunning) {
		m.status = "process finished • f12 returns to Hub"
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
		resolved, found := terminal.FindExecutable(searchName, pending.installDirs.Root, pending.installExtraDirs...)
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
	}
	m.status = fmt.Sprintf("install finished, registering %q…", pending.cfg.Name)
	return func() tea.Msg {
		// The manifest is only written once the DB save actually commits, so a
		// failed/lost save (e.g. the shared SQLite connection was busy) never
		// leaves behind a manifest claiming the executor is registered when
		// it isn't — that mismatch is what made every retry re-detect
		// "already installed" and silently repeat the same failed save.
		err := m.app.Store.SaveExecutor(context.Background(), pending.cfg)
		if err != nil {
			return savedMsg{kind: "executor", id: pending.cfg.ID, err: fmt.Errorf("save %q: %w", pending.cfg.Name, err)}
		}
		if pending.installDirs != nil {
			if err := executor.WriteManifest(*pending.installDirs, executor.Manifest{
				ID: pending.cfg.ID, Name: pending.cfg.Name, Slug: filepath.Base(pending.installDirs.Root),
				Command: pending.cfg.Command, BinaryName: pending.cfg.BinaryName,
				InstallCmd: pending.installLine, InstalledAt: time.Now().UTC(),
			}); err != nil {
				return savedMsg{kind: "executor", id: pending.cfg.ID,
					err: fmt.Errorf("%q was registered but its manifest failed to write: %w", pending.cfg.Name, err)}
			}
		}
		return savedMsg{kind: "executor", id: pending.cfg.ID}
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
		case "automation":
			return m, func() tea.Msg {
				err := m.app.AutomationScheduler.Delete(req.id)
				return savedMsg{kind: "automation", id: req.id, deleted: true, err: err}
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
		case "update":
			if m.availableUpdate != nil {
				rel := *m.availableUpdate
				m.isUpdating = true
				m.status = fmt.Sprintf("Downloading and applying update %s...", rel.TagName)
				m.toastMessage = fmt.Sprintf("Downloading update %s...", rel.TagName)
				m.toastExpires = time.Now().Add(30 * time.Second)
				return m, m.performSelfUpdateCmd(rel)
			}
		case "factory-reset-warn":
			m.form = newFactoryResetForm()
			return m, nil
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
	case "Remote":
		if m.remoteController != nil {
			m.status = fmt.Sprintf("Controlling %s • press d to return to this computer", m.remoteDevice.Name)
			return m, nil
		}
		devices := m.app.RemoteDevices()
		if m.selected < 0 || m.selected >= len(devices) {
			m.status = "No online SessionHub found yet — both apps must be open on the LAN or Tailscale"
			return m, nil
		}
		device := devices[m.selected]
		if !remote.SameVersion(m.app.Version, device.Version) {
			m.status = fmt.Sprintf("Can't connect to %s: it runs v%s, while this SessionHub runs v%s", device.Name, device.Version, m.app.Version)
			return m, nil
		}
		m.status = "Connecting to " + device.Name + "..."
		return m, m.connectRemote(device)
	case "Settings":
		if m.availableUpdate != nil {
			currentVer := "unknown"
			if m.app != nil {
				currentVer = m.app.Version
			}
			m.confirm = &confirmRequest{
				kind:    "update",
				name:    m.availableUpdate.TagName,
				message: fmt.Sprintf("Update Session Hub from %s to %s?", currentVer, m.availableUpdate.TagName),
			}
			return m, nil
		}
		m.isCheckingUpdate = true
		m.status = "Checking for updates..."
		m.toastMessage = "Checking for updates..."
		m.toastExpires = time.Now().Add(5 * time.Second)
		return m, m.checkUpdateCmd()
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
			m.status = "terminal focused • f12 returns to Hub"
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
	if m.remoteController != nil {
		width, height := m.terminalSize()
		controller := m.remoteController
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			instance, err := controller.StartTerminal(ctx, session.ID, cfg.ID, width, height)
			return remoteStartedMsg{instance: instance, cfg: cfg, err: err}
		}
	}
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
			m.status = fmt.Sprintf("%q focused • f12 returns to Hub", cfg.Name)
			m.resize()
			return m, nil
		}
	}
	if live, instance, ok := m.app.Executors.FindActive(context.Background(), session.ID, cfg.ID); ok {
		if m.tabInstances == nil {
			m.tabInstances = make(map[string]string)
		}
		m.tabInstances[key] = instance.ID
		localOwner := terminal.Owner{Kind: "local", ID: "operator"}
		if live.Owner().Empty() {
			_ = live.Acquire(localOwner)
		}
		m.activeTerminal, m.activeInstance = live, instance
		m.focus, m.scrollOffset = true, 0
		m.status = fmt.Sprintf("%q focused • f12 returns to Hub", cfg.Name)
		m.resize()
		return m, nil
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
	if m.remoteController != nil && m.focus && m.activeInstance.ID != "" {
		controller, instanceID := m.remoteController, m.activeInstance.ID
		go func() { _ = controller.Resize(context.Background(), instanceID, width, height) }()
	}
}

func (m Model) connectRemote(device remote.Device) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		name, _ := os.Hostname()
		controller, err := remote.ConnectController(ctx, device, name, m.app.Version)
		if err != nil {
			return remoteConnectedMsg{device: device, err: err}
		}
		sessions, err := controller.Sessions(ctx)
		if err != nil {
			controller.Close()
			return remoteConnectedMsg{device: device, err: err}
		}
		executors, err := controller.Executors(ctx)
		if err != nil {
			controller.Close()
			return remoteConnectedMsg{device: device, err: err}
		}
		statuses, err := controller.ExecutorStatuses(ctx)
		if err != nil {
			controller.Close()
			return remoteConnectedMsg{device: device, err: err}
		}
		return remoteConnectedMsg{controller: controller, device: controller.Device(), sessions: sessions, executors: executors, statuses: statuses}
	}
}

func (m Model) isRemotelyControlled() bool {
	if m.remoteController != nil || m.app == nil {
		return false
	}
	return m.app.RemoteHostStatus().Active
}

// updateRemoteControlled is intentionally tiny: the screen still receives
// size/tick messages so it can mirror the controller, but local mouse and
// keyboard input cannot affect sessions, CLIs or configuration.
func (m Model) updateRemoteControlled(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
	case tickMsg:
		m.applyRemoteNavigation(m.app.RemoteHostStatus().View)
		return m, tick()
	case tea.KeyPressMsg:
		if msg.String() == "r" {
			application := m.app
			m.status = "Revoking remote access..."
			return m, func() tea.Msg { return remoteRevokedMsg{revoked: application.RevokeRemoteControl()} }
		}
	case remoteRevokedMsg:
		if msg.revoked {
			m.status = "Remote access revoked"
		} else {
			m.status = "No remote controller was active"
		}
	}
	return m, nil
}

func (m Model) remoteViewState() remote.ViewState {
	view := remote.ViewState{Section: sections[m.section], TerminalFocused: m.focus}
	if m.activeSession >= 0 && m.activeSession < len(m.sessions) {
		view.SessionID = m.sessions[m.activeSession].ID
	}
	if m.focus && m.activeInstance.ExecutorID != "" {
		view.ExecutorID = m.activeInstance.ExecutorID
	}
	return view
}

// syncRemoteNavigation sends only meaningful navigation changes. The host
// applies this view state locally and displays it beneath its control lock.
func (m *Model) syncRemoteNavigation() tea.Cmd {
	if m.remoteController == nil {
		return nil
	}
	view := m.remoteViewState()
	if view == m.remoteLastNavigation {
		return nil
	}
	m.remoteLastNavigation = view
	controller := m.remoteController
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return remoteNavigationMsg{err: controller.Navigate(ctx, view)}
	}
}

// applyRemoteNavigation only changes presentation state. It must never
// acquire a terminal lease or start an executor on the controlled machine.
func (m *Model) applyRemoteNavigation(view remote.ViewState) {
	if view.Section != "" {
		for index, section := range sections {
			if section == view.Section {
				m.section = index
				m.selected = 0
				break
			}
		}
	}
	if view.SessionID != "" {
		for index, session := range m.sessions {
			if session.ID == view.SessionID {
				m.activeSession = index
				break
			}
		}
	}
	m.focus = false
	if !view.TerminalFocused || view.ExecutorID == "" || m.activeSession < 0 || m.activeSession >= len(m.sessions) || m.app == nil {
		return
	}
	session := m.sessions[m.activeSession]
	live, instance, ok := m.app.Executors.FindActive(context.Background(), session.ID, view.ExecutorID)
	if !ok {
		return
	}
	if m.tabInstances == nil {
		m.tabInstances = make(map[string]string)
	}
	m.tabInstances[tabKeyFor(session.ID, view.ExecutorID)] = instance.ID
	m.activeTerminal, m.activeInstance = live, instance
	// The terminal stays blurred while the lock is up; renderCenter still
	// shows its current output behind the modal, with no local input route.
	m.focus = true
}

func (m Model) updateRemoteTerminal(message tea.Msg) (Model, tea.Cmd, bool) {
	if m.remoteController == nil || m.activeInstance.ID == "" {
		return m, nil, false
	}
	controller, instanceID := m.remoteController, m.activeInstance.ID
	sendKey := func(key uv.Key, release bool) tea.Cmd {
		return func() tea.Msg {
			err := controller.SendKey(context.Background(), instanceID, key, release)
			return remoteInputMsg{err: err}
		}
	}
	switch msg := message.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "f12" {
			m.focus = false
			m.status = "REMOTE CONTROL • hub mode • d in Remote returns locally"
			return m, nil, true
		}
		return m, sendKey(uv.Key(msg.Key()), false), true
	case tea.KeyReleaseMsg:
		return m, sendKey(uv.Key(msg.Key()), true), true
	case tea.PasteMsg:
		text := msg.Content
		return m, func() tea.Msg { return remoteInputMsg{err: controller.Paste(context.Background(), instanceID, text)} }, true
	}
	return m, nil, false
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
	case "Automations":
		return len(m.app.AutomationScheduler.List())
	case "Remote":
		if m.remoteController != nil {
			return 1
		}
		return len(m.app.RemoteDevices())
	default:
		return 1
	}
}

func (m Model) selectedAutomation() (automation.SimpleAutomation, bool) {
	items := m.app.AutomationScheduler.List()
	if m.selected < 0 || m.selected >= len(items) {
		return automation.SimpleAutomation{}, false
	}
	return items[m.selected], true
}

func (m Model) sessionName(sessionID string) string {
	for _, session := range m.sessions {
		if session.ID == sessionID {
			return session.Name
		}
	}
	return "missing session"
}

func (m Model) automationSessionChoices() string {
	if len(m.sessions) == 0 {
		return "(none)"
	}
	choices := make([]string, 0, len(m.sessions))
	for _, session := range m.sessions {
		choices = append(choices, session.Name+" ("+session.ID+")")
	}
	return strings.Join(choices, ", ")
}

func (m Model) automationExecutorChoices() string {
	if len(m.executors) == 0 {
		return "(none)"
	}
	choices := make([]string, 0, len(m.executors))
	for _, cfg := range m.executors {
		choices = append(choices, cfg.Name+" ("+cfg.ID+")")
	}
	return strings.Join(choices, ", ")
}

func (m Model) View() tea.View {
	content := m.render()
	view := tea.NewView(content)
	view.AltScreen = true
	// Cell motion keeps sidebar/tab clicks working without enabling terminal
	// hover reporting. The latter can make Windows Terminal hide the pointer
	// and render stray mouse escape sequences after a screen refresh.
	view.MouseMode = tea.MouseModeCellMotion
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
	if m.isRemotelyControlled() {
		content = overlayModal(content, m.renderRemoteControlledPanel(), m.width, m.height)
	} else if m.form.kind != noForm {
		content = overlayModal(content, m.renderFormPanel(), m.width, m.height)
	} else if m.confirm != nil {
		content = overlayModal(content, m.renderConfirmPanel(), m.width, m.height)
	}
	if toast := m.renderToast(); toast != "" {
		content = overlayToast(content, toast, m.width, m.height)
	}
	if background := m.remoteBackground(); background != "" {
		content = lipgloss.NewStyle().Background(lipgloss.Color(background)).Width(max(0, m.width)).Height(max(0, m.height)).Render(content)
	}
	return content
}

func (m Model) renderRemoteControlledPanel() string {
	status := m.app.RemoteHostStatus()
	controller := status.Controller
	if controller == "" {
		controller = "another device"
	}
	viewing := sections[m.section]
	if m.activeSession >= 0 && m.activeSession < len(m.sessions) {
		viewing += " • " + m.sessions[m.activeSession].Name
	}
	if m.activeInstance.ExecutorID != "" {
		viewing += " • " + m.executorName(m.activeInstance.ExecutorID)
	}
	var body strings.Builder
	body.WriteString(titleStyle.Render("REMOTE CONTROL ACTIVE") + "\n\n")
	body.WriteString(fmt.Sprintf("%s is controlling this SessionHub.\n", controller))
	body.WriteString(mutedStyle.Render("This screen mirrors their navigation and local input is locked.") + "\n\n")
	body.WriteString("Viewing: " + viewing + "\n\n")
	body.WriteString(keyStyle.Render("[r Revoke access]") + "\n")
	body.WriteString(mutedStyle.Render("Disconnects the controller and restores local control."))
	return modalStyle.Width(min(72, max(40, m.width-8))).Render(body.String())
}

func (m Model) remoteBackground() string {
	if m.remoteController != nil {
		return "#10251F"
	} // controller: green
	if m.app != nil {
		if status := m.app.RemoteHostStatus(); status.Active {
			return "#2A1C13"
		}
	}
	return ""
}

func (m Model) remoteBanner() string {
	if m.remoteController != nil {
		return " REMOTE CONTROL • controlling " + m.remoteDevice.Name + " • press d in Remote to return local "
	}
	if m.app != nil {
		if status := m.app.RemoteHostStatus(); status.Active {
			return " REMOTE CONTROLLED • " + status.Controller + " is operating this SessionHub "
		}
	}
	return ""
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
		if m.remoteController != nil {
			if instanceID, ok := m.remoteTabInstances[key]; ok && instanceID != "" {
				marker = "●"
				running = true
			}
		} else if instanceID, ok := m.tabInstances[key]; ok {
			if _, alive := m.app.Terminals.Get(instanceID); alive {
				marker = "●"
				running = true
			}
		}
		// Automation can activate a lazy tab while the user is elsewhere in
		// the Hub, so there is no startedMsg to populate tabInstances in this
		// UI model. Ask the executor service's in-memory PTY registry as well;
		// this keeps the online dot truthful without running database work on
		// every screen refresh.
		if !running && m.remoteController == nil && m.app != nil && m.app.Executors != nil && m.app.Executors.IsActive(session.ID, cfg.ID) {
			marker, running = "●", true
		}
		label := tabLabel(i, marker, cfg.Name)
		style := tabStyle
		activeID := m.tabInstances[key]
		if m.remoteController != nil {
			activeID = m.remoteTabInstances[key]
		}
		if m.focus && running && activeID == m.activeInstance.ID {
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

func (m Model) renderConfirmPanel() string {
	var b strings.Builder
	title := "Confirm delete"
	if m.confirm.kind == "update" {
		title = "✨ Update Session Hub"
	}
	if m.confirm.kind == "factory-reset-warn" {
		title = "⚠ FACTORY RESET"
	}
	b.WriteString(errorStyle.Render(title) + "\n\n")
	b.WriteString(m.confirm.message + "\n\n")
	b.WriteString(mutedStyle.Render("y confirms • esc/n cancels"))
	return modalStyle.Width(min(72, max(40, m.width-8))).Render(b.String())
}

func overlayModal(base string, modalPanel string, width, height int) string {
	if modalPanel == "" {
		return base
	}
	baseLines := strings.Split(base, "\n")
	if len(baseLines) == 0 {
		return base
	}
	for len(baseLines) < height {
		baseLines = append(baseLines, strings.Repeat(" ", max(0, width)))
	}

	modalLines := strings.Split(modalPanel, "\n")
	modalHeight := len(modalLines)
	if modalHeight == 0 {
		return base
	}

	topLimit := 1
	bottomLimit := max(0, height-2)
	if bottomLimit < topLimit {
		topLimit = 0
		bottomLimit = max(0, height-1)
	}

	availableRows := bottomLimit - topLimit + 1
	startY := topLimit + (availableRows-modalHeight)/2
	if startY < topLimit {
		startY = topLimit
	}

	modalWidth := 0
	for _, l := range modalLines {
		if w := ansi.StringWidth(l); w > modalWidth {
			modalWidth = w
		}
	}

	startX := (width - modalWidth) / 2
	if startX < 0 {
		startX = 0
	}

	for i, mLine := range modalLines {
		y := startY + i
		if y > bottomLimit || y >= len(baseLines) {
			break
		}
		baseLines[y] = overlayLine(baseLines[y], mLine, startX, width)
	}

	return strings.Join(baseLines, "\n")
}

func (m Model) renderTop() string {
	session, workspace, executor, state := "No session", "", "No Executor", "idle"
	if m.activeSession >= 0 && m.activeSession < len(m.sessions) {
		session = m.sessions[m.activeSession].Name
		workspace = filepath.Base(m.sessions[m.activeSession].Workspace)
	}
	if m.activeTerminal != nil {
		executor, state = m.executorName(m.activeInstance.ExecutorID), string(m.activeTerminal.State())
	} else if m.remoteController != nil && m.activeInstance.ExecutorID != "" {
		executor = m.executorName(m.activeInstance.ExecutorID)
		_, remoteState := m.remoteController.Snapshot(m.activeInstance.ID)
		if remoteState != "" {
			state = string(remoteState)
		}
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
	if banner := m.remoteBanner(); banner != "" {
		text += banner
	}
	if m.isUpdating {
		text += "  ⏳ UPDATING... "
	} else if m.availableUpdate != nil {
		text += fmt.Sprintf("  ✨ UPDATE %s AVAILABLE (press 'u') ", m.availableUpdate.TagName)
	}
	// Style.Width() word-wraps content that's too long rather than
	// truncating it — left unbounded, a long session/workspace/branch combo
	// would wrap the top bar onto a second line and silently shift every
	// row below it (including the tab bar's hardcoded y==1 click target).
	if start, _, ok := m.voiceButtonBounds(); ok {
		text = truncate(text, start)
		button := m.voiceButtonStyle().Render(m.voiceButtonLabel())
		return m.topBarStyle().Width(start).Render(text) + button
	}
	text = truncate(text, max(0, m.width))
	return m.topBarStyle().Width(max(0, m.width)).Render(text)
}

func (m Model) topBarStyle() lipgloss.Style {
	if m.remoteController != nil {
		return topStyle.Background(lipgloss.Color("#1D6A4F"))
	}
	if m.app != nil {
		if status := m.app.RemoteHostStatus(); status.Active {
			return topStyle.Background(lipgloss.Color("#A25724"))
		}
	}
	return topStyle
}

func (m Model) voiceButtonStyle() lipgloss.Style {
	if m.recording {
		return voiceButtonRecordingStyle
	}
	if m.voiceBusy {
		return voiceButtonBusyStyle
	}
	return voiceButtonStyle
}

func (m Model) renderCenter() string {
	width, height := m.terminalSize()
	// Only show the terminal snapshot while actually focused on it — once
	// you leave with f12, the Hub's normal section content should be
	// back, even though the (possibly finished) process is still tracked
	// in m.activeTerminal for the status bar/top bar.
	if m.focus && m.remoteController != nil && m.activeInstance.ID != "" {
		content, _ := m.remoteController.Snapshot(m.activeInstance.ID)
		if content == "" {
			content = mutedStyle.Render("Connecting to remote terminal...")
		}
		m.viewport.SetWidth(width)
		m.viewport.SetHeight(height)
		m.viewport.SetContent(content)
	} else if m.focus && m.activeTerminal != nil {
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
	if m.app == nil || m.app.Terminals == nil {
		return false
	}
	if term, ok := m.app.Terminals.Get(instance.ID); ok {
		return term.State() == domain.StateRunning
	}
	return false
}

func (m Model) findLiveTerminal(executorID string) (*terminal.Session, domain.Instance, bool) {
	if m.app == nil || m.app.Terminals == nil {
		return nil, domain.Instance{}, false
	}
	if m.activeSession >= 0 && m.activeSession < len(m.sessions) {
		session := m.sessions[m.activeSession]
		key := tabKeyFor(session.ID, executorID)
		if instanceID, ok := m.tabInstances[key]; ok {
			if term, alive := m.app.Terminals.Get(instanceID); alive && term.State() == domain.StateRunning {
				for _, inst := range m.instances {
					if inst.ID == instanceID {
						return term, inst, true
					}
				}
				return term, domain.Instance{ID: instanceID, SessionID: session.ID, ExecutorID: executorID, State: domain.StateRunning}, true
			}
		}
	}
	for _, instance := range m.instances {
		if instance.ExecutorID == executorID {
			if term, alive := m.app.Terminals.Get(instance.ID); alive && term.State() == domain.StateRunning {
				return term, instance, true
			}
		}
	}
	return nil, domain.Instance{}, false
}

// sidebarLayout is the single source of truth for what appears in the
// sidebar and in what order — renderSidebar draws it, sidebarRowAt maps a
// clicked screen row back into it, so the two can never drift apart.
func (m Model) sidebarLayout() []sidebarRow {
	rows := make([]sidebarRow, 0, len(sections)+len(m.sessions)+len(m.executors)+4)
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
			_, _, live := m.findLiveTerminal(cfg.ID)
			marker := ""
			if live {
				marker = "● "
			} else if m.remoteController != nil {
				if status, ok := m.remoteExecutorStatus[cfg.ID]; ok {
					if status.Live || status.Activated || !status.LoginKnown {
						marker = "● "
					} else {
						marker = "○ "
					}
				}
			} else if cfg.InstallDir != "" {
				marker = "○ "
				if executorActivated(cfg) {
					marker = "● "
				}
			}
			b.WriteString(sideItemStyle.Render(prefix+marker+truncate(cfg.Name, 18)) + "\n")
		}
	}
	_, height := m.terminalSize()
	return sidebarStyle.Width(25).Height(height).Render(b.String())
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
			if term, instance, live := m.findLiveTerminal(cfg.ID); live {
				localOwner := terminal.Owner{Kind: "local", ID: "operator"}
				if term.Owner().Empty() {
					_ = term.Acquire(localOwner)
				}
				m.activeTerminal, m.activeInstance = term, instance
				m.focus = true
				m.scrollOffset = 0
				m.status = fmt.Sprintf("%q focused • f12 returns to Hub", cfg.Name)
				m.resize()
				return m, nil
			}
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
	}
	return m, nil
}

func (m Model) emptyContent(width, height int) string {
	var body strings.Builder
	body.WriteString(titleStyle.Render(sections[m.section]) + "\n\n")
	switch sections[m.section] {
	case "Sessions":
		if len(m.sessions) == 0 {
			body.WriteString("No sessions configured yet.\n\nPress " + keyStyle.Render("n") + " to create a new session (workspace + grouped CLI tabs).")
		} else {
			for i, session := range m.sessions {
				prefix := "  "
				titleSt := sideItemStyle
				if i == m.selected {
					prefix = "› "
					titleSt = sideActiveStyle
				}
				names := executorNamesForIDs(session.ExecutorIDs(), m.executors)
				executorList := "no Executors assigned"
				if len(names) > 0 {
					executorList = strings.Join(names, ", ")
				}
				body.WriteString(fmt.Sprintf("%s%s\n", prefix, titleSt.Render(session.Name)))
				body.WriteString(fmt.Sprintf("    %s %s\n    %s %s\n\n",
					mutedStyle.Render("Workspace:"), shortenPath(session.Workspace),
					mutedStyle.Render("Executors:"), executorList))
			}
			body.WriteString("\n" + titleStyle.Render("Shortcuts") + "\n")
			body.WriteString("  " + keyStyle.Render("Enter") + mutedStyle.Render(" Activate") + "   " +
				keyStyle.Render("n") + mutedStyle.Render(" New Session") + "   " +
				keyStyle.Render("e") + mutedStyle.Render(" Edit") + "   " +
				keyStyle.Render("d") + mutedStyle.Render(" Delete"))
		}
	case "Executors":
		if len(m.executors) == 0 {
			body.WriteString("No Executors registered.\n\nPress " + keyStyle.Render("i") + " to add a CLI (Codex, Claude Code, OpenCode, Antigravity, Custom) or " + keyStyle.Render("s") + " to scan disk.")
		} else {
			for i, cfg := range m.executors {
				prefix := "  "
				itemTitleStyle := sideItemStyle
				if i == m.selected {
					prefix = "› "
					itemTitleStyle = sideActiveStyle
				}
				status := m.executorStatusLabel(cfg)
				if status != "" {
					status = "  " + status
				}
				cmdName := filepath.Base(cfg.Command)
				argsStr := strings.Join(cfg.Args, " ")
				if argsStr != "" {
					cmdName += " " + argsStr
				}
				displayPath := shortenPath(cfg.Command)

				body.WriteString(fmt.Sprintf("%s%s%s\n", prefix, itemTitleStyle.Render(cfg.Name), status))
				if displayPath != "" && displayPath != cmdName {
					body.WriteString(fmt.Sprintf("    %s %s  %s\n\n",
						mutedStyle.Render("Command:"), keyStyle.Render(cmdName),
						mutedStyle.Render("("+displayPath+")")))
				} else {
					body.WriteString(fmt.Sprintf("    %s %s\n\n",
						mutedStyle.Render("Command:"), keyStyle.Render(cmdName)))
				}
			}
			body.WriteString("\n" + titleStyle.Render("Shortcuts") + "\n")
			body.WriteString("  " + keyStyle.Render("i") + mutedStyle.Render(" Add CLI") + "   " +
				keyStyle.Render("e") + mutedStyle.Render(" Edit") + "   " +
				keyStyle.Render("u") + mutedStyle.Render(" Update") + "   " +
				keyStyle.Render("x") + mutedStyle.Render(" Reset Login") + "   " +
				keyStyle.Render("d") + mutedStyle.Render(" Delete") + "\n")
			body.WriteString("  " + keyStyle.Render("s") + mutedStyle.Render(" Scan Disk") + "   " +
				keyStyle.Render("n") + mutedStyle.Render(" Custom") + "   " +
				keyStyle.Render("ctrl+t") + mutedStyle.Render(" Test PTY") + "   " +
				mutedStyle.Render("(Add to a Session tab to use)"))
		}
	case "Queues":
		body.WriteString("Prompt queues are persisted and idempotent.\n\nPress n to add an item. Completion requires an Executor rule or manual confirmation.")
	case "Pipelines":
		body.WriteString("Pipelines support prompt, deterministic command, approval, condition, parallel, and consolidation steps.\n\nDependencies, retries, workspace locks, and budgets are enforced by the engine.")
	case "Automations":
		body.WriteString(mutedStyle.Render("Automations run only while SessionHub is open.") + "\n\n")
		items := m.app.AutomationScheduler.List()
		if len(items) == 0 {
			body.WriteString("No automations configured yet.\n\nPress " + keyStyle.Render("n") + " to create one.")
		} else {
			for i, item := range items {
				prefix, style := "  ", sideItemStyle
				if i == m.selected {
					prefix, style = "› ", sideActiveStyle
				}
				next := "—"
				if item.NextRun != nil {
					next = item.NextRun.In(time.Local).Format("2006-01-02 15:04")
				}
				last := string(item.LastRun.Status)
				if last == "" {
					last = "—"
				}
				enabled := "disabled"
				if item.Enabled {
					enabled = "enabled"
				}
				body.WriteString(fmt.Sprintf("%s%s  %s\n", prefix, style.Render(item.Name), mutedStyle.Render("["+enabled+"]")))
				body.WriteString(fmt.Sprintf("    Session: %s  •  %s  •  Next: %s  •  %d step(s)\n", m.sessionName(item.SessionID), item.Schedule.Type, next, len(item.Steps)))
				if item.Status == automation.StatusRunning && item.CurrentStep > 0 {
					executorName := ""
					if item.CurrentStep <= len(item.Steps) {
						executorName = m.executorName(item.Steps[item.CurrentStep-1].ExecutorID)
					}
					body.WriteString(fmt.Sprintf("    %s\n", keyStyle.Render(fmt.Sprintf("Running step %d of %d • Executor: %s", item.CurrentStep, len(item.Steps), executorName))))
					if item.LastRun.Error != "" {
						body.WriteString(fmt.Sprintf("    %s\n", mutedStyle.Render(truncate(item.LastRun.Error, max(20, width-12)))))
					}
					if item.Activity != "" {
						body.WriteString(fmt.Sprintf("    %s\n", mutedStyle.Render(item.Activity)))
					}
					if item.LiveOutput != "" {
						body.WriteString("    " + mutedStyle.Render("Live output is available in History / Details.") + "\n")
					}
				} else {
					body.WriteString(fmt.Sprintf("    Status: %s  •  Last run: %s\n", item.Status, last))
				}
				body.WriteString("\n")
			}
		}
		body.WriteString("\n" + titleStyle.Render("Actions") + "\n")
		body.WriteString("  " + keyStyle.Render("[n New Automation]") + "  " +
			keyStyle.Render("[r Run Now]") + "  " + keyStyle.Render("[e Edit]") + "  " +
			keyStyle.Render("[c Cancel]") + "  " + keyStyle.Render("[l History / Details]") + "  " +
			keyStyle.Render("[d Delete]"))
	case "Metrics":
		body.WriteString(fmt.Sprintf("Input Tokens     %d\nOutput Tokens    %d\nTotal Tokens     %d\nCache Read       %d\nCache Write      %d\nDuration         %s\n\nCusto equivalente estimado em API: US$ %.6f",
			m.metrics.InputTokens, m.metrics.OutputTokens, m.metrics.TotalTokens(),
			m.metrics.CacheRead, m.metrics.CacheWrite,
			time.Duration(m.metrics.Duration)*time.Millisecond,
			float64(m.metrics.CostMicrosUSD)/1_000_000))
	case "Logs":
		body.WriteString("Audit logs retain state transitions, terminal events, automation effects, approvals, errors, and metrics.\n\nSecret environment values are redacted.")
	case "Remote":
		if m.remoteController != nil {
			body.WriteString(keyStyle.Render("REMOTE CONTROL ACTIVE") + "\n\n")
			body.WriteString(fmt.Sprintf("You are controlling %s over %s. Its sessions and CLI tabs are now shown throughout the Hub.\n\n", m.remoteDevice.Name, m.remoteDevice.Network))
			body.WriteString(mutedStyle.Render("The controlled SessionHub is amber and shows an explicit control notice. Only this pair is connected; other discovered devices remain available."))
			body.WriteString("\n\n" + keyStyle.Render("[d Return to local environment]"))
			break
		}
		body.WriteString(mutedStyle.Render("Online SessionHubs are discovered automatically on your local network and Tailscale. Remote control is always a one-to-one connection.\n\n"))
		devices := m.app.RemoteDevices()
		if len(devices) == 0 {
			body.WriteString("No other SessionHub is online yet. Open SessionHub on another computer in the same LAN or tailnet.\n\n")
			body.WriteString(mutedStyle.Render("Discovery refreshes automatically."))
		} else {
			for i, device := range devices {
				prefix, style := "  ", sideItemStyle
				if i == m.selected {
					prefix, style = "› ", sideActiveStyle
				}
				version := "v" + strings.TrimPrefix(device.Version, "v")
				if !remote.SameVersion(m.app.Version, device.Version) {
					body.WriteString(fmt.Sprintf("%s%s  %s\n", prefix, mutedStyle.Render("● "+device.Name), errorStyle.Render("incompatible • "+version+" required: v"+strings.TrimPrefix(m.app.Version, "v"))))
					continue
				}
				body.WriteString(fmt.Sprintf("%s%s  %s\n", prefix, style.Render("● "+device.Name), mutedStyle.Render("online • "+version+" • "+device.Network+" • "+device.Address)))
			}
			body.WriteString("\n" + keyStyle.Render("Enter Connect") + mutedStyle.Render(" • exact same version required • only one controller can connect to a SessionHub at a time"))
		}
	case "Settings":
		ver := ""
		if m.app != nil {
			ver = m.app.Version
		}
		if ver != "" && !strings.HasPrefix(ver, "v") {
			ver = "v" + ver
		}
		body.WriteString(titleStyle.Render("System") + "\n")
		body.WriteString(fmt.Sprintf("  Version      %s\n", keyStyle.Render(ver)))
		body.WriteString(fmt.Sprintf("  Data root    %s\n", mutedStyle.Render(m.app.Paths.Root)))
		body.WriteString(fmt.Sprintf("  Database     %s\n", mutedStyle.Render(m.app.Paths.Database)))
		body.WriteString(fmt.Sprintf("  Recovered    %d records\n\n", m.app.RecoveredCount()))

		network := m.app.RemoteNetworkStatus()
		body.WriteString(titleStyle.Render("Remote Network Control") + "\n")
		remoteState := "DISABLED"
		remoteDetail := "host and discovery are stopped"
		if network.RemoteEnabled {
			remoteState = "ENABLED"
			remoteDetail = "host + discovery are active"
			if !network.HostActive || !network.DiscoveryActive {
				remoteDetail = "starting or unavailable — press p to restart"
			}
		}
		body.WriteString("  " + keyStyle.Render("[r]") + " Remote Mode       " + keyStyle.Render(remoteState) + "  " + mutedStyle.Render(remoteDetail) + "\n")
		tailscaleState := "DISABLED"
		tailscaleDetail := "not advertised over Tailscale"
		if network.TailscaleEnabled {
			tailscaleState = "ENABLED"
			tailscaleDetail = "advertise and discover Tailscale peers"
		}
		body.WriteString("  " + keyStyle.Render("[t]") + " Tailscale         " + keyStyle.Render(tailscaleState) + "  " + mutedStyle.Render(tailscaleDetail) + "\n")
		body.WriteString("  " + keyStyle.Render("[p]") + " Restart network   " + mutedStyle.Render("rebind host and re-announce after Wi-Fi/VPN changes") + "\n\n")

		body.WriteString(mutedStyle.Render("Transport status") + "\n")
		localIPs := strings.Join(network.LocalIPs, ", ")
		if localIPs == "" {
			localIPs = "waiting for a private LAN address"
		}
		lanState := "OFFLINE"
		if network.RemoteEnabled && network.HostActive && network.DiscoveryActive {
			lanState = "ONLINE"
		}
		body.WriteString("  LAN        " + keyStyle.Render(lanState) + "  " + localIPs + "\n")
		if network.TailscaleDetected {
			status := "OFFLINE"
			if network.RemoteEnabled && network.TailscaleEnabled && network.HostActive && network.DiscoveryActive {
				status = "ONLINE"
			}
			body.WriteString("  Tailscale  " + keyStyle.Render(status) + "  " + strings.Join(network.TailscaleIPs, ", ") + "\n")
		} else {
			body.WriteString("  Tailscale  " + mutedStyle.Render("not detected on this computer") + "\n")
		}
		if network.Endpoint != "" {
			body.WriteString("  Host        " + mutedStyle.Render(network.Endpoint) + "\n")
		}
		body.WriteString("  Config      " + mutedStyle.Render(m.app.Paths.NetworkSettings) + "\n\n")

		body.WriteString(titleStyle.Render("Software Update") + "\n")
		if m.isUpdating {
			body.WriteString("  Status: ⏳ Downloading and applying update...\n")
		} else if m.isCheckingUpdate {
			body.WriteString("  Status: 🔍 Checking for updates on GitHub...\n")
		} else if m.availableUpdate != nil {
			body.WriteString(fmt.Sprintf("  Status: ✨ Update %s AVAILABLE!\n", m.availableUpdate.TagName))
			body.WriteString(fmt.Sprintf("  Release URL: %s\n", m.availableUpdate.HTMLURL))
			body.WriteString("\n  " + keyStyle.Render("[u] [Enter]") + mutedStyle.Render(" Install update now."))
		} else {
			body.WriteString("  Status: ✓ Session Hub is up to date\n")
			if !m.lastUpdateCheck.IsZero() {
				body.WriteString(fmt.Sprintf("  Last checked: %s\n", m.lastUpdateCheck.Format("15:04:05")))
			}
			body.WriteString("\n  " + keyStyle.Render("[u] [Enter]") + mutedStyle.Render(" Check for updates now."))
		}

		body.WriteString("\n\n" + titleStyle.Render("Danger Zone") + "\n")
		body.WriteString("  " + keyStyle.Render("[ctrl+r]") + mutedStyle.Render(" Factory reset — wipes sessions, executors, logins, logs and downloads.") + "\n")
		body.WriteString("  " + mutedStyle.Render("A y/n confirmation and typing "+strconv.Quote(factoryResetPhrase)+" are both required."))
	}
	return contentStyle.Width(width).Height(height).Render(body.String())
}

func (m Model) renderBottom() string {
	message := m.status
	if m.lastErr != "" {
		message = "Error: " + m.lastErr
	} else {
		if m.isUpdating {
			message = "Downloading and applying software update..."
		} else if m.isCheckingUpdate {
			message = "Checking for updates on GitHub..."
		}
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
		[]string{"Defaults to workspace folder name", "Absolute or relative directory (required)"})
	form.executorChoices = executorChoicesFrom(executors, nil)
	return form
}

func editSessionForm(session domain.Session, executors []domain.ExecutorConfig) formModel {
	form := makeForm(sessionForm, "Edit session: "+session.Name, sessionLabels,
		[]string{"Defaults to workspace folder name", "Absolute or relative directory (required)"})
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
	return executor.HasLoginState(cfg)
}

// executorStatusLabel renders the activated/deactivated marker for the
// Executors list, or "" for executors with no isolated config folder to
// check (manually registered).
func executorStatusLabel(cfg domain.ExecutorConfig) string {
	if cfg.InstallDir == "" {
		return ""
	}
	if executorActivated(cfg) {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#50FA7B")).Render("● activated")
	}
	return mutedStyle.Render("○ deactivated")
}

func (m Model) executorStatusLabel(cfg domain.ExecutorConfig) string {
	if m.remoteController != nil {
		status, ok := m.remoteExecutorStatus[cfg.ID]
		if !ok {
			return mutedStyle.Render("○ remote status unavailable")
		}
		if status.Live {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("#50FA7B")).Render("● active on remote")
		}
		if !status.LoginKnown {
			return mutedStyle.Render("● configured on remote")
		}
		if status.Activated {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("#50FA7B")).Render("● logged in on remote")
		}
		return mutedStyle.Render("○ login needed on remote")
	}
	return executorStatusLabel(cfg)
}

func shortenPath(path string) string {
	if path == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		if strings.HasPrefix(path, home) {
			path = "~" + strings.TrimPrefix(path, home)
		}
	}
	path = filepath.ToSlash(path)
	if len(path) > 55 {
		runes := []rune(path)
		return string(runes[:20]) + "..." + string(runes[len(runes)-30:])
	}
	return path
}

var executorCoreLabels = []string{
	"Display name", "Command", "Working directory",
}

var executorCorePlaceholders = []string{
	"My CLI", "executable and args, e.g. codex --yolo", "defaults to session workspace",
}

var executorAdvancedLabels = []string{
	"Environment", "Shell (optional)", "Resume command",
	"Recognition rules",
	"Timeout", "Prompt suffix", "Roles", "Model label",
	"Tokenizer", "Price ID",
}

var executorAdvancedPlaceholders = []string{
	"NAME=value; *SECRET=value (prefix with * for secret)", "shell executable", "executable and args, e.g. claude --continue",
	"name::kind::value::outcome ;; more rules (kinds: literal, pattern, prompt_return, stable_output, exit_code, process_exit, command_result, manual, timeout)",
	"30m or 0", `\r`, "comma, separated, roles", "metrics only", "unicode_words", "optional",
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
	"Display name", "Command", "Install command (only runs if not found)",
}

var installFormPlaceholders = []string{
	"Codex", "codex --yolo", `npm install @openai/codex`,
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
	form := makeForm(installForm, "Add Terminal / CLI — "+p.Name, installFormLabels, installFormPlaceholders)
	form.fields[0].SetValue(p.Name)
	form.fields[1].SetValue(shellJoinLine(p.Command, p.Args))
	if p.InstallCmd != nil {
		form.fields[2].SetValue(p.InstallCmd())
	} else {
		form.fields[2].SetValue("")
	}
	provider := p
	form.selectedProvider = &provider
	return form
}

// newProviderPickForm lists the verified catalog plus "Custom" as a
// single-select pick list (↑↓ moves, enter confirms) shown before the
// install form itself.
func newProviderPickForm() formModel {
	catalog := executor.CatalogForOS()
	names := make([]string, 0, len(catalog)+1)
	for _, p := range catalog {
		names = append(names, p.Name)
	}
	names = append(names, "Custom")
	return formModel{kind: providerPickForm, title: "Add Terminal / CLI — pick one", providerNames: names}
}

// editExecutorForm opens the same short (core-only) form as "new executor",
// prefilled from the existing config, so the common case (tweaking the
// command/args) isn't buried under ten rarely-used advanced fields. ctrl+a
// still reveals the advanced fields, prefilled too (see the ctrl+a handler).
// Fields that stay hidden are carried through from originalExecutor on save
// (see executorFromValues), so collapsing the form never silently drops data.
func editExecutorForm(cfg domain.ExecutorConfig) formModel {
	form := makeForm(executorForm, "Edit Executor: "+cfg.Name, executorCoreLabels, executorCorePlaceholders)
	form.editingID = cfg.ID
	form.originalExecutor = &cfg
	values := executorToValues(cfg)
	for i, label := range executorCoreLabels {
		if value, ok := values[label]; ok && value != "" {
			form.fields[i].SetValue(value)
		}
	}
	return form
}

func executorToValues(cfg domain.ExecutorConfig) map[string]string {
	values := map[string]string{
		"Display name":      cfg.Name,
		"Command":           shellJoinLine(cfg.Command, cfg.Args),
		"Working directory": cfg.WorkingDir,
		"Shell (optional)":  cfg.Shell,
		"Prompt suffix":     strings.NewReplacer("\r", `\r`, "\n", `\n`, "\t", `\t`).Replace(cfg.PromptSuffix),
		"Model label":       cfg.Model,
		"Tokenizer":         cfg.Tokenizer,
		"Price ID":          cfg.PriceID,
	}
	if cfg.Timeout > 0 {
		values["Timeout"] = cfg.Timeout.String()
	}
	if cfg.ResumeCommand != "" {
		values["Resume command"] = shellJoinLine(cfg.ResumeCommand, cfg.ResumeArgs)
	}
	if len(cfg.Environment) > 0 {
		values["Environment"] = formatEnvSpec(cfg.Environment)
	}
	if len(cfg.Rules) > 0 {
		values["Recognition rules"] = formatRules(cfg.Rules)
	}
	if len(cfg.Roles) > 0 {
		values["Roles"] = formatRoles(cfg.Roles)
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

func newAutomationForm(sessions []domain.Session, executors []domain.ExecutorConfig, activeSession int) formModel {
	form := makeForm(automationForm, "New Automation", []string{"Prompt", "Time"}, []string{"What should the executor do?", "14:00"})
	form.automationSessions = make([]automationChoice, len(sessions))
	for i, session := range sessions {
		form.automationSessions[i] = automationChoice{id: session.ID, name: session.Name, selected: i == activeSession || (activeSession < 0 && i == 0)}
	}
	if len(sessions) > 0 && !automationChoiceSelected(form.automationSessions) {
		form.automationSessions[0].selected = true
	}
	form.automationExecutors = make([]automationChoice, len(executors))
	for i, cfg := range executors {
		form.automationExecutors[i] = automationChoice{id: cfg.ID, name: cfg.Name, selected: i == 0}
	}
	form.fields[1].SetValue("14:00")
	form.fields[0].Blur()
	form.automationFocus = 0
	return form
}

func editAutomationForm(item automation.SimpleAutomation, sessions []domain.Session, executors []domain.ExecutorConfig) formModel {
	form := newAutomationForm(sessions, executors, -1)
	form.title, form.editingID = "Edit Automation: "+item.Name, item.ID
	for i := range form.automationSessions {
		form.automationSessions[i].selected = form.automationSessions[i].id == item.SessionID
	}
	for i := range form.automationExecutors {
		form.automationExecutors[i].selected = len(item.Steps) > 0 && form.automationExecutors[i].id == item.Steps[0].ExecutorID
	}
	switch item.Schedule.Type {
	case automation.ScheduleDaily:
		form.automationSchedule = 1
	case automation.ScheduleWeekly:
		form.automationSchedule = 2
	}
	for _, day := range item.Schedule.DaysOfWeek {
		form.automationDays[int(day)] = true
	}
	if len(item.Steps) > 0 {
		form.fields[0].SetValue(item.Steps[0].Prompt)
	}
	form.fields[1].SetValue(item.Schedule.Time)
	return form
}

func automationChoiceSelected(choices []automationChoice) bool {
	for _, choice := range choices {
		if choice.selected {
			return true
		}
	}
	return false
}

func automationDetailsForm(item automation.SimpleAutomation) formModel {
	last := item.LastRun
	started, finished := "—", "—"
	if last.StartedAt != nil {
		started = last.StartedAt.In(time.Local).Format("2006-01-02 15:04:05")
	}
	if last.FinishedAt != nil {
		finished = last.FinishedAt.In(time.Local).Format("2006-01-02 15:04:05")
	}
	duration := "—"
	if last.StartedAt != nil && last.FinishedAt != nil {
		duration = last.FinishedAt.Sub(*last.StartedAt).Round(time.Second).String()
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Status: %s\nTrigger: %s\nStarted: %s\nFinished: %s\nDuration: %s\n", last.Status, last.Trigger, started, finished, duration))
	if item.LiveOutput != "" {
		b.WriteString("\nLive terminal output:\n" + automation.SanitizeTerminalOutput(item.LiveOutput) + "\n")
	}
	if last.FailedStepID != "" {
		b.WriteString("Failed step: " + last.FailedStepID + "\n")
	}
	if last.Error != "" {
		b.WriteString("Error: " + last.Error + "\n")
	}
	if len(last.OutputPreview) > 0 {
		b.WriteString("\nOutput preview:\n")
		for i, preview := range last.OutputPreview {
			b.WriteString(fmt.Sprintf("\nStep %d:\n%s\n", i+1, truncate(preview, 900)))
		}
	}
	return formModel{kind: automationDetailsView, title: "Automation History: " + item.Name, details: b.String()}
}

func weekdayName(day time.Weekday) string {
	return []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}[int(day)]
}

func automationFromValues(values []string, editingID string) (automation.SimpleAutomation, error) {
	if len(values) != 8 {
		return automation.SimpleAutomation{}, errors.New("invalid automation form")
	}
	enabled, err := strconv.ParseBool(defaultString(values[6], "true"))
	if err != nil {
		return automation.SimpleAutomation{}, errors.New("enabled must be true or false")
	}
	schedule := automation.SimpleSchedule{
		Type: automation.SimpleScheduleType(strings.ToLower(strings.TrimSpace(values[2]))),
		Date: strings.TrimSpace(values[3]), Time: strings.TrimSpace(values[4]),
	}
	if schedule.Type == automation.ScheduleWeekly {
		for _, raw := range strings.Split(values[5], ",") {
			name := strings.ToLower(strings.TrimSpace(raw))
			if name == "" {
				continue
			}
			day, ok := map[string]time.Weekday{"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday, "thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday}[name]
			if !ok {
				return automation.SimpleAutomation{}, fmt.Errorf("unknown day %q (use mon,tue,wed,thu,fri,sat,sun)", name)
			}
			schedule.DaysOfWeek = append(schedule.DaysOfWeek, day)
		}
	}
	steps := make([]automation.SimpleStep, 0)
	for _, raw := range strings.Split(values[7], ";") {
		parts := strings.SplitN(raw, "|", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return automation.SimpleAutomation{}, errors.New("steps must use executor-id | prompt; executor-id | prompt")
		}
		steps = append(steps, automation.SimpleStep{ExecutorID: strings.TrimSpace(parts[0]), Prompt: strings.TrimSpace(parts[1])})
	}
	return automation.SimpleAutomation{ID: editingID, Name: strings.TrimSpace(values[0]), SessionID: strings.TrimSpace(values[1]), Enabled: enabled, Schedule: schedule, Steps: steps}, nil
}

func automationFromEditor(form formModel) (automation.SimpleAutomation, error) {
	selected := func(choices []automationChoice, label string) (automationChoice, error) {
		for _, choice := range choices {
			if choice.selected {
				return choice, nil
			}
		}
		return automationChoice{}, fmt.Errorf("select a %s", label)
	}
	session, err := selected(form.automationSessions, "session")
	if err != nil {
		return automation.SimpleAutomation{}, err
	}
	executorChoice, err := selected(form.automationExecutors, "executor")
	if err != nil {
		return automation.SimpleAutomation{}, err
	}
	prompt := strings.TrimSpace(form.fields[0].Value())
	if prompt == "" {
		return automation.SimpleAutomation{}, errors.New("prompt is required")
	}
	clock, err := time.Parse("15:04", strings.TrimSpace(form.fields[1].Value()))
	if err != nil {
		return automation.SimpleAutomation{}, errors.New("time must use HH:MM")
	}
	types := []automation.SimpleScheduleType{automation.ScheduleOnce, automation.ScheduleDaily, automation.ScheduleWeekly}
	if form.automationSchedule < 0 || form.automationSchedule >= len(types) {
		return automation.SimpleAutomation{}, errors.New("select a schedule type")
	}
	schedule := automation.SimpleSchedule{Type: types[form.automationSchedule], Time: clock.Format("15:04")}
	if schedule.Type == automation.ScheduleOnce {
		now := time.Now().In(time.Local)
		candidate := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, time.Local)
		if !candidate.After(now) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		schedule.Date = candidate.Format("2006-01-02")
	}
	if schedule.Type == automation.ScheduleWeekly {
		for day, checked := range form.automationDays {
			if checked {
				schedule.DaysOfWeek = append(schedule.DaysOfWeek, time.Weekday(day))
			}
		}
		if len(schedule.DaysOfWeek) == 0 {
			return automation.SimpleAutomation{}, errors.New("select at least one weekday")
		}
	}
	return automation.SimpleAutomation{ID: form.editingID, Name: truncate(prompt, 42), SessionID: session.id, Enabled: true,
		Schedule: schedule, Steps: []automation.SimpleStep{{ExecutorID: executorChoice.id, Prompt: prompt}}}, nil
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

// newFactoryResetForm builds step 3 of 3 for the factory reset flow: the
// operator must type factoryResetPhrase exactly, submitForm rejects anything
// else, so this last gate can't be crossed by a stray keypress.
func newFactoryResetForm() formModel {
	return makeForm(factoryResetForm,
		fmt.Sprintf("⚠ FACTORY RESET — step 3 of 3: type %q to wipe everything", factoryResetPhrase),
		[]string{"Confirmation phrase"}, []string{factoryResetPhrase})
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
		catalog := executor.CatalogForOS()
		if m.form.choiceCursor < len(catalog) {
			m.form = newInstallFormForProvider(catalog[m.form.choiceCursor])
		} else {
			m.form = newInstallForm()
		}
	}
	return m, nil
}

// updateAutomationEditor keeps the creation flow deliberately small. The
// selector sections use arrows/space; text input exists only for Prompt and
// Time, so operators never need to discover or paste internal IDs.
func (m Model) updateAutomationEditor(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	moveFocus := func(delta int) {
		if m.form.automationFocus == 4 || m.form.automationFocus == 5 {
			m.form.fields[m.form.automationFocus-4].Blur()
		}
		next := (m.form.automationFocus + delta + 6) % 6
		if next == 3 && m.form.automationSchedule != 2 {
			next = (next + delta + 6) % 6
		}
		m.form.automationFocus = next
		m.form.automationCursor = 0
		if m.form.automationFocus == 4 || m.form.automationFocus == 5 {
			m.form.fields[m.form.automationFocus-4].Focus()
		}
	}
	moveCursor := func(delta, count int) {
		if count > 0 {
			m.form.automationCursor = (m.form.automationCursor + delta + count) % count
		}
	}
	switch msg.String() {
	case "esc":
		m.form = formModel{}
		return m, nil
	case "ctrl+s":
		return m.submitForm()
	case "tab":
		moveFocus(1)
		return m, nil
	case "shift+tab":
		moveFocus(-1)
		return m, nil
	case "up", "k":
		switch m.form.automationFocus {
		case 0:
			moveCursor(-1, len(m.form.automationSessions))
		case 1:
			moveCursor(-1, len(m.form.automationExecutors))
		case 2:
			moveCursor(-1, 3)
		case 3:
			moveCursor(-1, 7)
		default:
			moveFocus(-1)
		}
		return m, nil
	case "down", "j":
		switch m.form.automationFocus {
		case 0:
			moveCursor(1, len(m.form.automationSessions))
		case 1:
			moveCursor(1, len(m.form.automationExecutors))
		case 2:
			moveCursor(1, 3)
		case 3:
			moveCursor(1, 7)
		default:
			moveFocus(1)
		}
		return m, nil
	case "left", "h":
		if m.form.automationFocus == 2 {
			moveCursor(-1, 3)
			return m, nil
		}
	case "right", "l":
		if m.form.automationFocus == 2 {
			moveCursor(1, 3)
			return m, nil
		}
	case " ", "enter":
		switch m.form.automationFocus {
		case 0:
			if len(m.form.automationSessions) > 0 {
				for i := range m.form.automationSessions {
					m.form.automationSessions[i].selected = i == m.form.automationCursor
				}
			}
		case 1:
			if len(m.form.automationExecutors) > 0 {
				for i := range m.form.automationExecutors {
					m.form.automationExecutors[i].selected = i == m.form.automationCursor
				}
			}
		case 2:
			m.form.automationSchedule = m.form.automationCursor
		case 3:
			m.form.automationDays[m.form.automationCursor] = !m.form.automationDays[m.form.automationCursor]
		default:
			moveFocus(1)
		}
		return m, nil
	}
	if m.form.automationFocus == 4 || m.form.automationFocus == 5 {
		idx := m.form.automationFocus - 4
		field, cmd := m.form.fields[idx].Update(msg)
		m.form.fields[idx] = field
		return m, cmd
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
			m.status = fmt.Sprintf("installing %q • f12 returns to Hub (auto-registers when it finishes)", msg.registerAfter.Name)
		} else {
			m.status = "test terminal focused • f12 returns to Hub"
		}
		m.resize()
		return m, nil
	case tea.KeyPressMsg:
		if m.form.kind == automationForm {
			return m.updateAutomationEditor(msg)
		}
		if m.form.kind == automationDetailsView {
			if msg.String() == "esc" || msg.String() == "enter" || msg.String() == "l" {
				m.form = formModel{}
			}
			return m, nil
		}
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
				var values map[string]string
				if m.form.originalExecutor != nil {
					values = executorToValues(*m.form.originalExecutor)
				}
				m.form.labels = append(m.form.labels, executorAdvancedLabels...)
				for i, placeholder := range executorAdvancedPlaceholders {
					field := textinput.New()
					field.Placeholder = placeholder
					field.SetWidth(70)
					if value, ok := values[executorAdvancedLabels[i]]; ok && value != "" {
						field.SetValue(value)
					}
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
		workspace = strings.Trim(workspace, `"'`)
		if workspace == "" {
			m.form.err = "workspace is required"
			return m, nil
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
		if name == "" {
			name = filepath.Base(absolute)
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
		if name == "" || strings.TrimSpace(values[1]) == "" {
			m.form.err = "display name and command are required"
			return m, nil
		}
		command, args, err := shellSplitLine(values[1])
		if err != nil {
			m.form.err = "invalid command: " + err.Error()
			return m, nil
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
		installLine := strings.TrimSpace(values[2])
		cfg := domain.ExecutorConfig{
			ID: id.New("exec"), Name: name, Command: command, BinaryName: command, Args: args,
			WorkingDir: workingDir, PromptSuffix: "\r", InstallDir: dirs.Root,
		}
		if provider := m.form.selectedProvider; provider != nil {
			if provider.UseHostHome {
				cfg.UseHostHome = true
			}
			if provider.ConfigEnvVar != "" {
				// Belt-and-suspenders alongside the universal HOME override:
				// some CLIs (especially native, non-Node binaries) only
				// reliably honor their own documented config-dir variable.
				cfg.Environment = append(cfg.Environment, domain.SecretEnv{
					Name: provider.ConfigEnvVar, Value: filepath.Join(dirs.Root, "config"),
				})
			}
		}
		var extraDirs []string
		if provider := m.form.selectedProvider; provider != nil && !provider.Isolated && provider.DefaultDirs != nil {
			// This provider's own installer doesn't support a custom
			// directory, so it always lands in its conventional location —
			// check there too instead of only the executor's own folder.
			extraDirs = provider.DefaultDirs()
		}
		if resolved, found := terminal.FindExecutable(command, dirs.Root, extraDirs...); found {
			// Already installed on system, PATH, standard default dirs, or executor folder:
			// register directly, no reinstall, no PTY needed.
			cfg.Command = resolved
			return m, func() tea.Msg {
				// Write the manifest only after the DB save actually commits
				// (see checkPendingRegister for why: a manifest written ahead
				// of a failed/lost save falsely claims "already installed" on
				// every later retry, so the same save just fails again).
				err := m.app.Store.SaveExecutor(context.Background(), cfg)
				if err != nil {
					return savedMsg{kind: "executor", id: cfg.ID, err: fmt.Errorf("save %q: %w", cfg.Name, err)}
				}
				if err := executor.WriteManifest(dirs, executor.Manifest{
					ID: cfg.ID, Name: name, Slug: slug, Command: resolved, BinaryName: command,
					InstallCmd: installLine, InstalledAt: time.Now().UTC(), AlreadyPresent: true,
				}); err != nil {
					return savedMsg{kind: "executor", id: cfg.ID,
						err: fmt.Errorf("%q was registered but its manifest failed to write: %w", cfg.Name, err)}
				}
				return savedMsg{kind: "executor", id: cfg.ID}
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
	case automationForm:
		item, err := automationFromEditor(m.form)
		if err != nil {
			m.form.err = err.Error()
			return m, nil
		}
		return m, func() tea.Msg {
			_, err := m.app.AutomationScheduler.Save(item)
			return savedMsg{kind: "automation", id: item.ID, err: err}
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
	case factoryResetForm:
		if strings.TrimSpace(values[0]) != factoryResetPhrase {
			m.form.err = fmt.Sprintf("phrase must match exactly: %q", factoryResetPhrase)
			return m, nil
		}
		application := m.app
		m.form = formModel{}
		m.status = "Factory reset in progress — wiping everything..."
		return m, func() tea.Msg {
			_ = application.Close()
			return factoryResetDoneMsg{err: os.RemoveAll(application.Paths.Root)}
		}
	}
	return m, nil
}

// executorFromValues looks fields up by label rather than fixed index, since
// the executor form may be the short (core-only) or expanded (ctrl+a,
// automation fields included) variant. When editing, advanced fields the
// operator never expanded into view (ctrl+a) are carried through from
// m.form.originalExecutor rather than treated as cleared, so collapsing the
// edit form to core-only never silently drops data the operator can't see.
func executorFromValues(labels, values []string, m Model) (domain.ExecutorConfig, error) {
	field := func(name string) string {
		for i, label := range labels {
			if label == name {
				return values[i]
			}
		}
		return ""
	}
	has := func(name string) bool {
		for _, label := range labels {
			if label == name {
				return true
			}
		}
		return false
	}
	orig := domain.ExecutorConfig{}
	if m.form.originalExecutor != nil {
		orig = *m.form.originalExecutor
	}

	command, args, err := shellSplitLine(field("Command"))
	if err != nil {
		return domain.ExecutorConfig{}, fmt.Errorf("invalid command: %w", err)
	}

	resumeCommand, resumeArgs := orig.ResumeCommand, orig.ResumeArgs
	if has("Resume command") {
		resumeCommand, resumeArgs = "", nil
		if raw := strings.TrimSpace(field("Resume command")); raw != "" {
			resumeCommand, resumeArgs, err = shellSplitLine(raw)
			if err != nil {
				return domain.ExecutorConfig{}, fmt.Errorf("invalid resume command: %w", err)
			}
		}
	}

	environment := orig.Environment
	if has("Environment") {
		if environment, err = parseEnvSpec(field("Environment")); err != nil {
			return domain.ExecutorConfig{}, err
		}
	}

	rules := orig.Rules
	if has("Recognition rules") {
		if rules, err = parseRules(field("Recognition rules")); err != nil {
			return domain.ExecutorConfig{}, err
		}
	}

	roles := orig.Roles
	if has("Roles") {
		roles = parseRoles(field("Roles"))
	}

	timeout := orig.Timeout
	if has("Timeout") {
		timeout = 0
		if raw := strings.TrimSpace(field("Timeout")); raw != "" {
			if timeout, err = time.ParseDuration(raw); err != nil {
				return domain.ExecutorConfig{}, fmt.Errorf("invalid timeout: %w", err)
			}
		}
	}

	shell := orig.Shell
	if has("Shell (optional)") {
		shell = field("Shell (optional)")
	}
	model := orig.Model
	if has("Model label") {
		model = field("Model label")
	}
	tokenizer := orig.Tokenizer
	if has("Tokenizer") {
		tokenizer = field("Tokenizer")
	}
	priceID := orig.PriceID
	if has("Price ID") {
		priceID = field("Price ID")
	}

	workingDir := strings.TrimSpace(field("Working directory"))
	if workingDir == "" && m.activeSession >= 0 {
		workingDir = m.sessions[m.activeSession].Workspace
	}

	suffix := orig.PromptSuffix
	if has("Prompt suffix") {
		suffix = strings.NewReplacer(`\r`, "\r", `\n`, "\n", `\t`, "\t").Replace(field("Prompt suffix"))
	}
	if suffix == "" {
		suffix = "\r"
	}

	cfg := domain.ExecutorConfig{
		ID: id.New("exec"), Name: field("Display name"), Command: command, Args: args,
		BinaryName: orig.BinaryName, InstallDir: orig.InstallDir, UseHostHome: orig.UseHostHome, CreatedAt: orig.CreatedAt,
		WorkingDir: workingDir, Environment: environment, Shell: shell,
		ResumeCommand: resumeCommand, ResumeArgs: resumeArgs, Rules: rules,
		Timeout: timeout, PromptSuffix: suffix, Roles: roles, Model: model,
		Tokenizer: tokenizer, PriceID: priceID,
	}
	return cfg, cfg.Validate()
}

// shellSplitLine tokenizes a single plain-text line — e.g. "codex --yolo" —
// into a command and its arguments, so executor forms take one free-text
// field instead of a separate command plus a JSON arguments array. Quotes
// allow a single argument to contain spaces ("mycli \"path with spaces\"").
// Backslash is treated as an ordinary character, not an escape: nearly every
// command here is a Windows path (C:\Users\...\claude.CMD), and escaping
// would mangle those on every parse.
func shellSplitLine(line string) (string, []string, error) {
	tokens, err := shellTokenize(line)
	if err != nil {
		return "", nil, err
	}
	if len(tokens) == 0 {
		return "", nil, fmt.Errorf("command is required")
	}
	return tokens[0], tokens[1:], nil
}

func shellTokenize(line string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	hasToken := false
	var quote rune
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
			hasToken = true
		case r == ' ' || r == '\t':
			if hasToken {
				tokens = append(tokens, current.String())
				current.Reset()
				hasToken = false
			}
		default:
			current.WriteRune(r)
			hasToken = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	if hasToken {
		tokens = append(tokens, current.String())
	}
	return tokens, nil
}

// shellJoinLine is the inverse of shellSplitLine, used to prefill the form
// field when editing an existing executor or a catalog suggestion.
func shellJoinLine(command string, args []string) string {
	if command == "" {
		return ""
	}
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuoteToken(command))
	for _, a := range args {
		parts = append(parts, shellQuoteToken(a))
	}
	return strings.Join(parts, " ")
}

// shellQuoteToken quotes a token only when shellTokenize would otherwise
// split or misparse it (whitespace or a quote character present). A bare
// backslash — the common case, since most commands here are Windows paths —
// never triggers quoting.
func shellQuoteToken(token string) string {
	if token == "" {
		return `""`
	}
	if !strings.ContainsAny(token, " \t\"'") {
		return token
	}
	if !strings.Contains(token, `"`) {
		return `"` + token + `"`
	}
	if !strings.Contains(token, `'`) {
		return `'` + token + `'`
	}
	// Contains both quote characters plus whitespace: shellTokenize has no
	// escape mechanism to round-trip this exactly. Extremely unlikely for a
	// CLI command/flag; fall back to double quotes (imperfect round-trip).
	return `"` + token + `"`
}

// parseEnvSpec parses "NAME=value; *SECRET=value" plain text into env vars.
// A leading '*' on an entry marks it secret (redacted in the UI).
func parseEnvSpec(spec string) ([]domain.SecretEnv, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	var out []domain.SecretEnv
	for _, part := range strings.Split(spec, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		secret := false
		if strings.HasPrefix(part, "*") {
			secret = true
			part = strings.TrimSpace(strings.TrimPrefix(part, "*"))
		}
		name, value, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("environment entry %q must be NAME=value", part)
		}
		out = append(out, domain.SecretEnv{Name: strings.TrimSpace(name), Value: value, Secret: secret})
	}
	return out, nil
}

func formatEnvSpec(envs []domain.SecretEnv) string {
	parts := make([]string, 0, len(envs))
	for _, e := range envs {
		prefix := ""
		if e.Secret {
			prefix = "*"
		}
		parts = append(parts, prefix+e.Name+"="+e.Value)
	}
	return strings.Join(parts, "; ")
}

// parseRoles splits a comma-separated plain-text list, e.g. "coder, reviewer".
func parseRoles(spec string) []string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	parts := strings.Split(spec, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func formatRoles(roles []string) string {
	return strings.Join(roles, ", ")
}

// parseRules parses "name::kind::value::outcome ;; name2::kind2::value2::outcome2"
// plain text into recognition rules, replacing the old JSON-array field.
func parseRules(spec string) ([]domain.RecognitionRule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	var out []domain.RecognitionRule
	for _, chunk := range strings.Split(spec, ";;") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		fields := strings.SplitN(chunk, "::", 4)
		if len(fields) != 4 {
			return nil, fmt.Errorf("recognition rule %q must have 4 fields: name::kind::value::outcome", chunk)
		}
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		out = append(out, domain.RecognitionRule{
			Name: fields[0], Kind: domain.RecognitionKind(fields[1]),
			Value: fields[2], Outcome: domain.State(fields[3]),
		})
	}
	return out, nil
}

func formatRules(rules []domain.RecognitionRule) string {
	parts := make([]string, 0, len(rules))
	for _, r := range rules {
		parts = append(parts, strings.Join([]string{r.Name, string(r.Kind), r.Value, string(r.Outcome)}, "::"))
	}
	return strings.Join(parts, ";; ")
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

func (m Model) renderProviderPickPanel() string {
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
	return modalStyle.Width(min(60, max(30, m.width-8))).Render(b.String())
}

func (m Model) renderFormPanel() string {
	if m.form.kind == providerPickForm {
		return m.renderProviderPickPanel()
	}
	if m.form.kind == automationForm {
		return m.renderAutomationEditor()
	}
	if m.form.kind == automationDetailsView {
		return modalStyle.Width(min(86, max(40, m.width-8))).Render(titleStyle.Render(m.form.title) + "\n\n" + m.form.details + "\n" + mutedStyle.Render("enter or esc closes"))
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
	case factoryResetForm:
		hint = fmt.Sprintf("type %q exactly, then ctrl+s to WIPE EVERYTHING • esc cancels", factoryResetPhrase)
	}
	b.WriteString("\n" + mutedStyle.Render(hint))
	return modalStyle.Width(min(86, max(40, m.width-8))).Render(b.String())
}

func (m Model) renderAutomationEditor() string {
	var b strings.Builder
	focus := func(index int, label string) {
		if m.form.automationFocus == index {
			b.WriteString(keyStyle.Render("› "+label) + "\n")
		} else {
			b.WriteString(mutedStyle.Render("  "+label) + "\n")
		}
	}
	radio := func(choice automationChoice, active bool, cursor bool) string {
		mark, prefix := "○", "  "
		if active {
			mark = "●"
		}
		if cursor {
			prefix = "› "
		}
		return prefix + mark + " " + choice.name
	}
	b.WriteString(titleStyle.Render(m.form.title) + "\n\n")
	focus(0, "Session")
	if len(m.form.automationSessions) == 0 {
		b.WriteString(errorStyle.Render("    No sessions available. Create one first.") + "\n")
	}
	for i, choice := range m.form.automationSessions {
		style := sideItemStyle
		if m.form.automationFocus == 0 && i == m.form.automationCursor {
			style = sideActiveStyle
		}
		b.WriteString(style.Render(radio(choice, choice.selected, m.form.automationFocus == 0 && i == m.form.automationCursor)) + "\n")
	}
	focus(1, "Executor")
	if len(m.form.automationExecutors) == 0 {
		b.WriteString(errorStyle.Render("    No executors available. Register one first.") + "\n")
	}
	for i, choice := range m.form.automationExecutors {
		style := sideItemStyle
		if m.form.automationFocus == 1 && i == m.form.automationCursor {
			style = sideActiveStyle
		}
		b.WriteString(style.Render(radio(choice, choice.selected, m.form.automationFocus == 1 && i == m.form.automationCursor)) + "\n")
	}
	focus(2, "Schedule")
	for i, name := range []string{"Once", "Daily", "Weekly"} {
		choice := automationChoice{name: name}
		style := sideItemStyle
		if m.form.automationFocus == 2 && i == m.form.automationCursor {
			style = sideActiveStyle
		}
		b.WriteString(style.Render(radio(choice, m.form.automationSchedule == i, m.form.automationFocus == 2 && i == m.form.automationCursor)) + "\n")
	}
	if m.form.automationSchedule == 2 {
		focus(3, "Days of Week")
		for day, name := range []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"} {
			prefix, mark, style := "  ", "[ ]", sideItemStyle
			if m.form.automationDays[day] {
				mark = "[x]"
			}
			if m.form.automationFocus == 3 && day == m.form.automationCursor {
				prefix, style = "› ", sideActiveStyle
			}
			b.WriteString(style.Render(prefix+mark+" "+name) + "\n")
		}
	} else {
		b.WriteString(mutedStyle.Render("  Days of Week (only needed for Weekly)") + "\n")
	}
	focus(4, "Prompt")
	b.WriteString(m.form.fields[0].View() + "\n")
	focus(5, "Time")
	b.WriteString(m.form.fields[1].View() + "\n")
	if m.form.automationSchedule == 0 {
		b.WriteString(mutedStyle.Render("  Once runs at the next occurrence of this time.") + "\n")
	}
	if m.form.err != "" {
		b.WriteString("\n" + errorStyle.Render(m.form.err) + "\n")
	}
	b.WriteString("\n" + mutedStyle.Render("tab moves • ↑↓ select • space chooses/toggles • ctrl+s save • esc cancel"))
	return modalStyle.Width(min(82, max(40, m.width-8))).Render(b.String())
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
	keyStyle        = lipgloss.NewStyle().Foreground(accent).Bold(true)
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
	voiceButtonStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(lipgloss.Color("#5F5FD7")).
				Bold(true)
	voiceButtonRecordingStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#FFFFFF")).
					Background(lipgloss.Color("#D74F62")).
					Bold(true)
	voiceButtonBusyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#D8D4E8")).
				Background(lipgloss.Color("#454052")).
				Bold(true)
)
