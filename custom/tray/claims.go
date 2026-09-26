package main

// "I'm working on this file."
//
// The server keeps the claims (lib/api/api_claims.go): one file per device in
// each folder's .desuq-claims directory, so they travel with the folder to
// exactly the people who share it. This is the half that reaches a person who
// is not looking at the web interface, which is everybody, all the time:
//
//   - When somebody else marks a file, say so. That is the moment to hear it:
//     before you open the file, not after you have both saved it.
//   - When a file somebody else has marked changes on THIS computer, say so
//     louder. That is the conflict in the making, and there is still time to
//     pick up the phone.
//   - Send To. Right-clicking a file in Explorer and sending it to
//     "desuqcafe Syncthing - I'm working on this" is how a modeller makes a
//     claim without opening a browser at all. See runClaim.
//
// It also keeps two things true that the folder cannot keep true by itself:
// the claims directory is hidden in Explorer, and a folder held back by the
// selective-sync picker still receives claims. See ensureClaimsException.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// claimsDirName matches claimsDir in lib/api/api_claims.go.
	claimsDirName = ".desuq-claims"

	// claimsException is the ignore line that keeps claims arriving in a
	// folder the picker is holding back with "*". Ignore patterns are first
	// match wins, so it has to sit above the picker's block.
	claimsException = "!/" + claimsDirName

	// claimsExceptionComment explains the line to whoever opens the Ignore
	// Patterns tab, the same courtesy the folder-icon line gets.
	claimsExceptionComment = "// Added by desuqcafe Syncthing: who is working on what must arrive even when files are held back"

	// claimsCheckInterval is how often the claims are read. A claim is only
	// useful if it arrives before the other person opens the file, and this
	// is one small local request per pass.
	claimsCheckInterval = 20 * time.Second

	// claimsFirstCheck lets Syncthing come up before the first pass.
	claimsFirstCheck = 20 * time.Second

	// claimsGrace is how far before start-up a claim still counts as new, for
	// the same reason as conflictGrace: a claim made while this computer was
	// asleep is news when it wakes, but a week-old one is not.
	claimsGrace = 12 * time.Hour

	// claimCollisionCooldown stops one file saved every few minutes raising a
	// toast every few minutes. Once is the message; after that it is noise.
	claimCollisionCooldown = 30 * time.Minute

	// claimsMaxNames is how many files one toast names before it counts.
	claimsMaxNames = 2
)

// claimRow is one claim as /rest/folder/claims reports it.
type claimRow struct {
	Folder string    `json:"folder"`
	Label  string    `json:"label"`
	Path   string    `json:"path"`
	Device string    `json:"device"`
	Name   string    `json:"name"`
	Mine   bool      `json:"mine"`
	Since  time.Time `json:"since"`
	Stale  bool      `json:"stale"`
	Auto   bool      `json:"auto"`
	// Unseen is, on this device's own claims, who does not have them yet.
	Unseen []claimPeer `json:"unseen"`
	// HandingTo and From are hand-overs (lib/api/api_handoff.go): who a
	// mark is being passed to, and who passed a mark on.
	HandingTo *claimRef `json:"handingTo"`
	From      *claimRef `json:"from"`
}

// claimRef names a device in a claim row; Mine is this computer.
type claimRef struct {
	Device string `json:"device"`
	Name   string `json:"name"`
	Mine   bool   `json:"mine"`
}

// claimPeer is one person a claim has not reached. State is "offline",
// "sending" or "heldBack"; see claimPeer in lib/api/api_claims.go.
type claimPeer struct {
	Device string `json:"device"`
	Name   string `json:"name"`
	State  string `json:"state"`
}

type claimsFolderInfo struct {
	Folder   string `json:"folder"`
	CanClaim bool   `json:"canClaim"`
	Reason   string `json:"reason"`
}

type claimsReply struct {
	Claims  []claimRow         `json:"claims"`
	Folders []claimsFolderInfo `json:"folders"`
}

func (c *client) claims() (claimsReply, error) {
	var out claimsReply
	err := c.get("/rest/folder/claims", nil, &out)
	return out, err
}

// claim marks or unmarks one file, and answers with that folder's claims
// afterwards -- which is where "who has not seen it yet" comes from. The
// error carries the server's sentence for the refusals a person can do
// something about. auto is a mark made because the file was saved; see
// autoclaim.go.
func (c *client) claim(folder, path string, release, auto bool) (claimsReply, error) {
	var out claimsReply
	body := map[string]any{"folder": folder, "path": path, "release": release, "auto": auto}
	err := c.do(http.MethodPost, "/rest/folder/claim", nil, body, &out)
	return out, err
}

