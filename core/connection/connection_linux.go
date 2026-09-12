// Package connection owns shell-free OpenSSH masters in retained Hovel sessions.
package connection

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Bochner/burrow/core/launch"
	"github.com/vibepwners/hovel/sdk/go/hovel"
	"golang.org/x/sys/unix"
)

type Config struct {
	identities     []string
	identitiesOnly bool
	Workspace      string `json:"workspace"`
	Name           string `json:"name"`
	Host           string `json:"host"`
	User           string `json:"user"`
	Port           int    `json:"port"`
	Key            string `json:"key,omitempty"`
	Agent          string `json:"agent,omitempty"`
	AgentExplicit  bool   `json:"agentExplicit,omitempty"`
	KnownHosts     string `json:"knownHosts"`
	Trust          string `json:"trust,omitempty"`
	SSHConfig      string `json:"sshConfig,omitempty"`
	Jump           string `json:"jump,omitempty"`
	Prompt         bool   `json:"prompt,omitempty"`
	PromptSocket   string `json:"promptSocket,omitempty"`
}
type State struct {
	Name        string `json:"name"`
	Host        string `json:"host"`
	User        string `json:"user"`
	Port        int    `json:"port"`
	State       string `json:"state"`
	Socket      string `json:"socket"`
	Session     string `json:"session"`
	OwnerPID    int    `json:"ownerPID"`
	MasterPID   int    `json:"masterPID"`
	SocketInode uint64 `json:"socketInode"`
	Detail      string `json:"detail"`
}

func (c Config) Validate() error {
	if _, e := launch.ConnectionPath(c.Workspace, c.Name); e != nil {
		return e
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:-]{0,252}$`).MatchString(c.Host) {
		return fmt.Errorf("host must be a literal hostname or IP address")
	}
	if c.User != "-" && !regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$`).MatchString(c.User) {
		return fmt.Errorf("invalid SSH username")
	}
	if c.Port < 0 || c.Port > 65535 {
		return fmt.Errorf("port must be 1–65535")
	}
	if c.Trust != "" && !regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`).MatchString(c.Trust) {
		return fmt.Errorf("trust must be a SHA256 host-key fingerprint")
	}
	for _, p := range []string{c.Key, c.Agent, c.KnownHosts, c.SSHConfig, c.PromptSocket} {
		if p != "" && (!filepath.IsAbs(p) || filepath.Clean(p) != p || strings.ContainsAny(p, "\x00\r\n\t\"%")) {
			return fmt.Errorf("key, agent and known-hosts paths must be absolute canonical paths without SSH expansions or control characters")
		}
	}
	if c.Jump != "" && !regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.:@,\[\]-]{0,1000}$`).MatchString(c.Jump) {
		return fmt.Errorf("invalid jump host; use [USER@]HOST[:PORT], separated by commas")
	}
	return nil
}

// Never retain raw SSH stderr, banners or credential-helper output.
type limitedBuffer struct{ data []byte }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	b.data = append(b.data, p[:min(len(p), max(0, 32768-len(b.data)))]...)
	return n, nil
}

// Classify only fixed OpenSSH diagnostics; never expose or persist banners/raw stderr.
type sshFailure struct {
	mu          sync.Mutex
	tail        string
	changed     bool
	trust       bool
	credentials bool
	transport   bool
}

func (f *sshFailure) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.tail + string(p)
	f.changed = f.changed || strings.Contains(s, "HOST IDENTIFICATION HAS CHANGED") || strings.Contains(s, "REVOKED HOST KEY")
	f.trust = f.trust || strings.Contains(s, "Host key verification failed")
	f.credentials = f.credentials || strings.Contains(s, "interactive authentication unavailable") || strings.Contains(s, "authentication frontend unavailable")
	f.transport = f.transport || strings.Contains(s, "Connection refused") || strings.Contains(s, "Connection timed out") || strings.Contains(s, "Could not resolve hostname") || strings.Contains(s, "administratively prohibited")
	f.tail = s[max(0, len(s)-40):]
	return len(p), nil
}
func (f *sshFailure) detail() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.changed {
		return "changed or revoked host key; refusing authentication even with --trust"
	}
	if f.trust {
		return "host key verification failed: unknown host approval rejected or unavailable; use --prompt to verify the fingerprint"
	}
	if f.credentials {
		return "SSH authentication failed: terminal entry unavailable; use --prompt for passwords/encrypted keys or select an accessible key/agent"
	}
	if f.transport {
		return "SSH connection or jump failed; check host/port, reachability and jump forwarding permission, then retry explicitly"
	}
	return "SSH ended: authentication failed, cancelled, or transport lost; reconnect explicitly"
}

