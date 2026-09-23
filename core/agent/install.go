// Package agent installs the release's standalone operator skills, offline.
package agent

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

//go:embed burrow-agent.zip
var bundled []byte

const receiptName = ".burrow-installed.json"

type Manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	Version       string            `json:"version"`
	Source        string            `json:"source"`
	Upstream      string            `json:"upstream"`
	Files         map[string]string `json:"files"`
}

type SkillResult struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Action string `json:"action"`
	State  string `json:"state"`
	Backup string `json:"backup,omitempty"`
}

type Result struct {
	Version      string        `json:"version"`
	BundleSHA256 string        `json:"bundleSHA256"`
	Source       string        `json:"source"`
	Provenance   string        `json:"provenance"`
	Upstream     string        `json:"upstream"`
	Destination  string        `json:"destination"`
	DryRun       bool          `json:"dryRun"`
	Complete     bool          `json:"complete"`
	Skills       []SkillResult `json:"skills"`
}

type receipt struct {
	SchemaVersion int               `json:"schemaVersion"`
	Version       string            `json:"version"`
	BundleSHA256  string            `json:"bundleSHA256"`
	Files         map[string]string `json:"files"`
}

func sum(data []byte) string { hash := sha256.Sum256(data); return hex.EncodeToString(hash[:]) }

func decode(data []byte, into any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(into); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("unexpected trailing JSON")
	}
	return nil
}

// Missing descendants are allowed during preflight, but every existing ancestor
// must be a real directory. Do not clean away a symlink followed by '..'.
func directory(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("use an absolute canonical directory: %q", path)
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && !info.IsDir() {
			return fmt.Errorf("refused symlink or non-directory: %q", current)
		}
		if current == "/" {
			return nil
		}
	}
}

func destination(host, scope string) (string, error) {
	names := map[string]string{"claude": ".claude", "codex": ".agents", "opencode": ".opencode"}
	if names[host] == "" || (scope != "user" && scope != "project") {
		return "", fmt.Errorf("expected agent install claude|codex|opencode --scope user|project [--source PATH] [--dry-run]")
	}
	base, err := os.UserHomeDir()
	if scope == "project" {
		base, err = os.Getwd()
	}
	if err != nil {
		return "", err
	}
	if err = directory(base); err != nil {
		return "", err
	}
	base = filepath.Join(base, names[host])
	if scope == "user" {
		if host == "claude" && os.Getenv("CLAUDE_CONFIG_DIR") != "" {
			base = os.Getenv("CLAUDE_CONFIG_DIR")
		}
		if host == "opencode" {
			config := os.Getenv("XDG_CONFIG_HOME")
			if config == "" {
				home, _ := os.UserHomeDir()
				config = filepath.Join(home, ".config")
			}
			if err = directory(config); err != nil {
				return "", err
			}
			base = filepath.Join(config, "opencode")
		}
	}
	if err = directory(base); err != nil {
		return "", err
	}
	return filepath.Join(base, "skills"), nil
}

// readTree bounds memory and refuses symlinks/special entries, including hidden
// files. Both the embedded archive and trusted local bundles use this validator.
func readTree(tree fs.FS) (map[string][]byte, error) {
	files := map[string][]byte{}
	total := 0
	err := fs.WalkDir(tree, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("refused symlink or special entry: %q", path)
		}
		file, err := tree.Open(path)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
		file.Close()
		if err != nil {
			return err
		}
		total += len(data)
		if len(data) > 1<<20 || total > 8<<20 || len(files) >= 256 {
			return fmt.Errorf("skill bundle exceeds 1 MiB/file, 8 MiB total or 256 files")
		}
		files[path] = data
		return nil
	})
	return files, err
}

func diskTree(path string) (map[string][]byte, error) {
	if err := directory(path); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return readTree(root.FS())
}

