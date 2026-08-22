//go:build !windows

package main

// The tray ships on Windows only. This exists so the module still builds and
// vets on a developer's Mac or Linux box.

func newNotifier(string, string) notifier { return nopNotifier{} }
