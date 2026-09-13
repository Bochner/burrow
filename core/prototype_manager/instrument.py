"""Declared #71 baseline instrumentation; fail closed if source anchors change."""
from pathlib import Path
import sys


def replace(text, old, new):
    assert text.count(old) == 1, f"baseline anchor changed: {old!r}"
    return text.replace(old, new)


out = Path(sys.argv[1])
out.mkdir(parents=True, exist_ok=True)
for path in map(Path, sys.argv[2:]):
    text = path.read_text()
    if path.name == "connection_linux.go":
        text = replace(text, "type State struct {", "type State struct {\n Dispatch int64 `json:\"dispatch\"`\n Connected int64 `json:\"connected\"`")
        text = replace(text, "func runConnection(ctx *hovel.Context) (hovel.Result, error) {", "func runConnection(ctx *hovel.Context) (hovel.Result, error) {\n dispatch := phaseNow()")
        text = replace(text, "s.state = State{Name: c.Name,", "s.state = State{Dispatch: dispatch, Name: c.Name,")
        text = replace(text, 's.state.State = "connected"', 's.state.State = "connected"\n s.state.Connected = phaseNow()')
        text += "\nfunc phaseNow() int64 {var t unix.Timespec; if unix.ClockGettime(unix.CLOCK_MONOTONIC, &t) != nil {panic(\"monotonic clock unavailable\")}; return t.Nano()}\n"
    if path.name == "ssh_config.go":
        # Equivalent accepted #70 policy for both measurements, not a production edit.
        text = replace(text, "StrictHostKeyChecking ask", "StrictHostKeyChecking no")
        text = replace(text, "strconv.Quote(trustPath)", 'strconv.Quote("/dev/null")')
    (out / path.name).write_text(text)
