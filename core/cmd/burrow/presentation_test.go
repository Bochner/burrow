package main

import (
	"fmt"
	"html"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Bochner/burrow/core/connection"
	"github.com/Bochner/burrow/core/launch"
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
				fmt.Fprintf(&svg, `<rect x="%d" y="%d" width="9" height="19" fill="%s"/>`, x*9, y*19, hex(cell.Style.Bg, "#000000"))
				fmt.Fprintf(&svg, `<text x="%d" y="%d" fill="%s" font-family="DejaVu Sans Mono,monospace" font-size="14">%s</text>`, x*9, y*19+14, hex(cell.Style.Fg, "#ffffff"), html.EscapeString(cell.Content))
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
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
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

		m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
		capturePresentation(t, m, prefix+"-help-top")
		helpTop := helpBorders(m.View().Content)
		for i := 0; i < 100; i++ {
			m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		}
		capturePresentation(t, m, prefix+"-help-bottom")
		if helpBorders(m.View().Content) != helpTop {
			t.Errorf("help bounds changed: %s => %s", helpTop, helpBorders(m.View().Content))
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
		m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		keep := capturePresentation(t, m, prefix+"-quit-keep")
		assertSelected(t, m, keep, "dismiss", true)
		assertSelected(t, m, keep, "quit-leave", false)
		m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		leave := capturePresentation(t, m, prefix+"-quit-leave")
		assertSelected(t, m, leave, "dismiss", false)
		assertSelected(t, m, leave, "quit-leave", true)
		m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
		m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		if m.current().management.leave {
			t.Fatal("reopened quit did not default to Keep")
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
		m.Update(tea.KeyPressMsg{Code: 'm', Mod: tea.ModAlt})
		capturePresentation(t, m, prefix+"-palette")
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
			got := cell != nil && colorMatches(cell.Style.Bg, lipgloss.Color(blueColor))
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
func TestSemanticOutput(t *testing.T) {
	m := newUI(launch.Info{}, false)
	m.output = `{"name":"gateway","port":22,"active":true}`
	styled := m.styledOutput()
	if ansi.Strip(styled) != m.output || !strings.Contains(styled, "38;2;166;227;161") || !strings.Contains(styled, "38;2;250;179;135") {
		t.Fatal("JSON text or semantic token roles lost", styled)
	}
	m.noColor = true
	if m.styledOutput() != m.output {
		t.Fatal("NO_COLOR changed output")
	}
}

func TestNavigationPresentation(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{})
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	for i := 0; i < 9; i++ {
		p := fmt.Sprintf("/tmp/ws%d", i)
		m.paths = append(m.paths, p)
		m.workspaces[p] = &workspaceView{management: newUI(launch.Info{Workspace: p}, true), focus: "prompt"}
	}
	m.resize()
	m.Update(tea.KeyPressMsg{Code: tea.KeyF6})
	for i := 0; i < 9; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		want := "> " + filepath.Base(m.paths[m.navIndex])
		if !strings.Contains(m.View().Content, want) {
			t.Fatalf("selected workspace offscreen: %s", want)
		}
	}
	if !strings.Contains(m.View().Content, "1–4/10") {
		t.Fatal("shell footer clipped", m.View().Content)
	}
	m.activate("hovel")
	if !strings.Contains(m.View().Content, "› Hovel") {
		t.Fatal("no-color active tab missing")
	}
}

func TestCommandPalette(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, false, launch.Options{})
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m.Update(tea.PasteMsg{Content: "saved draft"})
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if m.modal != "menu" {
		t.Fatal("Ctrl+P did not open commands")
	}
	original := helpBorders(m.View().Content)
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.menuIndex != len(menuActions)-1 {
		t.Fatal("palette arrows do not wrap")
	}
	m.Update(tea.PasteMsg{Content: "meta"})
	if m.menuIndex != 0 || len(m.menuMatches()) != 1 {
		t.Fatal("filter did not reset selection")
	}
	capturePresentation(t, m, "160x40-palette-filtered")
	if helpBorders(m.View().Content) != original {
		t.Fatal("filter resized palette")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.modal != "metadata" {
		t.Fatal("filtered action dispatched incorrectly")
	}
	for i := 0; i < 100; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	bottom := m.View().Content
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.View().Content == bottom {
		t.Fatal("metadata scroll stuck beyond end")
	}
	capturePresentation(t, m, "160x40-metadata")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m.Update(tea.PasteMsg{Content: "no such command"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.modal != "menu" || !strings.Contains(ansi.Strip(m.View().Content), "No matching commands") {
		t.Fatal("empty filter dispatched")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
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
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m.Update(tea.PasteMsg{Content: "saved draft"})
	for _, size := range [][2]int{{160, 40}, {120, 30}, {100, 24}, {80, 24}, {40, 16}, {1, 1}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
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
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.Update(tea.PasteMsg{Content: " editable"})
	if m.current().management.input.Value() != "saved draft editable" {
		t.Fatal("resize discarded or blocked draft")
	}
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
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
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
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
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m.Update(tea.PasteMsg{Content: "draft"})
	m.openPalette()
	paste := func() tea.Msg { return tea.PasteMsg{Content: "meta"} }
	m.Update(m.inputCommand("menu", paste)())
	if m.palette.Value() != "meta" || m.current().management.input.Value() != "draft" {
		t.Fatal("clipboard went to wrong input")
	}
	late := m.inputCommand("menu", paste)()
	m.openPalette()
	m.Update(late)
	if m.palette.Value() != "" {
		t.Fatal("late clipboard changed reopened palette")
	}
}

func TestRefinedPresentation(t *testing.T) {
	for _, size := range [][2]int{{160, 40}, {200, 50}} {
		m := newFrame(launch.Info{Workspace: "/tmp/one"}, false, launch.Options{})
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		plain := ansi.Strip(m.View().Content)
		for _, unwanted := range []string{"This session", "connect NAME HOST", "1–1/1", "AUTH", "DESTINATION"} {
			if strings.Contains(plain, unwanted) {
				t.Fatalf("unwanted chrome: %s", unwanted)
			}
		}
		m.current().management.connections = []connection.State{{Name: "gateway", Host: "example.com", User: "operator", Port: 22, State: "connected"}, {Name: "build", Host: "build.example.com", User: "runner", Port: 2222, State: "connecting"}}
		capturePresentation(t, m, fmt.Sprintf("%dx%d-populated", size[0], size[1]))
		m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
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
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		capturePresentation(t, m, fmt.Sprintf("%dx%d-demo", size[0], size[1]))
		plain := ansi.Strip(m.View().Content)
		for _, label := range []string{"DEMO", "production", "gateway", "5432"} {
			if !strings.Contains(plain, label) {
				t.Fatal("missing sample data", label, plain)
			}
		}
	}
	m.Update(tea.PasteMsg{Content: "connect real host user --key /tmp/key"})
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || m.current().management.busy {
		t.Fatal("demo executed command")
	}
	m.openNew()
	m.destination.SetValue("/tmp/must-not-launch-demo")
	if m.submitWorkspace() != nil || m.launchPending {
		t.Fatal("demo launched workspace")
	}
}

// Project presentation contract: compare final rendered cells to semantic roles,
// not merely the palette function's return value.
func TestTerminalStatusRoles(t *testing.T) {
	for _, state := range []string{"pending", "refused", "exited"} {
		m := newFrame(launch.Info{Workspace: "/tmp/terminal"}, false, launch.Options{})
		m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
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
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
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
			m.Update(tea.MouseClickMsg{X: x, Y: rows + 2, Button: tea.MouseLeft})
			if m.modal != "metadata" {
				t.Fatal("branding displaced daemon status pointer target")
			}
			m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
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
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
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
