package connection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Bochner/burrow/core/launch"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/vibepwners/hovel/sdk/go/hovel"
	"golang.org/x/sys/unix"
)

const shellKind = "burrow-shell-v1"
const shellOutputLimit = 64 << 10

var ErrShellUnavailable = errors.New("shell unavailable for this workspace/connection; no adoption or restoration")

type shellLaunchRequest struct {
	Connection State `json:"connection"`
	Columns    int   `json:"columns"`
	Rows       int   `json:"rows"`
}

func shellCreateOptions(args []string) (columns, rows int, flags []string, err error) {
	columns, rows = 80, 24
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		if args[i] != "--columns" && args[i] != "--rows" {
			flags = append(flags, args[i])
			continue
		}
		flag := args[i]
		if seen[flag] || i+1 == len(args) {
			return 0, 0, nil, fmt.Errorf("expected one value per geometry flag")
		}
		seen[flag] = true
		i++
		value, e := strconv.Atoi(args[i])
		if e != nil {
			return 0, 0, nil, fmt.Errorf("invalid shell dimensions")
		}
		if flag == "--columns" {
			columns = value
		} else {
			rows = value
		}
	}
	return
}

// Shell describes one Hovel session, not a second session registry. The
// connection owner keeps dependent references only to serialize launch/cleanup.
type Shell struct {
	adopted           bool   // Observed in this daemon's broker; absence afterward means explicit CloseSession.
	ID                string `json:"id"`
	Workspace         string `json:"workspace"`
	Connection        State  `json:"connection"`
	RunID             string `json:"runID"`
	OwnerPID          int    `json:"ownerPID"`
	PID               int    `json:"pid"`
	State             string `json:"state"`
	Detail            string `json:"detail"`
	Columns           int    `json:"columns"`
	Rows              int    `json:"rows"`
	Received          uint64 `json:"received"`
	Buffered          int    `json:"buffered"`
	Dropped           uint64 `json:"dropped"`
	Cleanup           string `json:"cleanup"`
	AuditError        string `json:"auditError,omitempty"`
	SSHExit           *int   `json:"sshExit,omitempty"`
	Controller        string `json:"controller"`
	ControlGeneration uint64 `json:"controlGeneration"`
}

type retainedShell struct {
	mu            sync.Mutex
	record        Shell
	cmd           *exec.Cmd
	pty           *os.File
	data          []byte
	done          chan struct{}
	drained       chan struct{}
	closed        chan struct{}
	closeMu       sync.Mutex
	audit         launch.Audit
	token         string // Ephemeral; never part of Shell, logs or evidence.
	screen        *vt.Emulator
	cursorVisible bool
	inputModes    map[ansi.Mode]bool
}

func sessionArgs(w string, args []string) (bool, string, error) {
	if len(args) < 3 {
		return false, "", fmt.Errorf("expected session ACTION CONNECTION [ID]; use help")
	}
	if _, err := launch.ConnectionPath(w, args[2]); err != nil {
		return false, "", err
	}
	switch args[1] {
	case "list":
		if len(args) == 3 {
			return false, "", nil
		}
	case "create":
		columns, rows, flags, err := shellCreateOptions(args[3:])
		if err != nil {
			return false, "", err
		}
		if err := shellGeometry(columns, rows); err != nil {
			return false, "", err
		}
		return closeOptions(flags)
	case "claim", "takeover", "input", "resize", "release", "observe", "snapshot", "inspect", "close":
		if len(args) < 4 || args[3] == "" || len(args[3]) > 256 || strings.ContainsAny(args[3], "/\\\x00\r\n\t ") {
			break
		}
		if args[1] == "close" {
			return closeOptions(args[4:])
		}
		if privateShellCommand(args[1]) {
			if len(args) == 5 && args[4] == "--request-stdin" {
				return false, "", nil
			}
			break
		}
		if args[1] == "snapshot" && len(args) == 5 {
			offset, err := strconv.Atoi(args[4])
			if err != nil || offset < 0 || offset > 1000 {
				return false, "", fmt.Errorf("invalid history offset; expected 0..1000")
			}
			return false, "", nil
		}
		if args[1] == "observe" && len(args) == 5 {
			_, err := strconv.ParseUint(args[4], 10, 64)
			if err != nil {
				return false, "", fmt.Errorf("invalid shell output position")
			}
			return false, "", nil
		}
		if len(args) == 4 {
			return false, "", nil
		}
	}
	return false, "", fmt.Errorf("invalid session arguments; use help for lifecycle, shared control and observation routes")
}

