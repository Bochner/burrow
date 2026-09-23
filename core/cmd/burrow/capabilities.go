package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/Bochner/burrow/core/connection"
	"github.com/Bochner/burrow/core/launch"
)

func capabilities(fs *flag.FlagSet, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("expected capabilities [ID]")
	}
	ops := append(connection.Operations(), frontendOperations...)
	sort.Slice(ops, func(i, j int) bool { return ops[i].ID < ops[j].ID })
	if len(args) == 1 {
		var selected []connection.Operation
		for _, op := range ops {
			if op.ID == args[0] {
				selected = append(selected, op)
			}
		}
		if len(selected) == 0 {
			return fmt.Errorf("unknown capability; use burrow capabilities")
		}
		ops = selected
	}
	binarySHA, err := launch.Build()
	if err != nil {
		return err
	}
	flags := []map[string]string{}
	fs.VisitAll(func(f *flag.Flag) {
		flags = append(flags, map[string]string{"name": "--" + f.Name, "default": f.DefValue, "description": f.Usage})
	})
	results := connection.ResultSchemas()
	results["SkillInstallation"] = map[string]any{
		"type": "object", "required": []string{"version", "bundleSHA256", "source", "provenance", "upstream", "destination", "dryRun", "complete", "skills"},
		"description": "Offline installation report. A failure may emit this partial report on stdout, then text stderr and exit 1; planned actions are not success. Native client discovery is not implied.",
		"properties": map[string]any{
			"version": map[string]string{"type": "string"}, "bundleSHA256": map[string]string{"type": "string", "description": "SHA256 of the manifest, which binds every bundled file hash; not a publisher signature."},
			"source": map[string]string{"type": "string"}, "provenance": map[string]string{"type": "string"}, "upstream": map[string]string{"type": "string"}, "destination": map[string]string{"type": "string"},
			"dryRun": map[string]string{"type": "boolean"}, "complete": map[string]string{"type": "boolean"},
			"skills": map[string]any{"type": "array", "items": map[string]any{"type": "object", "required": []string{"name", "path", "action", "state"}, "properties": map[string]any{
				"name": map[string]string{"type": "string"}, "path": map[string]string{"type": "string"}, "backup": map[string]string{"type": "string"},
				"action": map[string]any{"enum": []string{"install", "update", "unchanged"}}, "state": map[string]any{"enum": []string{"planned", "applied", "unchanged", "failed"}},
			}}},
		},
	}
	results["WorkspaceInventory"] = map[string]any{
		"type": "object", "required": []string{"scope", "registryAvailable", "workspaces"},
		"description": "Only explicitly supplied paths, never an authoritative registry. Each observation is independent; exit 0 can include unverified entries.",
		"properties": map[string]any{
			"scope": map[string]any{"const": "explicit-paths"}, "registryAvailable": map[string]any{"const": false},
			"workspaces": map[string]any{"type": "array", "items": map[string]any{
				"type": "object", "required": []string{"workspacePath", "state"},
				"properties": map[string]any{"workspacePath": map[string]string{"type": "string"}, "state": map[string]any{"enum": []string{"verified", "unverified"}}, "daemon": map[string]string{"$ref": "#/results/Workspace"}, "error": map[string]string{"type": "string"}},
			}},
		},
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"schemaVersion": 1,
		"module":        map[string]string{"name": (connection.Module{}).Info().Name, "version": (connection.Module{}).Info().Version},
		"moduleSchema":  (connection.Module{}).Schema(),
		"operations":    ops, "globalFlags": flags,
		"inputs": capabilityInputs, "results": results,
		"errors": map[string]any{"encoding": "text/stderr", "exitCode": 1, "prefix": "Burrow: ", "structured": false,
			"workspace": map[string]any{"routes": "workspace open|inspect|list|restart|retire", "encoding": "JSON/stderr", "exitCode": 1,
				"shape":     "{error:{operation,workspacePath?,code,message,review?:ManagerReview}}",
				"codes":     []string{"invalid_arguments", "invalid_selection", "unverified", "review_required", "review_changed", "cleanup_unconfirmed", "registration_failed"},
				"semantics": "Only workspace subcommands use this structured error contract. Global flag parsing errors remain text. cleanup_unconfirmed may follow partial cleanup; inspect before retrying. registration_failed includes review.state=retired. Missing acknowledgement never promises rollback."},
			"lifecycle": map[string]any{"routes": []string{"close", "profile connect"}, "encoding": "JSON/stderr", "exitCode": 1, "shape": "{error:{operation,workspacePath?,code:'operation_failed',message}}", "semantics": "Validation, stale review, ownership, dispatch and cleanup failures are structured. The message distinguishes the refusal; do not automatically retry an uncertain operation. Global flag parsing errors remain text."},
			"semantics": "Other routes: validation, ownership, unavailable-resource, approval, transport and persistence failures are text errors. Success/review JSON exits 0. A returned Run or transfer can still contain a failed/partial/uncertain outcome; inspect its state and capture fields. No stable error codes or automatic remote-exit propagation are promised."},
		"provenance": map[string]any{"binarySHA256": binarySHA, "source": "https://github.com/Bochner/burrow", "contractSource": "core/connection/capabilities.go", "frontendSource": "core/cmd/burrow/capabilities.go", "hovelRuntimeURL": launch.PackageURL, "hovelWheelSHA256": launch.WheelSHA, "hovelExecutableSHA256": launch.ExecutableSHA,
			"versionPolicy": "schemaVersion versions discovery; module version identifies the Hovel module. binarySHA256 identifies the exact producer, including local uncommitted builds. Source paths refer to that build's checkout, not necessarily public main."},
		"integration": map[string]any{
			"module":   "Only burrow@0.1.0; JSON-RPC stdio is owned by the public Hovel SDK. module is the Hovel launch entry point, not a human command.",
			"command":  "The moduleCommand field marks accepted base-module command routes. Supply workspace and a quoted command. An empty command verifies existing workspace identity. Execute only through the verified workspace daemon; normal Hovel planning, confirmation and --allow-dangerous apply. Profile results live in result data; other supported command results also appear as JSON in summary.",
			"adapters": "action/generation/session/request/review/build are opaque confirmed-manager adapter inputs emitted by Burrow. Use generated chains or Burrow CLI; do not hand-author owner requests, launch keys or build hashes. connection is a retired input that always refuses nonempty values.",
			"daemon":   "Supported public Hovel RPC over the verified owner-protected workspace socket; Burrow-started pinned instances only. Burrow does not publish a separate REST service or SDK. General existing-daemon attachment, generic Mesh routing and retained interactive shells remain unsupported.",
			"upstream": map[string]string{"rpc": "https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/docs/site/spec/reference/daemon-rpc.openapi.json", "sdk": "https://github.com/vibepwners/hovel/tree/a4cbfdf7769a9551695088c11061e3cabc368e07/sdk/go/hovel", "chains": "https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/docs/site/src/content/spec/chains-runs.html"},
			"errors":   "Upstream Hovel can return non-JSON CLI failures; its wrapper JSON and module result are separate from Burrow's CLI stdout contract.",
		},
		"presentation": map[string]any{
			"aliases":  map[string]string{"tunc CONNECTION l|r ...": "tunnel.create (forward|reverse)", "tund CONNECTION/ID": "tunnel.remove", "download-cancel ID": "transfer.cancel", "-ip/-socket/-ssh-key": "--host/--name/--key connection options", "--load PATH": "profile.load during startup", "file mode ls/tree/cd/pwd/get/mget/put": "files.* / transfer.* with the selected connection and per-tab directories", "file mode history": "files.history", "reports/report": "report.list/read with a human renderer in TUI", "Ctrl+N": "logs.view", "Saved profile Edit (Vim)": "profile.edit through a private one-profile draft; validates and binds revision on successful editor exit; CLI uses profile edit"},
			"controls": "Help/F1, Ctrl+P search/menu, Tab completion, command recall, scrolling, NO_COLOR, mouse toggle, clipboard copy, workspace/tab selection, Ctrl+L output/activity views, and file-mode back/exit alter presentation or select existing capabilities; they are not independent remote operations. Ctrl+] backgrounds a shell; frontend quit ends its local shells.",
			"stdout":   "Discovery always emits JSON. Workspace operations, close and profile connect use structured stderr on failure; human terminals apply the shared semantic JSON renderer to their results and errors. Other nonterminal stdout emits JSON; terminal stdout uses the shared semantic renderer. Shell, logs and run follow routes are interactive. Workspace follow emits NDJSON with --json or a pipe, otherwise a colored scrolling feed. Global flags precede the command; tokens after -- in run commands belong to the target program.",
		},
		"evidencePolicy": "Dispatch registration and successful route parsing prove reachability only. Reflected result schemas describe wire shapes, not execution outcomes. Referenced semantic checks assert selected real behavior; neither inventory size, shared help, nor upstream coverage percentages establish 100% parity.",
	})
}

