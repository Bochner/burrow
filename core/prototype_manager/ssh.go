// Disposable loopback SSH fixture, not a second production runtime.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Bochner/burrow/core/connection"
	"github.com/Bochner/burrow/core/launch"
	"golang.org/x/sys/unix"
)

type request struct {
	ID         string            `json:"id"`
	Generation string            `json:"generation"`
	Session    string            `json:"session"`
	Settings   connection.Config `json:"settings"`
}

func decodeRequest(raw string) (request, error) {
	var r request
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(&r); e != nil {
		return r, fmt.Errorf("invalid non-secret request")
	}
	if d.Decode(new(any)) != io.EOF {
		return r, fmt.Errorf("trailing request data")
	}
	if len(r.ID) != 32 || r.Generation == "" || r.Session == "" {
		return r, fmt.Errorf("missing immutable request identity")
	}
	if e := r.Settings.Validate(); e != nil {
		return r, e
	}
	if r.Settings.Host != "127.0.0.1" || r.Settings.Port == 0 || r.Settings.User != "tester" || r.Settings.Jump != "" || r.Settings.Trust != "" {
		return r, fmt.Errorf("proof supports only the declared loopback tester fixture")
	}
	return r, nil
}
func digest(raw string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(raw))) }
func mono() int64 {
	var t unix.Timespec
	if unix.ClockGettime(unix.CLOCK_MONOTONIC, &t) != nil {
		panic("monotonic clock unavailable")
	}
	return t.Nano()
}

type connectionState struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	RunID     string `json:"runID"`
	State     string `json:"state"`
	Socket    string `json:"socket"`
	PID       int    `json:"pid"`
	Dispatch  int64  `json:"dispatch"`
	Connected int64  `json:"connected,omitempty"`
}
type master struct {
	mu sync.Mutex
	connectionState
	request request
	dir     *os.File
	socket  os.FileInfo
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	done    chan struct{}
	manager *manager
	tunnels map[string]consumerTunnel
}

