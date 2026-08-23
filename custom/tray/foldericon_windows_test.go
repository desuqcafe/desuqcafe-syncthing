package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// checkExplorerAttributes asserts the two attributes without which the whole
// mechanism is inert: Explorer only reads a desktop.ini in a folder marked
// read-only or system, and the file itself has to be hidden and system or it
// shows up as clutter in the folder people are working in.
func checkExplorerAttributes(t *testing.T, folderID, dir, ini string) {
	t.Helper()

	dirAttrs, err := fileAttributes(dir)
	if err != nil {
		t.Errorf("%s: could not read folder attributes: %v", folderID, err)
	} else if dirAttrs&syscall.FILE_ATTRIBUTE_READONLY == 0 {
		t.Errorf("%s: folder is not marked read-only, so Explorer will never "+
			"read its desktop.ini (attrs %#x)", folderID, dirAttrs)
	} else if dirAttrs&syscall.FILE_ATTRIBUTE_HIDDEN != 0 {
		t.Errorf("%s: folder ended up hidden (attrs %#x)", folderID, dirAttrs)
	}

	iniAttrs, err := fileAttributes(ini)
	if err != nil {
		t.Errorf("%s: could not read desktop.ini attributes: %v", folderID, err)
		return
	}
	if iniAttrs&syscall.FILE_ATTRIBUTE_HIDDEN == 0 {
		t.Errorf("%s: desktop.ini is not hidden (attrs %#x)", folderID, iniAttrs)
	}
	if iniAttrs&syscall.FILE_ATTRIBUTE_SYSTEM == 0 {
		t.Errorf("%s: desktop.ini is not a system file (attrs %#x)", folderID, iniAttrs)
	}
}

func fileAttributes(path string) (uint32, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return syscall.GetFileAttributes(p)
}

// TestOverwriteHiddenSystemMarker covers the step that is easy to get wrong
// and only fails on the *second* run: CreateFile refuses to truncate a hidden
// or system file unless those attributes are passed in, so writing over an
// existing desktop.ini has to clear them first. A single-run test would pass
// with that handling removed.
func TestOverwriteHiddenSystemMarker(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, "desktop.ini")

	if err := writeFolderMarker(dir, desktopIniFor(`C:\icons\a.ico`, "First")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	attrs, err := fileAttributes(ini)
	if err != nil {
		t.Fatalf("attributes after first write: %v", err)
	}
	if attrs&syscall.FILE_ATTRIBUTE_HIDDEN == 0 || attrs&syscall.FILE_ATTRIBUTE_SYSTEM == 0 {
		t.Fatalf("first write did not set hidden+system: %#x", attrs)
	}

	if err := writeFolderMarker(dir, desktopIniFor(`C:\icons\b.ico`, "Second")); err != nil {
		t.Fatalf("overwriting a hidden, system desktop.ini: %v", err)
	}
	attrs, err = fileAttributes(ini)
	if err != nil {
		t.Fatalf("attributes after second write: %v", err)
	}
	if attrs&syscall.FILE_ATTRIBUTE_HIDDEN == 0 || attrs&syscall.FILE_ATTRIBUTE_SYSTEM == 0 {
		t.Errorf("second write left the attributes off: %#x", attrs)
	}

	// And the identical-content path, which returns early and must still
	// leave the folder marked.
	if err := writeFolderMarker(dir, desktopIniFor(`C:\icons\b.ico`, "Second")); err != nil {
		t.Errorf("rewriting identical content: %v", err)
	}
	dirAttrs, err := fileAttributes(dir)
	if err != nil {
		t.Fatalf("folder attributes: %v", err)
	}
	if dirAttrs&syscall.FILE_ATTRIBUTE_READONLY == 0 {
		t.Errorf("folder is not marked read-only: %#x", dirAttrs)
	}
}

// --- what the shell actually resolves -------------------------------------

type shFileInfo struct {
	hIcon         syscall.Handle
	iIcon         int32
	dwAttributes  uint32
	szDisplayName [260]uint16
	szTypeName    [80]uint16
}

// With SHGFI_ICONLOCATION the shell fills szDisplayName with the file it would
// load the icon from, and iIcon with the index -- so the answer can be
// asserted as a string instead of by comparing bitmaps.
const shgfiIconLocation = 0x000001000

// SHGFI_SYSICONINDEX gives the folder's slot in the system image list, which
// is what a folder view actually draws from. A customised folder gets its own
// slot, so this distinguishes "the shell has a special icon for this" from
// "it fell back to the generic one" without decoding any pixels.
const shgfiSysIconIndex = 0x000004000