func frontendOp(id, category, summary, syntax, human, status, result, effects, review, source, check string) connection.Operation {
	return connection.Operation{ID: id, Category: category, Summary: summary, Patterns: [][]string{}, Human: human,
		Agent: connection.Route{Status: status, Syntax: syntax, Example: []string{}}, Inputs: []string{}, Result: result,
		Scope: "Local operator installation/frontend; workspace operations require an explicit canonical path.", Effects: effects, Review: review,
		Evidence: []connection.Evidence{{Kind: "dispatch", Source: source}, {Kind: "semantic-check", Source: check}}}
}

var frontendOperations = []connection.Operation{
	frontendOp("logs.follow", "workspace", "Watch new Hovel and Burrow activity without initializing or controlling work", "burrow --workspace PATH follow [--json]", "Separate terminal; normal TUI and agent CLI can run alongside", "supported", "Activity", "Streams version-1 NDJSON with --json or nonterminal stdout; otherwise uses semantic colors and native scrollback. Starts with current resources and new evidence, not history replay. Includes Hovel PollLogs, existing Burrow audit evidence, retained-run stdout/stderr and transfer progress. Each viewer owns volatile cursors; gap/resumed events expose lost history, source failures and daemon changes. Output data is base64 with stream byte offsets; output-end marks a captured stream end. Shared interactive shell bytes and unpublished Hovel commands are unavailable. See operation logs documentation for limits.", "No approvals or target actions; requires verified existing daemon. Ctrl+C stops only this viewer. Startup errors use text stderr/nonzero exit; recoverable source errors stay in the stream. NO_COLOR/global --no-color preserve text.", "core/cmd/burrow/activity.go", "core/cmd/burrow/activity_check.py"),
	frontendOp("capabilities.list", "workspace", "Discover the current contract without initialization", "burrow capabilities [ID]", "CLI or API documentation tab", "supported", "Inventory", "Inspects this executable only; no workspace, cache, daemon, network or TUI initialization.", "None.", "core/cmd/burrow/main.go", "core/cmd/burrow/capabilities_check.py"),
	frontendOp("workspace.open", "workspace", "Initialize or reuse the verified workspace", "burrow --workspace PATH workspace open", "New workspace form, launch burrow --workspace PATH, or CLI status alias", "supported", "Workspace", "Initializes local workspace/cache as needed, downloads pinned Hovel unless --offline, starts or verifies the daemon, creates default profiles/file roots, and registers the base module. Never connects to an SSH target. status remains an initializing alias.", "The explicit invocation authorizes setup; ambiguous or stale owners are refused.", "core/cmd/burrow/workspace.go", "core/cmd/burrow/workspace_check.py"),
	frontendOp("workspace.inspect", "workspace", "Inspect an existing workspace daemon without initialization", "burrow --workspace PATH workspace inspect", "TUI status or Menu → Refresh daemon status", "supported", "Workspace", "Verifies the selected launch receipt, paths, daemon process and actual socket peer. Never creates, replaces or reconnects resources. This is daemon health; use connections for SSH state.", "None; failed verification returns an unverified error.", "core/cmd/burrow/workspace.go", "core/cmd/burrow/workspace_check.py"),
	frontendOp("workspace.list", "workspace", "Inspect explicitly supplied workspace paths", "burrow workspace list PATH [PATH...]", "CLI; TUI sidebar lists only paths opened in that frontend", "supported", "WorkspaceInventory", "Read-only observations of supplied paths only; no directory scanning, persisted registry, target creation or implicit selection. A verified entry describes a supported Burrow-started daemon, not its SSH connections.", "None. --workspace is not allowed with list; missing paths refuse. Unverified entries include an error and never claim an empty resource inventory.", "core/cmd/burrow/workspace.go", "core/cmd/burrow/workspace_check.py"),
	frontendOp("workspace.retire", "workspace", "Retire exactly the reviewed workspace manager headlessly", "burrow --workspace PATH workspace retire [--yes --review HASH]", "Headless CLI; restart also retires the manager", "supported", "ManagerReview", "Ends the whole manager, including concurrent additions and dependent connections, shells, transfers and tunnels. Does not register a build or start a replacement. Preserves saved settings, collected evidence, separate retained runs and Hovel.", "Returns state=review and exact action/daemon/manager digest. Confirm with --yes --review HASH; success state=retired retains affected identities. Unknown ownership refuses; cleanup errors may be partial. Reconnect explicitly.", "core/cmd/burrow/workspace.go", "core/cmd/burrow/workspace_lab.py"),
	frontendOp("workspace.tui", "workspace", "Open the management frontend", "burrow --workspace PATH [tui]", "Normal Burrow launch", "terminal-only", "Terminal", "Initializes as workspace.open then opens the TUI; --load opens a collection without connecting.", "The explicit invocation authorizes local setup.", "core/cmd/burrow/main.go", "core/cmd/burrow/terminal_check.py"),
	frontendOp("workspace.restart", "workspace", "Retire the reviewed manager and register the current build headlessly", "burrow --workspace PATH workspace restart [--yes --review HASH]", "Headless CLI; legacy restart [--yes] confirms in a terminal then opens the TUI", "supported", "ManagerReview", "Closes the selected manager and all its connections, shells, transfers and tunnels, including concurrent additions. Registers the current build without starting a manager, TUI or SSH connection. Preserves saved settings, evidence, separate retained runs and Hovel. Requires an already verified workspace.", "First returns state=review and digest bound to action and daemon/manager incarnation. --yes --review HASH is required for headless confirmation; success state=ready. Wrong, stale or unknown owners refuse; cleanup errors can be partial. Legacy terminal restart keeps its existing confirmation.", "core/cmd/burrow/workspace.go", "core/cmd/burrow/workspace_lab.py"),
	frontendOp("workspace.quit", "workspace", "Review connections across opened workspaces before quitting", "No standalone headless frontend-quit route", "TUI quit / Ctrl+C: keep running, close reviewed connections, or cancel", "unsupported", "Terminal", "Keep detaches and preserves daemon connections/tunnels; close tears down only reviewed owners. Both exit choices end frontend-local shells; cancel keeps the frontend.", "Shared quit review; uncertain ownership refuses teardown.", "core/cmd/burrow/quit.go", "core/cmd/burrow/connection_lab.py"),
	frontendOp("workspace.hovel-cli", "workspace", "Open or restart the embedded workspace Hovel CLI", "Use the installed pinned Hovel CLI with --workspace PATH --daemon-endpoint PATH/hoveld.sock", "Hovel tab / Alt+H; restart control; Menu closes the CLI", "delegated", "Terminal", "Starts a local Hovel frontend bound to the workspace; inherits Hovel operations and approval rules. Closing/restarting the tab ends only that frontend.", "Hovel owns confirmation and structured output; tab opening is not operation approval.", "core/cmd/burrow/terminal.go", "core/cmd/burrow/terminal_check.py"),
	frontendOp("installation.package", "installation", "Build the local Linux amd64 distributable", "aspect burrow package", "Same Aspect command; extract the produced tgz and run burrow", "supported", "Terminal", "Builds the executable with embedded skills, standalone skill bundle, base-module manifest and upstream license; does not start a workspace. No released distribution installer or automatic Burrow upgrade route.", "Explicit build invocation.", ".aspect/launch.axl", "core/cmd/burrow/setup_check.py"),
	frontendOp("installation.skills", "installation", "Install or update the bundled operator skills offline", "burrow agent install claude|codex|opencode --scope user|project [--source PATH] [--dry-run]", "CLI; explicit scope, optional dry-run, shared semantic JSON output", "supported", "SkillInstallation", "Installs burrow and burrow-inspect into the client's documented discovery directory. --source selects an absolute trusted directory extracted from burrow-agent.zip; otherwise uses the embedded release. No network/cache/daemon/client initialization. Preserves edited/unmanaged conflicts and unrelated integrations; updates retain previous directories outside discovery, with per-skill failure recovery. Identical unmanaged content is unchanged, not adopted. No suite transaction, concurrent-editor isolation, plugin distribution, mandatory MCP or model runtime. Omitted skills stay installed.", "Explicit install invocation and required scope; --dry-run preflights the complete suite without writing. Check complete and every skill state; on failed restoration the backup path identifies retained content. Native discovery is opt-in and missing clients are reported.", "core/agent/install.go", "core/cmd/burrow/agent_check.py"),
}

