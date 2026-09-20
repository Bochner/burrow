package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/Bochner/burrow/core/connection"
	"github.com/Bochner/burrow/core/launch"
	"github.com/charmbracelet/x/term"
	"github.com/vibepwners/hovel/sdk/go/hovel"
)

const usage = `Burrow — verified Hovel workspace
Usage: burrow --workspace /absolute/workspace [options] [status|tui|COMMAND]
       burrow --demo [--no-color]

Required:
  --workspace PATH      Explicit canonical Hovel workspace
Options:
  --hovel-package FILE  Local copy of the pinned Linux amd64 wheel
  --load PATH           Open a saved collection (never connects automatically)
  --offline             Use the verified cache only
  --demo                Preview sample tables without opening a workspace
  --no-color            Disable terminal colors (also respects NO_COLOR)
  --help                Show this help without starting anything

status opens/reuses the workspace and prints verified daemon identity as JSON.
tui opens the management interface (default); quit retains the daemon.
restart [--yes] retires the workspace's Burrow manager, then opens the current TUI.
--yes skips restart confirmation and ends the workspace's connections and shells.
It ends that manager's connections and shells; saved settings, evidence and Hovel remain.
Inside the interface: status, connections, connect, chain connect, inspect, shell, reconnect, close, help, quit.
Type chain then F1 for SSH chain examples and options; Alt+B selects Burrow management.
Linux amd64 only. Cache: $XDG_CACHE_HOME/burrow/hovel/0.4.2 (or ~/.cache).
Unknown/stale resources require manual investigation; no automatic cleanup.
`

