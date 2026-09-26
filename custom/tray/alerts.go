package main

// Which events become desktop notifications, and -- mostly -- which do not.
//
// The hard part of notifying from Syncthing's event stream is not raising a
// toast, it is not raising ten thousand. LocalChangeDetected fires once per
// file; FolderSummary fires every few seconds per folder while anything is
// moving. A naive "toast on interesting event" would make a first sync of a
// texture library unusable and train the user to dismiss everything, which is
// worse than no notifications at all.
//
// So there are seven things worth interrupting somebody for, and every one of
// them is either a question only a person can answer or a state that will not
// fix itself:
//
//	a device asking to connect      -- needs a human decision
//	a folder being offered          -- needs a human decision
//	a sync finishing                -- the "my files have arrived" signal
//	a folder erroring               -- will not clear on its own
//	a disk about to fill            -- will wedge the folder if ignored
//	two people editing one file     -- see conflicts.go; the only one of these
//	                                   that silently costs somebody a day's work
//	a teammate on a newer build     -- see update.go; will not fix itself
//	                                   either, because there is no auto-upgrade
//
// Everything else stays in the icon and the tooltip.

import (
	"context"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// syncSettleDelay is how long a folder has to stay finished before the
	// "sync complete" toast goes out. It coalesces several folders finishing
	// together into one toast, and swallows the case where a folder reaches
	// zero and immediately goes again because more changes arrived.
	syncSettleDelay = 10 * time.Second

	// syncCooldown is the minimum gap between two "sync complete" toasts for
	// the same folder, for a folder being edited live at the other end.
	syncCooldown = 2 * time.Minute

	// errorCooldown keeps a folder that is erroring on every pull attempt from
	// producing a toast every few seconds. Cleared as soon as it recovers.
	errorCooldown = 30 * time.Minute

	// diskCooldown is per drive. A full disk is not going to be fixed in the
	// next five minutes, and repeating the warning does not make it more true.
	diskCooldown = 6 * time.Hour

	// diskCheckInterval is how often free space is sampled. Disk space is the
	// one alert with no event behind it, so it is the one thing still polled.
	diskCheckInterval = 5 * time.Minute

	// updateCooldown is the gap between two "somebody you sync with is ahead
	// of you" toasts. A version that is newer today is still newer tomorrow,
	// and the person cannot act on it any faster for being told twice.
	updateCooldown = 24 * time.Hour

	// updateCheckInterval catches the case the DeviceConnected event cannot:
	// a tray started against an instance whose peers were already connected,
	// which is what -attach does and what a tray restarted by hand does.
	updateCheckInterval = 6 * time.Hour
)

// alerter turns events into notifications. Every method is safe to call from
// the event goroutine and the disk-check goroutine at once.
type alerter struct {
	notify notifier
	// guiURL is looked up per toast rather than captured, because Syncthing
	// rewrites its GUI address when the port it wanted turns out to be taken.
	guiURL  func() string
	current func() *client

	mu sync.Mutex

	// announced remembers which pending devices and folders have already been
	// toasted, so a PendingDevicesChanged caused by something else does not
	// re-announce the same request. Entries are dropped when the request goes
	// away, so a device that is dismissed and comes back does notify again.
	announcedDevices map[string]bool
	announcedFolders map[string]bool

	// behind is the most bytes a folder has been short by since it was last in
	// sync. It is the "was there actually anything to do" test that stops a
	// toast firing every time an idle folder reports itself idle.
	behind map[string]int64
	// finished is folders that have reached zero and are waiting out
	// syncSettleDelay, with the number of bytes they received.
	finished    map[string]int64
	settleTimer *time.Timer
	lastSynced  map[string]time.Time

	lastErrored map[string]time.Time
	lastDisk    map[string]time.Time

	// seenConflicts is the conflicting copies already accounted for, keyed by
	// full path. Entries are dropped when the file goes, so resolving a
	// conflict and hitting the same one again really does notify twice.
	seenConflicts map[string]bool
	// conflictsSince is the cutoff a conflict's modification time has to beat
	// to count as new. Set on the first pass rather than at construction, so
	// it cannot be skewed by however long start-up took. See conflicts.go.
	conflictsSince time.Time

	// staleNagged is when each device was last named in a "has not synced"
	// toast, so a machine that stays away for a fortnight is mentioned every
	// few days rather than every few hours. Keyed by device ID, and never
	// cleared: a device that comes back has its entry go stale on its own,
	// and the map is bounded by the number of devices configured.
	staleNagged map[string]time.Time

	// naggedVersion is the peer version last announced, and lastNag when. The
	// pair means a *newer* version still gets through inside the cooldown --
	// two releases in a day is unusual but the second one is not less true --
	// while the same one does not repeat on every reconnect.
	naggedVersion string
	lastNag       time.Time

	// started is when this tray began, apart is when each peer was last seen
	// to disconnect, and briefing is the peers whose "back in touch" toast is
	// being prepared. See reconnect.go.
	started   time.Time
	apart     map[string]time.Time
	connected map[string]bool
	briefing  map[string]bool
	// backStash is files deleted here that came back while a briefing was
	// being prepared; the briefing says them. See resurrect.go.
	backStash []resurrected
	// deleted remembers this computer's deletions across restarts. Set by
	// the app; nil in tests, where observe is a no-op.
	deleted *deletedMemo

	// claimsChanged is called after every claims pass, so the Explorer hover
	// text can follow the claims without a second poll. Set by the app; nil
	// in tests.
	claimsChanged func()

	// deliveries is what each peer has been missing of this computer's
	// changes, and since when. See delivery.go.
	deliveries deliveryTracker
}

