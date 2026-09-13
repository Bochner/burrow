package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/Bochner/burrow/core/connection"
	"github.com/Bochner/burrow/core/launch"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

var enter = key.NewBinding(key.WithKeys("enter"))
var escape = key.NewBinding(key.WithKeys("esc"))
var quit = key.NewBinding(key.WithKeys("ctrl+c", "ctrl+d"))
var help = key.NewBinding(key.WithKeys("f1", "ctrl+k"))
var choose = key.NewBinding(key.WithKeys("tab", "left", "right"))
var previous = key.NewBinding(key.WithKeys("up"))
var next = key.NewBinding(key.WithKeys("down"))
var pageUp = key.NewBinding(key.WithKeys("pgup"))
var pageDown = key.NewBinding(key.WithKeys("pgdown"))
var helpHome = key.NewBinding(key.WithKeys("home"))
var helpEnd = key.NewBinding(key.WithKeys("end"))
var inventoryUp = key.NewBinding(key.WithKeys("alt+up"))
var inventoryDown = key.NewBinding(key.WithKeys("alt+down"))
var tunnelsUp = key.NewBinding(key.WithKeys("alt+shift+up"))
var tunnelsDown = key.NewBinding(key.WithKeys("alt+shift+down"))
var completionNext = key.NewBinding(key.WithKeys("tab"))
var completionPrevious = key.NewBinding(key.WithKeys("shift+tab"))

type connectionTick struct{}
type connectionList struct {
	states []connection.State
	err    error
}
type tunnelList struct {
	tunnels []connection.Tunnel
	err     error
}

func refreshTunnels(workspace string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ts, err := connection.Tunnels(ctx, workspace)
		return tunnelList{ts, err}
	}
}

type connectionResult struct {
	result any
	err    error
}

func refreshConnections(workspace string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		states, e := connection.List(ctx, workspace)
		return connectionList{states, e}
	}
}
func connectionTimer() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return connectionTick{} })
}

type ui struct {
	tunnels                       []connection.Tunnel
	tunnelError                   string
	tunnelOffset                  int
	shellIDs                      []string
	shellControl                  []string
	profiles                      connection.Collection
	profileError, selectedProfile string
	profileOffset                 int
	profileHistoryLoaded          bool
	info                          launch.Info
	input                         textinput.Model
	width, height                 int
	noColor, busy, help, quitting bool
	demo                          bool
	connectionObserved            bool
	output                        string
	history                       []string
	historyIndex                  int
	draft                         string
	outputOffset                  int
	connectionOffset              int
	connections                   []connection.State
	connectionError               string
	helpOffset                    int
	completionValues              []string
	completionIndex               int
	completionValue               string
}

func newUI(info launch.Info, noColor bool) ui {
	input := textinput.New()
	styleInput(&input)
	input.Prompt = "╰─ "
	input.Placeholder = "connect · tunnel create · help · quit"
	input.ShowSuggestions = true
	input.KeyMap.AcceptSuggestion.Unbind()
	input.KeyMap.NextSuggestion = next
	input.KeyMap.PrevSuggestion = previous
	input.CharLimit = 2048
	input.Focus()
	m := ui{info: info, input: input, noColor: noColor, tunnelError: "UNVERIFIED · loading forwarding inventory", output: "Verified daemon · quit reviews connections: keep or close."}
	m.input.SetSuggestions(m.suggestions())
	return m
}

func (m ui) completionOptions() ([]string, int) {
	if len(m.completionValues) > 0 && m.input.Value() == m.completionValue {
		return m.completionValues, m.completionIndex
	}
	return m.input.MatchedSuggestions(), m.input.CurrentSuggestionIndex()
}

func (m *ui) cycleCompletion(backward bool) {
	if m.input.Position() != len([]rune(m.input.Value())) {
		return
	}
	values, index := m.completionOptions()
	if len(values) == 0 {
		return
	}
	if len(m.completionValues) > 0 && m.input.Value() == m.completionValue {
		if backward {
			index--
		} else {
			index++
		}
	} else if backward {
		index--
	}
	index = (index + len(values)) % len(values)
	m.completionValues, m.completionIndex = values, index
	m.completionValue = values[index]
	m.input.SetValue(m.completionValue)
	m.input.CursorEnd()
	m.input.ShowSuggestions = true
}

