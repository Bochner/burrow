// Disposable #71 public routing proof. Never installed in an operator workspace.
package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Bochner/burrow/core/connection"
	"github.com/Bochner/burrow/core/launch"
	"github.com/vibepwners/hovel/sdk/go/hovel"
)

const manifest = `apiVersion: hovel.dev/v1alpha1
kind: ModulePackage
metadata:
  name: burrow
  version: 0.1.0
  moduleType: survey
  summary: Disposable retained manager routing proof for issue 71.
  tags: [dangerous]
  license: Apache-2.0
runtime:
  protocol: jsonrpc-stdio
launch:
  - selector:
      os: linux
      arch: amd64
    command: ["burrow", "module"]
`

type module struct{}

func (module) Info() hovel.Info {
	return hovel.Info{Name: "burrow", Version: "0.1.0", Type: hovel.TypeSurvey, Tags: []string{"dangerous"}, Summary: "Disposable retained manager proof"}
}
func (module) Schema() hovel.Schema {
	var req []hovel.Requirement
	for _, key := range []string{"workspace", "action", "generation", "session", "request", "review"} {
		req = append(req, hovel.Requirement{Key: key, Type: "string"})
	}
	return hovel.Schema{ChainConfig: req}
}

type observation struct {
	Session    string `json:"session"`
	Generation string `json:"generation"`
	OwnerPID   int    `json:"ownerPID"`
	RunID      string `json:"runID"`
}
type manager struct {
	mu sync.Mutex
	observation
	dir         *os.File
	workspace   string
	closed      bool
	connections map[string]*master
	logMu       sync.Mutex
	log         *hovel.Logger
	logs        int
}