type Module struct{}

func (Module) Info() hovel.Info {
	return hovel.Info{Name: "burrow-connection", Version: "0.1.0", Type: hovel.TypeSurvey, Tags: []string{"dangerous"}, Summary: "Create a retained named SSH connection"}
}
func (Module) Schema() hovel.Schema {
	return hovel.Schema{ChainConfig: []hovel.Requirement{hovel.Req("connection", "string", "Non-secret explicit connection settings")}}
}
func (Module) Run(ctx *hovel.Context) (hovel.Result, error) {
	var c Config
	d := json.NewDecoder(strings.NewReader(ctx.InputString("connection", "")))
	d.DisallowUnknownFields()
	if e := d.Decode(&c); e != nil {
		return hovel.Result{}, fmt.Errorf("invalid connection settings")
	}
	if d.Decode(new(any)) != io.EOF {
		return hovel.Result{}, fmt.Errorf("invalid trailing connection settings")
	}
	if e := c.Validate(); e != nil {
		return hovel.Result{}, e
	}
	check, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, e := launch.Status(check, c.Workspace)
	if e != nil {
		return hovel.Result{}, e
	}
	if os.Getppid() != info.PID {
		return hovel.Result{}, fmt.Errorf("connection module must be launched by the verified workspace daemon")
	}
	dir, e := launch.ReserveConnection(check, c.Workspace, c.Name)
	if e != nil {
		return hovel.Result{}, e
	}
	s := &owner{config: c, dir: dir, done: make(chan struct{}), log: ctx.Log}
	s.state = State{Name: c.Name, Host: c.Host, User: c.User, Port: c.Port, State: "connecting", Socket: filepath.Join(dir.Name(), "master"), OwnerPID: os.Getpid()}
	if _, e = ctx.OpenSession(s, hovel.WithName(c.Name), hovel.WithKind("connection"), hovel.WithTransport("ssh")); e != nil {
		s.Close("registration failed")
		return hovel.Result{}, e
	}
	return hovel.Ok(nil, hovel.WithSummary("Connection attempt retained; inspect live owner state")), nil
}

type owner struct {
	mu         sync.Mutex
	config     Config
	dir        *os.File
	state      State
	master     *exec.Cmd
	socket     os.FileInfo
	trustFile  os.FileInfo
	configFile os.FileInfo
	trustBytes int
	done       chan struct{}
	cancel     context.CancelFunc
	closed     bool
	log        *hovel.Logger
	logs       int
}

