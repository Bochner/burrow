package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Bochner/burrow/core/agent"
)

func agentCommand(global *flag.FlagSet, args []string, noColor bool) error {
	var invalid string
	global.Visit(func(f *flag.Flag) {
		if f.Name != "offline" && f.Name != "no-color" {
			invalid = f.Name
		}
	})
	if invalid != "" {
		return fmt.Errorf("agent install does not use --%s", invalid)
	}
	if len(args) < 2 || args[0] != "install" {
		return fmt.Errorf("expected agent install claude|codex|opencode --scope user|project [--source PATH] [--dry-run]")
	}
	fs := flag.NewFlagSet("agent install", flag.ContinueOnError)
	scope := fs.String("scope", "", "required user or project scope")
	source := fs.String("source", "", "trusted absolute local bundle directory; default embedded release")
	dry := fs.Bool("dry-run", false, "preflight and show planned changes without writing")
	if err := fs.Parse(args[2:]); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected install arguments")
	}
	result, err := agent.Install(args[1], *scope, *source, *dry)
	if result.Destination != "" {
		if outputErr := printResult(os.Stdout, result, noColor); err == nil {
			err = outputErr
		}
	}
	return err
}
