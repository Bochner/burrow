package connection

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Bochner/burrow/core/launch"
)

// Profile is a portable allowlist, never an owner/session or authentication snapshot.
type Profile struct {
	Name          string `json:"name"`
	Host          string `json:"host"`
	User          string `json:"user"`
	Port          int    `json:"port"`
	Key           string `json:"key,omitempty"`
	Agent         string `json:"agent,omitempty"`
	AgentExplicit bool   `json:"agentExplicit,omitempty"`
	SSHConfig     string `json:"sshConfig,omitempty"`
	Jump          string `json:"jump,omitempty"`
	ProxyPort     int    `json:"proxyPort,omitempty"`
}

func saved(c Config) Profile {
	p := Profile{c.Name, c.Host, c.User, c.Port, c.Key, c.Agent, c.AgentExplicit, c.SSHConfig, c.Jump, c.ProxyPort}
	if !p.AgentExplicit {
		p.Agent = ""
	}
	return p
}
func (p Profile) Args() []string {
	args := []string{"connect", p.Name, p.Host, p.User}
	if p.Port != 0 {
		args = append(args, "--port", fmt.Sprint(p.Port))
	}
	if p.ProxyPort != 0 {
		args = append(args, "-proxy", fmt.Sprint(p.ProxyPort))
	}
	for _, option := range [][2]string{{"--key", p.Key}, {"--ssh-config", p.SSHConfig}, {"--jump", p.Jump}} {
		if option[1] != "" {
			args = append(args, option[0], option[1])
		}
	}
	if p.AgentExplicit {
		args = append(args, "--agent", p.Agent)
	}
	return args
}

type Collection struct {
	Path     string    `json:"path"`
	Revision string    `json:"revision"`
	Profiles []Profile `json:"profiles"`
}
type profileFile struct {
	Version  int       `json:"version"`
	Comment  string    `json:"_comment,omitempty"`
	Example  *Profile  `json:"_example,omitempty"`
	Profiles []Profile `json:"profiles"`
}

func decodeProfiles(w string, data []byte) (profileFile, error) {
	f := profileFile{}
	if data == nil {
		return f, fmt.Errorf("collection is missing; open a workspace to create its template or load another collection")
	}
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.DisallowUnknownFields()
	if e := d.Decode(&f); e != nil {
		return f, fmt.Errorf("invalid collection; expected version 1 non-secret profiles")
	}
	if d.Decode(new(any)) != io.EOF || f.Version != 1 || f.Profiles == nil {
		return f, fmt.Errorf("invalid collection version or profiles")
	}
	names := map[string]bool{}
	entries := append([]Profile{}, f.Profiles...)
	if f.Example != nil {
		entries = append(entries, *f.Example)
	}
	for i, p := range entries {
		if i < len(f.Profiles) && names[p.Name] {
			return f, fmt.Errorf("duplicate profile name")
		}
		names[p.Name] = true
		c, _, e := Parse(w, p.Args()[1:])
		if e != nil {
			return f, e
		}
		// Validate even unused agent references rather than accepting hidden junk.
		c.Agent = p.Agent
		if e = c.Validate(); e != nil {
			return f, e
		}
	}
	sort.Slice(f.Profiles, func(i, j int) bool { return f.Profiles[i].Name < f.Profiles[j].Name })
	return f, nil
}

