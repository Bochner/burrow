package connection

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Bochner/burrow/core/launch"
	"github.com/vibepwners/hovel/sdk/go/hovel"
)

const runKind = "burrow-run-v1"

// Run is the same retained observation for CLI, TUI and Hovel callers. Execution
// and capture are independent: a full disk must not erase a known remote exit.
type Run struct {
	ID             string   `json:"id"`
	RunID          string   `json:"runID"`
	LaunchRunID    string   `json:"launchRunID"`
	Connection     State    `json:"connection"`
	Command        []string `json:"command"`
	Input          RunInput `json:"input"`
	Staging        string   `json:"staging,omitempty"`
	StageCleanup   string   `json:"stageCleanup,omitempty"`
	TimedOut       bool     `json:"timedOut"`
	Budget         int64    `json:"budgetPerStream"`
	State          string   `json:"state"`
	RemoteExit     *int     `json:"remoteExit"`
	OutputComplete bool     `json:"outputComplete"`
	OutputError    string   `json:"outputError"`
	AuditError     string   `json:"auditError,omitempty"`
	CleanupError   string   `json:"cleanupError,omitempty"`
	Bytes          [2]int64 `json:"receivedBytes"`
	Stored         [2]int64 `json:"storedBytes"`
	Cancellation   string   `json:"cancellation"`
	CleanupScope   string   `json:"cleanupScope"`
	Collection     string   `json:"collection,omitempty"`
	OwnerPID       int      `json:"ownerPID"`
}

type runRequest struct {
	Connection State    `json:"connection"`
	Command    []string `json:"command"`
	Budget     int64    `json:"budgetPerStream"`
	Input      RunInput `json:"input"`
}

type remoteRun struct {
	control                   sync.Mutex
	mu                        sync.Mutex
	workspace                 string
	record                    Run
	dir                       string
	root                      *os.Root
	files                     [2]*os.File
	inputs                    [2]*os.File
	process                   *exec.Cmd
	done                      chan struct{}
	identityReady             chan struct{}
	closed                    bool
	pid, group                int
	start                     string
	stageDirectory, stageFile string
	stopRequested             bool
}

func parseRun(args []string) (runRequest, bool, string, error) {
	r := runRequest{Budget: 256 << 20}
	bad := fmt.Errorf("expected run prepare CONNECTION [--budget BYTES] -- COMMAND [ARG...], run now CONNECTION [--budget BYTES] [--yes] -- COMMAND [ARG...], run list, run inspect ID, run output ID stdout|stderr OFFSET, run launch ID [--collect] [--review HASH] [--yes], or run cancel|collect|close ID [--review HASH] [--yes]")
	if len(args) < 2 {
		return r, false, "", bad
	}
	if args[1] == "list" && len(args) == 2 {
		return r, false, "", nil
	}
	if len(args) < 3 || args[2] == "" || len(args[2]) > 128 || strings.ContainsAny(args[2], "\x00\r\n") {
		return r, false, "", bad
	}
	if args[1] == "prepare" || args[1] == "now" {
		r.Connection.Name = args[2]
		i := 3
		yes, budgetSet := false, false
		for i < len(args) && args[i] != "--" {
			switch {
			case args[i] == "--keep" && !r.Input.Keep:
				r.Input.Keep = true
				i++
			case slices.Contains([]string{"--script", "--mode", "--interpreter", "--stdin", "--timeout"}, args[i]) && i+1 < len(args):
				field := map[string]*string{"--script": &r.Input.Script.Path, "--mode": &r.Input.Mode, "--interpreter": &r.Input.Interpreter, "--stdin": &r.Input.Stdin.Path, "--timeout": &r.Input.Timeout}[args[i]]
				if *field != "" || args[i+1] == "" {
					return r, false, "", bad
				}
				*field = args[i+1]
				i += 2
			case args[i] == "--budget" && !budgetSet && i+1 < len(args):
				var err error
				r.Budget, err = strconv.ParseInt(args[i+1], 10, 64)
				if err != nil || r.Budget <= 0 {
					return r, false, "", fmt.Errorf("output budget must be a positive byte count per stream")
				}
				budgetSet = true
				i += 2
			case args[1] == "now" && args[i] == "--yes" && !yes:
				yes = true
				i++
			default:
				return r, false, "", bad
			}
		}
		if i >= len(args) || args[i] != "--" {
			return r, false, "", bad
		}
		r.Command = args[i+1:]
		if err := validateRunRequest(r); err != nil {
			return r, false, "", err
		}
		return r, yes, "", nil
	}
	if args[1] == "inspect" && len(args) == 3 {
		return r, false, "", nil
	}
	if args[1] == "output" && len(args) == 5 {
		offset, err := strconv.ParseInt(args[4], 10, 64)
		if err == nil && offset >= 0 && (args[3] == "stdout" || args[3] == "stderr") {
			return r, false, "", nil
		}
		return r, false, "", bad
	}
	if !slices.Contains([]string{"launch", "cancel", "collect", "close"}, args[1]) {
		return r, false, "", bad
	}
	yes, review, collect := false, "", false
	for i := 3; i < len(args); i++ {
		switch {
		case args[1] == "launch" && args[i] == "--collect" && !collect:
			collect = true
		case args[i] == "--yes" && !yes:
			yes = true
		case args[i] == "--review" && review == "" && i+1 < len(args):
			i++
			review = args[i]
		default:
			return r, false, "", bad
		}
	}
	return r, yes, review, nil
}

