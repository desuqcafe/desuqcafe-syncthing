// Command desuq-syncthing-tray puts desuqcafe Syncthing in the Windows
// notification area.
//
// Syncthing has no tray icon and no service mode, so once it is started from
// the sign-in shortcut there is nothing on screen to say whether it is running,
// stuck, or was closed by mistake. For two non-technical modellers that is the
// difference between "my files are syncing" and "my files stopped syncing three
// days ago and nobody noticed".
//
// This starts Syncthing, keeps it running, and shows its state as an icon with
// Open / Pause / Quit. It talks to Syncthing only over its REST API, so it
// stays a separate module and needs no change to upstream source.
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"
)

const appName = "desuqcafe Syncthing"

type options struct {
	home     string
	binary   string
	attach   bool
	open     bool
	interval time.Duration
}

// app owns everything the systray callbacks touch. systray delivers clicks on
// its own goroutines, so every field here is either immutable after start-up or
// guarded by mu.
type app struct {
	opts options
	sup  *supervisor

	mu     sync.Mutex
	cl     *client
	selfID string
	last   Status

	refresh chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc

	mStatus  *systray.MenuItem
	mOpen    *systray.MenuItem
	mPause   *systray.MenuItem
	mQuit    *systray.MenuItem
	quitOnce sync.Once
	openOnce sync.Once
}

func main() {
	exeDir := "."
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}

	var opts options
	flag.StringVar(&opts.home, "home", defaultHome(),
		"Syncthing configuration and database directory")
	flag.StringVar(&opts.binary, "binary", filepath.Join(exeDir, "desuq-syncthing.exe"),
		"Path to the Syncthing binary to supervise")
	flag.BoolVar(&opts.attach, "attach", false,
		"Do not start or stop Syncthing; only watch an instance that is already running")
	flag.BoolVar(&opts.open, "open", false,
		"Open the web interface once Syncthing is up. Used by the installer, not by the sign-in shortcut")
	flag.DurationVar(&opts.interval, "interval", 5*time.Second,
		"How often to poll Syncthing for its status")
	flag.Parse()

	// Log to the Syncthing home directory, where the support bundle and
	// everything else already lives. This binary is linked -H windowsgui, so
	// it has no stderr to fall back on: without a file there is no way at all
	// to find out why the icon did not appear.
	if f, err := os.OpenFile(filepath.Join(opts.home, "tray.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		defer f.Close()
		slog.SetDefault(slog.New(slog.NewTextHandler(f, nil)))
		// systray reports its own failures -- registering the icon, loading it
		// -- through the standard log package. Route that to the same file
		// rather than into the void.
		log.SetOutput(f)
		log.SetPrefix("systray: ")
	}

	a := &app{opts: opts, refresh: make(chan struct{}, 1)}
	a.ctx, a.cancel = context.WithCancel(context.Background())

	if !opts.attach {
		a.sup = newSupervisor(opts.binary, opts.home, a.connected)
		go a.sup.Run(a.ctx)
	}

	systray.Run(a.onReady, a.onExit)
}

func defaultHome() string {
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		return filepath.Join(dir, "desuqcafe-syncthing")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "desuqcafe-syncthing")
}

func (a *app) onReady() {
	systray.SetIcon(iconOffline)
	systray.SetTooltip(appName + "\nStarting...")

	a.mStatus = systray.AddMenuItem("Starting...", "Current status")
	a.mStatus.Disable()
	systray.AddSeparator()
	a.mOpen = systray.AddMenuItem("Open "+appName, "Open the web interface in your browser")
	a.mPause = systray.AddMenuItemCheckbox("Pause Syncing", "Stop syncing with all other devices", false)
	systray.AddSeparator()

	quitLabel := "Quit " + appName
	quitTip := "Stop Syncthing and close this icon"
	if a.opts.attach {
		quitLabel = "Close Tray Icon"
		quitTip = "Close this icon and leave Syncthing running"
	}
	a.mQuit = systray.AddMenuItem(quitLabel, quitTip)

	go a.watchClicks()
	go a.pollLoop()
}

func (a *app) onExit() {
	a.cancel()
}

func (a *app) watchClicks() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-a.mOpen.ClickedCh:
			a.openGUI()
		case <-a.mPause.ClickedCh:
			a.togglePause()
		case <-a.mQuit.ClickedCh:
			a.quit()
			return
		}
	}
}

