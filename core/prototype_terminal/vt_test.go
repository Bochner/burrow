package main

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

func TestScreenCandidate(t *testing.T) {
	e := vt.NewEmulator(80, 24)
	replies := make(chan string, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		var b [64]byte
		for {
			n, err := e.Read(b[:])
			if n > 0 {
				replies <- string(b[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() { e.InputPipe().(io.Closer).Close(); <-done; e.Close() }()
	e.SetScrollbackSize(128)
	stream := "NORMAL\x1b[?1049h\x1b[2J\x1b[3;5H\x1b[31mSCREEN-界é\x1b[0m\x1b[?25l"
	// Every byte boundary, including multibyte characters, can be a PTY read.
	for _, b := range []byte(stream) {
		e.Write([]byte{b})
	}
	if got := e.String(); !strings.Contains(got, "SCREEN-界é") || strings.Contains(got, "NORMAL") {
		t.Fatalf("fragmented screen: %q", got)
	}
	if e.CellAt(4, 2).Content != "S" || !e.IsAltScreen() {
		t.Fatal("cursor-addressed alternate screen mismatch")
	}
	e.WriteString("\x1b[6n")
	select {
	case got := <-replies:
		if got != "\x1b[3;15R" {
			t.Fatalf("cursor reply: %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal reply stalled")
	}
	before := e.Render()
	other := vt.NewEmulator(80, 24)
	defer other.Close()
	go io.Copy(io.Discard, other)
	other.SetScrollbackSize(128)
	for i := 0; i < 10000; i++ {
		other.WriteString("background line\r\n")
	}
	if other.ScrollbackLen() > 128 || e.Render() != before {
		t.Fatal("history bound or independent screen mismatch")
	}
	e.Resize(100, 35)
	if !strings.Contains(e.String(), "SCREEN-界é") || e.Width() != 100 || other.Width() != 80 {
		t.Fatal("resize changed screen or sibling geometry")
	}
	// Rendering into a fresh emulator must reproduce visible cells, not bytes.
	restored := vt.NewEmulator(100, 35)
	defer restored.Close()
	go io.Copy(io.Discard, restored)
	restored.WriteString(strings.ReplaceAll(e.Render(), "\n", "\r\n"))
	if restored.String() != e.String() {
		t.Fatalf("render roundtrip: %q != %q", restored.String(), e.String())
	}
	if restored.CellAt(4, 2).Style.Fg != e.CellAt(4, 2).Style.Fg {
		t.Fatal("foreground color lost")
	}
	e.WriteString("\x1b[?1049l")
	if !strings.Contains(e.String(), "NORMAL") {
		t.Fatal("normal buffer lost")
	}
}
