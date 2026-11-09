package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
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
	pick     []string
	save     string
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
func (h *fakeHost) PickFiles(_ string, done func([]string)) {
	if len(h.pick) > 0 {
		done(h.pick)
	}
}

func (h *fakeHost) SaveFile(_, _ string, done func(string)) {
	if h.save != "" {
		done(h.save)
	}
}

// mockURL is the repository's mock hub (scripts/mock/mock_hub.py); gitMockURL another one whose Git can be written to
// (MOCK_GIT_WRITE), whose main conversation dispatches tasks (MOCK_TASKS), whose Inbox has GitHub and Telegram threads
// and whose agents can be archived (MOCK_AGENT_ARCHIVES),
// which other tests would otherwise share.
// settingsMockURL is a third whose hub has an update to install (MOCK_HUB_UPDATE), a ledger of what was sent as the user
// (MOCK_SENT), channels (MOCK_CHANNELS), model providers that can be tested (MOCK_PROVIDERS) and terminal panes to browse
// (MOCK_TERMINAL_PANES).
var mockURL, gitMockURL, settingsMockURL string

// TestMain starts the mock hubs, which serve machines, agents, cards and an Inbox shaped as a real hub's, when
// python3 is there.
func TestMain(m *testing.M) {
	var stop []func()
	if py, err := exec.LookPath("python3"); err == nil {
		var kill func()
		mockURL, kill = startMock(py, "MOCK_EXTENSIONS=1")
		stop = append(stop, kill)
		gitMockURL, kill = startMock(py, "MOCK_GIT_WRITE=1", "MOCK_TASKS=1", "MOCK_GITHUB_INBOX=1", "MOCK_TELEGRAM=signed_in", "MOCK_TELEGRAM_INBOX=1", "MOCK_AGENT_ARCHIVES=1")
		stop = append(stop, kill)
		settingsMockURL, kill = startMock(py, "MOCK_HUB_UPDATE=available", "MOCK_SENT=1", "MOCK_CHANNELS=1", "MOCK_PROVIDERS=1", "MOCK_TERMINAL_PANES=1")
		stop = append(stop, kill)
	}
	code := m.Run()
	for _, kill := range stop {
		kill()
	}
	os.Exit(code)
}

func startMock(py string, env ...string) (string, func()) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", func() {}
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	cmd := exec.Command(py, "../../../scripts/mock/mock_hub.py")
	cmd.Env = append(append(os.Environ(), fmt.Sprintf("PORT=%d", port)), env...)
	if cmd.Start() != nil {
		return "", func() {}
	}
	kill := func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	for i := 0; i < 100; i++ {
		if r, err := http.Get(url + "/api/v1/health"); err == nil {
			r.Body.Close()
			return url, kill
		}
		time.Sleep(100 * time.Millisecond)
	}
	kill()
	return "", func() {}
}

// signedIn is the app signed in to the mock hub with everything read, the socket left out.
func signedIn(t *testing.T, lang string) (*App, *fakeHost) {
	t.Helper()
	return signedInTo(t, mockURL, lang)
}

