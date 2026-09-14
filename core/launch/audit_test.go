package launch_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Bochner/burrow/core/launch"
)

func TestAuditPersistenceIsolationAndFailures(t *testing.T) {
	w := t.TempDir()
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := launch.BeginAudit(w, fmt.Sprintf("get file-%d", i), "target", nil)
			if err == nil {
				err = a.Finish(map[string]any{"password": "AUTH_CANARY", "nested": map[string]any{"passphrase": "KEY_CANARY"}, "output": "\x1b[31m\n2026-09-13T00:00:00Z -- forged\r\u202e", "bytes": int64(9007199254740993)}, nil)
			}
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var b bytes.Buffer
	if err := launch.AuditSnapshot(w, &b); err != nil {
		t.Fatal(err)
	}
	text := b.String()
	if strings.Count(text, "End record\n\n") != 24 || strings.Count(text, "Status: completed") != 12 {
		t.Fatal("concurrent records missing or interleaved")
	}
	for _, forbidden := range []string{"AUTH_CANARY", "KEY_CANARY", "\x1b", "\r", "\u202e", "\n2026-09-13T00:00:00Z -- forged"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("unsafe output %q", forbidden)
		}
	}
	if !strings.Contains(text, "9007199254740993") || !strings.Contains(text, "forged") {
		t.Fatal("safe evidence lost")
	}
	b.Reset()
	if err := launch.AuditSnapshot(t.TempDir(), &b); err != nil || b.Len() != 0 {
		t.Fatal("workspace isolation", err)
	}
	path := filepath.Join(w, "burrow-logs", "operations.log")
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("private log", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := launch.BeginAudit(w, "refused", "target", nil); err == nil {
		t.Fatal("unsafe permissions accepted")
	}
	os.Chmod(path, 0600)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("partial write")
	f.Close()
	if _, err := launch.BeginAudit(w, "refused", "target", nil); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatal("incomplete record accepted", err)
	}
	if err := launch.AuditSnapshot(w, &b); err == nil {
		t.Fatal("incomplete snapshot accepted")
	}
}

func TestAuditRefusesLinksAndPreservesLargeResults(t *testing.T) {
	w := t.TempDir()
	a, err := launch.BeginAudit(w, "ls", "target", nil)
	if err != nil {
		t.Fatal(err)
	}
	large := strings.Repeat("customer output ", 100000)
	if err := a.Finish(large, nil); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := launch.AuditSnapshot(w, &b); err != nil || !strings.Contains(b.String(), large) {
		t.Fatal("result truncated", err)
	}
	path := filepath.Join(w, "burrow-logs", "operations.log")
	if err := os.Link(path, filepath.Join(w, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := launch.BeginAudit(w, "refused", "target", nil); err == nil {
		t.Fatal("hardlink accepted")
	}
	os.Remove(filepath.Join(w, "alias"))
	os.Rename(path, path+".original")
	os.Symlink(path+".original", path)
	if _, err := launch.BeginAudit(w, "refused", "target", nil); err == nil {
		t.Fatal("symlink accepted")
	}
}
