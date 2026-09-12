package connection

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Bochner/burrow/core/launch"
	"github.com/vibepwners/hovel/sdk/go/hovel"
)

const Help = `connections                         Inspect retained owners
connect NAME HOST USER [options]     Create shell-free SSH master
reconnect NAME HOST USER [options]   Explicitly replace a lost owned connection
inspect NAME                        State, endpoint and socket identity
close NAME [--yes]                   Review/close all owned connection resources

Required first: NAME HOST USER. Options: --key PATH or --agent PATH,
--port NUMBER (22), --known-hosts PATH (~/.ssh/known_hosts),
--trust SHA256:FINGERPRINT (explicit unknown-host approval), --yes.
Without --yes, connect/close only show a review. No passwords in commands.
Keys must be unencrypted or already unlocked in the selected SSH agent.
Unknown reservations require manual investigation; no force adoption.
`

const manifest = `apiVersion: hovel.dev/v1alpha1
kind: ModulePackage
metadata:
  name: burrow-connection
  version: 0.1.0
  moduleType: survey
  summary: Manage a retained named SSH connection.
  license: Apache-2.0
runtime:
  protocol: jsonrpc-stdio
launch:
  - selector:
      os: linux
      arch: amd64
    command: ["burrow", "connection-module"]
`

func Parse(workspace string, args []string) (Config, bool, error) {
	c := Config{Workspace: workspace, Port: 22, Agent: os.Getenv("SSH_AUTH_SOCK")}
	if len(args) < 3 {
		return c, false, fmt.Errorf("required first: NAME HOST USER; use help")
	}
	c.Name, c.Host, c.User = args[0], args[1], args[2]
	if _, e := launch.ConnectionPath(workspace, c.Name); e != nil {
		return c, false, e
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return c, false, e
	}
	c.KnownHosts = filepath.Join(home, ".ssh", "known_hosts")
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&c.Key, "key", "", "key path")
	fs.StringVar(&c.Agent, "agent", c.Agent, "agent socket")
	fs.StringVar(&c.KnownHosts, "known-hosts", c.KnownHosts, "known-hosts path")
	fs.StringVar(&c.Trust, "trust", "", "approved fingerprint")
	fs.IntVar(&c.Port, "port", 22, "SSH port")
	yes := fs.Bool("yes", false, "confirm reviewed operation")
	if e = fs.Parse(args[3:]); e != nil || fs.NArg() != 0 {
		return c, false, fmt.Errorf("invalid connection options; use help (secrets are not accepted)")
	}
	return c, *yes, c.Validate()
}

// ValidateCommand runs before setup so malformed requests cannot mutate a workspace.
func ValidateCommand(workspace string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("connection command required")
	}
	switch args[0] {
	case "connect", "reconnect":
		_, _, e := Parse(workspace, args[1:])
		return e
	case "connections":
		if len(args) != 1 {
			return fmt.Errorf("connections takes no arguments")
		}
	case "inspect", "close":
		if len(args) < 2 || len(args) > 3 || (len(args) == 3 && (args[0] != "close" || args[2] != "--yes")) {
			return fmt.Errorf("expected %s NAME%s", args[0], map[string]string{"close": " [--yes]"}[args[0]])
		}
		_, e := launch.ConnectionPath(workspace, args[1])
		return e
	default:
		return fmt.Errorf("unknown command; use help")
	}
	return nil
}

func ownerCommand(ctx context.Context, workspace, id, command string, args []string) (hovel.PayloadCommandResult, error) {
	var result hovel.PayloadCommandResult
	e := launch.Call(ctx, workspace, "RunSessionCommand", map[string]any{"SessionID": id, "Request": hovel.PayloadCommandRequest{Command: command, Args: args}}, &result)
	return result, e
}
func List(ctx context.Context, workspace string) ([]State, error) {
	var refs struct{ Sessions []hovel.SessionRef }
	if e := launch.Call(ctx, workspace, "ListSessions", map[string]any{}, &refs); e != nil {
		return nil, e
	}
	states := []State{}
	names := map[string]bool{}
	for _, ref := range refs.Sessions {
		if ref.ModuleID != "burrow-connection@0.1.0" || ref.Kind != "connection" || ref.State == "closed" {
			continue
		}
		result, e := ownerCommand(ctx, workspace, ref.ID, "connection-status", nil)
		socket, _ := launch.ConnectionPath(workspace, ref.Name)
		s := State{Name: ref.Name, Host: ref.Target, Socket: socket, State: "lost", Session: ref.ID, Detail: "owner unavailable; unknown runtime resources preserved; investigate manually"}
		if e == nil {
			if e = json.Unmarshal([]byte(result.Stdout), &s); e != nil {
				return nil, fmt.Errorf("invalid retained owner response")
			}
			s.Session = ref.ID
		}
		if s.State != "closed" {
			states = append(states, s)
			names[s.Name] = true
		}
	}
	// A dead module can disappear from Hovel's live sessions while its reservation
	// remains. Report that conflict without treating filesystem entries as owners.
	entries, e := os.ReadDir(filepath.Join(workspace, "burrow"))
	if e != nil {
		return nil, fmt.Errorf("cannot inspect connection reservations: %w", e)
	}
	for _, entry := range entries {
		socket, e := launch.ConnectionPath(workspace, entry.Name())
		if e == nil && !names[entry.Name()] {
			states = append(states, State{Name: entry.Name(), Socket: socket, State: "lost", Detail: "unidentified runtime reservation; owner unavailable; investigate manually"})
		}
	}
	sort.Slice(states, func(i, j int) bool { return states[i].Name < states[j].Name })
	return states, nil
}
func selected(ctx context.Context, w, name string) (State, error) {
	states, e := List(ctx, w)
	if e != nil {
		return State{}, e
	}
	var found []State
	for _, s := range states {
		if s.Name == name {
			found = append(found, s)
		}
	}
	if len(found) != 1 {
		return State{}, fmt.Errorf("select exactly one live retained owner for %q; unknown reservations are never adopted", name)
	}
	return found[0], nil
}
func closeOwned(ctx context.Context, w string, s State) error {
	if s.Session == "" {
		return fmt.Errorf("owner unavailable; inspect %q before manual recovery; unknown resources were not removed", s.Socket)
	}
	if _, e := ownerCommand(ctx, w, s.Session, "connection-close", []string{"confirm"}); e != nil {
		return fmt.Errorf("connection close failed or cleanup is uncertain; inspect %q and Hovel session %s; resources were not force-removed", s.Socket, s.Session)
	}
	var result map[string]any
	return launch.Call(ctx, w, "CloseSession", map[string]string{"SessionID": s.Session}, &result)
}

