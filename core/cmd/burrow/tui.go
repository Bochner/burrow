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
	info                                 launch.Info
	input                                textinput.Model
	width, height                        int
	noColor, busy, help, quitting, leave bool
	output                               string
	history                              []string
	historyIndex                         int
	draft                                string
	outputOffset                         int
	connectionOffset                     int
	connections                          []connection.State
	connectionError                      string
	helpOffset                           int
}

func newUI(info launch.Info, noColor bool) ui {
	input := textinput.New()
	styleInput(&input)
	input.Prompt = "╰─ "
	input.Placeholder = "connect · connections · help · quit"
	input.SetSuggestions(connection.Suggestions(nil))
	input.ShowSuggestions = true
	input.KeyMap.AcceptSuggestion = key.NewBinding(key.WithKeys("tab"))
	input.KeyMap.NextSuggestion = next
	input.KeyMap.PrevSuggestion = previous
	input.CharLimit = 2048
	input.Focus()
	return ui{info: info, input: input, noColor: noColor, output: "Verified daemon · quit retains workspace resources."}
}

func terminal(info launch.Info, noColor bool, options launch.Options) error {
	m := newFrame(info, noColor, options)
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
func (m ui) Init() tea.Cmd { return tea.Batch(m.input.Focus(), refreshConnections(m.info.Workspace)) }
func (m ui) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case connectionTick:
		return m, refreshConnections(m.info.Workspace)
	case connectionList:
		if v.err != nil {
			m.connectionError = "UNVERIFIED · connection owner status unavailable"
			for i := range m.connections {
				m.connections[i].State = "unverified"
			}
		} else {
			m.connectionError = ""
			m.connections = v.states
			m.input.SetSuggestions(connection.Suggestions(v.states))
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
	case tea.WindowSizeMsg:
		m.width = max(1, v.Width)
		m.height = max(1, v.Height)
		m.input.SetWidth(max(1, m.width-4))
	case tea.ColorProfileMsg:
		m.noColor = m.noColor || v.Profile == colorprofile.ASCII
	case tea.BackgroundColorMsg:
		// The owner selected Herdr's Catppuccin Mocha; keep the explicit dark palette.
	case tea.PasteMsg:
		if m.help || m.quitting {
			return m, nil
		}
		m.input.SetValue(m.input.Value() + safe(v.Content))
		m.input.CursorEnd()
		return m, nil
	case tea.KeyPressMsg:
		if m.quitting {
			switch {
			case key.Matches(v, escape):
				m.quitting = false
			case key.Matches(v, choose):
				m.leave = !m.leave
			case key.Matches(v, enter):
				if m.leave {
					return m, tea.Quit
				}
				m.quitting = false
			}
			return m, nil
		}
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
			m.leave = false
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
				m.leave = false
			case "status":
				m.busy = true
				return m, func() tea.Msg { return statusRequested{} }
			default:
				args, e := connection.Split(command)
				if e == nil {
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
func (m ui) resource(title string, headers []string, w int) string {
	t := table.New().Headers(headers...).Width(w).Border(lipgloss.NormalBorder()).BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).BorderColumn(false).BorderStyle(separatorStyle).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return tableCell.Bold(true)
			}
			return tableCell
		})
	return m.paint(heading, title) + "\n" + t.String() + "\n" + m.paint(secondary, "No resources — this capability is not implemented yet.")
}
func (m ui) connectionRows() int { return max(1, min(5, m.height-16)) }
func (m ui) activeConnections(w int) string {
	title := m.paint(heading, "ACTIVE SSH CONNECTIONS")
	if m.connectionError != "" {
		return title + "\n" + m.paint(errorStyle, m.connectionError)
	}
	if len(m.connections) == 0 {
		return title + "\n" + m.paint(secondary, "No connections · ") + m.syntax("connect NAME HOST USER --key PATH")
	}
	start := min(m.connectionOffset, max(0, len(m.connections)-m.connectionRows()))
	end := min(len(m.connections), start+m.connectionRows())
	visible := m.connections[start:end]
	overflow := ""
	if len(visible) < len(m.connections) {
		overflow = fmt.Sprintf("\n%d–%d of %d · Alt+↑↓ scroll", start+1, end, len(m.connections))
	}
	var rows [][]string
	for _, s := range visible {
		rows = append(rows, []string{safe(s.Name), safe(s.Host), safe(s.User), fmt.Sprint(s.Port), m.paint(connectionStyle(s.State), safe(s.State))})
	}
	if w < 60 {
		var b strings.Builder
		b.WriteString(title)
		for _, s := range visible {
			fmt.Fprintf(&b, "\n%s · %s · %s@%s:%d", safe(s.Name), m.paint(connectionStyle(s.State), safe(s.State)), safe(s.User), safe(s.Host), s.Port)
		}
		return b.String() + overflow
	}
	return title + "\n" + table.New().Headers("NAME", "HOST", "USER", "PORT", "STATE").Rows(rows...).Width(w).Border(lipgloss.NormalBorder()).BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).BorderColumn(false).BorderStyle(separatorStyle).StyleFunc(func(row, col int) lipgloss.Style {
		if row == table.HeaderRow {
			return tableCell.Foreground(lipgloss.Color(subtextColor)).Bold(true)
		}
		return tableCell
	}).String() + overflow
}
func (m ui) View() tea.View {
	w, h := max(1, m.width), max(1, m.height)
	if h < 12 || w < 24 {
		text := "Resize window\nCtrl+C to quit"
		if m.quitting {
			text = "Quit?\nTab: choose\nEnter: keep"
			if m.leave {
				text = "Quit?\nTab: choose\nEnter: quit"
			}
		}
		if m.quitting && h >= 6 && w >= 24 {
			text = "Quit Burrow?\n  Quit   › Keep\nTab choose · Enter\nEsc cancels"
			if m.leave {
				text = strings.Replace(text, "  Quit   › Keep", "› Quit     Keep", 1)
			}
		}
		v := tea.NewView(fit(m.paint(accent, text), w, h))
		v.AltScreen = true
		return v
	}
	var b strings.Builder

	bodyW := w
	content := m.resource("SAVED CONNECTION CONFIGURATIONS", []string{"NAME", "HOST", "USER", "PORT", "AUTH", "SHELL"}, bodyW) + "\n\n" +
		m.activeConnections(bodyW) + "\n\n" +
		m.resource("TUNNELS · grouped by connection", []string{"CONNECTION", "ID", "TYPE", "LISTEN", "DESTINATION"}, bodyW)
	if h < 34 || bodyW < 60 {
		content = m.paint(heading, "SAVED CONNECTIONS") + m.paint(secondary, " · not implemented") + "\n\n" + m.activeConnections(bodyW) + "\n\n" + m.paint(heading, "TUNNELS") + m.paint(secondary, " · not implemented")
	}
	// Reserve command output space even when endpoint text wraps in the table.
	content = fit(content, bodyW, min(lipgloss.Height(content), max(0, h-7)))
	outputLines := strings.Split(ansi.Wrap(m.styledOutput(), bodyW, ""), "\n")
	start := min(m.outputOffset, len(outputLines)-1)
	content += "\n\n" + m.paint(heading, "COMMAND OUTPUT · PgUp/PgDn scroll") + "\n" + strings.Join(outputLines[start:], "\n")
	b.WriteString(fit(content, w, max(0, h-3)))
	footer := "F1 help · Tab completion · Ctrl+C quit"
	if m.busy {
		footer = "Working… prompt remains editable · close NAME cancels a connection"
	}
	b.WriteString("\n" + m.paint(secondary, footer) + "\n" + m.paint(accent, "╭─ workspace › management") + "\n" + m.input.View())
	base := fit(b.String(), w, h)
	if !m.help && !m.quitting && m.input.Value() != "" && m.input.ShowSuggestions {
		matches := m.input.MatchedSuggestions()
		if len(matches) > 0 {
			rows := []string{"COMPLETION · ↑↓ select · Tab accept"}
			count := max(1, min(6, h-5))
			start := min(max(0, m.input.CurrentSuggestionIndex()-count+1), max(0, len(matches)-count))
			end := min(len(matches), start+count)
			if len(matches) > count {
				rows[0] = fmt.Sprintf("COMPLETION · %d–%d of %d · ↑↓", start+1, end, len(matches))
			}
			for i := start; i < end; i++ {
				s := matches[i]
				prefix := "  "
				if i == m.input.CurrentSuggestionIndex() {
					prefix = "› "
				}
				rows = append(rows, m.choice(s, prefix == "› ", w))
			}
			popup := solid(strings.Join(rows, "\n"), w, min(len(rows), max(1, h-3)), popupColor, m.noColor)
			base = lipgloss.NewCompositor(lipgloss.NewLayer(base), lipgloss.NewLayer(popup).Y(max(0, h-3-lipgloss.Height(popup))).Z(1)).Render()
		}
	}
	if m.help || m.quitting {
		base = m.overlay(base, w, h).Render()
	}

	base = solid(base, w, h, baseColor, m.noColor)
	v := tea.NewView(fit(base, w, h))
	v.AltScreen = true
	return v
}

