package connection

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Bochner/burrow/core/launch"
)

// RunFile describes a private preparation snapshot, never its contents.
type RunFile struct {
	Path   string `json:"path,omitempty"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256,omitempty"`
}

type RunInput struct {
	Survey      string  `json:"survey,omitempty"`
	Mode        string  `json:"mode,omitempty"`
	Interpreter string  `json:"interpreter,omitempty"`
	Script      RunFile `json:"script"`
	Stdin       RunFile `json:"stdin"`
	StagePath   string  `json:"stagePath,omitempty"`
	Keep        bool    `json:"keep"`
	Timeout     string  `json:"timeout,omitempty"`
}

func (s Run) request() runRequest {
	return runRequest{Connection: s.Connection, Command: s.Command, Execution: s.Execution, Budget: s.Budget, Input: s.Input}
}

func (in RunInput) validate() error {
	if in.Survey != "" && (in.Survey != "ubuntu" || in.Mode != "stream" || in.Interpreter != "/bin/sh" || in.Script.Path != "builtin:survey/ubuntu" || in.Stdin.Path != "" || in.Keep) {
		return fmt.Errorf("invalid Ubuntu survey input")
	}
	if in.Timeout != "" {
		d, err := time.ParseDuration(in.Timeout)
		if err != nil || d <= 0 {
			return fmt.Errorf("timeout must be a positive duration, such as 30s or 5m")
		}
	}
	if in.Keep && (in.Script.Path == "" || in.Mode != "stage") {
		return fmt.Errorf("--keep requires an explicitly staged script")
	}
	for _, p := range []string{in.Script.Path, in.Stdin.Path, in.Interpreter} {
		if len(p) > 4096 || strings.ContainsRune(p, 0) {
			return fmt.Errorf("invalid script or stdin path")
		}
	}
	if in.Script.Path == "" {
		if in.Mode != "" || in.Interpreter != "" {
			return fmt.Errorf("script mode and interpreter require --script PATH")
		}
		return nil
	}
	if in.Mode != "stream" && in.Mode != "inline" && in.Mode != "stage" {
		return fmt.Errorf("select explicit --mode stream|inline|stage")
	}
	if in.Mode == "stream" && in.Stdin.Path != "" {
		return fmt.Errorf("stream mode owns stdin; select inline or explicit stage for independent stdin")
	}
	if !filepath.IsAbs(in.Interpreter) || len(in.Interpreter) > 4096 || strings.ContainsRune(in.Interpreter, 0) {
		return fmt.Errorf("script requires an absolute --interpreter PATH")
	}
	return nil
}

func (in RunInput) review() string {
	var text string
	if in.Survey != "" {
		text = "\nSurvey: Ubuntu v1 · read-only probes, five-second bound per check, no sudo or installation.\nReport: save Markdown and execution/capture metadata through Hovel; open with reports.\nChecks: date, /etc/os-release, hostname, uname, id, /proc/uptime, /proc/loadavg, /proc/meminfo, df, ip addresses/routes, ss listeners, systemctl failed services.\nMissing tools and failed checks stay visible; other Linux userlands are unverified."
	}
	if in.Script.Path != "" {
		text += fmt.Sprintf("\nScript: %s\nMode: %s\nInterpreter: %s\nScript snapshot: %d bytes; SHA256 %s", in.Script.Path, in.Mode, in.Interpreter, in.Script.Bytes, in.Script.SHA256)
		if in.Mode == "stream" {
			text += "\nStandard input carries script source; no remote script file is created."
		}
		if in.Mode == "inline" {
			text += "\nInline source is visible in process arguments; NO SECRETS. Quoted invocation must fit 64 KiB. No remote script file is created."
		}
		if in.Mode == "stage" {
			text += "\nStaged script: " + in.StagePath + "\nCleanup: remove only the owned script and its empty private directory. Staging starts after confirmation."
		}
		if in.Keep {
			text += "\nKeep: retain staged files instead of removing them, including after failure or cancellation."
		}
	}
	if in.Stdin.Path != "" {
		text += fmt.Sprintf("\nProgram stdin: %s\nInput snapshot: %d bytes; SHA256 %s; contents are not recorded in requests.", in.Stdin.Path, in.Stdin.Bytes, in.Stdin.SHA256)
	}
	if in.Timeout != "" {
		text += "\nExecution timeout: " + in.Timeout + "; requests ordinary-group cancellation, with unconfirmed termination reported."
	}
	return text
}

