package connection

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Bochner/burrow/core/launch"
	"github.com/vibepwners/hovel/sdk/go/hovel"
)

// Version the control contract separately from the public module identity.
const managerKind = "burrow-manager-v1"

// The adapter's fixed pre-dispatch refusal, recognized again in throw results.
const buildMismatch = "installed Burrow module build differs from the requesting frontend; run burrow status to register this build; nothing was dispatched"

type managerIdentity struct {
	Session    string `json:"session"`
	Generation string `json:"generation"`
	Workspace  string `json:"workspace"`
	OwnerPID   int    `json:"ownerPID"`
	RunID      string `json:"runID"`
}

type managerRequest struct {
	ID       string          `json:"id"`
	Owner    managerIdentity `json:"owner"`
	Settings Config          `json:"settings"`
	Preview  string          `json:"preview"`
}

func digest(raw string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(raw))) }

func decodeManagerRequest(raw string) (managerRequest, error) {
	var r managerRequest
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF || len(r.ID) != 32 || r.Owner.Session == "" || r.Owner.Generation == "" || r.Settings.Workspace != r.Owner.Workspace {
		return r, fmt.Errorf("invalid immutable manager request")
	}
	return r, r.Settings.Validate()
}

type manager struct {
	mu sync.Mutex
	managerIdentity
	dir         *os.File
	closed      bool
	connections map[string]*owner
	logMu       sync.Mutex
	log         *hovel.Logger
	logs        int
}

func (m *manager) milestone(message string) {
	m.logMu.Lock()
	defer m.logMu.Unlock()
	// ponytail: aggregate 200-log ceiling; remove when Hovel safely drains retained logs (#30).
	if m.logs < 200 {
		m.log.Info(message)
	}
	if m.logs == 200 {
		m.log.Warn("diagnostic budget exhausted; further milestones suppressed")
	}
	if m.logs <= 200 {
		m.logs++
	}
}
func (m *manager) Open() error                        { return nil }
func (m *manager) Read(time.Duration) ([]byte, error) { return nil, nil }
func (m *manager) Write([]byte) error                 { return fmt.Errorf("use bounded manager controls") }
func (m *manager) Closed() bool                       { m.mu.Lock(); defer m.mu.Unlock(); return m.closed }
func (m *manager) Close(string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e := launch.VerifyReservation(c, m.Workspace, m.dir); e != nil {
		return e
	}
	for _, s := range m.connections {
		if e := s.Close("manager ended"); e != nil {
			return e
		}
	}
	if e := os.Remove(m.dir.Name()); e != nil {
		return e
	}
	m.closed = true
	return m.dir.Close()
}

func (m *manager) ListPayloadCommands(hovel.PayloadCommandListRequest) ([]hovel.PayloadCommand, error) {
	return []hovel.PayloadCommand{{Name: "identity", ReadOnly: true}, {Name: "list", ReadOnly: true}, {Name: "profile", ReadOnly: true}, {Name: "shell", ReadOnly: true, Summary: "Verify connection for a frontend-local shell; no session I/O recording"}, {Name: "close"}, {Name: "close-reviewed"}, {Name: "connect", Summary: "Confirmed adapter forwarding only; session commands do not certify approval"}}, nil
}

func (m *manager) inventory() ([]State, error) {
	states := []State{}
	for _, s := range m.connections {
		result, e := s.RunPayloadCommand(hovel.PayloadCommandRequest{Command: "connection-status"})
		if e != nil {
			return nil, e
		}
		var state State
		if e = json.Unmarshal([]byte(result.Stdout), &state); e != nil {
			return nil, e
		}
		if state.State != "closed" {
			states = append(states, state)
		}
	}
	slices.SortFunc(states, func(a, b State) int { return strings.Compare(a.Creation, b.Creation) })
	return states, nil
}

