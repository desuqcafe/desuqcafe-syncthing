//go:build windows

package main

// Windows toast notifications, spoken to WinRT directly.
//
// Three ways to raise a toast were considered and two rejected:
//
//   - Shell_NotifyIcon with NIF_INFO is the classic route and Windows 10+
//     renders it as a real toast. It needs the NOTIFYICONDATA of a tray icon,
//     and fyne.io/systray keeps its own private. Adding a second icon purely
//     to hang balloons off would put two entries in the notification area.
//
//   - Shelling out to PowerShell to call WinRT is what most small tools do.
//     It works, but spawns a ~30 MB process per toast from a program linked
//     -H windowsgui, and dies quietly under Constrained Language Mode -- which
//     is exactly the sort of managed-machine setting these users may have and
//     nobody would ever debug.
//
//   - So: call WinRT through combase.dll. It is more code than the other two,
//     but it is self-contained, needs no new dependency, no COM registration
//     and no server process, and it reports real errors.
//
// Clicks are handled with activationType="protocol", which makes Windows open
// a URL with the default handler. That is the whole reason no COM activator is
// needed: a toast that opens the GUI needs nothing registered beyond a name.

import (
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	combase = windows.NewLazySystemDLL("combase.dll")

	procRoInitialize           = combase.NewProc("RoInitialize")
	procRoUninitialize         = combase.NewProc("RoUninitialize")
	procRoGetActivationFactory = combase.NewProc("RoGetActivationFactory")
	procRoActivateInstance     = combase.NewProc("RoActivateInstance")
	procWindowsCreateString    = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString    = combase.NewProc("WindowsDeleteString")
)

// RO_INIT_MULTITHREADED. The toast thread does no UI and pumps no messages, so
// it wants the multi-threaded apartment rather than an STA it would have to
// service.
const roInitMultithreaded = 1

// Interface IDs, from the Windows SDK headers.
var (
	iidXMLDocumentIO            = mustGUID("{6cd0e74e-ee65-4489-9ebf-ca43e87ba637}")
	iidXMLDocument              = mustGUID("{f7f3a506-1e87-42d6-bcfb-b8c809fa5494}")
	iidToastNotificationFactory = mustGUID("{04124b20-82c6-4229-b109-fd9ed4662b53}")
	iidToastNotificationStatics = mustGUID("{50ac103f-d235-4598-bbef-98fe4d1a3ad4}")
)

// WinRT runtime class names.
const (
	classXMLDocument              = "Windows.Data.Xml.Dom.XmlDocument"
	classToastNotification        = "Windows.UI.Notifications.ToastNotification"
	classToastNotificationManager = "Windows.UI.Notifications.ToastNotificationManager"
)

// Vtable slots. Every WinRT interface starts with IUnknown's three methods and
// IInspectable's three, so the interface's own methods begin at 6.
const (
	slotQueryInterface = 0
	slotRelease        = 2

	slotLoadXML                   = 6 // IXmlDocumentIO::LoadXml
	slotCreateToastNotification   = 6 // IToastNotificationFactory::CreateToastNotification
	slotCreateToastNotifierWithID = 7 // IToastNotificationManagerStatics::CreateToastNotifierWithId
	slotShow                      = 6 // IToastNotifier::Show
)

func mustGUID(s string) windows.GUID {
	g, err := windows.GUIDFromString(s)
	if err != nil {
		panic(err)
	}
	return g
}

// hresult renders a COM error code the way every other Windows tool does, so a
// failure in the log can be pasted into a search engine.
type hresult uintptr

func (h hresult) Error() string { return fmt.Sprintf("HRESULT 0x%08X", uint32(h)) }

func check(r uintptr) error {
	if int32(r) < 0 {
		return hresult(r)
	}
	return nil
}

// hstring is a WinRT string handle. It has to be created and destroyed through
// combase rather than being a Go string.
type hstring uintptr

