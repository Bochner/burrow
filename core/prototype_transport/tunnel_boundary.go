// Disposable existing-tunnel consumer. Only the controlled HTTP fixture is supported.
package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/vibepwners/hovel/sdk/go/hovel"
)

type fixtureTunnel struct {
	ID               string `json:"id"`
	Connection       string `json:"connection"`
	Direction        string `json:"direction"`
	Mode             string `json:"mode"`
	Bind             string `json:"bind"`
	Destination      string `json:"destination"`
	ProbeDestination string `json:"probeDestination,omitempty"`
	EffectiveBind    string `json:"effectiveBind"`
}

func (s *ownedConnection) fixtureSSH(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	base := []string{"-F", os.Getenv("BURROW_OWNER_CONFIG"), "-S", filepath.Join(s.dir, "master"), "-o", "ProxyCommand=/bin/false"}
	return exec.CommandContext(ctx, "/usr/bin/ssh", append(base, args...)...).Output()
}

func (t fixtureTunnel) spec() string {
	if t.Direction == "D" {
		return t.Bind
	}
	return t.Bind + ":" + t.Destination
}

// This proof is Linux/IPv4-only. Inspect the remote kernel's listening sockets,
// not the client's requested bind or the fact that forwarding was accepted.
func (s *ownedConnection) reverseBind(port string) (string, error) {
	code := `import socket,sys
p=int(sys.argv[1]); found=[]
for path in ('/proc/net/tcp','/proc/net/tcp6'):
 for row in open(path).readlines()[1:]:
  f=row.split(); a,h=f[1].split(':')
  if int(h,16)==p and f[3]=='0A':
   found.append(socket.inet_ntop(socket.AF_INET, bytes.fromhex(a)[::-1]) if len(a)==8 else ('::' if int(a,16)==0 else 'ipv6'))
print(','.join(sorted(set(found))))`
	b, err := s.fixtureSSH("target", "python3 -c "+shellQuote(code)+" "+shellQuote(port))
	return strings.TrimSpace(string(b)), err
}

func (s *ownedConnection) openTunnels() error {
	s.tunnels = map[string]fixtureTunnel{}
	if len(s.requested) > 4 {
		return fmt.Errorf("fixture supports at most four tunnels")
	}
	for _, t := range s.requested {
		host, port, err := net.SplitHostPort(t.Bind)
		if err != nil {
			return fmt.Errorf("invalid bind endpoint")
		}
		if host == "" {
			host = "127.0.0.1"
			t.Bind = net.JoinHostPort(host, port)
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 || (host != "127.0.0.1" && host != "127.0.0.2" && host != "0.0.0.0") {
			return fmt.Errorf("invalid fixture bind")
		}
		if t.Direction != "L" && t.Direction != "R" && t.Direction != "D" {
			return fmt.Errorf("invalid direction")
		}
		dest, dp, err := net.SplitHostPort(t.Destination)
		dn, numErr := strconv.Atoi(dp)
		if err != nil || numErr != nil || dn < 1 || dn > 65535 || (dest != "127.0.0.1" && dest != "localhost") {
			return fmt.Errorf("fixture destination must be a loopback HTTP server")
		}
		if _, err := s.fixtureSSH("-O", "forward", "-"+t.Direction, t.spec(), "target"); err != nil {
			return fmt.Errorf("forward allocation refused")
		}
		t.EffectiveBind = host
		if t.Direction == "R" {
			actual, err := s.reverseBind(port)
			t.EffectiveBind = actual
			if err != nil || actual != host {
				// Close the full fixture connection if this allocation cannot be verified.
				return fmt.Errorf("reverse bind not honored: requested %s observed %s", host, actual)
			}
		}
		t.ID, t.Connection = rand.Text(), "gateway"
		t.Mode = "fixed"
		if t.Direction == "D" {
			t.Mode, t.ProbeDestination, t.Destination = "socks5", t.Destination, ""
		}
		s.tunnels[t.ID] = t
	}
	return nil
}

func (s *ownedConnection) closeTunnel(id string) error {
	s.tunnelMu.Lock()
	defer s.tunnelMu.Unlock()
	t, ok := s.tunnels[id]
	if !ok {
		return fmt.Errorf("selected tunnel unavailable")
	}
	if _, err := s.fixtureSSH("-O", "cancel", "-"+t.Direction, t.spec(), "target"); err != nil {
		return fmt.Errorf("tunnel close failed")
	}
	delete(s.tunnels, id)
	return nil
}

func (s *ownedConnection) probeTunnel(id, nonce string) (hovel.PayloadCommandResult, error) {
	if !regexp.MustCompile(`^[a-f0-9]{16}$`).MatchString(nonce) {
		return hovel.PayloadCommandResult{}, fmt.Errorf("invalid fixture nonce")
	}
	// ponytail: close waits for bounded active probes; per-flow cancellation if made a general stream adapter.
	s.tunnelMu.RLock()
	defer s.tunnelMu.RUnlock()
	t, ok := s.tunnels[id]
	if !ok {
		return hovel.PayloadCommandResult{}, fmt.Errorf("selected tunnel unavailable")
	}
	var data []byte
	var err error
	if t.Direction == "R" {
		code := `import sys,urllib.request
o=urllib.request.build_opener(urllib.request.ProxyHandler({}))
with o.open(sys.argv[1],timeout=2) as r: sys.stdout.buffer.write(r.read(128))`
		data, err = s.fixtureSSH("target", "python3 -c "+shellQuote(code)+" "+shellQuote("http://"+t.Bind+"/"+nonce))
	} else {
		transport := &http.Transport{DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext, DisableKeepAlives: true}
		defer transport.CloseIdleConnections()
		endpoint := t.Bind
		if t.Direction == "D" {
			proxy, _ := url.Parse("socks5h://" + t.Bind)
			transport.Proxy = http.ProxyURL(proxy)
			endpoint = t.ProbeDestination
		}
		client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("redirect refused") }}
		response, requestErr := client.Get("http://" + endpoint + "/" + nonce)
		err = requestErr
		if err == nil {
			data, err = io.ReadAll(io.LimitReader(response.Body, 128))
			response.Body.Close()
			if response.StatusCode != 200 {
				err = fmt.Errorf("HTTP fixture refused")
			}
		}
	}
	if err != nil || string(data) != nonce {
		return hovel.PayloadCommandResult{}, fmt.Errorf("selected tunnel routing failed")
	}
	return hovel.PayloadCommandResult{Command: "tunnel-probe", Stdout: string(data), Fields: map[string]string{"id": t.ID, "connection": t.Connection, "direction": t.Direction, "mode": t.Mode, "bind": t.Bind, "effectiveBind": t.EffectiveBind, "destination": t.Destination}}, nil
}
