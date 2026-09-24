package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/Bochner/burrow/core/connection"
	"github.com/charmbracelet/x/term"
)

func activityCommand(workspace string, args []string, noColor bool) error {
	fs := flag.NewFlagSet("follow", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	structured := fs.Bool("json", false, "emit newline-delimited JSON events")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			m := ui{noColor: noColor || os.Getenv("NO_COLOR") != "" || !term.IsTerminal(os.Stdout.Fd())}
			help := m.paint(heading, "Usage:") + " " + m.syntax("burrow --workspace PATH", true) + " " + m.paint(heading, "follow") + " " + m.syntax("[--json]", true) + "\n" + m.syntax("Follow new workspace activity. Ctrl+C stops only the viewer. Pipes emit NDJSON; terminals use Burrow colors.\nShared shell lifecycle, controller changes and provider results are included. Input counts are not command results; keystrokes and terminal bytes are excluded.", false)
			_, err = lipgloss.Fprintln(os.Stdout, help)
			return err
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("expected follow [--json]")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	encoder := json.NewEncoder(os.Stdout)
	plain := noColor || os.Getenv("NO_COLOR") != "" || !term.IsTerminal(os.Stdout.Fd())
	pending := map[string][]byte{}
	// The command owns this goroutine until process exit. A blocked stdout must
	// not prevent Ctrl+C from ending this standalone viewer.
	done := make(chan error, 1)
	go func() {
		done <- connection.FollowActivity(ctx, workspace, func(e connection.Activity) error {
			if *structured || !term.IsTerminal(os.Stdout.Fd()) {
				return encoder.Encode(e)
			}
			text := activityText(e, plain)
			if e.Kind == "output" || e.Kind == "output-end" {
				data, err := base64.StdEncoding.DecodeString(e.Data)
				if err != nil {
					return err
				}
				key := e.Resource + "/" + e.Stream
				data = append(pending[key], data...)
				end := 0
				for end < len(data) && utf8.FullRune(data[end:]) {
					_, n := utf8.DecodeRune(data[end:])
					end += n
				}
				var status struct {
					Stored int64 `json:"storedBytes"`
				}
				encoded, _ := json.Marshal(e.Details)
				json.Unmarshal(encoded, &status)
				if e.State != "running" && e.State != "prepared" && e.NextOffset >= status.Stored {
					end = len(data)
				}
				if end < len(data) {
					pending[key] = append([]byte(nil), data[end:]...)
				} else {
					delete(pending, key)
				}
				m := ui{noColor: plain}
				if end > 0 {
					for _, line := range strings.Split(strings.TrimSuffix(safeRunOutput(data[:end], true), "\n"), "\n") {
						text += "\n  " + m.paint(accent, safe(e.Resource)) + " " + m.paint(heading, e.Stream) + m.paint(secondary, " │ ") + m.paint(pageStyle, line)
					}
				}
			}
			_, err := lipgloss.Fprintln(os.Stdout, text)
			return err
		})
	}()
	select {
	case <-ctx.Done():
		return nil
	case err := <-done:
		return err
	}
}

func activityText(e connection.Activity, noColor bool) string {
	m := ui{noColor: noColor}
	if e.Kind == "ready" {
		return m.paint(heading, "BURROW FOLLOW") + "  " + m.paint(secondary, safe(e.Workspace)) + "  " + m.paint(successStyle, "LIVE") + "\n" + m.paint(secondary, safe(e.Message)) + "\n"
	}
	stamp := e.Time
	if t, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
		stamp = t.Local().Format("15:04:05")
	}
	style := connectionStyle(e.State)
	if e.State == "error" || e.State == "fatal" {
		style = errorStyle
	}
	if e.State == "warn" || e.State == "warning" {
		style = warningStyle
	}
	if e.Kind == "submitted" || e.Kind == "review" || e.Kind == "returned" {
		style = warningStyle
	}
	if e.Kind == "gap" {
		style = errorStyle
	}
	var details map[string]any
	raw, _ := json.Marshal(e.Details)
	json.Unmarshal(raw, &details)
	label := strings.ToUpper(e.Kind)
	if e.State != "" {
		label += " · " + strings.ToUpper(e.State)
	}
	text := m.paint(secondary, safe(stamp)) + "  " + m.paint(accent, safe(e.Source)) + "  " + m.paint(style, safe(label)) + "  " + m.syntax(safe(e.Message), false)
	if e.Resource != "" {
		text += "\n  " + m.paint(secondary, "Resource: ") + m.paint(accent, safe(e.Resource))
	}
	if e.Target != "" {
		text += "  " + m.paint(secondary, "Target: ") + m.paint(accent, safe(e.Target))
	}
	for _, field := range []struct{ label, value string }{{"Connection", e.Connection}, {"Controller label", e.Actor}, {"Operation", e.Operation}, {"Chain", e.Chain}, {"Run", e.RunID}, {"Correlation", e.ID}} {
		if field.value != "" {
			text += "\n  " + m.paint(secondary, field.label+": ") + m.paint(accent, safe(field.value))
		}
	}
	for _, name := range []string{"Fields", "Attributes"} {
		if fields, ok := details[name].(map[string]any); ok {
			keys := make([]string, 0, len(fields))
			for key := range fields {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				text += "\n  " + m.paint(heading, safe(key)) + ": " + m.paint(fieldStyle(strings.ToUpper(key)), safe(fmt.Sprint(fields[key])))
			}
		}
	}
	if _, ok := details["budgetPerStream"]; ok {
		var r connection.Run
		if json.Unmarshal(raw, &r) == nil {
			exit, location := r.RemoteExit, "remote"
			if r.Execution == "local" {
				exit, location = r.LocalExit, "local"
			}
			if exit != nil {
				style := successStyle
				if *exit != 0 {
					style = errorStyle
				}
				text += "\n  " + m.paint(secondary, location+" exit: ") + m.paint(style, fmt.Sprint(*exit))
			}
			text += "\n  " + m.paint(secondary, "Capture: ") + m.paint(numberStyle, fmt.Sprintf("%d B stdout · %d B stderr", r.Stored[0], r.Stored[1]))
			text += "\n  " + m.paint(successStyle, safe(r.Connection.User)) + "@" + m.paint(hostStyle, safe(r.Connection.Host)) + ":" + m.paint(warningStyle, fmt.Sprint(r.Connection.Port))
			if r.OutputError != "" {
				text += "\n  " + m.paint(errorStyle, "INCOMPLETE: "+safe(r.OutputError))
			}
			if r.Cancellation != "" && r.Cancellation != "not-requested" {
				text += "\n  " + m.paint(warningStyle, safe(r.Cancellation))
			}
			for _, warning := range []string{r.AuditError, r.CleanupError} {
				if warning != "" {
					text += "\n  " + m.paint(errorStyle, safe(warning))
				}
			}
			if r.TimedOut {
				text += "\n  " + m.paint(warningStyle, "TIMED OUT")
			}
			if r.Collection != "" {
				text += "\n  " + m.paint(secondary, "Collection: ") + m.paint(connectionStyle(r.Collection), safe(r.Collection))
			}
		}
	} else if len(details) > 0 && !strings.HasPrefix(e.Source, "hovel/") && e.Source != "burrow/output" {
		// Preserve result fields with the same semantic JSON renderer as CLI/TUI.
		body, _ := json.MarshalIndent(e.Details, "", "  ")
		lines := strings.Split(string(body), "\n")
		for i, line := range lines {
			lines[i] = safe(line)
		}
		m.output = strings.Join(lines, "\n")
		text += "\n  " + strings.ReplaceAll(m.styledOutput(), "\n", "\n  ")
	}
	if errorText, ok := details["error"].(string); ok && errorText != "" && errorText != "<nil>" {
		text += "\n  " + m.paint(errorStyle, safe(errorText))
	}
	return text
}
