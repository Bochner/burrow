package main

import (
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/Bochner/burrow/core/connection"
	"github.com/Bochner/burrow/core/launch"
)

// Explicit offline presentation preview: no launch, polling, or command execution.
func newDemoFrame(noColor bool) *frame {
	m := newFrame(launch.Info{Workspace: "/demo/workspace"}, noColor, launch.Options{})
	m.demo = true
	u := &m.current().management
	u.demo = true
	u.input.Placeholder = "Sample preview · help · quit"
	u.output = "Sample data only · no remote connections."
	u.connections = []connection.State{
		{Name: "gateway", Host: "gateway.example.com", User: "operator", Port: 22, State: "connected"},
		{Name: "build", Host: "build.example.com", User: "runner", Port: 2222, State: "connecting"},
		{Name: "staging", Host: "staging.example.com", User: "deploy", Port: 22, State: "failed"},
	}
	m.current().selected = "gateway"
	return m
}
func (m ui) demoResources(w int) string {
	saved := [][]string{{"production", "gateway.example.com", "SSH key"}, {"staging", "staging.example.com", "Agent"}}
	tunnels := [][]string{{"gateway", "Local", "127.0.0.1:5432", "db.internal:5432"}, {"build", "Dynamic", "127.0.0.1:1080", "SOCKS"}}
	if m.height < 30 {
		saved = saved[:1]
		tunnels = tunnels[:1]
	}
	render := func(title string, headers []string, rows [][]string) string {
		return m.paint(heading, title) + "\n" + table.New().Headers(headers...).Rows(rows...).Width(w).Wrap(false).
			Border(lipgloss.NormalBorder()).BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).BorderColumn(false).BorderStyle(separatorStyle).
			StyleFunc(func(row, col int) lipgloss.Style {
				s := tableCell.PaddingLeft(0).PaddingRight(2)
				if row == table.HeaderRow {
					return s.Foreground(lipgloss.Color(subtextColor)).Bold(true)
				}
				return s
			}).String()
	}
	return render("SAVED CONNECTIONS", []string{"NAME", "HOST", "AUTH"}, saved) + "\n\n" + m.activeConnections(w) + "\n\n" + render("TUNNELS", []string{"CONNECTION", "TYPE", "LISTEN", "DESTINATION"}, tunnels)
}
