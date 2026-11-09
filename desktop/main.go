// Persona for the desktop: macOS, Windows and Linux, built with MyGo (github.com/egoist/mygo).
//
// The app shows the web app that the hub serves, in the system's webview, so every feature of the web app is here,
// at the hub's version, with no second copy of the UI. Around it the app adds what a browser tab can't do: a list of
// hubs with pinned TLS from pairing links, system notifications, the needs-you count on the Dock / taskbar and in
// the tray, menus and shortcuts, a global shortcut, persona:// links, one instance and open at login
// (docs/desktop/desktop.md).
package main

import (
	"embed"
	"io/fs"
	"log"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
)

//go:embed all:frontend
var frontendFiles embed.FS

// version is the app's version when `mygo build` doesn't set it (mygo.json has the one releases use).
const version = "0.1.0"

func main() {
	if !mygo.App.RequestSingleInstanceLock() {
		return // the running instance takes over, with this one's command line
	}
	if mygo.App.Version() == "" {
		mygo.App.SetVersion(version)
	}
	web, err := fs.Sub(frontendFiles, "frontend")
	if err != nil {
		log.Fatal(err)
	}
	mygo.SetFrontend(web)

	d := newDesktop()
	mygo.Bind(&Connect{d: d}, &Persona{d: d})

	app := mygo.App
	// Registered before Run, so it also gets the link the app was started with.
	app.OnOpenURL(d.openLink)
	app.OnSecondInstance(func(args []string, _ string) {
		for _, a := range args {
			if strings.HasPrefix(strings.ToLower(a), "persona:") {
				d.openLink(a)
				return
			}
		}
		d.show()
	})
	app.OnNotificationClick(func(string) { d.show() })
	app.OnBeforeQuit(func(*mygo.QuitEvent) { d.quitting.Store(true) })
	// With a tray icon the app keeps running with its window closed, so notifications still come; without one
	// (Linux with no AppIndicator) closing the window quits.
	app.OnWindowAllClosed(func() {
		if d.tray == nil {
			app.Quit()
		}
	})
	app.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			d.show()
		}
	})
	app.WhenReady(func() {
		// A portable build on Windows or Linux registers persona:// itself; installers and macOS bundles declare it.
		if runtime.GOOS != "darwin" && !app.IsPackaged() && !mygo.IsDev() {
			if err := app.RegisterURLScheme("persona"); err != nil {
				log.Println("persona:// links:", err)
			}
		}
		d.start()
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