// RunWaits identifies commands that wait for remote completion and collection.
// Frontends keep these tied to their lifetime, without a default wait deadline.
func RunWaits(args []string) bool {
	return len(args) >= 3 && args[0] == "run" && (args[1] == "now" || (args[1] == "launch" && slices.Contains(args[3:], "--collect")))
}

func validateRunRequest(r runRequest) error {
	if err := r.Input.validate(); err != nil {
		return err
	}
	if r.Budget <= 0 || (r.Input.Script.Path == "" && (len(r.Command) == 0 || r.Command[0] == "" || strings.HasPrefix(r.Command[0], "-"))) {
		return fmt.Errorf("command and positive output budget required")
	}
	size := 0
	for _, a := range r.Command {
		size += len(a) + 1
		if strings.ContainsRune(a, 0) {
			return fmt.Errorf("NUL is not allowed in command arguments")
		}
	}
	if size > 64<<10 {
		return fmt.Errorf("command exceeds 64 KiB argument limit; no automatic staging")
	}
	return nil
}

func decodeRunRequest(raw string) (runRequest, error) {
	var r runRequest
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if len(raw) > 512<<10 || d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF {
		return r, fmt.Errorf("invalid retained command request")
	}
	return r, validateRunRequest(r)
}

func runSession(ctx context.Context, w, id string) error {
	var refs struct{ Sessions []hovel.SessionRef }
	if err := launch.Call(ctx, w, "ListSessions", map[string]any{}, &refs); err != nil {
		return err
	}
	for _, ref := range refs.Sessions {
		if ref.ID == id && ref.ModuleID == "burrow@0.1.0" && ref.Kind == runKind && ref.State != "closed" {
			return nil
		}
	}
	return fmt.Errorf("retained run unavailable in this workspace; no adoption or relaunch")
}

func runControl(ctx context.Context, w, id, command string, args []string, out any) error {
	if err := runSession(ctx, w, id); err != nil {
		return err
	}
	result, err := ownerCommand(ctx, w, id, command, args)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(result.Stdout), out)
}

