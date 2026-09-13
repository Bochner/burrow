package main

import (
	"context"
	"fmt"
	"html"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Bochner/burrow/core/connection"
	"github.com/Bochner/burrow/core/launch"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// Export the actual ANSI view through the existing pinned VT cell model. These
// artifacts reveal backgrounds and selected controls that text-only checks miss.
func capturePresentation(t *testing.T, m *frame, name string) *vt.Emulator {
	t.Helper()
	screen := vt.NewEmulator(m.width, m.height)
	t.Cleanup(func() { screen.Close() })
	content := m.View().Content
	if _, err := screen.Write([]byte(strings.ReplaceAll(content, "\n", "\r\n"))); err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("TEST_UNDECLARED_OUTPUTS_DIR"); dir != "" {
		var svg strings.Builder
		fmt.Fprintf(&svg, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`, m.width*9, m.height*19, m.width*9, m.height*19)
		for y := 0; y < m.height; y++ {
			for x := 0; x < m.width; x++ {
				cell := screen.CellAt(x, y)
				if cell == nil {
					continue
				}
				hex := func(c color.Color, fallback string) string {
					if c == nil {
						return fallback
					}
					r, g, b, _ := c.RGBA()
					return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
				}
				fg, bg := hex(cell.Style.Fg, "#ffffff"), hex(cell.Style.Bg, "#000000")
				if cell.Style.Attrs&uv.AttrReverse != 0 {
					fg, bg = bg, fg
				}
				fmt.Fprintf(&svg, `<rect x="%d" y="%d" width="9" height="19" fill="%s"/>`, x*9, y*19, bg)
				fmt.Fprintf(&svg, `<text x="%d" y="%d" fill="%s" font-family="DejaVu Sans Mono,monospace" font-size="14">%s</text>`, x*9, y*19+14, fg, html.EscapeString(cell.Content))
			}
		}
		svg.WriteString("</svg>")
		if err := os.WriteFile(filepath.Join(dir, name+".svg"), []byte(svg.String()), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return screen
}
func TestPresentation(t *testing.T) {
	for _, size := range [][2]int{{160, 40}, {200, 50}} {
		m := newFrame(launch.Info{Workspace: "/tmp/presentation-workspace", PID: 123}, false, launch.Options{})
		frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		prefix := fmt.Sprintf("%dx%d", size[0], size[1])
		screen := capturePresentation(t, m, prefix+"-management")
		left, right := m.columns()
		for _, x := range []int{left - 1, m.width - right} {
			if x >= m.width {
				continue
			}
			if screen.CellAt(x, 3).Content != "│" {
				t.Fatal("missing sidebar separator")
			}
		}
		if screen.CellAt(left+1, 3).Content != " " {
			t.Fatal("center gutter lost")
		}
		missing := 0
		for y := 0; y < m.height; y++ {
			for x := 0; x < m.width; x++ {
				cell := screen.CellAt(x, y)
				if cell == nil || cell.Style.Bg == nil {
					missing++
				}
			}
		}
		if missing > 0 {
			t.Errorf("%s: %d cells have no owned background", prefix, missing)
		}
		if strings.Contains(strings.Join(strings.Split(m.View().Content, "\n")[:4], "\n"), m.active) {
			t.Error("redundant workspace header")
		}

		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyF1})
		capturePresentation(t, m, prefix+"-help-top")
		helpTop := helpBorders(m.View().Content)
		for i := 0; i < 100; i++ {
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyDown})
		}
		capturePresentation(t, m, prefix+"-help-bottom")
		if helpBorders(m.View().Content) != helpTop {
			t.Errorf("help bounds changed: %s => %s", helpTop, helpBorders(m.View().Content))
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
		frameEvent(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		keep := capturePresentation(t, m, prefix+"-quit-keep")
		assertSelected(t, m, keep, "confirm-reject", true)
		assertSelected(t, m, keep, "confirm-accept", false)
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
		leave := capturePresentation(t, m, prefix+"-quit-leave")
		assertSelected(t, m, leave, "confirm-reject", false)
		assertSelected(t, m, leave, "confirm-accept", true)
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
		frameEvent(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		if m.form.GetFocusedField().GetValue().(bool) {
			t.Fatal("reopened quit did not default to Keep")
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
		frameEvent(m, tea.KeyPressMsg{Code: 'm', Mod: tea.ModAlt})
		capturePresentation(t, m, prefix+"-palette")
	}
}

func TestSSHRecap(t *testing.T) {
	preview := "Generated config:\n ConnectTimeout 8\n UserKnownHostsFile /dev/null\n StrictHostKeyChecking no\n"
	m := newFrame(launch.Info{Workspace: "/tmp/recap"}, false, launch.Options{})
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	m.reviewText = preview
	m.setForm("review", "Review exact target", confirmForm("Proceed?", "", "Proceed", "Cancel"))
	screen := capturePresentation(t, m, "160x40-recap-value-roles")
	for value, hex := range map[string]string{"8": "#fab387", "/dev/null": "#a6adc8", "no": "#cba6f7"} {
		found := false
		for y := 0; y < m.height; y++ {
			var line strings.Builder
			for x := 0; x < m.width; x++ {
				line.WriteString(screen.CellAt(x, y).Content)
			}
			at := strings.Index(line.String(), " "+value)
			if at < 0 {
				continue
			}
			x := ansi.StringWidth(line.String()[:at+1])
			if !colorMatches(screen.CellAt(x, y).Style.Fg, lipgloss.Color(hex)) {
				t.Fatalf("recap value %s lost semantic color %s", value, hex)
			}
			found = true
			break
		}
		if !found {
			t.Fatalf("recap value missing: %s", value)
		}
	}
	if got := (ui{noColor: true}).semanticText(preview); got != preview {
		t.Fatal("NO_COLOR changed exact config")
	}
	text := "connect gateway\nSSH command:\n'/usr/bin/ssh' '-i' '/tmp/client key' '-p' '2222'\nGenerated config:\nHost burrow-hop-0\n HostName 192.0.2.10\n User tester\n Port 2222\n IdentityFile \"/tmp/client key\"\n StrictHostKeyChecking no\n"
	styled := (ui{}).semanticText(text)
	if ansi.Strip(styled) != text {
		t.Fatal("SSH preview text changed while coloring")
	}
	command := "/usr/bin/ssh -M -S /tmp/master -o StrictHostKeyChecking=no -D 127.0.0.1:9050 -p 2222 alice@nas.example"
	colored := (ui{}).syntax(command, false)
	for _, token := range []string{keywordStyle.Render("StrictHostKeyChecking"), warningStyle.Render("9050")} {
		if !strings.Contains(colored, token) {
			t.Fatal("SSH command option lost semantic color", token)
		}
	}
	for _, part := range []string{heading.Render("HostName"), hostStyle.Render("192.0.2.10"), successStyle.Render("tester"), warningStyle.Render("2222")} {
		if !strings.Contains(styled, part) {
			t.Fatalf("missing semantic role %q", part)
		}
	}
	for _, size := range [][2]int{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
		m := newFrame(launch.Info{Workspace: "/tmp/recap", PID: 123}, false, launch.Options{})
		frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.reviewText = text + strings.Repeat(" ServerAliveInterval 2\n", 40) + "FINAL CONFIG LINE"
		m.setForm("review", "Review exact target", confirmForm("Proceed?", "", "Proceed", "Cancel"))
		for _, offset := range []int{0, 1000} {
			m.modalOffset = offset
			screen := capturePresentation(t, m, fmt.Sprintf("%dx%d-recap-%d", size[0], size[1], offset))
			if !strings.Contains(screen.String(), "Proceed?") {
				t.Fatal("long recap hid approval controls")
			}
			if offset > 0 && !strings.Contains(screen.String(), "FINAL CONFIG LINE") {
				t.Fatal("recap cannot scroll to end")
			}
		}
		m.noColor = true
		if strings.Contains(m.View().Content, "\x1b[") {
			t.Fatal("NO_COLOR recap contains ANSI")
		}
	}
}

func TestConnectionOptionCompletion(t *testing.T) {
	for _, size := range [][2]int{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
		m := newFrame(launch.Info{Workspace: "/tmp/auth-completion"}, true, launch.Options{})
		frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		frameEvent(m, tea.PasteMsg{Content: "connect gateway host user --j"})
		if !strings.Contains(ansi.Strip(m.View().Content), "COMPLETION") {
			t.Fatal("connection option completion unavailable")
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
		if got := m.current().management.input.Value(); got != "connect gateway host user --jump " {
			t.Fatalf("wrong completed option: %q", got)
		}
		capturePresentation(t, m, fmt.Sprintf("%dx%d-connect-options", size[0], size[1]))
	}
}

func helpBorders(view string) string {
	var found []string
	for y, line := range strings.Split(view, "\n") {
		if (strings.Contains(line, "╭") && strings.Contains(line, "╮")) || (strings.Contains(line, "╰") && strings.Contains(line, "╯")) {
			found = append(found, fmt.Sprint(y))
		}
	}
	return strings.Join(found, ",")
}

func assertSelected(t *testing.T, m *frame, screen *vt.Emulator, id string, want bool) {
	t.Helper()
	found := false
	compositor := m.compositor()
	for y := 0; y < m.height; y++ {
		for x := 0; x < m.width; x++ {
			if compositor.Hit(x, y).ID() != id {
				continue
			}
			found = true
			cell := screen.CellAt(x, y)
			background := blueColor
			if strings.HasPrefix(id, "profile:") || strings.HasPrefix(id, "resource:") {
				background = rowSelectionColor
			}
			got := cell != nil && colorMatches(cell.Style.Bg, lipgloss.Color(background))
			if got != want {
				t.Fatalf("%s selection background at %d,%d: got %v want %v", id, x, y, got, want)
			}
		}
	}
	if !found {
		t.Fatalf("missing native target %s", id)
	}
}
func colorMatches(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}
func TestCompletionPresentation(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 30}, {160, 40}, {200, 50}} {
		for _, plain := range []bool{false, true} {
			m := newFrame(launch.Info{Workspace: "/tmp/completion"}, plain, launch.Options{})
			defer m.terminals.close()
			frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			frameEvent(m, tea.PasteMsg{Content: "tunnel "})
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
			matches, index := m.current().management.completionOptions()
			if len(matches) != 3 || index != 0 || matches[0] != "tunnel create" || matches[1] != "tunnel check" || matches[2] != "tunnel remove" {
				t.Fatal("duplicate or missing candidates", matches, index)
			}
			screen := capturePresentation(t, m, fmt.Sprintf("completion-%dx%d-plain-%t", size[0], size[1], plain))
			bounds := m.selectionBounds()
			bounds.Min.Y = m.height - 7 // completion popup, excluding inventory prose
			if !plain {
				assertTextRole(t, screen, bounds, "tunnel create", blueColor)
				assertTextRole(t, screen, bounds, "CONNECTION", subtextColor)
			} else if strings.Contains(m.View().Content, "\x1b") {
				t.Fatal("NO_COLOR completion leaked ANSI")
			}
			// Polling may update inventory, but cannot collapse the active cycle.
			m.updateManagement(m.active, connectionList{})
			m.updateManagement(m.active, profilesReady{})
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
			if m.current().management.input.Value() != "tunnel check" {
				t.Fatal("refresh reset cycle")
			}
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
			m.current().management.input.Reset()
			frameEvent(m, tea.PasteMsg{Content: "connect "})
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
			if m.current().management.input.Value() != "connect -ip " {
				t.Fatal("editing did not reset command cycle", m.current().management.input.Value())
			}
		}
	}
}

func TestCommandRecommendationScope(t *testing.T) {
	m := newUI(launch.Info{}, true)
	for _, command := range []string{"connections", "profiles", "shells", "tunnel list"} {
		m.input.SetValue(command)
		for _, suggestion := range m.input.MatchedSuggestions() {
			if suggestion == command {
				t.Fatalf("initial recommendations bypass filter: %s", command)
			}
		}
		for _, suggestion := range m.suggestions() {
			if suggestion == command {
				t.Fatalf("dashboard inventory promoted in TUI: %s", command)
			}
		}
		if err := connection.ValidateCommand("/tmp/recommendations", strings.Fields(command)); err != nil {
			t.Fatalf("command removed instead of recommendation: %s: %v", command, err)
		}
	}
	if !strings.Contains(completionDescription("tunnel check gateway/id"), "connectivity") {
		t.Fatal("active check has no diagnostic description")
	}
}

func TestForwardArgumentGuidance(t *testing.T) {
	for _, size := range []image.Point{{80, 24}, {120, 30}, {160, 40}, {200, 50}} {
		for _, plain := range []bool{false, true} {
			for _, prefix := range []string{"tunnel create gateway forward", "tunnel create gateway reverse", "tunc gateway l", "tunc gateway r"} {
				m := newFrame(launch.Info{Workspace: "/tmp/guidance"}, plain, launch.Options{})
				defer m.terminals.close()
				frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
				for _, suffix := range []string{"", " ", " 8080 ", " 8080 localhost "} {
					m.current().management.input.Reset()
					frameEvent(m, tea.PasteMsg{Content: prefix + suffix})
					screen := capturePresentation(t, m, fmt.Sprintf("forward-guidance-%dx%d-%t-%s-%d", size.X, size.Y, plain, strings.ReplaceAll(prefix, " ", "-"), len(suffix)))
					for _, text := range []string{"LISTEN HOST PORT", "Example:", "8080", "localhost"} {
						if !strings.Contains(screen.String(), text) {
							t.Fatalf("guidance missing %q for %q at %v", text, prefix+suffix, size)
						}
					}
					if plain && strings.Contains(m.View().Content, "\x1b") {
						t.Fatal("NO_COLOR guidance leaked ANSI")
					}
					if !plain {
						assertTextRole(t, screen, image.Rect(0, 0, size.X, size.Y-3), "8080", "#f9e2af")
					}
					before := m.current().management.input.Value()
					frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
					if m.current().management.input.Value() != before {
						t.Fatal("non-selectable example changed command")
					}
				}
			}
		}
	}
}

func TestSemanticOutput(t *testing.T) {
	m := newUI(launch.Info{}, false)
	m.output = `{"name":"gateway","port":22,"count":-2.5e-3,"active":true,"missing":null,"items":[false],"session":"session-one","detail":"Master verified","error":"refused"}`
	styled := m.styledOutput()
	if ansi.Strip(styled) != m.output || !strings.Contains(styled, "38;2;180;190;254") || !strings.Contains(styled, "38;2;250;179;135") || !strings.Contains(styled, "38;2;249;226;175") {
		t.Fatal("JSON text or semantic token roles lost", styled)
	}
	screen := vt.NewEmulator(200, 2)
	defer screen.Close()
	if _, err := screen.Write([]byte(styled)); err != nil {
		t.Fatal(err)
	}
	for _, role := range []struct{ text, color string }{{"{", subtextColor}, {"}", subtextColor}, {"[", subtextColor}, {"]", subtextColor}, {":", subtextColor}, {",", subtextColor}, {"-2.5e-3", "#fab387"}, {"true", "#cba6f7"}, {"false", "#cba6f7"}, {"null", "#cba6f7"}, {`"session-one"`, lavenderColor}, {`"Master verified"`, subtextColor}, {`"refused"`, "#f38ba8"}} {
		assertTextRole(t, screen, image.Rect(0, 0, 200, 2), role.text, role.color)
	}
	m.noColor = true
	if m.styledOutput() != m.output {
		t.Fatal("NO_COLOR changed output")
	}
}

func TestForwardJSONRoles(t *testing.T) {
	m := newUI(launch.Info{}, false)
	m.output = `{"direction":"R","listen":"127.0.0.1:32451","destination":"[::1]:3000","requestedListen":"127.0.0.1:0"}`
	styled := m.styledOutput()
	if ansi.Strip(styled) != m.output {
		t.Fatal("endpoint styling changed JSON")
	}
	screen := vt.NewEmulator(160, 2)
	defer screen.Close()
	if _, err := screen.Write([]byte(styled)); err != nil {
		t.Fatal(err)
	}
	for _, port := range []string{"32451", "3000", "0\"}"} {
		assertTextRole(t, screen, image.Rect(0, 0, 160, 2), port, "#f9e2af")
	}
	m.noColor = true
	if m.styledOutput() != m.output {
		t.Fatal("NO_COLOR changed endpoint JSON")
	}
}

func TestConciseSSHRecap(t *testing.T) {
	result, err := connection.Execute(context.Background(), "/tmp/recap", []string{"connect", "gateway", "nas.example", "alice", "--ssh-config", "/dev/null", "-proxy", "1080"})
	if err != nil {
		t.Fatal(err)
	}
	review := result.(map[string]string)["review"]
	for _, plain := range []bool{false, true} {
		for _, size := range [][2]int{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
			m := newFrame(launch.Info{Workspace: "/tmp/recap"}, plain, launch.Options{})
			defer m.terminals.close()
			frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.reviewText = review
			m.setForm("review", "Review SSH connection", confirmForm("Proceed?", "", "Proceed", "Cancel"))
			bounds := m.dialogBounds()
			wantWidth := min(max(76, lipgloss.Width(review)+6), size[0]-4)
			if bounds.Dx() != wantWidth {
				t.Fatal("recap did not expand to its command", bounds, wantWidth)
			}
			screen := capturePresentation(t, m, fmt.Sprintf("ssh-command-%dx%d-plain-%t", size[0], size[1], plain))
			if !strings.Contains(screen.String(), "Proceed?") || strings.Contains(screen.String(), "Generated config:") {
				t.Fatal("recap lost controls or retained config dump")
			}
			if plain {
				if strings.Contains(m.View().Content, "\x1b") {
					t.Fatal("NO_COLOR recap leaked ANSI")
				}
			} else {
				for _, role := range []struct{ text, color string }{{"/usr/bin/ssh", blueColor}, {"-M", blueColor}, {"StrictHostKeyChecking", "#cba6f7"}, {"1080", "#f9e2af"}} {
					assertTextRole(t, screen, bounds, role.text, role.color)
				}
			}
			aligned := false
			for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
				if screen.CellAt(bounds.Min.X+3, y).Content == "/" {
					aligned = true
					break
				}
			}
			if !aligned {
				t.Fatal("SSH command is not left aligned")
			}
			m.modalOffset = 1000
			if m.dialogBounds() != bounds {
				t.Fatal("scroll changed recap geometry")
			}
		}
	}
}

func TestSOCKSTables(t *testing.T) {
	m := newUI(launch.Info{}, false)
	m.height = 40
	m.profiles.Profiles = []connection.Profile{{Name: "gateway", Host: "nas.example", User: "alice", Port: 22, Jump: "bastion", ProxyPort: 1080}}
	m.connections = []connection.State{{Name: "gateway", Host: "nas.example", User: "alice", Port: 22, Generation: "owner", State: "connected", ProxyPort: 1080}}
	for _, table := range []string{m.savedConnections(160), m.activeConnections(160)} {
		if !strings.Contains(ansi.Strip(table), "1080") || strings.Contains(ansi.Strip(table), "bastion") {
			t.Fatal("SOCKS port missing or confused with jump host", ansi.Strip(table))
		}
	}
	lines := strings.Split(ansi.Strip(m.activeConnections(160)), "\n")
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 8 || fields[4] != "1080" || fields[len(fields)-2] != "0" {
		t.Fatal("proxy counted as a tunnel", lines)
	}
	m.width = 160
	m.tunnelError = ""
	view := ansi.Strip(m.View().Content)
	if strings.Contains(view, "SOCKS") || strings.Contains(view, "/socks") || !strings.Contains(view, "No forwards") {
		t.Fatal("proxy listed under tunnels", view)
	}
	for _, size := range []image.Point{{80, 24}, {120, 30}, {160, 40}, {200, 50}} {
		for _, plain := range []bool{false, true} {
			f := newFrame(launch.Info{Workspace: "/tmp/proxy-tables"}, plain, launch.Options{})
			defer f.terminals.close()
			frameEvent(f, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			capturePresentation(t, f, fmt.Sprintf("proxy-empty-%dx%d-%t", size.X, size.Y, plain))
			f.current().management.connections = m.connections
			f.current().management.profiles = m.profiles
			capturePresentation(t, f, fmt.Sprintf("proxy-active-%dx%d-%t", size.X, size.Y, plain))
		}
	}
	m.connections[0].State = "lost"
	if strings.Contains(ansi.Strip(m.activeConnections(160)), "1080") {
		t.Fatal("lost proxy shown as live")
	}
	m.connectionError = "unverified"
	if !strings.Contains(ansi.Strip(m.activeConnections(160)), "unverified") {
		t.Fatal("unknown inventory reported as empty")
	}
}

func TestLocalForwardPresentation(t *testing.T) {
	for _, size := range []image.Point{{80, 24}, {120, 30}, {160, 40}, {200, 50}} {
		for _, plain := range []bool{false, true} {
			m := newFrame(launch.Info{Workspace: "/tmp/forward-ui"}, plain, launch.Options{})
			defer m.terminals.close()
			frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			u := &m.current().management
			if !strings.Contains(ansi.Strip(u.localForwards(size.X)), "UNVERIFIED") {
				t.Fatal("unobserved inventory reported as empty")
			}
			frameEvent(m, tunnelList{})
			capturePresentation(t, m, fmt.Sprintf("forward-empty-%dx%d-%t", size.X, size.Y, plain))
			for i := 0; i < 12; i++ {
				direction := "L"
				if i%2 != 0 {
					direction = "R"
				}
				u.tunnels = append(u.tunnels, connection.Tunnel{ID: fmt.Sprintf("gateway/%032x", i+1), Connection: "gateway", Direction: direction, Listen: fmt.Sprintf("127.0.0.1:%d", 8000+i), Destination: "nas.example:80", State: "listening"})
			}
			frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			screen := capturePresentation(t, m, fmt.Sprintf("forward-populated-%dx%d-%t", size.X, size.Y, plain))
			if !strings.Contains(screen.String(), "TUNNELS") {
				t.Fatal("tunnel section missing")
			}
			if plain && strings.Contains(m.View().Content, "\x1b") {
				t.Fatal("NO_COLOR forwarding leaked ANSI")
			}
			if !plain && size.X == 200 {
				if !strings.Contains(screen.String(), "Reverse") || !strings.Contains(screen.String(), "Remote") || !strings.Contains(screen.String(), "DESTINATION") {
					t.Fatal("reverse endpoint semantics missing", screen.String())
				}
				for _, role := range []struct{ text, color string }{{"gateway", lavenderColor}, {"Local", "#cba6f7"}, {"127.0.0.1", "#f5c2e7"}, {"8000", "#f9e2af"}, {"listening", "#a6e3a1"}, {"1–3", "#fab387"}, {"Alt+Shift+↑↓", "#cba6f7"}} {
					assertTextRole(t, screen, m.selectionBounds(), role.text, role.color)
				}
			}
			u.input.SetValue("tunnel remove ")
			u.input.SetSuggestions(u.suggestions())
			for i := 0; i < 12; i++ {
				u.cycleCompletion(false)
			}
			if u.input.Value() != "tunnel remove gateway/0000000000000000000000000000000c" {
				t.Fatal("overflow ID completion", u.input.Value())
			}
			capturePresentation(t, m, fmt.Sprintf("forward-completion-%dx%d-%t", size.X, size.Y, plain))
			u.input.Reset()
			for i := 0; i < 12; i++ {
				frameEvent(m, tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModAlt | tea.ModShift})
			}
			if !strings.Contains(ansi.Strip(u.localForwards(160)), "8011") {
				t.Fatal("tunnel overflow cannot scroll")
			}
			u.tunnels[11].Destination = "host\x1b]52;c;UNTRUSTED\x07"
			if strings.Contains(u.localForwards(160), "\x1b]52;") {
				t.Fatal("remote control sequence escaped renderer")
			}
			m.reviewText = "Create reverse forward\nConnection: gateway\nRemote listener: 127.0.0.1:8080\nLocal destination: nas.example:80"
			m.setForm("review", "Review reverse forward", confirmForm("Proceed?", "", "Proceed", "Cancel"))
			capturePresentation(t, m, fmt.Sprintf("forward-review-%dx%d-%t", size.X, size.Y, plain))
			bounds := m.dialogBounds()
			m.modalOffset = 100
			if m.dialogBounds() != bounds {
				t.Fatal("forward recap geometry changed with scroll")
			}
		}
	}
}

func TestTunnelConnectionCompletion(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/tunnel-completion"}, true, launch.Options{})
	defer m.terminals.close()
	frameEvent(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	frameEvent(m, connectionList{states: []connection.State{
		{Name: "gateway", State: "connected", Generation: "g"},
		{Name: "closed-host", State: "closed", Generation: "g"},
		{Name: "lost-host", State: "lost", Generation: "g"},
	}})
	u := &m.current().management
	for _, c := range []struct{ prefix, want string }{{"tunnel create ", "tunnel create gateway forward "}, {"tunc ", "tunc gateway l "}, {"tunnel create gateway r", "tunnel create gateway reverse "}, {"tunc gateway r", "tunc gateway r "}} {
		u.input.SetValue(c.prefix)
		u.input.CursorEnd()
		u.input.SetSuggestions(u.suggestions())
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
		if u.input.Value() != c.want {
			t.Fatalf("completion: %q, want %q", u.input.Value(), c.want)
		}
	}
	frameEvent(m, connectionList{err: fmt.Errorf("owner unavailable")})
	u.input.SetValue("tunc ")
	for _, suggestion := range u.suggestions() {
		if strings.HasPrefix(suggestion, "tunc gateway") {
			t.Fatal("failed observation offered stale connection")
		}
	}
}

func TestFinishedConnectionColors(t *testing.T) {
	for _, plain := range []bool{false, true} {
		m := newFrame(launch.Info{Workspace: "/tmp/result-colors"}, plain, launch.Options{})
		defer m.terminals.close()
		frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
		m.attempt = &authAttempt{path: m.active}
		frameEvent(m, authFinished{attempt: m.attempt, result: connection.State{Generation: "generation-one", Name: "gateway", Host: "192.0.2.50", User: "alice", Port: 2222, State: "connected", Socket: "/tmp/ssh.sock", Session: "session-one", OwnerPID: 4321, MasterPID: 5432, SocketInode: 6543, Detail: "Master verified"}})
		screen := capturePresentation(t, m, fmt.Sprintf("connect-result-plain-%t", plain))
		if !plain {
			for _, role := range []struct{ text, color string }{{"{", subtextColor}, {`"generation-one"`, lavenderColor}, {`"gateway"`, lavenderColor}, {`"192.0.2.50"`, "#f5c2e7"}, {`"alice"`, "#a6e3a1"}, {"2222", "#f9e2af"}, {`"connected"`, "#a6e3a1"}} {
				assertTextRole(t, screen, m.selectionBounds(), role.text, role.color)
			}
		} else if strings.Contains(m.View().Content, "\x1b") {
			t.Fatal("NO_COLOR result leaked ANSI")
		}
		for _, size := range [][2]int{{200, 50}, {120, 30}, {80, 24}} {
			frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			screen = capturePresentation(t, m, fmt.Sprintf("connect-result-%dx%d-plain-%t", size[0], size[1], plain))
			if !plain {
				assertTextRole(t, screen, m.selectionBounds(), "{", subtextColor)
			}
		}
	}
}

func TestNavigationPresentation(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{})
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	for i := 0; i < 9; i++ {
		p := fmt.Sprintf("/tmp/ws%d", i)
		m.paths = append(m.paths, p)
		m.workspaces[p] = &workspaceView{management: newUI(launch.Info{Workspace: p}, true), focus: "prompt"}
	}
	m.resize()
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyF6})
	for i := 0; i < 9; i++ {
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyDown})
		want := "›◌ " + filepath.Base(m.paths[m.navIndex])
		if !strings.Contains(m.View().Content, want) {
			t.Fatalf("selected workspace offscreen: %s", want)
		}
	}
	if strings.Contains(m.View().Content, "/10") || !strings.Contains(m.View().Content, "SHELLS - SSH") {
		t.Fatal("workspace tree hid entries behind a summary", m.View().Content)
	}
	m.activate("hovel")
	if !strings.Contains(m.View().Content, "› Hovel") {
		t.Fatal("no-color active tab missing")
	}
}

func TestCommandPalette(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, false, launch.Options{})
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	frameEvent(m, tea.PasteMsg{Content: "saved draft"})
	frameEvent(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if m.modal != "menu" {
		t.Fatal("Ctrl+P did not open commands")
	}
	original := helpBorders(m.View().Content)
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if func() bool { v, _ := m.menu.Hovered(); return v != len(menuActions)-1 }() {
		t.Fatal("palette arrows do not wrap")
	}
	frameEvent(m, tea.PasteMsg{Content: "meta"})
	if func() bool { v, _ := m.menu.Hovered(); return v != 1 }() {
		t.Fatal("filter did not reset selection")
	}
	capturePresentation(t, m, "160x40-palette-filtered")
	if helpBorders(m.View().Content) != original {
		t.Fatal("filter resized palette")
	}
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.modal != "metadata" {
		t.Fatal("filtered action dispatched incorrectly")
	}
	for i := 0; i < 100; i++ {
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	bottom := m.View().Content
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.View().Content == bottom {
		t.Fatal("metadata scroll stuck beyond end")
	}
	capturePresentation(t, m, "160x40-metadata")
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	frameEvent(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	frameEvent(m, tea.PasteMsg{Content: "no such command"})
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.modal != "menu" || !strings.Contains(ansi.Strip(m.View().Content), "No matching commands") {
		t.Fatal("empty filter dispatched")
	}
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.current().management.input.Value() != "saved draft" {
		t.Fatal("palette changed prompt")
	}
	screen := capturePresentation(t, m, "160x40-command-footer")
	for x := 0; x < m.width; x++ {
		if !colorMatches(screen.CellAt(x, m.height-1).Style.Bg, lipgloss.Color("#11111b")) {
			t.Fatal("footer background missing")
		}
	}
	if !strings.Contains(ansi.Strip(m.commandHelp()), "Ctrl+P menu") {
		t.Fatal("footer missing command shortcut")
	}
}

func TestShrinkingCenter(t *testing.T) {
	m := newDemoFrame(true)
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	frameEvent(m, tea.PasteMsg{Content: "saved draft"})
	for _, size := range [][2]int{{160, 40}, {120, 30}, {100, 24}, {80, 24}, {40, 16}, {1, 1}} {
		frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		plain := ansi.Strip(m.View().Content)
		if strings.Contains(plain, "Resize window") || strings.Contains(plain, "Minimum") {
			t.Fatal("resize gate returned", plain)
		}
		if size[0] >= 80 && (!strings.Contains(plain, "SAVED CONNECTIONS") || !strings.Contains(plain, "ACTIVE SSH CONNECTIONS")) {
			t.Fatal("center hidden", plain)
		}
		if size[0] >= 80 && m.View().Cursor == nil {
			t.Fatal("visible prompt blocked")
		}
		for _, line := range strings.Split(plain, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("overflow", line)
			}
		}
		capturePresentation(t, m, fmt.Sprintf("%dx%d-shrink", size[0], size[1]))
	}
	frameEvent(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	frameEvent(m, tea.PasteMsg{Content: " editable"})
	if m.current().management.input.Value() != "saved draft editable" {
		t.Fatal("resize discarded or blocked draft")
	}
	frameEvent(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if !strings.Contains(ansi.Strip(m.View().Content), "Type to filter") {
		t.Fatal("menu blocked below 160x40")
	}
}

func TestTableCentering(t *testing.T) {
	u := newUI(launch.Info{}, true)
	lines := strings.Split(ansi.Strip(u.dataTable("SECTION", []string{"NAME", "PORT"}, [][]string{{"node", "22"}}, 40)), "\n")
	if lines[0] != "SECTION" {
		t.Fatal("section title moved", lines[0])
	}
	for _, item := range []struct {
		row    int
		value  string
		center int
	}{{1, "NAME", 20}, {1, "PORT", 60}, {3, "node", 20}, {3, "22", 60}} {
		at := strings.Index(lines[item.row], item.value)
		if at < 0 || absInt(2*at+len(item.value)-item.center) > 1 {
			t.Fatal("cell not centered", lines)
		}
	}
	m := newDemoFrame(true)
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	selected := strings.Split(m.View().Content, "\n")
	m.current().selected = ""
	unselected := strings.Split(m.View().Content, "\n")
	for y, line := range selected {
		if strings.Contains(line, "›") && strings.Contains(line, "gateway") {
			if strings.Replace(strings.Split(line, "│")[1], "›", " ", 1) != strings.Split(unselected[y], "│")[1] {
				t.Fatal("selection shifted cells", line, unselected[y])
			}
			return
		}
	}
	t.Fatal("missing selected row")
}

func TestPaletteClipboardOrigin(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{})
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	frameEvent(m, tea.PasteMsg{Content: "draft"})
	m.openPalette()
	paste := func() tea.Msg { return tea.PasteMsg{Content: "meta"} }
	frameEvent(m, formCommand(m.active, m.inputEpoch, paste)())
	if !strings.Contains(m.form.View(), "meta") || m.current().management.input.Value() != "draft" {
		t.Fatal("clipboard went to wrong input")
	}
	late := formCommand(m.active, m.inputEpoch, paste)()
	m.openPalette()
	frameEvent(m, late)
	if strings.Contains(m.form.View(), "meta") {
		t.Fatal("late clipboard changed reopened palette")
	}
}

func TestRefinedPresentation(t *testing.T) {
	for _, size := range [][2]int{{160, 40}, {200, 50}} {
		m := newFrame(launch.Info{Workspace: "/tmp/one"}, false, launch.Options{})
		frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		plain := ansi.Strip(m.View().Content)
		for _, unwanted := range []string{"This session", "connect NAME HOST", "1–1/1", "AUTH", "DESTINATION"} {
			if strings.Contains(plain, unwanted) {
				t.Fatalf("unwanted chrome: %s", unwanted)
			}
		}
		m.current().management.connections = []connection.State{{Name: "gateway", Host: "example.com", User: "operator", Port: 22, State: "connected"}, {Name: "build", Host: "build.example.com", User: "runner", Port: 2222, State: "connecting"}}
		capturePresentation(t, m, fmt.Sprintf("%dx%d-populated", size[0], size[1]))
		frameEvent(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		lines := strings.Split(ansi.Strip(m.View().Content), "\n")
		for _, line := range lines {
			if at := strings.Index(line, "Quit Burrow?"); at >= 0 {
				left := ansi.StringWidth(line[:at])
				if absInt(2*left+12-m.width) > 1 {
					t.Fatal("quit title not centered", line)
				}
			}
		}
	}
}
func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func TestDemoPreview(t *testing.T) {
	m := newDemoFrame(false)
	if m.Init() != nil || m.check(m.active) != nil {
		t.Fatal("demo started I/O")
	}
	for _, size := range [][2]int{{160, 40}, {200, 50}} {
		frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		capturePresentation(t, m, fmt.Sprintf("%dx%d-demo", size[0], size[1]))
		plain := ansi.Strip(m.View().Content)
		for _, label := range []string{"DEMO", "production", "gateway", "5432"} {
			if !strings.Contains(plain, label) {
				t.Fatal("missing sample data", label, plain)
			}
		}
	}
	frameEvent(m, tea.PasteMsg{Content: "connect real host user --key /tmp/key"})
	_, cmd := frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || m.current().management.busy {
		t.Fatal("demo executed command")
	}
	m.openNew()
	frameEvent(m, tea.PasteMsg{Content: "/tmp/must-not-launch-demo"})
	if m.submitWorkspace() != nil || m.launchPending {
		t.Fatal("demo launched workspace")
	}
}

// Project presentation contract: compare final rendered cells to semantic roles,
// not merely the palette function's return value.
func assertTextRole(t *testing.T, screen *vt.Emulator, bounds image.Rectangle, value, hex string) {
	t.Helper()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		var line strings.Builder
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			line.WriteString(screen.CellAt(x, y).Content)
		}
		at := strings.Index(line.String(), value)
		if at < 0 {
			continue
		}
		x := bounds.Min.X + ansi.StringWidth(line.String()[:at])
		if !colorMatches(screen.CellAt(x, y).Style.Fg, lipgloss.Color(hex)) {
			t.Fatalf("%q lost semantic color %s", value, hex)
		}
		return
	}
	t.Fatalf("required text missing: %q", value)
}

func TestConnectionRecapColors(t *testing.T) {
	for _, operation := range []string{"connect", "close"} {
		for _, size := range [][2]int{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
			m := newFrame(launch.Info{Workspace: "/tmp/recap"}, false, launch.Options{})
			frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			review := "connect gateway\nEndpoint: alice@nas.example:2222\nSSH config: /tmp/config\nJump: bob@bastion:2200,192.0.2.1\nKey: /home/alice/.ssh/id_ed25519\nAgent: none"
			expect := map[string]string{"connect": blueColor, "gateway": lavenderColor, "Endpoint:": lavenderColor, "alice": "#a6e3a1", "nas.example": "#f5c2e7", "2222": "#f9e2af", "192.0.2.1": "#f5c2e7", "bob": "#a6e3a1", "bastion": "#f5c2e7", "2200": "#f9e2af", "Key:": lavenderColor, "/home/alice/.ssh/id_ed25519": "#94e2d5"}
			if operation == "close" {
				review = "Close NAS (alice@192.0.2.2:2222), state connected, master PID 123, socket /tmp/master. Ends all owned connection access; saved settings and artifacts remain. Repeat close NAS --yes to confirm."
				expect = map[string]string{"Close": blueColor, "NAS": lavenderColor, "alice": "#a6e3a1", "192.0.2.2": "#f5c2e7", "2222": "#f9e2af", "connected": "#a6e3a1", "123": "#fab387", "--yes": blueColor}
			}
			m.modal, m.commandArgs = "review", []string{operation, "gateway"}
			frameEvent(m, m.dispatch(m.active, func() tea.Msg {
				return commandReview{epoch: m.inputEpoch, args: m.commandArgs, review: review}
			})())
			screen := capturePresentation(t, m, fmt.Sprintf("recap-%s-%dx%d", operation, size[0], size[1]))
			bounds := m.dialogBounds()
			for value, hex := range expect {
				assertTextRole(t, screen, bounds, value, hex)
			}
			colored := ansi.Strip(m.View().Content)
			m.noColor, m.current().management.noColor = true, true
			plain := m.View().Content
			if plain != ansi.Strip(plain) || strings.Join(strings.Fields(plain), " ") != strings.Join(strings.Fields(colored), " ") || bounds != m.dialogBounds() {
				t.Fatal("NO_COLOR changed recap text/layout or leaked colors")
			}
		}
	}
}

func TestSharedFormAndHelpRoles(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/colors"}, false, launch.Options{})
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	m.setForm("profile-save", "Save settings", saveProfileForm("gateway", "/tmp/collection.json"))
	screen := capturePresentation(t, m, "shared-form-colors")
	assertTextRole(t, screen, m.dialogBounds(), "Collection:", lavenderColor)
	assertTextRole(t, screen, m.dialogBounds(), "/tmp/collection.json", subtextColor)
	assertTextRole(t, screen, m.dialogBounds(), "Enter", "#cba6f7")
	m.dismissForm()
	m.current().management.help = true
	screen = capturePresentation(t, m, "help-command-colors")
	bounds := image.Rect(35, 7, 125, 33)
	for value, hex := range map[string]string{"profile": blueColor, "create": blueColor, "NAME": "#f9e2af", "--as": blueColor} {
		assertTextRole(t, screen, bounds, value, hex)
	}
	u := &m.current().management
	v := u.helpViewport(m.width, m.height)
	for i, line := range strings.Split(ansi.Wrap(u.helpText(), v.Width(), ""), "\n") {
		if strings.Contains(line, "--key PATH") {
			u.helpOffset = i
			break
		}
	}
	screen = capturePresentation(t, m, "help-placeholder-colors")
	assertTextRole(t, screen, bounds, "PATH", "#f9e2af")
	assertTextRole(t, screen, bounds, "[USER@]HOST[:PORT][,...]", "#f9e2af")
	m.current().management.helpOffset = 1000
	screen = capturePresentation(t, m, "help-keybinding-colors")
	assertTextRole(t, screen, bounds, "Shift+F6", "#cba6f7")
	assertTextRole(t, screen, bounds, "PgUp/PgDn", "#cba6f7")
	m.noColor, m.current().management.noColor = true, true
	plain := m.View().Content
	if plain != ansi.Strip(plain) || !strings.Contains(plain, "PgUp/PgDn") {
		t.Fatal("NO_COLOR help lost text or leaked colors")
	}
}

func TestTerminalStatusRoles(t *testing.T) {
	for _, state := range []string{"pending", "refused", "exited"} {
		m := newFrame(launch.Info{Workspace: "/tmp/terminal"}, false, launch.Options{})
		frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
		m.current().tab = "hovel"
		m.current().cli = &cliTab{pending: state == "pending"}
		label, hex := "opening / closing", "#f9e2af"
		if state == "refused" {
			m.current().cli.error = "REFUSED: unavailable"
			label, hex = "REFUSED", "#f38ba8"
		}
		if state == "exited" {
			m.current().cli.screen.Exited = true
			label, hex = "CLI: exited", "#f38ba8"
		}
		screen := capturePresentation(t, m, "terminal-status-"+state)
		r := m.terminalBounds()
		rows := []int{m.height - 2}
		if state == "refused" {
			rows = append(rows, r.Min.Y)
		}
		for _, y := range rows {
			if !colorMatches(screen.CellAt(r.Min.X, y).Style.Fg, lipgloss.Color(hex)) {
				t.Fatalf("%s lost semantic color at row %d", label, y)
			}
		}
		m.noColor, m.current().management.noColor = true, true
		content := m.View().Content
		if ansi.Strip(content) != content || !strings.Contains(content, label) {
			t.Fatal("NO_COLOR lost terminal status or leaked styles")
		}
	}
}

func TestSidebarBrand(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 30}, {160, 40}, {200, 50}} {
		for _, demo := range []bool{false, true} {
			m := newFrame(launch.Info{Workspace: "/tmp/brand"}, false, launch.Options{})
			if demo {
				m = newDemoFrame(false)
			}
			frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			screen := capturePresentation(t, m, fmt.Sprintf("brand-%dx%d-demo-%t", size[0], size[1], demo))
			_, right := m.columns()
			x := m.width - right + 2
			want, rows := "BURROW", 1
			if size[0] >= 160 {
				want, rows = burrowWordmark, 6
			}
			for y, line := range strings.Split(want, "\n") {
				for offset, char := range []rune(line) {
					cell := screen.CellAt(x+offset, y+1)
					if cell.Content != string(char) || !colorMatches(cell.Style.Fg, lipgloss.Color(lavenderColor)) {
						t.Fatalf("brand cell changed at %d,%d: %+v", x+offset, y+1, cell)
					}
				}
			}
			frameEvent(m, tea.MouseClickMsg{X: x, Y: rows + 2, Button: tea.MouseLeft})
			if m.modal != "metadata" {
				t.Fatal("branding displaced daemon status pointer target")
			}
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
			m.noColor, m.current().management.noColor = true, true
			content := m.View().Content
			if content != ansi.Strip(content) || !strings.Contains(content, strings.Split(want, "\n")[0]) {
				t.Fatal("NO_COLOR lost branding or leaked styles")
			}
		}
	}
}

func TestTableAndMetadataRoles(t *testing.T) {
	m := newDemoFrame(false)
	m.current().selected = ""
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	screen := capturePresentation(t, m, "160x40-semantic-tables")
	expect := map[string]string{"production": "#b4befe", "10.20.0.10": "#f5c2e7", "operator": "#a6e3a1", "2222": "#f9e2af", "id_ed25519": "#94e2d5", "48.6 MiB": "#fab387"}
	for value, hex := range expect {
		found := false
		for y := 0; y < m.height; y++ {
			var line strings.Builder
			for x := 0; x < m.width; x++ {
				line.WriteString(screen.CellAt(x, y).Content)
			}
			at := strings.Index(line.String(), value)
			if at < 0 {
				continue
			}
			x := ansi.StringWidth(line.String()[:at])
			if !colorMatches(screen.CellAt(x, y).Style.Fg, lipgloss.Color(hex)) {
				t.Fatalf("%s lost semantic color %s", value, hex)
			}
			found = true
			break
		}
		if !found {
			t.Fatalf("required field/value missing: %s", value)
		}
	}
	live := newFrame(launch.Info{Workspace: "/tmp/live"}, true, launch.Options{})
	live.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	if !strings.Contains(live.metadata(), "Files: Unavailable") || strings.Contains(live.metadata(), "Files: 0") {
		t.Fatal("unavailable downloads misreported")
	}
	live.current().management.connectionObserved = true
	if !strings.Contains(live.metadata(), "DISCONNECTED") {
		t.Fatal("empty observed snapshot not disconnected")
	}
	live.current().management.connectionError = "owner unavailable"
	if !strings.Contains(live.metadata(), "UNVERIFIED") || !strings.Contains(live.metadata(), "Active in workspace: Unknown") {
		t.Fatal("failed observation presented as disconnected or a measured zero")
	}
	live.current().management.connections = []connection.State{{Name: "existing", State: "connected"}}
	if !strings.Contains(live.metadata(), "Active in workspace: Unknown") {
		t.Fatal("failed refresh advertises stale connection count")
	}
	if !strings.Contains(live.metadata(), "Check duration: Unavailable") || strings.Contains(live.metadata(), "Latency") {
		t.Fatal("unmeasured verification duration presented as latency")
	}
	m.noColor = true
	m.current().management.noColor = true
	if ansi.Strip(m.View().Content) != m.View().Content {
		t.Fatal("NO_COLOR leaked styles")
	}
}

func TestSavedProfilesPresentation(t *testing.T) {
	for _, size := range [][2]int{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
		for _, plain := range []bool{false, true} {
			m := newFrame(launch.Info{Workspace: "/tmp/profile-view"}, plain, launch.Options{})
			frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			prefix := fmt.Sprintf("%dx%d-profiles-%t", size[0], size[1], plain)
			capturePresentation(t, m, prefix+"-empty")
			list := connection.Collection{Path: "/tmp/homelab.json", Revision: strings.Repeat("a", 64), Profiles: []connection.Profile{{Name: "nas", Host: "192.168.1.20", User: "alice", Port: 2222, Key: "/home/alice/.ssh/key", Jump: "bastion"}, {Name: "router", Host: "192.168.1.1", User: "admin", Port: 22}}}
			m.updateManagement(m.active, profilesReady{collection: list})
			before := capturePresentation(t, m, prefix+"-populated")
			if !strings.Contains(ansi.Strip(m.View().Content), "SAVED CONNECTIONS") {
				t.Fatal("missing saved table")
			}
			// Actual row layers and selection retain field alignment and never run I/O.
			m.activate("profile:0")
			if m.current().management.selectedProfile != "nas" {
				t.Fatal("row did not select")
			}
			screen := capturePresentation(t, m, prefix+"-selected")
			assertSelected(t, m, screen, "profile:0", !plain)
			compositor := m.compositor()
			for y := 0; y < m.height; y++ {
				for x := m.selectionBounds().Min.X + 1; x < m.selectionBounds().Max.X; x++ {
					if compositor.Hit(x, y).ID() == "profile:0" {
						a, b := before.CellAt(x, y), screen.CellAt(x, y)
						if a.Content != b.Content || !colorMatches(a.Style.Fg, b.Style.Fg) {
							t.Fatalf("selection changed token at %d,%d", x, y)
						}
					}
				}
			}
			if !strings.Contains(ansi.Strip(m.View().Content), "›") {
				t.Fatal("selection marker missing")
			}
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
			capturePresentation(t, m, prefix+"-actions")
			if m.modal != "profile-menu" {
				t.Fatal("saved actions unavailable")
			}
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
			if got := m.current().management.input.Value(); got != "profile connect nas" {
				t.Fatalf("action did not prepare explicit connect: %q", got)
			}
			m.current().management.input.Reset()
			frameEvent(m, tea.PasteMsg{Content: "profile connect rou"})
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
			if got := m.current().management.input.Value(); got != "profile connect router" {
				t.Fatalf("profile completion: %q", got)
			}
			m.setForm("profile-save", "Connected · save for a future session?", saveProfileForm("nas", "/tmp/homelab.json"))
			capturePresentation(t, m, prefix+"-save")
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
			if m.modal != "" {
				t.Fatal("skip save did not return to management")
			}
			if plain && strings.Contains(m.View().Content, "\x1b") {
				t.Fatal("NO_COLOR leaked escapes")
			}
		}
	}
}

func TestSidebarStatusAndClickAway(t *testing.T) {
	for _, plain := range []bool{false, true} {
		m := newFrame(launch.Info{Workspace: "/tmp/polish"}, plain, launch.Options{})
		defer m.terminals.close()
		frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
		w := m.current()
		w.shell = &cliTab{id: "1", connection: "gateway"}
		w.shells = []*cliTab{w.shell}
		w.management.connectionObserved = true
		w.management.profiles.Profiles = []connection.Profile{{Name: "saved", Host: "example.com", User: "alice", Port: 22}}
		for _, test := range []struct{ state, dot, color string }{{"connected", "●", "#a6e3a1"}, {"connecting", "◐", "#f9e2af"}, {"lost", "○", "#f38ba8"}, {"unverified", "◌", "#f38ba8"}} {
			w.management.connections = []connection.State{{Name: "gateway", State: test.state}}
			screen := capturePresentation(t, m, fmt.Sprintf("sidebar-%s-%t", test.state, plain))
			if !plain {
				assertTextRole(t, screen, image.Rect(0, 3, 26, 4), test.dot, test.color)
				assertTextRole(t, screen, image.Rect(0, 25, 26, 26), test.dot, test.color)
			}
			if w.connectionState("") != test.state || w.connectionState("gateway") != test.state {
				t.Fatal("incorrect observed status", test.state)
			}
		}
		w.management.connectionError = "offline"
		if w.connectionState("") != "unverified" {
			t.Fatal("stale connection remained green")
		}
		w.management.connectionError = ""
		for _, id := range []string{"profile:0", "resource:0"} {
			m.activate(id)
			before := capturePresentation(t, m, "clear-"+strings.ReplaceAll(id, ":", "-")+fmt.Sprint(plain))
			assertSelected(t, m, before, id, !plain)
			r := m.selectionBounds()
			frameEvent(m, tea.MouseClickMsg{X: r.Min.X + 1, Y: r.Max.Y - 2, Button: tea.MouseLeft})
			if w.selected != "" || w.management.selectedProfile != "" || w.shell == nil {
				t.Fatal("click-away did not clear rows or destroyed shell")
			}
			m.activate(id)
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
			if w.selected != "" || w.management.selectedProfile != "" {
				t.Fatal("escape left row selected")
			}
		}
		m.activate("shell:1")
		selectedTab := capturePresentation(t, m, fmt.Sprintf("ssh-tab-%t", plain))
		left, _ := m.columns()
		if !plain && !colorMatches(selectedTab.CellAt(left+21, 1).Style.Bg, lipgloss.Color(rowSelectionColor)) {
			t.Fatal("SSH tab has no selected background")
		}
		if w.tab != "shell" || !strings.Contains(ansi.Strip(m.View().Content), "› Shell #1") || strings.Contains(ansi.Strip(m.View().Content), "› Burrow") {
			t.Fatal("SSH tab not exclusively selected")
		}
		w.focus = "tabs"
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyRight})
		if w.tab != "" {
			t.Fatal("SSH missing from tab navigation")
		}
		other := "/tmp/other-shell"
		m.paths = append(m.paths, other)
		m.workspaces[other] = &workspaceView{management: newUI(launch.Info{Workspace: other}, plain), shell: &cliTab{connection: "other"}}
		m.workspaces[other].shells = []*cliTab{m.workspaces[other].shell}
		w.focus, m.shellIndex = "shells", 3
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.active != other || m.current().tab != "shell" {
			t.Fatal("shell navigation opened wrong workspace")
		}
	}
}

func TestSaveOfferWaitsForWorkspaceAndDialog(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/first"}, true, launch.Options{})
	first := m.active
	m.workspaces["/tmp/second"] = &workspaceView{management: newUI(launch.Info{Workspace: "/tmp/second"}, true)}
	m.active = "/tmp/second"
	message := m.dispatch(first, func() tea.Msg {
		return saveOffered{name: "nas", offer: true, collection: connection.Collection{Path: "/tmp/homelab.json"}}
	})()
	m.Update(message)
	if m.modal != "" || len(m.workspaces[first].saveOffers) != 1 {
		t.Fatal("save offer lost or shown in wrong workspace")
	}
	m.active, m.modal = first, "menu"
	if m.showSaveOffer() != nil || len(m.current().saveOffers) != 1 {
		t.Fatal("save offer interrupted another dialog")
	}
	m.modal = ""
	m.showSaveOffer()
	if m.modal != "profile-save" || m.saveName != "nas" || len(m.current().saveOffers) != 0 {
		t.Fatal("pending save offer was not presented")
	}
}