// unseenFor finds who has not received this device's claims in one folder.
// Every claim of ours in a folder lives in the same file, so they all share
// one answer; the first row found is as good as any.
func unseenFor(reply claimsReply, folder string) []claimPeer {
	for _, r := range reply.Claims {
		if r.Mine && r.Folder == folder {
			return r.Unseen
		}
	}
	return nil
}

// reachSentence says who a mark has not reached, or "" when it has reached
// everybody or is on its way. "sending" is left out on purpose: it is a
// second's wait on a connected peer, and a toast that says "not yet" about
// something that will be true before it is read is only noise. Offline and
// held back are the two a person may need to do something about.
func reachSentence(unseen []claimPeer) string {
	var offline, held []string
	for _, p := range unseen {
		switch p.State {
		case "offline":
			offline = append(offline, p.Name)
		case "heldBack":
			held = append(held, p.Name)
		}
	}
	var parts []string
	if len(offline) > 0 {
		verb := " is offline"
		if len(offline) > 1 {
			verb = " are offline"
		}
		parts = append(parts, listNames(offline)+verb+" and will not see it until they reconnect.")
	}
	if len(held) > 0 {
		parts = append(parts, listNames(held)+" cannot see marks yet: their copy needs updating.")
	}
	return strings.Join(parts, " ")
}

// listNames joins every name, where joinNames counts past the second: a
// sentence about who has not seen something has to say who.
func listNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// diskEvent is one entry from /rest/events/disk.
type diskEvent struct {
	ID   int       `json:"id"`
	Type string    `json:"type"`
	Time time.Time `json:"time"`
	Data struct {
		Folder string `json:"folder"`
		Label  string `json:"label"`
		Action string `json:"action"`
		Type   string `json:"type"`
		Path   string `json:"path"`
		// ModifiedBy is the short ID of the device that made the change, which
		// is how reconnect.go tells what a particular peer did.
		ModifiedBy string `json:"modifiedBy"`
	} `json:"data"`
}

// diskEvents reads what has changed on disk since id, without waiting.
//
// /rest/events/disk is a fixed-mask endpoint with one server-side buffer, so
// the per-subscription numbering trap in events.go does not apply. What does
// apply is a restart: the buffer is memory only and IDs begin again at 1, so
// a caller must drop its cursor the moment an ID goes backwards.
func (c *client) diskEvents(since int) ([]diskEvent, error) {
	var out []diskEvent
	q := url.Values{"since": {strconv.Itoa(since)}, "timeout": {"0"}}
	err := c.get("/rest/events/disk", q, &out)
	return out, err
}

// latestDiskEventID is the newest ID in the disk-change buffer, or 0 when it
// is empty. limit=N returns the newest N.
func (c *client) latestDiskEventID() (int, error) {
	var out []diskEvent
	q := url.Values{"since": {"0"}, "limit": {"1"}, "timeout": {"0"}}
	if err := c.get("/rest/events/disk", q, &out); err != nil {
		return 0, err
	}
	if len(out) == 0 {
		return 0, nil
	}
	return out[len(out)-1].ID, nil
}

func claimKey(r claimRow) string { return r.Folder + "\x00" + r.Device + "\x00" + r.Path }

// claimWatch is the tray's memory between passes.
type claimWatch struct {
	mu sync.Mutex
	// seen is every peer claim already accounted for, so each is announced
	// once. Entries go when the claim does, so a file marked, released and
	// marked again is announced again -- it is news again.
	seen map[string]bool
	// since is the cutoff a claim's time has to beat to be announced.
	since time.Time
	// diskCursor is the last /rest/events/disk ID read; -1 before the first
	// pass, which only finds out where "now" is.
	diskCursor int
	// warned is when each (folder, path) last raised a collision toast.
	warned map[string]time.Time
	// handed is every mark handed to this computer already announced.
	handed map[string]bool
}

func newClaimWatch() *claimWatch {
	return &claimWatch{seen: map[string]bool{}, warned: map[string]time.Time{}, handed: map[string]bool{}, diskCursor: -1}
}