func executeRun(ctx context.Context, w string, args []string) (any, error) {
	req, yes, review, err := parseRun(args)
	if err != nil {
		return nil, err
	}
	if args[1] == "list" {
		return Runs(ctx, w)
	}
	if args[1] == "prepare" || args[1] == "now" {
		state, err := selected(ctx, w, req.Connection.Name)
		if err != nil {
			return nil, err
		}
		if state.State != "connected" || state.Generation == "" {
			return nil, fmt.Errorf("prepare requires an explicitly selected live manager connection")
		}
		req.Connection = state
		raw, _ := json.Marshal(req)
		var result Run
		err = managerThrow(ctx, w, map[string]string{"action": "run-prepare", "request": string(raw), "review": digest(string(raw))}, &result)
		if err != nil || args[1] == "prepare" {
			return result, err
		}
		// Reuse inspect, immutable review and launch of this exact prepared run.
		// Confirmation must launch this ID, never prepare another command.
		next := []string{"run", "launch", result.ID, "--collect"}
		if yes {
			next = append(next, "--yes")
		}
		launched, err := executeRun(ctx, w, next)
		if err != nil {
			return result, err
		}
		if details, ok := launched.(map[string]string); ok {
			details["id"] = result.ID
			details["confirm"] = "run launch " + result.ID + " --collect --review " + details["digest"] + " --yes"
		}
		return launched, nil
	}
	var state Run
	if err := runControl(ctx, w, args[2], "run-inspect", nil, &state); err != nil {
		return nil, err
	}
	if args[1] == "inspect" {
		return state, nil
	}
	if args[1] == "output" {
		var out RunOutput
		err := runControl(ctx, w, args[2], "run-output", args[3:], &out)
		return out, err
	}
	// The immutable command, selected connection and budgets bind every approval.
	bound, _ := json.Marshal(state.request())
	hash := digest(args[1] + state.ID + string(bound))
	if RunWaits(args) {
		hash = digest("launch-collect" + state.ID + string(bound))
	}
	if !yes {
		text := fmt.Sprintf("Run: %s\nConnection: %s\nEndpoint: %s@%s:%d\nAction: %s\nCommand: %s\nOutput budget: %d bytes per stream.\nUncollected output can be lost on module/daemon failure. Commands/arguments must not contain secrets.", state.ID, state.Connection.Name, state.Connection.User, state.Connection.Host, state.Connection.Port, args[1], quoteCommand(state.Command), state.Budget)
		text += state.Input.review()
		if args[1] == "close" {
			text += "\nClose removes uncollected working output; registered Hovel artifacts remain."
		}
		if RunWaits(args) {
			text += "\nWait for completion, then collect stdout, stderr and the result as Hovel evidence for Ctrl+L Results. Stopping the wait does not cancel the remote command."
		}
		return map[string]string{"review": text, "digest": hash}, nil
	}
	if review != "" && review != hash {
		return nil, fmt.Errorf("run review changed; inspect and review again")
	}
	var result Run
	err = managerThrow(ctx, w, map[string]string{"action": "run-" + args[1], "session": state.ID, "review": digest(args[1] + state.ID + string(bound))}, &result)
	if err != nil {
		return state, err
	}
	if RunWaits(args) {
		// Waiting holds no dispatch lock: inspect/cancel and sibling commands
		// remain available through the same retained owner.
		for result.State == "running" {
			select {
			case <-ctx.Done():
				return result, fmt.Errorf("wait stopped; remote command was not cancelled: %w", ctx.Err())
			case <-time.After(250 * time.Millisecond):
			}
			if err := runControl(ctx, w, state.ID, "run-inspect", nil, &result); err != nil {
				return result, fmt.Errorf("completion unverified: %w", err)
			}
		}
		collected, err := executeRun(ctx, w, []string{"run", "collect", state.ID, "--yes"})
		if err != nil {
			return result, fmt.Errorf("collection failed: %w", err)
		}
		return collected, nil
	}
	if err == nil && args[1] == "collect" {
		result.Collection = "succeeded"
	}
	return result, err
}

