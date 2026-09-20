package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Bochner/burrow/core/connection"
	"github.com/Bochner/burrow/core/launch"
	ptyhost "github.com/Bochner/burrow/core/terminal"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

func TestAuthenticationPopup(t *testing.T) {
	for _, plain := range []bool{false, true} {
		m := newFrame(launch.Info{Workspace: "/tmp/auth-popup"}, plain, launch.Options{})
		defer m.terminals.close()
		frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
		if m.startAuthentication([]string{"connect", "gateway", "example.test", "tester", "--yes"}) == nil {
			t.Fatal("authentication did not start commands")
		}
		defer m.attempt.cancel()
		before := m.authSpinner.View()
		_, tick := frameEvent(m, m.authSpinner.Tick())
		if tick == nil || m.authSpinner.View() == before {
			t.Fatal("Charm spinner did not animate")
		}
		for _, size := range []image.Point{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
			frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			screen := capturePresentation(t, m, fmt.Sprintf("auth-connecting-%dx%d-%t", size.X, size.Y, plain))
			bounds := m.dialogBounds()
			for row, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
				if strings.Contains(line, "Connecting…") && absInt(row-(bounds.Min.Y+bounds.Dy()/2)) > 1 {
					t.Fatal("connecting label is not vertically centered")
				}
			}
			if !strings.Contains(ansi.Strip(m.formText()), ansi.Strip(m.authSpinner.View())+" Connecting…") {
				t.Fatal("spinner missing to left of connecting label")
			}
			if !plain {
				assertTextRole(t, screen, bounds.Inset(1), "Connecting…", "#f9e2af")
			}
		}
		for _, prompt := range []string{"SSH password for tester@example.test:", "SSH key passphrase:"} {
			q := authQuestion{prompt: connection.Prompt{Text: prompt, Secret: true}, answer: make(chan []byte)}
			frameEvent(m, authQuestionReady{m.attempt, q})
			if _, cmd := frameEvent(m, m.authSpinner.Tick()); cmd != nil {
				t.Fatal("spinner kept ticking during password entry")
			}
			for _, value := range []string{"", "synthetic-é password"} {
				frameEvent(m, tea.PasteMsg{Content: value})
				for _, size := range []image.Point{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
					frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
					capturePresentation(t, m, fmt.Sprintf("auth-entry-%dx%d-%t-%d-%d", size.X, size.Y, plain, len(prompt), len(value)))
					view := ansi.Strip(m.View().Content)
					if !strings.Contains(view, prompt) || !strings.Contains(view, value) || !strings.Contains(view, "Enter submit") {
						t.Fatal("credential field or controls hidden")
					}
					if value != "" {
						bounds := m.dialogBounds()
						for row, line := range strings.Split(view, "\n") {
							if strings.Contains(line, value) && absInt(row-(bounds.Min.Y+bounds.Dy()/2)) > 1 {
								t.Fatal("password entry is not near dialog center")
							}
						}
					}
					if plain && m.View().Cursor == nil {
						t.Fatal("NO_COLOR input lost caret")
					}
				}
			}
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
			if m.form != nil || m.question != nil || strings.Contains(m.View().Content, "synthetic-é password") || !strings.Contains(m.View().Content, "Connecting…") {
				t.Fatal("submitted credential remained on screen")
			}
			if _, cmd := frameEvent(m, m.authSpinner.Tick()); cmd == nil {
				t.Fatal("spinner failed to resume after submission")
			}
		}
		q := authQuestion{prompt: connection.Prompt{Text: "SSH password:", Secret: true}, answer: make(chan []byte)}
		frameEvent(m, authQuestionReady{m.attempt, q})
		frameEvent(m, tea.PasteMsg{Content: "cancelled-secret"})
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
		if m.attempt.ctx.Err() == nil || m.modal != "" || strings.Contains(m.View().Content, "cancelled-secret") {
			t.Fatal("Escape did not cancel connection")
		}
		frameEvent(m, authQuestionReady{m.attempt, q})
		if m.modal != "" {
			t.Fatal("late prompt reopened cancelled connection")
		}
		if _, cmd := frameEvent(m, m.authSpinner.Tick()); cmd != nil {
			t.Fatal("dismissed spinner kept ticking")
		}
		m.startAuthentication([]string{"connect", "gateway", "example.test", "--user", "tester", "--password", "--yes"})
		defer m.attempt.cancel()
		frameEvent(m, authQuestionReady{m.attempt, q})
		frameEvent(m, tea.PasteMsg{Content: "explicit-password-hidden-canary"})
		if strings.Contains(m.View().Content, "explicit-password-hidden-canary") || !strings.Contains(m.View().Content, "SSH password") {
			t.Fatal("explicit --password did not select hidden entry")
		}
	}
	f := promptForm(connection.Prompt{Text: "SSH password:", Secret: true}, false)
	f.WithWidth(60)
	f.GetFocusedField().Focus()
	f.Update(tea.PasteMsg{Content: "synthetic-cli-secret"})
	if strings.Contains(f.View(), "synthetic-cli-secret") || f.GetFocusedField().GetValue() != "synthetic-cli-secret" {
		t.Fatal("CLI prompt visibility changed")
	}
}

