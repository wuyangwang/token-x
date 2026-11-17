package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The view runs without a window in tests, which click and type as a user
// would.
func TestView(t *testing.T) {
	a := &app{}
	tt := ui.NewTester(a.view, 480, 360)
	if !tt.HasText("token-x") {
		t.Errorf("texts %q", tt.Texts())
	}
}
