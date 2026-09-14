package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Bochner/burrow/core/connection"
	"golang.org/x/sys/unix"
)

// A SIGTERM during a secret prompt must end the form promptly. The CLI owns
// signals through signal.NotifyContext; if Bubble Tea also installs its own
// SIGTERM handler, that handler can block forever sending QuitMsg after the
// context already stopped the event loop, and Program.Run never returns. The
// race is timing-dependent, so the prompt is repeated to make a regression
// visible in one run.
func TestCLIFormReturnsAfterSIGTERM(t *testing.T) {
	master, tty := testTerminal(t)
	var mu sync.Mutex
	var screen []byte
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				mu.Lock()
				screen = append(screen, buf[:n]...)
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	rendered := func(from int) bool {
		mu.Lock()
		defer mu.Unlock()
		return bytes.Contains(screen[from:], []byte("SSH password"))
	}
	for i := 0; i < 40; i++ {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
		mu.Lock()
		from := len(screen)
		mu.Unlock()
		done := make(chan error, 1)
		go func() {
			done <- runCLIForm(ctx, tty, promptForm(connection.Prompt{Text: "SSH password (hidden; Ctrl+C cancels)", Secret: true}, false))
		}()
		deadline := time.Now().Add(5 * time.Second)
		for !rendered(from) {
			if time.Now().After(deadline) {
				t.Fatalf("iteration %d: prompt never rendered", i)
			}
			time.Sleep(5 * time.Millisecond)
		}
		if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err == nil {
				t.Fatalf("iteration %d: SIGTERM completed the prompt as if answered", i)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("iteration %d: prompt still running 5s after SIGTERM", i)
		}
		stop()
	}
}

// testTerminal returns a pseudo-terminal pair; the slave stands in for /dev/tty.
func testTerminal(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	fd := int(master.Fd())
	if err = unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	if err = unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Row: 30, Col: 120}); err != nil {
		t.Fatal(err)
	}
	tty, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		tty.Close()
		master.Close()
	})
	return master, tty
}
