package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
)

// themeFile sits next to prices.json in ~/.token-x. Its absence is the
// first-run default: follow the desktop's appearance.
const themeFile = "theme"

func themePath(dir string) string { return filepath.Join(dir, themeFile) }

// loadTheme reads the saved appearance. A missing, unreadable or unknown
// value returns ThemeSystem, so the first run follows the desktop and a
// damaged preference is harmless.
func loadTheme(dir string) mygo.ThemeSource {
	data, err := os.ReadFile(themePath(dir))
	if err != nil {
		return mygo.ThemeSystem
	}
	src := mygo.ThemeSource(strings.TrimSpace(string(data)))
	if src == mygo.ThemeLight || src == mygo.ThemeDark {
		return src
	}
	return mygo.ThemeSystem
}

// saveTheme persists an explicit choice. Writing is atomic, as for
// prices.json, so a crash cannot leave a half-written preference behind.
func saveTheme(dir string, src mygo.ThemeSource) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := themePath(dir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(string(src)), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