func shellReview(action, w string, state State, id string, columns, rows int) string {
	b, _ := json.Marshal([]any{action, w, state, id, columns, rows})
	return digest(string(b))
}

func executeSession(ctx context.Context, w string, args []string) (any, error) {
	yes, review, err := sessionArgs(w, args)
	if err != nil {
		return nil, err
	}
	if args[1] == "list" {
		return Shells(ctx, w, args[2])
	}
	if args[1] == "inspect" {
		return inspectShell(ctx, w, args[2], args[3])
	}
	if args[1] == "observe" || args[1] == "snapshot" {
		return observeShell(ctx, w, args)
	}
	if privateShellCommand(args[1]) {
		return nil, fmt.Errorf("private request required; use the headless CLI with --request-stdin")
	}
	state, err := selected(ctx, w, args[2])
	if err != nil {
		return nil, err
	}
	id, err := findManager(ctx, w)
	if err != nil || !id.RetainedShells || state.Session != id.Session || state.Generation != id.Generation {
		return nil, fmt.Errorf("retained shells require a verified compatible manager; review workspace restart explicitly; no adoption")
	}
	shellID := ""
	if args[1] == "close" {
		shellID = args[3]
		s, err := inspectShell(ctx, w, args[2], shellID)
		if err != nil {
			return nil, err
		}
		if s.Connection.Creation != state.Creation || s.Connection.Generation != id.Generation || s.Connection.Session != id.Session {
			return nil, fmt.Errorf("shell owner unavailable or replaced; no cleanup attempted")
		}
	} else if state.State != "connected" {
		return nil, fmt.Errorf("shell creation requires a connected master; reconnect explicitly")
	}
	columns, rows := 80, 24
	if args[1] == "create" {
		columns, rows, _, _ = shellCreateOptions(args[3:])
	}
	hash := shellReview(args[1], w, state, shellID, columns, rows)
	if !yes {
		text := fmt.Sprintf("Create one retained SSH shell at %dx%d through the verified master. It survives frontend exit; headless controllers and TUI observers share this session.", columns, rows)
		if shellID != "" {
			text = "Close only this shell's SSH client and channel; preserve the master and sibling resources. Remote commands and escaped descendants may have uncertain outcomes."
		}
		return map[string]string{"review": text + "\n" + targetLabel(state) + "\nShell: " + shellID, "digest": hash}, nil
	}
	if review != "" && review != hash {
		return nil, fmt.Errorf("session review changed; review again")
	}
	if shellID != "" {
		var result Shell
		err = managerControl(ctx, w, id, "shell-close", []string{state.Creation, shellID, closeDigest(w, state)}, &result)
		if err == nil && result.AuditError != "" {
			err = fmt.Errorf("shell closed; audit incomplete: %s", result.AuditError)
		}
		return result, err
	}
	raw, _ := json.Marshal(shellLaunchRequest{Connection: state, Columns: columns, Rows: rows})
	var result Shell
	err = managerThrow(ctx, w, map[string]string{"action": "shell-prepare", "request": string(raw), "review": digest(string(raw))}, &result)
	if err != nil {
		return nil, fmt.Errorf("shell preparation unconfirmed; inspect session list %s before retrying: %w", state.Name, err)
	}
	if result.Connection != state || result.ID == "" || result.Workspace != w {
		return nil, fmt.Errorf("shell preparation identity changed; inspect sessions")
	}
	shellID = result.ID
	err = managerThrow(ctx, w, map[string]string{"action": "shell-start", "session": shellID, "request": string(raw), "review": digest(string(raw))}, &result)
	if err != nil {
		return result, fmt.Errorf("shell %s launch unconfirmed; inspect that ID; never retry automatically: %w", shellID, err)
	}
	if result.ID != shellID || result.Connection != state {
		return nil, fmt.Errorf("shell launch identity changed; inspect sessions")
	}
	return result, nil
}

