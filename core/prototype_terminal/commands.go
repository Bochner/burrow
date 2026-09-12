package main

// Disposable local fixture: no SSH authentication, daemon, or remote execution.
import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type entry struct {
	Name     string `json:"name"`
	Mode     string `json:"mode"`
	Size     int64  `json:"bytes"`
	Modified string `json:"modified"`
}
type result struct {
	Command string  `json:"command"`
	Target  string  `json:"target"`
	Message string  `json:"message,omitempty"`
	Entries []entry `json:"entries,omitempty"`
	Error   string  `json:"error,omitempty"`
}
type target struct {
	name, status, cwd string
	tunnels           []string
}
type transfer struct {
	source, dest *os.File
	path         string
	done, total  int64
}
type fixture struct {
	root, mode, local string
	targets           []*target
	selected          int
	copy              *transfer
	progress          string
	quit              bool
}

func newFixture() (*fixture, error) {
	root, err := os.MkdirTemp("", "burrow-terminal-")
	if err != nil {
		return nil, err
	}
	f := &fixture{root: root, mode: "manage", local: "/uploads", targets: []*target{{name: "lab", status: "DISCONNECTED", cwd: "/"}, {name: "gateway", status: "DISCONNECTED", cwd: "/"}}}
	for _, dir := range []string{"lab/var/log", "lab/etc", "gateway/var/log", "uploads", "downloads"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			f.close()
			return nil, err
		}
	}
	files := map[string][]byte{"lab/var/log/auth.log": []byte("fixture: connection accepted\n"), "lab/var/log/system.log": bytes.Repeat([]byte("fixture log line\n"), 65536), "lab/etc/server config.txt": []byte("# inert sample configuration\n"), "uploads/notes.txt": []byte("Burrow transfer fixture\n")}
	for path, data := range files {
		if err := os.WriteFile(filepath.Join(root, path), data, 0600); err != nil {
			f.close()
			return nil, err
		}
	}
	return f, nil
}
func (f *fixture) current() *target { return f.targets[f.selected] }
func (f *fixture) close()           { f.cancel(); _ = os.RemoveAll(f.root) }
func (f *fixture) cancel() {
	if f.copy != nil {
		f.copy.source.Close()
		f.copy.dest.Close()
		os.Remove(f.copy.path)
		f.copy = nil
		f.progress = "Transfer cancelled; partial fixture file removed."
	}
}

// Resolve paths inside a fixture root, including symlinks. No remote ls parsing.
func within(root, cwd, name string) (string, error) {
	var path string
	if filepath.IsAbs(name) {
		path = filepath.Join(root, strings.TrimLeft(name, "/"))
	} else {
		path = filepath.Join(root, strings.TrimLeft(cwd, "/"), name)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if errors.Is(err, os.ErrNotExist) {
		var parent string
		parent, err = filepath.EvalSymlinks(filepath.Dir(path))
		resolved = filepath.Join(parent, filepath.Base(path))
	}
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("path leaves fixture root")
	}
	return resolved, nil
}

func words(line string) ([]string, error) {
	var out []string
	var word strings.Builder
	var quote rune
	escaped, started := false, false
	for _, r := range line {
		switch {
		case escaped:
			word.WriteRune(r)
			escaped = false
			started = true
		case r == '\\' && quote != '\'':
			escaped = true
			started = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			started = true
		case unicode.IsSpace(r):
			if started {
				out = append(out, word.String())
				word.Reset()
				started = false
			}
		default:
			word.WriteRune(r)
			started = true
		}
	}
	if escaped || quote != 0 {
		return nil, errors.New("unfinished quote or escape")
	}
	if started {
		out = append(out, word.String())
	}
	return out, nil
}

