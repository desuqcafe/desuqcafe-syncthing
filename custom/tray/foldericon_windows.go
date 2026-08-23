package main

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
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

// Explorer caches a folder's icon, so writing desktop.ini and setting the
// attribute changes nothing on screen for a window that is already open. The
// folder repaints when the cache next happens to be rebuilt, which can be
// after a restart -- so on the machine of the person watching for the icon to
// appear, the feature looks broken.
//
// SHChangeNotify is how you say "this directory's appearance changed". It
// needs no COM, no registration and no window.
//
//	SHCNE_UPDATEDIR   0x00001000  the directory's contents/attributes changed
//	SHCNF_PATHW       0x0005      the argument is a wide-character path.
//	                              SHCNF_PATHA is 0x0001, and passing a UTF-16
//	                              pointer with the ANSI flag reads the path as
//	                              bytes -- it does not fail, it notifies about
//	                              a different, nonexistent path.
//	SHCNF_FLUSHNOWAIT 0x3000      deliver promptly, but do not block us on the
//	                              shell doing it.
//
// The one thing worth knowing before touching this: whether the repaint
// happens cannot be asserted from a test process. SHGetFileInfo answers from
// its own per-process cache, which is not the one Explorer draws from, and it
// returns the fresh answer whether or not this call was made. What the tests
// can and do cover is that the export resolves -- LazyProc.Call panics on a
// misspelled name, which would take the tray down -- and that we only speak up
// when something changed. The repaint itself needs a person with a folder open.
const (
	shcneUpdateDir   = 0x00001000
	shcnfPathW       = 0x0005
	shcnfFlushNoWait = 0x3000
)

var (
	shell32            = syscall.NewLazyDLL("shell32.dll")
	procSHChangeNotify = shell32.NewProc("SHChangeNotify")
)

// notifyShellDirChanged asks Explorer to re-read a directory's customisation.
//
// A var so that the tests can count the calls. Whether the shell then actually
// repaints is not observable from here -- see the note on
// TestShellIsToldOnlyWhenSomethingChanged.
var notifyShellDirChanged = func(dir string) {
	dirPtr, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return
	}
	// Returns nothing, and has no failure mode we could act on.
	_, _, _ = procSHChangeNotify.Call(
		uintptr(shcneUpdateDir),
		uintptr(shcnfPathW|shcnfFlushNoWait),
		uintptr(unsafe.Pointer(dirPtr)),
		0,
	)
}

func writeFolderMarker(dir, content string) error {
	path := filepath.Join(dir, "desktop.ini")
	want := utf16LE(content)

	// Skip an identical rewrite. Explorer caches folder icons and rewriting
	// the file for no reason is a good way to make it re-read them, and it
	// would give Syncthing a modification to scan on every start.
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, want) {
		changed, err := markDirectory(dir)
		if err == nil && changed {
			notifyShellDirChanged(dir)
		}
		return err
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

	// The file itself was rewritten, so the icon has changed whatever the
	// attribute was already doing.
	if _, err := markDirectory(dir); err != nil {
		return err
	}
	notifyShellDirChanged(dir)
	return nil
}

// markDirectory reports whether it had to set the attribute, so the caller can
// avoid telling the shell about a folder that was already marked.
func markDirectory(dir string) (bool, error) {
	dirPtr, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return false, err
	}
	attrs, err := syscall.GetFileAttributes(dirPtr)
	if err != nil {
		return false, err
	}
	if attrs&attrReadonly != 0 {
		return false, nil
	}
	if err := syscall.SetFileAttributes(dirPtr, attrs|attrReadonly); err != nil {
		return false, err
	}
	return true, nil
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
		// Nothing left to clear, but the desktop.ini above may well have been
		// removed, so the shell still needs telling.
		notifyShellDirChanged(dir)
		return nil
	}
	if err := syscall.SetFileAttributes(dirPtr, attrs&^attrReadonly); err != nil {
		return err
	}
	notifyShellDirChanged(dir)
	return nil
}