func shellRefs(ctx context.Context, w string) ([]hovel.SessionRef, error) {
	var refs struct{ Sessions []hovel.SessionRef }
	if err := launch.Call(ctx, w, "ListSessions", map[string]any{}, &refs); err != nil {
		return nil, err
	}
	return refs.Sessions, nil
}

func Shells(ctx context.Context, w, name string) ([]Shell, error) {
	refs, err := shellRefs(ctx, w)
	if err != nil {
		return nil, err
	}
	states := []Shell{}
	for _, ref := range refs {
		if ref.ModuleID != "burrow@0.1.0" || ref.Kind != shellKind || ref.Name != name {
			continue
		}
		state, err := shellState(ctx, w, ref)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	slices.SortFunc(states, func(a, b Shell) int { return strings.Compare(a.ID, b.ID) })
	return states, nil
}

func shellState(ctx context.Context, w string, ref hovel.SessionRef) (Shell, error) {
	state := Shell{ID: ref.ID, Workspace: w, Connection: State{Name: ref.Name}, State: "unavailable", Detail: "shell module unavailable; remote-command outcome and cleanup uncertain; no restoration"}
	// A live provider can still report confirmed cleanup and an audit error after
	// the broker marks its stream closed. Module loss instead fails this query.
	result, err := ownerCommand(ctx, w, ref.ID, "inspect", nil)
	if err == nil {
		if json.Unmarshal([]byte(result.Stdout), &state) != nil || state.ID != ref.ID || state.Connection.Name != ref.Name || state.Workspace != w {
			return Shell{}, fmt.Errorf("invalid shell identity")
		}
	}
	return state, nil
}

func inspectShell(ctx context.Context, w, name, id string) (Shell, error) {
	refs, err := shellRefs(ctx, w)
	if err != nil {
		return Shell{}, err
	}
	for _, ref := range refs {
		if ref.ID == id && ref.ModuleID == "burrow@0.1.0" && ref.Kind == shellKind && ref.Name == name {
			return shellState(ctx, w, ref)
		}
	}
	return Shell{}, ErrShellUnavailable
}

func shellAdapter(ctx *hovel.Context, w string) (hovel.Result, error) {
	c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	raw := ctx.InputString("request", "")
	var request shellLaunchRequest
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if len(raw) > 16384 || d.Decode(&request) != nil || d.Decode(new(any)) != io.EOF || digest(raw) != ctx.InputString("review", "") {
		return hovel.Result{}, fmt.Errorf("changed shell request refused")
	}
	if err := shellGeometry(request.Columns, request.Rows); err != nil {
		return hovel.Result{}, err
	}
	connection := request.Connection
	id := managerIdentity{Session: connection.Session, Generation: connection.Generation}
	var result Shell
	switch ctx.InputString("action", "") {
	case "shell-prepare":
		// Register through Module.Run, as required by Hovel's session adoption.
		s := &retainedShell{record: Shell{Workspace: w, Connection: connection, RunID: ctx.RunID, OwnerPID: os.Getpid(), State: "prepared", Columns: request.Columns, Rows: request.Rows, Cleanup: "not requested"}}
		ref, err := ctx.OpenSession(s, hovel.WithName(connection.Name), hovel.WithKind(shellKind), hovel.WithTransport("ssh"), hovel.WithCapabilities("close"))
		if err != nil {
			return hovel.Result{}, err
		}
		s.mu.Lock()
		s.record.ID = ref.ID
		encoded, _ := json.Marshal(s.record)
		s.mu.Unlock()
		if err := managerControl(c, w, id, "shell-register", []string{connection.Creation, string(encoded)}, &result); err != nil {
			return hovel.Result{}, errors.Join(err, s.Close("preparation refused"))
		}
	case "shell-start":
		var err error
		err = managerControl(c, w, id, "shell-start", []string{connection.Creation, ctx.InputString("session", ""), raw, ctx.RunID}, &result)
		if err != nil {
			return hovel.Result{}, err
		}
	default:
		return hovel.Result{}, fmt.Errorf("unsupported shell adapter")
	}
	result.RunID = ctx.RunID
	b, _ := json.Marshal(result)
	return hovel.Ok(nil, hovel.WithSummary(string(b))), nil
}

// Called with the manager admission lock held. The session is registered before
// launch, so close and launch never race an untracked SSH subprocess.
func (m *manager) shellControl(c context.Context, req hovel.PayloadCommandRequest) (Shell, error) {
	if len(req.Args) < 3 {
		return Shell{}, fmt.Errorf("exact connection and shell required")
	}
	s := m.connections[req.Args[1]]
	if s == nil {
		return Shell{}, fmt.Errorf("connection unavailable; review again")
	}
	current, err := s.RunPayloadCommand(hovel.PayloadCommandRequest{Command: "connection-status"})
	if err != nil {
		return Shell{}, err
	}
	var live State
	if json.Unmarshal([]byte(current.Stdout), &live) != nil {
		return Shell{}, fmt.Errorf("invalid connection state")
	}
	if req.Command == "shell-register" && len(req.Args) == 3 {
		var shell Shell
		if json.Unmarshal([]byte(req.Args[2]), &shell) != nil || shell.Connection != live || live.State != "connected" || shell.Workspace != m.Workspace || shell.ID == "" || shell.State != "prepared" || shell.OwnerPID <= 0 || shellGeometry(shell.Columns, shell.Rows) != nil {
			return Shell{}, fmt.Errorf("shell request or connection changed; review again")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.shells == nil {
			s.shells = map[string]Shell{}
		}
		if _, exists := s.shells[shell.ID]; exists {
			return Shell{}, fmt.Errorf("shell already registered")
		}
		s.shells[shell.ID] = shell
		s.state.ShellCount = len(s.shells)
		s.state.ShellRevision++
		return shell, nil
	}
	shell, exists := s.shells[req.Args[2]]
	if !exists {
		return Shell{}, fmt.Errorf("shell does not belong to this connection")
	}
	switch {
	case req.Command == "shell-start" && len(req.Args) == 5:
		var expected shellLaunchRequest
		if json.Unmarshal([]byte(req.Args[3]), &expected) != nil || expected.Connection != shell.Connection || expected.Columns != shell.Columns || expected.Rows != shell.Rows || live.State != "connected" || live.MasterPID != expected.Connection.MasterPID || live.SocketInode != expected.Connection.SocketInode || req.Args[4] == "" {
			return shell, fmt.Errorf("shell connection changed; no launch")
		}
		observed, err := inspectShell(c, m.Workspace, live.Name, shell.ID)
		if err != nil || observed.Connection != shell.Connection || observed.OwnerPID != shell.OwnerPID || observed.State != "prepared" {
			return shell, fmt.Errorf("shell preparation unavailable or already launched; inspect instead of retrying")
		}
		shell.adopted = true
		s.mu.Lock()
		s.shells[shell.ID] = shell
		s.mu.Unlock()
		result, err := ownerCommand(c, m.Workspace, shell.ID, "start", []string{digest(req.Args[3]), req.Args[4]})
		if err != nil {
			return shell, err
		}
		if json.Unmarshal([]byte(result.Stdout), &observed) != nil || observed.ID != shell.ID || observed.Connection != shell.Connection {
			return shell, fmt.Errorf("shell launch correlation refused")
		}
		s.mu.Lock()
		observed.adopted = true
		s.shells[shell.ID] = observed
		s.mu.Unlock()
		return observed, nil
	case req.Command == "shell-close" && len(req.Args) == 4:
		if req.Args[3] != closeDigest(m.Workspace, live) {
			return shell, fmt.Errorf("connection changed; review close again")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.closeShell(shell)
	}
	return shell, fmt.Errorf("unsupported shell control")
}

// Hovel retains records on module loss and removes them only after successful
// CloseSession. Never infer closure for preparation not yet observed adopted.
func (s *owner) reconcileShells(c context.Context) error {
	if len(s.shells) == 0 {
		return nil
	}
	refs, err := shellRefs(c, s.config.Workspace)
	if err != nil {
		return err
	}
	for id, shell := range s.shells {
		present := slices.ContainsFunc(refs, func(ref hovel.SessionRef) bool { return ref.ID == id })
		if present {
			shell.adopted = true
			s.shells[id] = shell
		} else if shell.adopted {
			s.forgetShell(id)
		}
	}
	return nil
}

func (s *owner) forgetShell(id string) {
	delete(s.shells, id)
	s.state.ShellCount = len(s.shells)
	s.state.ShellRevision++
}

func (s *owner) closeShell(shell Shell) (Shell, error) {
	c, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	observed, err := inspectShell(c, s.config.Workspace, shell.Connection.Name, shell.ID)
	if err != nil || observed.Workspace != shell.Workspace || observed.Connection != shell.Connection || observed.OwnerPID != shell.OwnerPID {
		return shell, fmt.Errorf("shell %s owner unavailable or replaced; cleanup unconfirmed; owner preserved", shell.ID)
	}
	result, err := ownerCommand(c, s.config.Workspace, shell.ID, "stop", nil)
	if err != nil {
		return shell, fmt.Errorf("shell %s cleanup unconfirmed; owner preserved: %w", shell.ID, err)
	}
	if json.Unmarshal([]byte(result.Stdout), &observed) != nil || observed.ID != shell.ID || observed.Connection != shell.Connection || observed.OwnerPID != shell.OwnerPID || observed.State != "closed" {
		return shell, fmt.Errorf("shell cleanup correlation refused")
	}
	observed.adopted = true
	s.shells[shell.ID] = observed
	var out any
	if err := launch.Call(c, s.config.Workspace, "CloseSession", map[string]string{"SessionID": shell.ID}, &out); err != nil {
		return observed, fmt.Errorf("shell %s closed; Hovel removal unconfirmed; inspect before retrying: %w", shell.ID, err)
	}
	s.forgetShell(shell.ID)
	return observed, nil
}

func (s *owner) closeShells() error {
	var failures error
	for _, shell := range s.shells {
		final, err := s.closeShell(shell)
		failures = errors.Join(failures, err)
		if final.AuditError != "" {
			failures = errors.Join(failures, fmt.Errorf("shell %s audit incomplete: %s", shell.ID, final.AuditError))
		}
	}
	return failures
}

func (s *retainedShell) Open() error { s.closed = make(chan struct{}); return nil }

// Keep terminal outcomes inspectable until explicit close, like retained runs.
func (s *retainedShell) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.record.State == "closed"
}
func (s *retainedShell) Write([]byte) error {
	return fmt.Errorf("raw shell input disabled; use token-fenced input through RunSessionCommand")
}
func (s *retainedShell) Read(wait time.Duration) ([]byte, error) {
	if wait < 0 {
		<-s.closed
	} else if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-s.closed:
		case <-timer.C:
		}
	}
	return nil, nil
}
func (s *retainedShell) ListPayloadCommands(hovel.PayloadCommandListRequest) ([]hovel.PayloadCommand, error) {
	return []hovel.PayloadCommand{
		{Name: "inspect", ReadOnly: true},
		{Name: "observe", ReadOnly: true, Usage: "args: [decimal byte position]", Summary: "Independent bounded bytes, explicit gap/loss; not a current screen"},
		{Name: "snapshot", ReadOnly: true, Summary: "Bounded current display and byte position; no input or resize"},
		{Name: "claim", Summary: "Claim if unowned; private JSON inputData, inputEncoding=utf-8"},
		{Name: "takeover", Summary: "Explicit generation-checked takeover; private JSON inputData"},
		{Name: "input", Summary: "Private token and base64 data; accepted bytes are not a command result"},
		{Name: "resize", Summary: "Private token, columns and rows; current controller only"},
		{Name: "release", Summary: "Private token; retain shell and last geometry"},
		{Name: "stop", Summary: "End the owned channel and return final cleanup metadata"},
		{Name: "start", Summary: "Confirmed manager adapter only; never raw interactive input"},
	}, nil
}
func (s *retainedShell) RunPayloadCommand(req hovel.PayloadCommandRequest) (hovel.PayloadCommandResult, error) {
	if len(req.Config) != 0 || req.InputPath != "" || req.Reconnect != nil || req.InstalledPayloadID != "" || req.Target != "" || req.PayloadID != "" || req.Agent != nil {
		return hovel.PayloadCommandResult{}, fmt.Errorf("unexpected shell command inputs")
	}
	if privateShellCommand(req.Command) || req.Command == "observe" || req.Command == "snapshot" {
		s.mu.Lock()
		defer s.mu.Unlock()
		command := s.sharedCommand
		if privateShellCommand(req.Command) {
			command = s.controlCommand
		}
		value, err := command(req)
		if err != nil {
			return hovel.PayloadCommandResult{}, err
		}
		b, err := json.Marshal(value)
		return hovel.PayloadCommandResult{Command: req.Command, Stdout: string(b)}, err
	}
	if req.InputData != "" || req.InputEncoding != "" {
		return hovel.PayloadCommandResult{}, fmt.Errorf("unexpected shell command inputs")
	}
	if req.Command == "stop" && len(req.Args) == 0 {
		err := s.Close("selected shell close")
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.record.State != "closed" {
			return hovel.PayloadCommandResult{}, err
		}
		// Physical cleanup succeeded; preserve audit failure in the typed result.
		b, err := json.Marshal(s.record)
		return hovel.PayloadCommandResult{Command: req.Command, Stdout: string(b)}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case req.Command == "inspect" && len(req.Args) == 0:
	case req.Command == "start" && len(req.Args) == 2:
		b, _ := json.Marshal(shellLaunchRequest{Connection: s.record.Connection, Columns: s.record.Columns, Rows: s.record.Rows})
		if req.Args[0] != digest(string(b)) || req.Args[1] == "" || s.record.State != "prepared" {
			return hovel.PayloadCommandResult{}, fmt.Errorf("shell launch request changed or already submitted")
		}
		if err := s.start(req.Args[1]); err != nil {
			return hovel.PayloadCommandResult{}, err
		}
	default:
		return hovel.PayloadCommandResult{}, fmt.Errorf("unsupported shell command; raw input/observation disabled")
	}
	b, err := json.Marshal(s.record)
	return hovel.PayloadCommandResult{Command: req.Command, Stdout: string(b)}, err
}

