package main

import (
	"errors"
	"testing"
)

// unavailableStub is a source whose local store is absent.
type unavailableStub struct{ codexStub }

func (unavailableStub) Available() error { return errors.New("not installed") }

func TestInstalledSources(t *testing.T) {
	got := installedSources(codexStub{}, opencodeStub{}, unavailableStub{})
	if len(got) != 2 {
		t.Fatalf("installed sources = %d, want 2", len(got))
	}
	if got[0].Name() != "codex" || got[1].Name() != "opencode" {
		t.Errorf("installed sources = [%s %s], want [codex opencode]", got[0].Name(), got[1].Name())
	}
}

func TestInstalledSourcesNone(t *testing.T) {
	if got := installedSources(unavailableStub{}); len(got) != 0 {
		t.Errorf("installed sources = %d, want 0", len(got))
	}
}
