// Throwaway retained-resource proof. Fixed inert fixture, never operator targets.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/vibepwners/hovel/sdk/go/hovel"
)

type ownership struct{ connection *ownedConnection }

func (*ownership) Info() hovel.Info {
	return hovel.Info{Name: "burrow-transport-prototype", Version: "0.0.0", Type: hovel.TypeSurvey, Tags: []string{"dangerous"}, Summary: "Disposable connection ownership proof"}
}
func (*ownership) Schema() hovel.Schema { return hovel.Schema{} }
func (p *ownership) Run(ctx *hovel.Context) (hovel.Result, error) {
	dir := filepath.Join(os.Getenv("BURROW_OWNER_ROOT"), "gateway")
	// Atomic reservation: even a stale directory refuses; never adopt or erase it.
	if err := os.Mkdir(dir, 0700); err != nil {
		return hovel.Result{}, err
	}
	s := &ownedConnection{dir: dir, done: make(chan struct{})}
	p.connection = s
	var logMu sync.Mutex
	logCount := 0
	logMilestone := func(message string) {
		logMu.Lock()
		defer logMu.Unlock()
		// ponytail: 200 lifetime milestones, then one warning; remove when Hovel drains retained logs safely.
		if logCount < 200 {
			ctx.Log.Info(message, "connection", "gateway")
		}
		if logCount == 200 {
			ctx.Log.Warn("diagnostic budget exhausted; further milestones suppressed")
		}
		if logCount <= 200 {
			logCount++
		}
	}
	s.Handle = func(command string) (string, error) {
		switch command {
		case "status":
			return "owned connection", nil
		case "log":
			logMilestone("retained-owner-diagnostic")
			return "logged", nil
		case "bounded-logs":
			for i := 0; i < 600; i++ {
				logMilestone("bounded-owner-milestone")
			}
			return "further diagnostic milestones suppressed; control remains available", nil
		case "log-ceiling":
			for i := 0; i < 257; i++ {
				ctx.Log.Info("retained-owner-ceiling", "index", i)
			}
			return "emitted", nil
		default:
			return "", fmt.Errorf("fixture accepts status, log, log-ceiling only")
		}
	}
	if _, err := ctx.OpenSession(s, hovel.WithName("gateway ownership control"), hovel.WithKind("connection"), hovel.WithTransport("ssh")); err != nil {
		s.Close("open failed")
		return hovel.Result{}, err
	}
	logMilestone("owner-established")
	return hovel.Ok(nil, hovel.WithSummary(fmt.Sprintf("owner=%d master=%d", os.Getpid(), s.master.Process.Pid)), hovel.WithArtifacts(hovel.TextArtifact("ownership-proof", "Non-secret fixture evidence; preserve after connection close."))), nil
}

type ownedConnection struct {
	hovel.LineShellSession
	dir      string
	master   *exec.Cmd
	done     chan struct{}
	once     sync.Once
	closeErr error
}

func (s *ownedConnection) Open() error {
	s.master = exec.Command("/usr/bin/ssh", "-F", os.Getenv("BURROW_OWNER_CONFIG"), "-M", "-S", filepath.Join(s.dir, "master"), "-N", "target")
	// Child stdout must never enter the module's framed protocol stream.
	s.master.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	started := make(chan error, 1)
	go func() {
		// Linux ties parent-death signals to the creating thread. Keep it alive.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		err := s.master.Start()
		started <- err
		if err == nil {
			s.master.Wait()
		}
		close(s.done)
		s.LineShellSession.Close("transport unavailable")
	}()
	if err := <-started; err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-s.done:
			return fmt.Errorf("master failed")
		default:
		}
		if _, err := os.Lstat(filepath.Join(s.dir, "master")); err == nil {
			return s.LineShellSession.Open()
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("master readiness timeout")
}

func (s *ownedConnection) Close(reason string) error {
	s.once.Do(func() {
		if s.master != nil && s.master.Process != nil {
			s.master.Process.Signal(os.Interrupt)
			select {
			case <-s.done:
			case <-time.After(2 * time.Second):
				s.master.Process.Kill()
				<-s.done
			}
		}
		s.LineShellSession.Close(reason)
		// Remove only the now-empty reservation. Unknown residual contents refuse.
		s.closeErr = os.Remove(s.dir)
	})
	return s.closeErr
}
