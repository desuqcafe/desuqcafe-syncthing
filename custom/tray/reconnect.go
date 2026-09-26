package main

// "Back in touch with Kai": what happened while two computers were apart.
//
// Two people working apart -- one on a train, one with the machine asleep, or
// either of them with syncing paused -- is where every conflict in this setup
// comes from, and the moment they meet again is when it lands. Until now that
// moment said "Sync complete, up to date", which on a pair (2026-09-26) was
// the first thing shown while three conflict copies sat on disk, and the
// conflict toast followed five minutes later.
//
// So when a peer connects after being away, the tray waits for the folders it
// shares with them to settle, then says in one toast what they changed and
// which files both of them changed. It is quiet when there is nothing to say:
// a reconnect that brought nothing in is not news.
//
// WHAT "AWAY" MEANS
//
// A disconnect this tray saw, at least reconnectMinApart before the reconnect
// -- so a network blip is not a briefing. Or, for the first connection after
// the tray starts, unknown: that is signing in in the morning, which is the
// commonest way two people come back together, and the tray cannot know how
// long the machine was off. A connection it never saw drop, long after it
// started, is the event stream being re-established, not a reunion.
//
// DeviceConnected is not "a device connected". Syncthing 2 opens several
// connections to one peer and logs the event for every one of them, while
// DeviceDisconnected fires only when the last goes. Found on a pair: the
// second connection of a two-minute reunion found no disconnect on record,
// fell into the start-up rule, and briefed -- after the pull had finished,
// so it said nothing about what the peer had changed. Hence the connected
// set, and for the unknown case the connection's own start time: a link that
// has been up for more than a moment is an old one gaining a sibling.
//
// WHAT THEY CHANGED
//
// RemoteChangeDetected on the fixed-mask disk feed, attributed by modifiedBy,
// from the moment of connection on. That buffer is 1000 events and memory
// only, so on a big catch-up the count is a floor, and the toast says "at
// least" rather than a number it does not have.

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// reconnectMinApart is the shortest separation worth a briefing.
	reconnectMinApart = 10 * time.Minute

	// reconnectStartWindow is how long after the tray starts a first
	// connection still counts as coming back from being switched off.
	reconnectStartWindow = 10 * time.Minute

	// reconnectSettle is how often the shared folders are checked for having
	// finished, and reconnectGiveUp is when the briefing is said anyway with
	// what has arrived by then. A big catch-up over a slow link can take
	// hours; the person wants to know what is coming before that.
	reconnectSettle = 10 * time.Second
	reconnectGiveUp = 20 * time.Minute

	// reconnectMaxNames is how many of their files the toast names.
	reconnectMaxNames = 3

	// reconnectFreshLink is how old a connection can be and still be the
	// reunion itself rather than a sibling of an established link.
	reconnectFreshLink = 30 * time.Second
)

type deviceEvent struct {
	ID string `json:"id"`
}

// peerDisconnected records when a peer went, for the briefing when it comes
// back.
func (a *alerter) peerDisconnected(id string, at time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.apart[id] = at
	delete(a.connected, id)
}

// briefingWanted is the rule in WHAT "AWAY" MEANS, on its own.
func briefingWanted(apartSince time.Time, known bool, connectedAt, trayStart time.Time) bool {
	if known {
		return connectedAt.Sub(apartSince) >= reconnectMinApart
	}
	return connectedAt.Sub(trayStart) <= reconnectStartWindow
}

// briefingActive is whether any briefing is waiting to be said. The sync
// complete and conflict toasts stand aside meanwhile: the briefing says both,
// better, and three toasts about one reunion is how people learn to dismiss
// toasts.
func (a *alerter) briefingActive() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.briefing) > 0
}

// peerConnected starts a briefing for one peer if it has been away.
func (a *alerter) peerConnected(id string, at time.Time) {
	a.mu.Lock()
	if a.connected[id] {
		// A second connection to a peer already here. See above.
		a.mu.Unlock()
		return
	}
	a.connected[id] = true
	since, known := a.apart[id]
	delete(a.apart, id)
	if a.briefing[id] || !briefingWanted(since, known, at, a.started) {
		a.mu.Unlock()
		return
	}
	a.briefing[id] = true
	a.mu.Unlock()

	go func() {
		defer func() {
			a.mu.Lock()
			delete(a.briefing, id)
			a.mu.Unlock()
		}()
		a.brief(id, at, known)
	}()
}

// brief waits for the folders shared with one peer to settle and says what
// happened while the two were apart.
func (a *alerter) brief(id string, at time.Time, known bool) {
	cl := a.current()
	if cl == nil {
		return
	}
	cursor, err := cl.latestDiskEventID()
	if err != nil {
		return
	}
	if !known {
		// Nothing on record, so this may be a sibling connection on a link
		// that was already up when the tray attached. Ask the link its age.
		if conns, err := cl.connections(); err == nil {
			if c, ok := conns.Connections[id]; ok && !c.StartedAt.IsZero() &&
				at.Sub(c.StartedAt) > reconnectFreshLink {
				return
			}
		}
	}
	cfg, err := cl.config()
	if err != nil {
		return
	}
	name := shortDeviceID(id)
	for _, d := range cfg.Devices {
		if d.DeviceID == id && d.Name != "" {
			name = d.Name
		}
	}
	var shared []string
	for _, f := range cfg.Folders {
		for _, d := range f.Devices {
			if d.DeviceID == id && !f.Paused {
				shared = append(shared, f.ID)
			}
		}
	}
	if len(shared) == 0 {
		return
	}

	// Settled twice in a row, so a folder that goes idle for a moment between
	// the index arriving and the pull starting is not taken as finished.
	deadline := time.Now().Add(reconnectGiveUp)
	settled := 0
	for settled < 2 && time.Now().Before(deadline) {
		time.Sleep(reconnectSettle)
		if a.current() != cl {
			return // Syncthing restarted underneath us; the cursor means nothing now.
		}
		if foldersSettled(cl, shared) {
			settled++
		} else {
			settled = 0
		}
	}

	evs, err := cl.diskEvents(cursor)
	if err != nil {
		return
	}
	theirs, floor := changesBy(evs, shortDeviceID(id), cursor)
	conflicts := a.collectConflicts()
	title, body := briefingMessage(name, theirs, floor, conflicts, a.takeBackStash())
	if title == "" {
		slog.Info("back in touch with a peer; nothing to say", "device", name)
		return
	}
	slog.Info("briefed on what changed while apart", "device", name,
		"theirs", len(theirs), "conflicts", len(conflicts))
	a.notify.Notify(Notification{Title: title, Body: body, Launch: a.guiURL()})
}

