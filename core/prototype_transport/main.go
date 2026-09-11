// Throwaway interoperability probe. Commands only target the harness's local fixture.
package main

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	"fmt"
	"go/format"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/pkg/sftp"
	"github.com/vibepwners/hovel/sdk/go/hovel"
	"golang.org/x/crypto/ssh"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func bridge(path string) (*ssh.Client, error) {
	// Prototype accepts only sockets directly inside an owner-only directory.
	for _, p := range []string{filepath.Dir(path), path} {
		info, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
			return nil, fmt.Errorf("unsafe local socket ownership or permissions")
		}
	}
	c, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return nil, err
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	conn, chans, reqs, err := ssh.NewControlClientConn(c)
	if err != nil {
		c.Close()
		return nil, err
	}
	c.SetDeadline(time.Time{})
	return ssh.NewClient(conn, chans, reqs), nil
}

func command(c *ssh.Client, cmd string) string {
	s, err := c.NewSession()
	must(err)
	defer s.Close()
	b, err := s.Output(cmd)
	must(err)
	return string(b)
}

func main() {
	if len(os.Args) == 1 {
		if os.Getenv("BURROW_OWNER_ROOT") != "" {
			owner := &ownership{}
			err := hovel.ServeIO(owner, os.Stdin, os.Stdout)
			if owner.connection != nil {
				owner.connection.Close("module protocol ended")
			}
			must(err)
			return
		}
		hovel.Serve(prototype{})
		return
	}
	if os.Args[1] == "format" {
		for _, name := range []string{"main.go", "ownership.go"} {
			path := filepath.Join(os.Getenv("BUILD_WORKSPACE_DIRECTORY"), "core/prototype_transport", name)
			data, err := os.ReadFile(path)
			must(err)
			data, err = format.Source(data)
			must(err)
			must(os.WriteFile(path, data, 0644))
		}
		return
	}
	if os.Args[1] == "reserve" {
		path, err := reserveConnection(os.Args[2], os.Args[3])
		must(err)
		fmt.Println(path)
		return
	}
	if os.Args[1] == "password-server" {
		passwordServer(os.Args[2])
		return
	}
	// Hard bound for any unresponsive control-proxy global request.
	time.AfterFunc(8*time.Second, func() { fmt.Fprintln(os.Stderr, "probe timed out"); os.Exit(2) })
	var c *ssh.Client
	var fileClient *sftp.Client
	var err error
	if os.Args[1] == "pipe-sftp" {
		process := exec.Command("/usr/bin/ssh", "-F", os.Args[4], "-S", os.Args[2], "-o", "ProxyCommand=/bin/false", "-s", "target", "sftp")
		input, err := process.StdinPipe()
		must(err)
		output, err := process.StdoutPipe()
		must(err)
		must(process.Start())
		defer func() { input.Close(); must(process.Wait()) }()
		fileClient, err = sftp.NewClientPipe(output, input)
		must(err)
	} else {
		c, err = bridge(os.Args[2])
		must(err)
		defer c.Close()
	}
	switch os.Args[1] {
	case "exec":
		fmt.Print(command(c, "printf '%s' \"$SSH_CONNECTION\""))
	case "pty":
		for attempt := 0; attempt < 2; attempt++ {
			s, err := c.NewSession()
			must(err)
			input, err := s.StdinPipe()
			must(err)
			output, err := s.StdoutPipe()
			must(err)
			must(s.RequestPty("xterm", 24, 80, ssh.TerminalModes{ssh.ECHO: 0}))
			must(s.Start("exec /bin/sh"))
			reader := bufio.NewReader(output)
			for _, size := range []struct{ rows, cols int }{{24, 80}, {40, 120}} {
				must(s.WindowChange(size.rows, size.cols))
				_, err = io.WriteString(input, "stty size; printf 'GEOMETRY_END\\n'\n")
				must(err)
				var data []byte
				for !bytes.Contains(data, []byte("GEOMETRY_END\r\n")) {
					b, err := reader.ReadByte()
					must(err)
					data = append(data, b)
				}
				if !bytes.Contains(data, []byte(fmt.Sprintf("%d %d", size.rows, size.cols))) {
					panic(string(data))
				}
			}
			must(s.Close())
		}
		fmt.Println("PASS SSH PTY open/resize/close/reopen 80x24 -> 120x40")
	case "sftp", "loss", "pipe-sftp":
		f := fileClient
		if f == nil {
			f, err = sftp.NewClient(c)
			must(err)
		}
		defer f.Close()
		path := filepath.Join(os.Args[3], "roundtrip")
		out, err := f.Create(path)
		must(err)
		chunk := bytes.Repeat([]byte("burrow-fixture\n"), 2048)
		count := 0
		for i := 0; i < 32; i++ {
			n, err := out.Write(chunk)
			must(err)
			count += n
		}
		must(out.Close())
		in, err := f.Open(path)
		must(err)
		data, err := io.ReadAll(in)
		must(err)
		must(in.Close())
		if !bytes.Equal(data, bytes.Repeat(chunk, 32)) {
			panic("roundtrip mismatch")
		}
		entries, err := f.ReadDir(os.Args[3])
		must(err)
		if len(entries) == 0 {
			panic("empty directory")
		}
		fmt.Printf("PASS SFTP browse/upload/download measured bytes=%d\n", count)
		partial, err := f.Create(filepath.Join(os.Args[3], "partial"))
		must(err)
		n, err := partial.Write(chunk)
		must(err)
		if os.Args[1] == "loss" {
			fmt.Println("READY FOR MASTER LOSS")
			_, err := bufio.NewReader(os.Stdin).ReadString('\n')
			must(err)
			_, err = partial.Write(chunk)
			if err == nil {
				panic("lost transfer falsely succeeded")
			}
			if c.Wait() == nil {
				panic("master loss reported success")
			}
			fmt.Println("PASS master loss: transfer error and bridge Wait error")
			return
		}
		// Deterministic operator cancellation between acknowledged chunks.
		must(partial.Close())
		stat, err := f.Stat(filepath.Join(os.Args[3], "partial"))
		must(err)
		if stat.Size() != int64(n) || stat.Size() >= int64(count) {
			panic("incorrect partial length")
		}
		if _, err = partial.Write(chunk); err == nil {
			panic("cancelled transfer still writable")
		}
		fmt.Printf("PASS cancelled transfer retained explicitly partial bytes=%d\n", n)
	case "direct":
		fmt.Println("DIRECT opening stream")
		d, err := c.Dial("tcp", os.Args[3])
		must(err)
		fmt.Println("DIRECT stream opened")
		_, err = io.WriteString(d, "ping")
		must(err)
		data := make([]byte, 4)
		_, err = io.ReadFull(d, data)
		must(err)
		must(d.Close())
		if string(data) != "ping" {
			panic("forward mismatch")
		}
		fmt.Println("PASS Go direct-tcpip data echoed")
	case "refused":
		if d, err := c.Dial("tcp", os.Args[3]); err == nil {
			d.Close()
			panic("refusal falsely succeeded")
		}
		fmt.Println("PASS visible Go direct-tcpip refusal")
	case "remote":
		l, err := c.Listen("tcp", "127.0.0.1:0")
		must(err)
		defer l.Close()
		fmt.Println("REMOTE", l.Addr())
		must(l.Close())
		fmt.Println("PASS Go remote forwarding removal; parent=" + command(c, "printf alive"))
	default:
		panic("unknown probe")
	}
}

