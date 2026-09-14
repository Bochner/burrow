package main

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/tree"
	"github.com/Bochner/burrow/core/connection"
)

const fileHelp = `# FILE BROWSING
get REMOTE [LOCAL]\tReview file, size and effective destination before downloading
mget PATTERN [LOCAL_DIR]\tReview all nonrecursive regular-file matches; sequential copies
downloads\tOpen the progress popup; Esc closes it without cancelling copies
download-cancel ID\tRequest cancellation and wait for acknowledged cleanup
Downloads continue during browsing, help, shell attachment and frontend detach.
logs / Ctrl+N opens saved operation logs; Ctrl+N or :q returns to this file context.
Overwrite is explicit in review; old files survive failed replacement.
Labelled partials remain after failure/cancel; working files are not registered evidence.
Retry a selected failed file with get SOURCE DESTINATION; copying restarts from zero.
Rates are byte measurements averaged over intervals; ETA uses the overall average.
Unknown totals or insufficient/stalled measurements show unknown ETA.
ls [PATH] / cd [PATH] / pwd\tList oldest first, navigate, or show the remote directory
tree [PATH]\tExplicit recursive scan; directory links are shown, never traversed
Tab / Shift+Tab\tCycle quoted path completions; discovery is cached and throttled
PgUp / PgDn\tScroll listing, tree, or history
Ctrl+C\tCancel current work, or return to management when idle
back / exit\tReturn to management without closing the SSH connection
history\tShow this connection's retained SCP commands and workspace local commands

# WORKSPACE ROOTS
local\tShow persistent upload/download roots
local download PATH / local upload PATH\tPersist an absolute root; never move or delete existing files
local PATH\tShorthand for changing the download root
lcd [download|upload] PATH\tNavigate within a root; defaults to download
lls [download|upload] [PATH]\tList the current local directory within that root
Navigation never changes roots. Local links must resolve within their selected root.
Broken and escaping links remain visible, with traversal refused.

# METADATA AND LOAD
Permissions, owner, group, size, modification time, name and link target are shown.
Hidden files are included; only . and .. are omitted. Hard-link counts are omitted.
Reduced metadata is labelled when account names are unavailable; numeric IDs remain.
Automatic discovery does not poll, scan recursively, or run find commands.
Explicit tree is capped at 3000 entries and 30 seconds; incomplete scans are labelled.
Large responses are refused explicitly: narrow the directory rather than assume completeness.
put remains in the upload ticket.
F1 / Esc\tClose help
`

// Navigation is frontend state; roots and transport identity remain workspace-owned.
type fileMode struct {
	saved                    ui
	state                    connection.State
	roots                    connection.FileRoots
	remote, upload, download string
	listing                  connection.FileListing
	tree                     *connection.FileTree
	sequence                 uint64
	request                  string
	cancel                   context.CancelFunc
	discoveryRequest         string
	historyView              bool
	cache                    map[string]fileObservation
	matches                  []string
	edit                     uint64
	lookup                   bool
	nextLookup               time.Time
}

type fileObservation struct {
	listing connection.FileListing
	at      time.Time
}
type fileDiscoveryTick struct {
	mode *fileMode
	edit uint64
}
type fileDiscovery struct {
	mode      *fileMode
	key, line string
	listing   connection.FileListing
	err       error
}

type fileResult struct {
	mode            *fileMode
	sequence        uint64
	operation, area string
	value           any
	roots           connection.FileRoots
	err             error
	history         []string
}

func (m *ui) openFiles(name string) tea.Cmd {
	for _, state := range m.connections {
		if state.Name != name || state.State != "connected" {
			continue
		}
		saved := *m
		m.files = &fileMode{saved: saved, state: state, remote: "~", cache: map[string]fileObservation{}}
		m.history, m.historyIndex, m.draft = nil, 0, ""
		m.input.Placeholder = "ls · cd · tree · local · back"
		m.input.SetSuggestions(m.suggestions())
		return m.fileCommand([]string{"cd", "~"})
	}
	m.busy = false
	m.output = "REFUSED: select a verified live connection; reconnect explicitly"
	return nil
}

func (m *ui) leaveFiles() tea.Cmd {
	f := m.files
	cmd := m.cancelFiles()
	m.files = nil
	m.input, m.history, m.historyIndex, m.draft = f.saved.input, f.saved.history, f.saved.historyIndex, f.saved.draft
	m.output, m.outputOffset = f.saved.output, f.saved.outputOffset
	m.busy = false
	m.input.SetWidth(max(1, m.width-4))
	m.input.SetSuggestions(m.suggestions())
	return cmd
}

