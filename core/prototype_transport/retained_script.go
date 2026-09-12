// Disposable retained-run proof. No operator code or production job registry.
package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/vibepwners/hovel/sdk/go/hovel"
)

//go:embed script_fixture.py
var scriptFixture string

type retainedScript struct {
	hovel.LineShellSession
	deferred bool
	started  bool
	mu       sync.Mutex
	cancelMu sync.Mutex
	process  *exec.Cmd
	input    io.WriteCloser
	done     chan struct{}
	report   map[string]any
}

func (p *ownership) startRetainedScript(ctx *hovel.Context) (hovel.Result, error) {
	fixture := ctx.InputString("proof_case", "success")
	switch fixture {
	case "success", "nonzero", "cancel", "timeout", "loss", "launch-loss":
	default:
		return hovel.Result{}, fmt.Errorf("unsupported inert fixture")
	}
	keep := ctx.InputString("proof_keep", "no")
	if keep != "yes" && keep != "no" {
		return hovel.Result{}, fmt.Errorf("invalid keep value")
	}
	root := os.Getenv("BURROW_OWNER_ROOT")
	marker := filepath.Join(root, "retained-"+fixture)
	command := "exec " + shellQuote(os.Getenv("BURROW_SCRIPT_PYTHON")) + " -c " + shellQuote(scriptFixture) + " " + shellQuote(fixture) + " " + shellQuote(marker) + " " + shellQuote(keep)
	cmd := exec.Command("/usr/bin/ssh", "-F", os.Getenv("BURROW_OWNER_CONFIG"), "-S", filepath.Join(root, "gateway", "master"), "-o", "ProxyCommand=/bin/false", "-T", "target", command)
	s := &retainedScript{deferred: ctx.InputString("proof_action", "") == "script-prepare", process: cmd, done: make(chan struct{}), report: map[string]any{"state": "running", "cleanup": "unconfirmed"}}
	if s.deferred {
		s.report["state"] = "prepared"
	}
	if _, err := ctx.OpenSession(s, hovel.WithName("inert script run"), hovel.WithKind("script-run"), hovel.WithTransport("ssh")); err != nil {
		return hovel.Result{}, err
	}
	p.script = s
	if fixture == "launch-loss" && !s.deferred {
		time.Sleep(3 * time.Second)
	}
	return hovel.Ok(nil, hovel.WithSummary("Run accepted; completion requires status and confirmed collection")), nil
}

func (s *retainedScript) Open() error {
	if !s.deferred {
		if err := s.launch(); err != nil {
			return err
		}
	}
	return s.LineShellSession.Open()
}

func (s *retainedScript) launch() error {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	if s.started {
		return nil
	} // Retries use this session, never create another run.
	s.started = true
	err := s.startProcess()
	if err != nil {
		s.mu.Lock()
		s.report = map[string]any{"state": "not-started", "cleanup": "not-needed"}
		s.mu.Unlock()
		close(s.done)
	}
	return err
}

func (s *retainedScript) startProcess() error {
	var err error
	s.input, err = s.process.StdinPipe()
	if err != nil {
		return err
	}
	out, err := s.process.StdoutPipe()
	if err != nil {
		s.input.Close()
		return err
	}
	var transportError cappedOutput
	s.process.Stderr = &transportError
	if err := s.process.Start(); err != nil {
		out.Close()
		s.input.Close()
		return err
	}
	go func() {
		defer close(s.done)
		scanner := bufio.NewScanner(out)
		scanner.Buffer(make([]byte, 4096), 256*1024)
		final := false
		var completed map[string]any
		valid := true
		for scanner.Scan() {
			var report map[string]any
			if json.Unmarshal(scanner.Bytes(), &report) != nil {
				valid = false
				break
			}
			state, ok := report["state"].(string)
			if !ok {
				valid = false
				break
			}
			final = state != "running"
			if final {
				completed = report
			} else {
				s.mu.Lock()
				s.report = report
				s.mu.Unlock()
			}
		}
		// Scanner must finish before Wait closes stdout. A bounded remote supervisor
		// owns cancellation and staging; local ssh exit alone never proves cleanup.
		if scanner.Err() != nil || !valid {
			s.process.Process.Kill()
		}
		err := s.process.Wait()
		s.mu.Lock()
		if completed != nil {
			s.report = completed
		}
		if err != nil || !final || !valid {
			s.report["state"] = "transport-or-completion-unknown"
			s.report["cleanup"] = "unconfirmed"
			delete(s.report, "remoteExit")
		}
		s.mu.Unlock()
	}()
	return nil
}

// No execution through raw terminal input: the typed command is the control path.
func (s *retainedScript) Write([]byte) error {
	return fmt.Errorf("use script status or explicit cancel")
}

func (s *retainedScript) ListPayloadCommands(hovel.PayloadCommandListRequest) ([]hovel.PayloadCommand, error) {
	return []hovel.PayloadCommand{{Name: "script-launch", Destructive: true}, {Name: "script-status", ReadOnly: true}, {Name: "script-cancel", Destructive: true}}, nil
}

func (s *retainedScript) RunPayloadCommand(req hovel.PayloadCommandRequest) (hovel.PayloadCommandResult, error) {
	if len(req.Args) != 0 || req.Reconnect != nil || req.InstalledPayloadID != "" || req.InputPath != "" || req.InputData != "" || len(req.Config) != 0 || s.Closed() {
		return hovel.PayloadCommandResult{}, fmt.Errorf("unsupported or closed fixture request")
	}
	if req.Command == "script-launch" {
		if err := s.launch(); err != nil {
			return hovel.PayloadCommandResult{}, err
		}
	} else if req.Command == "script-cancel" {
		if err := s.cancel(); err != nil {
			return hovel.PayloadCommandResult{}, err
		}
	} else if req.Command != "script-status" {
		return hovel.PayloadCommandResult{}, fmt.Errorf("unsupported script command")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(s.report)
	return hovel.PayloadCommandResult{Command: req.Command, Stdout: string(data), Fields: map[string]string{"state": fmt.Sprint(s.report["state"])}}, err
}

func (s *retainedScript) cancel() error {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	if !s.started {
		s.started = true
		s.mu.Lock()
		s.report = map[string]any{"state": "cancelled-before-start", "cleanup": "not-needed"}
		s.mu.Unlock()
		close(s.done)
	}
	select {
	case <-s.done:
		return nil
	default:
	}
	if _, err := io.WriteString(s.input, "cancel\n"); err != nil {
		return err
	}
	select {
	case <-s.done:
		return nil
	case <-time.After(2 * time.Second):
		return fmt.Errorf("remote termination and cleanup unconfirmed")
	}
}

func (s *retainedScript) Close(reason string) error {
	err := s.cancel()
	if err != nil && s.process.Process != nil {
		s.process.Process.Kill()
	}
	if s.input != nil {
		s.input.Close()
	}
	s.LineShellSession.Close(reason)
	return err
}
