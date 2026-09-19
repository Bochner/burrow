package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/Bochner/burrow/core/connection"
	"github.com/Bochner/burrow/core/launch"
	"github.com/charmbracelet/x/ansi"
)

func formTheme(bool) *huh.Styles {
	s := huh.ThemeCatppuccin(true)
	s.Focused.Title = accent
	s.Focused.Description = secondary.Transform(func(text string) string { return (ui{}).semanticText(text) })
	s.Blurred.Title = accent
	s.Blurred.Description = s.Focused.Description
	s.Focused.ErrorMessage = errorStyle
	s.Focused.FocusedButton = selectedStyle.Padding(0, 1).Transform(func(s string) string { return "[› " + s + "]" })
	s.Focused.BlurredButton = secondary.Padding(0, 1).Transform(func(s string) string { return "[  " + s + "]" })
	s.Focused.SelectSelector = accent.SetString("› ")
	s.Blurred.Base = s.Blurred.Base.BorderLeft(false).PaddingLeft(0)
	s.Focused.Base = s.Focused.Base.BorderLeft(false).PaddingLeft(0)
	return s
}
func baseForm(groups ...*huh.Group) *huh.Form {
	keys := huh.NewDefaultKeyMap()
	keys.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"))
	keys.Confirm.Toggle = key.NewBinding(key.WithKeys("tab", "shift+tab", "left", "right"), key.WithHelp("Tab / ←→", "choose"))
	keys.Confirm.Next = key.NewBinding(key.WithKeys("enter"))
	keys.Confirm.Prev = key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "back"))
	keys.Confirm.Accept.SetKeys()
	keys.Confirm.Reject.SetKeys()
	return huh.NewForm(groups...).WithAccessible(false).WithKeyMap(keys)
}
func newForm(groups ...*huh.Group) *huh.Form {
	return baseForm(groups...).WithTheme(huh.ThemeFunc(formTheme))
}
func confirmForm(title, description, accept, reject string) *huh.Form {
	f := baseForm(huh.NewGroup(huh.NewConfirm().Key("approved").Title(title).Description(description).Affirmative(accept).Negative(reject).WithButtonAlignment(lipgloss.Center)))
	f.WithTheme(huh.ThemeFunc(func(dark bool) *huh.Styles {
		s := formTheme(dark)
		s.Focused.Title = s.Focused.Title.Align(lipgloss.Center)
		s.Focused.Description = s.Focused.Description.Align(lipgloss.Center)
		return s
	}))
	return f
}
func publicPrompt(p connection.Prompt) string {
	lines := strings.Split(p.Text, "\n")
	for i := range lines {
		lines[i] = safe(lines[i])
	}
	return strings.Join(lines, "\n")
}
func promptForm(p connection.Prompt, visible bool) *huh.Form {
	if !p.Secret {
		return confirmForm("Verify host key", publicPrompt(p), "Trust host", "Reject")
	}
	input := huh.NewInput().Key("answer").Title(publicPrompt(p)).EchoMode(huh.EchoModeNone).CharLimit(4096)
	if visible {
		input.EchoMode(huh.EchoModeNormal).Description("Visible while typing · not saved").Placeholder("Enter password or passphrase")
	}
	return newForm(huh.NewGroup(input)).WithShowHelp(!visible)
}
func promptAnswer(f *huh.Form, secret bool) []byte {
	if secret {
		return []byte(f.GetString("answer"))
	}
	if f.GetBool("approved") {
		return []byte("yes")
	}
	return []byte("no")
}

// Required and optional details stay editable in the same form. Only non-secret
// paths/settings become command arguments; all validation still reaches Parse.
type connectDetails struct{ name, host, user, port, key, config, jump, agent, proxy string }

func (d *connectDetails) args() []string {
	args := []string{"connect", d.name, d.host, d.user}
	for _, option := range [][2]string{{"--port", d.port}, {"--key", d.key}, {"--ssh-config", d.config}, {"--jump", d.jump}, {"--agent", d.agent}, {"-proxy", d.proxy}} {
		if option[1] != "" {
			args = append(args, option[0], option[1])
		}
	}
	return args
}

