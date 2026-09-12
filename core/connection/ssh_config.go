package connection

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Bochner/burrow/core/launch"
)

// OpenSSH evaluates the operator's trusted config (including Include/Match).
// Only endpoint, identity and jump settings enter the retained master config;
// forwarding, commands and trust overrides cannot change Burrow's contracts.
func (c Config) resolve(ctx context.Context) (Config, error) {
	config := c.SSHConfig
	if config == "" {
		config = "/dev/null"
	}
	if _, e := os.Stat(config); os.IsNotExist(e) {
		home, _ := os.UserHomeDir()
		if config != filepath.Join(home, ".ssh", "config") {
			return c, fmt.Errorf("selected SSH configuration unavailable")
		}
		config = "/dev/null"
	}
	args := []string{"-G", "-F", config}
	if c.User != "-" {
		args = append(args, "-l", c.User)
	}
	if c.Port != 0 {
		args = append(args, "-p", strconv.Itoa(c.Port))
	}
	args = append(args, "--", c.Host)
	cmd := exec.CommandContext(ctx, "/usr/bin/ssh", args...)
	var out limitedBuffer
	cmd.Stdout = &out
	if cmd.Run() != nil {
		return c, fmt.Errorf("SSH configuration could not be resolved; check Host/Match settings")
	}
	var identities []string
	for _, line := range strings.Split(string(out.data), "\n") {
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		switch key {
		case "hostname":
			c.Host = value
		case "user":
			c.User = value
		case "port":
			c.Port, _ = strconv.Atoi(value)
		case "identityfile":
			if value != "none" {
				identities = append(identities, value)
			}
		case "identityagent":
			if !c.AgentExplicit && value != "SSH_AUTH_SOCK" {
				c.Agent = value
				if value == "none" {
					c.Agent = ""
				}
			}
		case "identitiesonly":
			c.identitiesOnly = value == "yes"
		case "proxyjump":
			if c.Jump == "" && value != "none" {
				c.Jump = value
			}
		case "proxycommand":
			if value != "none" {
				return c, fmt.Errorf("ProxyCommand is not supported; select ProxyJump")
			}
		}
	}
	c.identities = nil
	if c.Key != "" {
		identities = []string{c.Key}
	}
	for _, identity := range identities {
		path := expandIdentity(identity, c)
		candidate := c
		candidate.Key = path
		if e := candidate.Validate(); e != nil {
			return c, e
		}
		c.identities = append(c.identities, path)
	}
	c.Agent = expandIdentity(c.Agent, c)
	return c, c.Validate()
}