func (r *retainedRun) startTimeout() {
	duration, _ := time.ParseDuration(r.record.Input.Timeout)
	if duration == 0 {
		return
	}
	go func() {
		timer := time.NewTimer(duration)
		defer timer.Stop()
		select {
		case <-r.done:
			return
		case <-timer.C:
		}
		r.control.Lock()
		defer r.control.Unlock()
		r.mu.Lock()
		if r.record.State != "running" {
			r.mu.Unlock()
			return
		}
		r.record.TimedOut = true
		before := r.record
		r.mu.Unlock()
		audit, beginErr := launch.BeginAudit(r.workspace, "run timeout", targetLabel(before.Connection), before)
		r.cancel()
		r.mu.Lock()
		defer r.mu.Unlock()
		if err := audit.Finish(r.record, beginErr); err != nil {
			r.record.AuditError = err.Error()
		}
	}()
}

//go:embed survey_ubuntu.sh
var ubuntuSurvey string

func (r *retainedRun) prepareInputs(ctx context.Context) error {
	if r.record.Input.Survey == "ubuntu" {
		f, err := r.root.OpenFile("script", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		r.inputs[0] = f
		if _, err := io.WriteString(f, ubuntuSurvey); err != nil {
			return err
		}
		r.record.Input.Script.Bytes = int64(len(ubuntuSurvey))
		r.record.Input.Script.SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(ubuntuSurvey)))
		return f.Sync()
	}
	if r.record.Input.Script.Path == "" && r.record.Input.Stdin.Path == "" {
		return nil
	}
	roots, err := TransferRoots(ctx, r.workspace)
	if err != nil {
		return err
	}
	root, err := launch.FileRoot(roots.Upload, false)
	if err != nil {
		return err
	}
	defer root.Close()
	if r.record.Input.Mode == "stage" {
		r.record.Input.StagePath = "/tmp/burrow-script." + rand.Text() + "/script"
		r.record.Staging, r.record.StageCleanup = "not-started", "not-created"
	}
	for i, meta := range []*RunFile{&r.record.Input.Script, &r.record.Input.Stdin} {
		if meta.Path == "" {
			continue
		}
		if err := r.snapshotInput(root, roots.Upload, i, meta); err != nil {
			return err
		}
	}
	_, _, err = r.invocation()
	return err
}

func (r *retainedRun) snapshotInput(root *os.Root, base string, i int, meta *RunFile) error {
	name := []string{"script", "stdin"}[i]
	rel, err := containedLocal(base, meta.Path)
	if err != nil {
		return err
	}
	source, err := root.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("%s source refused: %w", name, err)
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%s source must be a regular file", name)
	}
	// ponytail: 256 MiB per input snapshot; add configurable input quotas if larger inputs are needed.
	if info.Size() > 256<<20 {
		return fmt.Errorf("%s exceeds the 256 MiB input snapshot limit", name)
	}
	if i == 0 && r.record.Input.Mode == "inline" && info.Size() > 64<<10 {
		return fmt.Errorf("inline source exceeds 64 KiB; select stream or explicit stage")
	}
	r.inputs[i], err = r.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	if _, err := io.CopyN(io.MultiWriter(r.inputs[i], hash), source, info.Size()); err != nil {
		return fmt.Errorf("%s snapshot incomplete: %w", name, err)
	}
	after, err := source.Stat()
	if err != nil || localIdentity(info, false) != localIdentity(after, false) {
		return fmt.Errorf("source changed during preparation; prepare again")
	}
	if err := r.inputs[i].Sync(); err != nil {
		return err
	}
	*meta = RunFile{Path: filepath.Join(base, rel), Bytes: info.Size(), SHA256: fmt.Sprintf("%x", hash.Sum(nil))}
	return nil
}

