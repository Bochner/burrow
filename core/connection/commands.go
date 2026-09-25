package connection

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/Bochner/burrow/core/reports"
	"github.com/vibepwners/hovel/sdk/go/hovel"
)

const Help = `chain select CONNECTION             Query live forwards and SOCKS identities from their owner
chain http CONNECTION TUNNEL_ID URL [--yes] Review HTTP through exactly that existing tunnel
chain export CONNECTION TUNNEL_ID URL Export a saved Hovel consumer chain as JSON; no execution
chain connect NAME HOST --user USER [options] Export a saved Hovel connection chain; no authentication yet
Connection options: --user USER, --key PATH, --password [PASSWORD], --prompt, --agent PATH, --port NUMBER, --ssh-config PATH, --jump HOST.
A confirmed connection chain waits for authentication and registers a live named Burrow connection.
Requires an already-running OpenSSH/Dropbear server; no deployment. Bare --password opens a hidden field.
TUI connect/export stages a private chain file and opens Hovel with throw ready; Enter reviews.
CLI --password/--prompt exports JSON and stays alive as a private broker for a confirmed Hovel throw.
Keep that frontend alive; its one-use broker expires after 10 minutes. --password PASSWORD needs no TTY.
Existing names are refused; no adoption or automatic reconnect. Close via close NAME.
Exported chains bind this workspace/build and exact settings; regenerate after changes/upgrades.
Save CLI JSON to a file, then use hovel throw FILE --workspace PATH --allow-dangerous --json.
TUI examples (enter in Burrow management, Alt+B):
  chain connect target 192.168.10.50 --user alice --password
  chain connect target 192.168.10.50 --user alice --password PASSWORD
  chain connect target 192.168.10.50 --user alice --key ~/.ssh/id_ed25519
  chain connect target 192.168.10.50 --user alice --key ~/.ssh/id_ed25519 --prompt
Enter in Hovel reviews the staged throw; confirm yes, authenticate, then Alt+B after success.
F1 while typing chain shows examples and every connection option in the TUI.
CLI example (local shell):
  burrow --workspace /absolute/workspace chain connect target 192.168.10.50 --user alice --password > /absolute/workspace/connect.chain.json
Keep that terminal open. In another terminal, review and confirm with the installed Hovel CLI:
  hovel throw /absolute/workspace/connect.chain.json --workspace /absolute/workspace --daemon-endpoint /absolute/workspace/hoveld.sock --allow-dangerous --json
For unattended password export, supply --password PASSWORD and keep the exporter alive; confirm separately.
For unattended key/agent exports, omit --password/--prompt; JSON prints and the CLI exits.
--password [PASSWORD] cannot be combined with --key or a nonempty explicit --agent.
Hovel prompts normally; headless callers need matching confirmation or explicitly choose --now.
Consumers never create resources. Chain connect is a separate explicit creation operation.
HTTP is GET only, at most 8 seconds and 1 MiB; no TLS, redirects, credentials or query strings.
For L/R forwards, URL host/port must match the fixed destination. SOCKS accepts a hostname URL.
Reverse traffic originates at the SSH server through -W; no Python or remote helper is needed.
URL paths and ordinary request fields are public evidence; only --password PASSWORD is private input.
Hovel artifacts retain owner/tunnel identity, HTTP status, byte count and response SHA256,
without response bodies/headers. HTTP errors retain their status; routing/capture failures fail.
--review HASH binds CLI --yes to the recap. TUI uses the normal review dialog.
Concurrent consumers share forwarding; removal/close waits for bounded requests to finish.
Client disconnect stops waiting, not shared forwarding. Already-collected evidence survives close.

run survey CONNECTION --os ubuntu [--yes] Review, run and save an Ubuntu Markdown report
Survey uses a fixed read-only preset through /bin/sh, without sudo or installed helpers.
Checks: OS/kernel, identity, uptime/load/memory, disks, addresses/routes, listening ports,
and failed services. Each probe needs timeout (5 seconds); missing/failed checks are labelled.
Other Linux systems are unverified; this is inventory, not a vulnerability assessment.
Default survey bounds: 90 seconds and 1 MiB/stream; --timeout and --budget override them.
reports                             Open saved Reports (CLI: JSON inventory)
report ID                           Read a saved report (CLI: original Markdown plus metadata)
Reports are separate from Ctrl+L output/activity; viewing never runs or collects anything.
Markdown and versioned metadata use Hovel artifacts; they survive run/connection close.
Readers verify private files and SHA256; Markdown display is limited to 1 MiB.

run prepare CONNECTION [--budget BYTES] -- COMMAND [ARG...] Prepare without execution
run now CONNECTION [--budget BYTES] [--yes] -- COMMAND [ARG...] Review, launch, wait and collect a fresh run
run now CONNECTION --local [--yes] -- /absolute/TOOL [ARG...] Run locally with selected connection context
run prepare CONNECTION --script PATH --mode stream|inline|stage --interpreter PATH -- [ARG...]
run now CONNECTION --script PATH --mode stream|inline|stage --interpreter PATH [--yes] -- [ARG...]
Script/command options before --: --local, --stdin PATH, --timeout DURATION, --budget BYTES.
--local executes on the daemon host in the workspace directory; command paths must be absolute.
Local tools receive PATH=/usr/bin:/bin, LANG=C.UTF-8, BURROW_WORKSPACE, BURROW_CONNECTION,
BURROW_SOCKET, BURROW_SSH_CONFIG and BURROW_SSH_HOST; no inherited credentials or launch keys.
Local scripts support stream/inline snapshots; stage/keep is remote-only.
Local results use localExit/localSignal; remoteExit stays null, including local SSH exit 255.
Local cancel sends TERM then KILL to the ordinary local group; remote termination stays unconfirmed.
Raw same-user socket commands have no per-command Hovel approval/audit or remote cleanup guarantee.
--keep is only for explicit stage mode and retains uploaded files after the run.
Local script/stdin paths are confined to the workspace upload root; relative paths start there.
Preparation snapshots up to 256 MiB per input in private files, binding hashes to review.
Stream mode feeds shell source through stdin; it cannot also take --stdin, even an empty file.
Inline mode uses shell -c, exposes source in SSH argv, and MUST NOT contain secrets.
Inline source and the quoted invocation must fit 64 KiB; larger source needs stream or stage.
Stage mode creates an owned 0700 directory and 0600 script in /tmp after confirmation.
Staging needs /bin/sh, mkdir, stat, cat, rm and rmdir (Linux GNU/BusyBox); no Python/helper install.
Staging has an 8-second attempt bound; cleanup has a 4-second bound. Uncertainty stays visible.
Cleanup removes only owned staged files and their empty directory; existing/replaced files survive.
Existing remote script example: run now CONNECTION --stdin data.bin -- /bin/sh /opt/check.sh
Timeout example: --timeout 30s requests ordinary-group cancellation; no implicit execution timeout.
Input bytes stay out of recorded requests; scripts can themselves print sensitive data into output.
run launch ID [--collect] [--review HASH] [--yes] Launch once; --collect waits and saves output
run list                            Discover retained runs in this workspace
run inspect ID                      Execution, output completeness, budget and cleanup
run follow ID [stdout|stderr] [OFFSET] Open a live terminal viewer; Esc closes only the viewer
run output ID stdout|stderr OFFSET   Read up to 32 KiB at a byte offset (CLI: base64)
run cancel ID [--yes]                Review/request ordinary process-group termination
run collect ID [--yes]               Register completed/partial output as Hovel artifacts
run close ID [--yes]                 Drop working output; registered artifacts remain
TUI mutations open the shared review dialog; CLI repeats with --yes to confirm.
Repeated launch never repeats the action; viewing/quit does not cancel a run.
Remote commands reuse the selected master with no fresh login, implicit staging or remote Python.
Arguments are quoted individually; use /bin/sh -c explicitly for shell syntax.
Commands/arguments cannot carry secrets: Hovel records the request, and SSH argv is visible.
Default storage: 268435456 bytes per stream; --budget sets a positive byte count.
Separate private local stdout/stderr files retain all bytes within that budget.
Budget/write failures mark incomplete capture and retain collectible partial output.
There is no implicit execution timeout. Output reads do not collect evidence.
Run success, transport/completion uncertainty and collection success are separate.
Remote cancellation needs Linux /proc identity and an ordinary OpenSSH process group.
PID checking and signalling are not atomic; escaped descendants and cleanup after
connection loss remain unconfirmed. Stopping local SSH proves no remote cleanup.
Active runs must be cancelled before close. Closing a connection loses its run access.
Uncollected output can be lost on module/daemon failure; unknown leftovers are not adopted.
Collected evidence is inspectable with Hovel artifact list/inspect after run close.
Inspect also reports input snapshots, staging, stageCleanup and timedOut separately from remoteExit.
Live viewers keep independent positions and a 32 KiB preview per stream in this frontend.
Tab switches streams; scrolling pauses follow; End resumes; Alt+B returns to management.
Reopen with run follow ID; explicit OFFSET resets that stream. Positions end with the frontend.
Output JSON includes the captured snapshot's status, budget, stored/received bytes and errors.
Original bytes stay in capture; terminal controls and binary data are escaped only for display.

logs / Ctrl+N                      Open workspace log in embedded read-only Vim
logs --json                        Read the same historical notes as JSON {text}
Ctrl+N or :q returns to the previous context. Reopening refreshes the snapshot.
Ctrl+N is reserved in management, file mode and embedded SSH/Hovel/editor tabs.
Logs persist Burrow operations and shell lifecycle, not interactive shell I/O.
Attempts without terminal results mean unknown outcomes; review is not execution.
New target work requires logging; cleanup still runs if logging fails and reports it.
Logs: WORKSPACE/burrow-logs/operations.log (private). No automatic retention/rotation.
Authentication secrets are excluded; remote output may contain customer-sensitive data.
scp NAME [ls|tree|cd|pwd|complete] [PATH] Browse an existing live master (JSON)
Append --request ID for cancellable discovery; use a fresh ID per request.
scp NAME cancel ID                 Request cancellation of that exact discovery
Cancellation affects no transfers; inspect the original discovery outcome.
In the TUI, scp [NAME] enters file mode; back restores management.
local [download|upload] PATH         Persist an absolute workspace root (local PATH: download)
local                               Show effective upload and download roots
lcd [download|upload] PATH           Validate navigation inside a root
lls [download|upload] [PATH]         List inside a root (default: download)
files-history                       Retained file commands, separate from profile history
CLI navigation is stateless: pass PATH on each call; TUI keeps per-mode directories.
Defaults: WORKSPACE/burrow-files/{uploads,downloads}; config: burrow-files/config.json.
Root changes never move/delete files. Invalid roots fail without fallback.
Local links must resolve inside their root. Remote cd follows directory links;
tree never traverses directory links. Hidden files are included except . and ...
Listings are oldest-first; numeric owner/group fallback is clearly labelled.
Tree scans stop at 3000 entries or 30 seconds and label partial results.
Completion is cached and throttled; no idle scans or recursive prefetch.
scp NAME get REMOTE [LOCAL]          Review one download and actual destination
scp NAME mget PATTERN [LOCAL_DIR]     Review nonrecursive regular-file matches
scp NAME put LOCAL [REMOTE]           Review one contained upload and actual destination
Repeat with --review DIGEST --yes to approve the unchanged recap, including overwrite.
downloads [ID]                      Retained outcomes and workspace download totals
download-cancel ID                  Cancel and wait for transfer cleanup acknowledgement
transfers [ID]                      Retained download and upload outcomes
transfer-cancel ID                  Cancel either transfer direction and wait for cleanup
Transfers continue during shell use, browsing, help and frontend detach.
Existing destinations survive failed replacement; labelled partials are retained.
Rate is measured bytes/sec (interval average); ETA uses overall average or is unknown.
Retry selected failures with get or put using their recorded source/destination; restart, no resume.
Working transfers are not automatically registered Hovel evidence.

profiles                            List saved entries and selected collection
profile create NAME HOST --user USER [options] Save settings without connecting
profile select NAME                 Inspect saved settings only
profile connect NAME [--as LIVE] [--yes] [--prompt] [--review HASH] Connect reviewed saved settings
profile save CONNECTION [--as PROFILE] [--yes] Save authenticated settings
profile edit NAME HOST --user USER [options] Replace saved settings (--yes confirms)
profile delete NAME [--yes]          Delete settings; retain live resources
profile collection PATH             Create/open a collection
profile load PATH                   Open an existing collection; never connect
profile backup PATH                 Copy collection to a new backup file
history                             Retained profile management commands
Profiles keep aliases/overrides and key references, never authentication secrets.
Create/edit accepts key/agent settings, not --password/--prompt; profile save after a password connection keeps its authentication preference.
Default template: WORKSPACE/burrow-profiles.json; _example is documentation only.
All profile writes use the selected file, retained by Hovel across invocations.
Edit replaces all settings; include options to retain them. Review replies include
revision/collection; repeat --revision HASH --collection PATH --yes to pin review.

connections                         Inspect retained owners
proxy create CONNECTION LISTEN [--yes] Create SOCKS on the existing master
proxy inspect CONNECTION            Verified endpoint and retained owner identity
proxy remove CONNECTION [--yes]     Remove SOCKS only; retain connection/L/R
Proxy LISTEN is PORT (127.0.0.1 default) or IP:PORT, including [IPv6]:PORT.
One SOCKS4/5 TCP proxy per connection; no L/R tunnel IDs or counts consumed.
Names resolve on the SSH server. Explicit broader binds expose an unauthenticated
proxy. Creation and removal recaps support --review HASH to bind approval.
Inspection verifies the master-owned Linux listener, not destination reachability.
Live proxy edits do not change saved reconnect settings. Keep-running quit retains
SOCKS; close/loss ends it. Unverified/unavailable endpoints must not be consumed.
Compatible consumers use proxy inspect's session/generation/connectionCreation
and proxy creation identity, recheck before use and refuse stale owners/endpoints.
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
connect NAME HOST --user USER [options] Create shell-free SSH master
reconnect NAME HOST --user USER [options] Explicitly replace a lost owned connection
inspect NAME                        State, endpoint and socket identity
session create CONNECTION [--columns N --rows N] [--yes] [--review HASH] Review/create a retained Hovel SSH shell
session list CONNECTION              Discover retained Hovel shells (headless CLI)
session inspect CONNECTION ID        Read shell identity, lifecycle and output counts
session close CONNECTION ID [--yes] [--review HASH] Review/close only this retained shell
session claim CONNECTION ID --request-stdin Claim an unclaimed shell using private JSON
session takeover CONNECTION ID --request-stdin Replace the observed control generation
session input CONNECTION ID --request-stdin Send 1..4096 base64 bytes with the current token
session resize CONNECTION ID --request-stdin Resize with the current token
session release CONNECTION ID --request-stdin Release control; retain shell and geometry
session observe CONNECTION ID [OFFSET] Read independent bytes (default position 0)
session snapshot CONNECTION ID [HISTORY] Recover display; optional 0..1000 history lines
Retained sessions survive CLI exit and Keep running; connection/manager close ends them.
Creation uses supported Hovel throws and the existing verified master; no login fallback.
Headless initial size defaults to 80x24; TUI creation uses the pane dimensions.
Private commands require piped/file stdin AND stdout. Claim JSON defaults to
{"label":"agent"}; optional columns/rows retain the current geometry when omitted.
Takeover also requires the observed generation; old input/resize/release is refused.
Input uses {"token":"...","data":"BASE64"}; resize uses token, columns, rows;
release uses token. Tokens never belong in argv, history, logs or evidence.
An accepted byte count is not a command result. A disappeared controller keeps its
claim until explicit takeover. Labels do not assert liveness; there is no timeout.
Observe never consumes another reader's bytes or changes geometry. Gaps remain
out-of-sync; snapshot returns the current display and a fresh byte position.
Byte reads are stream-only, never proof of current screen state. Use snapshots
for current display; raw bytes are not a serialized emulator state.
Raw Hovel send/read/attach cannot control or observe these shells. Output stays in a
64 KiB memory suffix; inspect reports received/buffered/dropped counts.
Loss reports lost/unavailable and uncertain command outcomes; relaunch never restores state.
Failed creation may leave a prepared session: inspect/list, close explicitly, then review anew.

shell NAME [SESSION_ID]             Create a retained shell, or observe an existing opaque ID
shells                              Discover retained shells in this workspace
resume ID                           Select an attached shell by frontend number
shell-control [ID]                   Explicitly take control and apply pane dimensions
shell-detach [ID]                    Release this frontend's control; retain the shell
shell-close [ID]                     Close ID, or the last selected retained shell
close NAME [--yes] [--review HASH]    Review/close the exact owned connection
Shells reuse a verified master; no fresh login or authentication fallback.
Ctrl+] or Alt+B detaches to management; Ctrl+C reaches SSH only in CONTROL mode.
Select a Shells entry or use resume ID to return. Alt+T explicitly takes control.
OBSERVE never sends keys or resizes; controller label, dimensions and sync stay visible.
Multiple shells share the connection's existing master socket and authentication.
Shell exit/close preserves the connection. Keep running releases owned claims and
retains shells; Cancel changes nothing. Close connections tears down reviewed resources.
Shell bytes stay in bounded owner memory; they are not logs or collected evidence.

Required: NAME HOST --user USER (- uses SSH config user). Positional USER remains accepted, as does:
connect -ip HOST -port NUMBER -user USER -socket NAME [-ssh-key PATH]
Named flags may appear in any order; duplicate fields/aliases are refused.
Legacy -shell and -no-term are unsupported, never silently accepted.
Options (required fields are shown before optional settings):
-proxy [PORT] (local SOCKS4/5 on 127.0.0.1, default 9050; omitted = off),
--key PATH, --agent PATH (SSH_AUTH_SOCK default), --port NUMBER (config/22),
--ssh-config PATH (~/.ssh/config), --jump [USER@]HOST[:PORT][,...],
--prompt (CLI hidden password/passphrase entry), --password [PASSWORD] (password only), --yes (confirm review),
--review HASH (bind --yes to the exact previously displayed recap).
Examples (add burrow --workspace PATH when running from a local shell):
  connect target 192.168.10.50 --user alice --password
  connect target 192.168.10.50 --user alice --key ~/.ssh/id_ed25519
  reconnect target 192.168.10.50 --user alice --password
Every hop uses UserKnownHostsFile=/dev/null and StrictHostKeyChecking=no.
There is no host-key approval.
TUI connects review and prompt interactively; Ctrl+C/Esc cancels the attempt.
CLI without --yes reviews; --password/--prompt waits for authentication and cleans failure.
Bare --password opens hidden entry; --password PASSWORD supplies the target password without a popup.
Quote spaces; use --password=VALUE for leading dashes or an empty password (maximum 4096 bytes; no NUL/CR/LF).
Supplied values stay in frontend memory, out of Burrow history, logs, JSON, previews and child argv/environment.
TUI Up recalls the connection command with bare --password for fresh hidden entry.
Literal CLI values remain visible in the original process argv and may enter shell history.
Inline TUI text is visible while typed. Use the bare flag for hidden entry.
Only the target receives an automatic value, once; jump hosts need keys/agents or interactive entry.
Wrong values fail and clean up the attempt; retry explicitly. Headless direct connect still needs --yes.
An incompatible retained manager refuses before password staging/dispatch: inspect, then review burrow restart.
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
	fs.BoolVar(&c.PasswordAuth, "password", false, "prompt privately for password authentication")
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
			return c, false, fmt.Errorf("invalid connection options; use help")
		}
		// Interactive review appends --yes after an explicit --yes=false.
		// Only this approval switch may repeat; connection fields stay unique.
		if seen[name] && name != "yes" {
			return c, false, fmt.Errorf("duplicate connection option %s", name)
		}
		seen[name] = true
		if name == "password" {
			if !assigned && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				value, assigned = args[i], true
			}
			if assigned {
				c.Password = &value
			}
			options = append(options, "--password=true")
			continue
		}
		if !assigned && name == "proxy" {
			value, assigned = "9050", true
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				value = args[i]
			}
		}
		if !assigned && name != "yes" && name != "prompt" && name != "password" {
			i++
			if i == len(args) {
				return c, false, fmt.Errorf("missing value for %s; required NAME HOST --user USER; use help", name)
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
			return c, false, fmt.Errorf("invalid connection options; expected NAME HOST --user USER")
		}
		name := []string{"name", "host", "user"}[i]
		if seen[name] {
			return c, false, fmt.Errorf("duplicate connection option %s", name)
		}
		seen[name] = true
		options = append(options, "--"+name+"="+value)
	}
	if !seen["name"] || !seen["host"] || !seen["user"] {
		return c, false, fmt.Errorf("required NAME HOST --user USER (or -socket NAME -ip HOST -user USER); bare connect opens the form")
	}
	if e = fs.Parse(options); e != nil {
		return c, false, fmt.Errorf("invalid connection options; use help")
	}
	c.Prompt = c.Prompt || c.PasswordAuth
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
	case "session":
		_, _, err := sessionArgs(workspace, args)
		return err
	case "chain":
		return validateChain(workspace, args)
	case "run":
		_, _, _, err := parseRun(args)
		if err == nil && len(args) > 2 && (args[1] == "prepare" || args[1] == "now" || args[1] == "survey") {
			_, err = launch.ConnectionPath(workspace, args[2])
		}
		return err
	case "reports":
		if len(args) != 1 {
			return fmt.Errorf("expected reports")
		}
		return nil
	case "report":
		if len(args) != 2 || args[1] == "" || len(args[1]) > 256 || strings.ContainsAny(args[1], "/\\\x00\r\n") {
			return fmt.Errorf("expected report ID; use reports")
		}
		return nil
	case "downloads", "download-cancel", "transfers", "transfer-cancel":
		if len(args) > 2 || ((args[0] == "download-cancel" || args[0] == "transfer-cancel") && len(args) != 2) {
			return fmt.Errorf("expected downloads/transfers [ID] or download-cancel/transfer-cancel ID")
		}
		return nil
	case "logs":
		if len(args) != 1 && (len(args) != 2 || args[1] != "--json") {
			return fmt.Errorf("expected logs [--json]")
		}
		return nil
	case "files-history":
		if len(args) != 1 {
			return fmt.Errorf("expected files-history")
		}
		return nil
	case "scp":
		if len(args) > 2 && (args[2] == "get" || args[2] == "mget" || args[2] == "put") {
			_, _, _, _, err := downloadArgs(args)
			if err != nil {
				return err
			}
			_, err = launch.ConnectionPath(workspace, args[1])
			return err
		}
		_, err := fileArgs(args)
		if err != nil {
			return err
		}
		_, err = launch.ConnectionPath(workspace, args[1])
		return err
	case "local", "lcd", "lls":
		_, _, err := localArgs(args)
		return err
	case "proxy":
		_, err := proxyArgs(workspace, args)
		return err
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
	case "resume", "shell-close", "shell-control", "shell-detach":
		if args[0] != "resume" && len(args) == 1 {
			return nil
		}
		if len(args) != 2 {
			return fmt.Errorf("expected %s ID", args[0])
		}
		id, err := strconv.ParseUint(args[1], 10, 64)
		if err != nil || id == 0 || strconv.FormatUint(id, 10) != args[1] {
			return fmt.Errorf("shell ID must be a positive decimal integer; use shells")
		}
	case "close":
		if len(args) < 2 {
			return fmt.Errorf("expected close NAME [--yes] [--review HASH]")
		}
		if _, _, err := closeOptions(args[2:]); err != nil {
			return err
		}
		_, e := launch.ConnectionPath(workspace, args[1])
		return e
	case "inspect", "shell":
		if args[0] == "shell" && len(args) == 3 {
			_, _, err := sessionArgs(workspace, []string{"session", "inspect", args[1], args[2]})
			return err
		}
		if len(args) != 2 {
			return fmt.Errorf("expected %s NAME", args[0])
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
	return fmt.Sprintf("Close %s (%s@%s:%d), state %s, master PID %d, socket %s. Ends all owned connection access, including %d retained shells, transfers and tunnels; saved settings and artifacts remain. Repeat close %s --yes to confirm.", s.Name, s.User, s.Host, s.Port, s.State, s.MasterPID, s.Socket, s.ShellCount, s.Name)
}

func closeOptions(args []string) (bool, string, error) {
	fs := flag.NewFlagSet("close", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	yes := fs.Bool("yes", false, "confirm selected close")
	review := fs.String("review", "", "exact connection review digest")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || (*review != "" && (len(*review) != 64 || strings.Trim(*review, "0123456789abcdef") != "")) {
		return false, "", fmt.Errorf("expected close NAME [--yes] [--review HASH]")
	}
	return *yes, *review, nil
}

func closeDigest(w string, s State) string {
	b, _ := json.Marshal([]any{w, s.Name, s.Session, s.Generation, s.Creation, s.MasterPID, s.Socket, s.SocketInode, s.State, s.TunnelRevision, s.ShellRevision})
	return digest(string(b))
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
	if closeDigest(w, current) != closeDigest(w, expected) {
		return nil, fmt.Errorf("connection changed after review; inspect and review close again")
	}
	if expected.Generation != "" {
		id, e := findManager(ctx, w)
		if e != nil {
			return nil, e
		}
		// Older managers cannot own this slice's shells. Preserve their existing
		// explicit close route so an upgrade never requires forceful cleanup.
		if !id.RetainedShells {
			if e := closeOwned(ctx, w, expected); e != nil {
				return nil, e
			}
			return map[string]string{"state": "closed", "name": expected.Name}, nil
		}
		var result any
		if e := managerControl(ctx, w, managerIdentity{Session: expected.Session, Generation: expected.Generation}, "close-selected", []string{expected.Creation, closeDigest(w, expected)}, &result); e != nil {
			return nil, fmt.Errorf("selected close unconfirmed; inspect before retrying: %w", e)
		}
	} else if e := closeOwned(ctx, w, expected); e != nil {
		return nil, e
	}
	return map[string]string{"state": "closed", "name": expected.Name}, nil
}

// Execute is the shared CLI/TUI command boundary. Hovel remains the state owner.
func Execute(ctx context.Context, w string, args []string) (any, error) {
	return execute(ctx, w, args, "")
}

func execute(ctx context.Context, w string, args []string, promptSocket string) (value any, failure error) {
	if err := ValidateCommand(w, args); err != nil {
		return nil, err
	}
	// Capture submitted identity and full safe frontend result, including reviews
	// and pre-dispatch refusals. Owner records carry asynchronous actual outcomes.
	if (args[0] == "run" || args[0] == "session") && (args[1] == "list" || args[1] == "inspect" || args[1] == "output" || args[1] == "observe" || args[1] == "snapshot") {
		return executeOperation(ctx, w, args, promptSocket)
	}
	switch args[0] {
	case "connect", "reconnect", "profile", "chain", "run", "session", "close", "scp", "tunnel", "tunc", "tund", "proxy", "shell":
		a, err := launch.BeginAudit(w, commandIdentity(args), "submitted request; see owner result", nil)
		cleanup := args[0] == "close" || args[0] == "tund" || (len(args) > 1 && args[1] == "remove") || ((args[0] == "run" || args[0] == "session") && (args[1] == "cancel" || args[1] == "close")) || (args[0] == "scp" && len(args) > 2 && args[2] == "cancel")
		if err != nil && !cleanup {
			return nil, err
		}
		defer func() {
			status := "returned; see result state (review is not execution)"
			if failure != nil {
				status = "failed or refused; see owner records for execution outcome"
			}
			logErr := a.Record(status, map[string]any{"result": value, "error": fmt.Sprint(failure)})
			failure = errors.Join(failure, err, logErr)
			if failure != nil && (args[0] == "run" || args[0] == "session") {
				id := ""
				switch result := value.(type) {
				case Run:
					id = result.ID
				case Shell:
					id = result.ID
				case map[string]string:
					id = result["id"]
				}
				if id != "" {
					failure = fmt.Errorf("%s %s: %w; inspect that ID before retrying", args[0], id, failure)
				}
			}
		}()
	}
	return executeOperation(ctx, w, args, promptSocket)
}

func executeOperation(ctx context.Context, w string, args []string, promptSocket string) (value any, failure error) {
	if e := ValidateCommand(w, args); e != nil {
		return nil, e
	}
	operation, ok := CommandOperation(args)
	if !ok {
		return nil, fmt.Errorf("command has no registered capability")
	}
	if args[0] == "profile" && args[1] == "connect" {
		expanded, e := ProfileConnect(ctx, w, args)
		if e != nil {
			return nil, e
		}
		return execute(ctx, w, expanded, promptSocket)
	}
	switch operation.Dispatch {
	case "session":
		return executeSession(ctx, w, args)
	case "chain":
		return executeChain(ctx, w, args)
	case "run":
		return executeRun(ctx, w, args)
	case "reports":
		return reports.List(ctx, w)
	case "report":
		return reports.Read(ctx, w, args[1])
	case "downloads", "download-cancel", "transfers", "transfer-cancel":
		return executeDownloads(ctx, w, args)
	case "files-history":
		return FileHistory(ctx, w)
	case "scp":
		if len(args) > 2 && (args[2] == "get" || args[2] == "mget" || args[2] == "put") {
			return executeDownload(ctx, w, args)
		}
		query, _ := fileArgs(args)
		state, err := selected(ctx, w, args[1])
		if err != nil {
			return nil, err
		}
		value, err := Browse(ctx, w, state, query)
		return fileCommandResult(ctx, w, args, value, err)
	case "local", "lcd", "lls":
		value, err := executeLocal(ctx, w, args)
		return fileCommandResult(ctx, w, args, value, err)
	case "proxy":
		return executeProxy(ctx, w, args)
	case "profile", "profiles", "history":
		return executeProfile(ctx, w, args)
	case "tunnel", "tunc", "tund":
		expanded, _ := tunnelArgs(args) // validated before dispatch
		return executeForward(ctx, w, expanded)
	case "connections":
		return List(ctx, w)
	case "shell-close", "shells", "resume", "shell-control", "shell-detach":
		return nil, fmt.Errorf("%s selects frontend tabs; use session commands with opaque IDs headlessly", args[0])
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
		yes, review, _ := closeOptions(args[2:])
		if !yes {
			return map[string]string{"review": closeReview(s), "digest": closeDigest(w, s)}, nil
		}
		if review != "" && review != closeDigest(w, s) {
			return nil, fmt.Errorf("connection changed after review; inspect and review close again")
		}
		return CloseReviewed(ctx, w, s)
	}
	c, yes, e := Parse(w, args[1:])
	if e != nil {
		return nil, e
	}
	if e := checkPasswordManager(ctx, c); e != nil {
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
		return nil, fmt.Errorf("authentication requires a private frontend; use the CLI or TUI password flow")
	}
	a, auditErr := launch.BeginAudit(w, commandIdentity(args), c.User+"@"+c.Host, nil)
	if auditErr != nil {
		return nil, auditErr
	}
	defer func() { failure = a.Finish(value, failure) }()
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
	values := []string{"status", "connect", "connections", "proxy create", "proxy inspect", "proxy remove", "tunnel create", "tunnel list", "tunnel check", "tunnel remove", "tunc", "tund", "shells", "shell-close", "shell-control", "shell-detach", "help", "quit"}
	for _, s := range states {
		values = append(values, "proxy inspect "+s.Name)
		if s.State == "connected" && s.Generation != "" {
			if s.Proxy.ID != "" {
				values = append(values, "proxy remove "+s.Name)
			} else if s.ProxyPort == 0 {
				values = append(values, "proxy create "+s.Name+" ")
			}
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
	chain := e == nil && len(args) >= 2 && args[0] == "chain" && args[1] == "connect"
	if chain {
		args = args[1:]
	}
	if e == nil && len(args) >= 3 && args[0] == "run" {
		return runSuggestions(line, args)
	}
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
			if name == "proxy" || name == "password" {
				if !strings.Contains(args[i], "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					i++
				}
				continue
			}
			if !strings.Contains(args[i], "=") && name != "yes" && name != "prompt" && name != "password" {
				i++
			}
		} else {
			positionals++
		}
	}
	options := []string{"-proxy ", "-ssh-key ", "--key ", "--password", "--agent ", "--port ", "--ssh-config ", "--jump ", "--prompt", "--yes"}
	if positionals < 3 && !seen["user"] {
		options = append([]string{"--user "}, options...)
	}
	if chain {
		// The chain parser requires a positional name before the host/options.
		if positionals == 0 || (positionals < 2 && !seen["host"]) {
			return nil
		}
		if positionals < 3 && !seen["user"] {
			return []string{line[:start] + "--user "}
		}
	} else if positionals == 0 {
		for _, required := range [][2]string{{"host", "-ip "}, {"port", "-port "}, {"user", "-user "}, {"name", "-socket "}} {
			if !seen[required[0]] {
				return []string{line[:start] + required[1]}
			}
		}
	}
	values := []string{}
	for _, option := range options {
		if chain && (option == "--yes" || option == "-proxy ") {
			continue
		}
		if (seen["password"] && (option == "--key " || option == "-ssh-key " || option == "--agent ")) || (option == "--password" && (seen["key"] || seen["agent"])) {
			continue
		}
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