func Runs(ctx context.Context, w string) ([]Run, error) {
	var refs struct{ Sessions []hovel.SessionRef }
	if err := launch.Call(ctx, w, "ListSessions", map[string]any{}, &refs); err != nil {
		return nil, err
	}
	out := []Run{}
	for _, ref := range refs.Sessions {
		if ref.ModuleID != "burrow@0.1.0" || ref.Kind != runKind || ref.State == "closed" {
			continue
		}
		var state Run
		result, err := ownerCommand(ctx, w, ref.ID, "run-inspect", nil)
		if err != nil {
			return nil, fmt.Errorf("run %s is unverified; uncollected output may be lost", ref.ID)
		}
		if json.Unmarshal([]byte(result.Stdout), &state) != nil || state.ID != ref.ID {
			return nil, fmt.Errorf("invalid retained run identity")
		}
		out = append(out, state)
	}
	slices.SortFunc(out, func(a, b Run) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

func quoteCommand(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = "'" + strings.ReplaceAll(a, "'", "'\\''") + "'"
	}
	return strings.Join(parts, " ")
}

func runAdapter(ctx *hovel.Context, w string) (hovel.Result, error) {
	c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	action := strings.TrimPrefix(ctx.InputString("action", ""), "run-")
	if action == "prepare" {
		raw := ctx.InputString("request", "")
		req, err := decodeRunRequest(raw)
		if err != nil || ctx.InputString("review", "") != digest(raw) {
			return hovel.Result{}, fmt.Errorf("invalid prepare request")
		}
		var live State
		err = managerControl(c, w, managerIdentity{Session: req.Connection.Session, Generation: req.Connection.Generation}, "shell", []string{req.Connection.Creation}, &live)
		if err != nil || live != req.Connection {
			return hovel.Result{}, fmt.Errorf("selected connection changed or unavailable")
		}
		dir, err := os.MkdirTemp(w, ".burrow-run-")
		if err != nil {
			return hovel.Result{}, err
		}
		root, err := launch.FileRoot(dir, false)
		if err != nil {
			return hovel.Result{}, err
		}
		r := &remoteRun{workspace: w, dir: dir, root: root, done: make(chan struct{}), identityReady: make(chan struct{}), record: Run{RunID: ctx.RunID, OwnerPID: os.Getpid(), Connection: req.Connection, Command: req.Command, Budget: req.Budget, Input: req.Input, State: "prepared", Cancellation: "not-requested", CleanupScope: "ordinary process group only; escaped descendants and post-loss recovery unconfirmed"}}
		for i, name := range []string{"stdout", "stderr"} {
			r.files[i], err = root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if err != nil {
				r.Close("prepare failed")
				return hovel.Result{}, err
			}
		}
		if err := r.prepareInputs(c); err != nil {
			return hovel.Result{}, errors.Join(err, r.Close("prepare failed"))
		}
		ref, err := ctx.OpenSession(r, hovel.WithName("Run on "+req.Connection.Name), hovel.WithKind(runKind), hovel.WithTransport("ssh"))
		if err != nil {
			r.Close("prepare failed")
			return hovel.Result{}, err
		}
		r.mu.Lock()
		r.record.ID = ref.ID
		state := r.record
		r.mu.Unlock()
		data, _ := json.Marshal(state)
		ctx.Log.Info("retained command prepared; no remote command launched")
		return hovel.Ok(nil, hovel.WithSummary(string(data))), nil
	}
	id := ctx.InputString("session", "")
	var state Run
	if err := runControl(c, w, id, "run-inspect", nil, &state); err != nil {
		return hovel.Result{}, err
	}
	bound, _ := json.Marshal(state.request())
	if ctx.InputString("review", "") != digest(action+state.ID+string(bound)) {
		return hovel.Result{}, fmt.Errorf("run approval does not match immutable request")
	}
	var artifacts []hovel.Artifact
	switch action {
	case "launch":
		if err := runControl(c, w, id, "run-launch", []string{ctx.RunID}, &state); err != nil {
			return hovel.Result{}, err
		}
	case "cancel":
		if err := runControl(c, w, id, "run-cancel", []string{ctx.RunID}, &state); err != nil {
			return hovel.Result{}, err
		}
	case "collect":
		result, err := ownerCommand(c, w, id, "run-collect", nil)
		if err != nil {
			return hovel.Result{}, err
		}
		if json.Unmarshal([]byte(result.Stdout), &state) != nil {
			return hovel.Result{}, fmt.Errorf("invalid collection result")
		}
		artifacts = append(artifacts, hovel.JSONArtifact("run-"+id+"-result", state))
		for _, name := range []string{"stdout", "stderr"} {
			path := result.Fields[name+"Path"]
			if filepath.Dir(filepath.Dir(path)) != w || !strings.HasPrefix(filepath.Base(filepath.Dir(path)), ".burrow-run-") || filepath.Base(path) != name {
				return hovel.Result{}, fmt.Errorf("invalid run output path")
			}
			root, err := launch.FileRoot(filepath.Dir(path), false)
			if err != nil {
				return hovel.Result{}, err
			}
			info, err := root.Lstat(name)
			root.Close()
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
				return hovel.Result{}, fmt.Errorf("unsafe run output file")
			}
			artifacts = append(artifacts, hovel.FileArtifact("run-"+id+"-"+name, "application/octet-stream", path))
		}
		state.Collection = "pending Hovel artifact persistence"
	case "close":
		// All supported collect/close commands use the existing HovelDispatch
		// workspace lock, held through artifact materialization. Do not remove
		// working files from a frontend or an asynchronous result callback.
		if state.State == "running" {
			return hovel.Result{}, fmt.Errorf("run active; cancel explicitly before closing")
		}
		var result any
		if err := launch.Call(c, w, "CloseSession", map[string]string{"SessionID": id}, &result); err != nil {
			return hovel.Result{}, err
		}
		state.State = "closed"
	default:
		return hovel.Result{}, fmt.Errorf("unsupported run action")
	}
	state.RunID = ctx.RunID
	data, _ := json.Marshal(state)
	return hovel.Ok(nil, hovel.WithSummary(string(data)), hovel.WithArtifacts(artifacts...)), nil
}

