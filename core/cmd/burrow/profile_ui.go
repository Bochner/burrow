package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/Bochner/burrow/core/connection"
)

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
	line := m.input.Value()
	values := connection.CommandSuggestions(line, m.connections)
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
	return values
}

var completionDescriptions = map[string]string{
	"tunnel create": "CONNECTION forward LISTEN HOST PORT", "tunc": "CONNECTION l LISTEN HOST PORT", "tunnel list": "List retained forwarding inventory", "tunnel remove": "Remove selected local listener", "tund": "Remove selected local listener", "tunnel check": "Observe destination greeting without retaining content",
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
	if command == "profile" && len(words) > 1 {
		command += " " + words[1]
	}
	return completionDescriptions[command]
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
