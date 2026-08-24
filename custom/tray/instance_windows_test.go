//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestInstanceMutexNameIsStableAcrossHowThePathWasWritten(t *testing.T) {
	a := instanceMutexName(`C:\Users\x\AppData\Local\desuqcafe-syncthing`)
	b := instanceMutexName(`c:/users/x/appdata/local/desuqcafe-syncthing/`)
	if a != b {
		t.Errorf("the same home produced two locks:\n  %s\n  %s", a, b)
	}
	if c := instanceMutexName(`C:\Users\x\AppData\Local\other`); c == a {
		t.Error("two different homes share a lock")
	}
	if !strings.HasPrefix(a, `Local\`) {
		t.Errorf("the lock is not session-scoped: %q", a)
	}
	if strings.Contains(strings.TrimPrefix(a, `Local\`), `\`) {
		t.Errorf("a mutex name may not contain a backslash past the namespace: %q", a)
	}
}
