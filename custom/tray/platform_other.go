//go:build !windows

package main

import (
	"os/exec"
	"runtime"
)

// The tray ships on Windows only. These exist so the module still builds and
// vets on a developer's Mac or Linux box.

func hideWindow(*exec.Cmd) {}

func openURL(url string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.Command(opener, url).Start()
}
