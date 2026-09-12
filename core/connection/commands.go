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
connect                             Guided connection entry (terminal)
connect NAME HOST USER [options]     Create shell-free SSH master
reconnect NAME HOST USER [options]   Explicitly replace a lost owned connection
inspect NAME                        State, endpoint and socket identity
close NAME [--yes]                   Review/close all owned connection resources

Required: NAME HOST USER (- uses SSH config user), or:
connect -ip HOST -port NUMBER -user USER -socket NAME [-ssh-key PATH]
Named flags may appear in any order; duplicate fields/aliases are refused.
Legacy -proxy, -shell and -no-term are unsupported, never silently accepted.
Options (required fields are shown before optional settings):
--key PATH, --agent PATH (SSH_AUTH_SOCK default), --port NUMBER (config/22),
--ssh-config PATH (~/.ssh/config), --jump [USER@]HOST[:PORT][,...],
--known-hosts PATH (~/.ssh/known_hosts), --trust SHA256:FINGERPRINT,
--prompt (CLI hidden secret entry and host approval), --yes (confirm review).
TUI connects review and prompt interactively; Ctrl+C/Esc cancels the attempt.
CLI without --yes reviews; --prompt waits for authentication and cleans failure.
Passwords/passphrases are terminal-only: never put secrets in commands.
Aliases use OpenSSH HostName/User/Port/IdentityFile/IdentityAgent/ProxyJump.
ProxyCommand and config forwarding/commands/trust overrides are not imported.
Agent sockets must be accessible to the daemon; select the current socket with
frontend SSH_AUTH_SOCK or --agent. No vault or daemon environment refresh.
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

func optionName(arg string) string {
	name := strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-")
	switch name {
	case "ip":
		return "host"
	case "socket":
		return "name"
	case "ssh-key":
		return "key"
	}
	return name
}

