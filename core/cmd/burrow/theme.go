package main

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	helpview "charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Herdr's regular "catppuccin" is Catppuccin Mocha. Semantic roles follow
// its pinned palette and Catppuccin's style guide; see ui-reference-repair.md.
const (
	baseColor         = "#1e1e2e"
	sidebarColor      = "#181825"
	popupColor        = "#181825"
	textColor         = "#cdd6f4"
	subtextColor      = "#a6adc8"
	borderColor       = "#6c7086"
	blueColor         = "#89b4fa"
	lavenderColor     = "#b4befe"
	rowSelectionColor = "#313244"
)

var accent = lipgloss.NewStyle().Foreground(lipgloss.Color(lavenderColor)).Bold(true)

// Solid blocks and stippled shadow match pinned Hovel v0.4.2's wide CLI
// wordmark technique, redrawn for Burrow's 28-cell sidebar.
const burrowWordmark = `██  █ █ ██  ██  ███ █   █
█░█ █░█░█░█ █░█ █░█░█░  █░
██ ░█░█░██ ░██ ░█░█░█░█ █░
█░█ █░█░█░█ █░█ █░█░█░█░█░
██ ░███░█░█░█░█░███░ █ █ ░
 ░░  ░░░ ░ ░ ░ ░ ░░░  ░ ░`

var heading = lipgloss.NewStyle().Foreground(lipgloss.Color(blueColor)).Bold(true)
var secondary = lipgloss.NewStyle().Foreground(lipgloss.Color(subtextColor))
var separatorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(borderColor))
var pageStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(textColor))
var selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(baseColor)).Background(lipgloss.Color(blueColor)).Bold(true)
var activeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(lavenderColor)).Background(lipgloss.Color("#313244")).Bold(true)
var successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#a6e3a1"))
var warningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f9e2af"))
var errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f38ba8"))
var numberStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#fab387"))
var keywordStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#cba6f7"))
var dialogStyle = pageStyle.Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(lavenderColor)).Padding(1, 2)

// Adapted from the accepted prototype's overlay: fill only unspecified cells.
// Styling an ANSI string after composition leaves reset/background holes; forcing
// every background would instead erase the selected action's explicit fill.
func solid(text string, w, h int, bg string, noColor bool) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	blank := lipgloss.NewStyle().Background(lipgloss.Color(bg)).Width(w).Height(h).Render("")
	canvas := lipgloss.NewCanvas(w, h).Compose(lipgloss.NewLayer(blank)).Compose(lipgloss.NewLayer(fit(text, w, h)))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if cell := canvas.CellAt(x, y); cell != nil && cell.Width > 0 {
				filled := *cell
				if filled.Style.Bg == nil {
					filled.Style.Bg = lipgloss.Color(bg)
				}
				if filled.Style.Fg == nil {
					filled.Style.Fg = lipgloss.Color(textColor)
				}
				canvas.SetCell(x, y, &filled)
			}
		}
	}
	result := canvas.Render()
	if noColor {
		return ansi.Strip(result)
	}
	return result
}

// Tint rendered cells without replacing semantic foregrounds or column positions.
func selectedRow(text string, width int, noColor bool) string {
	canvas := lipgloss.NewCanvas(width, 1).Compose(lipgloss.NewLayer(solid(text, width, 1, rowSelectionColor, noColor)))
	for x := 0; x < width; x++ {
		if cell := canvas.CellAt(x, 0); cell != nil && cell.Width > 0 {
			filled := *cell
			filled.Style.Bg = lipgloss.Color(rowSelectionColor)
			if x == 0 {
				filled.Content = "›"
			}
			canvas.SetCell(x, 0, &filled)
		}
	}
	if noColor {
		return ansi.Strip(canvas.Render())
	}
	return canvas.Render()
}

