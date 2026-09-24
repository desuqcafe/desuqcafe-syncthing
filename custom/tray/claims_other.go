//go:build !windows

package main

// hideDir is a no-op off Windows, where a leading dot already hides it.
func hideDir(string) {}
