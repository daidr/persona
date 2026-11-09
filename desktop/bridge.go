package main

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/nwverse/persona/desktop/internal/hubs"
	"github.com/nwverse/persona/desktop/internal/l10n"
)

var errNotAllowed = errors.New("not allowed from this page")

// notificationClick tells the hub's page that the user clicked its notification, by the id the page gave it: the
// page's own Notification.onclick runs (preload.js), which opens the agent or the card, as in a browser.
var notificationClick = mygo.NewEvent[string]("personaNotificationClick")

// notifications are the system notifications the hub's page posted, by id (the web app's tag).
type notifications struct {
	mu   sync.Mutex
	byID map[string]*mygo.Notification
}

func (n *notifications) put(id string, note *mygo.Notification) (old *mygo.Notification) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.byID == nil {
		n.byID = map[string]*mygo.Notification{}
	}
	old = n.byID[id]
	n.byID[id] = note
	return old
}

func (n *notifications) take(id string, only *mygo.Notification) *mygo.Notification {
	n.mu.Lock()
	defer n.mu.Unlock()
	note := n.byID[id]
	if note == nil || (only != nil && note != only) {
		return nil
	}
	delete(n.byID, id)
	return note
}

// Persona is what the hub's page may call: the Notification API of the page, bridged to the system's (preload.js).
// Only the page of the hub the main window shows may call it; MyGo already lets no other site call Go.
type Persona struct{ d *desktop }

// NotificationRequest is a notification the page posts: the web app's `new Notification(title, {body, tag, silent})`.
type NotificationRequest struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Silent bool   `json:"silent"`
}

func (p *Persona) hubPage(ctx context.Context) (*mygo.Page, bool) {
	page := mygo.CallerPage(ctx)
	if page == nil {
		return nil, false
	}
	h, ok := p.d.currentHub()
	return page, ok && h.Contains(page.URL())
}

// Notify shows a system notification, replacing the one with the same id. It returns "denied" when the system
// refused it (macOS, by the user's answer or System Settings), which the page then reads as the permission.
func (p *Persona) Notify(ctx context.Context, req NotificationRequest) (string, error) {
	page, ok := p.hubPage(ctx)
	if !ok {
		return "", errNotAllowed
	}
	if req.ID == "" {
		req.ID = time.Now().Format(time.RFC3339Nano)
	}
	note := mygo.NewNotification(mygo.NotificationOptions{
		ID:     "persona/" + req.ID,
		Group:  "persona",
		Title:  req.Title,
		Body:   req.Body,
		Silent: req.Silent,
	})
	id := req.ID
	note.OnClick(func() {
		p.d.show()
		if err := notificationClick.EmitPage(page, id); err != nil {
			log.Println("notification click:", err)
		}
		if n := p.d.notes.take(id, note); n != nil {
			n.Close()
		}
	})
	if old := p.d.notes.put(id, note); old != nil {
		old.Close()
	}
	showCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	err := note.ShowContext(showCtx)
	if errors.Is(err, mygo.ErrNotificationsDenied) {
		p.d.notes.take(id, note)
		p.d.setNotificationsDenied(true)
		return "denied", nil
	}
	if err != nil {
		p.d.notes.take(id, note)
		return "", err
	}
	return "granted", nil
}

// Close takes back the notification with the id, as the page's Notification.close() does.
func (p *Persona) Close(ctx context.Context, id string) error {
	if _, ok := p.hubPage(ctx); !ok {
		return errNotAllowed
	}
	if n := p.d.notes.take(id, nil); n != nil {
		n.Close()
	}
	return nil
}

// RequestPermission answers Notification.requestPermission(). The system asks the user with the first
// notification (macOS) or never (Windows, Linux), so the page is told "granted" and a refusal shows up then.
func (p *Persona) RequestPermission(ctx context.Context) (string, error) {
	if _, ok := p.hubPage(ctx); !ok {
		return "", errNotAllowed
	}
	if !mygo.NotificationsSupported() {
		return "denied", nil
	}
	p.d.setNotificationsDenied(false)
	return "granted", nil
}

func (d *desktop) setNotificationsDenied(denied bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.store.Settings.NotificationsDenied == denied {
		return
	}
	d.store.Settings.NotificationsDenied = denied
	if err := d.store.Save(); err != nil {
		log.Println("saving settings:", err)
	}
}

// Connect is what the app's connect page calls: the list of hubs and the app's settings. Only the app's own pages
// may call it, not a hub's.
type Connect struct{ d *desktop }

// HubView is a hub as the connect page lists it.
type HubView struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Address string `json:"address"`
	Pinned  bool   `json:"pinned"`
	Current bool   `json:"current"`
}