// Selected collection is ordinary Hovel chain configuration. Reads never create
// a chain, authenticate, resolve SSH configuration, or create a missing file.
func collectionPath(ctx context.Context, w string) (string, error) {
	var snapshot struct {
		State struct {
			Operations []struct {
				Name   string
				Chains []struct {
					Name   string
					Config map[string]string
				}
			}
		}
	}
	if e := launch.Call(ctx, w, "Snapshot", map[string]string{}, &snapshot); e != nil {
		return "", e
	}
	for _, op := range snapshot.State.Operations {
		if op.Name == "burrow" {
			for _, chain := range op.Chains {
				if chain.Name == "profiles" && chain.Config["collection"] != "" {
					return chain.Config["collection"], nil
				}
			}
		}
	}
	return filepath.Join(w, "burrow-profiles.json"), nil
}
func profileScope(ctx context.Context, w string) error {
	for _, args := range [][]string{{"op", "create", "burrow"}, {"chain", "create", "profiles"}} {
		if _, e := launch.HovelCLI(ctx, w, append([]string{"--op", "burrow", "--chain", "profiles", "--"}, args...)...); e != nil {
			return e
		}
	}
	return nil
}
func Profiles(ctx context.Context, w string) (Collection, error) {
	path, e := collectionPath(ctx, w)
	if e != nil {
		return Collection{}, e
	}
	out := Collection{Path: path, Profiles: []Profile{}}
	e = launch.Settings(ctx, path, func(data []byte) ([]byte, error) {
		if data == nil && path != filepath.Join(w, "burrow-profiles.json") {
			return nil, fmt.Errorf("selected collection is missing; restore it or load another collection")
		}
		f, e := decodeProfiles(w, data)
		out.Profiles = f.Profiles
		out.Revision = fmt.Sprintf("%x", sha256.Sum256(data))
		return nil, e
	})
	return out, e
}
func ProfileConnect(ctx context.Context, w string, args []string) ([]string, error) {
	if len(args) < 3 || args[0] != "profile" || args[1] != "connect" {
		return args, nil
	}
	if e := validateProfile(w, args); e != nil {
		return nil, e
	}
	args, options, e := profileOptions(w, args)
	if e != nil {
		return nil, e
	}
	list, e := Profiles(ctx, w)
	if e != nil {
		return nil, e
	}
	for _, p := range list.Profiles {
		if p.Name == args[2] {
			if options["as"] != "" {
				p.Name = options["as"]
			}
			return append(p.Args(), args[3:]...), nil
		}
	}
	return nil, fmt.Errorf("saved profile not found")
}
func validateProfile(w string, args []string) error {
	var e error
	args, _, e = profileOptions(w, args)
	if e != nil {
		return e
	}
	if args[0] == "profiles" || args[0] == "history" {
		if len(args) == 1 {
			return nil
		}
		return fmt.Errorf("%s takes no arguments", args[0])
	}
	if len(args) < 3 {
		return fmt.Errorf("profile requires create/edit/save/select/delete/connect/load/collection/backup and a name or path; use help")
	}
	switch args[1] {
	case "create", "edit":
		c, _, e := Parse(w, args[2:])
		if e != nil {
			return e
		}
		if c.Prompt {
			return fmt.Errorf("profiles cannot retain authentication prompts")
		}
		return nil
	case "load", "collection", "backup":
		if len(args) != 3 || !filepath.IsAbs(args[2]) || filepath.Clean(args[2]) != args[2] || strings.ContainsAny(args[2], "\x00\r\n\t") {
			return fmt.Errorf("profile path must be absolute and canonical; use help")
		}
		return nil
	case "select", "save", "delete", "connect":
		if _, e := launch.ConnectionPath(w, args[2]); e != nil {
			return e
		}
		for _, arg := range args[3:] {
			if arg != "--yes" && !(args[1] == "connect" && arg == "--prompt") {
				return fmt.Errorf("invalid profile options; secrets are not accepted")
			}
		}
		if args[1] == "select" && len(args) != 3 {
			return fmt.Errorf("profile select takes one name")
		}
		return nil
	}
	return fmt.Errorf("unknown profile command; use help")
}
func profileHistory(ctx context.Context, w string) ([]string, error) {
	var response []struct{ Source, Message string }
	e := launch.Call(ctx, w, "ActiveLogs", map[string]string{"Operation": "burrow", "Chain": "profiles"}, &response)
	out := []string{}
	for _, entry := range response {
		if entry.Source == "burrow-profiles" {
			out = append(out, entry.Message)
		}
	}
	return out, e
}
func executeProfile(ctx context.Context, w string, args []string) (any, error) {
	if args[0] == "profiles" {
		return Profiles(ctx, w)
	}
	if args[0] == "history" {
		return profileHistory(ctx, w)
	}
	result, e := changeProfile(ctx, w, args)
	if e != nil {
		return nil, e
	}
	if e = profileScope(ctx, w); e != nil {
		return nil, fmt.Errorf("profile command completed but history unavailable: %w", e)
	}
	// Canonical validated commands only; rejected text and authentication answers
	// never reach persistent history. Hovel owns retention and timestamps.
	line := CommandLine(args)
	var ignored struct{}
	e = launch.Call(ctx, w, "AppendLog", map[string]any{"Operation": "burrow", "Chain": "profiles", "Entries": []map[string]any{{"Time": time.Now().UTC(), "Kind": "event", "Level": "info", "Source": "burrow-profiles", "Message": line}}}, &ignored)
	if e != nil {
		return nil, fmt.Errorf("profile command completed but history write failed: %w", e)
	}
	return result, nil
}
func CommandLine(args []string) string {
	out := make([]string, len(args))
	for i, arg := range args {
		if strings.ContainsAny(arg, " \t'\"\\") || arg == "" {
			out[i] = "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
		} else {
			out[i] = arg
		}
	}
	return strings.Join(out, " ")
}
func changeProfile(ctx context.Context, w string, args []string) (any, error) {
	args, options, e := profileOptions(w, args)
	if e != nil {
		return nil, e
	}
	verb, name := args[1], args[2]
	if verb == "load" || verb == "collection" {
		e := launch.Settings(ctx, name, func(data []byte) ([]byte, error) {
			if data == nil {
				if verb == "load" {
					return nil, fmt.Errorf("collection does not exist")
				}
				return templateProfiles(), nil
			}
			_, e := decodeProfiles(w, data)
			return nil, e
		})
		if e != nil {
			return nil, e
		}
		if e = profileScope(ctx, w); e != nil {
			return nil, e
		}
		var ignored struct{}
		if e = launch.Call(ctx, w, "SetChainConfig", map[string]string{"Operation": "burrow", "Chain": "profiles", "Key": "collection", "Value": name}, &ignored); e != nil {
			return nil, e
		}
		return Profiles(ctx, w)
	}
	list, e := Profiles(ctx, w)
	if e != nil {
		return nil, e
	}
	if (options["revision"] != "" && options["revision"] != list.Revision) || (options["collection"] != "" && options["collection"] != list.Path) {
		return nil, fmt.Errorf("collection changed after review; inspect and review again")
	}
	if verb == "backup" {
		var data []byte
		e = launch.Settings(ctx, list.Path, func(old []byte) ([]byte, error) {
			if _, err := decodeProfiles(w, old); err != nil {
				return nil, err
			}
			data = append([]byte{}, old...)
			return nil, nil
		})
		if e != nil {
			return nil, e
		}
		e = launch.Settings(ctx, name, func(old []byte) ([]byte, error) {
			if old != nil {
				return nil, fmt.Errorf("backup destination already exists; choose a new path")
			}
			return data, nil
		})
		return map[string]string{"backup": name, "collection": list.Path}, e
	}
	yes := false
	for _, arg := range args[3:] {
		yes = yes || arg == "--yes"
	}
	var p Profile
	switch verb {
	case "select":
		for _, p := range list.Profiles {
			if p.Name == name {
				return p, nil
			}
		}
		return nil, fmt.Errorf("saved profile not found")
	case "create", "edit":
		c, approved, err := Parse(w, args[2:])
		if err != nil {
			return nil, err
		}
		p = saved(c)
		yes = approved
	case "save":
		s, e := selected(ctx, w, name)
		if e != nil {
			return nil, e
		}
		result, e := profileCommand(ctx, w, s)
		if e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(result.Stdout), &p); e != nil {
			return nil, fmt.Errorf("invalid owner settings")
		}
	}
	if verb == "save" && options["as"] != "" {
		name = options["as"]
		p.Name = name
	}
	var result any
	e = launch.Settings(ctx, list.Path, func(data []byte) ([]byte, error) {
		if data == nil && list.Path != filepath.Join(w, "burrow-profiles.json") {
			return nil, fmt.Errorf("selected collection is missing")
		}
		f, e := decodeProfiles(w, data)
		if e != nil {
			return nil, e
		}
		if fmt.Sprintf("%x", sha256.Sum256(data)) != list.Revision {
			return nil, fmt.Errorf("collection changed; review again")
		}
		index := -1
		for i, entry := range f.Profiles {
			if entry.Name == name {
				index = i
			}
		}
		if verb == "create" && index >= 0 {
			return nil, fmt.Errorf("profile exists; use profile edit")
		}
		if (verb == "edit" || verb == "delete") && index < 0 {
			return nil, fmt.Errorf("saved profile not found")
		}
		if !yes && (verb == "edit" || verb == "delete" || (verb == "save" && index >= 0)) {
			result = map[string]string{"revision": list.Revision, "collection": list.Path, "review": fmt.Sprintf("%s profile %s in %s. Live resources and evidence remain. Repeat with --yes to confirm.", verb, name, list.Path)}
			return nil, nil
		}
		if verb == "delete" {
			f.Profiles = append(f.Profiles[:index], f.Profiles[index+1:]...)
		} else if index >= 0 {
			f.Profiles[index] = p
		} else {
			f.Profiles = append(f.Profiles, p)
		}
		result = map[string]string{"state": verb, "name": name, "collection": list.Path}
		return json.MarshalIndent(f, "", "  ")
	})
	return result, e
}