var connectFields = []struct{ key, title string }{
	{"host", "Host / IP *"}, {"port", "SSH port"}, {"user", "Username *"}, {"name", "Connection name *"},
	{"key", "SSH key path"}, {"proxy", "SOCKS proxy port"}, {"jump", "Jump host"}, {"agent", "Agent socket"}, {"config", "SSH config path"},
}

var workspaceFields = []struct{ key, title string }{
	{"workspace-name", "Workspace name"}, {"workspace-location", "Location (optional)"},
}

func workspaceParent(location string) (string, error) {
	if location == "" {
		root := os.Getenv("XDG_DATA_HOME")
		if !filepath.IsAbs(root) {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("Set a location: home directory is unavailable")
			}
			root = filepath.Join(home, ".local", "share")
		}
		location = filepath.Join(root, "burrow", "workspaces")
	} else if location == "~" || strings.HasPrefix(location, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("Use an absolute location: home directory is unavailable")
		}
		location = home + strings.TrimPrefix(location, "~")
	}
	if err := canonicalWorkspace(location); err != nil {
		return "", err
	}
	return location, nil
}

func workspaceDestination(name, location string) (string, error) {
	if name == "" || name == "." || strings.Contains(name, "..") {
		return "", fmt.Errorf("Enter a workspace name, not a path (for example lab)")
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '.' {
			return "", fmt.Errorf("Workspace name: use letters, digits, hyphens, underscores or dots; no slashes")
		}
	}
	parent, err := workspaceParent(location)
	if err != nil {
		return "", err
	}
	path := filepath.Join(parent, name)
	// Even the shortest connection name must fit the existing socket contract.
	if _, err := launch.ConnectionPath(path, "a"); err != nil {
		return "", fmt.Errorf("Workspace path is too long for SSH sockets; shorten the name or location")
	}
	return path, nil
}

func detailsForm(workspace string, d *connectDetails) *huh.Form {
	keys := huh.NewDefaultKeyMap()
	keys.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"))
	keys.Input.Prev = key.NewBinding(key.WithKeys("shift+tab", "up"), key.WithHelp("↑ / shift+tab", "back"))
	keys.Input.Next = key.NewBinding(key.WithKeys("enter", "tab", "down"), key.WithHelp("↓ / tab", "next"))
	required := func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("Required")
		}
		return nil
	}
	input := func(key, title, example string, value *string) *huh.Input {
		return huh.NewInput().Key(key).Title(title + ": ").Placeholder(example).Inline(true).Value(value).CharLimit(2048)
	}
	return newForm(huh.NewGroup(
		input("host", "Host / IP *", "192.0.2.10 or my-server", &d.host).Validate(required),
		input("port", "SSH port", "22 (blank uses SSH config)", &d.port).Validate(func(s string) error {
			if s == "" {
				return nil
			}
			n, e := strconv.Atoi(s)
			if e != nil || n < 1 || n > 65535 {
				return fmt.Errorf("Port must be 1–65535")
			}
			return nil
		}),
		input("user", "Username *", "ubuntu (- uses SSH config)", &d.user).Validate(required),
		input("name", "Connection name *", "gateway", &d.name).Validate(func(s string) error { _, e := launch.ConnectionPath(workspace, s); return e }),
		input("key", "SSH key path", "/home/you/.ssh/id_ed25519", &d.key),
		input("proxy", "SOCKS proxy port", "Off · enter 9050 or another port", &d.proxy).Validate(func(s string) error {
			if s == "" {
				return nil
			}
			n, e := strconv.Atoi(s)
			if e != nil || n < 1 || n > 65535 {
				return fmt.Errorf("Proxy port must be 1–65535")
			}
			return nil
		}),
		input("jump", "Jump host", "admin@bastion:22 (optional)", &d.jump),
		input("agent", "Agent socket", "/run/user/1000/ssh-agent.socket (optional)", &d.agent),
		input("config", "SSH config path", "/home/you/.ssh/config (optional)", &d.config).Validate(func(s string) error {
			copy := *d
			copy.config = s
			_, _, e := connection.Parse(workspace, copy.args()[1:])
			return e
		}),
	).Title("Connection details").Description("* Required · blank SOCKS port = off; other blanks use SSH defaults\n↑↓ / Tab / Shift+Tab to edit · Enter advances to review")).WithKeyMap(keys)

}

