package main

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const (
	sidebarColumns    = 30
	columnGap         = 2
	sidebarBreakpoint = 110
)

var layoutStyle = lipgloss.NewStyle()
var brandStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(draculaPurple)).
	Background(lipgloss.Color(draculaSurface)).
	Bold(true).Align(lipgloss.Center).
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color(draculaPurple)).
	BorderBackground(lipgloss.Color(draculaSurface))

func (m *model) render() string {
	if m.f.quit {
		return ""
	}
	w, h := max(12, m.width), max(6, m.height)
	if m.active >= 0 {
		if m.vtMode {
			return m.paint(draculaCyan, "Attaching local shell · Ctrl-] returns to management")
		}
		tail, discarded, _ := m.shells[m.active].snapshot()
		return m.paint(draculaPurple, fmt.Sprintf("LOCAL shell %d", m.active+1)) + "\n" + fitLines(safe(tail), w, h-3, 0) + "\n" + ansi.Truncate(fmt.Sprintf("Ctrl-] management · discarded %d bytes", discarded), w, "…")
	}
	fullWidth := w
	sidebarWidth := 0
	if w >= sidebarBreakpoint {
		sidebarWidth = sidebarColumns
		w -= sidebarWidth + columnGap
	}
	c := m.f.current()
	if h < 8 {
		out := m.overview(w, h) + "\n" + fitLines(strings.Join(m.lines, "\n"), w, h-4, m.offset) + "\n╰─ " + m.input.View()
		if m.helpOpen {
			out = m.helpOverlay(out)
		}
		return out
	}
	header := m.paint(draculaCyan, "homelab") + m.paint(draculaComment, " / SSH workspace · LOCAL FIXTURE")
	if sidebarWidth == 0 {
		brand := m.paint(draculaPurple, "Burrow")
		header = ansi.Truncate(header, max(1, fullWidth-9), "…")
		header = lipgloss.JoinHorizontal(lipgloss.Top,
			layoutStyle.Width(fullWidth-ansi.StringWidth(brand)).Render(header), brand)
	}
	gap := 0
	if h >= 18 {
		gap = 1
	}
	contentHeight := h - 4 - 2*gap
	context := "homelab › " + c.name + " › " + m.f.mode + fmt.Sprintf(" · SHELLS %d", len(m.shells))
	if m.f.mode == "files" {
		context = c.name + ":" + c.cwd + "  ↓ " + m.f.download + "  ↑ " + m.f.local
	}
	status := m.keyHelp.ShortHelpView([]key.Binding{keys.menu, keys.help, m.input.KeyMap.AcceptSuggestion, keys.quit})
	if m.busy {
		status = m.spinner.View() + " Working… prompt remains editable"
	}
	if m.offset > 0 {
		status = "OUTPUT PAUSED · PgDn follows · " + status
	}
	if m.helpOpen {
		status = "HELP · ↑↓ / PgUp/PgDn scroll · Tab topic · Esc closes"
	}
	footer := m.paint(draculaComment, ansi.Truncate(status, fullWidth, "…")) + "\n" + m.paint(draculaPurple, ansi.Truncate("╭─ "+safe(context), fullWidth, "…")) + "\n" + m.paint(draculaCyan, "╰─ ") + m.input.View()
	overview := m.overview(w, h)
	if m.f.mode == "files" {
		overview = m.overview(w, 12)
	}
	resourceLines := strings.Split(overview, "\n")
	resourceHeight := min(len(resourceLines), max(3, contentHeight-gap-2))
	if len(resourceLines) > resourceHeight {
		visible := resourceHeight - 1
		start := min(m.resourceOffset, len(resourceLines)-visible)
		overview = strings.Join(resourceLines[start:start+visible], "\n") + "\n" + m.paint(draculaComment, ansi.Truncate(fmt.Sprintf("Resources %d–%d/%d · Ctrl+↑↓ scroll", start+1, start+visible, len(resourceLines)), w, "…"))
	}
	available := max(1, contentHeight-gap-resourceHeight)
	var sections []string
	remaining := available
	if m.f.batch != nil || m.f.copy != nil {
		section := m.transferView(w, min(remaining-2, 10))
		sections = append(sections, section, "")
		remaining -= strings.Count(section, "\n") + 2
	}
	if m.f.mode == "files" && m.listing != nil && remaining >= 5 {
		rows := make([][]string, 0, len(m.listing))
		for _, e := range m.listing {
			rows = append(rows, []string{safe(e.Name), humanSize(e.Size), e.Mode, e.Modified})
		}
		section := m.resourceTable("FILES · "+m.listingTitle, []string{"NAME", "SIZE", "PERMISSIONS", "MODIFIED"}, rows, w, max(1, min(6, remaining-4)))
		sections = append(sections, section, "")
		remaining -= strings.Count(section, "\n") + 2
	}
	if remaining > 0 {
		output := make([]string, len(m.lines))
		for i, line := range m.lines {
			color := draculaForeground
			switch {
			case strings.HasPrefix(line, "ERROR:"):
				color = draculaRed
			case strings.HasPrefix(line, "❯ "):
				color = draculaPink
			case strings.Contains(line, "COMPLETE"):
				color = draculaGreen
			}
			output[i] = m.paint(color, line)
		}
		if len(m.history) == 0 && remaining >= 9 {
			// Keep the startup guide by the prompt, leaving the space above
			// it available for command output instead of a blank lower screen.
			for len(output) < remaining-9 {
				output = append(output, "")
			}
			output = append(output, "", m.paint(draculaPurple, "YOUR WORKFLOW"),
				m.paint(draculaCyan, "  connect")+"   establish access to the selected host",
				m.paint(draculaCyan, "  shell")+"     open a shell · Ctrl-] returns here",
				m.paint(draculaCyan, "  tunc")+"      forward a port through a connection",
				m.paint(draculaCyan, "  scp")+"       browse files · mget reviews a batch",
				"", m.paint(draculaComment, "Ctrl+K opens the command menu · F1 shows examples"))
		}
		if remaining >= 3 {
			sections = append(sections, m.paint(draculaCyan, "COMMAND OUTPUT ")+m.paint(draculaComment, strings.Repeat("─", max(0, w-15))))
			remaining--
		}
		sections = append(sections, fitLines(strings.Join(output, "\n"), w, remaining, m.offset))
	}
	body := fitTop(strings.Join(sections, "\n"), w, available)
	content := lipgloss.JoinVertical(lipgloss.Left, layoutStyle.MarginBottom(gap).Render(overview), body)
	if sidebarWidth > 0 {
		content = lipgloss.JoinHorizontal(lipgloss.Top,
			layoutStyle.Width(w).MarginRight(columnGap).Render(content), m.sidebar(sidebarWidth, contentHeight))
	}
	out := lipgloss.JoinVertical(lipgloss.Left,
		layoutStyle.MarginBottom(gap).Render(fitTop(header, fullWidth, 1)),
		layoutStyle.MarginBottom(gap).Render(fitTop(content, fullWidth, contentHeight)), footer)
	if dropdown := m.completionView(min(w, 76), min(7, h-5)); dropdown != "" {
		out = overlay(out, dropdown, 0, max(0, h-3-lipgloss.Height(dropdown)))
	}
	if m.helpOpen {
		out = m.helpOverlay(out)
	}
	if m.modal != "" {
		out = m.modalView(out, fullWidth, h)
	}
	return out
}

