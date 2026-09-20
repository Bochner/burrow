package launch

import (
	"bufio"
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
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type fileID struct{ Device, Inode uint64 }

func fileIdentity(path string) (fileID, error) {
	var s unix.Stat_t
	e := unix.Lstat(path, &s)
	return fileID{uint64(s.Dev), s.Ino}, e
}

type record struct {
	PID         int    `json:"pid"`
	Boot        string `json:"boot"`
	Ticks       string `json:"ticks"`
	SHA         string `json:"sha256"`
	Workspace   string `json:"workspace"`
	Started     string `json:"startedAt"`
	WorkspaceID fileID `json:"workspaceIdentity"`
	RuntimeID   fileID `json:"runtimeIdentity"`
	SocketID    fileID `json:"socketIdentity"`
}
type Info struct {
	Workspace string `json:"workspacePath"`
	PID       int    `json:"pid"`
	Started   string `json:"startedAt"`
	Health    string `json:"health"`
	Access    string `json:"access"`
}

func processIdentity(pid int) (record, error) {
	defer Phase("status.identity")()
	if pid <= 0 {
		return record{}, fmt.Errorf("invalid PID")
	}
	b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if e != nil {
		return record{}, e
	}
	at := strings.LastIndex(string(b), ")")
	if at < 0 {
		return record{}, fmt.Errorf("invalid process identity")
	}
	fields := strings.Fields(string(b)[at+1:])
	if len(fields) < 20 || fields[0] == "Z" {
		return record{}, fmt.Errorf("process ended")
	}
	boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		return record{}, e
	}
	sha, e := digest(fmt.Sprintf("/proc/%d/exe", pid))
	if e != nil {
		return record{}, e
	}
	if sha != ExecutableSHA {
		return record{}, fmt.Errorf("process executable does not match independent pin")
	}
	return record{PID: pid, Boot: strings.TrimSpace(string(boot)), Ticks: fields[19], SHA: sha}, nil
}

func rpc(conn net.Conn, method string, out any) error {
	return rpcBody(conn, method, []byte("{}"), out)
}
func rpcBody(conn net.Conn, method string, bodyInput []byte, out any) error {
	// A raw request uses exactly this verified connection; never implicit redial/retry.
	request, _ := http.NewRequest(http.MethodPost, "http://localhost/hovel.daemon.v1.DaemonService/"+method, bytes.NewReader(bodyInput))
	request.Header.Set("Content-Type", "application/json")
	if e := request.Write(conn); e != nil {
		return e
	}
	response, e := http.ReadResponse(bufio.NewReader(conn), request)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("Hovel %s rejected: HTTP %d", method, response.StatusCode)
	}
	limit := int64(1 << 20)
	if method == "Snapshot" {
		// The pinned public Snapshot exports all operations and retained logs,
		// even when the caller only needs one chain's configuration.
		// ponytail: 64 MiB snapshot ceiling; use a scoped config RPC when Hovel exposes one.
		limit = 64 << 20
	}
	body, e := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if e != nil {
		return e
	}
	if int64(len(body)) > limit {
		return fmt.Errorf("Hovel %s response exceeds %d-byte limit", method, limit)
	}
	return json.Unmarshal(body, out)
}

func inspect(ctx context.Context, workspace string, pid int) (record, Info, error) {
	var empty record
	var info Info
	fd, e := unix.PidfdOpen(pid, 0)
	if e != nil {
		return empty, info, e
	}
	defer unix.Close(fd)
	before, e := processIdentity(pid)
	if e != nil {
		return empty, info, e
	}
	endpoint := filepath.Join(workspace, "hoveld.sock")
	s, e := os.Lstat(endpoint)
	if e != nil {
		return empty, info, e
	}
	if s.Mode()&os.ModeSocket == 0 || s.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) || s.Mode().Perm()&0077 != 0 {
		return empty, info, refuse(endpoint, "expected owner-only Unix socket")
	}
	socketID, e := fileIdentity(endpoint)
	if e != nil {
		return empty, info, e
	}
	rpcPhase := Phase("status.rpc")
	conn, e := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", endpoint)
	if e != nil {
		return empty, info, e
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	raw, e := conn.(*net.UnixConn).SyscallConn()
	if e != nil {
		return empty, info, e
	}
	var peer *unix.Ucred
	var peerErr error
	e = raw.Control(func(fd uintptr) { peer, peerErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) })
	if e != nil {
		return empty, info, e
	}
	if peerErr != nil {
		return empty, info, peerErr
	}
	if int(peer.Pid) != pid || peer.Uid != uint32(os.Getuid()) {
		return empty, info, refuse(endpoint, "socket belongs to another process/user")
	}
	e = rpc(conn, "GetDaemonInfo", &info)
	rpcPhase()
	if e != nil {
		return empty, info, e
	}
	if info.PID != pid || info.Workspace != workspace || info.Started == "" {
		return empty, info, refuse(endpoint, "public workspace/process identity mismatch")
	}
	after, e := processIdentity(pid)
	if e != nil {
		return empty, info, e
	}
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	// Go's preemption signal can interrupt even a zero-timeout poll. Retry
	// that syscall without restarting identity verification or any operation.
	for {
		_, e = unix.Poll(poll, 0)
		if e != unix.EINTR || ctx.Err() != nil {
			break
		}
	}
	if e != nil {
		return empty, info, e
	}
	if before != after || poll[0].Revents != 0 {
		return empty, info, fmt.Errorf("daemon identity changed during verification")
	}
	current, e := fileIdentity(endpoint)
	if e != nil || current != socketID {
		return empty, info, refuse(endpoint, "socket replaced during verification")
	}
	before.Workspace = workspace
	before.Started = info.Started
	before.SocketID = socketID
	before.WorkspaceID, e = fileIdentity(workspace)
	if e != nil {
		return empty, info, e
	}
	before.RuntimeID, e = fileIdentity(filepath.Join(workspace, "burrow"))
	return before, info, e
}

