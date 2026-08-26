package main

import (
	"errors"
	"os/exec"
	"runtime"
	"syscall"

	"golang.org/x/sys/windows"
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

// openURL hands a URL to whatever the user has set as their browser.
//
// This used to be `rundll32 url.dll,FileProtocolHandler`, chosen because it
// raises no console window and has no quoting rules to get wrong. Both true,
// and it is also a textbook LOLBin: launching things through rundll32 is a
// documented evasion technique precisely because it hides the real parent
// process, so every behavioural scanner on earth is trained to notice it.
// Defender quarantined this binary as Trojan:Win32/Bearfoos.A!ml on
// 2026-08-26, and while nothing here proves that one line was the cause, it
// was the single most incriminating thing in the file for no benefit that
// ShellExecute does not also provide.
//
// ShellExecute is the API the shell actually exposes for this. No child
// process, no command line, no quoting.
//
// **COM must be initialised on this thread first.** ShellExecute may delegate
// to a shell extension, and the tray initialises COM nowhere -- it is the same
// trap as SHGetFileInfo in foldericon_windows.go, which returns a generic icon
// and no error when COM is cold. Hence LockOSThread: CoInitializeEx applies to
// the calling thread, and without the lock the Go runtime is free to move this
// goroutine to a different one between the two calls.
func openURL(url string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// RPC_E_CHANGED_MODE means COM is already up on this thread in the other
	// apartment model, which is fine to proceed under -- but it also means we
	// did not initialise it and must not tear it down.
	var ours bool
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err == nil {
		ours = true
	} else if !errors.Is(err, syscall.Errno(windows.RPC_E_CHANGED_MODE)) {
		// RPC_E_CHANGED_MODE is declared as a Handle, not an error, so it has
		// to be converted before it can be compared to one.
		return err
	}
	if ours {
		defer windows.CoUninitialize()
	}

	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL)
}
