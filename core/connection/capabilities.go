package connection

import (
	"reflect"
	"strings"
	"time"

	"github.com/Bochner/burrow/core/launch"
	"github.com/Bochner/burrow/core/reports"
)

// Operation describes a public route. Dispatch is also used by Execute, so a
// documented route cannot silently fall through to a different command family.
// Patterns select commands, not validate inputs; the existing parsers do that.
type Operation struct {
	ID            string     `json:"id"`
	Category      string     `json:"category"`
	Summary       string     `json:"summary"`
	Patterns      [][]string `json:"patterns"`
	Dispatch      string     `json:"dispatch"`
	Human         string     `json:"human"`
	Agent         Route      `json:"agent"`
	Inputs        []string   `json:"inputs"`
	Result        string     `json:"result"`
	Scope         string     `json:"scope"`
	Effects       string     `json:"effects"`
	Review        string     `json:"review"`
	ModuleCommand bool       `json:"moduleCommand"`
	Evidence      []Evidence `json:"evidence"`
}

type Route struct {
	Status     string   `json:"status"`
	Syntax     string   `json:"syntax"`
	Example    []string `json:"example"`
	Variants   []string `json:"variants,omitempty"`
	Limitation string   `json:"limitation,omitempty"`
}

type Evidence struct {
	Kind   string `json:"kind"`
	Source string `json:"source"`
}

// CommandOperation is shared by execution and the base module's command gate.
// Longest literal-prefix pattern wins; * is one resource/argument token.
func CommandOperation(args []string) (Operation, bool) {
	var found Operation
	length := 0
	for _, op := range commandOperations {
		for _, pattern := range op.Patterns {
			if len(pattern) <= length || len(args) < len(pattern) {
				continue
			}
			// The bare scp spelling means pwd only when no subcommand is given.
			// A future subcommand must register its own capability.
			if len(pattern) == 1 && pattern[0] == "scp" && len(args) != 2 {
				continue
			}
			matches := true
			for i, token := range pattern {
				if token != "*" && token != args[i] {
					matches = false
					break
				}
			}
			if matches {
				found, length = op, len(pattern)
			}
		}
	}
	return found, length != 0
}

func Operations() []Operation { return append([]Operation(nil), commandOperations...) }

// ShellRequestSchemas describes the same private objects decoded by the owner.
func ShellRequestSchemas() map[string]any {
	out := map[string]any{}
	for name, value := range map[string]any{"shell-claim": ShellClaim{}, "shell-takeover": ShellClaim{}, "shell-input": ShellInput{}, "shell-resize": ShellResize{}, "shell-release": ShellRelease{}} {
		shape := jsonShape(reflect.TypeOf(value))
		shape["additionalProperties"] = false
		shape["description"] = "One JSON object on private stdin with --request-stdin; at most 8192 bytes. Piped/file stdout required. Never put tokens in argv, history, logs or evidence."
		fields := shape["properties"].(map[string]any)
		for _, field := range []string{"columns", "rows"} {
			if _, ok := fields[field]; ok {
				fields[field] = map[string]any{"type": "integer", "minimum": 1, "maximum": 1000, "description": "At most 20000 cells in columns × rows. Omitted claim/takeover dimensions retain current geometry; explicit null is refused."}
			}
		}
		if _, ok := fields["label"]; ok {
			fields["label"] = map[string]any{"type": "string", "default": "agent", "maxLength": 64, "description": "At most 64 UTF-8 bytes; no control/format characters; not a liveness assertion."}
		}
		if name == "shell-claim" {
			delete(fields, "generation")
		}
		if name == "shell-takeover" {
			shape["required"] = []string{"generation"}
			fields["generation"] = map[string]any{"type": "integer", "minimum": 0, "description": "Observed controlGeneration from inspect; never guessed or retried automatically."}
		}
		if name == "shell-input" {
			fields["data"] = map[string]any{"type": "string", "contentEncoding": "base64", "description": "1..4096 decoded bytes. acceptedBytes counts only the accepted prefix; no execution result or automatic retry."}
		}
		out[name] = shape
	}
	return out
}

// op keeps common CLI/TUI/daemon semantics in one place. Variants and explicit
// gaps below override them; examples never imply approval.
func op(id, pattern, syntax, example, summary, inputs, result, effects, review, source, check string) Operation {
	category, _, _ := strings.Cut(id, ".")
	patterns := [][]string{}
	for _, p := range strings.Split(pattern, "|") {
		patterns = append(patterns, strings.Fields(p))
	}
	args, err := Split(example)
	if err != nil {
		panic(err)
	} // Static developer-authored invocation.
	dispatch := patterns[0][0]
	module := dispatch == "profile" || dispatch == "profiles" || dispatch == "history" || dispatch == "scp" || dispatch == "local" || dispatch == "lcd" || dispatch == "lls" || dispatch == "files-history" || dispatch == "downloads" || dispatch == "download-cancel" || dispatch == "transfers" || dispatch == "transfer-cancel" || dispatch == "run" || dispatch == "reports" || dispatch == "report"
	return Operation{ID: id, Category: category, Summary: summary, Patterns: patterns, Dispatch: dispatch,
		Human:  "Burrow management: " + syntax,
		Agent:  Route{Status: "supported", Syntax: "burrow --workspace PATH " + syntax, Example: append([]string{"burrow", "--workspace", "/absolute/workspace"}, args...)},
		Inputs: strings.Fields(inputs), Result: result, Scope: "Explicit workspace; resource names and IDs are workspace-local.", Effects: effects, Review: review, ModuleCommand: module,
		Evidence: []Evidence{{Kind: "dispatch", Source: source}, {Kind: "semantic-check", Source: check}},
	}
}

