package connection

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// Exercise the actual OpenSSH mux client, including cancel's zero exit on refusal.
func TestForwardControlAcknowledgement(t *testing.T) {
	for _, test := range []struct{ command, direction string }{{"forward", "L"}, {"cancel", "L"}, {"forward", "D"}, {"cancel", "D"}} {
		command := test.command
		for _, reply := range []string{"ok", "rejected", "lost"} {
			t.Run(test.direction+"/"+command+"/"+reply, func(t *testing.T) {
				dir, err := os.MkdirTemp("", "burrow-mux-")
				if err != nil {
					t.Fatal(err)
				}
				defer os.RemoveAll(dir)
				path := filepath.Join(dir, "s")
				listener, err := net.Listen("unix", path)
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				done := make(chan error, 1)
				go func() {
					c, err := listener.Accept()
					if err != nil {
						done <- err
						return
					}
					defer c.Close()
					read := func() ([]byte, error) {
						var length uint32
						if err := binary.Read(c, binary.BigEndian, &length); err != nil {
							return nil, err
						}
						if length > 4096 {
							return nil, io.ErrShortBuffer
						}
						b := make([]byte, length)
						_, err := io.ReadFull(c, b)
						return b, err
					}
					write := func(b []byte) error {
						if err := binary.Write(c, binary.BigEndian, uint32(len(b))); err != nil {
							return err
						}
						_, err := c.Write(b)
						return err
					}
					// MUX_MSG_HELLO, protocol version 4.
					if _, err = read(); err == nil {
						err = write([]byte{0, 0, 0, 1, 0, 0, 0, 4})
					}
					var request []byte
					if err == nil {
						request, err = read()
					}
					if err == nil && len(request) < 8 {
						err = io.ErrUnexpectedEOF
					}
					if err == nil && reply != "lost" {
						response := append([]byte{0x80, 0, 0, 1}, request[4:8]...)
						if reply == "rejected" {
							response[3] = 3 // MUX_S_FAILURE
							message := "Port forwarding failed"
							response = binary.BigEndian.AppendUint32(response, uint32(len(message)))
							response = append(response, message...)
						}
						err = write(response)
					}
					done <- err
				}()
				s := owner{state: State{Socket: path}}
				err = s.forwardControl(command, Tunnel{Direction: test.direction, Listen: "127.0.0.1:8123", Destination: "localhost:2222"})
				if (err == nil) != (reply == "ok") {
					t.Fatalf("%s acknowledgement: %v", reply, err)
				}
				if errors.Is(err, errForwardUncertain) != (reply == "lost") {
					t.Fatalf("incorrect certainty for %s: %v", reply, err)
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