func (m *manager) RunPayloadCommand(req hovel.PayloadCommandRequest) (hovel.PayloadCommandResult, error) {
	if len(req.Config) > 0 || req.Reconnect != nil || req.InstalledPayloadID != "" || req.InputPath != "" || req.InputData != "" {
		return hovel.PayloadCommandResult{}, fmt.Errorf("unsupported manager inputs")
	}
	if req.Command == "connect" && len(req.Args) == 3 {
		state, e := m.connect(req.Args[0], req.Args[1], req.Args[2])
		b, _ := json.Marshal(state)
		return hovel.PayloadCommandResult{Command: req.Command, Stdout: string(b)}, e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return hovel.PayloadCommandResult{}, fmt.Errorf("manager closed")
	}
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := launch.VerifyReservation(c, m.Workspace, m.dir); e != nil {
		return hovel.PayloadCommandResult{}, e
	}
	var value any
	if req.Command == "identity" && len(req.Args) == 0 {
		value = m.managerIdentity
	} else {
		if len(req.Args) < 1 || req.Args[0] != m.Generation {
			return hovel.PayloadCommandResult{}, fmt.Errorf("exact manager generation required")
		}
		switch req.Command {
		case "list":
			if len(req.Args) != 1 {
				return hovel.PayloadCommandResult{}, fmt.Errorf("unexpected list arguments")
			}
			states, e := m.inventory()
			if e != nil {
				return hovel.PayloadCommandResult{}, e
			}
			value = states
		case "profile", "close", "shell":
			if len(req.Args) != 2 {
				return hovel.PayloadCommandResult{}, fmt.Errorf("exact creation required")
			}
			s := m.connections[req.Args[1]]
			if s == nil {
				return hovel.PayloadCommandResult{}, fmt.Errorf("creation unavailable; review again")
			}
			if req.Command == "profile" {
				return s.RunPayloadCommand(hovel.PayloadCommandRequest{Command: "connection-profile"})
			}
			if req.Command == "shell" {
				return s.RunPayloadCommand(hovel.PayloadCommandRequest{Command: "connection-shell"})
			}
			if e := s.Close("operator confirmed selected close"); e != nil {
				return hovel.PayloadCommandResult{}, e
			}
			value = map[string]string{"state": "closed"}
		case "close-reviewed":
			var expected []State
			if len(req.Args) != 2 || json.Unmarshal([]byte(req.Args[1]), &expected) != nil {
				return hovel.PayloadCommandResult{}, fmt.Errorf("reviewed inventory required")
			}
			current, e := m.inventory()
			if e != nil {
				return hovel.PayloadCommandResult{}, e
			}
			slices.SortFunc(expected, func(a, b State) int { return strings.Compare(a.Creation, b.Creation) })
			if !slices.Equal(current, expected) {
				return hovel.PayloadCommandResult{}, fmt.Errorf("inventory changed; review quit again")
			}
			for _, s := range expected {
				if e := m.connections[s.Creation].Close("reviewed quit"); e != nil {
					return hovel.PayloadCommandResult{}, e
				}
			}
			value = map[string]string{"state": "closed"}
		default:
			return hovel.PayloadCommandResult{}, fmt.Errorf("bounded manager command required")
		}
	}
	b, e := json.Marshal(value)
	return hovel.PayloadCommandResult{Command: req.Command, Stdout: string(b)}, e
}