func TestChainCommandPresentation(t *testing.T) {
	for _, plain := range []bool{false, true} {
		m := newFrame(launch.Info{Workspace: "/tmp/chain-ui"}, plain, launch.Options{})
		defer m.terminals.close()
		frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
		u := m.current().management
		u.input.SetValue("chain connect gateway 192.168.10.50 ")
		if !strings.Contains(strings.Join(u.suggestions(), "\n"), "--user ") {
			t.Fatal("chain completion omitted explicit --user")
		}
		u.input.SetValue("chain connect gateway 192.168.10.50 --user alice ")
		if !strings.Contains(strings.Join(u.suggestions(), "\n"), "--password") {
			t.Fatal("chain completion omitted --password")
		}
		u.connections = []connection.State{{Name: "gateway", State: "connected", Generation: "owner", Proxy: connection.Tunnel{ID: "gateway/" + strings.Repeat("b", 32), State: "listening"}}}
		u.tunnelError = ""
		u.tunnels = []connection.Tunnel{{ID: "gateway/" + strings.Repeat("a", 32), Connection: "gateway", State: "listening", Direction: "L", Destination: "localhost:8080"}}
		u.input.SetValue("chain http gateway ")
		found := false
		for _, suggestion := range u.suggestions() {
			if strings.Contains(suggestion, u.tunnels[0].ID) && strings.HasPrefix(suggestion, "chain http gateway ") {
				found = true
			}
		}
		if !found {
			t.Fatal("chain completion lost live tunnel identity")
		}
		line := "chain http gateway " + u.tunnels[0].ID + " http://localhost:8080/"
		frameEvent(m, tea.PasteMsg{Content: line})
		// Recap arrives through the existing async review boundary.
		m.reviewText = "HTTP through existing tunnel\nConnection: gateway\nTunnel: " + u.tunnels[0].ID + "\nURL: http://localhost:8080/\nLimit: 8 seconds; 1 MiB response"
		m.modal = "review"
		m.formTitle = "Proceed?"
		m.form = confirmForm("Proceed?", m.reviewText, "Proceed", "Cancel")
		m.sizeForm()
		for _, size := range []image.Point{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
			frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			screen := capturePresentation(t, m, fmt.Sprintf("chain-review-%dx%d-%t", size.X, size.Y, plain))
			if !strings.Contains(ansi.Strip(m.View().Content), "Proceed?") {
				t.Fatal("chain recap lost approval at narrow size")
			}
			if size.X >= 160 && !plain {
				assertTextRole(t, screen, m.dialogBounds(), "Connection:", lavenderColor)
				assertTextRole(t, screen, m.dialogBounds(), "gateway", lavenderColor)
				assertTextRole(t, screen, m.dialogBounds(), "http://localhost:8080/", "#f5c2e7")
			}
		}
		if got := u.syntax(line, false); ansi.Strip(got) != line {
			t.Fatal("chain highlighting changed command")
		} else if !plain && !strings.Contains(got, "\x1b[") {
			t.Fatal("chain command lacks semantic colors")
		}
		m.current().cli = &cliTab{screen: ptyhost.Snapshot{Screen: "h0v3l> ", Visible: true}}
		m.current().tab, m.current().focus, m.modal = "hovel", "terminal", ""
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		m.attempt = &authAttempt{path: m.active, ctx: ctx, cancel: cancel, chain: true, label: "gateway · alice@192.168.10.50"}
		frameEvent(m, authQuestionReady{m.attempt, authQuestion{prompt: connection.Prompt{Text: "SSH password", Secret: true}, answer: make(chan []byte)}})
		frameEvent(m, tea.PasteMsg{Content: "chain-hidden-canary"})
		for _, size := range []image.Point{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
			frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			capturePresentation(t, m, fmt.Sprintf("chain-password-%dx%d-%t", size.X, size.Y, plain))
			view := ansi.Strip(m.View().Content)
			if strings.Contains(view, "chain-hidden-canary") || !strings.Contains(view, "SSH password") || (plain && m.View().Cursor == nil) {
				t.Fatalf("chain password visibility or keyboard access changed (%v, plain=%t): %s", size, plain, view)
			}
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.modal != "" || m.current().tab != "hovel" || strings.Contains(m.View().Content, "chain-hidden-canary") {
			t.Fatal("chain password submission did not return to Hovel")
		}
	}
}

func TestLiveOutputNavigation(t *testing.T) {
	for _, plain := range []bool{false, true} {
		m := newFrame(launch.Info{Workspace: "/tmp/live-view"}, plain, launch.Options{})
		defer m.terminals.close()
		frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
		frameEvent(m, tea.PasteMsg{Content: "run follow retained-1"})
		_, cmd := frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd == nil || !strings.Contains(ansi.Strip(m.View().Content), "LIVE OUTPUT") {
			t.Fatal("follow did not open a responsive live viewer", ansi.Strip(m.View().Content))
		}
		for _, size := range []image.Point{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
			frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			screen := capturePresentation(t, m, fmt.Sprintf("live-empty-%dx%d-%t", size.X, size.Y, plain))
			for _, text := range []string{"LIVE OUTPUT", "retained-1", "stdout", "Connecting"} {
				if !strings.Contains(ansi.Strip(m.View().Content), text) {
					t.Fatal("missing viewer identity or truthful initial state", text)
				}
			}
			if !plain {
				assertTextRole(t, screen, image.Rect(0, 0, size.X, size.Y), "LIVE OUTPUT", blueColor)
			}
		}
		// Presentation fixture only; follow_lab exercises these observations
		// through real SSH, capture failures and independent terminal processes.
		view := m.current().follow.follow
		exit := 0
		chunk := connection.RunOutput{Data: base64.StdEncoding.EncodeToString([]byte("partial")), NextOffset: 7,
			State: "exited", RemoteExit: &exit, Budget: 7, Stored: 7, Received: 14,
			OutputError: "output storage budget exceeded; partial evidence available"}
		frameEvent(m, m.dispatch(m.active, func() tea.Msg { return followRead{view: view, chunk: chunk} })())
		for _, size := range []image.Point{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
			frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			screen := capturePresentation(t, m, fmt.Sprintf("live-incomplete-%dx%d-%t", size.X, size.Y, plain))
			for _, text := range []string{"INCOMPLETE", "exit 0", "partial", "7/7 B"} {
				if !strings.Contains(screen.String(), text) {
					t.Fatal("live capture outcome hidden", text)
				}
			}
			if !plain {
				bounds := image.Rect(0, 0, size.X, size.Y)
				for text, color := range map[string]string{"retained-1": "#b4befe", "stdout": "#94e2d5", "INCOMPLETE": "#f38ba8", "7/7 B": "#fab387"} {
					assertTextRole(t, screen, bounds, text, color)
				}
			}
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
		if !strings.Contains(ansi.Strip(m.View().Content), "stderr") {
			t.Fatal("stream switch unavailable while read pending")
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyF1})
		if !strings.Contains(ansi.Strip(m.View().Content), "LIVE OUTPUT HELP") {
			t.Fatal("contextual live help missing")
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyF6})
		frameEvent(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		if strings.Contains(ansi.Strip(m.View().Content), "LIVE OUTPUT") || !strings.Contains(ansi.Strip(m.View().Content), "COMMAND OUTPUT") {
			t.Fatal("closing viewer did not return to management")
		}
	}
}

func TestRunReviewPresentation(t *testing.T) {
	for _, plain := range []bool{false, true} {
		m := newFrame(launch.Info{Workspace: "/tmp/run-ui"}, plain, launch.Options{})
		defer m.terminals.close()
		frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
		m.current().management.input.SetValue("run launch retained-1")
		_, dispatch := frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
		if dispatch == nil {
			t.Fatal("run launch did not dispatch")
		}
		frameEvent(m, dispatch())
		if m.modal != "review" {
			t.Fatal("run launch did not enter the shared review flow", m.modal)
		}
		m.reviewText = "Launch run retained-1 on gateway.\nCommand: /bin/echo 'untrusted \\u001b]52;c;data'\nOutput budget: 268435456 bytes per stream."
		m.setForm("review", "Review exact target", confirmForm("Proceed?", "", "Proceed", "Cancel"))
		for _, size := range []image.Point{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
			frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			screen := capturePresentation(t, m, fmt.Sprintf("run-review-%dx%d-%t", size.X, size.Y, plain))
			if !strings.Contains(ansi.Strip(m.View().Content), "retained-1") {
				t.Fatal("run identity hidden during review")
			}
			if !plain && size.X >= 120 {
				assertTextRole(t, screen, m.dialogBounds().Inset(1), "Command:", "#b4befe")
			}
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
		if m.modal != "" {
			t.Fatal("run review could not be cancelled")
		}
		frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
		for _, verb := range []string{"prepare", "now", "launch", "inspect", "cancel", "collect", "close", "output"} {
			m.reviewText = "Command: run " + verb + " retained-1"
			m.setForm("review", "Review command", confirmForm("Proceed?", "", "Proceed", "Cancel"))
			screen := capturePresentation(t, m, "run-command-"+verb+fmt.Sprint(plain))
			if !plain {
				assertTextRole(t, screen, m.dialogBounds(), "run", blueColor)
				assertTextRole(t, screen, m.dialogBounds(), verb, blueColor)
				assertTextRole(t, screen, m.dialogBounds(), "retained-1", lavenderColor)
			}
			m.dismissForm()
		}
		m.current().management.input.Reset()
		frameEvent(m, tea.PasteMsg{Content: "run prepare gateway --script check.sh --mode stag"})
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
		if !strings.Contains(m.current().management.input.Value(), "--mode stage") {
			t.Fatal("script mode completion unavailable")
		}
		m.reviewText = "Run: retained-1\nScript: /uploads/check.sh\nInterpreter: /bin/sh\nMode: stage\nScript snapshot: 42 bytes; SHA256 abc\nInput snapshot: 256 bytes; SHA256 def\nExecution timeout: 30s; requests cancellation\nStaged script: /tmp/burrow-script.X/script\nKeep: retain staged files"
		m.setForm("review", "Review script", confirmForm("Proceed?", "", "Proceed", "Cancel"))
		for _, size := range []image.Point{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
			frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			screen := capturePresentation(t, m, fmt.Sprintf("script-review-%dx%d-%t", size.X, size.Y, plain))
			if !strings.Contains(ansi.Strip(m.View().Content), "retained-1") {
				t.Fatal("script review identity hidden")
			}
			if !plain && size.X >= 120 {
				assertTextRole(t, screen, m.dialogBounds(), "/uploads/check.sh", "#a6adc8")
				assertTextRole(t, screen, m.dialogBounds(), "/bin/sh", "#94e2d5")
				assertTextRole(t, screen, m.dialogBounds(), "stage", "#cba6f7")
				assertTextRole(t, screen, m.dialogBounds(), "42 bytes", "#fab387")
				assertTextRole(t, screen, m.dialogBounds(), "256 bytes", "#fab387")
				assertTextRole(t, screen, m.dialogBounds(), "30s", "#fab387")
			}
		}
		m.dismissForm()
		frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
		frameEvent(m, connectionResult{result: map[string]string{
			"launchRunID": "execution-1", "outputError": "capture failed", "auditError": "audit incomplete", "cleanupError": "cleanup refused", "cancellation": "unconfirmed; no signal", "state": "transport-or-completion-unknown", "stageCleanup": "failed; unrelated files preserved", "timeout": "30s",
		}})
		screen := capturePresentation(t, m, fmt.Sprint("run-result-", plain))
		for value, hex := range map[string]string{"execution-1": lavenderColor, "capture failed": "#f38ba8", "audit incomplete": "#f38ba8", "cleanup refused": "#f38ba8", "unconfirmed; no signal": "#f9e2af", "transport-or-completion-unknown": "#f9e2af", "failed; unrelated files preserved": "#f38ba8", "30s": "#fab387"} {
			if plain {
				if !strings.Contains(m.View().Content, value) {
					t.Fatal("NO_COLOR hid run result", value)
				}
			} else {
				assertTextRole(t, screen, m.selectionBounds(), value, hex)
			}
		}
	}
}

func TestLocalRunPresentation(t *testing.T) {
	for _, plain := range []bool{false, true} {
		m := newFrame(launch.Info{Workspace: "/tmp/local-ui"}, plain, launch.Options{})
		defer m.terminals.close()
		frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
		frameEvent(m, tea.PasteMsg{Content: "run now gateway --loc"})
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
		if !strings.Contains(m.current().management.input.Value(), "--local ") {
			t.Fatal("local execution flag completion unavailable")
		}
		for _, suggestion := range connection.CommandSuggestions("run now gateway --local --mode ", nil) {
			if strings.Contains(suggestion, "stage") {
				t.Fatal("local completion offers unsupported remote staging", suggestion)
			}
		}
		m.reviewText = "Run: retained-local\nExecution: local (on the daemon host)\nConnection: gateway\nCommand: /bin/sh /uploads/tool.sh\nWorking directory: /tmp/local-ui\nSocket: /tmp/local-ui/master\nSSH config: /tmp/local-ui/ssh_config"
		m.setForm("review", "Review local run", confirmForm("Proceed?", "", "Proceed", "Cancel"))
		for _, size := range []image.Point{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
			frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			screen := capturePresentation(t, m, fmt.Sprintf("local-review-%dx%d-%t", size.X, size.Y, plain))
			if !strings.Contains(ansi.Strip(m.View().Content), "local (on the daemon host)") {
				t.Fatal("local execution location hidden")
			}
			if !plain && size.X >= 120 {
				assertTextRole(t, screen, m.dialogBounds(), "local (on the daemon host)", "#cba6f7")
			}
		}
		m.dismissForm()
		frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
		for _, state := range []string{"local-start-failed", "local-signaled"} {
			frameEvent(m, connectionResult{result: map[string]string{"state": state, "cancellation": "local-group-terminated; remote termination unconfirmed"}})
			screen := capturePresentation(t, m, fmt.Sprintf("local-result-%s-%t", state, plain))
			if plain {
				if !strings.Contains(m.View().Content, state) || !strings.Contains(m.View().Content, "remote termination unconfirmed") {
					t.Fatal("NO_COLOR hid local failure or cancellation uncertainty")
				}
			} else {
				assertTextRole(t, screen, m.selectionBounds(), state, "#f38ba8")
				assertTextRole(t, screen, m.selectionBounds(), "local-group-terminated; remote termination unconfirmed", "#f9e2af")
			}
		}
		u := m.current().management
		u.width = 80
		exit := 3
		u.follow = &runView{id: "retained-local"}
		u.follow.status[0] = connection.RunOutput{Execution: "local", LocalExit: &exit, State: "exited", OutputComplete: true}
		if text := ansi.Strip(u.followHeader()); !strings.Contains(text, "local exit 3") || strings.Contains(text, "remote exit") {
			t.Fatal("viewer mislabels the local result", text)
		}
	}
}

func TestDownloadReviewAndProgress(t *testing.T) {
	bar := newDownloadBar()
	bar.SetWidth(12)
	for _, check := range []struct {
		percent float64
		color   string
	}{{.25, "#f38ba8"}, {.75, "#f9e2af"}, {1, "#a6e3a1"}} {
		screen := vt.NewEmulator(12, 1)
		screen.Write([]byte(bar.ViewAs(check.percent)))
		if !colorMatches(screen.CellAt(0, 0).Style.Fg, lipgloss.Color(check.color)) {
			t.Fatalf("progress %.0f%% did not use %s", check.percent*100, check.color)
		}
		screen.Close()
	}
	plainBar := ansi.Strip(bar.ViewAs(.5))
	if !strings.Contains(plainBar, "━") || strings.ContainsAny(plainBar, "█▌░") {
		t.Fatal("progress bar is not a continuous line", plainBar)
	}
	m := newFrame(launch.Info{Workspace: "/tmp/download-ui"}, true, launch.Options{})
	defer m.terminals.close()
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	s := connection.State{Name: "gateway", State: "connected", Generation: "generation", Creation: "creation"}
	m.updateManagement(m.active, connectionList{states: []connection.State{s}})
	m.openFileTab("gateway")
	u := m.current().file
	u.busy = false
	u.files.remote = "/remote"
	u.files.download = "/tmp/download-ui/burrow-files/downloads"
	u.input.SetValue("get 'α file.txt'")
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.modal != "review" {
		t.Fatal("get did not open download review", m.modal, u.output)
	}
	plan := connection.DownloadPlan{Owner: s, Files: []connection.DownloadFile{{Source: "/remote/α file.txt", Destination: "/downloads/α file.txt", Size: 100, Existing: "reviewed", State: "pending"}}}
	deliver := func(msg tea.Msg) {
		t.Helper()
		cmd := m.dispatch(m.active, func() tea.Msg { return msg })
		frameEvent(m, cmd())
	}
	deliver(downloadReviewReady{m.inputEpoch, plan, nil})
	if text := ansi.Strip(m.View().Content); !strings.Contains(text, "Download 1 file to /downloads/α file.txt?") {
		t.Fatal("download approval omits resolved destination", text)
	}
	recap := ansi.Strip(m.downloadRecap(70))
	for _, part := range []string{"α file.txt", "100 B", "1 file", "OVERWRITE", "Replace"} {
		if !strings.Contains(recap, part) {
			t.Fatal("missing compact recap", part, recap)
		}
	}
	if strings.Contains(recap, "/downloads/") || strings.Contains(recap, "Source:") {
		t.Fatal("recap repeats known paths", recap)
	}
	plan.Operation = "put"
	m.downloadPlan = &plan
	if recap := ansi.Strip(m.downloadRecap(70)); !strings.Contains(recap, "/downloads/α file.txt") {
		t.Fatal("upload recap omits full remote destination", recap)
	}
	plan.Operation = "get"
	m.downloadPlan = &plan
	m.noColor, m.current().management.noColor, u.noColor = false, false, false
	screen := capturePresentation(t, m, "download-review-color")
	assertTextRole(t, screen, m.dialogBounds(), "α file.txt", lavenderColor)
	assertTextRole(t, screen, m.dialogBounds(), "100 B", "#fab387")
	assertTextRole(t, screen, m.dialogBounds(), "Replace", "#f9e2af")
	m.noColor, m.current().management.noColor, u.noColor = true, true, true
	for _, size := range []image.Point{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
		frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
		capturePresentation(t, m, fmt.Sprintf("download-review-%dx%d", size.X, size.Y))
	}
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.downloadPlan != nil {
		t.Fatal("cancelled review retained executable plan")
	}
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	u.fileCommand([]string{"history"})
	u.outputOffset = 3
	u.input.SetValue("downloads")
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !u.files.historyView || m.modal != "downloads" || u.outputOffset != 3 {
		t.Fatal("popup changed underlying history")
	}
	for _, op := range []string{"get", "mget", "put"} {
		u.fileCommand([]string{"help", op})
		if !u.help {
			t.Fatal("missing contextual help", op)
		}
		u.help = false
	}
	u.input.SetValue("mget '*.txt' sub")
	_, side, _, _, dirs := u.fileCompletionContext()
	if side != "download" || !dirs {
		t.Fatal("batch destination needs local directory completion")
	}
	u.input.SetValue("put 'α file.txt'")
	_, side, _, _, dirs = u.fileCompletionContext()
	if side != "upload" || dirs {
		t.Fatal("put source needs upload-area file completion")
	}
	u.input.SetValue("put 'α file.txt' remote")
	_, side, _, _, _ = u.fileCompletionContext()
	if side != "remote" {
		t.Fatal("put destination needs remote completion")
	}
	u.input.Reset()
	file := plan.Files[0]
	file.State = "failed"
	file.Bytes = 25
	file.Partial = "/downloads/.burrow-partial-check"
	file.Detail = "cancelled replacement"
	eta := 3.0
	d := connection.Download{ID: "download-check", Plan: plan, Files: []connection.DownloadFile{file}, State: "partial", Bytes: 25, Elapsed: 2, AverageRate: 12.5, ETA: &eta}
	deliver(downloadsReady{connection.Downloads{Records: []connection.Download{d}}, nil})
	text := ansi.Strip(m.View().Content)
	for _, part := range []string{"PARTIAL", "Overall progress", "Downloading α file.txt", "25/100 B", "0:00:02", "ETA 0:00:03"} {
		if !strings.Contains(text, part) {
			t.Fatal("missing measured presentation", part, text)
		}
	}
	if strings.Contains(m.View().Content, "\x1b[") {
		t.Fatal("NO_COLOR leaked ANSI")
	}
	for _, size := range []image.Point{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
		frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
		pw, ph := helpSize(size.X, size.Y)
		if b := m.dialogBounds(); b.Dx() != pw || b.Dy() != ph {
			t.Fatal("progress popup differs from help size", b)
		}
		capturePresentation(t, m, fmt.Sprintf("download-partial-%dx%d", size.X, size.Y))
	}
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	m.noColor, m.current().management.noColor, u.noColor = false, false, false
	screen = capturePresentation(t, m, "download-partial-color")
	assertTextRole(t, screen, image.Rect(0, 0, 160, 40), "PARTIAL", "#f38ba8")
	assertTextRole(t, screen, image.Rect(0, 0, 160, 40), "25/100 B", "#fab387")
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.modal != "" || !u.files.historyView || u.outputOffset != 3 {
		t.Fatal("Escape failed to restore underlying view")
	}
	u.input.SetValue("draft preserved")
	deliver(downloadsReady{connection.Downloads{Records: []connection.Download{d}}, nil})
	deliver(downloadStarted{mode: u.files})
	if m.modal != "" || u.input.Value() != "draft preserved" {
		t.Fatal("late update reopened popup or replaced draft")
	}
	u.input.SetValue("downloads")
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.modal != "downloads" || !strings.Contains(m.View().Content, "PARTIAL") {
		t.Fatal("downloads did not reopen retained progress")
	}
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	u.input.SetValue("get file")
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	deliver(downloadReviewReady{m.inputEpoch, plan, nil})
	button := image.Pt(-1, -1)
	compositor := m.compositor()
	for y := 0; y < m.height; y++ {
		for x := 0; x < m.width; x++ {
			if compositor.Hit(x, y).ID() == "confirm-accept" {
				button = image.Pt(x, y)
			}
		}
	}
	if button.X < 0 {
		t.Fatal("download has no mouse approval target")
	}
	frameEvent(m, tea.MouseClickMsg{X: button.X, Y: button.Y, Button: tea.MouseLeft})
	if m.downloadPlan != nil || m.modal != "downloads" {
		t.Fatal("mouse approval did not submit review")
	}
	deliver(downloadStarted{mode: u.files, err: fmt.Errorf("source changed")})
	if m.modal != "" || !strings.Contains(m.View().Content, "REFUSED: source changed") {
		t.Fatal("start failure hidden behind progress popup")
	}
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	frameEvent(m, tea.KeyPressMsg{Code: 'b', Mod: tea.ModAlt})
	m.current().management.input.SetValue("downloads")
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.modal != "downloads" {
		t.Fatal("management downloads did not open popup")
	}
}

func TestDownloadPopupRecapAndScroll(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/download-popup"}, false, launch.Options{})
	defer m.terminals.close()
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	m.downloadPlan = &connection.DownloadPlan{Files: []connection.DownloadFile{
		{Source: "/remote/first.txt", Destination: "/local/renamed.txt", Size: 1024},
		{Source: "/remote/second.txt", Destination: "/local/second.txt", Size: -1},
	}}
	m.setForm("review", "Download recap", confirmForm("Download these files?", "", "Download", "Cancel"))
	for _, size := range []image.Point{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
		frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
		capturePresentation(t, m, fmt.Sprintf("download-batch-recap-%dx%d", size.X, size.Y))
		if !strings.Contains(ansi.Strip(m.View().Content), "Download these files?") {
			t.Fatal("batch confirmation clipped")
		}
	}
	text := ansi.Strip(m.downloadRecap(70))
	for _, part := range []string{"2 files", "1.0 KiB + unknown total", "first.txt → renamed.txt", "Unknown"} {
		if !strings.Contains(text, part) {
			t.Fatal("missing batch recap field", part, text)
		}
	}
	if strings.Contains(text, "OVERWRITE") {
		t.Fatal("unnecessary overwrite column")
	}
	m.dismissForm()
	m.openDownloads()
	u := &m.current().management
	u.downloadObserved = true
	for _, size := range []image.Point{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
		frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
		capturePresentation(t, m, fmt.Sprintf("download-empty-popup-%dx%d", size.X, size.Y))
	}
	for i := 0; i < 12; i++ {
		u.downloads.Records = append(u.downloads.Records, connection.Download{ID: fmt.Sprintf("transfer-%02d", i), State: "complete"})
	}
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnd})
	if !strings.Contains(m.downloadsViewport().View(), "transfer-00") {
		t.Fatal("End did not reveal old outcomes")
	}
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyHome})
	if m.modalOffset != 0 || !strings.Contains(m.downloadsViewport().View(), "transfer-11") {
		t.Fatal("Home did not return to newest transfer")
	}
	frameEvent(m, tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	if m.modalOffset == 0 {
		t.Fatal("popup wheel did not scroll")
	}
}

