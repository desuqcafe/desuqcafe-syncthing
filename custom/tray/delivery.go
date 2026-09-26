package main

// "cabin.blend is now on Kai's computer": saying when your work has
// actually arrived.
//
// Saving a file here and having it on the other person's machine are
// separated by however long they are offline, plus the transfer. For the
// people this build is for, that gap is where "did you get my file?" phone
// calls come from, and where somebody switches off for the night with the
// only copy of the day's work still on their own disk.
//
// The main screen says it while you look (home.js, "Sending cabin.blend to
// Kai" / "Kai does not have your latest yet"). This is the other half:
// a toast when a delivery that has been *waiting* completes. Waiting is the
// point -- a save that reaches a connected peer in ten seconds is the
// ordinary case and announcing it would be a toast per save. So a delivery is
// only announced once this computer's changes have been missing on the other
// side for at least deliveryMinWait: a big file over a slow link, or a peer
// who was away and has come back.
//
// The facts come from /rest/db/delivery (lib/api/api_delivery.go), which
// reads the peer's remote need from their index, so an offline peer is
// counted -- that is the case this is mostly for.
//
// Pausing is the other moment it matters: see pausedDeliveryMessage.

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// deliveryFirstCheck and deliveryInterval pace the poll. A minute is fine:
	// nothing is announced until a delivery has waited longer than that.
	deliveryFirstCheck = 45 * time.Second
	deliveryInterval   = time.Minute

	// deliveryMinWait is how long your changes must have been missing on the
	// other side before their arrival is news.
	deliveryMinWait = 2 * time.Minute

	deliveryMaxNames = 2
)

type deliveryReply struct {
	Folder string         `json:"folder"`
	Peers  []deliveryPeer `json:"peers"`
}

type deliveryPeer struct {
	Device    string `json:"device"`
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	Yours     struct {
		Files int   `json:"files"`
		Bytes int64 `json:"bytes"`
	} `json:"yours"`
	Names []string `json:"names"`
}

func (c *client) delivery(folder string) (deliveryReply, error) {
	var out deliveryReply
	err := c.get("/rest/db/delivery", url.Values{"folder": {folder}}, &out)
	return out, err
}

// pendingDelivery is one peer still missing some of this computer's changes
// in one folder, since when, and the most files it has been missing -- the
// toast names the delivery as a whole, not what was left in the last minute.
type pendingDelivery struct {
	since  time.Time
	device string
	name   string
	folder string
	files  int
	names  []string
}

// deliveryTracker is the state machine on its own, so it can be tested
// without a server or a clock.
type deliveryTracker struct {
	pending map[string]*pendingDelivery // folder + " " + device
}

// observe takes one poll's answers for every folder and returns the
// deliveries that have completed after waiting at least deliveryMinWait.
// A pending entry nobody reported this round -- a folder unshared, a device
// removed -- is dropped without an announcement.
func (t *deliveryTracker) observe(now time.Time, replies []deliveryReply) []pendingDelivery {
	if t.pending == nil {
		t.pending = map[string]*pendingDelivery{}
	}
	seen := map[string]bool{}
	var done []pendingDelivery
	for _, r := range replies {
		for _, p := range r.Peers {
			key := r.Folder + " " + p.Device
			seen[key] = true
			rec := t.pending[key]
			if p.Yours.Files > 0 {
				if rec == nil {
					rec = &pendingDelivery{since: now, device: p.Device, folder: r.Folder}
					t.pending[key] = rec
				}
				rec.name = p.Name
				if p.Yours.Files >= rec.files {
					rec.files = p.Yours.Files
					if len(p.Names) > 0 {
						rec.names = append([]string(nil), p.Names...)
					}
				}
				continue
			}
			if rec != nil {
				if now.Sub(rec.since) >= deliveryMinWait {
					done = append(done, *rec)
				}
				delete(t.pending, key)
			}
		}
	}
	for key := range t.pending {
		if !seen[key] {
			delete(t.pending, key)
		}
	}
	return done
}

