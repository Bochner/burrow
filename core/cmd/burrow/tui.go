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
var inventoryUp = key.NewBinding(key.WithKeys("alt+up"))
var inventoryDown = key.NewBinding(key.WithKeys("alt+down"))
var completionNext = key.NewBinding(key.WithKeys("tab"))
var completionPrevious = key.NewBinding(key.WithKeys("shift+tab"))

type connectionTick struct{}
type connectionList struct {
	states []connection.State
	err    error
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
	input.Placeholder = "connect · connections · help · quit"
	input.SetSuggestions(connection.Suggestions(nil))
	input.ShowSuggestions = true
	input.KeyMap.AcceptSuggestion.Unbind()
	input.KeyMap.NextSuggestion = next
	input.KeyMap.PrevSuggestion = previous
	input.CharLimit = 2048
	input.Focus()
	return ui{info: info, input: input, noColor: noColor, output: "Verified daemon · quit reviews connections: keep or close."}
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
		return m, connectionTimer()
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
		return m, refreshProfiles(m.info.Workspace)
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
			if key.Matches(v, escape, help) {
				m.help = false
			}
			if key.Matches(v, previous, pageUp) {
				m.helpOffset = max(0, m.helpOffset-1)
			}
			if key.Matches(v, next, pageDown) {
				m.helpOffset++
			}
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
				m.output = "Running reviewed command through Hovel…"
				workspace := m.info.Workspace
				if args[0] == "shell-close" {
					return m, func() tea.Msg { return shellCloseRequested{} }
				}
				if args[0] == "shell" {
					return m, func() tea.Msg { return shellRequested{args[1]} }
				}
				if args[0] == "connect" || args[0] == "reconnect" || (args[0] == "close" && len(args) == 2) || (args[0] == "profile" && (args[1] == "connect" || args[1] == "edit" || args[1] == "delete" || args[1] == "save")) {
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
			tunnels = "0"
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
func (m ui) dataTable(title string, headers []string, rows [][]string, w int) string {
	return m.paint(heading, title) + "\n" + table.New().Headers(headers...).Rows(rows...).Width(w).Wrap(false).
		Border(lipgloss.NormalBorder()).BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).BorderColumn(false).BorderStyle(separatorStyle).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := fieldStyle(headers[col])
			if row == table.HeaderRow {
				style = accent
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
		m.activeConnections(bodyW) + "\n\n" + m.paint(heading, "TUNNELS") + "\n" + m.paint(secondary, "Not implemented")
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
		if len(matches) > 0 {
			rows := []string{"COMPLETION · Tab / Shift+Tab cycle"}
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
			popup := solid(strings.Join(rows, "\n"), w, min(len(rows), max(1, h-3)), popupColor, m.noColor)
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
	return "status   Verify this workspace and daemon\nhelp     Return to this reference\nquit     Review connections; keep running or close and quit\n\n" + connection.Help + "\nF6 / Shift+F6 focus: prompt, workspaces, New, Menu, shells, tabs, resources, saved. Saved: Enter actions; arrows select.\nArrows select; Enter activates; Esc returns to prompt.\nCtrl+P menu (Alt+M), Alt+N New, Alt+W workspace drawer.\nDrag selects only the middle panel; Ctrl+C or Copy copies (no auto-copy).\nEsc clears selection; Ctrl+Shift+V pastes. Ctrl+Shift+C copies if forwarded.\nAlt+S toggles native selection (includes sidebars). Alt+mouse sends to Hovel.\nTab completes the prompt. With mouse controls enabled, click daemon for metadata.\nAlt+↑↓ scroll connections. PgUp/PgDn scroll output.\nCLI: --workspace PATH is required first.\nOptions: --offline, --hovel-package FILE"
}
func (m ui) helpViewport(w, h int) viewport.Model {
	return scrollBody(m.syntax(m.helpText(), true), max(1, min(96, w-4)-6), max(1, min(30, h-4)-8), m.helpOffset)
}
func (m ui) overlay(base string, w, h int) *lipgloss.Compositor {
	pw, ph := max(8, min(96, w-4)), max(8, min(30, h-4))
	x, y := (w-pw)/2, (h-ph)/2
	bodyW := pw - 6
	text := m.paint(accent, "BURROW COMMAND MENU") + "\n\n" + m.helpViewport(w, h).View()
	popup := dialogStyle.Width(pw).Height(ph).Render(fit(text, bodyW, ph-4))
	footer := m.paint(secondary, "↑↓ scroll · Esc returns")
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(solid(m.paint(secondary, ansi.Strip(base)), w, h, baseColor, m.noColor)).ID("modal-backdrop"),
		lipgloss.NewLayer(solid(popup, pw, ph, popupColor, m.noColor)).X(x).Y(y).Z(1).ID("child-modal"),
		lipgloss.NewLayer(solid(footer, bodyW, 1, popupColor, m.noColor)).X(x+3).Y(y+ph-3).Z(2).ID("dismiss"),
	)
}
