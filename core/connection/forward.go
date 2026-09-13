package connection

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Bochner/burrow/core/launch"
	"github.com/vibepwners/hovel/sdk/go/hovel"
)

// Tunnel identity survives frontend detach, but never a new creation.
type Tunnel struct {
	ID                 string `json:"id"`
	Creation           string `json:"creation"`
	Connection         string `json:"connection"`
	ConnectionCreation string `json:"connectionCreation"`
	Session            string `json:"session"`
	Generation         string `json:"generation"`
	Direction          string `json:"direction"`
	Listen             string `json:"listen"`
	Destination        string `json:"destination"`
	State              string `json:"state"`
	RunID              string `json:"runID"`
}

type forwardRequest struct {
	Owner  managerIdentity `json:"owner"`
	Tunnel Tunnel          `json:"tunnel"`
}

var errForwardUncertain = errors.New("local forward acknowledgement uncertain; inspect inventory and close the connection if cleanup cannot be verified")

func decodeForward(raw string) (forwardRequest, error) {
	var r forwardRequest
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF {
		return r, fmt.Errorf("invalid forward request")
	}
	t := r.Tunnel
	listen, e := forwardEndpoint(t.Listen, true)
	dest, de := forwardEndpoint(t.Destination, false)
	if e != nil || de != nil || listen != t.Listen || dest != t.Destination || t.Direction != "L" || t.State != "" || t.RunID != "" || t.ID != t.Connection+"/"+t.Creation || t.Session != r.Owner.Session || t.Generation != r.Owner.Generation || t.ConnectionCreation == "" || t.Session == "" || t.Generation == "" {
		return r, fmt.Errorf("invalid forward endpoints or owner binding")
	}
	return r, validateTunnelCommand(r.Owner.Workspace, []string{"unforward", t.ID})
}

func (s *owner) forwardControl(command string, t Tunnel) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// -O requires this master; ProxyCommand also explicitly closes fallback.
	cmd := exec.CommandContext(ctx, "/usr/bin/ssh", "-F", "/dev/null", "-S", s.state.Socket,
		"-o", "ProxyCommand=/usr/bin/false", "-o", "ExitOnForwardFailure=yes", "-O", command, "-L", t.Listen+":"+t.Destination, "unused")
	var diagnostic limitedBuffer
	cmd.Stderr = &diagnostic
	err := cmd.Run()
	defer clear(diagnostic.data)
	// OpenSSH mux.c reports explicit master rejection on stderr; cancel can
	// even exit zero on failure. Never expose raw diagnostic bytes to callers.
	message := string(diagnostic.data)
	if strings.Contains(message, "mux_client_forward: forwarding request failed:") || strings.Contains(message, "Master refused forwarding request:") {
		return fmt.Errorf("local forward control failed; check bind address, occupied port and master availability; use tunnel list before retrying")
	}
	if err != nil || len(diagnostic.data) != 0 {
		return errForwardUncertain
	}
	return nil
}

