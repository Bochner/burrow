package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

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
	do("tunnel 1080")
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
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.input.Value() != "connections" {
		t.Fatalf("completion: %q", m.input.Value())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.input.Value() != "connections" {
		t.Fatal("history missing")
	}
	for _, size := range [][2]int{{80, 24}, {40, 12}, {20, 8}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View()
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

func TestLocalShellBackground(t *testing.T) {
	f, err := newFixture()
	if err != nil {
		t.Fatal(err)
	}
	defer f.close()
	m := newModel(f, false)
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
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
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
		return discarded > 0 && len(tail) <= 65536 && strings.Contains(tail, "BOUND_FINISHED")
	})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	m.submit("shell")
	if len(m.shells) != 2 {
		t.Fatal("second shell missing")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	m.submit("close --yes")
	for _, s := range m.shells {
		_, _, ended := s.snapshot()
		if !ended {
			t.Fatal("connection close left shell alive")
		}
	}
}
