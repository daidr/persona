// Persona for the desktop: macOS, Windows and Linux, built with MyGo (github.com/egoist/mygo).
//
// The window is drawn with MyGo's native UI (package ui, internal/app), with no webview: the app talks to the hub's
// HTTP and WebSocket API itself, as the phone apps do. Around the window it adds a tray icon that keeps the app
// running with the window closed, system notifications, the needs-you count on the Dock / taskbar, menus and
// shortcuts, a global shortcut, persona:// links, one instance and open at login (docs/desktop/desktop.md).
package main

import (
	"errors"
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/app"
	"github.com/nwverse/persona/desktop/internal/hubs"
	"github.com/nwverse/persona/desktop/internal/l10n"
)

// version is the app's version when `mygo build` doesn't set it (mygo.json has the one releases use).
const version = "0.2.0"

// desktop is the platform around the app: the window, tray, menus, notifications and secrets. Everything runs on
// the main thread except Post, which any goroutine calls.
type desktop struct {
	app      *app.App
	win      *mygo.Window
	tray     *mygo.Tray
	lang     string
	badge    int
	quitting atomic.Bool
	// pending is a persona:// link that arrived before the app was ready.
	mu      sync.Mutex
	ready   bool
	pending string
	notes   map[string]*mygo.Notification
}

func main() {
	if !mygo.App.RequestSingleInstanceLock() {
		return // the running instance takes over, with this one's command line
	}
	if mygo.App.Version() == "" {
		mygo.App.SetVersion(version)
	}
	d := &desktop{notes: map[string]*mygo.Notification{}}
	a := mygo.App
	// Registered before Run, so it also gets the link the app was started with.
	a.OnOpenURL(d.openLink)
	a.OnSecondInstance(func(args []string, _ string) {
		for _, arg := range args {
			if strings.HasPrefix(strings.ToLower(arg), "persona:") {
				d.openLink(arg)
				return
			}
		}
		d.show()
	})
	a.OnBeforeQuit(func(*mygo.QuitEvent) { d.quitting.Store(true) })
	// With a tray icon the app keeps running with its window closed, so notifications still come; without one
	// (Linux with no AppIndicator) closing the window quits.
	a.OnWindowAllClosed(func() {
		if d.tray == nil {
			a.Quit()
		}
	})
	a.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			d.show()
		}
	})
	a.WhenReady(d.start)
	if err := a.Run(); err != nil {
		log.Fatal(err)
	}
}

func (d *desktop) tr(key string, args ...string) string { return l10n.Tr(d.lang, key, args...) }

// start runs once the app is ready: the hubs, the window, menus, tray and shortcut, then the hub opened last or a
// link the app was started with.
func (d *desktop) start() {
	d.lang = l10n.Match(mygo.App.PreferredLanguages())
	// A portable build on Windows or Linux registers persona:// itself; installers and macOS bundles declare it.
	if runtime.GOOS != "darwin" && !mygo.App.IsPackaged() && !mygo.IsDev() {
		if err := mygo.App.RegisterURLScheme("persona"); err != nil {
			log.Println("persona:// links:", err)
		}
	}
	path := ""
	if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
		path = filepath.Join(dir, "hubs.json")
	} else {
		log.Println("data directory:", err)
	}
	d.app = app.New(d, hubs.Load(path), d.lang, mygo.App.Version())
	d.newWindow()
	d.setupTray()
	d.refreshMenus()
	d.applyShortcut()
	d.app.Start()

	d.mu.Lock()
	d.ready = true
	pending := d.pending
	d.mu.Unlock()
	if pending != "" {
		d.openLink(pending)
	}
}

func (d *desktop) newWindow() {
	d.win = mygo.NewWindow(mygo.WindowOptions{
		Title:     "Persona",
		Width:     1280,
		Height:    820,
		MinWidth:  760,
		MinHeight: 480,
		StateKey:  "main",
		Content:   ui.View(d.app.View),
	})
	d.win.OnClose(func(e *mygo.CloseEvent) {
		// Closing hides the window to the tray, keeping the hub's connection and its notifications.
		if !d.quitting.Load() && d.tray != nil {
			e.PreventDefault()
			d.win.Hide()
		}
	})
}

