// #71 measured baseline: actual per-connection implementation, no terminal UI.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Bochner/burrow/core/connection"
	"github.com/Bochner/burrow/core/launch"
	"github.com/vibepwners/hovel/sdk/go/hovel"
)

func main() {
	if os.Getenv("BURROW_ASKPASS") == "1" {
		if len(os.Args) != 2 || connection.Askpass(os.Args[1]) != nil {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "module" {
		hovel.Serve(connection.Module{})
		return
	}
	if len(os.Args) < 3 {
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	w := os.Args[2]
	var result any
	var err error
	if os.Args[1] == "install" {
		err = launch.RegisterModule(ctx, w, "burrow@0.1.0", connection.Manifest)
		result = map[string]bool{"installed": err == nil}
	} else if os.Args[1] == "prompt" {
		input := bufio.NewReader(os.Stdin)
		result, err = connection.ExecutePrompt(ctx, w, os.Args[3:], func(_ context.Context, p connection.Prompt) ([]byte, error) {
			if !p.Secret {
				return nil, fmt.Errorf("unexpected trust prompt")
			}
			fmt.Fprintln(os.Stderr, "PRIVATE_PROMPT")
			answer, e := input.ReadBytes('\n')
			if e != nil {
				return nil, e
			}
			return answer[:len(answer)-1], nil
		})
	} else {
		result, err = connection.Execute(ctx, w, os.Args[3:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if json.NewEncoder(os.Stdout).Encode(result) != nil {
		os.Exit(1)
	}
}
