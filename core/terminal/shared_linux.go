package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/Bochner/burrow/core/connection"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// SharedState is an observation of the real owner, never a resource registry.
type SharedState struct {
	Shell           connection.Shell
	Controlled      bool
	Synchronization string
	Alternate       bool
	RecoveredGap    bool
}

// Shared renders complete owner snapshots. It never feeds subsequent bytes into
// a display snapshot as though it were a terminal parser checkpoint.
type Shared struct {
	mu                  sync.Mutex
	state               Snapshot
	token               string
	generation          uint64
	modes               []int
	position            uint64
	controlPending      bool
	releasing           bool
	inputError          error
	offset              int
	workspace, name, id string
	ctx                 context.Context
	cancel              context.CancelFunc
	input               chan sharedEvent
	done                chan struct{}
}

type sharedEvent struct {
	value      any
	token      string
	generation uint64
	reply      chan error
}

// Control explicitly takes over the last observed generation at this geometry.
type Control image.Point
type Release struct{}
type historyMove struct{ event any }

func Attach(ctx context.Context, workspace, name, id string) (*Shared, error) {
	s := &Shared{workspace: workspace, name: name, id: id, input: make(chan sharedEvent, 128), done: make(chan struct{})}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.state.Shared = &SharedState{Shell: connection.Shell{ID: id, Workspace: workspace, Connection: connection.State{Name: name}}, Synchronization: "out-of-sync"}
	if err := s.refresh(); err != nil {
		s.cancel()
		return nil, err
	}
	go s.run()
	return s, nil
}

func (s *Shared) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.state
	shared := *v.Shared
	v.Shared = &shared
	return v
}

func (s *Shared) Send(value any) error {
	return s.enqueue(value, nil)
}

func (s *Shared) enqueue(value any, reply chan error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return fmt.Errorf("shell attachment ended")
	}
	_, control := value.(Control)
	_, release := value.(Release)
	if _, ok := s.scroll(value); ok {
		value = historyMove{value}
	}
	_, history := value.(historyMove)
	if s.controlPending {
		return fmt.Errorf("shell control change pending; wait before input or detach")
	}
	if s.inputError != nil && !control && !release && !history {
		if _, resize := value.(image.Point); !resize {
			return fmt.Errorf("input refused after an uncertain operation; inspect and explicitly take control again")
		}
	}
	if !control && !release && !history && (!s.state.Shared.Controlled || (s.state.Shared.Synchronization != "snapshot-current" && s.state.Shared.Synchronization != "snapshot-history")) {
		if _, resize := value.(image.Point); resize {
			return nil
		} // Observer geometry is local only.
		return fmt.Errorf("OBSERVE: input refused; explicitly take control first")
	}
	if text, ok := value.(string); ok && len(text) > 4096 {
		return fmt.Errorf("paste exceeds 4096 bytes; input not sent")
	}
	event := sharedEvent{value: value, token: s.token, generation: s.state.Shared.Shell.ControlGeneration, reply: reply}
	select {
	case s.input <- event:
		if control {
			s.controlPending = true
		}
		if release {
			s.releasing = true
			s.state.Shared.Controlled = false
		}
		return nil
	default:
		return fmt.Errorf("shell input busy; input was not sent")
	}
}

// Detach waits for this attachment's release; stale authority cannot release a
// replacement controller. A failed acknowledgement remains an explicit error.
func (s *Shared) Detach(ctx context.Context) error {
	reply := make(chan error, 1)
	if err := s.enqueue(Release{}, reply); err != nil {
		return err
	}
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return fmt.Errorf("shell release unconfirmed; inspect controller before takeover")
	case <-s.done:
		return fmt.Errorf("shell attachment ended before release was confirmed")
	}
}

func (s *Shared) Close()                { s.cancel(); <-s.done }
func (s *Shared) Done() <-chan struct{} { return s.done }

func (s *Shared) private(ctx context.Context, action string, request any) (connection.ShellControl, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return connection.ShellControl{}, err
	}
	value, err := connection.SessionCommand(ctx, s.workspace, []string{"session", action, s.name, s.id, "--request-stdin"}, bytes.NewReader(raw))
	if err != nil {
		return connection.ShellControl{}, err
	}
	return value.(connection.ShellControl), nil
}

