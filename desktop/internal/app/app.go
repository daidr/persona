// Package app is Persona's desktop interface, drawn with MyGo's native UI (package ui): the sign-in, the sidebar
// and the pages, and the connection to the hub behind them. It talks to the platform only through Host, so its
// views run in tests without a window.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"runtime"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/nwverse/persona/desktop/internal/hubapi"
	"github.com/nwverse/persona/desktop/internal/hubs"
	"github.com/nwverse/persona/desktop/internal/l10n"
	"github.com/nwverse/persona/desktop/internal/store"
)

// Notification is one the app asks the system to post.
type Notification struct {
	ID, Group, Title, Body string
	Silent                 bool
	// Open is the page a click on it opens.
	Open string
}

// Host is what the app needs from the platform. Every method is called on the UI thread.
type Host interface {
	// Post runs fn on the UI thread and draws a new frame; any goroutine may call it.
	Post(fn func())
	Notify(n Notification)
	SetBadge(n int)
	OpenURL(url string)
	// Focused tells whether the window has the user's input, so notifications for what is on screen are skipped.
	Focused() bool

	Secret(key string) (string, error)
	SetSecret(key, value string) error
	DeleteSecret(key string)

	OpenAtLogin() bool
	SetOpenAtLogin(on bool) error
	// Shortcut is the global shortcut's label, "" where there is none.
	Shortcut() string
	SetShortcut(on bool)
	// HubsChanged says the list of hubs or the one open changed, for the menus.
	HubsChanged()
	// PickFiles asks for files with the system's dialog and gives their paths to done on the main thread; it doesn't
	// call done when the user cancels.
	PickFiles(title string, done func(paths []string))
	// SaveFile asks where to save a file named name and gives the path to done on the main thread; it doesn't call
	// done when the person cancels.
	SaveFile(title, name string, done func(path string))
}

// App is the whole interface's state.
type App struct {
	Host    Host
	Hubs    *hubs.Store
	Lang    string
	Version string
	// Go runs background work: a goroutine in the app, inline in tests.
	Go func(func())
	// After runs fn on the main thread after d; tests run it at once.
	After func(d time.Duration, fn func())
	// RunSocket keeps the hub's WebSocket open until ctx ends; tests leave it out.
	RunSocket func(ctx context.Context, s *hubapi.Socket)

	router *ui.Router
	md     markdown.Options
	conn   *conn

	connect connectForm
	agents  agentsPage
	palette paletteState
	chats   map[string]*chatState
	// images are the chats' pictures, decoded, by item id.
	images    map[string]ui.ImageSource
	inbox     inboxPage
	sets      settingsPage
	routines  routinesPage
	act       agentAction
	todos     todosPage
	kb        kbPage
	terms     map[string]*termState
	gits      map[string]*changesState
	fileViews map[string]*filesState
	cards     map[string]*cardState
	persona   personaState
	start     startForm
	badge     int
	toasts    []string
}

// conn is the live connection to the hub shown.
type conn struct {
	hub    hubs.Hub
	client *hubapi.Client
	socket *hubapi.Socket
	data   *store.Store
	cancel context.CancelFunc
	status hubapi.Status
	loaded bool
	err    string
	// watching is the chat the socket follows.
	watching string
}

// New makes the app's state. Start connects to the hub opened last.
func New(host Host, list *hubs.Store, lang, version string) *App {
	a := &App{Host: host, Hubs: list, Lang: lang, Version: version, router: ui.NewRouter("/"), chats: map[string]*chatState{}, images: map[string]ui.ImageSource{}}
	a.Go = func(fn func()) { go fn() }
	a.RunSocket = func(ctx context.Context, s *hubapi.Socket) { go s.Run(ctx) }
	a.After = func(d time.Duration, fn func()) { time.AfterFunc(d, func() { a.Host.Post(fn) }) }
	a.router.Transition = ui.TransitionNone
	if a.Hubs.Settings.DeviceID == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		a.Hubs.Settings.DeviceID = hex.EncodeToString(b)
		_ = a.Hubs.Save()
	}
	return a
}

func (a *App) tr(key string, args ...string) string { return l10n.Tr(a.Lang, key, args...) }

// Start opens the hub opened last, when it has a sign-in.
func (a *App) Start() {
	if h, ok := a.Hubs.Get(a.Hubs.Settings.Current); ok {
		if tok, err := a.Host.Secret(secretKey(h)); err == nil && tok != "" {
			a.open(h, tok)
		}
	}
}

// Stop closes the connection.
func (a *App) Stop() {
	if a.conn != nil {
		a.conn.cancel()
		a.conn = nil
	}
}

func secretKey(h hubs.Hub) string { return "hub-token:" + h.URL }

// Navigate shows a page: "/", "/agents", "/agents/<machine>/<session>", "/inbox", "/machines", "/settings".
func (a *App) Navigate(path string) {
	if path != a.router.Path() {
		a.router.Push(path)
	}
}

// Path is the page shown.
func (a *App) Path() string { return a.router.Path() }

