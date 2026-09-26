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
	stop     bool
	claim    bool
	history  bool
	who      bool
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
	// markNow asks the folder marker for an early pass, so Explorer's hover
	// text follows the claims within seconds rather than minutes.
	markNow chan struct{}

	refresh chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc

	mStatus       *systray.MenuItem
	mOpen         *systray.MenuItem
	mPause        *systray.MenuItem
	mPauseFor     *systray.MenuItem
	mPauseChoices []*systray.MenuItem
	// hold is the timed pause. See pausefor.go.
	hold     pauseHold
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
	flag.BoolVar(&opts.stop, "shutdown", false,
		"Ask a running Syncthing to stop cleanly, wait for it to go, then exit. Used by the installer")
	flag.BoolVar(&opts.claim, "claim", false,
		"Mark the files named after the flags as being worked on here, or unmark them if they already are, then exit. Used by Send To")
	flag.BoolVar(&opts.history, "history", false,
		"Open the history screen on the file named after the flags, then exit. Used by the .blend right-click menu")
	flag.BoolVar(&opts.who, "who", false,
		"Say who is working on the file named after the flags and who has it, then exit. Used by the .blend right-click menu")
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

	// The other one-shot: stop a running Syncthing the way Quit does, over the
	// API, so the database closes rather than being taken away mid-write. The
	// installer calls this before replacing the binary. See installer.iss.
	if opts.stop {
		os.Exit(shutdownRunning(opts.home))
	}

	// The Send To one-shot (claims.go). Explorer appends the selected files
	// after the shortcut's own arguments, so they arrive as flag.Args(). It
	// talks to whatever Syncthing is running and never starts one, and like
	// the two above it runs before the single-instance check: it is how a
	// file gets marked while the tray is already up.
	//
	// -history and -who are the same shape, from the .blend right-click menu
	// (explorer.go).
	if opts.claim || opts.history || opts.who {
		var n notifier = nopNotifier{}
		if !opts.quiet {
			n = newNotifier(toastAppID, appName)
		}
		var code int
		switch {
		case opts.claim:
			code = runClaim(opts.home, flag.Args(), n)
		case opts.history:
			code = runHistory(opts.home, flag.Args(), n)
		default:
			code = runWho(opts.home, flag.Args(), n)
		}
		// The toast is handed to the shell before Notify returns, but give
		// it a moment before the process that raised it goes.
		time.Sleep(time.Second)
		n.Close()
		os.Exit(code)
	}

	// Past here the process is long-lived and puts an icon on screen, so it
	// has to be the only one for this home. Both one-shots above deliberately
	// run before this: they are over in a moment, do not draw anything, and
	// have to work while a tray is running.
	if !claimInstance(opts.home) {
		slog.Info("another tray is already running for this home; not starting a second")
		// Clicking the Start Menu shortcut while the tray is running should
		// still do the thing the click asked for. The running tray cannot be
		// signalled without inventing an IPC channel for one message, and it
		// does not need to be: opening a URL is something any process can do.
		if opts.open {
			openExistingGUI(opts.home)
		}
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
	a.markNow = make(chan struct{}, 1)
	a.alerts.deleted = loadDeletedMemo(opts.home)
	a.alerts.claimsChanged = func() {
		select {
		case a.markNow <- struct{}{}:
		default:
		}
	}

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

// shutdownStopTimeout is how long -shutdown waits for Syncthing to actually
// exit. A folder mid-scan can take a few seconds to put the database down;
// waiting is the entire point of the flag, and the installer's force-kill is
// still there for anything that outlasts this.
const shutdownStopTimeout = 30 * time.Second

// shutdownRunning implements -shutdown and returns a process exit code, which
// is the only channel this binary has: it is linked -H windowsgui and has no
// stdout to report on.
//
// Nothing running is success, not failure. The installer calls this on every
// install including the first, where there is no config.xml at all.
func shutdownRunning(home string) int {
	ep, err := readEndpoint(home)
	if err != nil {
		slog.Info("no Syncthing configuration to shut down", "home", home, "err", err)
		return 0
	}

	cl := newClient(ep)
	if err := cl.ping(); err != nil {
		slog.Info("nothing is answering; no shutdown needed", "url", ep.baseURL)
		return 0
	}

	if err := cl.shutdown(); err != nil {
		slog.Warn("shutdown request failed", "err", err, "url", ep.baseURL)
		return 1
	}

	// Poll rather than trust the 200: the request is answered before the
	// database is closed, and the installer's next move is to overwrite the
	// binary that is still holding it.
	deadline := time.Now().Add(shutdownStopTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
		if err := cl.ping(); err != nil {
			slog.Info("Syncthing stopped cleanly")
			return 0
		}
	}
	slog.Warn("Syncthing did not stop within the timeout", "waited", shutdownStopTimeout)
	return 1
}

// openExistingGUI opens the web interface of a Syncthing this process did not
// start, for the case where a second tray was launched and is standing down.
//
// It reads the address out of config.xml rather than asking the running tray,
// because that is where the running tray got it too.
func openExistingGUI(home string) {
	ep, err := readEndpoint(home)
	if err != nil {
		slog.Warn("could not find the web interface address", "home", home, "err", err)
		return
	}
	if err := openURL(ep.baseURL); err != nil {
		slog.Error("could not open the browser", "err", err, "url", ep.baseURL)
	}
}

func (a *app) onReady() {
	systray.SetIcon(iconOffline)
	systray.SetTooltip(appName + "\nStarting...")

	a.mStatus = systray.AddMenuItem("Starting...", "Current status")
	a.mStatus.Disable()
	systray.AddSeparator()
	a.mOpen = systray.AddMenuItem("Open "+appName, "Open the web interface in your browser")
	a.mPause = systray.AddMenuItemCheckbox("Pause Syncing", "Stop syncing with all other devices", false)
	// A pause with an end, beside the one without. See pausefor.go for why
	// the indefinite one is not simply replaced.
	a.mPauseFor = systray.AddMenuItem("Pause for a while", "Stop syncing, and start again by itself")
	for _, c := range pauseChoices {
		a.mPauseChoices = append(a.mPauseChoices, a.mPauseFor.AddSubMenuItem(c.label, c.tip))
	}
	systray.AddSeparator()

	quitLabel := "Quit " + appName
	quitTip := "Stop Syncthing and close this icon"
	if a.opts.attach {
		quitLabel = "Close Tray Icon"
		quitTip = "Close this icon and leave Syncthing running"
	}
	a.mQuit = systray.AddMenuItem(quitLabel, quitTip)

	go a.watchClicks()
	for i, item := range a.mPauseChoices {
		// One goroutine per entry rather than a select over a slice: the set
		// is fixed at two and this is the whole of the plumbing.
		go a.watchPauseChoice(item, pauseChoices[i].d)
	}
	go a.pollLoop()
	// The event stream both feeds the notifications and wakes the status
	// refresh, so the icon changes when Syncthing does rather than up to a
	// poll interval later.
	go watchEvents(a.ctx, a.client, a.onEvent)
	go a.alerts.watchDisk(a.ctx)
	go a.alerts.watchVersions(a.ctx)
	go a.alerts.watchConflicts(a.ctx)
	go a.alerts.watchStale(a.ctx)
	go a.alerts.watchClaims(a.ctx)
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
		// A claims pass runs every twenty seconds and pokes this after each
		// one; reconcile only writes a desktop.ini whose text has changed, so
		// being asked often costs a few local requests and no disk.
		t := time.NewTimer(folderMarkInterval)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-a.markNow:
			t.Stop()
		case <-t.C:
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

	// Pressing the button is a decision that outranks a timer set earlier --
	// both for Resume, which ends the hold, and for Pause, which turns a timed
	// pause into an indefinite one.
	a.hold.cancel()

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
	if !paused {
		go a.warnHeldClaims(cl)
	}
}

// warnHeldClaims says so when syncing is paused while this computer has files
// marked. The mark stays up for everybody else, and taking it off while
// paused reaches nobody: the claims file changes here and goes nowhere. Said
// after the pause rather than asked before it, because a tray menu has no
// way to ask anything -- and the pause is still the right call more often
// than not; this is so the person knows what the others are seeing.
func (a *app) warnHeldClaims(cl *client) {
	reply, err := cl.claims()
	if err != nil {
		return
	}
	if title, body := pausedClaimsMessage(reply.Claims); title != "" {
		a.notify.Notify(Notification{Title: title, Body: body, Launch: a.guiURL()})
	}
}

// watchPauseChoice waits on one entry of the "Pause for a while" submenu.
func (a *app) watchPauseChoice(item *systray.MenuItem, d time.Duration) {
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-item.ClickedCh:
			a.pauseFor(d)
		}
	}
}

