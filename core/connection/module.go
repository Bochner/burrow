package connection

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Bochner/burrow/core/launch"
	"github.com/vibepwners/hovel/sdk/go/hovel"
)

//go:embed hovel-module.yaml
var Manifest []byte

type Module struct{}

func (Module) Info() hovel.Info {
	return hovel.Info{Name: "burrow", Version: "0.1.0", Type: hovel.TypeSurvey, Tags: []string{"dangerous"}, Summary: "Manage Burrow workspace, saved profiles and retained SSH connections"}
}
func (Module) Schema() hovel.Schema {
	req := []hovel.Requirement{hovel.Req("workspace", "string", "Explicit canonical Burrow workspace"), {Key: "command", Type: "string", Description: "Saved-profile command; empty inspects workspace"}, {Key: "connection", Type: "string", Description: "Retired per-connection input; use the manager connect adapter"}}
	for _, key := range []string{"action", "generation", "session", "request", "review", "build"} {
		req = append(req, hovel.Requirement{Key: key, Type: "string"})
	}
	return hovel.Schema{ChainConfig: req}
}
func (Module) Run(ctx *hovel.Context) (hovel.Result, error) {
	if ctx.InputString("action", "") != "" {
		return runManager(ctx)
	}
	if ctx.InputString("connection", "") != "" {
		if ctx.InputString("command", "") != "" {
			return hovel.Result{}, fmt.Errorf("command and connection are mutually exclusive")
		}
		return hovel.Result{}, fmt.Errorf("legacy per-connection submission retired; use Burrow connect for a reviewed manager throw; existing owners remain inspectable and explicitly closeable")
	}
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	info, e := launch.Status(c, ctx.InputString("workspace", ""))
	if e != nil {
		return hovel.Result{}, e
	}
	if line := ctx.InputString("command", ""); line != "" {
		if os.Getppid() != info.PID {
			return hovel.Result{}, fmt.Errorf("commands must run in the verified workspace daemon")
		}
		args, err := Split(line)
		if err != nil {
			return hovel.Result{}, err
		}
		if len(args) == 0 || (args[0] != "downloads" && args[0] != "download-cancel" && args[0] != "profile" && args[0] != "profiles" && args[0] != "history" && args[0] != "scp" && args[0] != "local" && args[0] != "lcd" && args[0] != "lls" && args[0] != "files-history") || (args[0] == "profile" && len(args) > 1 && args[1] == "connect") {
			return hovel.Result{}, fmt.Errorf("expected saved-profile or file-browsing command; authenticate explicitly through the reviewed connect adapter")
		}
		result, err := Execute(c, info.Workspace, args)
		if err != nil {
			return hovel.Result{}, err
		}
		ctx.Log.Info("workspace command completed")
		if args[0] == "downloads" || args[0] == "download-cancel" || args[0] == "scp" || args[0] == "local" || args[0] == "lcd" || args[0] == "lls" || args[0] == "files-history" {
			encoded, err := json.Marshal(result)
			if err != nil {
				return hovel.Result{}, err
			}
			// Hovel's throw CLI exposes summary, not Result.Data. Keep the shared
			// structured result accessible to chain consumers without extra state.
			return hovel.Ok(map[string]any{"result": result}, hovel.WithSummary(string(encoded))), nil
		}
		return hovel.Ok(map[string]any{"result": result}), nil
	}
	ctx.Log.Info("verified Burrow workspace identity")
	return hovel.Ok(map[string]any{"workspacePath": info.Workspace, "pid": info.PID}, hovel.WithSummary(fmt.Sprintf("Verified Burrow workspace %q · daemon PID %d", info.Workspace, info.PID))), nil
}