func styleInput(input *textinput.Model) {
	input.SetVirtualCursor(false)
	styles := input.Styles()
	styles.Focused = textinput.StyleState{Text: pageStyle, Prompt: accent, Placeholder: secondary, Suggestion: secondary}
	styles.Blurred = styles.Focused
	styles.Cursor.Color = lipgloss.Color("#f5e0dc")
	styles.Cursor.Blink = true
	input.SetStyles(styles)
}
func (m ui) choice(label string, selected bool, width int) string {
	prefix := "  "
	style := pageStyle
	if selected {
		prefix = "› "
		style = selectedStyle
	}
	return m.paint(style.Width(width), ansi.Truncate(prefix+label, width, "…"))
}

var commandToken = regexp.MustCompile(`'[^']*'|"(?:\\.|[^"\\])*"|\S+`)

func (m ui) syntax(line string, reference bool) string {
	// ponytail: token hints cover Burrow's command reference and recaps; use a
	// shell lexer if arbitrary shell source becomes a supported viewer input.
	previous := ""
	index, connectionIndex := -1, -1
	fields := strings.Fields(line)
	if len(fields) > 1 && fields[0] == "tunnel" && fields[1] == "create" {
		connectionIndex = 2
	} else if len(fields) > 0 && fields[0] == "tunc" {
		connectionIndex = 1
	}
	return commandToken.ReplaceAllStringFunc(line, func(token string) string {
		index++
		word := strings.Trim(token, "'\"[](),.")
		prior := previous
		previous = strings.ToLower(word)
		style := pageStyle
		switch {
		case index == 0 && word == "burrow":
			style = heading
		case word == "/usr/bin/ssh":
			style = heading
		case prior == "-o":
			if option, value, ok := strings.Cut(word, "="); ok {
				valueStyle := keywordStyle
				if strings.HasPrefix(value, "/") {
					valueStyle = secondary
				}
				return strings.Replace(token, word, m.paint(keywordStyle, option)+m.paint(secondary, "=")+m.paint(valueStyle, value), 1)
			}
		case prior == "-d":
			return strings.Replace(token, word, m.endpoint(word), 1)
		case reference && strings.IndexFunc(word, unicode.IsLetter) >= 0 && strings.ToUpper(word) == word && word != "F1" && word != "F6":
			style = warningStyle
		case connectionIndex >= 0 && index == connectionIndex:
			style = accent
		case connectionIndex >= 0 && index == connectionIndex+1:
			style = keywordStyle
		case connectionIndex >= 0 && index == connectionIndex+2:
			if !strings.Contains(word, ":") {
				return m.paint(warningStyle, token)
			}
			return strings.Replace(token, word, m.endpoint(word), 1)
		case connectionIndex >= 0 && index == connectionIndex+3:
			style = hostStyle
		case connectionIndex >= 0 && index == connectionIndex+4:
			style = warningStyle
		case word == "burrow-hop-0":
			style = hostStyle
		case strings.Contains(word, "@"):
			return strings.Replace(token, word, m.endpoint(word), 1)
		case strings.HasPrefix(word, "-"):
			style = heading
		case prior == "tund" || prior == "remove" || prior == "check" || prior == "connect" || prior == "reconnect" || prior == "close" || prior == "inspect" || prior == "shell" || prior == "resume" || prior == "shell-close":
			style = accent
		case strings.HasPrefix(word, "#") && strings.TrimPrefix(word, "#") != "" && strings.Trim(strings.TrimPrefix(word, "#"), "0123456789") == "":
			style = accent
		case prior == "-p" || prior == "--port":
			style = warningStyle
		case prior == "-i" || prior == "--key":
			style = infoStyle
		case strings.HasPrefix(word, "/") || strings.HasPrefix(word, "~/"):
			style = secondary
		case strings.HasSuffix(word, ":"):
			style = accent
		case word == "state" || word == "master" || word == "PID" || word == "socket":
			style = accent
		case word == "connected" || word == "active" || word == "failed" || word == "lost" || word == "closed" || word == "disconnected" || word == "connecting":
			style = connectionStyle(word)
		case word == "background":
			style = infoStyle
		case word == "opening" || word == "closing":
			style = warningStyle
		case strings.Trim(word, "0123456789") == "" && word != "":
			style = numberStyle
		case strings.HasPrefix(word, "Ctrl") || strings.HasPrefix(word, "Alt") || strings.HasPrefix(word, "Shift+") || word == "Tab" || word == "Enter" || word == "Esc" || word == "F1" || word == "F6" || word == "PgUp/PgDn":
			style = keywordStyle
		case strings.IndexFunc(word, unicode.IsLetter) >= 0 && strings.ToUpper(word) == word:
			style = warningStyle
		case previous == "tunnel" || previous == "tunc" || previous == "tund" || previous == "shell" || previous == "shells" || previous == "resume" || previous == "shell-close" || previous == "ssh" || previous == "connect" || previous == "reconnect" || previous == "inspect" || previous == "connections" || previous == "close" || previous == "status" || previous == "help" || previous == "quit" || previous == "profile" || previous == "profiles" || previous == "history":
			style = heading
		case prior == "tunnel" && (word == "create" || word == "list" || word == "check" || word == "remove"):
			style = heading
		case prior == "profile" && (word == "create" || word == "select" || word == "save" || word == "edit" || word == "delete" || word == "collection" || word == "load" || word == "backup"):
			style = heading
		}
		return m.paint(style, token)
	})
}