func safe(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			fmt.Fprintf(&b, "\\u%04x", r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Human terminals share the TUI's semantic renderer; pipes remain JSON-only.
func printResult(result any, noColor bool) error {
	if !term.IsTerminal(os.Stdout.Fd()) {
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	m := ui{output: string(body), noColor: noColor || os.Getenv("NO_COLOR") != ""}
	_, err = lipgloss.Fprintln(os.Stdout, m.styledOutput())
	return err
}

func run(args []string) error {
	if os.Getenv("BURROW_ASKPASS") == "1" {
		if len(args) != 1 || term.IsTerminal(os.Stdout.Fd()) {
			return fmt.Errorf("invalid authentication helper invocation")
		}
		return connection.Askpass(args[0])
	}
	if len(args) == 1 && args[0] == "connection-module" {
		return fmt.Errorf("legacy connection-module entry point retired; use Burrow connect for reviewed manager submission; existing retained owners remain available through connections/inspect/close")
	}
	if len(args) == 1 && args[0] == "module" {
		hovel.Serve(connection.Module{})
		return nil
	}
	fs := flag.NewFlagSet("burrow", flag.ContinueOnError)
	var o launch.Options
	var noColor, demo bool
	var loadPath string
	fs.StringVar(&loadPath, "load", "", "open saved collection without connecting")
	fs.StringVar(&o.Workspace, "workspace", "", "explicit canonical workspace (required)")
	fs.StringVar(&o.Package, "hovel-package", "", "pinned wheel file")
	fs.BoolVar(&o.Offline, "offline", false, "verified cache only")
	fs.BoolVar(&demo, "demo", false, "sample-data UI preview")
	fs.BoolVar(&noColor, "no-color", false, "disable colors")
	fs.Usage = func() { fmt.Fprint(fs.Output(), usage+"\n"+connection.Help) }
	if e := fs.Parse(args); e != nil {
		if e == flag.ErrHelp {
			return nil
		}
		return e
	}
	if noColor {
		if e := os.Setenv("NO_COLOR", "1"); e != nil {
			return e
		}
	}
	if demo {
		if fs.NArg() > 0 {
			return fmt.Errorf("--demo takes no commands")
		}
		if !term.IsTerminal(os.Stdin.Fd()) {
			return fmt.Errorf("demo requires terminal input")
		}
		return terminal(newDemoFrame(noColor || os.Getenv("NO_COLOR") != ""), noColor || os.Getenv("NO_COLOR") != "")
	}
	if o.Workspace == "" {
		return fmt.Errorf("--workspace PATH is required; use --help")
	}
	command := "tui"
	if fs.NArg() > 0 {
		command = fs.Arg(0)
	}
	restartApproved := command == "restart" && fs.NArg() == 2 && fs.Arg(1) == "--yes"
	if command == "restart" {
		if fs.NArg() != 1 && !restartApproved {
			return fmt.Errorf("expected restart [--yes]")
		}
		if !term.IsTerminal(os.Stdin.Fd()) {
			return fmt.Errorf("restart requires terminal input")
		}
	}
	if command != "status" && command != "tui" && command != "restart" {
		args := fs.Args()
		wizard := len(args) == 1 && command == "connect"
		if !wizard {
			if e := connection.ValidateCommand(o.Workspace, args); e != nil {
				return e
			}
		}
		if (command == "shell" || command == "logs") && !term.IsTerminal(os.Stdin.Fd()) {
			return fmt.Errorf("shell requires terminal input; use inspect NAME for JSON")
		}
		following := command == "run" && len(args) > 1 && args[1] == "follow"
		if following && !term.IsTerminal(os.Stdin.Fd()) {
			return fmt.Errorf("run follow requires a terminal; use run output ID stdout|stderr OFFSET for bounded JSON reads")
		}
		defer launch.Phase("cli:" + command)()
		interrupt, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		ctx, cancel := context.WithTimeout(interrupt, 60*time.Second)
		defer cancel()
		info, e := launch.Status(ctx, o.Workspace)
		if e != nil {
			return e
		}
		if loadPath != "" {
			if _, e := connection.Execute(ctx, o.Workspace, []string{"profile", "load", loadPath}); e != nil {
				return e
			}
		}
		if command == "logs" {
			m := newFrame(info, noColor || os.Getenv("NO_COLOR") != "", o)
			m.initialLogs = true
			return terminal(m, m.noColor)
		}
		if following {
			m := newFrame(info, noColor || os.Getenv("NO_COLOR") != "", o)
			m.initialFollow = args
			return terminal(m, m.noColor)
		}
		if command == "shell" {
			m := newFrame(info, noColor || os.Getenv("NO_COLOR") != "", o)
			m.initialShell = args[1]
			return terminal(m, m.noColor)
		}
		args, e = connection.ProfileConnect(ctx, o.Workspace, args)
		if e != nil {
			return e
		}
		if args[0] == "chain" && args[1] == "connect" {
			c, _, err := connection.Parse(o.Workspace, args[2:])
			if err != nil {
				return err
			}
			if c.Prompt {
				return promptChainCLI(interrupt, o.Workspace, args, noColor)
			}
		}
		if args[0] == "connect" || args[0] == "reconnect" {
			interactive := wizard
			if len(args) > 1 {
				c, yes, err := connection.Parse(o.Workspace, args[1:])
				if err != nil {
					return err
				}
				interactive = c.Prompt || (!yes && term.IsTerminal(os.Stdin.Fd()))
			}
			if interactive {
				a := &authenticate{workspace: o.Workspace, args: args, noColor: noColor}
				if e := a.Run(); e != nil {
					return e
				}
				return printResult(a.result, noColor)
			}
		}
		if connection.RunWaits(args) {
			ctx = interrupt
		}
		result, e := connection.Execute(ctx, o.Workspace, args)
		if e != nil {
			return e
		}
		return printResult(result, noColor)
	}
	if fs.NArg() > 1 && !restartApproved {
		return fmt.Errorf("status and tui take no arguments")
	}
	if command == "tui" && !term.IsTerminal(os.Stdin.Fd()) {
		return fmt.Errorf("tui requires terminal input; use status for JSON")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if command == "tui" {
		fmt.Fprintln(os.Stderr, "Verifying Hovel package and workspace…")
	}
	info, e := openWorkspace(ctx, o)
	if e != nil {
		return e
	}
	if loadPath != "" {
		if _, e = connection.Execute(ctx, o.Workspace, []string{"profile", "load", loadPath}); e != nil {
			return e
		}
	}
	if command == "status" {
		return json.NewEncoder(os.Stdout).Encode(info)
	}
	if command == "restart" {
		err := connection.RestartManager(ctx, o.Workspace, func(states []connection.State) bool {
			if restartApproved {
				return true
			}
			style := ui{noColor: noColor || os.Getenv("NO_COLOR") != "" || !term.IsTerminal(os.Stderr.Fd())}
			fmt.Fprintln(os.Stderr, style.paint(accent, "Workspace:"), style.paint(secondary, safe(o.Workspace)))
			for _, s := range states {
				fmt.Fprintf(os.Stderr, "  %s: %s\n", style.paint(accent, safe(s.Name)), style.paint(connectionStyle(s.State), safe(s.State)))
			}
			fmt.Fprintln(os.Stderr, "Close other Burrow frontends first. Restart ends ALL connections and shells owned by this workspace's Burrow manager, including concurrent additions.")
			fmt.Fprintln(os.Stderr, "Saved settings, evidence and Hovel are preserved. Reconnect explicitly afterward.")
			fmt.Fprint(os.Stderr, "Type restart to confirm (anything else cancels): ")
			line, err := bufio.NewReader(os.Stdin).ReadString('\n')
			return err == nil && strings.TrimSpace(line) == "restart"
		})
		if err != nil {
			return err
		}
	}
	return terminal(newFrame(info, noColor || os.Getenv("NO_COLOR") != "", o), noColor || os.Getenv("NO_COLOR") != "")
}

func openWorkspace(ctx context.Context, options launch.Options) (launch.Info, error) {
	defer launch.Phase("open-workspace")()
	info, err := launch.Open(ctx, options)
	if err == nil {
		err = connection.EnsureProfiles(ctx, options.Workspace)
	}
	if err == nil {
		err = connection.EnsureFileRoots(ctx, options.Workspace)
	}
	if err == nil {
		err = launch.RegisterModule(ctx, options.Workspace, "burrow@0.1.0", connection.Manifest)
	}
	return info, err
}

func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, "Burrow: "+safe(e.Error()))
		os.Exit(1)
	}
}
