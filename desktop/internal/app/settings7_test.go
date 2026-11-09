package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

func settingsAt(t *testing.T, url string, tab int, h int) (*App, *ui.Tester) {
	t.Helper()
	a, _ := signedInTo(t, url, "en")
	a.Navigate("/settings")
	a.sets.tab = tab
	tt := ui.NewTester(a.View, 1280, h)
	tt.Frame()
	tt.Frame()
	return a, tt
}

func TestHubUpdateSettings(t *testing.T) {
	a, tt := settingsAt(t, settingsMockURL, hubTab, 1400)
	g := &a.sets.m7
	if g.hubNone || g.hubSt.Version == "" || !tt.HasText(a.tr("hubUpUpdateHub")) {
		t.Fatalf("hub: %+v %v", g.hubSt, tt.Texts())
	}
	shot(t, tt, "settings-hub-light")
	tt.SetDark(true)
	shot(t, tt, "settings-hub-dark")
	tt.SetDark(false)
	if err := tt.Click(a.tr("hubUpUpdateHub")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if g.hubSt.State == "available" {
		t.Fatalf("not started: %+v", g.hubSt)
	}
	// an older hub has no update route
	b, tb := settingsAt(t, mockURL, hubTab, 600)
	if !b.sets.m7.hubNone || !tb.HasText(b.tr("hubUpUnsupported")) {
		t.Fatalf("old hub: %v", tb.Texts())
	}
}

func TestManagersSettings(t *testing.T) {
	a, tt := settingsAt(t, mockURL, managersTab, 1000)
	g := &a.sets.m7
	if len(g.mgrList) == 0 || !tt.HasText(a.tr("mgrDefault")) {
		t.Fatalf("managers: %+v %v", g.mgrList, tt.Texts())
	}
	shot(t, tt, "settings-managers")
	g.mgrNew, g.mgrName = true, "Work laptop"
	a.createManager()
	tt.Frame()
	if g.mgrCode == nil || g.mgrCode.Code == "" || !tt.HasText(a.tr("mgrCodeFor", "name", "Work laptop")) {
		t.Fatalf("code: %+v %v", g.mgrCode, tt.Texts())
	}
	shot(t, tt, "settings-managers-code")
	g.mgrCode = nil
	tt.Frame()
	found := false
	for _, m := range g.mgrList {
		found = found || m.Name == "Work laptop"
	}
	if !found {
		t.Fatalf("not listed: %+v", g.mgrList)
	}
	g.mgrNew, g.mgrName = true, ""
	a.createManager()
	if g.mgrErr != a.tr("mgrErrName") {
		t.Fatalf("empty name: %q", g.mgrErr)
	}
}

func TestSentSettings(t *testing.T) {
	a, tt := settingsAt(t, settingsMockURL, sentTab, 1400)
	g := &a.sets.m7
	if g.sentWhy != "" || len(g.sentItems) == 0 {
		t.Fatalf("sent: %q %d", g.sentWhy, len(g.sentItems))
	}
	g.sentOpen = g.sentItems[0].ID
	tt.Frame()
	if !tt.HasText(a.tr("sentDAllowed")) {
		t.Fatalf("detail: %v", tt.Texts())
	}
	shot(t, tt, "settings-sent-light")
	tt.SetDark(true)
	shot(t, tt, "settings-sent-dark")
	tt.SetDark(false)
	if g.sentNext > 0 {
		n := len(g.sentItems)
		a.moreSent()
		if len(g.sentItems) <= n {
			t.Fatalf("earlier: %d", len(g.sentItems))
		}
	}
}

func TestChannelsSettings(t *testing.T) {
	a, tt := settingsAt(t, settingsMockURL, channelsTab, 1600)
	g := &a.sets.m7
	if len(g.chanList) == 0 {
		t.Fatal("no channels")
	}
	shot(t, tt, "settings-channels-light")
	tt.SetDark(true)
	shot(t, tt, "settings-channels-dark")
	tt.SetDark(false)
	// a channel waiting to be bound opens its first step
	var todo *hubapi.Channel
	for i, ch := range g.chanList {
		if ch.Installed && ch.Enabled && (ch.Account == nil || ch.Account.Name == "") && (ch.Bind == "fields" || ch.Bind == "code" || ch.Bind == "qr") {
			todo = &g.chanList[i]
			break
		}
	}
	if todo == nil {
		t.Skip("nothing to bind in the mock")
	}
	a.startSignIn(true, todo.ID, todo.Name, nil)
	tt.Frame()
	si := &g.signIn
	if !si.open || si.err != "" || (len(si.prompt.Fields) == 0 && si.prompt.Kind == "" && si.prompt.PairCode == "") {
		t.Fatalf("bind: %+v", si)
	}
	shot(t, tt, "settings-channel-bind")
}

func TestMailAccountsSettings(t *testing.T) {
	a, tt := settingsAt(t, mockURL, 5, 1600)
	g := &a.sets.m7
	if !g.mail.done || len(g.mailList) == 0 {
		t.Fatalf("mail: %+v", g.mail)
	}
	a.openAddMail()
	tt.Frame()
	g.mailForm.Email = "not an address"
	a.addMail()
	tt.Frame()
	if !tt.HasText(a.tr("mailLibMailEmailInvalid")) || !tt.HasText(a.tr("mailLibMailPasswordEmpty")) {
		t.Fatalf("checks: %v", tt.Texts())
	}
	shot(t, tt, "settings-mail-add")
	g.mailForm.Email, g.mailForm.Password = "ann@example.com", "app-password"
	a.addMail()
	tt.Frame()
	if g.mailAdd {
		t.Fatalf("not added: %q", g.mailErr)
	}
	if req := hubapi.MailRequest(hubapi.MailForm{Kind: "imap", Email: "ann@example.com", SMTPHost: "smtp.example.com", SMTPPort: "587"}); req["smtp_security"] != "starttls" {
		t.Fatalf("request: %+v", req)
	}
}

func TestShortcutsSettings(t *testing.T) {
	a, tt := settingsAt(t, mockURL, shortcutsTab, 800)
	if !tt.HasText(a.tr("shortcutsPalette")) || !tt.HasText("Ctrl+Alt+P") {
		t.Fatalf("shortcuts: %v", tt.Texts())
	}
	shot(t, tt, "settings-shortcuts")
}
