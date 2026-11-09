# Desktop app (`desktop/`)

Persona for macOS, Windows and Linux, built with [MyGo](https://github.com/egoist/mygo), a Go desktop framework
that shows pages in the system's own webview (WKWebView, WebKitGTK, WebView2) and needs no cgo. The app is one Go
binary of about 15 MB.

## What it is

The app shows **the web app that the hub serves**, in a window of its own. So every feature of the web app is in the
desktop app, with the same behavior and at the hub's version: the Persona chat, agents and their terminals, the
Inbox, computers, to-dos, the knowledge base, settings, extensions, the command palette (⌘K / Ctrl+K) and the rest
(`docs/web/web.md`). There is no second copy of the UI to keep in step; a hub update updates the app too.

Around the page the app adds what a browser tab can't do:

| | |
| --- | --- |
| Hubs | A connect page, the app's own (`desktop/frontend/`), lists the hubs; you add one by address, by the apps' pairing link (`persona://pair?…`) or by a web sign-in link. Each hub keeps its own sign-in in the webview's storage of its origin, as a browser does. |
| Pinned TLS | A pairing link's `fp` pins the hub's public key, so a hub with its own self-signed TLS opens without trusting its certificate anywhere else (below). |
| Notifications | The page's Notification API goes to the system's notifications (`desktop/preload.js`). Settings › Notifications in the web app works unchanged: needs you, finished, quiet hours, one notification for several finished agents; a click opens the agent. |
| Badge | The needs-you count (the web app's `(N)` title) goes on the Dock / taskbar icon and on the tray icon, with a menu line that opens the agents. |
| Tray | Closing the window hides it; the app keeps running in the menu bar / notification area, its connection to the hub open, so notifications keep coming. The tray menu shows the window, switches hubs and quits. Linux needs `libayatana-appindicator3` for it; without it, closing the window quits. |
| Menus | Settings (⌘, / Ctrl+,), Go (Persona, Agents, Inbox, Computers, To-dos, Knowledge on ⌘1–⌘6 / Ctrl+1–6, Back, Forward), Hub (the hubs, Manage Hubs ⌘⇧H), View (reload, zoom, full screen), Edit and Window. |
| Global shortcut | ⌥⌘P / Ctrl+Alt+P brings Persona to the front from any app, or hides it when it is in front; the connect page turns it off. |
| Links | `persona://pair?…` links open in the app and sign it in. Links to other sites open in the default browser; a page of the hub that opens a window gets another app window. |
| The rest | One instance (a second launch brings the first forward), the window's size and place remembered, open at login, downloads announced with a notification that shows the file. |

## How it works

- `desktop/main.go` starts the app: the single-instance lock, the `persona://` handler, then `desktop.start`, which
  opens the hub opened last or the connect page.
- `desktop/desktop.go` owns the main window. A MyGo window trusts the origins it was made with
  (`PageOptions.TrustedOrigins`): only the hub it shows may call the app's Go code. Opening a hub of another origin
  therefore makes a new window in place of the old one, at the same place and size. The window keeps to its hub:
  the web app's **Switch hub** (`<hub>/#hubs=…`, `web/src/lib/hubs.ts`) opens that hub the app's way, and other
  sites go to the browser. Pages run with background throttling off so the WebSocket stays live with the window
  hidden.
- `desktop/bridge.go` has the two services pages call: `Persona` (the hub's page: notifications) and `Connect` (the
  app's own connect page: the hubs and the app's settings). Each checks which page called.
- `desktop/preload.js` runs before the hub's page and, on that origin only, replaces `window.Notification` with one
  that calls `Persona.Notify` / `Persona.Close`. It reports `maxActions` 0, so the web app posts plain notifications
  (no Allow / Deny buttons) that open the card.
- `desktop/internal/hubs` is the list of hubs (`hubs.json` in the app's data directory, no secrets), the link
  parsing and the pin check; `desktop/internal/l10n` the app's texts. Neither imports MyGo, so their tests run
  without a window.

The web app learns nothing new from the desktop app except its name: the webview's user agent ends in
`PersonaDesktop/<version>`, and the web app names the device "Persona Desktop · Mac" (`web/src/lib/auth.ts`) in
Settings › Devices.

## Pinned TLS

A hub with TLS of its own uses a self-signed certificate, which a webview refuses like a browser does. The apps'
pairing link carries the hub's public-key fingerprint (`fp`, SHA-256 of the SubjectPublicKeyInfo, base64url) and its
TLS port (`tls`); the app then opens `https://<host of url>:<tls>` and accepts a certificate the system doesn't trust
only when its host is that hub's and its key has that fingerprint (`hubs.Hub.Pinned`). A certificate the system
trusts (a hub behind a reverse proxy with a public certificate) is accepted as in a browser, and the pin is not
consulted. Initial trust comes only from the pairing link, never from what the hub answers
(`docs/operations/network.md`, "TLS and public-key pinning"); on Linux and Windows a certificate allowed once stays
allowed for that host until the app quits, a limit of the webviews.

Get the link on a signed-in device: **Settings › Devices › Connect a new device**, the app's link (or
`personad hub pair` on the hub). A web sign-in link or a plain address works for a hub whose certificate the system
trusts, or a plain-HTTP hub on the LAN.

## Languages

The app's own texts (menus, tray, the connect page) are in `desktop/internal/l10n/locales/` in en, zh-Hans and ja,
chosen by the system's preferred languages; `l10n_test.go` checks that the three have the same keys and placeholders
and that every key the code uses exists. The hub's pages keep their own language setting.

## Build and check

Go 1.27.3 or later (MyGo's minimum; `GOTOOLCHAIN=auto` fetches it). Pages need the system webview: on Linux, GTK 3
and WebKitGTK 4.1 (`sudo apt install libwebkit2gtk-4.1-0`); on Windows 10, the WebView2 Runtime.

```sh
cd desktop
go run .                                            # a development run
go run github.com/egoist/mygo/cmd/mygo@v0.8.3 build # packaged apps in dist/: .app + .dmg, installer, .deb
scripts/ci/desktop.sh                               # from the repository root: vet, tests, every platform, pages
```

`mygo build` reads `desktop/mygo.json` (name, identifier, the `persona` URL scheme, macOS usage descriptions) and
`desktop/resources/icon.png`. Signing, notarization and auto-updates are MyGo's (its `distribution.md` and
`updates.md`); they need keys this repository doesn't hold and are not set up yet.

`scripts/ci/desktop.sh` is portable but not yet a lane of hosted CI. `desktop/test/pages.mjs` checks the connect page
and the notification bridge in Chromium against a stand-in for MyGo's bridge and writes screenshots, light and dark.

## Limits

- The web app needs the hub: offline the window shows the connect page with "Can't reach" and Try again, not a
  cached copy (service workers are not available in every webview).
- Notifications have no Allow / Deny buttons; clicking one opens the card.
- On macOS the native Mac app (`apple/Mac/`) also handles `persona://`; with both installed the system picks one.
  The desktop app is meant for Windows and Linux first, and for Macs without the native app.
- The identifier in `desktop/mygo.json` is a placeholder for local builds; a published build takes its identifier
  and signing from private config, like the other apps.
