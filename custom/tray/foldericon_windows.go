package main

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
)

// Explorer only looks for a desktop.ini in a folder that carries one of these
// two attributes. Read-only is the one to use: system would also hide the
// folder from anyone who has not turned off "hide protected operating system
// files", which for a folder whose whole purpose is to be visible would be
// perfectly backwards.
//
// Read-only on a *directory* is not what it sounds like. It does not stop
// anything being written inside; it is the flag that means "this folder has
// been customised". Go's os.Remove clears it and retries, so it does not get
// in the way of deleting the folder either.
const attrReadonly = syscall.FILE_ATTRIBUTE_READONLY

func writeFolderMarker(dir, content string) error {
	path := filepath.Join(dir, "desktop.ini")
	want := utf16LE(content)

	// Skip an identical rewrite. Explorer caches folder icons and rewriting
	// the file for no reason is a good way to make it re-read them, and it
	// would give Syncthing a modification to scan on every start.
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, want) {
		return markDirectory(dir)
	}

	namePtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	// An existing desktop.ini is hidden and system, and CreateFile refuses to
	// truncate such a file unless the same attributes are passed in. Clearing
	// them first is simpler than reimplementing os.WriteFile to pass them.
	if attrs, err := syscall.GetFileAttributes(namePtr); err == nil {
		_ = syscall.SetFileAttributes(namePtr, attrs&^(syscall.FILE_ATTRIBUTE_HIDDEN|
			syscall.FILE_ATTRIBUTE_SYSTEM|syscall.FILE_ATTRIBUTE_READONLY))
	}

	if err := os.WriteFile(path, want, 0o644); err != nil {
		return err
	}

	// Hidden so it does not clutter the folder the user is looking at, system
	// so Explorer treats it as a customisation file rather than as content.
	if err := syscall.SetFileAttributes(namePtr,
		syscall.FILE_ATTRIBUTE_HIDDEN|syscall.FILE_ATTRIBUTE_SYSTEM); err != nil {
		return err
	}

	return markDirectory(dir)
}

func markDirectory(dir string) error {
	dirPtr, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	attrs, err := syscall.GetFileAttributes(dirPtr)
	if err != nil {
		return err
	}
	if attrs&attrReadonly != 0 {
		return nil
	}
	return syscall.SetFileAttributes(dirPtr, attrs|attrReadonly)
}

// removeFolderMarker undoes writeFolderMarker.
func removeFolderMarker(dir string) error {
	path := filepath.Join(dir, "desktop.ini")

	if namePtr, err := syscall.UTF16PtrFromString(path); err == nil {
		// DeleteFile refuses a read-only file, and a hidden or system one
		// cannot be reopened without saying so, so clear the lot in one call.
		_ = syscall.SetFileAttributes(namePtr, syscall.FILE_ATTRIBUTE_NORMAL)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}

	dirPtr, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	attrs, err := syscall.GetFileAttributes(dirPtr)
	if err != nil {
		// The folder itself is gone, which is as cleared as it needs to be.
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if attrs&attrReadonly == 0 {
		return nil
	}
	return syscall.SetFileAttributes(dirPtr, attrs&^attrReadonly)
}