func expandIdentity(path string, c Config) string {
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(path, "~/") {
		path = filepath.Join(home, path[2:])
	}
	return strings.NewReplacer("%d", home, "%h", c.Host, "%r", c.User, "%p", strconv.Itoa(c.Port)).Replace(path)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// A generated -F file applies the same strict trust policy to every hop. Native
// ssh -W implements the ProxyJump transport without inheriting arbitrary config.
func (s *owner) sshConfig(ctx context.Context, trustPath string) ([]byte, error) {
	frontendAgent := s.config.Agent
	c, e := s.config.resolve(ctx)
	if e != nil {
		return nil, e
	}
	s.config = c
	s.state.Host, s.state.User, s.state.Port = c.Host, c.User, c.Port
	hops := []Config{c}
	seen := map[string]bool{}
	for i := 0; i < len(hops); i++ {
		if hops[i].Jump == "" {
			continue
		}
		if len(hops) >= 9 {
			return nil, fmt.Errorf("jump chain exceeds eight hops or contains a cycle")
		}
		parts := strings.Split(hops[i].Jump, ",")
		last := parts[len(parts)-1]
		if seen[last] {
			return nil, fmt.Errorf("jump chain contains a cycle")
		}
		seen[last] = true
		match := regexp.MustCompile(`^(?:([A-Za-z0-9_][A-Za-z0-9_.-]*)@)?(\[[A-Za-z0-9:]+\]|[A-Za-z0-9][A-Za-z0-9_.-]*)(?::([0-9]+))?$`).FindStringSubmatch(last)
		if match == nil {
			return nil, fmt.Errorf("invalid jump host; use [USER@]HOST[:PORT]")
		}
		jump := Config{Workspace: c.Workspace, Name: c.Name, Host: strings.Trim(match[2], "[]"), User: match[1], SSHConfig: c.SSHConfig, Agent: frontendAgent, AgentExplicit: c.AgentExplicit, KnownHosts: c.KnownHosts}
		if jump.User == "" {
			jump.User = "-"
		}
		if match[3] != "" {
			jump.Port, _ = strconv.Atoi(match[3])
			if jump.Port < 1 || jump.Port > 65535 {
				return nil, fmt.Errorf("invalid jump port")
			}
		}
		if len(parts) > 1 {
			jump.Jump = strings.Join(parts[:len(parts)-1], ",")
		}
		jump, e = jump.resolve(ctx)
		if e != nil {
			return nil, e
		}
		hops = append(hops, jump)
	}
	var b strings.Builder
	for i, hop := range hops {
		fmt.Fprintf(&b, "Host burrow-hop-%d\n HostName %s\n User %s\n Port %d\n", i, hop.Host, hop.User, hop.Port)
		for _, identity := range hop.identities {
			fmt.Fprintf(&b, " IdentityFile %s\n", strconv.Quote(identity))
		}
		if len(hop.identities) == 0 {
			b.WriteString(" IdentityFile none\n")
		}
		if hop.Key != "" || hop.identitiesOnly {
			b.WriteString(" IdentitiesOnly yes\n")
		}
		agent := hop.Agent
		if agent == "" {
			agent = "none"
		}
		fmt.Fprintf(&b, " IdentityAgent %s\n", strconv.Quote(agent))
		if i+1 < len(hops) {
			fmt.Fprintf(&b, " ProxyCommand /usr/bin/ssh -F %s -W %s burrow-hop-%d\n", shellQuote(filepath.Join(s.dir.Name(), "ssh_config")), shellQuote(fmt.Sprintf("[%s]:%d", hop.Host, hop.Port)), i+1)
		}
	}
	fmt.Fprintf(&b, "Host *\n StrictHostKeyChecking ask\n UserKnownHostsFile %s\n GlobalKnownHostsFile /dev/null\n UpdateHostKeys no\n CheckHostIP no\n HashKnownHosts no\n ControlMaster no\n ControlPersist no\n ClearAllForwardings yes\n ForwardAgent no\n PermitLocalCommand no\n RequestTTY no\n ConnectTimeout 8\n ServerAliveInterval 2\n ServerAliveCountMax 2\n NumberOfPasswordPrompts 3\n PreferredAuthentications publickey,password\n", strconv.Quote(trustPath))
	return []byte(b.String()), nil
}

func (s *owner) trustSnapshot(ctx context.Context) ([]byte, error) {
	store, e := launch.TrustStore(ctx, s.config.Workspace)
	if e != nil {
		return nil, e
	}
	defer store.Close()
	data, e := io.ReadAll(io.LimitReader(store, (1<<20)+1))
	if e != nil || len(data) > 1<<20 {
		return nil, fmt.Errorf("workspace trust snapshot unreadable or oversized")
	}
	file, e := os.Open(s.config.KnownHosts)
	if os.IsNotExist(e) {
		return data, nil
	}
	if e != nil {
		return nil, fmt.Errorf("selected known-hosts file unavailable")
	}
	defer file.Close()
	extra, e := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if e != nil || len(extra) > 1<<20 {
		return nil, fmt.Errorf("selected known-hosts file unreadable or oversized")
	}
	// The snapshot has the same bound as approval persistence. Reject its
	// combined size before starting SSH, not after credentials are offered.
	if len(data)+len(extra)+2 > 1<<20 {
		return nil, fmt.Errorf("combined trust snapshot oversized (limit 1 MiB)")
	}
	return append(append(data, '\n'), append(extra, '\n')...), nil
}

func (s *owner) saveTrust() error {
	path := filepath.Join(s.dir.Name(), "known_hosts")
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !os.SameFile(st, s.trustFile) {
		return fmt.Errorf("trust snapshot replaced; investigate manually")
	}
	data, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if e != nil || len(data) > 1<<20 || len(data) < s.trustBytes {
		return fmt.Errorf("trust snapshot unreadable or oversized")
	}
	if len(data) == s.trustBytes {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5e9)
	defer cancel()
	store, e := launch.TrustStore(ctx, s.config.Workspace)
	if e != nil {
		return e
	}
	defer store.Close()
	if _, e = store.Write(data[s.trustBytes:]); e != nil {
		return e
	}
	if e = store.Sync(); e == nil {
		s.trustBytes = len(data)
	}
	return e
}
