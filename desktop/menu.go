package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"log"
	"runtime"
	"strconv"

	"github.com/egoist/mygo"
	"github.com/nwverse/persona/desktop/internal/hubs"
)

// shortcut brings Persona to the front from any app.
const shortcut = "CmdOrCtrl+Alt+P"

func shortcutLabel() string {
	if runtime.GOOS == "darwin" {
		return "⌥⌘P"
	}
	return "Ctrl+Alt+P"
}

// pages are the web app's places the Go menu reaches, with their shortcuts (web/src/lib/router.ts).
var pages = []struct{ key, path, accel string }{
	{"goPersona", "/", "CmdOrCtrl+1"},
	{"goAgents", "/agents", "CmdOrCtrl+2"},
	{"goInbox", "/inbox", "CmdOrCtrl+3"},
	{"goMachines", "/machines", "CmdOrCtrl+4"},
	{"goTodos", "/todos", "CmdOrCtrl+5"},
	{"goKnowledge", "/kb", "CmdOrCtrl+6"},
}

// refreshMenus sets the app menu and the tray menu again, after the hubs, the badge or a setting changed.
func (d *desktop) refreshMenus() {
	mygo.App.SetMenu(d.appMenu())
	d.refreshTray()
}

func (d *desktop) hubItems() []*mygo.MenuItem {
	d.mu.Lock()
	list := append([]hubs.Hub(nil), d.store.Hubs...)
	current := d.store.Settings.Current
	d.mu.Unlock()
	var items []*mygo.MenuItem
	for _, h := range list {
		h := h
		items = append(items, &mygo.MenuItem{
			Label:   h.Title(),
			Type:    mygo.MenuItemRadio,
			Checked: h.URL == current,
			Click:   func(*mygo.MenuItem, *mygo.Window) { go d.openHub(h, "") },
		})
	}
	if len(items) > 0 {
		items = append(items, mygo.Separator())
	}
	return append(items, &mygo.MenuItem{Label: d.tr("menuManageHubs"), Accelerator: "CmdOrCtrl+Shift+H", Click: func(*mygo.MenuItem, *mygo.Window) { d.openConnect() }})
}

func (d *desktop) loginItem() *mygo.MenuItem {
	return &mygo.MenuItem{Label: d.tr("menuOpenAtLogin"), Type: mygo.MenuItemCheckbox, Checked: mygo.App.OpenAtLogin(), Click: func(item *mygo.MenuItem, _ *mygo.Window) {
		if err := mygo.App.SetOpenAtLogin(item.IsChecked()); err != nil {
			log.Println("open at login:", err)
		}
	}}
}

func (d *desktop) appMenu() *mygo.Menu {
	settings := &mygo.MenuItem{Label: d.tr("menuSettings"), Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { go d.goTo("/settings") }}
	var first *mygo.MenuItem
	if runtime.GOOS == "darwin" {
		first = &mygo.MenuItem{Label: "Persona", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			mygo.Separator(),
			settings,
			d.loginItem(),
			mygo.Separator(),
			{Role: mygo.RoleServices},
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}}
	} else {
		first = &mygo.MenuItem{Label: d.tr("menuFile"), Submenu: []*mygo.MenuItem{
			settings,
			d.loginItem(),
			mygo.Separator(),
			{Role: mygo.RoleClose},
			{Role: mygo.RoleQuit},
		}}
	}
	var goItems []*mygo.MenuItem
	for _, p := range pages {
		path := p.path
		goItems = append(goItems, &mygo.MenuItem{Label: d.tr(p.key), Accelerator: p.accel, Click: func(*mygo.MenuItem, *mygo.Window) { go d.goTo(path) }})
	}
	goItems = append(goItems, mygo.Separator(),
		&mygo.MenuItem{Label: d.tr("menuBack"), Accelerator: "CmdOrCtrl+[", Click: func(_ *mygo.MenuItem, w *mygo.Window) {
			if w != nil && w.Page() != nil && w.Page().CanGoBack() {
				w.Page().GoBack()
			}
		}},
		&mygo.MenuItem{Label: d.tr("menuForward"), Accelerator: "CmdOrCtrl+]", Click: func(_ *mygo.MenuItem, w *mygo.Window) {
			if w != nil && w.Page() != nil && w.Page().CanGoForward() {
				w.Page().GoForward()
			}
		}})
	view := []*mygo.MenuItem{
		{Role: mygo.RoleReload},
		{Role: mygo.RoleForceReload},
		mygo.Separator(),
		{Role: mygo.RoleResetZoom},
		{Role: mygo.RoleZoomIn},
		{Role: mygo.RoleZoomOut},
		mygo.Separator(),
		{Role: mygo.RoleToggleFullScreen},
	}
	if mygo.IsDev() {
		view = append(view, mygo.Separator(), &mygo.MenuItem{Role: mygo.RoleToggleDevTools})
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		first,
		{Role: mygo.RoleEditMenu},
		{Label: d.tr("menuView"), Submenu: view},
		{Label: d.tr("menuGo"), Submenu: goItems},
		{Label: d.tr("menuHub"), Submenu: d.hubItems()},
		{Role: mygo.RoleWindowMenu},
	})
}

