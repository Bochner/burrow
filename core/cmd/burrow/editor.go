package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Bochner/burrow/core/connection"
	ptyhost "github.com/Bochner/burrow/core/terminal"
)

// Edits are staged separately from the shared collection. The existing profile
// command owns validation, revision checks, atomic replacement and audit history.
type profileEdit struct {
	profile    connection.Profile
	collection connection.Collection
	directory  string
	original   []byte
}
type profileEditorOpened struct {
	opened cliOpened
	edit   *profileEdit
}
type profileEditorFinished struct {
	tab    *cliTab
	result any
	err    error
	notice string
}

func (m *frame) openProfileEditor(profile connection.Profile, collection connection.Collection) tea.Cmd {
	w := m.current()
	for _, tab := range w.shells {
		if tab.editor != nil && tab.editor.profile.Name == profile.Name && tab.editor.collection.Path == collection.Path {
			m.resumeShell(tab)
			return nil
		}
	}
	if m.invalidGeometry {
		w.management.output = invalidTerminalGeometry
		return nil
	}
	edit := &profileEdit{profile: profile, collection: collection}
	tab := &cliTab{id: fmt.Sprint(len(w.shells) + 1), connection: profile.Name, pending: true, editor: edit}
	w.shells = append(w.shells, tab)
	m.resumeShell(tab)
	workspace, bounds, lifetime, plain := m.active, m.terminalBounds(), m.terminals, m.noColor
	return m.dispatch(workspace, func() tea.Msg {
		if !lifetime.begin() {
			return profileEditorOpened{cliOpened{tab: tab, err: context.Canceled}, edit}
		}
		prepared := *edit
		command, err := prepareProfileEditor(&prepared, plain)
		var host *ptyhost.Host
		if err == nil {
			host, err = ptyhost.StartWithScrollback(lifetime.context, command, bounds.Dx(), bounds.Dy(), 1000)
		}
		if err != nil {
			lifetime.jobs.Done()
		} else {
			go func() { defer lifetime.jobs.Done(); <-host.Done() }()
		}
		return profileEditorOpened{cliOpened{tab, host, err}, &prepared}
	})
}

func prepareProfileEditor(edit *profileEdit, plain bool) (*exec.Cmd, error) {
	vim, err := exec.LookPath("vim")
	if err != nil {
		return nil, fmt.Errorf("Vim is not installed; install Vim with syntax support to edit saved connections")
	}
	dir, err := os.MkdirTemp("", "burrow-profile-edit-")
	if err != nil {
		return nil, err
	}
	edit.directory = dir
	edit.original, err = json.MarshalIndent(edit.profile, "", "  ")
	if err != nil {
		return nil, err
	}
	edit.original = append(edit.original, '\n')
	if err = os.WriteFile(filepath.Join(dir, "connection.json"), edit.original, 0600); err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(dir, "vimrc"), []byte(profileVimrc(plain)), 0600); err != nil {
		return nil, err
	}
	cmd := exec.Command(vim, "-N", "-u", filepath.Join(dir, "vimrc"), "-i", "NONE", "-n", "--", filepath.Join(dir, "connection.json"))
	cmd.Dir = dir
	return cmd, nil
}

func profileVimrc(plain bool) string {
	var b strings.Builder
	b.WriteString("set nocompatible nomodeline noexrc noloadplugins\nlet &runtimepath=$VIMRUNTIME\nset packpath=\nset number expandtab shiftwidth=2 softtabstop=2 tabstop=2\nset backspace=indent,eol,start hidden\nset noswapfile nobackup nowritebackup noundofile\nset mouse=a ttymouse=sgr scrolloff=3 sidescrolloff=3\nset conceallevel=0\nlet g:vim_json_conceal=0\nfiletype plugin indent on\nset background=dark\n")
	if plain {
		b.WriteString("syntax off\n")
		return b.String()
	}
	b.WriteString("if has('termguicolors')\n set termguicolors\nendif\nsyntax enable\n")
	for _, role := range []struct {
		name     string
		style    lipgloss.Style
		fallback int
	}{
		{"Normal", pageStyle, 253}, {"LineNr", secondary, 145}, {"Comment", secondary, 145}, {"jsonKeyword", heading, 111}, {"jsonString", successStyle, 150}, {"String", successStyle, 150}, {"Number", numberStyle, 216}, {"Boolean", keywordStyle, 183}, {"Constant", numberStyle, 216}, {"Statement", heading, 111}, {"Special", infoStyle, 116}, {"Delimiter", secondary, 145}, {"Error", errorStyle, 210},
	} {
		r, g, bl, _ := role.style.GetForeground().RGBA()
		fmt.Fprintf(&b, "highlight %s guifg=#%02x%02x%02x guibg=%s ctermfg=%d ctermbg=235\n", role.name, r>>8, g>>8, bl>>8, baseColor, role.fallback)
	}
	return b.String()
}

func editedProfileArgs(workspace string, edit *profileEdit) ([]string, error) {
	root, err := os.OpenRoot(edit.directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open("connection.json")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return nil, err
	}
	if len(data) > 65536 {
		return nil, fmt.Errorf("edited connection exceeds 64 KiB")
	}
	if bytes.Equal(data, edit.original) {
		return nil, nil
	}
	var p connection.Profile
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&p); err != nil {
		return nil, fmt.Errorf("invalid connection JSON: %w", err)
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("expected one JSON connection")
	}
	if p.Name != edit.profile.Name {
		return nil, fmt.Errorf("keep the connection name unchanged; create a separate profile to rename it")
	}
	if !p.AgentExplicit && p.Agent != "" {
		return nil, fmt.Errorf("agent requires agentExplicit: true")
	}
	args := append([]string{"profile", "edit"}, p.Args()[1:]...)
	args = append(args, "--collection", edit.collection.Path, "--revision", edit.collection.Revision, "--yes")
	if err = connection.ValidateCommand(workspace, args); err != nil {
		return nil, err
	}
	return args, nil
}

func finishProfileEditor(workspace string, tab *cliTab) tea.Cmd {
	edit, exitErr := tab.editor, tab.screen.Err
	return func() tea.Msg {
		result := profileEditorFinished{tab: tab}
		args, err := editedProfileArgs(workspace, edit)
		if exitErr != nil {
			err = fmt.Errorf("editor did not exit successfully: %w", exitErr)
		}
		if err == nil && args != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			result.result, err = connection.Execute(ctx, workspace, args)
		}
		if err != nil {
			result.err = fmt.Errorf("%w; edited draft retained at %s", err, filepath.Join(edit.directory, "connection.json"))
			return result
		}
		result.notice = "Editor closed without changes; saved connection unchanged"
		if args != nil {
			result.notice = "Saved connection updated; active SSH connections unchanged"
		}
		// Only this editor's verified private staging directory is removed.
		if err = os.RemoveAll(edit.directory); err != nil {
			result.notice += "; temporary draft cleanup failed: " + err.Error()
		}
		return result
	}
}