// Huh v2.0.3 returns Bubble Tea's private sequenceMsg as well as BatchMsg.
// Preserve sequence ordering while stamping every leaf with its form identity;
// returning an unwrapped sequence would let late clipboard/navigation messages
// enter a replacement form. Convertible []Cmd is the pinned Tea representation.
func formCommand(path string, epoch uint64, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		if msg != nil {
			v := reflect.ValueOf(msg)
			if v.Type().ConvertibleTo(reflect.TypeOf([]tea.Cmd{})) {
				commands := v.Convert(reflect.TypeOf([]tea.Cmd{})).Interface().([]tea.Cmd)
				wrapped := make([]tea.Cmd, len(commands))
				for i, c := range commands {
					wrapped[i] = formCommand(path, epoch, c)
				}
				if _, ok := msg.(tea.BatchMsg); ok {
					return tea.Batch(wrapped...)()
				}
				return tea.Sequence(wrapped...)()
			}
		}
		return formMessage{path, epoch, msg}
	}
}

type formMessage struct {
	path  string
	epoch uint64
	msg   tea.Msg
}
type authQuestion struct {
	prompt connection.Prompt
	answer chan []byte
}
type authQuestionReady struct {
	attempt  *authAttempt
	question authQuestion
}
type authFinished struct {
	attempt *authAttempt
	result  any
	err     error
}
type authAttempt struct {
	path      string
	cancel    context.CancelFunc
	ctx       context.Context
	questions chan authQuestion
	done      chan struct{}
}