func (s *owner) liveForwards() ([]Tunnel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := []Tunnel{}
	if s.closed {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := launch.VerifyReservation(ctx, s.config.Workspace, s.dir); err != nil {
		return nil, err
	}
	live := s.state.State == "connected" && s.checkMaster() == nil
	for _, t := range s.tunnels {
		if t.State == "removed" {
			continue
		}
		if !live {
			t.State = "unavailable"
		}
		result = append(result, t)
	}
	slices.SortFunc(result, func(a, b Tunnel) int { return strings.Compare(a.ID, b.ID) })
	return result, nil
}

func (m *manager) forward(raw, review, runID string) (Tunnel, error) {
	r, err := decodeForward(raw)
	if err != nil || digest(raw) != review || runID == "" {
		return Tunnel{}, fmt.Errorf("invalid confirmed forward request")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || r.Owner != m.managerIdentity {
		return Tunnel{}, fmt.Errorf("forward owner changed; review again")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := launch.VerifyReservation(ctx, m.Workspace, m.dir); err != nil {
		return Tunnel{}, err
	}
	s := m.connections[r.Tunnel.ConnectionCreation]
	if s == nil {
		return Tunnel{}, fmt.Errorf("forward connection unavailable; no recreation attempted")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.state.State != "connected" || s.state.Name != r.Tunnel.Connection {
		return Tunnel{}, fmt.Errorf("forward requires selected live connection")
	}
	if err := launch.VerifyReservation(ctx, m.Workspace, s.dir); err != nil {
		return Tunnel{}, err
	}
	if err := s.checkMaster(); err != nil {
		return Tunnel{}, err
	}
	for _, t := range s.tunnels {
		if t.ID == r.Tunnel.ID || (t.State != "removed" && t.Listen == r.Tunnel.Listen) {
			return Tunnel{}, fmt.Errorf("local forward already exists or request was previously submitted; use tunnel list")
		}
	}
	t := r.Tunnel
	t.RunID, t.State = runID, "listening"
	if s.tunnels == nil {
		s.tunnels = map[string]Tunnel{}
	}
	if err := s.forwardControl("forward", t); err != nil {
		t.State = "removed"
		if errors.Is(err, errForwardUncertain) {
			// Missing acknowledgements may follow successful allocation. Never hide it.
			t.State = "unverified"
			s.state.TunnelRevision++
		}
		s.tunnels[t.ID] = t
		return Tunnel{}, err
	}
	s.tunnels[t.ID] = t
	s.state.TunnelRevision++
	s.state.TunnelCount++
	m.milestone("local forward allocated; destination traffic not yet verified")
	return t, nil
}

func (m *manager) tunnelCommand(req hovel.PayloadCommandRequest) (hovel.PayloadCommandResult, error) {
	// ponytail: bounded checks serialize manager controls for at most two seconds
	// of probe I/O; move probes outside this lock if concurrent diagnostics need it.
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || len(req.Args) < 1 || req.Args[0] != m.Generation {
		return hovel.PayloadCommandResult{}, fmt.Errorf("exact manager generation required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := launch.VerifyReservation(ctx, m.Workspace, m.dir); err != nil {
		return hovel.PayloadCommandResult{}, err
	}
	var value any
	if req.Command == "tunnels" && len(req.Args) == 1 {
		list := []Tunnel{}
		for _, s := range m.connections {
			ts, err := s.liveForwards()
			if err != nil {
				return hovel.PayloadCommandResult{}, err
			}
			list = append(list, ts...)
		}
		slices.SortFunc(list, func(a, b Tunnel) int { return strings.Compare(a.ID, b.ID) })
		value = list
	} else if len(req.Args) == 3 && (req.Command == "unforward" || req.Command == "tunnel-check") {
		s := m.connections[req.Args[1]]
		if s == nil {
			return hovel.PayloadCommandResult{}, fmt.Errorf("tunnel connection unavailable")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		t, ok := s.tunnels[req.Args[2]]
		if !ok || t.State == "removed" || s.closed || s.state.State != "connected" {
			return hovel.PayloadCommandResult{}, fmt.Errorf("selected tunnel unavailable; no recreation attempted")
		}
		if err := launch.VerifyReservation(ctx, m.Workspace, s.dir); err != nil {
			return hovel.PayloadCommandResult{}, err
		}
		if err := s.checkMaster(); err != nil {
			return hovel.PayloadCommandResult{}, err
		}
		if req.Command == "unforward" {
			if err := s.forwardControl("cancel", t); err != nil {
				if errors.Is(err, errForwardUncertain) {
					if t.State == "listening" {
						s.state.TunnelCount--
					}
					t.State = "unverified"
					s.tunnels[t.ID] = t
					s.state.TunnelRevision++
				}
				return hovel.PayloadCommandResult{}, err
			}
			// Keep the creation tombstone: replaying an old Hovel request cannot reuse its ID.
			removed := t
			removed.State = "removed"
			s.tunnels[t.ID] = removed
			s.state.TunnelRevision++
			if t.State == "listening" {
				s.state.TunnelCount--
			}
			value = map[string]string{"id": t.ID, "state": "removed", "detail": "listener removed; already accepted streams may finish; connection and sibling forwards retained"}
			m.milestone("selected local forward removed")
		} else {
			value = checkTunnel(t)
		}
	} else {
		return hovel.PayloadCommandResult{}, fmt.Errorf("invalid tunnel control arguments")
	}
	b, err := json.Marshal(value)
	return hovel.PayloadCommandResult{Command: req.Command, Stdout: string(b)}, err
}

// A passive check never sends application commands or retains remote bytes.
// Silent protocols need their own client to establish application-level success.
func checkTunnel(t Tunnel) map[string]string {
	result := map[string]string{"id": t.ID, "state": "unverified", "detail": "listener check failed; inspect the master and local bind"}
	host, port, _ := net.SplitHostPort(t.Listen)
	if host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), time.Second)
	if err != nil {
		return result
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(time.Second))
	var b [1]byte
	n, err := c.Read(b[:])
	clear(b[:])
	if n > 0 {
		result["state"], result["detail"] = "traffic-observed", "received destination traffic through the listener; no remote content retained"
	} else if e, ok := err.(net.Error); ok && e.Timeout() {
		result["state"], result["detail"] = "inconclusive", "listener accepted; destination sent no greeting; verify using the service's normal client"
	} else {
		result["state"], result["detail"] = "failed", "no destination traffic; check destination host/port and server AllowTcpForwarding/PermitOpen restrictions"
	}
	return result
}

func Tunnels(ctx context.Context, w string) ([]Tunnel, error) {
	id, err := findManager(ctx, w)
	if err != nil {
		return nil, err
	}
	if id.Session == "" {
		states, err := List(ctx, w)
		if err != nil {
			return nil, err
		}
		if len(states) > 0 {
			return nil, fmt.Errorf("forwarding inventory unavailable for legacy or unidentified owners; resources preserved")
		}
		return []Tunnel{}, nil
	}
	var list []Tunnel
	err = managerControl(ctx, w, id, "tunnels", nil, &list)
	if err == nil {
		for _, t := range list {
			if t.Session != id.Session || t.Generation != id.Generation || t.ConnectionCreation == "" || validateTunnelCommand(w, []string{"unforward", t.ID}) != nil {
				return nil, fmt.Errorf("invalid retained tunnel identity")
			}
		}
	}
	return list, err
}

func executeForward(ctx context.Context, w string, args []string) (any, error) {
	if args[0] == "tunnels" {
		return Tunnels(ctx, w)
	}
	if args[0] != "forward" {
		ts, err := Tunnels(ctx, w)
		if err != nil {
			return nil, err
		}
		for _, t := range ts {
			if t.ID != args[1] {
				continue
			}
			if args[0] == "unforward" && len(args) == 2 {
				return map[string]string{"review": fmt.Sprintf("Remove local forward %s\nConnection: %s\nListen: %s\nDestination: %s\nRemoves this listener only; already accepted streams may finish. Siblings and connection remain. Repeat tunnel remove %s --yes.", t.ID, t.Connection, t.Listen, t.Destination, t.ID)}, nil
			}
			var result any
			err = managerControl(ctx, w, managerIdentity{Session: t.Session, Generation: t.Generation}, args[0], []string{t.ConnectionCreation, t.ID}, &result)
			return result, err
		}
		return nil, fmt.Errorf("selected tunnel unavailable; stale IDs never select a recreated forward")
	}
	o, err := parseForward(w, args)
	if err != nil {
		return nil, err
	}
	s, err := selected(ctx, w, o.Connection)
	if err != nil {
		return nil, err
	}
	if s.State != "connected" || s.Generation == "" {
		return nil, fmt.Errorf("local forward requires an existing manager connection")
	}
	id, err := findManager(ctx, w)
	if err != nil {
		return nil, err
	}
	if id.Session != s.Session || id.Generation != s.Generation {
		return nil, fmt.Errorf("forward owner changed; review again")
	}
	bound := digest(strings.Join([]string{w, id.Session, id.Generation, s.Creation, o.Listen, o.Destination}, "\n"))
	if !o.Yes {
		return map[string]string{"digest": bound, "review": fmt.Sprintf("Create local forward\nConnection: %s\nListen: %s\nDestination: %s\nDestination is reached from the selected SSH server. Broader binds expose access to other hosts.\nListener allocation does not verify destination traffic. Repeat with --yes --review %s.", s.Name, o.Listen, o.Destination, bound)}, nil
	}
	if o.Review != "" && o.Review != bound {
		return nil, fmt.Errorf("forward changed after review; review again")
	}
	creation := digest(rand.Text())[:32]
	r := forwardRequest{Owner: id, Tunnel: Tunnel{ID: s.Name + "/" + creation, Creation: creation, Connection: s.Name, ConnectionCreation: s.Creation, Session: id.Session, Generation: id.Generation, Direction: "L", Listen: o.Listen, Destination: o.Destination}}
	b, _ := json.Marshal(r)
	var t Tunnel
	err = managerThrow(ctx, w, map[string]string{"action": "forward", "generation": id.Generation, "session": id.Session, "request": string(b), "review": digest(string(b))}, &t)
	if err != nil {
		return nil, fmt.Errorf("local forward %s unconfirmed; use tunnel list before retrying; check bind address/occupied port and master availability: %w", r.Tunnel.ID, err)
	}
	if t.ID != r.Tunnel.ID || t.ConnectionCreation != s.Creation || t.Generation != id.Generation || t.Session != id.Session {
		return nil, fmt.Errorf("forward result identity changed; inspect before retrying")
	}
	return t, nil
}

type forwardOptions struct {
	Connection, Listen, Destination, Review string
	Yes                                     bool
}

// Both operator spellings use the same owner operations and validation.
func tunnelArgs(args []string) ([]string, error) {
	if args[0] == "tunc" {
		args = append([]string{"tunnel", "create"}, args[1:]...)
		if len(args) > 3 && args[3] == "l" {
			args[3] = "forward"
		}
	} else if args[0] == "tund" {
		args = append([]string{"tunnel", "remove"}, args[1:]...)
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("expected tunnel create|list|check|remove; use help")
	}
	switch args[1] {
	case "create":
		if len(args) < 7 {
			return nil, fmt.Errorf("required: tunnel create CONNECTION forward LISTEN HOST PORT [--yes] [--review HASH]; alias: tunc CONNECTION l LISTEN HOST PORT")
		}
		if args[3] != "forward" {
			return nil, fmt.Errorf("only forward tunnels are supported; reverse forwarding is not implemented")
		}
		return append([]string{"forward", args[2], args[4], net.JoinHostPort(args[5], args[6])}, args[7:]...), nil
	case "list":
		if len(args) != 2 {
			return nil, fmt.Errorf("tunnel list takes no arguments")
		}
		return []string{"tunnels"}, nil
	case "remove", "check":
		verb := "unforward"
		if args[1] == "check" {
			verb = "tunnel-check"
		}
		return append([]string{verb}, args[2:]...), nil
	default:
		return nil, fmt.Errorf("expected tunnel create|list|check|remove; use help")
	}
}

func forwardEndpoint(value string, listening bool) (string, error) {
	if listening && !strings.Contains(value, ":") {
		value = net.JoinHostPort("127.0.0.1", value)
	}
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return "", fmt.Errorf("destination/listening endpoint must be HOST:PORT (IPv6 uses [ADDRESS]:PORT)")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
		return "", fmt.Errorf("endpoint port must be 1–65535 in decimal")
	}
	if listening {
		if host == "" {
			host = "127.0.0.1"
		}
		ip, err := netip.ParseAddr(host)
		if err != nil || ip.Zone() != "" || ip.Is4In6() {
			return "", fmt.Errorf("listen host must be a literal IP address; default is 127.0.0.1")
		}
		host = ip.String()
	} else if ip, err := netip.ParseAddr(host); err == nil && ip.Zone() == "" {
		host = ip.String()
	} else if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,252}$`).MatchString(host) {
		return "", fmt.Errorf("destination host must be a hostname or literal IP address")
	}
	return net.JoinHostPort(host, port), nil
}

func parseForward(w string, args []string) (forwardOptions, error) {
	var o forwardOptions
	if len(args) < 4 {
		return o, fmt.Errorf("required: tunnel create CONNECTION forward LISTEN HOST PORT [--yes] [--review HASH]")
	}
	o.Connection = args[1]
	if _, err := launch.ConnectionPath(w, o.Connection); err != nil {
		return o, err
	}
	var err error
	if o.Listen, err = forwardEndpoint(args[2], true); err != nil {
		return o, err
	}
	if o.Destination, err = forwardEndpoint(args[3], false); err != nil {
		return o, err
	}
	for i := 4; i < len(args); i++ {
		switch args[i] {
		case "--yes":
			o.Yes = true
		case "--review":
			i++
			if i == len(args) || o.Review != "" || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(args[i]) {
				return o, fmt.Errorf("invalid forward options; --review requires recap digest")
			}
			o.Review = args[i]
		default:
			return o, fmt.Errorf("invalid forward options; use help")
		}
	}
	return o, nil
}

func validateTunnelCommand(w string, args []string) error {
	if args[0] == "forward" {
		_, err := parseForward(w, args)
		return err
	}
	if len(args) < 2 || len(args) > 3 || (len(args) == 3 && (args[0] != "unforward" || args[2] != "--yes")) {
		return fmt.Errorf("expected tunnel remove ID [--yes] or tunnel check ID; qualified tunnel ID required")
	}
	name, id, _ := strings.Cut(args[1], "/")
	if _, err := launch.ConnectionPath(w, name); err != nil || !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(id) {
		return fmt.Errorf("qualified tunnel ID required; select the complete ID from tunnel list")
	}
	return nil
}