// pauseFor pauses and arms the timer that will lift it.
//
// The hold is armed *before* the pause is asked for and dropped again if that
// fails, so there is no window in which the menu claims a deadline that
// nothing is going to honour.
func (a *app) pauseFor(d time.Duration) {
	a.mu.Lock()
	cl := a.cl
	a.mu.Unlock()
	if cl == nil {
		return
	}

	a.hold.arm(d, a.expirePause)
	if err := cl.pauseAll(); err != nil {
		a.hold.cancel()
		slog.Error("could not pause", "err", err, "for", d.String())
		return
	}
	a.mPause.SetTitle(pauseHoldTitle(a.hold.deadline()))
	a.pokeRefresh()
	slog.Info("syncing paused", "until", a.hold.deadline().Format(time.Kitchen))
	go a.warnHeldClaims(cl)
}


// expirePause runs when the hold's timer fires.
func (a *app) expirePause() {
	a.mu.Lock()
	cl := a.cl
	a.mu.Unlock()
	if cl == nil {
		return
	}
	if err := cl.resumeAll(); err != nil {
		slog.Error("could not resume after a timed pause", "err", err)
		return
	}
	a.mPause.SetTitle(pauseHoldTitle(time.Time{}))
	a.pokeRefresh()
	// Worth a toast: the point of the feature is that syncing comes back, and
	// something that happens silently an hour later is something nobody can
	// tell happened at all.
	a.notify.Notify(Notification{
		Title:  "Syncing has started again",
		Body:   "The pause you set has ended.",
		Launch: a.guiURL(),
	})
}

