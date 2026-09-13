package launch

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// Phase tracing is opt-in latency evidence for the acceptance lab. When
// BURROW_PHASE_TRACE names an absolute private file, every process on the
// connection path appends one line per completed phase: its process ID, the
// phase name, and monotonic begin/duration nanoseconds. Names are fixed
// program constants, validated command verbs, public RPC method names or a
// Hovel CLI verb; inputs, paths, workspace identities and secrets never enter
// the trace. Tracing never changes control flow or skips a check.
var trace = openTrace()

func openTrace() *os.File {
	path := os.Getenv("BURROW_PHASE_TRACE")
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil
	}
	fd, e := unix.Open(path, unix.O_WRONLY|unix.O_APPEND|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return nil
	}
	f := os.NewFile(uintptr(fd), path)
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || st.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		f.Close()
		return nil
	}
	return f
}

// Monotonic returns CLOCK_MONOTONIC nanoseconds, comparable across processes
// on one boot and with the lab's monotonic clock.
func Monotonic() int64 {
	var t unix.Timespec
	if unix.ClockGettime(unix.CLOCK_MONOTONIC, &t) != nil {
		panic("monotonic clock unavailable")
	}
	return t.Nano()
}

// Phase records one named step when tracing is enabled; call the result when
// the step completes. Without tracing it costs one nil check.
func Phase(name string) func() {
	if trace == nil {
		return func() {}
	}
	begin := Monotonic()
	return func() {
		fmt.Fprintf(trace, "{\"pid\":%d,\"phase\":%q,\"begin\":%d,\"ns\":%d}\n", os.Getpid(), name, begin, Monotonic()-begin)
	}
}