func TestProfileEditorValidation(t *testing.T) {
	dir := t.TempDir()
	profile := connection.Profile{Name: "gateway", Host: "example.test", User: "tester", Port: 22}
	original, _ := json.Marshal(profile)
	edit := &profileEdit{profile: profile, collection: connection.Collection{Path: "/tmp/profiles.json", Revision: strings.Repeat("a", 64)}, directory: dir, original: original}
	write := func(data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "connection.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(original)
	if args, err := editedProfileArgs("/tmp/edit-workspace", edit); err != nil || args != nil {
		t.Fatal(args, err)
	}
	profile.Port = 2222
	changed, _ := json.Marshal(profile)
	write(changed)
	args, err := editedProfileArgs("/tmp/edit-workspace", edit)
	if err != nil {
		t.Fatal(err)
	}
	line := connection.CommandLine(args)
	for _, part := range []string{"profile edit gateway", "--port 2222", "--revision " + strings.Repeat("a", 64), "--collection /tmp/profiles.json", "--yes"} {
		if !strings.Contains(line, part) {
			t.Fatal(line)
		}
	}
	for _, data := range []string{`{}`, `{"name":"other"}`, `{"name":"gateway","password":"secret"}`, `{"name":"gateway","host":"example.test","user":"tester","port":70000}`, string(changed) + ` {}`, strings.Repeat("x", 65537)} {
		write([]byte(data))
		if _, err := editedProfileArgs("/tmp/edit-workspace", edit); err == nil {
			t.Fatal("accepted invalid edit")
		}
		if _, err := os.Stat(filepath.Join(dir, "connection.json")); err != nil {
			t.Fatal("failed edit was lost")
		}
	}
	for _, want := range []string{"nomodeline", "noexrc", "noloadplugins", "noswapfile", "noundofile", "conceallevel=0", "highlight jsonKeyword", "highlight jsonString"} {
		if !strings.Contains(profileVimrc(false), want) {
			t.Fatal("missing Vim policy", want)
		}
	}
	if strings.Contains(profileVimrc(true), "guifg") || !strings.Contains(profileVimrc(true), "syntax off") {
		t.Fatal("Vim NO_COLOR policy")
	}
}

func TestFilePermissionAndTypeColors(t *testing.T) {
	mode := "-rwxr-Sr-t"
	for _, plain := range []bool{false, true} {
		u := ui{noColor: plain}
		s := vt.NewEmulator(40, 2)
		defer s.Close()
		s.Write([]byte(u.filePermissions(mode)))
		if !strings.Contains(s.String(), mode) {
			t.Fatal("permission text changed", s.String())
		}
		if !plain {
			for i, want := range []string{"#a6e3a1", "#f9e2af", "#f38ba8", "#a6e3a1", "#f9e2af", subtextColor, "#cba6f7", "#f9e2af", subtextColor, "#cba6f7"} {
				if !colorMatches(s.CellAt(i, 0).Style.Fg, lipgloss.Color(want)) {
					t.Fatalf("permission %d lost its role", i)
				}
			}
		}
		for _, entry := range []connection.FileEntry{{Name: "folder", Permissions: "drwxr-xr-x", Directory: true}, {Name: "run", Permissions: "-rwxr-xr-x"}, {Name: "alias", Permissions: "lrwxrwxrwx", Link: "folder"}, {Name: "broken", Permissions: "lrwxrwxrwx", Link: "missing", Error: "broken link"}, {Name: "pipe", Permissions: "prw-------"}, {Name: "socket", Permissions: "srw-------"}, {Name: "device", Permissions: "crw-------"}} {
			text := u.fileName(entry)
			if !strings.Contains(ansi.Strip(text), entry.Name) {
				t.Fatal(text)
			}
			if plain && strings.Contains(text, "\x1b") {
				t.Fatal("color leaked into NO_COLOR")
			}
			if !plain {
				v := vt.NewEmulator(60, 1)
				v.Write([]byte(text))
				if !colorMatches(v.CellAt(0, 0).Style.Fg, fileKindStyle(entry).GetForeground()) {
					t.Fatal("file kind color lost", entry.Name)
				}
				v.Close()
			}
		}
	}
}

func TestFileCompletionAndRetainedMetadata(t *testing.T) {
	u := newUI(launch.Info{Workspace: "/tmp/file-completion"}, true)
	u.files = &fileMode{remote: "/home/tester", cache: map[string]fileObservation{}, listing: connection.FileListing{Notice: "Reduced metadata: numeric IDs"}}
	for _, test := range []struct{ line, dir, prefix string }{
		{"cd 'sub ", "/home/tester", "sub "},
		{"cd \"sub ", "/home/tester", "sub "},
		{"cd sub\\ ", "/home/tester", "sub "},
		{"cd ", "/home/tester", ""},
		{"cd link/../", "/home/tester/link/../", ""},
	} {
		u.input.SetValue(test.line)
		_, side, dir, prefix, _ := u.fileCompletionContext()
		if side != "remote" || dir != test.dir || prefix != test.prefix {
			t.Fatalf("%q: %q %q %q", test.line, side, dir, prefix)
		}
	}
	for _, output := range []string{"Local upload directory: /tmp/upload", "Workspace roots verified", "Completion unavailable: denied"} {
		u.output = output
		if !strings.Contains(u.fileContent(100), u.files.listing.Notice) {
			t.Fatal("retained listing lost reduced-metadata warning")
		}
	}
	u.noColor = false
	screen := vt.NewEmulator(100, 4)
	defer screen.Close()
	screen.Write([]byte(u.dataTable("", []string{"OWNER", "GROUP", "SIZE", "MODIFIED"}, [][]string{{"Unavailable", "Unavailable", "Unavailable", "Unavailable"}}, 100)))
	for x := 0; x < 100; x++ {
		cell := screen.CellAt(x, 2)
		if cell != nil && cell.Content != "" && strings.Contains("Unavailable", cell.Content) && !colorMatches(cell.Style.Fg, secondary.GetForeground()) {
			t.Fatal("unavailable metadata lost subtext role")
		}
	}
}

func TestFileTabsAndContextMenus(t *testing.T) {
	for _, plain := range []bool{false, true} {
		m := newFrame(launch.Info{Workspace: "/tmp/file-tabs"}, plain, launch.Options{})
		defer m.terminals.close()
		frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
		a := connection.State{Name: "gateway", State: "connected", Generation: "g", Creation: "a"}
		b := a
		b.Name = "second"
		b.Creation = "b"
		m.updateManagement(m.active, connectionList{states: []connection.State{a, b}})
		m.current().management.profiles = connection.Collection{Path: "/tmp/profiles.json", Revision: "rev", Profiles: []connection.Profile{{Name: "saved", Host: "example.test", User: "tester"}}}
		for _, size := range []image.Point{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
			frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			m.openResourceMenu("profile:0", image.Pt(size.X-1, size.Y-1))
			if !m.dialogBounds().In(image.Rect(0, 0, size.X, size.Y)) {
				t.Fatal("menu escaped terminal")
			}
			capturePresentation(t, m, fmt.Sprintf("context-%dx%d-%t", size.X, size.Y, plain))
			if !strings.Contains(ansi.Strip(m.View().Content), "Edit in Vim") {
				t.Fatal("missing editor action")
			}
			frameEvent(m, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
			if m.modal != "" {
				t.Fatal("outside click did not dismiss menu")
			}
			m.openResourceMenu("resource:0", image.Pt(size.X-1, size.Y-1))
			capturePresentation(t, m, fmt.Sprintf("connection-context-%dx%d-%t", size.X, size.Y, plain))
			if !strings.Contains(ansi.Strip(m.View().Content), "Enter Shell") {
				t.Fatal("missing shell action")
			}
			m.dismissForm()
		}
		frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
		// Find the actual row hit target, then take the real right-click route.
		found := false
		for y := 3; y < 35 && !found; y++ {
			for x := 28; x < 126; x++ {
				if m.compositor().Hit(x, y).ID() == "resource:0" {
					frameEvent(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseRight})
					found = true
					break
				}
			}
		}
		if !found || m.modal != "context" {
			t.Fatal("right-click menu did not open")
		}
		if cmd := m.resourceAction(1); cmd == nil || m.current().tab != "shell" || m.current().shell.connection != a.Name {
			t.Fatal("shell action did not use selected connection")
		}
		m.current().removeShell(m.current().shell)
		m.activate("burrow")
		m.openResourceMenu("resource:0", image.Pt(30, 10))
		m.current().management.connectionError = "unverified"
		if cmd := m.resourceAction(1); cmd != nil || len(m.current().shells) != 0 {
			t.Fatal("shell action accepted unverified observation")
		}
		m.current().management.connectionError = ""
		m.openResourceMenu("resource:0", image.Pt(30, 10))
		m.resourceAction(0)
		first := m.current().file
		if first == nil || m.current().tab != "files" {
			t.Fatal("menu did not open file tab")
		}
		defer first.files.cancel()
		first.input.SetValue("cd draft")
		m.activate("burrow")
		m.updateManagement(m.active, fileResult{mode: first.files, sequence: first.files.sequence, operation: "cd", value: connection.FileListing{Path: "/home/gateway"}})
		if m.current().tab != "" || first.files.remote != "/home/gateway" {
			t.Fatal("background result stole tab or was lost")
		}
		m.openFileTab("second")
		second := m.current().file
		defer second.files.cancel()
		m.activate("burrow")
		if cmd := m.openFileTab("gateway"); cmd != nil {
			t.Fatal("reopening tab must not rediscover")
		}
		if m.current().file != first || first.input.Value() != "cd draft" || len(m.current().fileViews) != 2 {
			t.Fatal("tab state not retained")
		}
		m.activate("file-tab:1")
		if m.current().file != second {
			t.Fatal("file tab click selected wrong view")
		}
		second.busy = false
		frameEvent(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		if len(m.current().fileViews) != 1 || m.current().tab != "" {
			t.Fatal("back did not close only selected file tab")
		}
		a.Creation = "replacement"
		m.updateManagement(m.active, connectionList{states: []connection.State{a}})
		m.selectFileTab(first)
		if first.fileState() != "unavailable" || !strings.Contains(ansi.Strip(m.metadata()), "UNAVAILABLE") {
			t.Fatal("old file view borrowed replacement connection health")
		}
		m.current().shells = append(m.current().shells, &cliTab{editor: &profileEdit{profile: connection.Profile{Name: "draft"}}})
		if !strings.Contains(ansi.Strip(m.quitSummary()), "quitting loses unsaved edits") {
			t.Fatal("quit review omitted unsaved editor warning")
		}
	}
}

func TestFilePresentation(t *testing.T) {
	for _, plain := range []bool{false, true} {
		for _, size := range []image.Point{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
			m := newFrame(launch.Info{Workspace: "/tmp/files"}, plain, launch.Options{})
			t.Cleanup(m.terminals.close)
			frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			state := connection.State{Name: "gateway", Host: "example.test", User: "tester", State: "connected", Creation: "creation", Generation: "generation"}
			m.updateManagement(m.active, connectionList{states: []connection.State{state}})
			frameEvent(m, tea.PasteMsg{Content: "scp gateway"})
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
			f := m.current().activeUI().files
			if f == nil {
				t.Fatal("scp did not enter file mode")
			}
			if f.cancel != nil {
				f.cancel()
			}
			roots := connection.FileRoots{Version: 1, Upload: "/tmp/files/uploads", Download: "/tmp/files/downloads"}
			m.updateManagement(m.active, fileResult{mode: f, sequence: f.sequence, operation: "cd", roots: roots, value: connection.FileListing{Path: "/home/tester", Entries: []connection.FileEntry{}}})
			prefix := fmt.Sprintf("files-%dx%d-%t", size.X, size.Y, plain)
			screen := capturePresentation(t, m, prefix+"-empty")
			if !strings.Contains(screen.String(), "No entries") {
				t.Fatal(screen.String())
			}
			listing := connection.FileListing{Path: "/home/tester", Entries: []connection.FileEntry{{Name: "α.txt", Path: "/home/tester/α.txt", Permissions: "-rw-r--r--", Owner: "tester", Group: "staff", Size: 1234, Modified: time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)}, {Name: "linked", Link: "sub dir", Permissions: "lrwxrwxrwx", Owner: "tester", Group: "staff", Directory: true}, {Name: "bad\x1b]52;c;x\a", Error: "broken link"}}}
			m.updateManagement(m.active, fileResult{mode: f, sequence: f.sequence, operation: "ls", roots: roots, value: listing})
			screen = capturePresentation(t, m, prefix+"-listing")
			if strings.Contains(m.View().Content, "\x1b]52;") {
				t.Fatal("remote filename escaped into terminal control")
			}
			if size.X >= 160 {
				for _, want := range []string{"PERMISSIONS", "OWNER", "GROUP", "SIZE", "MODIFIED", "NAME", "α.txt", "linked → sub dir"} {
					if !strings.Contains(screen.String(), want) {
						t.Fatal("missing", want, screen.String())
					}
				}
				if !plain {
					left, right := m.columns()
					bounds := image.Rect(left+2, 3, m.width-right-2, m.height-4)
					assertTextRole(t, screen, bounds, "linked", "#94e2d5")
					assertTextRole(t, screen, bounds, "1234", "#fab387")
					assertTextRole(t, screen, bounds, "staff", "#a6e3a1")
				}
			}
			frameEvent(m, tea.PasteMsg{Content: "cd draft"})
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyF1})
			capturePresentation(t, m, prefix+"-help")
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
			if m.current().activeUI().input.Value() != "cd draft" {
				t.Fatal("help lost draft")
			}
			frameEvent(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
			m.updateManagement(m.active, fileResult{mode: f, sequence: f.sequence, operation: "ls", roots: roots, value: listing})
			if m.current().activeUI().files != nil {
				t.Fatal("late result reopened file mode")
			}
			if !strings.Contains(m.View().Content, "SAVED CONNECTION") {
				t.Fatal("management not restored")
			}
		}
	}
}

