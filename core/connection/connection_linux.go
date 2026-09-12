// Package connection owns shell-free OpenSSH masters in retained Hovel sessions.
package connection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Bochner/burrow/core/launch"
	"github.com/vibepwners/hovel/sdk/go/hovel"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/sys/unix"
)

type Config struct {
	Workspace  string `json:"workspace"`
	Name       string `json:"name"`
	Host       string `json:"host"`
	User       string `json:"user"`
	Port       int    `json:"port"`
	Key        string `json:"key,omitempty"`
	Agent      string `json:"agent,omitempty"`
	KnownHosts string `json:"knownHosts"`
	Trust      string `json:"trust,omitempty"`
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
	if !regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$`).MatchString(c.User) {
		return fmt.Errorf("invalid SSH username")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port must be 1–65535")
	}
	if c.Trust != "" && !regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`).MatchString(c.Trust) {
		return fmt.Errorf("trust must be a SHA256 host-key fingerprint")
	}
	for _, p := range []string{c.Key, c.Agent, c.KnownHosts} {
		if p != "" && (!filepath.IsAbs(p) || filepath.Clean(p) != p || strings.ContainsAny(p, "\x00\r\n\t\"%")) {
			return fmt.Errorf("key, agent and known-hosts paths must be absolute canonical paths without SSH expansions or control characters")
		}
	}
	if c.Key == "" && c.Agent == "" {
		return fmt.Errorf("select --key PATH or --agent PATH (SSH_AUTH_SOCK is the default)")
	}
	return nil
}