func (m *model) completionView(w, limit int) string {
	matches := m.input.MatchedSuggestions()
	if !m.input.ShowSuggestions || m.input.Value() == "" || len(matches) == 0 || limit < 2 || m.helpOpen || m.modal != "" {
		return ""
	}
	selected := m.input.CurrentSuggestionIndex()
	start := max(0, selected-(limit-2))
	lines := []string{m.paint(draculaComment, "COMPLETION · ↑↓ select · Tab accept · Esc dismiss")}
	for i := start; i < min(len(matches), start+limit-1); i++ {
		prefix, color := "  ", draculaForeground
		if i == selected {
			prefix, color = "› ", draculaCyan
		}
		text := matches[i]
		if at := strings.LastIndex(m.input.Value(), " "); at >= 0 {
			text = strings.TrimPrefix(text, m.input.Value()[:at+1])
		}
		for _, option := range connectionOptions {
			if strings.HasSuffix(text, option.flag+" ") || strings.HasSuffix(text, option.flag) {
				kind := "optional"
				if option.required {
					kind = "required"
				}
				text += "  " + kind + " · " + option.description
			}
		}
		row := m.paint(color, ansi.Truncate(prefix+text, w-4, "…"))
		if i == selected {
			row = selectedStyle.Width(w - 4).Render(ansi.Truncate(prefix+text, w-4, "…"))
		}
		lines = append(lines, row)
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], w-4, "…")
	}
	return dialogStyle.Padding(0, 1).Width(w).Render(strings.Join(lines, "\n"))
}

