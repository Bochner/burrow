package main

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/timer"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/Bochner/burrow/core/connection"
	"github.com/charmbracelet/x/ansi"
)

type downloadReviewReady struct {
	epoch uint64
	plan  connection.DownloadPlan
	err   error
}
type downloadsReady struct {
	value connection.Downloads
	err   error
}
type downloadStarted struct {
	mode  *fileMode
	value any
	err   error
}

func refreshDownloads(w string) tea.Cmd {
	return func() tea.Msg {
		ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		value, err := connection.DownloadHistory(ctx, w)
		return downloadsReady{value, err}
	}
}

func (m *frame) reviewDownload(u *ui, args []string) tea.Cmd {
	if len(args) < 2 || len(args) > 3 {
		u.output = "REFUSED: get REMOTE [LOCAL] · mget PATTERN [LOCAL_DIR]"
		return nil
	}
	f := u.files
	remote := args[1]
	if !path.IsAbs(remote) && !strings.HasPrefix(remote, "~") {
		remote = f.remote + "/" + remote
	}
	local := f.download + "/"
	if len(args) == 3 {
		local = args[2]
		if !filepath.IsAbs(local) {
			local = filepath.Join(f.download, local)
			if strings.HasSuffix(args[2], "/") {
				local += "/"
			}
		}
	}
	m.commandArgs = []string{"download"}
	m.downloadPlan = nil
	m.downloadMode = f
	m.reviewText = ""
	m.inputEpoch++
	epoch, w, state, operation := m.inputEpoch, m.active, f.state, args[0]
	m.modal = "review"
	m.formTitle = "Resolving download files and destination…"
	u.input.Reset()
	return m.dispatch(w, func() tea.Msg {
		ctx, stop := context.WithTimeout(context.Background(), 25*time.Second)
		defer stop()
		p, err := connection.ReviewDownloads(ctx, w, state, operation, remote, local)
		return downloadReviewReady{epoch, p, err}
	})
}

func (m *frame) acceptDownloadReview(w string, v downloadReviewReady) tea.Cmd {
	if w != m.active || m.modal != "review" || v.epoch != m.inputEpoch {
		return nil
	}
	u := m.workspaces[w].fileUI(m.downloadMode)
	if u == nil {
		m.dismissForm()
		return nil
	}
	if v.err != nil || len(v.plan.Files) == 0 {
		m.dismissForm()
		u.output = "No files match; nothing downloaded"
		if v.err != nil {
			u.output = "REFUSED: " + safe(v.err.Error())
		}
		return nil
	}
	m.downloadPlan = &v.plan
	m.modalOffset = 0
	return m.setForm("review", "Download recap", confirmForm("Download these files?", "", "Download", "Cancel"))
}

