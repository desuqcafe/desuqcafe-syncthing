package main

// Right-click a .blend in Explorer: Show history, and Who has this?
//
// "I'm working on this" is the third entry, and is runClaim in claims.go, the
// same one-shot the Send To shortcut uses. All three are verbs the installer
// registers under HKCU for the .blend extension alone -- a registry entry
// that runs this binary with the file's path, not a shell extension, so
// nothing is loaded into Explorer (see installer.iss and
// DEPLOYMENT-3D-TEAM.md section 27).
//
// Both of these are one-shots in the same mould as -claim: they talk to
// whatever Syncthing is running, never start one, and run before the
// single-instance check.

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

// whoHas is /rest/db/whohas; see lib/api/api_whohas.go.
type whoHas struct {
	Exists     bool   `json:"exists"`
	Behind     bool   `json:"behind"`
	ModifiedBy string `json:"modifiedBy"`
	Modified   string `json:"modified"`
	Peers      []struct {
		Name      string `json:"name"`
		Connected bool   `json:"connected"`
		Has       bool   `json:"has"`
		HeldBack  bool   `json:"heldBack"`
	} `json:"peers"`
}

func (c *client) whoHas(folder, file string) (whoHas, error) {
	var out whoHas
	err := c.get("/rest/db/whohas", url.Values{"folder": {folder}, "file": {file}}, &out)
	return out, err
}

// locate is the common start of both one-shots: find the running Syncthing
// and the folder the file is in. The failure is already worded as a toast.
func locate(home string, paths []string) (cl *client, ep endpoint, f restFolder, rel string, title, body string) {
	if len(paths) == 0 {
		return nil, ep, f, "", "Nothing was chosen", "Right-click a file first."
	}
	ep, err := readEndpoint(home)
	if err != nil {
		return nil, ep, f, "", "desuqcafe Syncthing is not set up", "Start it once from the Start menu, then try again."
	}
	cl = newClient(ep)
	cfg, err := cl.config()
	if err != nil {
		return nil, ep, f, "", "desuqcafe Syncthing is not running", "Start it from the Start menu, then try again."
	}
	f, rel, ok := folderForPath(cfg.Folders, paths[0])
	if !ok {
		return nil, ep, f, "", truncate(filepath.Base(paths[0])+" is not synced", 60),
			"That file is not in a folder this computer syncs."
	}
	return cl, ep, f, rel, "", ""
}

// runHistory opens the history screen on one file. The GUI reads the two
// query parameters and opens itself there (history.js, openFromURL).
func runHistory(home string, paths []string, n notifier) int {
	_, ep, f, rel, title, body := locate(home, paths)
	if title != "" {
		n.Notify(Notification{Title: title, Body: body})
		return 1
	}
	u := ep.baseURL + "/?desuq-history=" + url.QueryEscape(f.ID) + "&file=" + url.QueryEscape(rel)
	if err := openURL(u); err != nil {
		n.Notify(Notification{Title: "Could not open the browser", Body: "Open desuqcafe Syncthing from the Start menu instead."})
		return 1
	}
	return 0
}

// runWho answers "can I open this now, and does everybody have it" in a toast.
func runWho(home string, paths []string, n notifier) int {
	cl, _, f, rel, title, body := locate(home, paths)
	if title != "" {
		n.Notify(Notification{Title: title, Body: body})
		return 1
	}
	var claims []claimRow
	if reply, err := cl.claims(); err == nil {
		for _, r := range reply.Claims {
			if r.Folder == f.ID && r.Path == rel {
				claims = append(claims, r)
			}
		}
	}
	wh, err := cl.whoHas(f.ID, rel)
	if err != nil {
		n.Notify(Notification{
			Title: truncate(filepath.Base(rel), 60),
			Body:  "Syncthing does not know this file yet, or this version cannot say. Try again in a moment.",
		})
		return 1
	}
	t, b := whoMessage(filepath.Base(rel), claims, wh, time.Now())
	n.Notify(Notification{Title: t, Body: b})
	return 0
}

// whoMessage is the pure half of runWho.
func whoMessage(name string, claims []claimRow, wh whoHas, now time.Time) (title, body string) {
	title = truncate(name, 60)
	var parts []string

	for _, c := range claims {
		who := c.Name + " is"
		if c.Mine {
			who = "You are"
		}
		parts = append(parts, who+" working on it, "+sinceWords(c.Since, now)+".")
	}
	if len(claims) == 0 {
		parts = append(parts, "Nobody has marked it as being worked on.")
	}

	if !wh.Exists {
		parts = append(parts, "It has been deleted.")
		return title, strings.Join(parts, " ")
	}
	if wh.Behind {
		parts = append(parts, "A newer version by "+wh.ModifiedBy+" is on its way to you -- wait for it before opening.")
	}

	var has, missing, offline, held []string
	for _, p := range wh.Peers {
		switch {
		case p.HeldBack:
			held = append(held, p.Name)
		case p.Has:
			has = append(has, p.Name)
		case !p.Connected:
			offline = append(offline, p.Name)
		default:
			missing = append(missing, p.Name)
		}
	}
	if !wh.Behind {
		switch {
		case len(has) > 0 && len(missing)+len(offline) == 0:
			parts = append(parts, listNames(has)+" "+hasHave(len(has))+" the same version as you.")
		case len(has) > 0:
			parts = append(parts, listNames(has)+" "+hasHave(len(has))+" your version.")
		}
		if len(missing) > 0 {
			parts = append(parts, listNames(missing)+" "+isAre(len(missing))+" still receiving it.")
		}
		if len(offline) > 0 {
			parts = append(parts, listNames(offline)+" "+isAre(len(offline))+" offline and "+
				doesDo(len(offline))+" not have your version yet.")
		}
	}
	if len(held) > 0 {
		parts = append(parts, listNames(held)+" "+isAre(len(held))+" not keeping this file.")
	}
	switch {
	case wh.ModifiedBy == "" || wh.Behind:
	case wh.ModifiedBy == "You":
		parts = append(parts, "The latest change is yours.")
	default:
		parts = append(parts, "Last changed by "+wh.ModifiedBy+".")
	}
	return title, strings.Join(parts, " ")
}

func hasHave(n int) string {
	if n == 1 {
		return "has"
	}
	return "have"
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

func doesDo(n int) string {
	if n == 1 {
		return "does"
	}
	return "do"
}

// sinceWords is "since 14:20" today, "since Monday" within the week, a date
// beyond -- the same coarse register the main screen uses.
func sinceWords(t, now time.Time) string {
	t = t.Local()
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Local().Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return "since " + t.Format("15:04")
	case now.Sub(t) < 6*24*time.Hour:
		return "since " + t.Format("Monday")
	}
	return fmt.Sprintf("since %s", t.Format("2 January"))
}