// Execute is the shared CLI/TUI command boundary. Hovel remains the state owner.
func Execute(ctx context.Context, w string, args []string) (any, error) {
	if e := ValidateCommand(w, args); e != nil {
		return nil, e
	}
	switch args[0] {
	case "connections":
		return List(ctx, w)
	case "inspect":
		return selected(ctx, w, args[1])
	case "close":
		s, e := selected(ctx, w, args[1])
		if e != nil {
			return nil, e
		}
		review := fmt.Sprintf("Close %s (%s@%s:%d), state %s, master PID %d, socket %s. Ends all owned connection access; saved settings and artifacts remain. Repeat close %s --yes to confirm.", s.Name, s.User, s.Host, s.Port, s.State, s.MasterPID, s.Socket, s.Name)
		if len(args) == 2 {
			return map[string]string{"review": review}, nil
		}
		if e = closeOwned(ctx, w, s); e != nil {
			return nil, e
		}
		return map[string]string{"state": "closed", "name": s.Name}, nil
	}
	c, yes, e := Parse(w, args[1:])
	if e != nil {
		return nil, e
	}
	if !yes {
		return map[string]string{"review": fmt.Sprintf("%s %s: authenticate %s@%s:%d using key/agent, retaining a shell-free master after frontend quit. Repeat with --yes to confirm.", args[0], c.Name, c.User, c.Host, c.Port)}, nil
	}
	if args[0] == "reconnect" {
		s, e := selected(ctx, w, c.Name)
		if e != nil {
			return nil, e
		}
		if s.State != "lost" {
			return nil, fmt.Errorf("reconnect requires a lost connection; close an active connection explicitly")
		}
		if e = closeOwned(ctx, w, s); e != nil {
			return nil, e
		}
	}
	// Refuse collisions before installing/configuring anything. The module's mkdir
	// is the authoritative atomic reservation against simultaneous requests.
	path, _ := launch.ConnectionPath(w, c.Name)
	if _, e = os.Lstat(filepath.Dir(path)); !os.IsNotExist(e) {
		return nil, fmt.Errorf("connection reservation %q exists or cannot be inspected; inspect ownership before manual recovery", filepath.Dir(path))
	}
	module, e := launch.CacheModule(ctx, []byte(manifest))
	if e != nil {
		return nil, e
	}
	if _, e = launch.HovelCLI(ctx, w, "--", "module", "install", "--link", module, "--no-scripts"); e != nil {
		return nil, e
	}
	chain := "ssh-" + rand.Text()[:12]
	prefix := []string{"--op", "burrow", "--chain", chain, "--"}
	config, _ := json.Marshal(c)
	for _, cmd := range [][]string{{"op", "create", "burrow"}, {"chain", "create", chain}, {"chain", "add", "burrow-connection@0.1.0"}, {"target", "add", "ssh://" + c.Host}, {"chain", "config", "set", "connection", string(config)}} {
		if _, e = launch.HovelCLI(ctx, w, append(append([]string{}, prefix...), cmd...)...); e != nil {
			return nil, e
		}
	}
	// --now is Hovel's explicit single-operator confirmation, not an unconfirmed
	// ExecuteModule RPC; Hovel still checks dangerous allowance and launch policy.
	data, e := launch.HovelCLI(ctx, w, append(prefix, "throw", "--now", "--allow-dangerous", "--json")...)
	if e != nil {
		return nil, e
	}
	var result struct {
		Results []struct {
			State    string
			Sessions []hovel.SessionRef
		}
	}
	if e = json.Unmarshal(data, &result); e != nil {
		return nil, fmt.Errorf("invalid Hovel throw response")
	}
	if len(result.Results) != 1 || result.Results[0].State != "succeeded" || len(result.Results[0].Sessions) != 1 {
		return nil, fmt.Errorf("connection launch failed; inspect Hovel throw history")
	}
	return selected(ctx, w, c.Name)
}

func Suggestions(states []State) []string {
	values := []string{"status", "connections", "connect ", "help", "quit"}
	for _, s := range states {
		for _, verb := range []string{"inspect", "close", "reconnect"} {
			values = append(values, verb+" "+s.Name)
		}
	}
	return values
}

// Split supports quoted file paths without a shell or command substitution.
func Split(line string) ([]string, error) {
	var args []string
	var token strings.Builder
	var quote rune
	escaped, started := false, false
	for _, r := range line {
		if escaped {
			token.WriteRune(r)
			escaped = false
			started = true
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				token.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			started = true
			continue
		}
		if r == ' ' || r == '\t' {
			if started {
				args = append(args, token.String())
				token.Reset()
				started = false
			}
			continue
		}
		token.WriteRune(r)
		started = true
	}
	if escaped || quote != 0 {
		return nil, fmt.Errorf("unfinished quote or escape")
	}
	if started {
		args = append(args, token.String())
	}
	return args, nil
}
