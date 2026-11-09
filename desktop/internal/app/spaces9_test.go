package app

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

func pageAt(t *testing.T, url, path string, h int) (*App, *ui.Tester) {
	t.Helper()
	a, _ := signedInTo(t, url, "en")
	a.Navigate(path)
	tt := ui.NewTester(a.View, 1280, h)
	tt.Frame()
	tt.Frame()
	tt.Frame()
	return a, tt
}

func TestHealthSpace(t *testing.T) {
	a, tt := pageAt(t, mockURL, "/health", 1800)
	g := &a.health
	if len(g.convs) != 4 || g.machine == "" || len(g.reportList) != 3 || len(g.memList) == 0 {
		t.Fatalf("health: %d %q %d %d %q", len(g.convs), g.machine, len(g.reportList), len(g.memList), g.list.err)
	}
	if !tt.HasText(a.tr("healthHeroTitle")) || !tt.HasText(a.tr("healthPrivacyTitle")) {
		t.Fatalf("page: %v", tt.Texts())
	}
	shot(t, tt, "health-light")
	tt.SetDark(true)
	shot(t, tt, "health-dark")
	tt.SetDark(false)
	// a report with its record
	a.openReport(g.reportList[0])
	tt.Frame()
	tt.Frame()
	if g.record == "" || g.recordErr != "" {
		t.Fatalf("record: %q %q", g.record, g.recordErr)
	}
	shot(t, tt, "health-report")
	g.report = nil
	tt.Frame()
	// a suggestion fills the box; sending starts a conversation in the space
	if err := tt.Click(a.tr("healthSuggestionSleep")); err != nil {
		t.Fatal(err)
	}
	if g.draft != a.tr("healthSuggestionSleep") {
		t.Fatalf("draft: %q", g.draft)
	}
	a.startHealth(g.draft, true)
	tt.Frame()
	tt.Frame()
	if !strings.HasPrefix(a.Path(), "/health/") || g.draft != "" {
		t.Fatalf("start: %q %q", a.Path(), g.draft)
	}
	shot(t, tt, "health-conversation")
}

func TestMessages(t *testing.T) {
	a, _ := signedInTo(t, mockURL, "en")
	a.conn.data.Hello.Features = append(a.conn.data.Hello.Features, hubapi.FeatureLark)
	a.Navigate("/messages")
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	tt.Frame()
	g := &a.msgs
	if len(g.chats) == 0 || !tt.HasText(a.tr("msgPickTitle")) {
		t.Fatalf("chats: %q %v", g.list.err, tt.Texts())
	}
	shot(t, tt, "messages-list")
	a.Navigate("/messages/f2")
	tt.Frame()
	tt.Frame()
	ch := g.open["f2"]
	if ch == nil || len(ch.page.Messages) == 0 {
		t.Fatalf("chat: %+v", ch)
	}
	if !tt.HasText("Sounds good, thanks!") {
		t.Fatalf("messages: %v", tt.Texts())
	}
	for _, x := range g.chats {
		if x.ID == "f2" && x.Unread != 0 {
			t.Fatalf("not marked read: %+v", x)
		}
	}
	shot(t, tt, "messages-chat-light")
	tt.SetDark(true)
	shot(t, tt, "messages-chat-dark")
	tt.SetDark(false)
	ch.draft = "On my way"
	a.sendLark("f2", ch)
	// the hub's copy takes the place of the one on its way
	if last := ch.page.Messages[len(ch.page.Messages)-1]; last.Sending || last.Text != "On my way" || strings.HasPrefix(last.ID, "temp-") {
		t.Fatalf("sent: %+v", last)
	}
	tt.Frame()
	tt.Frame()
	for _, m := range ch.page.Messages {
		if m.Failed {
			t.Fatalf("send failed: %+v", m)
		}
	}
	// a hub without Feishu says so
	a.conn.data.Hello.Features = nil
	tt.Frame()
	if !tt.HasText(a.tr("msgNoLarkTitle")) {
		t.Fatalf("no lark: %v", tt.Texts())
	}
}

func TestWorkflowBoard(t *testing.T) {
	a, tt := pageAt(t, mockURL, "/board", 900)
	g := &a.board
	if !g.settingRead || g.setting.Enabled || !tt.HasText(a.tr("wbSetOffTitle")) {
		t.Fatalf("off: %+v %v", g.setting, tt.Texts())
	}
	// the board as a hub that has it on would give it
	yes := true
	mid := a.conn.data.Machines[0].ID
	g.setting.Enabled = true
	g.board = &hubapi.Board{Revision: 3, CanWrite: &yes, Columns: []hubapi.BoardColumn{{ID: "c1", Title: "Doing"}, {ID: "c2", Title: "Review"}}, Cards: []hubapi.BoardCard{
		{ID: "k1", MachineID: mid, Title: "persona", Cwd: "/home/me/code/persona", Branch: "main", ColumnID: "c1", Pinned: true},
		{ID: "k2", MachineID: mid, Title: "site", Cwd: "/home/me/code/site", ColumnID: "c1"},
		{ID: "k3", MachineID: mid, Title: "docs", Cwd: "/home/me/code/docs", ColumnID: "c2"},
	}}
	g.loaded, g.at = true, time.Now()
	tt.Frame()
	if !tt.HasText("Doing") || !tt.HasText("persona") || !tt.HasText(a.tr("wbBindWorkspace")) {
		t.Fatalf("board: %v", tt.Texts())
	}
	shot(t, tt, "board-light")
	tt.SetDark(true)
	shot(t, tt, "board-dark")
	tt.SetDark(false)
	g.query = "site"
	tt.Frame()
	if tt.HasText("docs") || !tt.HasText("site") {
		t.Fatalf("search: %v", tt.Texts())
	}
	g.query = ""
	g.detail = "k1"
	tt.Frame()
	if !tt.HasText(a.tr("wbAgentsHere")) || !tt.HasText(a.tr("wbBranch", "branch", "main")) {
		t.Fatalf("detail: %v", tt.Texts())
	}
	shot(t, tt, "board-card")
	g.detail = ""
	g.bind = &boardBind{column: "c1"}
	tt.Frame()
	if !tt.HasText(a.tr("wbModeExisting")) {
		t.Fatalf("bind: %v", tt.Texts())
	}
	g.bind = nil
	// sidebar entry while on
	if !tt.HasText(a.tr("wbTitle")) {
		t.Fatal("no sidebar entry")
	}
	ws := hubapi.ColumnCards(*g.board, "c1", "", "", false)
	if len(ws) != 2 || ws[0].ID != "k1" {
		t.Fatalf("pinned first: %+v", ws)
	}
	if !hubapi.SameDirectory("/a/b/", "/a/b") || hubapi.SameDirectory("/a/b", "/a/c") || !hubapi.SameDirectory(`C:\Code\X`, "c:/code/x") {
		t.Fatal("same directory")
	}
}