func (m *ui) cancelFiles() tea.Cmd {
	f := m.files
	f.sequence++
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
	m.busy = false
	m.output = "Cancelled waiting; previous directory retained"
	w, state, id, discovery := m.info.Workspace, f.state, f.request, f.discoveryRequest
	f.request = ""
	f.discoveryRequest = ""
	if id == "" && discovery == "" {
		return nil
	}
	return func() tea.Msg {
		ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		var err error
		for _, request := range []string{id, discovery} {
			if request != "" {
				_, err = connection.Browse(ctx, w, state, connection.FileQuery{Operation: "cancel", ID: request})
			}
		}
		return fileResult{mode: f, sequence: 0, err: err}
	}
}

func (m *ui) fileCommand(args []string) tea.Cmd {
	f := m.files
	if len(args) == 0 {
		return nil
	}
	if len(args) == 2 && args[0] == "help" && (args[1] == "get" || args[1] == "mget") {
		m.input.Reset()
		m.help = true
		return nil
	}
	if len(args) == 1 {
		switch args[0] {
		case "logs":
			m.input.Reset()
			return func() tea.Msg { return logsRequested{} }
		case "back", "exit":
			return m.leaveFiles()
		case "help":
			m.input.Reset()
			m.help = true
			return nil
		case "quit":
			m.quitting = true
			return nil
		case "history":
			m.input.Reset()
			f.historyView = true
			m.outputOffset = 0
			return nil
		}
	}
	if m.busy {
		m.output = "Working; Ctrl+C cancels · draft retained"
		return nil
	}
	op, area := args[0], "download"
	var query connection.FileQuery
	local := op == "local" || op == "lcd" || op == "lls"
	if local {
		if err := connection.ValidateCommand(m.info.Workspace, args); err != nil {
			m.output = "REFUSED: " + safe(err.Error())
			return nil
		}
		args = append([]string(nil), args...)
		if op != "local" {
			rest := args[1:]
			if len(rest) > 0 && (rest[0] == "upload" || rest[0] == "download") {
				area, rest = rest[0], rest[1:]
			}
			base := f.download
			if area == "upload" {
				base = f.upload
			}
			p := base
			if len(rest) > 0 {
				p = rest[0]
				if !filepath.IsAbs(p) {
					p = filepath.Join(base, p)
				}
			}
			args = []string{op, area, p}
		}
	} else {
		if len(args) > 2 || (op != "ls" && op != "tree" && op != "cd" && op != "pwd") {
			m.output = "REFUSED: use get, mget, downloads, ls, tree, cd, pwd, local, lcd, lls or back"
			return nil
		}
		p := f.remote
		if len(args) == 2 {
			p = args[1]
			if !path.IsAbs(p) && !strings.HasPrefix(p, "~") {
				p = f.remote + "/" + p
			}
		} else if op == "cd" {
			p = "~"
		}
		query = connection.FileQuery{Operation: op, Path: p}
	}
	f.sequence++
	f.request = fmt.Sprintf("%d-%d", time.Now().UnixNano(), f.sequence)
	query.ID = f.request
	seq, state, w := f.sequence, f.state, m.info.Workspace
	f.historyView = false
	m.busy = true
	m.output = "Working… Ctrl+C cancels"
	m.input.Reset()
	m.completionValues = nil
	ctx, stop := context.WithTimeout(context.Background(), 40*time.Second)
	f.cancel = stop
	return func() tea.Msg {
		defer stop()
		roots, err := connection.TransferRoots(ctx, w)
		var value any
		if err == nil {
			if local {
				value, err = connection.Execute(ctx, w, args)
			} else {
				value, err = connection.Browse(ctx, w, state, query)
				if err == nil {
					err = connection.RecordFileCommand(ctx, w, []string{"scp", state.Name, op, query.Path})
				}
			}
		}
		if changed, ok := value.(connection.FileRoots); ok {
			roots = changed
		}
		history, _ := connection.FileHistory(ctx, w)
		return fileResult{mode: f, sequence: seq, operation: op, area: area, value: value, roots: roots, err: err, history: history}
	}
}

