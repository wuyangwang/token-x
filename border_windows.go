//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

const (
	dwmwaBorderColor  = 34
	dwmwaColorDefault = 0xFFFFFFFF // DWMWA_COLOR_DEFAULT
)

var (
	dwmapi                    = syscall.NewLazyDLL("dwmapi.dll")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

// setWindowBorderColor overrides the border DWM draws around the window. On
// some Windows builds that border stays white in dark mode, leaving a light
// frame around a dark window. A zinc border blends with the themed content;
// the default restores the system color in light mode. The handle is the
// HWND from Window.NativeHandle.
func setWindowBorderColor(handle uintptr, dark bool) {
	if handle == 0 {
		return
	}
	color := uint32(dwmwaColorDefault)
	if dark {
		// COLORREF is 0x00BBGGRR: shadcn zinc-800, #3F3F46.
		color = 0x3F | 0x3F<<8 | 0x46<<16
	}
	procDwmSetWindowAttribute.Call(
		handle, dwmwaBorderColor, uintptr(unsafe.Pointer(&color)), 4,
	)
}