func newAlerter(n notifier, guiURL func() string, current func() *client) *alerter {
	return &alerter{
		notify:           n,
		guiURL:           guiURL,
		current:          current,
		announcedDevices: map[string]bool{},
		announcedFolders: map[string]bool{},
		behind:           map[string]int64{},
		finished:         map[string]int64{},
		lastSynced:       map[string]time.Time{},
		lastErrored:      map[string]time.Time{},
		lastDisk:         map[string]time.Time{},
		seenConflicts:    map[string]bool{},
		staleNagged:      map[string]time.Time{},
		started:          time.Now(),
		apart:            map[string]time.Time{},
		connected:        map[string]bool{},
		briefing:         map[string]bool{},
	}
}

// handle is called for every event the tray subscribed to.
func (a *alerter) handle(ev event) {
	switch ev.Type {
	case "PendingDevicesChanged":
		a.checkPendingDevices()
	case "PendingFoldersChanged":
		a.checkPendingFolders()
	case "FolderSummary":
		if s, err := decodeEvent[folderSummary](ev); err == nil {
			a.folderProgress(s.Folder, s.Summary.State,
				s.Summary.NeedBytes, s.Summary.NeedItems, s.Summary.Errors)
		}
	case "FolderErrors":
		if e, err := decodeEvent[folderErrors](ev); err == nil && len(e.Errors) > 0 {
			a.folderErrored(e.Folder, e.Errors[0].Error)
		}
	case "StateChanged":
		if s, err := decodeEvent[stateChanged](ev); err == nil && s.To == "error" {
			a.folderErrored(s.Folder, "")
		}
	case "DeviceConnected":
		// The moment a peer's version becomes knowable. The payload carries it
		// too, but the authoritative list is re-read for the same reason
		// checkPendingDevices does: one small local request beats tracking a
		// payload shape across upstream versions.
		a.checkPeerVersions()
		// The ID, though, is only in the payload.
		if d, err := decodeEvent[deviceEvent](ev); err == nil && d.ID != "" {
			a.peerConnected(d.ID, ev.Time)
		}
	case "DeviceDisconnected":
		if d, err := decodeEvent[deviceEvent](ev); err == nil && d.ID != "" {
			a.peerDisconnected(d.ID, ev.Time)
		}
	}
}

// --- pending devices and folders -----------------------------------------