func (m *ui) acceptFiles(result fileResult) {
	f := m.files
	if f == nil || result.mode != f || result.sequence != f.sequence {
		return
	}
	m.busy = false
	f.cancel = nil
	f.request = ""
	if result.history != nil {
		m.history = nil
		for _, line := range result.history {
			args, e := connection.Split(line)
			if e != nil || len(args) == 0 {
				continue
			}
			if args[0] == "scp" {
				if len(args) < 3 || args[1] != f.state.Name {
					continue
				}
				args = args[2:]
			}
			m.history = append(m.history, connection.CommandLine(args))
		}
		m.historyIndex = len(m.history)
	}
	if result.roots.Upload != "" && f.roots.Upload != result.roots.Upload {
		f.upload = result.roots.Upload
		clear(f.cache)
	}
	if result.roots.Download != "" && f.roots.Download != result.roots.Download {
		f.download = result.roots.Download
		clear(f.cache)
	}
	if result.roots.Version != 0 {
		f.roots = result.roots
	}
	if result.err != nil {
		m.output = "REFUSED: " + safe(result.err.Error())
		return
	}
	m.output = ""
	m.outputOffset = 0
	switch value := result.value.(type) {
	case connection.FileListing:
		side := "remote"
		if result.operation == "lls" {
			side = result.area
		}
		if result.operation != "lcd" {
			f.cache[side+"\n"+value.Path] = fileObservation{value, time.Now()}
		}
		if result.operation == "cd" || result.operation == "pwd" {
			f.remote = value.Path
		}
		if result.operation == "lcd" {
			m.output = "Local " + result.area + " directory: " + value.Path
			if result.area == "upload" {
				f.upload = value.Path
			} else {
				f.download = value.Path
			}
		} else {
			f.listing = value
			f.tree = nil
		}
		if value.Notice != "" {
			m.output = value.Notice
		}
	case connection.FileTree:
		f.tree = &value
	case connection.FileRoots:
		m.output = "Workspace roots verified; existing files preserved"
	}
	m.input.SetSuggestions(m.suggestions())
	m.fileMatches()
}

// Parse only the final path argument. Completion emits canonical quoted commands,
// never shell source. Unclosed quotes are accepted here, not by command execution.
func (m ui) fileCompletionContext() (args []string, side, dir, prefix string, dirs bool) {
	line := m.input.Value()
	args, err := connection.Split(line)
	unfinished := err != nil
	if err != nil {
		for _, quote := range []string{"'", "\""} {
			args, err = connection.Split(line + quote)
			if err == nil {
				break
			}
		}
	}
	if err != nil || len(args) == 0 {
		return nil, "", "", "", false
	}
	if !unfinished && strings.HasSuffix(line, " ") {
		probe, _ := connection.Split(line + "x")
		if len(probe) > len(args) {
			args = append(args, "")
		}
	}
	op := args[0]
	side = "remote"
	at := 1
	dirs = op == "cd" || op == "tree" || op == "lcd"
	if op == "lcd" || op == "lls" {
		side = "download"
		if len(args) > 1 && (args[1] == "upload" || args[1] == "download") {
			side = args[1]
			at = 2
		}
	} else if (op == "get" || op == "mget") && len(args) == 3 {
		side = "download"
		at = 2
		dirs = op == "mget"
	} else if op != "ls" && op != "cd" && op != "tree" && op != "get" && op != "mget" {
		return nil, "", "", "", false
	}
	if len(args) != at+1 {
		return nil, "", "", "", false
	}
	p := args[at]
	slash := strings.LastIndex(p, "/")
	prefix = p[slash+1:]
	dir = p[:slash+1]
	base := m.files.remote
	if side == "upload" {
		base = m.files.upload
	} else if side == "download" {
		base = m.files.download
	}
	if dir == "" {
		dir = base
	} else if !path.IsAbs(dir) && !strings.HasPrefix(dir, "~") {
		dir = strings.TrimSuffix(base, "/") + "/" + dir
	}
	return args, side, dir, prefix, dirs
}

func (m *ui) fileMatches() (string, string, string) {
	f := m.files
	args, side, dir, prefix, dirs := m.fileCompletionContext()
	f.matches = nil
	if side == "" {
		for _, s := range m.suggestions() {
			if strings.HasPrefix(s, m.input.Value()) {
				f.matches = append(f.matches, s)
			}
		}
		return "", "", ""
	}
	k := side + "\n" + dir
	if dir != "/" {
		k = side + "\n" + strings.TrimSuffix(dir, "/")
	}
	if cached, ok := f.cache[k]; ok && time.Since(cached.at) < 30*time.Second {
		for _, e := range cached.listing.Entries {
			if !strings.HasPrefix(e.Name, prefix) || (dirs && !e.Directory) || e.Error != "" {
				continue
			}
			p := args[len(args)-1]
			slash := strings.LastIndex(p, "/")
			p = p[:slash+1] + e.Name
			if e.Directory {
				p += "/"
			}
			completed := append([]string(nil), args...)
			completed[len(completed)-1] = p
			// A terminal cannot round-trip control bytes through its line editor.
			if safe(p) != p {
				continue
			}
			f.matches = append(f.matches, connection.CommandLine(completed))
		}
		return "", "", ""
	}
	return k, side, dir
}

