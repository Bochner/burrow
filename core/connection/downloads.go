package connection

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Bochner/burrow/core/launch"
	"github.com/pkg/sftp"
	"github.com/vibepwners/hovel/sdk/go/hovel"
)

// DownloadPlan freezes the effective paths and observed metadata before approval.
// Working files never become registered evidence implicitly.
type DownloadPlan struct {
	Operation    string         `json:"operation"`
	Pattern      string         `json:"pattern"`
	Local        string         `json:"local"`
	Workspace    string         `json:"workspace"`
	Owner        State          `json:"owner"`
	Root         string         `json:"root"`
	RootIdentity string         `json:"rootIdentity"`
	Files        []DownloadFile `json:"files"`
	Digest       string         `json:"digest"`
}

type DownloadFile struct {
	Started        time.Time `json:"started,omitzero"`
	Completed      time.Time `json:"completed,omitzero"`
	Source         string    `json:"source"`
	SourceIdentity string    `json:"sourceIdentity,omitempty"`
	Destination    string    `json:"destination"`
	Relative       string    `json:"relative"`
	Size           int64     `json:"size"` // -1 means unknown, never an empty file.
	Modified       time.Time `json:"modified"`
	Existing       string    `json:"existing"`
	Bytes          int64     `json:"bytes"`
	Elapsed        float64   `json:"elapsed"`
	AverageRate    float64   `json:"averageRate"`
	State          string    `json:"state"`
	Partial        string    `json:"partial,omitempty"`
	Detail         string    `json:"detail,omitempty"`
}

type Download struct {
	Completed      time.Time      `json:"completed,omitzero"`
	CompletedFiles int            `json:"completedFiles"`
	FailedFiles    int            `json:"failedFiles"`
	CancelledFiles int            `json:"cancelledFiles"`
	ID             string         `json:"id"`
	RunID          string         `json:"runID"`
	Plan           DownloadPlan   `json:"plan"`
	Files          []DownloadFile `json:"files"`
	State          string         `json:"state"`
	Started        time.Time      `json:"started"`
	Elapsed        float64        `json:"elapsed"`
	Bytes          int64          `json:"bytes"`
	Rate           float64        `json:"rate"` // bytes/sec averaged over the last measurement interval
	AverageRate    float64        `json:"averageRate"`
	ETA            *float64       `json:"eta"` // nil when unknown, stalled, or not enough measurements
	Detail         string         `json:"detail,omitempty"`
}

type Downloads struct {
	Warning     string                   `json:"warning,omitempty"`
	Records     []Download               `json:"records"`
	Files       int                      `json:"files"`
	Bytes       int64                    `json:"bytes"`
	Connections map[string]DownloadTotal `json:"connections"`
}

