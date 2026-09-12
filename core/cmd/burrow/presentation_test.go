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
	for _, size := range [][2]int{{80, 24}, {160, 40}} {
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
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
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
	if !strings.Contains(m.View().Content, "1–2/10 · wheel") {
		t.Fatal("shell footer clipped", m.View().Content)
	}
	m.activate("hovel")
	if !strings.Contains(m.View().Content, "› Hovel") {
		t.Fatal("no-color active tab missing")
	}
}

func TestCommandPalette(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, false, launch.Options{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
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
	capturePresentation(t, m, "80x24-palette-filtered")
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
	capturePresentation(t, m, "80x24-metadata")
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
	screen := capturePresentation(t, m, "80x24-command-footer")
	for x := 0; x < m.width; x++ {
		if !colorMatches(screen.CellAt(x, m.height-1).Style.Bg, lipgloss.Color("#11111b")) {
			t.Fatal("footer background missing")
		}
	}
	if !strings.Contains(ansi.Strip(m.commandHelp()), "Ctrl+P commands") {
		t.Fatal("footer missing command shortcut")
	}
}

func TestNarrowRecovery(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(tea.PasteMsg{Content: "status"})
	m.Update(tea.WindowSizeMsg{Width: 24, Height: 24})
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || m.current().management.busy || m.View().Cursor != nil {
		t.Fatal("hidden prompt remained active")
	}
	m.Update(tea.WindowSizeMsg{Width: 32, Height: 24})
	content := m.View().Content
	if !strings.Contains(content, "[Workspaces]") || !strings.Contains(content, "Hovel: unknown") {
		t.Fatal("narrow controls overlap", content)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	if !strings.Contains(m.View().Content, "1–1/1 · wheel") {
		t.Fatal("short sidebar footer clipped")
	}
	if m.current().management.input.Value() != "status" {
		t.Fatal("resize lost draft")
	}
}

func TestPaletteClipboardOrigin(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
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
