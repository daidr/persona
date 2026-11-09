package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/nwverse/persona/desktop/internal/hubs"
	"github.com/nwverse/persona/desktop/internal/l10n"
)

//go:embed preload.js
var preloadScript string

// desktop is the app: the hubs, the main window and what goes around it. MyGo calls its window, menu and tray
// handlers on the main thread and the bound methods on goroutines of their own; mu guards the store, the window and
// the badge.
type desktop struct {
	mu    sync.Mutex
	store *hubs.Store
	lang  string
	// win is the main window and origin the hub origin it trusts ("" before any hub). A window's trusted origins
	// are fixed when it is made, so opening a hub of another origin makes a new window in place of this one.
	win    *mygo.Window
	origin string
	badge  int
	ready  bool
	// pending is a persona:// link that arrived before the app was ready.
	pending string

	tray     *mygo.Tray
	quitting atomic.Bool
	notes    notifications
}

func newDesktop() *desktop {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Println("data directory:", err)
	}
	path := ""
	if dir != "" {
		path = filepath.Join(dir, "hubs.json")
	}
	return &desktop{store: hubs.Load(path), lang: "en"}
}

func (d *desktop) tr(key string, args ...string) string { return l10n.Tr(d.lang, key, args...) }

// start runs once the app is ready: menus, tray, shortcut, then the hub opened last, a link the app was started
// with, or the connect page.
func (d *desktop) start() {
	d.lang = l10n.Match(mygo.App.PreferredLanguages())
	d.mu.Lock()
	d.ready = true
	pending := d.pending
	current, ok := d.store.Get(d.store.Settings.Current)
	d.mu.Unlock()

	d.setupTray()
	d.refreshMenus()
	d.applyShortcut()

	switch {
	case pending != "":
		d.openLink(pending)
	case ok:
		d.openHub(current, "")
	default:
		d.openConnect()
	}
}

