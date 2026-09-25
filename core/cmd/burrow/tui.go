package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
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

type runList struct {
	runs []connection.Run
	err  error
}

func refreshRuns(workspace string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		runs, err := connection.Runs(ctx, workspace)
		return runList{runs, err}
	}
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
	follow                        *runView
	runs                          []connection.Run
	downloads                     connection.Downloads
	downloadObserved              bool
	downloadError                 string
	downloadBar                   progress.Model
	files                         *fileMode
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
	m := ui{downloadBar: newDownloadBar(), info: info, input: input, noColor: noColor, tunnelError: "UNVERIFIED · loading forwarding inventory", output: "Verified daemon · quit reviews connections: keep or close."}
	m.input.SetSuggestions(m.suggestions())
	return m
}

func (m ui) completionOptions() ([]string, int) {
	if len(m.completionValues) > 0 && m.input.Value() == m.completionValue {
		return m.completionValues, m.completionIndex
	}
	if m.files != nil {
		return m.files.matches, 0
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

func terminal(m *frame, noColor bool) (failure error) {
	defer m.stopAuthentication()
	defer func() { m.terminals.close(); failure = errors.Join(failure, m.terminals.auditErr) }()
	defer func() {
		for _, w := range m.workspaces {
			for _, u := range w.fileViews {
				if cmd := u.cancelFiles(); cmd != nil {
					cmd()
				}
			}
		}
	}()
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
	return tea.Batch(m.input.Focus(), refreshConnections(m.info.Workspace), refreshProfiles(m.info.Workspace), refreshRuns(m.info.Workspace))
}
func (m ui) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case runList:
		m.runs = nil
		if v.err == nil {
			m.runs = v.runs
		}
		m.input.SetSuggestions(m.suggestions())
		return m, nil
	case fileResult:
		m.acceptFiles(v)
		return m, nil
	case fileDiscoveryTick:
		cmd := m.discoverFiles(v)
		return m, cmd
	case fileDiscovery:
		cmd := m.acceptDiscovery(v)
		return m, cmd
	case tunnelList:
		if v.err != nil {
			m.tunnelError = "UNVERIFIED · forwarding inventory unavailable"
		} else {
			m.tunnels, m.tunnelError = v.tunnels, ""
		}
		m.input.SetSuggestions(m.suggestions())
		return m, nil
	case connectionTick:
		return m, tea.Batch(refreshConnections(m.info.Workspace), refreshProfiles(m.info.Workspace), refreshRuns(m.info.Workspace))
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
			if !m.profileHistoryLoaded && m.files == nil {
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
			if chunk, ok := v.result.(connection.RunOutput); ok {
				data, err := base64.StdEncoding.DecodeString(chunk.Data)
				if err == nil {
					lines := strings.Split(string(data), "\n")
					for i := range lines {
						lines[i] = safe(lines[i])
					}
					m.output = fmt.Sprintf("Output · next byte offset %d\n", chunk.NextOffset) + strings.Join(lines, "\n")
				}
			}
		}
		return m, tea.Batch(refreshProfiles(m.info.Workspace), refreshTunnels(m.info.Workspace), refreshRuns(m.info.Workspace))
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
		cmd := m.scheduleFileCompletion()
		return m, cmd
	case tea.KeyPressMsg:
		if m.help {
			m.updateHelp(v, m.width, m.height)
			return m, nil
		}
		if key.Matches(v, completionNext, completionPrevious) {
			m.cycleCompletion(key.Matches(v, completionPrevious))
			cmd := m.scheduleFileCompletion()
			return m, cmd
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
			if m.files != nil {
				var cmd tea.Cmd
				if m.busy {
					cmd = m.cancelFiles()
				} else {
					cmd = m.leaveFiles()
				}
				return m, cmd
			}
			m.quitting = true
			return m, nil
		case key.Matches(v, help):
			m.help = true
			m.helpOffset = 0
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
			if m.files != nil {
				args, e := connection.Split(command)
				if e != nil {
					m.output = "REFUSED: " + safe(e.Error())
					return m, nil
				}
				cmd := m.fileCommand(args)
				return m, cmd
			}
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
			m.input.SetSuggestions(nil)
			m.completionValues = nil
			m.completionValue, m.draft = "", ""
			switch command {
			case "logs":
				m.input.Reset()
				return m, func() tea.Msg { return logsRequested{} }
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
				if e == nil && len(args) > 0 && args[0] == "scp" && len(args) <= 2 {
					if len(args) == 1 {
						m.input.SetValue("scp ")
						m.input.SetSuggestions(m.suggestions())
						m.output = "Select a live connection with Tab, then Enter"
						return m, nil
					}
					cmd := m.openFiles(args[1])
					return m, cmd
				}
				wizard := len(args) == 1 && args[0] == "connect"
				if e == nil && !wizard {
					e = connection.ValidateCommand(m.info.Workspace, args)
				}
				if e != nil {
					m.output = "REFUSED: " + safe(e.Error())
					return m, nil
				}
				m.history = append(m.history, connection.RecallCommand(args))
				m.historyIndex = len(m.history)
				m.busy = true
				workspace := m.info.Workspace
				if args[0] == "shell-close" || args[0] == "shells" || args[0] == "resume" || args[0] == "shell-control" || args[0] == "shell-detach" {
					m.shellControl = args
					return m, nil
				}
				if args[0] == "shell" {
					id := ""
					if len(args) == 3 {
						id = args[2]
					}
					return m, func() tea.Msg { return shellRequested{name: args[1], session: id} }
				}
				m.output = "Running reviewed command through Hovel…"
				if (args[0] == "chain" && args[1] != "select") || (args[0] == "run" && (args[1] == "survey" || args[1] == "now" || args[1] == "launch" || args[1] == "cancel" || args[1] == "collect" || args[1] == "close")) {
					return m, func() tea.Msg { return authenticationRequested{args: args} }
				}
				if (args[0] == "proxy" && args[1] != "inspect") || args[0] == "tunc" || (args[0] == "tund" && len(args) == 2) || (args[0] == "tunnel" && (args[1] == "create" || (args[1] == "remove" && len(args) == 3))) || args[0] == "connect" || args[0] == "reconnect" || (args[0] == "close" && len(args) == 2) || (args[0] == "profile" && (args[1] == "connect" || args[1] == "edit" || args[1] == "delete" || args[1] == "save")) {
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
		cmd = tea.Batch(cmd, m.scheduleFileCompletion())
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
			proxy = "No"
			terminal = "Retained PTY"
			if row.State == "connected" {
				tunnels = fmt.Sprint(row.TunnelCount)
			}
			if row.State != "connected" {
				proxy = "Unavailable"
			} else if row.Proxy.State == "listening" && row.ProxyPort != 0 {
				proxy = fmt.Sprintf("Yes :%d", row.ProxyPort)
			} else if row.Proxy.ID != "" || row.ProxyPort != 0 {
				proxy = "Unverified"
			}
		}
		if m.demo {
			terminal, tunnels = "Native", "0"
			if row.Name == "gateway" && row.State == "connected" {
				proxy, tunnels = "Yes :1080", "2"
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
			} else if headers[col] == "PROXY" {
				if rows[row][col] == "Unavailable" {
					style = secondary
				} else if rows[row][col] == "Unverified" {
					style = warningStyle
				}
			}
			if row >= 0 && (rows[row][col] == "—" || rows[row][col] == "Unavailable" || rows[row][col] == "Unknown") {
				style = secondary
			}
			return style.Padding(0, 1).Align(lipgloss.Center)
		}).String()
}
func (m ui) View() tea.View {
	w, h := max(1, m.width), max(1, m.height)
	var b strings.Builder

	bodyW := w
	var content string
	if m.follow != nil {
		content = m.followContent()
	} else if m.files != nil {
		content = m.fileContent(bodyW)
	} else {
		content = m.savedConnections(bodyW) + "\n\n" +
			m.activeConnections(bodyW) + "\n\n" + m.localForwards(bodyW)
		if m.demo {
			content = m.demoResources(bodyW)
		}
		// Reserve command output space even when endpoint text wraps in the table.
		content = fit(content, bodyW, min(lipgloss.Height(content), max(0, h-7)))
		outputLines := strings.Split(ansi.Wrap(m.styledOutput(), bodyW, ""), "\n")
		start := min(m.outputOffset, len(outputLines)-1)
		content += "\n\n" + m.paint(heading, "COMMAND OUTPUT") + "\n" + strings.Join(outputLines[start:], "\n")
	}
	b.WriteString(fit(content, w, max(0, h-3)))
	footer := "F1 help · Tab completion · Ctrl+C quit"
	if m.busy {
		footer = "Working… prompt remains editable · close NAME cancels a connection"
	}
	prompt := "╭─ workspace › management"
	if m.files != nil {
		prompt = "╭─ scp › " + safe(m.files.state.Name) + " › " + safe(m.files.remote)
		footer = "F1 help · Tab completion · back management · Ctrl+C cancel/back"
	}
	if m.follow != nil {
		b.WriteString("\n" + m.paint(secondary, "↑↓/PgUp/PgDn scroll · End follow · Tab streams") + "\n" + m.paint(secondary, "Esc close viewer · Alt+B management · F1 help"))
		v := tea.NewView(fit(b.String(), w, h))
		v.AltScreen = true
		return v
	}
	b.WriteString("\n" + m.paint(secondary, footer) + "\n" + m.paint(accent, prompt) + "\n" + m.input.View())
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
				command := s
				if width := ansi.StringWidth(s); width > commandWidth {
					// Keep the option/value that distinguishes long suggestions visible.
					command = "…" + ansi.Cut(s, width-commandWidth+1, width)
				}
				line := "  " + m.paint(heading, command+strings.Repeat(" ", max(0, commandWidth-lipgloss.Width(command)))) + "  " + m.paint(secondary, completionDescription(s))
				line = ansi.Truncate(line, w, "…")
				if i == selected {
					line = selectedRow(line, w, m.noColor)
				}
				rows = append(rows, line)
			}
		}
		if hint := m.commandGuidance(); hint != "" {
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

const chainHelp = `# HOVEL CHAINS
chain connect target 192.168.10.50 --user alice --password	Example: SSH password, entered in a hidden field after Hovel confirmation
chain connect target 192.168.10.50 --user alice --password PASSWORD	Example: supply the target password automatically; PASSWORD is a placeholder
chain connect target 192.168.10.50 --user alice --key ~/.ssh/id_ed25519	Example: use a local private key instead
chain connect NAME HOST --user USER [options]	Stage connection settings for an already-running OpenSSH/Dropbear server
Enter these commands in Burrow management (Alt+B).
Enter in Burrow saves private JSON, switches to Hovel and prepares throw FILE --allow-dangerous.
Enter in Hovel shows the plan; type yes to confirm. Alt+B returns to the active connection after success.
An unfinished Hovel command/confirmation is preserved; the staged command remains in Burrow output.

# CHAIN CONNECTION OPTIONS
--user USER	SSH account name; use an explicit username in copied commands
--password [PASSWORD]	Bare: hidden popup; with value: automatic target password. Conflicts with --key/--agent
--key PATH	Local private key; add --prompt for an encrypted key
--prompt	Enable hidden password/passphrase entry after Hovel confirmation
--agent PATH	Use an available SSH agent socket
--port NUMBER	SSH port; defaults to SSH config or 22
--ssh-config PATH	Local SSH configuration file
--jump HOST	SSH jump host, optionally USER@HOST:PORT
Quote spaces; --password=VALUE allows a leading dash or empty value. Escape cancels hidden entry.
Inline text is visible while typed; supplied values stay out of Burrow recall, logs and saved JSON.
Up recalls the connection command with bare --password for fresh hidden entry.
CLI literals are visible in original argv and may enter shell history. Bare --password hides entry.
Automatic values answer the target once; jump hosts need keys/agents or interactive entry.
Keep Burrow open for any password/passphrase chain; its one-use broker expires after ten minutes.
Older retained managers may require an explicit reviewed restart after inspecting connections.
Ctrl+C in Hovel cancels a staged interactive chain, including after rejecting its plan.
No --yes or -proxy on chain connect; Hovel owns confirmation and proxy creation stays explicit.

# EXISTING TUNNEL CHAINS
chain select CONNECTION	Query current forward/SOCKS identities from the connection owner
chain http CONNECTION TUNNEL_ID URL	Review HTTP through that existing tunnel; collect metadata and hash
chain export CONNECTION TUNNEL_ID URL	Stage a private consumer chain and prepare Hovel throw; Enter reviews
HTTP GET only: 8 seconds, 1 MiB, no redirects, TLS, credentials or query strings.
Use a fixed forward's destination in URL; SOCKS accepts an explicit hostname.
No automatic tunnel creation/reconnect. Dropbear upload/start is separate deployment work.
Standalone CLI prints chain JSON; --password/--prompt keeps its broker alive.
Supplied passwords need no TTY; Hovel still requires confirmation or explicit --now.
Full TUI and CLI walkthroughs: https://bochner.github.io/burrow/spec/chains.html
`

func (m ui) helpText() string {
	if m.follow != nil {
		return strings.ReplaceAll(followHelp, "\\t", "\t")
	}
	if m.files != nil {
		return strings.ReplaceAll(fileHelp, "\\t", "\t")
	}
	if words := strings.Fields(m.input.Value()); len(words) > 0 && words[0] == "chain" {
		return chainHelp
	}
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
connect NAME HOST --user USER	Connect directly; review first, then authenticate
connect target 192.168.10.50 --user alice --password	Example: password-only authentication with hidden entry
connect target 192.168.10.50 --user alice --key ~/.ssh/id_ed25519	Example: local private key; add --prompt if encrypted
--password [PASSWORD] / --key PATH / --agent PATH	Choose hidden/automatic password entry, a key, or an SSH agent
--port NUMBER / --jump HOST / --ssh-config PATH	Select port, network hops or local SSH configuration
logs / Ctrl+N	Open workspace log in Vim; Ctrl+N or :q returns; reopen refreshes
Ctrl+N in SSH/Hovel/Vim	Burrow shortcut, not forwarded to the embedded program
Ctrl+L	Open Collected output and Activity log tabs; Ctrl+L or :qa returns
Tab / Shift+Tab in results	Switch tabs; / searches; reopen refreshes both snapshots
shell NAME [SESSION_ID] / resume ID	Create or observe a retained shell / select an attached tab
shells	Discover retained shells in this workspace
shell-control [ID] / Alt+T	Explicit takeover; previous controller is fenced
shell-detach [ID]	Release your control and retain the shell
Ctrl+] / Alt+1–9	Detach to management / select a shell
Alt+←/→	Cycle shells without closing them
shell-close ID	Close one shell, keeping its connection
inspect NAME / status	Inspect connection details / verify the daemon
reconnect NAME HOST --user USER	Explicitly replace a lost connection
The connection form includes SSH keys, agents, jump hosts and a SOCKS proxy.

# RETAINED COMMANDS
reports / Menu → Reports	Browse saved reports; Enter opens, Esc returns without changing context
report ID	Open one saved Markdown report; no execution or collection
run survey NAME --os ubuntu	Review read-only Ubuntu probes, run and save a report
run prepare NAME -- COMMAND [ARG...]	Prepare on an already connected SSH target; returns a run ID
run prepare NAME -- ps -elf	Example: prepare a remote process listing without executing yet
run now NAME -- ps -elf	Review once, launch, wait and collect; view output with Ctrl+L
run now NAME --yes -- COMMAND [ARG...]	Explicitly skip review; put Burrow options before --
run now NAME --local -- /usr/bin/tool [ARG...]	Review a local tool using selected socket/config context
run prepare NAME --script PATH --mode MODE --interpreter PATH -- [ARG...]	Prepare a local script snapshot; mode is stream, inline or stage
run now NAME --script check.sh --mode stream --interpreter /bin/sh --	Review, execute and collect a local shell script
run now NAME --stdin data.bin -- /bin/sh /opt/check.sh	Existing remote script with independent binary input
run launch ID [--collect]	Launch once; --collect waits and saves output; Tab completes IDs
run list	List retained runs in this workspace
run inspect ID	Inspect execution status, output completeness and storage budget
run output ID stdout|stderr OFFSET	Read a safely displayed preview; start with offset 0
run follow ID [stdout|stderr] [OFFSET]	Open live output; Tab streams, End follow, Esc closes viewer only
run cancel ID	Review cancellation of the run's ordinary process group; local stop proves no remote cleanup
run collect ID	Review saving completed or partial output as Hovel evidence
run close ID	Review removal of working output; collected evidence remains
Use the id returned by prepare. Leaving the view does not cancel execution.
Optional prepare setting: --budget BYTES before -- (default 256 MiB per stream).
Scripts and --stdin files come from the workspace upload root (256 MiB per input).
Stream owns stdin. Inline exposes source in argv (64 KiB limit); never use secrets.
Stage uploads only after review; --keep retains it, otherwise only owned files are removed.
--timeout 30s requests cancellation; failed/unconfirmed staging cleanup remains visible.
Tab completes script options, modes and interpreter examples; select an installed interpreter.
Output shows the next byte offset. Cancel active runs before close; never put secrets in arguments.

` + chainHelp + `
# FORWARDING
tunnel create NAME forward|reverse	Create a local or reverse listener; the prompt guides arguments
tunc myserver l 8080 localhost 80	Example: local port 8080 reaches port 80 from the SSH server
tunc myserver r 8080 localhost 80	Example: remote port 8080 reaches port 80 from this machine
tunnel check NAME/ID	Test destination traffic; listening alone does not prove reachability
tunnel remove NAME/ID	Review and remove one listener (alias: tund NAME/ID)
proxy create NAME LISTEN	Add SOCKS4/5 TCP to an existing master (default bind 127.0.0.1)
proxy inspect NAME	Verify SOCKS endpoint and owner metadata; not destination reachability
proxy remove NAME	Review removal of SOCKS only; preserve connection and L/R forwards
Reverse LISTEN 0 requests a random port. Tab completes live names and tunnel IDs.
The dashboard refreshes automatically, including changes made by external CLI clients.

# SAVED CONNECTIONS & WORKSPACES
Saved row → Enter	Connect, inspect, edit or delete saved settings
profile save NAME	Save an active connection's settings, never its passwords
profile create NAME HOST --user USER	Save key/agent settings without connecting; use profile save after a password connection
profile load PATH / profile backup PATH	Open a collection / back up the selected collection
Alt+N / Alt+W	Create a workspace / open the workspace drawer

# QUIT & MORE HELP
close NAME	Review closing the connection and all its shells and listeners
quit / Ctrl+C	Choose keep running, close connections, or cancel
Keep running retains shells and releases your claims; other controllers stay in control.
OBSERVE cannot type or resize. Saved settings and daemon remain.
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
			if bodyW < 90 || ansi.StringWidth(label) > 40 {
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
