package main

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"fmt"
	"github.com/Bochner/burrow/core/connection"
	"github.com/Bochner/burrow/core/launch"
	ptyhost "github.com/Bochner/burrow/core/terminal"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceFrame(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{Offline: true})
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	view := m.View().Content
	for _, label := range []string{"WORKSPACES", "New", "Menu", "Hovel", "SAVED CONNECTION", "No shells"} {
		if !strings.Contains(view, label) {
			t.Fatalf("missing %s: %s", label, view)
		}
	}
	frameEvent(m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModAlt})
	if !strings.Contains(m.View().Content, "Exact destination") {
		t.Fatal("New must show destination before launch")
	}
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
}

func TestEmbeddedTerminalKeepsFrameAndBackgroundOutput(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{})
	defer m.terminals.close()
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	frameEvent(m, tea.PasteMsg{Content: "saved draft"})
	r := m.terminalBounds()
	h, err := ptyhost.Start(context.Background(), exec.Command("/bin/sh", "-c", "stty -echo; printf ready; read line; printf '\\033[2J\\033[Hbackground-result\\033[3;4H'; read line"), r.Dx(), r.Dy())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	tab := &cliTab{host: h}
	m.current().cli = tab
	m.activate("hovel")
	refresh := func(needle string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			frameEvent(m, m.readCLI(m.active, tab)())
			if strings.Contains(tab.screen.Screen, needle) {
				return
			}
		}
		t.Fatal(tab.screen)
	}
	refresh("ready")
	frameEvent(m, tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	if m.current().focus != "tabs" {
		t.Fatal("missing frame escape")
	}
	m.activate("burrow")
	h.Send("continue")
	h.Send(uv.KeyPressEvent{Code: uv.KeyEnter})
	refresh("background-result")
	if !strings.Contains(m.View().Content, "saved draft") {
		t.Fatal("background output replaced overview draft")
	}
	m.activate("hovel")
	for _, size := range []image.Point{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
		frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
		frameEvent(m, m.readCLI(m.active, tab)())
		view := m.View()
		if !strings.Contains(view.Content, "background-result") || !strings.Contains(view.Content, "[New]") || !strings.Contains(view.Content, "[Menu]") || !strings.Contains(view.Content, "WORKSPACES") {
			t.Fatal(view.Content)
		}
		r := m.terminalBounds()
		if view.Cursor == nil || !image.Pt(view.Cursor.Position.X, view.Cursor.Position.Y).In(r) {
			t.Fatalf("cursor must stay in pane: %+v %+v", view.Cursor, r)
		}
		frameEvent(m, tea.MouseClickMsg{X: 2, Y: size.Y / 2, Button: tea.MouseLeft})
		if m.modal != "new" {
			t.Fatal("terminal intercepted New")
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	}
	frameEvent(m, tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	frameEvent(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModAlt})
	if m.View().MouseMode != tea.MouseModeNone {
		t.Fatal("mouse text selection unavailable")
	}
}

func TestHovelTabStartsOnlyOnExplicitAction(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/absent-hovel-tab"}, true, launch.Options{Offline: true})
	defer m.terminals.close()
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	_, cmd := frameEvent(m, tea.MouseClickMsg{X: 39, Y: 1, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("Hovel click must request verified interactive launch")
	}
	_, duplicate := frameEvent(m, tea.MouseClickMsg{X: 39, Y: 1, Button: tea.MouseLeft})
	if duplicate != nil {
		t.Fatal("pending launch must not be duplicated")
	}
	frameEvent(m, cmd())
	if !strings.Contains(m.View().Content, "REFUSED") {
		t.Fatal("unavailable daemon must refuse the tab")
	}
	_, retry := frameEvent(m, tea.MouseClickMsg{X: 39, Y: 1, Button: tea.MouseLeft})
	if retry != nil {
		t.Fatal("selecting a failed tab must not retry")
	}
	late := newFrame(launch.Info{Workspace: "/tmp/late-hovel-tab"}, true, launch.Options{Offline: true})
	late.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	_, pending := late.Update(tea.MouseClickMsg{X: 39, Y: 1, Button: tea.MouseLeft})
	late.terminals.close()
	late.Update(pending())
	if late.current().cli.host != nil {
		t.Fatal("queued launch survived frontend shutdown")
	}
}

func TestWorkspaceTabShortcuts(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{Offline: true})
	defer m.terminals.close()
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	frameEvent(m, tea.PasteMsg{Content: "saved draft"})
	frameEvent(m, m.dispatch(m.active, func() tea.Msg {
		return workspaceOpened{info: launch.Info{Workspace: "/tmp/two"}}
	})())
	press := func(code rune) tea.Cmd {
		_, cmd := frameEvent(m, tea.KeyPressMsg{Code: code, Mod: tea.ModAlt})
		return cmd
	}
	if press('h') == nil || !m.terminalFocused() || m.current().cli == nil {
		t.Fatal("Alt+H must explicitly open the workspace CLI")
	}
	tab := m.current().cli
	if press('h') != nil || m.current().cli != tab {
		t.Fatal("Alt+H duplicated a pending launch")
	}
	press('b')
	if m.current().tab != "" || m.current().focus != "prompt" || !strings.Contains(m.View().Content, "saved draft") {
		t.Fatal("Alt+B must restore the overview and draft")
	}
	press('h')
	if m.current().cli != tab || !m.terminalFocused() {
		t.Fatal("Alt+H must restore the same CLI")
	}
	m.openNew()
	press('b')
	if m.modal != "new" || m.current().tab != "hovel" {
		t.Fatal("shortcut escaped the modal")
	}
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	m.selectWorkspace(1)
	if m.current().tab != "" || m.current().cli != nil {
		t.Fatal("tab shortcut leaked into another workspace")
	}
	press('b')
	m.selectWorkspace(0)
	if m.current().tab != "hovel" || m.current().cli != tab {
		t.Fatal("workspace tab selection was not retained")
	}
}

func TestExitedCLIRestartControls(t *testing.T) {
	for _, size := range []image.Point{{80, 24}, {120, 30}, {160, 40}, {200, 50}} {
		m := newFrame(launch.Info{Workspace: "/tmp/restart"}, true, launch.Options{Offline: true})
		defer m.terminals.close()
		frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
		m.current().cli = &cliTab{screen: ptyhost.Snapshot{Exited: true}}
		m.current().lastSuccess = m.now
		m.activate("hovel")
		view := m.View().Content
		if !strings.Contains(view, "CLI: exited") || !strings.Contains(view, "[Restart CLI]") {
			t.Fatal(view)
		}
		old := m.current().cli
		r := m.terminalBounds()
		_, cmd := frameEvent(m, tea.MouseClickMsg{X: r.Min.X + 1, Y: size.Y - 3, Button: tea.MouseLeft})
		if cmd == nil || m.current().cli == old || !m.current().cli.pending {
			t.Fatal("restart click must request a new verified launch")
		}
		pending := m.current().cli
		m.activate("restart-cli")
		if m.current().cli != pending {
			t.Fatal("duplicate restart replaced pending launch")
		}
		frameEvent(m, cmd()) // Refused offline launch remains explicitly retryable.
		m.openNew()
		frameEvent(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
		if m.current().cli != pending {
			t.Fatal("restart escaped modal")
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
		_, cmd = frameEvent(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
		if cmd == nil || m.current().cli == pending {
			t.Fatal("Ctrl+R must retry refused CLI")
		}
	}
}

// Events and operation completions are the existing production interaction seam.
// Delaying a completion here makes races deterministic without a fake renderer.
func TestWorkspaceIsolationAndStatus(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{Offline: true})
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	send := func(path string, msg tea.Msg) { frameEvent(m, m.dispatch(path, func() tea.Msg { return msg })()) }
	send("/tmp/one", workspaceOpened{info: launch.Info{Workspace: "/tmp/two"}})
	frameEvent(m, tea.PasteMsg{Content: "draft-one"})
	delayed := m.dispatch("/tmp/one", func() tea.Msg {
		return connectionList{states: []connection.State{{Name: "same", Host: "host-one", State: "active"}}}
	})()
	frameEvent(m, tea.KeyPressMsg{Code: 'w', Mod: tea.ModAlt})
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyDown})
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	frameEvent(m, tea.PasteMsg{Content: "draft-two"})
	send("/tmp/two", connectionList{states: []connection.State{{Name: "same", Host: "host-two", State: "active"}}})
	frameEvent(m, delayed)
	if got := m.View().Content; !strings.Contains(got, "host-two") || strings.Contains(got, "host-one") || !strings.Contains(got, "draft-two") {
		t.Fatal(got)
	}
	frameEvent(m, tea.KeyPressMsg{Code: 'w', Mod: tea.ModAlt})
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyUp})
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.View().Content; !strings.Contains(got, "host-one") || !strings.Contains(got, "draft-one") {
		t.Fatal(got)
	}
	// Duplicate or forged origin responses cannot overwrite the workspace.
	frameEvent(m, delayed)
	forged := delayed.(workspaceMessage)
	forged.workspace = "/tmp/two"
	frameEvent(m, forged)
	now := time.Now()
	m.now = now
	send("/tmp/one", daemonObservation{info: launch.Info{Workspace: "/tmp/one", PID: 123}, at: now, duration: 23 * time.Millisecond})
	if !strings.Contains(m.View().Content, "Daemon: connected") {
		t.Fatal(m.View().Content)
	}
	m.now = now.Add(9 * time.Second)
	if !strings.Contains(m.View().Content, "Daemon: stale") {
		t.Fatal("stale success stayed green")
	}
	send("/tmp/one", daemonObservation{err: fmt.Errorf("identity refused"), at: m.now})
	if !strings.Contains(m.View().Content, "Daemon: UNVERIFIED") {
		t.Fatal(m.View().Content)
	}
}

