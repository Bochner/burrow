package connection

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Bochner/burrow/core/launch"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/vibepwners/hovel/sdk/go/hovel"
	"golang.org/x/sys/unix"
)

const shellRequestLimit = 8192

type ShellClaim struct {
	Label      string  `json:"label,omitempty"`
	Generation *uint64 `json:"generation,omitempty"`
	Columns    *int    `json:"columns,omitempty"`
	Rows       *int    `json:"rows,omitempty"`
}
type ShellResize struct {
	Token   string `json:"token"`
	Columns int    `json:"columns"`
	Rows    int    `json:"rows"`
}
type ShellRelease struct {
	Token string `json:"token"`
}
type ShellInput struct {
	Token string `json:"token"`
	Data  []byte `json:"data"`
}
type ShellControl struct {
	Generation    uint64 `json:"generation"`
	Controller    string `json:"controller"`
	Token         string `json:"token,omitempty"`
	AcceptedBytes int    `json:"acceptedBytes"`
	Backpressure  bool   `json:"backpressure"`
	InputError    string `json:"inputError,omitempty"`
	Columns       int    `json:"columns"`
	Rows          int    `json:"rows"`
	Released      bool   `json:"released"`
}
type ShellOutput struct {
	Shell           Shell        `json:"shell"`
	Data            []byte       `json:"data"`
	Oldest          uint64       `json:"oldest"`
	Next            uint64       `json:"next"`
	Gap             bool         `json:"gap"`
	Closed          bool         `json:"closed"`
	Lost            bool         `json:"lost"`
	Synchronization string       `json:"synchronization"`
	RecoveryError   string       `json:"recoveryError,omitempty"`
	Screen          *ShellScreen `json:"screen,omitempty"`
}

// A display snapshot, not serialized emulator/parser state. Consumers render
// this complete view and poll snapshots again; never replay bytes into it.
type ShellScreen struct {
	HistoryLines int    `json:"historyLines"`
	ScrollOffset int    `json:"scrollOffset"`
	InputModes   []int  `json:"inputModes"`
	Text         string `json:"text"`
	CursorX      int    `json:"cursorX"`
	CursorY      int    `json:"cursorY"`
	Visible      bool   `json:"visible"`
	Alternate    bool   `json:"alternate"`
}

func decodeShellRequest(raw string, value any) error {
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if len(raw) > shellRequestLimit || d.Decode(value) != nil || d.Decode(new(any)) != io.EOF || !strings.HasPrefix(strings.TrimSpace(raw), "{") {
		// Never echo input, field names or decoder errors: requests carry tokens.
		return fmt.Errorf("invalid private shell request; expected one bounded JSON object")
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(raw), &fields)
	for _, field := range fields {
		if strings.TrimSpace(string(field)) == "null" {
			return fmt.Errorf("null shell request fields are refused; omit optional fields for defaults")
		}
	}
	return nil
}

// SessionCommand carries private input through the public Hovel extension.
// Unlike Execute, neither its request nor its token-bearing result is audited.
func SessionCommand(ctx context.Context, w string, args []string, input io.Reader) (any, error) {
	if err := ValidateCommand(w, args); err != nil {
		return nil, err
	}
	if len(args) != 5 || args[0] != "session" || args[4] != "--request-stdin" {
		return nil, fmt.Errorf("private shell request required")
	}
	raw, err := io.ReadAll(io.LimitReader(input, shellRequestLimit+1))
	if err != nil || len(raw) > shellRequestLimit {
		return nil, fmt.Errorf("private shell request exceeds 8192 bytes or cannot be read")
	}
	var object map[string]json.RawMessage
	if err := decodeShellRequest(string(raw), &object); err != nil {
		return nil, err
	}
	s, err := inspectShell(ctx, w, args[2], args[3])
	if err != nil {
		return nil, err
	}
	if s.State != "running" {
		return nil, fmt.Errorf("shell is not running; inspect before continuing; no reconnect")
	}
	var result hovel.PayloadCommandResult
	err = launch.Call(ctx, w, "RunSessionCommand", map[string]any{"SessionID": s.ID, "Request": hovel.PayloadCommandRequest{Command: args[1], InputData: string(raw), InputEncoding: "utf-8"}}, &result)
	if err != nil {
		return nil, err
	}
	var control ShellControl
	if json.Unmarshal([]byte(result.Stdout), &control) != nil {
		return nil, fmt.Errorf("invalid shell control response; inspect before retrying")
	}
	return control, nil
}

func observeShell(ctx context.Context, w string, args []string) (ShellOutput, error) {
	s, err := inspectShell(ctx, w, args[2], args[3])
	if err != nil {
		return ShellOutput{}, err
	}
	if s.State == "unavailable" {
		return ShellOutput{Shell: s, Lost: true, Synchronization: "out-of-sync"}, nil
	}
	after := "0"
	if len(args) == 5 {
		after = args[4]
	}
	arguments := []string{after}
	if args[1] == "snapshot" && len(args) == 4 {
		arguments = nil
	}
	result, err := ownerCommand(ctx, w, s.ID, args[1], arguments)
	if err != nil {
		return ShellOutput{}, err
	}
	var output ShellOutput
	if json.Unmarshal([]byte(result.Stdout), &output) != nil || output.Shell.ID != s.ID {
		return ShellOutput{}, fmt.Errorf("invalid shell observation")
	}
	return output, nil
}

