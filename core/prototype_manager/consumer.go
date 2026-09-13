// Disposable #72 proof: one fixed local TCP forward and bounded HTTP consumers.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Bochner/burrow/core/launch"
	"github.com/vibepwners/hovel/sdk/go/hovel"
)

type consumerRequest struct {
	Action      string `json:"action"`
	Workspace   string `json:"workspace"`
	Session     string `json:"session"`
	Generation  string `json:"generation"`
	Connection  string `json:"connection"`
	Tunnel      string `json:"tunnel"`
	Bind        string `json:"bind"`
	Destination string `json:"destination"`
	Flow        string `json:"flow"`
	Nonce       string `json:"nonce"`
}

type consumerTunnel struct {
	Request consumerRequest
	Active  bool
}

func decodeConsumer(raw string) (consumerRequest, error) {
	var r consumerRequest
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF {
		return r, fmt.Errorf("invalid consumer request")
	}
	id := regexp.MustCompile(`^[a-f0-9]{32}$`)
	if (r.Action != "consume" && r.Action != "tunnel-open" && r.Action != "tunnel-close") || r.Workspace == "" || r.Session == "" || r.Generation == "" || !id.MatchString(r.Connection) || !id.MatchString(r.Tunnel) || !id.MatchString(r.Flow) || !regexp.MustCompile(`^[a-f0-9]{16}$`).MatchString(r.Nonce) {
		return r, fmt.Errorf("exact consumer identities required")
	}
	for _, endpoint := range []string{r.Bind, r.Destination} {
		host, port, e := net.SplitHostPort(endpoint)
		n, ne := strconv.Atoi(port)
		if e != nil || ne != nil || host != "127.0.0.1" || n < 1 || n > 65535 {
			return r, fmt.Errorf("controlled loopback endpoint required")
		}
	}
	return r, nil
}

func consumerAdapter(ctx *hovel.Context, w string) (hovel.Result, error) {
	raw, review := ctx.InputString("request", ""), ctx.InputString("review", "")
	r, e := decodeConsumer(raw)
	if e != nil || r.Action != ctx.InputString("action", "") || digest(raw) != review || r.Workspace != w || r.Session != ctx.InputString("session", "") || r.Generation != ctx.InputString("generation", "") {
		return hovel.Result{}, fmt.Errorf("approved consumer/owner binding refused")
	}
	c, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	var out hovel.PayloadCommandResult
	e = launch.Call(c, w, "RunSessionCommand", map[string]any{"SessionID": r.Session, "Request": hovel.PayloadCommandRequest{Command: ctx.InputString("action", ""), Args: []string{raw, review, ctx.RunID}}}, &out)
	if e != nil {
		return hovel.Result{}, e
	}
	var result map[string]any
	if json.Unmarshal([]byte(out.Stdout), &result) != nil || result["runID"] != ctx.RunID || result["review"] != review {
		return hovel.Result{}, fmt.Errorf("consumer result correlation refused")
	}
	result["adapterPID"] = os.Getpid()
	b, e := json.Marshal(result)
	return hovel.Ok(nil, hovel.WithSummary(string(b)), hovel.WithArtifacts(hovel.JSONArtifact("consumer-proof", result))), e
}