func (s *Shared) refresh() error {
	ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
	defer cancel()
	s.mu.Lock()
	offset := s.offset
	s.mu.Unlock()
	value, err := connection.Execute(ctx, s.workspace, []string{"session", "snapshot", s.name, s.id, strconv.Itoa(offset)})
	if err == nil && value.(connection.ShellOutput).Shell.State == "unavailable" {
		// An owner RPC failure is not proof that our claim was revoked.
		err = fmt.Errorf("shell owner unavailable; control and cleanup unverified")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.state.Shared.Shell.State = "unverified"
		if errors.Is(err, connection.ErrShellUnavailable) {
			s.token = ""
			s.state.Shared.Shell.State = "unavailable"
		}
		s.state.Shared.Synchronization = "out-of-sync"
		s.state.Shared.Controlled = false
		s.state.Visible = false
		s.state.Err = fmt.Errorf("shell observation UNVERIFIED; last view retained: %w", err)
		return err
	}
	out := value.(connection.ShellOutput)
	shared := s.state.Shared
	shared.Shell, shared.Synchronization = out.Shell, out.Synchronization
	if s.token != "" && (s.generation != out.Shell.ControlGeneration || out.Shell.State != "running") {
		s.token = ""
	}
	shared.Controlled = s.token != "" && out.Shell.State == "running" && !s.releasing
	s.state.Exited = out.Closed
	s.state.Err = s.inputError
	s.state.Visible = false
	if out.Screen != nil {
		if out.Screen.InputModes == nil {
			shared.Synchronization = "out-of-sync"
			shared.Controlled = false
			s.state.Err = fmt.Errorf("owner snapshot lacks input modes; explicitly restart the older manager")
			return s.state.Err
		}
		s.state.Screen = out.Screen.Text
		s.state.Cursor = image.Pt(out.Screen.CursorX, out.Screen.CursorY)
		s.state.Visible = out.Screen.Visible && !out.Lost && !out.Closed
		shared.Alternate = out.Screen.Alternate
		shared.RecoveredGap = shared.RecoveredGap || s.position < out.Oldest
		s.position = out.Next
		s.modes = append(s.modes[:0], out.Screen.InputModes...)
		s.state.MouseMotion = slices.Contains(s.modes, 1003)
		s.state.ScrollOffset, s.state.HistoryLines = out.Screen.ScrollOffset, out.Screen.HistoryLines
		s.offset = out.Screen.ScrollOffset
	}
	if out.RecoveryError != "" {
		s.state.Err = fmt.Errorf("out-of-sync: %s", out.RecoveryError)
	}
	if out.Lost {
		s.state.Err = fmt.Errorf("shell LOST; last-known screen; remote outcome uncertain; no reconnect")
	}
	if out.Shell.AuditError != "" {
		s.state.Err = errors.Join(s.state.Err, fmt.Errorf("shell audit incomplete: %s", out.Shell.AuditError))
	}
	return nil
}

func (s *Shared) run() {
	defer close(s.done)
	// ponytail: 50 ms snapshot polling; use owner notifications if measured RPC
	// or rendering cost requires it. Each attachment has its own observation.
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			s.mu.Lock()
			token := s.token
			s.mu.Unlock()
			if token != "" {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				_, _ = s.private(ctx, "release", connection.ShellRelease{Token: token})
				cancel()
			}
			return
		case event := <-s.input:
			err := s.handle(event)
			s.mu.Lock()
			if err != nil {
				s.inputError, s.state.Err = err, err
			}
			if _, control := event.value.(Control); control {
				s.controlPending = false
			}
			if _, release := event.value.(Release); release {
				s.releasing = false
			}
			s.mu.Unlock()
			if event.reply != nil {
				event.reply <- err
			}
		case <-ticker.C:
			_ = s.refresh()
		}
	}
}

