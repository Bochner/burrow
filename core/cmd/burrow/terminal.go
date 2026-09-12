package main

import (
	"context"
	"image"
	"sync"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/Bochner/burrow/core/launch"
	ptyhost "github.com/Bochner/burrow/core/terminal"
	uv "github.com/charmbracelet/ultraviolet"
)

var terminalEscape = key.NewBinding(key.WithKeys("ctrl+]"))
var toggleMouse = key.NewBinding(key.WithKeys("alt+s"))
var showBurrow = key.NewBinding(key.WithKeys("alt+b"), key.WithHelp("Alt+B", "Burrow"))
var showHovel = key.NewBinding(key.WithKeys("alt+h"), key.WithHelp("Alt+H", "Hovel"))
var restartTerminal = key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("Ctrl+R", "restart CLI"))

// Serializes launch registration against shutdown, including commands Bubble
// Tea has not started yet. Closing waits for every child that actually started.
type terminalLifetime struct {
	context context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	jobs    sync.WaitGroup
}

func (l *terminalLifetime) begin() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.context.Err() != nil {
		return false
	}
	l.jobs.Add(1)
	return true
}
func (l *terminalLifetime) close() {
	l.mu.Lock()
	l.cancel()
	l.mu.Unlock()
	l.jobs.Wait()
}

type cliTab struct {
	host    *ptyhost.Host
	screen  ptyhost.Snapshot
	pending bool
	error   string
}
type cliOpened struct {
	tab  *cliTab
	host *ptyhost.Host
	err  error
}
type cliScreen struct {
	tab    *cliTab
	screen ptyhost.Snapshot
}
type cliClosed struct{ tab *cliTab }

func (m *frame) terminalBounds() image.Rectangle {
	left, right := m.columns()
	return image.Rect(left+2, 3, max(left+3, m.width-right-2), max(4, m.height-2))
}
func (m *frame) terminalFocused() bool {
	w := m.current()
	return m.modal == "" && !w.management.help && !w.management.quitting && w.tab == "hovel" && w.focus == "terminal"
}
func (m *frame) openCLI() tea.Cmd {
	w := m.current()
	w.tab, w.focus = "hovel", "terminal"
	if w.cli != nil {
		return nil
	}
	tab := &cliTab{pending: true}
	w.cli = tab
	if m.demo {
		tab.pending = false
		tab.error = "Sample preview · CLI launch disabled"
		return nil
	}
	path, bounds, lifetime := m.active, m.terminalBounds(), m.terminals
	return m.dispatch(path, func() tea.Msg {
		if !lifetime.begin() {
			return cliOpened{tab: tab, err: context.Canceled}
		}
		ctx, cancel := context.WithTimeout(lifetime.context, 10*time.Second)
		defer cancel()
		cmd, err := launch.HovelShell(ctx, path)
		var host *ptyhost.Host
		if err == nil {
			host, err = ptyhost.Start(lifetime.context, cmd, bounds.Dx(), bounds.Dy())
		}
		if err != nil {
			lifetime.jobs.Done()
		} else {
			go func() { defer lifetime.jobs.Done(); <-host.Done() }()
		}
		return cliOpened{tab, host, err}
	})
}

func (w *workspaceView) canRestartCLI() bool {
	return w.cli != nil && !w.cli.pending && (w.cli.screen.Exited || w.cli.host == nil)
}

func (m *frame) restartCLI() tea.Cmd {
	w := m.current()
	if !w.canRestartCLI() {
		return nil
	}
	old := w.cli.host
	w.cli = nil
	start := m.openCLI()
	if old == nil {
		return start
	}
	return tea.Sequence(func() tea.Msg { old.Close(); return nil }, start)
}
func (m *frame) readCLI(path string, tab *cliTab) tea.Cmd {
	return m.dispatch(path, func() tea.Msg {
		// ponytail: 30 ms snapshots; switch to dirty-screen notifications if rendering costs warrant it.
		timer := time.NewTimer(30 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-m.terminals.context.Done():
		}
		return cliScreen{tab, tab.host.Snapshot()}
	})
}
func (m *frame) terminalResult(path string, msg tea.Msg) tea.Cmd {
	w := m.workspaces[path]
	switch v := msg.(type) {
	case cliOpened:
		if w.cli != v.tab {
			if v.host != nil {
				return func() tea.Msg { v.host.Close(); return nil }
			}
			return nil
		}
		v.tab.pending = false
		if v.err != nil {
			v.tab.error = "REFUSED: " + safe(v.err.Error())
			return nil
		}
		v.tab.host = v.host
		r := m.terminalBounds()
		v.host.Send(image.Pt(r.Dx(), r.Dy()))
		return m.readCLI(path, v.tab)
	case cliScreen:
		if w.cli != v.tab {
			return nil
		}
		v.tab.screen = v.screen
		if v.screen.Err != nil {
			v.tab.error = "CLI ended or input failed; inspect Hovel history before repeating work"
		}
		if !v.screen.Exited {
			return m.readCLI(path, v.tab)
		}
	case cliClosed:
		if w.cli == v.tab {
			w.cli = nil
		}
	}
	return nil
}
func (m *frame) closeCLI() tea.Cmd {
	w := m.current()
	tab := w.cli
	if tab == nil || tab.pending {
		return nil
	}
	w.tab, w.focus = "", "prompt"
	w.management.output = "CLI closed · daemon resources retained; inspect Hovel throw/session history for in-flight work."
	if tab.host == nil {
		w.cli = nil
		return nil
	}
	tab.pending = true
	return m.dispatch(m.active, func() tea.Msg { tab.host.Close(); return cliClosed{tab} })
}
func (m *frame) sendTerminal(event any) {
	tab := m.current().cli
	if tab == nil || tab.pending || tab.host == nil || tab.screen.Exited {
		return
	}
	if err := tab.host.Send(event); err != nil {
		tab.error = safe(err.Error())
	}
}
func (m *frame) terminalMouse(msg tea.MouseMsg) {
	r := m.terminalBounds()
	v := uv.Mouse(msg.Mouse())
	v.X -= r.Min.X
	v.Y -= r.Min.Y
	switch msg.(type) {
	case tea.MouseClickMsg:
		m.sendTerminal(uv.MouseClickEvent(v))
	case tea.MouseReleaseMsg:
		m.sendTerminal(uv.MouseReleaseEvent(v))
	case tea.MouseWheelMsg:
		m.sendTerminal(uv.MouseWheelEvent(v))
	case tea.MouseMotionMsg:
		m.sendTerminal(uv.MouseMotionEvent(v))
	}
}