// Own the PTY directly: SDK PTYSession has an unbounded intermediate queue.
// Output never waits for a reader; only token-fenced input reaches this PTY.
func (s *retainedShell) start(runID string) error {
	if err := shellGeometry(s.record.Columns, s.record.Rows); err != nil {
		return err
	}
	cmd, err := shellCommand(s.record.Workspace, s.record.Connection)
	if err != nil {
		return err
	}
	audit, err := launch.BeginAudit(s.record.Workspace, "session create", targetLabel(s.record.Connection), s.record)
	if err != nil {
		return err
	}
	s.audit = audit
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}
	master := os.NewFile(uintptr(fd), "/dev/ptmx")
	ok := false
	defer func() {
		if !ok {
			master.Close()
		}
	}()
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		return err
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		return err
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0600)
	if err != nil {
		return err
	}
	defer slave.Close()
	if err := unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(s.record.Columns), Row: uint16(s.record.Rows)}); err != nil {
		return err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0, Pdeathsig: syscall.SIGKILL}
	// Linux parent-death signals follow the spawning thread. Keep it alive until
	// Wait finishes, so ordinary Go thread retirement cannot kill a live shell.
	started := make(chan error, 1)
	s.done = make(chan struct{})
	s.drained = make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		err := cmd.Start()
		started <- err
		if err != nil {
			close(s.done)
			return
		}
		err = cmd.Wait()
		// Read the final channel output before closing the master. A descendant
		// holding the slave cannot defer completion indefinitely.
		select {
		case <-s.drained:
		case <-time.After(100 * time.Millisecond):
		}
		master.Close()
		<-s.drained
		s.mu.Lock()
		exit := cmd.ProcessState.ExitCode()
		s.record.SSHExit = &exit
		if s.record.State != "closing" {
			s.record.State = "exited"
			s.record.Detail = "SSH channel ended; terminal bytes do not establish command outcomes"
			if err != nil && (exit < 0 || exit == 255) {
				s.record.State, s.record.Detail = "lost", "SSH channel lost; remote-command outcome and cleanup uncertain; reconnect explicitly"
			}
		}
		snapshot := s.record
		s.mu.Unlock()
		if err := s.audit.Record(snapshot.State, snapshot); err != nil {
			s.mu.Lock()
			s.record.AuditError = err.Error()
			s.mu.Unlock()
		}
		close(s.done)
	}()
	if err := <-started; err != nil {
		s.record.State = "failed"
		return err
	}
	s.cmd, s.pty = cmd, master
	s.initScreen()
	s.record.PID, s.record.RunID, s.record.State = cmd.Process.Pid, runID, "running"
	s.record.Detail = "retained SSH shell; one explicit controller, independent observers; controller label does not assert liveness"
	ok = true
	go s.drain(master)
	if err := s.audit.Record("running", s.record); err != nil {
		s.record.AuditError = err.Error()
	}
	return nil
}