func ProfileSuggestions(collection Collection) []string {
	values := []string{"profiles", "profile create ", "profile edit ", "profile save ", "profile select ", "profile delete ", "profile connect ", "profile load ", "profile collection ", "profile backup ", "history"}
	for _, p := range collection.Profiles {
		for _, verb := range []string{"select", "edit", "delete", "connect"} {
			value := "profile " + verb + " " + p.Name
			if verb == "edit" {
				value = CommandLine(append(append([]string{"profile", "edit"}, p.Args()[1:]...), "--revision", collection.Revision, "--collection", collection.Path))
			}
			values = append(values, value)
		}
	}
	return values
}

func templateProfiles() []byte {
	data, _ := json.MarshalIndent(profileFile{Version: 1, Comment: "Copy _example into profiles and edit it, or use profile create / profile save. _example is documentation only and never connects. Store settings and key references only; never passwords, passphrases or private keys.", Example: &Profile{Name: "example", Host: "192.0.2.10", User: "alice", Port: 22}, Profiles: []Profile{}}, "", "  ")
	return append(data, '\n')
}

// EnsureProfiles creates only the default template; existing operator bytes stay intact.
func EnsureProfiles(ctx context.Context, w string) error {
	return launch.Settings(ctx, filepath.Join(w, "burrow-profiles.json"), func(data []byte) ([]byte, error) {
		if data == nil {
			return templateProfiles(), nil
		}
		return nil, nil
	})
}