const inspectEffect = "Inspects existing state; requires a verified running workspace. Operational audit/history may be appended; does not initialize a workspace or connect."
const confirmReview = "CLI returns a review first; repeat with --yes after approval. --review HASH, where supported, binds the recap. TUI uses the shared review dialog."
const noReview = "No additional approval; the invocation itself authorizes the described effect. Hovel throws still require their normal confirmation and --allow-dangerous."
const connectionInputs = "name host user connection-options"
const runInputs = "connection run-options command"

var commandOperations = []Operation{
	op("session.snapshot", "session snapshot", "session snapshot CONNECTION ID [HISTORY]", "session snapshot target shell-id", "Recover a current screen after missed output", "connection session-id history-offset", "ShellOutput", "Returns a bounded complete display snapshot and byte position, or up to 1000 lines back with snapshot-history. Includes input modes for safe encoding. Never sends input or changes geometry.", noReview, "core/connection/session_control_linux.go", "core/cmd/burrow/sessions_lab.py"),
	op("session.takeover", "session takeover", "session takeover CONNECTION ID --request-stdin", "session takeover target shell-id --request-stdin", "Explicitly replace the observed controller generation", "connection session-id shell-takeover", "ShellControl", "Shared shell owner enforces control; no automatic reconnect.", noReview, "core/connection/session_control_linux.go", "core/cmd/burrow/sessions_lab.py"),
	op("session.resize", "session resize", "session resize CONNECTION ID --request-stdin", "session resize target shell-id --request-stdin", "Resize the controlled shell", "connection session-id shell-resize", "ShellControl", "Shared shell owner enforces control; no automatic reconnect.", noReview, "core/connection/session_control_linux.go", "core/cmd/burrow/sessions_lab.py"),
	op("session.release", "session release", "session release CONNECTION ID --request-stdin", "session release target shell-id --request-stdin", "Release control and retain the shell", "connection session-id shell-release", "ShellControl", "Shared shell owner enforces control; no automatic reconnect.", noReview, "core/connection/session_control_linux.go", "core/cmd/burrow/sessions_lab.py"),
	op("session.claim", "session claim", "session claim CONNECTION ID --request-stdin", "session claim target shell-id --request-stdin", "Claim an unclaimed shell", "connection session-id shell-claim", "ShellControl", "Shared shell owner enforces control; no automatic reconnect.", noReview, "core/connection/session_control_linux.go", "core/cmd/burrow/sessions_lab.py"),
	op("session.input", "session input", "session input CONNECTION ID --request-stdin", "session input target shell-id --request-stdin", "Submit controller input", "connection session-id shell-input", "ShellControl", "Shared shell owner enforces control; no automatic reconnect.", noReview, "core/connection/session_control_linux.go", "core/cmd/burrow/sessions_lab.py"),
	op("session.observe", "session observe", "session observe CONNECTION ID [OFFSET]", "session observe target shell-id", "Read independent bounded shell output", "connection session-id", "ShellOutput", "Shared shell owner enforces control; no automatic reconnect.", noReview, "core/connection/session_control_linux.go", "core/cmd/burrow/sessions_lab.py"),
	op("session.create", "session create", "session create CONNECTION [--columns N --rows N] [--yes] [--review HASH]", "session create target", "Create a Hovel-retained SSH shell", "connection shell-geometry yes review", "ShellReview", "Creates one shell through the exact existing manager and master; survives launcher exit. Supports owner-fenced control and independent observation through session commands.", confirmReview, "core/connection/sessions_linux.go", "core/cmd/burrow/sessions_lab.py"),
	op("session.list", "session list", "session list CONNECTION", "session list target", "List retained shells for a connection", "connection", "Shells", "Reads Hovel session records; unavailable modules remain explicit.", noReview, "core/connection/sessions_linux.go", "core/cmd/burrow/sessions_lab.py"),
	op("session.inspect", "session inspect", "session inspect CONNECTION ID", "session inspect target shell-id", "Inspect retained shell identity and state", "connection session-id", "Shell", "Reads lifecycle and bounded output counts; never reads terminal bytes or reconnects.", noReview, "core/connection/sessions_linux.go", "core/cmd/burrow/sessions_lab.py"),
	op("session.close", "session close", "session close CONNECTION ID [--yes] [--review HASH]", "session close target shell-id", "Close one retained SSH shell", "connection session-id yes review", "ShellReview", "Terminates and waits for its owned SSH subprocess; preserves the master and sibling resources. Remote-command outcomes and escaped descendants remain uncertain.", confirmReview, "core/connection/sessions_linux.go", "core/cmd/burrow/sessions_lab.py"),
	op("connection.list", "connections", "connections", "connections", "List live, lost and unverified connection owners", "", "States", inspectEffect, noReview, "core/connection/commands.go", "core/cmd/burrow/manager_lab.py"),
	op("connection.inspect", "inspect", "inspect NAME", "inspect target", "Inspect one connection and its ownership", "name", "State", inspectEffect, noReview, "core/connection/commands.go", "core/cmd/burrow/manager_lab.py"),
	op("connection.connect", "connect", "connect NAME HOST --user USER [OPTIONS]", "connect target 192.0.2.10 --user alice", "Create a shell-free SSH master; bare connect opens a human form", connectionInputs, "ConnectionReview", "After approval authenticates and creates a retained connection; may initialize the workspace manager. SSH host keys are not verified under the accepted host-trust policy.", confirmReview, "core/connection/commands.go", "core/cmd/burrow/authentication_lab.py"),
	op("connection.reconnect", "reconnect", "reconnect NAME HOST --user USER [OPTIONS]", "reconnect target 192.0.2.10 --user alice", "Explicitly replace a lost owned connection", connectionInputs, "ConnectionReview", "After approval closes the lost owned resource and attempts a fresh connection; refuses active or unknown ownership.", confirmReview, "core/connection/commands.go", "core/cmd/burrow/manager_lab.py"),
	op("connection.close", "close", "close NAME [--yes] [--review HASH]", "close target", "Close all resources owned by a connection", "name yes review", "CloseReview", "Ends owned shells, transfers and tunnels; saved settings and artifacts survive.", "Review then --yes; --review HASH binds the exact workspace, owner, creation, observed state and tunnel revision. TUI binds the same identity.", "core/connection/commands.go", "core/cmd/burrow/workspace_lab.py"),
	op("profile.list", "profiles", "profiles", "profiles", "List settings and selected collection", "", "Collection", inspectEffect, noReview, "core/connection/profiles.go", "core/cmd/burrow/profile_check.py"),
	op("profile.select", "profile select", "profile select NAME", "profile select target", "Read saved settings without connecting", "name", "Profile", inspectEffect, noReview, "core/connection/profiles.go", "core/cmd/burrow/profile_check.py"),
	op("profile.create", "profile create", "profile create NAME HOST --user USER [OPTIONS]", "profile create target 192.0.2.10 --user alice", "Save new non-secret connection settings", "name host user profile-options", "ProfileChange", "Writes the selected collection; never connects or saves passwords.", noReview, "core/connection/profiles.go", "core/cmd/burrow/profile_check.py"),
	op("profile.edit", "profile edit", "profile edit NAME HOST --user USER [OPTIONS]", "profile edit target 192.0.2.10 --user alice", "Replace all saved settings", "name host user profile-options", "ProfileChange", "Replaces settings in the selected collection; omitted options reset to defaults.", "Review then --revision HASH --collection PATH --yes to bind approval; live resources remain.", "core/connection/profiles.go", "core/cmd/burrow/profile_check.py"),
	op("profile.delete", "profile delete", "profile delete NAME [--revision HASH] [--collection PATH] [--yes]", "profile delete target", "Delete saved settings", "name profile-review", "ProfileChange", "Removes one saved entry; live resources and artifacts remain.", "Review then --revision HASH --collection PATH --yes to bind approval.", "core/connection/profiles.go", "core/cmd/burrow/profile_check.py"),
	op("profile.save", "profile save", "profile save CONNECTION [--as NAME] [--revision HASH] [--collection PATH] [--yes]", "profile save target", "Save authenticated settings", "connection as profile-review", "ProfileChange", "Saves the connection's non-secret settings to the selected collection.", "New entry writes immediately. Replacement requires review and --yes; revision/collection can bind approval.", "core/connection/profiles.go", "core/cmd/burrow/profile_check.py"),
	op("profile.connect", "profile connect", "profile connect NAME [--as NAME] [--prompt] [--yes] [--review HASH]", "profile connect target", "Connect using selected saved settings", "name as prompt yes review", "ConnectionReview", "Expands saved settings into the same reviewed connection flow; --review binds the resolved settings, including the selected name, before authentication.", confirmReview, "core/connection/profiles.go", "core/cmd/burrow/profile_check.py"),
	op("profile.collection", "profile collection", "profile collection PATH", "profile collection /absolute/workspace/team.json", "Create or open a selected collection", "path", "Collection", "Creates a template if missing and persists collection selection; never connects.", noReview, "core/connection/profiles.go", "core/cmd/burrow/profile_check.py"),
	op("profile.load", "profile load", "profile load PATH", "profile load /absolute/workspace/team.json", "Select an existing saved collection", "path", "Collection", "Persists selection; missing collections fail; never connects.", noReview, "core/connection/profiles.go", "core/cmd/burrow/profile_check.py"),
	op("profile.backup", "profile backup", "profile backup PATH", "profile backup /absolute/workspace/team.backup.json", "Back up the selected collection", "path", "Backup", "Creates a new private file; refuses an existing destination.", noReview, "core/connection/profiles.go", "core/cmd/burrow/profile_check.py"),
	op("profile.history", "history", "history", "history", "Read retained profile command history", "", "Strings", inspectEffect, noReview, "core/connection/profiles.go", "core/cmd/burrow/profile_check.py"),
	op("shell.open", "shell", "shell NAME [SESSION_ID]", "shell target", "Create a retained shell or observe an existing session", "name session-id", "Terminal", "Creates and controls one retained shell, or observes the selected existing session. No login fallback; detach/Keep running preserve the shell.", noReview, "core/cmd/burrow/terminal.go", "core/cmd/burrow/shell_lab.py"),
	op("shell.list", "shells", "shells", "shells", "Discover retained workspace shells", "", "Terminal", "Discovers recognized shells from Hovel and opens independent observer tabs.", noReview, "core/cmd/burrow/terminal.go", "core/cmd/burrow/shell_lab.py"),
	op("shell.resume", "resume", "resume ID", "resume 1", "Select an attached retained shell", "shell-id", "Terminal", "Changes frontend focus without claiming control; does not create a connection or shell.", noReview, "core/cmd/burrow/terminal.go", "core/cmd/burrow/shell_lab.py"),
	op("shell.close", "shell-close", "shell-close [ID]", "shell-close 1", "Close the selected retained shell", "shell-id", "Terminal", "Explicitly closes that retained session; connection and sibling resources survive.", noReview, "core/cmd/burrow/terminal.go", "core/cmd/burrow/shell_lab.py"),
	op("shell.control", "shell-control", "shell-control [ID]", "shell-control 1", "Explicitly take control of an attached shell", "shell-id", "Terminal", "Generation-checked takeover fences the previous controller and applies the pane dimensions.", noReview, "core/cmd/burrow/shared_shells.go", "core/cmd/burrow/shell_lab.py"),
	op("shell.detach", "shell-detach", "shell-detach [ID]", "shell-detach 1", "Release this frontend's claim and retain the shell", "shell-id", "Terminal", "Releases only this attachment's token; preserves the shell, dimensions and other controllers.", noReview, "core/cmd/burrow/shared_shells.go", "core/cmd/burrow/shell_lab.py"),
	op("tunnel.create", "tunnel create|tunc", "tunnel create CONNECTION forward|reverse LISTEN HOST PORT [--review HASH] [--yes]", "tunnel create target forward 8080 127.0.0.1 80", "Create a local or reverse forward on an existing connection", "connection direction listen host port review yes", "TunnelReview", "After approval binds a listener; explicit broad IP binds expose it. Reverse LISTEN 0 requests a random high port; returns actual endpoint.", confirmReview, "core/connection/forward.go", "core/cmd/burrow/forward_lab.py"),
	op("tunnel.list", "tunnel list", "tunnel list", "tunnel list", "List retained forwards and exact qualified IDs", "", "Tunnels", inspectEffect, noReview, "core/connection/forward.go", "core/cmd/burrow/forward_lab.py"),
	op("tunnel.check", "tunnel check", "tunnel check CONNECTION/ID", "tunnel check target/0123456789abcdef0123456789abcdef", "Passively check the destination greeting", "tunnel-id", "TunnelCheck", "Attempts traffic through the existing listener; no remote bytes retained. Silent protocols need their own client to prove reachability.", noReview, "core/connection/forward.go", "core/cmd/burrow/forward_lab.py"),
	op("tunnel.remove", "tunnel remove|tund", "tunnel remove CONNECTION/ID [--yes]", "tunnel remove target/0123456789abcdef0123456789abcdef", "Remove exactly one listener", "tunnel-id yes", "TunnelReview", "Stops new connections on that listener; accepted streams may finish; siblings survive.", confirmReview, "core/connection/forward.go", "core/cmd/burrow/forward_lab.py"),
	op("tunnel.proxy-create", "proxy create", "proxy create CONNECTION LISTEN [--review HASH] [--yes]", "proxy create target 1080", "Create connection-owned SOCKS4/5", "connection listen review yes", "ProxyReview", "Binds an unauthenticated SOCKS listener; one per connection, separate from L/R IDs and counts.", confirmReview, "core/connection/proxy.go", "core/cmd/burrow/connection_lab.py"),
	op("tunnel.proxy-inspect", "proxy inspect", "proxy inspect CONNECTION", "proxy inspect target", "Inspect SOCKS endpoint and owner identity", "connection", "Proxy", inspectEffect, noReview, "core/connection/proxy.go", "core/cmd/burrow/connection_lab.py"),
	op("tunnel.proxy-remove", "proxy remove", "proxy remove CONNECTION [--review HASH] [--yes]", "proxy remove target", "Remove SOCKS only", "connection review yes", "ProxyReview", "Removes SOCKS while preserving the master and L/R forwards.", confirmReview, "core/connection/proxy.go", "core/cmd/burrow/connection_lab.py"),
	op("files.pwd", "scp|scp * pwd", "scp NAME [pwd [PATH]]", "scp target pwd", "Resolve a remote directory", "name remote-path", "FileListing", "Queries remote SFTP through the existing master; records file history. CLI is stateless; TUI keeps current directories.", noReview, "core/connection/remote_files.go", "core/cmd/burrow/files_lab.py"),
	op("files.list", "scp * ls", "scp NAME ls [PATH]", "scp target ls", "List remote files including hidden entries", "name remote-path", "FileListing", "Reads remote SFTP metadata; records history. Links are shown, with numeric UID/GID fallback.", noReview, "core/connection/remote_files.go", "core/cmd/burrow/files_lab.py"),
	op("files.tree", "scp * tree", "scp NAME tree [PATH]", "scp target tree", "Read a bounded remote directory tree", "name remote-path", "FileTree", "Reads at most 3000 entries or 30 seconds, reports partial results, and does not recurse into directory links.", noReview, "core/connection/remote_files.go", "core/cmd/burrow/files_lab.py"),
	op("files.cd", "scp * cd", "scp NAME cd [PATH]", "scp target cd /tmp", "Validate and resolve remote navigation", "name remote-path", "FileListing", "CLI returns the resolved directory; pass it on subsequent calls. TUI changes its file-tab directory.", noReview, "core/connection/remote_files.go", "core/cmd/burrow/files_lab.py"),
	op("files.complete", "scp * complete", "scp NAME complete [PATH]", "scp target complete /tmp/", "Read contextual remote path completion", "name remote-path", "FileListing", "Uses bounded cached SFTP discovery; completion is not command history.", noReview, "core/connection/remote_files.go", "core/cmd/burrow/files_lab.py"),
	op("files.cancel", "scp * cancel", "scp NAME cancel", "scp target cancel", "Cancel pending file discovery", "name", "FileListing", "Cancels pending owner discovery; does not cancel transfers. TUI Ctrl+C uses its exact discovery request ID.", noReview, "core/connection/remote_files.go", "core/cmd/burrow/files_lab.py"),
	op("files.roots", "local", "local", "local", "Inspect effective transfer roots", "", "FileRoots", inspectEffect, noReview, "core/connection/files.go", "core/cmd/burrow/files_check.py"),
	op("files.set-root", "local *", "local [download|upload] PATH", "local download /absolute/workspace/downloads", "Persist a workspace transfer root", "area path", "FileRoots", "Creates a missing private root directory, validates it and persists the selection; never moves or deletes files; unsafe roots fail without fallback.", noReview, "core/connection/files.go", "core/cmd/burrow/files_check.py"),
	op("files.local-cd", "lcd", "lcd [download|upload] PATH", "lcd download .", "Validate navigation within a local root", "area local-path", "FileListing", "Inspects containment and accessibility; records history. CLI navigation is stateless.", noReview, "core/connection/files.go", "core/cmd/burrow/files_check.py"),
	op("files.local-list", "lls", "lls [download|upload] [PATH]", "lls", "List contained local files", "area local-path", "FileListing", "Inspects local metadata and records history; escaping or broken links are labelled.", noReview, "core/connection/files.go", "core/cmd/burrow/files_check.py"),
	op("files.history", "files-history", "files-history", "files-history", "Read file command history", "", "Strings", inspectEffect, noReview, "core/connection/files.go", "core/cmd/burrow/files_check.py"),
	op("transfer.get", "scp * get", "scp NAME get REMOTE [LOCAL] [--review HASH] [--yes]", "scp target get /tmp/result.txt", "Review and start a single download", "name remote local-destination review yes", "DownloadReview", "Reviews exact destination and replacement; confirmed copying continues independently of the frontend. Working files are not automatically evidence.", "Repeat with --review DIGEST --yes from the unchanged plan; TUI reviews the same plan.", "core/connection/downloads.go", "core/cmd/burrow/files_lab.py"),
	op("transfer.mget", "scp * mget", "scp NAME mget PATTERN [LOCAL_DIR] [--review HASH] [--yes]", "scp target mget '/tmp/*.txt'", "Review nonrecursive regular-file downloads", "name pattern local-destination review yes", "DownloadReview", "Discovers and reviews a fixed file plan; confirmed copies report retained per-file outcomes and totals.", "Repeat with --review DIGEST --yes; changes to the reviewed discovery are refused.", "core/connection/downloads.go", "core/cmd/burrow/files_lab.py"),
	op("transfer.put", "scp * put", "scp NAME put LOCAL [REMOTE] [--review HASH] [--yes]", "scp target put result.txt /tmp/result.txt", "Review and start a contained upload", "name local-source remote-destination review yes", "DownloadReview", "Reads inside the upload root; after approval replaces the reviewed remote destination safely. Download totals exclude uploads.", "Repeat with --review DIGEST --yes from the unchanged plan.", "core/connection/downloads.go", "core/cmd/burrow/files_lab.py"),
	op("transfer.downloads", "downloads", "downloads [ID]", "downloads", "Inspect downloads and workspace download totals", "transfer-id", "TransferInventory", inspectEffect, noReview, "core/connection/downloads.go", "core/cmd/burrow/files_lab.py"),
	op("transfer.list", "transfers", "transfers [ID]", "transfers", "Inspect both upload and download outcomes", "transfer-id", "TransferInventory", inspectEffect, noReview, "core/connection/downloads.go", "core/cmd/burrow/files_lab.py"),
	op("transfer.cancel", "transfer-cancel|download-cancel", "transfer-cancel ID", "transfer-cancel TRANSFER_ID", "Cancel a transfer and await cleanup acknowledgement", "transfer-id", "Download", "Cancels either direction; retains labelled partials and protects existing destinations.", noReview, "core/connection/downloads.go", "core/cmd/burrow/files_lab.py"),
	op("run.prepare", "run prepare", "run prepare CONNECTION [OPTIONS] -- COMMAND [ARG...]", "run prepare target -- /usr/bin/id", "Prepare a command, script or selected local tool without executing", runInputs, "Run", "Creates a retained run and private snapshots; source/stdin paths must be within the upload root. Does not execute the command.", noReview, "core/connection/runs.go", "core/cmd/burrow/runs_lab.py"),
	op("run.now", "run now", "run now CONNECTION [OPTIONS] [--yes] -- COMMAND [ARG...]", "run now target -- /usr/bin/id", "Prepare, review, launch, wait and collect", runInputs+" yes", "RunReview", "Preparation allocates a run even before approval. Confirm the returned run launch command to avoid preparing another run. --local runs on the daemon host.", confirmReview, "core/connection/runs.go", "core/cmd/burrow/runs_lab.py"),
	op("run.launch", "run launch", "run launch ID [--collect] [--review HASH] [--yes]", "run launch RUN_ID", "Launch a prepared run once", "run-id collect review yes", "RunReview", "Executes once; repeated launch is idempotent. Optional --collect waits and registers evidence; default launches without waiting.", confirmReview, "core/connection/runs.go", "core/cmd/burrow/runs_lab.py"),
	op("run.list", "run list", "run list", "run list", "List retained runs", "", "Runs", inspectEffect, noReview, "core/connection/runs.go", "core/cmd/burrow/runs_lab.py"),
	op("run.inspect", "run inspect", "run inspect ID", "run inspect RUN_ID", "Inspect execution, capture and cleanup separately", "run-id", "Run", inspectEffect, noReview, "core/connection/runs.go", "core/cmd/burrow/runs_lab.py"),
	op("run.output", "run output", "run output ID stdout|stderr OFFSET", "run output RUN_ID stdout 0", "Read captured bytes without collecting", "run-id stream offset", "RunOutput", "Reads up to 32 KiB as base64 with nextOffset and capture status; does not alter execution or collect evidence.", noReview, "core/connection/runs.go", "core/cmd/burrow/follow_lab.py"),
	op("run.follow", "run follow", "run follow ID [stdout|stderr] [OFFSET]", "run follow RUN_ID", "Open an independent live output viewer", "run-id stream offset", "Terminal", "Viewer state is frontend-local; closing the viewer does not cancel or collect the run.", noReview, "core/cmd/burrow/follow_ui.go", "core/cmd/burrow/follow_lab.py"),
	op("run.cancel", "run cancel", "run cancel ID [--review HASH] [--yes]", "run cancel RUN_ID", "Request ordinary process-group cancellation", "run-id review yes", "RunReview", "Attempts ordinary-group cleanup. Escaped descendants, PID races and remote cleanup after loss remain unconfirmed; local-tool exit is not remote termination.", confirmReview, "core/connection/runs.go", "core/cmd/burrow/runs_lab.py"),
	op("run.collect", "run collect", "run collect ID [--review HASH] [--yes]", "run collect RUN_ID", "Register retained output as Hovel evidence", "run-id review yes", "RunReview", "Registers complete or partial output artifacts independently of command success; evidence survives close.", confirmReview, "core/connection/runs.go", "core/cmd/burrow/runs_lab.py"),
	op("run.close", "run close", "run close ID [--review HASH] [--yes]", "run close RUN_ID", "Close a stopped run and drop its working output", "run-id review yes", "RunReview", "Deletes uncollected working output; registered artifacts survive. Active runs must be cancelled first.", confirmReview, "core/connection/runs.go", "core/cmd/burrow/runs_lab.py"),
	op("report.survey", "run survey", "run survey CONNECTION --os ubuntu [--timeout DURATION] [--budget BYTES] [--yes]", "run survey target --os ubuntu", "Review an Ubuntu survey and collect a Markdown report", "connection survey-options yes", "RunReview", "Prepares a fixed read-only preset, then after approval executes and collects via the run lifecycle; no sudo or installed helper.", confirmReview, "core/connection/survey.go", "core/cmd/burrow/reports_lab.py"),
	op("report.list", "reports", "reports", "reports", "List saved Markdown reports", "", "Reports", inspectEffect, noReview, "core/reports/reports.go", "core/cmd/burrow/reports_lab.py"),
	op("report.read", "report", "report ID", "report REPORT_ID", "Read original Markdown and verified metadata", "report-id", "Document", "Verifies private artifact files and SHA256; reads at most 1 MiB for display; never runs or collects. TUI renders the original through Glamour.", noReview, "core/reports/reports.go", "core/cmd/burrow/reports_lab.py"),
	op("chain.select", "chain select", "chain select CONNECTION", "chain select target", "Discover current live tunnel identities", "connection", "ChainSelection", inspectEffect, noReview, "core/connection/chains.go", "core/cmd/burrow/chains_lab.py"),
	op("chain.http", "chain http", "chain http CONNECTION TUNNEL_ID URL [--review HASH] [--yes]", "chain http target target/0123456789abcdef0123456789abcdef http://127.0.0.1:80/", "Perform a reviewed HTTP GET through one existing tunnel", "connection tunnel-id url review yes", "HTTPReview", "Uses the exact live owner and tunnel; never provisions or reconnects. Bounded GET retains status, byte count and SHA256 evidence, not response bodies.", confirmReview, "core/connection/chains.go", "core/cmd/burrow/chains_lab.py"),
	op("chain.export", "chain export", "chain export CONNECTION TUNNEL_ID URL", "chain export target target/0123456789abcdef0123456789abcdef http://127.0.0.1:80/", "Export a Hovel saved chain for an existing tunnel", "connection tunnel-id url", "SavedChain", "CLI emits JSON without execution. TUI stages a private chain and opens Hovel with throw ready for review.", "Export does not approve execution. Hovel throw requires its normal planning/confirmation and --allow-dangerous.", "core/connection/chains.go", "core/cmd/burrow/chains_lab.py"),
	op("chain.connect", "chain connect", "chain connect NAME HOST --user USER [OPTIONS]", "chain connect target 192.0.2.10 --user alice", "Export a reviewed connection chain", connectionInputs, "SavedChain", "Requires an existing SSH server; never deploys it. Key/agent export exits; password/prompt export holds a private one-use authentication broker for up to 10 minutes. TUI stages the chain.", "Export does not connect. Confirm a separate Hovel throw; connection-only options exclude --yes and -proxy.", "core/connection/chains.go", "core/cmd/burrow/chains_lab.py"),
	op("logs.view", "logs", "logs", "logs", "View private workspace operation and shell-lifecycle logs", "", "Terminal", "Opens a refreshed read-only snapshot in embedded Vim; interactive shell I/O is not recorded.", noReview, "core/cmd/burrow/logs.go", "core/cmd/burrow/terminal_check.py"),
}

