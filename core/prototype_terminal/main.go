package main

import (
	"bufio"
	tea "charm.land/bubbletea/v2"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
	"os"
	"strings"
)

func run() int {
	vtMode := flag.Bool("vt", true, "internal full-screen shell emulator")
	jsonOutput := flag.Bool("json", false, "JSON results for fixture command/script mode")
	script := flag.String("script", "", "read fixture commands from file, or - for stdin")
	noColor := flag.Bool("no-color", false, "disable color")
	forceColor := flag.Bool("color", false, "force Dracula truecolor, overriding NO_COLOR")
	flag.Parse()
	f, err := newFixture()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer f.close()
	if *script != "" || flag.NArg() > 0 {
		var commands [][]string
		if *script != "" {
			var in *os.File
			if *script == "-" {
				in = os.Stdin
			} else {
				in, err = os.Open(*script)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 1
				}
				defer in.Close()
			}
			scanner := bufio.NewScanner(in)
			scanner.Buffer(make([]byte, 4096), 65536)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				args, e := words(line)
				if e != nil {
					fmt.Fprintln(os.Stderr, e)
					return 2
				}
				commands = append(commands, args)
			}
			if err = scanner.Err(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		} else {
			commands = [][]string{flag.Args()}
		}
		for _, args := range commands {
			r := f.execute(args)
			for r.Error == "" && f.copy != nil {
				if err = f.advance(); err != nil {
					r.Error = err.Error()
				}
			}
			if r.Command == "get" || r.Command == "put" || r.Command == "confirm" {
				r.Message = f.progress
			}
			if *jsonOutput {
				json.NewEncoder(os.Stdout).Encode(r)
			} else {
				fmt.Println(r.Message)
				for _, e := range r.Entries {
					fmt.Printf("%s %d %s\n", e.Mode, e.Size, e.Name)
				}
				if r.Error != "" {
					fmt.Fprintln(os.Stderr, r.Error)
				}
			}
			if r.Error != "" {
				return 1
			}
			if f.quit {
				break
			}
		}
		return 0
	}
	if !term.IsTerminal(os.Stdin.Fd()) {
		fmt.Fprintln(os.Stderr, "Interactive prototype needs a terminal; use --script - --json for a pipe.")
		return 2
	}
	color := !*noColor && (*forceColor || os.Getenv("NO_COLOR") == "")
	var options []tea.ProgramOption
	// Aspect captures child stdout. Render on the controlling terminal so
	// Bubble Tea can detect its size/colors and own terminal restoration.
	if !term.IsTerminal(os.Stdout.Fd()) {
		tty, openErr := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if openErr != nil {
			fmt.Fprintln(os.Stderr, "Cannot open interactive terminal:", openErr)
			return 2
		}
		defer tty.Close()
		options = append(options, tea.WithOutput(tty))
	}
	if !color {
		options = append(options, tea.WithColorProfile(colorprofile.ASCII))
	} else if *forceColor {
		options = append(options, tea.WithColorProfile(colorprofile.TrueColor))
	}
	m := newModel(f, color)
	m.vtMode = *vtMode
	defer func() {
		m.f.close()
		for _, s := range m.shells {
			s.close()
		}
	}()
	_, err = tea.NewProgram(m, options...).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("Prototype closed; local shells and scratch files removed.")
	return 0
}
func main() { os.Exit(run()) }
