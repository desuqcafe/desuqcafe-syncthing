package main

// "Why I changed this" (lib/api/api_notes.go): a toast when somebody's note
// arrives, and the latest note in "Who has this?".
//
// A note is usually read after the fact, on the main screen or in History.
// The toast is for the case where after the fact is too late: Kai has put
// the lighting back and you are about to open the file to fix the lighting.
//
// WHAT HAS BEEN ANNOUNCED IS KEPT ON DISK
//
// The marks get away with remembering in memory and a "newer than when the
// tray started" cutoff, because a mark is only news while it is held. A note
// is news whenever it arrives -- typically the morning after, when the
// computer that was off all night catches up -- and by then it is older than
// the tray. So this keeps, per author, the time of the newest note already
// accounted for, in tray-notes.json beside tray.log. Each author's notes are
// stamped by that author's own clock, so one high-water mark per author is
// exact, and a skewed clock on one machine cannot hide another's notes.

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	notesMemoFile = "tray-notes.json"

	// notesFirstLook is how far back a tray that has never run before
	// announces from. Without it, the first start after installing would
	// read out every note anybody ever wrote.
	notesFirstLook = 12 * time.Hour

	// notesMaxNames is how many files one toast names before it counts.
	notesMaxNames = 3
)

// noteRow is one note as /rest/folder/notes reports it.
type noteRow struct {
	Folder  string    `json:"folder"`
	Label   string    `json:"label"`
	Path    string    `json:"path"`
	Device  string    `json:"device"`
	Name    string    `json:"name"`
	Mine    bool      `json:"mine"`
	Text    string    `json:"text"`
	At      time.Time `json:"at"`
	Current bool      `json:"current"`
	Here    bool      `json:"here"`
}

type notesReply struct {
	Notes []noteRow `json:"notes"`
}

func (c *client) notes(folder, file string) (notesReply, error) {
	var out notesReply
	q := url.Values{}
	if folder != "" {
		q.Set("folder", folder)
	}
	if file != "" {
		q.Set("file", file)
	}
	err := c.get("/rest/folder/notes", q, &out)
	return out, err
}

// notesMemo is the per-author high-water mark, and when the memo began.
type notesMemo struct {
	mu   sync.Mutex
	path string
	// Since is when this memo was first made; nothing written before it is
	// announced. Seen is, per author device ID, the newest note accounted for.
	Since time.Time            `json:"since"`
	Seen  map[string]time.Time `json:"seen"`
}

func loadNotesMemo(home string, now time.Time) *notesMemo {
	m := &notesMemo{Seen: map[string]time.Time{}}
	if home != "" {
		m.path = filepath.Join(home, notesMemoFile)
		if raw, err := os.ReadFile(m.path); err == nil {
			_ = json.Unmarshal(raw, m)
		}
	}
	if m.Seen == nil {
		m.Seen = map[string]time.Time{}
	}
	if m.Since.IsZero() {
		m.Since = now.Add(-notesFirstLook)
		m.save()
	}
	return m
}

func (m *notesMemo) save() {
	if m.path == "" {
		return
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		slog.Debug("could not remember notes", "err", err)
		return
	}
	_ = os.Rename(tmp, m.path)
}

// fresh is the pure half: which of these notes by other people are new, and
// worth a toast. Every note by somebody else moves that author's mark,
// announced or not -- a note about a version that has already been replaced
// is history, not news, and should not surface later either.
func (m *notesMemo) fresh(rows []noteRow) []noteRow {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []noteRow
	moved := false
	next := map[string]time.Time{}
	for _, r := range rows {
		if r.Mine || r.Device == "" {
			continue
		}
		cut := m.Since
		if s, ok := m.Seen[r.Device]; ok && s.After(cut) {
			cut = s
		}
		if !r.At.After(cut) {
			continue
		}
		if r.At.After(next[r.Device]) {
			next[r.Device] = r.At
		}
		if r.Current {
			out = append(out, r)
		}
	}
	for dev, t := range next {
		if t.After(m.Seen[dev]) {
			m.Seen[dev] = t
			moved = true
		}
	}
	if moved {
		m.save()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// noteAnnouncement words the toast.
func noteAnnouncement(fresh []noteRow) (title, body string) {
	if len(fresh) == 0 {
		return "", ""
	}
	if len(fresh) == 1 {
		r := fresh[0]
		body = "“" + truncate(r.Text, 140) + "” — in " + r.Label + "."
		if !r.Here {
			body += " The new version is still on its way to you."
		}
		return truncate(r.Name+" changed "+filepath.Base(r.Path), 60), body
	}
	who := fresh[0].Name
	for _, r := range fresh {
		if r.Name != who {
			who = ""
			break
		}
	}
	parts := make([]string, 0, notesMaxNames)
	for i, r := range fresh {
		if i == notesMaxNames {
			break
		}
		parts = append(parts, filepath.Base(r.Path)+": "+truncate(r.Text, 50))
	}
	body = strings.Join(parts, "; ")
	if len(fresh) > notesMaxNames {
		body += fmt.Sprintf("; and %d more", len(fresh)-notesMaxNames)
	}
	if who != "" {
		return truncate(fmt.Sprintf("%s said why they changed %d files", who, len(fresh)), 60), body
	}
	return fmt.Sprintf("%d new notes on changed files", len(fresh)), body
}

// checkNotes runs on the claims cadence (claims.go).
func (a *alerter) checkNotes(cl *client) {
	if a.notesSeen == nil {
		return
	}
	reply, err := cl.notes("", "")
	if err != nil {
		slog.Debug("could not read notes", "err", err)
		return
	}
	if fresh := a.notesSeen.fresh(reply.Notes); len(fresh) > 0 {
		title, body := noteAnnouncement(fresh)
		slog.Info("announced notes on changed files", "count", len(fresh), "title", title)
		a.notify.Notify(Notification{Title: title, Body: body, Launch: a.guiURL()})
	}
}

// latestNote is the newest note about the version of one file that is in the
// folder now, for "Who has this?".
func latestNote(rows []noteRow) (noteRow, bool) {
	var best noteRow
	found := false
	for _, r := range rows {
		if r.Current && (!found || r.At.After(best.At)) {
			best, found = r, true
		}
	}
	return best, found
}

// runNote opens the main screen with the note form on one file. The GUI reads
// the query parameters and opens itself there (home.js, openNoteFromURL).
func runNote(home string, paths []string, n notifier) int {
	_, ep, f, rel, title, body := locate(home, paths)
	if title != "" {
		n.Notify(Notification{Title: title, Body: body})
		return 1
	}
	u := ep.baseURL + "/?desuq-note=" + url.QueryEscape(f.ID) + "&file=" + url.QueryEscape(rel)
	if err := openURL(u); err != nil {
		n.Notify(Notification{Title: "Could not open the browser", Body: "Open desuqcafe Syncthing from the Start menu instead."})
		return 1
	}
	return 0
}

// noteWhy is the note on the current version, as the last sentence of "Who
// has this?".
func noteWhy(r noteRow) string {
	who := r.Name
	if r.Mine {
		who = "you"
	}
	return "Why (" + who + "): “" + truncate(r.Text, 120) + "”"
}
