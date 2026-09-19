// Package reports registers and reads portable Markdown reports using Hovel's
// artifact inventory. It owns no database and never executes a report's content.
package reports

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Bochner/burrow/core/launch"
	"github.com/vibepwners/hovel/sdk/go/hovel"
)

const metadataKind = "application/vnd.burrow.report+json"

// MaxBytes bounds memory and rendering work; the original artifact is retained.
const MaxBytes = 1 << 20

type Metadata struct {
	Version    int    `json:"version"`
	Title      string `json:"title"`
	Producer   string `json:"producer"`
	Connection string `json:"connection"`
	Host       string `json:"host"`
	User       string `json:"user"`
	Port       int    `json:"port"`
	SourceRun  string `json:"sourceRun"`
	Status     string `json:"status"`
	Exit       *int   `json:"exit"`
	Complete   bool   `json:"complete"`
	Detail     string `json:"detail,omitempty"`
	Markdown   string `json:"markdown"`
}

type artifact struct {
	ID, RunID, ModuleID, Name, Kind, Path, SHA256, CreatedAt string
	Size                                                     int64
}

type Entry struct {
	Metadata
	ID        string `json:"id"`
	CreatedAt string `json:"createdAt"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Error     string `json:"error,omitempty"`
	document  artifact
}

type Document struct {
	Report Entry  `json:"report"`
	Text   string `json:"markdown"`
}

// Artifacts is the producer seam: return these through the existing Hovel SDK
// result, which performs persistence. A returned value is not proof of saving.
func Artifacts(name string, meta Metadata, path string) ([]hovel.Artifact, error) {
	if name == "" || len(name) > 128 || strings.ContainsAny(name, "/\\\x00\r\n") || meta.Title == "" || meta.Producer == "" {
		return nil, fmt.Errorf("report requires a safe name, title and producer")
	}
	meta.Version, meta.Markdown = 1, name+".md"
	data, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	return []hovel.Artifact{
		hovel.FileArtifact(meta.Markdown, "text/markdown", path),
		hovel.InlineArtifact(name+".report.json", metadataKind, string(data)),
	}, nil
}

// List projects registered report metadata, retaining an unavailable entry for
// malformed/missing metadata rather than silently hiding collected evidence.
func List(ctx context.Context, workspace string) ([]Entry, error) {
	data, err := launch.HovelCLI(ctx, workspace, "--", "artifact", "list", "--json")
	if err != nil {
		return nil, err
	}
	var inventory []artifact
	if err := json.Unmarshal(data, &inventory); err != nil {
		return nil, fmt.Errorf("invalid artifact inventory: %w", err)
	}
	byName := make(map[string]artifact)
	for _, a := range inventory {
		byName[a.RunID+"\x00"+a.ModuleID+"\x00"+a.Name] = a
	}
	entries := []Entry{}
	for _, a := range inventory {
		if a.Kind != metadataKind {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		e := Entry{ID: a.ID, CreatedAt: a.CreatedAt, Metadata: Metadata{Title: "Unavailable report"}}
		data, err := readArtifact(workspace, a, 64<<10)
		if err == nil && (json.Unmarshal(data, &e.Metadata) != nil || e.Version != 1 || e.Title == "" || e.Producer == "" || e.Markdown == "") {
			err = fmt.Errorf("invalid report metadata")
		}
		if err == nil {
			doc, ok := byName[a.RunID+"\x00"+a.ModuleID+"\x00"+e.Markdown]
			if !ok || doc.Kind != "text/markdown" {
				err = fmt.Errorf("Markdown artifact is missing from this collection")
			} else {
				e.document, e.Path, e.SHA256, e.Size = doc, doc.Path, doc.SHA256, doc.Size
			}
		}
		if err != nil {
			e.Error = err.Error()
		}
		entries = append(entries, e)
	}
	slices.SortFunc(entries, func(a, b Entry) int {
		at, _ := time.Parse(time.RFC3339Nano, a.CreatedAt)
		bt, _ := time.Parse(time.RFC3339Nano, b.CreatedAt)
		return bt.Compare(at)
	})
	return entries, nil
}

func Read(ctx context.Context, workspace, id string) (Document, error) {
	entries, err := List(ctx, workspace)
	if err != nil {
		return Document{}, err
	}
	for _, e := range entries {
		if e.ID != id {
			continue
		}
		if e.Error != "" {
			return Document{}, fmt.Errorf("report unavailable: %s", e.Error)
		}
		data, err := readArtifact(workspace, e.document, MaxBytes)
		if err != nil {
			return Document{}, err
		}
		return Document{Report: e, Text: string(data)}, nil
	}
	return Document{}, fmt.Errorf("report is not registered in this workspace")
}

func readArtifact(workspace string, a artifact, limit int64) ([]byte, error) {
	if !filepath.IsLocal(a.Path) || filepath.Clean(a.Path) != a.Path || !strings.HasPrefix(a.Path, "artifacts/") || a.Size < 0 || len(a.SHA256) != 64 {
		return nil, fmt.Errorf("invalid report artifact record")
	}
	if a.Size > limit {
		return nil, fmt.Errorf("report exceeds the %d-byte reader limit; original retained at %s", limit, a.Path)
	}
	root, err := launch.FileRoot(filepath.Join(workspace, filepath.Dir(a.Path)), false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.OpenFile(filepath.Base(a.Path), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || stat.Uid != uint32(os.Getuid()) || stat.Nlink != 1 || info.Size() != a.Size {
		return nil, fmt.Errorf("report artifact is not an unchanged private regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != a.Size || fmt.Sprintf("%x", sha256.Sum256(data)) != a.SHA256 {
		return nil, fmt.Errorf("report artifact changed; SHA256 verification failed")
	}
	return data, nil
}