func (m *ui) scheduleFileCompletion() tea.Cmd {
	if m.files == nil {
		return nil
	}
	f := m.files
	f.edit++
	m.fileMatches()
	edit := f.edit
	return tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg { return fileDiscoveryTick{f, edit} })
}

func (m *ui) discoverFiles(tick fileDiscoveryTick) tea.Cmd {
	f := m.files
	if f == nil || tick.mode != f || tick.edit != f.edit || f.lookup || m.busy || time.Now().Before(f.nextLookup) {
		return nil
	}
	k, side, dir := m.fileMatches()
	if k == "" {
		return nil
	}
	f.lookup = true
	f.nextLookup = time.Now().Add(time.Second)
	f.discoveryRequest = fmt.Sprintf("completion-%d", time.Now().UnixNano())
	id := f.discoveryRequest
	w, state, line := m.info.Workspace, f.state, m.input.Value()
	return func() tea.Msg {
		ctx, stop := context.WithTimeout(context.Background(), 35*time.Second)
		defer stop()
		var value any
		var err error
		if side == "remote" {
			value, err = connection.Browse(ctx, w, state, connection.FileQuery{Operation: "complete", Path: dir, ID: id})
		} else {
			value, err = connection.LocalListing(ctx, w, side, dir)
		}
		listing, _ := value.(connection.FileListing)
		return fileDiscovery{f, k, line, listing, err}
	}
}

func (m *ui) acceptDiscovery(result fileDiscovery) tea.Cmd {
	f := m.files
	if f == nil || f != result.mode {
		return nil
	}
	f.lookup = false
	f.discoveryRequest = ""
	if result.listing.Notice == "Discovery throttled" || result.listing.Notice == "Discovery already running" {
		m.output = result.listing.Notice + "; press Tab after a moment"
		return nil
	}
	// Negative observations are cached too; typing/Tab cannot hammer denied paths.
	if len(f.cache) >= 128 {
		clear(f.cache)
	}
	f.cache[result.key] = fileObservation{result.listing, time.Now()}
	if result.err != nil {
		m.output = "Completion unavailable: " + safe(result.err.Error())
	}
	m.fileMatches()
	if m.input.Value() != result.line {
		return m.scheduleFileCompletion()
	}
	return nil
}

func (m ui) fileState() string {
	status := "unavailable"
	for _, s := range m.connections {
		if s.Creation == m.files.state.Creation && s.Generation == m.files.state.Generation {
			status = s.State
		}
	}
	if m.connectionError != "" {
		status = "unverified"
	}
	return status
}

