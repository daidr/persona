package app

import (
	"testing"
)

func TestProvidersSettings(t *testing.T) {
	a, tt := settingsAt(t, settingsMockURL, modelsTab, 1600)
	g := &a.sets.m8
	if len(g.provList) == 0 || !tt.HasText("gw_full") {
		t.Fatalf("providers: %+v %v", g.provList, tt.Texts())
	}
	shot(t, tt, "settings-providers")
	a.openWizard()
	tt.Frame()
	w := &g.wiz
	if !w.open || len(w.presets) == 0 {
		t.Fatalf("wizard: %+v", w)
	}
	shot(t, tt, "settings-providers-pick")
	a.pickPreset(nil)
	w.name, w.baseURL, w.key = "gw_new", "https://gw.example.com", "bad-key"
	a.wizardDo("test")
	tt.Frame()
	if w.ok || w.result == "" {
		t.Fatalf("a bad key passed: %+v", w)
	}
	w.key = "good-key"
	a.wizardDo("discover")
	tt.Frame()
	if len(w.models) != 3 || w.model == "" {
		t.Fatalf("discover: %+v", w.models)
	}
	a.wizardDo("test")
	tt.Frame()
	if !w.ok {
		t.Fatalf("test: %+v", w)
	}
	shot(t, tt, "settings-providers-wizard")
	a.wizardDo("save")
	tt.Frame()
	tt.Frame()
	if w.open || w.err != "" {
		t.Fatalf("save: %+v", w)
	}
	found := false
	for _, x := range g.provList {
		found = found || x.Name == "gw_new"
	}
	if !found {
		t.Fatalf("not listed: %+v", g.provList)
	}
	// nothing typed: the wizard says what's missing
	a.openWizard()
	a.pickPreset(nil)
	a.wizardDo("save")
	if w.err != a.tr("provLibKeyRequired") {
		t.Fatalf("empty: %q", w.err)
	}
}

func TestConnectorSignOut(t *testing.T) {
	a, tt := settingsAt(t, gitMockURL, 5, 1600)
	if !tt.HasText(a.tr("connSignOut")) {
		t.Fatalf("no Sign out: %v", tt.Texts())
	}
	if err := tt.Click(a.tr("connSignOut")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.sets.m8.signOut == "" {
		t.Fatal("no question")
	}
	shot(t, tt, "settings-connector-signout")
}

func TestExtensionSettings(t *testing.T) {
	a, tt := settingsAt(t, mockURL, 4, 1600)
	if !tt.HasText(a.tr("extBrowseMarket")) {
		t.Fatalf("extensions: %v", tt.Texts())
	}
	a.openExtension("example-notes")
	tt.Frame()
	tt.Frame()
	g := &a.sets.m8
	if g.extD == nil || g.extD.Form == nil {
		t.Fatalf("detail: %q %+v", g.extErr, g.extD)
	}
	if g.extDraft["base_url"] != "https://notes.example.com" || g.extDraft["proxy.port"] != "8080" || g.extDraft["mode"] != "safe" {
		t.Fatalf("draft: %+v", g.extDraft)
	}
	shot(t, tt, "settings-extension-light")
	tt.SetDark(true)
	shot(t, tt, "settings-extension-dark")
	tt.SetDark(false)
	g.extDraft["every_minutes"] = "abc"
	a.saveExtension()
	if g.extBusy {
		t.Fatal("saved a bad number")
	}
	g.extDraft["every_minutes"] = "60"
	g.extDraft["folders"] = "Work\nHome"
	g.extDraft["include_shared"] = "true"
	a.saveExtension()
	tt.Frame()
	tt.Frame()
	cfg := g.extD.Config
	if cfg["every_minutes"] != float64(60) || cfg["include_shared"] != true || len(cfg["folders"].([]any)) != 2 {
		t.Fatalf("saved: %+v", cfg)
	}
	if px, _ := cfg["proxy"].(map[string]any); px == nil || px["port"] != float64(8080) {
		t.Fatalf("proxy: %+v", cfg["proxy"])
	}
}

func TestExtensionMarket(t *testing.T) {
	a, tt := settingsAt(t, mockURL, 4, 1600)
	g := &a.sets.m8
	g.mkt = true
	tt.Frame()
	tt.Frame()
	if len(g.catalog) == 0 || !tt.HasText(a.tr("mktInstall")) {
		t.Fatalf("market: %q %v", g.mktLoad.err, tt.Texts())
	}
	shot(t, tt, "settings-market")
	g.mktQuery = "日历" // i18n-ignore: the mock's catalog
	tt.Frame()
	if !tt.HasText("Example 日历") || tt.HasText("每日摘要") { // i18n-ignore
		t.Fatalf("search: %v", tt.Texts())
	}
	if err := tt.Click(a.tr("mktInstall")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if g.pv == nil || len(g.pv.Permissions) == 0 {
		t.Fatalf("preview: %+v", g.pv)
	}
	shot(t, tt, "settings-market-preview")
	a.installExtension()
	tt.Frame()
	tt.Frame()
	if g.pv != nil || g.pvErr != "" {
		t.Fatalf("install: %q", g.pvErr)
	}
}
