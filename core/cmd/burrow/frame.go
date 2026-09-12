package main

import (
	"context"
	"fmt"
	"image"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Bochner/burrow/core/launch"
	"github.com/charmbracelet/x/ansi"
)

var focusNext = key.NewBinding(key.WithKeys("f6", "shift+f6"))
var newWorkspace = key.NewBinding(key.WithKeys("alt+n"))
var openMenu = key.NewBinding(key.WithKeys("alt+m", "ctrl+p"))
var openNavigation = key.NewBinding(key.WithKeys("alt+w"))

const freshFor = 8 * time.Second

type workspaceView struct {
	management            ui
	focus, tab, selected  string
	shellOffset           int
	checking, showCheck   bool
	lastSuccess, observed time.Time
	duration              time.Duration
	failure               string
}

// This is presentation state only. All resource snapshots come from Hovel.
// Explicit paths live for this frontend session; no directory scanning or registry.
type frame struct {
	workspaces             map[string]*workspaceView
	paths                  []string
	active                 string
	options                launch.Options
	width, height          int
	noColor                bool
	demo                   bool
	navIndex, navOffset    int
	modal                  string
	modalOffset, menuIndex int
	destination            textinput.Model
	palette                textinput.Model
	launchPending          bool
	launchError            string
	sequence               uint64
	inputEpoch             uint64
	pending                map[uint64]string
	now                    time.Time
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
type modalInputResult struct {
	epoch   uint64
	modal   string
	message tea.Msg
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
	return tea.Batch(m.dispatch(m.active, m.current().management.Init()), m.check(m.active), m.tick())
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
	return "verified", successStyle
}
func (m *frame) resize() {
	left, right := m.columns()
	for _, w := range m.workspaces {
		w.management.width = max(1, m.width-left-right-4)
		w.management.height = max(1, m.height-4)
		w.management.input.SetWidth(max(1, w.management.width-4))
	}
}

const minimumWidth, minimumHeight = 160, 40

func (m *frame) tooSmall() bool      { return m.width < minimumWidth || m.height < minimumHeight }
func (m *frame) columns() (int, int) { return 26, 32 }
func (m *frame) selectWorkspace(i int) tea.Cmd {
	if i < 0 || i >= len(m.paths) {
		return nil
	}
	m.active = m.paths[i]
	m.navIndex = i
	m.modal = ""
	return m.check(m.active)
}
func (m *frame) openNew() {
	m.modal = "new"
	if m.launchPending {
		return
	}
	m.launchError = ""
	m.inputEpoch++
	m.destination = textinput.New()
	styleInput(&m.destination)
	m.destination.Prompt = "> "
	m.destination.Placeholder = "/absolute/workspace"
	m.destination.CharLimit = 2048
	m.destination.SetWidth(max(1, min(64, min(76, m.width-4)-9)))
	m.destination.Focus()
}
func (m *frame) submitWorkspace() tea.Cmd {
	if m.demo {
		m.launchError = "Sample data preview · workspace launch disabled"
		return nil
	}
	if m.launchPending {
		return nil
	}
	path := m.destination.Value()
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		m.launchError = "Use an absolute canonical path (no trailing / or ..)."
		return nil
	}
	m.launchPending = true
	m.launchError = ""
	options := m.options
	options.Workspace = path
	return m.dispatch(m.active, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		info, err := launch.Open(ctx, options)
		return workspaceOpened{info, err, path}
	})
}
func (m *frame) updateManagement(path string, msg tea.Msg) tea.Cmd {
	w := m.workspaces[path]
	model, cmd := w.management.Update(msg)
	w.management = model.(ui)
	if w.management.help {
		v := w.management.helpViewport(m.width, m.height)
		w.management.helpOffset = v.YOffset()
	}
	return m.dispatch(path, cmd)
}
func (m *frame) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
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
		case tea.QuitMsg:
			return m, tea.Quit
		case frameTick:
			m.now = time.Time(result)
			var cmd tea.Cmd
			w := m.current()
			if m.now.Sub(w.observed) >= 3*time.Second {
				cmd = m.check(m.active)
			}
			return m, tea.Batch(cmd, m.tick())
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
		case modalInputResult:
			if path != m.active || result.epoch != m.inputEpoch || result.modal != m.modal || (result.modal == "new" && m.launchPending) {
				return m, nil
			}
			if batch, ok := result.message.(tea.BatchMsg); ok {
				var cmds []tea.Cmd
				for _, cmd := range batch {
					cmds = append(cmds, m.inputCommand(result.modal, cmd))
				}
				return m, tea.Batch(cmds...)
			}
			return m, m.updateModalInput(result.modal, result.message)
		case workspaceOpened:
			m.launchPending = false
			if result.err != nil {
				m.launchError = "REFUSED: " + safe(result.err.Error())
				m.workspaces[path].management.output = m.launchError
				return m, nil
			}
			destination := result.info.Workspace
			var init tea.Cmd
			if _, ok := m.workspaces[destination]; !ok {
				m.paths = append(m.paths, destination)
				m.workspaces[destination] = &workspaceView{management: newUI(result.info, m.noColor), focus: "prompt"}
				init = m.dispatch(destination, m.workspaces[destination].management.Init())
			}
			m.resize()
			// Esc may dismiss a pending launch, but its completion never steals selection.
			if m.modal == "new" && m.active == path && m.destination.Value() == result.destination {
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
		m.width = max(1, v.Width)
		m.height = max(1, v.Height)
		m.resize()
		m.destination.SetWidth(max(1, min(64, min(76, m.width-4)-9)))
		m.palette.SetWidth(max(1, min(76, m.width-4)-9))
		return m, nil
	case tea.ColorProfileMsg:
		for path := range m.workspaces {
			m.updateManagement(path, v)
		}
		m.noColor = m.current().management.noColor
		return m, nil
	case tea.MouseMsg:
		if m.tooSmall() {
			return m, nil
		}
		// All pointer types are captured by the active modal, including the child's
		// help/quit overlay. An unnamed visual layer cannot allow click-through.
		mouse := v.Mouse()
		hit := m.compositor().Hit(mouse.X, mouse.Y)
		if wheel, ok := msg.(tea.MouseWheelMsg); ok {
			delta := 1
			if wheel.Button == tea.MouseWheelUp {
				delta = -1
			}
			if m.current().management.help {
				u := &m.current().management
				u.helpOffset = max(0, u.helpOffset+delta)
				v := u.helpViewport(m.width, m.height)
				u.helpOffset = v.YOffset()
				return m, nil
			}
			if m.modal == "menu" {
				m.menuIndex = max(0, min(len(m.menuMatches())-1, m.menuIndex+delta))
				return m, nil
			}
			if m.modal != "" && m.modal != "navigation" {
				if m.modal == "metadata" {
					m.scrollMetadata(delta)
				}
				return m, nil
			}
			if m.current().management.quitting {
				return m, nil
			}
			if strings.HasPrefix(hit.ID(), "workspace:") || hit.ID() == "workspaces" {
				m.navOffset = max(0, min(len(m.paths)-1, m.navOffset+delta))
			}
			if hit.ID() == "shells" {
				m.current().shellOffset = max(0, min(len(m.paths)-1, m.current().shellOffset+delta))
			}
			if strings.HasPrefix(hit.ID(), "resource:") {
				u := &m.current().management
				u.connectionOffset = max(0, min(max(0, len(u.connections)-u.connectionRows()), u.connectionOffset+delta))
			}
			if hit.ID() == "center" {
				m.current().management.outputOffset = max(0, m.current().management.outputOffset+delta)
			}
			return m, nil
		}
		if click, ok := msg.(tea.MouseClickMsg); !ok || click.Button != tea.MouseLeft {
			return m, nil
		}
		if m.current().management.help || m.current().management.quitting {
			switch hit.ID() {
			case "quit-leave":
				return m, tea.Quit
			case "dismiss":
				m.current().management.help = false
				m.current().management.quitting = false
			}
			return m, nil
		}
		return m, m.activate(hit.ID())
	case tea.PasteMsg:
		if m.tooSmall() || m.current().management.help || m.current().management.quitting {
			return m, nil
		}
		if m.modal == "menu" {
			m.palette.SetValue(m.palette.Value() + safe(v.Content))
			m.palette.CursorEnd()
			m.menuIndex = 0
			return m, nil
		}
		if m.modal == "new" && !m.launchPending {
			m.destination.SetValue(m.destination.Value() + safe(v.Content))
			m.destination.CursorEnd()
			return m, nil
		}
		if m.modal != "" || m.current().focus != "prompt" || m.current().tab != "" {
			return m, nil
		}
	case tea.KeyPressMsg:
		if m.tooSmall() {
			if key.Matches(v, quit) {
				return m, tea.Quit
			}
			return m, nil
		}
		if m.current().management.help || m.current().management.quitting {
			return m, m.updateManagement(m.active, msg)
		}
		if m.modal != "" {
			return m, m.modalKey(v)
		}
		switch {
		case key.Matches(v, newWorkspace):
			m.openNew()
			return m, nil
		case key.Matches(v, openMenu):
			m.openPalette()
			return m, nil
		case key.Matches(v, openNavigation):
			m.modal = "navigation"
			return m, nil
		case key.Matches(v, focusNext):
			names := []string{"prompt", "workspaces", "new", "menu", "shells", "tabs", "resources"}
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
				m.navIndex = max(0, min(len(m.paths)-1, m.navIndex+delta))
				m.navOffset = max(0, min(m.navOffset, m.navIndex))
				if m.navIndex >= m.navOffset+max(1, m.height/2-5) {
					m.navOffset = m.navIndex - max(1, m.height/2-5) + 1
				}
				if key.Matches(v, enter) {
					return m, m.selectWorkspace(m.navIndex)
				}
			case "shells":
				w.shellOffset = max(0, min(len(m.paths)-1, w.shellOffset+delta))
			case "tabs":
				if key.Matches(v, choose, previous, next) {
					if w.tab == "" {
						w.tab = "hovel"
					} else {
						w.tab = ""
					}
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
				w.focus = "prompt"
				w.tab = ""
			}
			return m, nil
		}
		if w.tab != "" {
			if key.Matches(v, escape) {
				w.tab = ""
			}
			return m, nil
		}
	}
	return m, m.updateManagement(m.active, msg)
}

