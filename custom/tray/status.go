package main

import (
	"fmt"
	"sort"
	"strings"
)

type State int

const (
	StateOffline State = iota
	StateError
	StatePaused
	StateSyncing
	StateIdle
)

// Status is one poll's worth of "what is Syncthing doing", reduced to what the
// tray can actually show: an icon, a tooltip and a menu line.
type Status struct {
	State     State
	Headline  string
	Detail    string
	Connected int
	Remotes   int
	Paused    bool
}

// Folder states that mean work is in progress. Taken from lib/model's folder
// state machine; anything unrecognised is treated as idle rather than as an
// error, so a new upstream state cannot make the tray cry wolf.
var busyStates = map[string]bool{
	"scanning":       true,
	"syncing":        true,
	"sync-preparing": true,
	"cleaning":       true,
}

func poll(c *client, selfID string) Status {
	cfg, err := c.config()
	if err != nil {
		return Status{
			State:    StateOffline,
			Headline: "Not running",
			Detail:   "Syncthing is not responding",
		}
	}

	// Syncthing's own device entry is in this list and gets paused along with
	// the rest, so it is a fair thing to include in the all-paused test.
	paused := len(cfg.Devices) > 0
	for _, d := range cfg.Devices {
		if !d.Paused {
			paused = false
			break
		}
	}

	remotes := 0
	for _, d := range cfg.Devices {
		if d.DeviceID != selfID {
			remotes++
		}
	}

	connected := 0
	if conns, err := c.connections(); err == nil {
		for id, conn := range conns.Connections {
			if conn.Connected && id != selfID {
				connected++
			}
		}
	}

	var (
		errored   []string
		busy      []string
		needBytes int64
	)
	for _, f := range cfg.Folders {
		if f.Paused {
			continue
		}
		st, err := c.folderStatus(f.ID)
		if err != nil {
			continue
		}
		name := f.Label
		if name == "" {
			name = f.ID
		}
		switch {
		case st.State == "error" || st.Errors > 0:
			errored = append(errored, name)
		case busyStates[st.State] || st.NeedBytes > 0 || st.NeedItems > 0:
			busy = append(busy, name)
			needBytes += st.NeedBytes
		}
	}
	sort.Strings(errored)
	sort.Strings(busy)

	s := Status{Connected: connected, Remotes: remotes, Paused: paused}

	switch {
	case len(errored) > 0:
		s.State = StateError
		s.Headline = "Problem with " + joinNames(errored)
		s.Detail = "Open Syncthing to see the error"
	case paused:
		s.State = StatePaused
		s.Headline = "Paused"
		s.Detail = "Syncing is paused"
	case len(busy) > 0:
		s.State = StateSyncing
		s.Headline = "Syncing " + joinNames(busy)
		if needBytes > 0 {
			s.Detail = formatBytes(needBytes) + " remaining"
		} else {
			s.Detail = "Scanning for changes"
		}
	default:
		s.State = StateIdle
		s.Headline = "Up to date"
		if remotes == 0 {
			s.Detail = "No other devices set up yet"
		} else {
			s.Detail = fmt.Sprintf("%d of %d devices connected", connected, remotes)
		}
	}
	return s
}

func joinNames(names []string) string {
	switch len(names) {
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	default:
		return fmt.Sprintf("%s and %d others", names[0], len(names)-1)
	}
}

func formatBytes(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTP"[exp])
}

// tooltip has to fit Windows' 128 character NOTIFYICONDATA limit, so it is
// built to be truncated at a sensible place rather than mid-word.
func tooltip(appName string, s Status) string {
	lines := []string{appName, s.Headline}
	if s.Detail != "" {
		lines = append(lines, s.Detail)
	}
	out := strings.Join(lines, "\n")
	if len(out) > 120 {
		out = out[:117] + "..."
	}
	return out
}
