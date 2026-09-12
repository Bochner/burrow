package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/ansi"
)

// Dracula Classic, https://draculatheme.com/spec (checked 2026-09-12).
// Application roles follow LazySSH's centralized Dracula theme.
const (
	draculaBackground = "#282a36"
	draculaSurface    = "#343746"
	draculaSelection  = "#44475a"
	draculaForeground = "#f8f8f2"
	draculaComment    = "#6272a4"
	draculaPurple     = "#bd93f9"
	draculaPink       = "#ff79c6"
	draculaCyan       = "#8be9fd"
	draculaGreen      = "#50fa7b"
	draculaYellow     = "#f1fa8c"
	draculaOrange     = "#ffb86c"
	draculaRed        = "#ff5555"
)

var terminalStyles = map[string]lipgloss.Style{
	draculaPurple:     lipgloss.NewStyle().Foreground(lipgloss.Color(draculaPurple)).Bold(true),
	draculaPink:       lipgloss.NewStyle().Foreground(lipgloss.Color(draculaPink)),
	draculaCyan:       lipgloss.NewStyle().Foreground(lipgloss.Color(draculaCyan)),
	draculaComment:    lipgloss.NewStyle().Foreground(lipgloss.Color(draculaComment)),
	draculaGreen:      lipgloss.NewStyle().Foreground(lipgloss.Color(draculaGreen)),
	draculaRed:        lipgloss.NewStyle().Foreground(lipgloss.Color(draculaRed)).Bold(true),
	draculaForeground: lipgloss.NewStyle().Foreground(lipgloss.Color(draculaForeground)),
	draculaYellow:     lipgloss.NewStyle().Foreground(lipgloss.Color(draculaYellow)),
	draculaOrange:     lipgloss.NewStyle().Foreground(lipgloss.Color(draculaOrange)),
}
var workspaceStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(draculaForeground)).Background(lipgloss.Color(draculaBackground))
var tableCell = lipgloss.NewStyle().Foreground(lipgloss.Color(draculaForeground)).Padding(0, 1)
var tableOddCell = tableCell
var selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(draculaForeground)).Background(lipgloss.Color(draculaSelection)).Bold(true)
var dialogStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(draculaForeground)).Background(lipgloss.Color(draculaSurface)).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(draculaPurple)).BorderBackground(lipgloss.Color(draculaSurface)).Padding(1, 2)

type tick time.Time
type commandDone struct {
	next *model
	line string
}

// Only one command owns fixture I/O at a time. The renderer keeps its previous
// snapshot while a command works; completion and editing remain responsive.
func (m *model) work(line string) tea.Cmd {
	m.busy = true
	next := *m
	next.lines = append([]string(nil), m.lines...)
	next.shells = append([]*localShell(nil), m.shells...)
	f := *m.f
	f.targets = make([]*target, len(m.f.targets))
	for i, old := range m.f.targets {
		t := *old
		t.tunnels = append([]tunnel(nil), old.tunnels...)
		f.targets[i] = &t
	}
	if f.copy != nil {
		t := *f.copy
		f.copy = &t
	}
	if f.batch != nil {
		b := *f.batch
		b.files = append([]batchFile(nil), b.files...)
		f.batch = &b
	}
	next.f = &f
	return func() tea.Msg {
		if line == "" {
			wasCopying := f.copy != nil
			if err := f.advance(); err != nil {
				next.log("ERROR: " + err.Error())
			}
			if wasCopying && f.copy == nil {
				next.log(f.progress)
			}
		} else {
			next.submit(line)
			next.refreshCompletion()
		}
		return commandDone{&next, line}
	}
}

func pulse() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tick(t) })
}

