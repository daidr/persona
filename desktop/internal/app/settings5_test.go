package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

func TestPersonaSettings(t *testing.T) {
	a, _ := signedIn(t, "en")
	a.Navigate("/settings")
	a.sets.tab = 1
	tt := ui.NewTester(a.View, 1280, 2600)
	tt.Frame()
	tt.Frame()
	g := &a.sets
	if !g.persona.done || g.persona.err != "" {
		t.Fatalf("load: %+v", g.persona)
	}
	for _, k := range []string{"setPersonaProactive", "setPersonaQuietTasks", "setPersonaAboutMe", "setPersonaMemory", "setPersonaRunsOn"} {
		if !tt.HasText(a.tr(k)) {
			t.Fatalf("%s missing: %v", k, tt.Texts())
		}
	}
	shot(t, tt, "settings-persona-light")
	tt.SetDark(true)
	shot(t, tt, "settings-persona-dark")
	tt.SetDark(false)

	// a switch goes to the hub
	quiet := hubapi.On(g.asst.MainTasksQuiet)
	a.setAssistant("main_tasks_quiet", !quiet)
	if hubapi.On(g.asst.MainTasksQuiet) == quiet || g.asstBusy != "" {
		t.Fatalf("quiet: %+v", g.asst)
	}
	// About me saved
	g.notes = "I keep code in ~/code."
	a.saveNotes()
	if g.asst.Notes != "I keep code in ~/code." {
		t.Fatalf("notes: %+v", g.asst)
	}
	// a memory opens, is edited and saved
	entries := hubapi.MemoryEntries(g.memory)
	if len(entries) == 0 {
		t.Skip("no memory in the mock")
	}
	e := entries[0]
	g.memOpen = &e
	tt.Frame()
	shot(t, tt, "settings-memory")
	g.memEditing, g.memEdit = true, e.File.Content+"\nOne more line."
	a.saveMemory(&e)
	if g.memOpen != nil {
		t.Fatal("dialog still open")
	}
	for _, f := range g.memory {
		if f.Name == e.File.Name && !strings.Contains(f.Content, "One more line.") {
			t.Fatalf("memory: %q", f.Content)
		}
	}
}

func TestAgentsSettings(t *testing.T) {
	a, _ := signedIn(t, "en")
	a.Navigate("/settings")
	a.sets.tab = 2
	tt := ui.NewTester(a.View, 1280, 1200)
	tt.Frame()
	tt.Frame()
	g := &a.sets
	if !g.triage.done {
		t.Fatalf("triage: %+v", g.triage)
	}
	if !tt.HasText(a.tr("setTriageDecide")) {
		t.Fatalf("rows: %v", tt.Texts())
	}
	shot(t, tt, "settings-agents")
	next := g.triageSet
	next.Enabled = !next.Enabled
	a.saveTriage(next)
	if g.triageSet.Enabled != next.Enabled {
		t.Fatalf("not saved: %+v", g.triageSet)
	}
}

func TestModelProfilesAndAppearance(t *testing.T) {
	a, _ := signedIn(t, "en")
	a.Navigate("/settings")
	a.sets.tab = modelsTab
	tt := ui.NewTester(a.View, 1280, 1600)
	tt.Frame()
	g := &a.sets
	g.profiles = hubapi.ModelProfiles{Active: "performance", Profiles: []hubapi.ModelProfile{
		{ID: "performance", Name: "Performance", Active: true}, {ID: "economy", Name: "Economy", Description: "Cheaper models", Changes: []string{"agent.new"}}}}
	g.profilesOK = true
	tt.Frame()
	if !tt.HasText(a.tr("setProfilesTitle")) || !tt.HasText("Economy") {
		t.Fatalf("profiles: %v", tt.Texts())
	}
	g.profileOpen = "economy"
	tt.Frame()
	if !tt.HasText(a.tr("setProfilesApply")) {
		t.Fatalf("dialog: %v", tt.Texts())
	}
	shot(t, tt, "settings-profiles")

	// Appearance: dark chosen draws dark whatever the system is
	a.sets.tab = 0
	a.Hubs.Settings.Appearance = "dark"
	tt.Frame()
	shot(t, tt, "settings-general-dark")
	if !a.dark(nil) {
		t.Fatal("not dark")
	}
	a.Hubs.Settings.Appearance = ""
}
