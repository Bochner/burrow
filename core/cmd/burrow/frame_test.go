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
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceFrame(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{Offline: true})
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	view := m.View().Content
	for _, label := range []string{"WORKSPACES", "New", "Menu", "Hovel", "SAVED CONNECTION", "No shells"} {
		if !strings.Contains(view, label) {
			t.Fatalf("missing %s: %s", label, view)
		}
	}
	m.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModAlt})
	if !strings.Contains(m.View().Content, "Exact destination") {
		t.Fatal("New must show destination before launch")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
}

func TestEmbeddedTerminalKeepsFrameAndBackgroundOutput(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{})
	defer m.terminals.close()
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m.Update(tea.PasteMsg{Content: "saved draft"})
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
			m.Update(m.readCLI(m.active, tab)())
			if strings.Contains(tab.screen.Screen, needle) {
				return
			}
		}
		t.Fatal(tab.screen)
	}
	refresh("ready")
	m.Update(tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
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
		m.Update(tea.WindowSizeMsg{Width: size.X, Height: size.Y})
		m.Update(m.readCLI(m.active, tab)())
		view := m.View()
		if !strings.Contains(view.Content, "background-result") || !strings.Contains(view.Content, "[New]") || !strings.Contains(view.Content, "[Menu]") || !strings.Contains(view.Content, "WORKSPACES") {
			t.Fatal(view.Content)
		}
		r := m.terminalBounds()
		if view.Cursor == nil || !image.Pt(view.Cursor.Position.X, view.Cursor.Position.Y).In(r) {
			t.Fatalf("cursor must stay in pane: %+v %+v", view.Cursor, r)
		}
		m.Update(tea.MouseClickMsg{X: 2, Y: size.Y / 2, Button: tea.MouseLeft})
		if m.modal != "new" {
			t.Fatal("terminal intercepted New")
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	}
	m.Update(tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModAlt})
	if m.View().MouseMode != tea.MouseModeNone {
		t.Fatal("mouse text selection unavailable")
	}
}

func TestHovelTabStartsOnlyOnExplicitAction(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/absent-hovel-tab"}, true, launch.Options{Offline: true})
	defer m.terminals.close()
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	_, cmd := m.Update(tea.MouseClickMsg{X: 39, Y: 1, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("Hovel click must request verified interactive launch")
	}
	_, duplicate := m.Update(tea.MouseClickMsg{X: 39, Y: 1, Button: tea.MouseLeft})
	if duplicate != nil {
		t.Fatal("pending launch must not be duplicated")
	}
	m.Update(cmd())
	if !strings.Contains(m.View().Content, "REFUSED") {
		t.Fatal("unavailable daemon must refuse the tab")
	}
	_, retry := m.Update(tea.MouseClickMsg{X: 39, Y: 1, Button: tea.MouseLeft})
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

// Events and operation completions are the existing production interaction seam.
// Delaying a completion here makes races deterministic without a fake renderer.
func TestWorkspaceIsolationAndStatus(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{Offline: true})
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	send := func(path string, msg tea.Msg) { m.Update(m.dispatch(path, func() tea.Msg { return msg })()) }
	send("/tmp/one", workspaceOpened{info: launch.Info{Workspace: "/tmp/two"}})
	m.Update(tea.PasteMsg{Content: "draft-one"})
	delayed := m.dispatch("/tmp/one", func() tea.Msg {
		return connectionList{states: []connection.State{{Name: "same", Host: "host-one", State: "active"}}}
	})()
	m.Update(tea.KeyPressMsg{Code: 'w', Mod: tea.ModAlt})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.PasteMsg{Content: "draft-two"})
	send("/tmp/two", connectionList{states: []connection.State{{Name: "same", Host: "host-two", State: "active"}}})
	m.Update(delayed)
	if got := m.View().Content; !strings.Contains(got, "host-two") || strings.Contains(got, "host-one") || !strings.Contains(got, "draft-two") {
		t.Fatal(got)
	}
	m.Update(tea.KeyPressMsg{Code: 'w', Mod: tea.ModAlt})
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.View().Content; !strings.Contains(got, "host-one") || !strings.Contains(got, "draft-one") {
		t.Fatal(got)
	}
	// Duplicate or forged origin responses cannot overwrite the workspace.
	m.Update(delayed)
	forged := delayed.(workspaceMessage)
	forged.workspace = "/tmp/two"
	m.Update(forged)
	now := time.Now()
	m.now = now
	send("/tmp/one", daemonObservation{info: launch.Info{Workspace: "/tmp/one", PID: 123}, at: now, duration: 23 * time.Millisecond})
	if !strings.Contains(m.View().Content, "Hovel: verified") {
		t.Fatal(m.View().Content)
	}
	m.now = now.Add(9 * time.Second)
	if !strings.Contains(m.View().Content, "Hovel: stale") {
		t.Fatal("stale success stayed green")
	}
	send("/tmp/one", daemonObservation{err: fmt.Errorf("identity refused"), at: m.now})
	if !strings.Contains(m.View().Content, "Hovel: UNVERIFIED") {
		t.Fatal(m.View().Content)
	}
}

