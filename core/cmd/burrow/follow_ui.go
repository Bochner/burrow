package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/Bochner/burrow/core/connection"
	"github.com/charmbracelet/x/ansi"
)

const followPreviewBytes = 32768

const followHelp = `# LIVE OUTPUT HELP
Tab / Shift+Tab\tSwitch stdout and stderr; each has its own reading position
Up / Down / PgUp / PgDn / Home\tScroll and pause follow; capture continues
End\tResume following from the next unread byte
Esc / Ctrl+C\tClose this viewer; run and capture continue
Alt+B\tBackground this viewer and return to management
run follow ID [stdout|stderr] [OFFSET]\tReopen; an explicit offset resets that stream
Positions and bounded previews stay in this frontend while it is open.
Other frontends have independent positions. No viewing action launches, cancels or collects.
The preview holds at most 32 KiB per stream; earlier bytes remain in working capture.
Storage budget, received/captured bytes, remote exit and capture errors are separate.
Control/binary bytes are escaped; incomplete UTF-8 waits for the next chunk.
Use run collect ID to register evidence; Ctrl+L opens collected results.
Uncollected output can be lost on owner failure. A viewer never reconnects SSH.
F1 / Esc\tReturn to the same output position
`

type runPreview struct {
	next     int64
	raw      []byte
	viewport viewport.Model
	tail     bool
	final    bool
}

type runView struct {
	id              string
	stream          int
	previews        [2]runPreview
	status          [2]connection.RunOutput
	err             string
	reading, queued bool
	generation      uint64
	cancel          context.CancelFunc
}

type followRead struct {
	view       *runView
	generation uint64
	stream     int
	offset     int64
	chunk      connection.RunOutput
	err        error
}
type followTick struct {
	view       *runView
	generation uint64
}

func (m *frame) openFollow(args []string) tea.Cmd {
	w := m.current()
	stream, offset, err := connection.FollowArgs(args)
	if err != nil {
		w.management.output = "REFUSED: " + safe(err.Error())
		return nil
	}
	if m.demo {
		w.management.output = "Sample data only · commands are disabled in preview."
		return nil
	}
	if w.followSaved == nil {
		w.followSaved = make(map[string]*ui)
	}
	u := w.followSaved[args[2]]
	if u == nil {
		model := newUI(w.management.info, m.noColor)
		u = &model
		u.follow = &runView{id: args[2]}
		for i := range u.follow.previews {
			p := &u.follow.previews[i]
			p.viewport = viewport.New()
			p.viewport.SoftWrap = true
			p.tail = true
		}
		w.followSaved[args[2]] = u
	}
	v := u.follow
	v.stream = slices.Index([]string{"stdout", "stderr"}, stream)
	if len(args) == 5 {
		m.stopFollow(v)
		p := &v.previews[v.stream]
		p.next, p.raw, p.tail, p.final = offset, nil, true, false
		p.viewport.SetContent("")
	}
	if !slices.Contains(w.followViews, u) {
		w.followViews = append(w.followViews, u)
	}
	u.width, u.height, u.noColor = w.management.width, w.management.height, m.noColor
	u.sizeFollow()
	w.management.input.Reset()
	m.selectFollow(u)
	return m.readFollow(m.active, u)
}

func (m *frame) selectFollow(u *ui) {
	m.current().follow, m.current().tab, m.current().focus = u, "follow", "prompt"
	m.selection = nil
}

func (m *frame) stopFollow(v *runView) {
	if v.cancel != nil {
		v.cancel()
	}
	v.generation++
	v.reading, v.queued = false, false
}

func (m *frame) closeFollow() {
	w := m.current()
	u := w.follow
	m.stopFollow(u.follow)
	w.followViews = slices.DeleteFunc(w.followViews, func(candidate *ui) bool { return candidate == u })
	w.follow, w.tab, w.focus = nil, "", "prompt"
}

