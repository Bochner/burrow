package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Bochner/burrow/core/connection"
	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/unix"
)

type authenticate struct {
	workspace string
	args      []string
	result    any
}
type authenticationRequested struct{ args []string }

// Bubble Tea releases and restores the terminal around this command. Both
// frontends share this guided/reviewed flow and the production command seam.
func (*authenticate) SetStdin(io.Reader)  {}
func (*authenticate) SetStdout(io.Writer) {}
func (*authenticate) SetStderr(io.Writer) {}
func (a *authenticate) Run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ctx, stopSignals := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	tty, e := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if e != nil {
		return fmt.Errorf("interactive connection requires a controlling terminal; use key/agent commands for automation")
	}
	defer tty.Close()
	ask := func(ctx context.Context, p connection.Prompt) ([]byte, error) { return readPrompt(ctx, tty, p) }
	args := append([]string{}, a.args...)
	if len(args) == 1 && args[0] == "connect" {
		fmt.Fprintln(tty, "\nSSH Connection · required details first")
		fields := []string{"Connection name", "Hostname, IP or SSH config alias", "Username (- uses SSH config)", "SSH port (Enter uses SSH config/22)", "SSH key path (Enter uses SSH config/agent/password)", "Jump host (Enter uses SSH config/none)"}
		values := make([]string, len(fields))
		for i, field := range fields {
			answer, err := ask(ctx, connection.Prompt{Text: field})
			if err != nil {
				return err
			}
			values[i] = strings.TrimSpace(string(answer))
			clear(answer)
		}
		args = append(args, values[0], values[1], values[2])
		for i, option := range []string{"--port", "--key", "--jump"} {
			if values[i+3] != "" {
				value := values[i+3]
				if option == "--key" && strings.HasPrefix(value, "~/") {
					home, _ := os.UserHomeDir()
					value = home + value[1:]
				}
				args = append(args, option, value)
			}
		}
	}
	_, yes, e := connection.Parse(a.workspace, args[1:])
	if e != nil {
		return e
	}
	if !yes {
		review, err := connection.Execute(ctx, a.workspace, args)
		if err != nil {
			return err
		}
		answer, err := ask(ctx, connection.Prompt{Text: review.(map[string]string)["review"] + "\nProceed? [yes/no]"})
		if err != nil {
			return err
		}
		approved := string(answer) == "yes"
		clear(answer)
		if !approved {
			return fmt.Errorf("connection cancelled; no authentication attempted")
		}
		args = append(args, "--yes")
	}
	fmt.Fprintln(tty, "Connecting · Ctrl+C cancels this attempt")
	a.result, e = connection.ExecutePrompt(ctx, a.workspace, args, ask)
	return e
}

// Read one bounded answer from the controlling terminal. Raw mode prevents
// echo/signals; polling observes cancellation and always restores termios.
func readPrompt(ctx context.Context, tty *os.File, p connection.Prompt) ([]byte, error) {
	lines := strings.Split(p.Text, "\n")
	for i := range lines {
		lines[i] = safe(lines[i])
	}
	state, e := term.MakeRaw(tty.Fd())
	if e != nil {
		return nil, e
	}
	defer term.Restore(tty.Fd(), state)
	defer fmt.Fprint(tty, "\r\n")
	// Establish no-echo before inviting input; an immediate paste must never
	// race the terminal-mode transition. Raw output needs explicit CRLF.
	fmt.Fprint(tty, "\r\n"+strings.Join(lines, "\r\n")+"\r\n> ")
	answer := make([]byte, 0, 128)
	ok := false
	defer func() {
		if !ok {
			clear(answer)
		}
	}()
	for ctx.Err() == nil {
		fds := []unix.PollFd{{Fd: int32(tty.Fd()), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, 100)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("terminal input unavailable")
		}
		if n == 0 {
			continue
		}
		var b [1]byte
		if n, e := tty.Read(b[:]); e != nil || n == 0 {
			return nil, fmt.Errorf("terminal input ended")
		}
		switch b[0] {
		case 3, 4, 27:
			return nil, fmt.Errorf("connection cancelled")
		case '\r', '\n':
			ok = true
			return answer, nil
		case 127, 8:
			if len(answer) > 0 {
				answer[len(answer)-1] = 0
				answer = answer[:len(answer)-1]
				if !p.Secret {
					fmt.Fprint(tty, "\b \b")
				}
			}
		default:
			if b[0] < 32 || len(answer) >= 4096 {
				return nil, fmt.Errorf("invalid or oversized terminal answer (limit 4096)")
			}
			answer = append(answer, b[0])
			if !p.Secret {
				tty.Write(b[:])
			}
		}
	}
	return nil, fmt.Errorf("connection cancelled or timed out")
}