// openLink signs in with a persona:// pairing link.
func (d *desktop) openLink(raw string) {
	d.mu.Lock()
	if !d.ready {
		d.pending = raw
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()
	mygo.RunOnMain(func() {
		d.show()
		d.app.SignIn(raw, "", "", func(err error) {
			if err != nil {
				log.Println("link:", err)
			}
		})
	})
}

// show brings the main window to the front, making it when there is none.
func (d *desktop) show() {
	if d.app == nil {
		return
	}
	if d.win == nil || d.win.IsDestroyed() {
		d.newWindow()
		return
	}
	if d.win.IsMinimized() {
		d.win.Restore()
	}
	d.win.Show()
	d.win.Focus()
}

// toggle is the global shortcut: it brings the window up, or hides it when it is already in front.
func (d *desktop) toggle() {
	if d.win != nil && !d.win.IsDestroyed() && d.win.IsVisible() && d.win.IsFocused() && d.tray != nil {
		d.win.Hide()
		return
	}
	d.show()
}

func (d *desktop) goTo(path string) {
	d.show()
	d.app.Navigate(path)
	d.redraw()
}

func (d *desktop) redraw() {
	if d.win != nil && !d.win.IsDestroyed() {
		d.win.Invalidate()
	}
}

// Host, for the app.

func (d *desktop) Post(fn func()) {
	mygo.RunOnMain(func() {
		fn()
		d.redraw()
	})
}

func (d *desktop) Notify(n app.Notification) {
	note := mygo.NewNotification(mygo.NotificationOptions{
		ID:     "persona/" + n.ID,
		Group:  n.Group,
		Title:  n.Title,
		Body:   n.Body,
		Silent: n.Silent,
	})
	open := n.Open
	note.OnClick(func() { mygo.RunOnMain(func() { d.goTo(open) }) })
	if old := d.notes[n.ID]; old != nil {
		old.Close()
	}
	if len(d.notes) > 100 {
		d.notes = map[string]*mygo.Notification{} // only for replacing one with the same id; old ones stay posted
	}
	d.notes[n.ID] = note
	go func() {
		if err := note.Show(); err != nil && !errors.Is(err, mygo.ErrNotificationsDenied) {
			log.Println("notification:", err)
		}
	}()
}

func (d *desktop) SetBadge(n int) {
	d.badge = n
	mygo.App.SetBadgeCount(n)
	d.refreshTray()
}

func (d *desktop) OpenURL(url string) {
	if err := mygo.Shell.OpenExternal(url); err != nil {
		log.Println("open:", err)
	}
}

func (d *desktop) PickFiles(title string, done func([]string)) {
	// the dialog waits for the user, so it is asked for off the main thread
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: d.win, Title: title, Multiple: true})
		if err != nil {
			log.Println("open dialog:", err)
		}
		if len(paths) > 0 {
			d.Post(func() { done(paths) })
		}
	}()
}

func (d *desktop) SaveFile(title, name string, done func(string)) {
	go func() {
		dest, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: d.win, Title: title, DefaultPath: name, CreateDirectories: true})
		if err != nil {
			log.Println("save dialog:", err)
		}
		if dest != "" {
			d.Post(func() { done(dest) })
		}
	}()
}

func (d *desktop) Focused() bool {
	return d.win != nil && !d.win.IsDestroyed() && d.win.IsVisible() && d.win.IsFocused()
}

// Device tokens live in the system's credential store, one per hub.
func (d *desktop) Secret(key string) (string, error) {
	b, err := mygo.Secrets.Get(key)
	return string(b), err
}

func (d *desktop) SetSecret(key, value string) error { return mygo.Secrets.Set(key, []byte(value)) }

func (d *desktop) DeleteSecret(key string) {
	if err := mygo.Secrets.Delete(key); err != nil {
		log.Println("secret:", err)
	}
}

func (d *desktop) OpenAtLogin() bool { return mygo.App.OpenAtLogin() }

func (d *desktop) SetOpenAtLogin(on bool) error {
	err := mygo.App.SetOpenAtLogin(on)
	d.refreshMenus()
	return err
}

func (d *desktop) Shortcut() string { return shortcutLabel() }

func (d *desktop) SetShortcut(bool) { d.applyShortcut() }

func (d *desktop) HubsChanged() { d.refreshMenus() }
