// Throwaway retained-resource proof. Fixed inert fixture, never operator targets.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/vibepwners/hovel/sdk/go/hovel"
)

type ownership struct{ connection *ownedConnection }

func reserveConnection(root, name string) (string, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,23}$`).MatchString(name) {
		return "", fmt.Errorf("name must be 1-24 ASCII letters/digits, underscore or hyphen, starting with a letter/digit")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || !filepath.IsAbs(root) || resolved != root || !info.IsDir() || info.Mode().Perm() != 0700 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return "", fmt.Errorf("workspace runtime root must be absolute, owner-only and free of symlinks")
	}
	dir := filepath.Join(root, name)
	if len(filepath.Join(dir, "master")) > 90 {
		return "", fmt.Errorf("socket path too long for OpenSSH temporary suffix")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

func (*ownership) Info() hovel.Info {
	return hovel.Info{Name: "burrow-transport-prototype", Version: "0.0.0", Type: hovel.TypeSurvey, Tags: []string{"dangerous"}, Summary: "Disposable connection ownership proof"}
}
func (*ownership) Schema() hovel.Schema { return hovel.Schema{} }

// Disposable Mesh dispatch probe, not a tunnel inventory implementation.
func (*ownership) DescribeMesh(hovel.MeshDescribeRequest) (hovel.MeshDescriptor, error) {
	return hovel.MeshDescriptor{Name: "burrow-transport-prototype", Version: "0.0.0"}, nil
}

func (p *ownership) ListMeshListeners(hovel.MeshListenerListRequest) ([]hovel.MeshListener, error) {
	if p.connection == nil {
		return nil, fmt.Errorf("mesh-dispatch-proof: pid=%d has no retained connection", os.Getpid())
	}
	return nil, fmt.Errorf("mesh-dispatch-proof: pid=%d reached retained owner", os.Getpid())
}

func (p *ownership) Run(ctx *hovel.Context) (hovel.Result, error) {
	if ctx.InputString("proof_action", "connect") == "tunnel-probe" {
		result, err := ownerCommand(ctx.InputString("proof_session", ""), "tunnel-probe", []string{ctx.InputString("proof_tunnel", ""), ctx.InputString("proof_nonce", "")})
		if err != nil {
			return hovel.Result{}, err
		}
		return hovel.Ok(nil, hovel.WithArtifacts(hovel.JSONArtifact("tunnel-probe", result))), nil
	}
	if ctx.InputString("proof_action", "connect") == "connection-status" {
		return selectedOwnerStatus(ctx)
	}
	if ctx.InputString("proof_action", "connect") == "script" {
		return scriptBoundary(ctx)
	}
	// Atomic reservation: even a stale directory refuses; never adopt or erase it.
	dir, err := reserveConnection(os.Getenv("BURROW_OWNER_ROOT"), "gateway")
	if err != nil {
		return hovel.Result{}, err
	}
	s := &ownedConnection{dir: dir, done: make(chan struct{})}
	if ctx.InputString("proof_action", "connect") == "tunnel-connect" {
		if err := json.Unmarshal([]byte(ctx.InputString("proof_tunnels", "")), &s.requested); err != nil {
			os.Remove(dir)
			return hovel.Result{}, fmt.Errorf("invalid fixture tunnel configuration")
		}
	}
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
		if id, ok := strings.CutPrefix(command, "tunnel-close "); ok {
			if err := s.closeTunnel(id); err != nil {
				return "", err
			}
			logMilestone("operator closed tunnel " + id)
			return "tunnel closed", nil
		}
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
	dir       string
	master    *exec.Cmd
	done      chan struct{}
	once      sync.Once
	closeErr  error
	tunnelMu  sync.RWMutex
	requested []fixtureTunnel
	tunnels   map[string]fixtureTunnel
}

// The SDK permits command providers on a Session without an installed payload.
// This read-only probe asks the authoritative owner, never a saved registry.
func (s *ownedConnection) ListPayloadCommands(hovel.PayloadCommandListRequest) ([]hovel.PayloadCommand, error) {
	return []hovel.PayloadCommand{{Name: "connection-status", ReadOnly: true}, {Name: "tunnel-list", ReadOnly: true}, {Name: "tunnel-probe", Summary: "Inert fixture HTTP request through an existing tunnel"}}, nil
}

func (s *ownedConnection) RunPayloadCommand(req hovel.PayloadCommandRequest) (hovel.PayloadCommandResult, error) {
	if req.Reconnect != nil || req.InstalledPayloadID != "" || req.InputPath != "" || req.InputData != "" || len(req.Config) != 0 {
		return hovel.PayloadCommandResult{}, fmt.Errorf("reconnect, payloads, input files and credentials are not supported")
	}
	if s.Closed() {
		return hovel.PayloadCommandResult{}, fmt.Errorf("selected connection is closed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	check := exec.CommandContext(ctx, "/usr/bin/ssh", "-F", os.Getenv("BURROW_OWNER_CONFIG"), "-S", filepath.Join(s.dir, "master"), "-O", "check", "target")
	if err := check.Run(); err != nil {
		return hovel.PayloadCommandResult{}, fmt.Errorf("selected connection is unavailable")
	}
	if req.Command == "tunnel-list" && len(req.Args) == 0 {
		s.tunnelMu.RLock()
		defer s.tunnelMu.RUnlock()
		data, err := json.Marshal(s.tunnels)
		return hovel.PayloadCommandResult{Command: req.Command, Stdout: string(data)}, err
	}
	if req.Command == "tunnel-probe" && len(req.Args) == 2 {
		return s.probeTunnel(req.Args[0], req.Args[1])
	}
	if req.Command != "connection-status" || len(req.Args) != 0 {
		return hovel.PayloadCommandResult{}, fmt.Errorf("unsupported fixture command")
	}
	return hovel.PayloadCommandResult{Command: req.Command, Fields: map[string]string{"ownerPID": fmt.Sprint(os.Getpid()), "connection": "gateway"}}, nil
}

// A real confirmed chain selects a session ID and queries its retained owner.
// Only the harness-supplied local daemon is allowed; no remote TCP or token.
func selectedOwnerStatus(ctx *hovel.Context) (hovel.Result, error) {
	result, err := ownerCommand(ctx.InputString("proof_session", ""), "connection-status", nil)
	if err != nil {
		return hovel.Result{}, err
	}
	if result.Fields["connection"] != "gateway" || result.Fields["ownerPID"] == "" {
		return hovel.Result{}, fmt.Errorf("invalid owner response")
	}
	return hovel.Ok(nil, hovel.WithSummary("selected owner="+result.Fields["ownerPID"])), nil
}

func ownerCommand(id, command string, args []string) (hovel.PayloadCommandResult, error) {
	if id == "" {
		return hovel.PayloadCommandResult{}, fmt.Errorf("select an existing connection session")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", os.Getenv("BURROW_PROOF_DAEMON"))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 8 * time.Second}
	body, err := json.Marshal(map[string]any{"SessionID": id, "Request": hovel.PayloadCommandRequest{Command: command, Args: args}})
	if err != nil {
		return hovel.PayloadCommandResult{}, err
	}
	response, err := client.Post("http://localhost/hovel.daemon.v1.DaemonService/RunSessionCommand", "application/json", bytes.NewReader(body))
	if err != nil {
		return hovel.PayloadCommandResult{}, fmt.Errorf("selected owner unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return hovel.PayloadCommandResult{}, fmt.Errorf("selected owner refused command")
	}
	var result hovel.PayloadCommandResult
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result); err != nil {
		return hovel.PayloadCommandResult{}, err
	}
	return result, nil
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
			if err := s.openTunnels(); err != nil {
				return err
			}
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