func (d *desktop) trayMenu() *mygo.Menu {
	d.mu.Lock()
	n := d.badge
	d.mu.Unlock()
	items := []*mygo.MenuItem{{Label: d.tr("trayShow"), Click: func(*mygo.MenuItem, *mygo.Window) { d.show() }}}
	if n > 0 {
		items = append(items, &mygo.MenuItem{Label: d.tr("trayNeedsYou", "count", strconv.Itoa(n)), Click: func(*mygo.MenuItem, *mygo.Window) { go d.goTo("/agents") }})
	}
	items = append(items, mygo.Separator())
	items = append(items, d.hubItems()...)
	items = append(items, mygo.Separator(), d.loginItem(), mygo.Separator(), &mygo.MenuItem{Label: d.tr("trayQuit"), Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }})
	return mygo.NewMenu(items)
}

func (d *desktop) setupTray() {
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:             "persona",
		Icon:           trayIcon(false),
		IconIsTemplate: true,
		ToolTip:        "Persona",
		Menu:           d.trayMenu(),
	})
	if err != nil {
		log.Println("tray:", err)
		return
	}
	d.tray = tray
}

// refreshTray shows the badge in the tray: a dot on the icon, the count beside it on macOS, the tooltip, and the
// menu's line that opens the agents.
func (d *desktop) refreshTray() {
	if d.tray == nil {
		return
	}
	d.mu.Lock()
	n := d.badge
	d.mu.Unlock()
	if err := d.tray.SetIcon(trayIcon(n > 0), true); err != nil {
		log.Println("tray icon:", err)
	}
	tip, title := "Persona", ""
	if n > 0 {
		tip = "Persona · " + d.tr("trayNeedsYou", "count", strconv.Itoa(n))
		title = strconv.Itoa(n)
	}
	d.tray.SetToolTip(tip)
	if runtime.GOOS == "darwin" {
		d.tray.SetTitle(title)
	}
	d.tray.SetMenu(d.trayMenu())
}

func (d *desktop) applyShortcut() {
	d.mu.Lock()
	off := d.store.Settings.NoShortcut
	d.mu.Unlock()
	registered := mygo.GlobalShortcut.IsRegistered(shortcut)
	switch {
	case off && registered:
		mygo.GlobalShortcut.Unregister(shortcut)
	case !off && !registered:
		if err := mygo.GlobalShortcut.Register(shortcut, d.toggle); err != nil {
			log.Println("global shortcut:", err)
		}
	}
}

// trayIcon draws the tray's template icon, black on transparent for the system to tint: a ring, the persona's
// outline, filled in the middle when something needs the user.
func trayIcon(attention bool) []byte {
	const size = 32
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			dx, dy := float64(x)-15.5, float64(y)-15.5
			r2 := dx*dx + dy*dy
			ring := r2 <= 13*13 && r2 >= 10*10
			dot := attention && r2 <= 6*6
			if ring || dot {
				img.Set(x, y, color.Black)
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
