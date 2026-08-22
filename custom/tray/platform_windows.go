package main

import (
	"crypto/tls"
	"os/exec"
	"syscall"
)

// hideWindow keeps the supervised Syncthing from flashing a console window.
// The binary is already linked -H windowsgui, but CREATE_NO_WINDOW also stops
// anything it shells out to from stealing focus.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}

func insecureLoopbackTLS() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // loopback only, self-signed by design
}

func openURL(url string) error {
	// rundll32 rather than "cmd /c start": no console window, and no quoting
	// rules to get wrong.
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
