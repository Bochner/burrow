package connection

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/Bochner/burrow/core/launch"
	"github.com/pkg/sftp"
	"github.com/vibepwners/hovel/sdk/go/hovel"
)

type FileQuery struct {
	Operation string `json:"operation"`
	Path      string `json:"path"`
	ID        string `json:"id,omitempty"`
}

type fileCache struct {
	listing FileListing
	at      time.Time
}
type accountName struct {
	name string
	at   time.Time
}

type FileTree struct {
	Path       string      `json:"path"`
	Entries    []FileEntry `json:"entries"`
	Incomplete bool        `json:"incomplete"`
	Errors     []string    `json:"errors"`
}

func fileArgs(args []string) (FileQuery, error) {
	q := FileQuery{Operation: "pwd", Path: "."}
	if len(args) < 2 || len(args) > 4 {
		return q, fmt.Errorf("expected scp NAME [ls|tree|cd|pwd|complete] [PATH]")
	}
	if len(args) > 2 {
		q.Operation = args[2]
	}
	if len(args) > 3 {
		q.Path = args[3]
	}
	return q, validateFileQuery(q)
}

func validateFileQuery(q FileQuery) error {
	if len(q.Path) > 4096 || strings.ContainsRune(q.Path, 0) || len(q.ID) > 64 {
		return fmt.Errorf("invalid file path or request")
	}
	switch q.Operation {
	case "ls", "tree", "cd", "pwd", "complete", "cancel":
		return nil
	}
	return fmt.Errorf("expected ls, tree, cd, pwd or complete; transfers follow in the next file milestone tickets")
}

// Browse binds every request to a specific live connection creation.
func Browse(ctx context.Context, w string, s State, q FileQuery) (any, error) {
	if err := validateFileQuery(q); err != nil {
		return nil, err
	}
	if s.Generation == "" || s.Creation == "" || s.State != "connected" {
		return nil, fmt.Errorf("file mode requires a verified live connection; reconnect explicitly")
	}
	raw, err := json.Marshal(q)
	if err != nil {
		return nil, err
	}
	var result json.RawMessage
	err = managerControl(ctx, w, managerIdentity{Session: s.Session, Generation: s.Generation}, "files", []string{s.Creation, string(raw)}, &result)
	if err != nil {
		return nil, err
	}
	var failure struct {
		Error string `json:"error"`
	}
	if err = json.Unmarshal(result, &failure); err != nil {
		return nil, err
	}
	if failure.Error != "" {
		return nil, fmt.Errorf("%s", failure.Error)
	}
	if q.Operation == "tree" {
		var out FileTree
		err = json.Unmarshal(result, &out)
		return out, err
	}
	var out FileListing
	err = json.Unmarshal(result, &out)
	return out, err
}