// Status never starts or replaces a daemon. Every invocation revalidates the
// selected paths and the actual peer, including after a prior disconnection.
func Status(ctx context.Context, workspace string) (Info, error) {
	defer Phase("status")()
	var info Info
	dir, e := directory(workspace, false, false)
	if e != nil {
		return info, e
	}
	defer dir.Close()
	runtime, e := directory(filepath.Join(workspace, "burrow"), false, true)
	if e != nil {
		return info, e
	}
	defer runtime.Close()
	if e = absent(filepath.Join(workspace, "burrow-launch.json.pending")); e != nil {
		return info, e
	}
	path := filepath.Join(workspace, "burrow-launch.json")
	b, e := regular(path, 0600, 8192)
	if e != nil {
		return info, refuse(path, e.Error())
	}
	var expected record
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&expected); e != nil {
		return info, refuse(path, "incomplete or invalid receipt")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return info, refuse(path, "invalid trailing receipt data")
	}
	if expected.Workspace != workspace {
		return info, refuse(path, "receipt cannot redirect selected workspace")
	}
	observed, info, e := inspect(ctx, workspace, expected.PID)
	if e != nil {
		return Info{}, refuse(path, e.Error())
	}
	if expected != observed {
		return Info{}, refuse(path, "stale receipt or replaced workspace/runtime/socket")
	}
	// Confirm containing paths still identify the descriptors validated above.
	for _, d := range []*os.File{dir, runtime} {
		st, _ := d.Stat()
		now, e := os.Lstat(d.Name())
		if e != nil || !os.SameFile(st, now) {
			return Info{}, refuse(d.Name(), "directory replaced")
		}
	}
	return info, nil
}

// Open is the shared CLI/TUI setup command. Starting a daemon is explicit here;
// later status/revalidation failure never silently starts another one.
func Open(ctx context.Context, o Options) (Info, error) {
	defer Phase("open")()
	var info Info
	endpoint := filepath.Join(o.Workspace, "hoveld.sock")
	if len([]byte(endpoint)) > 103 {
		return info, refuse(endpoint, "daemon endpoint is too long; choose a shorter workspace path")
	}
	dir, e := directory(o.Workspace, true, false)
	if e != nil {
		return info, e
	}
	defer dir.Close()
	if e = lock(ctx, dir); e != nil {
		return info, e
	}
	for _, n := range []string{"burrow-launch.json.pending"} {
		if e = absent(filepath.Join(o.Workspace, n)); e != nil {
			return info, e
		}
	}
	receipt := filepath.Join(o.Workspace, "burrow-launch.json")
	if _, e = os.Lstat(receipt); e == nil {
		return Status(ctx, o.Workspace)
	} else if !os.IsNotExist(e) {
		return info, e
	}
	// Never let a fresh receipt authorize old state. Hovel settings/artifacts are
	// preserved; runtime reservations need operator investigation before launch.
	for _, n := range []string{"hoveld.sock", "daemon.json", "daemon.lock", "burrow"} {
		if e = absent(filepath.Join(o.Workspace, n)); e != nil {
			return info, e
		}
	}
	executable, e := install(ctx, o)
	if e != nil {
		return info, e
	}
	runtime, e := directory(filepath.Join(o.Workspace, "burrow"), true, true)
	if e != nil {
		return info, e
	}
	runtime.Close()
	// Reserve publication before spawning: death at any later point is a visible
	// incomplete launch, never an invitation to adopt a surviving process.
	marker, e := os.OpenFile(receipt+".pending", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return info, e
	}
	if e = marker.Sync(); e != nil {
		marker.Close()
		return info, e
	}
	if e = dir.Sync(); e != nil {
		marker.Close()
		return info, e
	}
	logPath := filepath.Join(o.Workspace, "burrow-launch.log")
	log, e := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		marker.Close()
		return info, refuse(logPath, e.Error())
	}
	defer log.Close()
	cmd := exec.Command(executable, "daemon", "serve", "--workspace", o.Workspace, "--listen", endpoint)
	cmd.Env = []string{}
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "HOVEL_") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Dir = o.Workspace
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if e = cmd.Start(); e != nil {
		marker.Close()
		return info, e
	}
	success := false
	defer func() {
		if !success {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		} else {
			go cmd.Wait()
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	var observed record
	for time.Now().Before(deadline) {
		observed, info, e = inspect(ctx, o.Workspace, cmd.Process.Pid)
		if e == nil {
			break
		}
		select {
		case <-ctx.Done():
			marker.Close()
			return Info{}, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	if e != nil {
		marker.Close()
		return Info{}, refuse(receipt, "startup verification failed; inspect "+strconv.Quote(logPath)+": "+e.Error())
	}
	b, e := json.Marshal(observed)
	if e == nil {
		_, e = marker.Write(b)
	}
	if e == nil {
		e = marker.Sync()
	}
	closeErr := marker.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return Info{}, e
	}
	if e = os.Link(receipt+".pending", receipt); e != nil {
		return Info{}, e
	}
	if e = dir.Sync(); e != nil {
		return Info{}, e
	}
	if e = os.Remove(receipt + ".pending"); e != nil {
		return Info{}, e
	}
	if e = dir.Sync(); e != nil {
		return Info{}, e
	}
	success = true
	return Status(ctx, o.Workspace)
}
