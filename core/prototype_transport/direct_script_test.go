package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/vibepwners/hovel/sdk/go/hovel"
)

func TestDirectOutputCapture(t *testing.T) {
	for _, scenario := range []string{"full", "budget", "write-failure"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("BURROW_OWNER_ROOT", root)
			dir, err := os.MkdirTemp(root, "direct-output-")
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := os.OpenFile(filepath.Join(dir, "stdout"), os.O_CREATE|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer stdout.Close()
			stderr, err := os.OpenFile(filepath.Join(dir, "stderr"), os.O_CREATE|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer stderr.Close()
			d := &directRun{dir: dir, stdout: stdout, stderr: stderr, limit: 4 * 1024 * 1024, failWrite: scenario == "write-failure"}
			if scenario == "budget" {
				d.limit = 1024
			}
			data := bytes.Repeat([]byte{0, 1, 2, 255}, 512*1024)
			if n, err := io.Copy(directOutput{d, 0}, bytes.NewReader(data)); err != nil || n != int64(len(data)) {
				t.Fatalf("drain: %d %v", n, err)
			}
			fields := map[string]string{"stdoutPath": stdout.Name(), "stderrPath": stderr.Name(), "outputFormat": "markdown"}
			artifacts, err := directArtifacts(hovel.PayloadCommandResult{Fields: fields})
			if err != nil || len(artifacts) != 3 {
				t.Fatalf("file artifacts: %v %v", artifacts, err)
			}
			if artifacts[1].Name != "script-report.md" || artifacts[1].Kind != "text/markdown" || artifacts[1].Path != stdout.Name() || artifacts[1].Data != "" {
				t.Fatalf("metadata: %+v", artifacts[1])
			}
			stored, err := os.ReadFile(stdout.Name())
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "full" {
				if d.outputError == "" || int64(len(stored)) > d.limit {
					t.Fatalf("storage failure not explicit/bounded: %q %d", d.outputError, len(stored))
				}
				return
			}
			if d.outputError != "" || !bytes.Equal(stored, data) {
				t.Fatal("full output lost")
			}
			if d.preview[0].data.Len() != 65536 {
				t.Fatal("preview is not bounded")
			}
			// A viewer can start after the preview boundary and resume through all data.
			for offset := int64(65536); offset < int64(len(data)); {
				result, err := d.readOutput(hovel.PayloadCommandRequest{Command: "script-output", Args: []string{"stdout", strconv.FormatInt(offset, 10)}}, false)
				if err != nil {
					t.Fatal(err)
				}
				chunk, err := base64.StdEncoding.DecodeString(result.Stdout)
				if err != nil || len(chunk) == 0 || len(chunk) > 32768 || !bytes.Equal(chunk, data[offset:offset+int64(len(chunk))]) {
					t.Fatal("live read lost bytes")
				}
				offset += int64(len(chunk))
			}
			for _, args := range [][]string{{"../stdout", "0"}, {"stdout", "-1"}} {
				if _, err := d.readOutput(hovel.PayloadCommandRequest{Args: args}, false); err == nil {
					t.Fatal("invalid output selection accepted")
				}
			}
			fields["stdoutPath"] = filepath.Join(root, "outside")
			if _, err := directArtifacts(hovel.PayloadCommandResult{Fields: fields}); err == nil {
				t.Fatal("unowned artifact path accepted")
			}
		})
	}
}