func (s *owner) milestone(message string) {
	// ponytail: 200 lifetime diagnostics, then one warning; remove when Hovel drains retained logs safely (#30).
	if s.logs < 200 {
		s.log.Info(message, "connection", s.config.Name)
	}
	if s.logs == 200 {
		s.log.Warn("diagnostic budget exhausted; further milestones suppressed", "connection", s.config.Name)
	}
	if s.logs <= 200 {
		s.logs++
	}
}
func (s *owner) Open() error {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.connect(ctx)
	return nil
}
func (s *owner) connect(ctx context.Context) {
	defer close(s.done)
	auth, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	s.mu.Lock()
	keys, e := s.trustSnapshot(auth)
	if e == nil {
		e = launch.VerifyReservation(auth, s.config.Workspace, s.dir)
	}
	if e != nil {
		s.state.State = "lost"
		s.state.Detail = e.Error()
		s.milestone("connection trust or setup refused")
		s.mu.Unlock()
		return
	}
	path := filepath.Join(s.dir.Name(), "known_hosts")
	file, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e == nil {
		_, e = file.Write(keys)
		s.trustBytes = len(keys)
		s.trustFile, _ = file.Stat()
		ce := file.Close()
		if e == nil {
			e = ce
		}
	}
	if e != nil {
		s.state.State = "lost"
		s.state.Detail = "private trust snapshot could not be created"
		s.mu.Unlock()
		return
	}
	config, e := s.sshConfig(auth, path)
	configPath := filepath.Join(s.dir.Name(), "ssh_config")
	if e == nil {
		var f *os.File
		f, e = os.OpenFile(configPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e == nil {
			_, e = f.Write(config)
			s.configFile, _ = f.Stat()
			ce := f.Close()
			if e == nil {
				e = ce
			}
		}
	}
	if e != nil {
		s.state.State = "lost"
		s.state.Detail = "SSH configuration refused: " + e.Error()
		s.mu.Unlock()
		return
	}
	args := []string{"-F", configPath, "-M", "-N", "-T", "-S", s.state.Socket, "--", "burrow-hop-0"}
	s.master = exec.Command("/usr/bin/ssh", args...)
	// A jump child may hold stderr open after the master exits. Bound Wait so
	// the owner can reap the entire process group on loss as well as on close.
	s.master.WaitDelay = 2 * time.Second
	failure := &sshFailure{}
	s.master.Stderr = failure
	executable, e := os.Executable()
	if e != nil {
		s.state.State = "lost"
		s.state.Detail = "authentication helper unavailable"
		s.mu.Unlock()
		return
	}
	s.master.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "SSH_ASKPASS_REQUIRE=force", "SSH_ASKPASS=" + executable, "BURROW_ASKPASS=1", "BURROW_PROMPT_SOCKET=" + s.config.PromptSocket, "BURROW_TRUST=" + s.config.Trust}
	s.master.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM, Setpgid: true}
	// Linux parent-death signals follow the spawning thread, not the Go process.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	e = s.master.Start()
	if e != nil {
		s.state.State = "lost"
		s.state.Detail = "could not start OpenSSH"
		s.mu.Unlock()
		return
	}
	s.state.MasterPID = s.master.Process.Pid
	master := s.master
	s.milestone("connecting")
	s.mu.Unlock()
	exited := make(chan struct{})
	go func() { master.Wait(); close(exited) }()
	for {
		select {
		case <-exited:
			s.mu.Lock()
			syscall.Kill(-master.Process.Pid, syscall.SIGKILL)
			s.state.State = "lost"
			s.state.Detail = failure.detail()
			if e := s.saveTrust(); e != nil {
				s.state.Detail = "host approval persistence failed; inspect workspace trust"
			}
			s.milestone("connection lost")
			s.mu.Unlock()
			return
		case <-ctx.Done():
			syscall.Kill(-master.Process.Pid, syscall.SIGKILL)
			<-exited
			return
		case <-time.After(100 * time.Millisecond):
			s.mu.Lock()
			if s.state.State == "connecting" {
				st, err := os.Lstat(s.state.Socket)
				if err == nil && st.Mode()&os.ModeSocket != 0 && st.Sys().(*syscall.Stat_t).Uid == uint32(os.Getuid()) {
					s.socket = st
					if s.checkMaster() == nil {
						if e := s.saveTrust(); e != nil {
							syscall.Kill(-master.Process.Pid, syscall.SIGKILL)
							s.mu.Unlock()
							continue
						}
						s.state.State = "connected"
						s.state.SocketInode = st.Sys().(*syscall.Stat_t).Ino
						s.state.Detail = "shell-free master"
						s.milestone("connected")
					}
				}
				if auth.Err() != nil {
					syscall.Kill(-master.Process.Pid, syscall.SIGKILL)
				}
			}
			s.mu.Unlock()
		}
	}
}
func (s *owner) checkMaster() error {
	if s.socket == nil {
		return fmt.Errorf("no verified master socket")
	}
	st, e := os.Lstat(s.state.Socket)
	if e != nil || !os.SameFile(st, s.socket) {
		return fmt.Errorf("master socket missing or replaced")
	}
	conn, e := net.DialTimeout("unix", s.state.Socket, time.Second)
	if e != nil {
		return fmt.Errorf("master unavailable")
	}
	defer conn.Close()
	raw, e := conn.(*net.UnixConn).SyscallConn()
	if e != nil {
		return e
	}
	var peer *unix.Ucred
	var pe error
	e = raw.Control(func(fd uintptr) { peer, pe = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) })
	if e != nil {
		return e
	}
	if pe != nil {
		return pe
	}
	if peer.Pid != int32(s.state.MasterPID) || peer.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("unidentified master peer")
	}
	check, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if exec.CommandContext(check, "/usr/bin/ssh", "-F", "/dev/null", "-S", s.state.Socket, "-O", "check", "unused").Run() != nil {
		return fmt.Errorf("master check failed; no fresh login attempted")
	}
	return nil
}
func (s *owner) Closed() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.closed }
func (s *owner) Write([]byte) error {
	return fmt.Errorf("connection control uses structured session commands")
}
func (s *owner) Read(time.Duration) ([]byte, error) { return nil, nil }
func (s *owner) ListPayloadCommands(hovel.PayloadCommandListRequest) ([]hovel.PayloadCommand, error) {
	return []hovel.PayloadCommand{{Name: "connection-status", ReadOnly: true}, {Name: "connection-close", Summary: "Close all owned connection resources after review"}}, nil
}
func (s *owner) RunPayloadCommand(req hovel.PayloadCommandRequest) (hovel.PayloadCommandResult, error) {
	if req.Reconnect != nil || req.InstalledPayloadID != "" || req.InputPath != "" || req.InputData != "" || len(req.Config) > 0 {
		return hovel.PayloadCommandResult{}, fmt.Errorf("unsupported connection command inputs")
	}
	if req.Command == "connection-close" && len(req.Args) == 1 && req.Args[0] == "confirm" {
		e := s.Close("operator confirmed connection-wide close")
		return hovel.PayloadCommandResult{Command: req.Command}, e
	}
	if req.Command != "connection-status" || len(req.Args) != 0 {
		return hovel.PayloadCommandResult{}, fmt.Errorf("unsupported connection command")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !s.closed {
		if e := launch.VerifyReservation(c, s.config.Workspace, s.dir); e != nil {
			return hovel.PayloadCommandResult{}, e
		}
		if s.state.State == "connected" {
			if e := s.checkMaster(); e != nil {
				s.state.State = "lost"
				s.state.Detail = e.Error()
			}
		}
	}
	s.milestone("connection inspected")
	b, e := json.Marshal(s.state)
	return hovel.PayloadCommandResult{Command: req.Command, Stdout: string(b)}, e
}
func (s *owner) Close(reason string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	e := launch.VerifyReservation(c, s.config.Workspace, s.dir)
	cancel()
	if e != nil {
		s.mu.Unlock()
		return e
	}
	// A substituted path is never touched, including by the master's exit cleanup.
	if st, err := os.Lstat(s.state.Socket); err == nil && (s.socket == nil || !os.SameFile(st, s.socket)) {
		s.mu.Unlock()
		return fmt.Errorf("unidentified master socket preserved; investigate manually")
	}
	if s.cancel != nil {
		s.cancel()
	} else {
		close(s.done)
	}
	s.mu.Unlock()
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("master termination unverified; runtime preserved")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e = launch.VerifyReservation(c, s.config.Workspace, s.dir); e != nil {
		return e
	}
	for path, expected := range map[string]os.FileInfo{s.state.Socket: s.socket, filepath.Join(s.dir.Name(), "known_hosts"): s.trustFile, filepath.Join(s.dir.Name(), "ssh_config"): s.configFile} {
		st, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || expected == nil || !os.SameFile(st, expected) {
			return fmt.Errorf("unidentified runtime file preserved at %q", path)
		}
		if e = os.Remove(path); e != nil {
			return fmt.Errorf("cleanup failed at %q", path)
		}
	}
	if e = os.Remove(s.dir.Name()); e != nil {
		return fmt.Errorf("cleanup incomplete at %q; inspect remaining files; saved data and evidence preserved", s.dir.Name())
	}
	s.closed = true
	s.state.State = "closed"
	s.state.Detail = "owned master and runtime removed; saved data and evidence retained"
	s.milestone("connection closed")
	return s.dir.Close()
}