func terminal(m *frame, noColor bool) error {
	defer m.stopAuthentication()
	defer m.terminals.close()
	opts := []tea.ProgramOption{}
	// Aspect captures stdout; keep rendering on the actual controlling terminal.
	if !term.IsTerminal(os.Stdout.Fd()) {
		output, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		defer output.Close()
		opts = append(opts, tea.WithOutput(output))
	}
	if noColor {
		opts = append(opts, tea.WithColorProfile(colorprofile.ASCII))
	}
	_, e := tea.NewProgram(m, opts...).Run()
	return e
}
func (m ui) Init() tea.Cmd {
	return tea.Batch(m.input.Focus(), refreshConnections(m.info.Workspace), refreshProfiles(m.info.Workspace))
}
func (m ui) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tunnelList:
		if v.err != nil {
			m.tunnelError = "UNVERIFIED · forwarding inventory unavailable"
		} else {
			m.tunnels, m.tunnelError = v.tunnels, ""
		}
		m.input.SetSuggestions(m.suggestions())
		return m, nil
	case connectionTick:
		return m, tea.Batch(refreshConnections(m.info.Workspace), refreshProfiles(m.info.Workspace))
	case profilesReady:
		if v.err != nil {
			m.profileError = "UNAVAILABLE · " + safe(v.err.Error())
		} else {
			m.profileError = ""
			m.profiles = v.collection
			found := false
			for _, p := range m.profiles.Profiles {
				found = found || p.Name == m.selectedProfile
			}
			if !found {
				m.selectedProfile = ""
			}
			if !m.profileHistoryLoaded {
				m.history = append(v.history, m.history...)
				m.historyIndex = len(m.history)
				m.profileHistoryLoaded = true
			}
		}
		m.input.SetSuggestions(m.suggestions())
		return m, nil
	case connectionList:
		m.connectionObserved = true
		if v.err != nil {
			m.connectionError = "UNVERIFIED · connection owner status unavailable"
			for i := range m.connections {
				m.connections[i].State = "unverified"
			}
		} else {
			m.connectionError = ""
			m.connections = v.states
			m.input.SetSuggestions(m.suggestions())
		}
		return m, tea.Batch(connectionTimer(), refreshTunnels(m.info.Workspace))
	case connectionResult:
		m.busy = false
		m.outputOffset = 0
		if v.err != nil {
			m.output = "REFUSED: " + safe(v.err.Error())
		} else {
			b, _ := json.MarshalIndent(v.result, "", "  ")
			lines := strings.Split(string(b), "\n")
			for i := range lines {
				lines[i] = safe(lines[i])
			}
			m.output = strings.Join(lines, "\n")
		}
		return m, tea.Batch(refreshProfiles(m.info.Workspace), refreshTunnels(m.info.Workspace))
	case tea.WindowSizeMsg:
		m.width = max(1, v.Width)
		m.height = max(1, v.Height)
		m.input.SetWidth(max(1, m.width-4))
	case tea.ColorProfileMsg:
		m.noColor = m.noColor || v.Profile == colorprofile.ASCII
	case tea.BackgroundColorMsg:
		// The owner selected Herdr's Catppuccin Mocha; keep the explicit dark palette.
	case tea.PasteMsg:
		if m.help {
			return m, nil
		}
		m.completionValues = nil
		m.input.ShowSuggestions = true
		m.input.SetValue(m.input.Value() + safe(v.Content))
		m.input.CursorEnd()
		m.input.SetSuggestions(m.suggestions())
		return m, nil
	case tea.KeyPressMsg:
		if m.help {
			m.updateHelp(v, m.width, m.height)
			return m, nil
		}
		if key.Matches(v, completionNext, completionPrevious) {
			m.cycleCompletion(key.Matches(v, completionPrevious))
			return m, nil
		}
		if len(m.completionValues) > 0 && key.Matches(v, previous, next) {
			m.cycleCompletion(key.Matches(v, previous))
			return m, nil
		}
		m.completionValues = nil
		switch {
		case key.Matches(v, tunnelsUp):
			m.tunnelOffset = max(0, m.tunnelOffset-1)
			return m, nil
		case key.Matches(v, tunnelsDown):
			m.tunnelOffset = min(max(0, len(m.tunnels)-m.tunnelRows()), m.tunnelOffset+1)
			return m, nil
		case key.Matches(v, inventoryUp):
			m.connectionOffset = max(0, m.connectionOffset-1)
			return m, nil
		case key.Matches(v, inventoryDown):
			m.connectionOffset = min(max(0, len(m.connections)-m.connectionRows()), m.connectionOffset+1)
			return m, nil
		case key.Matches(v, pageUp):
			m.outputOffset = max(0, m.outputOffset-1)
			return m, nil
		case key.Matches(v, pageDown):
			m.outputOffset++
			return m, nil
		case key.Matches(v, quit):
			m.quitting = true
			return m, nil
		case key.Matches(v, help):
			m.help = true
			return m, nil
		case key.Matches(v, escape):
			m.input.ShowSuggestions = false
			return m, nil
		case key.Matches(v, previous, next) && (!m.input.ShowSuggestions || len(m.input.MatchedSuggestions()) == 0 || m.input.Value() == ""):
			if key.Matches(v, previous) {
				if m.historyIndex == len(m.history) {
					m.draft = m.input.Value()
				}
				m.historyIndex = max(0, m.historyIndex-1)
			} else {
				m.historyIndex = min(len(m.history), m.historyIndex+1)
			}
			value := m.draft
			if m.historyIndex < len(m.history) {
				value = m.history[m.historyIndex]
			}
			m.input.SetValue(value)
			m.input.CursorEnd()
			return m, nil
		case key.Matches(v, enter):
			command := strings.TrimSpace(m.input.Value())
			if m.demo && command != "help" && command != "quit" {
				m.output = "Sample data only · commands are disabled in preview."
				return m, nil
			}
			if command == "" {
				return m, nil
			}
			if m.busy {
				m.output = "Verification in progress; draft retained."
				return m, nil
			}
			if command == "status" || command == "help" || command == "quit" {
				m.history = append(m.history, command)
			}
			m.historyIndex = len(m.history)
			m.input.Reset()
			switch command {
			case "help":
				m.help = true
				m.helpOffset = 0
			case "quit":
				m.quitting = true
			case "status":
				m.busy = true
				return m, func() tea.Msg { return statusRequested{} }
			default:
				args, e := connection.Split(command)
				wizard := len(args) == 1 && args[0] == "connect"
				if e == nil && !wizard {
					e = connection.ValidateCommand(m.info.Workspace, args)
				}
				if e != nil {
					m.output = "REFUSED: " + safe(e.Error())
					return m, nil
				}
				m.history = append(m.history, command)
				m.historyIndex = len(m.history)
				m.busy = true
				workspace := m.info.Workspace
				if args[0] == "shell-close" || args[0] == "shells" || args[0] == "resume" {
					m.shellControl = args
					return m, nil
				}
				if args[0] == "shell" {
					return m, func() tea.Msg { return shellRequested{args[1]} }
				}
				m.output = "Running reviewed command through Hovel…"
				if args[0] == "tunc" || (args[0] == "tund" && len(args) == 2) || (args[0] == "tunnel" && (args[1] == "create" || (args[1] == "remove" && len(args) == 3))) || args[0] == "connect" || args[0] == "reconnect" || (args[0] == "close" && len(args) == 2) || (args[0] == "profile" && (args[1] == "connect" || args[1] == "edit" || args[1] == "delete" || args[1] == "save")) {
					return m, func() tea.Msg { return authenticationRequested{args: args} }
				}
				return m, func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
					defer cancel()
					result, e := connection.Execute(ctx, workspace, args)
					return connectionResult{result, e}
				}
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if _, ok := msg.(tea.KeyPressMsg); ok {
		m.input.ShowSuggestions = true
		m.input.SetSuggestions(m.suggestions())
	}
	return m, cmd
}
func (m ui) paint(style lipgloss.Style, s string) string {
	if m.noColor {
		return s
	}
	return style.Render(s)
}
func fit(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], w, "…")
	}
	return strings.Join(lines, "\n")
}
func (m ui) connectionRows() int { return max(1, min(5, m.height-24)) }
func (m ui) activeConnections(w int) string {
	title := m.paint(heading, "ACTIVE SSH CONNECTIONS")
	if m.connectionError != "" {
		return title + "\n" + m.paint(errorStyle, m.connectionError)
	}
	if len(m.connections) == 0 {
		return title + "\n" + m.paint(secondary, "No connections")
	}
	start := min(m.connectionOffset, max(0, len(m.connections)-m.connectionRows()))
	end := min(len(m.connections), start+m.connectionRows())
	visible := m.connections[start:end]
	overflow := ""
	if len(visible) < len(m.connections) {
		overflow = fmt.Sprintf("\n%d–%d of %d · Alt+↑↓ scroll", start+1, end, len(m.connections))
	}
	headers := []string{"NAME", "HOST", "USER", "PORT", "PROXY", "TERM", "TUNNELS", "SOCKET"}
	var rows [][]string
	for _, row := range visible {
		proxy, terminal, tunnels := "—", "—", "—"
		if row.Generation != "" {
			terminal = "Local PTY"
			if row.State == "connected" {
				tunnels = fmt.Sprint(row.TunnelCount)
			}
			if row.ProxyPort != 0 && row.State == "connected" {
				proxy = fmt.Sprint(row.ProxyPort)
			}
		}
		if m.demo {
			terminal, tunnels = "Native", "0"
			if row.Name == "gateway" && row.State == "connected" {
				proxy, tunnels = "1080", "2"
			}
		}
		socket := safe(row.Socket)
		if socket == "" {
			socket = "—"
		}
		rows = append(rows, []string{safe(row.Name), safe(row.Host), safe(row.User), fmt.Sprint(row.Port), proxy, terminal, tunnels, socket})
	}
	return m.dataTable("ACTIVE SSH CONNECTIONS", headers, rows, w) + overflow
}

