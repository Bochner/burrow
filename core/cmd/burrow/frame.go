package main

import (
	"context"
	"fmt"
	"image"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/timer"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/Bochner/burrow/core/connection"
	"github.com/Bochner/burrow/core/launch"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

var focusNext = key.NewBinding(key.WithKeys("f6", "shift+f6"))
var newWorkspace = key.NewBinding(key.WithKeys("alt+n"))
var openMenu = key.NewBinding(key.WithKeys("alt+m", "ctrl+p"))
var openNavigation = key.NewBinding(key.WithKeys("alt+w"))

const freshFor = 8 * time.Second

type workspaceView struct {
	downloadTimer         timer.Model
	fileViews             []*ui
	file                  *ui
	shells                []*cliTab
	shell                 *cliTab
	cli                   *cliTab
	management            ui
	focus, tab, selected  string
	checking, showCheck   bool
	lastSuccess, observed time.Time
	duration              time.Duration
	failure               string
	saveOffers            []saveOffered
}

// This is presentation state only. All resource snapshots come from Hovel.
// Explicit paths live for this frontend session; no directory scanning or registry.
type frame struct {
	downloadPlan                     *connection.DownloadPlan
	downloadMode                     *fileMode
	contextMenu                      *resourceMenu
	invalidGeometry                  bool
	initialShell                     string
	initialLogs                      bool
	terminals                        *terminalLifetime
	quitReview                       quitSnapshot
	quitClosing                      bool
	selection                        *textSelection
	mouseDisabled                    bool
	workspaces                       map[string]*workspaceView
	paths                            []string
	active                           string
	options                          launch.Options
	width, height                    int
	noColor                          bool
	demo                             bool
	navIndex, navOffset              int
	shellIndex, shellOffset          int
	modal                            string
	modalOffset                      int
	reviewText                       string
	destination                      string
	workspaceName, workspaceLocation string
	form                             *huh.Form
	formTitle                        string
	menu                             *huh.Select[int]
	details                          *connectDetails
	commandArgs                      []string
	closeTarget                      connection.State
	saveName                         string
	saveCollection                   connection.Collection
	attempt                          *authAttempt
	authSpinner                      spinner.Model
	question                         *authQuestion
	savedForm                        *huh.Form
	savedTitle                       string
	savedModal                       string
	launchPending                    bool
	launchError                      string
	sequence                         uint64
	inputEpoch                       uint64
	pending                          map[uint64]string
	now                              time.Time
}
type workspaceMessage struct {
	workspace string
	request   uint64
	message   tea.Msg
}
type daemonObservation struct {
	info     launch.Info
	err      error
	at       time.Time
	duration time.Duration
}
type workspaceOpened struct {
	info        launch.Info
	err         error
	destination string
}
type frameTick time.Time
type statusRequested struct{}

func newFrame(info launch.Info, noColor bool, options launch.Options) *frame {
	m := &frame{workspaces: make(map[string]*workspaceView), active: info.Workspace, paths: []string{info.Workspace}, options: options, noColor: noColor, pending: make(map[uint64]string), now: time.Now()}
	m.terminals = &terminalLifetime{}
	m.terminals.context, m.terminals.cancel = context.WithCancel(context.Background())
	m.workspaces[info.Workspace] = &workspaceView{management: newUI(info, noColor), focus: "prompt"}
	return m
}
func (m *frame) current() *workspaceView { return m.workspaces[m.active] }

// Every command, including nested Bubble Tea batches and timers, is stamped at
// dispatch. A result can update only its origin and is consumed exactly once.
func (m *frame) dispatch(workspace string, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	m.sequence++
	id := m.sequence
	m.pending[id] = workspace
	return func() tea.Msg { return workspaceMessage{workspace, id, cmd()} }
}
func (m *frame) Init() tea.Cmd {
	if m.demo {
		return nil
	}
	return tea.Batch(m.dispatch(m.active, m.current().activeUI().Init()), m.dispatch(m.active, refreshDownloads(m.active)), m.check(m.active), m.tick())
}
func (m *frame) tick() tea.Cmd {
	return m.dispatch(m.active, tea.Tick(time.Second, func(t time.Time) tea.Msg { return frameTick(t) }))
}
func (m *frame) check(path string) tea.Cmd {
	if m.demo {
		return nil
	}
	w := m.workspaces[path]
	if w.checking {
		return nil
	}
	w.checking = true
	return m.dispatch(path, func() tea.Msg {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		info, err := launch.Status(ctx, path)
		return daemonObservation{info, err, time.Now(), time.Since(start)}
	})
}
func (w *workspaceView) health(now time.Time) (string, lipgloss.Style) {
	if w.checking {
		return "checking", warningStyle
	}
	if w.failure != "" {
		return "UNVERIFIED / refused", errorStyle
	}
	if w.lastSuccess.IsZero() {
		return "unknown", warningStyle
	}
	if now.Sub(w.lastSuccess) > freshFor {
		return "stale", warningStyle
	}
	return "connected", successStyle
}
func (m *frame) resize() {
	m.revealSidebar(m.navIndex)
	m.revealShell(m.shellIndex)
	m.selection = nil
	left, right := m.columns()
	for _, w := range m.workspaces {
		w.management.width = max(1, m.width-left-right-4)
		w.management.height = max(1, m.height-4)
		w.management.input.SetWidth(max(1, w.management.width-4))
		for _, u := range w.fileViews {
			u.width, u.height = w.management.width, w.management.height
			u.input.SetWidth(max(1, u.width-4))
		}
		for _, tab := range w.terminals() {
			if tab != nil && tab.host != nil {
				r := m.terminalBounds()
				if err := tab.host.Send(image.Pt(r.Dx(), r.Dy())); err != nil && !tab.screen.Exited {
					tab.error = safe(err.Error())
				} else if err == nil && tab.error == invalidTerminalGeometry {
					tab.error = ""
				}
			}
		}
	}
}

const minimumWidth, minimumHeight = 28, 16

func (m *frame) tooSmall() bool      { return m.width < minimumWidth || m.height < minimumHeight }
func (m *frame) columns() (int, int) { return min(26, m.width/5), min(32, m.width/5) }
func (m *frame) selectWorkspace(i int) tea.Cmd {
	if i < 0 || i >= len(m.paths) {
		return nil
	}
	m.active = m.paths[i]
	m.revealSidebar(i)
	m.current().tab, m.current().focus = "", "prompt"
	m.modal = ""
	return m.check(m.active)
}

func (m *frame) sidebarRows() int { return max(1, m.height/2-4) }
func (m *frame) shellRows() int   { return max(1, m.height-m.height/2-5) }

func (m *frame) revealShell(i int) {
	m.shellIndex = max(0, min(len(m.shellEntries())-1, i))
	m.shellOffset = max(0, min(m.shellOffset, m.shellIndex))
	if m.shellIndex >= m.shellOffset+m.shellRows() {
		m.shellOffset = m.shellIndex - m.shellRows() + 1
	}
}

func (m *frame) revealSidebar(i int) {
	m.navIndex = max(0, min(len(m.paths)-1, i))
	m.navOffset = max(0, min(m.navOffset, m.navIndex))
	if m.navIndex >= m.navOffset+m.sidebarRows() {
		m.navOffset = m.navIndex - m.sidebarRows() + 1
	}
}
func (m *frame) openNew() tea.Cmd {
	m.modal = "new"
	if m.launchPending {
		return nil
	}
	m.launchError = ""
	m.inputEpoch++
	m.destination = ""
	m.workspaceName, m.workspaceLocation = "", ""
	return m.workspaceForm()
}
func (m *frame) workspaceForm() tea.Cmd {
	root, _ := workspaceParent("")
	return m.setForm("new", "NEW WORKSPACE", newForm(huh.NewGroup(
		huh.NewInput().Key("workspace-name").Title("Workspace name: ").Inline(true).Placeholder("lab").Value(&m.workspaceName).CharLimit(80),
		huh.NewInput().Key("workspace-location").Title("Location (optional): ").Inline(true).Placeholder(root).Value(&m.workspaceLocation).CharLimit(2048),
	).Description("Enter creates & opens · Tab edits location · Ctrl+O browses")).WithShowHelp(false))
}

func (m *frame) submitWorkspace() tea.Cmd {
	if m.demo {
		m.launchError = "Sample data preview · workspace launch disabled"
		return nil
	}
	if m.launchPending {
		return nil
	}
	path, err := workspaceDestination(m.workspaceName, m.workspaceLocation)
	if err != nil {
		m.launchError = err.Error()
		return nil
	}
	m.destination = path
	m.form = nil
	m.inputEpoch++
	m.launchPending = true
	m.launchError = ""
	options := m.options
	options.Workspace = path
	return m.dispatch(m.active, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		info, err := openWorkspace(ctx, options)
		return workspaceOpened{info, err, path}
	})
}
func (m *frame) updateManagement(path string, msg tea.Msg) tea.Cmd {
	w := m.workspaces[path]
	u := &w.management
	switch v := msg.(type) {
	case tea.KeyPressMsg:
		u = w.activeUI()
		if key.Matches(v, enter) && !u.busy {
			args, err := connection.Split(u.input.Value())
			if err == nil && len(args) == 1 && (args[0] == "downloads" || args[0] == "transfers") {
				u.input.Reset()
				return m.openDownloads()
			}
		}
		if u.files != nil && key.Matches(v, enter) && !u.busy {
			args, err := connection.Split(u.input.Value())
			if err == nil && len(args) > 0 {
				if args[0] == "get" || args[0] == "mget" || args[0] == "put" {
					return m.reviewDownload(u, args)
				}
				if (args[0] == "download-cancel" || args[0] == "transfer-cancel") && len(args) == 2 {
					mode := u.files
					u.output = "Cancellation requested; waiting for cleanup acknowledgement"
					u.input.Reset()
					return m.dispatch(path, func() tea.Msg {
						ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
						defer stop()
						d, err := connection.Execute(ctx, path, args)
						return downloadStarted{mode, d, err}
					})
				}
			}
		}
		if u.files == nil && key.Matches(v, enter) && !u.busy {
			args, err := connection.Split(u.input.Value())
			if err == nil && len(args) == 2 && args[0] == "scp" {
				return m.openFileTab(args[1])
			}
		}
	case tea.PasteMsg:
		u = w.activeUI()
	case fileResult:
		u = w.fileUI(v.mode)
	case fileDiscovery:
		u = w.fileUI(v.mode)
	case fileDiscoveryTick:
		u = w.fileUI(v.mode)
		// Background tabs retain observations, but never initiate discovery.
		if u != w.activeUI() {
			return nil
		}
	}
	if u == nil {
		return nil
	}
	w.management.shellIDs = nil
	for _, tab := range w.shells {
		w.management.shellIDs = append(w.management.shellIDs, tab.id)
	}
	model, cmd := u.Update(msg)
	*u = model.(ui)
	if u != &w.management && u.files == nil {
		w.fileViews = slices.DeleteFunc(w.fileViews, func(v *ui) bool { return v == u })
		if w.file == u {
			w.file, w.tab, w.focus = nil, "", "prompt"
		}
	}
	if args := w.management.shellControl; len(args) > 0 {
		w.management.shellControl = nil
		// Resolve positional shell numbers in the submitting event, before an
		// asynchronous background exit can renumber a different shell into place.
		return m.shellControl(args)
	}
	if u.quitting {
		u.quitting = false
		return m.openQuit()
	}
	if u.help {
		v := u.helpViewport(m.width, m.height)
		u.helpOffset = v.YOffset()
	}
	for _, files := range w.fileViews {
		files.connections, files.tunnels = w.management.connections, w.management.tunnels
		files.connectionError, files.info = w.management.connectionError, w.management.info
	}
	return m.dispatch(path, cmd)
}