func (module) Run(ctx *hovel.Context) (hovel.Result, error) {
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w := ctx.InputString("workspace", "")
	info, e := launch.Status(c, w)
	if e != nil {
		return hovel.Result{}, e
	}
	if os.Getppid() != info.PID {
		return hovel.Result{}, fmt.Errorf("adapter must be launched by the verified daemon")
	}
	generation := ctx.InputString("generation", "")
	if generation == "" {
		return hovel.Result{}, fmt.Errorf("missing generation")
	}
	switch ctx.InputString("action", "") {
	case "activate":
		dir, e := launch.ReserveConnection(c, w, "manager")
		if e != nil {
			return hovel.Result{}, e
		}
		s := &manager{observation: observation{Generation: generation, OwnerPID: os.Getpid(), RunID: ctx.RunID}, dir: dir, workspace: w, log: ctx.Log}
		ref, e := ctx.OpenSession(s, hovel.WithName("manager"), hovel.WithKind("manager-proof"))
		if e != nil {
			dir.Close()
			return hovel.Result{}, e
		}
		s.mu.Lock()
		s.Session = ref.ID
		s.mu.Unlock()
		s.milestone("manager activated; no connection permission granted")
		b, e := json.Marshal(s.observation)
		return hovel.Ok(nil, hovel.WithSummary(string(b))), e
	case "forward", "connect":
		var result hovel.PayloadCommandResult
		command := hovel.PayloadCommandRequest{Command: "probe", Args: []string{generation, ctx.RunID}}
		if ctx.InputString("action", "") == "connect" {
			raw := ctx.InputString("request", "")
			r, e := decodeRequest(raw)
			if e != nil || digest(raw) != ctx.InputString("review", "") || r.Generation != generation || r.Session != ctx.InputString("session", "") || r.Settings.Workspace != w {
				return hovel.Result{}, fmt.Errorf("changed request or selected owner refused")
			}
			command = hovel.PayloadCommandRequest{Command: "connect", Args: []string{raw, ctx.InputString("review", ""), ctx.RunID}}
		}
		e := launch.Call(c, w, "RunSessionCommand", map[string]any{"SessionID": ctx.InputString("session", ""), "Request": command}, &result)
		if e != nil {
			return hovel.Result{}, e
		}
		if command.Command == "connect" {
			var state connectionState
			if e = json.Unmarshal([]byte(result.Stdout), &state); e != nil {
				return hovel.Result{}, e
			}
			r, _ := decodeRequest(command.Args[0])
			if state.ID != r.ID || state.RunID != ctx.RunID {
				return hovel.Result{}, fmt.Errorf("creation correlation refused")
			}
			return hovel.Ok(nil, hovel.WithSummary(result.Stdout)), nil
		}
		var o observation
		if e = json.Unmarshal([]byte(result.Stdout), &o); e != nil {
			return hovel.Result{}, e
		}
		if o.Generation != generation || o.Session != ctx.InputString("session", "") || o.RunID != ctx.RunID {
			return hovel.Result{}, fmt.Errorf("owner correlation refused")
		}
		return hovel.Ok(nil, hovel.WithSummary(result.Stdout)), nil
	}
	return hovel.Result{}, fmt.Errorf("unsupported proof action")
}
func (s *manager) Open() error                        { return nil }
func (s *manager) Write([]byte) error                 { return fmt.Errorf("use bounded controls") }
func (s *manager) Read(time.Duration) ([]byte, error) { return nil, nil }
func (s *manager) Closed() bool                       { s.mu.Lock(); defer s.mu.Unlock(); return s.closed }
func (s *manager) Close(string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := launch.VerifyReservation(c, s.workspace, s.dir); e != nil {
		return e
	}
	for _, m := range s.connections {
		if e := m.close(); e != nil {
			return e
		}
	}
	if e := os.Remove(s.dir.Name()); e != nil {
		return e
	}
	s.closed = true
	return s.dir.Close()
}
func (s *manager) ListPayloadCommands(hovel.PayloadCommandListRequest) ([]hovel.PayloadCommand, error) {
	return []hovel.PayloadCommand{{Name: "identity", ReadOnly: true}, {Name: "probe", ReadOnly: true}, {Name: "list", ReadOnly: true}, {Name: "close"}, {Name: "close-reviewed"}, {Name: "connect", Summary: "Adapter forwarding only; session commands do not certify throw approval"}}, nil
}
func (s *manager) RunPayloadCommand(req hovel.PayloadCommandRequest) (hovel.PayloadCommandResult, error) {
	if len(req.Config) > 0 || req.Reconnect != nil || req.InstalledPayloadID != "" || req.InputPath != "" || req.InputData != "" {
		return hovel.PayloadCommandResult{}, fmt.Errorf("unsupported inputs")
	}
	var value any
	if req.Command == "identity" && len(req.Args) == 0 {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closed {
			return hovel.PayloadCommandResult{}, fmt.Errorf("owner closed")
		}
		value = s.observation
	} else if req.Command == "connect" && len(req.Args) == 3 {
		v, e := s.connect(req.Args[0], req.Args[1], req.Args[2])
		if e != nil {
			return hovel.PayloadCommandResult{}, e
		}
		value = v
	} else {
		s.mu.Lock()
		if s.closed || len(req.Args) < 1 || req.Args[0] != s.Generation {
			s.mu.Unlock()
			return hovel.PayloadCommandResult{}, fmt.Errorf("exact owner required")
		}
		switch req.Command {
		case "close-reviewed":
			defer s.mu.Unlock()
			var expected []connectionState
			if len(req.Args) != 2 || json.Unmarshal([]byte(req.Args[1]), &expected) != nil {
				return hovel.PayloadCommandResult{}, fmt.Errorf("review snapshot required")
			}
			current := []connectionState{}
			for _, m := range s.connections {
				state, e := m.snapshot()
				if e != nil {
					return hovel.PayloadCommandResult{}, e
				}
				if state.State != "closed" {
					current = append(current, state)
				}
			}
			slices.SortFunc(current, func(a, b connectionState) int { return strings.Compare(a.ID, b.ID) })
			if !slices.Equal(current, expected) {
				return hovel.PayloadCommandResult{}, fmt.Errorf("inventory changed; review quit again")
			}
			// The same lock serializes connection admission against this reviewed close.
			for _, state := range expected {
				if e := s.connections[state.ID].close(); e != nil {
					return hovel.PayloadCommandResult{}, fmt.Errorf("quit cleanup uncertain: %w", e)
				}
			}
			value = map[string]string{"state": "closed"}
		case "probe":
			if len(req.Args) != 2 {
				s.mu.Unlock()
				return hovel.PayloadCommandResult{}, fmt.Errorf("probe identity required")
			}
			o := s.observation
			o.RunID = req.Args[1]
			value = o
			s.mu.Unlock()
		case "list":
			if len(req.Args) != 1 {
				s.mu.Unlock()
				return hovel.PayloadCommandResult{}, fmt.Errorf("unexpected list inputs")
			}
			selected := make([]*master, 0, len(s.connections))
			for _, m := range s.connections {
				selected = append(selected, m)
			}
			s.mu.Unlock()
			states := []connectionState{}
			for _, m := range selected {
				v, e := m.snapshot()
				if e != nil {
					return hovel.PayloadCommandResult{}, e
				}
				if v.State != "closed" {
					states = append(states, v)
				}
			}
			value = states
			s.milestone("manager inventory inspected")
		case "close":
			if len(req.Args) != 3 || req.Args[2] != "confirm" {
				s.mu.Unlock()
				return hovel.PayloadCommandResult{}, fmt.Errorf("selected creation and confirmation required")
			}
			m := s.connections[req.Args[1]]
			s.mu.Unlock()
			if m == nil {
				return hovel.PayloadCommandResult{}, fmt.Errorf("selected creation unavailable")
			}
			if e := m.close(); e != nil {
				return hovel.PayloadCommandResult{}, e
			}
			value = map[string]string{"state": "closed", "id": req.Args[1]}
		default:
			s.mu.Unlock()
			return hovel.PayloadCommandResult{}, fmt.Errorf("bounded command required")
		}
	}
	b, e := json.Marshal(value)
	return hovel.PayloadCommandResult{Command: req.Command, Stdout: string(b)}, e
}

