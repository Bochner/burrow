package connection

import (
	"fmt"
	"strconv"

	"github.com/Bochner/burrow/core/reports"
)

func parseSurvey(args []string) (runRequest, bool, string, error) {
	r := runRequest{Execution: "remote", Budget: reports.MaxBytes, Input: RunInput{Survey: "ubuntu", Mode: "stream", Interpreter: "/bin/sh", Script: RunFile{Path: "builtin:survey/ubuntu"}, Timeout: "90s"}}
	bad := fmt.Errorf("expected run survey CONNECTION --os ubuntu [--timeout DURATION] [--budget BYTES] [--yes]")
	if len(args) < 5 || args[2] == "" {
		return r, false, "", bad
	}
	r.Connection.Name = args[2]
	yes := false
	seen := map[string]bool{}
	for i := 3; i < len(args); i++ {
		flag := args[i]
		if seen[flag] {
			return r, false, "", bad
		}
		seen[flag] = true
		if flag == "--yes" {
			yes = true
			continue
		}
		if i+1 >= len(args) {
			return r, false, "", bad
		}
		i++
		switch flag {
		case "--os":
			if args[i] != "ubuntu" {
				return r, false, "", fmt.Errorf("only --os ubuntu is supported")
			}
		case "--budget":
			var err error
			r.Budget, err = strconv.ParseInt(args[i], 10, 64)
			if err != nil || r.Budget <= 0 {
				return r, false, "", bad
			}
		case "--timeout":
			if args[i] == "" {
				return r, false, "", bad
			}
			r.Input.Timeout = args[i]
		default:
			return r, false, "", bad
		}
	}
	if !seen["--os"] {
		return r, false, "", bad
	}
	return r, yes, "", validateRunRequest(r)
}