// Trust scans public host keys only; it never sends an authentication credential.
// Existing trust is authoritative: an approval cannot override a changed key.
func (c Config) hostKeys(ctx context.Context) ([]byte, error) {
	store, e := launch.TrustStore(ctx, c.Workspace)
	if e != nil {
		return nil, e
	}
	defer store.Close()
	files := []string{store.Name()}
	if _, e := os.Stat(c.KnownHosts); e == nil {
		files = append(files, c.KnownHosts)
	} else if !os.IsNotExist(e) {
		return nil, fmt.Errorf("cannot read selected known-hosts file")
	}
	check, e := knownhosts.New(files...)
	if e != nil {
		return nil, fmt.Errorf("cannot parse selected known-hosts file")
	}
	address := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	// OpenSSH handles hashed names, patterns, revocations and algorithm choice.
	// Known hosts need no extra unauthenticated connections just to rediscover keys.
	var existing []byte
	for _, path := range files {
		cmd := exec.CommandContext(ctx, "/usr/bin/ssh-keygen", "-F", knownhosts.Normalize(address), "-f", path)
		var found limitedBuffer
		cmd.Stdout = &found
		err := cmd.Run()
		if err == nil {
			existing = append(existing, found.data...)
		} else if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	if len(existing) > 0 {
		return existing, nil
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/ssh-keyscan", "-T", "3", "-p", strconv.Itoa(c.Port), "--", c.Host)
	var scan limitedBuffer
	cmd.Stdout = &scan
	if e := cmd.Run(); e != nil {
		return nil, fmt.Errorf("host-key discovery failed; no authentication attempted")
	}
	var accepted []byte
	var approved []byte
	var fingerprints []string
	for _, line := range strings.Split(string(scan.data), "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		key, _, _, _, e := ssh.ParseAuthorizedKey([]byte(strings.Join(fields[1:], " ")))
		if e != nil {
			continue
		}
		fingerprint := ssh.FingerprintSHA256(key)
		fingerprints = append(fingerprints, fingerprint)
		trusted := false
		if check != nil {
			e = check(address, &net.TCPAddr{IP: net.ParseIP(c.Host), Port: c.Port}, key)
			if e == nil {
				trusted = true
			} else {
				var changed *knownhosts.KeyError
				if !errors.As(e, &changed) {
					return nil, fmt.Errorf("changed or revoked host key; refusing authentication")
				}
				for _, want := range changed.Want {
					if want.Key.Type() == key.Type() {
						return nil, fmt.Errorf("changed host key; refusing authentication even with --trust")
					}
				}
				// A server may offer additional algorithms. Existing trust for a different
				// algorithm is not a key change and does not approve these extra keys.
				if len(changed.Want) > 0 {
					continue
				}
			}
		}
		if trusted || c.Trust == fingerprint {
			line := []byte(knownhosts.Normalize(address) + " " + string(ssh.MarshalAuthorizedKey(key)))
			accepted = append(accepted, line...)
			if !trusted {
				approved = append(approved, line...)
			}
		}
	}
	if len(accepted) == 0 {
		return nil, fmt.Errorf("unknown host; verify a fingerprint independently, then repeat with --trust FINGERPRINT: %s", strings.Join(fingerprints, " "))
	}
	if len(approved) > 0 {
		if _, e = store.Write(approved); e != nil {
			return nil, fmt.Errorf("host approval could not be saved")
		}
		if e = store.Sync(); e != nil {
			return nil, fmt.Errorf("host approval sync failed")
		}
	}
	return accepted, nil
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
	mu      sync.Mutex
	tail    string
	changed bool
}

func (f *sshFailure) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.tail + string(p)
	f.changed = f.changed || strings.Contains(s, "HOST IDENTIFICATION HAS CHANGED") || strings.Contains(s, "REVOKED HOST KEY")
	f.tail = s[max(0, len(s)-40):]
	return len(p), nil
}
func (f *sshFailure) detail() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.changed {
		return "changed or revoked host key; refusing authentication even with --trust"
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
	mu        sync.Mutex
	config    Config
	dir       *os.File
	state     State
	master    *exec.Cmd
	socket    os.FileInfo
	trustFile os.FileInfo
	done      chan struct{}
	cancel    context.CancelFunc
	closed    bool
	log       *hovel.Logger
	logs      int
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
	auth, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	keys, e := s.config.hostKeys(auth)
	s.mu.Lock()
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
	args := []string{"-F", "/dev/null", "-M", "-N", "-T", "-S", s.state.Socket, "-o", "ControlPersist=no", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "GlobalKnownHostsFile=/dev/null", "-o", "UserKnownHostsFile=" + strconv.Quote(path), "-o", "UpdateHostKeys=no", "-o", "ClearAllForwardings=yes", "-o", "ConnectTimeout=8", "-o", "ServerAliveInterval=2", "-o", "ServerAliveCountMax=2", "-o", "PreferredAuthentications=publickey", "-p", strconv.Itoa(s.config.Port), "-l", s.config.User}
	if s.config.Key != "" {
		args = append(args, "-i", s.config.Key, "-o", "IdentitiesOnly=yes")
	}
	agent := s.config.Agent
	if agent == "" {
		agent = "none"
	}
	args = append(args, "-o", "IdentityAgent="+strconv.Quote(agent), "--", s.config.Host)
	s.master = exec.Command("/usr/bin/ssh", args...)
	failure := &sshFailure{}
	s.master.Stderr = failure
	s.master.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "SSH_ASKPASS_REQUIRE=never"}
	s.master.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
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
			s.state.State = "lost"
			s.state.Detail = failure.detail()
			s.milestone("connection lost")
			s.mu.Unlock()
			return
		case <-ctx.Done():
			master.Process.Kill()
			<-exited
			return
		case <-time.After(100 * time.Millisecond):
			s.mu.Lock()
			if s.state.State == "connecting" {
				st, err := os.Lstat(s.state.Socket)
				if err == nil && st.Mode()&os.ModeSocket != 0 && st.Sys().(*syscall.Stat_t).Uid == uint32(os.Getuid()) {
					s.socket = st
					if s.checkMaster() == nil {
						s.state.State = "connected"
						s.state.SocketInode = st.Sys().(*syscall.Stat_t).Ino
						s.state.Detail = "shell-free master"
						s.milestone("connected")
					}
				}
				if auth.Err() != nil {
					master.Process.Kill()
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
	s.cancel()
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
	for path, expected := range map[string]os.FileInfo{s.state.Socket: s.socket, filepath.Join(s.dir.Name(), "known_hosts"): s.trustFile} {
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
