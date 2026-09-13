package main

import (
	"context"
	"image"
	"os"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var copySelection = key.NewBinding(key.WithKeys("ctrl+c", "ctrl+shift+c"))

// An immutable visible-page snapshot: background commands and PTYs keep running.
type textSelection struct {
	hit             string
	bounds          image.Rectangle
	workspace, tab  string
	lines           []string
	start, end      image.Point
	dragging, moved bool
	notice          string
}

func (m *frame) selectionBounds() image.Rectangle {
	r := m.terminalBounds()
	if m.current().tab == "" || m.current().tab == "files" {
		r.Max.Y++
	}
	return r
}
func (m *frame) hasSelection() bool {
	s := m.selection
	return s != nil && s.moved && !m.tooSmall() && !m.mouseDisabled && m.modal == "" && !m.current().activeUI().help && s.workspace == m.active && s.tab == m.current().tab && s.bounds == m.selectionBounds()
}
func (m *frame) copyBounds() image.Rectangle {
	return image.Rect(m.width-8, m.height-1, m.width-2, m.height)
}
func (m *frame) selectionMouse(msg tea.MouseMsg, hit string) (bool, tea.Cmd) {
	if m.modal != "" || m.current().activeUI().help {
		m.selection = nil
		return false, nil
	}
	mouse := msg.Mouse()
	point := image.Pt(mouse.X, mouse.Y)
	switch v := msg.(type) {
	case tea.MouseClickMsg:
		if v.Button != tea.MouseLeft {
			m.selection = nil
			return false, nil
		}
		wasSelected := m.hasSelection()
		if wasSelected && point.In(m.copyBounds()) {
			return true, m.copySelection()
		}
		m.selection = nil
		r := m.selectionBounds()
		if wasSelected && point.In(r) {
			// The displayed snapshot may cover controls created by background output.
			return true, nil
		}
		// Alt+mouse remains available to applications running in the embedded PTY.
		if !point.In(r) || v.Mod.Contains(tea.ModAlt) || hit == "restart-cli" {
			return false, nil
		}
		s := &textSelection{hit: hit, bounds: r, workspace: m.active, tab: m.current().tab, start: point, end: point, dragging: true}
		rows := strings.Split(m.compositor().Render(), "\n")
		for y := r.Min.Y; y < r.Max.Y; y++ {
			s.lines = append(s.lines, ansi.Cut(rows[y], r.Min.X, r.Max.X))
		}
		m.selection = s
		if hit == "saved" {
			return true, nil
		}
		if m.current().tab != "" {
			m.current().focus = "terminal"
			return true, nil
		}
	case tea.MouseMotionMsg, tea.MouseReleaseMsg:
		s := m.selection
		if s == nil || !s.dragging {
			return false, nil
		}
		r := s.bounds
		s.end = image.Pt(max(r.Min.X, min(r.Max.X-1, point.X)), max(r.Min.Y, min(r.Max.Y-1, point.Y)))
		s.moved = s.moved || s.end != s.start
		if _, ok := msg.(tea.MouseReleaseMsg); ok {
			s.dragging = false
			if !s.moved && s.hit == "saved" {
				return true, m.activate(s.hit)
			}
		}
		return true, nil
	case tea.MouseWheelMsg:
		m.selection = nil
	}
	return false, nil
}

// Expand partial wide/combined characters to whole displayed graphemes.
func selectionColumns(line string, left, right int) (int, int) {
	column := 0
	for rest := ansi.Strip(line); len(rest) > 0; {
		cluster, width := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
		if column < left && left < column+width {
			left = column
		}
		if column < right && right < column+width {
			right = column + width
		}
		column += width
		rest = rest[len(cluster):]
	}
	return left, right
}
func (s *textSelection) span(y int) (int, int) {
	a, b := s.start.Sub(s.bounds.Min), s.end.Sub(s.bounds.Min)
	if a.Y > b.Y || (a.Y == b.Y && a.X > b.X) {
		a, b = b, a
	}
	if y < a.Y || y > b.Y {
		return 0, 0
	}
	left, right := 0, s.bounds.Dx()
	if y == a.Y {
		left = a.X
	}
	if y == b.Y {
		right = b.X + 1
	}
	return selectionColumns(s.lines[y], left, right)
}
func (s *textSelection) text() string {
	var rows []string
	for y, line := range s.lines {
		left, right := s.span(y)
		if right > left {
			rows = append(rows, strings.TrimRight(ansi.Strip(ansi.Cut(line, left, right)), " "))
		}
	}
	return strings.Join(rows, "\n")
}
func (m *frame) selectionView(base string) string {
	if !m.hasSelection() {
		return base
	}
	s := m.selection
	rows := strings.Split(base, "\n")
	for y, line := range s.lines {
		if m.noColor {
			line = ansi.Strip(line)
		}
		left, right := s.span(y)
		if right > left {
			// Reverse video also works in NO_COLOR; preserve the original text geometry.
			line = ansi.Cut(line, 0, left) + "\x1b[7m" + ansi.Strip(ansi.Cut(line, left, right)) + "\x1b[27m" + ansi.Cut(line, right, s.bounds.Dx())
		}
		at := s.bounds.Min.Y + y
		rows[at] = ansi.Cut(rows[at], 0, s.bounds.Min.X) + line + ansi.Cut(rows[at], s.bounds.Max.X, m.width)
	}
	footer := "Ctrl+C copy · Esc clear · selected snapshot"
	if s.notice != "" {
		footer = s.notice + " · Esc clear"
	}
	footer = fit(footer, m.width-8, 1)
	rows[m.height-1] = footer + strings.Repeat(" ", m.width-8-ansi.StringWidth(footer)) + "[Copy]  "
	return strings.Join(rows, "\n")
}

type clipboardResult struct {
	selection *textSelection
	notice    string
}

func (m *frame) copySelection() tea.Cmd {
	s := m.selection
	text := s.text()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.terminals.context, 2*time.Second)
		defer cancel()
		var commands [][]string
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			commands = append(commands, []string{"wl-copy"})
		}
		if os.Getenv("DISPLAY") != "" {
			commands = append(commands, []string{"xclip", "-selection", "clipboard"}, []string{"xsel", "--clipboard", "--input"})
		}
		for _, args := range commands {
			path, err := exec.LookPath(args[0])
			if err != nil {
				continue
			}
			cmd := exec.CommandContext(ctx, path, args[1:]...)
			cmd.Stdin = strings.NewReader(text)
			if cmd.Run() == nil {
				return clipboardResult{s, "Copied"}
			}
		}
		// OSC 52 has no success acknowledgement; do not claim that it copied.
		return tea.Sequence(tea.SetClipboard(text), func() tea.Msg { return clipboardResult{s, "Copy sent to terminal"} })()
	}
}
