package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// The fixture uses structured directory entries, never a shell-expanded pattern.
func (f *fixture) prepareBatch(pattern string) error {
	if _, err := filepath.Match(pattern, ""); err != nil {
		return err
	}
	directory, err := within(filepath.Join(f.root, f.current().name), f.current().cwd, ".")
	if err != nil {
		return err
	}
	items, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	b := &batchTransfer{directory: f.current().cwd, dest: f.download, state: "REVIEW"}
	for _, item := range items {
		match, _ := filepath.Match(pattern, item.Name())
		if !match {
			continue
		}
		info, err := item.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		b.files = append(b.files, batchFile{name: item.Name(), size: info.Size(), state: "QUEUED"})
	}
	if len(b.files) == 0 {
		return fmt.Errorf("no regular files match %q", pattern)
	}
	f.batch = b
	return nil
}

func (f *fixture) nextBatchFile() {
	b := f.batch
	for b.index < len(b.files) {
		item := &b.files[b.index]
		// Freeze paths at review time, even if the operator navigates during copying.
		mode := f.mode
		f.mode = "files"
		r := f.execute([]string{"get", filepath.Join(b.directory, item.name), filepath.Join(b.dest, item.name)})
		f.mode = mode
		if r.Error == "" {
			item.state = "RUNNING"
			return
		}
		item.state, item.detail = "FAILED", r.Error
		b.index++
	}
	b.state, b.updated = "COMPLETE", time.Now()
	count, total, done := b.counts()
	if count != len(b.files) {
		b.state = "PARTIAL"
	}
	f.progress = fmt.Sprintf("Batch %s · %d/%d files · %s / %s · %s elapsed", b.state, count, len(b.files), humanSize(done), humanSize(total), b.updated.Sub(b.started).Round(time.Millisecond))
}

func (b *batchTransfer) counts() (completed int, total, done int64) {
	for _, f := range b.files {
		if f.state == "COMPLETE" {
			completed++
		}
		total += f.size
		done += f.done
	}
	return
}

func (f *fixture) failTransfer(t *transfer, err error) {
	t.source.Close()
	t.dest.Close()
	os.Remove(t.path)
	f.copy = nil
	f.progress = "Transfer FAILED · " + err.Error()
	if b := f.batch; b != nil && b.state == "RUNNING" {
		b.files[b.index].state, b.files[b.index].detail = "FAILED", err.Error()
		b.index++
		f.nextBatchFile()
	}
}