func (m *manager) filesCommand(req hovel.PayloadCommandRequest) (result hovel.PayloadCommandResult, failure error) {
	// Hovel masks payload errors as HTTP 500. Carry read-only browsing failures
	// in structured output so all frontends retain the actionable SFTP error.
	defer func() {
		if failure != nil {
			raw, _ := json.Marshal(map[string]string{"error": failure.Error()})
			result = hovel.PayloadCommandResult{Command: req.Command, Stdout: string(raw)}
			failure = nil
		}
	}()
	if len(req.Args) != 3 || req.Args[0] != m.Generation {
		return hovel.PayloadCommandResult{}, fmt.Errorf("exact manager generation, connection creation and file query required")
	}
	var q FileQuery
	decoder := json.NewDecoder(strings.NewReader(req.Args[2]))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&q) != nil || decoder.Decode(new(any)) != io.EOF {
		return hovel.PayloadCommandResult{}, fmt.Errorf("invalid file query")
	}
	if err := validateFileQuery(q); err != nil {
		return hovel.PayloadCommandResult{}, err
	}
	m.mu.Lock()
	s := m.connections[req.Args[1]]
	closed := m.closed
	m.mu.Unlock()
	if closed || s == nil {
		return hovel.PayloadCommandResult{}, fmt.Errorf("file connection creation unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := launch.VerifyReservation(ctx, m.Workspace, m.dir); err != nil {
		return hovel.PayloadCommandResult{}, err
	}
	if q.Operation == "cancel" {
		s.mu.Lock()
		if s.fileCancelled == nil {
			s.fileCancelled = map[string]time.Time{}
		}
		for id, at := range s.fileCancelled {
			if time.Since(at) > time.Minute {
				delete(s.fileCancelled, id)
			}
		}
		if q.ID != "" {
			if len(s.fileCancelled) >= 1024 {
				oldest := ""
				for id, at := range s.fileCancelled {
					if oldest == "" || at.Before(s.fileCancelled[oldest]) {
						oldest = id
					}
				}
				delete(s.fileCancelled, oldest)
			}
			s.fileCancelled[q.ID] = time.Now()
		}
		if q.ID != "" && q.ID == s.fileRequest && s.fileCancel != nil {
			s.fileCancel()
		}
		s.mu.Unlock()
		return hovel.PayloadCommandResult{Command: req.Command, Stdout: `{"path":"","entries":[],"notice":"Cancellation requested"}`}, nil
	}
	value, err := s.browse(ctx, q)
	if err != nil {
		return hovel.PayloadCommandResult{}, err
	}
	raw, err := json.Marshal(value)
	if tree, ok := value.(FileTree); ok && len(raw) > 768<<10 {
		tree.Incomplete = true
		tree.Errors = append(tree.Errors, "response size limit reached; select a narrower subtree")
		for len(raw) > 768<<10 && len(tree.Entries) > 0 {
			tree.Entries = tree.Entries[:len(tree.Entries)/2]
			raw, err = json.Marshal(tree)
		}
	}
	if len(raw) > 768<<10 {
		return hovel.PayloadCommandResult{}, fmt.Errorf("listing exceeds the supported response size; select a narrower directory; no complete listing returned")
	}
	return hovel.PayloadCommandResult{Command: req.Command, Stdout: string(raw)}, err
}

