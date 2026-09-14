package connection

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Bochner/burrow/core/launch"
	"github.com/vibepwners/hovel/sdk/go/hovel"
)

// Log accepted control vocabulary, never arbitrary SDK input/config/auth bytes.
// Local inventory/profile/download polling does not contact the target. Reverse
// listener inspection is captured separately at reverseListeners.
func (m *manager) RunPayloadCommand(req hovel.PayloadCommandRequest) (result hovel.PayloadCommandResult, failure error) {
	var input any
	target := "manager " + m.Generation
	action := req.Command
	cleanup := false
	switch req.Command {
	case "forward":
		if len(req.Args) != 3 {
			return m.runPayloadCommand(req)
		}
		r, err := decodeForward(req.Args[0])
		if err != nil {
			return result, err
		}
		input = r
	case "unforward", "tunnel-check", "download-cancel", "shell":
		if len(req.Args) > 3 {
			return result, fmt.Errorf("unexpected control arguments")
		}
		input = req.Args
		cleanup = req.Command == "unforward" || req.Command == "download-cancel"
	default:
		return m.runPayloadCommand(req)
	}
	audit, err := launch.BeginAudit(m.Workspace, action, target, input)
	if err != nil && !cleanup {
		return result, err
	}
	defer func() {
		var value any = result
		if json.Valid([]byte(result.Stdout)) {
			value = json.RawMessage(result.Stdout)
		}
		failure = audit.Finish(value, failure)
		if err != nil {
			failure = fmt.Errorf("%v; cleanup logging unavailable: %w", failure, err)
		}
	}()
	return m.runPayloadCommand(req)
}

func targetLabel(s State) string {
	return fmt.Sprintf("%s (%s@%s:%d) creation=%s", s.Name, s.User, s.Host, s.Port, s.Creation)
}

func commandIdentity(args []string) string {
	// Only call after validation; never render rejected unknown options (secrets).
	var quoted []string
	for _, arg := range args {
		if strings.ContainsAny(arg, " \t\r\n\"\\") {
			b, _ := json.Marshal(arg)
			arg = string(b)
		}
		quoted = append(quoted, arg)
	}
	return strings.Join(quoted, " ")
}