// deliveredMessage is the toast for one peer's completed deliveries, across
// however many folders.
func deliveredMessage(name string, done []pendingDelivery) (title, body string) {
	files := 0
	var names []string
	for _, d := range done {
		files += d.files
		for _, n := range d.names {
			names = append(names, filepath.Base(filepath.FromSlash(strings.ReplaceAll(n, `\`, "/"))))
		}
	}
	if files == 0 {
		return "", ""
	}
	title = truncate("Now on "+name+"'s computer", 60)
	what := fmt.Sprintf("%d of your files", files)
	switch {
	case len(names) == 0:
	case files == 1:
		what = names[0]
	case files <= deliveryMaxNames && len(names) >= files:
		what = listNames(names[:files])
	default:
		what = fmt.Sprintf("%s and %d more of your files", names[0], files-1)
	}
	verb := "has"
	if files > 1 {
		verb = "have"
	}
	return title, fmt.Sprintf("%s %s reached %s.", what, verb, name)
}

// watchDelivery is the poll. See the file comment.
func (a *alerter) watchDelivery(ctx context.Context) {
	if !sleepCtx(ctx, deliveryFirstCheck) {
		return
	}
	for {
		a.checkDelivery()
		if !sleepCtx(ctx, deliveryInterval) {
			return
		}
	}
}

func (a *alerter) checkDelivery() {
	cl := a.current()
	if cl == nil {
		return
	}
	replies, err := allDeliveries(cl)
	if err != nil {
		slog.Debug("could not read delivery", "err", err)
		return
	}
	// A reunion briefing is being prepared: it is the one toast about that
	// reunion. Not observing this round keeps everything pending, so the
	// delivery is announced on the first poll after the briefing is said.
	if a.briefingActive() {
		return
	}
	a.mu.Lock()
	done := a.deliveries.observe(time.Now(), replies)
	a.mu.Unlock()

	byDevice := map[string][]pendingDelivery{}
	for _, d := range done {
		byDevice[d.device] = append(byDevice[d.device], d)
	}
	devices := make([]string, 0, len(byDevice))
	for dev := range byDevice {
		devices = append(devices, dev)
	}
	sort.Strings(devices)
	for _, dev := range devices {
		ds := byDevice[dev]
		title, body := deliveredMessage(ds[0].name, ds)
		if title == "" {
			continue
		}
		slog.Info("delivery completed after waiting", "device", ds[0].name, "folders", len(ds))
		a.notify.Notify(Notification{Title: title, Body: body, Launch: a.guiURL()})
	}
}

// allDeliveries asks for every folder that is running and shared with
// somebody.
func allDeliveries(cl *client) ([]deliveryReply, error) {
	cfg, err := cl.config()
	if err != nil {
		return nil, err
	}
	var out []deliveryReply
	for _, f := range cfg.Folders {
		if f.Paused || len(f.Devices) < 2 {
			continue
		}
		r, err := cl.delivery(f.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// pausedDeliveryMessage is said when syncing is paused while somebody is
// still missing this computer's changes: from now until resume, those
// changes exist only here. Nil when everybody has everything.
func pausedDeliveryMessage(replies []deliveryReply) string {
	type who struct {
		name  string
		files int
		first string
	}
	var list []who
	idx := map[string]int{}
	for _, r := range replies {
		for _, p := range r.Peers {
			if p.Yours.Files == 0 {
				continue
			}
			i, ok := idx[p.Device]
			if !ok {
				i = len(list)
				idx[p.Device] = i
				list = append(list, who{name: p.Name})
			}
			list[i].files += p.Yours.Files
			if list[i].first == "" && len(p.Names) > 0 {
				list[i].first = filepath.Base(filepath.FromSlash(strings.ReplaceAll(p.Names[0], `\`, "/")))
			}
		}
	}
	if len(list) == 0 {
		return ""
	}
	sort.Slice(list, func(i, j int) bool { return list[i].name < list[j].name })
	var parts []string
	for _, w := range list {
		what := fmt.Sprintf("%d of your files", w.files)
		if w.files == 1 && w.first != "" {
			what = "your latest " + w.first
		} else if w.first != "" {
			what = fmt.Sprintf("%s and %d more of your files", w.first, w.files-1)
		}
		parts = append(parts, w.name+" does not have "+what+" yet")
	}
	return strings.Join(parts, "; ") + ". Pausing keeps it from reaching them until you resume."
}