func (m *manager) connect(raw, review, runID string) (State, error) {
	defer launch.Phase("manager-connect")()
	dispatch := launch.Monotonic()
	r, e := decodeManagerRequest(raw)
	if e != nil || digest(raw) != review || runID == "" {
		return State{}, fmt.Errorf("request changed or missing run correlation")
	}
	// Resolve outside the admission lock; a config helper cannot stall sibling controls.
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resolved, config, e := r.Settings.generated(c)
	if e != nil {
		return State{}, e
	}
	if string(config) != r.Preview {
		return State{}, fmt.Errorf("SSH settings changed after review; review again")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || r.Owner != m.managerIdentity {
		return State{}, fmt.Errorf("owner binding changed; no automatic adoption")
	}
	if _, ok := m.connections[r.ID]; ok {
		return State{}, fmt.Errorf("creation already submitted; inspect instead of retrying")
	}
	if e = launch.VerifyReservation(c, m.Workspace, m.dir); e != nil {
		return State{}, e
	}
	dir, e := launch.ReserveConnection(c, m.Workspace, r.Settings.Name)
	if e != nil {
		return State{}, e
	}
	s := &owner{profile: saved(r.Settings), config: resolved, prepared: config, dir: dir, done: make(chan struct{}), manager: m}
	s.state = State{Name: resolved.Name, Host: resolved.Host, User: resolved.User, Port: resolved.Port, State: "connecting", Socket: filepath.Join(dir.Name(), "master"), Session: m.Session, Generation: m.Generation, Creation: r.ID, RunID: runID, OwnerPID: m.OwnerPID, Dispatch: dispatch}
	m.connections[r.ID] = s
	initial := s.state
	s.Open()
	m.milestone("approved connection dispatched")
	return initial, nil
}

func runManager(ctx *hovel.Context) (hovel.Result, error) {
	defer launch.Phase("module:" + ctx.InputString("action", ""))()
	c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	w := ctx.InputString("workspace", "")
	info, e := launch.Status(c, w)
	if e != nil {
		return hovel.Result{}, e
	}
	if os.Getppid() != info.PID {
		return hovel.Result{}, fmt.Errorf("manager adapter requires verified workspace daemon")
	}
	build, e := launch.Build()
	if e != nil {
		return hovel.Result{}, e
	}
	if ctx.InputString("build", "") != build {
		return hovel.Result{}, fmt.Errorf("%s", buildMismatch)
	}
	if ctx.InputString("command", "") != "" || ctx.InputString("connection", "") != "" {
		return hovel.Result{}, fmt.Errorf("manager action excludes legacy connection and profile commands")
	}
	switch ctx.InputString("action", "") {
	case "activate":
		generation := ctx.InputString("generation", "")
		if generation == "" {
			return hovel.Result{}, fmt.Errorf("activation generation required")
		}
		dir, e := launch.ReserveManager(c, w)
		if e != nil {
			return hovel.Result{}, e
		}
		m := &manager{managerIdentity: managerIdentity{Workspace: w, Generation: generation, OwnerPID: os.Getpid(), RunID: ctx.RunID}, dir: dir, connections: map[string]*owner{}, log: ctx.Log}
		ref, e := ctx.OpenSession(m, hovel.WithName("Burrow manager"), hovel.WithKind(managerKind))
		if e != nil {
			dir.Close()
			return hovel.Result{}, e
		}
		m.mu.Lock()
		m.Session = ref.ID
		m.mu.Unlock()
		m.milestone("manager activated; no connection permission granted")
		b, _ := json.Marshal(m.managerIdentity)
		return hovel.Ok(nil, hovel.WithSummary(string(b))), nil
	case "connect":
		raw := ctx.InputString("request", "")
		r, e := decodeManagerRequest(raw)
		if e != nil || r.Owner.Workspace != w || r.Owner.Session != ctx.InputString("session", "") || r.Owner.Generation != ctx.InputString("generation", "") || digest(raw) != ctx.InputString("review", "") {
			return hovel.Result{}, fmt.Errorf("changed request or owner refused")
		}
		result, e := ownerCommand(c, w, r.Owner.Session, "connect", []string{raw, digest(raw), ctx.RunID})
		if e != nil {
			return hovel.Result{}, e
		}
		var state State
		if json.Unmarshal([]byte(result.Stdout), &state) != nil || state.Creation != r.ID || state.Session != r.Owner.Session || state.Generation != r.Owner.Generation || state.RunID != ctx.RunID {
			return hovel.Result{}, fmt.Errorf("creation correlation refused")
		}
		return hovel.Ok(nil, hovel.WithSummary(result.Stdout)), nil
	}
	return hovel.Result{}, fmt.Errorf("unsupported manager action")
}

func managerControl(ctx context.Context, w string, id managerIdentity, command string, args []string, out any) error {
	if command != "identity" {
		args = append([]string{id.Generation}, args...)
	}
	result, e := ownerCommand(ctx, w, id.Session, command, args)
	if e != nil {
		return e
	}
	return json.Unmarshal([]byte(result.Stdout), out)
}

func findManager(ctx context.Context, w string) (managerIdentity, error) {
	defer launch.Phase("find-manager")()
	var refs struct{ Sessions []hovel.SessionRef }
	if e := launch.Call(ctx, w, "ListSessions", map[string]any{}, &refs); e != nil {
		return managerIdentity{}, e
	}
	var found managerIdentity
	for _, ref := range refs.Sessions {
		if ref.ModuleID != "burrow@0.1.0" || ref.Kind == "connection" || ref.State == "closed" {
			continue
		}
		if ref.Kind != managerKind {
			return found, fmt.Errorf("incompatible retained Burrow owner; keep its binary for explicit close; no adoption or automatic replacement")
		}
		var id managerIdentity
		if e := managerControl(ctx, w, managerIdentity{Session: ref.ID}, "identity", nil, &id); e != nil {
			return found, fmt.Errorf("manager identity unverified; preserve resources and inspect Hovel sessions")
		}
		if id.Session != ref.ID || id.Workspace != w || id.Generation == "" || id.OwnerPID <= 0 || found.Session != "" {
			return found, fmt.Errorf("ambiguous or changed manager identity; no adoption")
		}
		found = id
	}
	return found, nil
}

// Isolated request chains preserve the reviewed binding across independent frontends.
func managerThrow(ctx context.Context, w string, config map[string]string, out any) error {
	defer launch.Phase("manager-throw:" + config["action"])()
	op := "burrow-" + rand.Text()
	build, e := launch.Build()
	if e != nil {
		return e
	}
	for _, call := range []struct {
		method string
		input  any
	}{
		{"CreateOperation", map[string]string{"Operation": op}},
		{"CreateChain", map[string]string{"Operation": op, "Chain": "request"}},
		{"AddModule", map[string]string{"Operation": op, "Chain": "request", "ModuleID": "burrow@0.1.0"}},
		{"AddTarget", map[string]string{"Operation": op, "Chain": "request", "Target": "local://burrow-manager"}},
	} {
		var result any
		if e := launch.Call(ctx, w, call.method, call.input, &result); e != nil {
			if call.method == "AddModule" {
				return fmt.Errorf("Burrow module is not registered in this daemon; run burrow status to register this build: %w", e)
			}
			return e
		}
	}
	config["workspace"] = w
	config["build"] = build
	for key, value := range config {
		var result any
		if e := launch.Call(ctx, w, "SetChainConfig", map[string]string{"Operation": op, "Chain": "request", "Key": key, "Value": value}, &result); e != nil {
			return e
		}
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	// Once dispatched, finish the receipt so cancellation can close the exact
	// creation; cancellation before dispatch leaves SSH untouched.
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	defer cancel()
	b, e := launch.HovelCLI(finish, w, "--op", op, "--chain", "request", "--", "throw", "--now", "--allow-dangerous", "--json")
	if e != nil {
		return e
	}
	var result struct {
		Results []struct {
			State, Summary, RunID string
			Logs                  []struct{ Fields map[string]string }
		}
	}
	if json.Unmarshal(b, &result) != nil || len(result.Results) != 1 || result.Results[0].State != "succeeded" {
		// Surface only the adapter's fixed pre-dispatch refusal; other run
		// diagnostics stay in Hovel history rather than in this error.
		for _, entry := range result.Results {
			for _, log := range entry.Logs {
				if strings.HasSuffix(log.Fields["error"], buildMismatch) {
					return fmt.Errorf("manager throw refused before dispatch: %s", buildMismatch)
				}
			}
		}
		return fmt.Errorf("manager throw failed; inspect Hovel history; do not retry automatically")
	}
	var correlation struct {
		RunID string `json:"runID"`
	}
	if json.Unmarshal([]byte(result.Results[0].Summary), &correlation) != nil || correlation.RunID != result.Results[0].RunID {
		return fmt.Errorf("throw run correlation refused")
	}
	return json.Unmarshal([]byte(result.Results[0].Summary), out)
}

// Module registration is startup work (status/tui open). Each throw carries
// the frontend build digest, and the adapter refuses another build before
// dispatch, so the daemon's inventory is not re-read per connection.
func connectManaged(ctx context.Context, c Config, preview string) (State, error) {
	defer launch.Phase("connect-managed")()
	id, e := findManager(ctx, c.Workspace)
	if e != nil {
		return State{}, e
	}
	if id.Session == "" {
		generation := rand.Text()
		if e = managerThrow(ctx, c.Workspace, map[string]string{"action": "activate", "generation": generation}, &id); e != nil {
			// The exact activation can be observed after a lost acknowledgement, but
			// a different winner is not silently adopted and activation is never retried.
			check, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			observed, err := findManager(check, c.Workspace)
			if err != nil || observed.Generation != generation {
				return State{}, fmt.Errorf("activation uncertain or another frontend won; inspect connections before retrying: %w", e)
			}
			id = observed
		}
	}
	r := managerRequest{ID: digest(rand.Text())[:32], Owner: id, Settings: c, Preview: preview}
	raw, _ := json.Marshal(r)
	var state State
	e = managerThrow(ctx, c.Workspace, map[string]string{"action": "connect", "generation": id.Generation, "session": id.Session, "request": string(raw), "review": digest(string(raw))}, &state)
	if e != nil {
		check, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		var states []State
		if managerControl(check, c.Workspace, id, "list", nil, &states) == nil {
			for _, s := range states {
				if s.Creation == r.ID {
					return s, nil
				}
			}
		}
		return State{}, fmt.Errorf("connect outcome unverified (creation %s); inspect before retrying: %w", r.ID, e)
	}
	if state.Creation != r.ID || state.Session != id.Session || state.Generation != id.Generation {
		return State{}, fmt.Errorf("connection result identity changed; inspect before retrying")
	}
	return state, nil
}

// CloseInventory rechecks the manager's whole review under its admission lock.
// Partial cross-workspace/legacy cleanup never promises rollback.
func CloseInventory(ctx context.Context, w string, expected []State) error {
	current, e := List(ctx, w)
	if e != nil {
		return e
	}
	if !slices.Equal(current, expected) {
		return fmt.Errorf("inventory changed; review quit again")
	}
	var managed []State
	for _, s := range expected {
		if s.Generation != "" {
			managed = append(managed, s)
		} else {
			if e = closeOwned(ctx, w, s); e != nil {
				return e
			}
		}
	}
	if len(managed) > 0 {
		b, _ := json.Marshal(managed)
		var result any
		return managerControl(ctx, w, managerIdentity{Session: managed[0].Session, Generation: managed[0].Generation}, "close-reviewed", []string{string(b)}, &result)
	}
	return nil
}
