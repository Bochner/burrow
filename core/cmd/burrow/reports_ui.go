package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	gansi "charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"
	"github.com/Bochner/burrow/core/reports"
	"github.com/charmbracelet/x/ansi"
)

// Reports is an independent overlay. The underlying workspace/tab/focus and
// Ctrl+L's editors remain untouched, including when an asynchronous read fails.
type reportView struct {
	entries    []reports.Entry
	cursor     int
	id         string
	document   *reports.Document
	viewport   viewport.Model
	generation uint64
	loading    bool
	err        string
}

type reportsReady struct {
	view       *reportView
	generation uint64
	entries    []reports.Entry
	document   *reports.Document
	body       string
	reflow     bool
	err        error
}

func (m *frame) openReports(id string) tea.Cmd {
	m.modal = "reports"
	m.report = &reportView{id: id, viewport: viewport.New()}
	return m.loadReports()
}

func (m *frame) loadReports() tea.Cmd {
	r := m.report
	r.generation++
	r.loading, r.err = true, ""
	gen, id, workspace, width, plain := r.generation, r.id, m.active, m.dialogBounds().Dx()-6, m.noColor
	return m.dispatch(workspace, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		result := reportsReady{view: r, generation: gen}
		if id == "" {
			result.entries, result.err = reports.List(ctx, workspace)
		} else {
			doc, err := reports.Read(ctx, workspace, id)
			result.err = err
			if err == nil {
				result.document = &doc
				result.body = reportBody(doc, width, plain)
			}
		}
		return result
	})
}

func (m *frame) resizeReports() tea.Cmd {
	if m.modal != "reports" || m.report == nil {
		return nil
	}
	r := m.report
	b := m.dialogBounds()
	r.viewport.SetWidth(max(1, b.Dx()-6))
	r.viewport.SetHeight(max(1, b.Dy()-8))
	if r.loading {
		return m.loadReports()
	}
	if r.err != "" {
		r.viewport.SetContent(ansi.Wrap(m.current().management.paint(errorStyle, r.err), r.viewport.Width(), ""))
		return nil
	}
	if r.document == nil {
		m.reportList()
		return nil
	}
	r.generation++
	gen, doc, width, plain := r.generation, *r.document, r.viewport.Width(), m.noColor
	return m.dispatch(m.active, func() tea.Msg {
		return reportsReady{view: r, generation: gen, document: &doc, body: reportBody(doc, width, plain), reflow: true}
	})
}

func (m *frame) acceptReports(workspace string, v reportsReady) tea.Cmd {
	if workspace != m.active || m.modal != "reports" || m.report != v.view || v.generation != m.report.generation {
		return nil
	}
	r := m.report
	r.loading, r.err = false, ""
	b := m.dialogBounds()
	r.viewport.SetWidth(max(1, b.Dx()-6))
	r.viewport.SetHeight(max(1, b.Dy()-8))
	if v.err != nil {
		r.document = nil
		r.err = "Report unavailable: " + safe(v.err.Error())
		r.viewport.GotoTop()
		return m.resizeReports()
	}
	if r.id == "" {
		r.entries, r.document = v.entries, nil
		r.cursor = min(r.cursor, max(0, len(r.entries)-1))
		m.reportList()
	} else {
		position := 0.0
		if v.reflow {
			position = r.viewport.ScrollPercent()
		}
		r.document = v.document
		r.viewport.SetContent(v.body)
		r.viewport.SetYOffset(int(position * float64(max(0, r.viewport.TotalLineCount()-r.viewport.Height()))))
	}
	return nil
}

func (m *frame) reportList() {
	r, u := m.report, m.current().management
	var body strings.Builder
	selectedLine, line := 0, 0
	for i, e := range r.entries {
		marker := "  "
		if i == r.cursor {
			marker, selectedLine = "› ", line
		}
		label, style := reportStatus(e)
		text := marker + u.paint(accent, safe(e.Title)) + " · " + u.paint(style, label) + "\n" +
			"  " + u.paint(accent, safe(e.Connection)) + " · " + u.paint(successStyle, safe(e.User)) + "@" + u.paint(hostStyle, safe(e.Host)) + ":" + u.paint(warningStyle, strconv.Itoa(e.Port)) + "\n" +
			"  " + u.paint(secondary, safe(e.CreatedAt)) + " · " + u.paint(accent, safe(e.ID)) + "\n\n"
		text = ansi.Wrap(text, max(1, r.viewport.Width()), "")
		body.WriteString(text)
		line += strings.Count(text, "\n")
	}
	if len(r.entries) == 0 {
		body.WriteString(u.paint(secondary, "No saved reports"))
	}
	r.viewport.SetContent(body.String())
	if selectedLine < r.viewport.YOffset() || selectedLine+3 >= r.viewport.YOffset()+r.viewport.Height() {
		r.viewport.SetYOffset(selectedLine)
	}
}

