// Package terminal owns frontend-local Linux PTYs. It never owns daemon state.
package terminal

import (
	"context"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"golang.org/x/sys/unix"
)

type Snapshot struct {
	Screen                       string
	Cursor                       image.Point
	Visible, Exited, MouseMotion bool
	Err                          error
	ScrollOffset, HistoryLines   int
}

// Host accepts ordered input/resize events while independently draining output.
// Screens and scrollback are memory only; no transcript is written to disk.
type Host struct {
	mu      sync.Mutex
	em      *vt.Emulator
	pty     *os.File
	cmd     *exec.Cmd
	state   Snapshot
	modes   map[ansi.Mode]bool
	input   chan any
	done    chan struct{}
	stopped chan struct{}
	stop    sync.Once
	offset  int
}

func Start(ctx context.Context, cmd *exec.Cmd, width, height int) (*Host, error) {
	return StartWithScrollback(ctx, cmd, width, height, math.MaxInt)
}

// StartWithScrollback bounds retained history independently of draining output.
func StartWithScrollback(ctx context.Context, cmd *exec.Cmd, width, height, historyLines int) (*Host, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if width < 1 || height < 1 || width > 1000 || height > 1000 {
		return nil, fmt.Errorf("terminal geometry outside 1..1000 cells")
	}
	if historyLines < 0 {
		return nil, fmt.Errorf("negative terminal history limit")
	}
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	master := os.NewFile(uintptr(fd), "/dev/ptmx")
	ok := false
	defer func() {
		if !ok {
			master.Close()
		}
	}()
	if err = unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		return nil, err
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		return nil, err
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0600)
	if err != nil {
		return nil, err
	}
	defer slave.Close()
	if err = unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Row: uint16(height), Col: uint16(width)}); err != nil {
		return nil, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	env := cmd.Env
	cmd.Env = nil
	for _, value := range env {
		name, _, _ := strings.Cut(value, "=")
		if name != "TERM" && name != "COLORTERM" && name != "LINES" && name != "COLUMNS" {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "TERM=xterm-256color", "COLORTERM=truecolor")
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	s := &Host{em: vt.NewEmulator(width, height), pty: master, cmd: cmd, input: make(chan any, 128), done: make(chan struct{}), stopped: make(chan struct{}), modes: make(map[ansi.Mode]bool)}
	s.state.Visible = true
	// Lines allocate on demand. Hovel retains its complete CLI history; SSH
	// shells use a finite bound and never write transcripts to disk.
	s.em.SetScrollbackSize(historyLines)
	s.em.SetCallbacks(vt.Callbacks{
		CursorVisibility: func(on bool) { s.state.Visible = on },
		EnableMode:       func(mode ansi.Mode) { s.modes[mode] = true },
		DisableMode:      func(mode ansi.Mode) { delete(s.modes, mode) },
	})
	ok = true
	var workers sync.WaitGroup
	outputDone := make(chan struct{})
	workers.Add(3)
	go func() {
		defer workers.Done()
		defer close(outputDone)
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				s.mu.Lock()
				before := s.em.ScrollbackLen()
				s.em.Write(buf[:n])
				if s.offset > 0 {
					s.offset = min(s.em.ScrollbackLen(), max(0, s.offset+s.em.ScrollbackLen()-before))
				}
				if s.em.IsAltScreen() {
					s.offset = 0
				}
				s.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		buf := make([]byte, 4096)
		for {
			n, err := s.em.Read(buf)
			if n > 0 {
				master.SetWriteDeadline(time.Now().Add(time.Second))
				if _, err = master.Write(buf[:n]); err != nil {
					s.stopProcess()
					s.fail(err)
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.done:
				return
			case event := <-s.input:
				s.mu.Lock()
				s.inputEvent(event)
				s.mu.Unlock()
			}
		}
	}()
	go func() {
		stop := context.AfterFunc(ctx, s.stopProcess)
		err := cmd.Wait()
		// Drain final output; descendants holding the slave cannot delay cleanup indefinitely.
		select {
		case <-outputDone:
		case <-time.After(100 * time.Millisecond):
		}
		stop()
		s.stopProcess()
		s.mu.Lock()
		s.state.Exited = true
		if s.state.Err == nil {
			s.state.Err = err
		}
		s.mu.Unlock()
		close(s.done)
		workers.Wait()
		s.mu.Lock()
		s.em.Close()
		s.mu.Unlock()
		close(s.stopped)
	}()
	return s, nil
}

func (s *Host) fail(err error) { s.mu.Lock(); s.state.Err = err; s.mu.Unlock() }
func (s *Host) stopProcess() {
	s.stop.Do(func() {
		// This process group belongs only to this frontend PTY, never the daemon.
		syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
		s.pty.Close()
		s.em.InputPipe().(io.Closer).Close()
	})
}
func (s *Host) Close()                { s.stopProcess(); <-s.stopped }
func (s *Host) Done() <-chan struct{} { return s.stopped }

// Send never blocks the renderer. Refuse overload rather than lose/reorder input.
func (s *Host) Send(event any) error {
	if size, ok := event.(image.Point); ok && (size.X < 1 || size.Y < 1 || size.X > 1000 || size.Y > 1000) {
		return fmt.Errorf("terminal geometry outside 1..1000 cells")
	}
	select {
	case <-s.done:
		s.mu.Lock()
		local := s.scrollEvent(event)
		s.mu.Unlock()
		if local {
			return nil
		}
		return fmt.Errorf("terminal exited")
	default:
	}
	select {
	case s.input <- event:
		return nil
	default:
		return fmt.Errorf("terminal input busy; input was not sent")
	}
}
func (s *Host) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.state
	v.Screen = s.em.Render()
	v.Cursor = s.em.CursorPosition()
	v.MouseMotion = s.modes[ansi.ModeMouseAnyEvent]
	v.HistoryLines = s.em.ScrollbackLen()
	v.ScrollOffset = min(s.offset, v.HistoryLines)
	if v.ScrollOffset > 0 && !s.em.IsAltScreen() {
		buf := uv.NewRenderBuffer(s.em.Width(), s.em.Height())
		start := v.HistoryLines - v.ScrollOffset
		for y := range s.em.Height() {
			for x := range s.em.Width() {
				cell := s.em.ScrollbackCellAt(x, start+y)
				if start+y >= v.HistoryLines {
					cell = s.em.CellAt(x, start+y-v.HistoryLines)
				}
				buf.SetCell(x, y, cell)
			}
		}
		v.Screen, v.Visible = buf.Render(), false
	}
	return v
}

// Shift navigation belongs to the host. Ordinary keys still reach interactive apps.
func (s *Host) scrollEvent(event any) bool {
	if s.em.IsAltScreen() {
		return false
	}
	delta := 0
	switch v := event.(type) {
	case uv.KeyPressEvent:
		if v.Mod != uv.ModShift {
			return false
		}
		switch v.Code {
		case uv.KeyPgUp:
			delta = max(1, s.em.Height()-1)
		case uv.KeyPgDown:
			delta = -max(1, s.em.Height()-1)
		case uv.KeyHome:
			delta = s.em.ScrollbackLen()
		case uv.KeyEnd:
			delta = -s.offset
		default:
			return false
		}
	case uv.MouseWheelEvent:
		if s.modes[ansi.ModeMouseNormal] || s.modes[ansi.ModeMouseButtonEvent] || s.modes[ansi.ModeMouseAnyEvent] || s.modes[ansi.ModeMouseX10] {
			return false
		}
		switch v.Button {
		case uv.MouseWheelUp:
			delta = 3
		case uv.MouseWheelDown:
			delta = -3
		default:
			return false
		}
	default:
		return false
	}
	s.offset = max(0, min(s.em.ScrollbackLen(), s.offset+delta))
	return true
}
func (s *Host) inputEvent(event any) {
	if s.scrollEvent(event) {
		return
	}
	switch v := event.(type) {
	case image.Point:
		if v.X < 1 || v.Y < 1 || v.X > 1000 || v.Y > 1000 {
			s.state.Err = fmt.Errorf("terminal geometry outside 1..1000 cells")
			return
		}
		s.em.Resize(v.X, v.Y)
		// File.Fd would switch this pollable PTY back to blocking I/O.
		raw, err := s.pty.SyscallConn()
		if err == nil {
			var ioctlErr error
			err = raw.Control(func(fd uintptr) {
				ioctlErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Row: uint16(v.Y), Col: uint16(v.X)})
			})
			if err == nil {
				err = ioctlErr
			}
		}
		if err != nil {
			s.state.Err = err
		}
	case uv.KeyPressEvent:
		s.offset = 0
		// x/vt's legacy matcher compares the whole struct; discard event metadata.
		if v.Text != "" && v.Mod & ^uv.ModShift == 0 {
			s.em.SendText(v.Text)
		} else {
			s.em.SendKey(uv.KeyPressEvent{Code: v.Code, Mod: v.Mod})
		}
	case string:
		s.offset = 0
		// Unbracketed pasted newlines must not execute commands implicitly.
		if !s.modes[ansi.ModeBracketedPaste] {
			v = strings.NewReplacer("\r", " ", "\n", " ").Replace(v)
		}
		v = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
				return -1
			}
			return r
		}, v)
		s.em.Paste(v)
	case uv.MouseEvent:
		_, motion := v.(uv.MouseMotionEvent)
		_, release := v.(uv.MouseReleaseEvent)
		if motion && !s.modes[ansi.ModeMouseAnyEvent] && !(s.modes[ansi.ModeMouseButtonEvent] && v.Mouse().Button != uv.MouseNone) {
			return
		}
		if release && !(s.modes[ansi.ModeMouseNormal] || s.modes[ansi.ModeMouseButtonEvent] || s.modes[ansi.ModeMouseAnyEvent]) {
			return
		}
		s.em.SendMouse(v)
	}
}