func (m ui) fileContent(w int) string {
	f := m.files
	var b strings.Builder
	b.WriteString(m.paint(heading, "FILE MODE") + " · " + m.paint(accent, safe(f.state.Name)) + " · " + m.paint(successStyle, safe(f.state.User)) + "@" + m.paint(hostStyle, safe(f.state.Host)) + "\n")
	status := m.fileState()
	if status != "connected" {
		b.WriteString(m.paint(errorStyle, strings.ToUpper(status)+" · previous listing retained; reconnect explicitly") + "\n")
	}
	b.WriteByte('\n')
	for _, field := range [][2]string{{"Remote", f.remote}, {"Download root", f.roots.Download}, {"Upload root", f.roots.Upload}} {
		value := field[1]
		if value == "" {
			value = "Unavailable"
		}
		b.WriteString(m.paint(accent, fmt.Sprintf("%-16s", field[0]+":")) + m.paint(secondary, safe(value)) + "\n")
	}
	if f.download != f.roots.Download {
		b.WriteString(m.paint(accent, "Download cwd:   ") + m.paint(secondary, safe(f.download)) + "\n")
	}
	if f.upload != f.roots.Upload {
		b.WriteString(m.paint(accent, "Upload cwd:     ") + m.paint(secondary, safe(f.upload)) + "\n")
	}
	b.WriteString(m.paint(accent, "Resources:      ") + m.paint(numberStyle, fmt.Sprint(len(m.connections))) + m.paint(secondary, " connections · ") + m.paint(numberStyle, fmt.Sprint(len(m.tunnels))) + m.paint(secondary, " tunnels") + "\n\n")
	if m.output != "" {
		style := warningStyle
		if strings.HasPrefix(m.output, "REFUSED:") || strings.HasPrefix(m.output, "Completion unavailable:") {
			style = errorStyle
		}
		b.WriteString(m.paint(style, safe(m.output)) + "\n\n")
	}
	if f.lookup {
		b.WriteString(m.paint(secondary, "Discovering paths… prompt remains editable") + "\n")
	}
	if f.historyView {
		b.WriteString(m.paint(heading, "SCP HISTORY") + "\n")
		for _, line := range m.history[min(m.outputOffset, len(m.history)):] {
			b.WriteString(m.syntax(safe(line), false) + "\n")
		}
	} else if f.tree != nil {
		b.WriteString(m.paint(heading, "TREE ") + m.paint(secondary, safe(f.tree.Path)) + "\n")
		if f.tree.Incomplete {
			b.WriteString(m.paint(warningStyle, "INCOMPLETE: "+safe(strings.Join(f.tree.Errors, "; "))) + "\n")
		}
		root := tree.Root(m.paint(secondary, safe(f.tree.Path)))
		nodes := map[string]*tree.Tree{f.tree.Path: root}
		for _, e := range f.tree.Entries {
			label := m.fileName(e)
			node := tree.Root(label)
			nodes[e.Path] = node
			if parent := nodes[path.Dir(e.Path)]; parent != nil {
				parent.Child(node)
			}
		}
		lines := strings.Split(root.String(), "\n")
		b.WriteString(strings.Join(lines[min(m.outputOffset, len(lines)):], "\n"))
	} else {
		if f.listing.Notice != "" && f.listing.Notice != m.output {
			b.WriteString(m.paint(warningStyle, safe(f.listing.Notice)) + "\n")
		}
		var rows [][]string
		start := min(m.outputOffset, len(f.listing.Entries))
		for _, e := range f.listing.Entries[start:min(len(f.listing.Entries), start+max(1, m.height-12))] {
			name := m.fileName(e)
			permissions, owner, group, size, modified := e.Permissions, safe(e.Owner), safe(e.Group), fmt.Sprint(e.Size), e.Modified.Format("2006-01-02 15:04:05")
			if e.Permissions == "" {
				permissions, owner, group, size = "Unavailable", "Unavailable", "Unavailable", "Unavailable"
			}
			if e.Modified.IsZero() {
				modified = "Unavailable"
			}
			rows = append(rows, []string{m.filePermissions(permissions), owner, group, size, modified, name})
		}
		b.WriteString(m.dataTable("LISTING "+safe(f.listing.Path), []string{"PERMISSIONS", "OWNER", "GROUP", "SIZE", "MODIFIED", "NAME"}, rows, w))
		if len(f.listing.Entries) == 0 {
			b.WriteString("\n" + m.paint(secondary, "No entries"))
		}
		b.WriteString("\n" + m.paint(secondary, fmt.Sprintf("%d entries · oldest first · PgUp/PgDn scroll", len(f.listing.Entries))))
	}
	return b.String()
}

// Eza's file-kind/permission distinction, using Burrow's shared Mocha roles.
// Classification uses the observed mode, not filename guesses or extra remote IO.
func fileKindStyle(e connection.FileEntry) lipgloss.Style {
	if e.Error != "" {
		return errorStyle
	}
	if e.Link != "" || strings.HasPrefix(e.Permissions, "l") {
		return infoStyle
	}
	if e.Directory || strings.HasPrefix(e.Permissions, "d") {
		return heading
	}
	if len(e.Permissions) == 10 {
		switch e.Permissions[0] {
		case 'p':
			return warningStyle
		case 's':
			return keywordStyle
		case 'b', 'c':
			return numberStyle
		case '-':
			if strings.ContainsAny(e.Permissions[1:], "xst") {
				return successStyle
			}
		default:
			return keywordStyle
		}
	}
	return pageStyle
}

func (m ui) fileName(e connection.FileEntry) string {
	name := safe(e.Name)
	if e.Directory && e.Link == "" {
		name += "/"
	}
	text := m.paint(fileKindStyle(e), name)
	if e.Link != "" {
		text += m.paint(secondary, " → ") + m.paint(infoStyle, safe(e.Link))
	}
	if e.Error != "" {
		text += m.paint(errorStyle, " ["+safe(e.Error)+"]")
	}
	return text
}

func (m ui) filePermissions(mode string) string {
	if len(mode) != 10 {
		return m.paint(secondary, safe(mode))
	}
	var b strings.Builder
	for i, ch := range mode {
		style := secondary
		if i == 0 {
			style = fileKindStyle(connection.FileEntry{Permissions: mode})
		} else {
			switch ch {
			case 'r':
				style = warningStyle
			case 'w':
				style = errorStyle
			case 'x':
				style = successStyle
			case 's', 'S', 't', 'T':
				style = keywordStyle
			}
		}
		b.WriteString(m.paint(style, string(ch)))
	}
	return b.String()
}