// openLink opens a persona:// link or a web sign-in link: it adds the hub and opens it signed in.
func (d *desktop) openLink(raw string) {
	d.mu.Lock()
	if !d.ready {
		d.pending = raw
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()
	in, err := hubs.ParseInput(raw)
	if err != nil {
		log.Println("link:", err)
		d.openConnect()
		return
	}
	d.openHub(in.Hub, in.Hub.OpenURL(in.Token))
}

// currentHub is the hub the main window trusts and shows.
func (d *desktop) currentHub() (hubs.Hub, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.store.Get(d.store.Settings.Current)
}

// openHub adds the hub to the list (or updates it) and shows target, or the hub's root, in the main window.
func (d *desktop) openHub(h hubs.Hub, target string) {
	d.mu.Lock()
	h.Opened = time.Now()
	h = d.store.Upsert(h)
	d.store.Settings.Current = h.URL
	if err := d.store.Save(); err != nil {
		log.Println("saving hubs:", err)
	}
	if target == "" {
		target = h.OpenURL("")
	}
	old, oldOrigin := d.win, d.origin
	d.mu.Unlock()

	if old != nil && !old.IsDestroyed() && oldOrigin == h.Origin() {
		if err := old.Page().LoadURL(target); err != nil {
			log.Println("open hub:", err)
		}
		d.show()
	} else {
		w := d.newMainWindow(h.Origin(), target)
		if old != nil && !old.IsDestroyed() {
			w.SetBounds(old.Bounds())
			old.Destroy()
		}
		d.mu.Lock()
		d.win, d.origin = w, h.Origin()
		d.mu.Unlock()
		w.Show()
		w.Focus()
	}
	d.refreshMenus()
}

// openConnect shows the connect page, the app's own, in the main window.
func (d *desktop) openConnect() { d.openAppPage("/") }

func (d *desktop) openAppPage(path string) {
	d.mu.Lock()
	w := d.win
	d.mu.Unlock()
	if w == nil || w.IsDestroyed() {
		w = d.newMainWindow("", path)
		d.mu.Lock()
		d.win, d.origin = w, ""
		d.mu.Unlock()
	} else if err := w.Page().LoadURL(path); err != nil {
		log.Println("connect page:", err)
	}
	d.show()
}

// show brings the main window to the front, making it when there is none.
func (d *desktop) show() {
	d.mu.Lock()
	w, ready := d.win, d.ready
	current, ok := d.store.Get(d.store.Settings.Current)
	d.mu.Unlock()
	if !ready {
		return
	}
	if w == nil || w.IsDestroyed() {
		if ok {
			d.openHub(current, "")
		} else {
			d.openConnect()
		}
		return
	}
	if w.IsMinimized() {
		w.Restore()
	}
	w.Show()
	w.Focus()
}

// toggle is the global shortcut: it brings the window up, or hides it when it is already in front.
func (d *desktop) toggle() {
	d.mu.Lock()
	w := d.win
	d.mu.Unlock()
	if w != nil && !w.IsDestroyed() && w.IsVisible() && w.IsFocused() && d.tray != nil {
		w.Hide()
		return
	}
	d.show()
}

// pageOptions are the options of every window that shows a hub of origin: it may call the Persona service, gets
// the preload that bridges notifications, and keeps running in the background so the hub's WebSocket and its
// notifications go on with the window hidden.
func (d *desktop) pageOptions(origin string) mygo.PageOptions {
	opts := mygo.PageOptions{
		PreloadScript:        d.preload(origin),
		BackgroundThrottling: mygo.ThrottlingOff,
		UserAgentSuffix:      "PersonaDesktop/" + mygo.App.Version(),
		SafariTabFocus:       true,
	}
	if origin != "" {
		opts.TrustedOrigins = []string{origin}
	}
	return opts
}

// preload is preload.js with what it needs to know before the page runs: the hub's origin, which pages get the
// bridge, and whether notifications are allowed.
func (d *desktop) preload(origin string) string {
	d.mu.Lock()
	denied := d.store.Settings.NotificationsDenied
	d.mu.Unlock()
	permission := "granted"
	if denied || !mygo.NotificationsSupported() {
		permission = "denied"
	}
	cfg, _ := json.Marshal(map[string]string{"origin": origin, "permission": permission})
	return strings.Replace(preloadScript, "__PERSONA_DESKTOP__", string(cfg), 1)
}

func (d *desktop) newMainWindow(origin, target string) *mygo.Window {
	w := mygo.NewWindow(mygo.WindowOptions{
		Title:     "Persona",
		URL:       target,
		Width:     1280,
		Height:    820,
		MinWidth:  420,
		MinHeight: 480,
		StateKey:  "main",
		Page:      d.pageOptions(origin),
	})
	w.OnClose(func(e *mygo.CloseEvent) {
		// Closing hides the window to the tray, keeping the hub's connection and its notifications.
		if !d.quitting.Load() && d.tray != nil {
			e.PreventDefault()
			w.Hide()
		}
	})
	d.wirePage(w, origin)
	return w
}

// wirePage sets up a window showing a hub of origin: navigation, new windows, pinned certificates, load failures,
// the badge and downloads.
func (d *desktop) wirePage(w *mygo.Window, origin string) {
	p := w.Page()
	p.OnWillNavigate(func(e *mygo.NavigateEvent) { d.willNavigate(e) })
	p.SetWindowOpenHandler(func(req mygo.WindowOpenRequest) *mygo.WindowOptions {
		if h, ok := d.currentHub(); ok && h.Contains(req.URL) && h.Origin() == origin {
			// A page of the hub asked for a window of its own: one more window of the same hub.
			return &mygo.WindowOptions{Title: "Persona", Width: 1100, Height: 760, Page: d.pageOptions(origin)}
		}
		if req.UserInitiated && isWebURL(req.URL) {
			go openExternal(req.URL)
		}
		return nil
	})
	p.SetCertificateErrorHandler(func(e mygo.CertificateError) bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		for _, h := range d.store.Hubs {
			if h.Pinned(e.Host, e.Certificate) {
				return true
			}
		}
		log.Printf("certificate of %s refused: %s", e.Host, e.Error)
		return false
	})
	p.OnDidFailLoad(func(err *mygo.LoadError) {
		if cancelledLoad(err) {
			return
		}
		d.mu.Lock()
		h, ok := d.store.ForPage(err.URL)
		d.mu.Unlock()
		if !ok {
			return
		}
		q := url.Values{"offline": {h.URL}, "detail": {err.Description}}
		if loadErr := p.LoadURL("/?" + q.Encode()); loadErr != nil {
			log.Println("offline page:", loadErr)
		}
	})
	p.OnRenderProcessGone(func(string) { p.Reload() })
	p.OnPageTitleUpdated(func(e *mygo.TitleEvent) {
		if h, ok := d.currentHub(); ok && h.Contains(p.URL()) {
			d.setBadge(hubs.BadgeFromTitle(e.Title))
		}
	})
	p.OnDownloadDone(func(dl *mygo.Download) {
		if dl.Err != nil || dl.Path == "" {
			return
		}
		n := mygo.NewNotification(mygo.NotificationOptions{Title: d.tr("downloadDone"), Body: filepath.Base(dl.Path)})
		n.OnClick(func() { mygo.Shell.ShowItemInFolder(dl.Path); n.Close() })
		if err := n.Show(); err != nil && !errors.Is(err, mygo.ErrNotificationsDenied) {
			log.Println("download notification:", err)
		}
	})
}

