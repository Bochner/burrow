package main

import (
	"context"
	"fmt"
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
var openMenu = key.NewBinding(key.WithKeys("alt+m"))
var openNavigation = key.NewBinding(key.WithKeys("alt+w"))
var green = lipgloss.NewStyle().Foreground(lipgloss.Color("#50fa7b"))
var yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("#f1fa8c"))
var red = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555"))

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
	navIndex, navOffset    int
	modal                  string
	modalOffset, menuIndex int
	destination            textinput.Model
	launchPending          bool
	launchError            string
	sequence               uint64
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
	return tea.Batch(m.dispatch(m.active, m.current().management.Init()), m.check(m.active), m.tick())
}
func (m *frame) tick() tea.Cmd {
	return m.dispatch(m.active, tea.Tick(time.Second, func(t time.Time) tea.Msg { return frameTick(t) }))
}
func (m *frame) check(path string) tea.Cmd {
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
		return "checking", yellow
	}
	if w.failure != "" {
		return "UNVERIFIED / refused", red
	}
	if w.lastSuccess.IsZero() {
		return "unknown", yellow
	}
	if now.Sub(w.lastSuccess) > freshFor {
		return "stale", yellow
	}
	return "verified", green
}
func (m *frame) resize() {
	left, right := m.columns()
	for _, w := range m.workspaces {
		w.management.width = max(1, m.width-left-right)
		w.management.height = max(1, m.height-2)
		w.management.input.SetWidth(max(1, w.management.width-4))
	}
}
func (m *frame) columns() (int, int) {
	left, right := 0, 0
	if m.width >= 70 {
		left = 22
	}
	if m.width >= 118 {
		left = 24
		right = 30
	}
	return left, right
}
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
	m.destination = textinput.New()
	m.destination.Prompt = "> "
	m.destination.Placeholder = "/absolute/workspace"
	m.destination.CharLimit = 2048
	m.destination.SetWidth(max(1, min(64, m.width-8)))
	m.destination.Focus()
}
func (m *frame) submitWorkspace() tea.Cmd {
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
		m.destination.SetWidth(max(1, min(64, m.width-8)))
		return m, nil
	case tea.ColorProfileMsg:
		for path := range m.workspaces {
			m.updateManagement(path, v)
		}
		m.noColor = m.current().management.noColor
		return m, nil
	case tea.MouseMsg:
		if m.width < 24 || m.height < 16 {
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
				m.current().management.helpOffset = max(0, m.current().management.helpOffset+delta)
				return m, nil
			}
			if m.modal != "" && m.modal != "navigation" {
				m.modalOffset = max(0, m.modalOffset+delta)
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
		if m.width < 24 || m.height < 16 {
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
		if (m.width < 24 || m.height < 16) && !m.current().management.quitting {
			if key.Matches(v, quit) {
				m.current().management.help = false
				return m, m.updateManagement(m.active, v)
			}
			if key.Matches(v, escape) {
				m.modal = ""
				m.current().management.help = false
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
			m.modal = "menu"
			m.menuIndex = 0
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
				if m.navIndex >= m.navOffset+max(1, m.height/2-4) {
					m.navOffset = m.navIndex - max(1, m.height/2-4) + 1
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
		m.modal = "menu"
		m.menuIndex = 0
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
			var cmd tea.Cmd
			m.destination, cmd = m.destination.Update(v)
			return m.dispatch(m.active, cmd)
		}
	case "menu":
		if key.Matches(v, previous) {
			m.menuIndex = max(0, m.menuIndex-1)
		}
		if key.Matches(v, next) {
			m.menuIndex = min(len(menuActions)-1, m.menuIndex+1)
		}
		if key.Matches(v, enter) {
			return m.menuAction(m.menuIndex)
		}
	case "navigation":
		if key.Matches(v, newWorkspace) {
			m.openNew()
		}
		if key.Matches(v, openMenu) {
			m.modal = "menu"
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
			m.modalOffset = max(0, m.modalOffset-1)
		}
		if key.Matches(v, next, pageDown) {
			m.modalOffset++
		}
	}
	return nil
}
func (m *frame) metadata() string {
	w := m.current()
	state, _ := w.health(m.now)
	age := "never"
	if !w.lastSuccess.IsZero() {
		age = fmt.Sprintf("%ds ago", max(0, int(m.now.Sub(w.lastSuccess).Seconds())))
	}
	selected := "None · F6 resources, ↑↓ select"
	for _, row := range w.management.connections {
		if row.Name == w.selected {
			selected = fmt.Sprintf("%s\n%s@%s:%d\nSSH: %s", safe(row.Name), safe(row.User), safe(row.Host), row.Port, safe(row.State))
		}
	}
	owner := "No connection owner observed"
	if len(w.management.connections) > 0 {
		owner = "Connection owner observed"
	}
	if w.management.connectionError != "" {
		owner = w.management.connectionError
	}
	return fmt.Sprintf("SELECTED CONNECTION\n%s\n\nRESOURCES\n%d SSH connections (snapshot)\n%s\nShells / runs / transfers:\nNot implemented\n\nHOVEL DAEMON: %s\nLast success: %s\nVerification duration: %s\nLast known PID %d\nEndpoint: %s\nWorkspace: %s\n%s", selected, len(w.management.connections), owner, state, age, w.duration.Round(time.Millisecond), w.management.info.PID, safe(filepath.Join(m.active, "hoveld.sock")), safe(m.active), w.failure)
}

// All visible control cells and pointer targets come from these same layers.
func (m *frame) compositor() *lipgloss.Compositor {
	w, h := max(1, m.width), max(1, m.height)
	current := m.current()
	left, right := m.columns()
	layers := []*lipgloss.Layer{lipgloss.NewLayer(strings.Repeat(" ", w) + strings.Repeat("\n", h-1)).ID("background")}
	add := func(id, text string, x, y, width, height, z int) {
		if width <= 0 || height <= 0 {
			return
		}
		text = fit(text, width, height)
		lines := strings.Split(text, "\n")
		for i, line := range lines {
			lines[i] = line + strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
		}
		layers = append(layers, lipgloss.NewLayer(strings.Join(lines, "\n")).ID(id).X(x).Y(y).Z(z))
	}
	label := func(id, text string) string {
		if current.focus == id {
			return "> " + text
		}
		return text
	}
	state, style := current.health(m.now)
	status := current.management.paint(style, "● Hovel: "+state)
	add("daemon", status, w-min(w, 30), 0, min(w, 30), 1, 2)
	add("navigation", current.management.paint(cyan, "[Workspaces] "+safe(m.active)), 0, 0, max(0, w-30), 1, 1)
	tabLabel := "[Burrow]"
	hovelLabel := "[Hovel · soon]"
	if current.tab == "" {
		tabLabel = "> Burrow"
	} else {
		hovelLabel = "> Hovel · soon"
	}
	add("burrow", label("tabs", tabLabel), left, 1, 10, 1, 1)
	add("hovel", hovelLabel, left+10, 1, min(16, w-left-10), 1, 1)
	management := current.management
	management.help = false
	management.quitting = false
	center := management.View().Content
	add("center", center, left, 2, w-left-right, h-2, 1)
	// Identified row layers reuse the rendered cells rather than guessing table
	// border or wrapping offsets in the pointer handler.
	inActive := false
	for y, line := range strings.Split(center, "\n") {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "ACTIVE SSH CONNECTIONS") {
			inActive = true
			continue
		}
		if strings.Contains(plain, "TUNNELS") {
			inActive = false
		}
		if !inActive {
			continue
		}
		for i, row := range current.management.connections {
			fields := strings.Fields(plain)
			if len(fields) > 0 && fields[0] == row.Name {
				if current.selected == row.Name {
					line = current.management.paint(purple, ">"+ansi.Truncate(plain, w-left-right-1, "…"))
				}
				add(fmt.Sprintf("resource:%d", i), line, left, y+2, w-left-right, 1, 2)
				break
			}
		}
	}

	if current.tab != "" {
		add("center", "Hovel CLI · not implemented (#68)\n\nThe embedded terminal will appear here.\nEsc or Burrow tab returns to management.", left, 3, w-left-right, max(1, h-4), 3)
	}
	add("footer", "Focus: "+current.focus+" · F6 · Alt+N/M/W · F1 help", left, h-3, w-left-right, 1, 2)
	if right > 0 {
		add("metadata", ansi.Wrap(m.metadata(), right-2, ""), w-right, 2, right, h-2, 1)
	}
	sidebar := func(width, z int) {
		midpoint := max(4, h/2)
		add("workspaces", label("workspaces", "WORKSPACES")+"\nExplicit · this session", 0, 1, width, max(1, midpoint-1), z)
		rows := max(1, midpoint-4)
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
			add(fmt.Sprintf("workspace:%d", i), prefix+safe(filepath.Base(m.paths[i])), 0, 3+i-start, width, 1, z+1)
		}
		add("workspaces", fmt.Sprintf("%d–%d/%d · ↑↓ / wheel", start+1, end, len(m.paths)), 0, midpoint-1, width, 1, z+1)
		add("new", label("new", "[New]"), 0, midpoint, width/2, 1, z+1)
		add("menu", label("menu", "[Menu]"), width/2, midpoint, width-width/2, 1, z+1)
		rows = max(1, (h-midpoint-4)/2)
		start = min(current.shellOffset, max(0, len(m.paths)-1))
		end = min(len(m.paths), start+rows)
		text := label("shells", "SHELLS · not implemented")
		for _, path := range m.paths[start:end] {
			text += "\n" + safe(filepath.Base(path)) + "\n  No shells"
		}
		text += fmt.Sprintf("\n%d–%d/%d · wheel", start+1, end, len(m.paths))
		add("shells", text, 0, midpoint+1, width, max(1, h-midpoint-1), z)
	}
	if left > 0 {
		sidebar(left, 2)
	}
	if m.modal != "" {
		// A full-screen identified backdrop captures every pointer event.
		base := lipgloss.NewCompositor(layers...).Render()
		if !m.noColor {
			base = muted.Render(ansi.Strip(base))
		}
		layers = []*lipgloss.Layer{lipgloss.NewLayer(base).ID("modal-backdrop")}
		if m.modal == "navigation" {
			sidebar(min(32, w), 3)
			add("dismiss", "[Esc close]", max(0, w-12), 1, min(12, w), 1, 5)
		} else {
			pw := max(1, min(72, w-2))
			x := max(0, (w-pw)/2)
			y := max(1, (h-min(h-2, 14))/2)
			text := ""
			switch m.modal {
			case "new":
				text = "NEW / OPEN WORKSPACE\nExact destination (Enter launches):\n" + m.destination.View() + "\nDestination: " + safe(m.destination.Value()) + "\n\n" + m.launchError
				if m.launchPending {
					text += "\nLaunching and verifying…"
				}
			case "menu":
				text = "MENU · ↑↓ Enter · Esc close"
			case "metadata":
				text = m.metadata()
			}
			lines := strings.Split(ansi.Wrap(text, pw, ""), "\n")
			start := min(m.modalOffset, max(0, len(lines)-1))
			if m.modal != "metadata" {
				start = 0
			}
			add("modal", current.management.paint(surface, strings.Join(lines[start:], "\n")), x, y, pw, max(1, min(h-y-2, 14)), 3)
			if m.modal == "menu" {
				for i, action := range menuActions {
					prefix := "  "
					if i == m.menuIndex {
						prefix = "> "
					}
					add(fmt.Sprintf("action:%d", i), prefix+action, x, y+2+i, pw, 1, 4)
				}
			}
			if m.modal == "new" {
				add("submit", "[Launch exact destination]", x, min(h-2, y+10), pw, 1, 4)
			}
			add("dismiss", "[Esc close]", x, h-1, pw, 1, 4)
		}
	}
	if current.management.help || current.management.quitting {
		base := current.management.overlay(lipgloss.NewCompositor(layers...).Render(), w, h)
		layers = []*lipgloss.Layer{lipgloss.NewLayer(base).ID("child-modal")}
		add("modal-footer", "", 0, h-1, w, 1, 3)
		if current.management.quitting {
			add("quit-leave", "[Quit]", max(0, w/2-16), h-1, min(12, w), 1, 4)
		}
		add("dismiss", "[Esc / Keep working]", max(0, w/2), h-1, min(22, w-w/2), 1, 4)
	}
	return lipgloss.NewCompositor(layers...)
}
func (m *frame) View() tea.View {
	// Preserve the existing tiny-terminal quit recovery, with no stale click targets.
	if m.height < 16 || m.width < 24 {
		u := m.current().management
		u.width = m.width
		u.height = min(m.height, 11)
		return u.View()
	}
	base := m.compositor().Render()
	if m.noColor {
		base = ansi.Strip(base)
	} else {
		base = surface.Render(base)
	}
	v := tea.NewView(fit(base, m.width, m.height))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}