func TestResizeClickAndModalCapture(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{})
	click := func(x, y int) { frameEvent(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}) }
	for _, size := range [][2]int{{160, 48}, {200, 50}, {1, 1}, {160, 40}} {
		frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if size[0] < minimumWidth {
			continue
		}
		// Independent expected midpoint coordinates, followed by real native hit routing.
		click(2, size[1]/2)
		if !strings.Contains(m.View().Content, "Exact destination") {
			t.Fatal(m.View().Content)
		}
		click(25, 1) // underlying tab cannot capture input
		if m.modal != "new" {
			t.Fatal("modal click-through")
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
		click(22, size[1]/2)
		if m.modal != "menu" {
			t.Fatal("menu target moved after resize")
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
		for _, line := range strings.Split(m.View().Content, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("wide line", line)
			}
		}
	}
	frameEvent(m, tea.PasteMsg{Content: "draft"})
	frameEvent(m, tea.KeyPressMsg{Code: 'm', Mod: tea.ModAlt})
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyDown})
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(m.View().Content, "SELECTED CONNECTION") {
		t.Fatal("menu metadata action")
	}
	frameEvent(m, tea.PasteMsg{Content: "must-not-edit"})
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.current().management.input.Value() != "draft" {
		t.Fatal("modal changed draft")
	}
	// Failure is visible and never dispatches a replacement launch.
	frameEvent(m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModAlt})
	frameEvent(m, tea.PasteMsg{Content: "/tmp/refused"})
	frameEvent(m, m.dispatch(m.active, func() tea.Msg { return workspaceOpened{err: fmt.Errorf("unknown daemon refused")} })())
	if !strings.Contains(m.View().Content, "REFUSED") {
		t.Fatal(m.View().Content)
	}
}