var menuActions = []string{"Check daemon", "Metadata", "New workspace", "Keyboard help", "Quit"}

func (m *frame) activate(id string) tea.Cmd {
	if m.modal != "" && m.modal != "navigation" {
		if id == "dismiss" {
			m.modal = ""
			return nil
		}
		if m.modal == "new" && id == "submit" {
			return m.submitWorkspace()
		}
		if m.modal == "menu" && strings.HasPrefix(id, "action:") {
			i, _ := strconv.Atoi(strings.TrimPrefix(id, "action:"))
			return m.menuAction(i)
		}
		return nil
	}
	if strings.HasPrefix(id, "resource:") {
		i, err := strconv.Atoi(strings.TrimPrefix(id, "resource:"))
		rows := m.current().management.connections
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
	switch id {
	case "new":
		m.openNew()
	case "menu":
		m.openPalette()
	case "navigation":
		m.modal = "navigation"
	case "daemon":
		m.modal = "metadata"
		m.modalOffset = 0
	case "burrow":
		m.current().tab = ""
		m.current().focus = "prompt"
	case "hovel":
		m.current().tab = "hovel"
		m.current().focus = "tabs"
	case "center":
		m.current().focus = "prompt"
	case "shells":
		m.current().focus = "shells"
	case "dismiss":
		m.modal = ""
	}
	return nil
}
func (m *frame) openPalette() {
	m.modal = "menu"
	m.menuIndex = 0
	m.inputEpoch++
	m.palette = textinput.New()
	styleInput(&m.palette)
	m.palette.Prompt = "› "
	m.palette.Placeholder = "Type to filter"
	m.palette.CharLimit = 128
	m.palette.SetWidth(max(1, min(76, m.width-4)-9))
	m.palette.Focus()
}
func (m *frame) menuMatches() []int {
	// ponytail: substring filtering for five actions; use fuzzy matching if this catalog grows.
	var matches []int
	query := strings.ToLower(m.palette.Value())
	for i, action := range menuActions {
		if strings.Contains(strings.ToLower(action), query) {
			matches = append(matches, i)
		}
	}
	return matches
}

// Clipboard results belong to the input instance that requested them, just as
// daemon results belong to their originating workspace.
func (m *frame) inputCommand(modal string, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	epoch := m.inputEpoch
	return m.dispatch(m.active, func() tea.Msg { return modalInputResult{epoch, modal, cmd()} })
}
func (m *frame) updateModalInput(modal string, msg tea.Msg) tea.Cmd {
	input := &m.palette
	if modal == "new" {
		input = &m.destination
	}
	before := input.Value()
	model, cmd := input.Update(msg)
	*input = model
	if modal == "menu" && input.Value() != before {
		m.menuIndex = 0
	}
	return m.inputCommand(modal, cmd)
}
func (m *frame) menuAction(i int) tea.Cmd {
	m.modal = ""
	switch i {
	case 0:
		return m.check(m.active)
	case 1:
		m.modal = "metadata"
		m.modalOffset = 0
	case 2:
		m.openNew()
	case 3:
		m.current().management.help = true
	case 4:
		m.current().management.quitting = true
		m.current().management.leave = false
	}
	return nil
}
func (m *frame) modalKey(v tea.KeyPressMsg) tea.Cmd {
	if key.Matches(v, escape) {
		m.modal = ""
		return nil
	}
	switch m.modal {
	case "new":
		if key.Matches(v, enter) {
			return m.submitWorkspace()
		}
		if !m.launchPending {
			return m.updateModalInput("new", v)
		}
	case "menu":
		matches := m.menuMatches()
		if key.Matches(v, previous) || v.String() == "ctrl+p" {
			if len(matches) > 0 {
				m.menuIndex = (m.menuIndex - 1 + len(matches)) % len(matches)
			}
		} else if key.Matches(v, next) || v.String() == "ctrl+n" {
			if len(matches) > 0 {
				m.menuIndex = (m.menuIndex + 1) % len(matches)
			}
		} else if key.Matches(v, enter) {
			if len(matches) > 0 {
				return m.menuAction(matches[m.menuIndex])
			}
		} else {
			return m.updateModalInput("menu", v)
		}
	case "navigation":
		if key.Matches(v, newWorkspace) {
			m.openNew()
		}
		if key.Matches(v, openMenu) {
			m.openPalette()
		}
		if key.Matches(v, previous) {
			m.navIndex = max(0, m.navIndex-1)
		}
		if key.Matches(v, next) {
			m.navIndex = min(len(m.paths)-1, m.navIndex+1)
		}
		m.navOffset = m.navIndex
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
	u := w.management
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
	details := ""
	for _, row := range u.connections {
		if row.Name != w.selected {
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
	files, bytes := "Unavailable", "Unavailable"
	transferNote := "Transfers not implemented"
	if m.demo {
		files, bytes = "12", "48.6 MiB"
		transferNote = "Sample downloads"
	}
	count := fmt.Sprint(connected)
	if !u.connectionObserved && len(u.connections) == 0 {
		count = "Unknown"
	}
	text := section("SELECTED CONNECTION") + "\n" + u.paint(connectionColor.Bold(true), connectionState) + "\n" + selected + "\n" + field("Active in workspace", count, numberStyle) + "\n\n" +
		section("WORKSPACE DOWNLOADS") + "\n" + field("Files", files, numberStyle) + "\n" + field("Total size", bytes, numberStyle) + "\n" + u.paint(secondary, transferNote) + "\n\n" +
		section("HOVEL DAEMON") + "\n"
	if m.demo {
		return text + u.paint(warningStyle, "DEMO · no daemon") + details
	}
	age := "Never"
	if !w.lastSuccess.IsZero() {
		age = fmt.Sprintf("%ds ago", max(0, int(m.now.Sub(w.lastSuccess).Seconds())))
	}
	text += u.paint(statusStyle, strings.ToUpper(state)) + "\n" + field("Last success", age, infoStyle) + "\n" + field("Latency", w.duration.Round(time.Millisecond).String(), numberStyle) + "\n" + u.paint(secondary, "Last known ") + u.paint(numberStyle, fmt.Sprintf("PID %d", u.info.PID))
	// Keep identity paths after operational metrics so narrow sidebars show health first.
	text += "\n\n" + section("WORKSPACE") + "\n" + u.paint(secondary, safe(m.active)) + "\n" + field("Endpoint", safe(filepath.Join(m.active, "hoveld.sock")), secondary) + details
	if w.failure != "" {
		text += "\n" + u.paint(errorStyle, w.failure)
	}
	return text
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
	status := current.management.paint(style, "● Hovel: "+state)
	cx, cw := left+2, w-left-right-4
	if m.demo {
		status = current.management.paint(warningStyle, "DEMO · sample data")
	}
	add("daemon", status, w-26, 1, 26, 1, 2)
	tabStyle, hovelStyle := activeStyle, secondary
	tabLabel, hovelLabel := "› Burrow", "  Hovel · soon"
	if current.tab != "" {
		tabStyle, hovelStyle = secondary, activeStyle
		tabLabel, hovelLabel = "  Burrow", "› Hovel · soon"
	}
	add("burrow", current.management.paint(tabStyle, tabLabel), cx, 1, 10, 1, 1)
	add("hovel", current.management.paint(hovelStyle, hovelLabel), cx+10, 1, min(16, w-cx-10), 1, 1)
	management := current.management
	management.help = false
	management.quitting = false
	center := management.View().Content
	add("center", center, cx, 3, cw, h-4, 1)
	// Identified row layers reuse the rendered cells rather than guessing table
	// border or wrapping offsets in the pointer handler.
	inActive := false
	for y, line := range strings.Split(center, "\n") {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "ACTIVE SSH CONNECTIONS") {
			inActive = true
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(plain), "TUNNELS") {
			inActive = false
		}
		if !inActive {
			continue
		}
		for i, row := range current.management.connections {
			fields := strings.Fields(plain)
			if len(fields) > 0 && fields[0] == row.Name {
				if current.selected == row.Name {
					line = current.management.choice(strings.TrimPrefix(plain, "  "), true, cw)
				}
				add(fmt.Sprintf("resource:%d", i), line, cx, y+3, cw, 1, 2)
				break
			}
		}
	}

	if current.tab != "" {
		add("center", "Hovel CLI · not implemented (#68)\n\nThe embedded terminal will appear here.\nEsc or Burrow tab returns to management.", cx, 3, cw, max(1, h-4), 3)
	}
	add("footer", current.management.paint(secondary, "Management · focus: "+current.focus), cx, h-4, cw, 1, 2)
	if right > 0 {
		add("metadata", "", w-right, 0, right, h, 0)
		add("metadata", ansi.Wrap(m.metadata(), right-4, ""), w-right+2, 3, right-4, h-4, 1)
		add("separator", current.management.paint(separatorStyle, strings.Repeat("│\n", h)), w-right, 0, 1, h, 2)
	}
	sidebar := func(width, z int) {
		add("workspaces", "", 0, 0, width, h, z-1)
		add("separator", current.management.paint(separatorStyle, strings.Repeat("│\n", h)), width-1, 0, 1, h, z+1)
		width -= 3
		midpoint := max(4, h/2)
		add("workspaces", current.management.paint(heading, label("workspaces", "WORKSPACES")), 1, 1, width, max(1, midpoint-1), z)
		rows := max(1, midpoint-5)
		start := min(m.navOffset, max(0, len(m.paths)-1))
		end := min(len(m.paths), start+rows)
		for i := start; i < end; i++ {
			prefix := "  "
			if m.paths[i] == m.active {
				prefix = "● "
			}
			if current.focus == "workspaces" && i == m.navIndex {
				prefix = "> "
			}
			add(fmt.Sprintf("workspace:%d", i), current.management.paint(func() lipgloss.Style {
				if m.paths[i] == m.active || (current.focus == "workspaces" && i == m.navIndex) {
					return activeStyle.Width(width)
				}
				return pageStyle
			}(), ansi.Truncate(prefix+safe(filepath.Base(m.paths[i])), width, "…")), 1, 3+i-start, width, 1, z+1)
		}

		add("new", current.management.paint(heading, label("new", "[New]")), 1, midpoint, width/2, 1, z+1)
		add("menu", lipgloss.PlaceHorizontal(width-width/2, lipgloss.Right, current.management.paint(heading, label("menu", "[Menu]"))), 1+width/2, midpoint, width-width/2, 1, z+1)
		gap, groupHeight := "\n\n", 3
		rows = max(1, (h-midpoint-6)/groupHeight)
		start = min(current.shellOffset, max(0, len(m.paths)-1))
		end = min(len(m.paths), start+rows)
		text := current.management.paint(heading, label("shells", "SHELLS")) + "\n" + current.management.paint(secondary, "Not implemented")
		for _, path := range m.paths[start:end] {
			text += gap + safe(filepath.Base(path)) + "\n  No shells"
		}
		if end-start < len(m.paths) {
			text += fmt.Sprintf("\n%d–%d/%d", start+1, end, len(m.paths))
		}
		add("shells", text, 1, midpoint+2, width, max(1, h-midpoint-3), z)
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
			case "new":
				text = centered(current.management.paint(accent, "NEW / OPEN WORKSPACE"), bw) + "\n\nExact destination (Enter launches):\n" + m.destination.View() + "\n\n" + current.management.paint(errorStyle, m.launchError)
				if m.launchPending {
					text += "\nLaunching and verifying…"
				}
			case "menu":
				text = current.management.paletteTitle(bw) + "\n\n" + m.palette.View()
			case "metadata":
				v := scrollBody(m.metadata(), bw, ph-6, m.modalOffset)
				text = v.View()
			}
			popup := dialogStyle.Width(pw).Height(ph).Render(fit(text, bw, ph-4))
			layers = append(layers, lipgloss.NewLayer(solid(popup, pw, ph, popupColor, m.noColor)).ID("modal").X(x).Y(y).Z(3))
			control := func(id, text string, y int) {
				layers = append(layers, lipgloss.NewLayer(solid(text, bw, 1, popupColor, m.noColor)).ID(id).X(x+3).Y(y).Z(4))
			}
			if m.modal == "menu" {
				matches := m.menuMatches()
				count := max(1, ph-9)
				start := max(0, m.menuIndex-count+1)
				for j := start; j < min(len(matches), start+count); j++ {
					i := matches[j]
					control(fmt.Sprintf("action:%d", i), current.management.choice(menuActions[i], j == m.menuIndex, bw), y+6+j-start)
				}
				if len(matches) == 0 {
					control("no-matches", current.management.paint(secondary, "No matching commands"), y+6)
				}
			}
			if m.modal == "new" {
				control("submit", centered(current.management.paint(heading, "[Launch exact destination]"), bw), y+ph-4)
			}
			hint := "[Esc close]"
			if m.modal == "menu" {
				hint = "↑↓ select · Enter run · Esc close"
			}
			hint = current.management.paint(secondary, hint)
			if m.modal != "menu" {
				hint = centered(hint, bw)
			}
			control("dismiss", hint, y+ph-3)
		}
	}
	if current.management.help || current.management.quitting {
		return current.management.overlay(lipgloss.NewCompositor(layers...).Render(), w, h)
	}
	return lipgloss.NewCompositor(layers...)
}
func (m *frame) dialogBounds() image.Rectangle {
	pw, ph := min(76, m.width-4), min(18, m.height-4)
	x, y := (m.width-pw)/2, (m.height-ph)/2
	return image.Rect(x, y, x+pw, y+ph)
}
func (m *frame) View() tea.View {
	if m.tooSmall() {
		text := fmt.Sprintf("Resize window\nMinimum %d × %d\nCurrent %d × %d\nCtrl+C to quit", minimumWidth, minimumHeight, m.width, m.height)
		text = m.current().management.paint(accent, text)
		v := tea.NewView(solid(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, fit(text, m.width, min(4, m.height))), m.width, m.height, baseColor, m.noColor))
		v.AltScreen = true
		return v
	}
	base := m.compositor().Render()
	if m.noColor {
		base = ansi.Strip(base)
	} else {
		base = solid(base, m.width, m.height, baseColor, false)
	}
	v := tea.NewView(fit(base, m.width, m.height))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	if !m.current().management.help && !m.current().management.quitting {
		x, y := 0, 0
		switch m.modal {
		case "menu":
			v.Cursor = m.palette.Cursor()
			r := m.dialogBounds()
			x, y = r.Min.X+3, r.Min.Y+4
		case "new":
			if !m.launchPending {
				v.Cursor = m.destination.Cursor()
				r := m.dialogBounds()
				x, y = r.Min.X+3, r.Min.Y+5
			}
		case "":
			if m.current().focus == "prompt" && m.current().tab == "" {
				v.Cursor = m.current().management.input.Cursor()
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
	return v
}
