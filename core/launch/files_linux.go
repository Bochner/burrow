// Package launch verifies Burrow's Linux package, workspace and own Hovel daemon.
package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func refuse(path, reason string) error {
	return fmt.Errorf("refused %q: %s; inspect ownership, permissions and process evidence before manual recovery; existing resources were not adopted or removed", path, reason)
}

// Walk without following symlinks; only trusted owners may replace descendants.
// A root-owned sticky directory (e.g. /tmp) protects our owned child entries.
func directory(path string, create, private bool) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, refuse(path, "use an absolute canonical path without '..' or trailing separators")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if e == unix.ENOENT && create {
			e = unix.Mkdirat(fd, part, 0700)
			if e == nil || e == unix.EEXIST {
				next, e = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			}
		}
		unix.Close(fd)
		if e != nil {
			return nil, refuse(path, e.Error())
		}
		fd = next
		var st unix.Stat_t
		if e = unix.Fstat(fd, &st); e != nil {
			unix.Close(fd)
			return nil, e
		}
		last := i == len(parts)-1
		trusted := st.Uid == uint32(os.Getuid()) || (!last && st.Uid == 0)
		writable := st.Mode&0022 != 0
		sticky := !last && st.Uid == 0 && st.Mode&unix.S_ISVTX != 0
		if !trusted || (writable && !sticky) || (last && private && st.Mode&0777 != 0700) {
			unix.Close(fd)
			return nil, refuse(path, "unsafe directory owner or permissions")
		}
	}
	return os.NewFile(uintptr(fd), path), nil
}

func regular(path string, mode os.FileMode, limit int64) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Mode().Perm() != mode || st.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) || st.Size() > limit {
		return nil, refuse(path, "expected bounded private regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, refuse(path, "file exceeds size limit")
	}
	return b, err
}

func sum(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func digest(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	_, e = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), e
}
func lock(ctx context.Context, f *os.File) error {
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err != unix.EWOULDBLOCK {
			return err
		}
		select {
		case <-ctx.Done():
			return refuse(f.Name(), "timed out waiting for another launch")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Publish without overwriting any existing reservation, including dangling links.
// An interrupted temporary file remains visible and causes refusal on next launch.
func publish(dir *os.File, name string, data []byte, mode os.FileMode) error {
	path := filepath.Join(dir.Name(), name)
	f, e := os.OpenFile(path+".pending", os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if e != nil {
		return refuse(path+".pending", e.Error())
	}
	_, e = f.Write(data)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	if e = os.Link(path+".pending", path); e != nil {
		return refuse(path, e.Error())
	}
	if e = dir.Sync(); e != nil {
		return e
	}
	if e = os.Remove(path + ".pending"); e != nil {
		return e
	}
	return dir.Sync()
}
func absent(path string) error {
	_, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	return refuse(path, "unknown existing reservation")
}

// Settings serializes reads/replacements in a verified directory. A nil result
// leaves the file untouched. Missing files are passed as nil; no links followed.
func Settings(ctx context.Context, path string, edit func([]byte) ([]byte, error)) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("collection path must be absolute and canonical")
	}
	dir, e := directory(filepath.Dir(path), false, false)
	if e != nil {
		return e
	}
	defer dir.Close()
	if e = lock(ctx, dir); e != nil {
		return e
	}
	old, e := regular(path, 0600, 1<<20)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if os.IsNotExist(e) {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return fmt.Errorf("refused existing collection entry")
		}
	}
	data, e := edit(old)
	if e != nil || data == nil {
		return e
	}
	if len(data) > 1<<20 {
		return fmt.Errorf("collection exceeds 1 MiB")
	}
	f, e := os.CreateTemp(dir.Name(), ".burrow-settings-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(data)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	if e = dir.Sync(); e != nil {
		return fmt.Errorf("settings replaced but durability unverified: %w", e)
	}
	return nil
}