func TestOverflowAndResourceSelection(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{})
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	for i := 0; i < 20; i++ {
		path := fmt.Sprintf("/tmp/workspace-%02d", i)
		frameEvent(m, m.dispatch(m.active, func() tea.Msg { return workspaceOpened{info: launch.Info{Workspace: path}} })())
	}
	frameEvent(m, tea.MouseWheelMsg{X: 2, Y: 4, Button: tea.MouseWheelDown})
	// A wheel changes the list, never the independently anchored controls.
	if m.navOffset != 1 || !strings.Contains(m.View().Content, "workspace-00") || strings.Contains(m.View().Content, "● one") {
		t.Fatal(m.View().Content)
	}
	frameEvent(m, tea.MouseClickMsg{X: 2, Y: 20, Button: tea.MouseLeft})
	if m.modal != "new" {
		t.Fatal("New scrolled away")
	}
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	frameEvent(m, m.dispatch(m.active, func() tea.Msg {
		return connectionList{states: []connection.State{{Name: "gateway", Host: "界界.example", User: "operator", State: "connected"}}}
	})())
	for y, line := range strings.Split(m.View().Content, "\n") {
		if strings.Contains(line, "gateway") {
			frameEvent(m, tea.MouseClickMsg{X: 29, Y: y, Button: tea.MouseLeft})
			break
		}
	}
	if m.current().selected != "gateway" {
		t.Fatal("click did not select resource")
	}
	frameEvent(m, tea.KeyPressMsg{Code: 'm', Mod: tea.ModAlt})
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyDown})
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(m.View().Content, "界界.example") {
		t.Fatal("selected metadata missing", m.View().Content)
	}
}

