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
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/Bochner/burrow/core/connection"
	"github.com/Bochner/burrow/core/launch"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

// Adapted from the accepted core/prototype_terminal app/design at 6493f54.
// Dracula Classic; preserve its ordered overview, contextual prompt and overlays.
var purple = lipgloss.NewStyle().Foreground(lipgloss.Color("#bd93f9")).Bold(true)
var cyan = lipgloss.NewStyle().Foreground(lipgloss.Color("#8be9fd"))
var muted = lipgloss.NewStyle().Foreground(lipgloss.Color("#6272a4"))
var surface = lipgloss.NewStyle().Foreground(lipgloss.Color("#f8f8f2")).Background(lipgloss.Color("#282a36"))
var dialog = surface.Background(lipgloss.Color("#343746")).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#bd93f9")).Padding(1, 2)
var cell = lipgloss.NewStyle().Padding(0, 1)
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

type checked struct {
	info launch.Info
	err  error
}
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

func terminal(info launch.Info, noColor bool) error {
	input := textinput.New()
	input.Prompt = "╰─ "
	input.Placeholder = "connect · connections · help · quit"
	input.SetSuggestions(connection.Suggestions(nil))
	input.ShowSuggestions = true
	input.KeyMap.AcceptSuggestion = key.NewBinding(key.WithKeys("tab"))
	input.KeyMap.NextSuggestion = next
	input.KeyMap.PrevSuggestion = previous
	input.CharLimit = 2048
	input.Focus()
	m := ui{info: info, input: input, noColor: noColor, output: "Verified daemon · quit retains workspace resources."}
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
		// The owner selected Dracula Classic; terminal background doesn't change it.
	case checked:
		m.busy = false
		m.outputOffset = 0
		if v.err != nil {
			m.info.Health = "UNVERIFIED (last known PID)"
			m.output = "REFUSED: " + safe(v.err.Error())
		} else {
			m.info = v.info
			m.output = fmt.Sprintf("Verified daemon PID %d · %s", v.info.PID, safe(v.info.Health))
		}
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
			case "status":
				m.busy = true
				workspace := m.info.Workspace
				return m, func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					i, e := launch.Status(ctx, workspace)
					return checked{i, e}
				}
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
	t := table.New().Headers(headers...).Width(w).Border(lipgloss.NormalBorder()).BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).BorderColumn(false).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return cell.Bold(true)
			}
			return cell
		})
	return m.paint(cyan, title) + "\n" + t.String() + "\n" + m.paint(muted, "No resources — this capability is not implemented yet.")
}
func (m ui) connectionRows() int { return max(1, min(5, m.height-16)) }
func (m ui) activeConnections(w int) string {
	title := m.paint(cyan, "ACTIVE SSH CONNECTIONS")
	if m.connectionError != "" {
		return title + "\n" + m.connectionError
	}
	if len(m.connections) == 0 {
		return title + "\nNo connections · connect NAME HOST USER --key PATH"
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
		rows = append(rows, []string{safe(s.Name), safe(s.Host), safe(s.User), fmt.Sprint(s.Port), safe(s.State)})
	}
	if w < 60 {
		var b strings.Builder
		b.WriteString(title)
		for _, s := range visible {
			fmt.Fprintf(&b, "\n%s · %s · %s@%s:%d", safe(s.Name), safe(s.State), safe(s.User), safe(s.Host), s.Port)
		}
		return b.String() + overflow
	}
	return title + "\n" + table.New().Headers("NAME", "HOST", "USER", "PORT", "STATE").Rows(rows...).Width(w).Border(lipgloss.NormalBorder()).BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).BorderColumn(false).String() + overflow
}
func (m ui) View() tea.View {
	w, h := max(1, m.width), max(1, m.height)
	if h < 16 || w < 24 {
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
		v := tea.NewView(fit(m.paint(purple, text), w, h))
		v.AltScreen = true
		return v
	}
	var b strings.Builder
	b.WriteString(m.paint(cyan, safe(m.info.Workspace)) + m.paint(purple, " · Burrow") + "\n\n")
	bodyW := w
	sidebar := w >= 110
	if sidebar {
		bodyW -= 32
	}
	content := m.resource("SAVED CONNECTION CONFIGURATIONS", []string{"NAME", "HOST", "USER", "PORT", "AUTH", "SHELL"}, bodyW) + "\n\n" +
		m.activeConnections(bodyW) + "\n\n" +
		m.resource("TUNNELS · grouped by connection", []string{"CONNECTION", "ID", "TYPE", "LISTEN", "DESTINATION"}, bodyW)
	if h < 34 || bodyW < 60 {
		content = "SAVED CONNECTIONS · not implemented\n" + m.activeConnections(bodyW) + "\nTUNNELS · not implemented"
	}
	// Reserve command output space even when endpoint text wraps in the table.
	content = fit(content, bodyW, min(lipgloss.Height(content), max(0, h-10)))
	outputLines := strings.Split(ansi.Wrap(m.output, bodyW, ""), "\n")
	start := min(m.outputOffset, len(outputLines)-1)
	content += "\n\n" + m.paint(cyan, "COMMAND OUTPUT · PgUp/PgDn scroll") + "\n" + strings.Join(outputLines[start:], "\n")
	if sidebar {
		side := m.paint(purple, "B U R R O W\n\nWORKSPACE") + "\n" + safe(m.info.Workspace) + "\n\n" + m.paint(purple, "DAEMON") + fmt.Sprintf("\nPID %d\n%s\n\nSHELLS / RUNS / TRANSFERS\nNot implemented", m.info.PID, safe(m.info.Health))
		content = lipgloss.JoinHorizontal(lipgloss.Top, fit(content, bodyW, max(1, h-5)), "  ", fit(side, 30, max(1, h-5)))
	}
	b.WriteString(fit(content, w, max(0, h-5)))
	footer := "F1 help · Tab completion · Ctrl+C quit"
	if m.busy {
		footer = "Working… prompt remains editable · close NAME cancels a connection"
	}
	b.WriteString("\n" + m.paint(muted, footer) + "\n" + m.paint(purple, "╭─ workspace › management") + "\n" + m.input.View())
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
				rows = append(rows, prefix+s)
			}
			popup := fit(strings.Join(rows, "\n"), w, min(len(rows), max(1, h-3)))
			base = lipgloss.NewCompositor(lipgloss.NewLayer(base), lipgloss.NewLayer(popup).Y(max(0, h-3-lipgloss.Height(popup))).Z(1)).Render()
		}
	}
	if m.help || m.quitting {
		text := "BURROW COMMAND MENU · ↑↓ scroll · Esc returns\n\nstatus   Verify this workspace and daemon\nhelp     Return to this reference\nquit     Leave the daemon running\n\n" + connection.Help + "\nAlt+↑↓ scroll connections. PgUp/PgDn scroll output.\nCLI: --workspace PATH is required first.\nOptions: --offline, --hovel-package FILE"
		if m.quitting {
			text = "Quit Burrow?\n\nDaemon and workspace resources remain.\n\n  Quit   › Keep working\nTab choose · Enter confirm · Esc cancel"
			if h < 12 || w < 50 {
				text = "Quit Burrow?\n  Quit   › Keep\nTab choose · Enter\nEsc cancels"
			}
			if m.leave {
				text = strings.Replace(text, "  Quit   › Keep", "› Quit     Keep", 1)
			}
		}
		pw := max(1, min(64, w-2))
		if m.help {
			lines := strings.Split(ansi.Wrap(text, max(1, pw-6), ""), "\n")
			start := min(m.helpOffset, max(0, len(lines)-1))
			text = strings.Join(lines[start:], "\n")
		}
		popup := fit(text, max(1, pw-6), max(1, min(strings.Count(text, "\n")+1, h-6)))
		if !m.noColor {
			popup = dialog.Width(pw).Render(popup)
			base = muted.Render(ansi.Strip(base))
		}
		base = lipgloss.NewCompositor(lipgloss.NewLayer(base), lipgloss.NewLayer(popup).X(max(0, (w-lipgloss.Width(popup))/2)).Y(max(0, (h-lipgloss.Height(popup))/2)).Z(1)).Render()
	}
	if !m.noColor {
		base = surface.Width(w).Height(h).Render(base)
	} else {
		base = ansi.Strip(base)
	}
	v := tea.NewView(fit(base, w, h))
	v.AltScreen = true
	return v
}
