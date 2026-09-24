package connection

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Bochner/burrow/core/launch"
)

// Activity is an observation, not another audit store. Source identities and
// fields remain available even when the terminal presents a concise summary.
type Activity struct {
	Version    int    `json:"version"`
	Workspace  string `json:"workspacePath"`
	Time       string `json:"time"`
	Observed   string `json:"observedAt"`
	Source     string `json:"source"`
	Kind       string `json:"kind"`
	Message    string `json:"message"`
	ID         string `json:"id,omitempty"`
	Operation  string `json:"operation,omitempty"`
	RunID      string `json:"runID,omitempty"`
	Chain      string `json:"chain,omitempty"`
	Resource   string `json:"resourceID,omitempty"`
	Connection string `json:"connectionID,omitempty"`
	Actor      string `json:"actor,omitempty"` // Controller label, not an authenticated upstream identity.
	Target     string `json:"target,omitempty"`
	State      string `json:"state,omitempty"`
	Sequence   uint64 `json:"sequence,omitempty"`
	Stream     string `json:"stream,omitempty"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"nextOffset"`
	Data       string `json:"data,omitempty"` // base64, exactly the observed bytes
	Details    any    `json:"details,omitempty"`
}

type publishedActivity struct {
	Seq              uint64
	Operation, Chain string
	Entry            struct {
		ID, Time, Topic, Kind, Level, Source, Message, ChainID, ChainName, RunID, Target, ModuleID string
		ElapsedSeconds                                                                             *float64
		Fields, Attributes                                                                         map[string]string
	}
}

type activityRun struct {
	offsets          [2]int64
	stored           [2]int64
	ended            [2]bool
	state, signature string
}

type activityFeed struct {
	workspace                                              string
	emit                                                   func(Activity) error
	sequence                                               uint64
	daemon                                                 launch.Info
	audit                                                  launch.AuditCursor
	problems                                               map[string]string
	runs                                                   map[string]*activityRun
	transfers                                              map[string]string
	connections                                            map[string]State
	shells                                                 map[string]Shell
	logsReady, runsReady, connectionsReady, transfersReady bool
	shellsReady                                            bool
}

func (f *activityFeed) write(e Activity) error {
	e.Version, e.Workspace, e.Observed = 1, f.workspace, time.Now().UTC().Format(time.RFC3339Nano)
	if e.Time == "" {
		e.Time = e.Observed
	}
	var err error
	e.Details, err = launch.RedactedEvidence(e.Details)
	if err != nil {
		return err
	}
	return f.emit(e)
}

func (f *activityFeed) problem(source string, err error) error {
	if err == nil {
		if f.problems[source] != "" {
			delete(f.problems, source)
			return f.write(Activity{Source: source, Kind: "resumed", State: "connected", Message: "Observation resumed; retained cursors continue, unavailable history is not reconstructed"})
		}
		return nil
	}
	if f.problems[source] == err.Error() {
		return nil
	}
	f.problems[source] = err.Error()
	return f.write(Activity{Source: source, Kind: "gap", State: "unverified", Message: err.Error()})
}