func (m *master) snapshot() (connectionState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.State == "connected" {
		if e := m.verify(); e != nil {
			return connectionState{}, e
		}
	}
	return m.connectionState, nil
}
func (m *master) verify() error {
	c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := launch.VerifyReservation(c, m.request.Settings.Workspace, m.dir); e != nil {
		return e
	}
	st, e := os.Lstat(m.Socket)
	if e != nil || m.socket == nil || !os.SameFile(st, m.socket) {
		return fmt.Errorf("master socket unknown or replaced")
	}
	conn, e := net.DialTimeout("unix", m.Socket, time.Second)
	if e != nil {
		return fmt.Errorf("master unavailable; manual reconnect required")
	}
	defer conn.Close()
	raw, e := conn.(*net.UnixConn).SyscallConn()
	if e != nil {
		return e
	}
	var peer *unix.Ucred
	var pe error
	e = raw.Control(func(fd uintptr) { peer, pe = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) })
	if e != nil || pe != nil || peer.Pid != int32(m.PID) || peer.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("master peer identity refused")
	}
	if exec.CommandContext(c, "/usr/bin/ssh", "-F", "/dev/null", "-S", m.Socket, "-O", "check", "unused").Run() != nil {
		return fmt.Errorf("master authentication unverified")
	}
	return nil
}
func (m *master) start(ctx context.Context) {
	defer close(m.done)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	cfg := m.request.Settings
	args := []string{"-M", "-N", "-T", "-S", m.Socket, "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no", "-o", "ControlPersist=no", "-o", "ClearAllForwardings=yes", "-o", "PermitLocalCommand=no", "-o", "ConnectTimeout=8", "-p", strconv.Itoa(cfg.Port)}
	if cfg.SSHConfig != "" {
		args = append(args, "-F", cfg.SSHConfig)
	}
	if cfg.Key != "" {
		args = append(args, "-i", cfg.Key)
	}
	args = append(args, "--", cfg.User+"@"+cfg.Host)
	cmd := exec.Command("/usr/bin/ssh", args...)
	exe, e := os.Executable()
	if e != nil {
		m.failed()
		return
	}
	cmd.Env = append(os.Environ(), "SSH_ASKPASS_REQUIRE=force", "SSH_ASKPASS="+exe, "BURROW_ASKPASS=1", "BURROW_PROMPT_SOCKET="+cfg.PromptSocket, "SSH_AUTH_SOCK="+cfg.Agent)
	// Parent-death signal is attached to this locked spawning thread.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL, Setpgid: true}
	cmd.WaitDelay = time.Second
	m.mu.Lock()
	if ctx.Err() != nil {
		m.mu.Unlock()
		m.failed()
		return
	}
	e = cmd.Start()
	if e != nil {
		m.mu.Unlock()
		m.failed()
		return
	}
	m.cmd = cmd
	m.PID = cmd.Process.Pid
	m.mu.Unlock()
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-exited:
			m.failed()
			return
		case <-ctx.Done():
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-exited
			return
		case <-timer.C:
			m.mu.Lock()
			pending := m.State == "connecting"
			m.mu.Unlock()
			if pending {
				syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				<-exited
				m.failed()
				return
			}
		case <-tick.C:
			m.mu.Lock()
			if m.State == "connecting" {
				st, e := os.Lstat(m.Socket)
				if e == nil && st.Mode()&os.ModeSocket != 0 {
					m.socket = st
					if m.verify() == nil {
						m.State = "connected"
						m.Connected = mono()
						m.manager.milestone("connection authenticated")
					}
				}
			}
			m.mu.Unlock()
		}
	}
}
func (m *master) failed() {
	m.mu.Lock()
	m.State = "lost"
	m.mu.Unlock()
	m.manager.milestone("connection lost; reconnect manually")
}
func (m *master) close() error {
	m.mu.Lock()
	if m.State == "closed" {
		m.mu.Unlock()
		return nil
	}
	c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	e := launch.VerifyReservation(c, m.request.Settings.Workspace, m.dir)
	cancel()
	if e != nil {
		m.mu.Unlock()
		return e
	}
	if st, err := os.Lstat(m.Socket); err == nil && (m.socket == nil || !os.SameFile(st, m.socket)) {
		m.mu.Unlock()
		return fmt.Errorf("unknown master preserved")
	}
	m.cancel()
	m.mu.Unlock()
	select {
	case <-m.done:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("termination uncertain")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.State == "closed" {
		return nil
	}
	// The owned process has ended, but filesystem cleanup can still refuse.
	// Keep that uncertainty inspectable so the operator can review another close.
	m.State = "cleanup-pending"
	c, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e = launch.VerifyReservation(c, m.request.Settings.Workspace, m.dir); e != nil {
		return e
	}
	if st, err := os.Lstat(m.Socket); err == nil {
		if m.socket == nil || !os.SameFile(st, m.socket) {
			return fmt.Errorf("unknown socket preserved")
		}
		if e = os.Remove(m.Socket); e != nil {
			return e
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if e = os.Remove(m.dir.Name()); e != nil {
		return fmt.Errorf("unidentified runtime contents preserved")
	}
	m.State = "closed"
	m.dir.Close()
	m.manager.milestone("selected connection closed")
	return nil
}
func (s *manager) connect(raw, review, runID string) (connectionState, error) {
	dispatch := mono()
	if digest(raw) != review {
		return connectionState{}, fmt.Errorf("request changed; review again")
	}
	r, e := decodeRequest(raw)
	if e != nil {
		return connectionState{}, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || r.Generation != s.Generation || r.Session != s.Session || r.Settings.Workspace != s.workspace || runID == "" {
		return connectionState{}, fmt.Errorf("owner/request binding refused")
	}
	if s.connections == nil {
		s.connections = map[string]*master{}
	}
	if _, ok := s.connections[r.ID]; ok {
		return connectionState{}, fmt.Errorf("duplicate creation identity; inspect existing attempt")
	}
	c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dir, e := launch.ReserveConnection(c, s.workspace, r.Settings.Name)
	if e != nil {
		return connectionState{}, e
	}
	ctx, stop := context.WithCancel(context.Background())
	m := &master{connectionState: connectionState{ID: r.ID, Name: r.Settings.Name, RunID: runID, State: "connecting", Socket: filepath.Join(dir.Name(), "master"), Dispatch: dispatch}, request: r, dir: dir, cancel: stop, done: make(chan struct{}), manager: s}
	s.connections[r.ID] = m
	initial := m.connectionState
	go m.start(ctx)
	s.milestone("connection request dispatched")
	return initial, nil
}
func (s *manager) milestone(message string) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if s.logs < 200 {
		s.log.Info(message)
	}
	if s.logs == 200 {
		s.log.Warn("diagnostic budget exhausted; further milestones suppressed")
	}
	if s.logs <= 200 {
		s.logs++
	}
}