// Called while holding the same lock as launch, close, output and geometry.
func (s *retainedShell) sharedCommand(req hovel.PayloadCommandRequest) (any, error) {
	closed := s.record.State == "closed" || s.record.State == "exited"
	if closed && s.done != nil {
		// Do not publish final completion before its audit result is available.
		select {
		case <-s.done:
		default:
			closed = false
		}
	}
	if req.Command == "snapshot" && len(req.Args) <= 1 && req.InputData == "" && req.InputEncoding == "" {
		offset := 0
		if len(req.Args) == 1 {
			var err error
			offset, err = strconv.Atoi(req.Args[0])
			if err != nil || offset < 0 || offset > 1000 {
				return nil, fmt.Errorf("invalid history offset; expected 0..1000")
			}
		}
		out := ShellOutput{Shell: s.record, Data: []byte{}, Oldest: s.record.Dropped, Next: s.record.Received, Closed: closed, Lost: s.record.State == "lost", Synchronization: "out-of-sync"}
		if s.screen != nil {
			history := s.screen.ScrollbackLen()
			offset = min(offset, history)
			if s.screen.IsAltScreen() {
				offset = 0
			}
			cellAt := s.screen.CellAt
			if offset > 0 {
				cellAt = func(x, y int) *uv.Cell {
					line := history - offset + y
					if line < history {
						return s.screen.ScrollbackCellAt(x, line)
					}
					return s.screen.CellAt(x, line-history)
				}
			}
			// Bound variable-sized cell content before Render duplicates links per
			// line. Fixed-size style overhead is bounded by the geometry limit.
			contentBytes := 0
			for y := 0; y < s.record.Rows; y++ {
				for x := 0; x < s.record.Columns; x++ {
					if cell := cellAt(x, y); cell != nil {
						contentBytes += len(cell.Content) + len(cell.Link.URL) + len(cell.Link.Params)
						if contentBytes > 256<<10 {
							out.RecoveryError = "screen content exceeds 256 KiB render budget; view remains out-of-sync"
							return out, nil
						}
					}
				}
			}
			cursor := s.screen.CursorPosition()
			text := ""
			if offset == 0 {
				text = s.screen.Render()
			} else {
				buf := uv.NewRenderBuffer(s.record.Columns, s.record.Rows)
				for y := range s.record.Rows {
					for x := range s.record.Columns {
						buf.SetCell(x, y, cellAt(x, y))
					}
				}
				text = buf.Render()
			}
			out.Screen = &ShellScreen{Text: text, CursorX: cursor.X, CursorY: cursor.Y, Visible: s.cursorVisible && offset == 0, Alternate: s.screen.IsAltScreen(), HistoryLines: history, ScrollOffset: offset}
			out.Screen.InputModes = []int{}
			for _, mode := range []int{1, 9, 66, 1000, 1002, 1003, 1006, 2004} {
				if s.inputModes[ansi.DECMode(mode)] {
					out.Screen.InputModes = append(out.Screen.InputModes, mode)
				}
			}
			out.Synchronization = "snapshot-current"
			if offset > 0 {
				out.Synchronization = "snapshot-history"
			}
			if out.Lost {
				out.Synchronization = "last-known-screen"
			}
			b, err := json.Marshal(out)
			if err != nil || len(b) > 256<<10 {
				out.Screen = nil
				out.Synchronization = "out-of-sync"
				out.RecoveryError = "screen snapshot exceeds 256 KiB; view remains out-of-sync"
			}
		}
		return out, nil
	}
	if req.Command == "observe" && len(req.Args) == 1 && req.InputData == "" && req.InputEncoding == "" {
		after, err := strconv.ParseUint(req.Args[0], 10, 64)
		if err != nil || after > s.record.Received {
			return nil, fmt.Errorf("invalid shell output position")
		}
		oldest := s.record.Dropped
		out := ShellOutput{Shell: s.record, Data: []byte{}, Oldest: oldest, Next: after, Gap: after < oldest, Closed: closed, Lost: s.record.State == "lost", Synchronization: "stream-only"}
		if out.Gap {
			out.Synchronization = "out-of-sync"
		} else {
			out.Data = append(out.Data, s.data[after-oldest:]...)
			out.Next = s.record.Received
		}
		return out, nil
	}
	if len(req.Args) != 0 || req.InputEncoding != "utf-8" || s.record.State != "running" {
		return nil, fmt.Errorf("private JSON request and running shell required; raw routes refused")
	}
	result := ShellControl{}
	switch req.Command {
	case "claim", "takeover":
		var claim ShellClaim
		if err := decodeShellRequest(req.InputData, &claim); err != nil {
			return nil, err
		}
		if (req.Command == "claim" && (claim.Generation != nil || s.token != "")) || (req.Command == "takeover" && (claim.Generation == nil || *claim.Generation != s.record.ControlGeneration)) {
			return nil, fmt.Errorf("controller changed or shell already claimed; inspect before explicit generation-checked takeover")
		}
		if claim.Label == "" {
			claim.Label = "agent"
		}
		if len(claim.Label) > 64 || strings.IndexFunc(claim.Label, func(r rune) bool { return unicode.IsControl(r) || unicode.In(r, unicode.Cf) }) >= 0 {
			return nil, fmt.Errorf("invalid controller label")
		}
		columns, rows := s.record.Columns, s.record.Rows
		if claim.Columns != nil {
			columns = *claim.Columns
		}
		if claim.Rows != nil {
			rows = *claim.Rows
		}
		if err := s.resize(columns, rows); err != nil {
			return nil, err
		}
		s.record.ControlGeneration++
		s.record.Controller, s.token = claim.Label, rand.Text()
		result.Token = s.token
	case "input":
		var input ShellInput
		if err := decodeShellRequest(req.InputData, &input); err != nil {
			return nil, err
		}
		if s.token == "" || input.Token != s.token {
			return nil, fmt.Errorf("controller changed; input refused")
		}
		if len(input.Data) == 0 || len(input.Data) > 4096 {
			return nil, fmt.Errorf("expected 1..4096 base64 input bytes")
		}
		if err := s.pty.SetWriteDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
			return nil, fmt.Errorf("shell input deadline unavailable; input refused")
		}
		n, err := s.pty.Write(input.Data)
		result.AcceptedBytes, result.Backpressure = n, errors.Is(err, os.ErrDeadlineExceeded)
		if err != nil && !result.Backpressure {
			result.InputError = "PTY write failed; only the reported prefix was accepted; inspect before continuing"
		}
	case "resize":
		var input ShellResize
		if err := decodeShellRequest(req.InputData, &input); err != nil {
			return nil, err
		}
		if s.token == "" || input.Token != s.token {
			return nil, fmt.Errorf("controller changed; resize refused")
		}
		if err := s.resize(input.Columns, input.Rows); err != nil {
			return nil, err
		}
	case "release":
		var input ShellRelease
		if err := decodeShellRequest(req.InputData, &input); err != nil {
			return nil, err
		}
		if s.token == "" || input.Token != s.token {
			return nil, fmt.Errorf("controller changed; release refused")
		}
		s.token, s.record.Controller = "", ""
		s.record.ControlGeneration++
		result.Released = true
	default:
		return nil, fmt.Errorf("unsupported shell command")
	}
	result.Generation, result.Controller = s.record.ControlGeneration, s.record.Controller
	result.Columns, result.Rows = s.record.Columns, s.record.Rows
	return result, nil
}

