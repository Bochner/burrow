package launch

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReplaceOutdatedDaemon(t *testing.T) {
	workspace := t.TempDir()
	if err := os.Chmod(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, "burrow"), 0700); err != nil {
		t.Fatal(err)
	}
	endpoint := filepath.Join(workspace, "hoveld.sock")
	started := "2026-09-28T00:00:00Z"
	cmd := exec.Command(os.Args[0], "-test.run=^TestOutdatedDaemonHelper$")
	cmd.Env = append(os.Environ(), "BURROW_OUTDATED_HELPER="+workspace, "BURROW_OUTDATED_STARTED="+started)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Lstat(endpoint); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper socket did not appear")
		}
		time.Sleep(10 * time.Millisecond)
	}
	sha, err := digest(filepath.Join("/proc", fmt.Sprint(cmd.Process.Pid), "exe"))
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := processIdentity(cmd.Process.Pid, sha)
	if err != nil {
		t.Fatal(err)
	}
	receipt.Workspace, receipt.Started = workspace, started
	receipt.WorkspaceID, err = fileIdentity(workspace)
	if err != nil {
		t.Fatal(err)
	}
	receipt.RuntimeID, err = fileIdentity(filepath.Join(workspace, "burrow"))
	if err != nil {
		t.Fatal(err)
	}
	receipt.SocketID, err = fileIdentity(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(receipt)
	if err = os.WriteFile(filepath.Join(workspace, "burrow-launch.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"burrow-launch.log", "daemon.json", "daemon.lock"} {
		if err = os.WriteFile(filepath.Join(workspace, name), []byte("retired"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = Status(context.Background(), workspace); err == nil || !strings.Contains(err.Error(), "run restart --yes") {
		t.Fatalf("ordinary status must refuse the old pin with recovery guidance: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	replaced, err := ReplaceOutdated(ctx, workspace)
	if err != nil || !replaced {
		t.Fatalf("replace outdated daemon: replaced=%v err=%v", replaced, err)
	}
	entries, err := filepath.Glob(filepath.Join(workspace, ".burrow-upgrade-*", "burrow-launch.json"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("retired receipt was not preserved: %v %v", entries, err)
	}
	if _, err = os.Lstat(filepath.Join(workspace, "burrow-launch.json")); !os.IsNotExist(err) {
		t.Fatalf("active receipt remains after replacement: %v", err)
	}
}

func TestOutdatedDaemonHelper(t *testing.T) {
	workspace := os.Getenv("BURROW_OUTDATED_HELPER")
	if workspace == "" {
		return
	}
	endpoint := filepath.Join(workspace, "hoveld.sock")
	listener, err := net.Listen("unix", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(endpoint, 0600); err != nil {
		t.Fatal(err)
	}
	stopping := make(chan os.Signal, 1)
	signal.Notify(stopping, syscall.SIGTERM)
	go func() {
		<-stopping
		listener.Close()
		os.Exit(0)
	}()
	info := Info{Workspace: workspace, PID: os.Getpid(), Started: os.Getenv("BURROW_OUTDATED_STARTED"), Health: "healthy", Access: "owner"}
	http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(info) }))
}