func init() {
	for i := range commandOperations {
		op := &commandOperations[i]
		switch op.ID {
		case "session.create", "session.list", "session.inspect", "session.close", "session.claim", "session.takeover", "session.input", "session.resize", "session.release", "session.observe", "session.snapshot":
			op.Human = "Headless CLI: " + strings.TrimPrefix(op.Agent.Syntax, "burrow --workspace PATH ") + "; TUI shell tabs attach to these same retained sessions"
			op.Scope = "Explicit workspace, connection name and opaque Hovel shell ID; connection creation and manager generation are verified."
		case "run.prepare", "run.now":
			verb := strings.TrimPrefix(op.ID, "run.")
			yes := ""
			if verb == "now" {
				yes = " [--yes]"
			}
			op.Agent.Variants = []string{
				"burrow --workspace PATH run " + verb + " CONNECTION --script PATH --mode stream|inline|stage --interpreter PATH [OPTIONS]" + yes + " -- [ARG...]",
				"burrow --workspace PATH run " + verb + " CONNECTION --local [OPTIONS]" + yes + " -- /absolute/TOOL [ARG...]",
			}
		case "files.cancel":
			op.Agent.Status = "unsupported"
			op.Agent.Limitation = "The parser accepts scp NAME cancel, but the CLI cannot supply a discovery request ID, so it does not cancel another request. TUI Ctrl+C supplies its own request ID."
			op.Human = "Ctrl+C during file discovery in that file tab"
			op.Effects = "TUI cancels its current discovery request. The accepted CLI spelling has no cancellation effect."
			op.ModuleCommand = false
		case "tunnel.remove":
			op.Review = "Review then --yes. No --review flag; the exact qualified ID selects the listener."
		case "profile.connect":
			op.ModuleCommand = false
		case "shell.open", "run.follow", "logs.view":
			op.Agent.Status = "terminal-only"
			op.Agent.Limitation = "Requires terminal input; no headless structured route for this viewer."
			op.ModuleCommand = false
		case "shell.list", "shell.resume", "shell.close", "shell.control", "shell.detach":
			op.Agent.Status = "unsupported"
			op.Agent.Limitation = "These tab-number shortcuts require a frontend. Headless agents use session list/inspect/claim/takeover/input/resize/release/close with opaque Hovel IDs."
			op.Scope = "Current frontend and selected workspace only; IDs are local to that frontend."
		}
	}
}