// checkPendingDevices re-reads the authoritative list rather than decoding the
// event payload. The payload carries "added" and "removed" deltas whose shape
// has changed across upstream versions, and the list is small; asking for it is
// both simpler and more robust than tracking deltas correctly.
func (a *alerter) checkPendingDevices() {
	cl := a.current()
	if cl == nil {
		return
	}
	pending, err := cl.pendingDevices()
	if err != nil {
		slog.Debug("could not read pending devices", "err", err)
		return
	}

	fresh := a.reconcile(a.announcedDevices, keysOf(pending))
	for _, id := range fresh {
		name := pending[id].Name
		if name == "" {
			name = "An unnamed device"
		} else {
			name = "\"" + name + "\""
		}
		a.notify.Notify(Notification{
			Title:  "A new device wants to connect",
			Body:   name + " (" + shortDeviceID(id) + ") is asking to connect. Click to review it.",
			Launch: a.guiURL(),
		})
	}
}

func (a *alerter) checkPendingFolders() {
	cl := a.current()
	if cl == nil {
		return
	}
	pending, err := cl.pendingFolders()
	if err != nil {
		slog.Debug("could not read pending folders", "err", err)
		return
	}

	// Name the device doing the offering rather than showing a device ID.
	names := map[string]string{}
	if cfg, err := cl.config(); err == nil {
		for _, d := range cfg.Devices {
			names[d.DeviceID] = d.Name
		}
	}

	// Key on folder and device together: the same folder offered by two people
	// is two separate decisions.
	var keys []string
	labels := map[string]string{}
	for folderID, pf := range pending {
		for deviceID, offer := range pf.OfferedBy {
			key := folderID + "\x00" + deviceID
			keys = append(keys, key)

			who := names[deviceID]
			if who == "" {
				who = shortDeviceID(deviceID)
			}
			what := offer.Label
			if what == "" {
				what = folderID
			}
			labels[key] = who + " wants to share \"" + what + "\" with you. Click to review it."
		}
	}

	for _, key := range a.reconcile(a.announcedFolders, keys) {
		a.notify.Notify(Notification{
			Title:  "A new folder has been offered",
			Body:   labels[key],
			Launch: a.guiURL(),
		})
	}
}

// reconcile updates seen to exactly the given keys and returns the ones that
// were not there before.
func (a *alerter) reconcile(seen map[string]bool, keys []string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()

	present := make(map[string]bool, len(keys))
	var fresh []string
	for _, k := range keys {
		present[k] = true
		if !seen[k] {
			fresh = append(fresh, k)
		}
	}
	for k := range seen {
		if !present[k] {
			delete(seen, k)
		}
	}
	for _, k := range fresh {
		seen[k] = true
	}
	sort.Strings(fresh)
	return fresh
}

// --- sync completion ------------------------------------------------------