func (s *retainedShell) drain(pty *os.File) {
	defer close(s.drained)
	buf := make([]byte, 4096)
	for {
		n, err := pty.Read(buf)
		if n > 0 {
			s.mu.Lock()
			s.screen.Write(buf[:n])
			s.record.Received += uint64(n)
			// ponytail: bounded 64 KiB suffix copy; use a ring if profiling requires it.
			if len(s.data)+n > shellOutputLimit {
				keep := shellOutputLimit - n
				copy(s.data, s.data[len(s.data)-keep:])
				s.data = s.data[:keep]
			}
			s.data = append(s.data, buf[:n]...)
			s.record.Buffered = len(s.data)
			s.record.Dropped = s.record.Received - uint64(len(s.data))
			s.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (s *retainedShell) Close(string) error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	s.mu.Lock()
	if s.record.State == "closed" {
		s.mu.Unlock()
		return nil
	}
	if s.cmd == nil {
		s.record.State = "closed"
		s.record.Detail, s.record.Cleanup = "closed before launch", "no SSH client launched"
		close(s.closed)
		s.mu.Unlock()
		return nil
	}
	s.record.State = "closing"
	err := s.cmd.Process.Kill()
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		s.mu.Unlock()
		return fmt.Errorf("SSH client termination unconfirmed: %w", err)
	}
	s.pty.Close()
	done := s.done
	s.mu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("SSH client wait unconfirmed")
	}
	s.mu.Lock()
	s.record.State = "closed"
	s.record.Detail = "SSH client reaped; shell explicitly closed"
	s.record.Cleanup = "owned SSH client reaped; channel teardown requested; remote-command outcomes and escaped descendants unconfirmed"
	defer s.mu.Unlock()
	close(s.closed)
	if err := s.audit.Record("closed", s.record); err != nil {
		s.record.AuditError = err.Error()
		return err
	}
	return nil
}
