package main

import (
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
		{Name: "gateway", Host: "10.20.0.10", User: "operator", Port: 22, State: "connected", Socket: "/demo/gateway.sock"},
		{Name: "build", Host: "10.20.0.20", User: "runner", Port: 2222, State: "connecting", Socket: "/demo/build.sock"},
		{Name: "staging", Host: "10.30.0.10", User: "deploy", Port: 22, State: "failed", Socket: "/demo/staging.sock"},
	}
	m.current().selected = "gateway"
	return m
}
func (m ui) demoResources(w int) string {
	saved := [][]string{{"production", "10.20.0.10", "operator", "22", "id_ed25519", "bash", "1080", "No"}, {"staging", "10.30.0.10", "deploy", "2222", "Agent", "zsh", "—", "Yes"}}
	tunnels := [][]string{{"1", "gateway", "Local", "5432", "10.20.0.30:5432"}, {"2", "gateway", "Local", "8080", "10.20.0.40:80"}}
	if m.height < 30 {
		saved = saved[:1]
		tunnels = tunnels[:1]
	}
	savedHeaders := []string{"NAME", "HOST", "USER", "PORT", "KEY", "SHELL", "PROXY", "NO-TERM"}
	return m.dataTable("SAVED CONNECTIONS", savedHeaders, saved, w) + "\n\n" + m.activeConnections(w) + "\n\n" + m.dataTable("TUNNELS", []string{"ID", "CONNECTION", "TYPE", "LOCAL PORT", "REMOTE"}, tunnels, w)
}