func (m *frame) reportsKey(v tea.KeyPressMsg) tea.Cmd {
	r := m.report
	if key.Matches(v, quit) {
		m.dismissForm()
		return nil
	}
	if key.Matches(v, escape) {
		if r.id != "" {
			r.id, r.document = "", nil
			return m.loadReports()
		}
		m.dismissForm()
		return nil
	}
	if v.String() == "r" {
		return m.loadReports()
	}
	if r.loading {
		return nil
	}
	if r.id == "" && r.err == "" {
		switch {
		case key.Matches(v, enter):
			if len(r.entries) > 0 {
				r.id = r.entries[r.cursor].ID
				return m.loadReports()
			}
		case key.Matches(v, previous, pageUp):
			r.cursor = max(0, r.cursor-1)
		case key.Matches(v, next, pageDown):
			r.cursor = min(max(0, len(r.entries)-1), r.cursor+1)
		case key.Matches(v, helpHome):
			r.cursor = 0
		case key.Matches(v, helpEnd):
			r.cursor = max(0, len(r.entries)-1)
		}
		m.reportList()
		return nil
	}
	if key.Matches(v, helpHome) {
		r.viewport.GotoTop()
		return nil
	}
	if key.Matches(v, helpEnd) {
		r.viewport.GotoBottom()
		return nil
	}
	var cmd tea.Cmd
	r.viewport, cmd = r.viewport.Update(v)
	return cmd
}

func (m *frame) reportsContent() string {
	u, r := m.current().management, m.report
	text := "Loading reports…"
	if r != nil && !r.loading {
		text = r.viewport.View()
	}
	return centered(u.paint(accent, "Reports"), m.dialogBounds().Dx()-6) + "\n\n" + text
}

func reportStatus(e reports.Entry) (string, lipgloss.Style) {
	if e.Error != "" {
		return "UNAVAILABLE", errorStyle
	}
	if !e.Complete {
		return "INCOMPLETE CAPTURE", warningStyle
	}
	if e.Exit == nil {
		return "COMPLETION UNKNOWN", warningStyle
	}
	if *e.Exit != 0 {
		return "CHECK FAILURES / UNAVAILABLE", warningStyle
	}
	return "CHECKS COMPLETED", successStyle
}

func reportBody(doc reports.Document, width int, plain bool) string {
	u := ui{noColor: plain}
	e := doc.Report
	status, style := reportStatus(e)
	field := func(label, value string, role lipgloss.Style) string {
		return u.paint(secondary, label+": ") + u.paint(role, safe(value))
	}
	header := field("Report", e.Title, accent) + "\n" +
		field("Connection", e.Connection, accent) + " · " + u.paint(successStyle, safe(e.User)) + "@" + u.paint(hostStyle, safe(e.Host)) + ":" + u.paint(warningStyle, strconv.Itoa(e.Port)) + "\n" +
		field("Capture", status, style) + " · " + field("Execution", e.Status, connectionStyle(e.Status))
	if e.Exit != nil {
		header += " · " + field("Exit", strconv.Itoa(*e.Exit), numberStyle)
	}
	header += "\n" + field("Collected", e.CreatedAt, secondary) + " · " + field("Producer", e.Producer, keywordStyle) + "\n" +
		field("ID", e.ID, accent) + " · " + field("Source run", e.SourceRun, accent) + "\n" +
		field("Artifact", e.Path, secondary) + "\n" + field("SHA256", e.SHA256, secondary) + " · " + field("Bytes", strconv.FormatInt(e.Size, 10), numberStyle) + "\n"
	if e.Detail != "" {
		header += field("Detail", e.Detail, warningStyle) + "\n"
	}
	body, err := renderReport(doc.Text, width, plain)
	if err != nil {
		body = u.paint(warningStyle, "Markdown rendering unavailable; showing escaped source") + "\n" + safeRunOutput([]byte(doc.Text), true)
	}
	return ansi.Wrap(header+"\n"+body, max(1, width), "")
}