// newPeerClaims is the pure half of the announcement: which of these claims
// by other people have not been announced, and are recent enough to be news.
// It updates seen, dropping claims that have gone.
func newPeerClaims(rows []claimRow, seen map[string]bool, cutoff time.Time) []claimRow {
	present := map[string]bool{}
	var fresh []claimRow
	for _, r := range rows {
		if r.Mine {
			continue
		}
		k := claimKey(r)
		present[k] = true
		if seen[k] {
			continue
		}
		seen[k] = true
		if r.Since.After(cutoff) && !r.Stale {
			fresh = append(fresh, r)
		}
	}
	for k := range seen {
		if !present[k] {
			delete(seen, k)
		}
	}
	sort.Slice(fresh, func(i, j int) bool { return fresh[i].Since.Before(fresh[j].Since) })
	return fresh
}

// claimAnnouncement words the toast for new claims by other people.
func claimAnnouncement(fresh []claimRow) (title, body string) {
	if len(fresh) == 0 {
		return "", ""
	}
	who := fresh[0].Name
	same := true
	for _, r := range fresh {
		if r.Name != who {
			same = false
		}
	}
	if len(fresh) == 1 {
		r := fresh[0]
		switch {
		case r.From != nil && r.From.Mine:
			// The other half of a hand-over this computer made: the news is
			// that it arrived, not that somebody else has the file open.
			return truncate(who+" has taken "+filepath.Base(r.Path), 60),
				"You handed it over in " + r.Label + ". It is marked as theirs now."
		case r.From != nil:
			return truncate(who+" has taken over "+filepath.Base(r.Path), 60),
				"From " + r.From.Name + ", in " + r.Label + ". Leave it closed until " + who + " is done."
		}
		return truncate(who+" is working on "+filepath.Base(r.Path), 60),
			"In " + r.Label + ". Leave it closed until they are done, or you will both end up with a copy of your own."
	}
	names := make([]string, 0, claimsMaxNames)
	for i, r := range fresh {
		if i == claimsMaxNames {
			break
		}
		names = append(names, filepath.Base(r.Path))
	}
	list := strings.Join(names, ", ")
	if len(fresh) > claimsMaxNames {
		list += fmt.Sprintf(" and %d more", len(fresh)-claimsMaxNames)
	}
	if same {
		return truncate(fmt.Sprintf("%s is working on %d files", who, len(fresh)), 60),
			list + ". Leave them closed until " + who + " is done."
	}
	return fmt.Sprintf("%d files are being worked on", len(fresh)),
		list + ". Open the app to see who has which."
}

// newHandoffs is which of this computer's own marks were handed to it by
// somebody else and have not been announced. The server takes a hand-over by
// itself (lib/api/api_handoff.go), so without this the person it was meant
// for would learn about it only by opening the app. Same memory rules as
// newPeerClaims: once each, forgotten when the mark goes.
func newHandoffs(rows []claimRow, seen map[string]bool, cutoff time.Time) []claimRow {
	present := map[string]bool{}
	var fresh []claimRow
	for _, r := range rows {
		if !r.Mine || r.From == nil || r.From.Mine {
			continue
		}
		k := claimKey(r)
		present[k] = true
		if seen[k] {
			continue
		}
		seen[k] = true
		if r.Since.After(cutoff) {
			fresh = append(fresh, r)
		}
	}
	for k := range seen {
		if !present[k] {
			delete(seen, k)
		}
	}
	sort.Slice(fresh, func(i, j int) bool { return fresh[i].Since.Before(fresh[j].Since) })
	return fresh
}

// handoffAnnouncement words the toast for files handed to this computer.
func handoffAnnouncement(fresh []claimRow) (title, body string) {
	if len(fresh) == 0 {
		return "", ""
	}
	if len(fresh) == 1 {
		r := fresh[0]
		return truncate(r.From.Name+" handed you "+filepath.Base(r.Path), 60),
			"In " + r.Label + ". It is marked as yours now, so the others can see you have it. Press Done in the app when you are finished."
	}
	names := make([]string, 0, claimsMaxNames)
	for i, r := range fresh {
		if i == claimsMaxNames {
			break
		}
		names = append(names, filepath.Base(r.Path))
	}
	list := strings.Join(names, ", ")
	if len(fresh) > claimsMaxNames {
		list += fmt.Sprintf(" and %d more", len(fresh)-claimsMaxNames)
	}
	return fmt.Sprintf("%d files were handed to you", len(fresh)),
		list + ". They are marked as yours now."
}

