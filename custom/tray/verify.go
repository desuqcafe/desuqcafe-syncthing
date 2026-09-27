package main

// Saying so when somebody unverified is trying to connect.
//
// Nothing connects to a device until it has been verified on this computer
// (lib/model/desuq_verified.go): the daemon lets the Hello exchange happen,
// remembers the attempt as a "knock", and refuses. Without this, the only
// sign of it anywhere is a card on the main screen -- and the moment it
// happens is exactly the moment somebody is on the phone saying "I've added
// you, can you see me?".
//
// So a knock from a device that has not knocked recently is a toast, and
// clicking it opens the main screen, where that person's card has the Verify
// button. Knocks repeat on every dial, roughly once a minute, so each device
// is announced at most once per cooldown.

import (
	"context"
	"log/slog"
	"sort"
	"time"
)

const (
	// knockCheckInterval is how often /rest/cluster/unverified is read. A
	// knock is usually somebody waiting on a call, so this is quick, and the
	// request is local and tiny.
	knockCheckInterval = 20 * time.Second

	// knockCooldown is the gap between two toasts about the same device.
	// Their computer knocks every minute or so for as long as it runs; one
	// toast per sitting is the useful amount.
	knockCooldown = 6 * time.Hour
)

// knock is one refused attempt, as /rest/cluster/unverified reports it.
type knock struct {
	Name    string    `json:"name"`
	Address string    `json:"address"`
	At      time.Time `json:"at"`
}

func (c *client) unverifiedKnocks() (map[string]knock, error) {
	var out map[string]knock
	err := c.get("/rest/cluster/unverified", nil, &out)
	return out, err
}

// freshKnocks picks the knocks worth a toast: newer than the last toast about
// that device by at least the cooldown. names maps device ID to what this
// computer calls them; the name the other side sent is the fallback. Pure, so
// the rule is testable without a daemon.
func freshKnocks(knocks map[string]knock, told map[string]time.Time, names map[string]string,
	now time.Time, cooldown time.Duration,
) []Notification {
	ids := make([]string, 0, len(knocks))
	for id := range knocks {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var out []Notification
	for _, id := range ids {
		k := knocks[id]
		if last, ok := told[id]; ok && now.Sub(last) < cooldown {
			continue
		}
		who := names[id]
		if who == "" {
			who = k.Name
		}
		if who == "" {
			who = shortDeviceID(id)
		}
		told[id] = now
		out = append(out, Notification{
			Title: who + " is trying to connect",
			Body: "Their computer is turned away until you verify them. Get them on a call " +
				"and compare cards -- click to open.",
		})
	}
	return out
}

func (a *alerter) watchKnocks(ctx context.Context) {
	told := map[string]time.Time{}
	for {
		if !sleepCtx(ctx, knockCheckInterval) {
			return
		}
		cl := a.current()
		if cl == nil {
			continue
		}
		knocks, err := cl.unverifiedKnocks()
		if err != nil {
			// An older daemon without the endpoint, or one restarting.
			slog.Debug("could not read unverified knocks", "err", err)
			continue
		}
		names := map[string]string{}
		if cfg, err := cl.config(); err == nil {
			for _, d := range cfg.Devices {
				names[d.DeviceID] = d.Name
			}
		}
		for _, n := range freshKnocks(knocks, told, names, time.Now(), knockCooldown) {
			n.Launch = a.guiURL()
			a.notify.Notify(n)
		}
	}
}