func TestReviewRegressions(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{})
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	frameEvent(m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModAlt})
	frameEvent(m, tea.PasteMsg{Content: "/tmp/never-launch-hidden-form"})
	frameEvent(m, tea.WindowSizeMsg{Width: 30, Height: 8})
	_, cmd := frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || m.launchPending {
		t.Fatal("hidden form submitted")
	}
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	if !strings.Contains(m.View().Content, "never-launch-hidden-form") {
		t.Fatal("resize discarded modal draft")
	}
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	rows := []connection.State{}
	for i := 0; i < 9; i++ {
		rows = append(rows, connection.State{Name: fmt.Sprintf("row%d", i), Host: "host", State: "connected"})
	}
	frameEvent(m, m.dispatch(m.active, func() tea.Msg { return connectionList{states: rows} })())
	for i := 0; i < 9; i++ {
		frameEvent(m, tea.MouseWheelMsg{X: 29, Y: 9, Button: tea.MouseWheelDown})
	}
	if !strings.Contains(m.View().Content, "row8") {
		t.Fatal("mouse cannot scroll inventory", m.View().Content)
	}
	frameEvent(m, tea.PasteMsg{Content: "inspect"})
	for i := 0; i < 8; i++ {
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyDown})
		suggestion := m.current().management.input.CurrentSuggestion()
		if !strings.Contains(m.View().Content, "› "+suggestion) {
			t.Fatal("selected completion obscured", suggestion, m.View().Content)
		}
	}
}

func TestBareConnectKeepsManagement(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/form-check"}, true, launch.Options{Offline: true})
	defer m.terminals.close()
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	frameEvent(m, tea.PasteMsg{Content: "connect"})
	_, cmd := frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		frameEvent(m, cmd())
	}
	view := m.View()
	if !view.AltScreen || !strings.Contains(view.Content, "Connection name") || !strings.Contains(view.Content, "WORKSPACES") {
		t.Fatal(view.Content)
	}
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.modal != "" || m.current().management.busy {
		t.Fatal("cancel did not restore command entry")
	}
}

// Drain native child commands at the existing event seam. Network/terminal
// commands remain explicit in these tests; real program timing is checked by PTY.
func frameEvent(m *frame, msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.Update(msg)
	if m.form == nil || cmd == nil {
		return model, cmd
	}
	events := make(chan tea.Msg, 128)
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c != nil {
			go func() { events <- c() }()
		}
	}
	run(cmd)
	for {
		select {
		case event := <-events:
			if event != nil && reflect.TypeOf(event).ConvertibleTo(reflect.TypeOf([]tea.Cmd{})) {
				for _, c := range reflect.ValueOf(event).Convert(reflect.TypeOf([]tea.Cmd{})).Interface().([]tea.Cmd) {
					run(c)
				}
			} else if _, ok := event.(formMessage); ok {
				_, next := m.Update(event)
				if m.form != nil {
					run(next)
				}
			}
		case <-time.After(10 * time.Millisecond):
			return m, nil
		}
	}
}

