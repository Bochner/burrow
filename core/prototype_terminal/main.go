package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/muesli/termenv"
)

type tick time.Time

func pulse() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tick(t) })
}

type model struct {
	f                     *fixture
	input                 textinput.Model
	width, height, offset int
	lines, history        []string
	historyIndex          int
	shells                []*localShell
	active                int
	color                 bool
	vtMode                bool
}

func newModel(f *fixture, color bool) *model {
	i := textinput.New()
	i.Prompt = "❯ "
	i.CharLimit = 4096
	i.Focus()
	i.ShowSuggestions = true
	m := &model{f: f, input: i, width: 80, height: 24, active: -1, color: color}
	m.log("DISPOSABLE PROTOTYPE · no SSH or Hovel operations\nType help, or start: connect → scp → cd /var/log → ls\nShells run /bin/sh LOCALLY; they are not remote or sandboxed.")
	return m
}
func (m *model) Init() tea.Cmd { return tea.Batch(textinput.Blink, pulse()) }
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
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(text)
}
func (m *model) log(s string) {
	m.lines = append(m.lines, strings.Split(safe(s), "\n")...)
	if len(m.lines) > 400 {
		m.lines = m.lines[len(m.lines)-400:]
	}
	m.offset = 0
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
	return fmt.Sprintf("%.1f MiB", float64(n)/(1024*1024))
}
func (m *model) submit(line string) {
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

func (m *model) complete() []string {
	line := m.input.Value()
	names := []string{"help", "connections", "use lab", "use gateway", "connect", "scp", "ls", "cd", "lls", "lcd", "get", "put", "back", "cancel", "shell", "sessions", "resume", "tunnels", "tunnel", "run", "close --yes", "loss", "clear", "quit"}
	parts := strings.SplitN(line, " ", 2)
	if len(parts) == 2 && m.f.mode == "files" {
		cmd, partial := parts[0], strings.TrimLeft(parts[1], "\"'")
		if cmd == "cd" || cmd == "ls" || cmd == "get" || cmd == "put" || cmd == "lcd" || cmd == "lls" {
			root, cwd := filepath.Join(m.f.root, m.f.current().name), m.f.current().cwd
			if cmd == "put" || cmd == "lcd" || cmd == "lls" {
				root, cwd = m.f.root, m.f.local
			}
			dir, prefix := filepath.Split(partial)
			path, err := within(root, cwd, dir)
			if err == nil {
				entries, err := os.ReadDir(path)
				if err == nil {
					names = nil
					for _, e := range entries {
						if strings.HasPrefix(e.Name(), prefix) {
							name := dir + e.Name()
							if e.IsDir() {
								name += "/"
							}
							if strings.ContainsAny(name, " \t\"'") {
								name = strconv.Quote(name)
							}
							names = append(names, cmd+" "+name)
						}
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
func shellKey(k tea.KeyMsg) []byte {
	if k.Type == tea.KeyRunes {
		return []byte(string(k.Runes))
	}
	if k.Type >= 0 && k.Type <= 31 {
		return []byte{byte(k.Type)}
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
	switch v := msg.(type) {
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
		m.input.Width = max(1, m.width-6)
		if m.active >= 0 {
			if err := m.shells[m.active].resize(m.width, m.height-3); err != nil {
				m.log("Resize error: " + err.Error())
			}
		}
	case tick:
		wasCopying := m.f.copy != nil
		if err := m.f.advance(); err != nil {
			m.log("ERROR: " + err.Error())
		}
		if wasCopying && m.f.copy == nil {
			m.log(m.f.progress)
		}
		if m.active >= 0 {
			_, _, ended := m.shells[m.active].snapshot()
			if ended {
				m.active = -1
				m.log("Local shell ended; connection unchanged.")
			}
		}
		return m, pulse()
	case tea.KeyMsg:
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
		switch v.String() {
		case "ctrl+c":
			m.input.Reset()
			return m, nil
		case "ctrl+d":
			if m.input.Value() == "" {
				m.f.quit = true
				return m, tea.Quit
			}
		case "enter":
			line := strings.TrimSpace(m.input.Value())
			if line != "" {
				m.history = append(m.history, line)
				if len(m.history) > 100 {
					m.history = m.history[len(m.history)-100:]
				}
				m.historyIndex = len(m.history)
				m.submit(line)
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
			if m.historyIndex > 0 {
				m.historyIndex--
				m.input.SetValue(m.history[m.historyIndex])
				m.input.CursorEnd()
			}
			return m, nil
		case "down":
			if m.historyIndex < len(m.history) {
				m.historyIndex++
				m.input.Reset()
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
		case "tab":
			matches := m.complete()
			if len(matches) > 0 {
				m.input.SetValue(matches[0])
				m.input.CursorEnd()
			}
			return m, nil
		}
	}
	m.input.SetSuggestions(m.complete())
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.input.SetSuggestions(m.complete())
	return m, cmd
}
func (m *model) View() string {
	if m.f.quit {
		return ""
	}
	width, height := max(12, m.width), max(6, m.height)
	if m.active >= 0 {
		s := m.shells[m.active]
		tail, discarded, _ := s.snapshot()
		body := fitLines(safe(tail), width, height-3, 0)
		return m.paint("#ff2bd6", ansi.Truncate(fmt.Sprintf("LOCAL shell %d · %s · plain-output proof", m.active+1, s.target), width, "…")) + "\n" + body + "\n" + ansi.Truncate(fmt.Sprintf("Ctrl-] management · discarded %d bytes · no VT emulation", discarded), width, "…")
	}
	c := m.f.current()
	statusColor := "#ff0033"
	if c.status == "CONNECTED" {
		statusColor = "#22c55e"
	}
	header := m.paint("#ff2bd6", "BURROW") + "  " + m.paint(statusColor, "● "+c.status) + "  " + safe(c.name) + "  [LOCAL FIXTURE]"
	context := "homelab › " + c.name + " ● " + c.status + " › " + m.f.mode
	if m.f.mode == "files" {
		context += " › remote:" + c.cwd + " · local:" + m.f.local
		if width < 75 {
			context = "files › remote:" + c.cwd
		}
	}
	content := make([]string, len(m.lines))
	for i, line := range m.lines {
		if len(line) > 10 && (strings.HasPrefix(line, "-rw") || strings.HasPrefix(line, "drw")) {
			line = m.paint("#00e5ff", line[:10]) + m.paint("#9ca3af", line[10:])
		} else if strings.HasPrefix(line, "PERMISSIONS") || strings.HasSuffix(line, "/") {
			line = m.paint("#00e5ff", line)
		}
		content[i] = line
	}
	status := "Tab complete · ↑↓ history · PgUp output · help"
	if m.f.copy != nil {
		t := m.f.copy
		filled := 0
		if t.total > 0 {
			filled = int(t.done * 12 / t.total)
		}
		status = "[" + strings.Repeat("━", filled) + strings.Repeat("·", 12-filled) + "] " + m.f.progress + " · cancel"
	}
	return ansi.Truncate(header, width, "…") + "\n" + fitLines(strings.Join(content, "\n"), width, height-5, m.offset) + "\n" + m.paint("#9ca3af", ansi.Truncate(status, width, "…")) + "\n" + m.paint("#ff2bd6", ansi.Truncate("╭─ "+context, width, "…")) + "\n" + m.paint("#00e5ff", "╰─ ") + m.input.View()
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

func run() int {
	vtMode := flag.Bool("vt", false, "candidate internal full-screen shell emulator")
	jsonOutput := flag.Bool("json", false, "JSON results for fixture command/script mode")
	script := flag.String("script", "", "read fixture commands from file, or - for stdin")
	noColor := flag.Bool("no-color", false, "disable color")
	flag.Parse()
	f, err := newFixture()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer f.close()
	if *script != "" || flag.NArg() > 0 {
		var commands [][]string
		if *script != "" {
			var in *os.File
			if *script == "-" {
				in = os.Stdin
			} else {
				in, err = os.Open(*script)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 1
				}
				defer in.Close()
			}
			scanner := bufio.NewScanner(in)
			scanner.Buffer(make([]byte, 4096), 65536)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				args, e := words(line)
				if e != nil {
					fmt.Fprintln(os.Stderr, e)
					return 2
				}
				commands = append(commands, args)
			}
			if err = scanner.Err(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		} else {
			commands = [][]string{flag.Args()}
		}
		for _, args := range commands {
			r := f.execute(args)
			for r.Error == "" && f.copy != nil {
				if err = f.advance(); err != nil {
					r.Error = err.Error()
				}
			}
			if r.Command == "get" || r.Command == "put" {
				r.Message = f.progress
			}
			if *jsonOutput {
				json.NewEncoder(os.Stdout).Encode(r)
			} else {
				fmt.Println(r.Message)
				for _, e := range r.Entries {
					fmt.Printf("%s %d %s\n", e.Mode, e.Size, e.Name)
				}
				if r.Error != "" {
					fmt.Fprintln(os.Stderr, r.Error)
				}
			}
			if r.Error != "" {
				return 1
			}
			if f.quit {
				break
			}
		}
		return 0
	}
	if !term.IsTerminal(os.Stdin.Fd()) {
		fmt.Fprintln(os.Stderr, "Interactive prototype needs a terminal; use --script - --json for a pipe.")
		return 2
	}
	color := !*noColor && os.Getenv("NO_COLOR") == ""
	if !color {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	m := newModel(f, color)
	m.vtMode = *vtMode
	defer func() {
		for _, s := range m.shells {
			s.close()
		}
	}()
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("Prototype closed; local shells and scratch files removed.")
	return 0
}
func main() { os.Exit(run()) }