func (m *model) overview(w, h int) string {
	var saved, active, tunnels [][]string
	for i, c := range m.f.targets {
		name := "  " + c.name
		if i == m.f.selected {
			name = "› " + c.name
		}
		port, shell := c.port, c.shell
		if port == "" {
			port = "22"
		}
		if shell == "" {
			shell = "default"
		}
		saved = append(saved, []string{name, c.host, c.user, port, c.auth, shell})
		if c.status == "CONNECTED" {
			active = append(active, []string{name, c.host, c.user, port, c.proxyStatus(), "CONNECTED", fmt.Sprint(len(c.tunnels))})
			for _, t := range c.tunnels {
				tunnels = append(tunnels, []string{c.name, t.id, t.kind, t.listen, t.destination})
			}
		}
	}
	if h < 18 || w < 60 {
		return ansi.Truncate(fmt.Sprintf("CONNECTIONS %d · › %s %s", len(active), m.f.current().name, m.f.current().status), w, "…") + "\n" +
			ansi.Truncate(fmt.Sprintf("TUNNELS %d · simulated · tunnels for detail", len(tunnels)), w, "…") + "\n" +
			ansi.Truncate(fmt.Sprintf("SHELLS %d · local · sessions for detail", len(m.shells)), w, "…")
	}
	var sections []string
	sections = append(sections, m.resourceTable("SAVED CONNECTION CONFIGURATIONS", []string{"NAME", "HOST", "USER", "PORT", "AUTH", "SHELL"}, saved, w, max(1, len(saved))))
	if len(active) == 0 {
		active = [][]string{{"None", "", "", "", "No", "connect to start", ""}}
	}
	sections = append(sections, m.resourceTable("ACTIVE SSH CONNECTIONS · simulated", []string{"NAME", "HOST", "USER", "PORT", "PROXY", "STATE", "TUNNELS"}, active, w, max(1, len(active))))
	if len(tunnels) == 0 {
		tunnels = [][]string{{"None", "", "", "", ""}}
	}
	sections = append(sections, m.resourceTable("TUNNELS · grouped by connection", []string{"CONNECTION", "ID", "TYPE", "LISTEN", "DESTINATION"}, tunnels, w, max(1, len(tunnels))))
	shells := fmt.Sprintf("SHELLS · local  %d", len(m.shells))
	for i, s := range m.shells {
		_, _, ended := s.snapshot()
		state := "background"
		if ended {
			state = "ended"
		}
		shells += fmt.Sprintf("   #%d %s %s", i+1, s.target, state)
	}
	if h >= 32 {
		sections = append(sections, m.paint(draculaCyan, ansi.Truncate(shells, w, "…")))
	}
	return strings.Join(sections, "\n\n")
}

func (m *model) transferView(w, limit int) string {
	if b := m.f.batch; b != nil {
		completed, total, done := b.counts()
		color := draculaCyan
		if b.state == "COMPLETE" {
			color = draculaGreen
		}
		if b.state == "PARTIAL" || b.state == "CANCELLED" {
			color = draculaYellow
		}
		lines := []string{m.paint(color, fmt.Sprintf("BATCH DOWNLOAD · %s · %d/%d files · ↓ %s", b.state, completed, len(b.files), safe(b.dest)))}
		if b.state == "REVIEW" {
			lines = append(lines, fmt.Sprintf("%s total · confirm starts · cancel returns · no files written yet", humanSize(total)))
		} else {
			elapsed := b.updated.Sub(b.started)
			lines = append(lines, m.progressLine(done, total, elapsed, b.state == "RUNNING"))
		}
		maxRows := max(1, limit-3)
		start := max(0, min(b.index-1, len(b.files)-maxRows))
		for _, f := range b.files[start:min(len(b.files), start+maxRows)] {
			row := fmt.Sprintf("  %-10s %s  %s / %s", f.state, safe(f.name), humanSize(f.done), humanSize(f.size))
			if f.state == "RUNNING" {
				fraction := float64(f.done) / float64(max(int64(1), f.size))
				row += "  " + m.bar.ViewAs(fraction)
			}
			if f.detail != "" {
				row += " · " + safe(f.detail)
			}
			lines = append(lines, row)
		}
		if len(b.files) > maxRows {
			lines = append(lines, fmt.Sprintf("  %d files total · current file stays visible", len(b.files)))
		}
		for i := range lines {
			lines[i] = ansi.Truncate(lines[i], w, "…")
		}
		return strings.Join(lines, "\n")
	}
	t := m.f.copy
	if t == nil {
		return ""
	}
	return m.paint(draculaCyan, "FILE TRANSFER · cancel stops copying") + "\n" + ansi.Truncate(m.progressLine(t.done, t.total, time.Since(t.started), true), w, "…")
}

