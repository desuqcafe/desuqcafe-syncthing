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

// toastAppID is the AppUserModelID Windows attributes notifications to. It is
// only ever compared as a string, but the convention is CompanyName.AppName so
// that it cannot collide with another program's.
const toastAppID = "desuqcafe.Syncthing.Tray"

type options struct {
	home     string
	binary   string
	attach   bool
	open     bool
	quiet    bool
	noIcons  bool
	clear    bool
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

	notify notifier
	alerts *alerter
	marker *folderMarker

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
	// The event stream, not this ticker, is what makes the icon react. This is
	// only the safety net for an event that was missed or never fires, so it
	// is deliberately slow: polling every five seconds was the old design.
	flag.DurationVar(&opts.interval, "interval", 30*time.Second,
		"How often to re-read Syncthing's status as a fallback to the event stream")
	flag.BoolVar(&opts.quiet, "quiet", false,
		"Do not show desktop notifications")
	flag.BoolVar(&opts.noIcons, "no-folder-icons", false,
		"Do not give synced folders a custom icon in Explorer")
	flag.BoolVar(&opts.clear, "clear-folder-icons", false,
		"Remove the Explorer folder icons this has written, then exit. Works with Syncthing stopped")
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

	// A one-shot that has to work on a machine being taken apart, so it runs
	// before anything is started and reads the folder list off disk rather
	// than asking a Syncthing that is no longer there. The exit code is the
	// only signal it can give: this binary is linked -H windowsgui and has no
	// stdout to write to.
	if opts.clear {
		n, err := newFolderMarker(opts.home).clear()
		if err != nil {
			slog.Error("could not clear the folder icons", "err", err)
			os.Exit(1)
		}
		slog.Info("cleared Explorer folder icons", "folders", n)
		return
	}

	a := &app{opts: opts, refresh: make(chan struct{}, 1)}
	a.ctx, a.cancel = context.WithCancel(context.Background())

	a.notify = nopNotifier{}
	if !opts.quiet {
		a.notify = newNotifier(toastAppID, appName)
	}
	a.alerts = newAlerter(a.notify, a.guiURL, a.client)
	a.marker = newFolderMarker(opts.home)

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
	// The event stream both feeds the notifications and wakes the status
	// refresh, so the icon changes when Syncthing does rather than up to a
	// poll interval later.
	go watchEvents(a.ctx, a.client, a.onEvent)
	go a.alerts.watchDisk(a.ctx)
	go a.alerts.watchVersions(a.ctx)
	if !a.opts.noIcons {
		go a.watchFolders(a.ctx)
	}
}

// folderMarkInterval is how often the Explorer folder markers are reconciled.
// Nothing here is urgent -- a folder added a minute ago getting its icon a
// minute later is fine -- and each pass that finds nothing new is one local
// HTTP request, so there is no reason to be quicker about it.
const folderMarkInterval = 2 * time.Minute

// watchFolders keeps every synced folder marked with the fork's icon, so a
// folder that is added later is not left looking like any other folder. Doing
// it on a timer rather than once at start-up is also what covers a folder
// whose drive was not plugged in yet. See foldericon.go.
func (a *app) watchFolders(ctx context.Context) {
	// A short wait first: on a cold start Syncthing is not up yet, and a
	// folder it has not created on disk cannot be marked.
	if !sleepCtx(ctx, 15*time.Second) {
		return
	}
	for {
		a.marker.reconcile(a.client())
		if !sleepCtx(ctx, folderMarkInterval) {
			return
		}
	}
}

func (a *app) onExit() {
	a.cancel()
	a.notify.Close()
}

// client returns the current API client, or nil if Syncthing is not reachable.
// Everything that talks to Syncthing off the poll loop goes through this rather
// than holding a client, because the client is replaced whenever Syncthing
// restarts on a different port.
func (a *app) client() *client {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cl
}

func (a *app) guiURL() string {
	if cl := a.client(); cl != nil {
		return cl.ep.baseURL
	}
	return ""
}

// onEvent handles one event from the stream: it updates the icon promptly, and
// hands the event to the notification policy.
func (a *app) onEvent(ev event) {
	a.alerts.handle(ev)
	a.pokeRefresh()
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
//
// It is woken by the event stream rather than driving the pace itself. The
// ticker is the fallback for a change no event describes, which is why it is
// slow: making it fast again would defeat the point of subscribing.
func (a *app) pollLoop() {
	ticker := time.NewTicker(a.opts.interval)
	defer ticker.Stop()

	// Until Syncthing has come up and written its port, poll faster; a first
	// start with key generation takes a couple of seconds.
	fast := time.NewTicker(time.Second)
	defer fast.Stop()

	var lastPoll time.Time
	for {
		up := a.connected()
		if up {
			a.apply(a.readStatus())
		} else {
			a.apply(Status{
				State:    StateOffline,
				Headline: "Starting...",
				Detail:   "Waiting for Syncthing",
			})
		}
		lastPoll = time.Now()

		tick := fast.C
		if up {
			tick = ticker.C
		}

		select {
		case <-a.ctx.Done():
			return
		case <-tick:
		case <-a.refresh:
			// Events arrive in bursts -- a folder syncing publishes a summary
			// every few seconds, and each one would otherwise cost a full
			// config-plus-status read. Hold off briefly so a burst costs one
			// refresh rather than one per event.
			if wait := minRefreshGap - time.Since(lastPoll); wait > 0 {
				if !sleepCtx(a.ctx, wait) {
					return
				}
				// Anything that arrived while waiting is already covered by
				// the poll about to happen.
				select {
				case <-a.refresh:
				default:
				}
			}
		}
	}
}

// minRefreshGap is the shortest time between two event-driven status reads.
const minRefreshGap = 2 * time.Second

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
