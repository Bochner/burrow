package terminal

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPTYDrainAfterExit(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	cmd := exec.Command("/bin/sh", "-c", "read ready <&3; printf '%8192s\\nFINAL-DRAIN\\n' x")
	cmd.ExtraFiles = []*os.File{r}
	h, err := StartWithScrollback(context.Background(), cmd, 60, 10, 128)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	func() {
		// Hold the emulator as a slow snapshot would while the child exits
		// with more than one reader buffer of final output still queued.
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, err := w.Write([]byte("ready\n")); err != nil {
			t.Fatal(err)
		}
		for deadline := time.Now().Add(3 * time.Second); ; {
			if syscall.Kill(cmd.Process.Pid, 0) == syscall.ESRCH {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("fixture did not exit with queued output")
			}
			time.Sleep(time.Millisecond)
		}
		time.Sleep(200 * time.Millisecond)
	}()
	select {
	case <-h.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("final output did not drain")
	}
	if s := h.Snapshot(); !strings.Contains(s.Screen, "FINAL-DRAIN") {
		t.Fatal("child exit discarded queued output", s)
	}
}

func TestPTYExitStopsDescendants(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 60 & printf 'FINAL-DRAIN\\n'")
	h, err := Start(context.Background(), cmd, 60, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	select {
	case <-h.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("descendant holding the slave prevented cleanup")
	}
	if s := h.Snapshot(); !strings.Contains(s.Screen, "FINAL-DRAIN") {
		t.Fatal("descendant cleanup discarded final output", s)
	}
}