// Preserve field identity through color, padding and headers.
func (m ui) tunnelRows() int { return max(1, min(3, m.height-29)) }
func (m ui) localForwards(w int) string {
	title := m.paint(heading, "TUNNELS")
	if m.tunnelError != "" {
		return title + "\n" + m.paint(errorStyle, m.tunnelError)
	}
	if m.connectionError != "" {
		return title + "\n" + m.paint(errorStyle, "UNVERIFIED · connection owner status unavailable")
	}
	if len(m.tunnels) == 0 {
		return title + "\n" + m.paint(secondary, "No forwards")
	}
	start := min(m.tunnelOffset, max(0, len(m.tunnels)-m.tunnelRows()))
	end := min(len(m.tunnels), start+m.tunnelRows())
	var rows [][]string
	for _, t := range m.tunnels[start:end] {
		kind, listenSide, destinationSide := "Local", "Local ", "Remote "
		if t.Direction == "R" {
			kind, listenSide, destinationSide = "Reverse", "Remote ", "Local "
		}
		rows = append(rows, []string{safe(t.ID), safe(t.Connection), kind, m.paint(secondary, listenSide) + m.endpoint(safe(t.Listen)), m.paint(secondary, destinationSide) + m.endpoint(safe(t.Destination)), safe(t.State)})
	}
	result := m.dataTable("TUNNELS", []string{"ID", "CONNECTION", "TYPE", "LISTENER", "DESTINATION", "STATUS"}, rows, w)
	if end-start < len(m.tunnels) {
		result += "\n" + m.paint(numberStyle, fmt.Sprintf("%d–%d", start+1, end)) + m.paint(secondary, " of ") + m.paint(numberStyle, fmt.Sprint(len(m.tunnels))) + m.paint(secondary, " · ") + m.paint(keywordStyle, "Alt+Shift+↑↓") + m.paint(secondary, " scroll")
	}
	return result
}

