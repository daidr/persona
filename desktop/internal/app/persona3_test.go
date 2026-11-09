package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

func TestPersonaFeed(t *testing.T) {
	a, _ := signedInTo(t, gitMockURL, "en")
	d := a.conn.data
	main, ok := d.MainConversation()
	if !ok {
		t.Fatal("no main conversation")
	}
	a.Navigate("/persona/" + main.SessionID)
	tt := ui.NewTester(a.View, 1280, 1000)
	tt.Frame()
	tt.Frame()
	feed := d.Feeds[main.SessionID]
	if len(feed) == 0 {
		t.Fatalf("no feed for %s", main.SessionID)
	}
	kinds := map[string]bool{}
	for _, it := range feed {
		kinds[it.Kind] = true
	}
	for _, k := range []string{"plan", "task", "today", "brief", "receipt", "routed"} {
		if !kinds[k] {
			t.Fatalf("no %s in %v", k, kinds)
		}
	}
	shot(t, tt, "persona-feed-light")
	tt.SetDark(true)
	shot(t, tt, "persona-feed-dark")
	tt.SetDark(false)

	// the open plan starts
	var open hubapi.TaskPlan
	for _, p := range d.Plans {
		if p.State == "open" {
			open = p
		}
	}
	if open.ID == "" {
		t.Fatalf("no open plan: %+v", d.Plans)
	}
	a.planDo(open.ID, true)
	if d.Plans[open.ID].State != "started" {
		t.Fatalf("plan: %+v", d.Plans[open.ID])
	}

	// the rows: an item goes before the first message sent after it
	items := []hubapi.Item{{ID: "u1", Role: "user", Kind: "text", TS: "2026-01-01T10:00:00Z"}, {ID: "a1", Role: "assistant", Kind: "text"},
		{ID: "u2", Role: "user", Kind: "text", TS: "2026-01-01T12:00:00Z"}}
	fd := []hubapi.FeedItem{{ID: "f1", At: "2026-01-01T11:00:00Z"}, {ID: "f2", At: "2026-01-01T13:00:00Z"}}
	var keys []string
	for _, r := range chatRows(items, fd) {
		keys = append(keys, r.key())
	}
	if got := strings.Join(keys, " "); got != "u1 a1 feed-f1 u2 feed-f2" {
		t.Fatalf("rows: %s", got)
	}
}

func TestReadinessStep(t *testing.T) {
	next := "first_message"
	r := &hubapi.Readiness{Mode: "setup", Next: &next, Steps: []hubapi.ReadinessStep{
		{ID: "computer", State: "ok"}, {ID: "claude", State: "todo", Title: "Install Claude Code"}, {ID: "model", State: "todo"}}}
	if s := blockingStep(r); s == nil || s.ID != "claude" {
		t.Fatalf("step: %+v", s)
	}
	r.Steps[1].Code = "rate_limited"
	if s := blockingStep(r); s == nil || s.ID != "model" {
		t.Fatalf("rate limited: %+v", s)
	}
	r.Mode = "full"
	if blockingStep(r) != nil {
		t.Fatal("full")
	}

	a, _ := signedIn(t, "en")
	main, _ := a.conn.data.MainConversation()
	a.Navigate("/persona/" + main.SessionID)
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	a.ready.r = &hubapi.Readiness{Mode: "setup", Steps: []hubapi.ReadinessStep{{ID: "computer", State: "todo", Title: "Add a computer to run agents",
		Detail: "Persona starts agents on your computers.", Action: &struct {
			ID string `json:"id"`
		}{ID: "add_computer"}}}}
	tt.Frame()
	if !tt.HasText("Add a computer to run agents") {
		t.Fatalf("card: %v", tt.Texts())
	}
	shot(t, tt, "persona-readiness")
	if err := tt.Click(a.tr("readyAddComputer")); err != nil {
		t.Fatal(err)
	}
	if a.router.Path() != "/machines" {
		t.Fatalf("path: %s", a.router.Path())
	}
}

func TestModeChips(t *testing.T) {
	a, _ := signedIn(t, "en")
	x := weatherAgent(t, a)
	if !hasModes(x) {
		t.Fatalf("no modes: %+v", x)
	}
	a.Navigate("/agents/" + x.Key())
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	tt.Frame()
	mode := x.PermissionMode
	if mode == "" {
		mode = "default"
	}
	if err := tt.Click(a.modeLabel(mode)); err != nil {
		t.Fatalf("%v: %v", err, tt.Texts())
	}
	tt.Frame()
	if items := tt.Menu(); len(items) != len(hubapi.PermissionModes) {
		t.Fatalf("menu: %v", items)
	}
	if err := tt.ChooseMenuItem(a.tr("modePlan")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if y, _ := a.conn.data.Session(x.Key()); y.PermissionMode != "plan" {
		t.Fatalf("mode: %+v", y.PermissionMode)
	}
	shot(t, tt, "mode-chips")
	tt.SetDark(true)
	shot(t, tt, "mode-chips-dark")
	// back as it was, for the other tests
	a.setModes(x, map[string]any{"permission_mode": mode})
}

func TestStartOptions(t *testing.T) {
	a, _ := signedIn(t, "en")
	tt := ui.NewTester(a.View, 1280, 900)
	a.openStart("", "")
	tt.Frame()
	tt.Frame()
	f := &a.start
	if f.machine == "" || len(f.launchers) == 0 {
		t.Fatalf("form: %+v", f)
	}
	if !tt.HasText(a.tr("startMode")) || !tt.HasText(a.tr("startEffort")) {
		t.Fatalf("options: %v", tt.Texts())
	}
	shot(t, tt, "start-options")
	f.mode, f.effort, f.ultracode = a.modeLabel("acceptEdits"), a.effortLabel("high"), true
	var req hubapi.StartRequest
	a.startPicks(&req)
	if req.PermissionMode != "acceptEdits" || req.Effort != "high" || !req.Ultracode {
		t.Fatalf("start: %+v", req)
	}
	f.mode = a.modeLabel("default")
	req = hubapi.StartRequest{}
	a.startPicks(&req)
	if req.PermissionMode != "" {
		t.Fatalf("default mode sent: %+v", req)
	}
}

func TestProfile(t *testing.T) {
	a, _ := signedIn(t, "en")
	a.Navigate("/persona/profile")
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	tt.Frame()
	if !tt.HasText(a.tr("profileTitle")) || !tt.HasText(a.tr("profileReach")) {
		t.Fatalf("profile: %v", tt.Texts())
	}
	if len(a.profile.tasks) > 0 && !tt.HasText(a.tr("profileUpcoming")) {
		t.Fatalf("upcoming: %v", tt.Texts())
	}
	shot(t, tt, "profile-light")
	tt.SetDark(true)
	shot(t, tt, "profile-dark")
}
