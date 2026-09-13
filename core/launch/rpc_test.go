package launch

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestRPCResponseBounds(t *testing.T) {
	for _, test := range []struct {
		method  string
		size    int
		refused bool
	}{
		{"GetDaemonInfo", 1 << 20, false}, {"GetDaemonInfo", 1<<20 + 1, true},
		{"Snapshot", 1<<20 + 1, false}, {"Snapshot", 64<<20 + 1, true},
	} {
		t.Run(fmt.Sprintf("%s-%d", test.method, test.size), func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			go func() {
				defer server.Close()
				request, err := http.ReadRequest(bufio.NewReader(server))
				if err != nil {
					return
				}
				io.Copy(io.Discard, request.Body)
				fmt.Fprintf(server, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", test.size)
				io.WriteString(server, strings.Repeat(" ", test.size-2)+"{}")
			}()
			var out map[string]any
			err := rpc(client, test.method, &out)
			if test.refused {
				if err == nil || !strings.Contains(err.Error(), test.method+" response exceeds") {
					t.Fatalf("missing bounded refusal: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