// willNavigate keeps the window on the hub it trusts. Moving to another hub (the web app's Switch hub hands its list
// over in `#hubs=`, web/src/lib/hubs.ts) opens that hub as the app does, with a window that trusts it; persona://
// links pair; other sites open in the default browser.
func (d *desktop) willNavigate(e *mygo.NavigateEvent) {
	u, err := url.Parse(e.URL)
	if err != nil {
		e.PreventDefault()
		return
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	case "persona":
		e.PreventDefault()
		go d.openLink(e.URL)
		return
	case "mygo", "about", "blob", "data":
		return
	default:
		e.PreventDefault()
		return
	}
	if isAppURL(e.URL) {
		return
	}
	if h, ok := d.currentHub(); ok && h.Contains(e.URL) {
		return
	}
	d.mu.Lock()
	known, isKnown := d.store.ForPage(e.URL)
	d.mu.Unlock()
	frag, _ := url.ParseQuery(u.Fragment)
	if isKnown || frag.Has("hubs") {
		e.PreventDefault()
		h := known
		if !isKnown {
			base := *u
			base.RawQuery, base.Fragment, base.RawFragment = "", "", ""
			addr, err := hubs.Normalize(base.String())
			if err != nil {
				return
			}
			h = hubs.Hub{URL: addr}
		}
		go d.openHub(h, e.URL)
		return
	}
	e.PreventDefault()
	if e.UserInitiated {
		go openExternal(e.URL)
	}
}

// goTo shows a page of the web app in the current hub, without reloading it when the hub is already showing.
func (d *desktop) goTo(path string) {
	h, ok := d.currentHub()
	if !ok {
		d.openConnect()
		return
	}
	d.mu.Lock()
	w := d.win
	d.mu.Unlock()
	base, _ := url.Parse(h.URL)
	full := strings.TrimSuffix(base.Path, "/") + path
	if w != nil && !w.IsDestroyed() && h.Contains(w.Page().URL()) {
		js, _ := json.Marshal(full)
		// The web app's router follows popstate (web/src/lib/router.ts).
		if _, err := w.Page().Eval("history.pushState(null, '', " + string(js) + "); dispatchEvent(new PopStateEvent('popstate')); return true"); err == nil {
			d.show()
			return
		}
	}
	d.openHub(h, h.URL+path)
}

// setBadge shows how many agents need the user on the Dock / taskbar and the tray.
func (d *desktop) setBadge(n int) {
	d.mu.Lock()
	changed := d.badge != n
	d.badge = n
	d.mu.Unlock()
	if !changed {
		return
	}
	mygo.App.SetBadgeCount(n)
	d.refreshTray()
}

func isWebURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

// isAppURL tells the app's own pages: its frontend (mygo://localhost, served at http://mygo.localhost on Windows)
// and, in development, the dev server.
func isAppURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case u.Scheme == "mygo", u.Scheme == "about":
		return true
	case host == "mygo.localhost" || strings.HasSuffix(host, ".mygo.localhost"):
		return true
	case mygo.IsDev() && (host == "localhost" || host == "127.0.0.1"):
		return true
	}
	return false
}

func openExternal(raw string) {
	if err := mygo.Shell.OpenExternal(raw); err != nil {
		log.Println("open in browser:", err)
	}
}

// cancelledLoad tells a load that stopped because something else replaced it (a navigation the app turned down, a
// new page) from one that failed.
func cancelledLoad(err *mygo.LoadError) bool {
	if err == nil {
		return true
	}
	switch err.Code {
	case -999, 102, 302: // NSURLErrorCancelled, WebKit's frame load interrupted, WebKitGTK's cancelled
		return true
	}
	desc := strings.ToLower(err.Description)
	return strings.Contains(desc, "cancel") || strings.Contains(desc, "interrupted") || strings.Contains(desc, "aborted")
}
