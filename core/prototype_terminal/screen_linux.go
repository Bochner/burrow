package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/charmbracelet/x/vt"
	"golang.org/x/sys/unix"
)

type shellReturned struct{ err error }
type shellScreen struct {
	em          *vt.Emulator
	visible     bool
	modes       map[ansi.Mode]bool
	repliesDone chan struct{}
}

// Only input modes understood by the emulator are mirrored to the outer TTY.
var inputModes = []ansi.Mode{ansi.ModeCursorKeys, ansi.ModeNumericKeypad, ansi.ModeBracketedPaste,
	ansi.ModeMouseX10, ansi.ModeMouseNormal, ansi.ModeMouseButtonEvent, ansi.ModeMouseAnyEvent,
	ansi.ModeFocusEvent, ansi.ModeMouseExtSgr}

func (s *localShell) initScreen(w, h int) {
	v := &shellScreen{em: vt.NewEmulator(w, h), visible: true, modes: make(map[ansi.Mode]bool), repliesDone: make(chan struct{})}
	v.em.SetScrollbackSize(128)
	v.em.SetCallbacks(vt.Callbacks{
		CursorVisibility: func(on bool) { v.visible = on },
		EnableMode: func(mode ansi.Mode) {
			for _, allowed := range inputModes {
				if mode == allowed {
					v.modes[mode] = true
				}
			}
		},
		DisableMode: func(mode ansi.Mode) { delete(v.modes, mode) },
	})
	s.screen = v
}

// Bubble Tea releases its terminal while this attachment owns raw I/O.
// Background PTYs continue feeding their independent emulators.
type shellAttachment struct{ s *localShell }

func (*shellAttachment) SetStdin(io.Reader)  {}
func (*shellAttachment) SetStdout(io.Writer) {}
func (*shellAttachment) SetStderr(io.Writer) {}
func (a *shellAttachment) Run() error {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer tty.Close()
	fd := int(tty.Fd())
	old, err := term.MakeRaw(tty.Fd())
	if err != nil {
		return err
	}
	defer term.Restore(tty.Fd(), old)
	defer io.WriteString(tty, ansi.ResetMode(inputModes...)+"\x1b[0m\x1b[?25h\x1b[?1049l")
	io.WriteString(tty, "\x1b[?1049h")
	width, height := 0, 0
	last := ""
	for {
		w, h, err := term.GetSize(tty.Fd())
		if err != nil {
			return err
		}
		if w != width || h != height {
			if err = a.s.resize(w, h); err != nil {
				return err
			}
			width, height = w, h
			last = ""
		}
		a.s.mu.Lock()
		v := a.s.screen
		body := strings.ReplaceAll(v.em.Render(), "\n", "\r\n")
		pos := v.em.CursorPosition()
		modes := ""
		for _, mode := range inputModes {
			if v.modes[mode] {
				modes += ansi.SetMode(mode)
			} else {
				modes += ansi.ResetMode(mode)
			}
		}
		cursor := fmt.Sprintf("\x1b[%d;%dH", pos.Y+1, pos.X+1)
		if v.visible {
			cursor += "\x1b[?25h"
		} else {
			cursor += "\x1b[?25l"
		}
		ended := a.s.exited
		a.s.mu.Unlock()
		if ended {
			return nil
		}
		frame := modes + "\x1b[?25l\x1b[H\x1b[2J" + body + cursor
		// ponytail: full redraw with 30 ms idle polling; dirty-row rendering if this proves too costly.
		if frame != last {
			if _, err = io.WriteString(tty, frame); err != nil {
				return err
			}
			last = frame
		}
		polls := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		_, err = unix.Poll(polls, 30)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if polls[0].Revents&(unix.POLLHUP|unix.POLLERR) != 0 {
			return io.EOF
		}
		if polls[0].Revents&unix.POLLIN != 0 {
			var b [1]byte
			if _, err = io.ReadFull(tty, b[:]); err != nil {
				return err
			}
			if b[0] == 0x1d {
				return nil
			}
			if _, err = a.s.pty.Write(b[:]); err != nil {
				return err
			}
		}
	}
}