// Extra profile switches never enter the reusable SSH settings.
func profileOptions(w string, args []string) ([]string, map[string]string, error) {
	options := map[string]string{}
	if len(args) < 3 || args[0] != "profile" {
		return args, options, nil
	}
	out := append([]string{}, args[:3]...)
	for i := 3; i < len(args); i++ {
		name, value, assigned := strings.Cut(strings.TrimPrefix(args[i], "--"), "=")
		if !strings.HasPrefix(args[i], "--") || (name != "as" && name != "revision" && name != "collection") {
			out = append(out, args[i])
			continue
		}

		if !assigned {
			i++
			if i == len(args) {
				return nil, nil, fmt.Errorf("missing profile option value")
			}
			value = args[i]
		}
		if value == "" {
			return nil, nil, fmt.Errorf("empty profile option")
		}
		if old := options[name]; old != "" && old != value {
			return nil, nil, fmt.Errorf("conflicting profile option")
		}
		if name == "as" {
			if args[1] != "save" && args[1] != "connect" {
				return nil, nil, fmt.Errorf("--as requires save or connect")
			}
			if _, e := launch.ConnectionPath(w, value); e != nil {
				return nil, nil, e
			}
		} else {
			if args[1] != "edit" && args[1] != "delete" && args[1] != "save" {
				return nil, nil, fmt.Errorf("review switches require edit, delete or save")
			}
			if name == "revision" {
				if len(value) != 64 || strings.Trim(value, "0123456789abcdef") != "" {
					return nil, nil, fmt.Errorf("invalid collection revision")
				}
			}
			if name == "collection" && (!filepath.IsAbs(value) || filepath.Clean(value) != value || strings.ContainsAny(value, "\x00\r\n\t")) {
				return nil, nil, fmt.Errorf("invalid reviewed collection path")
			}
		}
		options[name] = value
	}
	return out, options, nil
}

// SaveOffer uses the original input retained by the authenticated owner, not
// today's resolved alias address or inherited agent socket. Unchanged saves skip.
func SaveOffer(ctx context.Context, w, name string) (bool, error) {
	s, e := selected(ctx, w, name)
	if e != nil {
		return false, e
	}
	result, e := profileCommand(ctx, w, s)
	if e != nil {
		return false, e
	}
	var p Profile
	if e = json.Unmarshal([]byte(result.Stdout), &p); e != nil {
		return false, e
	}
	list, e := Profiles(ctx, w)
	if e != nil {
		return false, e
	}
	for _, existing := range list.Profiles {
		c, _, err := Parse(w, existing.Args()[1:])
		if err == nil && saved(c) == p {
			return false, nil
		}
	}
	return true, nil
}