func (r *remoteRun) Open() error                        { return nil }
func (r *remoteRun) Read(time.Duration) ([]byte, error) { return nil, nil }
func (r *remoteRun) Write([]byte) error {
	return fmt.Errorf("use typed run controls; terminal input never launches a command")
}
func (r *remoteRun) Closed() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.closed }
func (r *remoteRun) ListPayloadCommands(hovel.PayloadCommandListRequest) ([]hovel.PayloadCommand, error) {
	return []hovel.PayloadCommand{{Name: "run-inspect", ReadOnly: true}, {Name: "run-output", ReadOnly: true}, {Name: "run-launch", Destructive: true}, {Name: "run-cancel", Destructive: true}, {Name: "run-collect", ReadOnly: true}}, nil
}
func (r *remoteRun) RunPayloadCommand(req hovel.PayloadCommandRequest) (hovel.PayloadCommandResult, error) {
	if req.InstalledPayloadID != "" || req.Reconnect != nil || req.InputPath != "" || req.InputData != "" || len(req.Config) != 0 {
		return hovel.PayloadCommandResult{}, fmt.Errorf("unsupported run inputs")
	}
	r.control.Lock()
	defer r.control.Unlock()
	if r.Closed() && req.Command != "run-inspect" {
		return hovel.PayloadCommandResult{}, fmt.Errorf("run closed")
	}
	var value any
	fields := map[string]string{}
	switch {
	case req.Command == "run-launch" && len(req.Args) == 1 && req.Args[0] != "":
		if err := r.launch(req.Args[0]); err != nil {
			return hovel.PayloadCommandResult{}, err
		}
	case req.Command == "run-cancel" && len(req.Args) == 1 && req.Args[0] != "":
		r.mu.Lock()
		before := r.record
		r.mu.Unlock()
		audit, beginErr := launch.BeginAudit(r.workspace, "run cancel", targetLabel(before.Connection), before)
		r.cancel()
		r.mu.Lock()
		err := errors.Join(beginErr, audit.Finish(r.record, nil))
		if err != nil {
			r.record.AuditError = err.Error()
		}
		r.mu.Unlock()
		if err != nil {
			return hovel.PayloadCommandResult{}, err
		}
	case req.Command == "run-inspect" && len(req.Args) == 0:
	case req.Command == "run-collect" && len(req.Args) == 0:
		r.mu.Lock()
		state := r.record
		r.mu.Unlock()
		if state.State == "running" || state.State == "prepared" {
			return hovel.PayloadCommandResult{}, fmt.Errorf("run has not finished; inspect or cancel first")
		}
		if err := r.verifyDirectory(); err != nil {
			return hovel.PayloadCommandResult{}, err
		}
		for i, name := range []string{"stdout", "stderr"} {
			path := filepath.Join(r.dir, name)
			live, err := r.root.Lstat(name)
			held, e := r.files[i].Stat()
			if err != nil || e != nil || !os.SameFile(live, held) {
				return hovel.PayloadCommandResult{}, fmt.Errorf("run output file replaced; collection refused")
			}
			fields[name+"Path"] = path
		}
	case req.Command == "run-output" && len(req.Args) == 2:
		out, err := r.output(req.Args)
		if err != nil {
			return hovel.PayloadCommandResult{}, err
		}
		value = out
	default:
		return hovel.PayloadCommandResult{}, fmt.Errorf("invalid run command")
	}
	if value == nil {
		r.mu.Lock()
		value = r.record
		r.mu.Unlock()
	}
	data, err := json.Marshal(value)
	return hovel.PayloadCommandResult{Command: req.Command, Stdout: string(data), Fields: fields}, err
}