// collisions is the pure half of the warning: which files that somebody else
// has marked just changed on this computer. Deletions count -- deleting a
// file somebody has open is the same collision, only worse.
func collisions(evs []diskEvent, rows []claimRow) []claimRow {
	byKey := map[string]claimRow{}
	for _, r := range rows {
		if r.Mine {
			continue
		}
		byKey[r.Folder+"\x00"+r.Path] = r
	}
	var hits []claimRow
	hit := map[string]bool{}
	for _, ev := range evs {
		if ev.Type != "LocalChangeDetected" || ev.Data.Type != "file" {
			continue
		}
		k := ev.Data.Folder + "\x00" + filepath.ToSlash(ev.Data.Path)
		if r, ok := byKey[k]; ok && !hit[k] {
			hit[k] = true
			hits = append(hits, r)
		}
	}
	return hits
}

func collisionMessage(r claimRow) (title, body string) {
	return truncate(r.Name+" is working on "+filepath.Base(r.Path), 60),
		"It just changed on this computer as well. Tell " + r.Name +
			" now: if you both save it, one of you ends up with the other's version."
}

// watchClaims runs the claims side of the tray for as long as ctx lives.
func (a *alerter) watchClaims(ctx context.Context) {
	w := newClaimWatch()
	if !sleepCtx(ctx, claimsFirstCheck) {
		return
	}
	for {
		a.checkClaims(w)
		if !sleepCtx(ctx, claimsCheckInterval) {
			return
		}
	}
}

func (a *alerter) checkClaims(w *claimWatch) {
	cl := a.current()
	if cl == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	reply, err := cl.claims()
	if err != nil {
		// A build older than the endpoint, or Syncthing restarting. Neither is
		// worth more than a debug line every twenty seconds.
		slog.Debug("could not read claims", "err", err)
		return
	}

	if w.since.IsZero() {
		w.since = time.Now().Add(-claimsGrace)
	}
	if fresh := newPeerClaims(reply.Claims, w.seen, w.since); len(fresh) > 0 {
		title, body := claimAnnouncement(fresh)
		slog.Info("announced files somebody else is working on", "count", len(fresh), "title", title)
		a.notify.Notify(Notification{Title: title, Body: body, Launch: a.guiURL()})
	}
	if fresh := newHandoffs(reply.Claims, w.handed, w.since); len(fresh) > 0 {
		title, body := handoffAnnouncement(fresh)
		slog.Info("announced files handed to this computer", "count", len(fresh), "title", title)
		a.notify.Notify(Notification{Title: title, Body: body, Launch: a.guiURL()})
	}

	evs := a.checkCollisions(cl, w, reply.Claims)
	a.autoClaim(cl, w, evs, reply.Claims)
	a.autoRelease(cl, reply.Claims, time.Now())
	a.checkResurrections(cl, evs)
	a.checkNotes(cl)
	ensureClaimsFolders(cl)
	if a.claimsChanged != nil {
		a.claimsChanged()
	}
}

// checkCollisions reads the disk-change feed and warns about any file that
// somebody else has marked and that just changed here. It hands back the
// events it read, which is also what the automatic marks are made from.
func (a *alerter) checkCollisions(cl *client, w *claimWatch, rows []claimRow) []diskEvent {
	// Where "now" is, every pass. A restarted Syncthing numbers from 1 again,
	// and asking it for events after an old, higher cursor returns nothing at
	// all -- silently, for ever. The newest ID going below the cursor is the
	// only way to see that happen.
	latest, err := cl.latestDiskEventID()
	if err != nil {
		slog.Debug("could not read disk events", "err", err)
		return nil
	}
	if w.diskCursor < 0 || latest < w.diskCursor {
		// First pass, or a restart. Either way, changes from before now are
		// not something anybody can still do anything about -- and after a
		// restart they are the initial scan, which reports every file.
		w.diskCursor = latest
		return nil
	}
	if latest == w.diskCursor {
		return nil
	}
	evs, err := cl.diskEvents(w.diskCursor)
	if err != nil {
		slog.Debug("could not read disk events", "err", err)
		return nil
	}
	for _, ev := range evs {
		if ev.ID > w.diskCursor {
			w.diskCursor = ev.ID
		}
	}

	now := time.Now()
	for _, r := range collisions(evs, rows) {
		k := r.Folder + "\x00" + r.Path
		if t, ok := w.warned[k]; ok && now.Sub(t) < claimCollisionCooldown {
			continue
		}
		w.warned[k] = now
		title, body := collisionMessage(r)
		slog.Info("warned that a file somebody else is working on changed here",
			"folder", r.Folder, "path", r.Path, "who", r.Name)
		a.notify.Notify(Notification{Title: title, Body: body, Launch: a.guiURL()})
	}
	return evs
}

