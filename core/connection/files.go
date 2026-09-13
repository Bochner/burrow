package connection

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Bochner/burrow/core/launch"
)

// FileRoots are persistent workspace choices, independent of navigation state.
type FileRoots struct {
	Version  int    `json:"version"`
	Upload   string `json:"upload"`
	Download string `json:"download"`
}

type FileEntry struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	Permissions string    `json:"permissions"`
	UID         uint32    `json:"uid"`
	GID         uint32    `json:"gid"`
	Owner       string    `json:"owner"`
	Group       string    `json:"group"`
	Size        int64     `json:"size"`
	Modified    time.Time `json:"modified"`
	Directory   bool      `json:"directory"`
	Link        string    `json:"link,omitempty"`
	Error       string    `json:"error,omitempty"`
}

type FileListing struct {
	Path    string      `json:"path"`
	Entries []FileEntry `json:"entries"`
	Notice  string      `json:"notice,omitempty"`
}

func fileRoots(ctx context.Context, w, area, path string, initialize bool) (FileRoots, error) {
	out := FileRoots{Version: 1, Upload: filepath.Join(w, "burrow-files/uploads"), Download: filepath.Join(w, "burrow-files/downloads")}
	if _, err := launch.Status(ctx, w); err != nil {
		return out, err
	}
	dir, err := launch.FileRoot(filepath.Join(w, "burrow-files"), initialize)
	if err != nil {
		return out, err
	}
	dir.Close()
	err = launch.Settings(ctx, filepath.Join(w, "burrow-files/config.json"), func(data []byte) ([]byte, error) {
		if data != nil {
			out = FileRoots{}
			decoder := json.NewDecoder(strings.NewReader(string(data)))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&out) != nil || decoder.Decode(new(any)) != io.EOF || out.Version != 1 || out.Upload == "" || out.Download == "" {
				return nil, fmt.Errorf("invalid workspace transfer roots; restore burrow-files/config.json; no fallback used")
			}
		} else if !initialize {
			return nil, fmt.Errorf("transfer roots missing; open the workspace first")
		}
		if area != "" {
			if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, 0) {
				return nil, fmt.Errorf("root must be an absolute canonical path")
			}
			if area == "upload" {
				out.Upload = path
			} else {
				out.Download = path
			}
		}
		for _, entry := range [][2]string{{"upload", out.Upload}, {"download", out.Download}} {
			root, err := launch.FileRoot(entry[1], data == nil || area == entry[0])
			if err != nil {
				return nil, err
			}
			root.Close()
		}
		if data != nil && area == "" {
			return nil, nil
		}
		return json.MarshalIndent(out, "", "  ")
	})
	return out, err
}

func EnsureFileRoots(ctx context.Context, w string) error {
	_, err := fileRoots(ctx, w, "", "", true)
	return err
}

func TransferRoots(ctx context.Context, w string) (FileRoots, error) {
	return fileRoots(ctx, w, "", "", false)
}

func FileHistory(ctx context.Context, w string) ([]string, error) {
	var response []struct{ Source, Message string }
	err := launch.Call(ctx, w, "ActiveLogs", map[string]string{"Operation": "burrow", "Chain": "files"}, &response)
	out := []string{}
	for _, entry := range response {
		if entry.Source == "burrow-files" {
			out = append(out, entry.Message)
		}
	}
	return out, err
}

// Only successfully executed, validated file commands are submitted here. Never
// persist speculative completion, rejected input, or authentication responses.
func RecordFileCommand(ctx context.Context, w string, args []string) error {
	if err := ValidateCommand(w, args); err != nil {
		return err
	}
	switch args[0] {
	case "scp":
		q, _ := fileArgs(args)
		if q.Operation == "complete" || q.Operation == "cancel" {
			return nil
		}
	case "local", "lcd", "lls":
	default:
		return fmt.Errorf("not a file command")
	}
	for _, args := range [][]string{{"op", "create", "burrow"}, {"chain", "create", "files"}} {
		if _, err := launch.HovelCLI(ctx, w, append([]string{"--op", "burrow", "--chain", "files", "--"}, args...)...); err != nil {
			return err
		}
	}
	var ignored struct{}
	return launch.Call(ctx, w, "AppendLog", map[string]any{"Operation": "burrow", "Chain": "files", "Entries": []map[string]any{{"Time": time.Now().UTC(), "Kind": "event", "Level": "info", "Source": "burrow-files", "Message": CommandLine(args)}}}, &ignored)
}

func fileCommandResult(ctx context.Context, w string, args []string, value any, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	if err = RecordFileCommand(ctx, w, args); err != nil {
		return value, fmt.Errorf("file command completed but history unavailable: %w", err)
	}
	return value, nil
}

func localArgs(args []string) (area, path string, err error) {
	area = "download"
	rest := args[1:]
	if len(rest) > 0 && (rest[0] == "upload" || rest[0] == "download") {
		area, rest = rest[0], rest[1:]
	}
	if len(rest) > 1 || ((args[0] == "lcd" || (args[0] == "local" && len(args) > 1)) && len(rest) != 1) {
		return "", "", fmt.Errorf("expected %s [download|upload] %s", args[0], map[string]string{"local": "PATH", "lcd": "PATH", "lls": "[PATH]"}[args[0]])
	}
	if len(rest) == 1 {
		path = rest[0]
	}
	if strings.ContainsRune(path, 0) {
		return "", "", fmt.Errorf("invalid path")
	}
	return area, path, nil
}

