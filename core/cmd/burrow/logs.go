package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Bochner/burrow/core/launch"
	ptyhost "github.com/Bochner/burrow/core/terminal"
)

var toggleLogs = key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("Ctrl+N", "logs / return"))

type logsRequested struct{}
type logView struct {
	tab, focus string
	shell      *cliTab
	file       *ui
}

func (w *workspaceView) restoreLogs(tab *cliTab) {
	previous := tab.logs
	active := w.shell == tab && w.tab == "shell"
	w.removeShell(tab)
	if active {
		w.tab, w.focus, w.shell, w.file = previous.tab, previous.focus, previous.shell, previous.file
		if w.shell != nil && !w.ownsTerminal(w.shell) {
			w.shell = nil
			w.tab, w.focus = "", "prompt"
		}
		if w.file != nil && w.fileUI(w.file.files) == nil {
			w.file = nil
			w.tab, w.focus = "", "prompt"
		}
	}
}

func (m *frame) openLogs() tea.Cmd {
	w := m.current()
	for _, tab := range w.shells {
		if tab.logs != nil {
			if w.shell != tab || w.tab != "shell" {
				m.resumeShell(tab)
				return nil
			}
			if tab.pending {
				return nil
			}
			return m.closeShellTab(tab)
		}
	}
	if m.invalidGeometry {
		w.management.output = invalidTerminalGeometry
		return nil
	}
	tab := &cliTab{id: fmt.Sprint(len(w.shells) + 1), connection: "Logs", pending: true,
		logs: &logView{w.tab, w.focus, w.shell, w.file}}
	w.shells = append(w.shells, tab)
	m.resumeShell(tab)
	workspace, bounds, lifetime, plain := m.active, m.terminalBounds(), m.terminals, m.noColor
	return m.dispatch(workspace, func() tea.Msg {
		if !lifetime.begin() {
			return cliOpened{tab: tab, err: context.Canceled}
		}
		cmd, directory, err := prepareLogViewer(workspace, plain)
		var host *ptyhost.Host
		if err == nil {
			host, err = ptyhost.StartWithScrollback(lifetime.context, cmd, bounds.Dx(), bounds.Dy(), 1000)
		}
		if err != nil {
			os.RemoveAll(directory)
			lifetime.jobs.Done()
		} else {
			go func() { defer lifetime.jobs.Done(); <-host.Done(); os.RemoveAll(directory) }()
		}
		return cliOpened{tab, host, err}
	})
}

func prepareLogViewer(workspace string, plain bool) (*exec.Cmd, string, error) {
	vim, err := exec.LookPath("vim")
	if err != nil {
		return nil, "", fmt.Errorf("Vim is not installed; install Vim to view logs")
	}
	dir, err := os.MkdirTemp("", "burrow-log-view-")
	if err != nil {
		return nil, "", err
	}
	f, err := os.OpenFile(filepath.Join(dir, "operations.log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, dir, err
	}
	err = launch.AuditSnapshot(workspace, f)
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, dir, fmt.Errorf("log snapshot unavailable: %w", err)
	}
	raw, err := os.Open(filepath.Join(dir, "operations.log"))
	if err != nil {
		return nil, dir, err
	}
	notes, err := os.OpenFile(filepath.Join(dir, "notes.log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		raw.Close()
		return nil, dir, err
	}
	err = writeLogNotes(raw, notes, workspace)
	raw.Close()
	closeErr = notes.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, dir, err
	}
	if err = os.WriteFile(filepath.Join(dir, "vimrc"), []byte(logVimrc(plain)), 0600); err != nil {
		return nil, dir, err
	}
	cmd := exec.Command(vim, "-N", "-M", "-u", filepath.Join(dir, "vimrc"), "-i", "NONE", "-n", "--", filepath.Join(dir, "notes.log"))
	cmd.Dir = dir
	return cmd, dir, nil
}

func logVimrc(plain bool) string {
	var b strings.Builder
	b.WriteString(profileVimrc(plain))
	b.WriteString("set nomodeline readonly nomodifiable nowrap\nset laststatus=2\nset statusline=Burrow\\ logs\\ snapshot\\ ·\\ Ctrl+N/:q\\ return\\ ·\\ reopen\\ refreshes\n")
	if plain {
		return b.String()
	}
	b.WriteString(`
syntax clear
syntax case ignore
syntax match burrowCommand / -- \zs.*/
syntax match burrowLabel /^  \w\+:/
syntax match burrowNumber /\<\d\+\%(\.\d\+\)\?\>/
syntax match burrowSuccess /\<\%(complete\|completed\|connected\|removed\|closed\)\>/
syntax match burrowFailure /\<\%(failed\|refused\|lost\|cancelled\|incomplete\|unverified\)\>/
syntax match burrowPending /\<\%(attempt\|running\|connecting\|unknown\|partial\)\>/
syntax match burrowNotePath /\%(^  \%(Saved\|Destination\|Partial\): \)\@<=.*/
syntax match burrowNoteUser /\<[[:alnum:]_.-]\+\ze@/
syntax match burrowNoteHost /@\zs[[:alnum:].-]\+/
syntax match burrowNoteName /\%(^  Target: \)\@<=[^ (]\+/
syntax match burrowNotePort /:\zs\d\+\>/
syntax match burrowTimestamp /^\d\{4}-\d\d-\d\dT\S\+\ze -- /
highlight link burrowCommand Statement
highlight link burrowLabel Special
highlight link burrowNumber Number
highlight link burrowSuccess String
highlight link burrowFailure Error
highlight link burrowNotePath Comment
highlight link burrowNoteUser String
highlight link burrowNoteHost burrowHost
highlight link burrowNoteName burrowName
highlight link burrowNotePort burrowPort
highlight link burrowPending burrowPort
`)
	for _, role := range []struct {
		name     string
		style    lipgloss.Style
		fallback int
	}{
		{"burrowHost", hostStyle, 218}, {"burrowName", accent, 183}, {"burrowPort", warningStyle, 229}, {"burrowType", keywordStyle, 183},
	} {
		r, g, bl, _ := role.style.GetForeground().RGBA()
		fmt.Fprintf(&b, "highlight %s guifg=#%02x%02x%02x guibg=%s ctermfg=%d ctermbg=235\n", role.name, r>>8, g>>8, bl>>8, baseColor, role.fallback)
	}
	// The accepted blue role on the page base contrasts with the log background.
	fmt.Fprintf(&b, "highlight burrowTimestamp guifg=%s guibg=%s ctermfg=235 ctermbg=111 gui=bold cterm=bold\n", baseColor, blueColor)
	return b.String()
}