func submit(c context.Context, w string, config map[string]string) (any, error) {
	if e := c.Err(); e != nil {
		return nil, e
	}
	// Isolated per-request chains: no frontend can overwrite another review.
	op := "proof-" + rand.Text()
	chain := map[string]string{"Operation": op, "Chain": "request"}
	calls := []struct {
		method string
		input  any
	}{
		{"CreateOperation", map[string]string{"Operation": op}},
		{"CreateChain", chain},
		{"AddModule", map[string]string{"Operation": op, "Chain": "request", "ModuleID": "burrow@0.1.0"}},
		{"AddTarget", map[string]string{"Operation": op, "Chain": "request", "Target": "local://manager-proof"}},
	}
	for _, call := range calls {
		var out any
		if e := launch.Call(c, w, call.method, call.input, &out); e != nil {
			return nil, e
		}
	}
	config["workspace"] = w
	for key, value := range config {
		var out any
		if e := launch.Call(c, w, "SetChainConfig", map[string]string{"Operation": op, "Chain": "request", "Key": key, "Value": value}, &out); e != nil {
			return nil, e
		}
	}
	b, e := launch.HovelCLI(c, w, "--op", op, "--chain", "request", "--", "throw", "--now", "--allow-dangerous", "--json")
	if e != nil {
		return nil, e
	}
	var result struct {
		Results []struct {
			State   string
			Summary string
		}
	}
	if e = json.Unmarshal(b, &result); e != nil {
		return nil, e
	}
	if len(result.Results) != 1 || result.Results[0].State != "succeeded" {
		return nil, fmt.Errorf("proof throw failed: %s", b)
	}
	var value any
	e = json.Unmarshal([]byte(result.Results[0].Summary), &value)
	return value, e
}
func control(c context.Context, w string, o observation, command string, args []string, out any) error {
	if command != "identity" {
		args = append([]string{o.Generation}, args...)
	}
	var result hovel.PayloadCommandResult
	if e := launch.Call(c, w, "RunSessionCommand", map[string]any{"SessionID": o.Session, "Request": hovel.PayloadCommandRequest{Command: command, Args: args}}, &result); e != nil {
		return e
	}
	return json.Unmarshal([]byte(result.Stdout), out)
}