// ensureClaimsFolders keeps each folder's claims directory hidden, and makes
// sure a folder the picker is holding back still lets claims through.
func ensureClaimsFolders(cl *client) {
	cfg, err := cl.config()
	if err != nil {
		return
	}
	for _, f := range cfg.Folders {
		if f.Path != "" {
			dir := filepath.Join(f.Path, claimsDirName)
			if st, err := os.Stat(dir); err == nil && st.IsDir() {
				hideDir(dir)
			}
		}
		ign, err := cl.ignores(f.ID)
		if err != nil || ign.Error != "" {
			continue
		}
		if next, changed := withClaimsException(ign.Ignore); changed {
			if err := cl.setIgnores(f.ID, next); err != nil {
				slog.Warn("could not let claims through the folder's hold-back", "folder", f.ID, "err", err)
				continue
			}
			slog.Info("let claims through the folder's hold-back", "folder", f.ID)
		}
	}
}

// selectiveBegin is the selective-sync picker's block marker. The picker
// writes everything else first and its block last (selectiveSync.js).
const selectiveBegin = "//// desuqcafe selective sync -- rewritten by the file picker, do not hand edit"

// withClaimsException returns the ignore lines with the claims exception in
// place, and whether anything had to change.
//
// Only a folder that ignores everything needs it -- a bare "*" or "**", which
// is what the picker writes to hold a folder back. Anywhere else the claims
// directory is not ignored in the first place, and a line that does nothing
// is a line somebody later wonders about. An existing mention of the
// directory, either way round, is left alone: somebody wrote it.
func withClaimsException(lines []string) ([]string, bool) {
	catchAll := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.Contains(t, claimsDirName) {
			return lines, false
		}
		if catchAll < 0 && (t == "*" || t == "**" || t == "/*" || t == "/**") {
			catchAll = i
		}
	}
	if catchAll < 0 {
		return lines, false
	}
	// Above the picker's block if there is one, so the picker's rewrite --
	// which keeps everything outside its block -- keeps this too.
	at := catchAll
	for i, l := range lines {
		if strings.TrimSpace(l) == selectiveBegin && i < at {
			at = i
		}
	}
	out := make([]string, 0, len(lines)+2)
	out = append(out, lines[:at]...)
	out = append(out, claimsExceptionComment, claimsException)
	out = append(out, lines[at:]...)
	return out, true
}

// --- Send To ---------------------------------------------------------------

// folderForPath finds which synced folder holds p, and p's path inside it.
// The longest root wins, so a folder synced inside another is found rather
// than its parent. Windows paths compare without regard to case.
func folderForPath(folders []restFolder, p string) (restFolder, string, bool) {
	clean := filepath.Clean(p)
	var best restFolder
	bestRel := ""
	bestLen := -1
	for _, f := range folders {
		if f.Path == "" {
			continue
		}
		root := filepath.Clean(f.Path)
		rel, ok := within(root, clean)
		if !ok || rel == "." {
			continue
		}
		if len(root) > bestLen {
			best, bestRel, bestLen = f, rel, len(root)
		}
	}
	if bestLen < 0 {
		return restFolder{}, "", false
	}
	return best, filepath.ToSlash(bestRel), true
}