func executeLocal(ctx context.Context, w string, args []string) (any, error) {
	area, path, err := localArgs(args)
	if err != nil {
		return nil, err
	}
	if args[0] == "local" {
		if len(args) == 1 {
			return TransferRoots(ctx, w)
		}
		return fileRoots(ctx, w, area, path, false)
	}
	roots, err := TransferRoots(ctx, w)
	if err != nil {
		return nil, err
	}
	base := roots.Download
	if area == "upload" {
		base = roots.Upload
	}
	root, err := launch.FileRoot(base, false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	rel, err := containedLocal(base, path)
	if err != nil {
		return nil, err
	}
	dir, err := root.Open(rel)
	if err != nil {
		return nil, fmt.Errorf("local directory refused: %w", err)
	}
	defer dir.Close()
	stat, err := dir.Stat()
	if err != nil || !stat.IsDir() {
		return nil, fmt.Errorf("local path is not an accessible directory")
	}
	out := FileListing{Path: filepath.Join(base, rel), Entries: []FileEntry{}}
	if args[0] == "lcd" {
		return out, nil
	}
	items, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := filepath.Join(rel, item.Name())
		info, err := root.Lstat(name)
		entry := FileEntry{Name: item.Name(), Path: filepath.Join(base, name)}
		if err != nil {
			entry.Error = "metadata unavailable"
			out.Entries = append(out.Entries, entry)
			continue
		}
		st := info.Sys().(*syscall.Stat_t)
		entry.Permissions = filePermissions(info.Mode())
		entry.UID, entry.GID = st.Uid, st.Gid
		entry.Owner, entry.Group = strconv.FormatUint(uint64(st.Uid), 10), strconv.FormatUint(uint64(st.Gid), 10)
		if owner, e := user.LookupId(entry.Owner); e == nil {
			entry.Owner = owner.Username
		}
		if group, e := user.LookupGroupId(entry.Group); e == nil {
			entry.Group = group.Name
		}
		entry.Size, entry.Modified, entry.Directory = info.Size(), info.ModTime(), info.IsDir()
		if info.Mode()&os.ModeSymlink != 0 {
			entry.Link, err = root.Readlink(name)
			target, e := containedLocal(base, name)
			if err != nil || e != nil {
				entry.Error = "broken or outside-root link; traversal refused"
			} else if targetInfo, e := root.Stat(target); e != nil {
				entry.Error = "link target unavailable"
			} else {
				entry.Directory = targetInfo.IsDir()
			}
		}
		out.Entries = append(out.Entries, entry)
	}
	sortFiles(out.Entries)
	return out, nil
}

// Completion shares containment checks but is never added to command history.
func LocalListing(ctx context.Context, w, area, path string) (any, error) {
	if area != "upload" && area != "download" {
		return nil, fmt.Errorf("invalid local area")
	}
	return executeLocal(ctx, w, []string{"lls", area, path})
}

// Resolve absolute links for usability, then access only through the pinned Root.
// A link/path replacement after resolution cannot redirect Root outside its tree.
func containedLocal(base, path string) (string, error) {
	if path == "" {
		path = "."
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	inside := func(path string) (string, error) {
		rel, err := filepath.Rel(base, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path is outside the configured area; place intended files inside it explicitly")
		}
		return rel, nil
	}
	if _, err := inside(path); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("local path unavailable: %w", err)
	}
	return inside(resolved)
}

func filePermissions(mode os.FileMode) string {
	s := []byte("----------")
	switch {
	case mode.IsDir():
		s[0] = 'd'
	case mode&os.ModeSymlink != 0:
		s[0] = 'l'
	case mode&os.ModeNamedPipe != 0:
		s[0] = 'p'
	case mode&os.ModeSocket != 0:
		s[0] = 's'
	case mode&os.ModeDevice != 0:
		s[0] = 'b'
		if mode&os.ModeCharDevice != 0 {
			s[0] = 'c'
		}
	}
	for i, c := range "rwxrwxrwx" {
		if mode.Perm()&(1<<uint(8-i)) != 0 {
			s[i+1] = byte(c)
		}
	}
	for _, special := range []struct {
		flag    os.FileMode
		at      int
		on, off byte
	}{{os.ModeSetuid, 3, 's', 'S'}, {os.ModeSetgid, 6, 's', 'S'}, {os.ModeSticky, 9, 't', 'T'}} {
		if mode&special.flag != 0 {
			if s[special.at] == 'x' {
				s[special.at] = special.on
			} else {
				s[special.at] = special.off
			}
		}
	}
	return string(s)
}

func sortFiles(entries []FileEntry) {
	slices.SortFunc(entries, func(a, b FileEntry) int {
		if c := a.Modified.Compare(b.Modified); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
}
