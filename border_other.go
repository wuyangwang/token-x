//go:build !windows

package main

// setWindowBorderColor is a no-op off Windows; other platforms theme their
// own window frame.
func setWindowBorderColor(handle uintptr, dark bool) {}
