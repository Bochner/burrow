package connection

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Bochner/burrow/core/launch"
	"github.com/vibepwners/hovel/sdk/go/hovel"
	"golang.org/x/sys/unix"
)

type chainSelection struct {
	Owner      managerIdentity `json:"owner"`
	Connection State           `json:"connection"`
	Tunnels    []Tunnel        `json:"tunnels"`
}

type tunnelHTTPRequest struct {
	Owner  managerIdentity `json:"owner"`
	Tunnel Tunnel          `json:"tunnel"`
	URL    string          `json:"url"`
}

type tunnelHTTPResult struct {
	RunID      string `json:"runID"`
	Selection  Tunnel `json:"selection"`
	URL        string `json:"url"`
	State      string `json:"state"`
	StatusCode int    `json:"statusCode"`
	Bytes      int    `json:"bytes"`
	SHA256     string `json:"sha256,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

func chainURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || strings.ContainsAny(raw, "\x00\r\n\t") || u.Scheme != "http" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Host == "" {
		return nil, fmt.Errorf("bounded consumer requires an http://HOST[:PORT]/PATH URL without credentials, query or fragment; all request fields are recorded")
	}
	port := u.Port()
	if port == "" {
		port = "80"
	}
	if _, err := forwardEndpoint(net.JoinHostPort(u.Hostname(), port), false); err != nil {
		return nil, err
	}
	return u, nil
}

func validateChain(w string, args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("required: chain select CONNECTION, or chain http|export CONNECTION TUNNEL_ID URL [--yes] [--review HASH]")
	}
	if _, err := launch.ConnectionPath(w, args[2]); err != nil {
		return err
	}
	if args[1] == "connect" {
		c, yes, err := Parse(w, args[2:])
		if err != nil {
			return err
		}
		if yes || c.Prompt || c.ProxyPort != 0 || c.Review != "" {
			return fmt.Errorf("chain connect exports connection-only settings; confirm the saved chain in Hovel; use an available key or agent without --prompt, --yes, --review or -proxy")
		}
		return nil
	}
	if args[1] == "select" && len(args) == 3 {
		return nil
	}
	if len(args) < 5 || (args[1] != "http" && args[1] != "export") {
		return fmt.Errorf("required: chain http|export CONNECTION TUNNEL_ID URL; select exact identities with chain select CONNECTION")
	}
	if !strings.HasPrefix(args[3], args[2]+"/") || validateTunnelCommand(w, []string{"tunnel-check", args[3]}) != nil {
		return fmt.Errorf("selected connection's complete tunnel creation ID required")
	}
	if _, err := chainURL(args[4]); err != nil {
		return err
	}
	seen := map[string]bool{}
	for i := 5; i < len(args); i++ {
		flag := args[i]
		if args[1] == "export" || seen[flag] {
			return fmt.Errorf("invalid chain options")
		}
		seen[flag] = true
		switch flag {
		case "--yes":
		case "--review":
			i++
			if i == len(args) || len(args[i]) != 64 {
				return fmt.Errorf("--review requires the recap digest")
			}
		default:
			return fmt.Errorf("invalid chain options")
		}
	}
	return nil
}

func selectChain(ctx context.Context, w, name string) (chainSelection, error) {
	s, err := selected(ctx, w, name)
	if err != nil {
		return chainSelection{}, err
	}
	if s.State != "connected" || s.Generation == "" || s.Creation == "" {
		return chainSelection{}, fmt.Errorf("chain requires a selected live connection; reconnect explicitly")
	}
	id, err := findManager(ctx, w)
	if err != nil || id.Session != s.Session || id.Generation != s.Generation {
		return chainSelection{}, fmt.Errorf("selected owner changed or unavailable")
	}
	var out chainSelection
	err = managerControl(ctx, w, id, "chain-inventory", []string{s.Creation}, &out)
	if err == nil && (out.Owner != id || out.Connection.Creation != s.Creation) {
		err = fmt.Errorf("chain inventory owner correlation refused")
	}
	return out, err
}

func executeChain(ctx context.Context, w string, args []string) (any, error) {
	if args[1] == "connect" {
		c, _, err := Parse(w, args[2:])
		if err != nil {
			return nil, err
		}
		_, preview, err := c.review(ctx, "connect")
		if err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(chainConnectionRequest{Settings: c, Preview: preview})
		build, err := launch.Build()
		if err != nil {
			return nil, err
		}
		return savedChain(map[string]string{"action": "chain-connect", "workspace": w, "build": build, "request": string(raw), "review": digest(string(raw))}), nil
	}
	selection, err := selectChain(ctx, w, args[2])
	if err != nil || args[1] == "select" {
		return selection, err
	}
	var chosen Tunnel
	for _, t := range selection.Tunnels {
		if t.ID == args[3] {
			chosen = t
		}
	}
	if chosen.ID == "" || chosen.State != "listening" {
		return nil, fmt.Errorf("selected live tunnel unavailable; no provisioning or fallback")
	}
	r := tunnelHTTPRequest{Owner: selection.Owner, Tunnel: chosen, URL: args[4]}
	raw, _ := json.Marshal(r)
	if _, err := decodeTunnelHTTP(string(raw)); err != nil {
		return nil, err
	}
	config := map[string]string{"action": "tunnel-http", "workspace": w, "session": selection.Owner.Session, "generation": selection.Owner.Generation, "request": string(raw), "review": digest(string(raw))}
	if args[1] == "export" {
		config["build"], err = launch.Build()
		if err != nil {
			return nil, err
		}
		return savedChain(config), nil
	}
	bound := digest(w + "\n" + string(raw))
	if !slices.Contains(args[5:], "--yes") {
		return map[string]string{"digest": bound, "review": fmt.Sprintf("HTTP through existing tunnel\nConnection: %s\nTunnel: %s\nDirection: %s\nListen: %s\nDestination: %s\nURL: %s\nLimit: 8 seconds; 1 MiB response; no redirects.\nCollect status, byte count and SHA256; response bodies and headers are discarded.\nShared forwarding remains running. Repeat with --yes --review %s.", chosen.Connection, chosen.ID, chosen.Direction, chosen.Listen, chosen.Destination, r.URL, bound)}, nil
	}
	if i := slices.Index(args[5:], "--review"); i >= 0 && args[i+6] != bound {
		return nil, fmt.Errorf("chain selection changed after review; review again")
	}
	var result tunnelHTTPResult
	err = managerThrow(ctx, w, config, &result)
	return result, err
}

func savedChain(config map[string]string) map[string]any {
	return map[string]any{"apiVersion": "hovel.dev/v1alpha1", "kind": "Chain", "metadata": map[string]string{"name": "burrow-chain"}, "spec": map[string]any{"mode": "configured", "steps": []any{map[string]string{"id": "burrow", "uses": "module:burrow@0.1.0"}}, "targets": []any{map[string]string{"id": "local://burrow"}}, "config": config}}
}

func decodeTunnelHTTP(raw string) (tunnelHTTPRequest, error) {
	var r tunnelHTTPRequest
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if len(raw) > 16<<10 || d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF {
		return r, fmt.Errorf("invalid tunnel consumer request")
	}
	t := r.Tunnel
	u, err := chainURL(r.URL)
	if err != nil {
		return r, err
	}
	if t.ID != t.Connection+"/"+t.Creation || validateTunnelCommand(r.Owner.Workspace, []string{"tunnel-check", t.ID}) != nil || t.ConnectionCreation == "" || t.Session == "" || t.Generation == "" || t.Session != r.Owner.Session || t.Generation != r.Owner.Generation || t.State != "listening" || (t.Direction != "L" && t.Direction != "R" && t.Direction != "D") {
		return r, fmt.Errorf("exact live owner and tunnel identity required")
	}
	if listen, err := forwardEndpoint(t.Listen, true); err != nil || listen != t.Listen {
		return r, fmt.Errorf("invalid selected listener")
	}
	port := u.Port()
	if port == "" {
		port = "80"
	}
	destination, err := forwardEndpoint(net.JoinHostPort(u.Hostname(), port), false)
	if err != nil || (t.Direction != "D" && destination != t.Destination) || (t.Direction == "D" && t.Destination != "") {
		return r, fmt.Errorf("HTTP URL must name the selected forward's fixed destination; SOCKS accepts an explicit destination")
	}
	return r, nil
}

func (m *manager) chainInventory(req hovel.PayloadCommandRequest) (hovel.PayloadCommandResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || len(req.Args) != 2 || req.Args[0] != m.Generation {
		return hovel.PayloadCommandResult{}, fmt.Errorf("exact chain owner and connection creation required")
	}
	s := m.connections[req.Args[1]]
	if s == nil {
		return hovel.PayloadCommandResult{}, fmt.Errorf("selected connection unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := launch.VerifyReservation(c, m.Workspace, m.dir); err != nil {
		return hovel.PayloadCommandResult{}, err
	}
	if err := launch.VerifyReservation(c, m.Workspace, s.dir); err != nil {
		return hovel.PayloadCommandResult{}, err
	}
	if s.closed || s.state.State != "connected" || s.checkMaster() != nil {
		return hovel.PayloadCommandResult{}, fmt.Errorf("selected connection is not live")
	}
	out := chainSelection{Owner: m.managerIdentity, Connection: s.state, Tunnels: []Tunnel{}}
	for _, t := range s.tunnels {
		if t.State != "removed" {
			if t.State == "listening" {
				if t.Direction == "R" {
					actual, err := s.reverseListeners(t.Listen)
					if err != nil || !slices.Equal(actual, []string{t.Listen}) {
						t.State = "unverified"
					}
				} else if live, err := s.proxyListener(t.Listen); err != nil || !live {
					t.State = "unverified"
				}
			}
			out.Tunnels = append(out.Tunnels, t)
		}
	}
	slices.SortFunc(out.Tunnels, func(a, b Tunnel) int { return strings.Compare(a.ID, b.ID) })
	b, err := json.Marshal(out)
	return hovel.PayloadCommandResult{Command: req.Command, Stdout: string(b)}, err
}

func tunnelHTTPAdapter(ctx *hovel.Context, w string) (hovel.Result, error) {
	raw := ctx.InputString("request", "")
	r, err := decodeTunnelHTTP(raw)
	if err != nil || r.Owner.Workspace != w || r.Owner.Session != ctx.InputString("session", "") || r.Owner.Generation != ctx.InputString("generation", "") || digest(raw) != ctx.InputString("review", "") {
		return hovel.Result{}, fmt.Errorf("changed tunnel consumer selection refused")
	}
	c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := ownerCommand(c, w, r.Owner.Session, "tunnel-http", []string{raw, digest(raw), ctx.RunID})
	if err != nil {
		return hovel.Result{}, err
	}
	var result tunnelHTTPResult
	if json.Unmarshal([]byte(out.Stdout), &result) != nil || result.RunID != ctx.RunID || result.Selection != r.Tunnel || result.URL != r.URL {
		return hovel.Result{}, fmt.Errorf("tunnel consumer result correlation refused")
	}
	artifact := hovel.WithArtifacts(hovel.JSONArtifact("tunnel-http", result))
	if result.State != "succeeded" {
		return hovel.Failed(out.Stdout, artifact), nil
	}
	return hovel.Ok(map[string]any{"result": result}, hovel.WithSummary(out.Stdout), artifact), nil
}

func (m *manager) consumeTunnel(req hovel.PayloadCommandRequest) (hovel.PayloadCommandResult, error) {
	if len(req.Args) != 3 || req.Args[2] == "" || digest(req.Args[0]) != req.Args[1] {
		return hovel.PayloadCommandResult{}, fmt.Errorf("reviewed request and run correlation required")
	}
	r, err := decodeTunnelHTTP(req.Args[0])
	if err != nil {
		return hovel.PayloadCommandResult{}, err
	}
	m.mu.Lock()
	s := m.connections[r.Tunnel.ConnectionCreation]
	valid := !m.closed && r.Owner == m.managerIdentity && s != nil
	m.mu.Unlock()
	if !valid {
		return hovel.PayloadCommandResult{}, fmt.Errorf("selected owner unavailable; no recreation attempted")
	}
	// ponytail: removal/close waits for bounded readers; use per-flow cancellation
	// if this ever becomes a general stream consumer.
	s.tunnelUse.RLock()
	defer s.tunnelUse.RUnlock()
	c, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	s.mu.Lock()
	t, exists := s.tunnels[r.Tunnel.ID]
	state := s.state
	if !exists || t != r.Tunnel || s.closed || s.state.State != "connected" {
		s.mu.Unlock()
		return hovel.PayloadCommandResult{}, fmt.Errorf("selected tunnel stale or unavailable; no fallback")
	}
	err = launch.VerifyReservation(c, m.Workspace, m.dir)
	if err == nil {
		err = launch.VerifyReservation(c, m.Workspace, s.dir)
	}
	if err == nil {
		err = s.checkMaster()
	}
	if err == nil && t.Direction == "R" {
		var actual []string
		actual, err = s.reverseListeners(t.Listen)
		if err == nil && !slices.Equal(actual, []string{t.Listen}) {
			err = fmt.Errorf("selected reverse exposure changed or unverified")
		}
	} else if err == nil {
		var live bool
		live, err = s.proxyListener(t.Listen)
		if err == nil && !live {
			err = fmt.Errorf("selected listener ownership unverified")
		}
	}
	s.mu.Unlock()
	if err != nil {
		return hovel.PayloadCommandResult{}, err
	}
	audit, err := launch.BeginAudit(m.Workspace, "chain http", t.ID, r)
	if err != nil {
		return hovel.PayloadCommandResult{}, err
	}
	result := tunnelHTTPResult{RunID: req.Args[2], Selection: t, URL: r.URL, State: "failed"}
	endpoint := consumerEndpoint(t.Listen)
	transport := &http.Transport{DisableKeepAlives: true, DisableCompression: true, MaxResponseHeaderBytes: 32 << 10, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", endpoint)
	}}
	if t.Direction == "D" {
		proxy, _ := url.Parse("socks5h://" + endpoint)
		transport.Proxy = http.ProxyURL(proxy)
	}
	if t.Direction == "R" {
		stream, closeStream, err := reverseHTTPStream(c, state, endpoint)
		if err != nil {
			return hovel.PayloadCommandResult{}, fmt.Errorf("remote-origin consumer could not start; no new login attempted")
		}
		defer closeStream()
		transport.DialContext = func(context.Context, string, string) (net.Conn, error) { return stream, nil }
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, _ := http.NewRequestWithContext(c, "GET", r.URL, nil)
	response, err := client.Do(request)
	if err == nil {
		result.StatusCode = response.StatusCode
		data, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		response.Body.Close()
		result.Bytes = len(data)
		if readErr == nil && len(data) <= 1<<20 {
			result.SHA256 = digest(string(data))
			result.State = "succeeded"
		}
		clear(data)
	}
	if result.State != "succeeded" {
		result.Detail = "selected tunnel HTTP request failed, timed out or exceeded 1 MiB; no response content retained"
	}
	if err := audit.Finish(result, nil); err != nil {
		return hovel.PayloadCommandResult{}, err
	}
	b, err := json.Marshal(result)
	return hovel.PayloadCommandResult{Command: req.Command, Stdout: string(b)}, err
}

func consumerEndpoint(listen string) string {
	host, port, _ := net.SplitHostPort(listen)
	if host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	return net.JoinHostPort(host, port)
}

// The SSH server opens -W's connection to its own reverse listener. A socket
// pair gives net/http a normal deadline-capable stream without remote helpers.
func reverseHTTPStream(ctx context.Context, state State, endpoint string) (net.Conn, func(), error) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	local := os.NewFile(uintptr(fds[0]), "reverse-http")
	peer := os.NewFile(uintptr(fds[1]), "reverse-ssh")
	defer local.Close()
	defer peer.Close()
	stream, err := net.FileConn(local)
	if err != nil {
		return nil, nil, err
	}
	cmd := fileSSH(ctx, state, "-W", endpoint, "unused")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = peer, peer, io.Discard
	if err := cmd.Start(); err != nil {
		stream.Close()
		return nil, nil, err
	}
	return stream, func() { stream.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }, nil
}

type chainConnectionRequest struct {
	Settings Config `json:"settings"`
	Preview  string `json:"preview"`
}

// Connection creation is a separate, explicitly confirmed chain operation.
// Tunnel consumers have no path to this action and never provision resources.
func chainConnectAdapter(ctx *hovel.Context, w string) (hovel.Result, error) {
	raw := ctx.InputString("request", "")
	var r chainConnectionRequest
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if len(raw) > 64<<10 || d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF || digest(raw) != ctx.InputString("review", "") {
		return hovel.Result{}, fmt.Errorf("invalid reviewed chain connection request")
	}
	c := r.Settings
	if c.Workspace != w || c.Validate() != nil || c.Prompt || c.PromptSocket != "" || c.ProxyPort != 0 || c.Review != "" {
		return hovel.Result{}, fmt.Errorf("explicit workspace and connection-only key/agent settings required")
	}
	operation, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, preview, err := c.review(operation, "connect")
	if err != nil || preview != r.Preview {
		return hovel.Result{}, fmt.Errorf("SSH settings changed since chain export; export and confirm again")
	}
	socket, _ := launch.ConnectionPath(w, c.Name)
	if _, err := os.Lstat(filepath.Dir(socket)); !os.IsNotExist(err) {
		return hovel.Result{}, fmt.Errorf("connection name is already reserved; no adoption or implicit reconnect")
	}
	state, err := connectManaged(operation, c, preview)
	if err != nil {
		return hovel.Result{}, err
	}
	for state.State != "connected" && state.State != "lost" && operation.Err() == nil {
		select {
		case <-operation.Done():
		case <-time.After(100 * time.Millisecond):
		}
		if operation.Err() != nil {
			break
		}
		next, err := selected(operation, w, state.Name)
		if err != nil || next.Session != state.Session || next.Generation != state.Generation || next.Creation != state.Creation {
			cancel()
			break
		}
		state = next
	}
	if state.State != "connected" || operation.Err() != nil {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if err := closeOwned(cleanup, w, state); err != nil {
			return hovel.Result{}, fmt.Errorf("chain SSH authentication failed or unverified; exact attempt %s cleanup uncertain: %w", state.Creation, err)
		}
		return hovel.Result{}, fmt.Errorf("chain SSH authentication failed or timed out; exact attempt closed; check key/agent, endpoint and account; retry explicitly")
	}
	result := map[string]any{"runID": ctx.RunID, "connection": state}
	b, _ := json.Marshal(result)
	return hovel.Ok(result, hovel.WithSummary(string(b)), hovel.WithArtifacts(hovel.JSONArtifact("chain-connection", result))), nil
}
