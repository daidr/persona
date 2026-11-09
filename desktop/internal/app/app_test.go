package app

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
	"github.com/nwverse/persona/desktop/internal/hubs"
)

// fakeHost runs everything at once, keeps secrets in memory and records what the app asked of the platform.
type fakeHost struct {
	secrets  map[string]string
	notes    []Notification
	badge    int
	opened   []string
	login    bool
	shortcut bool
	focused  bool
}

func newHost() *fakeHost { return &fakeHost{secrets: map[string]string{}} }

func (h *fakeHost) Post(fn func())        { fn() }
func (h *fakeHost) Notify(n Notification) { h.notes = append(h.notes, n) }
func (h *fakeHost) SetBadge(n int)        { h.badge = n }
func (h *fakeHost) OpenURL(u string)      { h.opened = append(h.opened, u) }
func (h *fakeHost) Focused() bool         { return h.focused }
func (h *fakeHost) Secret(k string) (string, error) {
	if v, ok := h.secrets[k]; ok {
		return v, nil
	}
	return "", errors.New("not found")
}
func (h *fakeHost) SetSecret(k, v string) error  { h.secrets[k] = v; return nil }
func (h *fakeHost) DeleteSecret(k string)        { delete(h.secrets, k) }
func (h *fakeHost) OpenAtLogin() bool            { return h.login }
func (h *fakeHost) SetOpenAtLogin(on bool) error { h.login = on; return nil }
func (h *fakeHost) Shortcut() string             { return "Ctrl+Alt+P" }
func (h *fakeHost) SetShortcut(on bool)          { h.shortcut = on }
func (h *fakeHost) HubsChanged()                 {}

var mockURL string

// TestMain starts the repository's mock hub (scripts/mock/mock_hub.py), which serves machines, agents, cards and
// an Inbox shaped as a real hub's, when python3 is there.
func TestMain(m *testing.M) {
	if py, err := exec.LookPath("python3"); err == nil {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err == nil {
			port := l.Addr().(*net.TCPAddr).Port
			l.Close()
			cmd := exec.Command(py, "../../../scripts/mock/mock_hub.py")
			cmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%d", port))
			if cmd.Start() == nil {
				defer cmd.Process.Kill()
				url := fmt.Sprintf("http://127.0.0.1:%d", port)
				for i := 0; i < 100; i++ {
					if r, err := http.Get(url + "/api/v1/health"); err == nil {
						r.Body.Close()
						mockURL = url
						break
					}
					time.Sleep(100 * time.Millisecond)
				}
			}
		}
	}
	code := m.Run()
	os.Exit(code)
}

// signedIn is the app signed in to the mock hub with everything read, the socket left out.
func signedIn(t *testing.T, lang string) (*App, *fakeHost) {
	t.Helper()
	if mockURL == "" {
		t.Skip("the mock hub needs python3")
	}
	host := newHost()
	a := New(host, hubs.Load(filepath.Join(t.TempDir(), "hubs.json")), lang, "1.0.0")
	a.Go = func(fn func()) { fn() }
	a.RunSocket = func(context.Context, *hubapi.Socket) {}
	var err error
	a.SignIn(mockURL, "Home", "jd_test", func(e error) { err = e })
	if err != nil {
		t.Fatal(err)
	}
	if !a.SignedIn() || !a.conn.loaded {
		t.Fatalf("not signed in: %+v", a.conn)
	}
	return a, host
}

