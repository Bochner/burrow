package terminal_test

import (
	"context"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Bochner/burrow/core/terminal"
	uv "github.com/charmbracelet/ultraviolet"
	"golang.org/x/sys/unix"
)

// The fixture is an actual child with a controlling PTY, not an emulator mock.
func TestPTYFixture(t *testing.T) {
	if os.Getenv("BURROW_PTY_FIXTURE") != "1" {
		return
	}
	settings, err := unix.IoctlGetTermios(0, unix.TCGETS)
	if err != nil {
		os.Exit(2)
	}
	settings.Lflag &^= unix.ICANON | unix.ECHO | unix.ISIG
	settings.Iflag &^= unix.ICRNL | unix.IXON
	settings.Cc[unix.VMIN], settings.Cc[unix.VTIME] = 1, 0
	if unix.IoctlSetTermios(0, unix.TCSETS, settings) != nil {
		os.Exit(3)
	}
	fmt.Print("\x1b[?2004h\x1b[?1000h\x1b[?1006h\x1b[?1hREADY\r\n")
	buf := make([]byte, 1)
	for {
		if _, err := io.ReadFull(os.Stdin, buf); err != nil {
			os.Exit(0)
		}
		switch buf[0] {
		case 'G':
			size, _ := unix.IoctlGetWinsize(0, unix.TIOCGWINSZ)
			fmt.Printf("\r\nSIZE=%dx%d\r\n", size.Col, size.Row)
		case 0x01:
			fmt.Print("\x1b[?1049h\x1b[2J\x1b[Halternate\x1b[4;6H")
		case 0x02:
			fmt.Print("\x1b[?1049l")
		case 0x04:
			fmt.Print("\x1b[6n")
		case 0x18:
			fmt.Print("\r\nFINAL\r\n")
			os.Exit(0)
		default:
			fmt.Printf("%02x ", buf[0])
		}
	}
}
func TestOwnedPTY(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.Command(os.Args[0], "-test.run=^TestPTYFixture$")
	cmd.Env = append(os.Environ(), "BURROW_PTY_FIXTURE=1")
	h, err := terminal.Start(ctx, cmd, 60, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	wait := func(needle string) terminal.Snapshot {
		t.Helper()
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) {
			s := h.Snapshot()
			if strings.Contains(strings.Join(strings.Fields(s.Screen), " "), needle) {
				return s
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("missing %q: %+v", needle, h.Snapshot())
		return terminal.Snapshot{}
	}
	send := func(v any) {
		t.Helper()
		if err := h.Send(v); err != nil {
			t.Fatal(err)
		}
	}
	wait("READY")
	send(uv.KeyPressEvent{Code: 'G'})
	wait("SIZE=60x16")
	send(image.Pt(72, 20))
	send(uv.KeyPressEvent{Code: 'G'})
	wait("SIZE=72x20")
	send(uv.KeyPressEvent{Code: uv.KeyUp, IsRepeat: true})
	wait("1b 4f 41") // application cursor, including repeated key metadata
	send(uv.KeyPressEvent{Code: 'c', Mod: uv.ModCtrl})
	wait("03")
	send("hé\n")
	wait("1b 5b 32 30 30 7e 68 c3 a9 0a 1b 5b 32 30 31 7e")
	send(uv.MouseClickEvent{X: 2, Y: 3, Button: uv.MouseLeft})
	wait("1b 5b 3c 30 3b 33 3b 34 4d")
	send(uv.KeyPressEvent{Code: 'a', Mod: uv.ModCtrl})
	s := wait("alternate")
	if s.Cursor != image.Pt(5, 3) {
		t.Fatal(s.Cursor)
	}
	send(uv.KeyPressEvent{Code: 'd', Mod: uv.ModCtrl})
	wait("1b 5b 34 3b 36 52") // terminal reply has pane-local cursor coordinates
	send(uv.KeyPressEvent{Code: 'b', Mod: uv.ModCtrl})
	wait("SIZE=72x20")
	send(uv.KeyPressEvent{Code: 'x', Mod: uv.ModCtrl})
	wait("FINAL")
	select {
	case <-h.Done():
	case <-time.After(4 * time.Second):
		t.Fatal("exit did not reap PTY")
	}
	if !h.Snapshot().Exited {
		t.Fatal("exit state missing")
	}
	if h.Send("must not replay") == nil {
		t.Fatal("input accepted after exit")
	}
	cmd = exec.Command("/bin/sh", "-c", "sleep 60 & wait")
	h2, err := terminal.Start(ctx, cmd, 40, 10)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-h2.Done():
	case <-time.After(4 * time.Second):
		t.Fatal("frontend cancellation did not reap child")
	}
}
