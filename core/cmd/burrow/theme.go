package main

import (
	"encoding/json"
	"regexp"
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
	baseColor     = "#1e1e2e"
	sidebarColor  = "#181825"
	popupColor    = "#181825"
	textColor     = "#cdd6f4"
	subtextColor  = "#a6adc8"
	borderColor   = "#6c7086"
	blueColor     = "#89b4fa"
	lavenderColor = "#b4befe"
)

var accent = lipgloss.NewStyle().Foreground(lipgloss.Color(lavenderColor)).Bold(true)
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
var tableCell = pageStyle.Padding(0, 1)

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

var commandToken = regexp.MustCompile(`\S+`)

func (m ui) syntax(line string) string {
	// Known command-reference syntax, following the accepted prototype's roles.
	return commandToken.ReplaceAllStringFunc(line, func(token string) string {
		word := strings.Trim(token, "[](),")
		style := pageStyle
		switch {
		case strings.HasPrefix(word, "--"):
			style = heading
		case strings.IndexFunc(word, unicode.IsLetter) >= 0 && strings.ToUpper(word) == word:
			style = warningStyle
		case strings.HasPrefix(word, "Ctrl") || strings.HasPrefix(word, "Alt") || word == "Tab" || word == "Enter" || word == "Esc" || word == "F6":
			style = keywordStyle
		case word == "connect" || word == "reconnect" || word == "inspect" || word == "connections" || word == "close" || word == "status" || word == "help" || word == "quit":
			style = heading
		}
		return m.paint(style, token)
	})
}

var jsonToken = regexp.MustCompile(`"(?:\\.|[^"\\])*"|\b(?:true|false|null|-?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?)\b`)

func (m ui) styledOutput() string {
	if m.noColor {
		return m.output
	}
	if !json.Valid([]byte(m.output)) {
		if strings.HasPrefix(m.output, "REFUSED:") {
			return m.paint(errorStyle, m.output)
		}
		return m.paint(pageStyle, m.output)
	}
	var b strings.Builder
	end := 0
	for _, at := range jsonToken.FindAllStringIndex(m.output, -1) {
		b.WriteString(m.output[end:at[0]])
		token := m.output[at[0]:at[1]]
		style := numberStyle
		if strings.HasPrefix(token, "\"") {
			style = successStyle
			if strings.HasPrefix(strings.TrimSpace(m.output[at[1]:]), ":") {
				style = heading
			}
		} else if token == "true" || token == "false" || token == "null" {
			style = keywordStyle
		}
		b.WriteString(m.paint(style, token))
		end = at[1]
	}
	b.WriteString(m.output[end:])
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
	case "connected", "active":
		return successStyle
	case "failed", "unverified":
		return errorStyle
	case "connecting", "reconnecting":
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
	keys := []key.Binding{binding("Ctrl+P", "menu"), binding("F1", "help"), binding("Ctrl+C", "quit")}
	if m.current().focus == "prompt" && m.current().tab == "" {
		keys = append(keys, binding("Tab", "complete"), binding("↑↓", "history"))
	} else {
		keys = append(keys, binding("↑↓", "select"), binding("Enter", "open"), binding("Esc", "prompt"))
	}
	keys = append(keys, binding("F6", "focus"))
	return solid(" "+h.ShortHelpView(keys), m.width, 1, "#11111b", m.noColor)
}

func (m ui) paletteTitle(width int) string {
	title := m.paint(accent, "Menu ")
	for _, c := range lipgloss.Blend1D(max(0, width-5), lipgloss.Color(lavenderColor), lipgloss.Color("#cba6f7")) {
		title += m.paint(lipgloss.NewStyle().Foreground(c), "╱")
	}
	return title
}

func centered(text string, width int) string {
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, ansi.Truncate(text, width, "…"))
}
func (m ui) button(label string, selected bool, width int) string {
	prefix := "  "
	style := pageStyle
	if selected {
		prefix = "› "
		style = selectedStyle
	}
	return m.paint(style, centered(prefix+label, width))
}