func (s *owner) browse(ctx context.Context, q FileQuery) (any, error) {
	// TryLock coalesces speculative requests rather than queueing remote work.
	if q.Operation == "complete" {
		if !s.fileMu.TryLock() {
			return FileListing{Path: q.Path, Entries: []FileEntry{}, Notice: "Discovery already running"}, nil
		}
	} else {
		s.fileMu.Lock()
	}
	defer s.fileMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.closed || s.state.State != "connected" {
		s.mu.Unlock()
		return nil, fmt.Errorf("file connection is unavailable; reconnect explicitly")
	}
	if err := s.checkMaster(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	state := s.state
	s.mu.Unlock()
	key := q.Path
	if key == "" {
		key = "."
	}
	if strings.HasPrefix(key, "~") && key != "~" && !strings.HasPrefix(key, "~/") {
		return nil, fmt.Errorf("use ~ or ~/PATH for the connected account, or an absolute remote path")
	}
	if q.Operation == "complete" {
		if cached, ok := s.fileListings[key]; ok && time.Since(cached.at) < 30*time.Second {
			return cached.listing, nil
		}
		if time.Now().Before(s.fileNext) {
			return FileListing{Path: key, Entries: []FileEntry{}, Notice: "Discovery throttled"}, nil
		}
	}
	s.fileNext = time.Now().Add(time.Second)
	operation, stop := context.WithCancel(ctx)
	defer stop()
	s.mu.Lock()
	if _, cancelled := s.fileCancelled[q.ID]; q.ID != "" && cancelled {
		s.mu.Unlock()
		return nil, context.Canceled
	}
	s.fileCancel = stop
	s.fileRequest = q.ID
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.fileCancel = nil; s.fileRequest = ""; s.mu.Unlock() }()
	process := fileSSH(operation, state, "-s", "unused", "sftp")
	input, err := process.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := process.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, err
	}
	if err = process.Start(); err != nil {
		input.Close()
		return nil, err
	}
	defer func() { input.Close(); process.Process.Kill(); process.Wait() }()
	client, err := sftp.NewClientPipe(output, input)
	if err != nil {
		return nil, fmt.Errorf("SFTP subsystem unavailable; no new authentication attempted")
	}
	defer client.Close()
	remote := key
	if key == "~" || strings.HasPrefix(key, "~/") {
		home, e := client.RealPath(".")
		if e != nil {
			return nil, e
		}
		remote = home + strings.TrimPrefix(key, "~")
	}
	canonical, err := client.RealPath(remote)
	if err != nil {
		return nil, fmt.Errorf("remote path unavailable: %w", err)
	}
	if q.Operation == "pwd" || q.Operation == "cd" {
		info, e := client.Stat(canonical)
		if e != nil || !info.IsDir() {
			return nil, fmt.Errorf("remote path is not an accessible directory")
		}
		// Opening the directory verifies actual listing access, rather than mode bits.
		listing, e := s.readFiles(operation, client, canonical, state, q.Operation != "complete")
		if e != nil {
			return nil, e
		}
		s.cacheFiles(key, listing)
		return listing, nil
	}
	if q.Operation == "tree" {
		out := FileTree{Path: canonical, Entries: []FileEntry{}, Errors: []string{}}
		pending := []string{canonical}
		for len(pending) > 0 {
			current := pending[0]
			pending = pending[1:]
			listing, e := s.readFiles(operation, client, current, state, false)
			if e != nil {
				out.Incomplete = true
				out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", current, e))
				if operation.Err() != nil {
					break
				}
				continue
			}
			for _, entry := range listing.Entries {
				if len(out.Entries) == 3000 {
					out.Incomplete = true
					out.Errors = append(out.Errors, "tree exceeds 3000 entries; select a narrower subtree")
					return out, nil
				}
				out.Entries = append(out.Entries, entry)
				if entry.Directory && entry.Link == "" && entry.Error == "" {
					pending = append(pending, entry.Path)
				}
			}
		}
		return out, nil
	}
	listing, err := s.readFiles(operation, client, canonical, state, q.Operation != "complete")
	if err != nil {
		return nil, err
	}
	s.cacheFiles(key, listing)
	return listing, nil
}