// Back and Forward go through the pages' history.
func (a *App) Back()    { a.router.Back() }
func (a *App) Forward() { a.router.Forward() }

// SignedIn tells whether a hub is open.
func (a *App) SignedIn() bool { return a.conn != nil }

// HubTitle is the open hub's name, or "".
func (a *App) HubTitle() string {
	if a.conn == nil {
		return ""
	}
	if a.conn.hub.Label == "" && a.conn.data.Hello.HubName != "" {
		return a.conn.data.Hello.HubName
	}
	return a.conn.hub.Title()
}

// NeedsYou counts the agents waiting on the user.
func (a *App) NeedsYou() int {
	if a.conn == nil {
		return 0
	}
	return a.conn.data.NeedsYou()
}

// DeviceName is how the hub lists this app in Settings › Devices.
func DeviceName() string {
	os := map[string]string{"darwin": "Mac", "windows": "Windows", "linux": "Linux"}[runtime.GOOS]
	if os == "" {
		os = runtime.GOOS
	}
	return "Persona Desktop · " + os
}

// SignIn adds a hub from what the user pasted, exchanging its token for this device's own.
func (a *App) SignIn(raw, label, token string, done func(error)) {
	in, err := hubs.ParseInput(raw)
	if err == nil && in.Token == "" {
		in.Token = token
	}
	if err == nil && in.Token == "" {
		err = errNeedToken
	}
	if err != nil {
		done(err)
		return
	}
	in.Hub.Label = label
	a.Go(func() {
		c := hubapi.New(in.Hub, in.Token, a.Lang)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tok, err := c.Exchange(ctx, DeviceName())
		a.Host.Post(func() {
			if err != nil {
				done(err)
				return
			}
			h := a.Hubs.Upsert(in.Hub)
			if err := a.Host.SetSecret(secretKey(h), tok); err != nil {
				done(err)
				return
			}
			a.open(h, tok)
			a.Navigate("/")
			done(nil)
		})
	})
}

var errNeedToken = errors.New("need token")

// Switch opens another hub of the list.
func (a *App) Switch(url string) {
	h, ok := a.Hubs.Get(url)
	if !ok {
		return
	}
	tok, err := a.Host.Secret(secretKey(h))
	if err != nil || tok == "" {
		a.Stop()
		a.connect = connectForm{input: h.URL, label: h.Label, needToken: h.FP == ""}
		a.Hubs.Settings.Current = ""
		_ = a.Hubs.Save()
		return
	}
	a.open(h, tok)
	a.Navigate("/")
}

// SignOut revokes this device on the hub (when it answers) and forgets the token.
func (a *App) SignOut() {
	if a.conn == nil {
		return
	}
	c := a.conn
	a.Host.DeleteSecret(secretKey(c.hub))
	a.Stop()
	a.Hubs.Settings.Current = ""
	_ = a.Hubs.Save()
	a.chats = map[string]*chatState{}
	a.updateBadge()
	a.Host.HubsChanged()
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.client.SignOut(ctx)
	})
}

// RemoveHub takes a hub off the list, signing out of it when it is open.
func (a *App) RemoveHub(url string) {
	if a.conn != nil && a.conn.hub.URL == url {
		a.SignOut()
	} else if h, ok := a.Hubs.Get(url); ok {
		a.Host.DeleteSecret(secretKey(h))
	}
	a.Hubs.Remove(url)
	_ = a.Hubs.Save()
	a.Host.HubsChanged()
}

func (a *App) open(h hubs.Hub, token string) {
	a.Stop()
	h.Opened = time.Now()
	h = a.Hubs.Upsert(h)
	a.Hubs.Settings.Current = h.URL
	_ = a.Hubs.Save()
	a.Host.HubsChanged()
	ctx, cancel := context.WithCancel(context.Background())
	c := &conn{hub: h, client: hubapi.New(h, token, a.Lang), data: store.New(), cancel: cancel}
	c.socket = &hubapi.Socket{Client: c.client, DeviceID: a.Hubs.Settings.DeviceID}
	c.socket.OnEvent = func(ev hubapi.Event) { a.Host.Post(func() { a.apply(c, ev) }) }
	c.socket.OnStatus = func(st hubapi.Status) { a.Host.Post(func() { a.setStatus(c, st) }) }
	a.conn = c
	a.chats = map[string]*chatState{}
	a.refresh(c)
	a.RunSocket(ctx, c.socket)
}

// live tells whether c is still the connection shown, for work that finishes after a switch.
func (a *App) live(c *conn) bool { return a.conn == c }

func (a *App) setStatus(c *conn, st hubapi.Status) {
	if !a.live(c) {
		return
	}
	c.status = st
	if errors.Is(st.Err, hubapi.ErrUnauthorized) {
		a.signedOutByHub(c)
	}
}

