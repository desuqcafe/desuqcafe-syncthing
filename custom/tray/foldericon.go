package main

// Make synced folders visible in Explorer.
//
// Until now there was no sign at all. `.stfolder` is created hidden, nothing
// writes a folder icon, and Syncthing has no shell integration -- so a synced
// folder and any other folder look identical, and somebody who moves one, or
// works in a copy of it, finds out days later.
//
// This gives every synced folder a custom icon through `desktop.ini`, which is
// the one folder-customisation mechanism Windows offers that costs nothing:
// per-user, no administrator, no COM, no registration, and nothing left behind
// but a file that Explorer ignores if the icon it names is gone.
//
// The alternative, an overlay-icon shell extension of the kind Dropbox and
// OneDrive use, was deliberately not built. It needs a registered in-process
// COM server, and overlays come out of a global pool of about fifteen slots
// that those two already crowd -- ours would very likely never be shown, and
// we would have installed a DLL into every Explorer process to achieve that.
//
// The one real cost is that `desktop.ini` lands inside a synced folder, so it
// has to be ignored first. Getting that order wrong would sync a machine-local
// file with an absolute path in it to everybody, and in a receive-only folder
// would show up as an unexpected local addition for someone to worry about.

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// iconFileName is where the icon is kept: the Syncthing home directory, which
// already holds tray.log and is guaranteed writable by whoever is running us.
// desktop.ini names it by absolute path, so it has to be somewhere stable.
const iconFileName = "folder.ico"

// The line written into a folder's ignore patterns when nothing there already
// covers desktop.ini. `(?d)` marks it deletable, so it cannot be the one file
// keeping an otherwise-empty directory alive.
const desktopIniIgnore = "(?d)desktop.ini"

// ignoreMarker explains the appended line in the file itself, for whoever
// opens the Ignore Patterns tab and wonders where it came from.
const ignoreMarkerComment = "// Added by desuqcafe Syncthing: the folder icon marker is per-machine and must not sync"

// desktopIniFor renders the file for one folder.
//
// IconResource supersedes the older IconFile/IconIndex pair on everything
// since Vista, so only it is written. The trailing ",0" is the icon index
// within the file.
func desktopIniFor(iconPath, label string) string {
	tip := "Synced by " + appName
	if label != "" {
		tip = label + " -- synced by " + appName
	}
	// CRLF: this is read by GetPrivateProfileString, not by Go.
	return strings.Join([]string{
		"[.ShellClassInfo]",
		"IconResource=" + iconPath + ",0",
		"InfoTip=" + tip,
		"",
	}, "\r\n")
}

// utf16LE encodes with the byte-order mark that makes the shell read a
// desktop.ini as Unicode. Without it GetPrivateProfileString falls back to the
// system code page, and a folder path containing a character that page cannot
// represent -- which is to say any user whose name is not plain ASCII -- would
// name an icon file that does not exist.
func utf16LE(s string) []byte {
	// The BOM is prepended as a code unit rather than as a literal U+FEFF in
	// the string: an invisible character in the source is exactly the kind of
	// thing an editor or a merge silently eats, and losing it here would not
	// fail loudly -- it would just leave one user with the wrong folder icon.
	units := append([]uint16{0xFEFF}, utf16.Encode([]rune(s))...)
	out := make([]byte, 0, len(units)*2)
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

// ignoresCoverDesktopIni reports whether the folder's existing patterns already
// say something about desktop.ini -- including a "!desktop.ini" that says to
// sync it deliberately. Either way it is not ours to change.
func ignoresCoverDesktopIni(lines []string) bool {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if strings.Contains(strings.ToLower(trimmed), "desktop.ini") {
			return true
		}
	}
	return false
}

// wantsIgnored reports whether desktop.ini would actually be excluded, as
// opposed to merely mentioned. A "!desktop.ini" line means the opposite, and
// in that case the marker file must not be written at all.
func wantsIgnored(lines []string) bool {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if !strings.Contains(strings.ToLower(trimmed), "desktop.ini") {
			continue
		}
		return !strings.HasPrefix(trimmed, "!")
	}
	return false
}

// folderMarker keeps track of which folders have been dealt with, so the
// reconcile can run on a timer without touching the disk every time.
type folderMarker struct {
	home     string
	iconPath string
	// Keyed by folder path. A folder only needs marking once per session; a
	// path that disappears and comes back is re-marked because the stat fails
	// in between and it is never recorded as done.
	done map[string]bool
}

