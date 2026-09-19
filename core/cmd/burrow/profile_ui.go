package main

import (
	"context"
	"fmt"
	"image"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/Bochner/burrow/core/connection"
)

var resourceActions = key.NewBinding(key.WithKeys("shift+f10"), key.WithHelp("Shift+F10", "row actions"))

type resourceMenu struct {
	at         image.Point
	labels     []string
	state      connection.State
	profile    connection.Profile
	collection connection.Collection
}

func (m *frame) openResourceMenu(hit string, at image.Point) tea.Cmd {
	w := m.current()
	u := &w.management
	menu := &resourceMenu{at: at}
	if hit == "" {
		if w.focus == "saved" {
			for i, p := range u.profiles.Profiles {
				if p.Name == u.selectedProfile {
					hit = fmt.Sprintf("profile:%d", i)
					break
				}
			}
		} else if w.focus == "resources" {
			for i, s := range u.connections {
				if s.Name == w.selected {
					hit = fmt.Sprintf("resource:%d", i)
					break
				}
			}
		}
	}
	if strings.HasPrefix(hit, "profile:") {
		i, err := strconv.Atoi(strings.TrimPrefix(hit, "profile:"))
		if err != nil || i < 0 || i >= len(u.profiles.Profiles) {
			return nil
		}
		menu.profile, menu.collection = u.profiles.Profiles[i], u.profiles
		menu.labels = []string{"Execute connection", "Edit in Vim"}
		u.selectedProfile = menu.profile.Name
		w.focus = "saved"
	} else if strings.HasPrefix(hit, "resource:") {
		i, err := strconv.Atoi(strings.TrimPrefix(hit, "resource:"))
		if err != nil || i < 0 || i >= len(u.connections) {
			return nil
		}
		menu.state = u.connections[i]
		if menu.state.State != "connected" || u.connectionError != "" {
			return nil
		}
		menu.labels = []string{"Enter file mode", "Enter Shell"}
		w.selected, w.focus = menu.state.Name, "resources"
	} else {
		return nil
	}
	m.selection = nil
	m.contextMenu = menu
	options := make([]huh.Option[int], len(menu.labels))
	for i, label := range menu.labels {
		options[i] = huh.NewOption(label, i)
	}
	m.menu = huh.NewSelect[int]().Key("action").Options(options...)
	name := menu.state.Name
	if menu.profile.Name != "" {
		name = menu.profile.Name
	}
	return m.setForm("context", safe(name), newForm(huh.NewGroup(m.menu)).WithShowHelp(false))
}

func (m *frame) resourceAction(i int) tea.Cmd {
	menu := m.contextMenu
	m.dismissForm()
	m.contextMenu = nil
	if menu == nil || i < 0 || i >= len(menu.labels) {
		return nil
	}
	if menu.profile.Name != "" {
		if i == 1 {
			return m.openProfileEditor(menu.profile, menu.collection)
		}
		return m.reviewCommand(menu.profile.Args())
	}
	for _, state := range m.current().management.connections {
		if state.Name == menu.state.Name && state.Creation == menu.state.Creation && state.Generation == menu.state.Generation && state.State == "connected" && m.current().management.connectionError == "" {
			if i == 1 {
				return m.openShell(state.Name)
			}
			return m.openFileTab(state.Name)
		}
	}
	m.current().management.output = "REFUSED: connection changed while the menu was open; select it again"
	return nil
}

type profilesReady struct {
	collection connection.Collection
	history    []string
	err        error
}

func (m *frame) showSaveOffer() tea.Cmd {
	w := m.current()
	if m.modal != "" || w.management.busy || len(w.saveOffers) == 0 {
		return nil
	}
	offer := w.saveOffers[0]
	w.saveOffers = w.saveOffers[1:]
	m.saveName, m.saveCollection = offer.name, offer.collection
	return m.setForm("profile-save", "Connected · save for a future session?", saveProfileForm(offer.name, offer.collection.Path))
}

