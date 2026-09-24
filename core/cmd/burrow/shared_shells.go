package main

import (
	"context"
	"fmt"
	"image"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Bochner/burrow/core/connection"
	ptyhost "github.com/Bochner/burrow/core/terminal"
)

type shellsDiscovered struct {
	shells []connection.Shell
	errors []string
	show   bool
}
type sharedClosed struct {
	tab *cliTab
	err error
}
type shellDetached struct {
	tab *cliTab
	err error
}

func (m *frame) openSharedShell(name, id string) tea.Cmd {
	w := m.current()
	w.management.busy = false
	if m.invalidGeometry {
		w.management.output = invalidTerminalGeometry
		return nil
	}
	for _, tab := range w.shells {
		if id != "" && tab.session == id && tab.connection == name {
			m.resumeShell(tab)
			if tab.host == nil && !tab.pending {
				tab.pending, tab.error = true, ""
				return m.attachShared(m.active, tab, false)
			}
			return nil
		}
	}
	tab := &cliTab{id: strconv.Itoa(len(w.shells) + 1), connection: name, session: id, pending: true}
	w.shells = append(w.shells, tab)
	w.management.output = "Opening retained SSH shell (" + tab.label() + ")…"
	w.management.outputOffset = 0
	m.resumeShell(tab)
	return m.attachShared(m.active, tab, id == "")
}

func (m *frame) attachShared(path string, tab *cliTab, create bool) tea.Cmd {
	lifetime, bounds, name, id := m.terminals, m.terminalBounds(), tab.connection, tab.session
	return m.dispatch(path, func() tea.Msg {
		if !lifetime.begin() {
			return cliOpened{tab: tab, err: context.Canceled}
		}
		ctx, cancel := context.WithTimeout(lifetime.context, 30*time.Second)
		defer cancel()
		if create {
			value, err := connection.Execute(ctx, path, []string{"session", "create", name, "--columns", strconv.Itoa(bounds.Dx()), "--rows", strconv.Itoa(bounds.Dy()), "--yes"})
			if err != nil {
				lifetime.jobs.Done()
				return cliOpened{tab: tab, err: err}
			}
			id = value.(connection.Shell).ID
		}
		host, err := ptyhost.Attach(lifetime.context, path, name, id)
		if err != nil {
			lifetime.jobs.Done()
			return cliOpened{tab: tab, err: fmt.Errorf("shell %s observation failed; retained state unverified: %w", id, err)}
		}
		go func() { defer lifetime.jobs.Done(); <-host.Done() }()
		return cliOpened{tab: tab, host: host}
	})
}

func (m *frame) discoverShells(path string, show bool) tea.Cmd {
	return m.dispatch(path, func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.terminals.context, 15*time.Second)
		defer cancel()
		out := shellsDiscovered{show: show}
		states, err := connection.List(ctx, path)
		if err != nil {
			out.errors = append(out.errors, "Shell inventory UNVERIFIED: "+safe(err.Error()))
			return out
		}
		for _, state := range states {
			shells, err := connection.Shells(ctx, path, state.Name)
			if err != nil {
				out.errors = append(out.errors, safe(state.Name)+": shell inventory UNVERIFIED; previous tabs retained")
				continue
			}
			out.shells = append(out.shells, shells...)
		}
		return out
	})
}

func (m *frame) acceptShells(path string, result shellsDiscovered) tea.Cmd {
	w := m.workspaces[path]
	var cmds []tea.Cmd
	for _, shell := range result.shells {
		if shell.State == "closed" || shell.State == "exited" {
			continue
		}
		if slices.ContainsFunc(w.shells, func(tab *cliTab) bool {
			return tab.session == shell.ID || (tab.pending && tab.session == "" && tab.connection == shell.Connection.Name)
		}) {
			continue
		}
		tab := &cliTab{id: strconv.Itoa(len(w.shells) + 1), connection: shell.Connection.Name, session: shell.ID, pending: true}
		w.shells = append(w.shells, tab)
		cmds = append(cmds, m.attachShared(path, tab, false))
	}
	if result.show {
		lines := append([]string{}, result.errors...)
		for _, tab := range w.shells {
			if tab.logs == nil && tab.editor == nil {
				lines = append(lines, "Shell "+tab.label()+" · "+tab.state()+" · "+tab.session)
			}
		}
		if len(lines) == 0 {
			lines = append(lines, "No retained shells in this workspace.")
		}
		w.management.output, w.management.busy = strings.Join(lines, "\n"), false
	} else if len(result.errors) > 0 {
		w.management.output = strings.Join(result.errors, "\n")
	}
	return tea.Batch(cmds...)
}

func (m *frame) controlShell(tab *cliTab) tea.Cmd {
	if tab == nil || tab.pending || tab.host == nil || tab.session == "" {
		return nil
	}
	r := m.terminalBounds()
	if err := tab.host.Send(ptyhost.Control(image.Pt(r.Dx(), r.Dy()))); err != nil {
		tab.error = safe(err.Error())
	} else {
		tab.error = ""
	}
	return nil
}

func (m *frame) detachShell(tab *cliTab) tea.Cmd {
	w := m.current()
	w.tab, w.focus = "", "prompt"
	if tab == nil {
		return nil
	}
	host, ok := tab.host.(*ptyhost.Shared)
	if !ok {
		return nil
	}
	w.management.output = "Releasing shell control; retained shell continues…"
	path := m.active
	return m.dispatch(path, func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.terminals.context, 5*time.Second)
		defer cancel()
		return shellDetached{tab, host.Detach(ctx)}
	})
}

func (m *frame) closeSharedShell(tab *cliTab) tea.Cmd {
	path, name, id, host := m.active, tab.connection, tab.session, tab.host
	tab.pending = true
	m.current().management.output = "Closing retained SSH shell…"
	return m.dispatch(path, func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.terminals.context, 15*time.Second)
		defer cancel()
		_, err := connection.Execute(ctx, path, []string{"session", "close", name, id, "--yes"})
		if err == nil && host != nil {
			host.Close()
		}
		return sharedClosed{tab, err}
	})
}
