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
// told about it every morning. New means the file was written after this tray
// started, which is the same question and needs no state on disk to answer.
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
)

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
}

func (a *alerter) checkConflicts() {
	cl := a.current()
	if cl == nil {
		return
	}
	cfg, err := cl.config()
	if err != nil {
		return
	}

	var found []conflictFile
	for _, f := range cfg.Folders {
		if f.Path == "" {
			continue
		}
		found = append(found, conflictsIn(f.Path, f.name())...)
	}

	fresh := a.freshConflicts(found)
	if len(fresh) == 0 {
		return
	}
	a.notify.Notify(conflictNotification(fresh, a.guiURL()))
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

		// Anything already on disk when this tray started is somebody's
		// existing situation, not news.
		info, err := os.Stat(c.path)
		if err != nil || !info.ModTime().After(a.conflictsSince) {
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
	for _, c := range fresh {
		folders[c.folder] = true
		if len(names) < conflictMaxNames {
			names = append(names, originalName(c.name))
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

	body := "Both versions were kept. " + subject
	if len(folders) == 1 {
		for f := range folders {
			body += " in " + f
		}
	}
	body += ". Open the folder, look for files with \"sync-conflict\" in the name, " +
		"and keep the one you want."

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
			out = append(out, conflictFile{folder: folderName, path: path, name: d.Name()})
		}
		return nil
	})
	if err != nil {
		slog.Debug("could not finish looking for conflicts", "folder", folderName, "err", err)
	}
	return out
}