func (m ui) endpoint(value string) string {
	if strings.Contains(value, ",") {
		hops := strings.Split(value, ",")
		for i, hop := range hops {
			hops[i] = m.endpoint(hop)
		}
		return strings.Join(hops, ",")
	}
	if user, host, ok := strings.Cut(value, "@"); ok {
		return m.paint(successStyle, user) + "@" + m.endpoint(host)
	}
	if at := strings.LastIndex(value, ":"); at >= 0 {
		if _, err := strconv.Atoi(value[at+1:]); err == nil {
			return m.paint(hostStyle, value[:at]) + ":" + m.paint(warningStyle, value[at+1:])
		}
	}
	return m.paint(hostStyle, value)
}

// Shared by form descriptions and plain operational output. Style only after
// sanitizing external text; retain whitespace and leave secrets to masked inputs.
func (m ui) semanticText(text string) string {
	lines := strings.Split(text, "\n")
	config := false
	for i, line := range lines {
		line = safe(line)
		if line == "SSH command:" {
			lines[i] = m.paint(heading, line)
			continue
		}
		if line == "Generated config:" {
			config = true
			lines[i] = m.paint(accent, line)
			continue
		}
		if config && strings.TrimSpace(line) == "" {
			config = false
		}
		if config {
			trimmed := strings.TrimLeft(line, " ")
			key, value, ok := strings.Cut(trimmed, " ")
			if ok {
				style := fieldStyle(strings.ToUpper(key))
				switch {
				case strings.Trim(value, "0123456789") == "" && key != "Port":
					style = numberStyle
				case strings.HasPrefix(strings.Trim(value, "\""), "/"):
					style = secondary
				case value == "yes" || value == "no":
					style = keywordStyle
				}
				switch key {
				case "Host":
					style = accent
				case "IdentityFile", "IdentityAgent":
					style = infoStyle
				}
				colored := m.paint(style, value)
				if key == "ProxyCommand" {
					colored = m.syntax(value, false)
				}
				lines[i] = line[:len(line)-len(trimmed)] + m.paint(heading, key) + " " + colored
				continue
			}
		}
		label, value, field := strings.Cut(line, ": ")
		if field && !strings.ContainsAny(label, "/@") {
			style := fieldStyle(strings.ToUpper(label))
			styled := m.paint(style, value)
			if label == "Endpoint" || label == "Jump" || label == "Listen" || label == "Destination" || label == "Remote listener" || label == "Local destination" || label == "Local listener" || label == "Remote destination" {
				styled = m.endpoint(value)
			}
			if label == "Destination" && strings.HasPrefix(value, "/") {
				styled = m.paint(secondary, value)
			}
			if label == "SOCKS proxy" {
				if protocol, endpoint, ok := strings.Cut(value, " · "); ok {
					styled = m.paint(infoStyle, protocol) + m.paint(secondary, " · ") + m.endpoint(endpoint)
				}
			}
			if value == "none" || value == "Off" || value == "Unavailable" || value == "Unknown" {
				styled = m.paint(secondary, value)
			}
			lines[i] = m.paint(accent, label+":") + " " + styled
		} else {
			lines[i] = m.syntax(line, false)
		}
	}
	return strings.Join(lines, "\n")
}

