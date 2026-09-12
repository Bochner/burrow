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
	"time"
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
	host, user, auth  string
	port, shell       string
	proxyPort         string
	tunnels           []tunnel
}
type tunnel struct{ id, kind, listen, destination string }

func (t *target) proxyStatus() string {
	if t.proxyPort == "" || t.status != "CONNECTED" {
		return "No"
	}
	return "Yes :" + t.proxyPort
}

type transfer struct {
	source, dest *os.File
	path         string
	done, total  int64
	started      time.Time
}
type batchFile struct {
	name, state, detail string
	size, done          int64
}
type batchTransfer struct {
	files            []batchFile
	directory, dest  string
	index            int
	started, updated time.Time
	state            string
}
type fixture struct {
	root, mode, local, download string
	targets                     []*target
	selected                    int
	copy                        *transfer
	progress                    string
	batch                       *batchTransfer
	quit                        bool
	nextTunnel                  int
}

func newFixture() (*fixture, error) {
	root, err := os.MkdirTemp("", "burrow-terminal-")
	if err != nil {
		return nil, err
	}
	f := &fixture{root: root, mode: "manage", local: "/uploads", download: "/downloads", targets: []*target{{name: "lab", host: "192.0.2.10", user: "ubuntu", auth: "SSH agent", status: "DISCONNECTED", cwd: "/"}, {name: "gateway", host: "192.0.2.20", user: "admin", auth: "SSH key", status: "DISCONNECTED", cwd: "/"}}}
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
	if f.batch != nil && (f.batch.state == "RUNNING" || f.batch.state == "REVIEW") {
		f.batch.state = "CANCELLED"
		f.batch.updated = time.Now()
		for i := range f.batch.files {
			if f.batch.files[i].state == "RUNNING" || f.batch.files[i].state == "QUEUED" {
				f.batch.files[i].state = "CANCELLED"
			}
		}
		f.progress = "Batch CANCELLED; completed files kept, partial fixture file removed."
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
		topic := ""
		if len(args) > 1 {
			topic = args[1]
		}
		r.Message = commandHelp(topic)
	case "connections", "list":
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
		if f.copy != nil || f.batch != nil && f.batch.state == "REVIEW" {
			return usage("finish or cancel the transfer before switching targets")
		}
		if !selectTarget(args[1]) {
			return usage("unknown fixture target")
		}
		f.mode = "manage"
		r.Target = f.current().name
		r.Message = "Selected " + r.Target + "; connection unchanged."
	case "connect":
		if len(args) > 1 {
			if err := f.connectOptions(args[1:]); err != nil {
				return fail(err)
			}
			c = f.current()
			r.Target = c.name
		}
		c.status = "CONNECTED"
		r.Message = "SIMULATED connection established; no network activity."
	case "scp":
		if len(args) > 2 {
			return usage("scp [lab|gateway]")
		}
		if len(args) == 2 {
			if f.copy != nil || f.batch != nil && f.batch.state == "REVIEW" {
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
	case "mget":
		if len(args) != 2 || f.mode != "files" || c.status != "CONNECTED" {
			return usage("mget PATTERN · enter scp mode first")
		}
		if f.copy != nil || (f.batch != nil && f.batch.state == "REVIEW") {
			return usage("finish or cancel the current transfer first")
		}
		if err := f.prepareBatch(args[1]); err != nil {
			return fail(err)
		}
		r.Message = "Review matching files, total size and destination. Type confirm to download or cancel."
	case "confirm":
		if f.batch == nil || f.batch.state != "REVIEW" {
			return usage("no batch awaiting confirmation")
		}
		f.batch.started, f.batch.updated = time.Now(), time.Now()
		f.batch.state = "RUNNING"
		f.nextBatchFile()
		r.Message = f.progress
	case "transfers":
		r.Message = f.progress
		if f.batch != nil {
			r.Message = "BATCH " + f.batch.state + " · destination " + f.batch.dest + "\n"
			for _, item := range f.batch.files {
				r.Message += fmt.Sprintf("%s  %s  %d/%d bytes  %s\n", item.state, item.name, item.done, item.size, item.detail)
			}
		}
	case "tree":
		if f.mode != "files" || c.status != "CONNECTED" {
			return usage("enter scp mode first")
		}
		name := "."
		if len(args) > 1 {
			name = args[1]
		}
		root, err := within(filepath.Join(f.root, c.name), c.cwd, name)
		if err != nil {
			return fail(err)
		}
		count := 0
		err = filepath.WalkDir(root, func(path string, item os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			count++
			if count > 200 {
				return errors.New("fixture tree capped at 200 entries; choose a narrower path")
			}
			rel, _ := filepath.Rel(root, path)
			r.Message += strings.Repeat("  ", strings.Count(rel, string(filepath.Separator))) + "├─ " + rel + "\n"
			return nil
		})
		if err != nil {
			return fail(err)
		}
	case "pwd":
		r.Message = c.cwd
	case "local":
		if len(args) == 1 {
			r.Message = "Download: " + f.download + "\nUpload: " + f.local
			break
		}
		if len(args) != 3 || (args[1] != "download" && args[1] != "upload") {
			return usage("local [download|upload PATH]")
		}
		path, err := within(f.root, "/", args[2])
		if err != nil {
			return fail(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return fail(err)
		}
		if !info.IsDir() {
			return usage("not a directory")
		}
		rel, _ := filepath.Rel(f.root, path)
		if args[1] == "download" {
			f.download = "/" + rel
		} else {
			f.local = "/" + rel
		}
		r.Message = "Local " + args[1] + " directory updated."
	case "ls", "lls", "cd", "lcd", "get", "put":
		if f.mode != "files" || c.status != "CONNECTED" {
			return usage("enter scp mode on a connected target first")
		}
		root, cwd := filepath.Join(f.root, c.name), c.cwd
		if args[0] == "lls" || args[0] == "lcd" {
			root, cwd = f.root, f.download
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
				r.Message = "local:" + f.download
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
				f.download = "/" + rel
			}
			r.Message = "Directory changed."
		case "get", "put":
			if f.batch != nil && f.batch.state == "REVIEW" {
				return usage("confirm or cancel the reviewed batch first")
			}
			if f.batch != nil && f.batch.state != "RUNNING" {
				f.batch = nil
			}
			if len(args) < 2 || len(args) > 3 {
				return usage("get REMOTE [LOCAL] / put LOCAL [REMOTE]")
			}
			if f.copy != nil {
				return usage("one fixture transfer at a time; use cancel")
			}
			srcRoot, srcCwd, dstRoot, dstCwd := filepath.Join(f.root, c.name), c.cwd, f.root, f.download
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
			f.copy = &transfer{source: in, dest: out, path: dest, total: info.Size(), started: time.Now()}
			f.progress = "Transfer started (real bytes; deliberately paced fixture)."
			r.Message = f.progress
		}
	case "cancel":
		f.cancel()
		r.Message = f.progress
	case "tunnels":
		r.Message = "SIMULATED endpoints only; no listening sockets.\n"
		for _, connection := range f.targets {
			for _, t := range connection.tunnels {
				r.Message += fmt.Sprintf("%s  %s  %s → %s\n", t.id, t.kind, t.listen, t.destination)
			}
		}
	case "tunc":
		if len(args) != 6 || (args[2] != "l" && args[2] != "r") {
			return usage("tunc NAME l|r LISTEN_PORT DEST_HOST DEST_PORT")
		}
		var connection *target
		for _, t := range f.targets {
			if t.name == args[1] {
				connection = t
			}
		}
		if connection == nil || connection.status != "CONNECTED" {
			return usage("name a connected target")
		}
		for _, index := range []int{3, 5} {
			port, err := strconv.Atoi(args[index])
			if err != nil || port < 1 || port > 65535 {
				return usage("ports must be 1..65535")
			}
		}
		if strings.ContainsAny(args[4], "\r\n\t\x00 ") || strings.HasPrefix(args[4], "-") {
			return usage("invalid destination host")
		}
		f.nextTunnel++
		t := tunnel{connection.name + "/" + strconv.Itoa(f.nextTunnel), strings.ToUpper(args[2]), "127.0.0.1:" + args[3], args[4] + ":" + args[5]}
		connection.tunnels = append(connection.tunnels, t)
		r.Message = "SIMULATED tunnel " + t.id + " · " + t.kind + " · " + t.listen + " → " + t.destination
	case "tund":
		if len(args) != 2 {
			return usage("tund CONNECTION/ID")
		}
		found := false
		for _, connection := range f.targets {
			for i, t := range connection.tunnels {
				if t.id == args[1] {
					connection.tunnels = append(connection.tunnels[:i], connection.tunnels[i+1:]...)
					found = true
					break
				}
			}
		}
		if !found {
			return usage("unknown qualified tunnel ID; use tunnels")
		}
		r.Message = "SIMULATED tunnel removed; connection remains available."
	case "proxy":
		if c.status != "CONNECTED" {
			return usage("connect explicitly first")
		}
		if len(args) != 2 {
			return usage("proxy PORT")
		}
		port, err := strconv.Atoi(args[1])
		if err != nil || port < 1 || port > 65535 {
			return usage("port must be 1..65535")
		}
		endpoint := fmt.Sprintf("127.0.0.1:%d", port)
		c.proxyPort = strconv.Itoa(port)
		r.Message = "SIMULATED SOCKS proxy " + endpoint
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
		c.proxyPort = ""
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

func (f *fixture) connectOptions(args []string) error {
	values := make(map[string]string)
	for i := 0; i < len(args); i++ {
		flag := args[i]
		known := false
		for _, option := range connectionOptions {
			if option.flag == flag {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("unknown connection option %q", flag)
		}
		if _, exists := values[flag]; exists {
			return fmt.Errorf("duplicate option %s", flag)
		}
		if flag == "-no-term" {
			values[flag] = "true"
			continue
		}
		if i+1 == len(args) || strings.HasPrefix(args[i+1], "-") {
			return fmt.Errorf("%s requires a value", flag)
		}
		i++
		values[flag] = args[i]
	}
	for _, option := range connectionOptions {
		if option.required && values[option.flag] == "" {
			return fmt.Errorf("required %s: %s", option.flag, option.description)
		}
	}
	for _, flag := range []string{"-port", "-proxy"} {
		if values[flag] == "" {
			continue
		}
		n, err := strconv.Atoi(values[flag])
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("%s must be 1..65535", flag)
		}
	}
	name := values["-socket"]
	if name == "." || name == ".." || len(name) > 64 {
		return errors.New("invalid connection name")
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r)) {
			return errors.New("connection name accepts letters, digits, dot, underscore and hyphen")
		}
	}
	if strings.ContainsAny(values["-ip"]+values["-user"], "\r\n\t\x00 ") {
		return errors.New("invalid host or username")
	}
	for _, t := range f.targets {
		if t.name == name {
			return fmt.Errorf("connection %s already exists; use %s then connect", name, name)
		}
	}
	if f.copy != nil || f.batch != nil && f.batch.state == "REVIEW" {
		return errors.New("finish or cancel the transfer before adding a connection")
	}
	if err := os.Mkdir(filepath.Join(f.root, name), 0700); err != nil {
		return err
	}
	auth := "SSH agent"
	if values["-ssh-key"] != "" {
		auth = values["-ssh-key"]
	}
	t := &target{name: name, host: values["-ip"], user: values["-user"], port: values["-port"], shell: values["-shell"], auth: auth, cwd: "/", status: "DISCONNECTED"}
	if values["-proxy"] != "" {
		t.proxyPort = values["-proxy"]
	}
	f.targets = append(f.targets, t)
	f.selected = len(f.targets) - 1
	return nil
}

func (f *fixture) advance() error {
	if f.copy == nil {
		return nil
	}
	t := f.copy
	n, err := io.CopyN(t.dest, t.source, 65536)
	t.done += n
	if f.batch != nil && f.batch.state == "RUNNING" {
		f.batch.files[f.batch.index].done = t.done
		f.batch.updated = time.Now()
	}
	f.progress = fmt.Sprintf("Transfer %d / %d bytes", t.done, t.total)
	if err == io.EOF && t.done < t.total {
		err = io.ErrUnexpectedEOF
	}
	if err != nil && err != io.EOF {
		f.failTransfer(t, err)
		return err
	}
	if t.done >= t.total {
		err = t.dest.Close()
		t.source.Close()
		f.copy = nil
		if err != nil {
			f.failTransfer(t, err)
			return err
		}
		f.progress = fmt.Sprintf("Transfer COMPLETE · %d bytes · %s elapsed", t.done, time.Since(t.started).Round(time.Millisecond))
		if f.batch != nil && f.batch.state == "RUNNING" {
			f.batch.files[f.batch.index].state = "COMPLETE"
			f.batch.index++
			f.nextBatchFile()
		}
	}
	return nil
}
