# Desktop app (`desktop/`)

Persona for macOS, Windows and Linux, built with [MyGo](https://github.com/egoist/mygo), a Go desktop framework
that needs no cgo. The window is drawn with MyGo's **native UI** (package `ui`, on the GPU: Metal, Direct3D 11,
OpenGL), not a webview: the app talks to the hub's HTTP and WebSocket API itself, as the phone apps do
(`docs/protocol/protocol.md`). It is one Go binary; on Linux it needs GTK 3 alone and on Windows no WebView2.

## What it has

The work goes in stages toward the web app's features. What it has so far:

| | |
| --- | --- |
| Sign-in | The apps' pairing link (`persona://pair?…`, from **Settings › Devices › Connect a new device** on a signed-in device, or `personad hub pair`), a browser sign-in link (`<hub>/#token=…`), or an address and a token. The token it carries is traded once for this device's own (`POST /api/v1/auth/tokens`, `kind: desktop`, named "Persona Desktop · Mac / Windows / Linux" in Settings › Devices), which goes into the system's credential store (Keychain, Credential Manager, Secret Service), one per hub. Several hubs, switched from the Hub menu, the tray or Settings. |
| Pinned TLS | A pairing link's `fp` pins the hub's public key (below). |
| Live connection | The WebSocket (`/api/v1/ws`) with `client_hello`, app-level pings, reconnecting with backoff (0.6 s to 15 s) and reading everything again after a drop; the sidebar's foot says online, connecting or offline with Retry. A token the hub refuses goes back to the sign-in with the hub filled in. |
| Persona | The assistant's chats beside the one open: the main chat first, then the others by their last activity, archived ones apart (Archive / Unarchive); a new chat starts with its first message (`POST /api/v1/assistant/conversations`). A chat shows its transcript (Markdown, streaming replies as they are written), sending, and its cards. |
| Cards in replies | `persona-view` blocks in the assistant's replies (`docs/assistant/assistant-views.md`) drawn natively: text, Markdown, header, rows with badges, buttons, links, metrics, stat grids, progress, tables, code, dividers and empty states, at most three a reply. A component the desktop app doesn't draw yet shows the card's fallback below it. Actions: a reply goes through `…/views/{key}/actions` (a hub without card actions gets the reply as a message), `open` opens an https page or an agent / to-do in the app, `push` / `pop` move through the card's pages, and `confirm` asks first. Read-only while the reply is still being written. |
| Agents | Every computer's agents grouped as the other clients group them (needs you, working, unread, idle, offline), searchable, beside the chat of the one chosen: the transcript (text, thinking folded, tool calls on one line, errors), earlier turns as you scroll up, sending with Enter (Shift+Enter for a new line), Stop while it works, and marking it read. |
| Terminal and changes | An agent's page switches between its chat, its terminal and its changes. The terminal is the agent's screen as text, read again every 1.5 s while it is shown (`GET /api/v1/sessions/{mid}/{sid}/screen`), with a line to type (Enter sends it with Enter) and Esc, Tab, ⌃C, the arrows and Enter (`POST …/keys`); a session without the `terminal` capability is watched only. Changes are the agent's folder's branch, upstream, ahead / behind and last commit, its changed files with their lines added and removed, and the chosen file's diff (`git/status`, `git/diff`; a change only in the index shows the staged diff). |
| Cards | Answering what an agent waits on, in its chat: permissions (Allow, the "always" choice when the hub offers one, Deny), questions (single and multiple choice), plans (Approve, or Keep planning with what to change), terminal dialogs (their options). MCP forms and typed answers say to use the web app or the phone; a card answered elsewhere goes away. |
| To-dos | Open and done to-dos, a new one from the field at the top (Enter), Mark done / Reopen, notes, the stage and pull request the hub tracked, the agents on it (each opens its chat), Delete after asking, and Start an agent for it. Read again on the hub's `todos` event. |
| New agent | From the agents list's + or a to-do: an online computer, a folder (typed, from the folders agents started in lately, or browsed through its subfolders and up), the computer's launcher (its default chosen first) and an optional first message; `POST /api/v1/sessions` (with `todo_id` from a to-do) and its chat opens. |
| Knowledge | The knowledge base by kind (projects, chats, people, docs, meetings, events, recordings), ranked as the hub ranks it (importance, relevance, last update), filtered by tag, and searched across every kind (`/api/v1/kb-search`). An entry shows its importance, relevance and tags, where the search's words appear, the agents on a project, its profile, timeline, recent messages, posts and related entries (each opens), and its folder. Tagging (importance, relevance, tags, a project's kind and status) is saved with `PUT /api/v1/kb/{kind}/{id}`; Ask Persona sends the entry as context to the main chat, as the web app does. |
| Inbox | Entries to handle and handled ones, the assistant's summary and why, Mark handled / Reopen, and Open in the connector's app. |
| Computers | Each computer: online or not, system, processor, memory, agents, Claude Code's version, last seen, errors. |
| Settings | The hubs (open, remove, add), notifications, open at login, the global shortcut, signing out (which revokes this device on the hub). |
| Notifications | The hub's `notify` events (needs you, finished, the assistant's alerts), quiet hours honored (`quiet` posts silently), none for the chat already in front; a click opens that chat. |
| Badge and tray | The needs-you count on the Dock / taskbar icon and the tray icon, with a tray line that opens the agents. Closing the window hides it; the app keeps running in the menu bar / notification area with its connection open. Linux needs `libayatana-appindicator3` for the tray; without it, closing the window quits. |
| Menus | Settings (⌘, / Ctrl+,), Go (Persona, Agents, To-dos, Inbox, Knowledge, Computers on ⌘1–⌘6 / Ctrl+1–6, Back, Forward), Hub (the hubs, Manage Hubs ⌘⇧H), View (zoom, full screen), Edit and Window. |
| The rest | ⌥⌘P / Ctrl+Alt+P shows or hides the window from any app; one instance (a second launch brings the first forward); `persona://pair` links sign in; the window's size and place remembered; open at login. |

