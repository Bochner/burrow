package main

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/Bochner/burrow/core/connection"
)

// A review snapshot, never another resource registry. CloseReviewed rechecks it.
type quitSnapshot struct {
	paths   []string
	states  map[string][]connection.State
	errors  []string
	closing bool
}

func (q quitSnapshot) count() int {
	n := 0
	for _, states := range q.states {
		n += len(states)
	}
	return n
}

func (m *frame) quitSummary() string {
	q := m.quitReview
	u := m.current().management
	var rows [][]string
	for _, path := range q.paths {
		for _, s := range q.states[path] {
			rows = append(rows, []string{safe(filepath.Base(path)), safe(s.Name), safe(s.State)})
		}
	}
	var lines []string
	if len(rows) > 0 {
		lines = append(lines, u.dataTable("", []string{"WORKSPACE", "CONNECTION", "STATUS"}, rows, max(1, m.dialogBounds().Dx()-6)))
	}
	for _, err := range q.errors {
		lines = append(lines, u.paint(errorStyle, err))
	}
	return strings.Join(lines, "\n")
}

func readQuit(ctx context.Context, paths []string) quitSnapshot {
	q := quitSnapshot{paths: paths, states: map[string][]connection.State{}}
	for _, path := range paths {
		states, err := connection.List(ctx, path)
		if err != nil {
			q.errors = append(q.errors, safe(path)+": UNVERIFIED; resources retained")
		} else {
			q.states[path] = states
		}
	}
	return q
}

func (m *frame) openQuit() tea.Cmd {
	m.modal, m.formTitle, m.form = "quit", "Quit Burrow?", nil
	m.modalOffset = 0
	m.inputEpoch++
	m.quitReview = quitSnapshot{errors: []string{"Checking connections…"}}
	if m.demo {
		return m.showQuit(quitSnapshot{})
	}
	paths := append([]string{}, m.paths...)
	return formCommand(m.active, m.inputEpoch, func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.terminals.context, 20*time.Second)
		defer cancel()
		return readQuit(ctx, paths)
	})
}

func (m *frame) showQuit(q quitSnapshot) tea.Cmd {
	m.quitClosing = false
	if !m.demo && !slices.Equal(q.paths, m.paths) {
		return m.openQuit()
	}
	m.quitReview = q
	if q.closing && m.launchPending {
		q.errors = append(q.errors, "Workspace opening is still pending; review again when it finishes.")
		m.quitReview = q
	}
	if q.closing && q.count() == 0 && len(q.errors) == 0 {
		return tea.Quit
	}
	if q.closing && q.count() > 0 {
		m.quitReview.errors = append(m.quitReview.errors, "Connections remain or changed; review again before quitting.")
	}
	f := quitForm()
	if q.count() > 0 {
		f = confirmForm("", "Keep connections, or close all listed connections and quit?\nSaved settings/evidence and daemon remain. Local terminals end.", "Keep running", "Close connections")
		keep := true // Safe default: Enter never tears resources down.
		f.GetFocusedField().(*huh.Confirm).Value(&keep)
	} else if len(q.errors) > 0 {
		f = confirmForm("", "Connection status is unverified. Quit keeps all resources.\nFrontend-local terminals end.", "Quit", "Keep working")
	}
	return m.setForm("quit", "Quit Burrow?", f)
}

func (m *frame) quitChanged() bool {
	return !m.demo && !slices.Equal(m.quitReview.paths, m.paths)
}

func (m *frame) closeForQuit() tea.Cmd {
	m.quitClosing = true
	m.form = nil
	m.inputEpoch++
	q := m.quitReview
	m.quitReview.errors = []string{"Closing reviewed connections; waiting for verified cleanup…"}
	return formCommand(m.active, m.inputEpoch, func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.terminals.context, 60*time.Second)
		defer cancel()
		var failures []string
		for _, path := range q.paths {
			if err := connection.CloseInventory(ctx, path, q.states[path]); err != nil {
				failures = append(failures, safe(path)+": "+safe(err.Error()))
			}
		}
		result := readQuit(ctx, q.paths)
		result.errors = append(failures, result.errors...)
		result.closing = true
		return result
	})
}
