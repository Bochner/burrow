// Disposable direct-OpenSSH proof. No installed or Python remote supervisor.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vibepwners/hovel/sdk/go/hovel"
)

type directRequest struct {
	OutputFormat   string   `json:"outputFormat"`
	Keep           bool     `json:"keep"`
	CleanupFailure bool     `json:"cleanupFailure"`
	Mode           string   `json:"mode"`
	Interpreter    string   `json:"interpreter"`
	Path           string   `json:"path"`
	Args           []string `json:"args"`
	ScriptBase64   string   `json:"scriptBase64"`
	StdinBase64    string   `json:"stdinBase64"`
	Limit          int64    `json:"limit"`
	FailWrite      bool     `json:"failWrite"`
	TimeoutMillis  int      `json:"timeoutMillis"`
}

type directRun struct {
	markdown                     bool
	staged, keep, cleanupFailure bool
	source                       []byte
	stage                        string
	dir                          string
	stdout, stderr               *os.File
	limit                        int64
	failWrite                    bool
	timeout                      time.Duration
	mu                           sync.Mutex
	bytes                        [2]int64
	preview                      [2]cappedOutput
	outputError                  string
	cancelled                    bool
	timedOut                     bool
}

func (p *ownership) prepareDirectScript(ctx *hovel.Context) (hovel.Result, error) {
	root := os.Getenv("BURROW_OWNER_ROOT")
	data, err := os.ReadFile(filepath.Join(root, "invocation.json"))
	if err != nil {
		return hovel.Result{}, err
	}
	var req directRequest
	if len(data) > 65536 || json.Unmarshal(data, &req) != nil {
		return hovel.Result{}, fmt.Errorf("invalid bounded fixture request")
	}
	if req.OutputFormat != "" && req.OutputFormat != "markdown" {
		return hovel.Result{}, fmt.Errorf("unsupported output format")
	}
	if req.Limit == 0 {
		req.Limit = 8 * 1024 * 1024
	}
	if req.Limit < 1 || req.Limit > 64*1024*1024 || req.TimeoutMillis < 0 || req.TimeoutMillis > 30000 {
		return hovel.Result{}, fmt.Errorf("invalid fixture output budget or timeout")
	}
	source, err := base64.StdEncoding.DecodeString(req.ScriptBase64)
	if err != nil {
		return hovel.Result{}, err
	}
	input, err := base64.StdEncoding.DecodeString(req.StdinBase64)
	if err != nil {
		return hovel.Result{}, err
	}
	for _, arg := range append(append([]string{req.Interpreter, req.Path}, req.Args...), string(source)) {
		if strings.ContainsRune(arg, 0) {
			return hovel.Result{}, fmt.Errorf("NUL in executable input")
		}
	}
	var argv []string
	switch req.Mode {
	case "command":
		argv = req.Args
	case "staged":
		argv = append([]string{req.Interpreter, "@stage@"}, req.Args...)
	case "existing", "local":
		argv = append([]string{req.Interpreter, req.Path}, req.Args...)
	case "streamed":
		if len(input) != 0 {
			return hovel.Result{}, fmt.Errorf("sh -s owns stdin; select inline or explicit staging for separate stdin")
		}
		argv = append([]string{req.Interpreter, "-s", "--"}, req.Args...)
		input = source
	case "inline":
		// Explicit shell -c semantics: source is an SSH exec argument, not a file.
		argv = append([]string{req.Interpreter, "-c", string(source), "burrow-script"}, req.Args...)
	default:
		return hovel.Result{}, fmt.Errorf("unsupported direct fixture mode")
	}
	if len(argv) == 0 || !filepath.IsAbs(argv[0]) {
		return hovel.Result{}, fmt.Errorf("select an absolute executable")
	}
	var cmd *exec.Cmd
	if req.Mode == "local" {
		cmd = exec.Command(argv[0], argv[1:]...)
	} else {
		name := ctx.InputString("proof_connection", "gateway")
		if name != "gateway" && name != "second" {
			return hovel.Result{}, fmt.Errorf("invalid fixture connection")
		}
		quoted := make([]string, len(argv))
		for i, arg := range argv {
			quoted[i] = shellQuote(arg)
		}
		cmd = exec.Command("/usr/bin/ssh", "-F", os.Getenv("BURROW_OWNER_CONFIG"), "-S", filepath.Join(root, name, "master"), "-o", "ProxyCommand=/bin/false", "-T", "target", "exec "+strings.Join(quoted, " "))
	}
	cmd.Stdin = bytes.NewReader(input)
	dir, err := os.MkdirTemp(root, "direct-output-")
	if err != nil {
		return hovel.Result{}, err
	}
	d := &directRun{markdown: req.OutputFormat == "markdown", staged: req.Mode == "staged", keep: req.Keep, cleanupFailure: req.CleanupFailure, source: source, dir: dir, limit: req.Limit, failWrite: req.FailWrite, timeout: time.Duration(req.TimeoutMillis) * time.Millisecond}
	d.stdout, err = os.OpenFile(filepath.Join(dir, "stdout"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err == nil {
		d.stderr, err = os.OpenFile(filepath.Join(dir, "stderr"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	}
	if err != nil {
		if d.stdout != nil {
			d.stdout.Close()
		}
		os.RemoveAll(dir)
		return hovel.Result{}, err
	}
	s := &retainedScript{deferred: true, process: cmd, done: make(chan struct{}), direct: d, report: map[string]any{"state": "prepared", "execution": req.Mode, "cleanup": "not-needed"}}
	p.script = s
	if _, err = ctx.OpenSession(s, hovel.WithName("direct script run"), hovel.WithKind("script-run"), hovel.WithTransport("ssh")); err != nil {
		s.Close("prepare failed")
		return hovel.Result{}, err
	}
	return hovel.Ok(nil, hovel.WithSummary("Direct execution prepared; no remote code has run")), nil
}

type directOutput struct {
	d      *directRun
	stream int
}

func (w directOutput) Write(b []byte) (int, error) {
	d := w.d
	d.mu.Lock()
	defer d.mu.Unlock()
	d.preview[w.stream].Write(b)
	d.bytes[w.stream] += int64(len(b))
	f := d.stdout
	if w.stream == 1 {
		f = d.stderr
	}
	if d.outputError == "" {
		if d.bytes[w.stream] > d.limit {
			d.outputError = "output storage budget exceeded"
		} else {
			if d.failWrite {
				f.Close()
				d.failWrite = false
			} // Actual failed file write, injected by the harness.
			if _, err := f.Write(b); err != nil {
				d.outputError = "output file write failed"
			}
		}
	}
	// Continue draining: an output error must never masquerade as complete output.
	return len(b), nil
}

func (d *directRun) start(s *retainedScript) error {
	if d.staged {
		stageCtx, stageCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stageCancel()
		setup := exec.CommandContext(stageCtx, s.process.Path, append(append([]string{}, s.process.Args[1:len(s.process.Args)-1]...), `umask 077; d=$(mktemp -d /tmp/burrow-script.XXXXXXXX) || exit 1; if cat > "$d/script"; then printf '%s\n' "$d/script"; else rm -f "$d/script"; rmdir "$d"; exit 1; fi`)...)
		setup.Stdin = bytes.NewReader(d.source)
		var path cappedOutput
		setup.Stdout = &path
		err := setup.Run()
		if err != nil || path.discarded != 0 {
			return fmt.Errorf("staging outcome unconfirmed")
		}
		d.stage = strings.TrimSpace(path.data.String())
		if !regexp.MustCompile(`^/tmp/burrow-script\.[A-Za-z0-9]{8}/script$`).MatchString(d.stage) {
			return fmt.Errorf("invalid staged path")
		}
		last := len(s.process.Args) - 1
		s.process.Args[last] = strings.Replace(s.process.Args[last], shellQuote("@stage@"), shellQuote(d.stage), 1)
	}

	s.process.Stdout = directOutput{d, 0}
	s.process.Stderr = directOutput{d, 1}
	if err := s.process.Start(); err != nil {
		return err
	}
	s.mu.Lock()
	s.report["state"] = "running"
	s.mu.Unlock()
	go func() {
		defer close(s.done)
		stop := make(chan struct{})
		go func() {
			timer := time.NewTimer(30 * time.Second)
			if d.timeout > 0 {
				timer.Reset(d.timeout)
			}
			defer timer.Stop()
			select {
			case <-stop:
				return
			case <-timer.C:
				d.mu.Lock()
				d.timedOut = true
				d.mu.Unlock()
				s.process.Process.Kill()
			}
		}()
		err := s.process.Wait()
		close(stop)
		d.mu.Lock()
		for _, f := range []*os.File{d.stdout, d.stderr} {
			if syncErr := f.Sync(); syncErr != nil && d.outputError == "" {
				d.outputError = "output file sync failed"
			}
		}
		cancelled, timedOut, outputError := d.cancelled, d.timedOut, d.outputError
		d.mu.Unlock()
		state := "remote-exit"
		exitKey := "remoteExit"
		s.mu.Lock()
		if s.report["execution"] == "local" {
			state = "local-exit"
			exitKey = "localExit"
		}
		code := s.process.ProcessState.ExitCode()
		if err != nil && (code < 0 || (exitKey == "remoteExit" && code == 255)) {
			state = "transport-or-completion-unknown"
		}
		if cancelled || timedOut {
			state = "local-client-stopped-remote-unconfirmed"
		}
		if outputError != "" {
			state = "output-incomplete"
		}
		s.report["state"] = state
		if state == "remote-exit" || state == "local-exit" {
			s.report[exitKey] = code
		}
		s.report["cancelRequested"] = cancelled
		s.report["timedOut"] = timedOut
		if d.staged {
			s.report["stage"] = d.stage
			s.report["cleanup"] = "unconfirmed"
			if code >= 0 && code != 255 && !cancelled && !timedOut {
				if d.keep {
					s.report["cleanup"] = "kept"
				} else {
					cleanup := "rm -- " + shellQuote(d.stage) + " && rmdir -- " + shellQuote(filepath.Dir(d.stage))
					if d.cleanupFailure {
						cleanup = "printf preserve > " + shellQuote(filepath.Join(filepath.Dir(d.stage), "unrelated")) + "; " + cleanup
					}
					cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					command := exec.CommandContext(cleanupCtx, s.process.Path, append(append([]string{}, s.process.Args[1:len(s.process.Args)-1]...), cleanup)...)
					if err := command.Run(); err == nil {
						s.report["cleanup"] = "removed"
					} else {
						s.report["cleanup"] = "failed"
					}
					cancel()
				}
			}
		}
		s.mu.Unlock()
	}()
	return nil
}

func (d *directRun) decorate(report map[string]any) map[string]any {
	d.mu.Lock()
	defer d.mu.Unlock()
	copy := make(map[string]any, len(report)+8)
	for k, v := range report {
		copy[k] = v
	}
	for i, name := range []string{"stdout", "stderr"} {
		copy[name+"Base64"] = base64.StdEncoding.EncodeToString(d.preview[i].data.Bytes())
		copy[name+"Bytes"] = d.bytes[i]
		copy[name+"PreviewDiscarded"] = d.preview[i].discarded
	}
	copy["outputComplete"] = d.outputError == "" && (report["state"] == "remote-exit" || report["state"] == "local-exit")
	copy["outputError"] = d.outputError
	return copy
}

// File paths come from the retained local owner, never from remote script output.
func directArtifacts(result hovel.PayloadCommandResult) ([]hovel.Artifact, error) {
	artifacts := []hovel.Artifact{hovel.JSONArtifact("direct-script-result", result)}
	for _, name := range []string{"stdout", "stderr"} {
		path := result.Fields[name+"Path"]
		root := os.Getenv("BURROW_OWNER_ROOT")
		rel, err := filepath.Rel(root, path)
		if err != nil || strings.HasPrefix(rel, "..") || !strings.HasPrefix(filepath.Base(filepath.Dir(path)), "direct-output-") || filepath.Base(path) != name {
			return nil, fmt.Errorf("invalid local output path")
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || resolved != path {
			return nil, fmt.Errorf("unsafe local output path")
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			return nil, fmt.Errorf("unsafe local output file")
		}
		artifactName, kind := "script-"+name, "application/octet-stream"
		if name == "stdout" && result.Fields["outputFormat"] == "markdown" {
			artifactName, kind = "script-report.md", "text/markdown"
		}
		artifacts = append(artifacts, hovel.FileArtifact(artifactName, kind, path))
	}
	return artifacts, nil
}

// Explicit, bounded fixture-only group signal: the target command writes its
// own PID/starttime to the harness; no production PID registry is introduced.
// ponytail: identity check and signal are not atomic; keep fixture-only until stronger remote identity control is proven.
func signalFixtureGroup(config, socket string, pid int, start string) error {
	if pid <= 1 {
		return fmt.Errorf("invalid fixture process")
	}
	if _, err := strconv.ParseUint(start, 10, 64); err != nil {
		return err
	}
	command := fmt.Sprintf(`IFS= read -r stat < /proc/%d/stat || exit 3
set -- ${stat##*) }
[ "$3" = %d ] || exit 4
shift 19
[ "$1" = %s ] || exit 5
kill -TERM -%d`, pid, pid, shellQuote(start), pid)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "/usr/bin/ssh", "-F", config, "-S", socket, "-o", "ProxyCommand=/bin/false", "-T", "target", "exec /bin/sh -c "+shellQuote(command)).Run()
}

var _ io.Writer = directOutput{}

func (d *directRun) readOutput(req hovel.PayloadCommandRequest, closed bool) (hovel.PayloadCommandResult, error) {
	if closed || len(req.Args) != 2 || (req.Args[0] != "stdout" && req.Args[0] != "stderr") || req.Reconnect != nil || req.InstalledPayloadID != "" || req.InputPath != "" || req.InputData != "" || len(req.Config) != 0 {
		return hovel.PayloadCommandResult{}, fmt.Errorf("invalid output read")
	}
	offset, err := strconv.ParseInt(req.Args[1], 10, 64)
	if err != nil || offset < 0 {
		return hovel.PayloadCommandResult{}, fmt.Errorf("invalid output offset")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	file, err := os.Open(filepath.Join(d.dir, req.Args[0]))
	if err != nil {
		return hovel.PayloadCommandResult{}, err
	}
	defer file.Close()
	data := make([]byte, 32768)
	n, err := file.ReadAt(data, offset)
	if err != nil && err != io.EOF {
		return hovel.PayloadCommandResult{}, err
	}
	return hovel.PayloadCommandResult{Command: req.Command, Stdout: base64.StdEncoding.EncodeToString(data[:n]), Fields: map[string]string{"nextOffset": strconv.FormatInt(offset+int64(n), 10)}}, nil
}