func (m ui) dataTable(title string, headers []string, rows [][]string, w int) string {
	prefix := ""
	if title != "" {
		prefix = m.paint(heading, title) + "\n"
	}
	return prefix + table.New().Headers(headers...).Rows(rows...).Width(w).Wrap(false).
		Border(lipgloss.NormalBorder()).BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).BorderColumn(false).BorderStyle(separatorStyle).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := fieldStyle(headers[col])
			if row == table.HeaderRow {
				style = accent
			} else if headers[col] == "STATUS" {
				style = connectionStyle(rows[row][col])
			}
			if row >= 0 && rows[row][col] == "—" {
				style = secondary
			}
			return style.Padding(0, 1).Align(lipgloss.Center)
		}).String()
}
func (m ui) View() tea.View {
	w, h := max(1, m.width), max(1, m.height)
	var b strings.Builder

	bodyW := w
	content := m.savedConnections(bodyW) + "\n\n" +
		m.activeConnections(bodyW) + "\n\n" + m.localForwards(bodyW)
	if m.demo {
		content = m.demoResources(bodyW)
	}
	// Reserve command output space even when endpoint text wraps in the table.
	content = fit(content, bodyW, min(lipgloss.Height(content), max(0, h-7)))
	outputLines := strings.Split(ansi.Wrap(m.styledOutput(), bodyW, ""), "\n")
	start := min(m.outputOffset, len(outputLines)-1)
	content += "\n\n" + m.paint(heading, "COMMAND OUTPUT") + "\n" + strings.Join(outputLines[start:], "\n")
	b.WriteString(fit(content, w, max(0, h-3)))
	footer := "F1 help · Tab completion · Ctrl+C quit"
	if m.busy {
		footer = "Working… prompt remains editable · close NAME cancels a connection"
	}
	b.WriteString("\n" + m.paint(secondary, footer) + "\n" + m.paint(accent, "╭─ workspace › management") + "\n" + m.input.View())
	base := fit(b.String(), w, h)
	if !m.help && m.input.Value() != "" && m.input.ShowSuggestions {
		matches, selected := m.completionOptions()
		var rows []string
		if len(matches) > 0 {
			rows = []string{"COMPLETION · Tab / Shift+Tab cycle"}
			count := max(1, min(6, h-5))
			start := min(max(0, selected-count+1), max(0, len(matches)-count))
			end := min(len(matches), start+count)
			if len(matches) > count {
				rows[0] = fmt.Sprintf("COMPLETION · %d–%d of %d · Tab/Shift+Tab", start+1, end, len(matches))
			}
			commandWidth := 0
			for _, value := range matches {
				commandWidth = max(commandWidth, lipgloss.Width(value))
			}
			commandWidth = min(commandWidth, max(1, w/2-2))
			for i := start; i < end; i++ {
				s := matches[i]
				command := ansi.Truncate(s, commandWidth, "…")
				line := "  " + m.paint(heading, command+strings.Repeat(" ", max(0, commandWidth-lipgloss.Width(command)))) + "  " + m.paint(secondary, completionDescription(s))
				line = ansi.Truncate(line, w, "…")
				if i == selected {
					line = selectedRow(line, w, m.noColor)
				}
				rows = append(rows, line)
			}
		}
		if hint := m.forwardingGuidance(); hint != "" {
			rows = append(rows, ansi.Wrap(hint, w, ""))
		}
		if len(rows) > 0 {
			text := strings.Join(rows, "\n")
			popup := solid(text, w, min(lipgloss.Height(text), max(1, h-3)), popupColor, m.noColor)
			base = lipgloss.NewCompositor(lipgloss.NewLayer(base), lipgloss.NewLayer(popup).Y(max(0, h-3-lipgloss.Height(popup))).Z(1)).Render()
		}
	}
	if m.help {
		base = m.overlay(base, w, h).Render()
	}

	base = solid(base, w, h, baseColor, m.noColor)
	v := tea.NewView(fit(base, w, h))
	v.AltScreen = true
	return v
}