func refreshProfiles(workspace string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		list, e := connection.Profiles(ctx, workspace)
		history, _ := connection.Execute(ctx, workspace, []string{"history"})
		lines, _ := history.([]string)
		return profilesReady{list, lines, e}
	}
}
func (m ui) suggestions() []string {
	if m.files != nil {
		values := []string{"reports", "report ", "get ", "mget ", "put ", "transfers", "transfer-cancel ", "downloads", "download-cancel ", "ls ", "cd ", "tree ", "pwd", "local", "local download ", "local upload ", "lcd ", "lcd upload ", "lls ", "lls upload ", "history", "back", "help", "quit"}
		for _, d := range m.downloads.Records {
			if d.State == "running" {
				values = append(values, "transfer-cancel "+d.ID)
			}
		}
		return values
	}
	line := m.input.Value()
	values := connection.CommandSuggestions(line, m.connections)
	values = append(values, "reports", "report ", "run list", "run prepare ", "run now ", "run survey ")
	for _, s := range m.connections {
		if s.State == "connected" && s.Generation != "" {
			values = append(values, "run prepare "+s.Name+" -- ", "run now "+s.Name+" -- ", "run survey "+s.Name+" --os ubuntu")
		}
	}
	for _, r := range m.runs {
		for _, verb := range []string{"inspect", "launch", "cancel", "collect", "close"} {
			values = append(values, "run "+verb+" "+r.ID)
		}
		values = append(values, "run output "+r.ID+" stdout 0", "run output "+r.ID+" stderr 0")
		values = append(values, "run follow "+r.ID, "run follow "+r.ID+" stdout", "run follow "+r.ID+" stderr")
	}
	values = append(values, "scp ", "local", "lls ", "lcd ")
	for _, s := range m.connections {
		if s.State == "connected" {
			values = append(values, "scp "+s.Name)
		}
	}
	if m.tunnelError == "" {
		for _, t := range m.tunnels {
			values = append(values, "tunnel remove "+t.ID, "tunnel check "+t.ID, "tund "+t.ID)
		}
	}
	for _, id := range m.shellIDs {
		values = append(values, "resume "+id, "shell-close "+id)
	}
	if strings.HasPrefix(line, "profile create ") || strings.HasPrefix(line, "profile edit ") {
		sub := strings.TrimPrefix(strings.TrimPrefix(line, "profile create "), "profile edit ")
		for _, value := range connection.CommandSuggestions("connect "+sub, nil) {
			if strings.HasPrefix(value, "connect ") {
				values = append(values, strings.TrimSuffix(line, sub)+strings.TrimPrefix(value, "connect "))
			}
		}
	}
	values = append(values, connection.ProfileSuggestions(m.profiles)...)
	for _, s := range m.connections {
		if s.State == "connected" {
			values = append(values, "profile save "+s.Name)
		}
	}
	// The dashboard already presents these inventories. Keep the commands
	// callable (and in shared CLI help), without promoting duplicate TUI actions.
	return slices.DeleteFunc(values, func(value string) bool {
		switch value {
		case "connections", "profiles", "shells", "tunnel list":
			return true
		}
		return false
	})
}

