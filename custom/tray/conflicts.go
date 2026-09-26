package main

// Telling somebody that two people changed the same file.
//
// This is the biggest real loss of work the target setup can produce, and
// until now nothing said a word about it. Two modellers open the same .blend,
// both save, and Syncthing does the safe thing: it keeps both, renaming one to
//
//	scene.sync-conflict-20260824-142233-K3PLM9Q.blend
//
// Nothing is lost -- and nothing is said. The file appears in a folder nobody
// has open, with a name that sorts next to the original and looks like a
// backup. The GUI does not mention it either. So the normal outcome is that
// one modeller's afternoon quietly stops being in scene.blend, and they find
// out days later when they notice their changes are gone.
//
// WHY A WALK
//
// The obvious route is the event stream, and it does not work. The conflicting
// copy is created by the local puller as part of resolving the conflict, so
// there is no ItemFinished naming it on the machine where it appears -- the
// event names the file that won. Watching for the loser means looking at the
// disk.
//
// WHAT COUNTS AS NEW
//
// Not "not seen before": the tray restarts at every sign-in, and a modeller
// with a fortnight-old conflict they have decided to live with should not be
// told about it every morning. New means the conflict happened after this
// tray started, which is the same question and needs no state on disk to
// answer.
//
// "Happened" is read from the timestamp Syncthing writes into the copy's
// name, not from the file's modification time. The copy keeps the mtime of
// the *edit*, and the whole point of the offline case is that the edit is
// old: somebody changes a file on Monday with the other computer off, the
// tray restarts on Tuesday, the two meet on Tuesday afternoon -- and a
// Monday mtime is before Tuesday's start, so the conflict was never
// announced. Verified on a pair, 2026-09-26: the copy made at 15:02:11 held
// the 15:01:42 mtime of the edit it preserved.
//
// The grace period exists because Syncthing is started by this process and
// pulls immediately: a conflict produced in the first seconds is genuinely new
// and would otherwise fall in the gap before the first pass.

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// conflictMarker is the infix Syncthing puts in the losing copy's name.
	// Defined in lib/fs; matching the string is the only way to find these
	// from outside, and it has been stable across every 1.x and 2.x release.
	conflictMarker = ".sync-conflict-"

	// conflictCheckInterval is slower than the folder-icon reconcile because
	// each pass reads every directory in every folder. WalkDir enumerates
	// rather than stats, so this is cheap on any tree that fits in the OS
	// cache -- but a texture library on a spinning disk is not that, and a
	// conflict five minutes old is no less true than one five seconds old.
	conflictCheckInterval = 5 * time.Minute

	// conflictGrace is how far before start-up a conflict still counts as new.
	// See the note above about the first pass.
	conflictGrace = 5 * time.Minute

	// conflictWalkBudget caps one folder's walk. Somebody will eventually
	// share a directory with a million files in it, and a notification that
	// nobody asked for is not worth pinning a disk for.
	conflictWalkBudget = 200000

	// conflictMaxNames is how many files a single toast names before it starts
	// counting instead. Two fits in a toast; ten is a wall of text nobody
	// reads, on a notification whose only job is to be read.
	conflictMaxNames = 2

	// conflictStampLayout is the time Syncthing writes into a conflict copy's
	// name, in local time: name.sync-conflict-20260926-150211-OHQN3WH.ext.
	conflictStampLayout = "20060102-150405"
)