func (m ui) helpText() string {
	return `# NAVIGATION
F6 / Shift+F6	Move focus between panels; arrows select, Enter opens
Ctrl+P	Open the searchable action menu
Tab / Shift+Tab	Cycle command suggestions; forwarding shows argument examples
Alt+H / Alt+B	Switch to Hovel / return to Burrow management
Alt+↑↓ / Alt+Shift+↑↓	Scroll connections / tunnels; PgUp/PgDn scroll command output
Drag / Ctrl+C / Alt+S	Select middle-panel text / copy selection / toggle native selection
Ctrl+Shift+V	Paste using your terminal's paste shortcut

# CONNECTIONS & SHELLS
connect	Open the guided connection form
connect NAME HOST USER	Connect directly; review first, then authenticate privately
shell NAME / resume ID	Open a shell / return to an existing frontend-local shell
Ctrl+] / Alt+1–9	Return from SSH to management / select a shell
Alt+←/→	Cycle shells without closing them
shell-close ID	Close one shell, keeping its connection
inspect NAME / status	Inspect connection details / verify the daemon
reconnect NAME HOST USER	Explicitly replace a lost connection
The connection form includes SSH keys, agents, jump hosts and a SOCKS proxy.

# FORWARDING
tunnel create NAME forward|reverse	Create a local or reverse listener; the prompt guides arguments
tunc myserver l 8080 localhost 80	Example: local port 8080 reaches port 80 from the SSH server
tunc myserver r 8080 localhost 80	Example: remote port 8080 reaches port 80 from this machine
tunnel check NAME/ID	Test destination traffic; listening alone does not prove reachability
tunnel remove NAME/ID	Review and remove one listener (alias: tund NAME/ID)
Reverse LISTEN 0 requests a random port. Tab completes live names and tunnel IDs.
The dashboard refreshes automatically, including changes made by external CLI clients.

# SAVED CONNECTIONS & WORKSPACES
Saved row → Enter	Connect, inspect, edit or delete saved settings
profile save NAME	Save an active connection's settings, never its passwords
profile create NAME HOST USER	Save settings without connecting
profile load PATH / profile backup PATH	Open a collection / back up the selected collection
Alt+N / Alt+W	Create a workspace / open the workspace drawer

# QUIT & MORE HELP
close NAME	Review closing the connection and all its shells and listeners
quit / Ctrl+C	Choose keep running, close connections, or cancel
Keeping connections does not keep frontend-local shells alive. Saved settings remain.
SSH host keys are not verified. Never put passwords or passphrases in commands.
Inventory commands remain callable for full details; they are omitted from TUI suggestions.
burrow --workspace PATH help	Full CLI reference (run outside the TUI)
Full command options, server requirements and walkthroughs:
https://bochner.github.io/burrow/index.html`
}