func waitQuestion(a *authAttempt) tea.Cmd {
	return func() tea.Msg {
		select {
		case q := <-a.questions:
			return authQuestionReady{a, q}
		case <-a.ctx.Done():
			return nil
		}
	}
}
func (m *frame) startAuthentication(args []string) tea.Cmd {
	path := m.active
	ctx, cancel := context.WithTimeout(m.terminals.context, 2*time.Minute)
	a := &authAttempt{path: path, ctx: ctx, cancel: cancel, questions: make(chan authQuestion), done: make(chan struct{})}
	m.attempt = a
	m.form = nil
	m.modal = "auth"
	m.question = nil
	m.authSpinner = spinner.New(spinner.WithSpinner(spinner.Dot))
	args = append([]string{}, args...)
	return tea.Batch(m.authSpinner.Tick, waitQuestion(a), func() tea.Msg {
		defer close(a.done)
		result, e := connection.ExecutePrompt(ctx, path, args, func(ctx context.Context, p connection.Prompt) ([]byte, error) {
			q := authQuestion{p, make(chan []byte)}
			select {
			case a.questions <- q:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			select {
			case answer := <-q.answer:
				return answer, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})
		cancel()
		return authFinished{a, result, e}
	})
}
func (m *frame) setForm(modal, title string, f *huh.Form) tea.Cmd {
	m.inputEpoch++
	m.modal, m.formTitle, m.form = modal, title, f
	m.sizeForm()
	return formCommand(m.active, m.inputEpoch, f.Init())
}
func (m *frame) sizeForm() {
	if m.form != nil {
		r := m.dialogBounds()
		width := max(1, r.Dx()-6)
		if m.modal == "auth" && m.question != nil && m.question.prompt.Secret {
			width = min(48, width)
		}
		m.form.WithWidth(width).WithHeight(max(1, r.Dy()-9))
	}
}
func (m *frame) dismissForm() {
	if m.modal == "quit" && m.quitClosing {
		return
	}
	if m.modal == "browse" && m.savedForm != nil {
		m.form, m.modal, m.formTitle = m.savedForm, m.savedModal, m.savedTitle
		m.savedForm = nil
		m.inputEpoch++
		m.sizeForm()
		return
	}
	m.form = nil
	m.details = nil
	m.downloadPlan = nil
	m.downloadMode = nil
	m.inputEpoch++
	m.modal = ""
	if m.attempt != nil {
		m.attempt.cancel()
		m.current().management.output = "Cancelling authentication; waiting for verified cleanup…"
	} else if m.commandArgs != nil {
		m.current().management.busy = false
	}
	m.commandArgs = nil
	m.question = nil
}
func (m *frame) updateForm(msg tea.Msg) tea.Cmd {
	if m.form == nil {
		return nil
	}
	if _, ok := msg.(tea.PasteMsg); ok && m.modal == "menu" {
		_, cmd := m.form.Update(msg)
		// Huh 2.0.3 updates filter text on paste but rebuilds options only on a key.
		_, refresh := m.form.Update(tea.KeyPressMsg{})
		return formCommand(m.active, m.inputEpoch, tea.Batch(cmd, refresh))
	}
	f, cmd := m.form.Update(msg)
	m.form = f.(*huh.Form)
	m.sizeForm()
	if m.form.State == huh.StateAborted {
		m.dismissForm()
		return nil
	}
	if m.form.State != huh.StateCompleted {
		return formCommand(m.active, m.inputEpoch, cmd)
	}
	completed := m.form
	m.form = nil
	m.inputEpoch++
	switch m.modal {
	case "quit":
		if m.quitChanged() {
			return m.openQuit()
		}
		if m.quitReview.count() > 0 && !completed.GetBool("approved") {
			return m.closeForQuit()
		}
		m.modal = ""
		if completed.GetBool("approved") {
			return tea.Quit
		}
	case "menu":
		return m.menuAction(completed.GetInt("action"))
	case "context":
		return m.resourceAction(completed.GetInt("action"))
	case "new":
		cmd := m.submitWorkspace()
		if !m.launchPending {
			return m.workspaceForm()
		}
		return cmd
	case "profile-menu":
		return m.profileAction(completed.GetString("command"))
	case "profile-save":
		name := completed.GetString("name")
		args := []string{"profile", "save", m.saveName, "--as", name, "--collection", m.saveCollection.Path, "--revision", m.saveCollection.Revision}
		m.saveName = ""
		if e := connection.ValidateCommand(m.active, args); e != nil {
			m.modal = ""
			return m.updateManagement(m.active, connectionResult{nil, e})
		}
		return m.reviewCommand(args)
	case "connect":
		args := m.details.args()
		m.details = nil
		return m.reviewCommand(args)
	case "review":
		if !completed.GetBool("approved") {
			m.downloadPlan = nil
			m.dismissForm()
			return nil
		}
		if m.downloadPlan != nil {
			return m.submitDownload()
		}
		args := append(append([]string{}, m.commandArgs...), "--yes")
		m.commandArgs = nil
		if args[0] == "run" || args[0] == "proxy" || args[0] == "profile" || args[0] == "tunnel" || args[0] == "tunc" || args[0] == "tund" {
			path := m.active
			parent := m.terminals.context
			m.modal = ""
			if connection.RunWaits(args) {
				m.current().management.output = "Run " + safe(args[2]) + ": waiting for completion and collecting output…"
			}
			return m.dispatch(path, func() tea.Msg {
				ctx, cancel := context.WithTimeout(parent, time.Minute)
				defer cancel()
				if connection.RunWaits(args) {
					ctx = parent
				}
				result, e := connection.Execute(ctx, path, args)
				return connectionResult{result, e}
			})
		}
		if args[0] == "close" {
			expected := m.closeTarget
			path := m.active
			m.modal = ""
			return m.dispatch(m.active, func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				result, e := connection.CloseReviewed(ctx, path, expected)
				return connectionResult{result, e}
			})
		}
		return m.startAuthentication(args)
	case "auth":
		answer := promptAnswer(completed, m.question.prompt.Secret)
		q, a := m.question, m.attempt
		m.question = nil
		return tea.Batch(m.authSpinner.Tick, waitQuestion(a), func() tea.Msg {
			select {
			case q.answer <- answer: // Copy belongs to the private transport until it returns.
			case <-a.ctx.Done():
				clear(answer)
			}
			return nil
		})
	case "browse":
		value := completed.GetString("path")
		if input, ok := m.savedForm.GetFocusedField().(*huh.Input); ok {
			switch m.savedModal {
			case "new":
				m.workspaceLocation = value
				input.Value(&m.workspaceLocation)
			case "connect":
				switch input.GetKey() {
				case "key":
					m.details.key = value
					input.Value(&m.details.key)
				case "config":
					m.details.config = value
					input.Value(&m.details.config)
				}
			}
		}
		m.form, m.modal, m.formTitle = m.savedForm, m.savedModal, m.savedTitle
		m.savedForm = nil
		m.sizeForm()
		return m.updateForm(nil)
	}
	return nil
}

type commandReview struct {
	result any
	epoch  uint64
	args   []string
	review string
	target connection.State
	err    error
}

func (m *frame) reviewCommand(args []string) tea.Cmd {
	if m.attempt != nil {
		return m.updateManagement(m.active, connectionResult{nil, fmt.Errorf("authentication cleanup is still pending; wait before connecting again")})
	}
	if len(args) == 1 {
		m.commandArgs = args
		m.details = &connectDetails{}
		return m.setForm("connect", "Connect · click a field or use ↑↓ / Tab", detailsForm(m.active, m.details))
	}
	if args[0] != "run" && args[0] != "proxy" && args[0] != "close" && args[0] != "profile" && args[0] != "tunnel" && args[0] != "tunc" && args[0] != "tund" {
		_, yes, e := connection.Parse(m.active, args[1:])
		if e != nil {
			m.dismissForm()
			return m.updateManagement(m.active, connectionResult{nil, e})
		}
		if yes {
			return m.startAuthentication(args)
		}
	}
	path := m.active
	parent := m.terminals.context
	m.commandArgs = args
	m.reviewText = ""
	m.inputEpoch++
	epoch := m.inputEpoch
	m.modal = "review"
	m.formTitle = "Resolving exact target…"
	return m.dispatch(path, func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, time.Minute)
		defer cancel()
		r := commandReview{epoch: epoch, args: args}
		if args[0] == "profile" && args[1] == "connect" {
			expanded, e := connection.ProfileConnect(ctx, path, args)
			if e != nil {
				r.err = e
				return r
			}
			r.args = expanded
			// Resolve for review even when --yes was supplied; authentication
			// must still travel through the private frontend path.
			_, yes, e := connection.Parse(path, expanded[1:])
			if e != nil {
				r.err = e
				return r
			}
			if yes {
				r.review = "approved-profile-connect"
				return r
			}
			args = expanded
		}
		if args[0] == "close" {
			r.target, r.review, r.err = connection.ReviewClose(ctx, path, args[1])
			return r
		}
		if connection.RunWaits(args) {
			ctx = parent
		}
		result, e := connection.Execute(ctx, path, args)
		r.err = e
		if e == nil {
			r.result = result
			if details, ok := result.(map[string]string); ok {
				r.review = details["review"]
				if args[0] == "run" && args[1] == "now" && details["id"] != "" {
					r.args = []string{"run", "launch", details["id"], "--collect"}
				}
				if details["digest"] != "" {
					r.args = append(r.args, "--review", details["digest"])
				}
				if details["revision"] != "" {
					r.args = append(r.args, "--revision", details["revision"], "--collection", details["collection"])
				}
			}
		}
		return r
	})
}