func reportColor(style lipgloss.Style) string {
	r, g, b, _ := style.GetForeground().RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

func reportStyles() gansi.StyleConfig {
	primitive := func(s lipgloss.Style) gansi.StylePrimitive {
		color := reportColor(s)
		return gansi.StylePrimitive{Color: &color}
	}
	yes, indent, separator := true, uint(2), "│"
	styles := gansi.StyleConfig{
		Document: gansi.StyleBlock{StylePrimitive: primitive(pageStyle)},
		Heading:  gansi.StyleBlock{StylePrimitive: primitive(heading)},
		Strong:   primitive(accent),
		Code:     gansi.StyleBlock{StylePrimitive: primitive(successStyle)},
		CodeBlock: gansi.StyleCodeBlock{StyleBlock: gansi.StyleBlock{Indent: &indent}, Chroma: &gansi.Chroma{
			Text: primitive(pageStyle), Keyword: primitive(heading), Name: primitive(accent), LiteralString: primitive(successStyle), LiteralNumber: primitive(numberStyle), Comment: primitive(secondary), Error: primitive(errorStyle),
		}},
		Link: primitive(secondary), LinkText: primitive(heading),
		Item: gansi.StylePrimitive{BlockPrefix: "• "}, Enumeration: gansi.StylePrimitive{BlockPrefix: ". "},
		List:  gansi.StyleList{LevelIndent: 2},
		Table: gansi.StyleTable{ColumnSeparator: &separator},
	}
	styles.Heading.Bold, styles.Strong.Bold, styles.Emph.Italic = &yes, &yes, &yes
	styles.Heading.BlockSuffix = "\n"
	return styles
}

func renderReport(source string, width int, plain bool) (string, error) {
	renderer, err := glamour.NewTermRenderer(glamour.WithStyles(reportStyles()), glamour.WithChromaFormatter("terminal16m"), glamour.WithWordWrap(max(1, width)), glamour.WithTableWrap(true))
	if err != nil {
		return "", err
	}
	out, err := renderer.Render(safeRunOutput([]byte(source), true))
	if err != nil {
		return "", err
	}
	// Glamour decodes entities and emits OSC links. Only selected palette SGR
	// survives this final boundary. No URLs are opened and no content is run.
	var body strings.Builder
	state := byte(0)
	for len(out) > 0 {
		seq, _, n, nextState := ansi.DecodeSequence(out, state, nil)
		if n <= 0 {
			body.WriteString(safeRunOutput([]byte(out), true))
			break
		}
		state, out = nextState, out[n:]
		switch {
		case reportSGR(seq):
			if !plain {
				body.WriteString(seq)
			}
		case strings.HasPrefix(seq, "\x1b]8;"): // inert link text is retained
		default:
			body.WriteString(safeRunOutput([]byte(seq), true))
		}
	}
	if !plain {
		body.WriteString("\x1b[0m")
	}
	return body.String(), nil
}

func reportSGR(seq string) bool {
	if seq == "\x1b[m" {
		return true
	} // SGR with omitted parameters is reset.
	if !strings.HasPrefix(seq, "\x1b[") || !strings.HasSuffix(seq, "m") {
		return false
	}
	values := strings.Split(seq[2:len(seq)-1], ";")
	for i := 0; i < len(values); i++ {
		switch values[i] {
		case "0", "1", "3", "4", "22", "23", "24", "39":
		case "38":
			if i+4 >= len(values) || values[i+1] != "2" {
				return false
			}
			var rgb [3]int
			for j := range rgb {
				n, err := strconv.Atoi(values[i+2+j])
				if err != nil || n < 0 || n > 255 {
					return false
				}
				rgb[j] = n
			}
			color := fmt.Sprintf("#%02x%02x%02x", rgb[0], rgb[1], rgb[2])
			allowed := false
			for _, role := range []lipgloss.Style{pageStyle, heading, accent, successStyle, numberStyle, secondary, errorStyle} {
				if reportColor(role) == color {
					allowed = true
					break
				}
			}
			if !allowed {
				return false
			}
			i += 4
		default:
			return false
		}
	}
	return true
}
