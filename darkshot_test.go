package main

import (
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	myui "github.com/wuyangwang/my-ui"
)

func TestDarkShot(t *testing.T) {
	a := testApp(t, fakeSessions())
	a.appearance = myui.AppearanceDark
	tt := ui.NewTester(a.view, 640, 720)
	tt.Advance(time.Second)
	f, err := os.Create(`C:\Users\匠多多\AppData\Local\Temp\tx-dark.png`)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}
