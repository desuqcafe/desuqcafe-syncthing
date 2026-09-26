package main

// A file you deleted, back again.
//
// When one computer deletes a file while another changes it, Syncthing keeps
// whichever happened later. If the change was later, the file comes back on
// the computer that deleted it -- with no conflict copy, no error and no
// event that says "resurrected". Verified on a pair, 2026-09-26: A deleted
// texture2.png, B then changed it, and on reconnecting texture2.png was
// simply there again on A with B's bytes. (The other order is a conflict
// copy, and conflicts.go says that one.)
//
// Nothing in the index remembers that this computer deleted it, so the tray
// does: every local deletion on the disk-change feed goes into a small file
// beside the configuration, and a remote change that brings one of those
// names back is said out loud. Kept on disk because the commonest way to be
// apart is to be switched off -- delete on Monday, restart, reconnect on
// Tuesday -- and a memory that dies with the process forgets exactly then.

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// deletedMemoFile lives in the Syncthing home, beside tray.log.
	deletedMemoFile = "tray-deleted.json"

	// deletedMemoKeep is how long a deletion is remembered. Two people apart
	// for longer than a month have bigger news than this.
	deletedMemoKeep = 30 * 24 * time.Hour

	// deletedMemoMax bounds the file. Emptying a folder of ten thousand
	// renders is a deletion of ten thousand files, and the one that comes
	// back is still worth hearing about -- but not at the price of a
	// megabyte rewritten every twenty seconds.
	deletedMemoMax = 5000
)

// deletedMemo is the remembered local deletions, keyed folder NUL path.
type deletedMemo struct {
	mu   sync.Mutex
	path string
	m    map[string]time.Time
}

func loadDeletedMemo(home string) *deletedMemo {
	d := &deletedMemo{path: filepath.Join(home, deletedMemoFile), m: map[string]time.Time{}}
	if raw, err := os.ReadFile(d.path); err == nil {
		_ = json.Unmarshal(raw, &d.m)
	}
	return d
}

func (d *deletedMemo) save() {
	raw, err := json.Marshal(d.m)
	if err != nil {
		return
	}
	tmp := d.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		slog.Debug("could not remember deletions", "err", err)
		return
	}
	_ = os.Rename(tmp, d.path)
}

// resurrected is one file that came back.
type resurrected struct {
	folder string
	path   string
	by     string // short device ID of whoever changed it
}

// observe feeds one batch of disk events through the memo and returns the
// files that came back. It is the whole rule, and pure but for the save.
func (d *deletedMemo) observe(evs []diskEvent, now time.Time) []resurrected {
	if d == nil || len(evs) == 0 {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	changed := false
	var back []resurrected
	for _, ev := range evs {
		if ev.Data.Type != "file" {
			continue
		}
		p := filepath.ToSlash(ev.Data.Path)
		if p == claimsDirName || strings.HasPrefix(p, claimsDirName+"/") {
			continue
		}
		k := ev.Data.Folder + "\x00" + p
		switch {
		case ev.Type == "LocalChangeDetected" && ev.Data.Action == "deleted":
			d.m[k] = ev.Time
			changed = true
		case ev.Type == "LocalChangeDetected":
			// Put back here, by hand: not something to announce.
			if _, ok := d.m[k]; ok {
				delete(d.m, k)
				changed = true
			}
		case ev.Type == "RemoteChangeDetected" && ev.Data.Action == "deleted":
			if _, ok := d.m[k]; ok {
				delete(d.m, k)
				changed = true
			}
		case ev.Type == "RemoteChangeDetected":
			if _, ok := d.m[k]; ok {
				delete(d.m, k)
				changed = true
				back = append(back, resurrected{folder: ev.Data.Folder, path: p, by: ev.Data.ModifiedBy})
			}
		}
	}
	for k, t := range d.m {
		if now.Sub(t) > deletedMemoKeep {
			delete(d.m, k)
			changed = true
		}
	}
	if len(d.m) > deletedMemoMax {
		keys := make([]string, 0, len(d.m))
		for k := range d.m {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return d.m[keys[i]].Before(d.m[keys[j]]) })
		for _, k := range keys[:len(keys)-deletedMemoMax] {
			delete(d.m, k)
		}
		changed = true
	}
	if changed {
		d.save()
	}
	return back
}

// resurrectionMessage words the toast. names maps a short device ID to a
// person's name.
func resurrectionMessage(back []resurrected, names map[string]string, labels map[string]string) (title, body string) {
	if len(back) == 0 {
		return "", ""
	}
	who := func(short string) string {
		if n := names[short]; n != "" {
			return n
		}
		return "Somebody"
	}
	first := back[0]
	if len(back) == 1 {
		where := ""
		if l := labels[first.folder]; l != "" {
			where = " in " + l
		}
		return truncate(filepath.Base(first.path)+" came back", 60),
			"You deleted it" + where + ", but " + who(first.by) + " had changed it while you were apart, " +
				"and the later change wins. Delete it again if it should go."
	}
	var files []string
	for _, b := range back {
		files = append(files, filepath.Base(b.path))
	}
	sort.Strings(files)
	return plural(len(back), "file") + " you deleted came back",
		someNames(files, reconnectMaxNames) + ". They had been changed somewhere else while you were apart, " +
			"and the later change wins. Delete them again if they should go."
}

// checkResurrections runs the memo over one pass's events and says what came
// back -- or leaves it for the reunion toast, when one is being prepared.
func (a *alerter) checkResurrections(cl *client, evs []diskEvent) {
	back := a.deleted.observe(evs, time.Now())
	if len(back) == 0 {
		return
	}
	a.mu.Lock()
	if len(a.briefing) > 0 {
		a.backStash = append(a.backStash, back...)
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()

	names, labels := map[string]string{}, map[string]string{}
	if cfg, err := cl.config(); err == nil {
		for _, d := range cfg.Devices {
			names[shortDeviceID(d.DeviceID)] = d.Name
		}
		for _, f := range cfg.Folders {
			labels[f.ID] = f.name()
		}
	}
	title, body := resurrectionMessage(back, names, labels)
	slog.Info("a file deleted here came back", "count", len(back), "title", title)
	a.notify.Notify(Notification{Title: title, Body: body, Launch: a.guiURL()})
}

// takeBackStash hands the briefing whatever came back while it waited.
func (a *alerter) takeBackStash() []resurrected {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.backStash
	a.backStash = nil
	return out
}