type DownloadTotal struct {
	Name  string `json:"name"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

type downloadWork struct {
	mu          sync.Mutex
	value       Download
	start       time.Time
	sample      time.Time
	sampleBytes int64
	cancel      context.CancelFunc
	done        chan struct{}
}

type downloadRequest struct {
	ID   string       `json:"id"`
	Plan DownloadPlan `json:"plan"`
}

func (p DownloadPlan) hash() string {
	p.Digest = ""
	b, _ := json.Marshal(p)
	return digest(string(b))
}

func localIdentity(info os.FileInfo, directory bool) string {
	s := info.Sys().(*syscall.Stat_t)
	if directory {
		return fmt.Sprintf("%d:%d", s.Dev, s.Ino)
	}
	return fmt.Sprintf("%d:%d:%d:%d:%d:%d:%d", s.Dev, s.Ino, info.Size(), s.Mtim.Sec, s.Mtim.Nsec, s.Ctim.Sec, s.Ctim.Nsec)
}

// Resolve the nearest existing ancestor, then access exclusively through Root.
// This also permits new nested destinations without writing during review.
func downloadDestination(root *os.Root, base, target string) (string, string, error) {
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	rel, err := filepath.Rel(base, target)
	if err != nil || !filepath.IsLocal(rel) {
		return "", "", fmt.Errorf("download destination is outside the configured root")
	}
	ancestor, suffix := target, ""
	for {
		_, err = os.Lstat(ancestor)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) || ancestor == base {
			return "", "", fmt.Errorf("download destination unavailable: %w", err)
		}
		suffix = filepath.Join(filepath.Base(ancestor), suffix)
		ancestor = filepath.Dir(ancestor)
	}
	resolved, err := containedLocal(base, ancestor)
	if err != nil {
		return "", "", err
	}
	rel = filepath.Join(resolved, suffix)
	info, err := root.Stat(rel)
	if os.IsNotExist(err) {
		return rel, "", nil
	}
	if err != nil {
		return "", "", err
	}
	if !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("destination must be a regular file, not a directory or special file")
	}
	return rel, localIdentity(info, false), nil
}

func remoteIdentity(info os.FileInfo) string {
	return fmt.Sprintf("%d:%d:%d", info.Mode(), info.Size(), info.ModTime().UnixNano())
}

func remoteDestination(client *sftp.Client, source, target string) (string, string, error) {
	if target == "" {
		target = path.Base(source)
	}
	if strings.HasPrefix(target, "~") {
		if target != "~" && !strings.HasPrefix(target, "~/") {
			return "", "", fmt.Errorf("use ~ or ~/PATH for the connected account")
		}
		home, err := client.RealPath(".")
		if err != nil {
			return "", "", err
		}
		target = home + strings.TrimPrefix(target, "~")
	}
	if !path.IsAbs(target) {
		cwd, err := client.RealPath(".")
		if err != nil {
			return "", "", err
		}
		target = path.Join(cwd, target)
	}
	if info, err := client.Lstat(target); err == nil && info.IsDir() {
		target = path.Join(target, path.Base(source))
	}
	dir, err := client.RealPath(path.Dir(target))
	if err != nil {
		return "", "", fmt.Errorf("remote destination directory unavailable: %w", err)
	}
	target = path.Join(dir, path.Base(target))
	info, err := client.Lstat(target)
	if os.IsNotExist(err) {
		return target, "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("remote destination unavailable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("remote destination must be a regular file")
	}
	return target, remoteIdentity(info), nil
}

func openFileClient(ctx context.Context, state State) (*sftp.Client, func(), error) {
	process := fileSSH(ctx, state, "-s", "unused", "sftp")
	in, err := process.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	out, err := process.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, nil, err
	}
	if err = process.Start(); err != nil {
		in.Close()
		return nil, nil, err
	}
	cleanup := func() { in.Close(); process.Process.Kill(); process.Wait() }
	client, err := sftp.NewClientPipe(out, in)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("SFTP subsystem unavailable; no new authentication attempted")
	}
	// Close waits for the reader goroutine: terminate our subsystem client first
	// so a server which keeps stdout open cannot delay cleanup indefinitely.
	return client, func() { in.Close(); process.Process.Kill(); client.Close(); process.Wait() }, nil
}

// ReviewDownloads is the same qualified seam used by the file tab and CLI.
func ReviewDownloads(ctx context.Context, w string, s State, operation, remote, local string) (DownloadPlan, error) {
	var result struct {
		Plan  DownloadPlan `json:"plan"`
		Error string       `json:"error"`
	}
	if operation != "get" && operation != "mget" && operation != "put" {
		return result.Plan, fmt.Errorf("expected get, mget or put")
	}
	if s.Generation == "" || s.Creation == "" || s.State != "connected" {
		return result.Plan, fmt.Errorf("transfer requires the selected live connection")
	}
	err := managerControl(ctx, w, managerIdentity{Session: s.Session, Generation: s.Generation}, "download-review", []string{s.Creation, operation, remote, local}, &result)
	if err == nil && result.Error != "" {
		err = fmt.Errorf("%s", result.Error)
	}
	return result.Plan, err
}

func (s *owner) reviewDownloads(ctx context.Context, operation, remote, local string) (value DownloadPlan, failure error) {
	s.mu.Lock()
	state := s.state
	s.mu.Unlock()
	a, err := launch.BeginAudit(s.config.Workspace, "review "+operation+" "+remote+" "+local, targetLabel(state), map[string]string{"operation": operation, "source": remote, "destination": local})
	if err != nil {
		return value, err
	}
	defer func() { failure = a.Finish(value, failure) }()
	return s.planDownloads(ctx, operation, remote, local)
}

func (s *owner) planDownloads(ctx context.Context, operation, remote, local string) (DownloadPlan, error) {
	if operation == "put" {
		return s.planUpload(ctx, remote, local)
	}
	p := DownloadPlan{Workspace: s.config.Workspace, Operation: operation, Pattern: remote, Local: local, Files: []DownloadFile{}}
	if len(remote) == 0 || len(remote) > 4096 || len(local) > 4096 || strings.ContainsRune(remote+local, 0) {
		return p, fmt.Errorf("invalid download path")
	}
	s.mu.Lock()
	if s.closed || s.downloadClosing || s.state.State != "connected" {
		s.mu.Unlock()
		return p, fmt.Errorf("download connection unavailable; reconnect explicitly")
	}
	err := s.checkMaster()
	p.Owner = s.state
	s.mu.Unlock()
	if err != nil {
		return p, err
	}
	// Bind only transport identity, not mutable tunnel observations.
	p.Owner.Proxy = Tunnel{}
	p.Owner.TunnelCount, p.Owner.TunnelRevision = 0, 0
	roots, err := TransferRoots(ctx, p.Workspace)
	if err != nil {
		return p, err
	}
	p.Root = roots.Download
	root, err := launch.FileRoot(p.Root, false)
	if err != nil {
		return p, err
	}
	defer root.Close()
	info, err := root.Stat(".")
	if err != nil {
		return p, err
	}
	p.RootIdentity = localIdentity(info, true)
	client, closeClient, err := openFileClient(ctx, p.Owner)
	if err != nil {
		return p, err
	}
	defer closeClient()
	if strings.HasPrefix(remote, "~") {
		if remote != "~" && !strings.HasPrefix(remote, "~/") {
			return p, fmt.Errorf("use ~ or ~/PATH for the connected account")
		}
		home, e := client.RealPath(".")
		if e != nil {
			return p, e
		}
		remote = home + strings.TrimPrefix(remote, "~")
	}
	sources := []string{remote}
	if operation == "mget" {
		dir, pattern := path.Split(remote)
		if dir == "" {
			dir = "."
		}
		if _, err := path.Match(pattern, ""); err != nil {
			return p, fmt.Errorf("invalid download pattern: %w", err)
		}
		entries, err := client.ReadDirContext(ctx, dir)
		if err != nil {
			return p, fmt.Errorf("batch discovery failed: %w", err)
		}
		sources = nil
		for _, entry := range entries {
			if strings.ContainsAny(entry.Name(), "/\x00") || entry.Name() == "." || entry.Name() == ".." {
				return p, fmt.Errorf("invalid remote filename")
			}
			match, _ := path.Match(pattern, entry.Name())
			if match && entry.Mode().IsRegular() {
				sources = append(sources, path.Join(dir, entry.Name()))
			}
		}
		slices.Sort(sources)
		if len(sources) > 1000 {
			return p, fmt.Errorf("batch exceeds 1000 files; narrow the pattern; no partial review returned")
		}
	}
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return p, err
		}
		canonical, err := client.RealPath(source)
		if err != nil {
			return p, err
		}
		info, err := client.Stat(canonical)
		if err != nil {
			return p, fmt.Errorf("remote file unavailable: %w", err)
		}
		if !info.Mode().IsRegular() {
			return p, fmt.Errorf("download source must be a regular file or a link to one")
		}
		attrs, ok := info.Sys().(*sftp.FileStat)
		if !ok || attrs.Mode&0170000 != 0100000 {
			return p, fmt.Errorf("remote regular-file type unavailable")
		}
		size := info.Size()
		// The pinned SFTP client drops attribute-presence flags. Probe an apparent
		// empty file so an omitted size can never masquerade as a zero-byte copy.
		if size == 0 {
			probe, e := client.Open(canonical)
			if e != nil {
				return p, e
			}
			var one [1]byte
			n, e := probe.Read(one[:])
			probe.Close()
			if n > 0 {
				size = -1
			} else if e != io.EOF {
				return p, fmt.Errorf("empty file could not be verified")
			}
		}
		destination := local
		if destination == "" {
			destination = path.Base(source)
		} else if operation == "mget" || strings.HasSuffix(destination, "/") {
			destination = filepath.Join(destination, path.Base(source))
		}
		rel, existing, err := downloadDestination(root, p.Root, destination)
		if err != nil {
			return p, err
		}
		p.Files = append(p.Files, DownloadFile{Source: canonical, Destination: filepath.Join(p.Root, rel), Relative: rel, Size: size, Modified: info.ModTime(), Existing: existing, State: "pending"})
	}
	encoded, _ := json.Marshal(p)
	if len(encoded) > 256<<10 {
		return p, fmt.Errorf("download review exceeds 256 KiB; narrow the pattern; no partial review returned")
	}
	p.Digest = p.hash()
	return p, nil
}

func (s *owner) planUpload(ctx context.Context, local, remote string) (DownloadPlan, error) {
	p := DownloadPlan{Workspace: s.config.Workspace, Operation: "put", Pattern: local, Local: remote, Files: []DownloadFile{}}
	if local == "" || len(local) > 4096 || len(remote) > 4096 || strings.ContainsRune(local+remote, 0) {
		return p, fmt.Errorf("invalid upload path")
	}
	s.mu.Lock()
	if s.closed || s.downloadClosing || s.state.State != "connected" {
		s.mu.Unlock()
		return p, fmt.Errorf("upload connection unavailable; reconnect explicitly")
	}
	err := s.checkMaster()
	p.Owner = s.state
	s.mu.Unlock()
	if err != nil {
		return p, err
	}
	p.Owner.Proxy = Tunnel{}
	p.Owner.TunnelCount, p.Owner.TunnelRevision = 0, 0
	roots, err := TransferRoots(ctx, p.Workspace)
	if err != nil {
		return p, err
	}
	p.Root = roots.Upload
	root, err := launch.FileRoot(p.Root, false)
	if err != nil {
		return p, err
	}
	defer root.Close()
	info, err := root.Stat(".")
	if err != nil {
		return p, err
	}
	p.RootIdentity = localIdentity(info, true)
	rel, err := containedLocal(p.Root, local)
	if err != nil {
		return p, err
	}
	source, err := root.Open(rel)
	if err != nil {
		return p, fmt.Errorf("upload source unavailable; place intended files in %s: %w", p.Root, err)
	}
	defer source.Close()
	info, err = source.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return p, fmt.Errorf("upload source must be a regular file inside %s", p.Root)
	}
	client, closeClient, err := openFileClient(ctx, p.Owner)
	if err != nil {
		return p, err
	}
	defer closeClient()
	destination, existing, err := remoteDestination(client, rel, remote)
	if err != nil {
		return p, err
	}
	p.Files = append(p.Files, DownloadFile{Source: filepath.Join(p.Root, rel), SourceIdentity: localIdentity(info, false), Destination: destination, Relative: rel, Size: info.Size(), Modified: info.ModTime(), Existing: existing, State: "pending"})
	p.Digest = p.hash()
	return p, nil
}

func StartDownloads(ctx context.Context, p DownloadPlan) (Download, error) {
	var result Download
	if p.Digest != p.hash() || len(p.Files) == 0 {
		return result, fmt.Errorf("empty or changed download review")
	}
	r := downloadRequest{ID: rand.Text(), Plan: p}
	raw, _ := json.Marshal(r)
	err := managerThrow(ctx, p.Workspace, map[string]string{"action": "download", "session": p.Owner.Session, "generation": p.Owner.Generation, "request": string(raw), "review": digest(string(raw))}, &result)
	return result, err
}

func (m *manager) downloadCommand(req hovel.PayloadCommandRequest) (result hovel.PayloadCommandResult, failure error) {
	defer func() {
		if failure != nil {
			b, _ := json.Marshal(map[string]string{"error": failure.Error()})
			result = hovel.PayloadCommandResult{Command: req.Command, Stdout: string(b)}
			failure = nil
		}
	}()
	if len(req.Args) < 1 || req.Args[0] != m.Generation {
		return result, fmt.Errorf("exact manager generation required")
	}
	ctx, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	if err := launch.VerifyReservation(ctx, m.Workspace, m.dir); err != nil {
		return result, err
	}
	var value any
	switch req.Command {
	case "download-review":
		if len(req.Args) != 5 || (req.Args[2] != "get" && req.Args[2] != "mget" && req.Args[2] != "put") {
			return result, fmt.Errorf("expected qualified transfer review")
		}
		m.mu.Lock()
		s := m.connections[req.Args[1]]
		closed := m.closed
		m.mu.Unlock()
		if s == nil || closed {
			return result, fmt.Errorf("download connection creation unavailable")
		}
		p, err := s.reviewDownloads(ctx, req.Args[2], req.Args[3], req.Args[4])
		if err != nil {
			return result, err
		}
		value = map[string]any{"plan": p}
	case "downloads":
		if len(req.Args) != 1 {
			return result, fmt.Errorf("unexpected download observation arguments")
		}
		m.downloadMu.Lock()
		works := slices.Collect(maps.Values(m.downloads))
		m.downloadMu.Unlock()
		records := []Download{}
		for _, work := range works {
			records = append(records, work.snapshot())
		}
		value = map[string]any{"records": records}
	case "download-cancel":
		if len(req.Args) != 2 {
			return result, fmt.Errorf("download ID required")
		}
		m.downloadMu.Lock()
		work := m.downloads[req.Args[1]]
		m.downloadMu.Unlock()
		if work == nil {
			return result, fmt.Errorf("download is not active in this owner")
		}
		work.cancel()
		select {
		case <-work.done:
		case <-ctx.Done():
			return result, fmt.Errorf("cancellation requested; cleanup unconfirmed; inspect downloads")
		}
		value = work.snapshot()
	default:
		return result, fmt.Errorf("unsupported download control")
	}
	b, err := json.Marshal(value)
	return hovel.PayloadCommandResult{Command: req.Command, Stdout: string(b)}, err
}

func (work *downloadWork) snapshot() Download {
	work.mu.Lock()
	defer work.mu.Unlock()
	d := work.value
	d.Files = slices.Clone(d.Files)
	if d.State == "running" {
		d.Elapsed = time.Since(work.start).Seconds()
	}
	d.Bytes = 0
	var total int64
	for i := range d.Files {
		f := &d.Files[i]
		if f.State == "running" && !f.Started.IsZero() {
			f.Elapsed = time.Since(f.Started).Seconds()
		}
		if f.Elapsed > 0 {
			f.AverageRate = float64(f.Bytes) / f.Elapsed
		}
		d.Bytes += f.Bytes
		if f.Size < 0 {
			total = -1
		} else if total >= 0 {
			total += f.Size
		}
	}
	if d.Elapsed > 0 {
		d.AverageRate = float64(d.Bytes) / d.Elapsed
	}
	now := time.Now()
	if now.Sub(work.sample) >= time.Second {
		d.Rate = float64(d.Bytes-work.sampleBytes) / now.Sub(work.sample).Seconds()
		work.sample, work.sampleBytes = now, d.Bytes
		work.value.Rate = d.Rate
	}
	if d.State != "running" {
		d.Rate = 0
	}
	d.ETA = nil
	if d.State == "running" && d.Elapsed >= 1 && d.Rate > 0 && total >= d.Bytes && d.AverageRate > 0 {
		eta := float64(total-d.Bytes) / d.AverageRate
		d.ETA = &eta
	}
	return d
}

func recordDownload(ctx context.Context, w string, d Download) (failure error) {
	a := launch.Audit{Workspace: w, Action: d.Plan.Operation + " " + d.Plan.Pattern + " " + d.Plan.Local, Target: targetLabel(d.Plan.Owner), ID: d.ID}
	auditErr := a.Record(d.State, d)
	if auditErr != nil {
		d.Detail += "; " + auditErr.Error()
	}
	defer func() { failure = errors.Join(failure, auditErr) }()

	for _, call := range []struct {
		method string
		input  any
	}{
		{"CreateOperation", map[string]string{"Operation": "burrow"}},
		{"CreateChain", map[string]string{"Operation": "burrow", "Chain": "downloads"}},
	} {
		var ignored any
		if err := launch.Call(ctx, w, call.method, call.input, &ignored); err != nil {
			return err
		}
	}
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	var ignored any
	return launch.Call(ctx, w, "AppendLog", map[string]any{"Operation": "burrow", "Chain": "downloads", "Entries": []map[string]any{{"Time": time.Now().UTC(), "Kind": "event", "Level": "info", "Source": "burrow-download", "Message": string(b)}}}, &ignored)
}

func recordDownloadFile(ctx context.Context, w string, d Download, index int) (failure error) {
	a := launch.Audit{Workspace: w, Action: d.Plan.Operation + " file " + d.Files[index].Source, Target: targetLabel(d.Plan.Owner), ID: d.ID}
	auditErr := a.Record(d.Files[index].State, d.Files[index])
	defer func() { failure = errors.Join(failure, auditErr) }()

	b, err := json.Marshal(struct {
		ID    string       `json:"id"`
		Index int          `json:"index"`
		File  DownloadFile `json:"file"`
	}{d.ID, index, d.Files[index]})
	if err != nil {
		return err
	}
	var ignored any
	return launch.Call(ctx, w, "AppendLog", map[string]any{"Operation": "burrow", "Chain": "downloads", "Entries": []map[string]any{{"Time": time.Now().UTC(), "Kind": "event", "Level": "info", "Source": "burrow-download-file", "Message": string(b)}}}, &ignored)
}

func DownloadHistory(ctx context.Context, w string) (Downloads, error) {
	// ponytail: replay the retained log; use a Hovel cursor if history volume makes polling costly.
	result := Downloads{Records: []Download{}, Connections: map[string]DownloadTotal{}}
	var logs []struct{ Source, Message string }
	if err := launch.Call(ctx, w, "ActiveLogs", map[string]string{"Operation": "burrow", "Chain": "downloads"}, &logs); err != nil {
		return result, err
	}
	records := map[string]Download{}
	for _, log := range logs {
		if log.Source == "burrow-download" {
			var d Download
			if err := json.Unmarshal([]byte(log.Message), &d); err != nil {
				return result, fmt.Errorf("download history invalid")
			}
			if d.State == "running" {
				d.State = "interrupted"
				d.Detail = "owner observation unavailable; inspect labelled partial and destination"
			}
			records[d.ID] = d
		}
		if log.Source == "burrow-download-file" {
			var event struct {
				ID    string       `json:"id"`
				Index int          `json:"index"`
				File  DownloadFile `json:"file"`
			}
			if json.Unmarshal([]byte(log.Message), &event) != nil {
				return result, fmt.Errorf("download file history invalid")
			}
			d, ok := records[event.ID]
			if !ok || event.Index < 0 || event.Index >= len(d.Files) {
				return result, fmt.Errorf("download history incomplete; totals unavailable")
			}
			d.Files[event.Index] = event.File
			records[event.ID] = d
		}
	}
	id, err := findManager(ctx, w)
	if err == nil && id.Session != "" {
		var live struct {
			Records []Download `json:"records"`
			Error   string     `json:"error"`
		}
		err = managerControl(ctx, w, id, "downloads", nil, &live)
		if err == nil && live.Error != "" {
			err = fmt.Errorf("%s", live.Error)
		}
		if err == nil {
			for _, d := range live.Records {
				records[d.ID] = d
			}
		}
	}
	if err != nil {
		result.Warning = "Live owner unverified; showing durable outcomes only: " + err.Error()
	}
	for _, d := range records {
		if d.State == "interrupted" {
			d.Bytes, d.Rate, d.AverageRate, d.ETA = 0, 0, 0, nil
			for _, f := range d.Files {
				d.Bytes += f.Bytes
			}
		}
		result.Records = append(result.Records, d)
		for _, f := range d.Files {
			if d.Plan.Operation == "put" {
				continue
			}
			if f.State == "complete" {
				result.Files++
				result.Bytes += f.Bytes
				total := result.Connections[d.Plan.Owner.Creation]
				total.Name = d.Plan.Owner.Name
				total.Files++
				total.Bytes += f.Bytes
				result.Connections[d.Plan.Owner.Creation] = total
			}
		}
	}
	slices.SortFunc(result.Records, func(a, b Download) int { return a.Started.Compare(b.Started) })
	return result, nil
}

func decodeDownload(raw string) (downloadRequest, error) {
	var r downloadRequest
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF || len(r.ID) != 26 || strings.ContainsAny(r.ID, "/\\.") || len(r.Plan.Files) == 0 || len(r.Plan.Files) > 1000 || r.Plan.Digest != r.Plan.hash() || (r.Plan.Operation != "get" && r.Plan.Operation != "mget" && r.Plan.Operation != "put") {
		return r, fmt.Errorf("invalid immutable transfer request")
	}
	for _, c := range r.ID {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return r, fmt.Errorf("invalid download ID")
		}
	}
	for _, f := range r.Plan.Files {
		pathsValid := path.IsAbs(f.Source) && filepath.IsLocal(f.Relative)
		if r.Plan.Operation == "put" {
			pathsValid = filepath.IsAbs(f.Source) && f.Source == filepath.Join(r.Plan.Root, f.Relative) && f.SourceIdentity != "" && path.IsAbs(f.Destination)
		} else {
			pathsValid = pathsValid && f.SourceIdentity == "" && f.Destination == filepath.Join(r.Plan.Root, f.Relative)
		}
		if !pathsValid || strings.ContainsRune(f.Source+f.Destination, 0) || f.State != "pending" || f.Bytes != 0 || f.Elapsed != 0 || f.AverageRate != 0 || f.Partial != "" || f.Detail != "" || !f.Started.IsZero() || !f.Completed.IsZero() {
			return r, fmt.Errorf("invalid reviewed file")
		}
	}
	return r, nil
}

func (m *manager) startDownload(raw, review, runID string) (Download, error) {
	r, err := decodeDownload(raw)
	if err != nil || digest(raw) != review || runID == "" {
		return Download{}, fmt.Errorf("invalid confirmed download request")
	}
	p := r.Plan
	m.mu.Lock()
	s := m.connections[p.Owner.Creation]
	closed := m.closed
	m.mu.Unlock()
	if closed || s == nil || p.Workspace != m.Workspace || p.Owner.Generation != m.Generation || p.Owner.Session != m.Session {
		return Download{}, fmt.Errorf("download owner changed; review again")
	}
	ctx, cancel := context.WithCancel(context.Background())
	work := &downloadWork{start: time.Now(), sample: time.Now(), cancel: cancel, done: make(chan struct{})}
	work.value = Download{ID: r.ID, RunID: runID, Plan: p, Files: slices.Clone(p.Files), State: "running", Started: time.Now().UTC()}
	for i := range work.value.Files {
		f := &work.value.Files[i]
		if p.Operation == "put" {
			f.Partial = path.Join(path.Dir(f.Destination), fmt.Sprintf(".burrow-partial-%s-%d", r.ID, i))
		} else {
			f.Partial = filepath.Join(filepath.Dir(f.Destination), fmt.Sprintf(".burrow-partial-%s-%d", r.ID, i))
		}
	}
	admission, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	if err := launch.VerifyReservation(admission, m.Workspace, m.dir); err != nil {
		cancel()
		return Download{}, err
	}
	root, err := p.openRoot(admission)
	if err != nil {
		cancel()
		return Download{}, err
	}
	root.Close()
	m.downloadMu.Lock()
	defer m.downloadMu.Unlock()
	if m.downloads == nil {
		m.downloads = map[string]*downloadWork{}
	}
	if m.downloads[r.ID] != nil {
		cancel()
		return Download{}, fmt.Errorf("download already submitted; inspect downloads")
	}
	for _, other := range m.downloads {
		d := other.snapshot()
		if d.State != "running" {
			continue
		}
		if (d.Plan.Operation == "put") != (p.Operation == "put") {
			continue
		}
		for _, f := range d.Files {
			for _, planned := range p.Files {
				if f.Destination == planned.Destination {
					cancel()
					return Download{}, fmt.Errorf("destination already has an active transfer")
				}
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.downloadClosing || s.state.State != "connected" || s.state.Name != p.Owner.Name {
		cancel()
		return Download{}, fmt.Errorf("selected connection no longer available")
	}
	if err := s.checkMaster(); err != nil {
		cancel()
		return Download{}, err
	}
	if err := recordDownload(admission, m.Workspace, work.value); err != nil {
		cancel()
		return Download{}, fmt.Errorf("cannot retain transfer outcome; nothing copied: %w", err)
	}
	m.downloads[r.ID] = work
	if s.downloads == nil {
		s.downloads = map[string]*downloadWork{}
	}
	s.downloads[r.ID] = work
	go func() {
		defer close(work.done)
		defer cancel()
		s.copyDownloads(ctx, work)
	}()
	return work.snapshot(), nil
}

func (p DownloadPlan) openRoot(ctx context.Context) (*os.Root, error) {
	roots, err := TransferRoots(ctx, p.Workspace)
	if err != nil {
		return nil, err
	}
	effective := roots.Download
	if p.Operation == "put" {
		effective = roots.Upload
	}
	if effective != p.Root {
		return nil, fmt.Errorf("transfer root changed after review; review again")
	}
	root, err := launch.FileRoot(p.Root, false)
	if err != nil {
		return nil, err
	}
	info, err := root.Stat(".")
	if err != nil || localIdentity(info, true) != p.RootIdentity {
		root.Close()
		return nil, fmt.Errorf("transfer root replaced after review")
	}
	return root, nil
}

func (s *owner) copyDownloads(ctx context.Context, work *downloadWork) {
	if work.value.Plan.Operation == "mget" {
		s.copyBatchDownloads(ctx, work)
	} else {
		for i := range work.value.Files {
			var err error
			if work.value.Plan.Operation == "put" {
				err = s.copyUpload(ctx, work, i)
			} else {
				err = s.copyDownload(ctx, work, i)
			}
			s.finishDownloadFile(ctx, work, i, err)
		}
	}
	s.finishDownloads(ctx, work)
}

func (s *owner) finishDownloadFile(ctx context.Context, work *downloadWork, i int, err error) {
	work.mu.Lock()
	f := &work.value.Files[i]
	if err != nil {
		f.State, f.Detail = "failed", err.Error()
		if ctx.Err() != nil {
			f.State, f.Detail = "cancelled", "copy cancelled; labelled partial retained if created"
		}
	} else {
		f.State = "complete"
		f.Partial = ""
	}
	f.Completed = time.Now().UTC()
	if !f.Started.IsZero() {
		f.Elapsed = f.Completed.Sub(f.Started).Seconds()
		if f.Elapsed > 0 {
			f.AverageRate = float64(f.Bytes) / f.Elapsed
		}
	}
	work.mu.Unlock()
	persist, stop := context.WithTimeout(context.Background(), 10*time.Second)
	err = recordDownloadFile(persist, s.config.Workspace, work.snapshot(), i)
	stop()
	if err != nil {
		work.mu.Lock()
		work.value.Detail = "outcome persistence failed; inspect working files: " + err.Error()
		work.mu.Unlock()
		work.cancel()
	}
}

func (s *owner) finishDownloads(ctx context.Context, work *downloadWork) {
	work.mu.Lock()
	work.value.State = "complete"
	complete := 0
	for _, f := range work.value.Files {
		if f.State == "complete" {
			complete++
			work.value.CompletedFiles++
		} else if f.State == "cancelled" {
			work.value.CancelledFiles++
		} else {
			work.value.FailedFiles++
		}
	}
	if complete != len(work.value.Files) {
		work.value.State = "failed"
		if ctx.Err() != nil {
			work.value.State = "cancelled"
		}
		if complete > 0 {
			work.value.State = "partial"
		}
	}
	work.value.Completed = time.Now().UTC()
	work.value.Elapsed = time.Since(work.start).Seconds()
	work.mu.Unlock()
	persist, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	if err := recordDownload(persist, s.config.Workspace, work.snapshot()); err != nil {
		work.mu.Lock()
		work.value.Detail = "completion persistence failed; files retained: " + err.Error()
		work.mu.Unlock()
	}
}

func (s *owner) copyDownload(ctx context.Context, work *downloadWork, index int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	work.mu.Lock()
	plan := work.value.Plan
	work.mu.Unlock()
	client, closeClient, err := openFileClient(ctx, plan.Owner)
	if err != nil {
		return err
	}
	defer closeClient()
	return s.copyDownloadWithClient(ctx, work, index, client)
}

func (s *owner) copyBatchDownloads(ctx context.Context, work *downloadWork) {
	work.mu.Lock()
	plan, count := work.value.Plan, len(work.value.Files)
	work.mu.Unlock()
	client, closeClient, err := openFileClient(ctx, plan.Owner)
	if err != nil {
		for i := range count {
			s.finishDownloadFile(ctx, work, i, err)
		}
		return
	}
	defer closeClient()
	type result struct {
		index int
		err   error
	}
	jobs, results := make(chan int, count), make(chan result, count)
	for range min(4, count) {
		go func() {
			for i := range jobs {
				results <- result{i, s.copyDownloadWithClient(ctx, work, i, client)}
			}
		}()
	}
	for i := range count {
		jobs <- i
	}
	close(jobs)
	for range count {
		result := <-results
		s.finishDownloadFile(ctx, work, result.index, result.err)
	}
}

func (s *owner) copyDownloadWithClient(ctx context.Context, work *downloadWork, index int, client *sftp.Client) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	work.mu.Lock()
	plan, f := work.value.Plan, work.value.Files[index]
	work.value.Files[index].State = "running"
	work.value.Files[index].Started = time.Now().UTC()
	work.mu.Unlock()
	root, err := plan.openRoot(ctx)
	if err != nil {
		return err
	}
	defer root.Close()
	canonical, err := client.RealPath(f.Source)
	if err != nil || canonical != f.Source {
		return fmt.Errorf("source changed or unavailable after review")
	}
	source, err := client.Open(f.Source)
	if err != nil {
		return fmt.Errorf("source cannot be opened: %w", err)
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() || (f.Size >= 0 && info.Size() != f.Size) || !info.ModTime().Equal(f.Modified) {
		return fmt.Errorf("source metadata changed after review; review again")
	}
	if err := root.MkdirAll(filepath.Dir(f.Relative), 0700); err != nil {
		return err
	}
	dir, err := root.OpenRoot(filepath.Dir(f.Relative))
	if err != nil {
		return err
	}
	defer dir.Close()
	name, partial := filepath.Base(f.Relative), filepath.Base(f.Partial)
	check := func() error {
		st, e := dir.Lstat(name)
		if f.Existing == "" && os.IsNotExist(e) {
			return nil
		}
		if e != nil || !st.Mode().IsRegular() || f.Existing == "" || localIdentity(st, false) != f.Existing {
			return fmt.Errorf("destination changed after review; original preserved")
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	destination, err := dir.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("cannot create labelled partial: %w", err)
	}
	defer destination.Close()
	buf := make([]byte, 128<<10)
	var copied int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := source.Read(buf)
		if n > 0 {
			if f.Size >= 0 && int64(n) > f.Size-copied {
				return fmt.Errorf("source exceeds reviewed size; labelled partial retained")
			}
			written, writeErr := destination.Write(buf[:n])
			copied += int64(written)
			work.mu.Lock()
			work.value.Files[index].Bytes += int64(written)
			work.mu.Unlock()
			if writeErr != nil {
				return fmt.Errorf("partial write failed: %w", writeErr)
			}
			if written != n {
				return io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("copy failed; labelled partial retained: %w", readErr)
		}
	}
	info, err = source.Stat()
	if err != nil || (f.Size >= 0 && (info.Size() != copied || copied != f.Size)) || !info.ModTime().Equal(f.Modified) {
		return fmt.Errorf("source changed during copy; labelled partial retained")
	}
	if err := destination.Sync(); err != nil {
		return err
	}
	if err := destination.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	verified, err := plan.openRoot(ctx)
	if err != nil {
		return err
	}
	defer verified.Close()
	current, err := verified.Stat(filepath.Dir(f.Relative))
	held, heldErr := dir.Stat(".")
	if err != nil || heldErr != nil || !os.SameFile(current, held) {
		return fmt.Errorf("destination directory changed during copy; partial retained")
	}
	if err := check(); err != nil {
		return err
	}
	// Root operations cannot follow a substituted directory outside the root.
	// Link provides atomic no-replace publication when no overwrite was approved.
	if f.Existing == "" {
		if err = dir.Link(partial, name); err == nil {
			err = dir.Remove(partial)
		}
	} else {
		err = dir.Rename(partial, name)
	}
	if err != nil {
		return fmt.Errorf("publication failed; inspect destination and labelled partial: %w", err)
	}
	folder, err := dir.Open(".")
	if err != nil {
		return fmt.Errorf("published but directory durability unverified: %w", err)
	}
	defer folder.Close()
	if err := folder.Sync(); err != nil {
		return fmt.Errorf("published but directory durability unverified: %w", err)
	}
	return nil
}

func (s *owner) copyUpload(ctx context.Context, work *downloadWork, index int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	work.mu.Lock()
	plan, f := work.value.Plan, work.value.Files[index]
	work.value.Files[index].State = "running"
	work.value.Files[index].Started = time.Now().UTC()
	work.mu.Unlock()
	root, err := plan.openRoot(ctx)
	if err != nil {
		return err
	}
	defer root.Close()
	source, err := root.Open(f.Relative)
	if err != nil {
		return fmt.Errorf("upload source cannot be opened inside %s: %w", plan.Root, err)
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() || localIdentity(info, false) != f.SourceIdentity {
		return fmt.Errorf("upload source changed after review; review again")
	}
	client, closeClient, err := openFileClient(ctx, plan.Owner)
	if err != nil {
		return err
	}
	defer closeClient()
	checkDirectory := func() error {
		directory := path.Dir(f.Destination)
		resolved, err := client.RealPath(directory)
		if err != nil || resolved != directory {
			return fmt.Errorf("remote destination directory changed after review; original preserved")
		}
		return nil
	}
	check := func() error {
		st, e := client.Lstat(f.Destination)
		if f.Existing == "" && os.IsNotExist(e) {
			return nil
		}
		if e != nil || !st.Mode().IsRegular() || f.Existing == "" || remoteIdentity(st) != f.Existing {
			return fmt.Errorf("remote destination changed after review; original preserved")
		}
		return nil
	}
	if err := checkDirectory(); err != nil {
		return err
	}
	if err := check(); err != nil {
		return err
	}
	destination, err := client.OpenFile(f.Partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return fmt.Errorf("cannot create labelled remote partial: %w", err)
	}
	defer destination.Close()
	if err := destination.Chmod(0600); err != nil {
		return fmt.Errorf("cannot protect labelled remote partial: %w", err)
	}
	buf := make([]byte, 128<<10)
	var copied int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := source.Read(buf)
		if n > 0 {
			if int64(n) > f.Size-copied {
				return fmt.Errorf("upload source exceeds reviewed size; labelled partial retained")
			}
			written, writeErr := destination.Write(buf[:n])
			copied += int64(written)
			work.mu.Lock()
			work.value.Files[index].Bytes += int64(written)
			work.mu.Unlock()
			if writeErr != nil {
				return fmt.Errorf("remote partial write failed: %w", writeErr)
			}
			if written != n {
				return io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("upload failed; labelled partial retained: %w", readErr)
		}
	}
	info, err = source.Stat()
	if err != nil || localIdentity(info, false) != f.SourceIdentity || copied != f.Size {
		return fmt.Errorf("upload source changed during copy; labelled partial retained")
	}
	if err := destination.Sync(); err != nil {
		return fmt.Errorf("remote partial durability unverified: %w", err)
	}
	if err := destination.Close(); err != nil {
		return err
	}
	partial, err := client.Stat(f.Partial)
	if err != nil || !partial.Mode().IsRegular() || partial.Size() != copied {
		return fmt.Errorf("uploaded bytes could not be verified; labelled partial retained")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkDirectory(); err != nil {
		return err
	}
	if err := check(); err != nil {
		return err
	}
	if f.Existing == "" {
		if err = client.Link(f.Partial, f.Destination); err == nil {
			err = client.Remove(f.Partial)
		}
	} else {
		err = client.PosixRename(f.Partial, f.Destination)
	}
	if err != nil {
		return fmt.Errorf("remote publication failed; inspect destination and labelled partial: %w", err)
	}
	return nil
}

func downloadArgs(args []string) (remote, local, review string, yes bool, err error) {
	if len(args) < 4 {
		err = fmt.Errorf("expected scp NAME get REMOTE [LOCAL], mget PATTERN [LOCAL_DIR], or put LOCAL [REMOTE]")
		return
	}
	var positional []string
	for i := 3; i < len(args); i++ {
		switch args[i] {
		case "--yes":
			yes = true
		case "--review":
			i++
			if i == len(args) || review != "" {
				err = fmt.Errorf("exact review digest required")
				return
			}
			review = args[i]
		default:
			positional = append(positional, args[i])
		}
	}
	if len(positional) < 1 || len(positional) > 2 {
		err = fmt.Errorf("expected source and optional destination")
		return
	}
	remote = positional[0]
	if len(positional) > 1 {
		local = positional[1]
	}
	if remote == "" || strings.ContainsRune(remote+local, 0) || len(remote) > 4096 || len(local) > 4096 {
		err = fmt.Errorf("invalid transfer path")
	}
	return
}

func executeDownload(ctx context.Context, w string, args []string) (any, error) {
	remote, local, review, yes, err := downloadArgs(args)
	if err != nil {
		return nil, err
	}
	s, err := selected(ctx, w, args[1])
	if err != nil {
		return nil, err
	}
	p, err := ReviewDownloads(ctx, w, s, args[2], remote, local)
	if err != nil {
		return nil, err
	}
	if !yes {
		return p, nil
	}
	if review == "" || p.Digest != review {
		return nil, fmt.Errorf("explicit approval requires --review DIGEST from the unchanged file/destination recap")
	}
	return StartDownloads(ctx, p)
}

func executeDownloads(ctx context.Context, w string, args []string) (any, error) {
	history, err := DownloadHistory(ctx, w)
	if err != nil {
		return nil, err
	}
	if len(args) == 1 {
		if args[0] == "downloads" {
			history.Records = slices.DeleteFunc(history.Records, func(d Download) bool { return d.Plan.Operation == "put" })
		}
		return history, nil
	}
	for _, d := range history.Records {
		if d.ID == args[1] {
			if args[0] == "downloads" && d.Plan.Operation == "put" {
				return nil, fmt.Errorf("upload ID; use transfers")
			}
			if args[0] == "downloads" || args[0] == "transfers" {
				return d, nil
			}
			var out struct {
				Download
				Error string `json:"error"`
			}
			err := managerControl(ctx, w, managerIdentity{Session: d.Plan.Owner.Session, Generation: d.Plan.Owner.Generation}, "download-cancel", []string{d.ID}, &out)
			if err == nil && out.Error != "" {
				err = fmt.Errorf("%s", out.Error)
			}
			return out.Download, err
		}
	}
	return nil, fmt.Errorf("transfer ID not found")
}