// ResultSchemas derives field names, optionality and nested types from the
// actual JSON result types. Map/union replies are declared separately below.
func ResultSchemas() map[string]any {
	values := map[string]any{
		"Activity": Activity{},
		"State":    State{}, "States": []State{}, "Profile": Profile{}, "Collection": Collection{},
		"Tunnel": Tunnel{}, "Tunnels": []Tunnel{},
		"FileRoots": FileRoots{}, "FileListing": FileListing{}, "FileTree": FileTree{},
		"Download": Download{}, "Downloads": Downloads{}, "DownloadPlan": DownloadPlan{},
		"Shell": Shell{}, "Shells": []Shell{}, "ShellControl": ShellControl{}, "ShellOutput": ShellOutput{},
		"Run": Run{}, "Runs": []Run{}, "RunOutput": RunOutput{},
		"Reports": []reports.Entry{}, "Document": reports.Document{}, "ChainSelection": chainSelection{},
		"TunnelHTTP": tunnelHTTPResult{}, "Workspace": launch.Info{}, "Strings": []string{}, "ManagerReview": ManagerReview{},
	}
	out := map[string]any{}
	for name, value := range values {
		out[name] = jsonShape(reflect.TypeOf(value))
	}
	for _, name := range []string{"ShellReview", "ConnectionReview", "CloseReview", "ProfileChange", "TunnelReview", "TunnelCheck", "Proxy", "ProxyReview", "DownloadReview", "TransferInventory", "RunReview", "HTTPReview", "Backup", "SavedChain", "Terminal", "Inventory"} {
		out[name] = map[string]any{"description": resultDescriptions[name]}
	}
	// These actual replies are maps or unions, not named Go structs.
	object := func(fields ...string) map[string]any {
		properties := map[string]any{}
		for _, field := range fields {
			properties[field] = map[string]string{"type": "string"}
		}
		return map[string]any{"type": "object", "properties": properties, "required": fields}
	}
	ref := func(name string) map[string]string { return map[string]string{"$ref": "#/results/" + name} }
	review := object("review", "digest")
	runReview := object("review", "digest")
	for _, field := range []string{"id", "confirm"} {
		runReview["properties"].(map[string]any)[field] = map[string]string{"type": "string"}
	}
	tunnelReview := object("review")
	tunnelReview["properties"].(map[string]any)["digest"] = map[string]string{"type": "string"}
	unions := map[string][]any{
		"ShellReview":       {review, ref("Shell")},
		"ConnectionReview":  {review, ref("State")},
		"CloseReview":       {review, object("state", "name")},
		"ProfileChange":     {object("review", "revision", "collection"), object("state", "name", "collection")},
		"TunnelReview":      {tunnelReview, ref("Tunnel"), object("id", "state", "detail")},
		"Proxy":             {ref("Tunnel"), object("state", "connection")},
		"ProxyReview":       {review, ref("Tunnel"), object("id", "state", "detail")},
		"DownloadReview":    {ref("DownloadPlan"), ref("Download")},
		"TransferInventory": {ref("Downloads"), ref("Download")},
		"RunReview":         {runReview, ref("Run")},
		"HTTPReview":        {review, ref("TunnelHTTP")},
	}
	for name, shapes := range unions {
		out[name].(map[string]any)["anyOf"] = shapes
	}
	out["TunnelCheck"] = object("id", "state", "detail")
	out["Backup"] = object("backup", "collection")
	out["SavedChain"].(map[string]any)["example"] = savedChain(map[string]string{"workspace": "/absolute/workspace", "command": "run list"})
	return out
}