var (
	// shell32 is declared in foldericon_windows.go, for SHChangeNotify.
	procSHGetFileInf = shell32.NewProc("SHGetFileInfoW")
	ole32            = syscall.NewLazyDLL("ole32.dll")
	procCoInit       = ole32.NewProc("CoInitializeEx")
	procCoUninit     = ole32.NewProc("CoUninitialize")
)

// withCOM runs fn on a thread that has an initialised COM apartment.
//
// This is not incidental test scaffolding, it is the thing that makes the
// question answerable at all. Without CoInitializeEx, SHGetFileInfo still
// succeeds and still returns an icon -- it just returns the *generic* folder
// icon out of imageres.dll, having never gone near the desktop.ini. So a test
// that skipped this would report the feature broken while Explorer, which of
// course has COM up, showed the icon perfectly well.
//
// The thread has to stay put: a COM apartment belongs to one thread, and a
// goroutine rescheduled onto another would lose it.
func withCOM(t *testing.T, fn func()) {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// COINIT_APARTMENTTHREADED. S_OK or S_FALSE (already initialised on this
	// thread) are both fine.
	hr, _, _ := procCoInit.Call(0, 0x2)
	if hr != 0 && hr != 1 {
		t.Fatalf("CoInitializeEx: %#x", hr)
	}
	defer procCoUninit.Call() //nolint:errcheck // no meaningful failure mode

	fn()
}

func shellFileInfo(t *testing.T, path string, flags uintptr) shFileInfo {
	t.Helper()
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("UTF16PtrFromString(%q): %v", path, err)
	}
	var info shFileInfo
	r, _, _ := procSHGetFileInf.Call(
		uintptr(unsafe.Pointer(p)),
		0,
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
		flags,
	)
	if r == 0 {
		t.Fatalf("SHGetFileInfo(%q, %#x) failed", path, flags)
	}
	return info
}

// TestShellResolvesOurFolderIcon is the assertion the rest of this file exists
// to support: not that the right bytes were written, but that Windows, asked
// the same question Explorer asks, answers with our icon.
//
// SHGetFileInfo is the API Explorer itself uses, so if it reads the
// desktop.ini then so does the folder view. Going through it also catches the
// failures no amount of checking our own output would: a missing folder
// attribute, an encoding the shell will not parse, an icon file it rejects.
func TestShellResolvesOurFolderIcon(t *testing.T) {
	// Fresh directories each run, and the control is a *different* one rather
	// than the same directory probed before marking. The shell caches icon
	// resolution per path, so asking about a folder and then customising it
	// gets the stale answer back -- which reads as the feature not working.
	dir := t.TempDir()
	control := t.TempDir()

	iconPath := filepath.Join(t.TempDir(), iconFileName)
	if err := os.WriteFile(iconPath, iconFolder, 0o644); err != nil {
		t.Fatalf("writing the icon: %v", err)
	}

	if err := writeFolderMarker(dir, desktopIniFor(iconPath, "Project Assets")); err != nil {
		t.Fatalf("writeFolderMarker: %v", err)
	}

	withCOM(t, func() {
		info := shellFileInfo(t, dir, shgfiIconLocation)
		got := syscall.UTF16ToString(info.szDisplayName[:])
		if !strings.EqualFold(got, iconPath) {
			t.Errorf("the shell resolves this folder's icon to %q (index %d); want %q",
				got, info.iIcon, iconPath)
		}
		if info.iIcon != 0 {
			t.Errorf("icon index is %d, want 0", info.iIcon)
		}

		// And the icon a folder view would actually draw is a different one
		// from the generic folder's, which is the whole point of the exercise.
		marked := shellFileInfo(t, dir, shgfiSysIconIndex).iIcon
		plain := shellFileInfo(t, control, shgfiSysIconIndex).iIcon
		if marked == plain {
			t.Errorf("marked and unmarked folders share system icon index %d, "+
				"so Explorer would draw them identically", marked)
		} else {
			t.Logf("system icon index: marked %d, plain %d", marked, plain)
		}
	})
}