type browserReady struct{ form *huh.Form }

func (m *frame) browse() tea.Cmd {
	if m.form == nil {
		return nil
	}
	if m.modal == "new" && m.form.GetFocusedField().GetKey() == "workspace-name" {
		m.form.NextField()
	}
	input, ok := m.form.GetFocusedField().(*huh.Input)
	if !ok {
		return nil
	}
	field := input.GetKey()
	if m.modal != "new" && (m.modal != "connect" || (field != "key" && field != "config")) {
		return nil
	}
	m.savedForm, m.savedModal, m.savedTitle = m.form, m.modal, m.formTitle
	m.form = nil
	m.modal = "browse"
	m.formTitle = "Local paths · Esc returns to typed entry"
	m.inputEpoch++
	directory := m.savedModal == "new"
	return formCommand(m.active, m.inputEpoch, func() tea.Msg {
		home, _ := os.UserHomeDir()
		picker := huh.NewFilePicker().Key("path").Title("Select local path").CurrentDirectory(home).ShowHidden(true).FileAllowed(!directory).DirAllowed(directory).Picking(true)
		return browserReady{newForm(huh.NewGroup(picker))}
	})
}
func canonicalWorkspace(s string) error {
	if !filepath.IsAbs(s) || filepath.Clean(s) != s || strings.IndexFunc(s, unicode.IsControl) >= 0 || len(s) > 2048 {
		return fmt.Errorf("Use an absolute canonical path (no trailing / or ..).")
	}
	return nil
}
func (m *frame) formText() string {
	if m.modal == "auth" {
		bounds := m.dialogBounds()
		width := max(1, bounds.Dx()-6)
		u := &m.current().management
		if m.question == nil {
			return lipgloss.Place(width, max(1, bounds.Dy()-4), lipgloss.Center, lipgloss.Center, u.paint(warningStyle, m.authSpinner.View()+" Connecting…"))
		}
		if m.question.prompt.Secret && m.form != nil {
			lines := strings.Split(m.form.View(), "\n")
			for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
				lines = lines[:len(lines)-1]
			}
			body := lipgloss.JoinVertical(lipgloss.Center, u.paint(accent, m.formTitle), "", strings.Join(lines, "\n"))
			return lipgloss.Place(width, max(1, bounds.Dy()-6), lipgloss.Center, lipgloss.Center, body)
		}
	}
	text := centered(m.current().management.paint(accent, m.formTitle), m.dialogBounds().Dx()-6)
	if m.modal == "quit" {
		bounds := m.dialogBounds()
		v := scrollBody(m.quitSummary(), bounds.Dx()-6, max(1, bounds.Dy()-14), m.modalOffset)
		text += "\n\n" + v.View()
	}
	if m.modal == "review" && (m.reviewText != "" || m.downloadPlan != nil) {
		bounds := m.dialogBounds()
		body := m.current().management.semanticText(m.reviewText)
		if m.downloadPlan != nil {
			body = m.downloadRecap(bounds.Dx() - 6)
		}
		v := scrollBody(body, bounds.Dx()-6, max(1, bounds.Dy()-14), m.modalOffset)
		text += "\n\n" + v.View()
	}
	if m.form != nil {
		body := m.form.View()
		if m.modal == "new" {
			lines := strings.Split(body, "\n")
			for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
				lines = lines[:len(lines)-1]
			}
			body = strings.Join(lines, "\n")
		}
		if m.modal == "menu" {
			if _, ok := m.menu.Hovered(); !ok {
				body += "\nNo matching commands"
			}
		}
		if m.modal == "quit" || m.modal == "review" || (m.modal == "auth" && m.question != nil && !m.question.prompt.Secret) {
			lines := strings.Split(body, "\n")
			for i, line := range lines {
				lines[i] = centered(strings.TrimSpace(line), m.dialogBounds().Dx()-6)
			}
			body = strings.Join(lines, "\n")
		}
		text += "\n\n" + body
	}
	if m.modal == "new" {
		parent, err := workspaceParent(m.workspaceLocation)
		if err == nil {
			name := m.workspaceName
			if name == "" {
				name = "<name>"
			}
			text += "\n\n" + ansi.Wrap(m.current().management.paint(secondary, "Destination: "+safe(strings.TrimSuffix(parent, "/"))+"/"+safe(name)), m.dialogBounds().Dx()-6, "")
		}
		if m.launchPending {
			text += "\nLaunching and verifying…"
		}
		text += "\n" + ansi.Wrap(m.current().management.paint(errorStyle, m.launchError), m.dialogBounds().Dx()-6, "")
	}
	return text
}

// Mouse targets are derived from the actual Huh-rendered lines, never a second
// filtered menu or confirmation selection model.
func (m *frame) formControls(text string) map[string]int {
	targets := map[string]int{}
	for y, line := range strings.Split(text, "\n") {
		plain := ansi.Strip(line)
		if m.modal == "connect" || m.modal == "new" {
			fields := connectFields
			if m.modal == "new" {
				fields = workspaceFields
			}
			for _, field := range fields {
				if strings.HasPrefix(strings.TrimSpace(plain), field.title+":") {
					targets["field:"+field.key] = y
				}
			}
		}
		if m.modal == "menu" || m.modal == "context" {
			labels := menuActions
			if m.modal == "context" && m.contextMenu != nil {
				labels = m.contextMenu.labels
			}
			for i, label := range labels {
				if strings.Contains(plain, label) {
					targets[fmt.Sprintf("action:%d", i)] = y
				}
			}
		}
	}
	return targets
}

func quitForm() *huh.Form {
	return confirmForm("", "No connections to close. Daemon remains.\nFrontend-local terminals end when Burrow exits.", "Quit", "Keep working")
}

func (m *frame) stopAuthentication() {
	if m.attempt != nil {
		m.attempt.cancel()
		<-m.attempt.done
	}
}