// Export the actual ANSI view through the existing pinned VT cell model. These
// artifacts reveal backgrounds and selected controls that text-only checks miss.
func capturePresentation(t *testing.T, m *frame, name string) *vt.Emulator {
	t.Helper()
	screen := vt.NewEmulator(m.width, m.height)
	t.Cleanup(func() { screen.Close() })
	content := m.View().Content
	if _, err := screen.Write([]byte(strings.ReplaceAll(content, "\n", "\r\n"))); err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("TEST_UNDECLARED_OUTPUTS_DIR"); dir != "" {
		var svg strings.Builder
		fmt.Fprintf(&svg, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`, m.width*9, m.height*19, m.width*9, m.height*19)
		for y := 0; y < m.height; y++ {
			for x := 0; x < m.width; x++ {
				cell := screen.CellAt(x, y)
				if cell == nil {
					continue
				}
				hex := func(c color.Color, fallback string) string {
					if c == nil {
						return fallback
					}
					r, g, b, _ := c.RGBA()
					return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
				}
				fg, bg := hex(cell.Style.Fg, "#ffffff"), hex(cell.Style.Bg, "#000000")
				if cell.Style.Attrs&uv.AttrReverse != 0 {
					fg, bg = bg, fg
				}
				fmt.Fprintf(&svg, `<rect x="%d" y="%d" width="9" height="19" fill="%s"/>`, x*9, y*19, bg)
				fmt.Fprintf(&svg, `<text x="%d" y="%d" fill="%s" font-family="DejaVu Sans Mono,monospace" font-size="14">%s</text>`, x*9, y*19+14, fg, html.EscapeString(cell.Content))
			}
		}
		svg.WriteString("</svg>")
		if err := os.WriteFile(filepath.Join(dir, name+".svg"), []byte(svg.String()), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return screen
}
func TestPresentation(t *testing.T) {
	for _, size := range [][2]int{{160, 40}, {200, 50}} {
		m := newFrame(launch.Info{Workspace: "/tmp/presentation-workspace", PID: 123}, false, launch.Options{})
		frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		prefix := fmt.Sprintf("%dx%d", size[0], size[1])
		screen := capturePresentation(t, m, prefix+"-management")
		left, right := m.columns()
		for _, x := range []int{left - 1, m.width - right} {
			if x >= m.width {
				continue
			}
			if screen.CellAt(x, 3).Content != "│" {
				t.Fatal("missing sidebar separator")
			}
		}
		if screen.CellAt(left+1, 3).Content != " " {
			t.Fatal("center gutter lost")
		}
		missing := 0
		for y := 0; y < m.height; y++ {
			for x := 0; x < m.width; x++ {
				cell := screen.CellAt(x, y)
				if cell == nil || cell.Style.Bg == nil {
					missing++
				}
			}
		}
		if missing > 0 {
			t.Errorf("%s: %d cells have no owned background", prefix, missing)
		}
		if strings.Contains(strings.Join(strings.Split(m.View().Content, "\n")[:4], "\n"), m.active) {
			t.Error("redundant workspace header")
		}

		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyF1})
		capturePresentation(t, m, prefix+"-help-top")
		helpTop := helpBorders(m.View().Content)
		for i := 0; i < 100; i++ {
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyDown})
		}
		capturePresentation(t, m, prefix+"-help-bottom")
		if helpBorders(m.View().Content) != helpTop {
			t.Errorf("help bounds changed: %s => %s", helpTop, helpBorders(m.View().Content))
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
		frameEvent(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		keep := capturePresentation(t, m, prefix+"-quit-keep")
		assertSelected(t, m, keep, "confirm-reject", true)
		assertSelected(t, m, keep, "confirm-accept", false)
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
		leave := capturePresentation(t, m, prefix+"-quit-leave")
		assertSelected(t, m, leave, "confirm-reject", false)
		assertSelected(t, m, leave, "confirm-accept", true)
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
		frameEvent(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		if m.form.GetFocusedField().GetValue().(bool) {
			t.Fatal("reopened quit did not default to Keep")
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
		frameEvent(m, tea.KeyPressMsg{Code: 'm', Mod: tea.ModAlt})
		capturePresentation(t, m, prefix+"-palette")
	}
}

func TestSSHRecap(t *testing.T) {
	preview := "Generated config:\n ConnectTimeout 8\n UserKnownHostsFile /dev/null\n StrictHostKeyChecking no\n"
	m := newFrame(launch.Info{Workspace: "/tmp/recap"}, false, launch.Options{})
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	m.reviewText = preview
	m.setForm("review", "Review exact target", confirmForm("Proceed?", "", "Proceed", "Cancel"))
	screen := capturePresentation(t, m, "160x40-recap-value-roles")
	for value, hex := range map[string]string{"8": "#fab387", "/dev/null": "#a6adc8", "no": "#cba6f7"} {
		found := false
		for y := 0; y < m.height; y++ {
			var line strings.Builder
			for x := 0; x < m.width; x++ {
				line.WriteString(screen.CellAt(x, y).Content)
			}
			at := strings.Index(line.String(), " "+value)
			if at < 0 {
				continue
			}
			x := ansi.StringWidth(line.String()[:at+1])
			if !colorMatches(screen.CellAt(x, y).Style.Fg, lipgloss.Color(hex)) {
				t.Fatalf("recap value %s lost semantic color %s", value, hex)
			}
			found = true
			break
		}
		if !found {
			t.Fatalf("recap value missing: %s", value)
		}
	}
	if got := (ui{noColor: true}).semanticText(preview); got != preview {
		t.Fatal("NO_COLOR changed exact config")
	}
	text := "connect gateway\nSSH command:\n'/usr/bin/ssh' '-i' '/tmp/client key' '-p' '2222'\nGenerated config:\nHost burrow-hop-0\n HostName 192.0.2.10\n User tester\n Port 2222\n IdentityFile \"/tmp/client key\"\n StrictHostKeyChecking no\n"
	styled := (ui{}).semanticText(text)
	if ansi.Strip(styled) != text {
		t.Fatal("SSH preview text changed while coloring")
	}
	command := "/usr/bin/ssh -M -S /tmp/master -o StrictHostKeyChecking=no -D 127.0.0.1:9050 -p 2222 alice@nas.example"
	colored := (ui{}).syntax(command, false)
	for _, token := range []string{keywordStyle.Render("StrictHostKeyChecking"), warningStyle.Render("9050")} {
		if !strings.Contains(colored, token) {
			t.Fatal("SSH command option lost semantic color", token)
		}
	}
	for _, part := range []string{heading.Render("HostName"), hostStyle.Render("192.0.2.10"), successStyle.Render("tester"), warningStyle.Render("2222")} {
		if !strings.Contains(styled, part) {
			t.Fatalf("missing semantic role %q", part)
		}
	}
	for _, size := range [][2]int{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
		m := newFrame(launch.Info{Workspace: "/tmp/recap", PID: 123}, false, launch.Options{})
		frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.reviewText = text + strings.Repeat(" ServerAliveInterval 2\n", 40) + "FINAL CONFIG LINE"
		m.setForm("review", "Review exact target", confirmForm("Proceed?", "", "Proceed", "Cancel"))
		for _, offset := range []int{0, 1000} {
			m.modalOffset = offset
			screen := capturePresentation(t, m, fmt.Sprintf("%dx%d-recap-%d", size[0], size[1], offset))
			if !strings.Contains(screen.String(), "Proceed?") {
				t.Fatal("long recap hid approval controls")
			}
			if offset > 0 && !strings.Contains(screen.String(), "FINAL CONFIG LINE") {
				t.Fatal("recap cannot scroll to end")
			}
		}
		m.noColor = true
		if strings.Contains(m.View().Content, "\x1b[") {
			t.Fatal("NO_COLOR recap contains ANSI")
		}
	}
}

func TestConnectionOptionCompletion(t *testing.T) {
	for _, size := range [][2]int{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
		m := newFrame(launch.Info{Workspace: "/tmp/auth-completion"}, true, launch.Options{})
		frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		frameEvent(m, tea.PasteMsg{Content: "connect gateway host user --j"})
		if !strings.Contains(ansi.Strip(m.View().Content), "COMPLETION") {
			t.Fatal("connection option completion unavailable")
		}
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
		if got := m.current().management.input.Value(); got != "connect gateway host user --jump " {
			t.Fatalf("wrong completed option: %q", got)
		}
		capturePresentation(t, m, fmt.Sprintf("%dx%d-connect-options", size[0], size[1]))
	}
}

func helpBorders(view string) string {
	var found []string
	for y, line := range strings.Split(view, "\n") {
		if (strings.Contains(line, "╭") && strings.Contains(line, "╮")) || (strings.Contains(line, "╰") && strings.Contains(line, "╯")) {
			found = append(found, fmt.Sprint(y))
		}
	}
	return strings.Join(found, ",")
}

func assertSelected(t *testing.T, m *frame, screen *vt.Emulator, id string, want bool) {
	t.Helper()
	found := false
	compositor := m.compositor()
	for y := 0; y < m.height; y++ {
		for x := 0; x < m.width; x++ {
			if compositor.Hit(x, y).ID() != id {
				continue
			}
			found = true
			cell := screen.CellAt(x, y)
			background := blueColor
			if strings.HasPrefix(id, "profile:") || strings.HasPrefix(id, "resource:") {
				background = rowSelectionColor
			}
			got := cell != nil && colorMatches(cell.Style.Bg, lipgloss.Color(background))
			if got != want {
				t.Fatalf("%s selection background at %d,%d: got %v want %v", id, x, y, got, want)
			}
		}
	}
	if !found {
		t.Fatalf("missing native target %s", id)
	}
}
func colorMatches(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}
func TestCompletionPresentation(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 30}, {160, 40}, {200, 50}} {
		for _, plain := range []bool{false, true} {
			m := newFrame(launch.Info{Workspace: "/tmp/completion"}, plain, launch.Options{})
			defer m.terminals.close()
			frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			frameEvent(m, tea.PasteMsg{Content: "tunnel "})
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
			matches, index := m.current().management.completionOptions()
			if len(matches) != 3 || index != 0 || matches[0] != "tunnel create" || matches[1] != "tunnel check" || matches[2] != "tunnel remove" {
				t.Fatal("duplicate or missing candidates", matches, index)
			}
			screen := capturePresentation(t, m, fmt.Sprintf("completion-%dx%d-plain-%t", size[0], size[1], plain))
			bounds := m.selectionBounds()
			bounds.Min.Y = m.height - 7 // completion popup, excluding inventory prose
			if !plain {
				assertTextRole(t, screen, bounds, "tunnel create", blueColor)
				assertTextRole(t, screen, bounds, "CONNECTION", subtextColor)
			} else if strings.Contains(m.View().Content, "\x1b") {
				t.Fatal("NO_COLOR completion leaked ANSI")
			}
			// Polling may update inventory, but cannot collapse the active cycle.
			m.updateManagement(m.active, connectionList{})
			m.updateManagement(m.active, profilesReady{})
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
			if m.current().management.input.Value() != "tunnel check" {
				t.Fatal("refresh reset cycle")
			}
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
			m.current().management.input.Reset()
			frameEvent(m, tea.PasteMsg{Content: "connect "})
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
			if m.current().management.input.Value() != "connect -ip " {
				t.Fatal("editing did not reset command cycle", m.current().management.input.Value())
			}
		}
	}
}

func TestCommandRecommendationScope(t *testing.T) {
	m := newUI(launch.Info{}, true)
	for _, command := range []string{"connections", "profiles", "shells", "tunnel list"} {
		m.input.SetValue(command)
		for _, suggestion := range m.input.MatchedSuggestions() {
			if suggestion == command {
				t.Fatalf("initial recommendations bypass filter: %s", command)
			}
		}
		for _, suggestion := range m.suggestions() {
			if suggestion == command {
				t.Fatalf("dashboard inventory promoted in TUI: %s", command)
			}
		}
		if err := connection.ValidateCommand("/tmp/recommendations", strings.Fields(command)); err != nil {
			t.Fatalf("command removed instead of recommendation: %s: %v", command, err)
		}
	}
	if !strings.Contains(completionDescription("tunnel check gateway/id"), "connectivity") {
		t.Fatal("active check has no diagnostic description")
	}
}

func TestForwardArgumentGuidance(t *testing.T) {
	for _, size := range []image.Point{{80, 24}, {120, 30}, {160, 40}, {200, 50}} {
		for _, plain := range []bool{false, true} {
			for _, prefix := range []string{"tunnel create gateway forward", "tunnel create gateway reverse", "tunc gateway l", "tunc gateway r"} {
				m := newFrame(launch.Info{Workspace: "/tmp/guidance"}, plain, launch.Options{})
				defer m.terminals.close()
				frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
				for _, suffix := range []string{"", " ", " 8080 ", " 8080 localhost "} {
					m.current().management.input.Reset()
					frameEvent(m, tea.PasteMsg{Content: prefix + suffix})
					screen := capturePresentation(t, m, fmt.Sprintf("forward-guidance-%dx%d-%t-%s-%d", size.X, size.Y, plain, strings.ReplaceAll(prefix, " ", "-"), len(suffix)))
					for _, text := range []string{"LISTEN HOST PORT", "Example:", "8080", "localhost"} {
						if !strings.Contains(screen.String(), text) {
							t.Fatalf("guidance missing %q for %q at %v", text, prefix+suffix, size)
						}
					}
					if plain && strings.Contains(m.View().Content, "\x1b") {
						t.Fatal("NO_COLOR guidance leaked ANSI")
					}
					if !plain {
						assertTextRole(t, screen, image.Rect(0, 0, size.X, size.Y-3), "8080", "#f9e2af")
					}
					before := m.current().management.input.Value()
					frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
					if m.current().management.input.Value() != before {
						t.Fatal("non-selectable example changed command")
					}
				}
			}
		}
	}
}

func TestSemanticOutput(t *testing.T) {
	m := newUI(launch.Info{}, false)
	m.output = `{"name":"gateway","port":22,"count":-2.5e-3,"active":true,"missing":null,"items":[false],"session":"session-one","detail":"Master verified","error":"refused"}`
	styled := m.styledOutput()
	if ansi.Strip(styled) != m.output || !strings.Contains(styled, "38;2;180;190;254") || !strings.Contains(styled, "38;2;250;179;135") || !strings.Contains(styled, "38;2;249;226;175") {
		t.Fatal("JSON text or semantic token roles lost", styled)
	}
	screen := vt.NewEmulator(200, 2)
	defer screen.Close()
	if _, err := screen.Write([]byte(styled)); err != nil {
		t.Fatal(err)
	}
	for _, role := range []struct{ text, color string }{{"{", subtextColor}, {"}", subtextColor}, {"[", subtextColor}, {"]", subtextColor}, {":", subtextColor}, {",", subtextColor}, {"-2.5e-3", "#fab387"}, {"true", "#cba6f7"}, {"false", "#cba6f7"}, {"null", "#cba6f7"}, {`"session-one"`, lavenderColor}, {`"Master verified"`, subtextColor}, {`"refused"`, "#f38ba8"}} {
		assertTextRole(t, screen, image.Rect(0, 0, 200, 2), role.text, role.color)
	}
	m.noColor = true
	if m.styledOutput() != m.output {
		t.Fatal("NO_COLOR changed output")
	}
}

func TestForwardJSONRoles(t *testing.T) {
	m := newUI(launch.Info{}, false)
	m.output = `{"direction":"R","listen":"127.0.0.1:32451","destination":"[::1]:3000","requestedListen":"127.0.0.1:0"}`
	styled := m.styledOutput()
	if ansi.Strip(styled) != m.output {
		t.Fatal("endpoint styling changed JSON")
	}
	screen := vt.NewEmulator(160, 2)
	defer screen.Close()
	if _, err := screen.Write([]byte(styled)); err != nil {
		t.Fatal(err)
	}
	for _, port := range []string{"32451", "3000", "0\"}"} {
		assertTextRole(t, screen, image.Rect(0, 0, 160, 2), port, "#f9e2af")
	}
	m.noColor = true
	if m.styledOutput() != m.output {
		t.Fatal("NO_COLOR changed endpoint JSON")
	}
}

func TestConciseSSHRecap(t *testing.T) {
	result, err := connection.Execute(context.Background(), "/tmp/recap", []string{"connect", "gateway", "nas.example", "alice", "--ssh-config", "/dev/null", "-proxy", "1080"})
	if err != nil {
		t.Fatal(err)
	}
	review := result.(map[string]string)["review"]
	for _, plain := range []bool{false, true} {
		for _, size := range [][2]int{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
			m := newFrame(launch.Info{Workspace: "/tmp/recap"}, plain, launch.Options{})
			defer m.terminals.close()
			frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.reviewText = review
			m.setForm("review", "Review SSH connection", confirmForm("Proceed?", "", "Proceed", "Cancel"))
			bounds := m.dialogBounds()
			wantWidth := min(max(76, lipgloss.Width(review)+6), size[0]-4)
			if bounds.Dx() != wantWidth {
				t.Fatal("recap did not expand to its command", bounds, wantWidth)
			}
			screen := capturePresentation(t, m, fmt.Sprintf("ssh-command-%dx%d-plain-%t", size[0], size[1], plain))
			if !strings.Contains(screen.String(), "Proceed?") || strings.Contains(screen.String(), "Generated config:") {
				t.Fatal("recap lost controls or retained config dump")
			}
			if plain {
				if strings.Contains(m.View().Content, "\x1b") {
					t.Fatal("NO_COLOR recap leaked ANSI")
				}
			} else {
				for _, role := range []struct{ text, color string }{{"/usr/bin/ssh", blueColor}, {"-M", blueColor}, {"StrictHostKeyChecking", "#cba6f7"}, {"1080", "#f9e2af"}} {
					assertTextRole(t, screen, bounds, role.text, role.color)
				}
			}
			aligned := false
			for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
				if screen.CellAt(bounds.Min.X+3, y).Content == "/" {
					aligned = true
					break
				}
			}
			if !aligned {
				t.Fatal("SSH command is not left aligned")
			}
			m.modalOffset = 1000
			if m.dialogBounds() != bounds {
				t.Fatal("scroll changed recap geometry")
			}
		}
	}
}

func TestSOCKSTables(t *testing.T) {
	m := newUI(launch.Info{}, false)
	m.height = 40
	m.profiles.Profiles = []connection.Profile{{Name: "gateway", Host: "nas.example", User: "alice", Port: 22, Jump: "bastion", ProxyPort: 1080}}
	m.connections = []connection.State{{Name: "gateway", Host: "nas.example", User: "alice", Port: 22, Generation: "owner", State: "connected", ProxyPort: 1080, Proxy: connection.Tunnel{ID: "gateway/proxy", State: "listening"}}}
	for _, table := range []string{m.savedConnections(160), m.activeConnections(160)} {
		if !strings.Contains(ansi.Strip(table), "1080") || strings.Contains(ansi.Strip(table), "bastion") {
			t.Fatal("SOCKS port missing or confused with jump host", ansi.Strip(table))
		}
	}
	lines := strings.Split(ansi.Strip(m.activeConnections(160)), "\n")
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 8 || fields[4] != "Yes" || fields[5] != ":1080" || fields[len(fields)-2] != "0" {
		t.Fatal("proxy counted as a tunnel", lines)
	}
	m.width = 160
	m.tunnelError = ""
	view := ansi.Strip(m.View().Content)
	if strings.Contains(view, "SOCKS") || strings.Contains(view, "/socks") || !strings.Contains(view, "No forwards") {
		t.Fatal("proxy listed under tunnels", view)
	}
	for _, size := range []image.Point{{80, 24}, {120, 30}, {160, 40}, {200, 50}} {
		for _, plain := range []bool{false, true} {
			f := newFrame(launch.Info{Workspace: "/tmp/proxy-tables"}, plain, launch.Options{})
			defer f.terminals.close()
			frameEvent(f, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			capturePresentation(t, f, fmt.Sprintf("proxy-empty-%dx%d-%t", size.X, size.Y, plain))
			f.current().management.connections = m.connections
			f.current().management.profiles = m.profiles
			screen := capturePresentation(t, f, fmt.Sprintf("proxy-active-%dx%d-%t", size.X, size.Y, plain))
			if !plain && size.X >= 160 {
				assertTextRole(t, screen, f.selectionBounds(), "Yes :1080", "#94e2d5")
			}
			for _, state := range []struct{ state, text, color string }{{"lost", "Unavailable", "#a6adc8"}, {"connected", "Unverified", "#f9e2af"}} {
				row := m.connections[0]
				row.State, row.Proxy.State = state.state, "unverified"
				f.current().management.connections = []connection.State{row}
				screen = capturePresentation(t, f, fmt.Sprintf("proxy-%s-%dx%d-%t", state.text, size.X, size.Y, plain))
				if !plain && size.X >= 160 {
					assertTextRole(t, screen, f.selectionBounds(), state.text, state.color)
				}
			}
			for _, verb := range []string{"Create", "Remove"} {
				f.reviewText = verb + " SOCKS proxy\nConnection: gateway\nListen: 127.0.0.1:1080\nConnection and L/R siblings remain."
				f.setForm("review", "Review SOCKS proxy", confirmForm("Proceed?", "", "Proceed", "Cancel"))
				capturePresentation(t, f, fmt.Sprintf("proxy-%s-review-%dx%d-%t", verb, size.X, size.Y, plain))
				if plain && strings.Contains(f.View().Content, "\x1b") {
					t.Fatal("proxy review leaked ANSI in NO_COLOR")
				}
			}
		}
	}
	m.connections[0].State = "lost"
	if strings.Contains(ansi.Strip(m.activeConnections(160)), "1080") {
		t.Fatal("lost proxy shown as live")
	}
	if !strings.Contains(ansi.Strip(m.activeConnections(160)), "Unavailable") {
		t.Fatal("lost proxy hidden instead of unavailable")
	}
	m.connections[0].State = "connected"
	m.connections[0].Proxy.State = "unverified"
	if !strings.Contains(ansi.Strip(m.activeConnections(160)), "Unverified") {
		t.Fatal("uncertain listener shown as usable")
	}
	m.connections[0].Proxy = connection.Tunnel{}
	m.connections[0].ProxyPort = 0
	if !strings.Contains(ansi.Strip(m.activeConnections(160)), "No") {
		t.Fatal("absent proxy not shown as No")
	}
	m.connectionError = "unverified"
	if !strings.Contains(ansi.Strip(m.activeConnections(160)), "unverified") {
		t.Fatal("unknown inventory reported as empty")
	}
}

func TestLocalForwardPresentation(t *testing.T) {
	for _, size := range []image.Point{{80, 24}, {120, 30}, {160, 40}, {200, 50}} {
		for _, plain := range []bool{false, true} {
			m := newFrame(launch.Info{Workspace: "/tmp/forward-ui"}, plain, launch.Options{})
			defer m.terminals.close()
			frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			u := &m.current().management
			if !strings.Contains(ansi.Strip(u.localForwards(size.X)), "UNVERIFIED") {
				t.Fatal("unobserved inventory reported as empty")
			}
			frameEvent(m, tunnelList{})
			capturePresentation(t, m, fmt.Sprintf("forward-empty-%dx%d-%t", size.X, size.Y, plain))
			for i := 0; i < 12; i++ {
				direction := "L"
				if i%2 != 0 {
					direction = "R"
				}
				u.tunnels = append(u.tunnels, connection.Tunnel{ID: fmt.Sprintf("gateway/%032x", i+1), Connection: "gateway", Direction: direction, Listen: fmt.Sprintf("127.0.0.1:%d", 8000+i), Destination: "nas.example:80", State: "listening"})
			}
			frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			screen := capturePresentation(t, m, fmt.Sprintf("forward-populated-%dx%d-%t", size.X, size.Y, plain))
			if !strings.Contains(screen.String(), "TUNNELS") {
				t.Fatal("tunnel section missing")
			}
			if plain && strings.Contains(m.View().Content, "\x1b") {
				t.Fatal("NO_COLOR forwarding leaked ANSI")
			}
			if !plain && size.X == 200 {
				if !strings.Contains(screen.String(), "Reverse") || !strings.Contains(screen.String(), "Remote") || !strings.Contains(screen.String(), "DESTINATION") {
					t.Fatal("reverse endpoint semantics missing", screen.String())
				}
				for _, role := range []struct{ text, color string }{{"gateway", lavenderColor}, {"Local", "#cba6f7"}, {"127.0.0.1", "#f5c2e7"}, {"8000", "#f9e2af"}, {"listening", "#a6e3a1"}, {"1–3", "#fab387"}, {"Alt+Shift+↑↓", "#cba6f7"}} {
					assertTextRole(t, screen, m.selectionBounds(), role.text, role.color)
				}
			}
			u.input.SetValue("tunnel remove ")
			u.input.SetSuggestions(u.suggestions())
			for i := 0; i < 12; i++ {
				u.cycleCompletion(false)
			}
			if u.input.Value() != "tunnel remove gateway/0000000000000000000000000000000c" {
				t.Fatal("overflow ID completion", u.input.Value())
			}
			capturePresentation(t, m, fmt.Sprintf("forward-completion-%dx%d-%t", size.X, size.Y, plain))
			u.input.Reset()
			for i := 0; i < 12; i++ {
				frameEvent(m, tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModAlt | tea.ModShift})
			}
			if !strings.Contains(ansi.Strip(u.localForwards(160)), "8011") {
				t.Fatal("tunnel overflow cannot scroll")
			}
			u.tunnels[11].Destination = "host\x1b]52;c;UNTRUSTED\x07"
			if strings.Contains(u.localForwards(160), "\x1b]52;") {
				t.Fatal("remote control sequence escaped renderer")
			}
			m.reviewText = "Create reverse forward\nConnection: gateway\nRemote listener: 127.0.0.1:8080\nLocal destination: nas.example:80"
			m.setForm("review", "Review reverse forward", confirmForm("Proceed?", "", "Proceed", "Cancel"))
			capturePresentation(t, m, fmt.Sprintf("forward-review-%dx%d-%t", size.X, size.Y, plain))
			bounds := m.dialogBounds()
			m.modalOffset = 100
			if m.dialogBounds() != bounds {
				t.Fatal("forward recap geometry changed with scroll")
			}
		}
	}
}

func TestTunnelConnectionCompletion(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/tunnel-completion"}, true, launch.Options{})
	defer m.terminals.close()
	frameEvent(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	frameEvent(m, connectionList{states: []connection.State{
		{Name: "gateway", State: "connected", Generation: "g"},
		{Name: "closed-host", State: "closed", Generation: "g"},
		{Name: "lost-host", State: "lost", Generation: "g"},
	}})
	u := &m.current().management
	for _, c := range []struct{ prefix, want string }{{"tunnel create ", "tunnel create gateway forward "}, {"tunc ", "tunc gateway l "}, {"tunnel create gateway r", "tunnel create gateway reverse "}, {"tunc gateway r", "tunc gateway r "}} {
		u.input.SetValue(c.prefix)
		u.input.CursorEnd()
		u.input.SetSuggestions(u.suggestions())
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
		if u.input.Value() != c.want {
			t.Fatalf("completion: %q, want %q", u.input.Value(), c.want)
		}
	}
	frameEvent(m, connectionList{err: fmt.Errorf("owner unavailable")})
	u.input.SetValue("tunc ")
	for _, suggestion := range u.suggestions() {
		if strings.HasPrefix(suggestion, "tunc gateway") {
			t.Fatal("failed observation offered stale connection")
		}
	}
}

func TestFinishedConnectionColors(t *testing.T) {
	for _, plain := range []bool{false, true} {
		m := newFrame(launch.Info{Workspace: "/tmp/result-colors"}, plain, launch.Options{})
		defer m.terminals.close()
		frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
		m.attempt = &authAttempt{path: m.active}
		frameEvent(m, authFinished{attempt: m.attempt, result: connection.State{Generation: "generation-one", Name: "gateway", Host: "192.0.2.50", User: "alice", Port: 2222, State: "connected", Socket: "/tmp/ssh.sock", Session: "session-one", OwnerPID: 4321, MasterPID: 5432, SocketInode: 6543, Detail: "Master verified"}})
		screen := capturePresentation(t, m, fmt.Sprintf("connect-result-plain-%t", plain))
		if !plain {
			for _, role := range []struct{ text, color string }{{"{", subtextColor}, {`"generation-one"`, lavenderColor}, {`"gateway"`, lavenderColor}, {`"192.0.2.50"`, "#f5c2e7"}, {`"alice"`, "#a6e3a1"}, {"2222", "#f9e2af"}, {`"connected"`, "#a6e3a1"}} {
				assertTextRole(t, screen, m.selectionBounds(), role.text, role.color)
			}
		} else if strings.Contains(m.View().Content, "\x1b") {
			t.Fatal("NO_COLOR result leaked ANSI")
		}
		for _, size := range [][2]int{{200, 50}, {120, 30}, {80, 24}} {
			frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			screen = capturePresentation(t, m, fmt.Sprintf("connect-result-%dx%d-plain-%t", size[0], size[1], plain))
			if !plain {
				assertTextRole(t, screen, m.selectionBounds(), "{", subtextColor)
			}
		}
	}
}

func TestNavigationPresentation(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{})
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	for i := 0; i < 9; i++ {
		p := fmt.Sprintf("/tmp/ws%d", i)
		m.paths = append(m.paths, p)
		m.workspaces[p] = &workspaceView{management: newUI(launch.Info{Workspace: p}, true), focus: "prompt"}
	}
	m.resize()
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyF6})
	for i := 0; i < 9; i++ {
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyDown})
		want := "›◌ " + filepath.Base(m.paths[m.navIndex])
		if !strings.Contains(m.View().Content, want) {
			t.Fatalf("selected workspace offscreen: %s", want)
		}
	}
	if strings.Contains(m.View().Content, "/10") || !strings.Contains(m.View().Content, "SHELLS - SSH") {
		t.Fatal("workspace tree hid entries behind a summary", m.View().Content)
	}
	m.activate("hovel")
	if !strings.Contains(m.View().Content, "› Hovel") {
		t.Fatal("no-color active tab missing")
	}
}

func TestCommandPalette(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, false, launch.Options{})
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	frameEvent(m, tea.PasteMsg{Content: "saved draft"})
	frameEvent(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if m.modal != "menu" {
		t.Fatal("Ctrl+P did not open commands")
	}
	original := helpBorders(m.View().Content)
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if func() bool { v, _ := m.menu.Hovered(); return v != len(menuActions)-1 }() {
		t.Fatal("palette arrows do not wrap")
	}
	frameEvent(m, tea.PasteMsg{Content: "meta"})
	if func() bool { v, _ := m.menu.Hovered(); return v != 1 }() {
		t.Fatal("filter did not reset selection")
	}
	capturePresentation(t, m, "160x40-palette-filtered")
	if helpBorders(m.View().Content) != original {
		t.Fatal("filter resized palette")
	}
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.modal != "metadata" {
		t.Fatal("filtered action dispatched incorrectly")
	}
	for i := 0; i < 100; i++ {
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	bottom := m.View().Content
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.View().Content == bottom {
		t.Fatal("metadata scroll stuck beyond end")
	}
	capturePresentation(t, m, "160x40-metadata")
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	frameEvent(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	frameEvent(m, tea.PasteMsg{Content: "no such command"})
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.modal != "menu" || !strings.Contains(ansi.Strip(m.View().Content), "No matching commands") {
		t.Fatal("empty filter dispatched")
	}
	frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.current().management.input.Value() != "saved draft" {
		t.Fatal("palette changed prompt")
	}
	screen := capturePresentation(t, m, "160x40-command-footer")
	for x := 0; x < m.width; x++ {
		if !colorMatches(screen.CellAt(x, m.height-1).Style.Bg, lipgloss.Color("#11111b")) {
			t.Fatal("footer background missing")
		}
	}
	if !strings.Contains(ansi.Strip(m.commandHelp()), "Ctrl+P menu") {
		t.Fatal("footer missing command shortcut")
	}
}

func TestMenuThemeConsistency(t *testing.T) {
	for _, size := range []image.Point{{80, 24}, {120, 30}, {160, 40}, {200, 50}} {
		for _, plain := range []bool{false, true} {
			m := newFrame(launch.Info{Workspace: "/tmp/menu"}, plain, launch.Options{})
			if plain {
				m = newDemoFrame(plain)
			}
			defer m.terminals.close()
			frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			frameEvent(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
			bounds := m.dialogBounds()
			for _, filter := range []string{"", "meta", "no match"} {
				frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
				frameEvent(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
				frameEvent(m, tea.PasteMsg{Content: filter})
				screen := capturePresentation(t, m, fmt.Sprintf("menu-%dx%d-plain-%t-%s", size.X, size.Y, plain, strings.ReplaceAll(filter, " ", "-")))
				title := strings.Split(ansi.Strip(m.formText()), "\n")[0]
				if title != centered("Menu", bounds.Dx()-6) || strings.Contains(m.formText(), "╱") || bounds != m.dialogBounds() {
					t.Fatal("menu diverged from shared dialog title or stable bounds")
				}
				footer := ansi.Cut(strings.Split(m.View().Content, "\n")[bounds.Max.Y-3], bounds.Min.X+3, bounds.Max.X-3)
				if ansi.Strip(footer) != centered("↑↓ select · Enter run · Esc close", bounds.Dx()-6) {
					t.Fatal("menu footer is not centered like other dialogs")
				}
				if plain {
					if strings.Contains(m.View().Content, "\x1b") {
						t.Fatal("NO_COLOR menu leaked ANSI")
					}
				} else {
					assertTextRole(t, screen, bounds.Inset(1), "Menu", lavenderColor)
					assertTextRole(t, screen, bounds.Inset(1), "Esc close", subtextColor)
				}
			}
		}
	}
}

func TestShrinkingCenter(t *testing.T) {
	m := newDemoFrame(true)
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	frameEvent(m, tea.PasteMsg{Content: "saved draft"})
	for _, size := range [][2]int{{160, 40}, {120, 30}, {100, 24}, {80, 24}, {40, 16}, {1, 1}} {
		frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		plain := ansi.Strip(m.View().Content)
		if strings.Contains(plain, "Resize window") || strings.Contains(plain, "Minimum") {
			t.Fatal("resize gate returned", plain)
		}
		if size[0] >= 80 && (!strings.Contains(plain, "SAVED CONNECTIONS") || !strings.Contains(plain, "ACTIVE SSH CONNECTIONS")) {
			t.Fatal("center hidden", plain)
		}
		if size[0] >= 80 && m.View().Cursor == nil {
			t.Fatal("visible prompt blocked")
		}
		for _, line := range strings.Split(plain, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("overflow", line)
			}
		}
		capturePresentation(t, m, fmt.Sprintf("%dx%d-shrink", size[0], size[1]))
	}
	frameEvent(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	frameEvent(m, tea.PasteMsg{Content: " editable"})
	if m.current().management.input.Value() != "saved draft editable" {
		t.Fatal("resize discarded or blocked draft")
	}
	frameEvent(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if !strings.Contains(ansi.Strip(m.View().Content), "Type to filter") {
		t.Fatal("menu blocked below 160x40")
	}
}

func TestTableCentering(t *testing.T) {
	u := newUI(launch.Info{}, true)
	lines := strings.Split(ansi.Strip(u.dataTable("SECTION", []string{"NAME", "PORT"}, [][]string{{"node", "22"}}, 40)), "\n")
	if lines[0] != "SECTION" {
		t.Fatal("section title moved", lines[0])
	}
	for _, item := range []struct {
		row    int
		value  string
		center int
	}{{1, "NAME", 20}, {1, "PORT", 60}, {3, "node", 20}, {3, "22", 60}} {
		at := strings.Index(lines[item.row], item.value)
		if at < 0 || absInt(2*at+len(item.value)-item.center) > 1 {
			t.Fatal("cell not centered", lines)
		}
	}
	m := newDemoFrame(true)
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	selected := strings.Split(m.View().Content, "\n")
	m.current().selected = ""
	unselected := strings.Split(m.View().Content, "\n")
	for y, line := range selected {
		if strings.Contains(line, "›") && strings.Contains(line, "gateway") {
			if strings.Replace(strings.Split(line, "│")[1], "›", " ", 1) != strings.Split(unselected[y], "│")[1] {
				t.Fatal("selection shifted cells", line, unselected[y])
			}
			return
		}
	}
	t.Fatal("missing selected row")
}

func TestPaletteClipboardOrigin(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/one"}, true, launch.Options{})
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	frameEvent(m, tea.PasteMsg{Content: "draft"})
	m.openPalette()
	paste := func() tea.Msg { return tea.PasteMsg{Content: "meta"} }
	frameEvent(m, formCommand(m.active, m.inputEpoch, paste)())
	if !strings.Contains(m.form.View(), "meta") || m.current().management.input.Value() != "draft" {
		t.Fatal("clipboard went to wrong input")
	}
	late := formCommand(m.active, m.inputEpoch, paste)()
	m.openPalette()
	frameEvent(m, late)
	if strings.Contains(m.form.View(), "meta") {
		t.Fatal("late clipboard changed reopened palette")
	}
}

func TestRefinedPresentation(t *testing.T) {
	for _, size := range [][2]int{{160, 40}, {200, 50}} {
		m := newFrame(launch.Info{Workspace: "/tmp/one"}, false, launch.Options{})
		frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		plain := ansi.Strip(m.View().Content)
		for _, unwanted := range []string{"This session", "connect NAME HOST", "1–1/1", "AUTH", "DESTINATION"} {
			if strings.Contains(plain, unwanted) {
				t.Fatalf("unwanted chrome: %s", unwanted)
			}
		}
		m.current().management.connections = []connection.State{{Name: "gateway", Host: "example.com", User: "operator", Port: 22, State: "connected"}, {Name: "build", Host: "build.example.com", User: "runner", Port: 2222, State: "connecting"}}
		capturePresentation(t, m, fmt.Sprintf("%dx%d-populated", size[0], size[1]))
		frameEvent(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		lines := strings.Split(ansi.Strip(m.View().Content), "\n")
		for _, line := range lines {
			if at := strings.Index(line, "Quit Burrow?"); at >= 0 {
				left := ansi.StringWidth(line[:at])
				if absInt(2*left+12-m.width) > 1 {
					t.Fatal("quit title not centered", line)
				}
			}
		}
	}
}
func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func TestDemoPreview(t *testing.T) {
	m := newDemoFrame(false)
	if m.Init() != nil || m.check(m.active) != nil {
		t.Fatal("demo started I/O")
	}
	for _, size := range [][2]int{{160, 40}, {200, 50}} {
		frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		capturePresentation(t, m, fmt.Sprintf("%dx%d-demo", size[0], size[1]))
		plain := ansi.Strip(m.View().Content)
		for _, label := range []string{"DEMO", "production", "gateway", "5432"} {
			if !strings.Contains(plain, label) {
				t.Fatal("missing sample data", label, plain)
			}
		}
	}
	frameEvent(m, tea.PasteMsg{Content: "connect real host user --key /tmp/key"})
	_, cmd := frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || m.current().management.busy {
		t.Fatal("demo executed command")
	}
	m.openNew()
	frameEvent(m, tea.PasteMsg{Content: "/tmp/must-not-launch-demo"})
	if m.submitWorkspace() != nil || m.launchPending {
		t.Fatal("demo launched workspace")
	}
}

// Project presentation contract: compare final rendered cells to semantic roles,
// not merely the palette function's return value.
func assertTextRole(t *testing.T, screen *vt.Emulator, bounds image.Rectangle, value, hex string) {
	t.Helper()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		var line strings.Builder
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			line.WriteString(screen.CellAt(x, y).Content)
		}
		at := strings.Index(line.String(), value)
		if at < 0 {
			continue
		}
		x := bounds.Min.X + ansi.StringWidth(line.String()[:at])
		if !colorMatches(screen.CellAt(x, y).Style.Fg, lipgloss.Color(hex)) {
			t.Fatalf("%q lost semantic color %s", value, hex)
		}
		return
	}
	t.Fatalf("required text missing: %q", value)
}

func TestConnectionRecapColors(t *testing.T) {
	for _, operation := range []string{"connect", "close"} {
		for _, size := range [][2]int{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
			m := newFrame(launch.Info{Workspace: "/tmp/recap"}, false, launch.Options{})
			frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			review := "connect gateway\nEndpoint: alice@nas.example:2222\nSSH config: /tmp/config\nJump: bob@bastion:2200,192.0.2.1\nKey: /home/alice/.ssh/id_ed25519\nAgent: none"
			expect := map[string]string{"connect": blueColor, "gateway": lavenderColor, "Endpoint:": lavenderColor, "alice": "#a6e3a1", "nas.example": "#f5c2e7", "2222": "#f9e2af", "192.0.2.1": "#f5c2e7", "bob": "#a6e3a1", "bastion": "#f5c2e7", "2200": "#f9e2af", "Key:": lavenderColor, "/home/alice/.ssh/id_ed25519": "#94e2d5"}
			if operation == "close" {
				review = "Close NAS (alice@192.0.2.2:2222), state connected, master PID 123, socket /tmp/master. Ends all owned connection access; saved settings and artifacts remain. Repeat close NAS --yes to confirm."
				expect = map[string]string{"Close": blueColor, "NAS": lavenderColor, "alice": "#a6e3a1", "192.0.2.2": "#f5c2e7", "2222": "#f9e2af", "connected": "#a6e3a1", "123": "#fab387", "--yes": blueColor}
			}
			m.modal, m.commandArgs = "review", []string{operation, "gateway"}
			frameEvent(m, m.dispatch(m.active, func() tea.Msg {
				return commandReview{epoch: m.inputEpoch, args: m.commandArgs, review: review}
			})())
			screen := capturePresentation(t, m, fmt.Sprintf("recap-%s-%dx%d", operation, size[0], size[1]))
			bounds := m.dialogBounds()
			for value, hex := range expect {
				assertTextRole(t, screen, bounds, value, hex)
			}
			colored := ansi.Strip(m.View().Content)
			m.noColor, m.current().management.noColor = true, true
			plain := m.View().Content
			if plain != ansi.Strip(plain) || strings.Join(strings.Fields(plain), " ") != strings.Join(strings.Fields(colored), " ") || bounds != m.dialogBounds() {
				t.Fatal("NO_COLOR changed recap text/layout or leaked colors")
			}
		}
	}
}

func TestSharedFormAndHelpRoles(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/colors"}, false, launch.Options{})
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	m.setForm("profile-save", "Save settings", saveProfileForm("gateway", "/tmp/collection.json"))
	screen := capturePresentation(t, m, "shared-form-colors")
	assertTextRole(t, screen, m.dialogBounds(), "Collection:", lavenderColor)
	assertTextRole(t, screen, m.dialogBounds(), "/tmp/collection.json", subtextColor)
	assertTextRole(t, screen, m.dialogBounds(), "Enter", "#cba6f7")
	m.dismissForm()
	m.current().management.help = true
	screen = capturePresentation(t, m, "help-command-colors")
	bounds := image.Rect(23, 4, 137, 36)
	for value, hex := range map[string]string{"NAVIGATION": blueColor, "connect NAME HOST USER": blueColor, "NAME": "#f9e2af", "F6 / Shift+F6": "#cba6f7"} {
		assertTextRole(t, screen, bounds, value, hex)
	}
	if screen.CellAt(65, 7).Content != "M" || screen.CellAt(65, 8).Content != "O" {
		t.Fatal("help descriptions are not aligned in their own column")
	}
	assertTextRole(t, screen, bounds, "Move focus", subtextColor)
	m.current().management.helpOffset = 1000
	screen = capturePresentation(t, m, "help-keybinding-colors")
	assertTextRole(t, screen, bounds, "PATH", "#f9e2af")
	assertTextRole(t, screen, bounds, "PgUp/PgDn", "#cba6f7")
	m.noColor, m.current().management.noColor = true, true
	plain := m.View().Content
	if plain != ansi.Strip(plain) || !strings.Contains(plain, "PgUp/PgDn") {
		t.Fatal("NO_COLOR help lost text or leaked colors")
	}
}

func TestHelpQuickReference(t *testing.T) {
	for _, size := range []image.Point{{80, 24}, {120, 30}, {160, 40}, {200, 50}} {
		for _, plain := range []bool{false, true} {
			m := newFrame(launch.Info{Workspace: "/tmp/help"}, plain, launch.Options{})
			if plain {
				m = newDemoFrame(plain)
			}
			defer m.terminals.close()
			frameEvent(m, tea.WindowSizeMsg{Width: size.X, Height: size.Y})
			frameEvent(m, tea.PasteMsg{Content: "connect draft"})
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyF1})
			u := &m.current().management
			top := capturePresentation(t, m, fmt.Sprintf("help-%dx%d-%t-top", size.X, size.Y, plain))
			if !strings.Contains(top.String(), "Burrow Help") || !strings.Contains(top.String(), "NAVIGATION") {
				t.Fatal("quick reference heading missing")
			}
			geometry := helpBorders(m.View().Content)
			v := u.helpViewport(size.X, size.Y)
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyPgDown})
			if u.helpOffset != min(v.Height(), v.TotalLineCount()-v.Height()) || u.helpOffset <= 1 {
				t.Fatal("PgDown did not move a page", u.helpOffset, v.Height())
			}
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyPgUp})
			if u.helpOffset != 0 {
				t.Fatal("PgUp did not return to top")
			}
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnd})
			bottom := capturePresentation(t, m, fmt.Sprintf("help-%dx%d-%t-bottom", size.X, size.Y, plain))
			if !strings.Contains(bottom.String(), "bochner.github.io/burrow") || !strings.Contains(bottom.String(), "100%") || geometry != helpBorders(m.View().Content) {
				t.Fatal("end navigation, documentation or stable geometry missing")
			}
			if plain && strings.Contains(m.View().Content, "\x1b") {
				t.Fatal("NO_COLOR help leaked ANSI")
			}
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyHome})
			if u.helpOffset != 0 {
				t.Fatal("Home did not return to top")
			}
			var pages strings.Builder
			for {
				pages.WriteString(ansi.Strip(m.View().Content))
				pages.WriteByte('\n')
				before := u.helpOffset
				frameEvent(m, tea.KeyPressMsg{Code: tea.KeyPgDown})
				if u.helpOffset == before {
					break
				}
			}
			for _, verb := range []string{"prepare", "launch", "list", "inspect", "output", "cancel", "collect", "close"} {
				if !strings.Contains(pages.String(), "run "+verb) {
					t.Fatal("F1 help hides retained command", verb, size, plain)
				}
			}
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
			if u.help || u.input.Value() != "connect draft" {
				t.Fatal("help dismissal lost draft")
			}
		}
	}
}

func TestTerminalStatusRoles(t *testing.T) {
	for _, state := range []string{"pending", "refused", "exited"} {
		m := newFrame(launch.Info{Workspace: "/tmp/terminal"}, false, launch.Options{})
		frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
		m.current().tab = "hovel"
		m.current().cli = &cliTab{pending: state == "pending"}
		label, hex := "opening / closing", "#f9e2af"
		if state == "refused" {
			m.current().cli.error = "REFUSED: unavailable"
			label, hex = "REFUSED", "#f38ba8"
		}
		if state == "exited" {
			m.current().cli.screen.Exited = true
			label, hex = "CLI: exited", "#f38ba8"
		}
		screen := capturePresentation(t, m, "terminal-status-"+state)
		r := m.terminalBounds()
		rows := []int{m.height - 2}
		if state == "refused" {
			rows = append(rows, r.Min.Y)
		}
		for _, y := range rows {
			if !colorMatches(screen.CellAt(r.Min.X, y).Style.Fg, lipgloss.Color(hex)) {
				t.Fatalf("%s lost semantic color at row %d", label, y)
			}
		}
		m.noColor, m.current().management.noColor = true, true
		content := m.View().Content
		if ansi.Strip(content) != content || !strings.Contains(content, label) {
			t.Fatal("NO_COLOR lost terminal status or leaked styles")
		}
	}
}

func TestSidebarBrand(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 30}, {160, 40}, {200, 50}} {
		for _, demo := range []bool{false, true} {
			m := newFrame(launch.Info{Workspace: "/tmp/brand"}, false, launch.Options{})
			if demo {
				m = newDemoFrame(false)
			}
			frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			screen := capturePresentation(t, m, fmt.Sprintf("brand-%dx%d-demo-%t", size[0], size[1], demo))
			_, right := m.columns()
			x := m.width - right + 2
			want, rows := "BURROW", 1
			if size[0] >= 160 {
				want, rows = burrowWordmark, 6
			}
			for y, line := range strings.Split(want, "\n") {
				for offset, char := range []rune(line) {
					cell := screen.CellAt(x+offset, y+1)
					if cell.Content != string(char) || !colorMatches(cell.Style.Fg, lipgloss.Color(lavenderColor)) {
						t.Fatalf("brand cell changed at %d,%d: %+v", x+offset, y+1, cell)
					}
				}
			}
			frameEvent(m, tea.MouseClickMsg{X: x, Y: rows + 2, Button: tea.MouseLeft})
			if m.modal != "metadata" {
				t.Fatal("branding displaced daemon status pointer target")
			}
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
			m.noColor, m.current().management.noColor = true, true
			content := m.View().Content
			if content != ansi.Strip(content) || !strings.Contains(content, strings.Split(want, "\n")[0]) {
				t.Fatal("NO_COLOR lost branding or leaked styles")
			}
		}
	}
}

func TestTableAndMetadataRoles(t *testing.T) {
	m := newDemoFrame(false)
	m.current().selected = ""
	frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	screen := capturePresentation(t, m, "160x40-semantic-tables")
	expect := map[string]string{"production": "#b4befe", "10.20.0.10": "#f5c2e7", "operator": "#a6e3a1", "2222": "#f9e2af", "id_ed25519": "#94e2d5", "48.6 MB": "#fab387"}
	for value, hex := range expect {
		found := false
		for y := 0; y < m.height; y++ {
			var line strings.Builder
			for x := 0; x < m.width; x++ {
				line.WriteString(screen.CellAt(x, y).Content)
			}
			at := strings.Index(line.String(), value)
			if at < 0 {
				continue
			}
			x := ansi.StringWidth(line.String()[:at])
			if !colorMatches(screen.CellAt(x, y).Style.Fg, lipgloss.Color(hex)) {
				t.Fatalf("%s lost semantic color %s", value, hex)
			}
			found = true
			break
		}
		if !found {
			t.Fatalf("required field/value missing: %s", value)
		}
	}
	live := newFrame(launch.Info{Workspace: "/tmp/live"}, true, launch.Options{})
	live.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	if !strings.Contains(live.metadata(), "Completed files: Unavailable") || strings.Contains(live.metadata(), "Completed files: 0") {
		t.Fatal("unavailable downloads misreported")
	}
	live.current().management.downloadObserved = true
	live.current().management.downloads.Files = 7
	live.current().management.downloads.Bytes = 1_200_000_000
	if metadata := live.metadata(); !strings.Contains(metadata, "Completed files: 7") || !strings.Contains(metadata, "Downloaded: 1.2 GB") {
		t.Fatal("workspace download totals are not explicit or human-readable", metadata)
	}
	live.current().management.connectionObserved = true
	if !strings.Contains(live.metadata(), "DISCONNECTED") {
		t.Fatal("empty observed snapshot not disconnected")
	}
	live.current().management.connectionError = "owner unavailable"
	if !strings.Contains(live.metadata(), "UNVERIFIED") || !strings.Contains(live.metadata(), "Active in workspace: Unknown") {
		t.Fatal("failed observation presented as disconnected or a measured zero")
	}
	live.current().management.connections = []connection.State{{Name: "existing", State: "connected"}}
	if !strings.Contains(live.metadata(), "Active in workspace: Unknown") {
		t.Fatal("failed refresh advertises stale connection count")
	}
	if !strings.Contains(live.metadata(), "Check duration: Unavailable") || strings.Contains(live.metadata(), "Latency") {
		t.Fatal("unmeasured verification duration presented as latency")
	}
	m.noColor = true
	m.current().management.noColor = true
	if ansi.Strip(m.View().Content) != m.View().Content {
		t.Fatal("NO_COLOR leaked styles")
	}
}

func TestSavedProfilesPresentation(t *testing.T) {
	for _, size := range [][2]int{{160, 40}, {200, 50}, {120, 30}, {80, 24}} {
		for _, plain := range []bool{false, true} {
			m := newFrame(launch.Info{Workspace: "/tmp/profile-view"}, plain, launch.Options{})
			frameEvent(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			prefix := fmt.Sprintf("%dx%d-profiles-%t", size[0], size[1], plain)
			capturePresentation(t, m, prefix+"-empty")
			list := connection.Collection{Path: "/tmp/homelab.json", Revision: strings.Repeat("a", 64), Profiles: []connection.Profile{{Name: "nas", Host: "192.168.1.20", User: "alice", Port: 2222, Key: "/home/alice/.ssh/key", Jump: "bastion"}, {Name: "router", Host: "192.168.1.1", User: "admin", Port: 22}}}
			m.updateManagement(m.active, profilesReady{collection: list})
			before := capturePresentation(t, m, prefix+"-populated")
			if !strings.Contains(ansi.Strip(m.View().Content), "SAVED CONNECTIONS") {
				t.Fatal("missing saved table")
			}
			// Actual row layers and selection retain field alignment and never run I/O.
			m.activate("profile:0")
			if m.current().management.selectedProfile != "nas" {
				t.Fatal("row did not select")
			}
			screen := capturePresentation(t, m, prefix+"-selected")
			assertSelected(t, m, screen, "profile:0", !plain)
			compositor := m.compositor()
			for y := 0; y < m.height; y++ {
				for x := m.selectionBounds().Min.X + 1; x < m.selectionBounds().Max.X; x++ {
					if compositor.Hit(x, y).ID() == "profile:0" {
						a, b := before.CellAt(x, y), screen.CellAt(x, y)
						if a.Content != b.Content || !colorMatches(a.Style.Fg, b.Style.Fg) {
							t.Fatalf("selection changed token at %d,%d", x, y)
						}
					}
				}
			}
			if !strings.Contains(ansi.Strip(m.View().Content), "›") {
				t.Fatal("selection marker missing")
			}
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
			capturePresentation(t, m, prefix+"-actions")
			if m.modal != "profile-menu" {
				t.Fatal("saved actions unavailable")
			}
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
			if got := m.current().management.input.Value(); got != "profile connect nas" {
				t.Fatalf("action did not prepare explicit connect: %q", got)
			}
			m.current().management.input.Reset()
			frameEvent(m, tea.PasteMsg{Content: "profile connect rou"})
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyTab})
			if got := m.current().management.input.Value(); got != "profile connect router" {
				t.Fatalf("profile completion: %q", got)
			}
			m.setForm("profile-save", "Connected · save for a future session?", saveProfileForm("nas", "/tmp/homelab.json"))
			capturePresentation(t, m, prefix+"-save")
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
			if m.modal != "" {
				t.Fatal("skip save did not return to management")
			}
			if plain && strings.Contains(m.View().Content, "\x1b") {
				t.Fatal("NO_COLOR leaked escapes")
			}
		}
	}
}

func TestSidebarStatusAndClickAway(t *testing.T) {
	for _, plain := range []bool{false, true} {
		m := newFrame(launch.Info{Workspace: "/tmp/polish"}, plain, launch.Options{})
		defer m.terminals.close()
		frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
		w := m.current()
		w.shell = &cliTab{id: "1", connection: "gateway"}
		w.shells = []*cliTab{w.shell}
		w.management.connectionObserved = true
		w.management.profiles.Profiles = []connection.Profile{{Name: "saved", Host: "example.com", User: "alice", Port: 22}}
		for _, test := range []struct{ state, dot, color string }{{"connected", "●", "#a6e3a1"}, {"connecting", "◐", "#f9e2af"}, {"lost", "○", "#f38ba8"}, {"unverified", "◌", "#f38ba8"}} {
			w.management.connections = []connection.State{{Name: "gateway", State: test.state}}
			screen := capturePresentation(t, m, fmt.Sprintf("sidebar-%s-%t", test.state, plain))
			if !plain {
				assertTextRole(t, screen, image.Rect(0, 3, 26, 4), test.dot, test.color)
				assertTextRole(t, screen, image.Rect(0, 25, 26, 26), test.dot, test.color)
			}
			if w.connectionState("") != test.state || w.connectionState("gateway") != test.state {
				t.Fatal("incorrect observed status", test.state)
			}
		}
		w.management.connectionError = "offline"
		if w.connectionState("") != "unverified" {
			t.Fatal("stale connection remained green")
		}
		w.management.connectionError = ""
		for _, id := range []string{"profile:0", "resource:0"} {
			m.activate(id)
			before := capturePresentation(t, m, "clear-"+strings.ReplaceAll(id, ":", "-")+fmt.Sprint(plain))
			assertSelected(t, m, before, id, !plain)
			r := m.selectionBounds()
			frameEvent(m, tea.MouseClickMsg{X: r.Min.X + 1, Y: r.Max.Y - 2, Button: tea.MouseLeft})
			if w.selected != "" || w.management.selectedProfile != "" || w.shell == nil {
				t.Fatal("click-away did not clear rows or destroyed shell")
			}
			m.activate(id)
			frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEsc})
			if w.selected != "" || w.management.selectedProfile != "" {
				t.Fatal("escape left row selected")
			}
		}
		m.activate("shell:1")
		selectedTab := capturePresentation(t, m, fmt.Sprintf("ssh-tab-%t", plain))
		left, _ := m.columns()
		if !plain && !colorMatches(selectedTab.CellAt(left+21, 1).Style.Bg, lipgloss.Color(rowSelectionColor)) {
			t.Fatal("SSH tab has no selected background")
		}
		if w.tab != "shell" || !strings.Contains(ansi.Strip(m.View().Content), "› Shell #1") || strings.Contains(ansi.Strip(m.View().Content), "› Burrow") {
			t.Fatal("SSH tab not exclusively selected")
		}
		w.focus = "tabs"
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyRight})
		if w.tab != "" {
			t.Fatal("SSH missing from tab navigation")
		}
		other := "/tmp/other-shell"
		m.paths = append(m.paths, other)
		m.workspaces[other] = &workspaceView{management: newUI(launch.Info{Workspace: other}, plain), shell: &cliTab{connection: "other"}}
		m.workspaces[other].shells = []*cliTab{m.workspaces[other].shell}
		w.focus, m.shellIndex = "shells", 3
		frameEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.active != other || m.current().tab != "shell" {
			t.Fatal("shell navigation opened wrong workspace")
		}
	}
}

func TestSaveOfferWaitsForWorkspaceAndDialog(t *testing.T) {
	m := newFrame(launch.Info{Workspace: "/tmp/first"}, true, launch.Options{})
	first := m.active
	m.workspaces["/tmp/second"] = &workspaceView{management: newUI(launch.Info{Workspace: "/tmp/second"}, true)}
	m.active = "/tmp/second"
	message := m.dispatch(first, func() tea.Msg {
		return saveOffered{name: "nas", offer: true, collection: connection.Collection{Path: "/tmp/homelab.json"}}
	})()
	m.Update(message)
	if m.modal != "" || len(m.workspaces[first].saveOffers) != 1 {
		t.Fatal("save offer lost or shown in wrong workspace")
	}
	m.active, m.modal = first, "menu"
	if m.showSaveOffer() != nil || len(m.current().saveOffers) != 1 {
		t.Fatal("save offer interrupted another dialog")
	}
	m.modal = ""
	m.showSaveOffer()
	if m.modal != "profile-save" || m.saveName != "nas" || len(m.current().saveOffers) != 0 {
		t.Fatal("pending save offer was not presented")
	}
}

func TestLogViewerRestoresContext(t *testing.T) {
	for _, shortcut := range []rune{'n', 'l'} {
		for _, tabName := range []string{"", "files", "shell", "hovel"} {
			m := newFrame(launch.Info{Workspace: "/tmp/log-context"}, true, launch.Options{})
			defer m.terminals.close()
			frameEvent(m, tea.WindowSizeMsg{Width: 160, Height: 40})
			w := m.current()
			original := &cliTab{connection: "gateway", id: "1"}
			w.shells = []*cliTab{original}
			w.shell = original
			w.tab, w.focus = tabName, "prompt"
			if tabName == "shell" || tabName == "hovel" {
				w.focus = "terminal"
			}
			w.management.input.SetValue("unfinished command")
			focus := w.focus
			_, cmd := frameEvent(m, tea.KeyPressMsg{Code: shortcut, Mod: tea.ModCtrl})
			if cmd == nil || w.shell.logs == nil || w.tab != "shell" {
				t.Fatalf("Ctrl+%c did not open the viewer from %s", shortcut, tabName)
			}
			viewer := w.shell
			viewer.pending = false
			// Exercise normal terminal exit through the same event path as :q.
			m.terminalResult(m.active, cliScreen{viewer, ptyhost.Snapshot{Exited: true}})
			if w.tab != tabName || w.focus != focus || w.shell != original || w.management.input.Value() != "unfinished command" {
				t.Fatal("viewer lost previous context", tabName, w.tab, w.focus)
			}
			if strings.Contains(logVimrc(true), "highlight") || strings.Contains(logVimrc(true), "syntax match") {
				t.Fatal("NO_COLOR syntax enabled")
			}
			if !strings.Contains(logVimrc(false), "highlight burrowTimestamp guifg="+baseColor+" guibg="+blueColor) {
				t.Fatal("timestamp contrast missing")
			}
		}
	}
}

func TestLogVimRenderedColors(t *testing.T) {
	vim, err := exec.LookPath("vim")
	if err != nil {
		t.Fatal("Vim required for log presentation check", err)
	}
	const log = `2026-09-13T14:00:00-04:00 -- mget /data/*
  Target: gateway (tester@192.0.2.10:2222)
  COMPLETE · 2/2 files · 1.0 KiB · 2s elapsed
  Files: 2 completed · 0 failed · 0 cancelled
  Saved: /downloads/data

2026-09-19T14:00:00Z -- collected command
  Command: ps -elf
  Script: /uploads/check.sh
  Execution: local tool on daemon host
  Mode: stage
  Interpreter: /bin/sh
  Stage cleanup: kept
  Timeout: 30s
  Outcome: SUCCEEDED (exit 0)
  Capture: INCOMPLETE
  Run: retained-1
  Collection: collection-1
  Cancellation: unconfirmed; master unavailable
  Capture error: disk full
STDOUT · 123 stored / 456 received bytes
  File: /workspace/artifacts/output
    UID PID COMMAND
    root 1 init
  PREVIEW TRUNCATED: first 123 of 456 saved bytes
`
	for _, plain := range []bool{false, true} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "vimrc"), []byte(logVimrc(plain)), 0600)
		os.WriteFile(filepath.Join(dir, "operations.log"), []byte(log+strings.Repeat("Scrolling line\n", 100)), 0600)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.Command(vim, "-N", "-M", "-u", filepath.Join(dir, "vimrc"), "-i", "NONE", "-n", "--", filepath.Join(dir, "operations.log"))
		host, err := ptyhost.StartWithScrollback(ctx, cmd, 160, 40, 0)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		var snap ptyhost.Snapshot
		for {
			snap = host.Snapshot()
			if strings.Contains(ansi.Strip(snap.Screen), "Burrow logs snapshot") {
				break
			}
			if time.Now().After(deadline) {
				host.Close()
				cancel()
				t.Fatal("Vim did not render", snap.Screen)
			}
			time.Sleep(20 * time.Millisecond)
		}
		screen := vt.NewEmulator(160, 40)
		screen.Write([]byte(strings.ReplaceAll(snap.Screen, "\n", "\r\n")))
		for text, want := range map[string]string{"gateway": "#b4befe", "tester": "#a6e3a1", "192.0.2.10": "#f5c2e7", "2222": "#f9e2af", "COMPLETE": "#a6e3a1", "failed": "#f38ba8", "/downloads/data": "#a6adc8",
			"/uploads/check.sh": "#a6adc8", "stage": "#cba6f7", "local tool on daemon host": "#cba6f7", "/bin/sh": "#94e2d5", "kept": "#f9e2af", "30s": "#fab387",
			"ps -elf": blueColor, "-elf": blueColor, "SUCCEEDED": "#a6e3a1", "INCOMPLETE": "#f38ba8", "retained-1": lavenderColor, "collection-1": lavenderColor, "unconfirmed": "#f9e2af", "disk full": "#f38ba8", "STDOUT": blueColor, "123": "#fab387", "PREVIEW TRUNCATED": "#f9e2af", "/workspace/artifacts/output": "#a6adc8"} {
			if !plain {
				assertTextRole(t, screen, image.Rect(0, 1, 160, 26), text, want)
			}
			if !strings.Contains(screen.String(), text) {
				t.Fatal("viewer text lost", text, plain)
			}
		}
		if !strings.Contains(screen.String(), "2026-09-13T14:00:00-04:00 -- mget /data/*") {
			t.Fatal("log text changed")
		}
		cell := screen.CellAt(4, 0)
		if !plain && !colorMatches(cell.Style.Bg, lipgloss.Color(blueColor)) {
			t.Fatal("timestamp background missing", cell.Style.Bg, snap.Screen)
		}
		if plain && cell.Style.Bg != nil {
			t.Fatal("NO_COLOR timestamp background")
		}
		// Wheel input must reach Vim, not the host's empty alternate-screen history.
		if err := host.Send(uv.MouseWheelEvent{X: 20, Y: 10, Button: uv.MouseWheelDown}); err != nil {
			t.Fatal(err)
		}
		deadline = time.Now().Add(3 * time.Second)
		for strings.Contains(ansi.Strip(host.Snapshot().Screen), "2026-09-13T14:00:00") {
			if time.Now().After(deadline) {
				t.Fatal("mouse wheel did not scroll Vim")
			}
			time.Sleep(20 * time.Millisecond)
		}
		for _, size := range []image.Point{{200, 50}, {120, 30}, {80, 24}} {
			if err := host.Send(size); err != nil {
				t.Fatal(err)
			}
		}
		host.Close()
		cancel()
		screen.Close()
	}
}

func TestLogNotesSummarizeOperations(t *testing.T) {
	var records strings.Builder
	record := func(action, status, payload string) {
		fmt.Fprintf(&records, "2026-09-13T14:00:00-04:00 -- %s\n  Target: gateway\n  Status: %s\n  Result:\n    %s\nEnd record\n\n", action, status, payload)
	}
	record("get file /one", "complete", `{"source":"/one","state":"complete"}`)
	record("mget", "running", `{"state":"running"}`)
	record("inspect reverse listeners", "completed", `[]`)
	record("mget /data/*", "complete", `{"plan":{"operation":"mget","pattern":"/data/*","owner":{"name":"gateway","user":"tester","host":"example.test"}},"files":[{"state":"complete","destination":"/downloads/one"},{"state":"failed","destination":"/downloads/two"},{"state":"cancelled","destination":"/downloads/three"}],"state":"cancelled","bytes":1024,"elapsed":2,"detail":"one transfer failed"}`)
	record("scp gateway tree /data", "completed", `{"result":{"path":"/data","entries":[{}],"incomplete":true,"errors":["permission denied"]}}`)
	record("shell gateway", "opened", `{"state":"opened"}`)
	var notes strings.Builder
	if err := writeLogNotes(strings.NewReader(records.String()), &notes, "/workspace"); err != nil {
		t.Fatal(err)
	}
	got := notes.String()
	for _, want := range []string{"mget /data/*", "1/3 files", "1 completed · 1 failed · 1 cancelled", "Destination: /downloads", "INCOMPLETE · 1 entries", "permission denied", "OPENED"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	for _, unwanted := range []string{`"state"`, "get file", "inspect reverse", "RUNNING", "End record"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("backend detail %q leaked into notes: %s", unwanted, got)
		}
	}
	if strings.Count(got, " -- mget") != 1 {
		t.Fatal("batch summary repeated", got)
	}
	if err := writeLogNotes(strings.NewReader("2026-09-13T14:00:00Z -- incomplete\n"), &strings.Builder{}, "/workspace"); err == nil {
		t.Fatal("partial record accepted")
	}
}

func TestLogNoteFailedDownload(t *testing.T) {
	note, _, err := operationNote("get /one", "failed", "gateway", []byte(`{"plan":{"operation":"get","pattern":"/one"},"files":[{"state":"failed","destination":"/downloads/one","partial":"/downloads/one.partial","detail":"disk full"}],"state":"failed"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Destination: /downloads/one", "Partial: /downloads/one.partial", "disk full"} {
		if !strings.Contains(note, want) {
			t.Fatal(note)
		}
	}
	if strings.Contains(note, "Saved:") {
		t.Fatal("failed destination claimed saved", note)
	}
}
