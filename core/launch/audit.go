package launch

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"

	"golang.org/x/sys/unix"
)

const auditEnd = "End record\n\n"

// Audit is a correlation token, not operational state. An attempt without a
// terminal result means unknown outcome, including after process or disk loss.
type Audit struct{ Workspace, Action, Target, ID string }

func BeginAudit(workspace, action, target string, input any) (Audit, error) {
	a := Audit{workspace, action, target, rand.Text()}
	return a, a.Record("attempt", input)
}

// Finish preserves the operation error and reports persistence separately.
func (a Audit) Finish(result any, failure error) error {
	state := "completed"
	if failure != nil {
		state = "failed"
	}
	if errors.Is(failure, context.Canceled) {
		state = "cancelled"
	}
	return errors.Join(failure, a.Record(state, map[string]any{"result": result, "error": errorText(failure)}))
}

func errorText(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}

// Escape controls rather than deleting evidence. Every payload line is indented;
// only Burrow can emit a column-zero timestamp or record boundary.
func auditText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			fmt.Fprintf(&b, "\\u%04x", r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func redactAudit(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, value := range x {
			name := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(k))
			if strings.Contains(name, "password") || strings.Contains(name, "passphrase") || strings.Contains(name, "privatekey") || strings.Contains(name, "secret") || name == "token" || name == "inputdata" || name == "promptsocket" {
				x[k] = "[REDACTED]"
			} else {
				redactAudit(value)
			}
		}
	case []any:
		for _, value := range x {
			redactAudit(value)
		}
	}
}

func (a Audit) Record(state string, result any) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("audit incomplete: encode result: %w", err)
	}
	var value any
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	if err = d.Decode(&value); err != nil {
		return err
	}
	redactAudit(value)
	raw, err = json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s -- %s\n  Workspace: %s\n  Target: %s\n  Operation: %s\n  Status: %s\n  Result:\n", time.Now().Format(time.RFC3339Nano), auditText(a.Action), auditText(a.Workspace), auditText(a.Target), auditText(a.ID), auditText(state))
	for _, line := range strings.Split(string(raw), "\n") {
		fmt.Fprintf(&b, "    %s\n", auditText(line))
	}
	b.WriteString(auditEnd)
	err = auditFile(a.Workspace, func(f *os.File) error {
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			return err
		}
		if _, err := io.WriteString(f, b.String()); err != nil {
			return err
		}
		return f.Sync()
	})
	if err != nil {
		return fmt.Errorf("audit incomplete: cannot persist %s: %w", state, err)
	}
	return nil
}

// Directory-descriptor traversal and flock are shared with workspace storage.
// No rotation/retention policy: preserve customer records until explicitly managed.
func auditFile(workspace string, use func(*os.File) error) error {
	w, err := directory(workspace, false, false)
	if err != nil {
		return err
	}
	defer w.Close()
	dir, err := directory(filepath.Join(workspace, "burrow-logs"), true, true)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err = w.Sync(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = lock(ctx, dir); err != nil {
		return err
	}
	fd, err := unix.Openat(int(dir.Fd()), "operations.log", unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), "operations.log")
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	stat := st.Sys().(*syscall.Stat_t)
	if !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || stat.Uid != uint32(os.Getuid()) || stat.Nlink != 1 {
		return fmt.Errorf("unsafe audit file owner, permissions or links")
	}
	if st.Size() > 0 {
		tail := make([]byte, len(auditEnd))
		if st.Size() < int64(len(tail)) {
			return fmt.Errorf("incomplete audit record; preserve and repair log before new work")
		}
		if _, err = f.ReadAt(tail, st.Size()-int64(len(tail))); err != nil {
			return err
		}
		if string(tail) != auditEnd {
			return fmt.Errorf("incomplete audit record; preserve and repair log before new work")
		}
	}
	if err = use(f); err != nil {
		return err
	}
	return dir.Sync()
}

// AuditSnapshot copies under the writer lock. Vim sees a private snapshot and
// cannot overwrite the live append log even if its readonly option is overridden.
func AuditSnapshot(workspace string, dst io.Writer) error {
	return auditFile(workspace, func(f *os.File) error { _, err := io.Copy(dst, f); return err })
}