Later stages, in the web app's order of use: Inbox replies and drafts, MCP forms, images and files in
messages, the command palette, and the rest of Settings (devices, extensions, connectors, models). Until then those
are in the web app and the phone apps, which work with the same hub at the same time.

## How it works

- `desktop/main.go` and `desktop/menu.go` are the platform: the window (its content is `ui.View(app.View)`), tray,
  menus, notifications, badge, secrets, global shortcut, single instance and `persona://` links. They implement
  `app.Host` and run the app's callbacks on the main thread (`mygo.RunOnMain`).
- `desktop/internal/app` is the interface: the sign-in, the sidebar with a `ui.Router` of pages, and the pages
  (`agents.go`, `chat.go`, `cards.go`, `inbox.go`, `machines.go`, `settings.go`). It changes its state only on the UI
  thread; network work runs in goroutines that hand their result back with `Host.Post`. The colors are the web
  app's (`web/src/styles.css`), light and dark following the system; the icons are the web app's
  (`web/src/ui/icons.tsx`), as SVG files.
- `desktop/internal/hubapi` is the hub client: the wire types (only the fields the app reads), REST calls with
  `X-Persona-Op-Id` on writes, the pinned TLS transport and the WebSocket (`github.com/coder/websocket`, as the
  server uses). It imports no MyGo.
- `desktop/internal/store` is one hub's state (computers, agents, cards, transcripts, the assistant, the Inbox) and
  how each WebSocket event changes it, plus what the pages derive (groups, needs-you count). Plain data, no locks.
- `desktop/internal/hubs` is the list of hubs and the app's settings (`hubs.json` in the app's data directory, no
  secrets), link parsing and the pin check; `desktop/internal/l10n` the app's texts.

## Pinned TLS

A hub with TLS of its own uses a self-signed certificate. The apps' pairing link carries the hub's public-key
fingerprint (`fp`, SHA-256 of the SubjectPublicKeyInfo, base64url) and its TLS port (`tls`); the app then talks to
`https://<host of url>:<tls>` and accepts a certificate the system doesn't trust only when its host is that hub's and
its key has that fingerprint (`hubs.Hub.Pinned`, `hubapi.TLSConfig`). A certificate the system trusts (a hub behind a
reverse proxy with a public certificate) is accepted as usual. Initial trust comes only from the pairing link, never
from what the hub answers (`docs/operations/network.md`, "TLS and public-key pinning"). A browser sign-in link or an
address works for a hub whose certificate the system trusts, or a plain-HTTP hub on the LAN.

## Languages

The app's texts are in `desktop/internal/l10n/locales/` in en, zh-Hans and ja, chosen by the system's preferred
languages; requests send it as `Accept-Language` and `client_hello.locale`, so the hub's own texts (errors,
notifications) match. `l10n_test.go` checks that the three have the same keys and placeholders, that every key the
code uses exists and that every key is used.

## Build and check

Go 1.27.3 or later (MyGo's minimum; `GOTOOLCHAIN=auto` fetches it).

```sh
cd desktop
go run .                                            # a development run
go run github.com/egoist/mygo/cmd/mygo@v0.8.3 build # packaged apps in dist/: .app + .dmg, installer, .deb
scripts/ci/desktop.sh                               # from the repository root: gofmt, vet, tests, every platform
```

The view tests (`desktop/internal/app/app_test.go`) run the window without a screen (`ui.NewTester`) against the
repository's mock hub (`scripts/mock/mock_hub.py`, started by the test; skipped without python3): signing in, the
pages, answering a question, a plan and a permission, sending, the Inbox, signing out and notifications, in en,
zh-Hans and ja. With `DESKTOP_SHOTS=<dir>` (or `PERSONA_DESKTOP_SHOTS` for `go test`) they write screenshots of each
page, light and dark, to check by eye.

`mygo build` reads `desktop/mygo.json` (name, identifier, the `persona` URL scheme, `linux.webview: false`) and
`desktop/resources/icon.png`. Signing, notarization and auto-updates are MyGo's (its `distribution.md` and
`updates.md`); they need keys this repository doesn't hold and are not set up yet. `scripts/ci/desktop.sh` is
portable but not yet a lane of hosted CI.

## Limits

- On macOS the native Mac app (`apple/Mac/`) also handles `persona://`; with both installed the system picks one.
  The desktop app is meant for Windows and Linux first, and for Macs without the native app.
- The identifier in `desktop/mygo.json` is a placeholder for local builds; a published build takes its identifier
  and signing from private config, like the other apps.
- Push without the app running is the phone apps'; the desktop app notifies while it runs (in the tray).
