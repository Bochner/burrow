package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/Bochner/burrow/core/connection"
)

// Notes are a presentation of the durable records, never another history or
// operational-state store. Internal discovery and per-file checkpoints stay in
// the backend log; operators see the final operation summary once.
func writeLogNotes(source io.Reader, destination io.Writer, workspace string) error {
	if _, err := fmt.Fprintf(destination, "Workspace: %s\nOperator notes · detailed records: burrow-logs/operations.log\n\n", safe(workspace)); err != nil {
		return err
	}
	reader := bufio.NewReader(source)
	var header, target, status string
	var payload strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF && line == "" {
			if header != "" {
				return fmt.Errorf("incomplete log snapshot")
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("incomplete log snapshot: %w", err)
		}
		switch {
		case line == "End record\n":
			stamp, action, ok := strings.Cut(strings.TrimSpace(header), " -- ")
			if !ok {
				return fmt.Errorf("invalid operation log header")
			}
			if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
				return fmt.Errorf("invalid operation timestamp")
			}
			note, title, err := operationNote(action, status, target, []byte(payload.String()))
			if err != nil {
				return err
			}
			if note != "" {
				if _, err := fmt.Fprintf(destination, "%s -- %s\n%s\n\n", stamp, safe(title), note); err != nil {
					return err
				}
			}
			header, target, status = "", "", ""
			payload.Reset()
		case strings.HasPrefix(line, "    "):
			payload.WriteString(strings.TrimPrefix(line, "    "))
		case strings.HasPrefix(line, "  Target: "):
			target = strings.TrimSpace(strings.TrimPrefix(line, "  Target: "))
		case strings.HasPrefix(line, "  Status: "):
			status = strings.TrimSpace(strings.TrimPrefix(line, "  Status: "))
		case !strings.HasPrefix(line, " ") && strings.TrimSpace(line) != "":
			header = line
		}
	}
}

