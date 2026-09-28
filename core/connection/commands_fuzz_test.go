package connection

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"unicode/utf8"
)

func TestMain(m *testing.M) {
	flag.Parse()
	// Keep minimized failures in Bazel's retained test outputs, never runfiles.
	// Checked-in regressions are replayed alongside the seeds on every smoke run.
	if output := os.Getenv("TEST_UNDECLARED_OUTPUTS_DIR"); output != "" {
		if flag.Lookup("test.fuzzworker").Value.String() != "true" {
			if err := os.CopyFS(filepath.Join(output, "testdata"), os.DirFS("testdata")); err != nil && !os.IsNotExist(err) {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		if err := os.Chdir(output); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	flag.Set("test.fuzzcachedir", "fuzz-cache")
	os.Exit(m.Run())
}

func FuzzCommandLine(f *testing.F) {
	for _, seed := range []string{
		"", "files node ls '/a b'", `run node -- printf '%s' 'a'"'"'b'`,
		`'' "" a\ b`, "\t界é\n", `$(touch /tmp/never) ; | &`,
		"'unfinished", `"unfinished`, "trailing\\", "\x00\xff",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, line string) {
		// A literal argument must not split into options/commands on recall.
		if utf8.ValidString(line) {
			quoted := CommandLine([]string{line})
			got, err := Split(quoted)
			if err != nil || !slices.Equal(got, []string{line}) {
				t.Fatalf("literal argument changed: input=%q got=%q error=%v", line, got, err)
			}
			for _, suffix := range []string{"'", "\"", "\\"} {
				if got, err := Split(quoted + suffix); err == nil || len(got) != 0 {
					t.Fatalf("unfinished quote/escape accepted: %q => %q, %v", quoted+suffix, got, err)
				}
			}
		}
		args, err := Split(line)
		if err != nil {
			if len(args) != 0 {
				t.Fatalf("malformed input returned executable partial arguments: %q", args)
			}
			return
		}
		got, err := Split(CommandLine(args))
		if err != nil || !slices.Equal(got, args) {
			t.Fatalf("recall changed arguments: input=%q want=%q got=%q error=%v", line, args, got, err)
		}
	})
}
