package launch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// ConnectionPath validates before any workspace or connection mutation.
func ConnectionPath(workspace, name string) (string, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,23}$`).MatchString(name) {
		return "", fmt.Errorf("name must be 1–24 ASCII letters/digits, underscores or hyphens, starting with a letter/digit")
	}
	if !filepath.IsAbs(workspace) || filepath.Clean(workspace) != workspace {
		return "", fmt.Errorf("workspace must be an absolute canonical path")
	}
	path := filepath.Join(workspace, "burrow", name, "master")
	if len(path) > 90 {
		return "", fmt.Errorf("SSH socket %q exceeds 90 bytes; select a shorter workspace path", path)
	}
	return path, nil
}

func ReserveConnection(ctx context.Context, workspace, name string) (*os.File, error) {
	path, e := ConnectionPath(workspace, name)
	if e != nil {
		return nil, e
	}
	if _, e = Status(ctx, workspace); e != nil {
		return nil, e
	}
	dir := filepath.Dir(path)
	if e = os.Mkdir(dir, 0700); e != nil {
		return nil, refuse(dir, "existing or unavailable connection reservation")
	}
	return directory(dir, false, true)
}

// ReserveManager uses a non-connection name so existing operator names remain valid.
func ReserveManager(ctx context.Context, workspace string) (*os.File, error) {
	if _, e := Status(ctx, workspace); e != nil {
		return nil, e
	}
	path := filepath.Join(workspace, "burrow", ".manager-v1")
	if e := os.Mkdir(path, 0700); e != nil {
		return nil, refuse(path, "manager reservation exists; inspect retained sessions; do not retry activation automatically")
	}
	return directory(path, false, true)
}

// VerifyReservation rechecks the enclosing launch receipt and the held directory.
func VerifyReservation(ctx context.Context, workspace string, dir *os.File) error {
	if _, e := Status(ctx, workspace); e != nil {
		return e
	}
	checked, e := directory(dir.Name(), false, true)
	if e != nil {
		return e
	}
	defer checked.Close()
	before, e := dir.Stat()
	if e != nil {
		return e
	}
	after, e := checked.Stat()
	if e != nil {
		return e
	}
	if !os.SameFile(before, after) {
		return refuse(dir.Name(), "connection directory replaced")
	}
	return nil
}

// TrustStore is ordinary OpenSSH known_hosts data, retained outside disposable
// runtime directories. Holding the file serializes approval and append.
func TrustStore(ctx context.Context, workspace string) (*os.File, error) {
	if _, e := Status(ctx, workspace); e != nil {
		return nil, e
	}
	path := filepath.Join(workspace, "burrow-known_hosts")
	fd, e := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_APPEND|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if e != nil {
		return nil, refuse(path, "cannot open trust store")
	}
	f := os.NewFile(uintptr(fd), path)
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || st.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) || st.Size() > 1<<20 {
		f.Close()
		return nil, refuse(path, "unsafe or oversized trust store")
	}
	if e = lock(ctx, f); e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}

// Call uses one verified Unix peer, with no reconnect/retry after validation.
func Call(ctx context.Context, workspace, method string, input, output any) error {
	info, e := Status(ctx, workspace)
	if e != nil {
		return e
	}
	conn, e := (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(workspace, "hoveld.sock"))
	if e != nil {
		return e
	}
	defer conn.Close()
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetDeadline(deadline)
	raw, e := conn.(*net.UnixConn).SyscallConn()
	if e != nil {
		return e
	}
	var peer *unix.Ucred
	var pe error
	if e = raw.Control(func(fd uintptr) { peer, pe = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); e != nil {
		return e
	}
	if pe != nil {
		return pe
	}
	if peer.Pid != int32(info.PID) || peer.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("refused changed Hovel peer")
	}
	if _, e = Status(ctx, workspace); e != nil {
		return e
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	return rpcInput(conn, method, input, output)
}

// HovelCLI preserves the public CLI's persisted throw plans, confirmation and
// launch-key policy. These contracts are not replicated in a Burrow plan store.
func HovelCLI(ctx context.Context, workspace string, args ...string) ([]byte, error) {
	cmd, e := hovelCommand(ctx, workspace, "run", args...)
	if e != nil {
		return nil, e
	}
	var out, diagnostic bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	if e = cmd.Run(); e != nil {
		return nil, fmt.Errorf("Hovel operation refused or failed; inspect the workspace throw history (no automatic retry)")
	}
	if _, e = Status(ctx, workspace); e != nil {
		return nil, e
	}
	return out.Bytes(), nil
}

// HovelShell prepares the verified interactive CLI. Verification is bounded by
// ctx; the terminal host owns the resulting process lifetime, not that deadline.
func HovelShell(ctx context.Context, workspace string) (*exec.Cmd, error) {
	return hovelCommand(ctx, workspace, "shell")
}

func hovelCommand(ctx context.Context, workspace, role string, args ...string) (*exec.Cmd, error) {
	if _, e := Status(ctx, workspace); e != nil {
		return nil, e
	}
	exe, e := install(ctx, Options{Offline: true})
	if e != nil {
		return nil, e
	}
	cmd := exec.Command(exe, append([]string{role, "--workspace", workspace}, args...)...)
	if role == "run" {
		cmd = exec.CommandContext(ctx, exe, cmd.Args[1:]...)
	}
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "HOVEL_") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Env = append(cmd.Env, "HOVEL_DAEMON_ENDPOINT="+filepath.Join(workspace, "hoveld.sock"))
	cmd.Dir = workspace
	return cmd, nil
}

// CacheModule publishes an immutable, private copy of this executable and its
// manifest for Hovel's public linked-package installer.
func CacheModule(ctx context.Context, manifest []byte) (string, error) {
	exe, e := os.Executable()
	if e != nil {
		return "", e
	}
	binary, e := os.ReadFile(exe)
	if e != nil {
		return "", e
	}
	cache, e := os.UserCacheDir()
	if e != nil {
		return "", e
	}
	path := filepath.Join(cache, "burrow", "modules", sum(binary), sum(manifest))
	dir, e := directory(path, true, true)
	if e != nil {
		return "", e
	}
	defer dir.Close()
	if e = lock(ctx, dir); e != nil {
		return "", e
	}
	for name, data := range map[string][]byte{"burrow": binary, "hovel-module.yaml": manifest} {
		mode := os.FileMode(0600)
		if name == "burrow" {
			mode = 0700
		}
		old, err := regular(filepath.Join(path, name), mode, packageLimit)
		if os.IsNotExist(err) {
			err = publish(dir, name, data, mode)
		} else if err == nil && !bytes.Equal(old, data) {
			err = refuse(path, "module cache differs from running build")
		}
		if err != nil {
			return "", err
		}
	}
	return path, nil
}

// RegisterModule installs this build through Hovel and verifies its live catalog.
// The workspace lock also serializes module registration with setup.
func RegisterModule(ctx context.Context, workspace, id string, manifest []byte) error {
	if _, err := Status(ctx, workspace); err != nil {
		return err
	}
	root, err := CacheModule(ctx, manifest)
	if err != nil {
		return err
	}
	dir, err := directory(workspace, false, false)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err = lock(ctx, dir); err != nil {
		return err
	}
	data, err := HovelCLI(ctx, workspace, "--", "module", "installed", "--json")
	if err != nil {
		return err
	}
	var inventory struct {
		Modules []struct {
			ID, Source        string
			Linked, Installed bool
		}
	}
	if err = json.Unmarshal(data, &inventory); err != nil {
		return fmt.Errorf("invalid Hovel module inventory")
	}
	matching := false
	for _, module := range inventory.Modules {
		if module.ID == id && module.Source == root && module.Linked && module.Installed {
			matching = true
		}
	}
	if !matching {
		if _, err = HovelCLI(ctx, workspace, "--", "module", "install", "--link", root, "--no-scripts", "--replace"); err != nil {
			return fmt.Errorf("Burrow module installation failed: %w", err)
		}
	}
	var catalog struct {
		Modules []struct {
			ID      string
			Enabled bool
		}
	}
	if err = Call(ctx, workspace, "GetModuleCatalog", struct{}{}, &catalog); err != nil {
		return fmt.Errorf("Burrow module discovery failed: %w", err)
	}
	for _, module := range catalog.Modules {
		if module.ID == id && module.Enabled {
			return nil
		}
	}
	return fmt.Errorf("Burrow module %s is unavailable in the daemon catalog", id)
}

func rpcInput(conn net.Conn, method string, input, output any) error {
	data, e := json.Marshal(input)
	if e != nil {
		return e
	}
	return rpcBody(conn, method, data, output)
}