var resultDescriptions = map[string]string{
	"ShellReview":       "Review {review,digest}; confirmation returns Shell. Creation prepares then launches through supported Hovel throws. Failed acknowledgement requires inspection, never automatic retry.",
	"ConnectionReview":  "Review object {review:string,digest:string} or State after confirmation. Interactive authentication may show a private prompt before State.",
	"CloseReview":       "{review:string,digest:string} before approval; {state:'closed',name:string} after close. --review binds the exact creation, state and tunnel revision.",
	"ProfileChange":     "Replacement review {review:string,revision:string,collection:string}; success {state:string,name:string,collection:string}.",
	"TunnelReview":      "Create review {review:string,digest:string}; confirmed creation returns Tunnel. Remove review {review:string}; confirmed removal returns {id:string,state:'removed',detail:string}.",
	"TunnelCheck":       "{id:string,state:string,detail:string}; state describes the observed check, not guaranteed destination availability.",
	"Proxy":             "Tunnel while configured; otherwise {state:'off'|'unavailable',connection:string}.",
	"ProxyReview":       "Review {review:string,digest:string}; confirmed creation returns Tunnel, removal returns {id:string,state:'removed',detail:string}.",
	"TransferInventory": "Downloads aggregate when ID is omitted; one Download when an ID is supplied. transfers includes both directions, downloads excludes uploads. Aggregate files/bytes remain download-only.",
	"DownloadReview":    "Review returns DownloadPlan directly (including digest); confirmed start returns Download; asynchronous state must be inspected.",
	"RunReview":         "Review {review:string,digest:string}, with id/confirm for now/survey; confirmed action returns Run. Inspect remoteExit/localExit, outputComplete, cancellation and collection separately; exit 0 is not proof of remote success.",
	"HTTPReview":        "Review {review:string,digest:string}; confirmed HTTP returns TunnelHTTP, including HTTP error status and evidence identity.",
	"Backup":            "{backup:string,collection:string}.",
	"SavedChain":        "Hovel saved-chain JSON: apiVersion, kind, metadata and spec (mode, steps, targets, config). Generated workspace/build/owner fields are opaque; regenerate after changes. Authoritative schema belongs to Hovel.",
	"Terminal":          "Human terminal interaction; no structured JSON result contract for this route.",
	"Inventory":         "Versioned capability inventory, optionally filtered by stable operation ID. Includes inputs, result shapes, errors, provenance and module schema.",
}

