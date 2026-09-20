package connection

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Prompt contains public display text only. Answers travel over an ephemeral
// same-user Unix socket, never through Hovel RPC, chain settings or logging.
type Prompt struct {
	Text           string
	Secret         bool
	TargetPassword bool
}
type PromptFunc func(context.Context, Prompt) ([]byte, error)

func sameUser(c *net.UnixConn) error {
	raw, e := c.SyscallConn()
	if e != nil {
		return e
	}
	var peer *unix.Ucred
	var pe error
	if e = raw.Control(func(fd uintptr) { peer, pe = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); e != nil {
		return e
	}
	if pe != nil || peer.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("authentication peer refused")
	}
	return nil
}

// Askpass is invoked only as OpenSSH's helper. It writes its answer solely to
// OpenSSH's anonymous pipe; it is never run as a module or an operator command.
func Askpass(prompt string) error {
	request := Prompt{}
	switch {
	case strings.HasPrefix(prompt, "Enter passphrase for key "):
		request = Prompt{Text: "SSH key passphrase (hidden; Ctrl+C cancels)", Secret: true}
	case strings.HasSuffix(strings.TrimSpace(prompt), "password:"):
		request = Prompt{Text: "SSH password (hidden; Ctrl+C cancels)", Secret: true}
		request.TargetPassword = strings.TrimSpace(prompt) == os.Getenv("BURROW_PASSWORD_PROMPT")
	default:
		return fmt.Errorf("unsupported authentication prompt")
	}
	path := os.Getenv("BURROW_PROMPT_SOCKET")
	if path == "" {
		return fmt.Errorf("interactive authentication unavailable; use --prompt")
	}
	c, e := net.DialTimeout("unix", path, time.Second)
	if e != nil {
		return fmt.Errorf("authentication frontend unavailable")
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Minute))
	if e = sameUser(c.(*net.UnixConn)); e != nil {
		return e
	}
	if e = json.NewEncoder(c).Encode(request); e != nil {
		return e
	}
	var answer []byte
	if e = json.NewDecoder(io.LimitReader(c, 16384)).Decode(&answer); e != nil {
		return fmt.Errorf("authentication cancelled")
	}
	defer clear(answer)
	if len(answer) > 4096 || strings.ContainsAny(string(answer), "\x00\r\n") {
		return fmt.Errorf("invalid authentication answer")
	}
	if _, e = os.Stdout.Write(answer); e != nil {
		return e
	}
	_, e = os.Stdout.Write([]byte{'\n'})
	return e
}

// ExecutePrompt uses the same reviewed Hovel launch and waits for observed
// authentication. Cancel/failure closes only the owner this attempt created.
func ExecutePrompt(ctx context.Context, w string, args []string, ask PromptFunc) (any, error) {
	if len(args) < 2 || (args[0] != "connect" && args[0] != "reconnect") {
		return nil, fmt.Errorf("explicit connection command required")
	}
	c, _, err := Parse(w, args[1:])
	if err != nil {
		return nil, err
	}
	if c.Password != nil {
		password := []byte(*c.Password)
		defer clear(password)
		answered := false
		ask = func(_ context.Context, p Prompt) ([]byte, error) {
			if !p.Secret || !p.TargetPassword {
				return nil, fmt.Errorf("automatic password refused for non-target authentication; use keys/agents for jump hosts or bare --password for interactive entry")
			}
			if answered {
				return nil, fmt.Errorf("supplied password rejected; retry explicitly")
			}
			answered = true
			return append([]byte{}, password...), nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	dir, e := os.MkdirTemp("", "burrow-auth-")
	if e != nil {
		return nil, fmt.Errorf("private authentication directory unavailable")
	}
	defer os.Remove(dir)
	path := filepath.Join(dir, "prompt")
	listener, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		return nil, e
	}
	defer listener.Close()
	if e = os.Chmod(path, 0600); e != nil {
		return nil, e
	}
	stop := context.AfterFunc(ctx, func() { listener.Close() })
	defer stop()
	finished := make(chan struct{})
	questionError := make(chan error, 1)
	go func() {
		defer close(finished)
		for {
			c, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(2 * time.Minute))
				stop := context.AfterFunc(ctx, func() { c.Close() })
				defer stop()
				if sameUser(c) != nil {
					return
				}
				var p Prompt
				if json.NewDecoder(io.LimitReader(c, 8192)).Decode(&p) != nil {
					return
				}
				if ask == nil {
					cancel()
					return
				}
				answer, err := ask(ctx, p)
				defer clear(answer)
				if err != nil {
					select {
					case questionError <- err:
					default:
					}
					cancel()
					return
				}
				json.NewEncoder(c).Encode(answer)
			}()
		}
	}()
	defer func() { cancel(); listener.Close(); <-finished }()
	// Finish Hovel's launch receipt even if secret entry cancels while throw is
	// returning, so cleanup addresses the exact owner instead of losing its ID.
	launchCtx, finish := context.WithTimeout(ctx, 60*time.Second)
	defer finish()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("authentication cancelled")
	}
	result, e := execute(launchCtx, w, args, path)
	if e != nil {
		return nil, e
	}
	s, ok := result.(State)
	if !ok {
		return result, nil
	}
	for {
		if s.State == "connected" && ctx.Err() == nil {
			return s, nil
		}
		if s.State == "lost" || ctx.Err() != nil {
			reason := s.Detail
			if ctx.Err() != nil {
				reason = "authentication cancelled or timed out"
			}
			select {
			case err := <-questionError:
				reason = err.Error()
			default:
			}
			cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
			e := closeOwned(cleanup, w, s)
			done()
			if e != nil {
				return nil, fmt.Errorf("%s; cleanup uncertain: %w", reason, e)
			}
			return nil, fmt.Errorf("%s; attempt closed, retry connect explicitly", reason)
		}
		select {
		case <-ctx.Done():
		case <-time.After(100 * time.Millisecond):
		}
		if ctx.Err() == nil {
			next, err := selected(ctx, w, s.Name)
			if err != nil {
				cancel()
			} else {
				if next.Session != s.Session || next.Generation != s.Generation || next.Creation != s.Creation {
					cancel()
				} else {
					s = next
				}
			}
		}
	}
}
