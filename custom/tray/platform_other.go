//go:build !windows

package main

import (
	"crypto/tls"
	"os/exec"
	"runtime"
)

// The tray ships on Windows only. These exist so the module still builds and
// vets on a developer's Mac or Linux box.

func hideWindow(*exec.Cmd) {}

func insecureLoopbackTLS() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // loopback only, self-signed by design
}

func openURL(url string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.Command(opener, url).Start()
}
