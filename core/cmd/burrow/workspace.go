package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Bochner/burrow/core/connection"
	"github.com/Bochner/burrow/core/launch"
)

type commandError struct {
	Operation string                    `json:"operation"`
	Workspace string                    `json:"workspacePath,omitempty"`
	Code      string                    `json:"code"`
	Message   string                    `json:"message"`
	Review    *connection.ManagerReview `json:"review,omitempty"`
}

func (e *commandError) Error() string { return e.Message }

// Headless workspace operations share setup and resource owners with the TUI.
// Only open initializes. Discovery observes caller-supplied paths, never a scan.
func workspaceCommand(o launch.Options, args []string, selections int, loadPath string, demo bool) (failure error) {
	problem := &commandError{Operation: "workspace", Workspace: o.Workspace, Code: "invalid_arguments"}
	defer func() {
		if failure != nil {
			problem.Message = failure.Error()
			failure = problem
		}
	}()
	if len(args) == 0 {
		return fmt.Errorf("expected workspace open|inspect|list|restart|retire")
	}
	action := args[0]
	problem.Operation += "." + action
	if demo || (loadPath != "" && action != "open") {
		return fmt.Errorf("workspace commands do not support --demo; --load is only supported by workspace open")
	}
	interrupt, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(interrupt, 60*time.Second)
	defer cancel()
	if action == "list" {
		if selections != 0 || len(args) < 2 {
			problem.Code = "invalid_selection"
			return fmt.Errorf("expected workspace list PATH [PATH...], without --workspace; no global workspace registry is available")
		}
		items := []map[string]any{}
		for _, path := range args[1:] {
			item := map[string]any{"workspacePath": path, "state": "unverified"}
			info, err := launch.Status(ctx, path)
			if err != nil {
				item["error"] = err.Error()
			} else {
				item["state"], item["daemon"] = "verified", info
			}
			items = append(items, item)
		}
		return printResult(os.Stdout, map[string]any{"scope": "explicit-paths", "registryAvailable": false, "workspaces": items}, false)
	}
	if selections != 1 || o.Workspace == "" {
		problem.Code = "invalid_selection"
		return fmt.Errorf("select exactly one workspace with --workspace PATH before the command")
	}
	switch action {
	case "open", "inspect":
		if len(args) != 1 {
			return fmt.Errorf("workspace %s takes no arguments; select with --workspace PATH", action)
		}
		problem.Code = "unverified"
		var info launch.Info
		var err error
		if action == "open" {
			info, err = openWorkspace(ctx, o)
			if err == nil && loadPath != "" {
				_, err = connection.Execute(ctx, o.Workspace, []string{"profile", "load", loadPath})
			}
		} else {
			info, err = launch.Status(ctx, o.Workspace)
		}
		if err != nil {
			return err
		}
		return printResult(os.Stdout, info, false)
	case "restart", "retire":
		fs := flag.NewFlagSet(action, flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		yes := fs.Bool("yes", false, "approve the reviewed manager retirement")
		digest := fs.String("review", "", "exact manager review digest")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return fmt.Errorf("expected workspace %s [--yes --review HASH]", action)
		}
		problem.Code = "unverified"
		review, err := connection.ReviewManager(ctx, o.Workspace, action)
		if err != nil {
			return err
		}
		problem.Review = &review
		if !*yes {
			return printResult(os.Stdout, review, false)
		}
		if *digest == "" {
			problem.Code = "review_required"
			return fmt.Errorf("read workspace %s, then confirm with --yes --review HASH; resources retained", action)
		}
		if *digest != review.Digest {
			problem.Code = "review_changed"
			return fmt.Errorf("manager or action changed after review; review again; resources retained")
		}
		problem.Code = "cleanup_unconfirmed"
		if err := connection.RetireManager(ctx, o.Workspace, review); err != nil {
			return err
		}
		review.State = "retired"
		if action == "restart" {
			problem.Code = "registration_failed"
			if err := launch.RegisterModule(ctx, o.Workspace, "burrow@0.1.0", connection.Manifest); err != nil {
				return err
			}
			review.State = "ready"
		}
		return printResult(os.Stdout, review, false)
	default:
		return fmt.Errorf("expected workspace open|inspect|list|restart|retire")
	}
}