func newFolderMarker(home string) *folderMarker {
	return &folderMarker{
		home:     home,
		iconPath: filepath.Join(home, iconFileName),
		done:     map[string]bool{},
	}
}

// ensureIcon writes the embedded icon out beside the configuration. Rewritten
// only when it is missing or a different size, so a session start is not 350
// kilobytes of pointless I/O -- and so Explorer's icon cache is not invalidated
// for no reason.
func (m *folderMarker) ensureIcon() error {
	if info, err := os.Stat(m.iconPath); err == nil && info.Size() == int64(len(iconFolder)) {
		return nil
	}
	if err := os.MkdirAll(m.home, 0o755); err != nil {
		return err
	}
	return os.WriteFile(m.iconPath, iconFolder, 0o644)
}

// mark deals with one folder: make sure Syncthing will not sync the marker,
// then write it.
//
// The order matters. Writing the file first would give Syncthing a window in
// which to index it, and in a receive-only folder that window is enough to
// leave a "local addition" someone then has to go and revert.
func (m *folderMarker) mark(cl *client, f restFolder) error {
	if f.Path == "" {
		return nil
	}
	info, err := os.Stat(f.Path)
	if err != nil || !info.IsDir() {
		// Not there yet, or on a drive that is not plugged in. Try again on
		// the next pass rather than recording it as done.
		return err
	}

	ign, err := cl.ignores(f.ID)
	if err != nil {
		return err
	}
	if ign.Error != "" {
		// The folder is already stopped by patterns it cannot read. Adding a
		// line would only make the eventual diagnosis harder.
		return nil
	}

	if !ignoresCoverDesktopIni(ign.Ignore) {
		lines := append(append([]string{}, ign.Ignore...), ignoreMarkerComment, desktopIniIgnore)
		if err := cl.setIgnores(f.ID, lines); err != nil {
			return err
		}
		slog.Info("excluded the folder icon marker from syncing", "folder", f.ID)
	} else if !wantsIgnored(ign.Ignore) {
		// An explicit "!desktop.ini". Somebody wants that file synced, and a
		// folder icon is not worth overruling them for.
		slog.Info("leaving the folder unmarked: desktop.ini is deliberately not ignored",
			"folder", f.ID)
		return nil
	}

	return writeFolderMarker(f.Path, desktopIniFor(m.iconPath, f.Label))
}

// clear undoes what mark did: the marker file goes, and with it the folder
// attribute that made Explorer look for it. The appended ignore line is left
// alone -- it excludes a file that no longer exists, which costs nothing, and
// removing it could just as easily delete a line somebody wrote themselves.
//
// Deliberately driven from config.xml rather than the API, because the point
// at which anyone wants this is with Syncthing already stopped.
func (m *folderMarker) clear() (int, error) {
	paths, err := folderPathsFromConfig(m.home)
	if err != nil {
		return 0, err
	}
	cleared := 0
	for _, p := range paths {
		if err := removeFolderMarker(p); err != nil {
			slog.Warn("could not remove the folder marker", "path", p, "err", err)
			continue
		}
		cleared++
	}
	// The icon itself lives in the configuration directory, which the
	// uninstaller removes wholesale, but a hand-run --clear-folder-icons
	// should not leave it behind either.
	if err := os.Remove(m.iconPath); err != nil && !os.IsNotExist(err) {
		slog.Warn("could not remove the folder icon", "path", m.iconPath, "err", err)
	}
	return cleared, nil
}

// reconcile marks every folder that has not been marked yet.
func (m *folderMarker) reconcile(cl *client) {
	if cl == nil {
		return
	}
	if err := m.ensureIcon(); err != nil {
		slog.Error("could not write the folder icon", "path", m.iconPath, "err", err)
		return
	}
	cfg, err := cl.config()
	if err != nil {
		return
	}
	for _, f := range cfg.Folders {
		if m.done[f.Path] {
			continue
		}
		if err := m.mark(cl, f); err != nil {
			slog.Debug("could not mark folder", "folder", f.ID, "path", f.Path, "err", err)
			continue
		}
		m.done[f.Path] = true
	}
}
