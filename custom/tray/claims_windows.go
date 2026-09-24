package main

import "syscall"

// hideDir sets the hidden attribute on the claims directory, the way
// Syncthing hides .stfolder. Attributes do not sync, so each computer has to
// do this for itself: the server hides the directory where a claim is made,
// and this is what hides it where the claims arrive.
func hideDir(path string) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	attrs, err := syscall.GetFileAttributes(p)
	if err != nil || attrs&syscall.FILE_ATTRIBUTE_HIDDEN != 0 {
		return
	}
	_ = syscall.SetFileAttributes(p, attrs|syscall.FILE_ATTRIBUTE_HIDDEN)
}