func (r *remoteRun) launch(runID string) (failure error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.record.State != "prepared" {
		return nil
	}
	audit, err := launch.BeginAudit(r.workspace, "run launch", targetLabel(r.record.Connection), r.record)
	if err != nil {
		return err
	}
	defer func() {
		if failure != nil {
			logErr := audit.Record("failed", map[string]any{"result": r.record, "error": failure.Error()})
			if logErr != nil {
				r.record.AuditError = logErr.Error()
			}
			failure = errors.Join(failure, logErr)
		}
	}()
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := r.record.Connection
	var live State
	if err := managerControl(c, r.workspace, managerIdentity{Session: s.Session, Generation: s.Generation}, "shell", []string{s.Creation}, &live); err != nil {
		return err
	}
	if live.Socket != s.Socket || live.SocketInode != s.SocketInode || live.MasterPID != s.MasterPID {
		return fmt.Errorf("selected master changed; prepare a new run explicitly")
	}
	argv, input, err := r.invocation()
	if err != nil {
		return err
	}
	if r.record.Input.Mode == "stage" {
		r.record.LaunchRunID = runID
		if err := r.stage(); err != nil {
			r.record.State = "staging-failed"
			r.record.OutputComplete = true
			r.cleanupStage()
			close(r.done)
			return err
		}
	}
	marker := "burrow-start-" + rand.Text() + ":"
	// OpenSSH's non-PTY child is the process-group leader. No remote file or
	// installed supervisor is needed. The one prefix precedes executable output.
	script := `(if { IFS= read -r stat < /proc/$$/stat; } 2>/dev/null; then set -- ${stat##*) }; group=$3; shift 19; printf '` + marker + `%s:%s:%s\n' "$$" "$group" "$1"; else printf '` + marker + `0:0:0\n'; fi) >&2
exec ` + quoteCommand(argv)
	r.process = fileSSH(context.Background(), s, "unused", "exec /bin/sh -c "+quoteCommand([]string{script}))
	r.process.Stdin = input
	r.process.Stdout = runWriter{r, 0}
	r.process.Stderr = &runPrefix{r: r, marker: marker}
	r.record.State, r.record.LaunchRunID = "running", runID
	if err := r.process.Start(); err != nil {
		r.record.State = "transport-or-completion-unknown"
		r.cleanupStage()
		close(r.done)
		return err
	}
	go func() {
		err := r.process.Wait()
		prefix := r.process.Stderr.(*runPrefix)
		prefix.flush()
		r.mu.Lock()
		defer r.mu.Unlock()
		defer close(r.done)
		code := r.process.ProcessState.ExitCode()
		r.record.State = "transport-or-completion-unknown"
		if prefix.seen && code >= 0 && code != 255 {
			r.record.State = "exited"
			r.record.RemoteExit = &code
		}
		for _, f := range r.files {
			if e := f.Sync(); e != nil && r.record.OutputError == "" {
				r.record.OutputError = "output file sync failed"
			}
		}
		if err == exec.ErrWaitDelay && r.record.OutputError == "" {
			r.record.OutputError = "output drain incomplete after local SSH exit"
		}
		r.record.OutputComplete = r.record.OutputError == "" && r.record.State == "exited" && err != exec.ErrWaitDelay
		if !r.stopRequested {
			if r.record.State == "exited" || r.record.Input.Keep {
				r.cleanupStage()
			} else if r.record.Input.Mode == "stage" {
				r.record.StageCleanup = "unconfirmed; execution observation lost"
			}
		}
		if err := audit.Finish(r.record, nil); err != nil {
			r.record.AuditError = err.Error()
		}
	}()
	r.startTimeout()
	return nil
}

type runWriter struct {
	r      *remoteRun
	stream int
}

func (w runWriter) Write(p []byte) (int, error) {
	r := w.r
	r.mu.Lock()
	defer r.mu.Unlock()
	i := w.stream
	r.record.Bytes[i] += int64(len(p))
	remaining := r.record.Budget - r.record.Stored[i]
	keep := min(int64(len(p)), remaining)
	if keep > 0 {
		n, err := r.files[i].Write(p[:keep])
		r.record.Stored[i] += int64(n)
		if err != nil {
			r.record.OutputError = "output file write failed; partial evidence available"
		}
	}
	if keep < int64(len(p)) && r.record.OutputError == "" {
		r.record.OutputError = "output storage budget exceeded; partial evidence available"
	}
	return len(p), nil // Keep draining independently of storage or viewers.
}

type runPrefix struct {
	r            *remoteRun
	marker       string
	buffer       []byte
	parsed, seen bool
}

