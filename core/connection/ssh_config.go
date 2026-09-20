package connection

import (
	"context"
	"encoding/json"
	"fmt"
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
	defer launch.Phase("ssh-resolve")()
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
	c.authOptions = nil
	for _, line := range strings.Split(string(out.data), "\n") {
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		switch key {
		case "passwordauthentication", "pubkeyauthentication", "preferredauthentications":
			if !regexp.MustCompile(`^[A-Za-z0-9,@._+-]+$`).MatchString(value) {
				return c, fmt.Errorf("unsupported SSH authentication configuration")
			}
			c.authOptions = append(c.authOptions, key+" "+value)
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
	if c.PasswordAuth {
		c.Agent = ""
		c.authOptions = []string{"PasswordAuthentication yes", "PubkeyAuthentication no", "PreferredAuthentications password"}
		return c, c.Validate()
	}
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

func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// A generated -F file applies the same accepted LazySSH host policy to every hop. Native
// ssh -W implements the ProxyJump transport without inheriting arbitrary config.
func (original Config) generated(ctx context.Context) (Config, []byte, error) {
	frontendAgent := original.Agent
	c, e := original.resolve(ctx)
	if e != nil {
		return c, nil, e
	}
	hops := []Config{c}
	seen := map[string]bool{}
	for i := 0; i < len(hops); i++ {
		if hops[i].Jump == "" {
			continue
		}
		if len(hops) >= 9 {
			return c, nil, fmt.Errorf("jump chain exceeds eight hops or contains a cycle")
		}
		parts := strings.Split(hops[i].Jump, ",")
		last := parts[len(parts)-1]
		if seen[last] {
			return c, nil, fmt.Errorf("jump chain contains a cycle")
		}
		seen[last] = true
		match := regexp.MustCompile(`^(?:([A-Za-z0-9_][A-Za-z0-9_.-]*)@)?(\[[A-Za-z0-9:]+\]|[A-Za-z0-9][A-Za-z0-9_.-]*)(?::([0-9]+))?$`).FindStringSubmatch(last)
		if match == nil {
			return c, nil, fmt.Errorf("invalid jump host; use [USER@]HOST[:PORT]")
		}
		jump := Config{Workspace: c.Workspace, Name: c.Name, Host: strings.Trim(match[2], "[]"), User: match[1], SSHConfig: c.SSHConfig, Agent: frontendAgent, AgentExplicit: c.AgentExplicit}
		if jump.User == "" {
			jump.User = "-"
		}
		if match[3] != "" {
			jump.Port, _ = strconv.Atoi(match[3])
			if jump.Port < 1 || jump.Port > 65535 {
				return c, nil, fmt.Errorf("invalid jump port")
			}
		}
		if len(parts) > 1 {
			jump.Jump = strings.Join(parts[:len(parts)-1], ",")
		}
		jump, e = jump.resolve(ctx)
		if e != nil {
			return c, nil, e
		}
		hops = append(hops, jump)
	}
	var b strings.Builder
	for i, hop := range hops {
		fmt.Fprintf(&b, "Host burrow-hop-%d\n HostName %s\n User %s\n Port %d\n", i, hop.Host, hop.User, hop.Port)
		for _, option := range hop.authOptions {
			fmt.Fprintf(&b, " %s\n", option)
		}
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
			// Jump descendants must never receive an automatically supplied target password,
			// even when another hop happens to use the same username and hostname.
			b.WriteString(" ProxyCommand ")
			if c.PasswordAuth {
				b.WriteString("/usr/bin/env BURROW_PASSWORD_PROMPT= ")
			}
			fmt.Fprintf(&b, "/usr/bin/ssh -F %s -W %s burrow-hop-%d\n", shellQuote(configPath(c)), shellQuote(fmt.Sprintf("[%s]:%d", hop.Host, hop.Port)), i+1)
		}
	}
	b.WriteString("Host *\n StrictHostKeyChecking no\n UserKnownHostsFile /dev/null\n GlobalKnownHostsFile /dev/null\n UpdateHostKeys no\n CheckHostIP no\n HashKnownHosts no\n ControlMaster no\n ControlPersist no\n ClearAllForwardings yes\n ForwardAgent no\n PermitLocalCommand no\n RequestTTY no\n ConnectTimeout 8\n ServerAliveInterval 2\n ServerAliveCountMax 2\n NumberOfPasswordPrompts 3\n PreferredAuthentications publickey,password\n")
	return c, []byte(b.String()), nil
}

func configPath(c Config) string {
	socket, _ := launch.ConnectionPath(c.Workspace, c.Name)
	return filepath.Join(filepath.Dir(socket), "ssh_config")
}
func (c Config) sshArgs() []string {
	socket, _ := launch.ConnectionPath(c.Workspace, c.Name)
	args := []string{"-F", configPath(c), "-M", "-N", "-T", "-S", socket, "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no"}
	if c.Key != "" {
		args = append(args, "-i", c.Key)
	}
	if c.ProxyPort != 0 {
		// Only the master receives forwarding. Jump children retain the generated
		// ClearAllForwardings=yes policy; user-config forwards are never imported.
		args = append(args, "-o", "ClearAllForwardings=no", "-o", "ExitOnForwardFailure=yes", "-D", fmt.Sprintf("127.0.0.1:%d", c.ProxyPort))
	}
	return append(args, "--", "burrow-hop-0")
}
func (c Config) reviewDigest(verb, config string) string {
	c.Review = ""
	c.PromptSocket = ""
	raw, _ := json.Marshal(c)
	return digest(verb + "\x00" + string(raw) + "\x00" + config)
}
func (c Config) review(ctx context.Context, verb string) (string, string, error) {
	resolved, config, e := c.generated(ctx)
	if e != nil {
		return "", "", e
	}
	args := append([]string{"/usr/bin/ssh"}, c.sshArgs()...)
	for i := range args {
		args[i] = shellQuote(args[i])
	}
	proxy := "Off"
	if c.ProxyPort != 0 {
		proxy = fmt.Sprintf("SOCKS4/5 · 127.0.0.1:%d", c.ProxyPort)
	}
	text := fmt.Sprintf("SSH command:\n%s\n\n%s %s\nEndpoint: %s@%s:%d\nSOCKS proxy: %s\nJump: %s\nKey: %s\nAgent: %s\n\nHost trust: verification disabled; known-host writes discarded.", strings.Join(args, " "), verb, c.Name, resolved.User, resolved.Host, resolved.Port, proxy, displaySetting(resolved.Jump, "none"), displaySetting(c.Key, "SSH config/default identities"), displaySetting(resolved.Agent, "none"))
	if c.PasswordAuth {
		text += "\nAuthentication: password only (private frontend)"
	}
	return text, string(config), nil
}
