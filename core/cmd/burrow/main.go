package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/Bochner/burrow/core/launch"
	"github.com/charmbracelet/x/term"
	"github.com/vibepwners/hovel/sdk/go/hovel"
)

const usage = `Burrow — verified Hovel workspace
Usage: burrow --workspace /absolute/workspace [options] [status|tui]

Required:
  --workspace PATH      Explicit canonical Hovel workspace
Options:
  --hovel-package FILE  Local copy of the pinned Linux amd64 wheel
  --offline             Use the verified cache only
  --no-color            Disable terminal colors (also respects NO_COLOR)
  --help                Show this help without starting anything

status opens/reuses the workspace and prints verified daemon identity as JSON.
tui opens the management interface (default); quit retains the daemon.
Inside the interface: status revalidates without restarting; help; quit.
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
	if len(args) == 1 && args[0] == "module" {
		hovel.Serve(workspaceModule{})
		return nil
	}
	fs := flag.NewFlagSet("burrow", flag.ContinueOnError)
	var o launch.Options
	var noColor bool
	fs.StringVar(&o.Workspace, "workspace", "", "explicit canonical workspace (required)")
	fs.StringVar(&o.Package, "hovel-package", "", "pinned wheel file")
	fs.BoolVar(&o.Offline, "offline", false, "verified cache only")
	fs.BoolVar(&noColor, "no-color", false, "disable colors")
	fs.Usage = func() { fmt.Fprint(fs.Output(), usage) }
	if e := fs.Parse(args); e != nil {
		if e == flag.ErrHelp {
			return nil
		}
		return e
	}
	if o.Workspace == "" {
		return fmt.Errorf("--workspace PATH is required; use --help")
	}
	command := "tui"
	if fs.NArg() > 0 {
		command = fs.Arg(0)
	}
	if fs.NArg() > 1 || (command != "status" && command != "tui") {
		return fmt.Errorf("expected status or tui; use --help")
	}
	if command == "tui" && (!term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd())) {
		return fmt.Errorf("tui requires terminal input/output; use status for JSON")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if command == "tui" {
		fmt.Fprintln(os.Stderr, "Verifying Hovel package and workspace…")
	}
	info, e := launch.Open(ctx, o)
	if e != nil {
		return e
	}
	if command == "status" {
		return json.NewEncoder(os.Stdout).Encode(info)
	}
	return terminal(info, noColor || os.Getenv("NO_COLOR") != "")
}

// Read-only inspection through the public SDK. It cannot bootstrap another
// daemon from inside Hovel, and shares the frontend's verification command.
type workspaceModule struct{}

func (workspaceModule) Info() hovel.Info {
	return hovel.Info{Name: "burrow", Version: "0.1.0", Type: hovel.TypeSurvey, Summary: "Inspect a verified Burrow workspace"}
}
func (workspaceModule) Schema() hovel.Schema {
	return hovel.Schema{ChainConfig: []hovel.Requirement{hovel.Req("workspace", "string", "Explicit canonical Burrow workspace")}}
}
func (workspaceModule) Run(ctx *hovel.Context) (hovel.Result, error) {
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, e := launch.Status(c, ctx.InputString("workspace", ""))
	if e != nil {
		return hovel.Result{}, e
	}
	ctx.Log.Info("verified Burrow workspace identity")
	return hovel.Ok(map[string]any{"workspacePath": info.Workspace, "pid": info.PID}, hovel.WithSummary("Verified Burrow workspace")), nil
}
func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, "Burrow: "+safe(e.Error()))
		os.Exit(1)
	}
}