// ConnectState is everything the connect page shows.
type ConnectState struct {
	Lang        string            `json:"lang"`
	Strings     map[string]string `json:"strings"`
	Hubs        []HubView         `json:"hubs"`
	Current     string            `json:"current"`
	OpenAtLogin bool              `json:"openAtLogin"`
	Shortcut    string            `json:"shortcut"`
	ShortcutOn  bool              `json:"shortcutOn"`
	Version     string            `json:"version"`
}

func appCaller(ctx context.Context) bool {
	page := mygo.CallerPage(ctx)
	return page != nil && isAppURL(page.URL())
}

// State returns what the connect page shows.
func (c *Connect) State(ctx context.Context) (ConnectState, error) {
	if !appCaller(ctx) {
		return ConnectState{}, errNotAllowed
	}
	d := c.d
	d.mu.Lock()
	defer d.mu.Unlock()
	st := ConnectState{
		Lang:        d.lang,
		Strings:     l10n.Catalog(d.lang),
		Current:     d.store.Settings.Current,
		OpenAtLogin: mygo.App.OpenAtLogin(),
		Shortcut:    shortcutLabel(),
		ShortcutOn:  !d.store.Settings.NoShortcut,
		Version:     mygo.App.Version(),
		Hubs:        []HubView{},
	}
	for _, h := range d.store.Hubs {
		addr := h.URL
		if h.Title() == addr {
			addr = ""
		}
		st.Hubs = append(st.Hubs, HubView{URL: h.URL, Title: h.Title(), Address: addr, Pinned: h.FP != "", Current: h.URL == st.Current})
	}
	return st, nil
}

// Add reads what the user typed or pasted (an address, a pairing link or a sign-in link), adds the hub and opens
// it. It returns a message in the user's language when it can't.
func (c *Connect) Add(ctx context.Context, input, label string) (string, error) {
	if !appCaller(ctx) {
		return "", errNotAllowed
	}
	in, err := hubs.ParseInput(input)
	if err != nil {
		return c.d.tr(inputError(err)), nil
	}
	in.Hub.Label = label
	go c.d.openHub(in.Hub, in.Hub.OpenURL(in.Token))
	return "", nil
}

func inputError(err error) string {
	switch {
	case errors.Is(err, hubs.ErrEmpty):
		return "errorEmpty"
	case errors.Is(err, hubs.ErrMachineLink):
		return "errorMachineLink"
	case errors.Is(err, hubs.ErrPinNoPort):
		return "errorPinNoPort"
	case errors.Is(err, hubs.ErrNoToken):
		return "errorNoToken"
	}
	return "errorAddress"
}

// Open opens a hub of the list.
func (c *Connect) Open(ctx context.Context, rawURL string) error {
	if !appCaller(ctx) {
		return errNotAllowed
	}
	c.d.mu.Lock()
	h, ok := c.d.store.Get(rawURL)
	c.d.mu.Unlock()
	if !ok {
		return errors.New("no such hub")
	}
	go c.d.openHub(h, "")
	return nil
}

// Remove takes a hub off the list. Its sign-in stays in the webview's storage of its origin, as a browser keeps it.
func (c *Connect) Remove(ctx context.Context, rawURL string) error {
	if !appCaller(ctx) {
		return errNotAllowed
	}
	c.d.mu.Lock()
	c.d.store.Remove(rawURL)
	err := c.d.store.Save()
	c.d.mu.Unlock()
	c.d.refreshMenus()
	return err
}

// Rename gives a hub the user's own name; "" goes back to its address.
func (c *Connect) Rename(ctx context.Context, rawURL, label string) error {
	if !appCaller(ctx) {
		return errNotAllowed
	}
	c.d.mu.Lock()
	c.d.store.Rename(rawURL, label)
	err := c.d.store.Save()
	c.d.mu.Unlock()
	c.d.refreshMenus()
	return err
}

// SetOpenAtLogin starts the app when the user logs in, or stops it.
func (c *Connect) SetOpenAtLogin(ctx context.Context, open bool) error {
	if !appCaller(ctx) {
		return errNotAllowed
	}
	err := mygo.App.SetOpenAtLogin(open)
	c.d.refreshMenus()
	return err
}

// SetShortcut turns the global shortcut on or off.
func (c *Connect) SetShortcut(ctx context.Context, on bool) error {
	if !appCaller(ctx) {
		return errNotAllowed
	}
	c.d.mu.Lock()
	c.d.store.Settings.NoShortcut = !on
	err := c.d.store.Save()
	c.d.mu.Unlock()
	c.d.applyShortcut()
	return err
}
