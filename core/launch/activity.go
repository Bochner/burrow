package launch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// AuditCursor is private to a reader. A fresh reader starts at the current end;
// replacement/truncation restarts at zero with an explicit gap, never silently.
type AuditCursor struct {
	Device, Inode uint64
	Offset        int64
	Started       bool
}

type AuditEntry struct {
	Time, Action, Target, ID, State string
	Result                          json.RawMessage
	Offset                          int64
}

// ReadAudit reads complete records under the existing writer lock without
// creating files or moving any writer/other observer's position.
func ReadAudit(ctx context.Context, workspace string, cursor *AuditCursor) ([]AuditEntry, string, error) {
	path := filepath.Join(workspace, "burrow-logs")
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		if cursor.Inode != 0 {
			return nil, "", fmt.Errorf("operation log directory disappeared; position retained")
		}
		cursor.Started = true
		return nil, "", nil
	}
	dir, err := directory(path, false, true)
	if err != nil {
		return nil, "", err
	}
	defer dir.Close()
	if err := lock(ctx, dir); err != nil {
		return nil, "", err
	}
	fd, err := unix.Openat(int(dir.Fd()), "operations.log", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err == unix.ENOENT {
		if cursor.Inode != 0 {
			return nil, "", fmt.Errorf("operation log disappeared; position retained")
		}
		cursor.Started = true
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	f := os.NewFile(uintptr(fd), "operations.log")
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, "", err
	}
	s := st.Sys().(*syscall.Stat_t)
	if !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || s.Uid != uint32(os.Getuid()) || s.Nlink != 1 {
		return nil, "", fmt.Errorf("unsafe audit file owner, permissions or links")
	}
	next := *cursor
	gap := ""
	if !next.Started {
		if st.Size() > 0 {
			tail := make([]byte, len(auditEnd))
			if st.Size() < int64(len(tail)) {
				return nil, "", fmt.Errorf("operation log has an incomplete record; position retained")
			}
			if _, err := f.ReadAt(tail, st.Size()-int64(len(tail))); err != nil {
				return nil, "", err
			}
			if string(tail) != auditEnd {
				return nil, "", fmt.Errorf("operation log has an incomplete record; position retained")
			}
		}
		next.Offset = st.Size()
	} else if (next.Inode != 0 && (next.Device != uint64(s.Dev) || next.Inode != s.Ino)) || next.Offset > st.Size() {
		next.Offset = 0
		gap = "Operation log replaced or truncated; earlier evidence may be missing and replayed records may repeat"
	}
	next.Started, next.Device, next.Inode = true, uint64(s.Dev), s.Ino
	// ponytail: 8 MiB per read/record ceiling; add incremental large-record parsing
	// if a real operation exceeds it. Never skip oversized or incomplete evidence.
	const limit = 8 << 20
	data := make([]byte, min(limit+1, st.Size()-next.Offset))
	n, err := f.ReadAt(data, next.Offset)
	if err != nil && err != io.EOF {
		return nil, "", err
	}
	data = data[:n]
	end := bytes.LastIndex(data, []byte(auditEnd))
	if end < 0 {
		if len(data) > 0 {
			return nil, "", fmt.Errorf("operation log has an incomplete or oversized record; position retained")
		}
		*cursor = next
		return nil, gap, nil
	}
	complete := data[:end+len(auditEnd)]
	var records []AuditEntry
	for _, block := range bytes.Split(complete, []byte(auditEnd)) {
		if len(block) == 0 {
			continue
		}
		var record AuditEntry
		lines := strings.Split(string(block), "\n")
		stamp, action, ok := strings.Cut(lines[0], " -- ")
		if !ok {
			return nil, "", fmt.Errorf("invalid operation header; position retained")
		}
		if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
			return nil, "", fmt.Errorf("invalid operation timestamp; position retained")
		}
		record.Time, record.Action = stamp, action
		var body strings.Builder
		var scope string
		for _, line := range lines[1:] {
			switch {
			case strings.HasPrefix(line, "    "):
				body.WriteString(line[4:])
				body.WriteByte('\n')
			case strings.HasPrefix(line, "  Workspace: "):
				scope = strings.TrimPrefix(line, "  Workspace: ")
			case strings.HasPrefix(line, "  Target: "):
				record.Target = strings.TrimPrefix(line, "  Target: ")
			case strings.HasPrefix(line, "  Operation: "):
				record.ID = strings.TrimPrefix(line, "  Operation: ")
			case strings.HasPrefix(line, "  Status: "):
				record.State = strings.TrimPrefix(line, "  Status: ")
			}
		}
		if scope != auditText(workspace) || record.ID == "" || record.State == "" || !json.Valid([]byte(body.String())) {
			return nil, "", fmt.Errorf("invalid operation record; position retained")
		}
		record.Result = json.RawMessage(body.String())
		next.Offset += int64(len(block) + len(auditEnd))
		record.Offset = next.Offset
		records = append(records, record)
	}
	*cursor = next
	return records, gap, nil
}

// RedactedEvidence copies structured evidence and applies the same credential
// field policy as operation notes. Arbitrary program output is not a secret vault.
func RedactedEvidence(input any) (any, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	redactAudit(value)
	return value, nil
}