func (w *workspaceView) activeUI() *ui {
	if w.tab == "files" && w.file != nil {
		return w.file
	}
	return &w.management
}

func (w *workspaceView) fileUI(mode *fileMode) *ui {
	for _, u := range w.fileViews {
		if u.files == mode {
			return u
		}
	}
	return nil
}

func (m *frame) openFileTab(name string) tea.Cmd {
	w := m.current()
	var observed connection.State
	for _, s := range w.management.connections {
		if s.Name == name && s.State == "connected" {
			observed = s
			break
		}
	}
	if observed.Name == "" || w.management.connectionError != "" {
		w.management.output = "REFUSED: select a verified live connection; reconnect explicitly"
		return nil
	}
	for _, u := range w.fileViews {
		if u.files.state.Name == name && u.files.state.Creation == observed.Creation && u.files.state.Generation == observed.Generation {
			m.selectFileTab(u)
			w.management.input.Reset()
			return nil
		}
	}
	u := newUI(w.management.info, m.noColor)
	u.connections, u.tunnels, u.connectionError = w.management.connections, w.management.tunnels, w.management.connectionError
	u.width, u.height = w.management.width, w.management.height
	u.input.SetWidth(max(1, u.width-4))
	cmd := u.openFiles(name)
	if u.files == nil {
		w.management.output = u.output
		return nil
	}
	w.management.input.Reset()
	w.fileViews = append(w.fileViews, &u)
	m.selectFileTab(&u)
	return m.dispatch(m.active, cmd)
}

