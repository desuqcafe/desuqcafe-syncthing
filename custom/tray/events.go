package main

// The Syncthing event stream.
//
// Syncthing publishes everything interesting it does to /rest/events, a
// long-poll endpoint: ask for events after ID N, and the request blocks until
// something happens or the timeout expires. That is strictly better than the
// five-second poll the tray started with -- the icon reacts the moment a
// device connects instead of up to five seconds later, and an idle Syncthing
// costs one parked HTTP request rather than a config-plus-status round trip
// every five seconds.
//
// The poll is kept as a slow heartbeat rather than removed. Events tell you
// what changed, never what the current total is, and a missed event would
// otherwise leave the icon wrong until the next thing happened.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"time"
)

// event is one entry from /rest/events. Data stays raw because each type has
// its own shape and the tray only decodes the handful it acts on.
type event struct {
	ID   int             `json:"id"`
	Type string          `json:"type"`
	Time time.Time       `json:"time"`
	Data json.RawMessage `json:"data"`
}

// subscribedEvents is the mask asked for. Keeping it explicit rather than
// taking Syncthing's default mask matters for more than tidiness: the default
// includes LocalChangeDetected and RemoteChangeDetected, which fire once per
// file and would have the tray waking up thousands of times during a big
// initial sync.
var subscribedEvents = []string{
	// Things a person has to answer.
	"PendingDevicesChanged",
	"PendingFoldersChanged",

	// Things that change what the icon should say.
	"StateChanged",
	"FolderSummary",
	"FolderErrors",
	"FolderPaused",
	"FolderResumed",
	"DeviceConnected",
	"DeviceDisconnected",
	"DevicePaused",
	"DeviceResumed",
	"ConfigSaved",
}

// eventPollTimeout is how long Syncthing holds an idle request open. Long
// enough that an idle instance is nearly free, short enough that a dead
// connection is noticed within the minute.
const eventPollTimeout = 60 * time.Second

// events performs one long poll. The context bounds the whole request, so
// cancelling it unblocks a parked call immediately.
func (c *client) events(ctx context.Context, since int, types []string) ([]event, error) {
	q := url.Values{
		"since":   {strconv.Itoa(since)},
		"timeout": {strconv.Itoa(int(eventPollTimeout / time.Second))},
	}
	if len(types) > 0 {
		q.Set("events", joinComma(types))
	}

	var out []event
	err := c.getLongPoll(ctx, "/rest/events", q, &out)
	return out, err
}

// latestEventID returns the ID of the most recent event without blocking, so a
// fresh subscription can start from now rather than replaying a backlog.
//
// This matters: without it, connecting to a Syncthing that has been up for a
// week would raise a toast for every pending device it has ever seen.
//
// It must be asked with the same event mask the subsequent polls use. IDs are
// per-subscription, not global: Syncthing keeps one buffer per distinct mask
// and numbers each from 1. Bootstrapping against the default mask and then
// polling a filtered one -- which is what this did at first -- compares two
// unrelated counters, and silently skips every event the filtered
// subscription had already numbered below the default's.
func (c *client) latestEventID(ctx context.Context, types []string) (int, error) {
	q := url.Values{
		"since":   {"0"},
		"limit":   {"1"},
		"timeout": {"0"},
	}
	if len(types) > 0 {
		q.Set("events", joinComma(types))
	}
	var out []event
	if err := c.getLongPoll(ctx, "/rest/events", q, &out); err != nil {
		return 0, err
	}
	if len(out) == 0 {
		return 0, nil
	}
	return out[len(out)-1].ID, nil
}

func joinComma(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}

// watchEvents keeps a subscription running for as long as ctx lives, handing
// each event to handle.
//
// The outer loop exists because the subscription cannot outlive the Syncthing
// that issued it. Event IDs are per-subscription and restart at 1 when
// Syncthing does, so after any failure the only safe thing is to re-establish
// where "now" is rather than carry a stale ID across.
func watchEvents(ctx context.Context, current func() *client, handle func(event)) {
	const retry = 3 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		cl := current()
		if cl == nil {
			if !sleepCtx(ctx, retry) {
				return
			}
			continue
		}

		since, err := cl.latestEventID(ctx, subscribedEvents)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Debug("could not start the event subscription", "err", err)
			if !sleepCtx(ctx, retry) {
				return
			}
			continue
		}
		slog.Info("watching the event stream", "since", since)

		for {
			evs, err := cl.events(ctx, since, subscribedEvents)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Debug("event stream dropped; resubscribing", "err", err)
				break
			}
			for _, ev := range evs {
				// A lower ID than we asked from means Syncthing restarted and
				// began numbering again. Resubscribe rather than silently
				// ignoring everything from here on.
				if ev.ID <= since {
					slog.Info("event IDs went backwards; resubscribing",
						"since", since, "got", ev.ID)
					since = 0
					break
				}
				since = ev.ID
				handle(ev)
			}
			if since == 0 {
				break
			}
		}

		if !sleepCtx(ctx, retry) {
			return
		}
	}
}

// sleepCtx waits for d, reporting false if the context ended first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// --- payloads the tray decodes -------------------------------------------

// stateChanged is StateChanged's payload: one folder's move through the folder
// state machine.
type stateChanged struct {
	Folder string `json:"folder"`
	From   string `json:"from"`
	To     string `json:"to"`
}

// folderSummary is FolderSummary's payload. This is the one event that carries
// the numbers the tray needs, so "is this folder finished" can be answered
// without a follow-up request.
type folderSummary struct {
	Folder  string `json:"folder"`
	Summary struct {
		State     string `json:"state"`
		NeedBytes int64  `json:"needBytes"`
		NeedItems int64  `json:"needTotalItems"`
		Errors    int    `json:"errors"`
	} `json:"summary"`
}

// folderErrors is FolderErrors' payload.
type folderErrors struct {
	Folder string `json:"folder"`
	Errors []struct {
		Path  string `json:"path"`
		Error string `json:"error"`
	} `json:"errors"`
}

func decodeEvent[T any](ev event) (T, error) {
	var out T
	if err := json.Unmarshal(ev.Data, &out); err != nil {
		return out, fmt.Errorf("decode %s: %w", ev.Type, err)
	}
	return out, nil
}
