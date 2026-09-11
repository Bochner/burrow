// Disposable terminal boundary probe; no SSH or command execution.
package main

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/vibepwners/hovel/sdk/go/hovel"
	"golang.org/x/sys/unix"
)

type prototype struct{}

func (prototype) Info() hovel.Info {
	return hovel.Info{Name: "burrow-sdk-prototype", Version: "0.0.0", Type: hovel.TypeSurvey, Summary: "Inert terminal boundary proof."}
}

func (prototype) Schema() hovel.Schema { return hovel.Schema{} }

func terminal(input io.Reader, output io.Writer) error {
	fd := int(input.(*os.File).Fd())
	state, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return err
	}
	raw := *state
	raw.Lflag &^= unix.ICANON | unix.ECHO | unix.ISIG | unix.IEXTEN
	raw.Iflag &^= unix.ICRNL | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Cc[unix.VMIN], raw.Cc[unix.VTIME] = 1, 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &raw); err != nil {
		return err
	}
	defer unix.IoctlSetTermios(fd, unix.TCSETS, state)
	if _, err := fmt.Fprint(output, "terminal probe ready\r\n"); err != nil {
		return err
	}
	buf := make([]byte, 1)
	for {
		if _, err := io.ReadFull(input, buf); err != nil {
			return err
		}
		size, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "byte=%02x size=%dx%d\r\n", buf[0], size.Col, size.Row); err != nil {
			return err
		}
		// Explicitly probe presentation modes; detach must restore these too.
		if buf[0] == 'a' {
			if _, err := fmt.Fprint(output, "\x1b[?1049h\x1b[?25l"); err != nil {
				return err
			}
		}
		if buf[0] == 'r' {
			if _, err := fmt.Fprint(output, "\x1b[?25h\x1b[?1049l"); err != nil {
				return err
			}
		}
	}
}

func (prototype) Run(ctx *hovel.Context) (hovel.Result, error) {
	ctx.Log.Info("opening inert terminal probe")
	_, err := ctx.OpenSession(&hovel.PTYSession{Frontend: terminal}, hovel.WithName("Burrow terminal prototype"))
	if err != nil {
		return hovel.Result{}, err
	}
	return hovel.Ok(nil, hovel.WithSummary("inert prototype pid=" + strconv.Itoa(os.Getpid()))), nil
}

func main() { hovel.Serve(prototype{}) }