func (m ui) helpText() string {
	return "status   Verify this workspace and daemon\nhelp     Return to this reference\nquit     Leave the daemon running\n\n" + connection.Help + "\nF6 / Shift+F6 focus: prompt, workspaces, New, Menu, shells, tabs, resources.\nArrows select; Enter activates; Esc returns to prompt.\nCtrl+P commands (Alt+M), Alt+N New, Alt+W workspace drawer.\nTab completes the prompt. Click daemon for metadata.\nAlt+↑↓ scroll connections. PgUp/PgDn scroll output.\nCLI: --workspace PATH is required first.\nOptions: --offline, --hovel-package FILE"
}
func (m ui) helpViewport(w, h int) viewport.Model {
	return scrollBody(m.syntax(m.helpText()), max(1, min(96, w-4)-6), max(1, min(30, h-4)-8), m.helpOffset)
}
func (m ui) overlay(base string, w, h int) *lipgloss.Compositor {
	pw, ph := min(96, w-4), min(30, h-4)
	if m.quitting {
		pw, ph = min(64, w-4), 10
	}
	pw, ph = max(8, pw), max(8, min(ph, h))
	x, y := (w-pw)/2, (h-ph)/2
	bodyW := pw - 6
	title := m.paint(accent, "BURROW COMMAND MENU")
	body := m.helpViewport(w, h).View()
	footer := m.paint(secondary, "↑↓ scroll · Esc returns")
	if m.quitting {
		title = m.paint(accent, "Quit Burrow?")
		body = fit("Daemon and workspace resources remain.", bodyW, 1)
		footer = m.paint(secondary, "Tab choose · Enter confirm · Esc cancel")
	}
	text := title + "\n\n" + body
	popup := dialogStyle.Width(pw).Height(ph).Render(fit(text, bodyW, ph-4))
	layers := []*lipgloss.Layer{lipgloss.NewLayer(solid(m.paint(secondary, ansi.Strip(base)), w, h, baseColor, m.noColor)).ID("modal-backdrop"), lipgloss.NewLayer(solid(popup, pw, ph, popupColor, m.noColor)).X(x).Y(y).Z(1).ID("child-modal")}
	if m.quitting {
		layers = append(layers, lipgloss.NewLayer(solid(footer, bodyW, 1, popupColor, m.noColor)).X(x+3).Y(y+ph-3).Z(2).ID("modal-footer"))
		bw := min(20, (bodyW-2)/2)
		keepLabel := "Keep working"
		if bw < 14 {
			keepLabel = "Keep"
		}
		layers = append(layers, lipgloss.NewLayer(solid(m.choice("Quit", m.leave, bw), bw, 1, popupColor, m.noColor)).X(x+3).Y(y+ph-4).Z(2).ID("quit-leave"), lipgloss.NewLayer(solid(m.choice(keepLabel, !m.leave, bw), bw, 1, popupColor, m.noColor)).X(x+5+bw).Y(y+ph-4).Z(2).ID("dismiss"))
	} else {
		layers = append(layers, lipgloss.NewLayer(solid(footer, bodyW, 1, popupColor, m.noColor)).X(x+3).Y(y+ph-3).Z(2).ID("dismiss"))
	}
	return lipgloss.NewCompositor(layers...)
}
