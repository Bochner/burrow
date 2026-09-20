package connection

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand/v2"
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
	RequestedListen    string `json:"requestedListen,omitempty"`
	Destination        string `json:"destination"`
	State              string `json:"state"`
	RunID              string `json:"runID"`
}

type forwardRequest struct {
	Owner  managerIdentity `json:"owner"`
	Tunnel Tunnel          `json:"tunnel"`
}

var errForwardUncertain = errors.New("forward acknowledgement or cleanup uncertain; inspect inventory and close the connection if cleanup cannot be verified")

func decodeForward(raw string) (forwardRequest, error) {
	var r forwardRequest
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF {
		return r, fmt.Errorf("invalid forward request")
	}
	t := r.Tunnel
	listen, e := forwardListen(t.Listen, t.Direction)
	dest, de := forwardEndpoint(t.Destination, false)
	if t.Direction == "D" && t.Destination == "" {
		de = nil
	}
	if t.Direction == "D" && t.Destination != "" {
		return r, fmt.Errorf("SOCKS destinations are chosen by its clients, not the forwarding request")
	}
	if e != nil || de != nil || listen != t.Listen || dest != t.Destination || (t.Direction != "L" && t.Direction != "R" && t.Direction != "D") || t.RequestedListen != "" || t.State != "" || t.RunID != "" || t.ID != t.Connection+"/"+t.Creation || t.Session != r.Owner.Session || t.Generation != r.Owner.Generation || t.ConnectionCreation == "" || t.Session == "" || t.Generation == "" {
		return r, fmt.Errorf("invalid forward endpoints or owner binding")
	}
	return r, validateTunnelCommand(r.Owner.Workspace, []string{"unforward", t.ID})
}

func (s *owner) forwardControl(command string, t Tunnel) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// -O requires this master; ProxyCommand also explicitly closes fallback.
	direction := "-L"
	spec := t.Listen + ":" + t.Destination
	if t.Direction == "R" {
		direction = "-R"
	} else if t.Direction == "D" {
		direction, spec = "-D", t.Listen
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/ssh", "-F", "/dev/null", "-S", s.state.Socket,
		"-o", "ProxyCommand=/usr/bin/false", "-o", "ExitOnForwardFailure=yes", "-O", command, direction, spec, "unused")
	var diagnostic, output limitedBuffer
	cmd.Stderr, cmd.Stdout = &diagnostic, &output
	err := cmd.Run()
	defer clear(diagnostic.data)
	defer clear(output.data)
	// OpenSSH mux.c reports explicit master rejection on stderr; cancel can
	// even exit zero on failure. Never expose raw diagnostic bytes to callers.
	message := string(diagnostic.data)
	if strings.Contains(message, "mux_client_forward: forwarding request failed:") || strings.Contains(message, "Master refused forwarding request:") {
		return fmt.Errorf("forward control failed; check bind address, occupied port, server forwarding policy and master availability; use tunnel list before retrying")
	}
	if len(output.data) != 0 {
		return errForwardUncertain
	}
	if err != nil || len(diagnostic.data) != 0 {
		return errForwardUncertain
	}
	return nil
}

// Observe the remote kernel, not the requested SSH options. The authenticated
// server must allow shell commands and readable Linux socket tables. No helper
// is staged. Filter by the validated port remotely; discard all raw output.
func (s *owner) reverseListeners(listen string) (value []string, failure error) {
	a, err := launch.BeginAudit(s.config.Workspace, "inspect reverse listeners "+listen, targetLabel(s.state), nil)
	if err != nil {
		return nil, err
	}
	defer func() { failure = a.Finish(value, failure) }()
	return s.readReverseListeners(listen)
}