func (m *model) progressLine(done, total int64, elapsed time.Duration, running bool) string {
	fraction := float64(done) / float64(max(int64(1), total))
	rate, eta := "—", "—"
	if elapsed > 0 && done > 0 && (!running || elapsed >= 500*time.Millisecond) {
		speed := float64(done) / elapsed.Seconds()
		rate = humanSize(int64(speed)) + "/s"
		if running {
			remaining := time.Duration(float64(max(int64(0), total-done)) / speed * float64(time.Second))
			eta = remaining.Round(time.Second).String()
			if remaining > 0 && remaining < time.Second {
				eta = "<1s"
			}
		}
	}
	return fmt.Sprintf("%s %3.0f%% · %s / %s\n  %s avg · elapsed %s · ETA %s", m.bar.ViewAs(fraction), fraction*100, humanSize(done), humanSize(total), rate, elapsed.Round(100*time.Millisecond), eta)
}

func fitTop(s string, w, h int) string {
	if h <= 0 {
		return ""
	}
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

// Crush's fixed sidebar branding and modal focus are interaction references;
// these views are Burrow-owned, using the Dracula palette and existing primitives.
func (m *model) sidebar(w, h int) string {
	c := m.f.current()
	connections, tunnels := 0, 0
	for _, c := range m.f.targets {
		if c.status == "CONNECTED" {
			connections++
		}
		tunnels += len(c.tunnels)
	}
	stateColor := draculaGreen
	if c.status != "CONNECTED" {
		stateColor = draculaYellow
	}
	lines := []string{
		brandStyle.Width(w).Render("B U R R O W"), "",
		m.paint(draculaPurple, "WORKSPACE"), m.paint(draculaForeground, "homelab"),
		m.paint(draculaComment, "Local design fixture"), "",
		m.paint(draculaPurple, "SELECTED CONNECTION"), m.paint(draculaCyan, safe(c.name)),
		m.paint(draculaGreen, safe(c.user)) + m.paint(draculaComment, "@") + m.paint(draculaPurple, safe(c.host)),
		m.paint(stateColor, "● "+c.status), "",
		m.paint(draculaPurple, "RESOURCES"),
		fmt.Sprintf("%s connections   %s tunnels", m.paint(draculaGreen, fmt.Sprint(connections)), m.paint(draculaYellow, fmt.Sprint(tunnels))),
		fmt.Sprintf("%s local shells", m.paint(draculaCyan, fmt.Sprint(len(m.shells)))), "",
		m.paint(draculaPurple, "ACTIVITY"), m.paint(draculaComment, "Ready · updates on change"),
	}
	if m.busy {
		lines[len(lines)-1] = m.spinner.View() + " Working…"
	}
	if b := m.f.batch; b != nil {
		lines = append(lines, m.paint(draculaCyan, "Batch · "+b.state))
	}
	return fitTop(strings.Join(lines, "\n"), w, h)
}

var menuItems = []struct{ title, command, description string }{
	{"Connect to host", "connect ", "Host → port → user → name"},
	{"Browse files", "scp ", "Choose a connected host"},
	{"Create tunnel", "tunc ", "Connection, direction, endpoints"},
	{"Collect files", "mget ", "Review matching files before download"},
	{"Shell sessions", "sessions", "Inspect background shells"},
	{"Transfer results", "transfers", "Inspect every file outcome"},
	{"Command reference", "help", "Browse commands and examples"},
	{"Quit Burrow", "quit", "Confirm before leaving"},
}

func (m *model) updateModal(v tea.KeyPressMsg) tea.Cmd {
	if m.quitPending {
		return nil
	}
	switch v.String() {
	case "esc":
		m.modal = ""
		return nil
	case "left", "right", "tab", "shift+tab", "up", "down":
		if m.modal == "quit" {
			switch v.String() {
			case "left":
				m.quitSelected = true
			case "right":
				m.quitSelected = false
			default:
				m.quitSelected = !m.quitSelected
			}
		} else {
			delta := 1
			if v.String() == "up" || v.String() == "shift+tab" {
				delta = -1
			}
			m.menuIndex = (m.menuIndex + delta + len(menuItems)) % len(menuItems)
		}
	case "ctrl+c":
		if m.modal != "quit" {
			m.modal = "quit"
			m.quitSelected = false
			return nil
		}
		return m.confirmQuit()
	case "enter":
		if m.modal == "quit" {
			if m.quitSelected {
				return m.confirmQuit()
			}
			m.modal = ""
			return nil
		}
		command := menuItems[m.menuIndex].command
		if command == "quit" {
			m.modal = "quit"
			m.quitSelected = false
			return nil
		}
		m.modal = ""
		if command == "help" {
			m.helpOpen = true
			m.helpView.SetContent(m.styledHelp())
			m.helpView.GotoTop()
			return nil
		}
		m.input.SetValue(command)
		m.input.CursorEnd()
		m.input.ShowSuggestions = true
		m.input.SetSuggestions(m.complete())
	}
	return nil
}

func (m *model) confirmQuit() tea.Cmd {
	if m.busy {
		m.quitPending = true
		return nil
	}
	m.f.quit = true
	return tea.Quit
}

func overlay(base, popup string, x, y int) string {
	// Nested ANSI resets leave text and padding cells without a background.
	// Resolve those cells on the popup's surface before layering it over the
	// workspace; keep explicit backgrounds such as the selected action intact.
	layer := lipgloss.NewLayer(popup)
	canvas := lipgloss.NewCanvas(layer.Width(), layer.Height()).Compose(layer)
	for row := 0; row < canvas.Height(); row++ {
		for col := 0; col < canvas.Width(); col++ {
			if cell := canvas.CellAt(col, row); cell != nil && cell.Style.Bg == nil {
				filled := *cell
				filled.Style.Bg = lipgloss.Color(draculaSurface)
				canvas.SetCell(col, row, &filled)
			}
		}
	}
	popup = canvas.Render()
	return lipgloss.NewCompositor(lipgloss.NewLayer(base), lipgloss.NewLayer(popup).X(x).Y(y).Z(1)).Render()
}

func (m *model) modalView(base string, w, h int) string {
	width := min(64, w-2)
	if h <= 10 && m.modal == "quit" {
		choice := "  Quit › Keep"
		if m.quitSelected {
			choice = "› Quit   Keep"
		}
		popup := dialogStyle.Align(lipgloss.Center).Padding(0, 1).Width(width).Render(m.paint(draculaPurple, "Quit Burrow?") + "\n" + m.paint(draculaCyan, choice) + "\n" + ansi.Truncate("Tab · Enter · Esc", max(1, width-4), ""))
		return overlay(base, popup, max(0, (w-lipgloss.Width(popup))/2), max(0, (h-lipgloss.Height(popup))/2))
	}
	var lines []string
	if m.modal == "quit" {
		lines = []string{m.paint(draculaPurple, "Quit Burrow?"), "", "Local fixture shells will stop.", "Temporary fixture files will be removed.", ""}
		stay, leave := layoutStyle.Padding(0, 2).Render("Keep working"), layoutStyle.Padding(0, 2).Render("Quit Burrow")
		if m.quitSelected {
			leave = selectedStyle.Render(leave)
		} else {
			stay = selectedStyle.Render(stay)
		}
		buttons := lipgloss.JoinHorizontal(lipgloss.Top, layoutStyle.MarginRight(columnGap).Render(leave), stay)
		lines = append(lines, buttons, "", m.paint(draculaComment, "←→ / Tab choose · Enter confirm · Esc cancel"))
		if m.quitPending {
			lines = append(lines, "Finishing current file operation before exit…")
		}
	} else {
		lines = []string{m.paint(draculaPurple, "BURROW COMMAND MENU"), m.paint(draculaComment, "Choose a command to prepare in the prompt"), ""}
		start := max(0, m.menuIndex-max(1, h-11)+1)
		for i := start; i < min(len(menuItems), start+max(1, h-11)); i++ {
			text := "  " + menuItems[i].title
			if width >= 58 {
				text = fmt.Sprintf("  %-20s %s", menuItems[i].title, menuItems[i].description)
			}
			text = ansi.Truncate(text, width-6, "…")
			if i == m.menuIndex {
				text = selectedStyle.Width(width - 6).Render("›" + strings.TrimPrefix(text, " "))
			}
			lines = append(lines, text)
		}
		lines = append(lines, "", m.paint(draculaComment, "↑↓ choose · Enter select · Esc cancel"))
	}
	if h < 14 && m.modal == "quit" {
		lines = []string{m.paint(draculaPurple, "Quit Burrow?"), "Stops local shells and removes scratch files.", lines[5], "Tab choose · Enter confirm · Esc cancel"}
	}
	style := dialogStyle.Width(width).Padding(1, 2)
	if m.modal == "quit" {
		style = style.Align(lipgloss.Center)
	}
	popup := style.Render(fitTop(strings.Join(lines, "\n"), max(1, width-6), min(len(lines), max(1, h-6))))
	return overlay(base, popup, max(0, (w-lipgloss.Width(popup))/2), max(0, (h-lipgloss.Height(popup))/2))
}