// folderProgress tracks one folder's distance from being in sync, and fires
// when it arrives.
//
// The transition that matters is "was behind, now is not". Testing the state
// alone would toast every time an idle folder rescanned and found nothing,
// which is every rescan interval, for every folder, forever.
func (a *alerter) folderProgress(folder, state string, needBytes, needItems int64, errors int) {
	if errors > 0 || state == "error" {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// A healthy summary is also the signal that a folder recovered. Clearing
	// the cooldown here means the next failure after a working spell is
	// announced, rather than swallowed by a cooldown started half an hour ago.
	delete(a.lastErrored, folder)

	if needBytes > 0 || needItems > 0 {
		if needBytes > a.behind[folder] {
			a.behind[folder] = needBytes
		}
		// A folder that is behind again should not still be listed as
		// finished-and-waiting-to-be-announced.
		delete(a.finished, folder)
		return
	}

	// In sync. Only interesting if there was something to catch up on.
	was := a.behind[folder]
	if was == 0 {
		return
	}
	delete(a.behind, folder)

	if last, ok := a.lastSynced[folder]; ok && time.Since(last) < syncCooldown {
		return
	}
	a.finished[folder] = was
	a.armSettleTimerLocked()
}

// armSettleTimerLocked schedules the coalesced announcement. Called with mu
// held. Resetting rather than starting a second timer is what makes several
// folders finishing within a few seconds into a single toast.
func (a *alerter) armSettleTimerLocked() {
	if a.settleTimer != nil {
		a.settleTimer.Reset(syncSettleDelay)
		return
	}
	a.settleTimer = time.AfterFunc(syncSettleDelay, a.announceFinished)
}

func (a *alerter) announceFinished() {
	a.mu.Lock()
	if len(a.finished) == 0 {
		a.mu.Unlock()
		return
	}

	var (
		names []string
		total int64
		now   = time.Now()
	)
	cl := a.current()
	labels := map[string]string{}
	if cl != nil {
		if cfg, err := cl.config(); err == nil {
			for _, f := range cfg.Folders {
				labels[f.ID] = f.name()
			}
		}
	}
	for id, bytes := range a.finished {
		name := labels[id]
		if name == "" {
			name = id
		}
		names = append(names, name)
		total += bytes
		a.lastSynced[id] = now
		delete(a.finished, id)
	}
	a.mu.Unlock()

	// A folder finishing is the moment a conflict is made, so look now
	// rather than at the next five-minute pass. If there is one, it is the
	// only thing worth saying: "up to date" over two people's clashing edits
	// is true in the way that misleads. And while a peer who has been away is
	// being caught up with, the briefing says all of it (reconnect.go).
	if a.briefingActive() || a.checkConflicts() {
		return
	}

	sort.Strings(names)

	body := joinNames(names) + " is up to date."

	if len(names) > 1 {
		body = joinNames(names) + " are up to date."
	}
	if total > 0 {
		body += " " + formatBytes(total) + " received."
	}

	a.notify.Notify(Notification{
		Title:  "Sync complete",
		Body:   body,
		Launch: a.guiURL(),
	})
}

// --- folder errors --------------------------------------------------------

func (a *alerter) folderErrored(folder, detail string) {
	a.mu.Lock()
	if last, ok := a.lastErrored[folder]; ok && time.Since(last) < errorCooldown {
		a.mu.Unlock()
		return
	}
	a.lastErrored[folder] = time.Now()
	a.mu.Unlock()

	name := folder
	if cl := a.current(); cl != nil {
		if cfg, err := cl.config(); err == nil {
			for _, f := range cfg.Folders {
				if f.ID == folder {
					name = f.name()
					break
				}
			}
		}
	}

	body := "Syncing has stopped for this folder. Click to see why."
	if detail != "" {
		body = truncate(detail, 120) + " Click to open Syncthing."
	}
	a.notify.Notify(Notification{
		Title:  "Problem with " + name,
		Body:   body,
		Launch: a.guiURL(),
	})
}

// --- disk space -----------------------------------------------------------

// --- somebody you sync with is running a newer build ----------------------

// watchVersions is the slow backstop for the DeviceConnected event. See
// updateCheckInterval.
func (a *alerter) watchVersions(ctx context.Context) {
	// Long enough after start-up that the first connections have had time to
	// complete and report a version; anything sooner just reads empty fields.
	if !sleepCtx(ctx, 90*time.Second) {
		return
	}
	for {
		a.checkPeerVersions()
		if !sleepCtx(ctx, updateCheckInterval) {
			return
		}
	}
}

// checkPeerVersions compares this build against the devices it is connected to
// and says something if one of them is ahead.
//
// Nothing here contacts anything but the local Syncthing -- see update.go for
// why this is a version comparison between peers rather than a poll of a
// releases API, and for the rule that keeps a peer on stock Syncthing from
// ever triggering it.
func (a *alerter) checkPeerVersions() {
	cl := a.current()
	if cl == nil {
		return
	}

	mine, err := cl.version()
	if err != nil {
		slog.Debug("could not read this build's version", "err", err)
		return
	}

	conns, err := cl.connections()
	if err != nil {
		slog.Debug("could not read connections", "err", err)
		return
	}

	names := map[string]string{}
	if cfg, err := cl.config(); err == nil {
		for _, d := range cfg.Devices {
			names[d.DeviceID] = d.Name
		}
	}

	var peers []peerVersion
	for id, c := range conns.Connections {
		if !c.Connected || c.ClientVersion == "" {
			continue
		}
		who := names[id]
		if who == "" {
			who = shortDeviceID(id)
		}
		peers = append(peers, peerVersion{name: who, version: c.ClientVersion})
	}

	ahead, found := newestPeerAhead(mine, peers)
	if !found {
		return
	}

	a.mu.Lock()
	repeat := ahead.version == a.naggedVersion && time.Since(a.lastNag) < updateCooldown
	if !repeat {
		a.naggedVersion = ahead.version
		a.lastNag = time.Now()
	}
	a.mu.Unlock()
	if repeat {
		return
	}

	a.notify.Notify(Notification{
		Title: "An update is available",
		Body: ahead.name + " is running " + ahead.version + " and you have " + mine +
			". Click to download the new installer.",
		// The one external address in the tray, and it is only ever handed to
		// a browser by somebody clicking this. Nothing fetches it.
		Launch: releasesURL,
	})
	slog.Info("a connected device is running a newer build",
		"peer", ahead.name, "theirs", ahead.version, "ours", mine)
}

// watchStale is the "somebody stopped syncing and nobody noticed" check. See
// stale.go for the rules and for why the wording is so careful.
func (a *alerter) watchStale(ctx context.Context) {
	if !sleepCtx(ctx, staleFirstCheck) {
		return
	}
	for {
		a.checkStale()
		if !sleepCtx(ctx, staleCheckInterval) {
			return
		}
	}
}

// checkStale gathers the three things stale.go needs -- who is configured, who
// is connected now, and when each was last seen -- and raises at most one
// toast for the lot.
func (a *alerter) checkStale() {
	cl := a.current()
	if cl == nil {
		return
	}

	cfg, err := cl.config()
	if err != nil {
		slog.Debug("could not read the configuration", "err", err)
		return
	}
	stats, err := cl.deviceStats()
	if err != nil {
		slog.Debug("could not read device statistics", "err", err)
		return
	}
	// The local device is in /rest/config like any other and would otherwise
	// be judged for not having connected to itself.
	me, err := cl.myID()
	if err != nil {
		slog.Debug("could not read this device's ID", "err", err)
		return
	}
	conns, err := cl.connections()
	if err != nil {
		slog.Debug("could not read connections", "err", err)
		return
	}

	var peers []peerSeen
	for _, d := range cfg.Devices {
		if d.DeviceID == me {
			continue
		}
		name := d.Name
		if name == "" {
			name = shortDeviceID(d.DeviceID)
		}
		peers = append(peers, peerSeen{
			id:        d.DeviceID,
			name:      name,
			paused:    d.Paused,
			connected: conns.Connections[d.DeviceID].Connected,
			lastSeen:  stats[d.DeviceID].LastSeen,
		})
	}

	now := time.Now()
	v := stalePeers(peers, now, staleAfter)
	if len(v.Stale) == 0 {
		return
	}

	// At least one of the devices in the message has to be outside its own
	// cooldown, or this is a repeat. Stamping every named device -- not only
	// the one that got through -- is what stops a second device going stale a
	// day later from re-announcing the first.
	fresh := false
	a.mu.Lock()
	for _, p := range v.Stale {
		if now.Sub(a.staleNagged[p.id]) >= staleCooldown {
			fresh = true
			break
		}
	}
	if fresh {
		for _, p := range v.Stale {
			a.staleNagged[p.id] = now
		}
	}
	a.mu.Unlock()
	if !fresh {
		return
	}

	title, body := staleMessage(v)
	if title == "" {
		return
	}
	a.notify.Notify(Notification{
		Title:  title,
		Body:   body,
		Launch: a.guiURL(),
	})
	slog.Info("a device has not been seen for a while",
		"devices", len(v.Stale), "silence", v.Oldest.Round(time.Hour).String(), "all", v.All)
}

// watchDisk is the one alert with no event behind it. Syncthing publishes
// nothing about free space -- see DEPLOYMENT-3D-TEAM.md section 1 -- so this
// samples the fork's own /rest/system/diskfree instead.
func (a *alerter) watchDisk(ctx context.Context) {
	// A first check shortly after start-up, then on the slow interval. Waiting
	// a full interval would miss a disk that was already full at sign-in,
	// which is exactly when somebody wants to be told.
	if !sleepCtx(ctx, 30*time.Second) {
		return
	}
	for {
		a.checkDisk()
		if !sleepCtx(ctx, diskCheckInterval) {
			return
		}
	}
}

func (a *alerter) checkDisk() {
	cl := a.current()
	if cl == nil {
		return
	}
	cfg, err := cl.config()
	if err != nil {
		return
	}

	// Several folders usually live on one drive, and the drive is what is
	// full. Warn per drive, naming the folder with the largest reserve.
	type drive struct {
		free, total uint64
		reserve     int64
		folder      string
	}
	drives := map[string]*drive{}

	for _, f := range cfg.Folders {
		if f.Paused || f.Path == "" {
			continue
		}
		df, err := cl.diskFree(f.Path)
		if err != nil {
			// Stock Syncthing has no such endpoint; say so once at debug and
			// leave the disk warnings off rather than logging every cycle.
			slog.Debug("no disk figures for folder", "folder", f.ID, "err", err)
			continue
		}
		key := volumeOf(df.MeasuredPath)
		reserve := reserveBytes(f.MinDiskFree.Value, f.MinDiskFree.Unit, df.Total)

		d := drives[key]
		if d == nil {
			d = &drive{free: df.Free, total: df.Total}
			drives[key] = d
		}
		if reserve > d.reserve {
			d.reserve, d.folder = reserve, f.name()
		}
	}

	for key, d := range drives {
		a.judgeDrive(key, d.free, d.total, d.reserve, d.folder)
	}
}

// judgeDrive decides whether a drive is worth a warning.
//
// The thresholds hang off the folder's own minDiskFree rather than a fixed
// percentage, because that reserve is already the number Syncthing will refuse
// to pull below. Below it, syncing has actually stopped; within twice it,
// there is time to do something. The fork seeds 20 GB (see
// DEPLOYMENT-3D-TEAM.md section 4), so "nearly full" means under 40 GB, which
// is about one asset drop's warning.
func (a *alerter) judgeDrive(key string, free, total uint64, reserve int64, folder string) {
	if reserve <= 0 || total == 0 {
		return
	}

	var title, severity string
	switch {
	case int64(free) <= reserve:
		title, severity = "Disk full - syncing has stopped", "full"
	case int64(free) <= reserve*2:
		title, severity = "Disk nearly full", "low"
	default:
		// Recovered. Clear the cooldown so the next slide down warns again
		// rather than being suppressed by a warning from hours ago.
		a.mu.Lock()
		delete(a.lastDisk, key)
		a.mu.Unlock()
		return
	}

	a.mu.Lock()
	if last, ok := a.lastDisk[key]; ok && time.Since(last) < diskCooldown {
		a.mu.Unlock()
		return
	}
	a.lastDisk[key] = time.Now()
	a.mu.Unlock()

	where := key
	if where == "" {
		where = "The sync drive"
	}
	body := where + " has " + formatBytes(int64(free)) + " free of " +
		formatBytes(int64(total)) + ". " + folder + " keeps " +
		formatBytes(reserve) + " in reserve"
	if severity == "full" {
		body += ", so nothing more will download."
	} else {
		body += "."
	}

	a.notify.Notify(Notification{
		Title:  title,
		Body:   body,
		Launch: a.guiURL(),
	})
}

// reserveBytes converts a minDiskFree setting to bytes. Syncthing stores it as
// a value plus a unit, where "%" is a proportion of the whole filesystem and
// an empty unit means bytes.
func reserveBytes(value float64, unit string, total uint64) int64 {
	if value <= 0 {
		return 0
	}
	switch unit {
	case "%":
		return int64(float64(total) * value / 100)
	case "kB":
		return int64(value * 1000)
	case "MB":
		return int64(value * 1000 * 1000)
	case "GB":
		return int64(value * 1000 * 1000 * 1000)
	case "TB":
		return int64(value * 1000 * 1000 * 1000 * 1000)
	default:
		return int64(value)
	}
}

// volumeOf names the drive a path is on, so folders sharing one disk share one
// warning. Falls back to the path itself where there is no volume concept.
func volumeOf(path string) string {
	if v := filepath.VolumeName(path); v != "" {
		return v
	}
	return path
}

func shortDeviceID(id string) string {
	if len(id) > 7 {
		return id[:7]
	}
	return id
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
