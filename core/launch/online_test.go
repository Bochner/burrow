package launch_test

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Bochner/burrow/core/launch"
)

var wheel = flag.String("wheel", "", "declared pinned Hovel wheel")

type pinnedDownload struct{ requests int }

func (d *pinnedDownload) RoundTrip(r *http.Request) (*http.Response, error) {
	d.requests++
	if r.URL.String() != launch.PackageURL {
		return nil, fmt.Errorf("unexpected download URL")
	}
	body, err := os.Open(filepath.Join(os.Getenv("TEST_SRCDIR"), os.Getenv("TEST_WORKSPACE"), *wheel))
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Body: body, Header: make(http.Header)}, nil
}
func TestFirstOnlineWorkspace(t *testing.T) {
	// Only the network boundary is substituted: real pinned package, installation,
	// daemon launch and public verification still run through launch.Open.
	root, err := os.MkdirTemp("", "bo-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("HOME", root)
	transport := &pinnedDownload{}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = previous }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	info, err := launch.Open(ctx, launch.Options{Workspace: filepath.Join(root, "w")})
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(info.PID, syscall.SIGTERM)
	verified, err := launch.Status(ctx, info.Workspace)
	if err != nil || verified.PID != info.PID || transport.requests != 1 {
		t.Fatalf("first download launch: %+v, %v, requests %d", verified, err, transport.requests)
	}
	cached, err := launch.Open(ctx, launch.Options{Workspace: info.Workspace, Offline: true})
	if err != nil || cached.PID != info.PID || transport.requests != 1 {
		t.Fatalf("cache reuse: %+v, %v", cached, err)
	}
	// SQLite's Unix WAL lifetime lock prevents another public Hovel client
	// from truncating shared memory while this daemon still maps it. Query
	// from this separate process; do not acquire or change any database lock.
	shared, err := os.OpenFile(filepath.Join(info.Workspace, "workspace.db-shm"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	lock := syscall.Flock_t{Type: syscall.F_WRLCK, Whence: 0, Start: 128, Len: 1}
	if err := syscall.FcntlFlock(shared.Fd(), syscall.F_GETLK, &lock); err != nil {
		t.Fatal(err)
	}
	if lock.Type != syscall.F_RDLCK || int(lock.Pid) != info.PID {
		t.Fatalf("live Hovel daemon %d has lost SQLite WAL lifetime lock: type=%d owner=%d", info.PID, lock.Type, lock.Pid)
	}
}
