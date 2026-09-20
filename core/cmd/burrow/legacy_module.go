// Test-only fixture for a retained owner with the pre-#76 catalog identity.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/Bochner/burrow/core/connection"
	"github.com/vibepwners/hovel/sdk/go/hovel"
)

type legacyModule struct{ connection.Module }

func (legacyModule) Info() hovel.Info {
	info := (connection.Module{}).Info()
	info.Name = "burrow-connection"
	if len(os.Args) == 2 && (os.Args[1] == "base-module" || os.Args[1] == "old-manager") {
		info.Name = "burrow"
	}
	return info
}

// Public retained-session fixture for a manager predating password capability
// negotiation. It cannot launch SSH; its identity has the historical wire shape.
type oldManager struct {
	mu       sync.Mutex
	id       map[string]any
	closed   bool
	attempts int
}

func (m *oldManager) Open() error                        { return nil }
func (m *oldManager) Read(time.Duration) ([]byte, error) { return nil, nil }
func (m *oldManager) Write([]byte) error                 { return fmt.Errorf("unsupported") }
func (m *oldManager) Closed() bool                       { m.mu.Lock(); defer m.mu.Unlock(); return m.closed }
func (m *oldManager) Close(string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}
func (m *oldManager) ListPayloadCommands(hovel.PayloadCommandListRequest) ([]hovel.PayloadCommand, error) {
	return []hovel.PayloadCommand{{Name: "identity", ReadOnly: true}, {Name: "list", ReadOnly: true}, {Name: "attempts", ReadOnly: true}, {Name: "connect"}}, nil
}
func (m *oldManager) RunPayloadCommand(r hovel.PayloadCommandRequest) (hovel.PayloadCommandResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var value any
	switch r.Command {
	case "identity":
		value = m.id
	case "list":
		value = []any{}
	case "attempts":
		value = m.attempts
	case "connect":
		m.attempts++
		return hovel.PayloadCommandResult{}, fmt.Errorf("request changed or missing run correlation")
	default:
		return hovel.PayloadCommandResult{}, fmt.Errorf("unsupported")
	}
	b, e := json.Marshal(value)
	return hovel.PayloadCommandResult{Command: r.Command, Stdout: string(b)}, e
}
func (m legacyModule) Run(ctx *hovel.Context) (hovel.Result, error) {
	if len(os.Args) != 2 || os.Args[1] != "old-manager" {
		return m.Module.Run(ctx)
	}
	s := &oldManager{id: map[string]any{"generation": "old-generation", "workspace": ctx.InputString("workspace", ""), "ownerPID": os.Getpid(), "runID": ctx.RunID}}
	r, e := ctx.OpenSession(s, hovel.WithKind("burrow-manager-v1"))
	if e != nil {
		return hovel.Result{}, e
	}
	s.mu.Lock()
	s.id["session"] = r.ID
	b, _ := json.Marshal(s.id)
	s.mu.Unlock()
	return hovel.Ok(nil, hovel.WithSummary(string(b))), nil
}

func main() {
	if os.Getenv("BURROW_ASKPASS") == "1" {
		if len(os.Args) != 2 || connection.Askpass(os.Args[1]) != nil {
			os.Exit(1)
		}
		return
	}
	hovel.Serve(legacyModule{})
}