func TestFormsBrowseValidationAndIsolation(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/forms"}, true, launch.Options{Offline: true})
	defer m.terminals.close()
	frameEvent(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	frameEvent(m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModAlt})
	frameEvent(m, tea.PasteMsg{Content: "relative"})
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.launchPending || !strings.Contains(m.View().Content, "absolute canonical") {
		t.Fatal("path validation not inline", m.View().Content)
	}
	frameEvent(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if m.modal != "browse" {
		t.Fatal("local browse unavailable")
	}
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.modal != "new" || !strings.Contains(m.View().Content, "relative") {
		t.Fatal("browse cancellation lost typed draft")
	}
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	frameEvent(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.modal != "quit" {
		t.Fatal("Tab authorized quit")
	}
	// Mouse uses the same Huh approval, including default-negative selection.
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	frameEvent(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	for y := 0; y < m.height; y++ {
		for x := 0; x < m.width; x++ {
			if m.compositor().Hit(x, y).ID() == "confirm-reject" {
				frameEvent(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
				if m.modal != "" {
					t.Fatal("Keep working click failed")
				}
				return
			}
		}
	}
	t.Fatal("no confirmation pointer target")
}

func TestFormDraftsAndCaretAcrossSizes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.WriteFile(filepath.Join(home, "id_ed25519"), nil, 0600)
	for _, size := range []image.Point{{80, 24}, {120, 30}, {160, 40}, {200, 50}} {
		m := newFrame(launch.Info{Workspace: "/tmp/forms"}, true, launch.Options{})
		defer m.terminals.close()
		frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
		frameEvent(m, tea.PasteMsg{Content: "connect"})
		_, cmd := frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
		frameEvent(m, cmd())
		frameEvent(m, tea.PasteMsg{Content: "example.com"})
		view := m.View()
		if view.Cursor == nil || !image.Pt(view.Cursor.Position.X, view.Cursor.Position.Y).In(m.dialogBounds()) {
			t.Fatal("no-color form caret missing", view.Content)
		}
		capturePresentation(t, m, fmt.Sprintf("%dx%d-connect-no-color", size.X, size.Y))
		for _, value := range []string{"", "", "operator", "gateway"} {
			if value != "" {
				frameEvent(m, tea.PasteMsg{Content: value})
			}
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
		}
		if !strings.Contains(m.View().Content, "SSH key path") {
			t.Fatal("required-first navigation failed", m.View().Content)
		}
		_, cmd = frameEvent(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
		frameEvent(m, cmd())
		if !strings.Contains(m.View().Content, "id_ed25519") {
			t.Fatal("extensionless key omitted", m.View().Content)
		}
		capturePresentation(t, m, fmt.Sprintf("%dx%d-local-browser", size.X, size.Y))
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.modal != "connect" || !strings.Contains(m.View().Content, "id_ed25519") {
			t.Fatal("browse did not restore typed path", m.View().Content)
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	}
}

func TestPendingWorkspaceDoesNotBlockOtherForms(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/forms"}, true, launch.Options{Offline: true})
	defer m.terminals.close()
	frameEvent(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	frameEvent(m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModAlt})
	frameEvent(m, tea.PasteMsg{Content: "/tmp/retry-destination"})
	// Advance the actual form; hold the returned launch command to deliver a
	// deterministic failure at the public operation-completion seam.
	f := m.form
	var pending tea.Cmd
	events := []tea.Msg{tea.KeyPressMsg{Code: tea.KeyEnter}}
	for len(events) > 0 {
		event := events[0]
		events = events[1:]
		_, cmd := m.Update(event)
		if cmd == nil {
			continue
		}
		if m.launchPending {
			pending = cmd
			break
		}
		result := cmd()
		if result != nil && reflect.TypeOf(result).ConvertibleTo(reflect.TypeOf([]tea.Cmd{})) {
			for _, c := range reflect.ValueOf(result).Convert(reflect.TypeOf([]tea.Cmd{})).Interface().([]tea.Cmd) {
				events = append(events, c())
			}
		} else {
			events = append(events, result)
		}
	}
	if pending == nil || f == nil {
		t.Fatal("workspace form did not submit")
	}
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	frameEvent(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.modal != "" {
		t.Fatal("pending launch blocked Keep working")
	}
	m.openNew()
	frameEvent(m, m.dispatch(m.active, func() tea.Msg { return workspaceOpened{destination: m.destination, err: fmt.Errorf("launch refused")} })())
	if m.form == nil || !strings.Contains(m.View().Content, "retry-destination") {
		t.Fatal("failed launch lost editable draft", m.View().Content)
	}
	frameEvent(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if !strings.Contains(m.View().Content, "retry-destinationx") {
		t.Fatal("failed destination cannot be edited")
	}
}

func TestConnectSinglePageAndQuitChoices(t *testing.T) {
	for _, size := range []image.Point{{80, 24}, {120, 30}, {160, 40}, {200, 50}} {
		m := newFrame(launch.Info{Workspace: "/tmp/forms"}, true, launch.Options{})
		defer m.terminals.close()
		frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
		frameEvent(m, tea.PasteMsg{Content: "connect"})
		_, cmd := frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
		frameEvent(m, cmd())
		if m.form.GetFocusedField().GetKey() != "host" {
			t.Fatal("host must come first")
		}
		if size.Y >= 40 {
			for _, field := range connectFields {
				if !strings.Contains(m.View().Content, field.title) {
					t.Fatal("missing form field", field.title, m.View().Content)
				}
			}
		}
		compositor := m.compositor()
	clicked:
		for y := 0; y < size.Y; y++ {
			for x := 0; x < size.X; x++ {
				if compositor.Hit(x, y).ID() == "field:name" {
					frameEvent(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
					break clicked
				}
			}
		}
		if m.form.GetFocusedField().GetKey() != "name" {
			t.Fatal("click did not focus name", m.View().Content)
		}
		frameEvent(m, tea.PasteMsg{Content: "clicked"})
		if m.details.name != "clicked" || m.details.host != "" {
			t.Fatal("click edited wrong field")
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyUp})
		if m.form.GetFocusedField().GetKey() != "user" {
			t.Fatal("up did not go back")
		}
		frameEvent(m, tea.PasteMsg{Content: "operator"})
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyDown})
		if m.form.GetFocusedField().GetKey() != "name" {
			t.Fatal("down did not advance")
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
		if m.form.GetFocusedField().GetKey() != "key" {
			t.Fatal("tab did not advance")
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
		if m.form.GetFocusedField().GetKey() != "name" || m.details.name != "clicked" {
			t.Fatal("shift-tab lost field or draft")
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
		frameEvent(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		for _, label := range []string{"Keep working]", "Quit]"} {
			if !strings.Contains(m.View().Content, label) {
				t.Fatal("quit choice clipped", label, m.View().Content)
			}
		}
		capturePresentation(t, m, fmt.Sprintf("%dx%d-quit-visible", size.X, size.Y))
	}
}

func TestConnectSuggestionsFollowLazySSHOrder(t *testing.T) {
	for _, check := range []struct{ line, option string }{
		{"connect ", "-ip "}, {"connect -ip host ", "-port "},
		{"connect -ip host -port 22 ", "-user "}, {"connect -ip host -port 22 -user operator ", "-socket "},
	} {
		got := connection.CommandSuggestions(check.line, nil)
		if len(got) != 1 || got[0] != check.line+check.option {
			t.Fatal("incorrect guided completion", check.line, got)
		}
	}
	got := connection.CommandSuggestions("connect -ip host -port 22 -user operator -socket gateway ", nil)
	if len(got) == 0 || !strings.HasSuffix(got[0], "-ssh-key ") {
		t.Fatal("optional settings missing", got)
	}
}

func TestConnectSuggestionsExcludeSuppliedAliases(t *testing.T) {
	for _, line := range []string{
		"connect -ip host -port 22 -user operator -socket gateway -ssh-key /tmp/key ",
		"connect --host host --port=22 --user operator --name gateway --key /tmp/key ",
	} {
		for _, got := range connection.CommandSuggestions(line, nil) {
			for _, duplicate := range []string{"-ip ", "-port ", "-user ", "-socket ", "--port ", "--key ", "-ssh-key "} {
				if strings.TrimPrefix(got, line) == duplicate {
					t.Fatal("suggested duplicate", got)
				}
			}
		}
	}
}

func TestReviewedApprovalOverridesExplicitNo(t *testing.T) {
	_, approved, err := connection.Parse("/tmp/forms", []string{"gateway", "example.com", "operator", "--yes=false", "--yes"})
	if err != nil || !approved {
		t.Fatal("explicit no prevented later interactive approval", err)
	}
}