var skillName = regexp.MustCompile(`^burrow(?:-[a-z0-9]+)*$`)
var version = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// The canonical bundle uses Hovel's scalar frontmatter and string metadata.
// Refuse other YAML constructs instead of guessing how clients will parse them.
func metadata(data []byte, name, bundleVersion string) error {
	text := string(data)
	if !utf8.Valid(data) || !strings.HasPrefix(text, "---\n") {
		return fmt.Errorf("missing UTF-8 frontmatter for %s", name)
	}
	header, body, ok := strings.Cut(text[4:], "\n---\n")
	if !ok || strings.TrimSpace(body) == "" {
		return fmt.Errorf("missing skill body for %s", name)
	}
	values := map[string]string{}
	meta := false
	for _, line := range strings.Split(header, "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if line == "metadata:" && !meta {
			meta = true
			continue
		}
		if meta {
			if !strings.HasPrefix(key, "  ") {
				return fmt.Errorf("invalid metadata for %s", name)
			}
			key = strings.TrimPrefix(key, "  ")
			if key != "burrow-skill-version" && key != "burrow-cli-contract" {
				return fmt.Errorf("unsupported metadata for %s", name)
			}
		} else if key != "name" && key != "description" && key != "compatibility" {
			return fmt.Errorf("unsupported frontmatter for %s", name)
		}
		if !ok || values[key] != "" || strings.TrimSpace(value) != value || value == "" || strings.IndexFunc(value, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0 {
			return fmt.Errorf("invalid or duplicate frontmatter for %s", name)
		}
		if strings.HasPrefix(value, `"`) {
			if err := json.Unmarshal([]byte(value), &value); err != nil {
				return fmt.Errorf("invalid quoted metadata for %s", name)
			}
		} else if meta || !((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')) || strings.ContainsAny(value, "[]{}&*!|>#'\"") || strings.Contains(value, ": ") || strings.HasSuffix(value, ":") || slices.Contains([]string{"null", "true", "false", "yes", "no", "on", "off"}, strings.ToLower(value)) {
			return fmt.Errorf("use plain text starting with a letter or JSON-quoted strings; version metadata must be quoted: %s", name)
		}
		values[key] = value
	}
	if !meta || len(values) != 5 || values["name"] != name || len(values["description"]) == 0 || utf8.RuneCountInString(values["description"]) > 1024 || values["compatibility"] == "" || values["burrow-skill-version"] != bundleVersion || values["burrow-cli-contract"] != "1" {
		return fmt.Errorf("invalid name, description, compatibility or version metadata for %s", name)
	}
	return nil
}

func hashes(files map[string][]byte) map[string]string {
	out := map[string]string{}
	for path, data := range files {
		out[path] = sum(data)
	}
	return out
}

func load(source string) (Manifest, map[string]map[string][]byte, string, error) {
	var manifest Manifest
	var files map[string][]byte
	var err error
	if source == "" {
		var archive *zip.Reader
		archive, err = zip.NewReader(bytes.NewReader(bundled), int64(len(bundled)))
		if err == nil {
			files, err = readTree(archive)
		}
	} else {
		files, err = diskTree(source)
	}
	if err != nil {
		return manifest, nil, "", err
	}
	raw := files["burrow-agent.json"]
	if err = decode(raw, &manifest); err != nil {
		return manifest, nil, "", fmt.Errorf("invalid bundle manifest: %w", err)
	}
	delete(files, "burrow-agent.json")
	if manifest.SchemaVersion != 1 || !version.MatchString(manifest.Version) || manifest.Source != "https://github.com/Bochner/burrow/tree/main/agent" || manifest.Upstream == "" || len(files) == 0 || !maps.Equal(hashes(files), manifest.Files) {
		return manifest, nil, "", fmt.Errorf("invalid bundle metadata or content checksums")
	}
	skills := map[string]map[string][]byte{}
	for path, data := range files {
		parts := strings.SplitN(path, "/", 3)
		if len(parts) != 3 || parts[0] != "skills" || !skillName.MatchString(parts[1]) || len(parts[1]) > 64 || !fs.ValidPath(parts[2]) || strings.Split(parts[2], "/")[0] == receiptName {
			return manifest, nil, "", fmt.Errorf("invalid bundle path: %q", path)
		}
		if skills[parts[1]] == nil {
			skills[parts[1]] = map[string][]byte{}
		}
		skills[parts[1]][parts[2]] = data
	}
	for name, entries := range skills {
		if err = metadata(entries["SKILL.md"], name, manifest.Version); err != nil {
			return manifest, nil, "", err
		}
	}
	return manifest, skills, sum(raw), nil
}

// Install preflights the whole suite, stages each replacement and preserves old
// versions outside discovery. It never starts a daemon, client or network call.
func Install(host, scope, source string, dryRun bool) (result Result, err error) {
	dest, err := destination(host, scope)
	if err != nil {
		return result, err
	}
	backupRoot := filepath.Join(filepath.Dir(dest), "burrow-skill-backups")
	for _, path := range []string{dest, backupRoot} {
		if err = directory(path); err != nil {
			return result, err
		}
	}
	manifest, incoming, digest, err := load(source)
	if err != nil {
		return result, err
	}
	result = Result{Version: manifest.Version, BundleSHA256: digest, Source: source, Provenance: manifest.Source, Upstream: manifest.Upstream, Destination: dest, DryRun: dryRun, Skills: []SkillResult{}}
	if source == "" {
		result.Source = "embedded release bundle (no network or cache)"
	}
	previous := map[string]map[string][]byte{}
	for _, name := range slices.Sorted(maps.Keys(incoming)) {
		target := filepath.Join(dest, name)
		old, e := diskTree(target)
		if e != nil && !os.IsNotExist(e) {
			return result, e
		}
		previous[name] = old
		action := "install"
		if old != nil {
			content := maps.Clone(old)
			delete(content, receiptName)
			if maps.Equal(hashes(content), hashes(incoming[name])) {
				action = "unchanged"
			} else {
				var installed receipt
				if decode(old[receiptName], &installed) != nil || installed.SchemaVersion != 1 || !version.MatchString(installed.Version) || len(installed.BundleSHA256) != 64 || !maps.Equal(installed.Files, hashes(content)) {
					return result, fmt.Errorf("preserved edited/unmanaged skill; move it aside and reconcile before retrying: %q", target)
				}
				action = "update"
			}
		}
		result.Skills = append(result.Skills, SkillResult{Name: name, Path: target, Action: action, State: "planned"})
	}
	if dryRun {
		result.Complete = true
		return result, nil
	}
	for i := range result.Skills {
		skill := &result.Skills[i]
		if skill.Action == "unchanged" {
			skill.State = "unchanged"
			continue
		}
		skill.State = "failed"
		if err = replace(dest, backupRoot, skill, previous[skill.Name], incoming[skill.Name], manifest.Version, digest); err != nil {
			return result, err
		}
		skill.State = "applied"
	}
	result.Complete = true
	return result, nil
}

func rename(source, target string) error {
	return unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE)
}

