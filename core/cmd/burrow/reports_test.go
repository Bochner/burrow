package main

import (
	"fmt"
	"image"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Bochner/burrow/core/launch"
	"github.com/Bochner/burrow/core/reports"
	"github.com/charmbracelet/x/ansi"
)

func TestReportsReadOnlyPresentation(t *testing.T) {
	zero := 0
	doc := reports.Document{Report: reports.Entry{ID: "report-123", CreatedAt: "2026-09-19T12:00:00Z", Path: "artifacts/survey.md", SHA256: strings.Repeat("a", 64), Metadata: reports.Metadata{Title: "Ubuntu host survey", Producer: "burrow survey ubuntu v1", Connection: "ubuntu-test", Host: "ubuntu.test", User: "tester", Port: 22, SourceRun: "source-123", Status: "exited", Complete: true, Exit: &zero}}, Text: "# Survey heading\n\n| Check | Observation |\n| --- | --- |\n| Memory | 4096 kB |\n\n```sh\nprintf 'observed'\n```\n\n" + strings.Repeat("A long report paragraph with readable observations. ", 80) + "\n\nLAST-OBSERVATION\n"}
	for _, plain := range []bool{false, true} {
		m := newFrame(launch.Info{Workspace: "/tmp/report-view"}, plain, launch.Options{})
		defer m.terminals.close()
		frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
		m.current().management.input.SetValue("unchanged draft")
		m.current().management.output = "previous output"
		m.current().focus = "saved"
		m.openReports("") // Discard I/O; feed the actual asynchronous result boundary.
		r := m.report
		m.acceptReports(m.active, reportsReady{view: r, generation: r.generation, entries: []reports.Entry{doc.Report}})
		for _, size := range []image.Point{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
			frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			screen := capturePresentation(t, m, fmt.Sprintf("reports-list-%dx%d-%t", size.X, size.Y, plain))
			if !strings.Contains(ansi.Strip(m.View().Content), "Ubuntu host survey") {
				t.Fatal("report selection lost")
			}
			if !plain {
				assertTextRole(t, screen, m.dialogBounds().Inset(1), "ubuntu.test", "#f5c2e7")
			}
		}
		m.reportsKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		m.acceptReports(m.active, reportsReady{view: r, generation: r.generation, document: &doc, body: reportBody(doc, r.viewport.Width(), plain)})
		for _, size := range []image.Point{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
			_, cmd := frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			if cmd != nil {
				frameEvent(m, cmd())
			}
			screen := capturePresentation(t, m, fmt.Sprintf("reports-read-%dx%d-%t", size.X, size.Y, plain))
			if !plain {
				assertTextRole(t, screen, m.dialogBounds().Inset(1), "ubuntu.test", "#f5c2e7")
				if size.Y >= 40 {
					assertTextRole(t, screen, m.dialogBounds().Inset(1), "Survey heading", blueColor)
				}
			}
			m.reportsKey(tea.KeyPressMsg{Code: tea.KeyEnd})
			if !strings.Contains(ansi.Strip(m.View().Content), "LAST-OBSERVATION") {
				t.Fatal("report End did not reach final observation")
			}
			m.reportsKey(tea.KeyPressMsg{Code: tea.KeyHome})
		}
		m.reportsKey(tea.KeyPressMsg{Code: tea.KeyEsc})
		m.acceptReports(m.active, reportsReady{view: r, generation: r.generation, entries: []reports.Entry{doc.Report}})
		m.reportsKey(tea.KeyPressMsg{Code: tea.KeyEsc})
		m.acceptReports(m.active, reportsReady{view: r, generation: r.generation, document: &doc, body: "late read"})
		if m.modal != "" || m.report != nil || m.current().focus != "saved" || m.current().management.input.Value() != "unchanged draft" || m.current().management.output != "previous output" {
			t.Fatal("report view changed previous context or accepted late result")
		}
		m.openReports("")
		r = m.report
		m.acceptReports(m.active, reportsReady{view: r, generation: r.generation})
		for _, size := range []image.Point{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
			frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			capturePresentation(t, m, fmt.Sprintf("reports-empty-%dx%d-%t", size.X, size.Y, plain))
			if !strings.Contains(m.reportsContent(), "No saved reports") {
				t.Fatal("missing empty state")
			}
		}
		m.acceptReports(m.active, reportsReady{view: r, generation: r.generation, err: fmt.Errorf("SHA256 verification failed")})
		frameEvent(m, tea.WindowSizeMsg{Width: 120, Height: 30})
		if !strings.Contains(m.reportsContent(), "SHA256") {
			t.Fatal("read failure lost through resize")
		}
	}
}

func TestReportMarkdownTerminalBoundary(t *testing.T) {
	source := "# Heading\n\nA **strong** and *emphasized* observation.\n\n| Name | Value |\n| --- | --- |\n| Memory | 4096 |\n\n```sh\nprintf 'hello'\n```\n\n"
	clean, err := renderReport(source, 60, false)
	if err != nil || strings.Contains(clean, "\\u001b") {
		t.Fatalf("renderer style escaped as content: %v %q", err, clean)
	}
	// Encoded controls appear only after the Markdown renderer decodes entities.
	for _, payload := range []string{"\x1b[2JRAW\x07", "&#27;[8mHIDDEN&#27;[0m", "`&#27;]52;c;ZW1wdHk=&#7;`", "[LINK](https://example.test)", "&#8238;BIDI", "\xff\r\bEND", "&#27;[" + strings.Repeat("1;", 128) + "mLONG", "&#27;Pdata&#27;\\", "&#27;[", "e\u0301 unicode"} {
		for _, plain := range []bool{false, true} {
			out, err := renderReport(source+payload, 60, plain)
			if err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{"Heading", "strong", "Memory", "4096", "hello"} {
				if !strings.Contains(ansi.Strip(out), text) {
					t.Fatalf("lost %s in %q", text, out)
				}
			}
			remaining, state := out, byte(0)
			for len(remaining) > 0 {
				seq, _, n, next := ansi.DecodeSequence(remaining, state, nil)
				if n <= 0 {
					t.Fatal("decoder failed to progress")
				}
				if strings.ContainsAny(seq, "\x1b\x07\r\b") && (plain || !reportSGR(seq)) {
					t.Fatalf("unsafe control passed: %q", seq)
				}
				state, remaining = next, remaining[n:]
			}
			if strings.ContainsRune(out, '\u202e') {
				t.Fatal("bidi control passed")
			}
		}
	}
	for _, seq := range []string{"\x1b[8m", "\x1b[5m", "\x1b[?1m", "\x1b[48;2;0;0;0m", "\x1b[38;2;0;0;0m", "\x1b[0;m"} {
		if reportSGR(seq) {
			t.Fatalf("unsafe style allowed %q", seq)
		}
	}
}
