// Disposable #87 public-session proof. Install only in the scratch lab workspace.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/Bochner/burrow/core/connection"
	"github.com/Bochner/burrow/core/launch"
	"github.com/vibepwners/hovel/sdk/go/hovel"
	"golang.org/x/sys/unix"
)

type module struct{ connection.Module }

// The optional command is a bounded public-SDK candidate, not Hovel resize.
// There is deliberately no controller claim: raw writes have no caller identity.
type shell struct {
	hovel.PTYSession
	ready      chan struct{}
	tty        *os.File
	startupErr error
	cmd        *exec.Cmd
	stopped    chan struct{}
}

func (s *shell) Close(reason string) error {
	<-s.ready
	if s.startupErr == nil {
		// This module owns the SSH subprocess; closing SDK descriptors alone
		// does not reliably stop a quiet shell while its PTY read is blocked.
		if err := s.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
	}
	<-s.stopped
	return s.PTYSession.Close(reason)
}

func (s *shell) ListPayloadCommands(hovel.PayloadCommandListRequest) ([]hovel.PayloadCommand, error) {
	return []hovel.PayloadCommand{{Name: "resize", Summary: "PROTOTYPE columns rows; no controller arbitration"}}, nil
}

func (s *shell) RunPayloadCommand(req hovel.PayloadCommandRequest) (hovel.PayloadCommandResult, error) {
	if req.Command != "resize" || len(req.Args) != 2 || len(req.Config) != 0 || req.InputData != "" || req.InputEncoding != "" || req.InputPath != "" || req.Reconnect != nil || req.InstalledPayloadID != "" {
		return hovel.PayloadCommandResult{}, fmt.Errorf("expected only resize columns rows")
	}
	cols, e1 := strconv.Atoi(req.Args[0])
	rows, e2 := strconv.Atoi(req.Args[1])
	if e1 != nil || e2 != nil || cols < 1 || rows < 1 || cols > 1000 || rows > 1000 {
		return hovel.PayloadCommandResult{}, fmt.Errorf("geometry outside 1..1000")
	}
	<-s.ready
	if s.startupErr != nil {
		return hovel.PayloadCommandResult{}, s.startupErr
	}
	if s.Closed() {
		return hovel.PayloadCommandResult{}, fmt.Errorf("shell closed")
	}
	err := unix.IoctlSetWinsize(int(s.tty.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(cols), Row: uint16(rows)})
	return hovel.PayloadCommandResult{Command: req.Command, Stdout: "geometry applied"}, err
}

func newShell(cmd *exec.Cmd, initialGeometry bool) *shell {
	s := &shell{ready: make(chan struct{}), stopped: make(chan struct{}), cmd: cmd}
	s.Frontend = func(input io.Reader, output io.Writer) error {
		defer close(s.stopped)
		s.tty = input.(*os.File) // Public PTYSession provides the PTY slave.
		if initialGeometry {
			s.startupErr = unix.IoctlSetWinsize(int(s.tty.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: 80, Row: 24})
		}
		cmd.Stdin, cmd.Stdout, cmd.Stderr = input, output, output
		cmd.Env = append(cmd.Env, "TERM=xterm-256color")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
		if s.startupErr == nil {
			s.startupErr = cmd.Start()
		}
		// Publish readiness after initial sizing and process creation.
		close(s.ready)
		if s.startupErr != nil {
			return s.startupErr
		}
		return cmd.Wait()
	}
	return s
}

func (module) Run(ctx *hovel.Context) (hovel.Result, error) {
	if ctx.InputString("action", "") != "prototype-shell" {
		return (connection.Module{}).Run(ctx)
	}
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w := ctx.InputString("workspace", "")
	info, err := launch.Status(c, w)
	if err != nil {
		return hovel.Result{}, err
	}
	if os.Getppid() != info.PID {
		return hovel.Result{}, fmt.Errorf("proof must run in the verified workspace daemon")
	}
	// Production currently refuses unknown Burrow session kinds on discovery.
	// Prepare the verified channels before registering any proof session.
	var commands []*exec.Cmd
	for range 3 {
		cmd, err := connection.ShellCommand(c, w, ctx.InputString("command", ""))
		if err != nil {
			return hovel.Result{}, err
		}
		commands = append(commands, cmd)
	}
	var ids []string
	for i, cmd := range commands {
		var session hovel.Session = newShell(cmd, i > 0)
		options := []hovel.SessionOption{hovel.WithKind("shared-shell-proof"), hovel.WithName(fmt.Sprintf("PROTOTYPE shell %d", i+1))}
		if i == 2 {
			session = &controlled{shell: session.(*shell)}
			options = append(options, hovel.WithCapabilities("close"))
		}
		ref, err := ctx.OpenSession(session, options...)
		if err != nil {
			return hovel.Result{}, err
		}
		ids = append(ids, ref.ID)
	}
	b, err := json.Marshal(ids)
	return hovel.Ok(nil, hovel.WithSummary(string(b))), err
}

func run() error {
	if len(os.Args) == 2 && os.Args[1] == "module" {
		hovel.Serve(module{})
		return nil
	}
	if len(os.Args) == 4 && os.Args[1] == "rpc" {
		// Headless proof client: the same verified, public daemon contract as Burrow.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var request, response any
		if err := json.NewDecoder(io.LimitReader(os.Stdin, 65536)).Decode(&request); err != nil {
			return err
		}
		if err := launch.Call(ctx, os.Args[2], os.Args[3], request, &response); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(response)
	}
	if len(os.Args) != 3 || os.Args[1] != "install" {
		return fmt.Errorf("prototype: expected install WORKSPACE, rpc WORKSPACE METHOD (JSON stdin), or module")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := launch.RegisterModule(ctx, os.Args[2], "burrow@0.1.0", connection.Manifest); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]bool{"prototype": true})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