func (m *frame) selectFileTab(u *ui) {
	w := m.current()
	w.file, w.tab, w.focus = u, "files", "prompt"
	m.selection = nil
	for i, entry := range m.shellEntries() {
		if entry.files == u {
			m.revealShell(i)
			break
		}
	}
}
func (m *frame) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case spinner.TickMsg:
		if m.attempt == nil || m.modal != "auth" || m.question != nil {
			return m, nil
		}
		var cmd tea.Cmd
		m.authSpinner, cmd = m.authSpinner.Update(v)
		return m, cmd
	case formMessage:
		if v.path != m.active || v.epoch != m.inputEpoch {
			return m, nil
		}
		if ready, ok := v.msg.(quitSnapshot); ok {
			return m, m.showQuit(ready)
		}
		if ready, ok := v.msg.(browserReady); ok {
			return m, m.setForm("browse", m.formTitle, ready.form)
		}
		return m, m.updateForm(v.msg)
	case authQuestionReady:
		if m.attempt != v.attempt || v.attempt.ctx.Err() != nil {
			return m, nil
		}
		m.question = &v.question
		return m, m.setForm("auth", "SSH authentication", promptForm(v.question.prompt, true))
	case authFinished:
		if m.attempt != v.attempt {
			return m, nil
		}
		m.attempt = nil
		if m.modal == "auth" {
			m.dismissForm()
		}
		cmd := m.updateManagement(v.attempt.path, connectionResult{v.result, v.err})
		if v.err == nil {
			if state, ok := v.result.(connection.State); ok && state.State == "connected" {
				return m, tea.Batch(cmd, m.dispatch(v.attempt.path, offerSave(v.attempt.path, state.Name)))
			}
		}
		return m, cmd

	case workspaceMessage:
		path, ok := m.pending[v.request]
		if !ok || path != v.workspace {
			return m, nil
		}
		delete(m.pending, v.request)
		if batch, ok := v.message.(tea.BatchMsg); ok {
			cmds := make([]tea.Cmd, 0, len(batch))
			for _, cmd := range batch {
				cmds = append(cmds, m.dispatch(path, cmd))
			}
			return m, tea.Batch(cmds...)
		}
		switch result := v.message.(type) {
		case downloadReviewReady:
			return m, m.acceptDownloadReview(path, result)
		case downloadsReady:
			return m, m.acceptDownloads(path, result)
		case timer.TickMsg:
			w := m.workspaces[path]
			var cmd tea.Cmd
			w.downloadTimer, cmd = w.downloadTimer.Update(result)
			return m, m.dispatch(path, cmd)
		case timer.TimeoutMsg:
			if result.ID != m.workspaces[path].downloadTimer.ID() {
				return m, nil
			}
			return m, m.dispatch(path, refreshDownloads(path))
		case downloadStarted:
			if u := m.workspaces[path].fileUI(result.mode); u != nil {
				u.output = "Download state updated; files retained independently of this view"
				if result.err != nil {
					u.output = "REFUSED: " + safe(result.err.Error())
					u.outputOffset = 0
					if path == m.active && m.modal == "downloads" && m.current().activeUI() == u {
						m.dismissForm()
					}
				}
			}
			return m, m.dispatch(path, refreshDownloads(path))
		case logsRequested:
			if path == m.active {
				return m, m.openLogs()
			}
			return m, nil
		case shellRequested:
			if path != m.active {
				return m, m.updateManagement(path, connectionResult{nil, fmt.Errorf("shell cancelled after workspace switch")})
			}
			return m, m.openShell(result.name)
		case authenticationRequested:
			if path != m.active {
				return m, m.updateManagement(path, connectionResult{nil, fmt.Errorf("connection request cancelled after workspace switch")})
			}
			return m, m.reviewCommand(result.args)
		case saveOffered:
			if result.err != nil {
				m.workspaces[path].management.output += "\nConnection active; save offer unavailable. Use profile save NAME after fixing the collection."
				return m, nil
			}
			if result.offer {
				m.workspaces[path].saveOffers = append(m.workspaces[path].saveOffers, result)
			}
			return m, m.showSaveOffer()
		case commandReview:
			if path != m.active || m.modal != "review" || m.commandArgs == nil || result.epoch != m.inputEpoch {
				return m, nil
			}
			if result.err != nil {
				m.dismissForm()
				return m, m.updateManagement(path, connectionResult{nil, result.err})
			}
			if result.review == "approved-profile-connect" {
				m.commandArgs = nil
				return m, m.startAuthentication(result.args)
			}
			if result.review == "" {
				m.dismissForm()
				return m, m.updateManagement(path, connectionResult{result.result, nil})
			}
			m.commandArgs = result.args
			m.closeTarget = result.target
			m.reviewText = publicPrompt(connection.Prompt{Text: result.review})
			m.modalOffset = 0
			return m, m.setForm("review", "Review exact target · ↑↓ / PgUp/PgDn scroll", confirmForm("Proceed?", "", "Proceed", "Cancel"))
		case cliOpened, cliScreen, cliClosed:
			return m, m.terminalResult(path, result)
		case profileEditorOpened:
			result.opened.tab.editor = result.edit
			return m, m.terminalResult(path, result.opened)
		case profileEditorFinished:
			w := m.workspaces[path]
			if !w.ownsTerminal(result.tab) {
				return m, nil
			}
			w.removeShell(result.tab)
			cmd := m.updateManagement(path, connectionResult{result.result, result.err})
			if result.err == nil {
				w.management.output = result.notice
			}
			return m, cmd
		case tea.QuitMsg:
			return m, tea.Quit
		case frameTick:
			m.now = time.Time(result)
			var cmd tea.Cmd
			w := m.current()
			if m.now.Sub(w.observed) >= 3*time.Second {
				cmd = m.check(m.active)
			}
			return m, tea.Batch(cmd, m.tick(), m.showSaveOffer())
		case statusRequested:
			m.workspaces[path].showCheck = true
			return m, m.check(path)
		case daemonObservation:
			w := m.workspaces[path]
			w.checking = false
			w.observed = result.at
			w.duration = result.duration
			if result.err != nil {
				w.failure = safe(result.err.Error())
				w.management.info.Health = "UNVERIFIED (last known PID)"
			} else {
				w.failure = ""
				w.lastSuccess = result.at
				w.management.info = result.info
			}

			if w.showCheck {
				w.showCheck = false
				w.management.busy = false
				w.management.outputOffset = 0
				if result.err != nil {
					w.management.output = "REFUSED: " + w.failure
				} else {
					w.management.output = fmt.Sprintf("Verified daemon PID %d · %s", result.info.PID, safe(result.info.Health))
				}
			}
			return m, nil
		case workspaceOpened:
			m.launchPending = false
			if result.err != nil {
				m.launchError = "REFUSED: " + safe(result.err.Error())
				m.workspaces[path].management.output = m.launchError
				if m.modal == "new" && path == m.active && m.destination == result.destination {
					return m, m.workspaceForm()
				}
				return m, nil
			}
			destination := result.info.Workspace
			var init tea.Cmd
			if _, ok := m.workspaces[destination]; !ok {
				m.paths = append(m.paths, destination)
				m.workspaces[destination] = &workspaceView{management: newUI(result.info, m.noColor), focus: "prompt"}
				init = m.dispatch(destination, tea.Batch(m.workspaces[destination].management.Init(), refreshDownloads(destination)))
			}
			m.resize()
			// Esc may dismiss a pending launch, but its completion never steals selection.
			if m.modal == "new" && m.active == path && m.destination == result.destination {
				for i, p := range m.paths {
					if p == destination {
						return m, tea.Batch(init, m.selectWorkspace(i))
					}
				}
			}
			return m, tea.Batch(init, m.check(destination))
		default:
			return m, m.updateManagement(path, v.message)
		}
	case tea.WindowSizeMsg:
		m.invalidGeometry = v.Width <= 0 || v.Height <= 0 || v.Width > 1000 || v.Height > 1000
		if m.invalidGeometry {
			for _, w := range m.workspaces {
				w.management.output = invalidTerminalGeometry
				for _, tab := range w.terminals() {
					if tab != nil {
						tab.error = w.management.output
					}
				}
			}
			return m, nil
		}
		m.width = max(1, v.Width)
		m.height = max(1, v.Height)
		m.resize()
		m.sizeForm()
		if m.initialLogs {
			m.initialLogs = false
			return m, m.openLogs()
		}
		if m.initialShell != "" {
			name := m.initialShell
			m.initialShell = ""
			return m, m.openShell(name)
		}
		return m, nil
	case tea.ResumeMsg:
		return m, tea.RequestWindowSize
	case tea.ColorProfileMsg:
		for path := range m.workspaces {
			m.updateManagement(path, v)
		}
		m.noColor = m.current().activeUI().noColor
		return m, nil
	case tea.MouseMsg:
		if m.tooSmall() {
			return m, nil
		}
		// All pointer types are captured by the active modal, including the child's
		// help/quit overlay. An unnamed visual layer cannot allow click-through.
		mouse := v.Mouse()
		hit := m.compositor().Hit(mouse.X, mouse.Y)
		if m.mouseDisabled {
			return m, nil
		}
		if click, ok := v.(tea.MouseClickMsg); ok {
			if m.modal == "context" && !image.Pt(click.X, click.Y).In(m.dialogBounds()) {
				m.dismissForm()
				return m, nil
			}
			if click.Button == tea.MouseRight && m.modal == "" && !m.current().activeUI().help {
				return m, m.openResourceMenu(hit.ID(), image.Pt(click.X, click.Y))
			}
		}
		if click, ok := v.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft && m.modal == "" && !m.current().activeUI().help {
			copying := m.hasSelection() && image.Pt(mouse.X, mouse.Y).In(m.copyBounds())
			if !copying && hit.ID() != "saved" && !strings.HasPrefix(hit.ID(), "profile:") && !strings.HasPrefix(hit.ID(), "resource:") {
				m.clearRows()
			}
		}
		if handled, cmd := m.selectionMouse(v, hit.ID()); handled {
			return m, cmd
		}
		if hit.ID() == "terminal" && m.modal == "" && !m.current().activeUI().help {
			if click, ok := v.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft {
				m.current().focus = "terminal"
			}
			if m.terminalFocused() {
				m.terminalMouse(v)
			}
			return m, nil
		}
		if wheel, ok := msg.(tea.MouseWheelMsg); ok {
			delta := 1
			if wheel.Button == tea.MouseWheelUp {
				delta = -1
			}
			if m.modal == "quit" || m.modal == "review" {
				m.modalOffset = max(0, m.modalOffset+delta)
				return m, nil
			}
			if m.modal == "downloads" {
				m.scrollDownloads(delta)
				return m, nil
			}
			if m.current().activeUI().help {
				u := m.current().activeUI()
				u.helpOffset = max(0, u.helpOffset+delta)
				v := u.helpViewport(m.width, m.height)
				u.helpOffset = v.YOffset()
				return m, nil
			}
			if m.modal == "menu" || m.modal == "context" {
				code := tea.KeyDown
				if delta < 0 {
					code = tea.KeyUp
				}
				return m, m.updateForm(tea.KeyPressMsg{Code: code})
			}
			if m.modal != "" && m.modal != "navigation" {
				if m.modal == "metadata" {
					m.scrollMetadata(delta)
				}
				return m, nil
			}
			if strings.HasPrefix(hit.ID(), "workspace:") || hit.ID() == "workspaces" {
				m.navOffset = max(0, min(max(0, len(m.paths)-m.sidebarRows()), m.navOffset+delta))
			}
			if strings.HasPrefix(hit.ID(), "shell:") || hit.ID() == "shell-list" {
				m.shellOffset = max(0, min(max(0, len(m.shellEntries())-m.shellRows()), m.shellOffset+delta))
			}
			if strings.HasPrefix(hit.ID(), "profile:") || hit.ID() == "saved" {
				u := m.current().activeUI()
				u.profileOffset = max(0, min(max(0, len(u.profiles.Profiles)-u.profileRows()), u.profileOffset+delta))
			}
			if strings.HasPrefix(hit.ID(), "resource:") {
				u := m.current().activeUI()
				u.connectionOffset = max(0, min(max(0, len(u.connections)-u.connectionRows()), u.connectionOffset+delta))
			}
			if hit.ID() == "center" {
				m.current().activeUI().outputOffset = max(0, m.current().activeUI().outputOffset+delta)
			}
			return m, nil
		}
		if click, ok := msg.(tea.MouseClickMsg); !ok || click.Button != tea.MouseLeft {
			return m, nil
		}
		if m.current().activeUI().help {
			switch hit.ID() {
			case "dismiss":
				m.current().activeUI().help = false
			}
			return m, nil
		}
		return m, m.activate(hit.ID())
	case clipboardResult:
		if m.selection == v.selection {
			m.selection.notice = v.notice
		}
		return m, nil
	case tea.PasteMsg:
		m.selection = nil
		if m.tooSmall() || m.current().activeUI().help {
			return m, nil
		}
		if m.terminalFocused() {
			m.sendTerminal(v.Content)
			return m, nil
		}
		if m.form != nil {
			return m, m.updateForm(tea.PasteMsg{Content: safe(v.Content)})
		}
		if m.modal != "" || m.current().focus != "prompt" || (m.current().tab != "" && m.current().tab != "files") {
			return m, nil
		}
	case tea.KeyPressMsg:
		if m.hasSelection() {
			if key.Matches(v, copySelection) {
				return m, m.copySelection()
			}
			m.selection = nil
			if key.Matches(v, escape) {
				return m, nil
			}
		}
		if key.Matches(v, toggleMouse) {
			m.mouseDisabled = !m.mouseDisabled
			m.selection = nil
			return m, nil
		}
		if m.tooSmall() {
			if m.modal == "quit" {
				if m.quitClosing {
					return m, nil
				}
				if key.Matches(v, quit) {
					return m, tea.Quit
				}
				if key.Matches(v, escape) {
					m.dismissForm()
				}
				return m, nil
			}
			if key.Matches(v, quit) {
				return m, m.openQuit()
			}
			return m, nil
		}
		if m.current().activeUI().help {
			m.current().activeUI().updateHelp(v, m.width, m.height)
			return m, nil
		}
		if m.modal != "" {
			return m, m.modalKey(v)
		}
		if key.Matches(v, toggleLogs) {
			return m, m.openLogs()
		}
		if key.Matches(v, toggleCollected) {
			return m, m.openLogView(true)
		}
		if key.Matches(v, showBurrow) {
			return m, m.activate("burrow")
		}
		if key.Matches(v, showHovel) {
			return m, m.activate("hovel")
		}
		if key.Matches(v, numberedShell) {
			i := int(v.Code - '1')
			if i >= 0 && i < len(m.current().shells) {
				m.resumeShell(m.current().shells[i])
			}
			return m, nil
		}
		if key.Matches(v, previousShell, nextShell) {
			delta := 1
			if key.Matches(v, previousShell) {
				delta = -1
			}
			m.cycleShell(delta)
			return m, nil
		}
		if m.terminalFocused() {
			if m.current().tab == "shell" && key.Matches(v, terminalEscape) {
				m.current().tab, m.current().focus = "", "prompt"
				return m, nil
			}
			if m.current().tab == "hovel" && key.Matches(v, restartTerminal) && m.current().canRestartCLI() {
				return m, m.restartCLI()
			} else if key.Matches(v, terminalEscape) {
				m.current().focus = "tabs"
			} else {
				m.sendTerminal(uv.KeyPressEvent(v))
			}
			return m, nil
		}
		switch {
		case key.Matches(v, newWorkspace):
			return m, m.openNew()
		case key.Matches(v, openMenu):
			return m, m.openPalette()
		case key.Matches(v, resourceActions):
			left, _ := m.columns()
			return m, m.openResourceMenu("", image.Pt(left+2, 4))
		case key.Matches(v, openNavigation):
			m.modal = "navigation"
			return m, nil
		case key.Matches(v, focusNext):
			names := []string{"prompt", "workspaces", "new", "menu", "shells", "tabs", "resources", "saved"}
			if m.current().activeUI().files != nil {
				names = names[:6]
			}
			for i, name := range names {
				if name == m.current().focus {
					delta := 1
					if v.Mod.Contains(tea.ModShift) {
						delta = -1
					}
					m.current().focus = names[(i+delta+len(names))%len(names)]
					break
				}
			}
			return m, nil
		case key.Matches(v, quit, help):
			return m, m.updateManagement(m.active, msg)
		}
		w := m.current()
		if w.focus != "prompt" {
			delta := 0
			if key.Matches(v, previous) {
				delta = -1
			}
			if key.Matches(v, next) {
				delta = 1
			}
			switch w.focus {
			case "workspaces":
				m.revealSidebar(m.navIndex + delta)
				if key.Matches(v, enter) {
					return m, m.selectWorkspace(m.navIndex)
				}
			case "shells":
				m.revealShell(m.shellIndex + delta)
				if key.Matches(v, enter) {
					return m, m.activate(fmt.Sprintf("shell:%d", m.shellIndex))
				}
			case "tabs":
				if key.Matches(v, enter) && w.tab == "files" {
					w.focus = "prompt"
					return m, nil
				}
				if key.Matches(v, enter) && w.tab == "hovel" {
					return m, m.openCLI()
				}
				if key.Matches(v, enter) && w.tab == "shell" {
					return m, m.activate("shells")
				}
				if key.Matches(v, choose, previous, next) {
					if delta == 0 {
						delta = 1
					}
					m.cycleTab(delta)
				}
			case "saved":
				rows := w.management.profiles.Profiles
				index := 0
				for i, p := range rows {
					if p.Name == w.management.selectedProfile {
						index = i
					}
				}
				if len(rows) > 0 {
					index = max(0, min(len(rows)-1, index+delta))
					w.management.selectedProfile = rows[index].Name
					w.management.profileOffset = index
				}
			case "resources":
				rows := w.management.connections
				index := 0
				for i, row := range rows {
					if row.Name == w.selected {
						index = i
					}
				}
				if len(rows) > 0 {
					index = max(0, min(len(rows)-1, index+delta))
					w.selected = rows[index].Name
					w.management.connectionOffset = index
				}
			}
			if key.Matches(v, enter) {
				return m, m.activate(w.focus)
			}
			if key.Matches(v, escape) {
				m.clearRows()
				w.focus = "prompt"
				w.tab = ""
			}
			return m, nil
		}
		if w.tab != "" && w.tab != "files" {
			if key.Matches(v, escape) {
				w.tab = ""
			}
			return m, nil
		}
	}
	return m, m.updateManagement(m.active, msg)
}