var jsonToken = regexp.MustCompile(`"(?:\\.|[^"\\])*"|true|false|null|-?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?`)

func (m ui) styledOutput() string {
	if m.noColor {
		return m.output
	}
	if !json.Valid([]byte(m.output)) {
		if strings.HasPrefix(m.output, "REFUSED:") {
			return m.paint(errorStyle, m.output)
		}
		return m.semanticText(m.output)
	}
	var b strings.Builder
	end := 0
	field := ""
	for _, at := range jsonToken.FindAllStringIndex(m.output, -1) {
		b.WriteString(m.paint(secondary, m.output[end:at[0]]))
		token := m.output[at[0]:at[1]]
		style := numberStyle
		if strings.HasPrefix(token, "\"") {
			style = successStyle
			if strings.HasPrefix(strings.TrimSpace(m.output[at[1]:]), ":") {
				style = heading
				_ = json.Unmarshal([]byte(token), &field)
			} else {
				switch field {
				case "listen", "destination", "requestedListen":
					b.WriteString(m.paint(secondary, `"`) + m.endpoint(token[1:len(token)-1]) + m.paint(secondary, `"`))
					end = at[1]
					continue
				case "direction":
					style = keywordStyle
				case "name", "id", "generation", "creation", "runID", "session", "host", "hostname", "user", "username", "key", "agent", "shell", "socket", "jump", "sshConfig", "collection", "detail", "error":
					style = fieldStyle(strings.ToUpper(field))
				case "state", "status":
					var state string
					_ = json.Unmarshal([]byte(token), &state)
					style = connectionStyle(state)
				}
			}
		} else if token == "true" || token == "false" || token == "null" {
			style = keywordStyle
		} else if field == "port" || field == "proxyPort" {
			style = warningStyle
		}
		b.WriteString(m.paint(style, token))
		end = at[1]
	}
	b.WriteString(m.paint(secondary, m.output[end:]))
	return b.String()
}
func scrollBody(text string, width, height, offset int) viewport.Model {
	v := viewport.New(viewport.WithWidth(max(1, width)), viewport.WithHeight(max(1, height)))
	v.SetContent(ansi.Wrap(text, max(1, width), ""))
	v.SetYOffset(offset)
	return v
}

func connectionStyle(state string) lipgloss.Style {
	switch state {
	case "connected", "active", "running", "listening", "traffic-observed":
		return successStyle
	case "failed", "lost", "closed", "disconnected", "unverified", "unavailable":
		return errorStyle
	case "connecting", "reconnecting", "opening", "closing":
		return warningStyle
	default:
		return secondary
	}
}

