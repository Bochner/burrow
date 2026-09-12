package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

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
Inside the interface: status, connections, connect, inspect, reconnect, close, help, quit.
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

func run(args []string) error {
	if os.Getenv("BURROW_ASKPASS") == "1" {
		if len(args) != 1 || term.IsTerminal(os.Stdout.Fd()) {
			return fmt.Errorf("invalid authentication helper invocation")
		}
		return connection.Askpass(args[0])
	}
	if len(args) == 1 && args[0] == "connection-module" {
		hovel.Serve(connection.Module{})
		return nil
	}
	if len(args) == 1 && args[0] == "module" {
		hovel.Serve(workspaceModule{})
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
	if command != "status" && command != "tui" {
		args := fs.Args()
		wizard := len(args) == 1 && command == "connect"
		if !wizard {
			if e := connection.ValidateCommand(o.Workspace, args); e != nil {
				return e
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if _, e := launch.Status(ctx, o.Workspace); e != nil {
			return e
		}
		if loadPath != "" {
			if _, e := connection.Execute(ctx, o.Workspace, []string{"profile", "load", loadPath}); e != nil {
				return e
			}
		}
		var e error
		args, e = connection.ProfileConnect(ctx, o.Workspace, args)
		if e != nil {
			return e
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
				a := &authenticate{workspace: o.Workspace, args: args}
				if e := a.Run(); e != nil {
					return e
				}
				return json.NewEncoder(os.Stdout).Encode(a.result)
			}
		}
		result, e := connection.Execute(ctx, o.Workspace, args)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	if fs.NArg() > 1 {
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
	return terminal(newFrame(info, noColor || os.Getenv("NO_COLOR") != "", o), noColor || os.Getenv("NO_COLOR") != "")
}

//go:embed hovel-module.yaml
var workspaceManifest []byte

func openWorkspace(ctx context.Context, options launch.Options) (launch.Info, error) {
	info, err := launch.Open(ctx, options)
	if err == nil {
		err = connection.EnsureProfiles(ctx, options.Workspace)
	}
	if err == nil {
		err = launch.RegisterModule(ctx, options.Workspace, "burrow@0.1.0", workspaceManifest)
	}
	return info, err
}

// Read-only inspection through the public SDK. It cannot bootstrap another
// daemon from inside Hovel, and shares the frontend's verification command.
type workspaceModule struct{}

func (workspaceModule) Info() hovel.Info {
	return hovel.Info{Name: "burrow", Version: "0.1.0", Type: hovel.TypeSurvey, Summary: "Inspect a verified Burrow workspace"}
}
func (workspaceModule) Schema() hovel.Schema {
	return hovel.Schema{ChainConfig: []hovel.Requirement{hovel.Req("workspace", "string", "Explicit canonical Burrow workspace"), hovel.Requirement{Key: "command", Type: "string", Description: "Saved-profile command; empty inspects workspace"}}}
}
func (workspaceModule) Run(ctx *hovel.Context) (hovel.Result, error) {
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	info, e := launch.Status(c, ctx.InputString("workspace", ""))
	if e != nil {
		return hovel.Result{}, e
	}
	if line := ctx.InputString("command", ""); line != "" {
		if os.Getppid() != info.PID {
			return hovel.Result{}, fmt.Errorf("profile commands must run in the verified workspace daemon")
		}
		args, err := connection.Split(line)
		if err != nil {
			return hovel.Result{}, err
		}
		if len(args) == 0 || (args[0] != "profile" && args[0] != "profiles" && args[0] != "history") || (len(args) > 1 && args[1] == "connect") {
			return hovel.Result{}, fmt.Errorf("expected saved-profile management command; use the connection module for authentication")
		}
		result, err := connection.Execute(c, info.Workspace, args)
		if err != nil {
			return hovel.Result{}, err
		}
		ctx.Log.Info("saved-profile management completed")
		return hovel.Ok(map[string]any{"result": result}), nil
	}
	ctx.Log.Info("verified Burrow workspace identity")
	return hovel.Ok(map[string]any{"workspacePath": info.Workspace, "pid": info.PID}, hovel.WithSummary(fmt.Sprintf("Verified Burrow workspace %s · daemon PID %d", safe(info.Workspace), info.PID))), nil
}
func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, "Burrow: "+safe(e.Error()))
		os.Exit(1)
	}
}