func signedInTo(t *testing.T, mockURL, lang string) (*App, *fakeHost) {
	t.Helper()
	if mockURL == "" {
		t.Skip("the mock hub needs python3")
	}
	host := newHost()
	a := New(host, hubs.Load(filepath.Join(t.TempDir(), "hubs.json")), lang, "1.0.0")
	a.Go = func(fn func()) { fn() }
	a.After = func(_ time.Duration, fn func()) { fn() }
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

func TestKnowledge(t *testing.T) {
	a, _ := signedIn(t, "zh-Hans")
	a.Navigate("/knowledge")
	tt := ui.NewTester(a.View, 1280, 800)
	tt.Frame()
	tt.Frame()
	if !tt.HasText("天气 App") || !tt.HasText(a.tr("kbKindPeople")) {
		t.Fatalf("list: %q", tt.Texts())
	}
	// a kind chip lists that kind
	if err := tt.Click(a.tr("kbKindPeople")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if a.Path() != "/knowledge/people" || !tt.HasText("韩梅梅") {
		t.Fatalf("people at %s: %q", a.Path(), tt.Texts())
	}
	// search, then open what it found: the page says where the words are
	a.kb.query = "小组件"
	a.kbSearch()
	tt.Frame()
	if len(a.kb.found) == 0 {
		t.Fatal("nothing found")
	}
	var e hubapi.KBEntry
	for _, r := range a.kb.found {
		if r.Entry.ID == "weather-app" {
			e = r.Entry
		}
	}
	a.openKB(e, "小组件")
	a.kb.query = ""
	a.kbSearch()
	tt.Frame()
	tt.Frame()
	if !tt.HasText(a.tr("kbTimeline")) || !tt.HasText(a.tr("kbFound", "query", "小组件")) || !tt.HasText(a.tr("kbRelated")) {
		t.Fatalf("entry: %q", tt.Texts())
	}
	shot(t, tt, "knowledge-zh")
	tt.SetDark(true)
	shot(t, tt, "knowledge-zh-dark")
	tt.SetDark(false)

	// Tagging saves and reads the entry again
	if err := tt.Click(a.tr("kbTagging")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.kb.form.importance, a.kb.form.tags = 1, "iOS, #发布"
	a.kb.form.status = a.tr("kbStatusPaused")
	if u := a.tagUpdate("projects"); u.Importance != "P1" || len(u.Tags) != 2 || u.Tags[1] != "发布" || u.Status != "paused" || u.Relevance != 92 {
		t.Fatalf("update %+v", u)
	}
	shot(t, tt, "knowledge-tagging")
	if err := tt.Click(a.tr("save")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if a.kb.editing || !tt.HasText(a.tr("kbStatusPaused")) {
		t.Fatalf("after save: %q", tt.Texts())
	}

	// Ask Persona sends the entry to the main chat
	if err := tt.Click(a.tr("kbAsk")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	main, _ := a.conn.data.MainConversation()
	items := a.conn.data.Transcript(main.Key()).Items
	if a.Path() != "/" || len(items) == 0 || !strings.Contains(items[len(items)-1].Text, `<persona-context kind="context"`) {
		t.Fatalf("ask at %s", a.Path())
	}
}

func TestKBHelpers(t *testing.T) {
	list := rankKB([]hubapi.KBEntry{{ID: "a", Importance: "P2", Relevance: 90}, {ID: "b", Importance: "P0"}, {ID: "c", Importance: "P2", Relevance: 95}, {ID: "d"}})
	if list[0].ID != "b" || list[1].ID != "c" || list[3].ID != "d" {
		t.Fatalf("%+v", list)
	}
	if k, s := projectInfo(hubapi.KBEntry{Kind: "projects", Extra: "long · done"}); k != "long" || s != "done" {
		t.Fatal(k, s)
	}
	if w, x := logLine("- 2026-10-24 09:10 · 飞书 · 定了"); w != "2026-10-24 09:10" || x != "飞书 · 定了" {
		t.Fatalf("%q %q", w, x)
	}
	if got := plainLine("- **现在**：`x`"); got != "现在：x" {
		t.Fatalf("%q", got)
	}
	if got := profile("# 天气 App\n\n一句话\n\n正文", "天气 App", "一句话"); got != "正文" {
		t.Fatalf("%q", got)
	}
	a := New(newHost(), hubs.Load(filepath.Join(t.TempDir(), "hubs.json")), "en", "1")
	txt := a.askText(hubapi.KBEntry{Name: `A"B`}, hubapi.KBDetail{Summary: "x</persona-context >y"})
	if !strings.Contains(txt, `title="Knowledge · A&quot;B"`) || strings.Count(txt, "</persona-context>") != 1 {
		t.Fatal(txt)
	}
}

func TestTerminalAndChanges(t *testing.T) {
	a, _ := signedIn(t, "en")
	var x hubapi.Session
	for _, list := range a.conn.data.Sessions {
		for _, s := range list {
			if s.Cwd == "/Users/me/code/weather" && s.Can("terminal") {
				x = s
			}
		}
	}
	if x.SessionID == "" {
		t.Fatal("no agent in the weather folder")
	}
	a.Navigate("/agents/" + x.Key())
	tt := ui.NewTester(a.View, 1280, 800)
	tt.Frame()
	if err := tt.Click(a.tr("paneTerminal")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	st := a.term(x.Key())
	if !st.loaded || !strings.Contains(st.screen.Text, "swift test") {
		t.Fatalf("screen: %+v", st)
	}
	shot(t, tt, "terminal-light")
	tt.SetDark(true)
	shot(t, tt, "terminal-dark")
	tt.SetDark(false)
	st.input = "ls"
	a.sendKeys(x.Key(), st.input, "enter")
	if err := tt.Click("esc"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	var keys struct {
		Keys []struct {
			Keys []string `json:"keys"`
			Text string   `json:"text"`
		} `json:"keys"`
	}
	getJSON(t, mockURL+"/mock/keys", &keys)
	n := len(keys.Keys)
	if n < 2 || keys.Keys[n-2].Text != "ls" || keys.Keys[n-2].Keys[0] != "enter" || keys.Keys[n-1].Keys[0] != "esc" {
		t.Fatalf("keys: %+v", keys.Keys)
	}

	if err := tt.Click(a.tr("paneChanges")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if !tt.HasText("main → origin/main  ↑1") || !tt.HasText("README.md") {
		t.Fatalf("changes: %q", tt.Texts())
	}
	if err := tt.Click("README.md"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if !tt.HasText("+A small sample app.") {
		t.Fatalf("diff: %q", tt.Texts())
	}
	shot(t, tt, "changes-light")
	tt.SetDark(true)
	shot(t, tt, "changes-dark")
	if fileMark(hubapi.GitFile{Index: "A", Worktree: "."}) != "A" || fileMark(hubapi.GitFile{Index: "?", Worktree: "?"}) != "?" ||
		!staged(hubapi.GitFile{Index: "A", Worktree: "."}) || staged(hubapi.GitFile{Index: "M", Worktree: "M"}) {
		t.Fatal("marks")
	}
}

func getJSON(t *testing.T, u string, out any) {
	t.Helper()
	res, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		t.Fatal(err)
	}
}

func TestAssistantChatsAndCards(t *testing.T) {
	a, _ := signedIn(t, "en")
	d := a.conn.data
	open, _ := a.conversations()
	if len(open) < 2 || open[0].SessionID != d.Assistant.Main {
		t.Fatalf("conversations %+v", open)
	}
	tt := ui.NewTester(a.View, 1280, 1000)
	tt.Frame()
	if !tt.HasText(a.tr("personaMain")) || !tt.HasText(titleOf(open[1])) {
		t.Fatalf("list: %q", tt.Texts())
	}
	// another chat opens beside the list
	if err := tt.Click(titleOf(open[1])); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.Path() != "/persona/"+open[1].SessionID || !tt.HasText(a.tr("personaArchive")) {
		t.Fatalf("at %s: %q", a.Path(), tt.Texts())
	}

	// a card in the main chat: drawn, a row's reply answers it
	main, _ := d.MainConversation()
	view := `{"title":"Which option?","fallback":"Recommend B.","sections":[{"items":[` +
		`{"type":"row","title":"A Rewrite","subtitle":"3 days","action":{"id":"a","reply":"Choose A"}},` +
		`{"type":"row","title":"B Deduplicate","badge":{"text":"Recommended","tone":"positive"},"action":{"id":"b","reply":"Choose B"}},` +
		`{"type":"stat_grid","stats":[{"label":"Queue","value":"1,204","tone":"caution"},{"label":"Errors","value":"3"}]},` +
		`{"type":"progress","label":"Migrated","ratio":0.62},` +
		`{"type":"table","columns":[{"title":"Day"},{"title":"Runs","align":"end"}],"rows":[["Mon","12"],["Tue","9"]]},` +
		`{"type":"chart","kind":"bar"}]}]}`
	tr := d.Transcript(main.Key())
	tr.Merge([]hubapi.Item{{ID: "card-1", Seq: 9999, Role: "assistant", Kind: "text", Text: "Three ways:\n\n```persona-view\n" + view + "\n```\n\nB is the quickest."}})
	a.Navigate("/")
	tt.Frame()
	a.chat(main.Key()).list.ScrollToEnd()
	for range 30 {
		tt.Frame()
	}
	if !tt.HasText("Which option?") || !tt.HasText("1,204") || !tt.HasText("62%") || !tt.HasText("Recommend B.") || !tt.HasText("B is the quickest.") {
		t.Fatalf("card: %q", tt.Texts())
	}
	shot(t, tt, "assistant-card-light")
	tt.SetDark(true)
	shot(t, tt, "assistant-card-dark")
	tt.SetDark(false)
	if err := tt.Click("B Deduplicate"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.toasts) > 0 || a.cardOf("card-1/0").busy {
		t.Fatalf("card action: %q", a.toasts)
	}

	a.openTarget("persona://session/macbook/s%2F1")
	if a.Path() != "/agents/macbook/s/1" {
		t.Fatalf("open session: %s", a.Path())
	}
	a.openTarget("persona://task/t1")
	if a.Path() != "/todos/t1" {
		t.Fatalf("open task: %s", a.Path())
	}
	a.openTarget("javascript:alert(1)")
	if a.Path() != "/todos/t1" {
		t.Fatal("opened a script")
	}

	// a new chat starts with its first message
	a.Navigate("/persona/new")
	tt.Frame()
	a.persona.draft = "Plan my week"
	a.startConversation()
	tt.Frame()
	if !strings.HasPrefix(a.Path(), "/persona/") || a.Path() == "/persona/new" {
		t.Fatalf("new chat at %s", a.Path())
	}
}

func TestInboxReply(t *testing.T) {
	a, _ := signedIn(t, "zh-Hans")
	d := a.conn.data
	var drafted, mail hubapi.InboxItem
	for _, it := range d.Inbox {
		if it.Drafted() != "" && !it.Done() && drafted.ID == "" {
			drafted = it
		}
		if it.Source == "mail" && !it.Done() && it.Drafted() == "" && mail.ID == "" {
			mail = it
		}
	}
	if drafted.ID == "" || mail.ID == "" {
		t.Fatal("no drafted entry or mail")
	}
	a.Navigate("/inbox/" + drafted.ID)
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	if a.inbox.reply != drafted.Drafted() || !tt.HasText(a.tr("inboxDrafted")) {
		t.Fatalf("draft not offered: %q", a.inbox.reply)
	}
	shot(t, tt, "inbox-reply-zh")
	tt.SetDark(true)
	shot(t, tt, "inbox-reply-zh-dark")
	tt.SetDark(false)
	if err := tt.Click(a.tr("inboxSend")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, it := range d.Inbox {
		if it.ID == drafted.ID && !it.Done() {
			t.Fatal("replied entry not handled")
		}
	}

	// Persona drafts a reply to a mail
	a.Navigate("/inbox/" + mail.ID)
	tt.Frame()
	if err := tt.Click(a.tr("inboxDraft")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.inbox.reply == "" {
		t.Fatal("no draft")
	}
}

func TestMCPForm(t *testing.T) {
	a, _ := signedIn(t, "en")
	var x hubapi.Session
	for _, list := range a.conn.data.Sessions {
		for _, s := range list {
			if s.Can("approve") && len(a.conn.data.SessionInteractions(s.Key())) == 0 && x.SessionID == "" {
				x = s
			}
		}
	}
	var ix hubapi.Interaction
	if err := json.Unmarshal([]byte(`{"id":"form-1","machine_id":"`+x.MachineID+`","session_id":"`+x.SessionID+`","kind":"elicitation",
		"mcp_server_name":"deploy-mcp","elicitation":{"mode":"form","message":"Confirm the deployment",
		"requested_schema":{"type":"object","properties":{
			"env":{"type":"string","title":"Environment","oneOf":[{"const":"staging","title":"Staging"},{"const":"prod","title":"Production"}]},
			"replicas":{"type":"integer","title":"Replicas","default":2},
			"notify":{"type":"boolean","title":"Notify the team","default":true},
			"regions":{"type":"array","title":"Regions","items":{"enum":["us","eu"]}},
			"note":{"type":"string","title":"Note"}},
		"required":["env","replicas"]}}}`), &ix); err != nil {
		t.Fatal(err)
	}
	if f := ix.Elicitation.Schema.Fields; len(f) != 5 || f[0].Name != "env" || f[4].Name != "note" {
		t.Fatalf("fields out of order: %+v", f)
	}
	a.conn.data.Interactions[ix.ID] = ix
	a.Navigate("/agents/" + x.Key())
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	if !tt.HasText("Confirm the deployment") || !tt.HasText("Replicas *") || !tt.HasText(a.tr("formMissing", "field", "Environment")) {
		t.Fatalf("form: %q", tt.Texts())
	}
	shot(t, tt, "mcp-form-light")
	tt.SetDark(true)
	shot(t, tt, "mcp-form-dark")
	vals := a.chat(x.Key()).answers[ix.ID]
	vals["env"] = []string{"prod"}
	vals["regions"] = []string{"eu"}
	content, missing := formContent(ix.Elicitation.Schema.Fields, ix.Elicitation.Schema.Required, vals)
	if missing != "" || content["env"] != "prod" || content["replicas"] != float64(2) || content["notify"] != true ||
		len(content["regions"].([]string)) != 1 || content["note"] != nil {
		t.Fatalf("content %+v missing %q", content, missing)
	}
	vals["replicas"] = []string{"2.5"}
	if _, missing := formContent(ix.Elicitation.Schema.Fields, ix.Elicitation.Schema.Required, vals); missing != "Replicas" {
		t.Fatalf("a fraction passes as an integer: %q", missing)
	}
}

func TestAttachments(t *testing.T) {
	a, h := signedIn(t, "en")
	var x hubapi.Session
	for _, list := range a.conn.data.Sessions {
		for _, s := range list {
			if s.Cwd == "/Users/me/code/weather" && s.Can("send") {
				x = s
			}
		}
	}
	if x.SessionID == "" {
		t.Fatal("no agent in the weather folder")
	}
	dir := t.TempDir()
	pic := filepath.Join(dir, "chart.png")
	img := image.NewRGBA(image.Rect(0, 0, 120, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 120; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 2), uint8(y * 3), 200, 255})
		}
	}
	f, _ := os.Create(pic)
	_ = png.Encode(f, img)
	f.Close()
	notes := filepath.Join(dir, "release notes.txt")
	_ = os.WriteFile(notes, []byte("v2"), 0o644)
	big := filepath.Join(dir, "huge.png")
	_ = os.WriteFile(big, make([]byte, maxImage+1), 0o644)

	a.Navigate("/agents/" + x.Key())
	tt := ui.NewTester(a.View, 1280, 800)
	tt.Frame()
	h.pick = []string{pic, notes, big}
	if err := tt.Click(a.tr("attach")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	st := a.chat(x.Key())
	if len(st.files) != 1 || st.files[0].name != "chart.png" || st.files[0].img == nil {
		t.Fatalf("pictures: %+v", st.files)
	}
	if !strings.HasPrefix(st.draft, "'/tmp/release notes-") || st.uploads != 0 {
		t.Fatalf("draft: %q", st.draft)
	}
	if !tt.HasText(a.tr("attachImageTooBig", "name", "huge.png")) {
		t.Fatalf("no word on the picture that's too big: %v", tt.Texts())
	}
	// a picture dragged onto the composer is attached too
	r, ok := tt.Find(a.tr("composerPlaceholder"))
	if !ok {
		t.Fatal("no composer")
	}
	if !tt.DropFiles(r.X+10, r.Y+5, pic) {
		t.Fatal("the composer didn't take the drop")
	}
	tt.Frame()
	if len(st.files) != 2 {
		t.Fatalf("after the drop: %d pictures", len(st.files))
	}
	shot(t, tt, "attachments-light")
	tt.SetDark(true)
	shot(t, tt, "attachments-dark")
	tt.SetDark(false)
	if err := tt.Click(a.tr("attachRemove", "name", "chart.png")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(st.files) != 1 {
		t.Fatalf("after removing one: %d", len(st.files))
	}
	if err := tt.Click(a.tr("send")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(st.files) != 0 || st.draft != "" {
		t.Fatalf("not sent: %+v %q", st.files, st.draft)
	}
	var sent struct {
		Sent []struct {
			Text   string         `json:"text"`
			Images []hubapi.Image `json:"images"`
		} `json:"sent"`
	}
	getJSON(t, mockURL+"/mock/sent", &sent)
	last := sent.Sent[len(sent.Sent)-1]
	if len(last.Images) != 1 || last.Images[0].Mime != "image/png" || !strings.Contains(last.Text, "release notes") {
		t.Fatalf("sent: %+v", last)
	}
	// the picture shows in the chat, from the hub's copy or the one being sent
	shown := false
	for _, it := range a.conn.data.Transcript(x.Key()).Items {
		if it.Kind == "image" && a.images[it.ID] != nil {
			shown = true
		}
	}
	tt.Frame()
	for _, it := range a.conn.data.Transcript(x.Key()).Items {
		if it.Kind == "image" && a.images[it.ID] != nil {
			shown = true
		}
	}
	if !shown {
		t.Fatal("the picture isn't in the chat")
	}
	shot(t, tt, "attachments-sent")
}

func TestPalette(t *testing.T) {
	a, _ := signedIn(t, "en")
	tt := ui.NewTester(a.View, 1280, 800)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	if !a.palette.open || !tt.HasText(a.tr("startTitle")) {
		t.Fatal("⌘K didn't open the palette")
	}
	shot(t, tt, "palette-light")
	tt.Type("heic")
	tt.Frame()
	list := matchCommands(a.commands(), a.palette.query)
	if len(list) == 0 || !strings.Contains(list[0].title, "HEIC") {
		t.Fatalf("matches for heic: %+v", list)
	}
	tt.SetDark(true)
	shot(t, tt, "palette-dark")
	tt.SetDark(false)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.palette.open || !strings.HasPrefix(a.router.Path(), "/agents/") || !strings.HasSuffix(a.router.Path(), list[0].id[len("agent:"):]) {
		t.Fatalf("enter: open %v at %s", a.palette.open, a.router.Path())
	}
	// arrows move the choice; a page by its name
	a.OpenPalette()
	tt.Frame()
	tt.Type("to-dos")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.router.Path() != "/todos" {
		t.Fatalf("to-dos went to %s", a.router.Path())
	}
	a.OpenPalette()
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if a.palette.sel != 1 {
		t.Fatalf("choice after down, down, up: %d", a.palette.sel)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.router.Path() != "/persona/new" {
		t.Fatalf("the second command went to %s", a.router.Path())
	}
	a.OpenPalette()
	tt.Frame()
	tt.Type("zzzz-nothing")
	tt.Frame()
	if !tt.HasText(a.tr("paletteNothing")) {
		t.Fatal("no word when nothing matches")
	}
}

func TestMatchCommands(t *testing.T) {
	all := []command{{id: "a", title: "Settings", sub: "Page"}, {id: "b", title: "Open settings of the hub", sub: "Action"}, {id: "c", title: "Weather", sub: "MacBook"}}
	got := matchCommands(all, "set")
	if len(got) != 2 || got[0].id != "a" {
		t.Fatalf("set: %+v", got)
	}
	if got := matchCommands(all, "weather macbook"); len(got) != 1 || got[0].id != "c" {
		t.Fatalf("two words: %+v", got)
	}
	if got := matchCommands(all, " "); len(got) != 3 {
		t.Fatalf("empty: %+v", got)
	}
}

func TestHubSettings(t *testing.T) {
	a, _ := signedIn(t, "en")
	a.Navigate("/settings")
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatalf("%s: %v (%v)", label, err, tt.Texts())
		}
		tt.Frame()
		tt.Frame()
	}

	// Devices: the list, revoking one after asking, a pairing link with its QR code
	click(a.tr("settingsDevices"))
	g := &a.sets
	if !g.devices.done || len(g.tokens) == 0 || !tt.HasText(a.tr("deviceThis")) {
		t.Fatalf("devices: %+v", g.devices)
	}
	shot(t, tt, "settings-devices-light")
	tt.SetDark(true)
	shot(t, tt, "settings-devices-dark")
	tt.SetDark(false)
	victim := ""
	for _, tok := range g.tokens {
		if tok.RevokedAt == "" && tok.ID != g.current {
			victim = tok.ID
			break
		}
	}
	g.revoking = victim
	// other tests' sign-ins pile up on the mock: room for all of them
	tt.SetSize(1280, 2400)
	tt.Frame()
	click(a.tr("deviceRevokeConfirm"))
	tt.SetSize(1280, 900)
	for _, tok := range g.tokens {
		if tok.ID == victim && tok.RevokedAt == "" {
			t.Fatalf("%s not revoked: %+v", victim, g.tokens)
		}
	}
	click(a.tr("devicesAdd"))
	if g.pair == nil || g.pairQR == nil || !strings.HasPrefix(g.pair.PairURL, "persona://pair") {
		t.Fatalf("pairing: %+v %q", g.pair, g.pairErr)
	}
	shot(t, tt, "settings-pair")
	click(a.tr("pairCopy"))
	if tt.Clipboard() != g.pair.PairURL {
		t.Fatalf("clipboard: %q", tt.Clipboard())
	}
	click(a.tr("pairDoneButton"))
	if g.pairOpen {
		t.Fatal("the pairing dialog stayed open")
	}

	// Extensions: switching one off
	click(a.tr("settingsExtensions"))
	if !g.extensions.done || len(g.exts) == 0 {
		t.Fatalf("extensions: %+v", g.extensions)
	}
	shot(t, tt, "settings-extensions-light")
	var on hubapi.Extension
	for _, x := range g.exts {
		if x.On() && x.Status == "running" {
			on = x
			break
		}
	}
	if err := tt.Click(a.tr("extSwitch", "name", on.Name)); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	for _, x := range g.exts {
		if x.ID == on.ID && x.On() {
			t.Fatalf("%s still on", x.ID)
		}
	}

	// Connectors
	click(a.tr("settingsConnectors"))
	if !g.connectors.done || len(g.conns) == 0 {
		t.Fatalf("connectors: %+v", g.connectors)
	}
	shot(t, tt, "settings-connectors-light")

	// Models: a category's provider changed and back to Default
	click(a.tr("settingsModels"))
	if !g.models.done || len(g.modelCfg.Purposes) == 0 {
		t.Fatalf("models: %+v", g.models)
	}
	tt.SetDark(true)
	shot(t, tt, "settings-models-dark")
	tt.SetDark(false)
	var m hubapi.ModelPurpose
	for _, x := range g.modelCfg.Purposes {
		if x.Editable() && len(x.ProviderIDs) > 0 {
			m = x
			break
		}
	}
	g.modelEdit, g.modelProv, g.modelName = m.ID, a.providerName(m.ProviderIDs[0]), "custom-model"
	tt.Frame()
	click(a.tr("save"))
	for _, x := range g.modelCfg.Purposes {
		if x.ID == m.ID && x.Override.Provider != m.ProviderIDs[0] {
			t.Fatalf("%s: %+v", x.ID, x.Override)
		}
	}
	if g.modelEdit != "" {
		t.Fatal("the editor stayed open")
	}
}

// Messages another conversation or a scheduled run wrote are never user bubbles; the user's own lose their blocks.
func TestMessageSources(t *testing.T) {
	a, _ := signedIn(t, "en")
	key := "macbook/src-test"
	tr := a.conn.data.Transcript(key)
	tr.Loaded = true
	tr.Merge([]hubapi.Item{
		{ID: "s1", Seq: 1, Role: "user", Kind: "text", Text: "Collect the numbers", Source: &hubapi.Source{Kind: "conversation", Title: "Research"}},
		{ID: "s2", Seq: 2, Role: "user", Kind: "text", Text: "<persona-context kind=\"context\" title=\"Notes\">n</persona-context>My own words"},
		{ID: "s3", Seq: 3, Role: "user", Kind: "text", Text: "x", Injected: &hubapi.Injected{Kind: "followup", State: "daily", Line: "Daily check", Detail: "Check the inbox"}},
	})
	st := a.chat(key)
	tt := ui.NewTester(func(c *ui.Context) {
		p := a.theme(c)
		ui.Column(c).Fill().Children(func() {
			for _, it := range tr.Items {
				a.itemView(c, p, st, it, "")
			}
		})
	}, 900, 600)
	tt.Frame()
	for _, want := range []string{"@Research", "Collect the numbers", "My own words", "Notes", "@" + a.tr("sourcePatrol"), "Check the inbox"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %v", want, tt.Texts())
		}
	}
	if tt.HasText("<persona-context") || strings.Contains(strings.Join(tt.Texts(), " "), "</persona-context>") {
		t.Errorf("raw blocks shown: %v", tt.Texts())
	}
	shot(t, tt, "message-sources")
}

func TestStartPreset(t *testing.T) {
	a, _ := signedIn(t, "en")
	a.openStart("", "")
	f := &a.start
	if len(f.launchers) == 0 {
		t.Fatal("no launchers")
	}
	f.tiers = []hubapi.Tier{{Name: "fast", Launcher: f.launchers[len(f.launchers)-1].Name, Model: "small", Effort: "low", Description: "Quick fixes"}}
	f.tiersProfile = "Performance"
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	if !tt.HasText(a.tr("startPresetHint", "profile", "Performance")) {
		t.Fatalf("no preset field: %v", tt.Texts())
	}
	f.tier, f.tierFor = "fast", launcherLabel(f.launchers[len(f.launchers)-1])
	f.launcher = f.tierFor
	tt.Frame()
	if !tt.HasText("Quick fixes") {
		t.Fatal("no preset hint")
	}
	if tier, ok := f.pickedTier(); !ok || tier.Model != "small" {
		t.Fatalf("picked: %+v %v", tier, ok)
	}
	// a launcher picked by hand afterwards unmarks the preset
	f.launcher = launcherLabel(f.launchers[0])
	if _, ok := f.pickedTier(); ok && len(f.launchers) > 1 {
		t.Fatal("the preset outlived a launcher picked by hand")
	}
}