func replace(dest, backupRoot string, skill *SkillResult, previous, incoming map[string][]byte, version, digest string) error {
	for _, path := range []string{dest, backupRoot} {
		if err := directory(path); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(dest), ".burrow-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	staged := filepath.Join(stage, skill.Name)
	content := maps.Clone(incoming)
	content[receiptName], _ = json.Marshal(receipt{SchemaVersion: 1, Version: version, BundleSHA256: digest, Files: hashes(incoming)})
	for path, data := range content {
		path = filepath.Join(staged, path)
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err = os.WriteFile(path, data, 0644); err != nil {
			return err
		}
	}
	current, err := diskTree(skill.Path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if (current == nil) != (previous == nil) || !maps.Equal(hashes(current), hashes(previous)) {
		return fmt.Errorf("skill changed during installation: %q", skill.Path)
	}
	// ponytail: per-skill recovery, not a suite transaction or protection against
	// concurrent editors. Add stronger isolation only if that contract is needed.
	if previous != nil {
		if err = os.MkdirAll(backupRoot, 0700); err != nil {
			return err
		}
		backup, err := os.MkdirTemp(backupRoot, skill.Name+"-")
		if err != nil {
			return err
		}
		backupPath := filepath.Join(backup, skill.Name)
		if err = rename(skill.Path, backupPath); err != nil {
			return fmt.Errorf("backup failed for %q: %w", skill.Path, err)
		}
		skill.Backup = backupPath
	}
	if err = rename(staged, skill.Path); err != nil {
		if skill.Backup != "" {
			if restore := rename(skill.Backup, skill.Path); restore != nil {
				return fmt.Errorf("replacement failed: %v; restore failed: %v; previous version remains at %q", err, restore, skill.Backup)
			}
			skill.Backup = ""
			return fmt.Errorf("replacement failed; previous version restored at %q: %w", skill.Path, err)
		}
		return err
	}
	return nil
}