func downloadSize(n int64) string {
	if n < 0 {
		return "Unknown"
	}
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	for _, unit := range []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"} {
		value /= 1024
		if value < 1024 {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return fmt.Sprintf("%d B", n)
}

func (m *frame) downloadRecap(width int) string {
	u := m.current().management
	p := m.downloadPlan
	rows := make([][]string, 0, len(p.Files))
	overwrites, unknown := false, false
	var total int64
	for _, f := range p.Files {
		overwrites = overwrites || f.Existing != ""
	}
	for _, f := range p.Files {
		name := path.Base(f.Source)
		if target := filepath.Base(f.Destination); target != name {
			name += " → " + target
		}
		row := []string{safe(name), downloadSize(f.Size)}
		if overwrites {
			mark := "—"
			if f.Existing != "" {
				mark = "Replace"
			}
			row = append(row, mark)
		}
		rows = append(rows, row)
		if f.Size < 0 {
			unknown = true
		} else {
			total += f.Size
		}
	}
	headers := []string{"NAME", "SIZE"}
	if overwrites {
		headers = append(headers, "OVERWRITE")
	}
	size := downloadSize(total)
	if unknown {
		size += " + unknown"
	}
	label := "files"
	if len(p.Files) == 1 {
		label = "file"
	}
	return centered(u.paint(numberStyle, fmt.Sprintf("%d %s · %s total", len(p.Files), label, size)), width) + "\n\n" + u.dataTable("", headers, rows, width)
}

func (m *frame) openDownloads() tea.Cmd {
	m.modal, m.modalOffset = "downloads", 0
	return m.dispatch(m.active, refreshDownloads(m.active))
}

func (m *frame) downloadsViewport() viewport.Model {
	b := m.dialogBounds()
	return scrollBody(m.current().activeUI().downloadContent(b.Dx()-6), b.Dx()-6, b.Dy()-8, m.modalOffset)
}

func (m *frame) scrollDownloads(delta int) {
	v := m.downloadsViewport()
	v.SetYOffset(v.YOffset() + delta)
	m.modalOffset = v.YOffset()
}

func (m *frame) submitDownload() tea.Cmd {
	p, mode, w := *m.downloadPlan, m.downloadMode, m.active
	m.downloadPlan = nil
	m.dismissForm()
	if u := m.current().fileUI(mode); u != nil {
		u.output = "Starting approved download…"
	}
	m.modal, m.modalOffset = "downloads", 0
	return m.dispatch(w, func() tea.Msg {
		ctx, stop := context.WithTimeout(context.Background(), time.Minute)
		defer stop()
		d, err := connection.StartDownloads(ctx, p)
		return downloadStarted{mode, d, err}
	})
}

func (m *frame) acceptDownloads(w string, v downloadsReady) tea.Cmd {
	u := &m.workspaces[w].management
	u.downloadObserved = true
	u.downloadError = ""
	if v.err != nil {
		u.downloadError = "UNVERIFIED · " + safe(v.err.Error())
	} else {
		u.downloads = v.value
	}
	for _, f := range m.workspaces[w].fileViews {
		f.downloads, f.downloadError, f.downloadObserved = u.downloads, u.downloadError, true
	}
	// Bubbles owns the polling timer; copied bytes and monotonic elapsed belong
	// to the retained connection, so changing tabs cannot reset measurements.
	delay := 3 * time.Second
	for _, d := range u.downloads.Records {
		if d.State == "running" {
			delay = time.Second
			break
		}
	}
	m.workspaces[w].downloadTimer = timer.New(delay, timer.WithInterval(delay))
	return m.dispatch(w, m.workspaces[w].downloadTimer.Init())
}

func (m ui) downloadContent(w int) string {
	var b strings.Builder
	if m.downloads.Warning != "" {
		b.WriteString(m.paint(warningStyle, safe(m.downloads.Warning)) + "\n")
	}
	if m.files != nil && m.downloadObserved && m.downloadError == "" {
		total := m.downloads.Connections[m.files.state.Creation]
		b.WriteString(m.paint(numberStyle, fmt.Sprintf("Connection totals: %d completed files · %d bytes", total.Files, total.Bytes)) + "\n")
	}
	if m.downloadError != "" {
		b.WriteString(m.paint(errorStyle, m.downloadError) + "\nRates and outcomes below are last observed.\n")
	}
	if !m.downloadObserved {
		return b.String() + "Loading transfer records…"
	}
	count := 0
	for i := len(m.downloads.Records) - 1; i >= 0; i-- {
		d := m.downloads.Records[i]
		if m.files != nil && d.Plan.Owner.Creation != m.files.state.Creation {
			continue
		}
		count++
		stateStyle := warningStyle
		if d.State == "complete" {
			stateStyle = successStyle
		} else if d.State != "running" {
			stateStyle = errorStyle
		}
		b.WriteString(m.paint(accent, safe(d.ID)) + " · " + m.paint(stateStyle, strings.ToUpper(d.State)) + "\n")
		var total int64
		complete := 0
		for _, f := range d.Files {
			if f.Size < 0 {
				total = -1
			} else if total >= 0 {
				total += f.Size
			}
			if f.State == "complete" {
				complete++
			}
		}
		bar := m.downloadBar
		bar.SetWidth(max(1, min(w-16, 60)))
		fraction := 0.0
		if total > 0 {
			fraction = float64(d.Bytes) / float64(total)
		} else if d.State == "complete" {
			fraction = 1
		}
		view := bar.ViewAs(fraction)
		if m.noColor {
			view = ansi.Strip(view)
		}
		b.WriteString(m.paint(secondary, "Overall ") + view + "\n")
		eta := "Unknown"
		if d.ETA != nil && m.downloadError == "" {
			eta = (time.Duration(*d.ETA * float64(time.Second))).Round(time.Second).String()
		}
		b.WriteString(m.paint(numberStyle, fmt.Sprintf("%d/%d files · %d bytes · %s elapsed", complete, len(d.Files), d.Bytes, (time.Duration(d.Elapsed*float64(time.Second))).Round(time.Millisecond))) + "\n")
		if m.downloadError == "" {
			b.WriteString(m.paint(numberStyle, fmt.Sprintf("%.0f B/s interval avg · %.0f B/s overall avg", d.Rate, d.AverageRate)) + "\n")
		}
		b.WriteString(m.paint(secondary, "ETA (overall average): "+eta) + "\n")
		for _, f := range d.Files {
			style := warningStyle
			if f.State == "complete" {
				style = successStyle
			} else if f.State == "failed" || f.State == "cancelled" {
				style = errorStyle
			}
			b.WriteString(m.paint(style, strings.ToUpper(f.State)) + " " + m.paint(accent, safe(path.Base(f.Source))) + "\n")
			if f.State == "running" {
				fraction := 0.0
				if f.Size > 0 {
					fraction = float64(f.Bytes) / float64(f.Size)
				}
				view := bar.ViewAs(fraction)
				if m.noColor {
					view = ansi.Strip(view)
				}
				b.WriteString("Current " + view + "\n")
			}
			expected := "Unknown"
			if f.Size >= 0 {
				expected = fmt.Sprint(f.Size)
			}
			b.WriteString(m.paint(numberStyle, fmt.Sprintf("%d copied / %s expected bytes", f.Bytes, expected)) + "\n")
			if f.Partial != "" && f.State != "running" && f.State != "pending" {
				b.WriteString(m.paint(secondary, "Partial (if created): "+safe(f.Partial)) + "\n")
			}
			if f.Detail != "" {
				b.WriteString(m.paint(errorStyle, safe(f.Detail)) + "\n")
			}
		}
		if d.Detail != "" {
			b.WriteString(m.paint(errorStyle, safe(d.Detail)) + "\n")
		}
		b.WriteByte('\n')
	}
	if count == 0 {
		b.WriteString(m.paint(secondary, "No downloads yet") + "\n")
	}
	return b.String()
}

func newDownloadBar() progress.Model {
	return progress.New(progress.WithColors(warningStyle.GetForeground()), progress.WithWidth(40))
}
