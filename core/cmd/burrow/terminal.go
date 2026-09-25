package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/Bochner/burrow/core/connection"
	"github.com/Bochner/burrow/core/launch"
	ptyhost "github.com/Bochner/burrow/core/terminal"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

var terminalEscape = key.NewBinding(key.WithKeys("ctrl+]"))
var takeShellControl = key.NewBinding(key.WithKeys("alt+t"), key.WithHelp("Alt+T", "take control"))

const invalidTerminalGeometry = "REFUSED: terminal geometry outside 1..1000 cells; previous size retained."

var toggleMouse = key.NewBinding(key.WithKeys("alt+s"))
var showBurrow = key.NewBinding(key.WithKeys("alt+b"), key.WithHelp("Alt+B", "Burrow"))
var showHovel = key.NewBinding(key.WithKeys("alt+h"), key.WithHelp("Alt+H", "Hovel"))
var previousShell = key.NewBinding(key.WithKeys("alt+left"), key.WithHelp("Alt+←/→", "shells"))
var nextShell = key.NewBinding(key.WithKeys("alt+right"))
var numberedShell = key.NewBinding(key.WithKeys("alt+1", "alt+2", "alt+3", "alt+4", "alt+5", "alt+6", "alt+7", "alt+8", "alt+9"), key.WithHelp("Alt+1–9", "shell"))
var restartTerminal = key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("Ctrl+R", "restart CLI"))

// Serializes launch registration against shutdown, including commands Bubble
// Tea has not started yet. Closing waits for every child that actually started.
type terminalLifetime struct {
	context  context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	jobs     sync.WaitGroup
	auditErr error
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
func (l *terminalLifetime) recordAuditError(err error) {
	if err != nil {
		l.mu.Lock()
		l.auditErr = errors.Join(l.auditErr, err)
		l.mu.Unlock()
	}
}

func (l *terminalLifetime) close() {
	l.mu.Lock()
	l.cancel()
	l.mu.Unlock()
	l.jobs.Wait()
}

type terminalHost interface {
	Send(any) error
	Snapshot() ptyhost.Snapshot
	Close()
	Done() <-chan struct{}
}

type cliTab struct {
	session    string
	setupInput string
	setupWait  string
	prefill    string
	prefillBy  time.Time
	notice     string
	logs       *logView
	auditDone  chan error
	editor     *profileEdit
	id         string
	connection string
	host       terminalHost
	screen     ptyhost.Snapshot
	pending    bool
	error      string
}
type cliOpened struct {
	tab  *cliTab
	host terminalHost
	err  error
}
type cliScreen struct {
	tab    *cliTab
	screen ptyhost.Snapshot
}
type cliClosed struct{ tab *cliTab }
type shellRequested struct{ name, session string }

type chainStaged struct {
	file string
	err  error
}

func saveChain(path, name string, chain any) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var ignored struct{}
	if err := launch.Call(ctx, path, "CreateOperation", map[string]string{"Operation": "burrow"}, &ignored); err != nil {
		return "", err
	}
	if err := launch.Call(ctx, path, "CreateChain", map[string]string{"Operation": "burrow", "Chain": "burrow-chain"}, &ignored); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(chain, "", "  ")
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp(path, name+"-*.chain.json")
	if err != nil {
		return "", err
	}
	_, err = f.Write(append(data, '\n'))
	err = errors.Join(err, f.Close())
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func (m *frame) stageChain(args []string) tea.Cmd {
	path, parent := m.active, m.terminals.context
	if args[1] == "connect" {
		c, _, err := connection.Parse(path, args[2:])
		if err == nil && c.Prompt {
			ctx, cancel := context.WithCancel(parent)
			a := &authAttempt{path: path, ctx: ctx, cancel: cancel, questions: make(chan authQuestion), done: make(chan struct{}), chain: true, label: c.Name + " · " + c.User + "@" + c.Host}
			m.attempt = a
			staged := make(chan chainStaged, 1)
			return tea.Batch(waitQuestion(a), m.dispatch(path, func() tea.Msg {
				select {
				case v := <-staged:
					return v
				case <-ctx.Done():
					return nil
				}
			}), func() tea.Msg {
				defer close(a.done)
				defer cancel()
				result, err := connection.PrepareChainPrompt(ctx, path, args, a.ask, func(chain any) error {
					file, err := saveChain(path, args[1]+"-"+args[2], chain)
					if err == nil {
						staged <- chainStaged{file: file}
					}
					return err
				})
				return authFinished{a, result, err}
			})
		}
	}
	return m.dispatch(path, func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, time.Minute)
		defer cancel()
		chain, err := connection.Execute(ctx, path, args)
		if err != nil {
			return chainStaged{err: err}
		}
		file, err := saveChain(path, args[1]+"-"+args[2], chain)
		return chainStaged{file, err}
	})
}