func (s *manager) consumerControl(req hovel.PayloadCommandRequest) (hovel.PayloadCommandResult, error) {
	if req.Command == "flow-close" || req.Command == "flow-status" {
		s.mu.Lock()
		defer s.mu.Unlock()
		if req.Command == "flow-status" {
			if s.closed || len(req.Args) != 2 || req.Args[0] != s.Generation {
				return hovel.PayloadCommandResult{}, fmt.Errorf("exact flow required")
			}
			cancel, exists := s.flows[req.Args[1]]
			if !exists {
				return hovel.PayloadCommandResult{}, fmt.Errorf("flow unknown")
			}
			b, e := json.Marshal(map[string]bool{"finished": cancel == nil})
			return hovel.PayloadCommandResult{Stdout: string(b)}, e
		}
		if s.closed || len(req.Args) != 3 || req.Args[0] != s.Generation || req.Args[2] != "confirm" || s.flows[req.Args[1]] == nil {
			return hovel.PayloadCommandResult{}, fmt.Errorf("exact active flow required")
		}
		s.flows[req.Args[1]]()
		return hovel.PayloadCommandResult{Stdout: `{"state":"cancellation-requested"}`}, nil
	}
	if len(req.Args) != 3 || digest(req.Args[0]) != req.Args[1] || req.Args[2] == "" {
		return hovel.PayloadCommandResult{}, fmt.Errorf("reviewed consumer request required")
	}
	r, e := decodeConsumer(req.Args[0])
	if e != nil || r.Action != req.Command {
		return hovel.PayloadCommandResult{}, fmt.Errorf("consumer action/request refused")
	}
	s.mu.Lock()
	if s.closed || r.Workspace != s.workspace || r.Session != s.Session || r.Generation != s.Generation || s.connections[r.Connection] == nil {
		s.mu.Unlock()
		return hovel.PayloadCommandResult{}, fmt.Errorf("selected owner/connection unavailable")
	}
	m := s.connections[r.Connection]
	if s.flows == nil {
		s.flows = map[string]context.CancelFunc{}
	}
	// ponytail: retain at most 128 used IDs for this disposable proof; durable
	// replay policy belongs to production integration, never silently evict IDs.
	if len(s.flows) >= 128 {
		s.mu.Unlock()
		return hovel.PayloadCommandResult{}, fmt.Errorf("proof flow limit reached")
	}
	if _, exists := s.flows[r.Flow]; exists {
		s.mu.Unlock()
		return hovel.PayloadCommandResult{}, fmt.Errorf("flow identity already used")
	}
	c, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	s.flows[r.Flow] = cancel
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); s.flows[r.Flow] = nil; s.mu.Unlock() }()
	// ponytail: one bounded operation per master; per-flow locks if generalized.
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.State != "connected" {
		return hovel.PayloadCommandResult{}, fmt.Errorf("connection unavailable; no fallback login")
	}
	if e = m.verify(); e != nil {
		return hovel.PayloadCommandResult{}, e
	}
	if m.tunnels == nil {
		m.tunnels = map[string]consumerTunnel{}
	}
	switch req.Command {
	case "tunnel-open":
		if _, exists := m.tunnels[r.Tunnel]; exists {
			return hovel.PayloadCommandResult{}, fmt.Errorf("tunnel identity already used")
		}
		for _, t := range m.tunnels {
			if t.Request.Bind == r.Bind {
				return hovel.PayloadCommandResult{}, fmt.Errorf("bind already owned or uncertain")
			}
		}
		// Reserve before dispatch: a lost forwarding acknowledgement must not
		// make this identity usable or permit adoption through a second request.
		m.tunnels[r.Tunnel] = consumerTunnel{Request: r}
		e = exec.CommandContext(c, "/usr/bin/ssh", "-F", "/dev/null", "-S", m.Socket, "-o", "ProxyCommand=/bin/false", "-O", "forward", "-L", r.Bind+":"+r.Destination, "unused").Run()
		if e == nil {
			m.tunnels[r.Tunnel] = consumerTunnel{Request: r, Active: true}
		}
	case "consume", "tunnel-close":
		tunnel, exists := m.tunnels[r.Tunnel]
		t := tunnel.Request
		if !exists || !tunnel.Active || t.Bind != r.Bind || t.Destination != r.Destination {
			return hovel.PayloadCommandResult{}, fmt.Errorf("selected tunnel/destination unavailable")
		}
		if req.Command == "tunnel-close" {
			m.tunnels[r.Tunnel] = consumerTunnel{Request: t}
			e = exec.CommandContext(c, "/usr/bin/ssh", "-F", "/dev/null", "-S", m.Socket, "-o", "ProxyCommand=/bin/false", "-O", "cancel", "-L", t.Bind+":"+t.Destination, "unused").Run()
			if e == nil {
				m.tunnels[r.Tunnel] = consumerTunnel{}
			}
		} else {
			transport := &http.Transport{DisableKeepAlives: true, DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("redirect refused") }}
			request, _ := http.NewRequestWithContext(c, "GET", "http://"+t.Bind+"/"+r.Nonce, nil)
			response, err := client.Do(request)
			e = err
			if err == nil {
				data, err := io.ReadAll(io.LimitReader(response.Body, 128))
				response.Body.Close()
				if err != nil || response.StatusCode != 200 || string(data) != r.Nonce {
					e = fmt.Errorf("fixture nonce exchange failed")
				}
			}
		}
	}
	if e != nil {
		return hovel.PayloadCommandResult{}, fmt.Errorf("selected flow failed: %w", e)
	}
	b, e := json.Marshal(map[string]any{"selection": r, "nonce": r.Nonce, "ownerPID": s.OwnerPID, "masterPID": m.PID, "runID": req.Args[2], "review": req.Args[1]})
	return hovel.PayloadCommandResult{Command: req.Command, Stdout: string(b)}, e
}

func (module) DescribeMesh(hovel.MeshDescribeRequest) (hovel.MeshDescriptor, error) {
	return hovel.MeshDescriptor{Name: "burrow", Summary: "Negative #72 foreign-session adoption probe; no execution tasks"}, nil
}

func (module) OpenMeshStream(_ *hovel.MeshContext, r hovel.MeshStreamRequest) (hovel.SessionRef, error) {
	// Investigation only: prove that an existing owner's ID cannot be adopted
	// by a fresh provider. This does not claim a working generic TCP stream.
	raw, _ := r.Config["request"].(string)
	selected, e := decodeConsumer(raw)
	host, port, _ := net.SplitHostPort(selected.Destination)
	if e != nil || r.Protocol != "tcp" || r.DestinationHost != host || strconv.Itoa(r.DestinationPort) != port {
		return hovel.SessionRef{}, fmt.Errorf("bounded TCP investigation required")
	}
	c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	info, e := launch.Status(c, selected.Workspace)
	if e != nil || os.Getppid() != info.PID {
		return hovel.SessionRef{}, fmt.Errorf("verified daemon required")
	}
	var o observation
	if e = control(c, selected.Workspace, observation{Session: selected.Session}, "identity", nil, &o); e != nil || o.Generation != selected.Generation || o.OwnerPID == os.Getpid() {
		return hovel.SessionRef{}, fmt.Errorf("exact retained owner required")
	}
	return hovel.SessionRef{ID: o.Session, ModuleID: "burrow@0.1.0", RunID: o.RunID, Kind: "manager-proof", Transport: "tcp", State: "open"}, nil
}
