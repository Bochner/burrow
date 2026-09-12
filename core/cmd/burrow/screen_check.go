// Replay real PTY output using the same pinned VT already exercised by the proofs.
package main

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/charmbracelet/x/vt"
)

func main() {
	w, h := 80, 24
	if len(os.Args) == 3 {
		w, _ = strconv.Atoi(os.Args[1])
		h, _ = strconv.Atoi(os.Args[2])
	}
	screen := vt.NewEmulator(w, h)
	defer screen.Close()
	go io.Copy(io.Discard, screen)
	if _, err := io.Copy(screen, os.Stdin); err != nil {
		panic(err)
	}
	fmt.Print(screen.String())
}
