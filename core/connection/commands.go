package connection

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Bochner/burrow/core/launch"
	"github.com/vibepwners/hovel/sdk/go/hovel"
)

const Help = `profiles                            List saved entries and selected collection
profile create NAME HOST USER [options] Save settings without connecting
profile select NAME                 Inspect saved settings only
profile connect NAME [--as LIVE] [--yes] [--prompt] Connect a saved profile
profile save CONNECTION [--as PROFILE] [--yes] Save authenticated settings
profile edit NAME HOST USER [options] Replace saved settings (--yes confirms)
profile delete NAME [--yes]          Delete settings; retain live resources
profile collection PATH             Create/open a collection
profile load PATH                   Open an existing collection; never connect
profile backup PATH                 Copy collection to a new backup file
history                             Retained profile management commands
Profiles keep aliases/overrides and key references, never authentication secrets.
Default template: WORKSPACE/burrow-profiles.json; _example is documentation only.
All profile writes use the selected file, retained by Hovel across invocations.
Edit replaces all settings; include options to retain them. Review replies include
revision/collection; repeat --revision HASH --collection PATH --yes to pin review.

connections                         Inspect retained owners
tunnel create CONNECTION forward LISTEN HOST PORT [--yes] Create local tunnel
tunnel create CONNECTION reverse LISTEN HOST PORT [--yes] Create reverse tunnel
tunc CONNECTION l LISTEN HOST PORT [--yes] Alias for tunnel create ... forward
tunc CONNECTION r LISTEN HOST PORT [--yes] Alias for tunnel create ... reverse
tunnel list                         List retained tunnels and exact IDs
tunnel remove CONNECTION/ID [--yes] Review/remove exactly one listener
tund CONNECTION/ID [--yes]          Alias for tunnel remove
tunnel check CONNECTION/ID          Passive greeting check; no remote bytes retained
LISTEN is PORT (127.0.0.1 default) or IP:PORT; broader binds require explicit IP.
Forward: local listener, remote destination reached from the SSH server.
Reverse: remote listener, local destination reached from the SSH client.
HOST may be bare IPv6. Reverse LISTEN 0 requests a random high port (49152–65535);
up to eight real bind attempts; inventory reports the successfully assigned endpoint.
Reverse exposure requires Linux /proc/net/tcp tables and remote shell awk/od.
GatewayPorts overrides are checked after allocation; mismatches are removed.
The listener may briefly have the server-forced exposure before cleanup.
Reverse checks originate on the server and require AllowTcpForwarding/PermitOpen.
Creation uses a confirmed Hovel throw. --review HASH binds --yes to the recap.
Keep-running quit retains forwards; connection close/loss ends their listeners.
Removal stops new connections; already accepted streams may finish.
Silent protocols need their normal client to verify traffic; a bound port alone
does not prove destination reachability or server forwarding permission.
Tunnel IDs include an opaque creation identity: complete with Tab or use tunnel list.

connect                             Guided connection entry (terminal)
connect NAME HOST USER [options]     Create shell-free SSH master
reconnect NAME HOST USER [options]   Explicitly replace a lost owned connection
inspect NAME                        State, endpoint and socket identity
shell NAME                          Open a local interactive SSH terminal
shells                              List this frontend's shells in the workspace
resume ID                           Resume a local shell by ID
shell-close [ID]                     Close ID, or the last selected local shell
close NAME [--yes]                   Review/close all owned connection resources
Shells reuse a verified master; no fresh login or authentication fallback.
Ctrl+] returns to management, keeping the shell; Ctrl+C reaches SSH.
Select a Shells entry or use resume ID to return; shell-close ends only that client.
Multiple shells share the connection's existing master socket and authentication.
Shell exit/close preserves the connection. Frontend quit ends local shells.
Connection close ends its shells, transfers and tunnels. Shell bytes stay local,
in memory; they are not Hovel-recorded session I/O or collected evidence.

Required: NAME HOST USER (- uses SSH config user), or:
connect -ip HOST -port NUMBER -user USER -socket NAME [-ssh-key PATH]
Named flags may appear in any order; duplicate fields/aliases are refused.
Legacy -shell and -no-term are unsupported, never silently accepted.
Options (required fields are shown before optional settings):
-proxy [PORT] (local SOCKS4/5 on 127.0.0.1, default 9050; omitted = off),
--key PATH, --agent PATH (SSH_AUTH_SOCK default), --port NUMBER (config/22),
--ssh-config PATH (~/.ssh/config), --jump [USER@]HOST[:PORT][,...],
--prompt (CLI hidden password/passphrase entry), --yes (confirm review),
--review HASH (bind --yes to the exact previously displayed recap).
Every hop uses UserKnownHostsFile=/dev/null and StrictHostKeyChecking=no.
There is no host-key approval.
TUI connects review and prompt interactively; Ctrl+C/Esc cancels the attempt.
CLI without --yes reviews; --prompt waits for authentication and cleans failure.
Passwords/passphrases are terminal-only: never put secrets in commands.
Aliases use OpenSSH HostName/User/Port/IdentityFile/IdentityAgent/ProxyJump.
ProxyCommand and config forwarding/commands/trust overrides are not imported.
SOCKS is separate from --jump: configure tools with socks5 127.0.0.1 PORT.
The connection owns its proxy; close/loss ends it, leaving other listeners alone.
Agent sockets must be accessible to the daemon; select the current socket with
frontend SSH_AUTH_SOCK or --agent. No vault or daemon environment refresh.
Hovel catalog/chain identity: burrow@0.1.0 for every capability.
Hovel throws require --allow-dangerous, including workspace/profile actions.
Existing burrow-connection@0.1.0 owners remain inspectable/closeable; no adoption.
Earlier burrow@0.1.0 connection sessions also remain separate from the manager.
Old connection-JSON chain submissions are retired; use Burrow connect.
Manager failure may end all workspace connections; reconnect is always explicit.
Retire old chain submissions, close their owners explicitly, then manually
uninstall the old module through Hovel; retain chains/evidence for inspection.
Unknown reservations require manual investigation; no force adoption.
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
	c.SSHConfig = filepath.Join(home, ".ssh", "config")
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&c.Name, "name", "", "connection name")
	fs.StringVar(&c.Host, "host", "", "host or alias")
	fs.StringVar(&c.User, "user", "", "username")
	fs.StringVar(&c.Key, "key", "", "key path")
	fs.StringVar(&c.Agent, "agent", c.Agent, "agent socket")
	fs.StringVar(&c.SSHConfig, "ssh-config", c.SSHConfig, "OpenSSH configuration")
	fs.StringVar(&c.Jump, "jump", "", "jump hosts")
	fs.StringVar(&c.Review, "review", "", "exact SSH preview digest")
	fs.BoolVar(&c.Prompt, "prompt", false, "private terminal authentication")
	fs.IntVar(&c.Port, "port", 0, "SSH port (configuration or 22)")
	fs.IntVar(&c.ProxyPort, "proxy", 0, "local SOCKS port (9050 when flag is bare)")
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
		if !assigned && name == "proxy" {
			value, assigned = "9050", true
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				value = args[i]
			}
		}
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
		if (f.Name == "port" && c.Port == 0) || (f.Name == "proxy" && c.ProxyPort == 0) {
			invalidPort = true
		}
	})
	if invalidPort {
		return c, false, fmt.Errorf("SSH and proxy ports must be 1–65535")
	}
	if strings.HasPrefix(c.Key, "~/") {
		c.Key = filepath.Join(home, c.Key[2:])
	}
	return c, *yes, c.Validate()
}

// ValidateCommand runs before setup so malformed requests cannot mutate a workspace.
func ValidateCommand(workspace string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("connection command required")
	}
	switch args[0] {
	case "tunnel", "tunc", "tund":
		expanded, err := tunnelArgs(args)
		if err != nil || expanded[0] == "tunnels" {
			return err
		}
		return validateTunnelCommand(workspace, expanded)
	case "profile", "profiles", "history":
		return validateProfile(workspace, args)
	case "connect", "reconnect":
		_, _, e := Parse(workspace, args[1:])
		return e
	case "connections", "shells":
		if len(args) != 1 {
			return fmt.Errorf("%s takes no arguments", args[0])
		}
	case "resume", "shell-close":
		if args[0] == "shell-close" && len(args) == 1 {
			return nil
		}
		if len(args) != 2 {
			return fmt.Errorf("expected %s ID", args[0])
		}
		id, err := strconv.ParseUint(args[1], 10, 64)
		if err != nil || id == 0 || strconv.FormatUint(id, 10) != args[1] {
			return fmt.Errorf("shell ID must be a positive decimal integer; use shells")
		}
	case "inspect", "close", "shell":
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
func profileCommand(ctx context.Context, w string, s State) (hovel.PayloadCommandResult, error) {
	if s.Generation != "" {
		return ownerCommand(ctx, w, s.Session, "profile", []string{s.Generation, s.Creation})
	}
	return ownerCommand(ctx, w, s.Session, "connection-profile", nil)
}
func List(ctx context.Context, workspace string) ([]State, error) {
	var refs struct{ Sessions []hovel.SessionRef }
	if e := launch.Call(ctx, workspace, "ListSessions", map[string]any{}, &refs); e != nil {
		return nil, e
	}
	states := []State{}
	names := map[string]bool{}
	id, e := findManager(ctx, workspace)
	if e != nil {
		return nil, e
	}
	if id.Session != "" {
		if e = managerControl(ctx, workspace, id, "list", nil, &states); e != nil {
			return nil, fmt.Errorf("manager inventory unverified; resources preserved: %w", e)
		}
		for _, s := range states {
			if s.Session != id.Session || s.Generation != id.Generation || s.Creation == "" {
				return nil, fmt.Errorf("invalid manager inventory identity")
			}
			names[s.Name] = true
		}
	}
	// Compatibility is limited to existing sessions, never a legacy registration.
	for _, ref := range refs.Sessions {
		if (ref.ModuleID != "burrow@0.1.0" && ref.ModuleID != "burrow-connection@0.1.0") || ref.Kind != "connection" || ref.State == "closed" {
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
	if id.Session == "" && len(states) == 0 {
		if _, e := os.Lstat(filepath.Join(workspace, "burrow", ".manager-v1")); !os.IsNotExist(e) {
			return nil, fmt.Errorf("manager unavailable with an unidentified reservation; inventory unverified; investigate burrow/.manager-v1 manually")
		}
	}
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
	if s.Generation != "" {
		var result any
		if e := managerControl(ctx, w, managerIdentity{Session: s.Session, Generation: s.Generation}, "close", []string{s.Creation}, &result); e != nil {
			return fmt.Errorf("selected close unverified; inspect creation %s; no rollback promised: %w", s.Creation, e)
		}
		return nil
	}
	if _, e := ownerCommand(ctx, w, s.Session, "connection-close", []string{"confirm"}); e != nil {
		return fmt.Errorf("connection close failed or cleanup is uncertain; inspect %q and Hovel session %s; resources were not force-removed", s.Socket, s.Session)
	}
	var result map[string]any
	return launch.Call(ctx, w, "CloseSession", map[string]string{"SessionID": s.Session}, &result)
}

func closeReview(s State) string {
	return fmt.Sprintf("Close %s (%s@%s:%d), state %s, master PID %d, socket %s. Ends all owned connection access, including shells, transfers and tunnels; saved settings and artifacts remain. Repeat close %s --yes to confirm.", s.Name, s.User, s.Host, s.Port, s.State, s.MasterPID, s.Socket, s.Name)
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
	if current.Session != expected.Session || current.Generation != expected.Generation || current.Creation != expected.Creation || current.MasterPID != expected.MasterPID || current.Socket != expected.Socket || current.SocketInode != expected.SocketInode || current.State != expected.State || current.TunnelRevision != expected.TunnelRevision {
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
	if args[0] == "profile" && args[1] == "connect" {
		expanded, e := ProfileConnect(ctx, w, args)
		if e != nil {
			return nil, e
		}
		return execute(ctx, w, expanded, promptSocket)
	}
	switch args[0] {
	case "profile", "profiles", "history":
		return executeProfile(ctx, w, args)
	case "tunnel", "tunc", "tund":
		expanded, _ := tunnelArgs(args) // validated before dispatch
		return executeForward(ctx, w, expanded)
	case "connections":
		return List(ctx, w)
	case "shell-close", "shells", "resume":
		return nil, fmt.Errorf("%s is frontend-local; use management in the frontend that opened the shell", args[0])
	case "inspect":
		return selected(ctx, w, args[1])
	case "shell":
		s, e := selected(ctx, w, args[1])
		if e != nil {
			return nil, e
		}
		if s.State != "connected" || s.Generation == "" || s.Creation == "" {
			return nil, fmt.Errorf("shell requires a verified live manager connection; reconnect explicitly")
		}
		var live State
		e = managerControl(ctx, w, managerIdentity{Session: s.Session, Generation: s.Generation}, "shell", []string{s.Creation}, &live)
		return live, e
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
	review, preview, e := c.review(ctx, args[0])
	if e != nil {
		return nil, e
	}
	if !yes {
		return map[string]string{"review": review, "digest": c.reviewDigest(args[0], preview)}, nil
	}
	if c.Review != "" && c.Review != c.reviewDigest(args[0], preview) {
		return nil, fmt.Errorf("SSH settings changed after review; review again")
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
	state, e := connectManaged(ctx, c, preview)
	if e == nil && ctx.Err() != nil {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if e = closeOwned(cleanup, w, state); e != nil {
			return nil, fmt.Errorf("cancelled; cleanup uncertain: %w", e)
		}
		return nil, fmt.Errorf("cancelled; exact attempt closed")
	}
	return state, e
}

func displaySetting(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func Suggestions(states []State) []string {
	values := []string{"status", "connect", "connections", "tunnel create", "tunnel list", "tunnel check", "tunnel remove", "tunc", "tund", "shells", "shell-close", "help", "quit"}
	for _, s := range states {
		if s.State == "connected" && s.Generation != "" {
			values = append(values, "shell "+s.Name)
			values = append(values, "tunnel create "+s.Name+" forward ", "tunc "+s.Name+" l ")
			values = append(values, "tunnel create "+s.Name+" reverse ", "tunc "+s.Name+" r ")
		}
		for _, verb := range []string{"inspect", "close", "reconnect"} {
			values = append(values, verb+" "+s.Name)
		}
	}
	return values
}

func CommandSuggestions(line string, states []State) []string {
	args, e := Split(line)
	if len(args) == 1 && !strings.ContainsAny(line, " \t") {
		return Suggestions(states)
	}
	if e != nil || len(args) < 1 || (args[0] != "connect" && args[0] != "reconnect") {
		return Suggestions(states)
	}
	start := strings.LastIndexByte(line, ' ') + 1
	if start < 1 || (start < len(line) && !strings.HasPrefix(line[start:], "-")) {
		return nil
	}
	if start == len(line) && len(args) > 1 && strings.HasPrefix(args[len(args)-1], "-") && !strings.Contains(args[len(args)-1], "=") {
		switch optionName(args[len(args)-1]) {
		case "key", "agent", "port", "ssh-config", "jump", "host", "name", "user":
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
			if name == "proxy" {
				if !strings.Contains(args[i], "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					i++
				}
				continue
			}
			if !strings.Contains(args[i], "=") && name != "yes" && name != "prompt" {
				i++
			}
		} else {
			positionals++
		}
	}
	options := []string{"-proxy ", "-ssh-key ", "--key ", "--agent ", "--port ", "--ssh-config ", "--jump ", "--prompt", "--yes"}
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
