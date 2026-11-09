package app

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

func TestUsageBars(t *testing.T) {
	a, _ := signedIn(t, "en")
	a.Navigate("/agents")
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	tt.Frame()
	g := &a.usage
	if len(g.accounts) == 0 || !tt.HasText("86%") || !tt.HasText("Codex Pro") {
		t.Fatalf("bars: %+v %v", g.accounts, tt.Texts())
	}
	shot(t, tt, "usage-bars")
	g.open = true
	tt.Frame()
	if !tt.HasText(a.tr("usagePaceUnder")) || !tt.HasText(a.tr("usageResetIn", "countdown", a.tr("usageMinutes", "m", "55"))) {
		t.Fatalf("popover: %v", tt.Texts())
	}
	shot(t, tt, "usage-popover-light")
	tt.SetDark(true)
	shot(t, tt, "usage-popover-dark")
	tt.SetDark(false)
}

func TestUsageHelpers(t *testing.T) {
	now := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	iso := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	w := hubapi.UsageWindow{Label: "5h", UsedPercent: 83, ResetsAt: iso(time.Hour)}
	if s, e, ok := w.Pace(now); !ok || s != "on" || int(e) != 80 {
		t.Fatalf("pace: %s %v %v", s, e, ok)
	}
	if _, _, ok := (hubapi.UsageWindow{Label: "5h", UsedPercent: 1, ResetsAt: iso(299 * time.Minute)}).Pace(now); ok {
		t.Fatal("judged in the first 5%")
	}
	if m := (hubapi.UsageWindow{Label: "7d"}).Minutes(); m != 10080 {
		t.Fatalf("minutes: %d", m)
	}
	accs := []hubapi.UsageAccount{
		{ID: "old", SampledAt: iso(-80 * time.Hour), Windows: []hubapi.UsageWindow{{Label: "5h", UsedPercent: 99}}},
		{ID: "reset", SampledAt: iso(0), Windows: []hubapi.UsageWindow{{Label: "5h", ResetsAt: iso(-time.Minute)}}},
		{ID: "stale", SampledAt: iso(0), Stale: true, Windows: []hubapi.UsageWindow{{Label: "5h", UsedPercent: 95}}},
		{ID: "a", SampledAt: iso(0), Windows: []hubapi.UsageWindow{{Label: "5h", UsedPercent: 20}, {Label: "7d", UsedPercent: 100, Model: "M"}}},
		{ID: "b", SampledAt: iso(0), Windows: []hubapi.UsageWindow{{Label: "5h", UsedPercent: 60}}},
	}
	live := hubapi.LiveUsage(accs, now)
	if len(live) != 3 {
		t.Fatalf("live: %+v", live)
	}
	if top := hubapi.TopUsage(live, 2); top[0].ID != "b" || top[1].ID != "a" {
		t.Fatalf("top: %+v", top)
	}
	if (hubapi.UsageAccount{Name: "Codex Pro", Plan: "pro"}).PlanNote() != "" {
		t.Fatal("plan repeated")
	}
}

func TestArchives(t *testing.T) {
	a, _ := signedInTo(t, gitMockURL, "en")
	a.Navigate("/agents")
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	d := a.conn.data
	d.Hello.Features = append(d.Hello.Features, hubapi.FeatureArchives, hubapi.FeatureArchiveClose) // the socket's hello, which tests don't run
	var plain, closable *hubapi.Session
	for _, grp := range d.Agents("") {
		for _, x := range grp.Agents {
			archive, cl := hubapi.ArchiveOffer(x, d.Hello.Features)
			switch {
			case cl == "ok" && closable == nil:
				closable = &x
			case archive == "ok" && plain == nil:
				plain = &x
			}
		}
	}
	if plain == nil || closable == nil {
		t.Fatalf("no agent to archive (%v / %v)", plain, closable)
	}

	// the menu offers Archive in Remove's place
	a.Navigate("/agents/" + plain.Key())
	tt.Frame()
	if err := tt.Click(a.tr("agentMore")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	items := strings.Join(tt.Menu(), "|")
	if !strings.Contains(items, a.tr("agentArchive")) || strings.Contains(items, a.tr("agentRemove")) {
		t.Fatalf("menu: %q", items)
	}
	tt.Key(0, ui.KeyEscape)
	a.archiveAgent(*plain, false)
	tt.Frame()
	if _, ok := d.Session(plain.Key()); ok || a.Path() != "/agents" {
		t.Fatalf("still listed, at %s", a.Path())
	}

	// Archive and close asks first, then closes it
	a.act = agentAction{open: true, kind: "archiveClose", key: closable.Key(), title: titleOf(*closable), machine: d.MachineName(closable.MachineID), session: *closable}
	tt.Frame()
	shot(t, tt, "archive-close")
	if err := tt.Click(a.tr("archiveCloseConfirm")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.act.open {
		t.Fatalf("dialog: %+v", a.act)
	}

	// both are under Archived, and come back from there
	a.Navigate("/agents/archived")
	tt.Frame()
	tt.Frame()
	g := &a.archived
	if !g.loaded || len(g.rows) < 4 || !tt.HasText(titleOf(*plain)) {
		t.Fatalf("archived: %+v %v", g, tt.Texts())
	}
	shot(t, tt, "archived-light")
	tt.SetDark(true)
	shot(t, tt, "archived-dark")
	tt.SetDark(false)
	for _, x := range []hubapi.Session{*plain, *closable} {
		a.restoreAgent(x)
	}
	for _, x := range g.rows {
		if x.Key() == plain.Key() || x.Key() == closable.Key() {
			t.Fatalf("not restored: %s", x.Key())
		}
	}
	if _, ok := d.Session(plain.Key()); !ok {
		t.Fatal("not back in Agents")
	}
}
