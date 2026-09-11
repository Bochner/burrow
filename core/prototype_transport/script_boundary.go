// Disposable prerequisite probe: does the existing Hovel run path retain a
// noninteractive SSH command and its evidence when its caller disappears?
package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vibepwners/hovel/sdk/go/hovel"
)

// Drain everything, retaining only a bounded prefix per stream. Binary output
// uses base64 in the artifact so arbitrary bytes survive JSON transport.
type cappedOutput struct {
	data      bytes.Buffer
	discarded int
}

func (b *cappedOutput) Write(p []byte) (int, error) {
	n := min(len(p), 65536-b.data.Len())
	b.data.Write(p[:n])
	b.discarded += len(p) - n
	return len(p), nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

func scriptBoundary(ctx *hovel.Context) (hovel.Result, error) {
	root := os.Getenv("BURROW_OWNER_ROOT")
	socket := filepath.Join(root, "gateway", "master")
	// Only fixed, inert fixture code. Remote marker files are observations owned
	// by the harness, not an application ledger or a staged-script implementation.
	marker := ctx.InputString("proof_marker", "")
	if marker != "normal" && marker != "disconnect" && marker != "direct" {
		return hovel.Result{}, fmt.Errorf("invalid fixture marker")
	}
	script := `printf ready > "$1.started"
sleep 2
printf '%s\n' "$2"
head -c 70000 /dev/zero
printf 'fixture stderr\n' >&2
printf completed > "$1.finished"
exit 7
`
	command := "exec /bin/sh -s -- " + shellQuote(filepath.Join(root, marker)) + " " + shellQuote("space ' quote ; $(not-executed) ☃")
	p := exec.Command("/usr/bin/ssh", "-F", os.Getenv("BURROW_OWNER_CONFIG"), "-S", socket, "-o", "ProxyCommand=/bin/false", "-T", "target", command)
	p.Stdin = strings.NewReader(script)
	var stdout, stderr cappedOutput
	p.Stdout, p.Stderr = &stdout, &stderr
	err := p.Run()
	if p.ProcessState == nil {
		return hovel.Result{}, err
	}
	code := p.ProcessState.ExitCode()
	state := "remote-exit"
	if code < 0 || code == 255 {
		state = "transport-or-completion-unknown"
	}
	if err != nil && code == 0 {
		return hovel.Result{}, err
	}
	return hovel.Failed("inert remote script exited nonzero", hovel.WithArtifacts(
		hovel.JSONArtifact("script-result", map[string]any{"state": state, "sshExit": code,
			"stdoutBase64": base64.StdEncoding.EncodeToString(stdout.data.Bytes()), "stderrBase64": base64.StdEncoding.EncodeToString(stderr.data.Bytes()),
			"stdoutDiscarded": stdout.discarded, "stderrDiscarded": stderr.discarded}),
	)), nil
}