type model struct {
	f                     *fixture
	input                 textinput.Model
	width, height, offset int
	lines, history        []string
	historyIndex          int
	historyDraft          string
	shells                []*localShell
	active                int
	color                 bool
	vtMode                bool
	helpTopic             string
	helpOpen              bool
	helpScroll            int
	listing               []entry
	listingTitle          string
	bar                   progress.Model
	busy                  bool
	completionFiles       map[string][]string
	spinner               spinner.Model
	keyHelp               help.Model
	helpView              viewport.Model
	modal                 string
	menuIndex             int
	quitSelected          bool
	quitPending           bool
	resourceOffset        int
}

func newModel(f *fixture, color bool) *model {
	i := textinput.New()
	i.Prompt = "❯ "
	i.Placeholder = "Type a command · help"
	i.CharLimit = 4096
	i.SetVirtualCursor(false)
	state := textinput.StyleState{
		Text:        lipgloss.NewStyle().Foreground(lipgloss.Color(draculaForeground)),
		Prompt:      terminalStyles[draculaCyan],
		Placeholder: terminalStyles[draculaComment],
		Suggestion:  terminalStyles[draculaComment],
	}
	i.SetStyles(textinput.Styles{Focused: state, Blurred: state,
		Cursor: textinput.CursorStyle{Color: lipgloss.Color(draculaCyan), Shape: tea.CursorBar, Blink: true}})
	i.Focus()
	i.ShowSuggestions = true
	i.KeyMap.NextSuggestion.SetKeys("down", "ctrl+n")
	i.KeyMap.PrevSuggestion.SetKeys("up", "ctrl+p")
	i.KeyMap.AcceptSuggestion.SetHelp("Tab", "complete")
	i.KeyMap.NextSuggestion.SetHelp("↑↓", "history")
	m := &model{f: f, input: i, width: 80, height: 24, active: -1, color: color}
	m.bar = progress.New(progress.WithColors(lipgloss.Color(draculaGreen), lipgloss.Color(draculaCyan)), progress.WithoutPercentage())
	m.bar.EmptyColor = lipgloss.Color(draculaComment)
	m.spinner = spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(terminalStyles[draculaCyan]))
	m.keyHelp = help.New()
	m.keyHelp.Styles = help.Styles{ShortKey: terminalStyles[draculaCyan], FullKey: terminalStyles[draculaCyan], ShortDesc: terminalStyles[draculaComment], FullDesc: terminalStyles[draculaComment], ShortSeparator: terminalStyles[draculaComment], FullSeparator: terminalStyles[draculaComment], Ellipsis: terminalStyles[draculaComment]}
	helpWidth, helpHeight := m.helpViewportSize()
	m.helpView = viewport.New(viewport.WithWidth(helpWidth), viewport.WithHeight(helpHeight))
	m.helpView.SoftWrap = true
	m.helpView.SetContent(m.styledHelp())
	m.log("DISPOSABLE PROTOTYPE · no SSH or Hovel operations\nType help, or start: connect → scp → cd /var/log → ls\nShells run /bin/sh LOCALLY; they are not remote or sandboxed.")
	return m
}
func (m *model) Init() tea.Cmd { return tea.Batch(textinput.Blink, pulse(), m.spinner.Tick) }
func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, strings.ToValidUTF8(ansi.Strip(s), "�"))
}
func (m *model) paint(color, text string) string {
	if !m.color {
		return text
	}
	return terminalStyles[color].Render(text)
}
func (m *model) log(s string) {
	added := strings.Split(safe(s), "\n")
	m.lines = append(m.lines, added...)
	if m.offset > 0 {
		m.offset += len(added)
	}
	if len(m.lines) > 400 {
		m.lines = m.lines[len(m.lines)-400:]
	}
}
func (m *model) renderResult(r result) {
	if r.Error != "" {
		m.log("ERROR: " + r.Error)
		return
	}
	if r.Message != "" {
		m.log(r.Message)
	}
	if r.Entries != nil {
		m.listing = r.Entries
		m.listingTitle = r.Message
		m.log("PERMISSIONS      SIZE  MODIFIED      NAME")
		for _, e := range r.Entries {
			name := e.Name
			if strings.ContainsAny(name, "\n\r\t\x1b") {
				name = strconv.Quote(name)
			}
			m.log(fmt.Sprintf("%s %9s  %s  %s", e.Mode, humanSize(e.Size), e.Modified, name))
		}
	}
}
func humanSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	}
	if n >= 1024*1024*1024 {
		return fmt.Sprintf("%.1f GiB", float64(n)/(1024*1024*1024))
	}
	return fmt.Sprintf("%.1f MiB", float64(n)/(1024*1024))
}
func (m *model) submit(line string) {
	m.offset = 0
	m.log("❯ " + line)
	args, err := words(line)
	if err != nil {
		m.log("ERROR: " + err.Error())
		return
	}
	if len(args) == 0 {
		return
	}
	switch args[0] {
	case "help":
		m.helpOpen, m.helpScroll = true, 0
		m.helpTopic = ""
		if len(args) > 1 {
			m.helpTopic = args[1]
		}
	case "shell":
		if m.f.current().status != "CONNECTED" {
			m.log("ERROR: connect explicitly first")
			return
		}
		s, err := openShell(filepath.Join(m.f.root, m.f.current().name), m.f.current().name, m.width, m.height-3, m.vtMode)
		if err != nil {
			m.log("ERROR: " + err.Error())
			return
		}
		m.shells = append(m.shells, s)
		m.active = len(m.shells) - 1
	case "sessions":
		for i, s := range m.shells {
			_, discarded, exited := s.snapshot()
			m.log(fmt.Sprintf("%d · %s · LOCAL PTY · exited=%t · discarded=%d bytes", i+1, s.target, exited, discarded))
		}
		if len(m.shells) == 0 {
			m.log("No local fixture shells; type shell.")
		}
	case "resume":
		if len(args) != 2 {
			m.log("ERROR: resume ID")
			return
		}
		id, err := strconv.Atoi(args[1])
		if err != nil || id < 1 || id > len(m.shells) {
			m.log("ERROR: unknown shell")
			return
		}
		s := m.shells[id-1]
		_, _, exited := s.snapshot()
		if exited {
			m.log("ERROR: shell ended")
			return
		}
		if err = s.resize(m.width, m.height-3); err != nil {
			m.log("ERROR: " + err.Error())
			return
		}
		m.active = id - 1
	default:
		r := m.f.execute(args)
		if r.Error == "" && (args[0] == "scp" || args[0] == "cd") {
			listing := m.f.execute([]string{"ls"})
			m.listing, m.listingTitle = listing.Entries, listing.Message
		}
		if args[0] == "clear" {
			m.lines = nil
		} else {
			m.renderResult(r)
		}
		if r.Error == "" && (args[0] == "close" || args[0] == "loss") {
			for _, s := range m.shells {
				if s.target == m.f.current().name {
					_, _, ended := s.snapshot()
					if !ended {
						s.close()
					}
				}
			}
		}
	}
}