func jsonShape(t reflect.Type) map[string]any {
	if t == reflect.TypeOf(time.Time{}) {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	if t.Kind() == reflect.Pointer {
		return map[string]any{"anyOf": []any{jsonShape(t.Elem()), map[string]any{"type": "null"}}}
	}
	switch t.Kind() {
	case reflect.Struct:
		properties, required := map[string]any{}, []string{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			name, options, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "-" {
				continue
			}
			if f.Anonymous && name == "" {
				child := jsonShape(f.Type)
				for key, shape := range child["properties"].(map[string]any) {
					properties[key] = shape
				}
				required = append(required, child["required"].([]string)...)
				continue
			}
			if name == "" {
				name = f.Name
			}
			properties[name] = jsonShape(f.Type)
			if !strings.Contains(options, "omit") {
				required = append(required, name)
			}
		}
		return map[string]any{"type": "object", "properties": properties, "required": required}
	case reflect.Slice, reflect.Array:
		if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": []string{"string", "null"}, "contentEncoding": "base64"}
		}
		shape := map[string]any{"type": "array", "items": jsonShape(t.Elem())}
		if t.Kind() == reflect.Slice {
			shape["type"] = []string{"array", "null"}
		}
		return shape
	case reflect.Map:
		return map[string]any{"type": []string{"object", "null"}, "additionalProperties": jsonShape(t.Elem())}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.String:
		return map[string]any{"type": "string"}
	default:
		return map[string]any{}
	}
}