func (m *frame) acceptChain(path string, v chainStaged) tea.Cmd {
	w := m.workspaces[path]
	w.management.busy = false
	if v.err != nil {
		w.management.output = "REFUSED: " + safe(v.err.Error())
		return nil
	}
	command := connection.CommandLine([]string{"throw", v.file, "--allow-dangerous"})
	w.management.output = "Chain staged: " + v.file + "\nHovel command:\n" + command + "\nEnter in Hovel to review; after completion, Alt+B shows connections."
	if path != m.active {
		return nil
	}
	cmd := m.openCLI()
	tab := w.cli
	if !tab.pending && (tab.host == nil || !emptyHovelPrompt(tab.screen)) {
		tab.notice = "Chain staged · Hovel input retained · Alt+B for command"
		return cmd
	}
	tab.prefill, tab.prefillBy = command, time.Now().Add(10*time.Second)
	return cmd
}

// ponytail: recognize the pinned Hovel primary prompt only; use an upstream
// prefill API when available. Config/confirmation prompts and drafts are refused.
var hovelPrimaryPrompt = regexp.MustCompile(`^h0v3l(?:>| \(.* \| steps:[0-9]+ targets:[0-9]+\) >| \[op:[^\]]+\]>| \[[^\]]+ \| steps:[0-9]+ targets:[0-9]+\] >) $`)

func emptyHovelPrompt(s ptyhost.Snapshot) bool {
	if s.Exited || s.Err != nil || !s.Visible || s.ScrollOffset != 0 {
		return false
	}
	lines := strings.Split(ansi.Strip(s.Screen), "\n")
	if s.Cursor.Y < 0 || s.Cursor.Y >= len(lines) {
		return false
	}
	line := strings.TrimRight(lines[s.Cursor.Y], " ") + " "
	return hovelPrimaryPrompt.MatchString(line) && s.Cursor.X == ansi.StringWidth(line)
}

func (tab *cliTab) prepareChainInput() {
	if tab.prefill == "" {
		return
	}
	s := tab.screen
	if s.Exited || time.Now().After(tab.prefillBy) {
		tab.prefill, tab.setupInput, tab.setupWait = "", "", ""
		tab.notice = "Chain staged · Hovel input retained · Alt+B for command"
		return
	}
	lines := strings.Split(ansi.Strip(s.Screen), "\n")
	if s.Cursor.Y < 0 || s.Cursor.Y >= len(lines) || !s.Visible || s.ScrollOffset != 0 {
		return
	}
	line := strings.TrimRight(lines[s.Cursor.Y], " ")
	if tab.setupInput != "" {
		prefix, ok := strings.CutSuffix(line, tab.setupInput)
		if ok && hovelPrimaryPrompt.MatchString(prefix) && s.Cursor.X == ansi.StringWidth(line) {
			// Only these two fixed context-selection commands are submitted.
			// The throw itself is always left for the operator's Enter.
			if err := tab.host.Send(uv.KeyPressEvent{Code: uv.KeyEnter}); err != nil {
				tab.prefill = ""
				tab.notice = "Chain staged · input not sent · Alt+B for command"
			}
			tab.setupInput = ""
		}
		return
	}
	inOperation := line == "h0v3l [op:burrow]>" || strings.HasPrefix(line, "h0v3l [burrow/")
	inChain := strings.HasPrefix(line, "h0v3l [burrow/burrow-chain |")
	if !emptyHovelPrompt(s) || (tab.setupWait == "operation" && !inOperation) || (tab.setupWait == "chain" && !inChain) {
		return
	}
	tab.setupWait = ""
	input := tab.prefill
	if !inChain {
		input = "op use burrow"
		tab.setupWait = "operation"
		if inOperation {
			input = "chain use burrow-chain"
			tab.setupWait = "chain"
		}
		tab.setupInput = input
	} else {
		tab.prefill = ""
		tab.notice = "Chain ready · Enter reviews · Alt+B connections"
	}
	if err := tab.host.Send(input); err != nil {
		tab.prefill, tab.setupInput, tab.setupWait = "", "", ""
		tab.notice = "Chain staged · input not sent · Alt+B for command"
	}
}

