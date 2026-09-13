package launch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func digestOf(t *testing.T, path string) string {
	t.Helper()
	sum, err := digest(path)
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

func TestDigestReuseFollowsFileIdentity(t *testing.T) {
	// Reuse is process-local and keyed by the open file's identity: an
	// unchanged file is hashed once, while in-place writes and replacements
	// are always hashed again. No digest survives the process.
	dir := t.TempDir()
	path := filepath.Join(dir, "pinned")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	start := hashes.Load()
	first := digestOf(t, path)
	if digestOf(t, path) != first || hashes.Load() != start+1 {
		t.Fatalf("unchanged file hashed %d times", hashes.Load()-start)
	}
	// Preserve size and modification time; only content and change time move.
	original, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(path, []byte("FIRST"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, original.ModTime(), original.ModTime()); err != nil {
		t.Fatal(err)
	}
	second := digestOf(t, path)
	if second == first || hashes.Load() != start+2 {
		t.Fatalf("in-place write not rehashed: %d hashes", hashes.Load()-start)
	}
	replacement := filepath.Join(dir, "replacement")
	if err := os.WriteFile(replacement, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if digestOf(t, path) != first || hashes.Load() != start+3 {
		t.Fatalf("replacement not rehashed: %d hashes", hashes.Load()-start)
	}
	if digestOf(t, path) != first || hashes.Load() != start+3 {
		t.Fatal("stable replacement hashed again")
	}
}

func TestRegularDigestKeepsFileChecks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exe")
	if err := os.WriteFile(path, []byte("binary"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := regularDigest(path, 0700, 1<<20); err == nil {
		t.Fatal("wrong mode accepted")
	}
	if _, err := regularDigest(path, 0600, 3); err == nil {
		t.Fatal("oversized file accepted")
	}
	sum, err := regularDigest(path, 0600, 1<<20)
	if err != nil || sum != digestOf(t, path) {
		t.Fatalf("digest %q, %v", sum, err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := regularDigest(link, 0600, 1<<20); err == nil {
		t.Fatal("symlink followed")
	}
}

func TestPhaseWithoutTraceIsInert(t *testing.T) {
	if trace != nil {
		t.Skip("tracing enabled in this environment")
	}
	Phase("test")()
}