var completionDescriptions = map[string]string{
	"run follow": "ID [stdout|stderr] [OFFSET] · independent live viewer",
	"run survey": "CONNECTION --os ubuntu · review, run and save Markdown report",
	"reports":    "Browse saved Markdown reports", "report": "ID · open a saved report",
	"--local":  "Run the tool on the daemon host with selected connection context",
	"--script": "Local script inside the workspace upload root", "--mode": "Explicit stream, inline or remote stage semantics", "--interpreter": "Absolute interpreter path on the selected execution host", "--stdin": "Independent binary input from the upload root", "--keep": "Keep explicitly staged files", "--timeout": "Execution deadline, such as 30s or 5m", "--budget": "Positive output byte budget per stream",
	"run now": "CONNECTION [--local] -- COMMAND [ARG...] · review, launch, wait and collect", "run prepare": "CONNECTION -- COMMAND [ARG...] · no execution", "run launch": "Launch once; --collect waits and saves output", "run list": "List retained local/remote runs", "run inspect": "Local/remote status, capture and cleanup", "run output": "Read stdout/stderr at a byte offset", "run cancel": "Request ordinary process-group termination", "run collect": "Register output as Hovel evidence", "run close": "Drop working output; preserve collected evidence",
	"proxy create": "CONNECTION LISTEN · port or IP:port", "proxy inspect": "Verify SOCKS endpoint and owner identity", "proxy remove": "Remove SOCKS only; preserve connection/L/R",
	"tunnel create": "CONNECTION forward|reverse LISTEN HOST PORT", "tunc": "CONNECTION l|r LISTEN HOST PORT", "tunnel list": "List retained forwarding inventory", "tunnel remove": "Remove selected listener", "tund": "Remove selected listener", "tunnel check": "Test tunnel connectivity (destination greeting)",
	"status": "Verify workspace and daemon", "connect": "Open SSH connection form", "connections": "List active SSH connections",
	"inspect": "Inspect connection state", "reconnect": "Replace a lost SSH connection", "close": "Review and close connection",
	"shell": "Open interactive SSH shell", "shells": "List this frontend's local shells", "resume": "Resume a local shell ID", "shell-close": "Close selected local shell or ID", "help": "Show command reference", "quit": "Review connections and quit",
	"profiles": "List saved connections", "history": "Show retained command history",
	"profile create": "Save connection settings", "profile edit": "Replace saved settings", "profile save": "Save authenticated settings",
	"profile select": "Inspect saved settings", "profile delete": "Delete saved settings only", "profile connect": "Connect saved SSH profile",
	"profile load": "Open existing collection", "profile collection": "Create/open collection", "profile backup": "Back up saved collection",
	"-ip": "SSH host or config alias", "-port": "SSH port", "-user": "SSH username", "-socket": "Connection name", "-ssh-key": "Private-key file path",
	"--key": "Private-key file path", "--agent": "SSH agent socket", "--port": "SSH port", "--ssh-config": "SSH config file",
	"--jump": "SSH jump host", "--prompt": "Hidden authentication prompt", "--yes": "Confirm reviewed connection",
	"-proxy": "Local SOCKS proxy (default 9050)",
}

func completionDescription(value string) string {
	words := strings.Fields(value)
	if len(words) == 0 {
		return ""
	}
	if description := completionDescriptions[words[len(words)-1]]; strings.HasPrefix(words[len(words)-1], "-") && description != "" {
		return description
	}
	command := words[0]
	if (command == "run" || command == "profile" || command == "tunnel" || command == "proxy") && len(words) > 1 {
		command += " " + words[1]
	}
	return completionDescriptions[command]
}