type shellEntry struct {
	workspace int
	tab       *cliTab
	files     *ui
}

func (m *frame) shellEntries() []shellEntry {
	var entries []shellEntry
	for i, path := range m.paths {
		w := m.workspaces[path]
		entries = append(entries, shellEntry{workspace: i})
		if w == nil {
			continue
		}
		for _, tab := range w.shells {
			entries = append(entries, shellEntry{workspace: i, tab: tab})
		}
		for _, files := range w.fileViews {
			entries = append(entries, shellEntry{workspace: i, files: files})
		}
	}
	return entries
}

func (w *workspaceView) terminals() []*cliTab { return append([]*cliTab{w.cli}, w.shells...) }
func (w *workspaceView) ownsTerminal(tab *cliTab) bool {
	return w.cli == tab || slices.Contains(w.shells, tab)
}
func (w *workspaceView) removeShell(tab *cliTab) {
	w.shells = slices.DeleteFunc(w.shells, func(s *cliTab) bool { return s == tab })
	// Numbers are workspace-local positions; asynchronous results use pointers,
	// never these reusable labels, to identify their terminal.
	for i, shell := range w.shells {
		shell.id = strconv.Itoa(i + 1)
	}
	if w.shell == tab {
		w.shell = nil
		if w.tab == "shell" {
			w.tab, w.focus = "", "prompt"
		}
		if len(w.shells) > 0 {
			w.shell = w.shells[len(w.shells)-1]
		}
	}
}
func (tab *cliTab) label() string {
	if tab.logs != nil {
		return tab.connection
	}
	if tab.editor != nil {
		return "Edit " + tab.editor.profile.Name
	}
	return tab.connection + " #" + tab.id
}
func (tab *cliTab) state() string {
	if tab.screen.Shared != nil && !tab.pending {
		return tab.screen.Shared.Shell.State
	}
	if tab.pending {
		if tab.host == nil {
			return "opening"
		}
		return "closing"
	}
	if tab.screen.Exited {
		return "exited"
	}
	if tab.error != "" {
		return "failed"
	}
	return "running"
}

func (m *frame) resumeShell(tab *cliTab) {
	w := m.current()
	m.selection = nil
	w.shell, w.tab, w.focus = tab, "shell", "terminal"
	for i, entry := range m.shellEntries() {
		if entry.tab == tab {
			m.revealShell(i)
			break
		}
	}
	if tab.host != nil && !tab.pending && !m.invalidGeometry {
		r := m.terminalBounds()
		if err := tab.host.Send(image.Pt(r.Dx(), r.Dy())); err != nil {
			tab.error = safe(err.Error())
		}
	}
}

func (m *frame) shellControl(args []string) tea.Cmd {
	w := m.current()
	w.management.busy = false
	w.management.outputOffset = 0
	if args[0] == "shells" && !m.demo {
		return m.discoverShells(m.active, true)
	}
	if args[0] == "shells" {
		var lines []string
		for _, tab := range w.shells {
			state := tab.state()
			if state == "running" {
				state = "background"
				if w.tab == "shell" && w.shell == tab {
					state = "active"
				}
			}
			lines = append(lines, "Shell "+tab.label()+" · "+state+" · demo / not recorded")
		}
		w.management.output = "No retained shell tabs in this workspace."
		if len(lines) > 0 {
			w.management.output = strings.Join(lines, "\n")
		}
		return nil
	}
	tab := w.shell
	if len(args) == 2 {
		tab = nil
		for _, candidate := range w.shells {
			if candidate.id == args[1] {
				tab = candidate
				break
			}
		}
	}
	if tab == nil {
		w.management.output = "REFUSED: no matching shell tab in this workspace; use shells."
		return nil
	}
	if args[0] == "resume" {
		m.resumeShell(tab)
		w.management.output = "SSH shell selected (" + tab.label() + ") · " + tab.state() + "."
		return nil
	}
	if args[0] == "shell-control" {
		m.resumeShell(tab)
		return m.controlShell(tab)
	}
	if args[0] == "shell-detach" {
		return m.detachShell(tab)
	}
	return m.closeShellTab(tab)
}