func helpSize(w, h int) (int, int) {
	return max(8, min(120, w-4)), max(8, min(40, h-4))
}

func (m *ui) updateHelp(k tea.KeyPressMsg, w, h int) {
	v := m.helpViewport(w, h)
	switch {
	case key.Matches(k, escape, help):
		m.help = false
	case key.Matches(k, previous):
		v.SetYOffset(v.YOffset() - 1)
	case key.Matches(k, next):
		v.SetYOffset(v.YOffset() + 1)
	case key.Matches(k, pageUp):
		v.SetYOffset(v.YOffset() - v.Height())
	case key.Matches(k, pageDown):
		v.SetYOffset(v.YOffset() + v.Height())
	case key.Matches(k, helpHome):
		v.GotoTop()
	case key.Matches(k, helpEnd):
		v.GotoBottom()
	}
	m.helpOffset = v.YOffset()
}

func (m ui) helpViewport(w, h int) viewport.Model {
	pw, ph := helpSize(w, h)
	bodyW := max(1, pw-6)
	var lines []string
	for _, line := range strings.Split(m.helpText(), "\n") {
		label, description, row := strings.Cut(line, "\t")
		switch {
		case strings.HasPrefix(line, "# "):
			lines = append(lines, m.paint(heading, strings.TrimPrefix(line, "# ")))
		case row:
			styled := m.syntax(label, true)
			if label[0] >= 'A' && label[0] <= 'Z' {
				styled = m.paint(keywordStyle, label)
			}
			if bodyW < 90 {
				lines = append(lines, styled, "  "+m.paint(secondary, description))
			} else {
				column := 42
				lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top,
					pageStyle.Width(column).Render(styled),
					m.paint(secondary, ansi.Wrap(description, bodyW-column, ""))))
			}
		default:
			lines = append(lines, m.paint(secondary, line))
		}
	}
	return scrollBody(strings.Join(lines, "\n"), bodyW, max(1, ph-8), m.helpOffset)
}
func (m ui) overlay(base string, w, h int) *lipgloss.Compositor {
	pw, ph := helpSize(w, h)
	x, y := (w-pw)/2, (h-ph)/2
	bodyW := pw - 6
	v := m.helpViewport(w, h)
	text := centered(m.paint(accent, "Burrow Help"), bodyW) + "\n\n" + v.View()
	popup := dialogStyle.Width(pw).Height(ph).Render(fit(text, bodyW, ph-4))
	footer := m.paint(keywordStyle, "Esc close · ↑↓ scroll · PgUp/PgDn · Home/End") + m.paint(numberStyle, fmt.Sprintf(" · %.0f%%", v.ScrollPercent()*100))
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(solid(m.paint(secondary, ansi.Strip(base)), w, h, baseColor, m.noColor)).ID("modal-backdrop"),
		lipgloss.NewLayer(solid(popup, pw, ph, popupColor, m.noColor)).X(x).Y(y).Z(1).ID("child-modal"),
		lipgloss.NewLayer(solid(footer, bodyW, 1, popupColor, m.noColor)).X(x+3).Y(y+ph-3).Z(2).ID("dismiss"),
	)
}
