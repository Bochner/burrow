package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/Bochner/burrow/core/connection"
	"github.com/charmbracelet/x/term"
)

type authenticate struct {
	workspace string
	noColor   bool
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
		// Keep the actual command and concise details in terminal scrollback; the single
		// approval remains visible even on a narrow terminal.
		fmt.Fprintln(tty, (ui{noColor: a.noColor || os.Getenv("NO_COLOR") != ""}).semanticText(review.(map[string]string)["review"]))
		f := confirmForm("Proceed?", "Connect using the SSH command above?", "Connect", "Cancel")
		if e := run(f); e != nil {
			return e
		}
		if !f.GetBool("approved") {
			return fmt.Errorf("connection cancelled; no authentication attempted")
		}
		args = append(args, "--yes")
		args = append(args, "--review", review.(map[string]string)["digest"])
	}
	a.result, e = connection.ExecutePrompt(ctx, a.workspace, args, func(ctx context.Context, p connection.Prompt) ([]byte, error) { return readPrompt(ctx, tty, p) })
	if e != nil {
		return e
	}
	state, ok := a.result.(connection.State)
	if !ok || state.State != "connected" {
		return nil
	}
	offer, err := connection.SaveOffer(ctx, a.workspace, state.Name)
	if err != nil {
		fmt.Fprintln(tty, "Connection active; saved collection unavailable. Use profile save NAME after fixing it.")
		return nil
	}
	if !offer {
		return nil
	}
	list, err := connection.Profiles(ctx, a.workspace)
	if err != nil {
		fmt.Fprintln(tty, "Connection active; collection unavailable for saving.")
		return nil
	}
	f := saveProfileForm(state.Name, list.Path)
	if err = run(f); err != nil {
		return nil
	} // Skipping save never cancels a successful connection.
	saveArgs := []string{"profile", "save", state.Name, "--as", f.GetString("name"), "--collection", list.Path, "--revision", list.Revision}
	result, err := connection.Execute(ctx, a.workspace, saveArgs)
	if err == nil {
		if review, ok := result.(map[string]string); ok && review["review"] != "" {
			confirm := confirmForm("Replace saved profile?", safe(review["review"]), "Save", "Cancel")
			if run(confirm) != nil || !confirm.GetBool("approved") {
				return nil
			}
			_, err = connection.Execute(ctx, a.workspace, append(saveArgs, "--yes", "--revision", review["revision"], "--collection", review["collection"]))
		}
	}
	if err != nil {
		fmt.Fprintln(tty, "Connection active; settings not saved: "+safe(err.Error()))
	}
	return nil
}
func runCLIForm(ctx context.Context, tty *os.File, f *huh.Form) error {
	// Establish no-echo before Huh can render its first prompt, including an
	// immediate paste. Huh owns editing; the outer guard restores original modes.
	state, e := term.MakeRaw(tty.Fd())
	if e != nil {
		return e
	}
	defer term.Restore(tty.Fd(), state)
	// ctx already ends the form on SIGINT/SIGTERM. Bubble Tea's own handler
	// would race it: after the context stops the event loop, its unbuffered
	// QuitMsg send never completes and Program.Run never returns.
	// WithProgramOptions replaces Huh's defaults, so it precedes input/output.
	f.WithProgramOptions(tea.WithoutSignalHandler())
	if e = f.WithInput(tty).WithOutput(tty).RunWithContext(ctx); e != nil {
		return fmt.Errorf("connection cancelled or timed out")
	}
	return nil
}
func readPrompt(ctx context.Context, tty *os.File, p connection.Prompt) ([]byte, error) {
	f := promptForm(p, false)
	if e := runCLIForm(ctx, tty, f); e != nil {
		return nil, e
	}
	return promptAnswer(f, p.Secret), nil
}

func promptChainCLI(ctx context.Context, workspace string, args []string, noColor bool) error {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("password/passphrase chain needs a controlling terminal; use an available key or agent for unattended exports")
	}
	defer tty.Close()
	_, err = connection.PrepareChainPrompt(ctx, workspace, args, func(ctx context.Context, p connection.Prompt) ([]byte, error) {
		return readPrompt(ctx, tty, p)
	}, func(chain any) error {
		if err := printResult(chain, noColor); err != nil {
			return err
		}
		fmt.Fprintln(tty, "Chain JSON exported. Keep this terminal open; confirm the chain in Hovel from another terminal within 10 minutes. Ctrl+C cancels.")
		return nil
	})
	if err == nil {
		fmt.Fprintln(tty, "SSH connection established; Hovel is collecting the chain result. Burrow retains the connection.")
	}
	return err
}