func (a *app) quit() {
	a.quitOnce.Do(func() {
		a.mu.Lock()
		cl := a.cl
		a.mu.Unlock()

		// A timed pause must not outlive the tray that promised to lift it.
		// In the normal case the daemon is about to be stopped anyway and the
		// pause would come back with it at the next sign-in; under -attach the
		// daemon stays up and would sit paused for ever. Both are fixed here.
		if a.hold.cancel() && cl != nil {
			if err := cl.resumeAll(); err != nil {
				slog.Error("could not lift the timed pause on the way out", "err", err)
			}
		}

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
		a.mPauseFor.Disable()
	} else {
		a.mOpen.Enable()
		a.mPause.Enable()
	}

	if s.Paused {
		a.mPause.Check()
		// The deadline comes from the hold rather than from Status, which is
		// what Syncthing knows and Syncthing does not know about deadlines.
		a.mPause.SetTitle(pauseHoldTitle(a.hold.deadline()))
		a.mPauseFor.Disable()
	} else {
		a.mPause.Uncheck()
		a.mPause.SetTitle("Pause Syncing")
		// Not simply Enable: the offline branch above has already disabled
		// this, and an unpaused *offline* instance is exactly the state where
		// it must stay that way.
		if s.State != StateOffline {
			a.mPauseFor.Enable()
		}
	}
}