func (s *Shared) handle(event sharedEvent) error {
	ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
	defer cancel()
	s.mu.Lock()
	token, modes := s.token, append([]int{}, s.modes...)
	s.mu.Unlock()
	_, history := event.value.(historyMove)
	if _, control := event.value.(Control); !control && !history && (event.token == "" || event.token != token) {
		if _, release := event.value.(Release); release {
			return nil
		}
		return fmt.Errorf("controller changed; queued input refused")
	}
	var result connection.ShellControl
	var err error
	switch value := event.value.(type) {
	case historyMove:
		s.mu.Lock()
		if offset, ok := s.scroll(value.event); ok {
			s.offset = offset
		}
		s.mu.Unlock()
	case Control:
		size := image.Point(value)
		result, err = s.private(ctx, "takeover", connection.ShellClaim{Label: fmt.Sprintf("tui-%d", os.Getpid()), Generation: &event.generation, Columns: &size.X, Rows: &size.Y})
		if err == nil {
			s.mu.Lock()
			s.token, s.generation = result.Token, result.Generation
			s.inputError = nil
			s.mu.Unlock()
		}
	case Release:
		_, err = s.private(ctx, "release", connection.ShellRelease{Token: event.token})
		if err == nil {
			s.mu.Lock()
			s.token = ""
			s.mu.Unlock()
		}
		// Reconcile a concurrent takeover: releasing our obsolete claim is done.
		if err != nil {
			_ = s.refresh()
			s.mu.Lock()
			replaced := s.token == ""
			s.mu.Unlock()
			if replaced {
				err = nil
			}
		}
	case image.Point:
		_, err = s.private(ctx, "resize", connection.ShellResize{Token: event.token, Columns: value.X, Rows: value.Y})
	default:
		s.mu.Lock()
		blocked := s.inputError != nil
		s.mu.Unlock()
		if blocked {
			return fmt.Errorf("queued input refused after an uncertain operation; inspect before taking control")
		}
		s.mu.Lock()
		s.offset = 0
		s.mu.Unlock()
		data := encodeSharedInput(value, modes)
		if len(data) == 0 {
			return nil
		}
		if len(data) > 4096 {
			return fmt.Errorf("encoded input exceeds 4096 bytes; input not sent")
		}
		result, err = s.private(ctx, "input", connection.ShellInput{Token: event.token, Data: data})
		if err == nil && (result.AcceptedBytes != len(data) || result.Backpressure || result.InputError != "") {
			err = fmt.Errorf("input incomplete: %d/%d bytes accepted; inspect shell before continuing", result.AcceptedBytes, len(data))
		}
	}
	if err == nil {
		_ = s.refresh()
	}
	return err
}

// Navigation is local to each observer; it never takes control or resizes.
func (s *Shared) scroll(event any) (int, bool) {
	if s.state.Shared.Alternate {
		return 0, false
	}
	offset := s.offset
	switch v := event.(type) {
	case uv.KeyPressEvent:
		if v.Mod != uv.ModShift {
			return 0, false
		}
		switch v.Code {
		case uv.KeyPgUp:
			offset += max(1, s.state.Shared.Shell.Rows-1)
		case uv.KeyPgDown:
			offset -= max(1, s.state.Shared.Shell.Rows-1)
		case uv.KeyHome:
			offset = s.state.HistoryLines
		case uv.KeyEnd:
			offset = 0
		default:
			return 0, false
		}
	case uv.MouseWheelEvent:
		for _, mode := range []int{9, 1000, 1002, 1003} {
			if slices.Contains(s.modes, mode) {
				return 0, false
			}
		}
		switch v.Button {
		case uv.MouseWheelUp:
			offset += 3
		case uv.MouseWheelDown:
			offset -= 3
		default:
			return 0, false
		}
	default:
		return 0, false
	}
	return max(0, min(s.state.HistoryLines, offset)), true
}

// Reuse the pinned VT encoder with only the owner's input modes. No remote
// display bytes or terminal queries enter this encoder or the operator's tty.
func encodeSharedInput(value any, modes []int) []byte {
	em := vt.NewEmulator(1, 1)
	var data bytes.Buffer
	done := make(chan struct{})
	go func() { _, _ = io.Copy(&data, em); close(done) }()
	inputModes := make(map[ansi.Mode]bool)
	for _, mode := range modes {
		switch mode {
		case 1, 9, 66, 1000, 1002, 1003, 1006, 2004:
			em.WriteString(fmt.Sprintf("\x1b[?%dh", mode))
			inputModes[ansi.DECMode(mode)] = true
		}
	}
	sendVTInput(em, value, inputModes)
	em.InputPipe().(io.Closer).Close()
	<-done
	em.Close()
	return data.Bytes()
}
