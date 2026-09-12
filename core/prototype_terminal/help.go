package main

import (
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var keys = struct{ help, back, up, down, quit, menu, resourcesUp, resourcesDown key.Binding }{
	key.NewBinding(key.WithKeys("f1"), key.WithHelp("F1", "help")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "back")),
	key.NewBinding(key.WithKeys("pgup"), key.WithHelp("PgUp", "previous page")),
	key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("PgDn", "next page")),
	key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("Ctrl+C", "quit")),
	key.NewBinding(key.WithKeys("ctrl+k"), key.WithHelp("Ctrl+K", "menu")),
	key.NewBinding(key.WithKeys("ctrl+up"), key.WithHelp("Ctrl+↑", "resources up")),
	key.NewBinding(key.WithKeys("ctrl+down"), key.WithHelp("Ctrl+↓", "resources down")),
}

type helpRow struct{ syntax, description string }
type helpSection struct {
	title, heading string
	rows           []helpRow
	examples       []string
	note           string
}

// Order follows LazySSH command_mode.py: required arguments first, optional last.
var connectionOptions = []struct {
	flag, description string
	required          bool
}{
	{"-ip", "host or address", true},
	{"-port", "SSH port", true},
	{"-user", "login username", true},
	{"-socket", "connection name", true},
	{"-proxy", "SOCKS port", false},
	{"-ssh-key", "private key path", false},
	{"-shell", "shell command", false},
	{"-no-term", "connect without a shell", false},
}

// LazySSH command_mode.py and scp_mode.py: grouped syntax, descriptions,
// argument emphasis, examples, and focused command help. Only runnable commands.
var helpSections = []helpSection{
	{title: "connections", heading: "CONNECTIONS & SAVED PROFILES", rows: []helpRow{
		{"list / connections", "Inspect names and connection state"},
		{"use NAME", "Select lab or gateway; never auto-connect"},
		{"connect", "Establish the selected simulated connection"},
		{"connect -ip HOST -port PORT -user USER -socket NAME", "Create a named connection; see help connect for optional flags"},
		{"proxy PORT", "Set the selected connection’s SOCKS proxy port"},
		{"close --yes", "End selected access, shells and tunnels"},
	}, examples: []string{"use lab", "connect", "shell"}, note: "Connection endpoints in this preview are simulated."},
	{title: "shells", heading: "INTERACTIVE SHELLS", rows: []helpRow{
		{"shell", "Open a local /bin/sh PTY on the selected fixture"},
		{"sessions", "Inspect shell IDs and lifecycle"},
		{"resume ID", "Return to a background shell"},
		{"Ctrl-]", "Background the shell and return to management"},
		{"exit", "End only the attached shell"},
	}, examples: []string{"shell", "Ctrl-]", "shell", "Ctrl-]", "resume 1"}, note: "Shells run locally in this preview. Ctrl+C interrupts the shell; Ctrl+] returns here."},
	{title: "tunnels", heading: "TUNNEL MANAGEMENT", rows: []helpRow{
		{"tunnels", "Inspect selected connection endpoints"},

		{"tunc NAME l PORT HOST PORT", "Local forward; bind on this machine"},
		{"tunc NAME r PORT HOST PORT", "Reverse forward; bind on the SSH target"},
		{"tund CONNECTION/ID", "Remove exactly one qualified tunnel"},
	}, examples: []string{"connect", "tunc lab l 8080 localhost 80", "tunnels", "tund lab/1"}, note: "Tunnel rows are simulated in this preview; no listener is opened."},
	{title: "files", heading: "SCP MODE · NAVIGATION & COLLECTION", rows: []helpRow{
		{"scp [NAME]", "Enter file mode on a connected target"},
		{"ls [PATH] / cd PATH", "List metadata / change remote directory"},
		{"tree [PATH]", "Inspect a directory tree without following symlinks"},
		{"pwd", "Show remote directory"},
		{"lls [PATH] / lcd PATH", "Inspect / change the download destination"},
		{"local", "Show separate download and upload roots"},
		{"local download PATH", "Set the download root"},
		{"local upload PATH", "Set the upload root"},
		{"get REMOTE [LOCAL]", "Download one file; refuse existing destination"},
		{"put LOCAL [REMOTE]", "Upload from the separate upload root"},
		{"mget PATTERN", "Review a batch of matching regular files"},
		{"back", "Return to management"},
	}, examples: []string{"scp", "cd /var/log", "ls", "mget *.log", "confirm"}, note: "File operations use temporary local samples. Existing destinations are never overwritten."},
	{title: "mget", heading: "MGET · BATCH TRANSFERS", rows: []helpRow{
		{"mget PATTERN", "Discover files in the current remote directory"},
		{"confirm", "Start the reviewed batch"},
		{"cancel", "Cancel review or remaining work"},
		{"transfers", "Inspect every file and its result; PgUp/PgDn scroll"},
	}, examples: []string{"scp", "cd /var/log", "mget *.log", "confirm"}, note: "Review first. Completed files survive cancellation; only the partial fixture file is removed."},
	{title: "keyboard", heading: "PROMPT, HISTORY & HELP", rows: []helpRow{
		{"F1 / help [TOPIC]", "Open contextual help without losing your draft"},
		{"Esc / F1", "Close help; restore the previous screen"},
		{"Up / Down", "Scroll help; select suggestions when help is closed"},
		{"PgUp / PgDn", "Scroll one page"},
		{"Tab / Shift+Tab", "Next / previous help topic"},
		{"Tab", "Accept completion when help is closed"},
		{"Ctrl-K", "Open the command menu"},
		{"Ctrl-Up / Ctrl-Down", "Scroll all resource rows"},
		{"Ctrl-C", "Open quit confirmation; Esc keeps working"},
		{"Ctrl-D", "Open quit confirmation when prompt is empty"},
		{"clear", "Clear output, retaining resource context"},
		{"quit", "End the fixture and its local shells"},
	}, examples: []string{}, note: "Quitting this preview stops local shells and removes scratch files."},
}