func (s *owner) readReverseListeners(listen string) ([]string, error) {
	_, port, err := net.SplitHostPort(listen)
	n, parseErr := strconv.Atoi(port)
	if err != nil || parseErr != nil || n < 1 || n > 65535 {
		return nil, fmt.Errorf("assigned remote port unavailable; close the connection")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	code := fmt.Sprintf(`LC_ALL=C; export LC_ALL
printf '\001\000\000\000' | od -An -tu4
set -- /proc/net/tcp
if test -e /proc/net/tcp6; then set -- "$@" /proc/net/tcp6; fi
awk 'FNR == 1 { if ($2 != "local_address") exit 1; next } $4 == "0A" && $2 ~ /:%04X$/ { print $2 }' "$@" && printf 'end\n'`, n)
	cmd := exec.CommandContext(ctx, "/usr/bin/ssh", "-F", "/dev/null", "-S", s.state.Socket,
		"-o", "ControlMaster=no", "-o", "ProxyCommand=/usr/bin/false", "-T", "unused", code)
	var output, diagnostic limitedBuffer
	cmd.Stdout, cmd.Stderr = &output, &diagnostic
	err = cmd.Run()
	defer clear(output.data)
	defer clear(diagnostic.data)
	fail := fmt.Errorf("remote exposure unverifiable; requires readable Linux /proc/net/tcp tables and shell awk/od; no remote output retained")
	fields := strings.Fields(string(output.data))
	if err != nil || len(diagnostic.data) != 0 || len(fields) < 2 || fields[len(fields)-1] != "end" || (fields[0] != "1" && fields[0] != "16777216") {
		return nil, fail
	}
	result := []string{}
	for _, field := range fields[1 : len(fields)-1] {
		address, p, ok := strings.Cut(field, ":")
		b, err := hex.DecodeString(address)
		if !ok || err != nil || p != fmt.Sprintf("%04X", n) || (len(b) != 4 && len(b) != 16) {
			return nil, fail
		}
		// procfs encodes each 32-bit address word in the remote host's byte order.
		if fields[0] == "1" {
			for i := 0; i < len(b); i += 4 {
				slices.Reverse(b[i : i+4])
			}
		}
		ip, _ := netip.AddrFromSlice(b)
		result = append(result, net.JoinHostPort(ip.String(), port))
	}
	slices.Sort(result)
	return slices.Compact(result), nil
}

func (s *owner) cancelForward(t Tunnel) error {
	if strings.HasSuffix(t.Listen, ":0") {
		return errForwardUncertain // No assigned port: only connection-wide close is safe.
	}
	if err := s.forwardControl("cancel", t); err != nil {
		return err
	}
	if t.Direction == "D" {
		listening, err := s.proxyListener(t.Listen)
		if err != nil || listening {
			return errForwardUncertain
		}
	}
	if t.Direction == "R" {
		// Cancellation acknowledgement may precede server processing. Observe it
		// before claiming removal; never remove another same-port listener.
		for range 3 {
			listeners, err := s.reverseListeners(t.Listen)
			if err != nil {
				return errForwardUncertain
			}
			if len(listeners) == 0 {
				return nil
			}
		}
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
		if t.State == "removed" || t.Direction == "D" {
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

func (m *manager) forward(raw, review, runID string) (result Tunnel, err error) {
	defer func() {
		if err != nil {
			// Errors here contain validated endpoints and fixed diagnostics only.
			m.milestone("forward refused: " + err.Error())
		}
	}()
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
		if t.ID == r.Tunnel.ID {
			return Tunnel{}, fmt.Errorf("forward request was previously submitted; use tunnel list")
		}
		if r.Tunnel.Direction == "D" && t.Direction == "D" && t.State != "removed" {
			return Tunnel{}, fmt.Errorf("connection already owns a proxy; inspect or remove it first")
		}
	}
	t := r.Tunnel
	t.RunID, t.State = runID, "listening"
	if s.tunnels == nil {
		s.tunnels = map[string]Tunnel{}
	}
	if t.Direction == "R" && strings.HasSuffix(t.Listen, ":0") {
		t.RequestedListen = t.Listen
	}
	var allocation error
attempts:
	for range 8 {
		if t.RequestedListen != "" {
			host, _, _ := net.SplitHostPort(t.RequestedListen)
			t.Listen = net.JoinHostPort(host, strconv.Itoa(49152+mathrand.IntN(16384)))
		}
		for _, existing := range s.tunnels {
			if existing.State != "removed" && (existing.Direction == "R") == (t.Direction == "R") && existing.Listen == t.Listen {
				allocation = fmt.Errorf("forward endpoint already reserved; use tunnel list")
				if t.RequestedListen != "" {
					continue attempts
				}
				break attempts
			}
		}
		if t.Direction == "R" {
			actual, err := s.reverseListeners(t.Listen)
			if err != nil {
				return Tunnel{}, err // Missing observation capability must not allocate anything.
			}
			if len(actual) != 0 {
				allocation = fmt.Errorf("remote listening port already occupied; existing listeners preserved")
				if t.RequestedListen != "" {
					continue
				}
				break
			}
		}
		allocation = s.forwardControl("forward", t)
		if allocation == nil || errors.Is(allocation, errForwardUncertain) || t.RequestedListen == "" {
			break
		}
	}
	if allocation != nil {
		t.State = "removed"
		if errors.Is(allocation, errForwardUncertain) {
			// Missing acknowledgements may follow successful allocation. Never hide it.
			t.State = "unverified"
			s.state.TunnelRevision++
		}
		s.tunnels[t.ID] = t
		return Tunnel{}, allocation
	}
	if t.Direction == "R" {
		actual, err := s.reverseListeners(t.Listen)
		if err != nil || !slices.Equal(actual, []string{t.Listen}) {
			t.State = "removed"
			if cleanup := s.cancelForward(t); cleanup != nil {
				t.State = "unverified"
				s.tunnels[t.ID] = t
				s.state.TunnelRevision++
				return Tunnel{}, fmt.Errorf("reverse exposure mismatch or unverifiable; %w", errForwardUncertain)
			}
			s.tunnels[t.ID] = t
			if err != nil {
				return Tunnel{}, err
			}
			return Tunnel{}, fmt.Errorf("reverse exposure mismatch: requested %s, observed %s; listener removed; check GatewayPorts", t.Listen, strings.Join(actual, ","))
		}
	}
	if t.Direction == "D" {
		listening, err := s.proxyListener(t.Listen)
		if err != nil || !listening {
			t.State = "unverified"
			if s.cancelForward(t) == nil {
				t.State = "removed"
			}
			s.tunnels[t.ID] = t
			s.state.TunnelRevision++
			return Tunnel{}, fmt.Errorf("SOCKS endpoint unverified; inspect proxy state before retrying; close the connection if cleanup is uncertain")
		}
	}
	s.tunnels[t.ID] = t
	s.state.TunnelRevision++
	if t.Direction != "D" {
		s.state.TunnelCount++
	}
	m.milestone("forward allocated; destination traffic not yet verified")
	return t, nil
}

func (m *manager) tunnelCommand(req hovel.PayloadCommandRequest) (hovel.PayloadCommandResult, error) {
	// ponytail: bounded remote observations and probes serialize manager controls;
	// move probes outside this lock if concurrent diagnostics need it.
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || len(req.Args) < 1 || req.Args[0] != m.Generation {
		return hovel.PayloadCommandResult{}, fmt.Errorf("exact manager generation required")
	}
	var s *owner
	if len(req.Args) == 3 && (req.Command == "unforward" || req.Command == "tunnel-check") {
		s = m.connections[req.Args[1]]
		if s == nil {
			return hovel.PayloadCommandResult{}, fmt.Errorf("tunnel connection unavailable")
		}
		if req.Command == "unforward" {
			// Give verification its full deadline after admitted readers finish.
			s.tunnelUse.Lock()
			defer s.tunnelUse.Unlock()
		}
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
			if err := s.cancelForward(t); err != nil {
				if errors.Is(err, errForwardUncertain) {
					if t.State == "listening" && t.Direction != "D" {
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
			if t.State == "listening" && t.Direction != "D" {
				s.state.TunnelCount--
			}
			value = map[string]string{"id": t.ID, "state": "removed", "detail": "listener removed; already accepted streams may finish; connection and sibling forwards retained"}
			m.milestone("selected forward removed")
		} else {
			if t.Direction == "R" {
				result := s.checkReverse(t)
				if result["state"] == "unverified" && t.State == "listening" {
					t.State = "unverified"
					s.tunnels[t.ID] = t
					s.state.TunnelCount--
					s.state.TunnelRevision++
				}
				value = result
			} else {
				value = checkTunnel(t)
			}
		}
	} else {
		return hovel.PayloadCommandResult{}, fmt.Errorf("invalid tunnel control arguments")
	}
	b, err := json.Marshal(value)
	return hovel.PayloadCommandResult{Command: req.Command, Stdout: string(b)}, err
}

func (s *owner) checkReverse(t Tunnel) map[string]string {
	result := map[string]string{"id": t.ID, "state": "unverified", "detail": "remote exposure or ownership unverified; inspect and close the connection if cleanup cannot be verified"}
	actual, err := s.reverseListeners(t.Listen)
	if err != nil || t.State != "listening" || !slices.Equal(actual, []string{t.Listen}) {
		return result
	}
	host, port, _ := net.SplitHostPort(t.Listen)
	if host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// -W opens from the remote server to its listener; it never dials a local
	// same-port service. This needs server AllowTcpForwarding/PermitOpen access.
	cmd := exec.CommandContext(ctx, "/usr/bin/ssh", "-F", "/dev/null", "-S", s.state.Socket,
		"-o", "ControlMaster=no", "-o", "ProxyCommand=/usr/bin/false", "-W", net.JoinHostPort(host, port), "unused")
	cmd.Stderr = io.Discard
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return result
	}
	if err = cmd.Start(); err != nil {
		return result
	}
	var b [1]byte
	n, _ := pipe.Read(b[:])
	clear(b[:])
	timedOut := ctx.Err() != nil
	cancel()
	_ = cmd.Wait()
	result["state"], result["detail"] = "failed", "remote listener verified but no destination traffic; check local destination and server AllowTcpForwarding/PermitOpen; try the service's normal client on the remote host"
	if n > 0 {
		result["state"], result["detail"] = "traffic-observed", "received local destination traffic through the remote listener; no remote content retained"
	} else if timedOut {
		result["state"], result["detail"] = "inconclusive", "remote listener verified; no greeting observed; verify using the service's normal client on the remote host"
	}
	return result
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
	if args[0] != "forward" && args[0] != "reverse" && args[0] != "dynamic" {
		ts, err := Tunnels(ctx, w)
		if err != nil {
			return nil, err
		}
		for _, t := range ts {
			if t.ID != args[1] {
				continue
			}
			if args[0] == "unforward" && len(args) == 2 {
				kind, listen, dest := "local", "Local listener", "Remote destination"
				if t.Direction == "R" {
					kind, listen, dest = "reverse", "Remote listener", "Local destination"
				}
				return map[string]string{"review": fmt.Sprintf("Remove %s forward %s\nConnection: %s\n%s: %s\n%s: %s\nRemoves this listener only; already accepted streams may finish. Siblings and connection remain. Repeat tunnel remove %s --yes.", kind, t.ID, t.Connection, listen, t.Listen, dest, t.Destination, t.ID)}, nil
			}
			var result any
			err = managerControl(ctx, w, managerIdentity{Session: t.Session, Generation: t.Generation}, args[0], []string{t.ConnectionCreation, t.ID}, &result)
			if err != nil {
				return nil, fmt.Errorf("selected tunnel control unconfirmed; inspect tunnel list; close the connection if cleanup cannot be verified: %w", err)
			}
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
		return nil, fmt.Errorf("forward requires an existing manager connection")
	}
	id, err := findManager(ctx, w)
	if err != nil {
		return nil, err
	}
	if id.Session != s.Session || id.Generation != s.Generation {
		return nil, fmt.Errorf("forward owner changed; review again")
	}
	bound := digest(strings.Join([]string{w, id.Session, id.Generation, s.Creation, o.Direction, o.Listen, o.Destination}, "\n"))
	if !o.Yes {
		if o.Direction == "D" {
			return map[string]string{"digest": bound, "review": fmt.Sprintf("Create SOCKS proxy\nConnection: %s\nListen: %s\nSOCKS4/5 TCP through the existing master; names resolve on the SSH server.\nBroader binds expose unauthenticated proxy access to other hosts.\nListener verification does not prove destination reachability. Repeat with --yes --review %s.", s.Name, o.Listen, bound)}, nil
		}
		if o.Direction == "R" {
			return map[string]string{"digest": bound, "review": fmt.Sprintf("Create reverse forward\nConnection: %s\nRemote listener: %s\nLocal destination: %s\nDestination is reached from the local SSH client. Port 0 requests a random high port (49152–65535).\nBroader binds expose access to other hosts. Linux socket-table access and shell awk/od are required.\nRemote exposure is checked after allocation; a server override may briefly expose the listener before cleanup.\nRepeat with --yes --review %s.", s.Name, o.Listen, o.Destination, bound)}, nil
		}
		return map[string]string{"digest": bound, "review": fmt.Sprintf("Create local forward\nConnection: %s\nListen: %s\nDestination: %s\nDestination is reached from the selected SSH server. Broader binds expose access to other hosts.\nListener allocation does not verify destination traffic. Repeat with --yes --review %s.", s.Name, o.Listen, o.Destination, bound)}, nil
	}
	if o.Review != "" && o.Review != bound {
		return nil, fmt.Errorf("forward changed after review; review again")
	}
	creation := digest(rand.Text())[:32]
	r := forwardRequest{Owner: id, Tunnel: Tunnel{ID: s.Name + "/" + creation, Creation: creation, Connection: s.Name, ConnectionCreation: s.Creation, Session: id.Session, Generation: id.Generation, Direction: o.Direction, Listen: o.Listen, Destination: o.Destination}}
	b, _ := json.Marshal(r)
	var t Tunnel
	err = managerThrow(ctx, w, map[string]string{"action": "forward", "generation": id.Generation, "session": id.Session, "request": string(b), "review": digest(string(b))}, &t)
	if err != nil {
		if o.Direction == "D" {
			return nil, fmt.Errorf("SOCKS proxy unconfirmed; use proxy inspect %s before retrying; check bind address/occupied port and master availability; close the connection if cleanup is uncertain: %w", s.Name, err)
		}
		if o.Direction == "R" {
			return nil, fmt.Errorf("reverse forward %s unconfirmed; inspect tunnel list before retrying; check occupied remote port, GatewayPorts/AllowTcpForwarding/PermitListen and Linux socket-table access; close the connection if cleanup cannot be verified: %w", r.Tunnel.ID, err)
		}
		return nil, fmt.Errorf("local forward %s unconfirmed; use tunnel list before retrying; check bind address/occupied port and master availability: %w", r.Tunnel.ID, err)
	}
	if t.ID != r.Tunnel.ID || t.ConnectionCreation != s.Creation || t.Generation != id.Generation || t.Session != id.Session {
		return nil, fmt.Errorf("forward result identity changed; inspect before retrying")
	}
	return t, nil
}

type forwardOptions struct {
	Connection, Direction, Listen, Destination, Review string
	Yes                                                bool
}

// Both operator spellings use the same owner operations and validation.
func tunnelArgs(args []string) ([]string, error) {
	if args[0] == "tunc" {
		args = append([]string{"tunnel", "create"}, args[1:]...)
		if len(args) > 3 && args[3] == "l" {
			args[3] = "forward"
		} else if len(args) > 3 && args[3] == "r" {
			args[3] = "reverse"
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
			return nil, fmt.Errorf("required: tunnel create CONNECTION forward|reverse LISTEN HOST PORT [--yes] [--review HASH]; alias: tunc CONNECTION l|r LISTEN HOST PORT; reverse needs a remote listener and explicit local destination")
		}
		if args[3] != "forward" && args[3] != "reverse" {
			return nil, fmt.Errorf("direction must be forward or reverse (aliases l or r)")
		}
		return append([]string{args[3], args[2], args[4], net.JoinHostPort(args[5], args[6])}, args[7:]...), nil
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

func forwardListen(value, direction string) (string, error) {
	if direction == "R" && (value == "0" || strings.HasSuffix(value, ":0")) {
		endpoint, err := forwardEndpoint(strings.TrimSuffix(value, "0")+"1", true)
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(endpoint, "1") + "0", nil
	}
	return forwardEndpoint(value, true)
}

func parseForward(w string, args []string) (forwardOptions, error) {
	var o forwardOptions
	if len(args) < 4 {
		return o, fmt.Errorf("required: tunnel create CONNECTION forward LISTEN HOST PORT [--yes] [--review HASH]")
	}
	o.Connection = args[1]
	o.Direction = "L"
	if args[0] == "reverse" {
		o.Direction = "R"
	} else if args[0] == "dynamic" {
		o.Direction = "D"
	}
	if _, err := launch.ConnectionPath(w, o.Connection); err != nil {
		return o, err
	}
	var err error
	if o.Listen, err = forwardListen(args[2], o.Direction); err != nil {
		return o, err
	}
	if o.Direction != "D" {
		if o.Destination, err = forwardEndpoint(args[3], false); err != nil {
			return o, err
		}
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
	if args[0] == "forward" || args[0] == "reverse" {
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