func foldersSettled(cl *client, ids []string) bool {
	for _, id := range ids {
		st, err := cl.folderStatus(id)
		if err != nil {
			return false
		}
		if st.State != "idle" || st.NeedItems > 0 || st.NeedBytes > 0 {
			return false
		}
	}
	return true
}

// theirChange is one file a peer changed.
type theirChange struct {
	path    string
	deleted bool
}

// changesBy is the pure half: the files one device changed, as they arrived
// here, from the disk feed after cursor. floor reports that the feed had
// already dropped events past the cursor, so the list is incomplete.
func changesBy(evs []diskEvent, short string, cursor int) (out []theirChange, floor bool) {
	if len(evs) > 0 && evs[0].ID > cursor+1 {
		floor = true
	}
	seen := map[string]int{}
	for _, ev := range evs {
		if ev.Type != "RemoteChangeDetected" || ev.Data.Type != "file" || ev.Data.ModifiedBy != short {
			continue
		}
		p := filepath.ToSlash(ev.Data.Path)
		if p == claimsDirName || strings.HasPrefix(p, claimsDirName+"/") ||
			strings.Contains(filepath.Base(p), conflictMarker) {
			continue
		}
		k := ev.Data.Folder + "\x00" + p
		c := theirChange{path: p, deleted: ev.Data.Action == "deleted"}
		if i, ok := seen[k]; ok {
			out[i] = c // the last word on a file is the one that stands
			continue
		}
		seen[k] = len(out)
		out = append(out, c)
	}
	return out, floor
}

// briefingMessage words the toast, or returns empty strings when there is
// nothing worth saying.
func briefingMessage(name string, theirs []theirChange, floor bool, conflicts []conflictFile, back []resurrected) (title, body string) {
	if len(theirs) == 0 && len(conflicts) == 0 && len(back) == 0 {
		return "", ""
	}
	title = truncate("Back in touch with "+name, 60)

	both := map[string]bool{}
	var parts []string
	if len(back) > 0 {
		var names []string
		for _, b := range back {
			n := filepath.Base(b.path)
			both[n] = true
			names = append(names, n)
		}
		sort.Strings(names)
		verb := " came back: you had deleted it, but they changed it while you were apart, and the later change wins."
		if len(names) > 1 {
			verb = " came back: you had deleted them, but they changed them while you were apart, and the later change wins."
		}
		parts = append(parts, someNames(names, reconnectMaxNames)+verb)
	}

	if len(conflicts) > 0 {
		var names, gone []string
		for _, c := range conflicts {
			n := originalName(c.name)
			both[n] = true
			if c.gone {
				gone = append(gone, n)
			} else {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		sort.Strings(gone)
		if len(names) > 0 {
			parts = append(parts, "You both changed "+someNames(names, reconnectMaxNames)+
				" while apart. Both versions were kept; look for \"sync-conflict\" in the name.")
		}
		if len(gone) > 0 {
			verb := " was"
			if len(gone) > 1 {
				verb = " were"
			}
			parts = append(parts, someNames(gone, reconnectMaxNames)+verb+
				" deleted on one side and changed on the other. The changes were kept as a copy.")
		}
	}

	var changed, deleted []string
	for _, c := range theirs {
		base := filepath.Base(c.path)
		if both[base] {
			continue
		}
		if c.deleted {
			deleted = append(deleted, base)
		} else {
			changed = append(changed, base)
		}
	}
	if len(changed)+len(deleted) > 0 {
		var what []string
		if len(changed) > 0 {
			what = append(what, "changed "+countedNames(changed, floor))
		}
		if len(deleted) > 0 {
			what = append(what, "deleted "+countedNames(deleted, floor))
		}
		lead := "While you were apart they "
		if len(parts) > 0 {
			lead = "They also "
		}
		parts = append(parts, lead+strings.Join(what, ", and ")+".")
	}
	return title, strings.Join(parts, " ")
}

// someNames names up to max files and counts the rest.
func someNames(names []string, max int) string {
	if len(names) <= max {
		return listNames(names)
	}
	return strings.Join(names[:max], ", ") + " and " + plural(len(names)-max, "other file")
}

// countedNames is someNames, with "at least" when the list is known short.
func countedNames(names []string, floor bool) string {
	sort.Strings(names)
	if !floor {
		return someNames(names, reconnectMaxNames)
	}
	return fmt.Sprintf("at least %d files, including %s", len(names),
		listNames(names[:min(len(names), reconnectMaxNames)]))
}