const help = `connections             list fixture targets
use lab | gateway        select a target (does not connect)
connect                  simulate connection establishment
scp [lab|gateway]        enter file mode on connected target
ls [path] / cd path      inspect/navigate remote fixture
lls / lcd path           inspect/navigate local fixture
get remote [local]       download into local fixture
put local [remote]       upload from local fixture
cancel                   cancel active transfer
back                     leave file mode
shell / sessions         local /bin/sh PTY fixture / list shells
resume ID                foreground a background shell
tunnels / tunnel PORT    list/add a SIMULATED loopback tunnel
run                      show selected-target tool context only
close --yes / loss       end selected fixture access
clear / help / quit      clear output / help / exit prototype
Tab completion; Up/Down history; PgUp/PgDn output; Ctrl-C clears input
In a shell: Ctrl-] backgrounds; exit ends that shell.
All target paths and transfers are TEMPORARY LOCAL FIXTURES.`

func (f *fixture) execute(args []string) (r result) {
	r.Target = f.current().name
	if len(args) == 0 {
		return
	}
	r.Command = args[0]
	c := f.current()
	fail := func(err error) result { r.Error = err.Error(); return r }
	usage := func(text string) result { return fail(errors.New(text)) }
	selectTarget := func(name string) bool {
		for i, t := range f.targets {
			if t.name == name {
				f.selected = i
				return true
			}
		}
		return false
	}
	switch args[0] {
	case "help":
		r.Message = help
	case "connections":
		for i, t := range f.targets {
			mark := " "
			if i == f.selected {
				mark = ">"
			}
			r.Message += fmt.Sprintf("%s %-10s ● %s\n", mark, t.name, t.status)
		}
	case "use":
		if len(args) != 2 {
			return usage("use lab|gateway")
		}
		if f.copy != nil {
			return usage("finish or cancel the transfer before switching targets")
		}
		if !selectTarget(args[1]) {
			return usage("unknown fixture target")
		}
		f.mode = "manage"
		r.Target = f.current().name
		r.Message = "Selected " + r.Target + "; connection unchanged."
	case "connect":
		c.status = "CONNECTED"
		r.Message = "SIMULATED connection established; no network activity."
	case "scp":
		if len(args) > 2 {
			return usage("scp [lab|gateway]")
		}
		if len(args) == 2 {
			if f.copy != nil {
				return usage("finish or cancel transfer first")
			}
			if !selectTarget(args[1]) {
				return usage("unknown fixture target")
			}
			c = f.current()
			r.Target = c.name
		}
		if c.status != "CONNECTED" {
			return usage("target disconnected; connect explicitly")
		}
		f.mode = "files"
		r.Message = "File mode. Remote paths map to temporary LOCAL fixtures. Try ls, cd /var/log, get auth.log."
	case "back":
		f.mode = "manage"
		r.Message = "Management prompt. Connection remains available."
	case "ls", "lls", "cd", "lcd", "get", "put":
		if f.mode != "files" || c.status != "CONNECTED" {
			return usage("enter scp mode on a connected target first")
		}
		root, cwd := filepath.Join(f.root, c.name), c.cwd
		if args[0] == "lls" || args[0] == "lcd" {
			root, cwd = f.root, f.local
		}
		name := "."
		if len(args) > 1 {
			name = args[1]
		}
		path := ""
		var err error
		if args[0] != "get" && args[0] != "put" {
			path, err = within(root, cwd, name)
			if err != nil {
				return fail(err)
			}
		}
		switch args[0] {
		case "ls", "lls":
			items, err := os.ReadDir(path)
			if err != nil {
				return fail(err)
			}
			for _, item := range items {
				info, err := item.Info()
				if err != nil {
					return fail(err)
				}
				name := item.Name()
				if info.IsDir() {
					name += "/"
				}
				r.Entries = append(r.Entries, entry{name, info.Mode().String(), info.Size(), info.ModTime().Format("Jan 02 15:04")})
			}
			sort.SliceStable(r.Entries, func(i, j int) bool {
				return strings.HasSuffix(r.Entries[i].Name, "/") && !strings.HasSuffix(r.Entries[j].Name, "/")
			})
			r.Message = "remote:" + c.cwd
			if args[0] == "lls" {
				r.Message = "local:" + f.local
			}
		case "cd", "lcd":
			if len(args) != 2 {
				return usage("cd PATH / lcd PATH")
			}
			info, err := os.Stat(path)
			if err != nil {
				return fail(err)
			}
			if !info.IsDir() {
				return usage("not a directory")
			}
			rel, _ := filepath.Rel(root, path)
			if rel == "." {
				rel = ""
			}
			if args[0] == "cd" {
				c.cwd = "/" + rel
			} else {
				f.local = "/" + rel
			}
			r.Message = "Directory changed."
		case "get", "put":
			if len(args) < 2 || len(args) > 3 {
				return usage("get REMOTE [LOCAL] / put LOCAL [REMOTE]")
			}
			if f.copy != nil {
				return usage("one fixture transfer at a time; use cancel")
			}
			srcRoot, srcCwd, dstRoot, dstCwd := filepath.Join(f.root, c.name), c.cwd, f.root, "/downloads"
			if args[0] == "put" {
				srcRoot, srcCwd, dstRoot, dstCwd = f.root, f.local, filepath.Join(f.root, c.name), c.cwd
			}
			src, err := within(srcRoot, srcCwd, args[1])
			if err != nil {
				return fail(err)
			}
			destName := filepath.Base(src)
			if len(args) == 3 {
				destName = args[2]
			}
			dest, err := within(dstRoot, dstCwd, destName)
			if err != nil {
				return fail(err)
			}
			in, err := os.Open(src)
			if err != nil {
				return fail(err)
			}
			info, err := in.Stat()
			if err != nil {
				in.Close()
				return fail(err)
			}
			if !info.Mode().IsRegular() {
				in.Close()
				return usage("fixture transfers require regular files")
			}
			out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				in.Close()
				return fail(err)
			}
			f.copy = &transfer{source: in, dest: out, path: dest, total: info.Size()}
			f.progress = "Transfer started (real bytes; deliberately paced fixture)."
			r.Message = f.progress
		}
	case "cancel":
		f.cancel()
		r.Message = f.progress
	case "tunnels":
		r.Message = "SIMULATED endpoints only; no listening sockets.\n" + strings.Join(c.tunnels, "\n")
	case "tunnel":
		if c.status != "CONNECTED" {
			return usage("connect explicitly first")
		}
		if len(args) != 2 {
			return usage("tunnel PORT")
		}
		port, err := strconv.Atoi(args[1])
		if err != nil || port < 1 || port > 65535 {
			return usage("port must be 1..65535")
		}
		endpoint := fmt.Sprintf("127.0.0.1:%d", port)
		c.tunnels = append(c.tunnels, endpoint)
		r.Message = "SIMULATED tunnel " + endpoint
	case "run":
		if c.status != "CONNECTED" {
			return usage("connect explicitly first")
		}
		r.Message = "Tool context: " + c.name + "; separate connection operation, not the interactive shell environment. No tool executed."
	case "close", "loss":
		if args[0] == "close" && (len(args) != 2 || args[1] != "--yes") {
			return usage("close --yes explicitly ends connection, shells and transfers")
		}
		c.status = "DISCONNECTED"
		c.tunnels = nil
		f.cancel()
		r.Message = "Fixture access ended; reconnect explicitly."
	case "quit":
		f.quit = true
		r.Message = "Prototype exiting. Local fixture shells end; all scratch files are removed. Production quit retains daemon connections/tunnels."
	case "clear":
	default:
		return usage("unknown command; type help")
	}
	return r
}

func (f *fixture) advance() error {
	if f.copy == nil {
		return nil
	}
	t := f.copy
	n, err := io.CopyN(t.dest, t.source, 65536)
	t.done += n
	f.progress = fmt.Sprintf("Transfer %d / %d bytes", t.done, t.total)
	if err == io.EOF && t.done < t.total {
		err = io.ErrUnexpectedEOF
	}
	if err != nil && err != io.EOF {
		f.cancel()
		return err
	}
	if t.done >= t.total {
		err = t.dest.Close()
		t.source.Close()
		f.copy = nil
		if err != nil {
			os.Remove(t.path)
			return err
		}
		f.progress = fmt.Sprintf("Transfer COMPLETE · %d bytes", t.done)
	}
	return nil
}
