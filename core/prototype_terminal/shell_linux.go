package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// Reuses the existing local_terminal.py proof's Linux PTY mechanism.
// Plain-output baseline plus the opt-in internal VT screen candidate.
type localShell struct {
	screen    *shellScreen
	mu        sync.Mutex
	stop      sync.Once
	pty       *os.File
	cmd       *exec.Cmd
	target    string
	tail      []byte
	discarded int
	exited    bool
	done      chan struct{}
}

func openShell(dir, target string, width, height int, screenMode bool) (*localShell, error) {
	if screenMode && (width < 1 || height < 1 || width > 320 || height > 120) {
		return nil, fmt.Errorf("screen proof supports 1..320 columns and 1..120 rows")
	}
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		master.Close()
		return nil, err
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		master.Close()
		return nil, err
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0600)
	if err != nil {
		master.Close()
		return nil, err
	}
	defer slave.Close()
	s := &localShell{pty: master, target: target, done: make(chan struct{})}
	if err = s.resize(width, height); err != nil {
		master.Close()
		return nil, err
	}
	if screenMode {
		s.initScreen(width, height)
	}
	s.cmd = exec.Command("/bin/sh", "-i")
	s.cmd.Dir = dir
	s.cmd.Env = append(os.Environ(), "PS1=local-fixture$ ", "TERM=dumb")
	if screenMode {
		s.cmd.Env = append(s.cmd.Env, "TERM=xterm-256color")
	}
	s.cmd.Stdin = slave
	s.cmd.Stdout = slave
	s.cmd.Stderr = slave
	s.cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err = s.cmd.Start(); err != nil {
		if s.screen != nil {
			s.screen.em.Close()
		}
		master.Close()
		return nil, err
	}
	if s.screen != nil {
		go func() { defer close(s.screen.repliesDone); io.Copy(s.pty, s.screen.em) }()
	}
	go func() {
		defer close(s.done)
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				s.mu.Lock()
				if s.screen != nil {
					s.screen.em.Write(buf[:n])
				}
				s.tail = append(s.tail, buf[:n]...)
				if extra := len(s.tail) - 65536; extra > 0 {
					s.tail = s.tail[extra:]
					s.discarded += extra
				}
				s.mu.Unlock()
			}
			if err != nil {
				break
			}
		}
		s.cmd.Wait()
		s.mu.Lock()
		s.exited = true
		s.mu.Unlock()
	}()
	return s, nil
}
func (s *localShell) resize(width, height int) error {
	if s.screen != nil {
		if width < 1 || height < 1 || width > 320 || height > 120 {
			return fmt.Errorf("screen proof supports 1..320 columns and 1..120 rows")
		}
		s.mu.Lock()
		s.screen.em.Resize(width, height)
		s.mu.Unlock()
	}
	return unix.IoctlSetWinsize(int(s.pty.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: uint16(max(1, min(height, 65535))), Col: uint16(max(1, min(width, 65535)))})
}
func (s *localShell) snapshot() (string, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.tail), s.discarded, s.exited
}
func (s *localShell) close() {
	s.stop.Do(func() {
		// The process group is created exclusively for this local fixture shell.
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
		_ = s.pty.Close()
		if s.screen != nil {
			s.screen.em.InputPipe().(io.Closer).Close()
			<-s.screen.repliesDone
		}
		<-s.done
		if s.screen != nil {
			s.screen.em.Close()
		}
	})
}