// signedOutByHub handles a token the hub no longer takes: back to the sign-in, with the hub's address filled in.
func (a *App) signedOutByHub(c *conn) {
	h := c.hub
	a.Host.DeleteSecret(secretKey(h))
	a.Stop()
	a.Hubs.Settings.Current = ""
	_ = a.Hubs.Save()
	a.connect = connectForm{input: h.URL, label: h.Label, err: a.tr("signedOut"), needToken: true}
	a.updateBadge()
	a.Host.HubsChanged()
}

// refresh reads everything the pages show; the socket keeps it current after.
func (a *App) refresh(c *conn) {
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		machines, err := c.client.Machines(ctx)
		if err != nil {
			a.Host.Post(func() { a.failed(c, err) })
			return
		}
		sessions := map[string][]hubapi.Session{}
		for _, m := range machines {
			if list, err := c.client.Sessions(ctx, m.ID); err == nil {
				sessions[m.ID] = list
			}
		}
		ixs, _ := c.client.Interactions(ctx)
		as, _ := c.client.Assistant(ctx)
		inbox, inboxErr := c.client.Inbox(ctx)
		todos, todosErr := c.client.Todos(ctx)
		pins, pinsErr := c.client.Pins(ctx)
		a.Host.Post(func() {
			if !a.live(c) {
				return
			}
			c.data.SetMachines(machines)
			for id, list := range sessions {
				c.data.SetSessions(id, list)
			}
			c.data.SetInteractions(ixs)
			c.data.Assistant = as
			if inboxErr == nil {
				c.data.SetInbox(inbox)
			}
			if todosErr == nil {
				c.data.SetTodos(todos)
			}
			if pinsErr == nil {
				c.data.Pins, c.data.PinsLoaded = pins, true
			}
			c.loaded, c.err = true, ""
			a.updateBadge()
		})
	})
}

func (a *App) failed(c *conn, err error) {
	if !a.live(c) {
		return
	}
	if errors.Is(err, hubapi.ErrUnauthorized) {
		a.signedOutByHub(c)
		return
	}
	c.err = err.Error()
}

func (a *App) refreshInbox(c *conn) {
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		list, err := c.client.Inbox(ctx)
		if err != nil {
			return
		}
		a.Host.Post(func() {
			if a.live(c) {
				c.data.SetInbox(list)
			}
		})
	})
}

func (a *App) refreshTodos(c *conn) {
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		list, err := c.client.Todos(ctx)
		if err != nil {
			return
		}
		a.Host.Post(func() {
			if a.live(c) {
				c.data.SetTodos(list)
			}
		})
	})
}

func (a *App) refreshAssistant(c *conn) {
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		as, err := c.client.Assistant(ctx)
		if err != nil {
			return
		}
		a.Host.Post(func() {
			if a.live(c) {
				c.data.Assistant = as
			}
		})
	})
}

func (a *App) apply(c *conn, ev hubapi.Event) {
	if !a.live(c) {
		return
	}
	fx := c.data.Apply(ev)
	if name := c.data.Hello.HubName; ev.Type == "hello" && name != "" && name != c.hub.Name {
		c.hub.Name = name
		c.hub = a.Hubs.Upsert(c.hub)
		_ = a.Hubs.Save()
		a.Host.HubsChanged()
	}
	if ev.Type == "hello" && c.loaded {
		// back after a drop: what changed meanwhile is read again
		a.refresh(c)
	}
	if fx.Inbox && ev.Type != "hello" {
		a.refreshInbox(c)
	}
	if fx.Assistant && ev.Type != "hello" {
		a.refreshAssistant(c)
	}
	if fx.Todos && ev.Type != "hello" {
		a.refreshTodos(c)
	}
	if fx.Schedule {
		a.routines.load.asked = false
	}
	if fx.Notify != nil {
		a.notify(c, *fx.Notify)
	}
	a.updateBadge()
}

func (a *App) notify(c *conn, n store.Notify) {
	if a.Hubs.Settings.NoNotifications {
		return
	}
	open := "/agents"
	if n.MachineID != "" && n.SessionID != "" {
		open = "/agents/" + n.MachineID + "/" + n.SessionID
		if main, ok := c.data.MainConversation(); ok && main.MachineID == n.MachineID && main.SessionID == n.SessionID {
			open = "/"
		}
	}
	if a.Host.Focused() && a.router.Path() == open {
		return
	}
	title := n.Title
	if n.Subtitle != "" {
		title += " · " + n.Subtitle
	}
	id := n.InteractionID
	if id == "" {
		id = n.ID
	}
	if id == "" {
		id = n.Kind + ":" + n.MachineID + "/" + n.SessionID
	}
	a.Host.Notify(Notification{ID: id, Group: n.MachineID + "/" + n.SessionID, Title: title, Body: n.Body, Silent: n.Quiet, Open: open})
}

func (a *App) updateBadge() {
	n := a.NeedsYou()
	if n != a.badge {
		a.badge = n
		a.Host.SetBadge(n)
	}
}

// RetryNow reconnects at once.
func (a *App) RetryNow() {
	if a.conn != nil {
		a.conn.socket.RetryNow()
		if !a.conn.loaded {
			a.refresh(a.conn)
		}
	}
}