func fileSSH(ctx context.Context, state State, args ...string) *exec.Cmd {
	base := []string{"-F", "/dev/null", "-S", state.Socket, "-o", "ControlMaster=no", "-o", "ProxyCommand=/usr/bin/false", "-o", "BatchMode=yes", "-T"}
	cmd := exec.CommandContext(ctx, "/usr/bin/ssh", append(base, args...)...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	cmd.WaitDelay = time.Second
	return cmd
}

func (s *owner) cacheFiles(key string, listing FileListing) {
	if s.fileListings == nil {
		s.fileListings = map[string]fileCache{}
	}
	for _, key := range []string{key, listing.Path} {
		if _, exists := s.fileListings[key]; !exists && len(s.fileListings) >= 128 {
			var oldest string
			var at time.Time
			for k, v := range s.fileListings {
				if oldest == "" || v.at.Before(at) {
					oldest, at = k, v.at
				}
			}
			delete(s.fileListings, oldest)
		}
		s.fileListings[key] = fileCache{listing, time.Now()}
	}
}

func (s *owner) readFiles(ctx context.Context, client *sftp.Client, dir string, state State, enrich bool) (FileListing, error) {
	out := FileListing{Path: dir, Entries: []FileEntry{}}
	infos, err := client.ReadDirContext(ctx, dir)
	if err != nil {
		return out, fmt.Errorf("remote directory cannot be read: %w", err)
	}
	for _, info := range infos {
		name := info.Name()
		if name == "." || name == ".." || name == "" || strings.ContainsAny(name, "/\x00") {
			return out, fmt.Errorf("invalid remote directory entry")
		}
		attrs, ok := info.Sys().(*sftp.FileStat)
		if !ok {
			return out, fmt.Errorf("remote metadata unavailable")
		}
		entry := FileEntry{Name: name, Path: path.Join(dir, name), Permissions: filePermissions(info.Mode()), UID: attrs.UID, GID: attrs.GID, Owner: strconv.FormatUint(uint64(attrs.UID), 10), Group: strconv.FormatUint(uint64(attrs.GID), 10), Size: info.Size(), Modified: info.ModTime(), Directory: info.IsDir()}
		if info.Mode()&os.ModeSymlink != 0 {
			entry.Link, err = client.ReadLink(entry.Path)
			if err != nil {
				entry.Error = "link target unavailable"
			} else if target, e := client.Stat(entry.Path); e != nil {
				entry.Error = "broken or inaccessible link"
			} else {
				entry.Directory = target.IsDir()
			}
		}
		out.Entries = append(out.Entries, entry)
	}
	if enrich {
		s.resolveAccounts(ctx, state, out.Entries)
	}
	for i := range out.Entries {
		entry := &out.Entries[i]
		if cached, ok := s.fileAccounts["u"+entry.Owner]; ok && time.Since(cached.at) < 5*time.Minute && cached.name != "" {
			entry.Owner = cached.name
		} else {
			out.Notice = "Reduced metadata: numeric owner/group IDs where account names are unavailable"
		}
		if cached, ok := s.fileAccounts["g"+entry.Group]; ok && time.Since(cached.at) < 5*time.Minute && cached.name != "" {
			entry.Group = cached.name
		} else {
			out.Notice = "Reduced metadata: numeric owner/group IDs where account names are unavailable"
		}
	}
	sortFiles(out.Entries)
	return out, nil
}

func (s *owner) resolveAccounts(ctx context.Context, state State, entries []FileEntry) {
	if s.fileAccounts == nil {
		s.fileAccounts = map[string]accountName{}
	}
	missing := map[string]bool{}
	for _, entry := range entries {
		for _, id := range []string{"u" + entry.Owner, "g" + entry.Group} {
			if cached, ok := s.fileAccounts[id]; !ok || time.Since(cached.at) >= 5*time.Minute {
				missing[id] = true
			}
		}
	}
	if len(missing) == 0 {
		return
	}
	if len(s.fileAccounts) > 4096 {
		clear(s.fileAccounts)
	}
	var users, groups []string
	for id := range missing {
		if len(users)+len(groups) >= 256 {
			break
		}
		s.fileAccounts[id] = accountName{at: time.Now()}
		if id[0] == 'u' {
			users = append(users, id[1:])
		} else {
			groups = append(groups, id[1:])
		}
	}
	// Only parsed numeric IDs enter fixed commands. Strip password/member fields remotely.
	var script strings.Builder
	if len(users) > 0 {
		script.WriteString("printf 'u\\n'; getent passwd " + strings.Join(users, " ") + " | cut -d: -f1,3; ")
	}
	if len(groups) > 0 {
		script.WriteString("printf 'g\\n'; getent group " + strings.Join(groups, " ") + " | cut -d: -f1,3")
	}
	lookup, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	cmd := fileSSH(lookup, state, "unused", script.String())
	var output limitedFileOutput
	cmd.Stdout = &output
	if cmd.Run() != nil || output.full {
		return
	}
	kind := ""
	for _, line := range strings.Split(output.String(), "\n") {
		if line == "u" || line == "g" {
			kind = line
			continue
		}
		name, id, ok := strings.Cut(line, ":")
		if !ok || kind == "" || !missing[kind+id] || len(name) == 0 || len(name) > 256 || strings.ContainsAny(name, "\x00\r\t\x1b:") {
			continue
		}
		s.fileAccounts[kind+id] = accountName{name, time.Now()}
	}
}

type limitedFileOutput struct {
	bytes.Buffer
	full bool
}

func (b *limitedFileOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 64<<10 {
		b.full = true
		return len(p), nil
	}
	return b.Buffer.Write(p)
}