func (w *workspaceView) followUI(v *runView) *ui {
	for _, u := range w.followViews {
		if u.follow == v {
			return u
		}
	}
	return nil
}

func (m *frame) readFollow(path string, u *ui) tea.Cmd {
	v := u.follow
	if v.reading || v.queued || path != m.active || m.current().activeUI() != u {
		return nil
	}
	v.reading = true
	ctx, cancel := context.WithTimeout(m.terminals.context, 5*time.Second)
	v.cancel = cancel
	id, generation, stream, offset := v.id, v.generation, v.stream, v.previews[v.stream].next
	return m.dispatch(path, func() tea.Msg {
		defer cancel()
		value, err := connection.Execute(ctx, path, []string{"run", "output", id, []string{"stdout", "stderr"}[stream], strconv.FormatInt(offset, 10)})
		chunk, _ := value.(connection.RunOutput)
		return followRead{v, generation, stream, offset, chunk, err}
	})
}

func (m *frame) acceptFollow(path string, result followRead) tea.Cmd {
	w := m.workspaces[path]
	u := w.followUI(result.view)
	if u == nil || result.generation != result.view.generation {
		return nil
	}
	v := u.follow
	v.reading = false
	v.cancel = nil
	if result.err != nil {
		v.err = "UNVERIFIED: " + safe(result.err.Error())
	} else {
		data, err := base64.StdEncoding.DecodeString(result.chunk.Data)
		if err != nil || len(data) > 32768 || result.chunk.NextOffset != result.offset+int64(len(data)) || result.chunk.State == "" || result.chunk.Budget <= 0 {
			v.err = "UNVERIFIED: invalid output response; reading position preserved"
		} else {
			v.err = ""
			result.chunk.Data = "" // The bounded byte preview owns the display data.
			v.status[result.stream] = result.chunk
			p := &v.previews[result.stream]
			// Scrolling freezes the preview, even if a read was already in flight.
			// The same offset is retried on resume; status keeps updating meanwhile.
			if p.tail && p.next == result.offset {
				p.next = result.chunk.NextOffset
				if len(data) > 0 {
					p.raw = append(p.raw, data...)
					if len(p.raw) > followPreviewBytes {
						start := len(p.raw) - followPreviewBytes
						// Drop a whole rune only when the boundary splits valid UTF-8;
						// standalone continuation bytes must remain visible as binary.
						for i := max(0, start-utf8.UTFMax+1); i < start; i++ {
							_, n := utf8.DecodeRune(p.raw[i:])
							if n > 1 && i+n > start {
								start = i + n
								break
							}
						}
						p.raw = append([]byte(nil), p.raw[start:]...)
					}
				}
				final := result.chunk.State != "running" && result.chunk.State != "prepared" && p.next >= result.chunk.Stored
				if len(data) > 0 || p.final != final {
					p.viewport.SetContent(safeRunOutput(p.raw, final))
					p.viewport.GotoBottom()
					p.final = final
				}
			}
		}
	}
	u.sizeFollow()
	if path != m.active || w.activeUI() != u {
		return nil
	}
	delay := 250 * time.Millisecond
	if v.previews[v.stream].tail && v.previews[v.stream].next < v.status[v.stream].Stored {
		delay = 50 * time.Millisecond
	}
	if v.err != "" {
		delay = time.Second
	}
	if state := v.status[v.stream]; state.State != "" && state.State != "running" && state.State != "prepared" && v.previews[v.stream].next >= state.Stored {
		delay = time.Second
	}
	v.queued = true
	generation := v.generation
	return m.dispatch(path, tea.Tick(delay, func(time.Time) tea.Msg { return followTick{v, generation} }))
}

// Escape remote control and binary bytes before they reach any terminal parser.
// Keep a partial final rune buffered until its remaining bytes arrive.
func safeRunOutput(data []byte, final bool) string {
	var b strings.Builder
	for len(data) > 0 {
		if !final && !utf8.FullRune(data) {
			break
		}
		r, n := utf8.DecodeRune(data)
		switch {
		case r == utf8.RuneError && n == 1:
			fmt.Fprintf(&b, "\\x%02x", data[0])
		case r == '\n':
			b.WriteByte('\n')
		case unicode.IsControl(r) || unicode.In(r, unicode.Cf):
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			b.WriteRune(r)
		}
		data = data[n:]
	}
	return b.String()
}