// The retained file descriptor supplies the reviewed bytes, independently of
// the caller's lifetime and subsequent changes to the source path.
func (r *retainedRun) invocation() ([]string, io.Reader, error) {
	in := r.record.Input
	for i, meta := range []RunFile{in.Script, in.Stdin} {
		if meta.Path == "" {
			continue
		}
		hash := sha256.New()
		if _, err := io.Copy(hash, io.NewSectionReader(r.inputs[i], 0, meta.Bytes)); err != nil {
			return nil, nil, err
		}
		if fmt.Sprintf("%x", hash.Sum(nil)) != meta.SHA256 {
			return nil, nil, fmt.Errorf("prepared input changed; launch refused")
		}
	}
	argv := r.record.Command
	var input io.Reader
	if in.Stdin.Path != "" {
		input = io.NewSectionReader(r.inputs[1], 0, in.Stdin.Bytes)
	}
	switch in.Mode {
	case "stage":
		argv = append([]string{in.Interpreter, in.StagePath}, argv...)
	case "stream":
		argv = append([]string{in.Interpreter, "-s", "--"}, argv...)
		input = io.NewSectionReader(r.inputs[0], 0, in.Script.Bytes)
	case "inline":
		source, err := io.ReadAll(io.NewSectionReader(r.inputs[0], 0, in.Script.Bytes))
		if err != nil {
			return nil, nil, err
		}
		if strings.ContainsRune(string(source), 0) {
			return nil, nil, fmt.Errorf("inline source contains NUL; select explicit stage")
		}
		argv = append([]string{in.Interpreter, "-c", string(source), "burrow-script"}, argv...)
	}
	if len(quoteCommand([]string{quoteCommand(argv)})) > 64<<10 {
		return nil, nil, fmt.Errorf("quoted invocation exceeds 64 KiB; shorten arguments or select stream/explicit stage")
	}
	return argv, input, nil
}

func stageIdentity(value, mode string) bool {
	parts := strings.Split(value, ":")
	if len(parts) != 4 || parts[3] != mode {
		return false
	}
	for _, part := range parts[:3] {
		if _, err := strconv.ParseUint(part, 10, 64); err != nil {
			return false
		}
	}
	return true
}

func runSuggestions(line string, args []string) []string {
	if len(args) < 3 || (args[1] != "prepare" && args[1] != "now") || slices.Contains(args[3:], "--") {
		return nil
	}
	if strings.HasSuffix(line, " ") {
		args = append(args, "")
	}
	if len(args) < 4 {
		return nil
	}
	last := args[len(args)-1]
	prefix := line[:strings.LastIndexByte(line, ' ')+1]
	var choices []string
	switch args[len(args)-2] {
	case "--mode":
		choices = []string{"stream ", "inline "}
		if !slices.Contains(args, "--local") {
			choices = append(choices, "stage ")
		}
	case "--interpreter":
		choices = []string{"/bin/sh ", "/bin/bash "}
	case "--script", "--stdin", "--timeout", "--budget":
		return nil
	default:
		if last != "" && !strings.HasPrefix(last, "-") {
			return nil
		}
		choices = []string{"--local ", "--script ", "--mode ", "--interpreter ", "--stdin ", "--timeout ", "--budget ", "-- "}
		if slices.Contains(args, "stage") {
			choices = append(choices, "--keep ")
		}
		if args[1] == "now" {
			choices = append(choices, "--yes ")
		}
		choices = slices.DeleteFunc(choices, func(s string) bool { return slices.Contains(args[3:len(args)-1], strings.TrimSpace(s)) })
	}
	var out []string
	for _, choice := range choices {
		if strings.HasPrefix(choice, last) {
			out = append(out, prefix+choice)
		}
	}
	return out
}