// Called by a command, never from rendering or a keystroke handler.
func (m *model) refreshCompletion() {
	m.completionFiles = make(map[string][]string)
	for _, source := range []struct{ key, root, cwd string }{
		{"remote", filepath.Join(m.f.root, m.f.current().name), m.f.current().cwd},
		{"upload", m.f.root, m.f.local}, {"download", m.f.root, m.f.download},
	} {
		path, err := within(source.root, source.cwd, ".")
		if err != nil {
			continue
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() {
				name += "/"
			}
			m.completionFiles[source.key] = append(m.completionFiles[source.key], name)
		}
	}
}

func (m *model) complete() []string {
	line := m.input.Value()
	names := []string{"help", "help files", "help mget", "list", "mget *.log", "transfers", "local", "pwd", "tree", "tunc", "tund", "connections", "connect", "scp", "ls", "cd", "lls", "lcd", "get", "put", "back", "cancel", "shell", "sessions", "resume", "tunnels", "proxy", "run", "close --yes", "loss", "clear", "quit"}
	for _, target := range m.f.targets {
		for _, command := range []string{"use", "scp", "tunc"} {
			names = append(names, command+" "+target.name)
		}
		for _, tunnel := range target.tunnels {
			names = append(names, "tund "+tunnel.id)
		}
	}
	if m.f.batch != nil && m.f.batch.state == "REVIEW" {
		names = append(names, "confirm")
	}
	if strings.HasPrefix(line, "connect ") {
		return connectionSuggestions(line)
	}
	parts := strings.SplitN(line, " ", 2)
	if len(parts) == 2 && m.f.mode == "files" {
		cmd, partial := parts[0], strings.TrimLeft(parts[1], "\"'")
		if cmd == "cd" || cmd == "ls" || cmd == "get" || cmd == "put" || cmd == "lcd" || cmd == "lls" {
			context := "remote"
			if cmd == "put" {
				context = "upload"
			}
			if cmd == "lcd" || cmd == "lls" {
				context = "download"
			}
			if !strings.Contains(partial, "/") {
				names = nil
				for _, name := range m.completionFiles[context] {
					if strings.HasPrefix(name, partial) {
						if strings.ContainsAny(name, " \t\"'") {
							name = strconv.Quote(name)
						}
						names = append(names, cmd+" "+name)
					}
				}
			}

		}
	}
	var matches []string
	for _, name := range names {
		if strings.HasPrefix(name, line) {
			matches = append(matches, name)
		}
	}
	return matches
}
func shellKey(k tea.KeyPressMsg) []byte {
	if k.Text != "" {
		return []byte(k.Text)
	}
	if k.Mod.Contains(tea.ModCtrl) && k.Code >= '@' && k.Code <= '_' {
		return []byte{byte(k.Code & 31)}
	}
	if k.Mod.Contains(tea.ModCtrl) && k.Code >= 'a' && k.Code <= 'z' {
		return []byte{byte(k.Code & 31)}
	}
	switch k.String() {
	case "enter":
		return []byte{'\r'}
	case "backspace":
		return []byte{127}
	case "up":
		return []byte("\x1b[A")
	case "down":
		return []byte("\x1b[B")
	case "right":
		return []byte("\x1b[C")
	case "left":
		return []byte("\x1b[D")
	case "space", " ":
		return []byte{' '}
	}
	return nil
}
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.modal != "" || m.helpOpen {
		if _, pasted := msg.(tea.PasteMsg); pasted {
			return m, nil
		}
	}
	var spinnerCmd tea.Cmd
	m.spinner, spinnerCmd = m.spinner.Update(msg)
	switch v := msg.(type) {
	case commandDone:
		m.busy = false
		m.f, m.lines, m.shells, m.active = v.next.f, v.next.lines, v.next.shells, v.next.active
		m.listing, m.listingTitle = v.next.listing, v.next.listingTitle
		m.completionFiles = v.next.completionFiles
		m.input.SetSuggestions(m.complete())
		if m.quitPending {
			m.f.quit = true
			return m, tea.Quit
		}
		if v.line != "" {
			m.offset = 0
			if strings.HasPrefix(v.line, "help") {
				m.helpOpen, m.helpTopic, m.helpScroll = true, v.next.helpTopic, 0
				m.helpView.SetContent(m.styledHelp())
				m.helpView.GotoTop()
			}
		}
		if m.f.quit {
			return m, tea.Quit
		}
		if m.modal != "" && m.active >= 0 {
			m.active = -1
		}
		if m.vtMode && m.active >= 0 {
			return m, tea.Exec(&shellAttachment{s: m.shells[m.active]}, func(err error) tea.Msg { return shellReturned{err} })
		}
		return m, nil
	case shellReturned:
		m.active = -1
		if v.err != nil {
			m.log("Shell: " + v.err.Error())
		}
		m.log("Returned to management; sessions / resume ID.")
		return m, nil
	case tea.WindowSizeMsg:
		m.width = max(12, v.Width)
		m.height = max(6, v.Height)
		m.input.SetWidth(max(1, m.width-6))
		// Bubbles 2.2.1 SetWidth does not invalidate its cached visible range.
		value, position := m.input.Value(), m.input.Position()
		m.input.Reset()
		m.input.SetValue(value)
		m.input.SetCursor(position)
		m.bar.SetWidth(max(4, min(36, m.width/3)))
		m.keyHelp.SetWidth(m.width)
		helpWidth, helpHeight := m.helpViewportSize()
		m.helpView.SetWidth(helpWidth)
		m.helpView.SetHeight(helpHeight)
		m.helpView.SetContent(m.styledHelp())
		if m.active >= 0 {
			if err := m.shells[m.active].resize(m.width, m.height-3); err != nil {
				m.log("Resize error: " + err.Error())
			}
		}
	case tea.ColorProfileMsg, tea.BackgroundColorMsg:
		// Bubble Tea down-samples Dracula colors. NO_COLOR leaves the
		// terminal foreground and background untouched.
		return m, nil
	case tick:
		if m.active >= 0 {
			_, _, ended := m.shells[m.active].snapshot()
			if ended {
				m.active = -1
				m.log("Local shell ended; connection unchanged.")
			}
		}
		if !m.busy && m.f.copy != nil {
			return m, tea.Batch(pulse(), m.work(""))
		}
		return m, pulse()
	case tea.KeyPressMsg:
		if m.active >= 0 {
			if v.String() == "ctrl+]" {
				m.active = -1
				m.log("Shell backgrounded. Use sessions / resume ID.")
				return m, nil
			}
			if _, err := m.shells[m.active].pty.Write(shellKey(v)); err != nil {
				m.log("Shell input error: " + err.Error())
				m.active = -1
			}
			return m, nil
		}
		if m.modal != "" {
			return m, m.updateModal(v)
		}
		if key.Matches(v, keys.quit) {
			m.modal, m.quitSelected = "quit", false
			return m, nil
		}
		if key.Matches(v, keys.menu) {
			m.modal, m.menuIndex = "menu", 0
			return m, nil
		}
		if !m.helpOpen && key.Matches(v, keys.resourcesUp, keys.resourcesDown) {
			delta := 3
			if key.Matches(v, keys.resourcesUp) {
				delta = -delta
			}
			m.resourceOffset = max(0, m.resourceOffset+delta)
			return m, nil
		}
		if key.Matches(v, keys.help) {
			m.helpOpen = !m.helpOpen
			m.helpScroll = 0
			m.helpView.SetContent(m.styledHelp())
			m.helpView.GotoTop()
			return m, nil
		}
		if key.Matches(v, keys.back) {
			if m.helpOpen {
				m.helpOpen = false
			} else {
				m.input.ShowSuggestions = false
			}
			return m, nil
		}
		if m.helpOpen && key.Matches(v, keys.up, keys.down) {
			delta := m.helpView.Height()
			if key.Matches(v, keys.up) {
				delta = -delta
			}
			if delta < 0 {
				m.helpView.ScrollUp(-delta)
			} else {
				m.helpView.ScrollDown(delta)
			}
			return m, nil
		}
		if m.helpOpen {
			switch v.String() {
			case "up":
				m.helpView.ScrollUp(1)
				return m, nil
			case "down":
				m.helpView.ScrollDown(1)
				return m, nil
			case "tab", "shift+tab":
				topics := []string{"", "connect", "connections", "tunnels", "shells", "files", "mget", "keyboard"}
				index := 0
				for i, topic := range topics {
					if topic == m.helpTopic {
						index = i
					}
				}
				delta := 1
				if v.String() == "shift+tab" {
					delta = -1
				}
				m.helpTopic = topics[(index+delta+len(topics))%len(topics)]
				m.helpView.SetContent(m.styledHelp())
				m.helpView.GotoTop()
				return m, nil
			}
		}
		if m.helpOpen {
			return m, nil
		}
		if m.input.ShowSuggestions && m.input.Value() != "" && len(m.input.MatchedSuggestions()) > 0 && (v.String() == "up" || v.String() == "down") {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(v)
			return m, cmd
		}
		if v.Text != "" || v.String() == "ctrl+n" || v.String() == "ctrl+p" || v.String() == "tab" {
			m.input.ShowSuggestions = true
		}
		switch v.String() {
		case "ctrl+d":
			if m.input.Value() == "" && !m.busy {
				m.modal, m.quitSelected = "quit", false
				return m, nil
			}
		case "tab":
			m.input.SetSuggestions(m.complete())
			if suggestion := m.input.CurrentSuggestion(); suggestion != "" {
				// Complete the token and move to its next argument. Keeping an
				// exact command as its own suggestion made Tab appear inert.
				hasArguments := strings.Contains(suggestion, " ")
				switch suggestion {
				case "connect", "scp", "cd", "get", "put", "mget", "tunc", "tund", "use", "resume", "lcd", "lls", "help":
					hasArguments = true
				}
				if hasArguments && !strings.HasSuffix(suggestion, " ") && !strings.HasSuffix(suggestion, "/") {
					suggestion += " "
				}
				m.input.SetValue(suggestion)
				m.input.CursorEnd()
				m.input.SetSuggestions(m.complete())
			}
			return m, nil
		case "enter":
			if m.busy {
				return m, nil
			}
			line := strings.TrimSpace(m.input.Value())
			if line == "quit" {
				m.modal, m.quitSelected = "quit", false
				return m, nil
			}
			if line != "" {
				m.history = append(m.history, line)
				if len(m.history) > 100 {
					m.history = m.history[len(m.history)-100:]
				}
				m.historyIndex = len(m.history)
				m.input.Reset()
				return m, m.work(line)
			}
			if m.vtMode && m.active >= 0 {
				m.input.Reset()
				return m, tea.Exec(&shellAttachment{s: m.shells[m.active]}, func(err error) tea.Msg { return shellReturned{err} })
			}
			m.input.Reset()
			if m.f.quit {
				return m, tea.Quit
			}
			return m, nil
		case "up":
			m.input.ShowSuggestions = false
			if m.historyIndex == len(m.history) {
				m.historyDraft = m.input.Value()
			}
			if m.historyIndex > 0 {
				m.historyIndex--
				m.input.SetValue(m.history[m.historyIndex])
				m.input.CursorEnd()
			}
			return m, nil
		case "down":
			if m.historyIndex < len(m.history) {
				m.historyIndex++
				m.input.SetValue(m.historyDraft)
				if m.historyIndex < len(m.history) {
					m.input.SetValue(m.history[m.historyIndex])
					m.input.CursorEnd()
				}
			}
			return m, nil
		case "pgup":
			m.offset = min(len(m.lines), m.offset+max(1, m.height-8))
			return m, nil
		case "pgdown":
			m.offset = max(0, m.offset-max(1, m.height-8))
			return m, nil

		}
	}
	m.input.SetSuggestions(m.complete())
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.input.SetSuggestions(m.complete())
	return m, tea.Batch(cmd, spinnerCmd)
}
func (m *model) View() tea.View {
	content := m.render()
	if m.color && m.active < 0 && !m.f.quit {
		content = workspaceStyle.Width(m.width).Height(m.height).Render(content)
	}
	if !m.color {
		content = ansi.Strip(content)
	}
	v := tea.NewView(content)
	if m.color && m.active < 0 && !m.f.quit {
		// Nested components reset SGR colors between cells. Set the framework's
		// terminal defaults too, so those resets keep the Dracula canvas.
		v.BackgroundColor = lipgloss.Color(draculaBackground)
		v.ForegroundColor = lipgloss.Color(draculaForeground)
	}
	if m.active < 0 && !m.f.quit && m.modal == "" && !m.helpOpen {
		v.Cursor = m.input.Cursor()
		if v.Cursor != nil {
			if !m.color {
				v.Cursor.Color = nil
			}
			v.Cursor.X += 3
			v.Cursor.Y = strings.Count(content, "\n")
		}
	}
	v.AltScreen = true
	return v
}
func (m *model) resourceTable(title string, headers []string, rows [][]string, width, limit int) string {
	// Size columns to their actual content; a wide terminal is not a reason
	// to stretch short names and port numbers across the whole workspace.
	natural := 1
	for col, heading := range headers {
		cells := ansi.StringWidth(heading)
		for _, row := range rows {
			if col < len(row) {
				cells = max(cells, ansi.StringWidth(row[col]))
			}
		}
		natural += cells + 3
	}
	width = min(width, max(28, natural))
	total := len(rows)
	if total == 0 {
		rows = [][]string{make([]string, max(1, len(headers)))}
		rows[0][0] = "None"
	}
	if len(rows) > limit {
		start := 0
		for i, row := range rows {
			if len(row) > 0 && strings.HasPrefix(row[0], "›") {
				start = max(0, i-limit+1)
			}
		}
		rows = rows[start : start+limit]
		title += fmt.Sprintf(" · %d more", total-limit)
	}
	t := table.New().Rows(rows...).Width(width).Wrap(false).
		Border(lipgloss.RoundedBorder()).
		BorderTop(false).BorderBottom(true).BorderLeft(true).BorderRight(true).
		BorderStyle(terminalStyles[draculaComment]).
		BorderColumn(true).BorderHeader(m.height > 28).
		StyleFunc(func(row, col int) lipgloss.Style {
			base := tableCell
			if row == table.HeaderRow {
				return base.Foreground(lipgloss.Color(draculaPurple)).Bold(true)
			}
			if row%2 == 1 {
				base = tableOddCell
			}
			text := ""
			if row < len(rows) && col < len(rows[row]) {
				text = rows[row][col]
			}
			switch {
			case text == "CONNECTED":
				return base.Foreground(lipgloss.Color(draculaGreen))
			case text == "DISCONNECTED":
				return base.Foreground(lipgloss.Color(draculaRed))
			case strings.HasPrefix(text, "›"):
				return base.Foreground(lipgloss.Color(draculaCyan)).Bold(true)
			case strings.HasSuffix(text, "/"):
				return base.Foreground(lipgloss.Color(draculaCyan))
			}
			if col < len(headers) {
				switch headers[col] {
				case "HOST", "TYPE":
					return base.Foreground(lipgloss.Color(draculaPink))
				case "USER":
					return base.Foreground(lipgloss.Color(draculaGreen))
				case "PORT", "SIZE", "LISTEN", "PROXY":
					return base.Foreground(lipgloss.Color(draculaOrange))
				case "AUTH", "PERMISSIONS", "MODIFIED":
					return base.Foreground(lipgloss.Color(draculaComment))
				}
			}
			return base
		})
	if len(headers) > 0 {
		t.Headers(headers...)
	}
	rendered := t.String()
	width = lipgloss.Width(rendered)
	label := ansi.Truncate("─ "+title+" ", max(1, width-2), "…")
	return m.paint(draculaComment, "╭") + m.paint(draculaPurple, label) + m.paint(draculaComment, strings.Repeat("─", max(0, width-2-ansi.StringWidth(label)))+"╮") + "\n" + rendered
}

func fitLines(text string, width, height, offset int) string {
	lines := strings.Split(ansi.Hardwrap(text, max(1, width), false), "\n")
	end := max(0, len(lines)-offset)
	start := max(0, end-height)
	lines = lines[start:end]
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