func shellGeometry(columns, rows int) error {
	if columns < 1 || rows < 1 || columns > 1000 || rows > 1000 || columns*rows > 20000 {
		return fmt.Errorf("shell geometry requires 1..1000 columns/rows and at most 20000 cells")
	}
	return nil
}

func (s *retainedShell) resize(columns, rows int) error {
	if err := shellGeometry(columns, rows); err != nil {
		return err
	}
	// Fd() would switch this pollable descriptor back to blocking I/O.
	raw, err := s.pty.SyscallConn()
	if err != nil {
		return err
	}
	var resizeErr error
	err = raw.Control(func(fd uintptr) {
		resizeErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(columns), Row: uint16(rows)})
	})
	if err != nil {
		return err
	}
	if resizeErr != nil {
		return resizeErr
	}
	s.record.Columns, s.record.Rows = columns, rows
	if s.screen != nil {
		s.screen.Resize(columns, rows)
	}
	return nil
}

func (s *retainedShell) initScreen() {
	s.screen = vt.NewEmulator(s.record.Columns, s.record.Rows)
	s.screen.SetScrollbackSize(1000)
	s.cursorVisible = true
	s.inputModes = make(map[ansi.Mode]bool)
	s.screen.SetCallbacks(vt.Callbacks{
		CursorVisibility: func(visible bool) { s.cursorVisible = visible },
		EnableMode:       func(mode ansi.Mode) { s.inputModes[mode] = true },
		DisableMode:      func(mode ansi.Mode) { delete(s.inputModes, mode) },
	})
	// The pinned VT's RIS reset makes the cursor visible without its callback.
	s.screen.RegisterEscHandler('c', func() bool {
		s.cursorVisible = true
		clear(s.inputModes)
		return false // Continue through the emulator's actual reset handler.
	})
	// Render-only: terminal queries cannot inject unfenced input. Controllers may
	// answer them through input. Closing the pipe also prevents parser backpressure.
	s.screen.InputPipe().(io.Closer).Close()
}

func privateShellCommand(command string) bool {
	switch command {
	case "claim", "takeover", "input", "resize", "release":
		return true
	}
	return false
}