func (w *workspaceView) activeTerminal() *cliTab {
	if w.tab == "shell" {
		return w.shell
	}
	return w.cli
}

func (m *frame) openShell(name string) tea.Cmd {
	return m.openSharedShell(name, "")
}

func (m *frame) cycleShell(delta int) {
	w := m.current()
	if len(w.shells) == 0 {
		return
	}
	i := slices.Index(w.shells, w.shell)
	if w.tab != "shell" {
		i = -1
		if delta < 0 {
			i = 0
		}
	}
	m.resumeShell(w.shells[(i+delta+len(w.shells))%len(w.shells)])
}

func (m *frame) cycleTab(delta int) {
	w := m.current()
	total := len(w.shells) + len(w.fileViews) + len(w.followViews) + 2
	i := 0
	if w.tab == "hovel" {
		i = 1
	} else if w.tab == "shell" {
		i = slices.Index(w.shells, w.shell) + 2
	} else if w.tab == "files" {
		i = slices.Index(w.fileViews, w.file) + len(w.shells) + 2
	} else if w.tab == "follow" {
		i = slices.Index(w.followViews, w.follow) + len(w.shells) + len(w.fileViews) + 2
	}
	i = (i + delta + total) % total
	w.tab = ""
	if i == 1 {
		w.tab = "hovel"
	} else if i >= len(w.shells)+len(w.fileViews)+2 {
		m.selectFollow(w.followViews[i-len(w.shells)-len(w.fileViews)-2])
	} else if i >= len(w.shells)+2 {
		m.selectFileTab(w.fileViews[i-len(w.shells)-2])
	} else if i >= 2 {
		m.resumeShell(w.shells[i-2])
	}
	w.focus = "tabs"
}

func (m *frame) closeShellTab(tab *cliTab) tea.Cmd {
	w, path := m.current(), m.active
	if tab != nil && tab.editor != nil {
		w.management.output = "Finish the Vim edit with :wq to save or :q! to discard"
		m.resumeShell(tab)
		return nil
	}
	if tab == nil || tab.pending {
		w.management.busy = false
		w.management.output = "No ready local shell to close."
		return nil
	}
	w.management.busy = false
	if tab.logs == nil && w.shell == tab && w.tab == "shell" {
		w.tab, w.focus = "", "prompt"
	}
	if tab.session != "" {
		return m.closeSharedShell(tab)
	}
	w.management.output = "Closing local SSH shell…"
	if tab.host == nil {
		w.removeShell(tab)
		return nil
	}
	tab.pending = true
	lifetime := m.terminals
	return m.dispatch(path, func() tea.Msg {
		if !lifetime.begin() {
			return nil
		}
		defer lifetime.jobs.Done()
		if tab.logs == nil {
			a, err := launch.BeginAudit(path, "shell-close "+tab.connection, tab.connection, map[string]string{"frontendShell": tab.id})
			tab.host.Close()
			logErr := a.Record("closed", map[string]string{"scope": "frontend shell only; connection retained"})
			lifetime.recordAuditError(errors.Join(err, logErr))
		} else {
			tab.host.Close()
		}
		return cliClosed{tab}
	})
}