func connectionSuggestions(line string) []string {
	at := strings.LastIndex(line, " ")
	base, partial := line[:at+1], line[at+1:]
	tokens, err := words(base)
	if err != nil {
		return nil
	}
	used := make(map[string]bool)
	expecting := ""
	for _, token := range tokens[1:] {
		if expecting != "" {
			used[expecting] = true
			expecting = ""
			continue
		}
		if token == "-no-term" {
			used[token] = true
		} else {
			expecting = token
		}
	}
	var options []string
	if expecting != "" {
		values := map[string][]string{
			"-ip": {"192.0.2.10", "192.0.2.20"}, "-port": {"22", "2222"},
			"-user": {"ubuntu", "admin"}, "-socket": {"server"},
			"-proxy": {"1080", "8080"}, "-shell": {"/bin/bash", "/bin/sh"},
		}
		for _, value := range values[expecting] {
			if strings.HasPrefix(value, partial) {
				options = append(options, base+value)
			}
		}
		return options
	}
	for _, option := range connectionOptions {
		if used[option.flag] {
			continue
		}
		if strings.HasPrefix(option.flag, partial) {
			options = append(options, base+option.flag+" ")
		}
		if option.required {
			break
		}
	}
	return options
}

func commandHelp(topic string) string {
	m := &model{width: 80, helpTopic: topic}
	return m.styledHelp()
}

func (m *model) helpSyntax(s string) string {
	var out strings.Builder
	for i, token := range strings.Fields(s) {
		if i > 0 {
			out.WriteByte(' ')
		}
		color := draculaForeground
		word := strings.Trim(token, "[]")
		switch {
		case strings.HasPrefix(word, "-"):
			color = draculaCyan
		case strings.IndexFunc(word, unicode.IsLetter) >= 0 && strings.ToUpper(word) == word:
			color = draculaYellow
		case i == 0 || strings.HasPrefix(word, "Ctrl") || word == "Tab":
			color = draculaPink
		}
		if strings.HasPrefix(token, "[") {
			out.WriteString(m.paint(draculaComment, "["))
		}
		out.WriteString(m.paint(color, word))
		if strings.HasSuffix(token, "]") {
			out.WriteString(m.paint(draculaComment, "]"))
		}
	}
	return out.String()
}

var helpBackdropStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(draculaComment)).
	Background(lipgloss.Color(draculaBackground)).Faint(true)

func (m *model) helpViewportSize() (int, int) {
	height := m.height - 8 // Outside margin, border, title and navigation.
	if m.height < 12 {
		height = m.height - 4 // Small terminals show only scrollable help.
	}
	return max(1, m.width-8), max(1, height)
}

