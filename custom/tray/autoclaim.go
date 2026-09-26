package main

// Marking a .blend as "I'm working on this" without being asked.
//
// A mark only helps if it is made, and the Send To entry depends on somebody
// remembering it before they open the file -- which is exactly the moment
// their attention is on the file and not on the tool. So the tray makes the
// mark itself the first time a .blend is saved here, and takes it off again
// once the file has been left alone for autoClaimQuiet.
//
// WHY A SAVE AND NOT BLENDER OPENING THE FILE
//
// Opening is the better moment, and the only way to see it from outside is to
// watch other processes -- enumerate them, read their command lines or their
// open handles. That is the behaviour a behavioural classifier is built to
// dislike, on a binary Defender already quarantines as a false positive
// (DEPLOYMENT-3D-TEAM.md section 20). A save arrives on the disk-change feed
// the tray already reads for the collision warning, and costs nothing new. A
// Blender add-on that marks on open is the right way to get the earlier
// moment, and is left for later.
//
// WHAT DOES NOT COUNT AS A SAVE
//
// The feed reports every file when a folder is first scanned -- one event per
// file, all at once (CLAUDE.md) -- and after a restart the cursor starts at
// "now" so that dump is never read. A folder added while the tray runs has no
// such protection, so a burst of more than autoClaimBurst .blend files in one
// folder in one pass is read as a scan, not as somebody saving. Blender saves
// one file at a time.
//
// Conflict copies end in .blend too, and are made by Syncthing, not by a
// person; so are Blender's own .blend1 backups, which the extension test
// already leaves out.

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// autoClaimQuiet is how long a file marked on a save has to go without
	// another save before the mark comes off. Long enough to cover lunch and
	// a meeting; short enough that a mark left behind at the end of the day
	// is gone by the next morning.
	autoClaimQuiet = 4 * time.Hour

	// autoClaimBurst is the most .blend files one folder can report in one
	// pass and still be read as saves. See the note above.
	autoClaimBurst = 5
)

// autoClaimable is whether a changed path is a file a person saved in
// Blender, as opposed to something Syncthing or Blender made on the side.
func autoClaimable(p string) bool {
	slash := filepath.ToSlash(p)
	if slash == claimsDirName || strings.HasPrefix(slash, claimsDirName+"/") {
		return false
	}
	base := filepath.Base(p)
	if strings.Contains(base, conflictMarker) {
		return false
	}
	return strings.EqualFold(filepath.Ext(base), ".blend")
}

// savedBlends is the pure half: which files, per folder, look like somebody
// saving a .blend here. Folders reporting a burst are dropped whole.
func savedBlends(evs []diskEvent) map[string][]string {
	byFolder := map[string]map[string]bool{}
	for _, ev := range evs {
		if ev.Type != "LocalChangeDetected" || ev.Data.Type != "file" || ev.Data.Action != "modified" {
			continue
		}
		if !autoClaimable(ev.Data.Path) {
			continue
		}
		set := byFolder[ev.Data.Folder]
		if set == nil {
			set = map[string]bool{}
			byFolder[ev.Data.Folder] = set
		}
		set[filepath.ToSlash(ev.Data.Path)] = true
	}
	out := map[string][]string{}
	for folder, set := range byFolder {
		if len(set) > autoClaimBurst {
			slog.Info("not marking saved files: too many at once to be somebody saving",
				"folder", folder, "files", len(set))
			continue
		}
		for p := range set {
			out[folder] = append(out[folder], p)
		}
		sort.Strings(out[folder])
	}
	return out
}

// autoClaim marks each .blend just saved here, unless somebody -- this
// computer included -- already has it marked. Somebody else's mark is left
// for the collision warning, which has already fired by now: marking over it
// would tell the other person that two people are working on one file, which
// is the conflict the warning is trying to prevent, not a fix for it.
func (a *alerter) autoClaim(cl *client, w *claimWatch, evs []diskEvent, rows []claimRow) {
	saved := savedBlends(evs)
	if len(saved) == 0 {
		return
	}
	cfg, err := cl.config()
	if err != nil {
		return
	}
	folders := map[string]restFolder{}
	for _, f := range cfg.Folders {
		folders[f.ID] = f
	}
	held := map[string]bool{}
	for _, r := range rows {
		held[r.Folder+"\x00"+r.Path] = true
	}

	var marked []string
	reach := ""
	for folder, paths := range saved {
		f, ok := folders[folder]
		// Nobody to tell: a folder shared with nobody, or one that is paused,
		// which the server would refuse anyway.
		if !ok || f.Paused || len(f.Devices) < 2 {
			continue
		}
		for _, p := range paths {
			if held[folder+"\x00"+p] {
				continue
			}
			reply, err := cl.claim(folder, p, false, true)
			if err != nil {
				// A receive-only folder refuses, and that is the right answer:
				// nothing saved there reaches anybody either.
				slog.Debug("did not mark a saved file", "folder", folder, "path", p, "err", err)
				continue
			}
			slog.Info("marked a saved file as being worked on here", "folder", folder, "path", p)
			marked = append(marked, filepath.Base(p))
			if r := reachSentence(unseenFor(reply, folder)); r != "" {
				reach = r
			}
		}
	}
	if len(marked) == 0 {
		return
	}
	sort.Strings(marked)
	title := "You are marked as working on " + marked[0]
	if len(marked) > 1 {
		title = fmt.Sprintf("You are marked as working on %d files", len(marked))
	}
	body := "Because you saved it here. "
	if len(marked) > 1 {
		body = "Because you saved them here. "
	}
	if reach != "" {
		body += reach + " "
	} else {
		body += "Everyone you share it with can see that now. "
	}
	body += "The mark comes off by itself after " + plural(int(autoClaimQuiet/time.Hour), "hour") + " without a save."
	a.notify.Notify(Notification{Title: truncate(title, 60), Body: body, Launch: a.guiURL()})
}

// autoRelease takes off the automatic marks on files that have been left
// alone. "Left alone" is read from the file's own modification time, not from
// anything remembered: the tray restarts at every sign-in and would otherwise
// forget, and a mark nobody takes off is the failure that makes people stop
// trusting marks at all. A file that has gone -- deleted, renamed -- is not
// being worked on either.
func (a *alerter) autoRelease(cl *client, rows []claimRow, now time.Time) {
	var due []claimRow
	for _, r := range rows {
		if r.Mine && r.Auto {
			due = append(due, r)
		}
	}
	if len(due) == 0 {
		return
	}
	cfg, err := cl.config()
	if err != nil {
		return
	}
	paths := map[string]string{}
	for _, f := range cfg.Folders {
		paths[f.ID] = f.Path
	}
	for _, r := range due {
		root, ok := paths[r.Folder]
		if !ok || root == "" {
			continue
		}
		var mtime time.Time
		if st, err := os.Stat(filepath.Join(root, filepath.FromSlash(r.Path))); err == nil {
			mtime = st.ModTime()
		}
		if !autoReleaseDue(r.Since, mtime, now) {
			continue
		}
		if _, err := cl.claim(r.Folder, r.Path, true, false); err != nil {
			slog.Debug("could not take an automatic mark off", "folder", r.Folder, "path", r.Path, "err", err)
			continue
		}
		slog.Info("took an automatic mark off a file left alone", "folder", r.Folder, "path", r.Path)
	}
}

// autoReleaseDue is the rule on its own. A zero mtime is a file that is not
// there any more.
func autoReleaseDue(since, mtime, now time.Time) bool {
	if mtime.IsZero() {
		return true
	}
	last := since
	if mtime.After(last) {
		last = mtime
	}
	return now.Sub(last) > autoClaimQuiet
}
