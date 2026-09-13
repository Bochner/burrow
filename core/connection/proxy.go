package connection

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Bochner/burrow/core/launch"
)

// Verify the exact kernel endpoint belongs to the authenticated master, rather
// than treating a successful dial to an unrelated listener as proof of ownership.
func (s *owner) proxyListener(listen string) (bool, error) {
	endpoint, err := netip.ParseAddrPort(listen)
	if err != nil {
		return false, err
	}
	ip := endpoint.Addr().AsSlice()
	address := ""
	for i := 0; i < len(ip); i += 4 {
		address += fmt.Sprintf("%08X", binary.NativeEndian.Uint32(ip[i:i+4]))
	}
	address += fmt.Sprintf(":%04X", endpoint.Port())
	path := fmt.Sprintf("/proc/%d", s.state.MasterPID)
	files, err := os.ReadDir(filepath.Join(path, "fd"))
	if err != nil {
		return false, err
	}
	inodes := map[string]bool{}
	for _, f := range files {
		link, err := os.Readlink(filepath.Join(path, "fd", f.Name()))
		if os.IsNotExist(err) {
			continue // A completed stream can disappear during observation.
		}
		if err != nil {
			return false, err
		}
		inodes[link] = true
	}
	table := "tcp"
	if endpoint.Addr().Is6() {
		table = "tcp6"
	}
	data, err := os.ReadFile(filepath.Join(path, "net", table))
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(data), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) >= 10 && fields[1] == address && fields[3] == "0A" && inodes["socket:["+fields[9]+"]"] {
			return true, nil
		}
	}
	return false, nil
}

// Called under the owner lock. Live metadata is a projection of the existing
// forwarding records; saved reconnect settings do not change on proxy removal.
func (s *owner) observeProxy() {
	s.state.Proxy, s.state.ProxyPort = Tunnel{}, 0
	for _, t := range s.tunnels {
		if t.Direction != "D" || t.State == "removed" {
			continue
		}
		if s.closed || s.state.State != "connected" {
			t.State = "unavailable"
		} else if listening, err := s.proxyListener(t.Listen); err != nil || !listening {
			t.State = "unverified"
		}
		s.state.Proxy = t
		if t.State == "listening" {
			_, port, _ := net.SplitHostPort(t.Listen)
			s.state.ProxyPort, _ = strconv.Atoi(port)
		}
	}
}

func proxyArgs(w string, args []string) ([]string, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("required: proxy create CONNECTION LISTEN [--yes] [--review HASH], proxy inspect CONNECTION, or proxy remove CONNECTION [--yes] [--review HASH]")
	}
	if _, err := launch.ConnectionPath(w, args[2]); err != nil {
		return nil, err
	}
	switch args[1] {
	case "create":
		if len(args) < 4 {
			return nil, fmt.Errorf("required: proxy create CONNECTION LISTEN; LISTEN is PORT or IP:PORT")
		}
		expanded := append([]string{"dynamic", args[2], args[3], ""}, args[4:]...)
		_, err := parseForward(w, expanded)
		return expanded, err
	case "inspect":
		if len(args) != 3 {
			return nil, fmt.Errorf("proxy inspect takes only CONNECTION")
		}
	case "remove":
		// Reuse the existing confirmation-option parser, with inert endpoints.
		_, err := parseForward(w, append([]string{"dynamic", args[2], "1", ""}, args[3:]...))
		return args, err
	default:
		return nil, fmt.Errorf("expected proxy create|inspect|remove")
	}
	return args, nil
}

func executeProxy(ctx context.Context, w string, args []string) (any, error) {
	expanded, _ := proxyArgs(w, args) // Validated before workspace setup/dispatch.
	if args[1] == "create" {
		return executeForward(ctx, w, expanded)
	}
	s, err := selected(ctx, w, args[2])
	if err != nil {
		return nil, err
	}
	if s.Generation == "" {
		return nil, fmt.Errorf("proxy ownership unavailable; legacy owners cannot be adopted")
	}
	t := s.Proxy
	if t.ID == "" && s.ProxyPort != 0 {
		return nil, fmt.Errorf("retained manager lacks proxy ownership metadata; use reviewed burrow restart to upgrade")
	}
	if t.ID != "" && (t.Direction != "D" || t.Connection != s.Name || t.ConnectionCreation != s.Creation || t.Session != s.Session || t.Generation != s.Generation || validateTunnelCommand(w, []string{"unforward", t.ID}) != nil) {
		return nil, fmt.Errorf("invalid retained proxy owner identity")
	}
	if args[1] == "inspect" {
		if t.ID == "" {
			if s.State != "connected" {
				return map[string]string{"state": "unavailable", "connection": s.Name}, nil
			}
			return map[string]string{"state": "off", "connection": s.Name}, nil
		}
		return t, nil
	}
	if t.ID == "" || s.State != "connected" {
		return nil, fmt.Errorf("selected proxy unavailable; no recreation attempted")
	}
	o, _ := parseForward(w, append([]string{"dynamic", s.Name, "1", ""}, args[3:]...))
	b, _ := json.Marshal(t)
	bound := digest(w + "\n" + string(b))
	if !o.Yes {
		return map[string]string{"digest": bound, "review": fmt.Sprintf("Remove SOCKS proxy\nConnection: %s\nListen: %s\nState: %s\nRemoves this listener only; already accepted streams may finish.\nConnection and L/R siblings remain. Repeat with --yes --review %s.", s.Name, t.Listen, t.State, bound)}, nil
	}
	if o.Review != "" && o.Review != bound {
		return nil, fmt.Errorf("proxy changed after review; review again")
	}
	var result any
	err = managerControl(ctx, w, managerIdentity{Session: t.Session, Generation: t.Generation}, "unforward", []string{t.ConnectionCreation, t.ID}, &result)
	if err != nil {
		return nil, fmt.Errorf("proxy removal unconfirmed; inspect proxy state; close connection if cleanup is uncertain: %w", err)
	}
	return result, nil
}