func newHString(s string) (hstring, error) {
	// UTF16FromString appends the terminating NUL, which is not counted in the
	// length WindowsCreateString wants.
	u16, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h hstring
	r, _, _ := procWindowsCreateString.Call(
		uintptr(unsafe.Pointer(&u16[0])),
		uintptr(len(u16)-1),
		uintptr(unsafe.Pointer(&h)),
	)
	if err := check(r); err != nil {
		return 0, fmt.Errorf("WindowsCreateString(%q): %w", s, err)
	}
	return h, nil
}

func (h hstring) free() {
	if h != 0 {
		procWindowsDeleteString.Call(uintptr(h))
	}
}

// comObject is a pointer to a COM interface: a pointer to a struct whose first
// field points at the vtable.
//
// These are deliberately unsafe.Pointer rather than uintptr. The memory is the
// OS's and the collector never moves it, so either would work at runtime --
// but a uintptr that is converted back to a pointer is exactly the pattern
// `go vet` flags, and keeping the real type means the round trip never happens.
type comObject unsafe.Pointer

// comCall invokes the slot'th entry of obj's vtable with obj as the implicit
// first argument, which is the calling convention every COM method uses.
func comCall(obj comObject, slot uintptr, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, slot*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

func comRelease(obj *comObject) {
	if *obj != nil {
		comCall(*obj, slotRelease)
		*obj = nil
	}
}

func comQueryInterface(obj comObject, iid *windows.GUID) (comObject, error) {
	var out comObject
	r := comCall(obj, slotQueryInterface,
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if err := check(r); err != nil {
		return nil, fmt.Errorf("QueryInterface: %w", err)
	}
	return out, nil
}

func roActivateInstance(class string) (comObject, error) {
	h, err := newHString(class)
	if err != nil {
		return nil, err
	}
	defer h.free()

	var out comObject
	r, _, _ := procRoActivateInstance.Call(uintptr(h), uintptr(unsafe.Pointer(&out)))
	if err := check(r); err != nil {
		return nil, fmt.Errorf("RoActivateInstance(%s): %w", class, err)
	}
	return out, nil
}

func roGetActivationFactory(class string, iid *windows.GUID) (comObject, error) {
	h, err := newHString(class)
	if err != nil {
		return nil, err
	}
	defer h.free()

	var out comObject
	r, _, _ := procRoGetActivationFactory.Call(
		uintptr(h), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if err := check(r); err != nil {
		return nil, fmt.Errorf("RoGetActivationFactory(%s): %w", class, err)
	}
	return out, nil
}

// --- the toaster ---------------------------------------------------------

// winToaster serialises every toast onto one OS thread. WinRT is initialised
// per thread, so the alternative is calling RoInitialize on whichever
// goroutine happens to be delivering -- and Go moves goroutines between
// threads freely, which would leave the apartment uninitialised at random.
type winToaster struct {
	appID string

	reqs      chan Notification
	closeOnce sync.Once
	done      chan struct{}
}

// newNotifier returns a notifier that raises Windows toasts attributed to
// appName. It never returns nil: if WinRT is unavailable the tray carries on
// silently rather than failing to start over a notification.
func newNotifier(appID, appName string) notifier {
	if err := registerAppID(appID, appName); err != nil {
		// Not fatal. Without the registration the toast still appears, just
		// attributed to a generic name.
		slog.Warn("could not register the toast app identity", "err", err, "appID", appID)
	}

	t := &winToaster{
		appID: appID,
		// Buffered so a burst never blocks the event loop. Toasts are rare by
		// design; if this ever fills, dropping is the right answer.
		reqs: make(chan Notification, 16),
		done: make(chan struct{}),
	}
	go t.run()
	return t
}

func (t *winToaster) run() {
	// The apartment belongs to the thread, so the thread must not be reused.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	r, _, _ := procRoInitialize.Call(roInitMultithreaded)
	// RPC_E_CHANGED_MODE means somebody already initialised this thread with
	// the other apartment model; everything still works, so carry on.
	if err := check(r); err != nil && uint32(r) != 0x80010106 {
		slog.Error("RoInitialize failed; desktop notifications are off", "err", err)
		// Drain so senders never block.
		for range t.reqs {
		}
		return
	}
	defer procRoUninitialize.Call()

	for n := range t.reqs {
		if err := t.show(n); err != nil {
			slog.Error("could not show a notification", "err", err, "title", n.Title)
		}
	}
	close(t.done)
}

func (t *winToaster) show(n Notification) error {
	doc, err := roActivateInstance(classXMLDocument)
	if err != nil {
		return err
	}
	defer comRelease(&doc)

	docIO, err := comQueryInterface(doc, &iidXMLDocumentIO)
	if err != nil {
		return err
	}
	defer comRelease(&docIO)

	xml, err := newHString(toastXML(n))
	if err != nil {
		return err
	}
	defer xml.free()

	if err := check(comCall(docIO, slotLoadXML, uintptr(xml))); err != nil {
		return fmt.Errorf("LoadXml: %w", err)
	}

	xmlDoc, err := comQueryInterface(doc, &iidXMLDocument)
	if err != nil {
		return err
	}
	defer comRelease(&xmlDoc)

	factory, err := roGetActivationFactory(classToastNotification, &iidToastNotificationFactory)
	if err != nil {
		return err
	}
	defer comRelease(&factory)

	var toast comObject
	if err := check(comCall(factory, slotCreateToastNotification,
		uintptr(xmlDoc), uintptr(unsafe.Pointer(&toast)))); err != nil {
		return fmt.Errorf("CreateToastNotification: %w", err)
	}
	defer comRelease(&toast)

	statics, err := roGetActivationFactory(classToastNotificationManager, &iidToastNotificationStatics)
	if err != nil {
		return err
	}
	defer comRelease(&statics)

	appID, err := newHString(t.appID)
	if err != nil {
		return err
	}
	defer appID.free()

	var shower comObject
	if err := check(comCall(statics, slotCreateToastNotifierWithID,
		uintptr(appID), uintptr(unsafe.Pointer(&shower)))); err != nil {
		return fmt.Errorf("CreateToastNotifierWithId: %w", err)
	}
	defer comRelease(&shower)

	if err := check(comCall(shower, slotShow, uintptr(toast))); err != nil {
		return fmt.Errorf("Show: %w", err)
	}
	slog.Info("notification shown", "title", n.Title, "body", n.Body)
	return nil
}

func (t *winToaster) Notify(n Notification) {
	select {
	case t.reqs <- n:
	default:
		slog.Warn("notification queue full; dropped", "title", n.Title)
	}
}

func (t *winToaster) Close() {
	t.closeOnce.Do(func() { close(t.reqs) })
}

// toastXML builds the ToastGeneric payload.
//
// activationType="protocol" is what makes the toast click-through without a
// COM activator: Windows hands the launch string to the default protocol
// handler, so an http URL simply opens the GUI in the browser.
func toastXML(n Notification) string {
	var b strings.Builder
	b.WriteString(`<toast activationType="protocol" launch="`)
	xmlEscape(&b, n.Launch)
	b.WriteString(`"><visual><binding template="ToastGeneric"><text>`)
	xmlEscape(&b, n.Title)
	b.WriteString(`</text><text>`)
	xmlEscape(&b, n.Body)
	b.WriteString(`</text></binding></visual></toast>`)
	return b.String()
}

// xmlEscape is written out rather than using encoding/xml because that escapes
// newlines and tabs as character references, which show up literally in a
// toast.
func xmlEscape(b *strings.Builder, s string) {
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			b.WriteRune(r)
		}
	}
}

// registerAppID gives the toasts a name and an icon of their own.
//
// Windows attributes a toast to an AppUserModelID. An unregistered one still
// produces a toast, but with no display name. Registering it is a per-user
// registry write -- no admin, no COM server, no Start Menu shortcut parsing --
// so it is cheap enough to do on every start and self-healing if a profile is
// copied to another machine.
func registerAppID(appID, displayName string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER,
		`Software\Classes\AppUserModelId\`+appID, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	if err := key.SetStringValue("DisplayName", displayName); err != nil {
		return err
	}
	// ShowInSettings lets the user turn these off in Windows' own
	// Notifications settings, which is where they will look for the switch.
	return key.SetDWordValue("ShowInSettings", 1)
}