func shot(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("PERSONA_DESKTOP_SHOTS")
	if dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestSignInPage(t *testing.T) {
	host := newHost()
	a := New(host, hubs.Load(filepath.Join(t.TempDir(), "hubs.json")), "en", "1.0.0")
	a.Go = func(fn func()) { fn() }
	a.RunSocket = func(context.Context, *hubapi.Socket) {}
	tt := ui.NewTester(a.View, 1100, 720)
	if !tt.HasText("Add a hub") || !tt.HasText("Sign in") {
		t.Fatalf("texts %q", tt.Texts())
	}
	if err := tt.Click("Sign in"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText(a.tr("errorEmpty")) {
		t.Fatalf("no error for an empty field: %q", tt.Texts())
	}
	a.connect.input = "hub.example.com:18662"
	a.connect.needToken = true
	tt.Frame()
	if !tt.HasText("Sign-in token") {
		t.Fatal("an address alone asks for a token")
	}
	shot(t, tt, "signin-light")
	tt.SetDark(true)
	shot(t, tt, "signin-dark")
}

func TestShell(t *testing.T) {
	a, host := signedIn(t, "en")
	if a.HubTitle() != "Home" || host.secrets[secretKey(a.conn.hub)] == "" {
		t.Fatalf("hub %q, secrets %v", a.HubTitle(), host.secrets)
	}
	if host.badge == 0 || host.badge != a.NeedsYou() {
		t.Fatalf("badge %d, needs you %d", host.badge, a.NeedsYou())
	}
	tt := ui.NewTester(a.View, 1280, 800)
	for _, s := range []string{"Persona", "Agents", "Inbox", "Computers", "Settings"} {
		if !tt.HasText(s) {
			t.Errorf("sidebar lacks %q", s)
		}
	}
	shot(t, tt, "persona-light")
	tt.SetDark(true)
	shot(t, tt, "persona-dark")
	tt.SetDark(false)

	if err := tt.Click("Agents"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.Path() != "/agents" || !tt.HasText("Needs you") || !tt.HasText("Working") {
		t.Fatalf("agents page %s: %q", a.Path(), tt.Texts())
	}
	shot(t, tt, "agents-light")
}

func TestAgentChatAndCards(t *testing.T) {
	a, _ := signedIn(t, "en")
	d := a.conn.data
	// the question whose agent has the fewest cards, so the whole card shows without scrolling
	var key string
	for _, ix := range d.Interactions {
		k := ix.SessionKey()
		if ix.Kind == "question" && (key == "" || len(d.SessionInteractions(k)) < len(d.SessionInteractions(key)) ||
			len(d.SessionInteractions(k)) == len(d.SessionInteractions(key)) && k < key) {
			key = k
		}
	}
	if key == "" {
		t.Fatal("the mock hub has no question card")
	}
	a.Navigate("/agents/" + key)
	tt := ui.NewTester(a.View, 1280, 860)
	tt.Frame()
	if !d.Transcript(key).Loaded {
		t.Fatal("transcript not read")
	}
	if !tt.HasText("The agent has a question") {
		t.Fatalf("no card: %q", tt.Texts())
	}
	shot(t, tt, "chat-light")
	tt.SetDark(true)
	shot(t, tt, "chat-dark")
	tt.SetDark(false)

	var q hubapi.Interaction
	for _, ix := range d.SessionInteractions(key) {
		if ix.Kind == "question" {
			q = ix
		}
	}
	if err := tt.Click(q.Questions[0].Options[0].Label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Answer"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, open := d.Interactions[q.ID]; open {
		t.Fatal("the answered card is still open")
	}

	before := len(d.Transcript(key).Items)
	a.chat(key).draft = "Use the first one"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	items := d.Transcript(key).Items
	if len(items) <= before || items[len(items)-1].Text != "Use the first one" {
		t.Fatalf("message not shown: %d items", len(items))
	}
}

func TestPlanAndPermission(t *testing.T) {
	a, _ := signedIn(t, "zh-Hans")
	d := a.conn.data
	for _, kind := range []string{"plan", "permission"} {
		var ix hubapi.Interaction
		for _, x := range d.Interactions {
			if x.Kind == kind && (ix.ID == "" || len(d.SessionInteractions(x.SessionKey())) < len(d.SessionInteractions(ix.SessionKey())) ||
				len(d.SessionInteractions(x.SessionKey())) == len(d.SessionInteractions(ix.SessionKey())) && x.ID < ix.ID) {
				ix = x
			}
		}
		if ix.ID == "" {
			continue
		}
		a.Navigate("/agents/" + ix.SessionKey())
		tt := ui.NewTester(a.View, 1280, 860)
		tt.Frame()
		shot(t, tt, "card-"+kind+"-zh")
		button := a.tr("approve")
		if kind == "permission" {
			button = a.tr("allow")
		}
		if err := tt.Click(button); err != nil {
			t.Fatalf("%s: %v (%q)", kind, err, tt.Texts())
		}
		tt.Frame()
		if _, open := d.Interactions[ix.ID]; open {
			t.Fatalf("%s card still open", kind)
		}
	}
}

func TestInboxAndComputers(t *testing.T) {
	a, _ := signedIn(t, "ja")
	d := a.conn.data
	if len(d.Inbox) == 0 {
		t.Fatal("inbox not read")
	}
	first := a.inboxItems()[0]
	a.Navigate("/inbox/" + first.ID)
	tt := ui.NewTester(a.View, 1280, 800)
	tt.Frame()
	shot(t, tt, "inbox-ja")
	if err := tt.Click(a.tr("markDone")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, it := range d.Inbox {
		if it.ID == first.ID && !it.Done() {
			t.Fatal("not marked handled")
		}
	}
	a.Navigate("/machines")
	tt.Frame()
	if !tt.HasText(d.Machines[0].Name) {
		t.Fatalf("computers: %q", tt.Texts())
	}
	shot(t, tt, "machines-ja")
	tt.SetDark(true)
	shot(t, tt, "machines-ja-dark")
}

func TestSettingsAndSignOut(t *testing.T) {
	a, host := signedIn(t, "en")
	a.Navigate("/settings")
	tt := ui.NewTester(a.View, 1100, 800)
	tt.Frame()
	shot(t, tt, "settings-light")
	if !tt.HasText("Show Persona from anywhere with Ctrl+Alt+P") {
		t.Fatalf("%q", tt.Texts())
	}
	if err := tt.Click("Sign out"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.SignedIn() || len(host.secrets) != 0 || !tt.HasText("Add a hub") {
		t.Fatalf("still signed in: %v %v", a.SignedIn(), host.secrets)
	}
}

func TestNotify(t *testing.T) {
	a, host := signedIn(t, "en")
	ev := hubapi.Event{Type: "notify", Raw: []byte(`{"type":"notify","kind":"done","machine_id":"m","session_id":"s","title":"Fix tests","body":"Done"}`)}
	a.apply(a.conn, ev)
	if len(host.notes) != 1 || host.notes[0].Open != "/agents/m/s" || !strings.Contains(host.notes[0].Title, "Fix tests") {
		t.Fatalf("%+v", host.notes)
	}
	a.Hubs.Settings.NoNotifications = true
	a.apply(a.conn, ev)
	if len(host.notes) != 1 {
		t.Fatal("notified with notifications off")
	}
}

func TestTodosAndStart(t *testing.T) {
	a, _ := signedIn(t, "en")
	d := a.conn.data
	if !d.TodosLoaded || len(d.Todos) == 0 {
		t.Fatal("to-dos not read")
	}
	a.Navigate("/todos")
	tt := ui.NewTester(a.View, 1280, 800)
	tt.Frame()
	if !tt.HasText(a.tr("todosOpen")) {
		t.Fatalf("to-dos: %q", tt.Texts())
	}
	// a new to-do opens once the hub has it
	a.todos.draft = "Write the release notes"
	a.addTodo()
	tt.Frame()
	if a.Path() == "/todos" || !tt.HasText("Write the release notes") {
		t.Fatalf("new to-do at %s: %q", a.Path(), tt.Texts())
	}
	id := a.Path()[len("/todos/"):]
	if err := tt.Click(a.tr("todoMarkDone")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if x, ok := d.Todo(id); !ok || !x.Done() {
		t.Fatalf("not done: %+v", x)
	}
	if err := tt.Click(a.tr("reopen")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	// a to-do an agent worked on
	var linked hubapi.Todo
	for _, x := range d.Todos {
		if len(x.Agents) > 0 && !x.Done() {
			linked = x
			break
		}
	}
	if linked.ID != "" {
		a.Navigate("/todos/" + linked.ID)
		tt.Frame()
		shot(t, tt, "todo-light")
		tt.SetDark(true)
		shot(t, tt, "todo-dark")
		tt.SetDark(false)
	}

	// New agent for the to-do: a computer, a folder from its list, Start
	a.Navigate("/todos/" + id)
	tt.Frame()
	if err := tt.Click(a.tr("startAgent")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	f := &a.start
	if !f.open || f.machine == "" || f.cwd == "" || len(f.launchers) == 0 || f.launcher == "" {
		t.Fatalf("start form: %+v", f)
	}
	if !tt.HasText(a.tr("startForTodo", "title", "Write the release notes")) {
		t.Fatalf("dialog: %q", tt.Texts())
	}
	if len(f.dirs.Entries) > 0 {
		before := f.cwd
		a.loadStart(f.machine, f.dirs.Entries[0].Path)
		tt.Frame()
		if f.cwd == before {
			t.Fatal("folder not opened")
		}
	}
	shot(t, tt, "start-light")
	tt.SetDark(true)
	shot(t, tt, "start-dark")
	tt.SetDark(false)
	if err := tt.Click(a.tr("startGo")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if f.open || f.err != "" || len(a.Path()) <= len("/agents/") || a.Path()[:8] != "/agents/" {
		t.Fatalf("after start at %s: %+v", a.Path(), f)
	}
	if _, ok := d.Session(a.Path()[len("/agents/"):]); !ok {
		t.Fatal("started agent not in the list")
	}

	// Delete asks first
	a.Navigate("/todos/" + id)
	tt.Frame()
	if err := tt.Click(a.tr("todoDelete")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.todos.deleting {
		t.Fatal("no confirmation")
	}
	a.deleteTodo(id)
	tt.Frame()
	if _, ok := d.Todo(id); ok || a.Path() != "/todos" {
		t.Fatalf("not deleted, at %s", a.Path())
	}
}
