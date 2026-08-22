package main

// Desktop notifications.
//
// Syncthing has a rich event stream but no way at all to reach the user with
// it: unless the web GUI happens to be open, a device asking to connect, a
// folder wedged on an error, or a disk about to fill up are all completely
// silent. For two non-technical modellers that is the same failure mode the
// tray icon was added to fix, one level up -- the icon says "something is
// wrong", but nobody is looking at the icon either.
//
// So the tray raises real Windows toasts for the handful of things that
// genuinely need a person. The bar for "genuinely needs a person" is high on
// purpose; see alerts.go for the policy that decides.

// Notification is one thing worth telling the user about.
type Notification struct {
	// Title is the bold first line. Keep it under about 40 characters: Windows
	// truncates rather than wraps it.
	Title string
	// Body is the detail line, up to two lines before it is truncated.
	Body string
	// Launch is opened when the toast is clicked. Always a GUI URL here, so
	// that every toast is click-through to the page that can act on it.
	Launch string
}

// notifier delivers notifications to whatever the platform provides. The tray
// keeps working when this fails or does nothing -- notifications are an
// addition to the icon, never the only way to find something out.
type notifier interface {
	// Notify shows n. It is safe to call from any goroutine and never blocks
	// for longer than it takes to hand the toast to the OS.
	Notify(n Notification)
	// Close releases whatever the platform implementation is holding.
	Close()
}

// nopNotifier is used when the platform has no notification support, or when
// setting it up failed. Everything else can then treat notifications as always
// available and never nil-check.
type nopNotifier struct{}

func (nopNotifier) Notify(Notification) {}
func (nopNotifier) Close()              {}
