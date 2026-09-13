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
		case 'U':
			// Force UTF-8, CSI and mode changes across separate PTY reads.
			for _, b := range []byte("\x1b[?1049h\x1b[2J\x1b[H\x1b[32m界é\x1b[0m\x1b[3;4H\x1b[?25l") {
				os.Stdout.Write([]byte{b})
				time.Sleep(2 * time.Millisecond)
			}
		case 'H':
			fmt.Print("\x1b[?1000l")
			for i := 0; i < 10045; i++ {
				fmt.Printf("history-%02d\r\n", i)
			}
			if os.Getenv("BURROW_SCROLL_FIXTURE") == "1" {
				io.ReadFull(os.NewFile(3, "continue"), buf)
				fmt.Print("ASYNC\r\n")
			}
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

func TestPTYScrollback(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestPTYFixture$")
	cmd.Env = append(os.Environ(), "BURROW_PTY_FIXTURE=1", "BURROW_SCROLL_FIXTURE=1")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	cmd.ExtraFiles = []*os.File{r}
	h, err := terminal.Start(context.Background(), cmd, 60, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	wait := func(needle string) terminal.Snapshot {
		t.Helper()
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
			s := h.Snapshot()
			if strings.Contains(s.Screen, needle) {
				return s
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("missing %q: %s", needle, h.Snapshot().Screen)
		return terminal.Snapshot{}
	}
	wait("READY")
	if err := h.Send(uv.KeyPressEvent{Code: 'H'}); err != nil {
		t.Fatal(err)
	}
	wait("history-10044")
	h.Send(uv.KeyPressEvent{Code: uv.KeyHome, Mod: uv.ModShift})
	s := wait("history-00")
	if s.Visible {
		t.Fatal("live cursor shown over history")
	}
	if _, err := w.Write([]byte("continue")); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(3 * time.Second); h.Snapshot().HistoryLines == s.HistoryLines && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if after := h.Snapshot(); after.Screen != s.Screen || after.HistoryLines <= s.HistoryLines {
		t.Fatal("background output moved history or stopped draining", after)
	}
	if err := h.Send(uv.KeyPressEvent{Code: uv.KeyEnd, Mod: uv.ModShift}); err != nil {
		t.Fatal(err)
	}
	wait("ASYNC")
	waitOffset := func(offset int) {
		t.Helper()
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
			if h.Snapshot().ScrollOffset == offset {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("scroll offset: got %d, want %d", h.Snapshot().ScrollOffset, offset)
	}
	h.Send(uv.MouseWheelEvent{Button: uv.MouseWheelUp})
	waitOffset(3)
	h.Send(uv.KeyPressEvent{Code: uv.KeyPgUp, Mod: uv.ModShift})
	waitOffset(12)
	h.Send(uv.KeyPressEvent{Code: uv.KeyPgDown, Mod: uv.ModShift})
	waitOffset(3)
	// Adjacent resize/scroll events must use the new pane height in queue order.
	h.Send(image.Pt(60, 20))
	h.Send(uv.KeyPressEvent{Code: uv.KeyEnd, Mod: uv.ModShift})
	h.Send(uv.KeyPressEvent{Code: uv.KeyPgUp, Mod: uv.ModShift})
	waitOffset(19)
	h.Send(uv.KeyPressEvent{Code: 'G'})
	h.Send(uv.KeyPressEvent{Code: uv.KeyHome, Mod: uv.ModShift})
	wait("history-00")
	h.Send(uv.KeyPressEvent{Code: 'a', Mod: uv.ModCtrl})
	wait("alternate")
	h.Send(uv.KeyPressEvent{Code: uv.KeyHome, Mod: uv.ModShift})
	if h.Snapshot().ScrollOffset != 0 {
		t.Fatal("main history leaked into alternate screen")
	}
	h.Send(uv.KeyPressEvent{Code: 'b', Mod: uv.ModCtrl})
	wait("ASYNC")
	h.Send(uv.KeyPressEvent{Code: 'x', Mod: uv.ModCtrl})
	select {
	case <-h.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("fixture did not exit")
	}
	if err := h.Send(uv.KeyPressEvent{Code: uv.KeyHome, Mod: uv.ModShift}); err != nil {
		t.Fatal(err)
	}
	wait("history-00")
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
	if err := h.Send(image.Pt(0, 20)); err == nil {
		t.Fatal("invalid resize reported success")
	}
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
	send(uv.KeyPressEvent{Code: 'U'})
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		s = h.Snapshot()
		if !s.Visible {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(s.Screen, "界é") || !strings.Contains(s.Screen, "\x1b[32m") || s.Visible || s.Cursor != image.Pt(3, 2) {
		t.Fatal("fragmented VT output lost text/style/cursor", s)
	}
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

func TestBoundedPTYHistory(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "i=0; while [ $i -lt 2000 ]; do echo history-$i; i=$((i+1)); done; echo BOUNDED-DONE")
	h, err := terminal.StartWithScrollback(context.Background(), cmd, 60, 10, 128)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	select {
	case <-h.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("output did not drain")
	}
	s := h.Snapshot()
	if s.HistoryLines != 128 || !strings.Contains(s.Screen, "BOUNDED-DONE") {
		t.Fatal("history bound or output lost", s)
	}
}