func (m *frame) terminalBounds() image.Rectangle {
	left, right := m.columns()
	return image.Rect(left+2, 3, max(left+3, m.width-right-2), max(4, m.height-2))
}
func (m *frame) terminalFocused() bool {
	w := m.current()
	return m.modal == "" && !w.activeUI().help && !w.activeUI().quitting && (w.tab == "hovel" || w.tab == "shell") && w.focus == "terminal"
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
		screen := tab.host.Snapshot()
		if screen.Exited && tab.auditDone != nil {
			if err := <-tab.auditDone; err != nil {
				screen.Err = errors.Join(screen.Err, err)
			}
		}
		return cliScreen{tab, screen}
	})
}
func (m *frame) terminalResult(path string, msg tea.Msg) tea.Cmd {
	w := m.workspaces[path]
	switch v := msg.(type) {
	case cliOpened:
		if !w.ownsTerminal(v.tab) {
			if v.host != nil {
				return func() tea.Msg { v.host.Close(); return nil }
			}
			return nil
		}
		v.tab.pending = false
		if v.err != nil {
			v.tab.error = "REFUSED: " + safe(v.err.Error())
			if v.tab.editor != nil && v.tab.editor.directory != "" {
				v.tab.error += "; draft retained at " + safe(v.tab.editor.directory)
			}
			if v.tab.connection != "" {
				if v.tab.logs != nil {
					w.restoreLogs(v.tab)
				} else if v.tab.session == "" {
					w.removeShell(v.tab)
				}
				w.management.output = v.tab.error
			}
			return nil
		}
		v.tab.host = v.host
		v.tab.screen = v.host.Snapshot()
		created := v.tab.session == "" && v.tab.screen.Shared != nil
		if shared := v.tab.screen.Shared; shared != nil {
			v.tab.session = shared.Shell.ID
			if w.management.output == "Opening retained SSH shell ("+v.tab.label()+")…" {
				w.management.output = "Retained SSH shell attached (" + v.tab.label() + "); shell survives detach and Keep running."
			}
		}
		if v.tab.connection != "" && w.management.output == "Opening retained SSH shell ("+v.tab.label()+")…" {
			w.management.output = "Local SSH shell started (" + v.tab.label() + "); output is in its shell tab."
		}
		r := m.terminalBounds()
		var geometry any = image.Pt(r.Dx(), r.Dy())
		if created {
			geometry = ptyhost.Control(image.Pt(r.Dx(), r.Dy()))
		}
		if m.invalidGeometry {
			v.tab.error = invalidTerminalGeometry
		} else if err := v.host.Send(geometry); err != nil {
			v.tab.error = safe(err.Error())
		} else if v.tab.error == invalidTerminalGeometry {
			v.tab.error = ""
		}
		return m.readCLI(path, v.tab)
	case cliScreen:
		if !w.ownsTerminal(v.tab) {
			return nil
		}
		if v.tab.connection != "" && v.tab.pending {
			return nil
		} // Explicit close reports after reaping.
		v.tab.screen = v.screen
		v.tab.prepareChainInput()
		if v.tab.connection != "" && v.screen.Exited {
			if v.tab.logs != nil {
				w.restoreLogs(v.tab)
				return nil
			}
			if v.tab.editor != nil {
				v.tab.pending = true
				return m.dispatch(path, finishProfileEditor(path, v.tab))
			}
			w.removeShell(v.tab)
			w.management.output = "Local SSH shell exited (" + v.tab.label() + "); connection retained if still live."
			if v.screen.Shared != nil {
				w.management.output = "Retained SSH shell exited (" + v.tab.label() + "); connection retained if still live."
			}
			if v.screen.Err != nil {
				w.management.output = fmt.Sprintf("SSH shell ended (%s): %s. Inspect the connection before reopening.", v.tab.label(), safe(v.screen.Err.Error()))
			}
			if v.tab.host != nil {
				return func() tea.Msg { v.tab.host.Close(); return nil }
			}
			return nil
		}
		if v.screen.Err != nil && v.screen.Shared == nil {
			v.tab.error = "CLI ended or input failed; inspect Hovel history before repeating work"
			if v.tab.connection != "" {
				v.tab.error = "SSH terminal: " + safe(v.screen.Err.Error())
			}
		}
		if !v.screen.Exited {
			return m.readCLI(path, v.tab)
		}
	case cliClosed:
		if slices.Contains(w.shells, v.tab) {
			if v.tab.logs != nil {
				w.restoreLogs(v.tab)
				return nil
			}
			w.removeShell(v.tab)
			w.management.output = "Local SSH shell closed (" + v.tab.label() + "); connection retained."
		}
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
	tab := m.current().activeTerminal()
	if tab == nil || tab.pending || tab.host == nil {
		return
	}
	if press, ok := event.(uv.KeyPressEvent); ok && press.Code == 'c' && press.Mod == uv.ModCtrl && tab == m.current().cli && m.attempt != nil && m.attempt.chain && m.attempt.path == m.active {
		m.attempt.cancel()
	}
	// A user editing during startup takes precedence over an automatic prefill.
	if _, ok := event.(uv.KeyPressEvent); ok {
		tab.prefill, tab.notice, tab.setupInput, tab.setupWait = "", "", "", ""
	}
	if _, ok := event.(string); ok {
		tab.prefill, tab.notice, tab.setupInput, tab.setupWait = "", "", "", ""
	}
	if err := tab.host.Send(event); err != nil {
		tab.error = safe(err.Error())
	} else if tab.screen.Shared != nil {
		switch event.(type) {
		case uv.KeyPressEvent, string:
			tab.error = ""
		}
	}
	tab.screen = tab.host.Snapshot()
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
