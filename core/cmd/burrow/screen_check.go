// Replay real PTY output using the same pinned VT already exercised by the proofs.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

func main() {
	w, h := 80, 24
	if len(os.Args) >= 3 {
		w, _ = strconv.Atoi(os.Args[1])
		h, _ = strconv.Atoi(os.Args[2])
	}
	screen := vt.NewEmulator(w, h)
	defer screen.Close()
	go io.Copy(io.Discard, screen)
	if _, err := io.Copy(screen, os.Stdin); err != nil {
		panic(err)
	}
	if len(os.Args) == 4 && os.Args[3] == "--cells" {
		type cell struct {
			Text  string `json:"text"`
			Color string `json:"color"`
		}
		rows := make([][]cell, h)
		for y := 0; y < h; y++ {
			rows[y] = make([]cell, w)
			for x := 0; x < w; x++ {
				if c := screen.CellAt(x, y); c != nil {
					rows[y][x].Text = c.Content
					if c.Style.Fg != nil {
						r, g, b, _ := c.Style.Fg.RGBA()
						rows[y][x].Color = fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
					}
				}
			}
		}
		if err := json.NewEncoder(os.Stdout).Encode(rows); err != nil {
			panic(err)
		}
		return
	}
	if len(os.Args) == 4 && os.Args[3] == "--cursor-line" {
		fmt.Print(strings.Split(screen.String(), "\n")[screen.CursorPosition().Y])
		return
	}
	if len(os.Args) == 4 && os.Args[3] == "--input-line" {
		// Standalone Huh forms draw an inverse software cursor; the hidden
		// terminal cursor remains at the footer instead of the focused input.
		lines := strings.Split(screen.String(), "\n")
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if cell := screen.CellAt(x, y); cell != nil && cell.Style.Attrs&uv.AttrReverse != 0 {
					fmt.Print(lines[y])
					return
				}
			}
		}
		return
	}
	fmt.Print(screen.String())
}
