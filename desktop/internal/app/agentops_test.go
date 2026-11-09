package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
	"github.com/nwverse/persona/desktop/internal/hubs"
)

func TestOverview(t *testing.T) {
	a, _ := signedIn(t, "en")
	a.Navigate("/overview")
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	if !tt.HasText(a.tr("goOverview")) || !tt.HasText(a.tr("goMachines")) {
		t.Fatalf("overview: %v", tt.Texts())
	}
	// the line counts what waits on you: every open card, and agents waiting without one
	waiting := 0
	for _, g := range a.conn.data.Agents("") {
		if g.State != hubapi.StateNeedsYou {
			continue
		}
		for _, x := range g.Agents {
			waiting += max(1, len(a.conn.data.SessionInteractions(x.Key())))
		}
	}
	if waiting > 0 && !tt.HasText(a.tr("overviewSummaryNeeds", "count", itoa(waiting))) {
		t.Fatalf("summary for %d: %v", waiting, tt.Texts())
	}
	shot(t, tt, "overview-light")
	tt.SetDark(true)
	shot(t, tt, "overview-dark")
	tt.SetDark(false)
	// Mark all as seen leaves no agent unread
	if tt.HasText(a.tr("overviewMarkAllSeen")) {
		tt.SetSize(1280, 4000)
		tt.Frame()
		if err := tt.Click(a.tr("overviewMarkAllSeen")); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		for _, g := range a.conn.data.Agents("") {
			if g.State == hubapi.StateUnread {
				t.Fatalf("still unread: %+v", g.Agents)
			}
		}
	}
}

func TestRoutines(t *testing.T) {
	a, _ := signedIn(t, "en")
	a.Navigate("/routines")
	tt := ui.NewTester(a.View, 1280, 1400)
	tt.Frame()
	tt.Frame()
	g := &a.routines
	if !g.load.done || g.load.err != "" {
		t.Fatalf("load: %+v", g.load)
	}
	shot(t, tt, "routines-light")
	tt.SetDark(true)
	shot(t, tt, "routines-dark")
	tt.SetDark(false)

	// a new daily task, saved and listed
	if err := tt.Click(a.tr("routinesNew")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	g.edit.Title, g.edit.Prompt, g.edit.Time = "Desktop test task", "Say hello", "07:30"
	tt.Frame()
	shot(t, tt, "routines-editor")
	g.edit.Time = "7h"
	a.saveTask()
	if g.editErr != a.tr("routinesBadTime") || !g.editing {
		t.Fatalf("bad time saved: %q", g.editErr)
	}
	g.edit.Time = "07:30"
	a.saveTask()
	if g.editing || g.editErr != "" {
		t.Fatalf("not saved: %q", g.editErr)
	}
	tt.Frame()
	tt.Frame()
	var made hubapi.Task
	for _, x := range g.tasks {
		if x.Title == "Desktop test task" {
			made = x
		}
	}
	if made.ID == "" || made.Repeat != "daily" || made.Time != "07:30" {
		t.Fatalf("task: %+v", made)
	}
	if !tt.HasText(a.tr("routinesDaily", "time", "07:30")) {
		t.Fatalf("when: %v", tt.Texts())
	}

	// deleting it after asking
	g.confirm = made.ID
	tt.SetSize(1280, 4000)
	tt.Frame()
	if err := tt.Click(a.tr("routinesDeleteConfirm")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	for _, x := range g.tasks {
		if x.ID == made.ID {
			t.Fatalf("still there: %+v", x)
		}
	}
}

func TestTaskWhen(t *testing.T) {
	a := New(newHost(), hubs.Load(filepath.Join(t.TempDir(), "hubs.json")), "en", "1.0.0")
	cases := map[string]hubapi.Task{
		"Every day at 09:00":           {Repeat: "daily", Time: "09:00"},
		"Every day at 09:00 · Mon Wed": {Repeat: "daily", Time: "09:00", Weekdays: []int{1, 3}},
		"Every 30 min":                 {Repeat: "interval", EveryMinutes: 30},
		"Once":                         {Repeat: "once"},
		"Every day at 08:15 · Sat Sun": {Repeat: "daily", Time: "08:15", Weekdays: []int{6, 7}},
	}
	for want, task := range cases {
		if got := a.taskWhen(task); got != want {
			t.Errorf("%+v: %q, want %q", task, got, want)
		}
	}
}

func TestAgentActions(t *testing.T) {
	a, _ := signedIn(t, "en")
	d := a.conn.data
	var x hubapi.Session
	for _, g := range d.Agents("") {
		if g.State == hubapi.StateIdle && len(g.Agents) > 0 {
			x = g.Agents[0]
			break
		}
	}
	if x.SessionID == "" {
		t.Skip("no idle agent in the mock")
	}
	key := x.Key()
	a.Navigate("/agents/" + key)
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()

	// the menu lists its actions
	if err := tt.Click(a.tr("agentMore")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	items := strings.Join(tt.Menu(), "|")
	for _, want := range []string{"agentRename", "agentInfo", "agentCopyID", "agentRemove"} {
		if !strings.Contains(items, a.tr(want)) {
			t.Fatalf("%s not in %q", want, items)
		}
	}

	// pin: the agent shows in Pinned on top
	if d.PinsLoaded {
		if err := tt.ChooseMenuItem(a.tr("agentPin")); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		tt.Frame()
		if !d.Pinned(key) || !tt.HasText(a.tr("agentsPinned")) {
			t.Fatalf("not pinned: %+v", d.Pins)
		}
		shot(t, tt, "agents-pinned")
		a.togglePin(key)
		tt.Frame()
		if d.Pinned(key) {
			t.Fatal("still pinned")
		}
	}

	// rename
	a.act = agentAction{open: true, kind: "rename", key: key, title: "Renamed from desktop"}
	tt.Frame()
	shot(t, tt, "agent-rename")
	a.renameAgent()
	tt.Frame()
	if y, _ := d.Session(key); y.Title != "Renamed from desktop" || a.act.open {
		t.Fatalf("rename: %+v %+v", y, a.act)
	}

	// session info
	a.openInfo(x)
	tt.Frame()
	if a.act.err != "" || a.act.info == nil {
		t.Fatalf("info: %+v", a.act)
	}
	if !tt.HasText(a.tr("infoSession")) {
		t.Fatalf("info rows: %v", tt.Texts())
	}
	shot(t, tt, "agent-info")
	tt.SetDark(true)
	shot(t, tt, "agent-info-dark")
	tt.SetDark(false)
	a.act.open = false
}

func TestMatchChats(t *testing.T) {
	list := []hubapi.Session{{SessionID: "1", Title: "Weekly report"}, {SessionID: "2", Title: "Trip", LastText: "Booked the weekly train"}, {SessionID: "3", Title: "Other"}}
	if got := matchChats(list, "weekly"); len(got) != 2 {
		t.Fatalf("weekly: %+v", got)
	}
	if got := matchChats(list, "weekly train"); len(got) != 1 || got[0].SessionID != "2" {
		t.Fatalf("weekly train: %+v", got)
	}
	if got := matchChats(list, " "); len(got) != 3 {
		t.Fatalf("blank: %+v", got)
	}
}