// Each entry is a named input or an option group. Required/optional positions
// are specified by the operation's syntax; these definitions give constraints
// and defaults shared across those invocations.
var capabilityInputs = map[string]any{
	"name":               map[string]any{"type": "string", "description": "Workspace-local connection/profile name, 1–24 ASCII letters, digits, underscores or hyphens. Must start with a letter or digit; no paths."},
	"connection":         map[string]any{"type": "string", "description": "Explicit existing connection name; live verified manager ownership is required for operations using its transport."},
	"host":               map[string]any{"type": "string", "description": "SSH host/alias for connection calls; destination hostname or literal IP for tunnel calls."},
	"user":               map[string]any{"type": "string", "description": "Required --user USER; '-' uses the SSH config user. Positional third USER also accepted."},
	"port":               map[string]any{"type": "integer", "minimum": 1, "maximum": 65535},
	"listen":             map[string]any{"type": "string", "description": "PORT defaults to 127.0.0.1, or explicit IP:PORT ([IPv6]:PORT). Reverse only: 0 chooses 49152–65535 with at most 8 attempts."},
	"direction":          map[string]any{"type": "string", "enum": []string{"forward", "reverse"}, "description": "tunc aliases use l/r."},
	"path":               map[string]any{"type": "string", "description": "Absolute canonical local path; ownership, permissions and symlink checks apply."},
	"area":               map[string]any{"type": "string", "enum": []string{"download", "upload"}, "default": "download"},
	"local-path":         map[string]any{"type": "string", "default": ".", "description": "Contained within the selected root; absolute contained paths accepted. lcd requires a path."},
	"remote-path":        map[string]any{"type": "string", "default": ".", "description": "Remote SFTP path; CLI never retains a working directory between invocations; TUI does."},
	"remote":             map[string]any{"type": "string", "description": "Required remote source path, up to 4096 bytes, no NUL."},
	"pattern":            map[string]any{"type": "string", "description": "Nonrecursive remote regular-file glob; quote it to prevent local shell expansion."},
	"local-destination":  map[string]any{"type": "string", "description": "Optional destination within download root; default derives from remote filename (mget uses the root). Exact destinations and existing files appear in review."},
	"local-source":       map[string]any{"type": "string", "description": "Required source contained in upload root; relative paths start there."},
	"remote-destination": map[string]any{"type": "string", "description": "Optional destination, default source basename at remote directory; inspect the exact review before approval."},
	"shell-id":           map[string]any{"type": "integer", "minimum": 1, "description": "Canonical positive decimal ID in the frontend's shells inventory. shell-close defaults to last selected shell."},
	"run-id":             map[string]any{"type": "string", "description": "Opaque retained ID from run prepare/list/now; not the Hovel launch runID."},
	"report-id":          map[string]any{"type": "string", "description": "Opaque ID returned by reports; no path separators."},
	"transfer-id":        map[string]any{"type": "string", "description": "Opaque ID from transfers/downloads; omit to list, required for cancellation."},
	"tunnel-id":          map[string]any{"type": "string", "description": "Exact qualified CONNECTION/creation-ID from tunnel list or chain select; never synthesize an ID from a port."},
	"url":                map[string]any{"type": "string", "description": "http://HOST[:PORT]/PATH only; no TLS, redirects, credentials or query. Fixed tunnels require the exact destination. GET limit 8 seconds and 1 MiB."},
	"stream":             map[string]any{"type": "string", "enum": []string{"stdout", "stderr"}, "description": "Required for output; follow defaults to stdout."},
	"offset":             map[string]any{"type": "integer", "minimum": 0, "description": "Required byte offset for output; follow defaults to its remembered position or zero. Reads return nextOffset."},
	"command":            map[string]any{"type": "array", "items": map[string]string{"type": "string"}, "description": "Tokens after --, individually quoted remotely. Use /bin/sh -c explicitly for shell syntax. For --script, these are script arguments; --local requires an absolute tool path. No secrets in commands/source/arguments."},
	"yes":                map[string]any{"type": "boolean", "default": false, "description": "--yes is explicit approval; discovery never adds it automatically."},
	"review":             map[string]any{"type": "string", "description": "--review HASH from recap binds the reviewed request or resource. Required with --yes for transfers and headless workspace restart/retire; optional for connect, profile connect, close and other advertised routes. Not accepted by tunnel remove."},
	"as":                 map[string]any{"type": "string", "description": "Optional --as NAME; defaults to saved/live name."},
	"prompt":             map[string]any{"type": "boolean", "default": false, "description": "--prompt requests private interactive authentication."},
	"collect":            map[string]any{"type": "boolean", "default": false, "description": "--collect on launch waits for completion and registers output; stopping the wait does not cancel execution."},
	"profile-review":     map[string]any{"type": "object", "description": "--revision HASH and --collection PATH optionally bind a saved-settings review; --yes confirms replacement/deletion."},
	"profile-options":    map[string]any{"type": "object", "description": "Connection key/agent/port/ssh-config/jump/proxy options; no --password or --prompt. Edit replaces all fields. --revision HASH/--collection PATH bind review, --yes approves."},
	"connection-options": map[string]any{"type": "object", "properties": map[string]any{
		"--port":       map[string]any{"type": "integer", "description": "Default SSH config port or 22."},
		"--key":        map[string]any{"type": "string", "description": "Absolute private-key reference; ~/ expansion supported; default current-user SSH identities."},
		"--agent":      map[string]any{"type": "string", "description": "Default frontend SSH_AUTH_SOCK / effective SSH configuration; must be accessible by daemon."},
		"--ssh-config": map[string]any{"type": "string", "description": "Default ~/.ssh/config; supports aliases and selected safe settings."},
		"--jump":       map[string]any{"type": "string", "description": "[USER@]HOST[:PORT][,...]; config ProxyJump otherwise."},
		"--password":   map[string]any{"description": "Bare flag prompts privately; optional explicit value supplies target once, mutually exclusive with key/explicit nonempty agent. Value remains visible in original process argv; never persist it."},
		"--prompt":     map[string]any{"type": "boolean", "default": false},
		"-proxy":       map[string]any{"description": "Optional SOCKS port; omitted off, bare flag 9050. Not accepted by chain connect."},
		"--yes":        map[string]any{"type": "boolean", "default": false, "description": "Not accepted by chain connect; exported chains need a separate confirmed Hovel throw."},
		"--review":     map[string]any{"type": "string", "description": "Bind direct connection approval to preview digest."},
	}},
	"run-options": map[string]any{"type": "object", "properties": map[string]any{
		"--local":       map[string]any{"type": "boolean", "default": false, "description": "Run on daemon host in workspace; allowlisted environment; localExit differs from remoteExit."},
		"--script":      map[string]any{"type": "string", "description": "Upload-root source snapshot; requires explicit --mode and absolute --interpreter. Up to 256 MiB/input."},
		"--mode":        map[string]any{"enum": []string{"stream", "inline", "stage"}, "description": "Stream owns stdin; inline exposes source in argv and has a 64 KiB bound; stage uploads owned temporary files, remote only."},
		"--interpreter": map[string]any{"type": "string", "description": "Absolute interpreter path for scripts."},
		"--stdin":       map[string]any{"type": "string", "description": "Independent upload-root byte snapshot; incompatible with stream mode."},
		"--keep":        map[string]any{"type": "boolean", "default": false, "description": "Explicit stage mode only; retain uploaded script."},
		"--timeout":     map[string]any{"type": "string", "description": "Optional positive duration, e.g. 30s; default no execution timeout."},
		"--budget":      map[string]any{"type": "integer", "minimum": 1, "default": 268435456, "description": "Bytes per captured stream; incomplete capture is reported."},
	}},
	"survey-options": map[string]any{"type": "object", "required": []string{"--os"}, "properties": map[string]any{"--os": map[string]any{"enum": []string{"ubuntu"}}, "--timeout": map[string]any{"default": "90s", "description": "Optional positive duration."}, "--budget": map[string]any{"type": "integer", "minimum": 1, "default": 1048576}}},
}
