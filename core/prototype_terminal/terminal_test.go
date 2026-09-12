package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestHelpSurfaceBackground(t *testing.T) {
	f, err := newFixture()
	if err != nil {
		t.Fatal(err)
	}
	defer f.close()
	m := newModel(f, true)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
	canvas := lipgloss.NewCanvas(120, 40).Compose(lipgloss.NewLayer(m.View().Content))
	wantR, wantG, wantB, _ := lipgloss.Color(draculaSurface).RGBA()
	for y := 1; y < 39; y++ {
		for x := 2; x < 118; x++ {
			cell := canvas.CellAt(x, y)
			if cell == nil || cell.Style.Bg == nil {
				t.Fatalf("help surface falls back to terminal background at (%d,%d)", x, y)
			}
			r, g, b, _ := cell.Style.Bg.RGBA()
			if r != wantR || g != wantG || b != wantB {
				t.Fatalf("help surface has a background patch at (%d,%d): %v", x, y, cell.Style.Bg)
			}
		}
	}
}

func TestPersistentWorkspaceAndModalFocus(t *testing.T) {
	f, err := newFixture()
	if err != nil {
		t.Fatal(err)
	}
	defer f.close()
	m := newModel(f, true)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	brand := strings.Split(ansi.Strip(m.sidebar(sidebarColumns, 30)), "\n")[1]
	if !strings.Contains(brand, "B U R R O W") || ansi.StringWidth(brand) != sidebarColumns {
		t.Fatal("brand must fit the sidebar box", brand)
	}
	left := strings.Index(brand, "B U R R O W")
	// The left border occupies one cell but three UTF-8 bytes.
	left = ansi.StringWidth(brand[:left])
	right := sidebarColumns - left - ansi.StringWidth("B U R R O W")
	if left-right < -1 || left-right > 1 {
		t.Fatal("brand must be centered", brand)
	}
	for _, r := range "con" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tick(time.Now()))
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "connect " {
		t.Fatalf("typing/arrow/tick/Tab: %q", m.input.Value())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "connect -ip " {
		t.Fatalf("required argument: %q", m.input.Value())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "connect -ip 192.0.2.20 " {
		t.Fatalf("selected value: %q", m.input.Value())
	}
	if !strings.Contains(m.View().Content, "-port") {
		t.Fatal("next required argument not visible")
	}
	draft := m.input.Value()
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m.Update(tea.PasteMsg{Content: "do not edit the draft"})
	if m.View().Cursor != nil || !strings.Contains(m.View().Content, "Quit Burrow?") {
		t.Fatal("quit modal did not own focus")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Quit Burrow      Keep working") {
		t.Fatal("quit action must appear to the left of keep working")
	}
	for _, code := range []rune{tea.KeyLeft, tea.KeyLeft, tea.KeyRight, tea.KeyRight} {
		m.Update(tea.KeyPressMsg{Code: code})
		if m.quitSelected != (code == tea.KeyLeft) {
			t.Fatal("quit arrows must select the action on that side")
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.modal != "" || m.f.quit || m.input.Value() != draft {
		t.Fatal("default quit choice lost work")
	}
	m.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.input.Value() != "scp " || m.f.mode == "files" {
		t.Fatal("menu must prepare, not execute, a command")
	}
	m.input.Reset()
	for i := 0; i < 6; i++ {
		c := *f.current()
		c.name = fmt.Sprintf("host-%d", i)
		f.targets = append(f.targets, &c)
	}
	view := ansi.Strip(m.View().Content)
	for i := 0; i < 6; i++ {
		if !strings.Contains(view, fmt.Sprintf("host-%d", i)) {
			t.Fatal("resource row silently capped", i)
		}
	}
	if !strings.Contains(view, "SELECTED CONNECTION") || !strings.Contains(view, "COMMAND OUTPUT") {
		t.Fatal("persistent workspace regions missing")
	}
	for _, size := range [][2]int{{160, 48}, {120, 40}, {80, 24}, {40, 12}, {20, 8}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, modal := range []string{"", "menu", "quit"} {
			m.modal = modal
			v := m.View().Content
			if len(strings.Split(v, "\n")) > size[1] {
				t.Fatalf("modal height overflow %s %v", modal, size)
			}
			for _, line := range strings.Split(v, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("modal width overflow %s %v: %q", modal, size, line)
				}
			}
		}
	}
	m.modal = "quit"
	m.quitSelected = true
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.f.quit || cmd == nil {
		t.Fatal("confirmed quit did not exit")
	}
}

func TestTerminalWorkflow(t *testing.T) {
	f, err := newFixture()
	if err != nil {
		t.Fatal(err)
	}
	defer f.close()
	do := func(line string) result {
		t.Helper()
		args, err := words(line)
		if err != nil {
			t.Fatal(err)
		}
		r := f.execute(args)
		if r.Error != "" {
			t.Fatalf("%s: %s", line, r.Error)
		}
		return r
	}
	if r := f.execute([]string{"scp"}); r.Error == "" {
		t.Fatal("disconnected file mode accepted")
	}
	do("connect")
	do("scp")
	do("cd /var/log")
	if r := do("ls"); len(r.Entries) != 2 {
		t.Fatalf("listing: %+v", r)
	}
	do("get system.log")
	for f.copy != nil {
		if err := f.advance(); err != nil {
			t.Fatal(err)
		}
	}
	src, _ := os.ReadFile(filepath.Join(f.root, "lab/var/log/system.log"))
	dst, _ := os.ReadFile(filepath.Join(f.root, "downloads/system.log"))
	if !bytes.Equal(src, dst) {
		t.Fatal("transfer changed bytes")
	}
	if r := f.execute([]string{"get", "system.log"}); r.Error == "" {
		t.Fatal("overwrite accepted")
	}
	do("get auth.log cancelled.log")
	do("cancel")
	if _, err := os.Stat(filepath.Join(f.root, "downloads/cancelled.log")); !os.IsNotExist(err) {
		t.Fatal("partial remained")
	}
	do("put notes.txt")
	for f.copy != nil {
		if err := f.advance(); err != nil {
			t.Fatal(err)
		}
	}
	do("lcd /uploads")
	do("get auth.log")
	for f.copy != nil {
		if err := f.advance(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(f.root, "uploads/auth.log")); err != nil {
		t.Fatal("lcd did not select the download destination:", err)
	}
	do("cd /etc")
	if r := do(`ls "."`); len(r.Entries) != 1 || r.Entries[0].Name != "server config.txt" {
		t.Fatalf("quoted listing %+v", r)
	}
	if _, err := within(filepath.Join(f.root, "lab"), "/", "../../etc/passwd"); err == nil {
		t.Fatal("escaped root")
	}
	if err := os.Symlink("/etc", filepath.Join(f.root, "lab/escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := within(filepath.Join(f.root, "lab"), "/", "escape/passwd"); err == nil {
		t.Fatal("symlink escaped root")
	}
	args, err := words(`put "file with spaces" 'x;$(not-a-shell)'`)
	if err != nil || !reflect.DeepEqual(args, []string{"put", "file with spaces", "x;$(not-a-shell)"}) {
		t.Fatalf("quoting: %v %v", args, err)
	}
	if _, err := words(`get "unfinished`); err == nil {
		t.Fatal("unfinished quote accepted")
	}
	do("proxy 1080")
	do("loss")
	if f.current().status != "DISCONNECTED" || len(f.current().tunnels) != 0 {
		t.Fatal("loss did not clear access")
	}
	do("connect")
	if len(f.current().tunnels) != 0 {
		t.Fatal("recreated tunnels")
	}
	m := newModel(f, false)
	m.input.SetValue("con")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "connections" {
		t.Fatalf("completion: %q", m.input.Value())
	}
	_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command != nil {
		m.Update(command())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.input.Value() != "connections" {
		t.Fatal("history missing")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.input.SetValue("unfinished command")
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.input.Value() != "unfinished command" {
		t.Fatal("history lost the draft")
	}
	m.input.SetValue("con")
	m.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "connect " {
		t.Fatalf("cannot choose alternate completion: %q", m.input.Value())
	}
	m.input.Reset()
	m.Update(tea.PasteMsg{Content: "connect\nquit"})
	if f.quit {
		t.Fatal("paste submitted a command")
	}
	m.input.Reset()
	m.offset = 3
	m.submit("clear")
	if m.offset != 0 {
		t.Fatal("clear left output scrolling paused")
	}
	for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 12}, {20, 8}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View().Content
		for _, label := range []string{"CONNECTIONS", "TUNNELS", "SHELLS"} {
			if !strings.Contains(strings.ToUpper(view), label) {
				t.Fatalf("overview lost %s at %v", label, size)
			}
		}
		if strings.Contains(view, "\x1b") {
			t.Fatal("color in plain mode")
		}
		lines := strings.Split(view, "\n")
		if len(lines) > size[1] {
			t.Fatalf("height overflow %d > %d", len(lines), size[1])
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("width overflow %q", line)
			}
		}
	}
	if strings.Contains(safe("file\x1b[2J\x07\n"), "\x1b") {
		t.Fatal("terminal controls leaked")
	}
}

func TestHelpHighlightingAndProxyOwnership(t *testing.T) {
	f, err := newFixture()
	if err != nil {
		t.Fatal(err)
	}
	defer f.close()
	m := newModel(f, true)
	m.helpTopic = "connect"
	help := m.styledHelp()
	for _, part := range []string{"REQUIRED PARAMETERS", "OPTIONAL PARAMETERS", m.paint(draculaCyan, "-port"), m.paint(draculaYellow, "PORT"), "Examples", "38;2;80;250;123"} {
		if !strings.Contains(help, part) {
			t.Fatalf("missing semantic help emphasis %q", part)
		}
	}
	if strings.Contains(commandHelp("connect"), "\x1b") {
		t.Fatal("ANSI leaked into plain help")
	}
	m.input.SetValue("preserved draft")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
	if m.View().Cursor != nil || !strings.Contains(m.View().Content, "HELP · CONNECT") || m.helpView.Width() != 112 {
		t.Fatal("help must own focus in a wide overlay")
	}
	if !strings.Contains(m.View().Content, helpBackdropStyle.Render("homelab")) {
		// Compositing can split SGR sequences; check the dim rendition itself.
		if !strings.Contains(m.View().Content, ";2m") && !strings.Contains(m.View().Content, "\x1b[2;") {
			t.Fatal("help backdrop is not dimmed")
		}
	}
	m.Update(tea.PasteMsg{Content: "unwanted paste"})
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.helpView.YOffset() == 0 {
		t.Fatal("help arrow did not scroll")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "preserved draft" || m.helpTopic != "connections" {
		t.Fatal("help navigation changed prompt")
	}
	for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 12}, {20, 8}, {12, 6}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View().Content
		if len(strings.Split(view, "\n")) > size[1] {
			t.Fatal("help overlay exceeds terminal height", size)
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("help overlay exceeds terminal width", size)
			}
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.helpOpen || m.input.Value() != "preserved draft" || m.View().Cursor == nil {
		t.Fatal("closing help must restore the prompt")
	}
	for _, line := range []string{"connect -ip 192.0.2.30 -port 22 -user ubuntu -socket proxy-host -proxy 1080", "tunc proxy-host l 8080 localhost 80", "tunc proxy-host r 9000 localhost 9001"} {
		args, _ := words(line)
		if r := m.f.execute(args); r.Error != "" {
			t.Fatal(r.Error)
		}
	}
	c := m.f.current()
	if c.proxyStatus() != "Yes :1080" || len(c.tunnels) != 2 || c.tunnels[0].id != "proxy-host/1" {
		t.Fatal("proxy consumed a tunnel identity or count")
	}
	view := ansi.Strip(m.overview(120, 40))
	if !strings.Contains(view, "PROXY") || !strings.Contains(view, "Yes :1080") {
		t.Fatal("proxy missing from connection table")
	}
	tunnels := strings.Split(view, "TUNNELS · grouped by connection")
	if len(tunnels) != 2 || strings.Contains(tunnels[1], "1080") || strings.Contains(tunnels[1], "SOCKS") {
		t.Fatal("proxy leaked into tunnel inventory")
	}
	m.f.execute([]string{"loss"})
	if c.proxyStatus() != "No" || c.proxyPort != "" || len(c.tunnels) != 0 {
		t.Fatal("lost connection retained active endpoints")
	}
}

func TestLocalShellBackground(t *testing.T) {
	f, err := newFixture()
	if err != nil {
		t.Fatal(err)
	}
	defer f.close()
	m := newModel(f, false)
	m.vtMode = true
	m.submit("connect")
	m.submit("shell")
	if len(m.shells) != 1 {
		t.Fatalf("shell did not start: %v", m.lines)
	}
	defer func() {
		for _, s := range m.shells {
			s.close()
		}
	}()
	s := m.shells[0]
	waitFor := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if check() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		tail, _, _ := s.snapshot()
		t.Fatalf("shell timeout: %q", tail)
	}
	_, err = s.pty.Write([]byte("sleep 0.1; printf 'background-%s\\n' complete\n"))
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	if m.active != -1 {
		t.Fatal("did not background")
	}
	waitFor(func() bool { tail, _, _ := s.snapshot(); return strings.Contains(tail, "background-complete") })
	m.width = 60
	m.height = 20
	m.submit("resume 1")
	if m.active != 0 {
		t.Fatal("did not resume")
	}
	_, err = s.pty.Write([]byte("stty size\n"))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(func() bool { tail, _, _ := s.snapshot(); return strings.Contains(tail, "17 60") })
	_, err = s.pty.Write([]byte("i=0; while [ $i -lt 2000 ]; do printf 'bounded fixture output 012345678901234567890123456789012345678901234567890123456789\\n'; i=$((i+1)); done; printf 'BOUND_%s\\n' FINISHED\n"))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(func() bool {
		tail, discarded, _ := s.snapshot()
		s.mu.Lock()
		screenOK := s.screen.em.ScrollbackLen() <= 128 && strings.Contains(s.screen.em.String(), "BOUND_FINISHED")
		s.mu.Unlock()
		return screenOK && discarded > 0 && len(tail) <= 65536 && strings.Contains(tail, "BOUND_FINISHED")
	})
	m.Update(tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	m.submit("shell")
	if len(m.shells) != 2 {
		t.Fatal("second shell missing")
	}
	m.Update(tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	m.submit("close --yes")
	for _, s := range m.shells {
		_, _, ended := s.snapshot()
		if !ended {
			t.Fatal("connection close left shell alive")
		}
	}
}

func TestBatchPromptDesign(t *testing.T) {
	f, err := newFixture()
	if err != nil {
		t.Fatal(err)
	}
	defer f.close()
	m := newModel(f, false)
	run := func(line string) {
		t.Helper()
		cmd := m.work(line)
		m.Update(cmd())
	}
	run("connect")
	run("scp")
	run("cd /var/log")
	run("mget *.log")
	if m.f.batch == nil || m.f.batch.state != "REVIEW" || m.f.copy != nil {
		t.Fatal("batch copied before review")
	}
	if _, err := os.Stat(filepath.Join(f.root, "downloads/auth.log")); !os.IsNotExist(err) {
		t.Fatal("review wrote file")
	}
	run("confirm")
	for m.f.copy != nil {
		m.Update(m.work("")())
	}
	if m.f.batch.state != "COMPLETE" {
		t.Fatal(m.f.progress)
	}
	for _, item := range m.f.batch.files {
		source, _ := os.ReadFile(filepath.Join(f.root, "lab/var/log", item.name))
		dest, _ := os.ReadFile(filepath.Join(f.root, "downloads", item.name))
		if !bytes.Equal(source, dest) {
			t.Fatal("batch changed bytes")
		}
	}
	// Existing destinations are individual failures, never silently overwritten.
	run("mget *.log")
	run("confirm")
	if m.f.batch.state != "PARTIAL" {
		t.Fatal("existing files reported successful")
	}
	run("mget *.log")
	run("cancel")
	if m.f.batch.state != "CANCELLED" {
		t.Fatal("review cancellation lost")
	}
	run("back")
	run("tunc lab l 8080 localhost 80")
	run("tunc lab r 9000 localhost 9001")
	if len(m.f.current().tunnels) != 2 || m.f.current().tunnels[1].kind != "R" {
		t.Fatal("forwarding directions missing")
	}
	remainingID := m.f.current().tunnels[1].id
	run("tund " + m.f.current().tunnels[0].id)
	if len(m.f.current().tunnels) != 1 || m.f.current().tunnels[0].id != remainingID {
		t.Fatal("tunnel removal changed sibling identity")
	}
	if r := m.f.execute([]string{"tund", "1"}); r.Error == "" {
		t.Fatal("ambiguous tunnel ID accepted")
	}
	run("scp")
	m.input.SetValue("put n")
	if got := m.complete(); len(got) != 1 || got[0] != "put notes.txt" {
		t.Fatal("upload completion used the remote directory", got)
	}
	m.input.Reset()
	m.input.ShowSuggestions = true
	m.input.SetValue("con")
	m.input.SetSuggestions(m.complete())
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.input.CurrentSuggestion() != "connect" {
		t.Fatal("arrow selection missing", m.input.CurrentSuggestion())
	}
	if m.input.Value() != "con" {
		t.Fatal("arrow submitted/changed draft")
	}
	if !strings.Contains(m.View().Content, "COMPLETION") {
		t.Fatal("dropdown invisible")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "connect " {
		t.Fatal("Tab did not advance the selected command to its arguments")
	}
	m.input.SetValue("unfinished draft")
	m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
	if !m.helpOpen || !strings.Contains(m.View().Content, "COMMAND REFERENCE") {
		t.Fatal("help missing")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.helpOpen || m.input.Value() != "unfinished draft" {
		t.Fatal("help lost draft")
	}
	for _, sample := range []struct{ line, want string }{
		{"connect ", "connect -ip "},
		{"connect -ip ", "connect -ip 192.0.2.10"},
		{"connect -ip example.org ", "connect -ip example.org -port "},
		{"connect -ip example.org -port 22 -user ubuntu -socket demo ", "connect -ip example.org -port 22 -user ubuntu -socket demo -proxy "},
	} {
		got := connectionSuggestions(sample.line)
		if len(got) == 0 || got[0] != sample.want {
			t.Fatalf("ordered completion %q: %v", sample.line, got)
		}
	}
	if r := m.f.execute([]string{"connect", "-ip", "example.org"}); !strings.Contains(r.Error, "-port") {
		t.Fatal("missing required argument accepted")
	}
	for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 12}, {20, 8}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, help := range []bool{false, true} {
			m.helpOpen = help
			view := m.View().Content
			if len(strings.Split(view, "\n")) > size[1] {
				t.Fatalf("height overflow %v", size)
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("width overflow %v: %q", size, line)
				}
			}
		}
	}
}
