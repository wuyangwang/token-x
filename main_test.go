package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The view runs without a window in tests, which click and type as a user
// would.
func TestView(t *testing.T) {
	a := testApp(t, nil)
	tt := ui.NewTester(a.view, 640, 620)
	if !tt.HasText("今日金额") {
		t.Errorf("texts %q", tt.Texts())
	}
}
