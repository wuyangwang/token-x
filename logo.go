package main

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

// appIcon is the window/app icon, shown again at the top-left of the custom
// title bar so the frameless window still carries the app's mark.
//
//go:embed resources/icon.png
var appIconPNG []byte

// appIcon is decoded once and reused every frame.
var appIcon, _ = ui.DecodeImage(appIconPNG)