// Never reuse a colliding directory. Acknowledged inode/owner identities bind
// later cleanup to files created by this run, including partial uploads.
func (r *retainedRun) stage() error {
	dir := filepath.Dir(r.record.Input.StagePath)
	script := "umask 077\nmkdir -m 700 -- " + quoteCommand([]string{dir}) + " || exit 20\ncd -P -- " + quoteCommand([]string{dir}) + ` || exit 21
d=$(stat -c '%d:%i:%u:%a' .) || exit 21
printf 'directory %s\n' "$d"
(set -C; : > script) || exit 22
f=$(stat -c '%d:%i:%u:%a' script) || exit 23
printf 'file %s\n' "$f"
cat > script || exit 24
printf 'ready\n'`
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := fileSSH(ctx, r.record.Connection, "unused", "exec /bin/sh -c "+quoteCommand([]string{script}))
	cmd.Stdin = io.NewSectionReader(r.inputs[0], 0, r.record.Input.Script.Bytes)
	var out limitedFileOutput
	cmd.Stdout = &out
	r.record.Staging, r.record.StageCleanup = "unconfirmed", "unconfirmed"
	err := cmd.Run()
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) > 0 {
		value := strings.TrimPrefix(lines[0], "directory ")
		if stageIdentity(value, "700") {
			r.stageDirectory = value
		}
	}
	if r.stageDirectory != "" && len(lines) > 1 {
		value := strings.TrimPrefix(lines[1], "file ")
		if stageIdentity(value, "600") {
			r.stageFile = value
		}
	}
	if err == nil && !out.full && len(lines) == 3 && lines[2] == "ready" && r.stageDirectory != "" && r.stageFile != "" {
		r.record.Staging, r.record.StageCleanup = "ready", "pending"
		return nil
	}
	if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() >= 0 && cmd.ProcessState.ExitCode() != 255 {
		r.record.Staging = "failed"
		if cmd.ProcessState.ExitCode() == 20 && out.Len() == 0 {
			r.record.StageCleanup = "not-created; directory creation refused"
		}
	}
	return fmt.Errorf("script staging failed or was not acknowledged; script not launched; inspect staging and stageCleanup")
}

// The caller holds mu. Removal never follows replacement links or recursively
// deletes a directory. Unknown identities remain visible for manual recovery.
func (r *retainedRun) cleanupStage() {
	if r.record.Input.Mode != "stage" || r.stageDirectory == "" {
		return
	}
	if r.record.Input.Keep {
		r.record.StageCleanup = "kept"
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	s := r.record.Connection
	live, err := selected(ctx, r.workspace, s.Name)
	if err != nil || live.State != "connected" || live.Session != s.Session || live.Generation != s.Generation || live.Creation != s.Creation || live.Socket != s.Socket || live.SocketInode != s.SocketInode || live.MasterPID != s.MasterPID {
		r.record.StageCleanup = "unconfirmed; selected master unavailable"
		return
	}
	dir := filepath.Dir(r.record.Input.StagePath)
	script := "d=" + quoteCommand([]string{dir}) + "\nexpected=" + quoteCommand([]string{r.stageDirectory}) + `
if [ ! -e "$d" ] && [ ! -L "$d" ]; then printf 'removed'; exit 0; fi
[ ! -L "$d" ] && [ "$(stat -c '%d:%i:%u:%a' "$d")" = "$expected" ] || exit 30
cd -P -- "$d" || exit 31
[ "$(stat -c '%d:%i:%u:%a' .)" = "$expected" ] || exit 32
`
	if r.stageFile != "" {
		script += "expected=" + quoteCommand([]string{r.stageFile}) + `
if [ -e script ] || [ -L script ]; then
 [ ! -L script ] && [ "$(stat -c '%d:%i:%u:%a' script)" = "$expected" ] || exit 33
 rm -- script || exit 34
fi
`
	}
	script += `rmdir -- "$d" || exit 35
printf 'removed'`
	cmd := fileSSH(ctx, r.record.Connection, "unused", "exec /bin/sh -c "+quoteCommand([]string{script}))
	var out limitedFileOutput
	cmd.Stdout = &out
	r.record.StageCleanup = "unconfirmed"
	if err := cmd.Run(); err == nil && out.String() == "removed" && !out.full {
		r.record.StageCleanup = "removed"
	} else if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() >= 0 && cmd.ProcessState.ExitCode() != 255 {
		r.record.StageCleanup = "failed; changed or unrelated files preserved"
	}
}