func Parse(workspace string, args []string) (Config, bool, error) {
	c := Config{Workspace: workspace, Agent: os.Getenv("SSH_AUTH_SOCK")}
	home, e := os.UserHomeDir()
	if e != nil {
		return c, false, e
	}
	c.KnownHosts = filepath.Join(home, ".ssh", "known_hosts")
	c.SSHConfig = filepath.Join(home, ".ssh", "config")
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&c.Name, "name", "", "connection name")
	fs.StringVar(&c.Host, "host", "", "host or alias")
	fs.StringVar(&c.User, "user", "", "username")
	fs.StringVar(&c.Key, "key", "", "key path")
	fs.StringVar(&c.Agent, "agent", c.Agent, "agent socket")
	fs.StringVar(&c.KnownHosts, "known-hosts", c.KnownHosts, "known-hosts path")
	fs.StringVar(&c.Trust, "trust", "", "approved fingerprint")
	fs.StringVar(&c.SSHConfig, "ssh-config", c.SSHConfig, "OpenSSH configuration")
	fs.StringVar(&c.Jump, "jump", "", "jump hosts")
	fs.BoolVar(&c.Prompt, "prompt", false, "private terminal authentication")
	fs.IntVar(&c.Port, "port", 0, "SSH port (configuration or 22)")
	yes := fs.Bool("yes", false, "confirm reviewed operation")
	// Normalize the pinned LazySSH spellings, then let flag validate values.
	// Required positionals and named options can be interspersed; duplicates
	// are refused rather than silently choosing a different endpoint or key.
	seen := map[string]bool{}
	var options, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		name, value, assigned := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		name = optionName(name)
		f := fs.Lookup(name)
		if f == nil {
			return c, false, fmt.Errorf("invalid connection options; use help (secrets are not accepted)")
		}
		// Interactive review appends --yes after an explicit --yes=false.
		// Only this approval switch may repeat; connection fields stay unique.
		if seen[name] && name != "yes" {
			return c, false, fmt.Errorf("duplicate connection option %s", name)
		}
		seen[name] = true
		if !assigned && name != "yes" && name != "prompt" {
			i++
			if i == len(args) {
				return c, false, fmt.Errorf("missing value for %s; required NAME HOST USER; use help", name)
			}
			value, assigned = args[i], true
		}
		option := "--" + name
		if assigned {
			option += "=" + value
		}
		options = append(options, option)
	}
	for i, value := range positional {
		if i >= 3 {
			return c, false, fmt.Errorf("expected NAME HOST USER and options; use help")
		}
		name := []string{"name", "host", "user"}[i]
		if seen[name] {
			return c, false, fmt.Errorf("duplicate connection option %s", name)
		}
		seen[name] = true
		options = append(options, "--"+name+"="+value)
	}
	if !seen["name"] || !seen["host"] || !seen["user"] {
		return c, false, fmt.Errorf("required NAME HOST USER (or -socket NAME -ip HOST -user USER); bare connect opens the form")
	}
	if e = fs.Parse(options); e != nil {
		return c, false, fmt.Errorf("invalid connection options; use help (secrets are not accepted)")
	}
	if _, e := launch.ConnectionPath(workspace, c.Name); e != nil {
		return c, false, e
	}
	var invalidPort bool
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "agent" {
			c.AgentExplicit = true
		}
		if f.Name == "port" && c.Port == 0 {
			invalidPort = true
		}
	})
	if invalidPort {
		return c, false, fmt.Errorf("port must be 1–65535")
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

func closeReview(s State) string {
	return fmt.Sprintf("Close %s (%s@%s:%d), state %s, master PID %d, socket %s. Ends all owned connection access; saved settings and artifacts remain. Repeat close %s --yes to confirm.", s.Name, s.User, s.Host, s.Port, s.State, s.MasterPID, s.Socket, s.Name)
}

// ReviewClose binds the displayed consequence to the exact observed owner.
func ReviewClose(ctx context.Context, w, name string) (State, string, error) {
	s, e := selected(ctx, w, name)
	if e != nil {
		return s, "", e
	}
	return s, closeReview(s), nil
}

// CloseReviewed refuses replacement owners after a frontend has shown a target.
func CloseReviewed(ctx context.Context, w string, expected State) (any, error) {
	current, e := selected(ctx, w, expected.Name)
	if e != nil {
		return nil, e
	}
	if current.Session != expected.Session || current.MasterPID != expected.MasterPID || current.Socket != expected.Socket || current.SocketInode != expected.SocketInode || current.State != expected.State {
		return nil, fmt.Errorf("connection changed after review; inspect and review close again")
	}
	if e := closeOwned(ctx, w, expected); e != nil {
		return nil, e
	}
	return map[string]string{"state": "closed", "name": expected.Name}, nil
}

// Execute is the shared CLI/TUI command boundary. Hovel remains the state owner.
func Execute(ctx context.Context, w string, args []string) (any, error) {
	return execute(ctx, w, args, "")
}

func execute(ctx context.Context, w string, args []string, promptSocket string) (any, error) {
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
		review := closeReview(s)
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
		c, e = c.resolve(ctx)
		if e != nil {
			return nil, e
		}
		return map[string]string{"review": fmt.Sprintf("%s %s\nEndpoint: %s@%s:%d\nSSH config: %s\nJump: %s\nKey: %s\nAgent: %s\nHost trust is verified. Retain a shell-free master after frontend quit. Repeat with --yes to confirm.", args[0], c.Name, c.User, c.Host, c.Port, c.SSHConfig, displaySetting(c.Jump, "none"), displaySetting(c.Key, "SSH config/default identities"), displaySetting(c.Agent, "none"))}, nil
	}
	if c.Prompt && promptSocket == "" {
		return nil, fmt.Errorf("--prompt requires a private interactive frontend; secrets cannot be supplied as command inputs")
	}
	c.PromptSocket = promptSocket
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
	if e = launch.RegisterModule(ctx, w, "burrow-connection@0.1.0", []byte(manifest)); e != nil {
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

func displaySetting(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func Suggestions(states []State) []string {
	values := []string{"status", "connections", "connect", "connect ", "help", "quit"}
	for _, s := range states {
		for _, verb := range []string{"inspect", "close", "reconnect"} {
			values = append(values, verb+" "+s.Name)
		}
	}
	return values
}

func CommandSuggestions(line string, states []State) []string {
	args, e := Split(line)
	if e != nil || len(args) < 1 || (args[0] != "connect" && args[0] != "reconnect") {
		return Suggestions(states)
	}
	start := strings.LastIndexByte(line, ' ') + 1
	if start < 1 || (start < len(line) && !strings.HasPrefix(line[start:], "-")) {
		return nil
	}
	if start == len(line) && len(args) > 1 && strings.HasPrefix(args[len(args)-1], "-") && !strings.Contains(args[len(args)-1], "=") {
		switch optionName(args[len(args)-1]) {
		case "key", "agent", "port", "ssh-config", "jump", "known-hosts", "trust", "host", "name", "user":
			return nil
		}
	}
	// Guide named entry in LazySSH order; positional commands retain their
	// established NAME HOST USER syntax and go straight to optional flags.
	seen := map[string]bool{}
	positionals := 0
	for i := 1; i < len(args); i++ {
		name := optionName(args[i])
		if strings.HasPrefix(args[i], "-") {
			seen[name] = true
			if !strings.Contains(args[i], "=") && name != "yes" && name != "prompt" {
				i++
			}
		} else {
			positionals++
		}
	}
	options := []string{"-ssh-key ", "--key ", "--agent ", "--port ", "--ssh-config ", "--jump ", "--known-hosts ", "--trust ", "--prompt", "--yes"}
	if positionals == 0 {
		for _, required := range [][2]string{{"host", "-ip "}, {"port", "-port "}, {"user", "-user "}, {"name", "-socket "}} {
			if !seen[required[0]] {
				return []string{line[:start] + required[1]}
			}
		}
	}
	values := []string{}
	for _, option := range options {
		if !seen[optionName(strings.TrimSpace(option))] {
			values = append(values, line[:start]+option)
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