func (m ui) forwardingGuidance() string {
	words := strings.Fields(safe(m.input.Value()))
	direction := 2
	if len(words) >= 2 && words[0] == "tunnel" && words[1] == "create" {
		direction = 3
	} else if len(words) == 0 || words[0] != "tunc" {
		return ""
	}
	if len(words) <= direction {
		return ""
	}
	side := "Local listener · remote destination"
	switch words[direction] {
	case "forward", "l":
	case "reverse", "r":
		side = "Remote listener · local destination · LISTEN 0: random port"
	default:
		return ""
	}
	example := strings.Join(words[:direction+1], " ") + " 8080 localhost 80"
	return m.paint(warningStyle, "LISTEN HOST PORT") + "\n" +
		m.paint(secondary, side+"\nLISTEN: port or IP:port (default 127.0.0.1)") + "\n" +
		m.paint(accent, "Example:") + "\n" + m.syntax(example, false)
}
func (m ui) profileRows() int { return max(1, min(3, (m.height-20)/2)) }
func (m ui) savedConnections(w int) string {
	title := "SAVED CONNECTIONS"
	if m.profileError != "" {
		return m.paint(heading, title) + "\n" + m.paint(errorStyle, m.profileError)
	}
	if len(m.profiles.Profiles) == 0 {
		return m.paint(heading, title) + "\n" + m.paint(secondary, "No saved connections")
	}
	start := min(m.profileOffset, max(0, len(m.profiles.Profiles)-m.profileRows()))
	end := min(len(m.profiles.Profiles), start+m.profileRows())
	var rows [][]string
	for _, p := range m.profiles.Profiles[start:end] {
		port := fmt.Sprint(p.Port)
		if p.Port == 0 {
			port = "Config"
		}
		auth := p.Key
		if auth == "" {
			auth = "Config/agent"
		}
		if p.AgentExplicit {
			auth = "Agent: " + p.Agent
			if p.Agent == "" {
				auth = "Agent off"
			}
			if p.Key != "" {
				auth = p.Key + " + " + auth
			}
		}
		proxy := "—"
		if p.ProxyPort != 0 {
			proxy = fmt.Sprint(p.ProxyPort)
		}
		rows = append(rows, []string{safe(p.Name), safe(p.Host), safe(p.User), port, safe(auth), "—", proxy, "Yes"})
	}
	text := m.dataTable(title, []string{"NAME", "HOST", "USER", "PORT", "KEY", "SHELL", "PROXY", "NO-TERM"}, rows, w)
	if end-start < len(m.profiles.Profiles) {
		text += fmt.Sprintf("\n%d–%d of %d saved", start+1, end, len(m.profiles.Profiles))
	}
	return text
}
func (m *frame) profileMenu() tea.Cmd {
	name := m.current().management.selectedProfile
	options := []huh.Option[string]{huh.NewOption("Create profile", "profile create "), huh.NewOption("Open collection", "profile load "), huh.NewOption("New collection", "profile collection "), huh.NewOption("Backup collection", "profile backup ")}
	if name != "" {
		options = append([]huh.Option[string]{huh.NewOption("Connect "+name, "profile connect "+name), huh.NewOption("Edit "+name, "profile edit "+name), huh.NewOption("Delete "+name, "profile delete "+name), huh.NewOption("Inspect "+name, "profile select "+name)}, options...)
	}
	for _, s := range m.current().management.connections {
		if s.State == "connected" {
			options = append(options, huh.NewOption("Save active "+s.Name, "profile save "+s.Name))
		}
	}
	return m.setForm("profile-menu", "Saved connections", newForm(huh.NewGroup(huh.NewSelect[string]().Key("command").Title("Choose an action").Options(options...))))
}
func (m *frame) profileAction(command string) tea.Cmd {
	m.modal = ""
	// Present edits as a fully populated shared command so all supported fields
	// remain available, without a second profile-specific form implementation.
	if strings.HasPrefix(command, "profile edit ") {
		for _, p := range m.current().management.profiles.Profiles {
			if command == "profile edit "+p.Name {
				command = connection.CommandLine(append([]string{"profile", "edit"}, p.Args()[1:]...))
				command += " --revision " + m.current().management.profiles.Revision + " --collection " + connection.CommandLine([]string{m.current().management.profiles.Path})
			}
		}
	}
	u := &m.current().management
	u.input.SetValue(command)
	u.input.CursorEnd()
	u.input.SetSuggestions(u.suggestions())
	m.current().focus = "prompt"
	return nil
}

type saveOffered struct {
	name       string
	offer      bool
	collection connection.Collection
	err        error
}

func offerSave(workspace, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		yes, e := connection.SaveOffer(ctx, workspace, name)
		var list connection.Collection
		if e == nil && yes {
			list, e = connection.Profiles(ctx, workspace)
		}
		return saveOffered{name, yes, list, e}
	}
}
func saveProfileForm(name, path string) *huh.Form {
	return newForm(huh.NewGroup(huh.NewInput().Key("name").Title("Save profile as").Value(&name).Description("Collection: " + safe(path) + "\nEnter saves · Esc skips; connection stays active")))
}
