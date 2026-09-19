package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Bochner/burrow/core/connection"
	"github.com/Bochner/burrow/core/launch"
)

// This is a projection of Hovel's public artifact inventory, not a second run
// registry. Working spools are never scanned or adopted by the viewer.
type collectedArtifact struct {
	RunID     string `json:"runId"`
	ModuleID  string `json:"moduleId"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"createdAt"`
}

func writeCollectedNotes(ctx context.Context, workspace string, dst io.Writer) (err error) {
	out := bufio.NewWriter(dst)
	defer func() { err = errors.Join(err, out.Flush()) }()
	fmt.Fprintf(out, "Collected output\nWorkspace: %s\nSaved command results · newest collection first · reopen to refresh\n\n", safe(workspace))
	data, err := launch.HovelCLI(ctx, workspace, "--", "artifact", "list", "--json")
	if err != nil {
		return err
	}
	var inventory []collectedArtifact
	if err = json.Unmarshal(data, &inventory); err != nil {
		return fmt.Errorf("invalid Hovel artifact inventory: %w", err)
	}
	byName := make(map[string]collectedArtifact)
	var results []collectedArtifact
	for _, a := range inventory {
		if a.ModuleID != "burrow@0.1.0" || !strings.HasPrefix(a.Name, "run-") {
			continue
		}
		byName[a.RunID+"\x00"+a.Name] = a
		if strings.HasSuffix(a.Name, "-result") {
			results = append(results, a)
		}
	}
	slices.SortStableFunc(results, func(a, b collectedArtifact) int {
		ta, _ := time.Parse(time.RFC3339Nano, a.CreatedAt)
		tb, _ := time.Parse(time.RFC3339Nano, b.CreatedAt)
		return tb.Compare(ta)
	})
	if len(results) == 0 {
		fmt.Fprintln(out, "No collected command output in this workspace.")
	}
	for _, a := range results {
		if err := ctx.Err(); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s -- collected command\n", safe(a.CreatedAt))
		data, truncated, err := readCollectedArtifact(workspace, a, 1<<20)
		var run connection.Run
		if err == nil && (truncated || json.Unmarshal(data, &run) != nil || a.Name != "run-"+run.ID+"-result" || (len(run.Command) == 0 && run.Input.Script.Path == "")) {
			err = fmt.Errorf("invalid or oversized collected run result")
		}
		if err != nil {
			fmt.Fprintf(out, "  UNAVAILABLE: %s\n  File: %s\n\n", safe(err.Error()), safe(filepath.Join(workspace, a.Path)))
			continue
		}
		outcome := "UNKNOWN · " + safe(run.State)
		if run.RemoteExit != nil {
			outcome = fmt.Sprintf("FAILED (exit %d)", *run.RemoteExit)
			if *run.RemoteExit == 0 {
				outcome = "SUCCEEDED (exit 0)"
			}
		} else if strings.HasPrefix(run.State, "cancelled") {
			outcome = "CANCELLED · " + safe(run.State)
		} else if run.State == "timed-out" {
			outcome = "TIMED OUT · ordinary group terminated"
		} else if run.State == "staging-failed" {
			outcome = "FAILED · staging; script not launched"
		}
		capture := "INCOMPLETE"
		if run.OutputComplete {
			capture = "COMPLETE"
		}
		if run.Input.Script.Path != "" {
			fmt.Fprintf(out, "  Script: %s\n  Mode: %s\n  Interpreter: %s\n  Arguments: %s\n", safe(run.Input.Script.Path), safe(run.Input.Mode), safe(run.Input.Interpreter), safe(connection.CommandLine(run.Command)))
		} else {
			fmt.Fprintf(out, "  Command: %s\n", safe(connection.CommandLine(run.Command)))
		}
		fmt.Fprintf(out, "  Target: %s (%s@%s:%d)\n  Outcome: %s\n  Capture: %s\n  Run: %s\n  Collection: %s\n",
			safe(run.Connection.Name), safe(run.Connection.User), safe(run.Connection.Host), run.Connection.Port,
			outcome, capture, safe(run.ID), safe(a.RunID))
		for _, detail := range []struct{ label, value string }{
			{"Cancellation", run.Cancellation}, {"Capture error", run.OutputError}, {"Audit error", run.AuditError}, {"Cleanup error", run.CleanupError},
			{"Program stdin", run.Input.Stdin.Path}, {"Staged script", run.Input.StagePath}, {"Staging", run.Staging}, {"Stage cleanup", run.StageCleanup}, {"Timeout", run.Input.Timeout},
		} {
			if detail.value != "" {
				fmt.Fprintf(out, "  %s: %s\n", detail.label, safe(detail.value))
			}
		}
		for i, name := range []string{"stdout", "stderr"} {
			fmt.Fprintf(out, "\n%s · %d stored / %d received bytes\n", strings.ToUpper(name), run.Stored[i], run.Bytes[i])
			stream, ok := byName[a.RunID+"\x00run-"+run.ID+"-"+name]
			if !ok {
				fmt.Fprintln(out, "  UNAVAILABLE: stream is not registered in this collection")
				continue
			}
			fmt.Fprintf(out, "  File: %s\n", safe(filepath.Join(workspace, stream.Path)))
			// ponytail: a 64 KiB preview per stream keeps large captures out of
			// Vim; add paged artifact reading if full in-app browsing is needed.
			data, truncated, err := readCollectedArtifact(workspace, stream, 64<<10)
			if err != nil {
				fmt.Fprintf(out, "  UNAVAILABLE: %s\n", safe(err.Error()))
				continue
			}
			if len(data) == 0 {
				fmt.Fprintln(out, "    (empty)")
			} else {
				for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
					fmt.Fprintf(out, "    %s\n", safe(line))
				}
			}
			if truncated {
				fmt.Fprintf(out, "  PREVIEW TRUNCATED: first %d of %d saved bytes; full output is in the file above.\n", len(data), stream.Size)
			}
		}
		fmt.Fprintln(out, "\n----------------------------------------\n")
	}
	return nil
}

func readCollectedArtifact(workspace string, a collectedArtifact, limit int64) ([]byte, bool, error) {
	if !filepath.IsLocal(a.Path) || filepath.Clean(a.Path) != a.Path || !strings.HasPrefix(a.Path, "artifacts/") || a.Size < 0 {
		return nil, false, fmt.Errorf("invalid artifact path or size")
	}
	root, err := launch.FileRoot(filepath.Join(workspace, filepath.Dir(a.Path)), false)
	if err != nil {
		return nil, false, err
	}
	defer root.Close()
	f, err := root.OpenFile(filepath.Base(a.Path), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || stat.Uid != uint32(os.Getuid()) || stat.Nlink != 1 || info.Size() != a.Size {
		return nil, false, fmt.Errorf("artifact is not an unchanged private regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(data)) != min(a.Size, limit+1) {
		err = fmt.Errorf("artifact changed while reading")
	}
	return data[:min(int64(len(data)), limit)], int64(len(data)) > limit, err
}
