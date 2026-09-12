package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"charm.land/huh/v2"
	"github.com/Bochner/burrow/core/connection"
	"github.com/charmbracelet/x/term"
)

type authenticate struct {
	workspace string
	args      []string
	result    any
}
type authenticationRequested struct{ args []string }

// Standalone forms belong only to the interactive CLI. Input and output use the
// controlling terminal, leaving JSON stdout available for redirection.
func (a *authenticate) Run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	tty, e := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if e != nil {
		return fmt.Errorf("interactive connection requires a controlling terminal; use key/agent commands for automation")
	}
	defer tty.Close()
	run := func(f *huh.Form) error { return runCLIForm(ctx, tty, f) }
	args := append([]string{}, a.args...)
	if len(args) == 1 && args[0] == "connect" {
		d := &connectDetails{}
		if e := run(detailsForm(a.workspace, d)); e != nil {
			return e
		}
		args = d.args()
	}
	_, yes, e := connection.Parse(a.workspace, args[1:])
	if e != nil {
		return e
	}
	if !yes {
		review, err := connection.Execute(ctx, a.workspace, args)
		if err != nil {
			return err
		}
		f := confirmForm("Proceed?", review.(map[string]string)["review"], "Connect", "Cancel")
		if e := run(f); e != nil {
			return e
		}
		if !f.GetBool("approved") {
			return fmt.Errorf("connection cancelled; no authentication attempted")
		}
		args = append(args, "--yes")
	}
	a.result, e = connection.ExecutePrompt(ctx, a.workspace, args, func(ctx context.Context, p connection.Prompt) ([]byte, error) { return readPrompt(ctx, tty, p) })
	return e
}
func runCLIForm(ctx context.Context, tty *os.File, f *huh.Form) error {
	// Establish no-echo before Huh can render its first prompt, including an
	// immediate paste. Huh owns editing; the outer guard restores original modes.
	state, e := term.MakeRaw(tty.Fd())
	if e != nil {
		return e
	}
	defer term.Restore(tty.Fd(), state)
	if e = f.WithInput(tty).WithOutput(tty).RunWithContext(ctx); e != nil {
		return fmt.Errorf("connection cancelled or timed out")
	}
	return nil
}
func readPrompt(ctx context.Context, tty *os.File, p connection.Prompt) ([]byte, error) {
	f := promptForm(p)
	if e := runCLIForm(ctx, tty, f); e != nil {
		return nil, e
	}
	return promptAnswer(f, p.Secret), nil
}