func operationNote(action, status, target string, payload []byte) (note, title string, err error) {
	title = action
	if status == "attempt" || status == "running" || strings.Contains(status, "returned;") || strings.HasPrefix(action, "inspect reverse listeners") || action == "shell" || action == "download-cancel" {
		return "", title, nil
	}
	var fields map[string]json.RawMessage
	if string(payload) != "null\n" && json.Unmarshal(payload, &fields) != nil {
		return "", title, fmt.Errorf("invalid operation result in log")
	}
	// Individual file checkpoints do not belong in the notes. Keep final batch
	// outcomes, including mixed failures and cancellation, below.
	if strings.HasPrefix(action, "get file ") || strings.HasPrefix(action, "mget file ") {
		return "", title, nil
	}
	if fields["plan"] != nil && fields["files"] != nil {
		var d connection.Download
		if err := json.Unmarshal(payload, &d); err != nil {
			return "", title, err
		}
		if d.State == "running" {
			return "", title, nil
		}
		title = d.Plan.Operation + " " + d.Plan.Pattern
		complete, failed, cancelled, partial := 0, 0, 0, 0
		for _, f := range d.Files {
			if f.Partial != "" {
				partial++
			}
			switch f.State {
			case "complete":
				complete++
			case "cancelled":
				cancelled++
			default:
				failed++
			}
		}
		saved := d.Plan.Root
		if len(d.Files) == 1 {
			saved = d.Files[0].Destination
		} else if len(d.Files) > 1 {
			saved = filepath.Dir(d.Files[0].Destination)
		}
		location := "Saved"
		if d.State != "complete" {
			location = "Destination"
		}
		note = fmt.Sprintf("  Target: %s\n  %s · %d/%d files · %s · %s elapsed\n  Files: %d completed · %d failed · %d cancelled\n  %s: %s",
			safe(d.Plan.Owner.Name+" ("+d.Plan.Owner.User+"@"+d.Plan.Owner.Host+")"), strings.ToUpper(d.State), complete, len(d.Files), downloadSize(d.Bytes), (time.Duration(d.Elapsed * float64(time.Second))).Round(time.Millisecond), complete, failed, cancelled, location, safe(saved))
		if d.Detail != "" {
			note += "\n  Note: " + safe(d.Detail)
		}
		if len(d.Files) == 1 {
			f := d.Files[0]
			if f.Detail != "" && f.Detail != d.Detail {
				note += "\n  Note: " + safe(f.Detail)
			}
			if f.Partial != "" {
				note += "\n  Partial: " + safe(f.Partial)
			}
		} else if partial > 0 {
			note += fmt.Sprintf("\n  Partial files: %d · see downloads for paths", partial)
		}
		return note, title, nil
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return "", title, err
	}
	if envelope.Error != "" && envelope.Error != "<nil>" {
		return "  Target: " + safe(target) + "\n  FAILED / REFUSED · " + safe(envelope.Error), title, nil
	}
	if strings.HasPrefix(action, "review ") || strings.HasPrefix(target, "submitted request") {
		return "", title, nil
	}
	if envelope.Result != nil && string(envelope.Result) != "null" {
		payload = envelope.Result
		if json.Unmarshal(payload, &fields) != nil {
			return "  " + strings.ToUpper(status), title, nil
		}
	}
	text := func(key string) string { var s string; json.Unmarshal(fields[key], &s); return safe(s) }
	if fields["masterPID"] != nil {
		var s connection.State
		if err := json.Unmarshal(payload, &s); err != nil {
			return "", title, err
		}
		if s.State == "connecting" || (status == "ended" && s.State == "connected") {
			return "", title, nil
		}
		verb := "connect"
		if strings.HasPrefix(action, "close ") {
			verb = "close"
		} else if s.State == "lost" {
			verb = "connection lost"
		}
		title = verb + " " + s.Name
		note = fmt.Sprintf("  %s · %s@%s:%d", strings.ToUpper(s.State), safe(s.User), safe(s.Host), s.Port)
		if s.Detail != "" && s.Detail != "shell-free master" {
			note += "\n  Note: " + safe(s.Detail)
		}
		return note, title, nil
	}
	if fields["direction"] != nil {
		var t connection.Tunnel
		if err := json.Unmarshal(payload, &t); err != nil {
			return "", title, err
		}
		title = "tunnel create " + t.Connection
		if t.Direction == "D" {
			title = "proxy create " + t.Connection
		}
		note = "  " + strings.ToUpper(t.State) + " · " + safe(t.Listen)
		if t.Destination != "" {
			note += " → " + safe(t.Destination)
		}
		return note, title, nil
	}
	if action == "unforward" {
		return "  REMOVED · connection retained", "tunnel remove " + text("id"), nil
	}
	if strings.HasPrefix(action, "scp ") {
		if strings.Contains(action, " complete ") || text("notice") == "Discovery already running" {
			return "", title, nil
		}
		var entries []json.RawMessage
		json.Unmarshal(fields["entries"], &entries)
		outcome := "COMPLETED"
		if string(fields["incomplete"]) == "true" {
			outcome = "INCOMPLETE"
		}
		note = fmt.Sprintf("  %s · %d entries · %s", outcome, len(entries), text("path"))
		var failures []string
		json.Unmarshal(fields["errors"], &failures)
		if len(failures) > 0 {
			note += fmt.Sprintf("\n  Errors: %d · %s", len(failures), safe(failures[0]))
		}
		if text("notice") != "" {
			note += "\n  Note: " + text("notice")
		}
		return note, title, nil
	}
	if strings.HasPrefix(action, "shell ") {
		if status != "opened" && status != "ended" {
			return "", title, nil
		}
		return "  " + strings.ToUpper(status) + " · connection retained", title, nil
	}
	if strings.HasPrefix(action, "shell-close ") {
		return "  CLOSED · connection retained", title, nil
	}
	if action == "shell" || action == "download-cancel" {
		return "", title, nil
	}
	note = "  " + strings.ToUpper(status)
	if text("state") != "" {
		note = "  " + strings.ToUpper(text("state"))
	}
	if text("detail") != "" {
		note += " · " + text("detail")
	}
	return note, title, nil
}