func (m *ui) sizeFollow() {
	if m.follow == nil {
		return
	}
	header := m.followHeader()
	for i := range m.follow.previews {
		p := &m.follow.previews[i]
		width, height := max(1, m.width), max(1, m.height-len(strings.Split(header, "\n"))-3)
		if p.viewport.Width() != width {
			p.viewport.SetWidth(width)
		}
		if p.viewport.Height() != height {
			p.viewport.SetHeight(height)
		}
		if p.tail {
			p.viewport.GotoBottom()
		}
	}
}

func (m ui) followHeader() string {
	v := m.follow
	p, state := v.previews[v.stream], v.status[v.stream]
	mode := "FOLLOWING"
	if !p.tail {
		mode = "PAUSED · End resumes"
	}
	if p.tail && p.next < state.Stored {
		mode = "CATCHING UP"
	}
	status := "Connecting…"
	style := warningStyle
	if state.State != "" {
		status = safe(state.State)
		style = connectionStyle(state.State)
		if state.RemoteExit != nil {
			status += fmt.Sprintf(" · exit %d", *state.RemoteExit)
			style = successStyle
			if *state.RemoteExit != 0 {
				style = errorStyle
			}
		}
	}
	header := m.paint(heading, "LIVE OUTPUT") + " · " + m.paint(accent, safe(v.id)) + "\n" +
		m.paint(infoStyle, []string{"stdout", "stderr"}[v.stream]) + " · " + m.paint(warningStyle, mode) + "\n" +
		m.paint(secondary, "Run: ") + m.paint(style, status)
	if state.State != "" {
		capture := "capturing"
		captureStyle := warningStyle
		if state.State == "prepared" {
			capture = "waiting for launch"
		}
		if state.OutputComplete {
			capture = "complete"
			captureStyle = successStyle
		}
		if state.OutputError != "" || (state.State != "running" && state.State != "prepared" && !state.OutputComplete) {
			capture = "INCOMPLETE"
			captureStyle = errorStyle
		}
		header += "\n" + m.paint(secondary, "Capture: ") + m.paint(captureStyle, capture) + " · " + m.paint(numberStyle, fmt.Sprintf("%d/%d B stored; %d received", state.Stored, state.Budget, state.Received))
		if state.OutputError != "" {
			header += "\n" + m.paint(errorStyle, safe(state.OutputError))
		}
	}
	header += "\n" + m.paint(secondary, fmt.Sprintf("Preview bytes %d–%d · not collected evidence", p.next-int64(len(p.raw)), p.next))
	if v.err != "" {
		header += "\n" + m.paint(errorStyle, v.err)
	}
	return ansi.Wrap(header, max(1, m.width), "")
}

func (m ui) followContent() string {
	return m.followHeader() + "\n" + m.follow.previews[m.follow.stream].viewport.View()
}

func (m *frame) followKey(v tea.KeyPressMsg) tea.Cmd {
	u := m.current().follow
	f := u.follow
	p := &f.previews[f.stream]
	switch {
	case key.Matches(v, escape, quit):
		m.closeFollow()
		return nil
	case key.Matches(v, completionNext, completionPrevious):
		f.stream = 1 - f.stream
	case key.Matches(v, helpEnd):
		p.tail = true
		p.viewport.GotoBottom()
	case key.Matches(v, previous, next, pageUp, pageDown, helpHome):
		p.tail = false
		if key.Matches(v, helpHome) {
			p.viewport.GotoTop()
		} else {
			p.viewport, _ = p.viewport.Update(v)
		}
	}
	u.sizeFollow()
	return m.readFollow(m.active, u)
}
