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
		{Name: "gateway", Host: "gateway.example.com", User: "operator", Port: 22, State: "connected", Socket: "/demo/gateway.sock"},
		{Name: "build", Host: "build.example.com", User: "runner", Port: 2222, State: "connecting", Socket: "/demo/build.sock"},
		{Name: "staging", Host: "staging.example.com", User: "deploy", Port: 22, State: "failed", Socket: "/demo/staging.sock"},
	}
	m.current().selected = "gateway"
	return m
}
func (m ui) demoResources(w int) string {
	saved := [][]string{{"production", "gateway.example.com", "operator", "22", "id_ed25519", "bash", "1080", "No"}, {"staging", "staging.example.com", "deploy", "2222", "Agent", "zsh", "—", "Yes"}}
	tunnels := [][]string{{"1", "gateway", "Local", "5432", "db.internal:5432"}, {"2", "build", "Dynamic", "1080", "SOCKS"}}
	savedHeaders := []string{"NAME", "HOST", "USER", "PORT", "KEY", "SHELL", "PROXY", "NO-TERM"}
	return m.dataTable("SAVED CONNECTIONS", savedHeaders, saved, w) + "\n\n" + m.activeConnections(w) + "\n\n" + m.dataTable("TUNNELS", []string{"ID", "CONNECTION", "TYPE", "LOCAL PORT", "REMOTE"}, tunnels, w)
}
