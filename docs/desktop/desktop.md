# Desktop app (`desktop/`)

Persona for macOS, Windows and Linux, built with [MyGo](https://github.com/egoist/mygo), a Go desktop framework
that needs no cgo. The window is drawn with MyGo's **native UI** (package `ui`, on the GPU: Metal, Direct3D 11,
OpenGL), not a webview: the app talks to the hub's HTTP and WebSocket API itself, as the phone apps do
(`docs/protocol/protocol.md`). It is one Go binary; on Linux it needs GTK 3 alone and on Windows no WebView2.

## What it has

It covers the web app's everyday features:

| | |
| --- | --- |
| Sign-in | The apps' pairing link (`persona://pair?…`, from **Settings › Devices › Connect a new device** on a signed-in device, or `personad hub pair`), a browser sign-in link (`<hub>/#token=…`), or an address and a token. The token it carries is traded once for this device's own (`POST /api/v1/auth/tokens`, `kind: desktop`, named "Persona Desktop · Mac / Windows / Linux" in Settings › Devices), which goes into the system's credential store (Keychain, Credential Manager, Secret Service), one per hub. Several hubs, switched from the Hub menu, the tray or Settings. |
| Pinned TLS | A pairing link's `fp` pins the hub's public key (below). |
| Live connection | The WebSocket (`/api/v1/ws`) with `client_hello`, app-level pings, reconnecting with backoff (0.6 s to 15 s) and reading everything again after a drop; the sidebar's foot says online, connecting or offline with Retry. A token the hub refuses goes back to the sign-in with the hub filled in. |
| Persona | The assistant's chats beside the one open: the main chat first, then the others by their last activity, archived ones apart (Archive / Unarchive), searched by title and last line; a new chat starts with its first message (`POST /api/v1/assistant/conversations`). A chat shows its transcript (Markdown, streaming replies as they are written), sending, and its cards. |
| Cards in replies | `persona-view` blocks in the assistant's replies (`docs/assistant/assistant-views.md`) drawn natively: text, Markdown, header, rows with badges, buttons, links, metrics, stat grids, progress, tables, code, dividers and empty states, at most three a reply. A component the desktop app doesn't draw yet shows the card's fallback below it. Actions: a reply goes through `…/views/{key}/actions` (a hub without card actions gets the reply as a message), `open` opens an https page or an agent / to-do in the app, `push` / `pop` move through the card's pages, and `confirm` asks first. Read-only while the reply is still being written. |
| Persona's work in the chat | The assistant's records drawn where they happened, each before the first message you wrote after it (`GET /api/v1/assistant/conversations/{id}/feed`, `assistant_feed` and `task_plan` events): task plans with each task's computer, folder and model (Start, Dismiss), started tasks, Today's line-up (`…/today`, once a day in the main chat), briefs (waiting for you, failed, done, to verify with Accept, questions with Handled, in progress), receipts, answered and routed lines, and messages. |
| Readiness | Above the main chat's composer while something still blocks it (`GET /api/v1/assistant/readiness`): the first unmet step (a computer, a model, a provider) with its action (Computers, Settings › Models, Check again with `?probe=1`, or the hub's admin page), a note when only chatting works, and Hide. |
| Profile | The spark button above the conversations: where Persona runs and whether that computer is online, what it does on its own next (the scheduled tasks' next runs) and what it can reach. |
| Modes | Under a Claude Code agent's composer: its permission mode, model (from the launcher's models, `/machines/{mid}/launchers/{name}/models`) and effort with ultracode, each changed in place (`POST …/modes`) and put back when the hub refuses; the model waits until the agent is idle. |
| Overview | What needs you first: the open cards, answered in place, with the agent each belongs to (Open goes to its chat); agents waiting without a card; up to six open to-dos (See all); the agents working; the ones done but not seen, with Mark all as seen; and the computers, online or not. The page's line counts what waits on you or what works. |
| Scheduled | The assistant's scheduled tasks (`/api/v1/assistant/tasks`): when each runs in words (once at a time, daily at a time on some weekdays, or every N minutes), its next run, last run with its status and error, its switch, Run now (which opens the chat it ran in), Edit, Open chat and Delete after asking; New task and Edit share a dialog (title, what to do, once / daily / interval with their time, weekdays or minutes). Recent runs below, each opening its chat. Read again on the hub's `assistant_schedule` event. |
| Agents | Every computer's agents grouped as the other clients group them (needs you, working, unread, idle, offline), searchable, beside the chat of the one chosen: the transcript (text, thinking folded, tool calls on one line, errors), earlier turns as you scroll up, sending with Enter (Shift+Enter for a new line), Stop while it works, and marking it read.. Messages that another of Persona's chats, a scheduled run, a follow-up or supervision wrote show as "@where it came from" above their words, never as your bubble (`Item.source`, the transcript's `message-source` block, then `injected`); your own lose their `<persona-context>`, `<persona-file>` and attached-photo blocks, shown as chips above them. The header names the branch the agent is on, and its … menu has Pin / Unpin (the hub's pins, `GET` / `PUT /api/v1/pins`, shown in their own group on top of the list), Rename (`…/rename`), Session info (`…/info`: launcher, model, effort, permissions, folder, branch, version, transcript, turns, context), Copy session ID, End after asking (`…/close`) and Remove after asking (`DELETE /api/v1/sessions/{mid}/{sid}`); an assistant chat's menu has Rename, Session info, Copy session ID and End. |
| Images and files | The composer's paper clip or files dragged onto it: pictures (PNG, JPEG, GIF, WebP, up to 8 MB, four a message) go with the message and show as thumbnails until it is sent; other files (up to 200 MB) are sent to the agent's computer (`POST /machines/{mid}/files`) and their path, quoted when it has spaces, lands in the draft. Pictures in a chat show in it. |
| Terminal and changes | An agent's page switches between its chat, its terminal, its changes and its files. The terminal is the agent's screen as text, read again every 1.5 s while it is shown (`GET /api/v1/sessions/{mid}/{sid}/screen`), with a line to type (Enter sends it with Enter) and Esc, Tab, ⌃C, the arrows and Enter (`POST …/keys`); a session without the `terminal` capability is watched only. Changes are the agent's folder's branch, upstream, ahead / behind and last commit, its changed files with their lines added and removed, and the chosen file's diff (`git/status`, `git/diff`; a change only in the index shows the staged diff).. A branch whose upstream was deleted on the remote says so (`gone`). |
| Git | On a hub with `git_write`: each changed file's check box stages or unstages it (mixed when only part is staged), Stage all / Unstage all, a commit message and Commit, Push (asked first; a branch with no upstream is published and tracks it), Branches (find, switch, or create one), Stashes (stash with a message and optionally new files; apply, pop, drop after asking, by index and object id, never by index alone) and the branch's pull request (state, review, failing checks, Open, Merge or Merge when ready, and New pull request once every commit is pushed; `git/pr`, read through gh on that computer). A refusal shows what happened, git's own words and the files it is about. History lists the commits, older pages on demand (`git/log`), and a commit shows its message, author and whole diff with a header per file (`git/show`), with Copy hash. |
| Files | An agent's page's Files: its folder, folder by folder with a path back to each level (`GET /machines/{mid}/fs`), folders first with size, time and Git marks (a folder marked when something under it changed), hidden files when asked for. A file opens beside the list (`GET …/file`): text with line numbers and later pages on demand, Markdown rendered or as source, pictures, or no preview; Copy path, and Save a Copy… downloads it where you choose (`…/file/download`). |
| Cards | Answering what an agent waits on, in its chat: permissions (Allow, the "always" choice when the hub offers one, Deny), questions (single and multiple choice), plans (Approve, or Keep planning with what to change), terminal dialogs (their options). MCP forms (`elicitation`): the server's fields in its order as text inputs, numbers, switches, drop-downs and check boxes, required ones checked before Submit (`action: accept` with `content`), or Decline; a link to open, then Done. Typed answers say to use the web app or the phone; a card answered elsewhere goes away. |
| To-dos | Open and done to-dos, a new one from the field at the top (Enter), Mark done / Reopen, notes, the stage and pull request the hub tracked, the agents on it (each opens its chat), Delete after asking, and Start an agent for it. Read again on the hub's `todos` event. |
| New agent | From the agents list's + or a to-do: an online computer, a folder (typed, from the folders agents started in lately, or browsed through its subfolders and up), the computer's launcher (its default chosen first) and an optional first message; `POST /api/v1/sessions` (with `todo_id` from a to-do) and its chat opens.. A preset (a tier of the model profile in use, from the launchers' `tiers`) picks the launcher and sends its model and effort with the start; choosing a launcher by hand afterwards drops it. A Claude launcher also offers the permission mode, model, effort and ultracode (model and effort hidden while a preset picks them). |
| Knowledge | The knowledge base by kind (projects, chats, people, docs, meetings, events, recordings), ranked as the hub ranks it (importance, relevance, last update), filtered by tag, and searched across every kind (`/api/v1/kb-search`). An entry shows its importance, relevance and tags, where the search's words appear, the agents on a project, its profile, timeline, recent messages, posts and related entries (each opens), and its folder. Tagging (importance, relevance, tags, a project's kind and status) is saved with `PUT /api/v1/kb/{kind}/{id}`; Ask Persona sends the entry as context to the main chat, as the web app does. Confirmed facts (`pinned`) are edited in place and kept over Persona's updates; a relationship a person keeps (people, chats, projects, linked items) is removed after asking, which locks both lists against automatic changes, and both entries are read back to confirm it. |
| Inbox | Entries to handle and handled ones (connectors' entries too, `?sources=all`), the assistant's summary and why, Mark handled / Reopen, and Open in the connector's app. An entry Persona can answer has a reply box, filled with the AI twin's draft when it wrote one (and its note); Draft with Persona asks for a draft (`…/draft`, with an optional instruction); Send replies where the entry came from (`…/reply`, with a `client_id` so a retry sends once and the entry's `version`) and marks it handled. Hand to Persona sends it to the main chat (`…/handoff`); Ignore dismisses it. A mail shows its whole message (`GET /mail/messages/{account}/{uid}`, marked read when opened): who, the body (its HTML as text when it has no plain text) and attachments saved where you choose; Reply, Reply all (never to your own address) and the Inbox's New email open one dialog (from, to, cc, bcc, subject, message; `POST /mail/send`), and closing it any way but Cancel keeps what was typed. A connector's entry opens its thread (`GET /connectors/{id}/thread`): a chat app's messages as bubbles with a reply quoting one, a GitHub pull request or issue as its header, description, comments, reviews, line comments and checks with their logs, earlier pages on demand, and a box to answer in when the thread allows it (`…/thread/reply`). |
| Computers | Each computer: online or not, system, processor, memory, agents, Claude Code's version, last seen, errors. |
| Settings | General: the hubs (open, remove, add), notifications, open at login, the global shortcut, signing out (which revokes this device on the hub). Devices: the paired devices and the revoked ones, Revoke after asking, Connect a new device (a ten-minute pairing link with its QR code and Copy, saying which device used it; `/auth/tokens`, `/auth/pair-codes`). Extensions: each one's state, version, what it still needs and its switch (`/extensions`, `/plugins` on an older hub). Connectors: state, account, last sync, how to fix it, and the switch (`/connectors`). Models: each use's provider and model, changed per category or back to Default (`/model-config`). |
| Notifications | The hub's `notify` events (needs you, finished, the assistant's alerts), quiet hours honored (`quiet` posts silently), none for the chat already in front; a click opens that chat. |
| Badge and tray | The needs-you count on the Dock / taskbar icon and the tray icon, with a tray line that opens the agents. Closing the window hides it; the app keeps running in the menu bar / notification area with its connection open. Linux needs `libayatana-appindicator3` for the tray; without it, closing the window quits. |
| Command palette | ⌘K / Ctrl+K (or Go › Go to Anything…): New agent, New chat, every page, every agent, the assistant's chats, open to-dos and the other hubs, filtered by what you type (every word must match, titles that start with it first); the arrows choose, Enter goes. |
| Menus | Settings (⌘, / Ctrl+,), Go (Persona, Overview, Agents, To-dos, Inbox, Scheduled, Knowledge, Computers on ⌘1–⌘8 / Ctrl+1–8, Go to Anything ⌘K, Back, Forward), Hub (the hubs, Manage Hubs ⌘⇧H), View (zoom, full screen), Edit and Window. |
| The rest | ⌥⌘P / Ctrl+Alt+P shows or hides the window from any app; one instance (a second launch brings the first forward); `persona://pair` links sign in; the window's size and place remembered; open at login. |

Not in the desktop app: installing extensions and markets, an extension's own settings and pages, connector sign-ins,
model providers and profiles, and the web app's rarer pages (mail settings, health, workspaces). Those stay in the web
app and the phone apps, which work with the same hub at the same time.

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
