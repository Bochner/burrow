package terminal

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestSharedInputModes(t *testing.T) {
	for _, test := range []struct {
		name  string
		mode  int
		event any
		want  string
	}{
		{"normal ignores drag", 1000, uv.MouseMotionEvent{X: 2, Y: 3, Button: uv.MouseLeft}, ""},
		{"X10 ignores release", 9, uv.MouseReleaseEvent{X: 2, Y: 3, Button: uv.MouseLeft}, ""},
		{"button mode ignores hover", 1002, uv.MouseMotionEvent{X: 2, Y: 3, Button: uv.MouseNone}, ""},
		{"normal click", 1000, uv.MouseClickEvent{X: 2, Y: 3, Button: uv.MouseLeft}, "\x1b[<0;3;4M"},
		{"button drag", 1002, uv.MouseMotionEvent{X: 2, Y: 3, Button: uv.MouseLeft}, "\x1b[<32;3;4M"},
		{"application cursor", 1, uv.KeyPressEvent{Code: uv.KeyUp, IsRepeat: true}, "\x1bOA"},
		{"safe paste", 0, "a\nb", "a b"},
		{"bracketed paste", 2004, "a\nb", "\x1b[200~a\nb\x1b[201~"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := string(encodeSharedInput(test.event, []int{test.mode, 1006})); got != test.want {
				t.Fatalf("encoded input %q, want %q", got, test.want)
			}
		})
	}
}