// Like Crush's status help, this row is reserved below the editor and uses
// Bubbles' width-aware key/description rendering. The darker crust is our palette.
func (m *frame) commandHelp() string {
	h := helpview.New()
	h.SetWidth(max(1, m.width-2))
	h.Styles = helpview.Styles{ShortKey: accent, ShortDesc: secondary, ShortSeparator: separatorStyle, Ellipsis: secondary}
	binding := func(keys, label string) key.Binding {
		return key.NewBinding(key.WithKeys(strings.ToLower(keys)), key.WithHelp(keys, label))
	}
	keys := []key.Binding{showHovel, binding("Ctrl+P", "menu"), binding("F1", "help"), binding("Ctrl+C", "quit")}
	if m.current().activeUI().files != nil {
		keys = []key.Binding{binding("back", "management"), binding("Ctrl+C", "cancel/back"), binding("F1", "help"), binding("PgUp/PgDn", "scroll"), showHovel}
	}
	selection := binding("Alt+S", "native selection")
	if m.mouseDisabled {
		selection = binding("Alt+S", "panel selection")
		keys = []key.Binding{selection, binding("drag", "highlight"), binding("Ctrl+Shift+C/V", "copy/paste"), showHovel}
		if m.terminalFocused() {
			keys[len(keys)-1] = showBurrow
		}
		return solid(" "+h.ShortHelpView(keys), m.width, 1, "#11111b", m.noColor)
	}
	if m.terminalFocused() {
		if m.current().tab == "shell" {
			keys = []key.Binding{binding("Ctrl+]", "management"), numberedShell, previousShell, binding("Ctrl+C", "interrupt"), binding("drag", "select text")}
			return solid(" "+h.ShortHelpView(keys), m.width, 1, "#11111b", m.noColor)
		}
		keys = []key.Binding{showBurrow, binding("drag", "select text"), selection, binding("Shift+PgUp/PgDn", "scroll"), binding("Shift+Home/End", "oldest/live"), binding("Ctrl+]", "frame controls"), binding("Ctrl+C", "interrupt CLI")}
		if m.current().canRestartCLI() {
			keys = []key.Binding{restartTerminal, showBurrow, binding("drag", "select text"), binding("Shift+PgUp/PgDn", "scroll"), binding("Shift+Home/End", "oldest/live"), binding("Ctrl+]", "frame controls")}
		}
		return solid(" "+h.ShortHelpView(keys), m.width, 1, "#11111b", m.noColor)
	}
	keys = append(keys, binding("drag", "select text"), selection)
	if m.current().focus == "prompt" && (m.current().tab == "" || m.current().tab == "files") {
		keys = append(keys, binding("Tab/Shift+Tab", "cycle"), binding("↑↓", "history"))
	} else {
		keys = append(keys, binding("↑↓", "select"), binding("Enter", "open"), binding("Esc", "prompt"))
	}
	keys = append(keys, binding("F6", "focus"))
	return solid(" "+h.ShortHelpView(keys), m.width, 1, "#11111b", m.noColor)
}

func centered(text string, width int) string {
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, ansi.Truncate(text, width, "…"))
}

// LazySSH separates field roles; mapped to Catppuccin rather than decoration.
var hostStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f5c2e7"))
var infoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#94e2d5"))

func fieldStyle(header string) lipgloss.Style {
	switch header {
	case "NAME", "ID", "WORKSPACE", "CONNECTION", "GENERATION", "CREATION", "RUNID", "SESSION":
		return accent
	case "HOST", "HOSTNAME", "IP", "REMOTE", "JUMP", "LISTEN", "LISTENER", "DESTINATION":
		return hostStyle
	case "USER", "USERNAME", "OWNER", "GROUP":
		return successStyle
	case "PORT", "LOCAL PORT", "OVERWRITE":
		return warningStyle
	case "KEY", "AGENT", "SHELL", "PROXY", "SOCKS PROXY":
		return infoStyle
	case "TERM", "TYPE", "PERMISSIONS":
		return keywordStyle
	case "TUNNELS", "MASTER PID", "OWNER PID", "SIZE", "MODIFIED", "FILES", "KNOWN TOTAL":
		return numberStyle
	case "SOCKET", "NO-TERM", "SSH CONFIG", "SSHCONFIG", "COLLECTION", "DETAIL", "SOURCE", "DESTINATION PATH":
		return secondary
	case "ERROR":
		return errorStyle
	default:
		return pageStyle
	}
}
