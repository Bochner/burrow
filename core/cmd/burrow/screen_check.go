// Replay real PTY output using the same pinned VT already exercised by the proofs.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/x/vt"
)

func main() {
	screen := vt.NewEmulator(80, 24)
	defer screen.Close()
	go io.Copy(io.Discard, screen)
	if _, err := io.Copy(screen, os.Stdin); err != nil {
		panic(err)
	}
	fmt.Print(screen.String())
}