func (p *runPrefix) Write(data []byte) (int, error) {
	n := len(data)
	if p.parsed {
		return (runWriter{p.r, 1}).Write(data)
	}
	p.buffer = append(p.buffer, data...)
	end := bytes.IndexByte(p.buffer, '\n')
	if end < 0 && len(p.buffer) < 512 {
		return n, nil
	}
	p.parsed = true
	if end >= 0 && strings.HasPrefix(string(p.buffer[:end]), p.marker) {
		var pid, group int
		var start string
		if _, err := fmt.Sscanf(string(p.buffer[len(p.marker):end]), "%d:%d:%s", &pid, &group, &start); err == nil {
			p.seen = true
			p.r.mu.Lock()
			p.r.pid, p.r.group, p.r.start = pid, group, start
			p.r.mu.Unlock()
			p.buffer = p.buffer[end+1:]
		}
	}
	p.flush()
	close(p.r.identityReady)
	return n, nil
}
func (p *runPrefix) flush() {
	if len(p.buffer) > 0 {
		(runWriter{p.r, 1}).Write(p.buffer)
		p.buffer = nil
	}
}

type RunOutput struct {
	Data       string `json:"data"`
	NextOffset int64  `json:"nextOffset"`
}

func (r *remoteRun) output(args []string) (RunOutput, error) {
	i := slices.Index([]string{"stdout", "stderr"}, args[0])
	offset, err := strconv.ParseInt(args[1], 10, 64)
	if i < 0 || err != nil || offset < 0 {
		return RunOutput{}, fmt.Errorf("select stdout/stderr and nonnegative byte offset")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	data := make([]byte, 32768)
	n, err := r.files[i].ReadAt(data, offset)
	if err != nil && err != io.EOF {
		return RunOutput{}, err
	}
	return RunOutput{base64.StdEncoding.EncodeToString(data[:n]), offset + int64(n)}, nil
}

// The caller holds control, so launch/cancel/close cannot race one another.
// PID/starttime verification is not atomic with signalling; stronger containment
// and post-loss recovery remain outside the accepted ordinary-group contract.
func (r *remoteRun) cancel() {
	defer func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.record.State == "transport-or-completion-unknown" && r.record.Cancellation == "not-requested" {
			r.record.Cancellation = "unconfirmed; execution observation lost; no signal sent"
		}
		if r.record.Input.Mode == "stage" && r.record.StageCleanup == "pending" && r.record.State != "running" {
			if r.record.State == "exited" || r.record.Cancellation == "ordinary-group-terminated" || r.record.Input.Keep {
				r.cleanupStage()
			} else {
				r.record.StageCleanup = "unconfirmed; remote termination not verified"
			}
		}
	}()
	r.mu.Lock()
	if r.record.State == "prepared" {
		r.record.State = "cancelled-before-launch"
		r.record.Cancellation = "not-started"
		r.record.OutputComplete = true
		close(r.done)
		r.mu.Unlock()
		return
	}
	if r.record.State != "running" {
		r.mu.Unlock()
		return
	}
	r.stopRequested = true
	r.mu.Unlock()
	select {
	case <-r.identityReady:
	case <-r.done:
		return
	case <-time.After(time.Second):
	}
	r.mu.Lock()
	if r.record.State != "running" {
		r.mu.Unlock()
		return
	}
	pid, group, start, s := r.pid, r.group, r.start, r.record.Connection
	r.mu.Unlock()
	outcome := "unconfirmed; remote identity unavailable; no signal sent"
	_, validStart := strconv.ParseUint(start, 10, 64)
	if pid > 1 && group == pid && validStart == nil {
		ctx, stop := context.WithTimeout(context.Background(), 8*time.Second)
		defer stop()
		// Inventory verifies the existing master's identity without requiring a
		// new-work audit; cleanup must remain possible during log failure.
		live, err := selected(ctx, r.workspace, s.Name)
		if err == nil && live.State == "connected" && live.Session == s.Session && live.Generation == s.Generation && live.Creation == s.Creation && live.Socket == s.Socket && live.SocketInode == s.SocketInode && live.MasterPID == s.MasterPID {
			// Values come only from the launch prefix and are parsed numerically.
			script := fmt.Sprintf(`IFS= read -r stat < /proc/%d/stat || exit 3
set -- ${stat##*) }
[ "$3" = %d ] || exit 4
shift 19
[ "$1" = %s ] || exit 5
kill -TERM -%d || exit 6
attempt=0
while [ "$attempt" -lt 20 ]; do
 live=0
 for f in /proc/[0-9]*/stat; do
  { IFS= read -r stat < "$f"; } 2>/dev/null || continue
  set -- ${stat##*) }
  if [ "$3" = %d ] && [ "$1" != Z ] && [ "$1" != X ]; then live=1; fi
 done
 if [ "$live" = 0 ]; then printf 'ordinary-group-terminated'; exit 0; fi
 sleep 0.05
 attempt=$((attempt+1))
done
exit 7`, pid, pid, start, pid, pid)
			cmd := fileSSH(ctx, s, "unused", "exec /bin/sh -c "+quoteCommand([]string{script}))
			var out limitedFileOutput
			cmd.Stdout = &out
			err = cmd.Run()
			outcome = "unconfirmed; identity changed, signal failed or ordinary group still live"
			if err == nil && out.String() == "ordinary-group-terminated" {
				outcome = "ordinary-group-terminated"
			}
		} else {
			outcome = "unconfirmed; selected master unavailable; no signal sent"
		}
	}
	// Allow ordinary termination to drain the streams. Escaped descendants can
	// hold the pipes open; local client stop after this bound proves no cleanup.
	select {
	case <-r.done:
	case <-time.After(time.Second):
		r.process.Process.Kill()
		<-r.done
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.record.Cancellation = outcome
	if outcome == "ordinary-group-terminated" {
		r.record.State = "cancelled"
		if r.record.TimedOut {
			r.record.State = "timed-out"
		}
		r.record.RemoteExit = nil
	} else if r.record.State != "exited" {
		r.record.State = "transport-or-completion-unknown"
	}
	r.record.OutputComplete = false
}

func (r *remoteRun) Close(string) error {
	r.control.Lock()
	defer r.control.Unlock()
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	if r.record.State == "running" {
		r.mu.Unlock()
		return fmt.Errorf("run active; cancel explicitly before closing")
	}
	if err := r.verifyDirectory(); err != nil {
		r.mu.Unlock()
		return err
	}
	audit, auditErr := launch.BeginAudit(r.workspace, "run close", targetLabel(r.record.Connection), r.record)
	r.closed = true
	r.record.State = "closed"
	r.mu.Unlock()
	var failures error
	for i, f := range r.files {
		if f != nil {
			name := []string{"stdout", "stderr"}[i]
			live, err := r.root.Lstat(name)
			held, e := f.Stat()
			if err == nil && e == nil && os.SameFile(live, held) {
				failures = errors.Join(failures, r.root.Remove(name))
			} else {
				failures = errors.Join(failures, fmt.Errorf("output replaced; unknown file preserved"))
			}
			failures = errors.Join(failures, f.Close())
		}
	}
	for i, f := range r.inputs {
		if f != nil {
			name := []string{"script", "stdin"}[i]
			live, err := r.root.Lstat(name)
			held, e := f.Stat()
			if err == nil && e == nil && os.SameFile(live, held) {
				failures = errors.Join(failures, r.root.Remove(name))
			} else {
				failures = errors.Join(failures, fmt.Errorf("input replaced; unknown file preserved"))
			}
			failures = errors.Join(failures, f.Close())
		}
	}
	if err := r.verifyDirectory(); err != nil {
		failures = errors.Join(failures, err)
	} else {
		failures = errors.Join(failures, os.Remove(r.dir))
	}
	failures = errors.Join(failures, r.root.Close())
	r.mu.Lock()
	defer r.mu.Unlock()
	if failures != nil {
		r.record.CleanupError = failures.Error()
	}
	logErr := audit.Record("closed", r.record)
	if err := errors.Join(auditErr, logErr); err != nil {
		r.record.AuditError = err.Error()
	}
	return errors.Join(failures, auditErr, logErr)
}

func (r *remoteRun) verifyDirectory() error {
	live, err := launch.FileRoot(r.dir, false)
	if err != nil {
		return fmt.Errorf("run output directory unavailable; unknown paths preserved")
	}
	defer live.Close()
	a, err := live.Stat(".")
	b, heldErr := r.root.Stat(".")
	if err != nil || heldErr != nil || !os.SameFile(a, b) {
		return fmt.Errorf("run output directory replaced; unknown paths preserved")
	}
	return nil
}
