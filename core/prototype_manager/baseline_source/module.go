package connection

import (
	"context"
	_ "embed"
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
	return hovel.Schema{ChainConfig: []hovel.Requirement{hovel.Req("workspace", "string", "Explicit canonical Burrow workspace"), hovel.Requirement{Key: "command", Type: "string", Description: "Saved-profile command; empty inspects workspace"}, hovel.Requirement{Key: "connection", Type: "string", Description: "Non-secret connection JSON; mutually exclusive with command"}}}
}
func (Module) Run(ctx *hovel.Context) (hovel.Result, error) {
	if ctx.InputString("connection", "") != "" {
		if ctx.InputString("command", "") != "" {
			return hovel.Result{}, fmt.Errorf("command and connection are mutually exclusive")
		}
		return runConnection(ctx)
	}
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
		args, err := Split(line)
		if err != nil {
			return hovel.Result{}, err
		}
		if len(args) == 0 || (args[0] != "profile" && args[0] != "profiles" && args[0] != "history") || (len(args) > 1 && args[1] == "connect") {
			return hovel.Result{}, fmt.Errorf("expected saved-profile management command; set connection JSON on the burrow module for authentication")
		}
		result, err := Execute(c, info.Workspace, args)
		if err != nil {
			return hovel.Result{}, err
		}
		ctx.Log.Info("saved-profile management completed")
		return hovel.Ok(map[string]any{"result": result}), nil
	}
	ctx.Log.Info("verified Burrow workspace identity")
	return hovel.Ok(map[string]any{"workspacePath": info.Workspace, "pid": info.PID}, hovel.WithSummary(fmt.Sprintf("Verified Burrow workspace %q · daemon PID %d", info.Workspace, info.PID))), nil
}