func (a *app) openGUI() {
	a.mu.Lock()
	cl := a.cl
	a.mu.Unlock()
	if cl == nil {
		slog.Warn("open requested before Syncthing was reachable")
		return
	}
	if err := openURL(cl.ep.baseURL); err != nil {
		slog.Error("could not open the browser", "err", err)
	}
}

func (a *app) togglePause() {
	a.mu.Lock()
	cl, paused := a.cl, a.last.Paused
	a.mu.Unlock()
	if cl == nil {
		return
	}

	var err error
	if paused {
		err = cl.resumeAll()
	} else {
		err = cl.pauseAll()
	}
	if err != nil {
		slog.Error("could not change pause state", "err", err, "wasPaused", paused)
		return
	}
	a.pokeRefresh()
}

func (a *app) quit() {
	a.quitOnce.Do(func() {
		a.mu.Lock()
		cl := a.cl
		a.mu.Unlock()

		if a.sup != nil {
			a.mStatus.SetTitle("Stopping...")
			a.sup.Stop(cl)
		}
		a.cancel()
		systray.Quit()
	})
}

func (a *app) pokeRefresh() {
	select {
	case a.refresh <- struct{}{}:
	default:
	}
}

// pollLoop is the only place the status is read, so the menu and the icon can
// never disagree about what state they are showing.
func (a *app) pollLoop() {
	ticker := time.NewTicker(a.opts.interval)
	defer ticker.Stop()

	// Until Syncthing has come up and written its port, poll faster; a first
	// start with key generation takes a couple of seconds.
	fast := time.NewTicker(time.Second)
	defer fast.Stop()

	for {
		if a.connected() {
			a.apply(a.readStatus())
		} else {
			a.apply(Status{
				State:    StateOffline,
				Headline: "Starting...",
				Detail:   "Waiting for Syncthing",
			})
		}

		var tick <-chan time.Time
		if a.connected() {
			tick = ticker.C
		} else {
			tick = fast.C
		}

		select {
		case <-a.ctx.Done():
			return
		case <-tick:
		case <-a.refresh:
		}
	}
}

// connected resolves the API endpoint if it has not been resolved yet. The
// address and key are re-read from config.xml rather than cached across
// failures, because Syncthing rewrites the GUI address when its preferred port
// turns out to be taken.
func (a *app) connected() bool {
	a.mu.Lock()
	cl := a.cl
	a.mu.Unlock()

	if cl != nil {
		if err := cl.ping(); err == nil {
			return true
		}
		a.mu.Lock()
		a.cl = nil
		a.mu.Unlock()
	}

	ep, err := readEndpoint(a.opts.home)
	if err != nil {
		return false
	}
	cl = newClient(ep)
	if err := cl.ping(); err != nil {
		return false
	}

	var status struct {
		MyID string `json:"myID"`
	}
	_ = cl.get("/rest/system/status", nil, &status)

	a.mu.Lock()
	a.cl, a.selfID = cl, status.MyID
	a.mu.Unlock()
	slog.Info("connected to Syncthing", "url", ep.baseURL)

	// Right after installation there is nothing to show for a successful
	// install but a new icon, so open the web interface once. Deliberately
	// only on the first connection, and never from the sign-in shortcut.
	a.openOnce.Do(func() {
		if a.opts.open {
			a.openGUI()
		}
	})
	return true
}

func (a *app) readStatus() Status {
	a.mu.Lock()
	cl, selfID := a.cl, a.selfID
	a.mu.Unlock()
	if cl == nil {
		return Status{State: StateOffline, Headline: "Not running"}
	}
	return poll(cl, selfID)
}

func (a *app) apply(s Status) {
	a.mu.Lock()
	previous := a.last
	a.last = s
	a.mu.Unlock()

	// systray redraws on every call, so only touch it when something changed.
	if previous == s {
		return
	}

	if previous.State != s.State {
		systray.SetIcon(iconFor(s.State))
	}
	systray.SetTooltip(tooltip(appName, s))

	title := s.Headline
	if s.Detail != "" && !strings.EqualFold(s.Detail, s.Headline) {
		title += " - " + s.Detail
	}
	a.mStatus.SetTitle(title)

	if s.State == StateOffline {
		a.mOpen.Disable()
		a.mPause.Disable()
	} else {
		a.mOpen.Enable()
		a.mPause.Enable()
	}

	if s.Paused {
		a.mPause.Check()
		a.mPause.SetTitle("Resume Syncing")
	} else {
		a.mPause.Uncheck()
		a.mPause.SetTitle("Pause Syncing")
	}
}