// FollowActivity reads only supported shared sources. Each invocation owns its
// cursors; cancelling the viewer never sends a cancellation to a resource.
func FollowActivity(ctx context.Context, workspace string, emit func(Activity) error) error {
	verify, cancel := context.WithTimeout(ctx, 3*time.Second)
	info, err := launch.Status(verify, workspace)
	cancel()
	if err != nil {
		return err
	}
	f := activityFeed{workspace: workspace, emit: emit, daemon: info, problems: map[string]string{}, runs: map[string]*activityRun{}, transfers: map[string]string{}, connections: map[string]State{}, shells: map[string]Shell{}}
	initial := true
	for {
		for _, read := range []func(context.Context) error{
			f.logs, f.notes, f.runOutput, f.connectionStates, f.shellStates, f.transferProgress,
		} {
			poll, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := read(poll)
			cancel()
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				return err
			} // Only a consumer/write error ends the feed.
		}
		if initial {
			if err := f.write(Activity{Source: "follower", Kind: "ready", State: "connected", Message: "Following new workspace activity; earlier history is not replayed. Shared shell control results are not command completion; keystrokes and terminal bytes are excluded. Controller labels are not authenticated actors; Hovel supplies no caller/request identity for retained controls.", Details: info}); err != nil {
				return err
			}
			initial = false
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (f *activityFeed) shellStates(ctx context.Context) error {
	refs, err := shellRefs(ctx, f.workspace)
	if err != nil {
		return f.problem("burrow/shells", err)
	}
	current := map[string]Shell{}
	for _, ref := range refs {
		if ref.ModuleID != "burrow@0.1.0" || ref.Kind != shellKind {
			continue
		}
		state, err := shellState(ctx, f.workspace, ref)
		if err != nil {
			return f.problem("burrow/shells", err)
		}
		previous, seen := f.shells[state.ID]
		if state.State == "unavailable" && seen {
			// Failed observation is not a new empty identity or reset counters.
			state = previous
			state.State, state.Detail = "unavailable", "owner unavailable; all other fields are last observed; remote outcome and cleanup unconfirmed"
		}
		current[state.ID] = state
		if seen && string(valueBytes(previous)) == string(valueBytes(state)) {
			continue
		}
		e := Activity{Source: "burrow/shell", Kind: "progress", Resource: state.ID, Connection: state.Connection.Creation, RunID: state.RunID, Target: state.Connection.Name, State: state.State, Message: "Observed retained shell state; SSH channel exit is not a structured command result", Details: state}
		if !f.shellsReady {
			e.Kind = "snapshot"
		}
		if state.State == "unavailable" {
			e.Kind, e.Message = "gap", "Shell owner observation unavailable; last identity retained, remote outcome and cleanup unconfirmed; no reconnect"
			e.Connection, e.RunID = previous.Connection.Creation, previous.RunID
		} else if seen && previous.State == "unavailable" {
			e.Kind, e.Message = "resumed", "Shell owner observation resumed; intermediate states may be missing"
		}
		if state.Dropped > previous.Dropped {
			gap := e
			gap.Kind, gap.Message = "gap", "Shell byte history was dropped; routine activity contains no terminal transcript; use session snapshot for a current display"
			if err := f.write(gap); err != nil {
				return err
			}
		}
		if state.AuditError != "" && state.AuditError != previous.AuditError {
			gap := e
			gap.Kind, gap.Message = "gap", "Shell operation evidence is incomplete: "+state.AuditError
			if err := f.write(gap); err != nil {
				return err
			}
		}
		if err := f.write(e); err != nil {
			return err
		}
	}
	for id, state := range f.shells {
		if _, present := current[id]; !present {
			if err := f.write(Activity{Source: "burrow/shell", Kind: "gap", Resource: id, Connection: state.Connection.Creation, RunID: state.RunID, Target: state.Connection.Name, State: "unavailable", Message: "Shell left the Hovel registry; consult owner operation evidence for confirmed closure; unread terminal history is unavailable"}); err != nil {
				return err
			}
		}
	}
	f.shells, f.shellsReady = current, true
	return f.problem("burrow/shells", nil)
}

func (f *activityFeed) connectionStates(ctx context.Context) error {
	states, err := List(ctx, f.workspace)
	if err != nil {
		return f.problem("burrow/connections", err)
	}
	if err := f.problem("burrow/connections", nil); err != nil {
		return err
	}
	current := map[string]State{}
	for _, state := range states {
		key := state.Creation
		if key == "" {
			key = "name:" + state.Name
		}
		current[key] = state
		if string(valueBytes(f.connections[key])) == string(valueBytes(state)) {
			continue
		}
		kind := "progress"
		if !f.connectionsReady {
			kind = "snapshot"
		}
		if err := f.write(Activity{Source: "burrow/connection", Kind: kind, Resource: state.Creation, Target: state.Name, State: state.State, Message: "Observed connection state", Details: state}); err != nil {
			return err
		}
	}
	for id, state := range f.connections {
		if _, present := current[id]; !present {
			if err := f.write(Activity{Source: "burrow/connection", Kind: "result", Resource: state.Creation, Target: state.Name, State: "unavailable", Message: "Connection left the owner inventory; operation evidence records any confirmed closure"}); err != nil {
				return err
			}
		}
	}
	f.connections, f.connectionsReady = current, true
	return nil
}

func (f *activityFeed) logs(ctx context.Context) error {
	info, err := launch.Status(ctx, f.workspace)
	if err != nil {
		return f.problem("hovel", err)
	}
	if info.PID != f.daemon.PID || info.Started != f.daemon.Started {
		f.sequence, f.daemon = 0, info
		if err := f.write(Activity{Source: "hovel", Kind: "gap", State: "unverified", Message: "Hovel daemon changed; live log history was reset. Reading the new retained tail."}); err != nil {
			return err
		}
	}
	var batch struct {
		Last uint64
		Logs []publishedActivity
	}
	if err := launch.Call(ctx, f.workspace, "PollLogs", map[string]any{"Since": f.sequence}, &batch); err != nil {
		return f.problem("hovel", err)
	}
	if err := f.problem("hovel", nil); err != nil {
		return err
	}
	if !f.logsReady {
		f.logsReady = true
		f.sequence = batch.Last
		return nil
	}
	if batch.Last < f.sequence {
		f.sequence = 0
		return f.write(Activity{Source: "hovel", Kind: "gap", State: "unverified", Message: "Hovel sequence reset; reading the available retained tail on the next poll"})
	}
	for _, item := range batch.Logs {
		if item.Seq <= f.sequence || item.Seq > batch.Last {
			return f.problem("hovel", fmt.Errorf("invalid live log sequence; cursor retained"))
		}
		if item.Seq > f.sequence+1 {
			if err := f.write(Activity{Source: "hovel", Kind: "gap", State: "unverified", Sequence: item.Seq, Message: fmt.Sprintf("%d Hovel log events are no longer available", item.Seq-f.sequence-1)}); err != nil {
				return err
			}
		}
		e := item.Entry
		if err := f.write(Activity{Source: "hovel/" + e.Source, Kind: "event", Time: e.Time, ID: e.ID, Operation: item.Operation, Chain: item.Chain, RunID: e.RunID, Resource: e.RunID, Target: e.Target, State: e.Level, Sequence: item.Seq, Message: e.Message, Details: e}); err != nil {
			return err
		}
		f.sequence = item.Seq
	}
	if batch.Last != f.sequence {
		return f.problem("hovel", fmt.Errorf("Hovel live log cursor changed without retained events; observation gap"))
	}
	return nil
}

func (f *activityFeed) notes(ctx context.Context) error {
	entries, gap, err := launch.ReadAudit(ctx, f.workspace, &f.audit)
	if err != nil {
		return f.problem("burrow/audit", err)
	}
	if err := f.problem("burrow/audit", nil); err != nil {
		return err
	}
	if gap != "" {
		if err := f.write(Activity{Source: "burrow/audit", Kind: "gap", State: "unverified", Message: gap}); err != nil {
			return err
		}
	}
	for _, entry := range entries {
		e := auditActivity(entry)
		if err := f.write(e); err != nil {
			return err
		}
	}
	return nil
}

func auditActivity(entry launch.AuditEntry) Activity {
	e := Activity{Source: "burrow/audit", Time: entry.Time, Kind: "result", Message: entry.Action, ID: entry.ID, Target: entry.Target, State: entry.State, NextOffset: entry.Offset, Details: entry.Result}
	var value map[string]json.RawMessage
	json.Unmarshal(entry.Result, &value)
	var failure string
	json.Unmarshal(value["error"], &failure)
	var result map[string]json.RawMessage
	if json.Unmarshal(value["result"], &result) == nil && result != nil {
		value = result
		if failure != "" && failure != "<nil>" {
			value["error"] = valueBytes(failure)
		}
		e.Details = json.RawMessage(valueBytes(value))
	}
	text := func(key string) string { var s string; json.Unmarshal(value[key], &s); return s }
	e.Resource = text("id")
	if e.Resource == "" {
		e.Resource = text("creation")
	}
	if args, err := Split(entry.Action); err == nil && len(args) > 2 && args[0] == "run" {
		switch args[1] {
		case "launch", "cancel", "collect", "close":
			if e.Resource == "" {
				e.Resource = args[2]
			}
		}
	}
	switch {
	case entry.State == "attempt":
		e.Kind = "submitted"
	case failure != "" && failure != "<nil>":
		e.Kind, e.State = "failed", "failed"
	case text("review") != "" || strings.HasPrefix(entry.Action, "review "):
		e.Kind = "review"
	case entry.State == "running":
		e.Kind = "started"
	case text("collection") == "succeeded":
		e.Kind = "collected"
	case strings.HasPrefix(entry.Target, "submitted request"):
		e.Kind = "returned" // Owner evidence is required before claiming execution.
	}
	if e.Kind != "submitted" && text("state") != "" {
		e.State = text("state")
	}
	if strings.HasPrefix(entry.Action, "session ") {
		e.RunID, e.Connection = text("runID"), text("connectionID")
		var state State
		if json.Unmarshal(value["connection"], &state) == nil && state.Creation != "" {
			e.Connection, e.Target = state.Creation, state.Name
		}
		if strings.HasPrefix(text("operation"), "session ") {
			e.Source, e.Operation, e.Actor, e.Target = "burrow/shell-control", text("operation"), text("actor"), text("connectionName")
			e.Message = e.Operation + ": provider operation result; input acceptance is not command completion"
			if e.Kind == "submitted" {
				e.Message = e.Operation + ": submitted to shell owner; execution not yet confirmed"
			}
		}
	}
	return e
}

func valueBytes(value any) []byte { data, _ := json.Marshal(value); return data }

func (f *activityFeed) runOutput(ctx context.Context) error {
	states, err := Runs(ctx, f.workspace)
	if err != nil {
		return f.problem("burrow/runs", err)
	}
	if err := f.problem("burrow/runs", nil); err != nil {
		return err
	}
	initial := !f.runsReady
	f.runsReady = true
	present := map[string]bool{}
	for _, r := range states {
		present[r.ID] = true
		view := f.runs[r.ID]
		if view == nil {
			view = &activityRun{}
			f.runs[r.ID] = view
			if initial {
				view.offsets = r.Stored
			}
		}
		kind := "progress"
		if initial {
			kind = "snapshot"
		} else if r.State != "running" && r.State != "prepared" {
			kind = "result"
		}
		signature := string(valueBytes(r))
		if view.signature != signature {
			if err := f.write(Activity{Source: "burrow/run", Kind: kind, Resource: r.ID, RunID: r.LaunchRunID, Target: r.Connection.Name, State: r.State, Message: quoteCommand(r.Command), Details: r}); err != nil {
				return err
			}
			view.signature = signature
		}
		view.state, view.stored = r.State, r.Stored
		for i, stream := range []string{"stdout", "stderr"} {
			// Bounded work per source keeps sibling runs and cancellation responsive.
			for page := 0; page < 8 && view.offsets[i] < r.Stored[i]; page++ {
				var chunk RunOutput
				offset := view.offsets[i]
				err := runControl(ctx, f.workspace, r.ID, "run-output", []string{stream, strconv.FormatInt(offset, 10)}, &chunk)
				if err != nil {
					if err := f.problem("burrow/output/"+r.ID, fmt.Errorf("run output unavailable; unread bytes may be lost: %w", err)); err != nil {
						return err
					}
					break
				}
				data, decode := base64.StdEncoding.DecodeString(chunk.Data)
				if decode != nil || len(data) == 0 || len(data) > 32768 || chunk.NextOffset != offset+int64(len(data)) {
					if err := f.problem("burrow/output/"+r.ID, fmt.Errorf("invalid output chunk; byte position retained")); err != nil {
						return err
					}
					break
				}
				if err := f.problem("burrow/output/"+r.ID, nil); err != nil {
					return err
				}
				if err := f.write(Activity{Source: "burrow/output", Kind: "output", Resource: r.ID, Target: r.Connection.Name, State: chunk.State, Stream: stream, Offset: offset, NextOffset: chunk.NextOffset, Data: chunk.Data, Message: "Live run output", Details: map[string]any{"storedBytes": chunk.Stored, "receivedBytes": chunk.Received, "outputComplete": chunk.OutputComplete, "outputError": chunk.OutputError}}); err != nil {
					return err
				}
				view.offsets[i] = chunk.NextOffset
			}
			if !view.ended[i] && r.State != "running" && r.State != "prepared" && view.offsets[i] >= r.Stored[i] {
				if err := f.write(Activity{Source: "burrow/output", Kind: "output-end", Resource: r.ID, Target: r.Connection.Name, State: r.State, Stream: stream, Offset: view.offsets[i], NextOffset: view.offsets[i], Message: "Captured stream ended", Details: map[string]any{"storedBytes": r.Stored[i], "receivedBytes": r.Bytes[i], "outputComplete": r.OutputComplete, "outputError": r.OutputError}}); err != nil {
					return err
				}
				view.ended[i] = true
			}
		}
	}
	for id, view := range f.runs {
		if !present[id] {
			if view.state == "running" || view.state == "prepared" || view.offsets != view.stored {
				if err := f.write(Activity{Source: "burrow/runs", Kind: "gap", Resource: id, State: "unavailable", Message: "Retained run disappeared; execution outcome and unread output may be unavailable"}); err != nil {
					return err
				}
			}
			delete(f.runs, id)
			delete(f.problems, "burrow/output/"+id)
		}
	}
	return nil
}

func (f *activityFeed) transferProgress(ctx context.Context) error {
	id, err := findManager(ctx, f.workspace)
	if err != nil {
		return f.problem("burrow/transfers", err)
	}
	var live struct {
		Records []Download
		Error   string
	}
	if id.Session != "" {
		err = managerControl(ctx, f.workspace, id, "downloads", nil, &live)
		if err == nil && live.Error != "" {
			err = fmt.Errorf("%s", live.Error)
		}
	}
	if err != nil {
		return f.problem("burrow/transfers", err)
	}
	if err := f.problem("burrow/transfers", nil); err != nil {
		return err
	}
	current := map[string]string{}
	for _, d := range live.Records {
		signature := fmt.Sprintf("%s:%d:%d:%d:%d:%s", d.State, d.Bytes, d.CompletedFiles, d.FailedFiles, d.CancelledFiles, d.Detail)
		current[d.ID] = signature
		if f.transfers[d.ID] == signature {
			continue
		}
		kind := "progress"
		if d.State != "running" {
			kind = "result"
		}
		if !f.transfersReady {
			kind = "snapshot"
		}
		if err := f.write(Activity{Source: "burrow/transfer", Kind: kind, Resource: d.ID, Target: d.Plan.Owner.Name, State: d.State, RunID: d.RunID, Message: d.Plan.Operation + " " + d.Plan.Pattern, Details: d}); err != nil {
			return err
		}
	}
	for id, signature := range f.transfers {
		if _, present := current[id]; !present && strings.HasPrefix(signature, "running:") {
			if err := f.write(Activity{Source: "burrow/transfers", Kind: "gap", Resource: id, State: "unavailable", Message: "Transfer owner disappeared; inspect retained operation evidence for its outcome"}); err != nil {
				return err
			}
		}
	}
	f.transfers, f.transfersReady = current, true
	return nil
}