var menuActions = []string{"Check daemon", "Metadata", "New workspace", "Keyboard help", "Quit", "Open Hovel CLI", "Close Hovel CLI", "Toggle mouse / text selection"}

func (m *frame) clearRows() {
	for _, w := range m.workspaces {
		w.selected, w.management.selectedProfile = "", ""
		switch w.focus {
		case "saved", "resources", "workspaces", "shells":
			w.focus = "prompt"
		}
	}
}

func (m *frame) activate(id string) tea.Cmd {
	if m.modal != "" && m.modal != "navigation" {
		if (m.modal == "connect" || m.modal == "new") && strings.HasPrefix(id, "field:") && m.form != nil {
			target := strings.TrimPrefix(id, "field:")
			current, next := -1, -1
			fields := connectFields
			if m.modal == "new" {
				fields = workspaceFields
			}
			for i, field := range fields {
				if field.key == m.form.GetFocusedField().GetKey() {
					current = i
				}
				if field.key == target {
					next = i
				}
			}
			var cmds []tea.Cmd
			for current >= 0 && next >= 0 && current != next {
				if current < next {
					cmds = append(cmds, m.form.NextField())
					current++
				} else {
					cmds = append(cmds, m.form.PrevField())
					current--
				}
			}
			return formCommand(m.active, m.inputEpoch, tea.Batch(cmds...))
		}
		if id == "browse" {
			return m.browse()
		}
		if id == "dismiss" {
			m.dismissForm()
			return nil
		}
		if m.modal == "new" && id == "submit" {
			return m.submitWorkspace()
		}
		if (m.modal == "menu" || m.modal == "context") && strings.HasPrefix(id, "action:") {
			i, _ := strconv.Atoi(strings.TrimPrefix(id, "action:"))
			m.menu.Value(&i)
			return m.updateForm(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
		if strings.HasPrefix(id, "confirm-") && m.form != nil {
			if c, ok := m.form.GetFocusedField().(*huh.Confirm); ok {
				approve := id == "confirm-accept"
				c.Value(&approve)
				return m.updateForm(tea.KeyPressMsg{Code: tea.KeyEnter})
			}
		}
		return nil
	}
	if strings.HasPrefix(id, "profile:") {
		m.clearRows()
		i, err := strconv.Atoi(strings.TrimPrefix(id, "profile:"))
		u := m.current().activeUI()
		if err == nil && i >= 0 && i < len(u.profiles.Profiles) {
			u.selectedProfile = u.profiles.Profiles[i].Name
			m.current().focus = "saved"
		}
		return nil
	}
	if strings.HasPrefix(id, "resource:") {
		m.clearRows()
		i, err := strconv.Atoi(strings.TrimPrefix(id, "resource:"))
		rows := m.current().activeUI().connections
		if err == nil && i >= 0 && i < len(rows) {
			m.current().selected = rows[i].Name
			m.current().focus = "resources"
		}
		return nil
	}
	if strings.HasPrefix(id, "workspace:") {
		i, _ := strconv.Atoi(strings.TrimPrefix(id, "workspace:"))
		return m.selectWorkspace(i)
	}
	if strings.HasPrefix(id, "shell:") {
		i, err := strconv.Atoi(strings.TrimPrefix(id, "shell:"))
		entries := m.shellEntries()
		if err == nil && i >= 0 && i < len(entries) {
			entry := entries[i]
			cmd := m.selectWorkspace(entry.workspace)
			m.revealShell(i)
			if entry.tab != nil {
				m.resumeShell(entry.tab)
			}
			if entry.files != nil {
				m.selectFileTab(entry.files)
			}
			return cmd
		}
		return nil
	}
	if strings.HasPrefix(id, "shell-tab:") {
		i, err := strconv.Atoi(strings.TrimPrefix(id, "shell-tab:"))
		if err == nil && i >= 0 && i < len(m.current().shells) {
			m.resumeShell(m.current().shells[i])
		}
		return nil
	}
	if strings.HasPrefix(id, "file-tab:") {
		i, err := strconv.Atoi(strings.TrimPrefix(id, "file-tab:"))
		if err == nil && i >= 0 && i < len(m.current().fileViews) {
			m.selectFileTab(m.current().fileViews[i])
		}
		return nil
	}
	switch id {
	case "previous-shell":
		if len(m.current().fileViews) > 0 {
			m.cycleTab(-1)
			return nil
		}
		m.cycleShell(-1)
	case "next-shell":
		if len(m.current().fileViews) > 0 {
			m.cycleTab(1)
			return nil
		}
		m.cycleShell(1)
	case "saved":
		return m.profileMenu()
	case "new":
		return m.openNew()
	case "menu":
		return m.openPalette()
	case "navigation":
		m.modal = "navigation"
	case "daemon":
		m.modal = "metadata"
		m.modalOffset = 0
	case "burrow":
		m.current().tab = ""
		m.current().focus = "prompt"
	case "hovel":
		return m.openCLI()
	case "restart-cli":
		return m.restartCLI()
	case "center":
		m.current().focus = "prompt"
	case "shells":
		m.current().focus = "shells"
		if m.current().shell != nil {
			m.resumeShell(m.current().shell)
		}
	case "shell-list":
		m.current().focus = "shells"
	case "workspaces":
		m.current().focus = "workspaces"
	case "dismiss":
		m.dismissForm()
	}
	return nil
}
func (m *frame) openPalette() tea.Cmd {
	m.modal = "menu"
	m.inputEpoch++
	options := make([]huh.Option[int], len(menuActions))
	for i, label := range menuActions {
		options[i] = huh.NewOption(label, i)
	}
	m.menu = huh.NewSelect[int]().Key("action").Title("Type to filter").Description("Type to filter").Options(options...).Filtering(true)
	m.formTitle = "Menu"
	m.form = newForm(huh.NewGroup(m.menu)).WithShowHelp(false)
	m.sizeForm()
	return formCommand(m.active, m.inputEpoch, m.form.Init())
}

// Clipboard results belong to the input instance that requested them, just as
// daemon results belong to their originating workspace.
func (m *frame) menuAction(i int) tea.Cmd {
	m.form = nil
	m.modal = ""
	switch i {
	case 0:
		return m.check(m.active)
	case 1:
		m.modal = "metadata"
		m.modalOffset = 0
	case 2:
		return m.openNew()
	case 3:
		m.current().activeUI().help = true
	case 4:
		return m.openQuit()
	case 5:
		return m.openCLI()
	case 6:
		return m.closeCLI()
	case 7:
		m.mouseDisabled = !m.mouseDisabled
	}
	return nil
}
func (m *frame) modalKey(v tea.KeyPressMsg) tea.Cmd {
	if m.modal == "quit" && m.quitClosing {
		return nil
	}
	if (m.modal == "quit" || m.modal == "review") && key.Matches(v, pageUp, pageDown, previous, next) {
		delta := 1
		if key.Matches(v, pageUp, previous) {
			delta = -1
		}
		m.modalOffset = max(0, m.modalOffset+delta)
		return nil
	}
	if key.Matches(v, escape, quit) {
		m.dismissForm()
		return nil
	}
	if m.modal == "downloads" {
		view := m.downloadsViewport()
		switch {
		case key.Matches(v, previous):
			m.scrollDownloads(-1)
		case key.Matches(v, next):
			m.scrollDownloads(1)
		case key.Matches(v, pageUp):
			m.scrollDownloads(-view.Height())
		case key.Matches(v, pageDown):
			m.scrollDownloads(view.Height())
		case key.Matches(v, helpHome):
			m.modalOffset = 0
		case key.Matches(v, helpEnd):
			view.GotoBottom()
			m.modalOffset = view.YOffset()
		}
		return nil
	}
	switch m.modal {
	case "new", "menu", "context", "connect", "review", "auth", "quit", "browse", "profile-menu", "profile-save":
		if m.modal == "new" {
			if m.launchPending {
				return nil
			}
			if key.Matches(v, enter) {
				return m.submitWorkspace()
			}
			if m.form != nil && (v.String() == "tab" || v.String() == "shift+tab") {
				if m.form.GetFocusedField().GetKey() == "workspace-name" {
					return m.activate("field:workspace-location")
				}
				return m.activate("field:workspace-name")
			}
		}
		if v.String() == "ctrl+o" {
			return m.browse()
		}
		if m.modal != "new" || !m.launchPending {
			return m.updateForm(v)
		}
	case "navigation":
		if key.Matches(v, newWorkspace) {
			return m.openNew()
		}
		if key.Matches(v, openMenu) {
			return m.openPalette()
		}
		if key.Matches(v, previous) {
			m.revealSidebar(m.navIndex - 1)
		}
		if key.Matches(v, next) {
			m.revealSidebar(m.navIndex + 1)
		}
		if key.Matches(v, enter) {
			return m.selectWorkspace(m.navIndex)
		}
	default:
		if key.Matches(v, previous, pageUp) {
			m.scrollMetadata(-1)
		}
		if key.Matches(v, next, pageDown) {
			m.scrollMetadata(1)
		}
	}
	return nil
}
func (m *frame) scrollMetadata(delta int) {
	bounds := m.dialogBounds()
	v := scrollBody(m.metadata(), bounds.Dx()-6, bounds.Dy()-6, m.modalOffset+delta)
	m.modalOffset = v.YOffset()
}
func (m *frame) metadata() string {
	w := m.current()
	u := *w.activeUI()
	field := func(label, value string, style lipgloss.Style) string {
		if value == "Unavailable" || value == "Unknown" || value == "Never" {
			style = secondary
		}
		return u.paint(secondary, label+": ") + u.paint(style, value)
	}
	section := func(label string) string { return u.paint(accent, label) }
	state, statusStyle := w.health(m.now)
	connected := 0
	for _, row := range u.connections {
		if row.State == "connected" || row.State == "active" {
			connected++
		}
	}
	connectionState := "DISCONNECTED"
	connectionColor := errorStyle
	if !u.connectionObserved && len(u.connections) == 0 {
		connectionState = "UNKNOWN"
		connectionColor = secondary
	}
	if connected > 0 {
		connectionState = "CONNECTED"
		connectionColor = successStyle
	}
	selected := field("Selection", "None", secondary)
	selectedName := w.selected
	if u.files != nil {
		selectedName = u.files.state.Name
	}
	details := ""
	for _, row := range u.connections {
		if row.Name != selectedName {
			continue
		}
		connectionState = strings.ToUpper(safe(row.State))
		if row.State == "closed" || row.State == "lost" {
			connectionState = "DISCONNECTED · " + strings.ToUpper(row.State)
		}
		connectionColor = connectionStyle(row.State)
		selected = field("Name", safe(row.Name), accent) + "\n" + field("Host", safe(row.Host), hostStyle) + "\n" + field("User", safe(row.User), successStyle) + "\n" + field("Port", fmt.Sprint(row.Port), warningStyle)
		socket := safe(row.Socket)
		if socket == "" {
			socket = "Unavailable"
		}
		details = "\n\n" + section("CONNECTION DETAILS") + "\n" + field("Socket", socket, secondary) + "\n" + field("Owner PID", fmt.Sprint(row.OwnerPID), numberStyle) + "\n" + field("Master PID", fmt.Sprint(row.MasterPID), numberStyle)
		break
	}
	if u.connectionError != "" {
		connectionState = "UNVERIFIED"
		connectionColor = warningStyle
	}
	if u.files != nil && u.fileState() == "unavailable" {
		connectionState, connectionColor = "UNAVAILABLE", errorStyle
		selected = field("Name", safe(u.files.state.Name), accent)
		details = "\nPrevious connection creation unavailable; reconnect explicitly"
	}
	files, bytes := "Unavailable", "Unavailable"
	transferNote := "Completed downloads · workspace scope"
	if w.management.downloadObserved && w.management.downloadError == "" {
		files = fmt.Sprint(w.management.downloads.Files)
		bytes = downloadTotalSize(w.management.downloads.Bytes)
	}
	if w.management.downloadError != "" {
		transferNote = "Download records unverified"
	}
	if m.demo {
		files, bytes = "12", "48.6 MB"
		transferNote = "Sample downloads"
	}
	count := fmt.Sprint(connected)
	if u.connectionError != "" || (!u.connectionObserved && len(u.connections) == 0) {
		count = "Unknown"
	}
	text := section("SELECTED CONNECTION") + "\n" + u.paint(connectionColor.Bold(true), connectionState) + "\n" + selected + "\n" + field("Active in workspace", count, numberStyle) + "\n\n" +
		section("WORKSPACE DOWNLOADS") + "\n" + field("Completed files", files, numberStyle) + "\n" + field("Downloaded", bytes, numberStyle) + "\n" + u.paint(secondary, transferNote) + "\n\n" +
		section("HOVEL DAEMON") + "\n"
	if m.demo {
		return text + u.paint(warningStyle, "DEMO · no daemon") + details
	}
	age := "Never"
	if !w.lastSuccess.IsZero() {
		age = fmt.Sprintf("%ds ago", max(0, int(m.now.Sub(w.lastSuccess).Seconds())))
	}
	duration := "Unavailable"
	if !w.observed.IsZero() {
		duration = w.duration.Round(time.Millisecond).String()
	}
	text += u.paint(statusStyle, strings.ToUpper(state)) + "\n" + field("Last success", age, infoStyle) + "\n" + field("Check duration", duration, numberStyle) + "\n" + u.paint(secondary, "Last known ") + u.paint(numberStyle, fmt.Sprintf("PID %d", u.info.PID))
	// Keep identity paths after operational metrics so narrow sidebars show health first.
	text += "\n\n" + section("SAVED COLLECTION") + "\n" + u.paint(secondary, safe(u.profiles.Path))
	text += "\n\n" + section("WORKSPACE") + "\n" + u.paint(secondary, safe(m.active)) + "\n" + field("Endpoint", safe(filepath.Join(m.active, "hoveld.sock")), secondary) + details
	if w.failure != "" {
		text += "\n" + u.paint(errorStyle, w.failure)
	}
	return text
}

// Connection observations, not daemon availability, determine SSH health.
func (w *workspaceView) connectionState(name string) string {
	if w == nil {
		return "unknown"
	}
	if w.management.connectionError != "" {
		return "unverified"
	}
	if !w.management.connectionObserved && len(w.management.connections) == 0 {
		return "unknown"
	}
	state := "disconnected"
	for _, c := range w.management.connections {
		if name != "" {
			if c.Name == name {
				return c.State
			}
			continue
		}
		switch c.State {
		case "failed", "lost", "unverified":
			return c.State
		case "connecting", "reconnecting", "opening", "closing":
			state = "connecting"
		case "connected", "active":
			if state != "connecting" {
				state = "connected"
			}
		}
	}
	return state
}

func (w *workspaceView) statusDot(state string) string {
	dot := "○"
	switch state {
	case "connected", "active", "running":
		dot = "●"
	case "connecting", "reconnecting", "opening", "closing":
		dot = "◐"
	case "unknown", "unverified":
		dot = "◌"
	}
	return w.management.paint(connectionStyle(state), dot)
}

// All visible control cells and pointer targets come from these same layers.
func (m *frame) compositor() *lipgloss.Compositor {
	w, h := max(1, m.width), max(1, m.height)
	current := m.current()
	left, right := m.columns()
	layers := []*lipgloss.Layer{lipgloss.NewLayer(solid("", w, h, baseColor, m.noColor)).ID("background")}
	add := func(id, text string, x, y, width, height, z int) {
		if width <= 0 || height <= 0 {
			return
		}
		bg := baseColor
		if x < left || (right > 0 && x >= w-right) {
			bg = sidebarColor
		}
		layers = append(layers, lipgloss.NewLayer(solid(text, width, height, bg, m.noColor)).ID(id).X(x).Y(y).Z(z))
	}
	label := func(id, text string) string {
		if current.focus == id {
			return "> " + text
		}
		return text
	}
	state, style := current.health(m.now)
	status := current.management.paint(style, "● Daemon: "+state)
	cx, cw := left+2, w-left-right-4
	if m.demo {
		status = current.management.paint(warningStyle, "DEMO · sample data")
	}
	brand, brandHeight := "BURROW", 1
	if right-4 >= lipgloss.Width(burrowWordmark) && h >= 30 {
		brand, brandHeight = burrowWordmark, lipgloss.Height(burrowWordmark)
	}
	add("brand", current.management.paint(accent, brand), w-right+2, 1, right-4, brandHeight, 2)
	add("daemon", status, w-right+2, brandHeight+2, max(1, min(26, right-2)), 1, 2)
	tabStyle, hovelStyle := secondary, secondary
	tabLabel, hovelLabel := "  Burrow", "  Hovel"
	if current.tab == "" {
		tabStyle, tabLabel = activeStyle, "› Burrow"
	}
	if current.tab == "hovel" {
		hovelStyle, hovelLabel = activeStyle, "› Hovel"
	}
	add("burrow", current.management.paint(tabStyle, tabLabel), cx, 1, min(10, cw), 1, 1)
	add("hovel", current.management.paint(hovelStyle, hovelLabel), cx+10, 1, min(9, cw-10), 1, 1)
	totalTabs := len(current.shells) + len(current.fileViews)
	if totalTabs > 0 && cw > 19 {
		available, x := cw-19, cx+19
		tabWidth := max(5, min(18, available/totalTabs))
		count := max(1, available/tabWidth)
		if count < totalTabs && available >= 9 {
			add("previous-shell", current.management.paint(accent, "‹"), x, 1, 2, 1, 2)
			add("next-shell", current.management.paint(accent, "›"), cx+cw-2, 1, 2, 1, 2)
			x, available = x+2, available-4
			count = max(1, available/tabWidth)
		}
		selected := max(0, slices.Index(current.shells, current.shell))
		if current.tab == "files" {
			selected = len(current.shells) + slices.Index(current.fileViews, current.file)
		}
		start := min((selected/count)*count, max(0, totalTabs-count))
		for i := start; i < min(totalTabs, start+count); i++ {
			id, text, active := "", "", false
			if i < len(current.shells) {
				tab := current.shells[i]
				id, text, active = fmt.Sprintf("shell-tab:%d", i), "Shell #"+tab.id, current.tab == "shell" && current.shell == tab
				if tab.logs != nil {
					text = tab.label()
				}
				if tab.editor != nil {
					text = tab.label()
				}
			} else {
				j := i - len(current.shells)
				u := current.fileViews[j]
				id, text, active = fmt.Sprintf("file-tab:%d", j), "Files "+safe(u.files.state.Name), current.tab == "files" && current.file == u
			}
			style, marker := secondary, "  "
			if active {
				style, marker = activeStyle, "› "
			}
			add(id, current.management.paint(style, ansi.Truncate(marker+text, min(tabWidth, available), "…")), x+(i-start)*tabWidth, 1, min(tabWidth, available), 1, 2)
		}
	}
	management := *current.activeUI()
	management.help = false
	management.quitting = false
	center := management.View().Content
	add("center", center, cx, 3, cw, h-4, 1)
	// Identified row layers reuse the rendered cells rather than guessing table
	// border or wrapping offsets in the pointer handler.
	savedLine := -1
	profileStart := min(management.profileOffset, max(0, len(management.profiles.Profiles)-management.profileRows()))
	for y, line := range strings.Split(center, "\n") {
		if management.files != nil {
			break
		}
		plain := ansi.Strip(line)
		if strings.Contains(plain, "SAVED CONNECTIONS") {
			savedLine = 0
			add("saved", line, cx, y+3, cw, 1, 2)
			continue
		}
		if strings.Contains(plain, "ACTIVE SSH CONNECTIONS") {
			break
		}
		if savedLine < 0 || management.profileError != "" {
			continue
		}
		savedLine++
		i := profileStart + savedLine - 3
		if i < profileStart || i >= min(len(management.profiles.Profiles), profileStart+management.profileRows()) {
			continue
		}
		if management.selectedProfile == management.profiles.Profiles[i].Name {
			line = selectedRow(line, cw, m.noColor)
		}
		add(fmt.Sprintf("profile:%d", i), line, cx, y+3, cw, 1, 2)
	}
	activeLine := -1
	start := min(management.connectionOffset, max(0, len(management.connections)-management.connectionRows()))
	for y, line := range strings.Split(center, "\n") {
		if management.files != nil {
			break
		}
		plain := ansi.Strip(line)
		if strings.Contains(plain, "ACTIVE SSH CONNECTIONS") && management.connectionError == "" {
			activeLine = 0
			continue
		}
		if activeLine < 0 {
			continue
		}
		// Wrap(false) gives one header, one separator, then one line per row.
		activeLine++
		i := start + activeLine - 3
		if i < start || i >= min(len(management.connections), start+management.connectionRows()) {
			continue
		}
		if current.selected == management.connections[i].Name {
			// Use the first padding cell for the marker; keep all column positions.
			line = selectedRow(line, cw, m.noColor)
		}
		add(fmt.Sprintf("resource:%d", i), line, cx, y+3, cw, 1, 2)
	}

	if current.tab == "hovel" || current.tab == "shell" {
		text := "Select Hovel or press Enter to start the CLI."
		status := "CLI: not started"
		statusStyle := secondary
		if tab := current.activeTerminal(); tab != nil {
			text = tab.screen.Screen
			status = "CLI: running · " + safe(m.active)
			if tab.connection != "" {
				status = "SSH: " + safe(tab.label()) + " · local / not recorded"
				if tab.logs != nil {
					status = "Logs · read-only snapshot · Ctrl+N / :q returns"
					if tab.logs.collected {
						status = "Results · Tab switches · / searches · Ctrl+L / :qa returns"
					}
				}
				statusStyle = accent
			}
			if tab.editor != nil {
				status = "Vim · " + safe(tab.editor.profile.Name) + " · :wq save · :q! discard · Ctrl+] background"
			}
			if tab.pending {
				status = "Hovel · opening / closing…"
				if tab.connection != "" {
					status = "SSH · opening / closing…"
				}
				if tab.editor != nil {
					status = "Vim · opening / validating saved edit…"
				}
				statusStyle = warningStyle
			}
			if tab.screen.Exited {
				status = "CLI: exited"
				statusStyle = errorStyle
			}
			if tab.error != "" {
				status = tab.error
				statusStyle = errorStyle
				if tab.host == nil {
					text = current.management.paint(errorStyle, tab.error)
				}
			}
			if tab.screen.ScrollOffset > 0 {
				status = fmt.Sprintf("History %d/%d · %s", tab.screen.ScrollOffset, tab.screen.HistoryLines, status)
			}
		}
		r := m.terminalBounds()
		add("terminal", text, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), 3)
		add("terminal-status", current.management.paint(statusStyle, status), cx, h-2, cw, 1, 3)
		if current.tab == "hovel" && current.canRestartCLI() {
			add("restart-cli", current.management.paint(activeStyle, "[Restart CLI]"), cx, h-3, min(13, cw), 1, 4)
		}
	}
	footer := "Management · focus: " + current.focus
	if management.files != nil {
		footer = "File mode · Ctrl+C cancel/back · F1 help · PgUp/PgDn scroll"
	}
	add("footer", current.management.paint(secondary, footer), cx, h-4, cw, 1, 2)
	if right > 0 {
		add("metadata", "", w-right, 0, right, h, 0)
		metadataY := brandHeight + 4
		add("metadata", ansi.Wrap(m.metadata(), right-4, ""), w-right+2, metadataY, right-4, h-metadataY-1, 1)
		add("separator", current.management.paint(separatorStyle, strings.Repeat("│\n", h)), w-right, 0, 1, h, 2)
	}
	sidebar := func(width, z int) {
		add("workspaces", "", 0, 0, width, h, z-1)
		add("separator", current.management.paint(separatorStyle, strings.Repeat("│\n", h)), width-1, 0, 1, h, z+1)
		width -= 3
		mid := h / 2
		add("workspaces", current.management.paint(heading, label("workspaces", "WORKSPACES")), 1, 1, width, max(1, mid-2), z)
		start := min(m.navOffset, max(0, len(m.paths)-m.sidebarRows()))
		for i := start; i < min(len(m.paths), start+m.sidebarRows()); i++ {
			path := m.paths[i]
			line := " " + current.statusDot(m.workspaces[path].connectionState("")) + " " + current.management.paint(accent, safe(filepath.Base(path)))
			line = ansi.Truncate(line, width, "…")
			if (current.focus == "workspaces" && i == m.navIndex) || (current.focus != "workspaces" && path == m.active && current.tab == "") {
				line = selectedRow(line, width, m.noColor)
			}
			add(fmt.Sprintf("workspace:%d", i), line, 1, 3+i-start, width, 1, z+1)
		}
		add("shell-list", current.management.paint(heading, label("shells", "SHELLS - SSH")), 1, mid+2, width, max(1, h-mid-3), z)
		entries := m.shellEntries()
		start = min(m.shellOffset, max(0, len(entries)-m.shellRows()))
		end := min(len(entries), start+m.shellRows())
		for i := start; i < end; i++ {
			entry := entries[i]
			path := m.paths[entry.workspace]
			workspace := m.workspaces[path]
			line := " " + current.statusDot(workspace.connectionState("")) + " " + current.management.paint(accent, safe(filepath.Base(path)))
			id := fmt.Sprintf("shell:%d", i)
			if tab := entry.tab; tab != nil {
				state := tab.state()
				if state == "running" {
					state = workspace.connectionState(tab.connection)
				}
				line = "   " + workspace.statusDot(state) + " " + current.management.paint(accent, "#"+tab.id) + " " + current.management.paint(secondary, safe(tab.connection))
				if tab.editor != nil {
					line = "   " + workspace.statusDot(tab.state()) + " " + current.management.paint(infoStyle, safe(tab.label()))
				}
			}
			if u := entry.files; u != nil {
				line = "   " + workspace.statusDot(u.fileState()) + " " + current.management.paint(infoStyle, "Files ") + current.management.paint(secondary, safe(u.files.state.Name))
			}
			line = ansi.Truncate(line, width, "…")
			if (current.focus == "shells" && i == m.shellIndex) || (entry.tab != nil && m.active == path && current.tab == "shell" && current.shell == entry.tab) || (entry.files != nil && m.active == path && current.tab == "files" && current.file == entry.files) {
				line = selectedRow(line, width, m.noColor)
			}
			add(id, line, 1, mid+4+i-start, width, 1, z+1)
		}
		add("new", current.management.paint(heading, label("new", "[New]")), 1, mid, width/2, 1, z+1)
		add("menu", lipgloss.PlaceHorizontal(width-width/2, lipgloss.Right, current.management.paint(heading, label("menu", "[Menu]"))), 1+width/2, mid, width-width/2, 1, z+1)
	}
	if left > 0 {
		sidebar(left, 2)
	}
	layers = append(layers, lipgloss.NewLayer(m.commandHelp()).Y(h-1).Z(3).ID("command-help"))
	if m.modal != "" {
		// A full-screen identified backdrop captures every pointer event.
		base := lipgloss.NewCompositor(layers...).Render()
		if !m.noColor {
			base = solid(secondary.Render(ansi.Strip(base)), w, h, baseColor, false)
		}
		layers = []*lipgloss.Layer{lipgloss.NewLayer(base).ID("modal-backdrop")}
		if m.modal == "navigation" {
			sidebar(min(32, w), 3)
			add("dismiss", "[Esc close]", max(0, w-12), 1, min(12, w), 1, 5)
		} else {
			bounds := m.dialogBounds()
			pw, ph := bounds.Dx(), bounds.Dy()
			x, y := bounds.Min.X, bounds.Min.Y
			bw := pw - 6
			text := ""
			switch m.modal {
			case "new", "menu", "context", "connect", "review", "auth", "quit", "browse", "profile-menu", "profile-save":
				text = m.formText()
			case "metadata":
				v := scrollBody(m.metadata(), bw, ph-6, m.modalOffset)
				text = v.View()
			case "downloads":
				v := m.downloadsViewport()
				text = centered(current.management.paint(accent, "Transfers"), bw) + "\n\n" + v.View()
			}
			popup := dialogStyle.Width(pw).Height(ph).Render(fit(text, bw, ph-4))
			layers = append(layers, lipgloss.NewLayer(solid(popup, pw, ph, popupColor, m.noColor)).ID("modal").X(x).Y(y).Z(3))
			control := func(id, text string, y int) {
				layers = append(layers, lipgloss.NewLayer(solid(text, bw, 1, popupColor, m.noColor)).ID(id).X(x+3).Y(y).Z(4))
			}
			for id, row := range m.formControls(text) {
				if row < ph-5 {
					control(id, strings.Split(text, "\n")[row], y+2+row)
				}
			}
			if m.form != nil {
				for row, line := range strings.Split(text, "\n") {
					if row >= ph-5 {
						break
					}
					plain := ansi.Strip(line)
					for _, label := range []string{"Proceed", "Download", "Cancel", "Trust host", "Reject", "Quit", "Keep working", "Keep running", "Close connections"} {
						if at := strings.Index(plain, label+"]"); at >= 0 {
							start := ansi.StringWidth(plain[:at])
							id := "confirm-reject"
							if label == "Proceed" || label == "Download" || label == "Trust host" || label == "Quit" || label == "Keep running" {
								id = "confirm-accept"
							}
							layers = append(layers, lipgloss.NewLayer(solid(ansi.Cut(line, start, start+len(label)), len(label), 1, popupColor, m.noColor)).ID(id).X(x+3+start).Y(y+2+row).Z(5))
						}
					}
				}
			}
			if m.modal == "new" && !m.launchPending {
				control("submit", current.management.paint(heading, "[Create & open]"), y+ph-4)
			}
			if m.form != nil && (m.modal == "new" || m.modal == "connect") {
				if input, ok := m.form.GetFocusedField().(*huh.Input); ok && (m.modal == "new" || input.GetKey() == "key" || input.GetKey() == "config") {
					label := current.management.paint(heading, "[Browse paths]")
					layers = append(layers, lipgloss.NewLayer(solid(label, 14, 1, popupColor, m.noColor)).ID("browse").X(x+pw-17).Y(y+ph-4).Z(5))
				}
			}
			hint := "[Esc close]"
			if m.modal == "auth" {
				hint = "Esc / Ctrl+C cancel connection"
				if m.question != nil {
					hint = "Enter submit · " + hint
				}
			}
			if m.modal == "quit" {
				hint = "↑↓ scroll connections · Esc cancel"
			}
			if m.modal == "review" && (m.reviewText != "" || m.downloadPlan != nil) {
				hint = "↑↓ / PgUp/PgDn scroll recap · Esc cancel"
			}
			if m.modal == "downloads" {
				hint = "Esc close · ↑↓ / PgUp/PgDn scroll · transfers reopens"
			}
			if m.modal == "menu" {
				hint = "↑↓ select · Enter run · Esc close"
			}
			hint = current.management.paint(secondary, hint)
			hint = centered(hint, bw)
			control("dismiss", hint, y+ph-3)
		}
	}
	if current.activeUI().help {
		return current.activeUI().overlay(lipgloss.NewCompositor(layers...).Render(), w, h)
	}
	return lipgloss.NewCompositor(layers...)
}
func (m *frame) dialogBounds() image.Rectangle {
	if m.modal == "context" && m.contextMenu != nil {
		width, height := min(44, m.width-2), min(12, m.height-2)
		x, y := max(0, min(m.contextMenu.at.X, m.width-width)), max(0, min(m.contextMenu.at.Y, m.height-height))
		return image.Rect(x, y, x+width, y+height)
	}
	pw, ph := min(76, m.width-4), min(18, m.height-4)
	if m.modal == "downloads" {
		pw, ph = helpSize(m.width, m.height)
	}
	if m.modal == "new" {
		ph = min(22, m.height-4)
	}
	if m.modal == "quit" {
		pw, ph = min(76, m.width-4), min(max(20, m.quitReview.count()+17), min(26, m.height-4))
	}
	if m.modal == "review" {
		// Size from the complete recap, never the scroll position. The command
		// gets the available terminal width before wrapping is necessary.
		pw = min(max(76, lipgloss.Width(m.reviewText)+6), m.width-4)
		ph = min(max(18, lipgloss.Height(ansi.Wrap(m.reviewText, max(1, pw-6), ""))+14), m.height-4)
		if m.downloadPlan != nil {
			pw, ph = min(76, m.width-4), min(len(m.downloadPlan.Files)+18, min(28, m.height-4))
		}
	}
	if m.modal == "connect" {
		pw, ph = min(96, m.width-4), min(34, m.height-4)
	}
	x, y := (m.width-pw)/2, (m.height-ph)/2
	return image.Rect(x, y, x+pw, y+ph)
}
func (m *frame) View() tea.View {
	if m.tooSmall() {
		if m.modal == "quit" {
			text := "Quit?\nCtrl+C: keep and quit\nEsc: cancel\nResize to review/close\n" + m.quitSummary()
			if m.quitClosing {
				text = "Closing connections…\nWaiting for verified cleanup."
			}
			v := tea.NewView(fit(text, m.width, m.height))
			v.AltScreen = true
			return v
		}
		// Keep the application visible even in a terminal too small for controls.
		preview := *m
		preview.width, preview.height = max(minimumWidth, m.width), max(minimumHeight, m.height)
		text := preview.compositor().Render()
		if m.noColor {
			text = ansi.Strip(text)
		}
		v := tea.NewView(fit(text, m.width, m.height))
		v.AltScreen = true
		return v
	}
	base := m.compositor().Render()
	if m.noColor {
		base = ansi.Strip(base)
	} else {
		base = solid(base, m.width, m.height, baseColor, false)
	}
	v := tea.NewView(m.selectionView(fit(base, m.width, m.height)))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	if m.mouseDisabled {
		v.MouseMode = tea.MouseModeNone
	}
	if m.terminalFocused() && m.current().activeTerminal() != nil && m.current().activeTerminal().screen.MouseMotion && !m.mouseDisabled {
		v.MouseMode = tea.MouseModeAllMotion
	}
	if !m.current().activeUI().help && !m.hasSelection() {
		x, y := 0, 0
		switch m.modal {

		case "":
			if m.terminalFocused() && m.current().activeTerminal() != nil {
				s := m.current().activeTerminal().screen
				r := m.terminalBounds()
				if s.Visible && !s.Exited && s.Cursor.In(image.Rect(0, 0, r.Dx(), r.Dy())) {
					v.Cursor = tea.NewCursor(s.Cursor.X, s.Cursor.Y)
					x, y = r.Min.X, r.Min.Y
				}
			}
			if m.current().focus == "prompt" && (m.current().tab == "" || m.current().tab == "files") {
				v.Cursor = m.current().activeUI().input.Cursor()
				left, _ := m.columns()
				x, y = left+2, m.height-2
			}
		}
		if v.Cursor != nil {
			v.Cursor.Position.X += x
			v.Cursor.Position.Y += y
			if m.noColor {
				v.Cursor.Color = nil
			}
		}
	}
	if m.noColor && m.form != nil && !m.current().activeUI().help {
		// Huh exposes its caret as a reverse-video cell, not a public Cursor().
		// Recover that exact cell before NO_COLOR stripping; reuse the compositor's
		// native cell parser rather than duplicating textinput's editing offsets.
		r := m.dialogBounds()
		canvas := lipgloss.NewCanvas(r.Dx()-6, r.Dy()-6).Compose(lipgloss.NewLayer(m.formText()))
		for row := 0; row < r.Dy()-6; row++ {
			for col := 0; col < r.Dx()-6; col++ {
				if cell := canvas.CellAt(col, row); cell != nil && cell.Style.Attrs&uv.AttrReverse != 0 {
					v.Cursor = tea.NewCursor(r.Min.X+3+col, r.Min.Y+2+row)
					return v
				}
			}
		}
	}

	return v
}