// Password authentication is isolated from host accounts/PAM. It accepts only
// the harness's random secret and emits a fixed response, never executes input.
func passwordServer(root string) {
	key, err := os.ReadFile(filepath.Join(root, "host"))
	must(err)
	signer, err := ssh.ParsePrivateKey(key)
	must(err)
	secret, err := os.ReadFile(filepath.Join(root, "secret"))
	must(err)
	config := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		if subtle.ConstantTimeCompare(p, secret) != 1 {
			return nil, fmt.Errorf("authentication rejected")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	must(err)
	must(os.WriteFile(filepath.Join(root, "password-port"), []byte(l.Addr().String()), 0600))
	for {
		socket, err := l.Accept()
		must(err)
		go func() {
			conn, chans, reqs, err := ssh.NewServerConn(socket, config)
			if err != nil {
				socket.Close()
				return
			}
			defer conn.Close()
			go ssh.DiscardRequests(reqs)
			for ch := range chans {
				if ch.ChannelType() != "session" {
					ch.Reject(ssh.UnknownChannelType, "fixture only")
					continue
				}
				channel, requests, err := ch.Accept()
				if err != nil {
					return
				}
				go func() {
					defer channel.Close()
					for r := range requests {
						if r.Type == "exec" {
							r.Reply(true, nil)
							io.WriteString(channel, "password-ok")
							channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
							return
						}
						r.Reply(false, nil)
					}
				}()
			}
		}()
	}
}

// This adapter tests the public Session contract without inserting resize bytes
// into shell input or adding an undocumented daemon method.
type shell struct {
	client  *ssh.Client
	session *ssh.Session
	input   io.WriteCloser
	output  chan []byte
	done    chan struct{}
	closed  atomic.Bool
	once    sync.Once
}

func (s *shell) Open() error {
	s.output = make(chan []byte, 64)
	s.done = make(chan struct{})
	var err error
	s.session, err = s.client.NewSession()
	if err != nil {
		return err
	}
	s.input, err = s.session.StdinPipe()
	if err != nil {
		s.session.Close()
		return err
	}
	out, err := s.session.StdoutPipe()
	if err != nil {
		s.session.Close()
		return err
	}
	if err = s.session.RequestPty("xterm", 24, 80, ssh.TerminalModes{ssh.ECHO: 0}); err != nil {
		s.session.Close()
		return err
	}
	if err = s.session.Start("exec /bin/sh"); err != nil {
		s.session.Close()
		return err
	}
	go func() {
		defer s.Close("remote EOF")
		for {
			buf := make([]byte, 4096)
			n, err := out.Read(buf)
			if n > 0 {
				select {
				case s.output <- buf[:n]:
				case <-s.done:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return nil
}
func (s *shell) Write(b []byte) error {
	if s.closed.Load() {
		return io.ErrClosedPipe
	}
	_, err := s.input.Write(b)
	return err
}
func (s *shell) Read(wait time.Duration) ([]byte, error) {
	if wait < 0 {
		select {
		case b := <-s.output:
			return b, nil
		case <-s.done:
			return nil, nil
		}
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case b := <-s.output:
		return b, nil
	case <-s.done:
		return nil, nil
	case <-timer.C:
		return nil, nil
	}
}
func (s *shell) Close(string) error {
	s.once.Do(func() { s.closed.Store(true); close(s.done); s.session.Close(); s.client.Close() })
	return nil
}
func (s *shell) Closed() bool { return s.closed.Load() }

type prototype struct{}

func (prototype) Info() hovel.Info {
	return hovel.Info{Name: "burrow-transport-prototype", Version: "0.0.0", Type: hovel.TypeSurvey, Summary: "Controlled local SSH proof"}
}
func (prototype) Schema() hovel.Schema { return hovel.Schema{} }
func (prototype) Run(ctx *hovel.Context) (hovel.Result, error) {
	c, err := bridge(os.Getenv("BURROW_PROOF_SOCKET"))
	if err != nil {
		return hovel.Result{}, err
	}
	_, err = ctx.OpenSession(&shell{client: c}, hovel.WithName("SSH transport proof"), hovel.WithTransport("ssh"))
	if err != nil {
		c.Close()
		return hovel.Result{}, err
	}
	ctx.Log.Info("opened controlled SSH PTY with initial geometry 80x24")
	return hovel.Ok(nil, hovel.WithSummary("controlled local SSH")), nil
}