func (m *model) helpOverlay(base string) string {
	w, _ := m.helpViewportSize()
	content := m.helpView.View()
	if m.height >= 12 {
		topic := m.helpTopic
		if topic == "" {
			topic = "overview"
		}
		content = lipgloss.JoinVertical(lipgloss.Left,
			layoutStyle.MarginBottom(1).Render(m.paint(draculaPurple, "HELP · "+strings.ToUpper(topic))),
			content,
			layoutStyle.MarginTop(1).Render(m.paint(draculaComment, ansi.Truncate("↑↓ scroll · PgUp/PgDn page · Tab topic · Esc close", w, "…"))))
	}
	popup := dialogStyle.Padding(0, 1).Width(w + 4).Render(content)
	// Remove underlying ANSI styles so their resets cannot cancel the dimming.
	base = helpBackdropStyle.Render(ansi.Strip(base))
	return overlay(base, popup, max(0, (m.width-lipgloss.Width(popup))/2), max(0, (m.height-lipgloss.Height(popup))/2))
}

func (m *model) styledHelp() string {
	w, _ := m.helpViewportSize()
	var out strings.Builder
	line := func(s string) { out.WriteString(s + "\n") }
	heading := func(s string) {
		line(m.paint(draculaPurple, s) + " " + m.paint(draculaComment, strings.Repeat("─", max(0, w-ansi.StringWidth(s)-1))))
	}
	row := func(r helpRow) {
		left := m.helpSyntax(r.syntax)
		column := min(34, w/2)
		if w >= 64 && ansi.StringWidth(left) < column {
			description := strings.Split(ansi.Hardwrap(r.description, w-column-2, false), "\n")
			line("  " + left + strings.Repeat(" ", column-ansi.StringWidth(left)) + m.paint(draculaForeground, description[0]))
			for _, continuation := range description[1:] {
				line(strings.Repeat(" ", column+2) + m.paint(draculaForeground, continuation))
			}
		} else {
			line("  " + strings.ReplaceAll(ansi.Hardwrap(left, max(1, w-2), false), "\n", "\n  "))
			line("    " + strings.ReplaceAll(ansi.Hardwrap(m.paint(draculaForeground, r.description), max(1, w-4), false), "\n", "\n    "))
		}
	}
	examples := func(items []string) {
		line(m.paint(draculaComment, "  Examples"))
		for _, example := range items {
			line("  " + strings.ReplaceAll(ansi.Hardwrap(m.paint(draculaGreen, example), max(1, w-2), false), "\n", "\n  "))
		}
	}
	line(m.paint(draculaCyan, "BURROW COMMAND REFERENCE"))
	line(m.paint(draculaComment, "Commands · ") + m.paint(draculaYellow, "REQUIRED") + m.paint(draculaComment, " · [optional] · ") + m.paint(draculaGreen, "examples"))
	line("")
	topic := m.helpTopic
	if topic == "connect" {
		heading("CONNECT TO A HOST")
		row(helpRow{"connect", "Connect the selected profile in this preview"})
		row(helpRow{"connect -ip HOST -port PORT -user USER -socket NAME", "Create a named connection with explicit settings"})
		for _, required := range []bool{true, false} {
			line("")
			title := "OPTIONAL PARAMETERS"
			if required {
				title = "REQUIRED PARAMETERS"
			}
			heading(title)
			values := map[string]string{"-ip": "HOST", "-port": "PORT", "-user": "USER", "-socket": "NAME", "-proxy": "PORT", "-ssh-key": "PATH", "-shell": "SHELL"}
			for _, option := range connectionOptions {
				if option.required == required {
					syntax := strings.TrimSpace(option.flag + " " + values[option.flag])
					if !required {
						syntax = "[" + syntax + "]"
					}
					row(helpRow{syntax, option.description})
				}
			}
		}
		line("")
		examples([]string{"connect -ip 192.0.2.10 -port 22 -user ubuntu -socket lab", "connect -ip 192.0.2.10 -port 22 -user ubuntu -socket lab -proxy 1080"})
		line(m.paint(draculaComment, "  Preview: connection endpoints are simulated."))
		return strings.TrimRight(out.String(), "\n")
	}
	if topic == "scp" {
		topic = "files"
	}
	if topic == "list" {
		topic = "connections"
	}
	found := false
	for _, section := range helpSections {
		var rows []helpRow
		for _, r := range section.rows {
			if topic == "" || topic == section.title || strings.Fields(r.syntax)[0] == topic {
				rows = append(rows, r)
			}
		}
		if len(rows) == 0 {
			continue
		}
		found = true
		heading(section.heading)
		for _, r := range rows {
			row(r)
		}
		if topic != "" {
			line("")
			examples(section.examples)
			line("  " + m.paint(draculaComment, section.note))
		}
		line("")
	}
	if !found {
		line(m.paint(draculaRed, "Unknown help topic: "+safe(topic)))
	}
	line(m.paint(draculaComment, "Use ") + m.helpSyntax("help COMMAND") + m.paint(draculaComment, " for usage and examples."))
	return strings.TrimRight(out.String(), "\n")
}
