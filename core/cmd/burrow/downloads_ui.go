package main

import (
	"context"
	"fmt"
	"image/color"
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
		u.output = "REFUSED: get REMOTE [LOCAL] · mget PATTERN [LOCAL_DIR] · put LOCAL [REMOTE]"
		return nil
	}
	f := u.files
	operation, remote, local := args[0], args[1], f.download+"/"
	if operation == "put" {
		source := remote
		if !filepath.IsAbs(source) {
			source = filepath.Join(f.upload, source)
		}
		destination := path.Join(f.remote, path.Base(source))
		if len(args) == 3 {
			destination = args[2]
			if !path.IsAbs(destination) && !strings.HasPrefix(destination, "~") {
				destination = path.Join(f.remote, destination)
			}
		}
		remote, local = source, destination
	} else {
		if !path.IsAbs(remote) && !strings.HasPrefix(remote, "~") {
			remote = f.remote + "/" + remote
		}
		if len(args) == 3 {
			local = args[2]
			if !filepath.IsAbs(local) {
				local = filepath.Join(f.download, local)
				if strings.HasSuffix(args[2], "/") {
					local += "/"
				}
			}
		}
	}
	m.commandArgs = []string{"download"}
	m.downloadPlan = nil
	m.downloadMode = f
	m.reviewText = ""
	m.inputEpoch++
	epoch, w, state := m.inputEpoch, m.active, f.state
	m.modal = "review"
	m.formTitle = "Resolving transfer source and destination…"
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
	count, noun := len(v.plan.Files), "files"
	if count == 1 {
		noun = "file"
	}
	destination := v.plan.Files[0].Destination
	if count > 1 {
		destination = filepath.Dir(destination)
	}
	title, action := "Download recap", "Download"
	if v.plan.Operation == "put" {
		title, action = "Upload recap", "Upload"
	}
	question := fmt.Sprintf("%s %d %s to %s?", action, count, noun, safe(destination))
	return m.setForm("review", title, confirmForm(question, "", action, "Cancel"))
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

func downloadTotalSize(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	for _, unit := range []string{"KB", "MB", "GB", "TB", "PB", "EB"} {
		value /= 1000
		if value < 1000 {
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
		row := []string{safe(name)}
		if p.Operation == "put" {
			row = append(row, safe(f.Destination))
		} else if target := filepath.Base(f.Destination); target != name {
			name += " → " + target
			row[0] = safe(name)
		}
		row = append(row, downloadSize(f.Size))
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
	if p.Operation == "put" {
		headers = []string{"NAME", "DESTINATION PATH", "SIZE"}
	}
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
		u.output = "Starting approved transfer…"
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
		b.WriteString(m.paint(numberStyle, fmt.Sprintf("Connection totals: %d completed files · %s", total.Files, downloadTotalSize(total.Bytes))) + "\n")
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
		for _, f := range d.Files {
			if f.Size < 0 {
				total = -1
			} else if total >= 0 {
				total += f.Size
			}
		}
		b.WriteString(m.transferRow(w, "Overall progress", transferFraction(d.Bytes, total, d.State), transferAmount(d.Bytes, total), transferRate(d.AverageRate), transferClock(d.Elapsed)) + "\n")
		eta := "Unknown"
		if d.State == "complete" {
			eta = "0:00:00"
		} else if d.ETA != nil && m.downloadError == "" {
			eta = transferClock(*d.ETA)
		}
		b.WriteString(m.paint(secondary, fmt.Sprintf("%*s", w, "ETA "+eta+" · overall average")) + "\n")
		for _, f := range d.Files {
			action := "Downloading "
			if d.Plan.Operation == "put" {
				action = "Uploading "
			}
			rate, clock := f.AverageRate, transferClock(f.Elapsed)
			if f.Started.IsZero() {
				clock = "—"
			}
			b.WriteString(m.transferRow(w, action+safe(path.Base(f.Source)), transferFraction(f.Bytes, f.Size, f.State), transferAmount(f.Bytes, f.Size), transferRate(rate), clock) + "\n")
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
		b.WriteString(m.paint(secondary, "No transfers yet") + "\n")
	}
	return b.String()
}

func transferFraction(copied, total int64, state string) float64 {
	if total > 0 {
		return min(1, float64(copied)/float64(total))
	}
	if state == "complete" {
		return 1
	}
	return 0
}

func transferAmount(copied, total int64) string {
	if total < 0 {
		return downloadTotalSize(copied) + "/?"
	}
	unit, divisor := "B", float64(1)
	for _, next := range []string{"KB", "MB", "GB", "TB", "PB", "EB"} {
		if float64(total)/divisor < 1000 {
			break
		}
		unit, divisor = next, divisor*1000
	}
	if divisor == 1 {
		return fmt.Sprintf("%d/%d %s", copied, total, unit)
	}
	return fmt.Sprintf("%.1f/%.1f %s", float64(copied)/divisor, float64(total)/divisor, unit)
}

func transferRate(rate float64) string {
	if rate <= 0 {
		return "?"
	}
	return downloadTotalSize(int64(rate)) + "/s"
}

func transferClock(seconds float64) string {
	total := int64(seconds + .5)
	return fmt.Sprintf("%d:%02d:%02d", total/3600, total/60%60, total%60)
}

func transferCell(value string, width int) string {
	value = ansi.Truncate(value, width, "…")
	return value + strings.Repeat(" ", max(0, width-ansi.StringWidth(value)))
}

func (m ui) transferRow(width int, label string, fraction float64, amount, rate, elapsed string) string {
	stats := fmt.Sprintf("%6.1f%%  %17s  %10s  %8s", fraction*100, amount, rate, elapsed)
	labelWidth := min(36, max(18, width/3))
	barWidth := width - labelWidth - ansi.StringWidth(stats) - 2
	bar := m.downloadBar
	if barWidth < 8 {
		barWidth = max(8, width-ansi.StringWidth(stats)-1)
		labelWidth = width
	}
	bar.SetWidth(barWidth)
	view := bar.ViewAs(fraction)
	if m.noColor {
		view = ansi.Strip(view)
	}
	row := transferCell(label, labelWidth) + " " + view + " " + m.paint(numberStyle, stats)
	if labelWidth == width {
		row = transferCell(label, labelWidth) + "\n" + view + " " + m.paint(numberStyle, stats)
	}
	return row
}

func newDownloadBar() progress.Model {
	return progress.New(
		progress.WithColorFunc(func(total, _ float64) color.Color {
			if total < .5 {
				return errorStyle.GetForeground()
			}
			if total < 1 {
				return warningStyle.GetForeground()
			}
			return successStyle.GetForeground()
		}),
		progress.WithFillCharacters('━', '─'),
		progress.WithoutPercentage(),
		progress.WithWidth(40),
	)
}