// within reports p relative to root when p is inside it.
func within(root, p string) (string, bool) {
	r, pp := root, p
	if filepath.Separator == '\\' {
		r, pp = strings.ToLower(r), strings.ToLower(p)
	}
	if pp != r && !strings.HasPrefix(pp, strings.TrimSuffix(r, string(filepath.Separator))+string(filepath.Separator)) {
		return "", false
	}
	rel, err := filepath.Rel(root, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return rel, true
}

// runClaim is the Send To one-shot: toggle "I'm working on this" for every
// file handed over, then say what happened in a toast and exit.
//
// Toggle rather than two entries because two nearly identical lines in the
// Send To menu is one more thing to get wrong; the toast says which way it
// went, and how to undo it.
func runClaim(home string, paths []string, n notifier) int {
	say := func(title, body string) {
		n.Notify(Notification{Title: title, Body: body})
	}
	if len(paths) == 0 {
		say("Nothing to mark", "Right-click a file, then Send to. Folders cannot be marked, only files.")
		return 2
	}

	ep, err := readEndpoint(home)
	if err != nil {
		say("desuqcafe Syncthing is not set up", "Start it once from the Start menu, then try again.")
		return 1
	}
	cl := newClient(ep)
	cfg, err := cl.config()
	if err != nil {
		say("desuqcafe Syncthing is not running", "Start it from the Start menu, then try again. Nothing was marked.")
		return 1
	}
	current, err := cl.claims()
	if err != nil {
		say("Could not mark that file", "This version of desuqcafe Syncthing does not support it yet. Update it, then try again.")
		return 1
	}
	held := map[string]bool{}
	for _, r := range current.Claims {
		if r.Mine {
			held[r.Folder+"\x00"+r.Path] = true
		}
	}

	var marked, released, refused []string
	reason, reach := "", ""
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			refused = append(refused, filepath.Base(p))
			reason = "Folders cannot be marked, only the files in them."
			continue
		}
		f, rel, ok := folderForPath(cfg.Folders, p)
		if !ok {
			refused = append(refused, filepath.Base(p))
			reason = "That file is not in a folder this computer syncs."
			continue
		}
		release := held[f.ID+"\x00"+rel]
		reply, err := cl.claim(f.ID, rel, release, false)
		if err != nil {
			refused = append(refused, filepath.Base(p))
			reason = claimRefusal(err)
			continue
		}
		if release {
			released = append(released, filepath.Base(p))
		} else {
			marked = append(marked, filepath.Base(p))
			if r := reachSentence(unseenFor(reply, f.ID)); r != "" {
				reach = r
			}
		}
	}

	switch {
	case len(marked) > 0 && len(released) == 0 && len(refused) == 0:
		// Only "everyone can see that now" when it is true. A mark made
		// while the other person's computer is off reaches nobody, and this
		// toast used to say it had.
		body := "Everyone you share it with can see that now. Mark it again when you are done."
		if reach != "" {
			body = reach + " Mark it again when you are done."
		}
		say(truncate("You are working on "+joinNames(marked), 60), body)
	case len(released) > 0 && len(marked) == 0 && len(refused) == 0:
		say(truncate("Done with "+joinNames(released), 60),
			"You are no longer marked as working on it.")
	case len(refused) > 0 && len(marked) == 0 && len(released) == 0:
		say(truncate("Could not mark "+joinNames(refused), 60), reason)
		return 1
	default:
		var parts []string
		if len(marked) > 0 {
			parts = append(parts, "Marked: "+joinNames(marked)+".")
			if reach != "" {
				parts = append(parts, reach)
			}
		}

		if len(released) > 0 {
			parts = append(parts, "Done with: "+joinNames(released)+".")
		}
		if len(refused) > 0 {
			parts = append(parts, "Not marked: "+joinNames(refused)+". "+reason)
		}
		say("Updated what you are working on", strings.Join(parts, " "))
	}
	return 0
}

// claimRefusal turns a failed POST into a sentence. The server's refusals are
// already sentences; anything else is not something a person can act on.
func claimRefusal(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "409"):
		return "That folder only receives on this computer, or is paused, so nobody else would see the mark."
	case strings.Contains(msg, "404"):
		return "Syncthing does not know that file yet. Wait for it to finish checking the folder, then try again."
	}
	return "Something went wrong. Open desuqcafe Syncthing and try again."
}

// pausedClaimsMessage words the warning for pausing while holding marks, or
// returns empty strings when this computer holds none.
func pausedClaimsMessage(rows []claimRow) (title, body string) {
	var mine []string
	for _, r := range rows {
		if r.Mine {
			mine = append(mine, filepath.Base(r.Path))
		}
	}
	if len(mine) == 0 {
		return "", ""
	}
	sort.Strings(mine)
	what, it := mine[0], "it"
	if len(mine) > 1 {
		what, it = fmt.Sprintf("%d files", len(mine)), "them"
	}
	return truncate("Paused while you are working on "+what, 60),
		"The others still see your mark, so they will leave " + it + " alone -- but they will not see " +
			"it come off until you resume. If you are done with " + it + ", resume, take the mark off, then pause."
}
