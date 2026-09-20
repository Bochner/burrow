// Disposable typed-session candidate. Hovel owns the session registry/lifecycle;
// only this session owns its terminal bytes. Native reads return no duplicate log.
package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/vibepwners/hovel/sdk/go/hovel"
)

type controlled struct {
	*shell
	mu                sync.Mutex
	data              []byte
	end, generation   uint64
	controller, token string
	drained           bool
}

func (s *controlled) TerminalPTYSession() bool { return false }
func (s *controlled) Write([]byte) error       { return fmt.Errorf("use controller-fenced input command") }
func (s *controlled) Read(wait time.Duration) ([]byte, error) {
	if wait != 0 {
		time.Sleep(50 * time.Millisecond)
	}
	return nil, nil
}
func (s *controlled) Open() error {
	if err := s.shell.Open(); err != nil {
		return err
	}
	go func() {
		for {
			data, _ := s.PTYSession.Read(50 * time.Millisecond)
			s.mu.Lock()
			s.end += uint64(len(data))
			s.data = append(s.data, data...)
			// ponytail: copy a 64 KiB suffix; use ring storage if throughput needs it.
			if len(s.data) > 65536 {
				s.data = append([]byte(nil), s.data[len(s.data)-65536:]...)
			}
			s.drained = len(data) == 0 && s.Closed()
			done := s.drained
			s.mu.Unlock()
			if done {
				return
			}
		}
	}()
	return nil
}
func (s *controlled) ListPayloadCommands(hovel.PayloadCommandListRequest) ([]hovel.PayloadCommand, error) {
	return []hovel.PayloadCommand{{Name: "observe", ReadOnly: true}, {Name: "control"}, {Name: "input"}, {Name: "resize"}, {Name: "release"}}, nil
}
func (s *controlled) RunPayloadCommand(req hovel.PayloadCommandRequest) (hovel.PayloadCommandResult, error) {
	if len(req.Config) != 0 || req.InputData != "" || req.InputEncoding != "" || req.InputPath != "" || req.Reconnect != nil || req.InstalledPayloadID != "" {
		return hovel.PayloadCommandResult{}, fmt.Errorf("prototype commands accept only positional arguments")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var value any
	switch {
	case req.Command == "observe" && len(req.Args) == 1:
		cursor, err := strconv.ParseUint(req.Args[0], 10, 64)
		if err != nil || cursor > s.end {
			return hovel.PayloadCommandResult{}, fmt.Errorf("invalid cursor")
		}
		oldest := s.end - uint64(len(s.data))
		gap := cursor < oldest
		var data []byte
		if !gap {
			data = s.data[cursor-oldest:]
		}
		value = map[string]any{"data": data, "oldest": oldest, "next": s.end, "gap": gap, "closed": s.drained, "controller": s.controller, "generation": s.generation}
	case req.Command == "control" && len(req.Args) == 4:
		generation, err := strconv.ParseUint(req.Args[0], 10, 64)
		label := req.Args[1]
		if err != nil || generation != s.generation || len(label) == 0 || len(label) > 64 || strings.IndexFunc(label, unicode.IsControl) >= 0 {
			return hovel.PayloadCommandResult{}, fmt.Errorf("controller changed or invalid label; inspect before explicit takeover")
		}
		if _, err := s.shell.RunPayloadCommand(hovel.PayloadCommandRequest{Command: "resize", Args: req.Args[2:]}); err != nil {
			return hovel.PayloadCommandResult{}, err
		}
		s.generation++
		s.controller, s.token = label, rand.Text()
		value = map[string]any{"generation": s.generation, "token": s.token}
	case (req.Command == "input" && len(req.Args) == 2) || (req.Command == "resize" && len(req.Args) == 3) || (req.Command == "release" && len(req.Args) == 1):
		if s.token == "" || req.Args[0] != s.token || s.Closed() {
			return hovel.PayloadCommandResult{}, fmt.Errorf("controller changed or shell closed; input was not sent")
		}
		switch req.Command {
		case "input":
			data, err := base64.StdEncoding.DecodeString(req.Args[1])
			if err != nil || len(data) == 0 || len(data) > 4096 {
				return hovel.PayloadCommandResult{}, fmt.Errorf("expected 1..4096 base64 input bytes")
			}
			if err := s.PTYSession.Write(data); err != nil {
				return hovel.PayloadCommandResult{}, err
			}
			value = map[string]any{"acceptedBytes": len(data)} // Not an execution result.
		case "resize":
			if _, err := s.shell.RunPayloadCommand(hovel.PayloadCommandRequest{Command: "resize", Args: req.Args[1:]}); err != nil {
				return hovel.PayloadCommandResult{}, err
			}
			value = map[string]bool{"resized": true}
		case "release":
			s.controller, s.token = "", ""
			s.generation++
			value = map[string]bool{"released": true}
		}
	default:
		return hovel.PayloadCommandResult{}, fmt.Errorf("invalid prototype command or arguments")
	}
	data, err := json.Marshal(value)
	return hovel.PayloadCommandResult{Command: req.Command, Stdout: string(data)}, err
}