// TestClearRemovesEverythingItWrote covers the undo path, which nobody
// exercises until they are uninstalling and least want a surprise.
func TestClearRemovesEverythingItWrote(t *testing.T) {
	home := t.TempDir()
	folder := t.TempDir()

	// A config.xml with one folder in it, which is all clear() reads. Written
	// by hand rather than by Syncthing because the whole point of the flag is
	// that Syncthing is not running.
	cfg := `<configuration><gui><address>127.0.0.1:8384</address><apikey>k</apikey></gui>` +
		`<folder id="assets" label="Assets" path="` + folder + `"></folder></configuration>`
	if err := os.WriteFile(filepath.Join(home, "config.xml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newFolderMarker(home)
	if err := m.ensureIcon(); err != nil {
		t.Fatalf("ensureIcon: %v", err)
	}
	if err := writeFolderMarker(folder, desktopIniFor(m.iconPath, "Assets")); err != nil {
		t.Fatalf("writeFolderMarker: %v", err)
	}

	n, err := m.clear()
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if n != 1 {
		t.Errorf("cleared %d folders, want 1", n)
	}

	if _, err := os.Stat(filepath.Join(folder, "desktop.ini")); !os.IsNotExist(err) {
		t.Errorf("desktop.ini survived: %v", err)
	}
	if _, err := os.Stat(m.iconPath); !os.IsNotExist(err) {
		t.Errorf("the icon survived: %v", err)
	}
	attrs, err := fileAttributes(folder)
	if err != nil {
		t.Fatalf("folder attributes: %v", err)
	}
	if attrs&syscall.FILE_ATTRIBUTE_READONLY != 0 {
		t.Errorf("the folder is still marked read-only: %#x", attrs)
	}

	// And it has to be safe to run twice, because an uninstall that is
	// re-run, or a flag typed by hand after an uninstall, both land here.
	if _, err := m.clear(); err != nil {
		t.Errorf("second clear: %v", err)
	}
}

// TestShellChangeNotifyExportResolves catches the failure mode of a LazyProc:
// a misspelled export is not an error, it is a panic at the first call, in the
// tray, on a user's machine.
func TestShellChangeNotifyExportResolves(t *testing.T) {
	if err := procSHChangeNotify.Find(); err != nil {
		t.Fatalf("shell32!SHChangeNotify: %v", err)
	}
	// And calling it is harmless on a directory nothing is looking at.
	notifyShellDirChanged(t.TempDir())
}

// TestShellIsToldOnlyWhenSomethingChanged is the assertion that matters for
// the notification, because it is the one with a cost.
//
// What cannot be asserted here is the part everybody wants: that an Explorer
// window already showing the folder repaints. SHGetFileInfo answers out of a
// per-process cache that is not the one a folder view draws from, and it
// returns the marked answer whether or not SHChangeNotify was called -- a test
// built on it passes with the notification removed, which is worse than no
// test. That claim is left unverified in DEPLOYMENT-3D-TEAM.md rather than
// asserted by something that does not assert it.
//
// What is verifiable, and is the real risk of adding this call, is the
// opposite: the tray reconciles every folder every two minutes, and telling
// the shell that a hundred folders changed every two minutes would be worse
// than never telling it at all.
func TestShellIsToldOnlyWhenSomethingChanged(t *testing.T) {
	var notified []string
	real := notifyShellDirChanged
	notifyShellDirChanged = func(dir string) { notified = append(notified, dir) }
	t.Cleanup(func() { notifyShellDirChanged = real })

	dir := t.TempDir()
	ini := desktopIniFor(`C:\icons\a.ico`, "Project Assets")

	if err := writeFolderMarker(dir, ini); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if len(notified) != 1 {
		t.Fatalf("marking a fresh folder notified %d times; want 1", len(notified))
	}

	// The reconcile loop's steady state: same content, folder already marked.
	// Nothing changed, so the shell must not be told.
	for i := 0; i < 3; i++ {
		if err := writeFolderMarker(dir, ini); err != nil {
			t.Fatalf("reconcile pass %d: %v", i, err)
		}
	}
	if len(notified) != 1 {
		t.Errorf("three no-op reconciles produced %d notifications; want none "+
			"after the first (%v)", len(notified)-1, notified)
	}

	// A changed icon path is a real change and must be announced.
	if err := writeFolderMarker(dir, desktopIniFor(`C:\icons\b.ico`, "Project Assets")); err != nil {
		t.Fatalf("rewrite with a different icon: %v", err)
	}
	if len(notified) != 2 {
		t.Errorf("rewriting the marker notified %d times in total; want 2", len(notified))
	}

	// And so is removing it.
	if err := removeFolderMarker(dir); err != nil {
		t.Fatalf("removeFolderMarker: %v", err)
	}
	if len(notified) != 3 {
		t.Errorf("clearing the marker notified %d times in total; want 3", len(notified))
	}
}