func findOwner(c context.Context, w, generation string) (observation, error) {
	var refs struct{ Sessions []hovel.SessionRef }
	if e := launch.Call(c, w, "ListSessions", map[string]any{}, &refs); e != nil {
		return observation{}, e
	}
	var owners []observation
	for _, ref := range refs.Sessions {
		if ref.ModuleID != "burrow@0.1.0" || ref.Kind != "manager-proof" || ref.State == "closed" {
			continue
		}
		var o observation
		if e := control(c, w, observation{Session: ref.ID}, "identity", nil, &o); e != nil {
			return observation{}, e
		}
		if o.Session != ref.ID {
			return observation{}, fmt.Errorf("session identity changed")
		}
		if generation == "" || o.Generation == generation {
			owners = append(owners, o)
		}
	}
	if len(owners) != 1 {
		return observation{}, fmt.Errorf("exact owner not observed; preserve resources; do not retry startup automatically")
	}
	return owners[0], nil
}

// Review-only snapshots; the manager remains the sole live connection inventory.
type quitWorkspace struct {
	Workspace   string            `json:"workspace"`
	Owner       observation       `json:"owner"`
	Connections []connectionState `json:"connections"`
}

func quit(c context.Context, path, review, action string) (any, error) {
	if action == "keep" || action == "cancel" {
		return map[string]string{"state": action}, nil
	}
	if action != "close" {
		return nil, fmt.Errorf("keep/cancel/close required")
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	var workspaces []quitWorkspace
	if digest(string(b)) != review || json.Unmarshal(b, &workspaces) != nil || len(workspaces) == 0 {
		return nil, fmt.Errorf("quit review changed")
	}
	for _, w := range workspaces {
		var current []connectionState
		if e = control(c, w.Workspace, w.Owner, "list", nil, &current); e != nil {
			return nil, e
		}
		slices.SortFunc(current, func(a, b connectionState) int { return strings.Compare(a.ID, b.ID) })
		if !slices.Equal(current, w.Connections) {
			return nil, fmt.Errorf("inventory changed; review quit again")
		}
	}
	// A later failure can follow successful closes; never promise rollback.
	for _, w := range workspaces {
		raw, _ := json.Marshal(w.Connections)
		var result any
		if e = control(c, w.Workspace, w.Owner, "close-reviewed", []string{string(raw)}, &result); e != nil {
			return nil, fmt.Errorf("quit incomplete; inspect all opened workspaces: %w", e)
		}
	}
	for _, w := range workspaces {
		var remaining []connectionState
		if e = control(c, w.Workspace, w.Owner, "list", nil, &remaining); e != nil {
			return nil, e
		}
		if len(remaining) > 0 {
			return nil, fmt.Errorf("connections remain; review quit again")
		}
	}
	return map[string]string{"state": "closed"}, nil
}

func run() (any, error) {
	if len(os.Args) < 3 {
		return nil, fmt.Errorf("expected install/activate/forward WORKSPACE")
	}
	interrupt, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c, cancel := context.WithTimeout(interrupt, 40*time.Second)
	defer cancel()
	w := os.Args[2]
	switch os.Args[1] {
	case "prepare":
		if len(os.Args) < 7 {
			return nil, fmt.Errorf("session, generation and connection arguments required")
		}
		cfg, _, e := connection.Parse(w, os.Args[5:])
		return request{ID: digest(rand.Text())[:32], Session: os.Args[3], Generation: os.Args[4], Settings: cfg}, e
	case "reconcile":
		if len(os.Args) < 4 || len(os.Args) > 5 {
			return nil, fmt.Errorf("known generation and optional creation required")
		}
		o, e := findOwner(c, w, os.Args[3])
		if e != nil || len(os.Args) == 4 {
			return o, e
		}
		var states []connectionState
		if e = control(c, w, o, "list", nil, &states); e != nil {
			return nil, e
		}
		for _, state := range states {
			if state.ID == os.Args[4] {
				return state, nil
			}
		}
		return nil, fmt.Errorf("creation not observed; dispatch/cleanup uncertain; do not retry automatically")
	case "quit-review":
		var review []quitWorkspace
		for _, path := range os.Args[2:] {
			o, e := findOwner(c, path, "")
			if e != nil {
				return nil, e
			}
			var states []connectionState
			if e = control(c, path, o, "list", nil, &states); e != nil {
				return nil, e
			}
			slices.SortFunc(states, func(a, b connectionState) int { return strings.Compare(a.ID, b.ID) })
			review = append(review, quitWorkspace{path, o, states})
		}
		return review, nil
	case "quit":
		if len(os.Args) != 5 {
			return nil, fmt.Errorf("quit REVIEW_FILE DIGEST keep/cancel/close required")
		}
		return quit(c, w, os.Args[3], os.Args[4])
	case "setup":
		if len(os.Args) != 4 {
			return nil, fmt.Errorf("pinned package required")
		}
		return launch.Open(c, launch.Options{Workspace: w, Package: os.Args[3]})
	case "install":
		e := launch.RegisterModule(c, w, "burrow@0.1.0", []byte(manifest))
		return map[string]bool{"installed": e == nil}, e
	case "activate":
		generation := rand.Text()
		if len(os.Args) == 5 && os.Args[4] == "cancel" {
			return map[string]string{"state": "cancelled-before-dispatch", "generation": os.Args[3]}, nil
		}
		if len(os.Args) == 4 {
			generation = os.Args[3]
		} else if len(os.Args) != 3 {
			return nil, fmt.Errorf("optional known generation required")
		}
		return submit(c, w, map[string]string{"action": "activate", "generation": generation})
	case "connect":
		if len(os.Args) != 5 && !(len(os.Args) == 6 && os.Args[5] == "cancel") {
			return nil, fmt.Errorf("request file and reviewed digest required")
		}
		b, e := os.ReadFile(os.Args[3])
		if e != nil {
			return nil, e
		}
		raw := string(b)
		r, e := decodeRequest(raw)
		if e != nil {
			return nil, e
		}
		if digest(raw) != os.Args[4] || r.Settings.Workspace != w {
			return nil, fmt.Errorf("request changed; review again")
		}
		if len(os.Args) == 6 {
			return map[string]string{"state": "cancelled-before-dispatch", "id": r.ID}, nil
		}
		if e = launch.RegisterModule(c, w, "burrow@0.1.0", []byte(manifest)); e != nil {
			return nil, e
		}
		return submit(c, w, map[string]string{"action": "connect", "session": r.Session, "generation": r.Generation, "request": raw, "review": os.Args[4]})
	case "control":
		if len(os.Args) < 6 || (os.Args[4] != "probe" && os.Args[4] != "list" && os.Args[4] != "close") {
			return nil, fmt.Errorf("session, command and generation required")
		}
		var result hovel.PayloadCommandResult
		e := launch.Call(c, w, "RunSessionCommand", map[string]any{"SessionID": os.Args[3], "Request": hovel.PayloadCommandRequest{Command: os.Args[4], Args: os.Args[5:]}}, &result)
		if e != nil {
			return nil, e
		}
		var value any
		e = json.Unmarshal([]byte(result.Stdout), &value)
		return value, e
	case "forward":
		if len(os.Args) != 5 {
			return nil, fmt.Errorf("session and generation required")
		}
		return submit(c, w, map[string]string{"action": "forward", "session": os.Args[3], "generation": os.Args[4]})
	}
	return nil, fmt.Errorf("unsupported proof command")
}
func main() {
	if os.Getenv("BURROW_ASKPASS") == "1" {
		if len(os.Args) != 2 || connection.Askpass(os.Args[1]) != nil {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "module" {
		hovel.Serve(module{})
		return
	}
	result, e := run()
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	if e = json.NewEncoder(os.Stdout).Encode(result); e != nil {
		os.Exit(1)
	}
}