// conflictTime reads when the conflict happened out of the copy's name. False
// when the name does not carry a stamp it can read, and the caller falls back
// to the file's modification time.
func conflictTime(name string) (time.Time, bool) {
	i := strings.Index(name, conflictMarker)
	if i < 0 {
		return time.Time{}, false
	}
	rest := name[i+len(conflictMarker):]
	if len(rest) < len(conflictStampLayout) {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(conflictStampLayout, rest[:len(conflictStampLayout)], time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// watchConflicts is the second alert with no event behind it. See the note at
// the top of this file for why the event stream cannot answer this.
func (a *alerter) watchConflicts(ctx context.Context) {
	// Long enough after start-up for Syncthing to be up and for the first pull
	// to have got going, so the seeding pass sees a settled tree.
	if !sleepCtx(ctx, 45*time.Second) {
		return
	}
	for {
		a.checkConflicts()
		if !sleepCtx(ctx, conflictCheckInterval) {
			return
		}
	}
}

// conflictFile is one conflicting copy, as found on disk.
type conflictFile struct {
	folder string // the folder's display name
	path   string // full path on disk
	name   string // base name, with the conflict suffix still on it
	// gone is the original name no longer existing beside the copy: one side
	// deleted the file while the other changed it. The delete keeps the name
	// and the change survives only as the copy -- not the other way round,
	// which is what this setup was believed to do until it was tried on a
	// pair (DEPLOYMENT-3D-TEAM.md section 26). Worth its own sentence,
	// because "a second copy" of a file that is no longer there sends the
	// reader looking for a first one.
	gone bool
}

// checkConflicts announces any new conflicts, and reports whether it did.
//
// Every five minutes on its own, and also straight after a folder finishes
// syncing -- which is the moment a conflict is made. Five minutes on its own
// meant the first thing said after two people's edits met was "Sync
// complete, up to date", with the conflict toast following up to five
// minutes later.
func (a *alerter) checkConflicts() bool {
	// Left for the reunion toast to say, when one is being prepared. Taking
	// them here would mark them seen and leave it nothing to report.
	if a.briefingActive() {
		return false
	}
	fresh := a.collectConflicts()
	if len(fresh) == 0 {
		return false
	}
	a.notify.Notify(conflictNotification(fresh, a.guiURL()))
	return true
}

// collectConflicts is checkConflicts without the toast, for a caller that
// wants to say it in its own words (reconnect.go).
func (a *alerter) collectConflicts() []conflictFile {
	cl := a.current()
	if cl == nil {
		return nil
	}
	cfg, err := cl.config()
	if err != nil {
		return nil
	}

	var found []conflictFile
	for _, f := range cfg.Folders {
		if f.Path == "" {
			continue
		}
		found = append(found, conflictsIn(f.Path, f.name())...)
	}
	return a.freshConflicts(found)
}

// freshConflicts narrows everything on disk to what is worth saying out loud,
// and records what it has accounted for.
//
// Separate from checkConflicts so it can be tested against real files without
// a Syncthing to ask for the folder list -- the two rules it implements are
// the whole behaviour, and both are easy to get subtly wrong.
func (a *alerter) freshConflicts(found []conflictFile) []conflictFile {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.conflictsSince.IsZero() {
		a.conflictsSince = time.Now().Add(-conflictGrace)
	}

	var fresh []conflictFile
	for _, c := range found {
		if a.seenConflicts[c.path] {
			continue
		}
		a.seenConflicts[c.path] = true

		// Anything that happened before this tray started is somebody's
		// existing situation, not news. See WHAT COUNTS AS NEW.
		when, ok := conflictTime(c.name)
		if !ok {
			info, err := os.Stat(c.path)
			if err != nil {
				continue
			}
			when = info.ModTime()
		}
		if !when.After(a.conflictsSince) {
			continue
		}
		fresh = append(fresh, c)
	}

	// Forget files that have been dealt with, so a conflict resolved and then
	// hit again really does notify twice.
	still := make(map[string]bool, len(found))
	for _, c := range found {
		still[c.path] = true
	}
	for p := range a.seenConflicts {
		if !still[p] {
			delete(a.seenConflicts, p)
		}
	}

	return fresh
}

// conflictNotification writes the toast for a pass's worth of new conflicts.
//
// Split out from checkConflicts so the wording can be tested without a disk,
// which matters more here than usual: this is the one notification whose whole
// job is to make a non-technical reader understand what happened to their
// afternoon.
func conflictNotification(fresh []conflictFile, launch string) Notification {
	sort.Slice(fresh, func(i, j int) bool { return fresh[i].path < fresh[j].path })

	folders := map[string]bool{}
	names := make([]string, 0, len(fresh))
	gone := 0
	for _, c := range fresh {
		folders[c.folder] = true
		if len(names) < conflictMaxNames {
			names = append(names, originalName(c.name))
		}
		if c.gone {
			gone++
		}
	}
	where := ""
	if len(folders) == 1 {
		for f := range folders {
			where = " in " + f
		}
	}

	// Every one of them deleted on one side and changed on the other. The
	// usual wording would be wrong twice over: nobody "changed the same file"
	// in the sense the reader will picture, and there is no first copy for
	// the second one to sit beside.
	if gone == len(fresh) {
		title := "A deleted file was changed somewhere else"
		subject := names[0] + " was deleted on one computer and changed on another"
		if len(fresh) > 1 {
			title = "Deleted files were changed somewhere else"
			subject = strings.Join(names, " and ") + " were deleted on one computer and changed on another"
			if len(fresh) > len(names) {
				subject = strings.Join(names, ", ") + " and " + plural(len(fresh)-len(names), "other file") +
					" were deleted on one computer and changed on another"
			}
		}
		return Notification{
			Title: title,
			Body: subject + where + ". The deletion kept the name; the changes were kept as a copy " +
				"with \"sync-conflict\" in its name. Rename it back if the file was still needed.",
			Launch: launch,
		}
	}

	title := "Two people changed the same file"
	if len(fresh) > 1 {
		title = "Two people changed the same files"
	}

	// joinNames is not used here: its "and N others" does not say what the
	// others are, and "scene.blend and 4 others" reads like four people rather
	// than four files.
	var subject string
	switch {
	case len(fresh) == 1:
		subject = names[0] + " now has a second copy"
	case len(fresh) <= conflictMaxNames:
		subject = strings.Join(names, " and ") + " now have second copies"
	default:
		subject = strings.Join(names, ", ") + " and " +
			plural(len(fresh)-len(names), "other file") + " now have second copies"
	}

	body := "Both versions were kept. " + subject + where + ". Open the folder, look for files " +
		"with \"sync-conflict\" in the name, and keep the one you want."
	if gone > 0 {
		lead := "One of them was"
		if gone > 1 {
			lead = fmt.Sprintf("%d of them were", gone)
		}
		body += " " + lead + " deleted on one side; there the copy is all that is left."
	}

	return Notification{
		Title:  title,
		Body:   body,
		Launch: launch,
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// originalName strips the conflict suffix, so the toast names the file the
// person recognises rather than the generated one.
//
//	scene.sync-conflict-20260824-142233-K3PLM9Q.blend -> scene.blend
//
// The suffix runs from the marker to the extension, and the extension is kept
// because "scene" and "scene.blend" are not equally recognisable to somebody
// looking at a folder listing.
func originalName(name string) string {
	i := strings.Index(name, conflictMarker)
	if i < 0 {
		return name
	}
	rest := name[i+len(conflictMarker):]
	// The generated part is date-time-modifier; the extension, if any, is
	// whatever follows the next dot.
	if dot := strings.Index(rest, "."); dot >= 0 {
		return name[:i] + rest[dot:]
	}
	return name[:i]
}

// conflictsIn walks one folder and returns every conflicting copy in it.
//
// Errors are swallowed on purpose. A folder whose drive is not plugged in, or
// a directory the user has locked, is a normal state of affairs and not
// something to interrupt anybody about -- this is a notifier, not a health
// check.
func conflictsIn(root, folderName string) []conflictFile {
	var out []conflictFile
	seen := 0

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Unreadable directory: skip it, keep walking the rest.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if seen++; seen > conflictWalkBudget {
			slog.Warn("stopped looking for conflicts; folder is too large",
				"folder", folderName, "budget", conflictWalkBudget)
			return fs.SkipAll
		}

		if d.IsDir() {
			// .stversions is where Syncthing's own file versioning keeps old
			// copies, including old conflicts. Announcing those would be
			// announcing the archive, not the event.
			switch d.Name() {
			case ".stfolder", ".stversions":
				return fs.SkipDir
			}
			return nil
		}
		if strings.Contains(d.Name(), conflictMarker) {
			_, err := os.Lstat(filepath.Join(filepath.Dir(path), originalName(d.Name())))
			out = append(out, conflictFile{
				folder: folderName, path: path, name: d.Name(),
				gone: os.IsNotExist(err),
			})
		}

		return nil
	})
	if err != nil {
		slog.Debug("could not finish looking for conflicts", "folder", folderName, "err", err)
	}
	return out
}