func TestResizeClickAndModalCapture(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{})
	click := func(x, y int) { m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}) }
	for _, size := range [][2]int{{160, 48}, {200, 50}, {1, 1}, {160, 40}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
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
		m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
		click(22, size[1]/2)
		if m.modal != "menu" {
			t.Fatal("menu target moved after resize")
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
		for _, line := range strings.Split(m.View().Content, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("wide line", line)
			}
		}
	}
	m.Update(tea.PasteMsg{Content: "draft"})
	m.Update(tea.KeyPressMsg{Code: 'm', Mod: tea.ModAlt})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(m.View().Content, "SELECTED CONNECTION") {
		t.Fatal("menu metadata action")
	}
	m.Update(tea.PasteMsg{Content: "must-not-edit"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.current().management.input.Value() != "draft" {
		t.Fatal("modal changed draft")
	}
	// Failure is visible and never dispatches a replacement launch.
	m.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModAlt})
	m.Update(tea.PasteMsg{Content: "/tmp/refused"})
	m.Update(m.dispatch(m.active, func() tea.Msg { return workspaceOpened{err: fmt.Errorf("unknown daemon refused")} })())
	if !strings.Contains(m.View().Content, "REFUSED") {
		t.Fatal(m.View().Content)
	}
}

func TestOverflowAndResourceSelection(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{})
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	for i := 0; i < 20; i++ {
		path := fmt.Sprintf("/tmp/workspace-%02d", i)
		m.Update(m.dispatch(m.active, func() tea.Msg { return workspaceOpened{info: launch.Info{Workspace: path}} })())
	}
	m.Update(tea.MouseWheelMsg{X: 2, Y: 4, Button: tea.MouseWheelDown})
	// A wheel changes the list, never the independently anchored controls.
	if m.navOffset != 1 || !strings.Contains(m.View().Content, "workspace-00") || strings.Contains(m.View().Content, "● one") {
		t.Fatal(m.View().Content)
	}
	m.Update(tea.MouseClickMsg{X: 2, Y: 20, Button: tea.MouseLeft})
	if m.modal != "new" {
		t.Fatal("New scrolled away")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m.Update(m.dispatch(m.active, func() tea.Msg {
		return connectionList{states: []connection.State{{Name: "gateway", Host: "界界.example", User: "operator", State: "connected"}}}
	})())
	for y, line := range strings.Split(m.View().Content, "\n") {
		if strings.Contains(line, "gateway") {
			m.Update(tea.MouseClickMsg{X: 29, Y: y, Button: tea.MouseLeft})
			break
		}
	}
	if m.current().selected != "gateway" {
		t.Fatal("click did not select resource")
	}
	m.Update(tea.KeyPressMsg{Code: 'm', Mod: tea.ModAlt})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(m.View().Content, "界界.example") {
		t.Fatal("selected metadata missing", m.View().Content)
	}
}

func TestReviewRegressions(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{})
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModAlt})
	m.Update(tea.PasteMsg{Content: "/tmp/never-launch-hidden-form"})
	m.Update(tea.WindowSizeMsg{Width: 30, Height: 8})
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || m.launchPending {
		t.Fatal("hidden form submitted")
	}
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	if !strings.Contains(m.View().Content, "never-launch-hidden-form") {
		t.Fatal("resize discarded modal draft")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	rows := []connection.State{}
	for i := 0; i < 9; i++ {
		rows = append(rows, connection.State{Name: fmt.Sprintf("row%d", i), Host: "host", State: "connected"})
	}
	m.Update(m.dispatch(m.active, func() tea.Msg { return connectionList{states: rows} })())
	for i := 0; i < 9; i++ {
		m.Update(tea.MouseWheelMsg{X: 29, Y: 9, Button: tea.MouseWheelDown})
	}
	if !strings.Contains(m.View().Content, "row8") {
		t.Fatal("mouse cannot scroll inventory", m.View().Content)
	}
	m.Update(tea.PasteMsg{Content: "inspect"})
	for i := 0; i < 8; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		suggestion := m.current().management.input.CurrentSuggestion()
		if !strings.Contains(m.View().Content, "› "+suggestion) {
			t.Fatal("selected completion obscured", suggestion, m.View().Content)
		}
	}
}
