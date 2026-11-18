package main

import (
	"os"
	"testing"

	"github.com/egoist/mygo"
)

func TestLoadThemeDefaultsToSystem(t *testing.T) {
	dir := t.TempDir()
	if got := loadTheme(dir); got != mygo.ThemeSystem {
		t.Errorf("loadTheme with no file = %q, want system", got)
	}
}

func TestThemeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	for _, src := range []mygo.ThemeSource{mygo.ThemeDark, mygo.ThemeLight} {
		if err := saveTheme(dir, src); err != nil {
			t.Fatalf("saveTheme(%q): %v", src, err)
		}
		if got := loadTheme(dir); got != src {
			t.Errorf("loadTheme = %q after saving %q", got, src)
		}
	}
}

func TestLoadThemeIgnoresUnknownValue(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(themePath(dir), []byte("nonsense"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadTheme(dir); got != mygo.ThemeSystem {
		t.Errorf("loadTheme with a bad file = %q, want system", got)
	}
}

func TestToggleAppearancePersistsChoice(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t, nil)
	a.themeDir = dir

	a.toggleAppearance()
	if !a.dark {
		t.Error("first toggle did not switch to dark")
	}
	if got := loadTheme(dir); got != mygo.ThemeDark {
		t.Errorf("saved theme = %q, want dark", got)
	}

	a.toggleAppearance()
	if a.dark {
		t.Error("second toggle did not switch to light")
	}
	if got := loadTheme(dir); got != mygo.ThemeLight {
		t.Errorf("saved theme = %q, want light", got)
	}
}
