package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

func TestMailReply(t *testing.T) {
	a, _ := signedIn(t, "en")
	const id = "mail:acc1:t4:504"
	a.Navigate("/inbox/" + id)
	tt := ui.NewTester(a.View, 1280, 1000)
	tt.Frame()
	tt.Frame()
	if !tt.HasText(a.tr("mailFrom")) || !tt.HasText("Lucy Wang <lucy@example.com>") {
		t.Fatalf("mail: %v", tt.Texts())
	}
	shot(t, tt, "inbox-mail-light")
	tt.SetDark(true)
	shot(t, tt, "inbox-mail-dark")
	tt.SetDark(false)

	// Reply: to the sender, Re:, the mail quoted
	if err := tt.Click(a.tr("mailReply")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	cs := &a.mailData().compose
	if !cs.open || cs.d.To != "lucy@example.com" || !strings.HasPrefix(cs.d.Subject, "Re: ") || !strings.Contains(cs.d.Body, "\n> ") {
		t.Fatalf("reply: %+v", cs.d)
	}
	cs.d.Body = "Tuesday works.\n" + cs.d.Body
	tt.Frame()
	shot(t, tt, "inbox-mail-compose")
	tt.SetDark(true)
	shot(t, tt, "inbox-mail-compose-dark")
	tt.SetDark(false)
	a.sendMail()
	tt.Frame()
	if cs.open || cs.err != "" {
		t.Fatalf("not sent: %+v", cs)
	}

	// Reply all leaves out my own address
	b := a.mailOf(a.inboxItem(id))
	all, ok := hubapi.ReplyAllDraft("acc1", b.msg, "me@example.com", "")
	if !ok || strings.Contains(all.To+all.Cc, "me@example.com") || !strings.Contains(all.To, "lucy@example.com") {
		t.Fatalf("reply all: %+v %v", all, ok)
	}
}

func TestMailCompose(t *testing.T) {
	a, _ := signedIn(t, "en")
	a.Navigate("/inbox")
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	if len(a.mailAccounts(false)) == 0 {
		t.Skip("no mail accounts in the mock")
	}
	a.newMail()
	tt.Frame()
	cs := &a.mailData().compose
	if !cs.open || cs.d.AccountID == "" || cs.from == a.tr("mailNoAccount") {
		t.Fatalf("new: %+v", cs)
	}
	// an address that isn't one is named
	cs.d.To = "lucy@example.com, not-an-address"
	a.sendMail()
	if cs.err != a.tr("mailBadAddresses", "addresses", "not-an-address") || !cs.open {
		t.Fatalf("bad address: %q", cs.err)
	}
	// closed without Cancel: kept for next time
	cs.d.To, cs.d.Subject = "lucy@example.com", "Kept"
	cs.open = false
	a.keepDraft()
	a.newMail()
	if a.mailData().compose.d.Subject != "Kept" {
		t.Fatalf("not kept: %+v", a.mailData().compose.d)
	}
	// Cancel drops it
	tt.Frame()
	if err := tt.Click(a.tr("cancel")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.newMail()
	if a.mailData().compose.d.Subject != "" {
		t.Fatalf("still kept: %+v", a.mailData().compose.d)
	}
}

func TestParseAddresses(t *testing.T) {
	got := hubapi.ParseAddresses(`"Lucy Wang" <lucy@example.com>; bob@example.com，` + "\nann@example.com")
	if len(got) != 3 || got[0].Name != "Lucy Wang" || got[0].Email != "lucy@example.com" || got[2].Email != "ann@example.com" {
		t.Fatalf("%+v", got)
	}
	if bad := hubapi.BadAddresses("a@b.co, nope"); len(bad) != 1 || bad[0] != "nope" {
		t.Fatalf("bad: %v", bad)
	}
	if s := hubapi.HTMLText("<style>x{}</style><p>Hi&nbsp;there</p><p>Two<br>lines &amp; more</p>"); s != "Hi there\nTwo\nlines & more" {
		t.Fatalf("html: %q", s)
	}
}

func TestConnectorThread(t *testing.T) {
	a, _ := signedInTo(t, gitMockURL, "en")
	tt := ui.NewTester(a.View, 1280, 1400)

	// GitHub: the pull request's header and timeline, and a comment
	a.Navigate("/inbox/github:review12")
	tt.Frame()
	tt.Frame()
	th := a.threads["github:review12"]
	if th == nil || !th.loaded || th.err != "" {
		t.Fatalf("thread: %+v", th)
	}
	if !tt.HasText(a.tr("threadStateOpen")) || !tt.HasText("bob "+a.tr("threadReviewApproved")) || !tt.HasText("uploader/retry.go:42") {
		t.Fatalf("github: %v", tt.Texts())
	}
	shot(t, tt, "inbox-github-light")
	tt.SetDark(true)
	shot(t, tt, "inbox-github-dark")
	tt.SetDark(false)
	th.text = "Looks good from the desktop"
	a.sendInThread(a.inboxItem("github:review12"), th)
	tt.Frame()
	if th.text != "" || th.page.Messages[len(th.page.Messages)-1].Text != "Looks good from the desktop" {
		t.Fatalf("comment: %+v", th.page.Messages[len(th.page.Messages)-1])
	}

	// Telegram: bubbles, quoting one to reply to it
	a.Navigate("/inbox/telegram:dm1")
	tt.Frame()
	tt.Frame()
	tg := a.threads["telegram:dm1"]
	if tg == nil || !tg.loaded || len(tg.page.Messages) == 0 {
		t.Fatalf("telegram: %+v", tg)
	}
	last := tg.page.Messages[len(tg.page.Messages)-1]
	tg.quote = &last
	tt.Frame()
	if !tt.HasText(a.tr("threadQuoted", "name", last.Sender.Name)) {
		t.Fatalf("quote: %v", tt.Texts())
	}
	shot(t, tt, "inbox-telegram-light")
	tt.SetDark(true)
	shot(t, tt, "inbox-telegram-dark")
	tt.SetDark(false)
}

func TestConfirmedFacts(t *testing.T) {
	a, _ := signedIn(t, "en")
	a.Navigate("/knowledge/projects/weather-app")
	tt := ui.NewTester(a.View, 1280, 3000)
	tt.Frame()
	tt.Frame()
	if !tt.HasText(a.tr("kbFacts")) {
		t.Fatalf("no facts card: %v", tt.Texts())
	}
	if err := tt.Click(a.tr("kbFactsAdd")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	f := &a.kb.facts
	f.draft = "Ships before the end of the month."
	tt.Frame()
	shot(t, tt, "kb-facts-edit")
	a.saveFacts("projects", "weather-app")
	tt.Frame()
	if f.open || f.err != "" || !tt.HasText("Ships before the end of the month.") {
		t.Fatalf("facts: %+v %v", f, tt.Texts())
	}
	shot(t, tt, "kb-facts")
	tt.SetDark(true)
	shot(t, tt, "kb-facts-dark")
	tt.SetDark(false)
}

func TestRelationshipRemoval(t *testing.T) {
	d := hubapi.KBDetail{Kind: "projects", ID: "weather-app", Meta: map[string]any{"people": []any{"p_han"}}}
	r := relationshipRemoval(d, "Weather", hubapi.KBEntry{Kind: "people", ID: "p_han", Name: "Han"})
	if r == nil || r.target != [2]string{"projects", "weather-app"} || r.patch["remove_people"].([]string)[0] != "p_han" || r.ends[1].field != "projects" {
		t.Fatalf("removal: %+v", r)
	}
	// provenance only: no removal
	if relationshipRemoval(d, "Weather", hubapi.KBEntry{Kind: "chats", ID: "oc_team"}) != nil {
		t.Fatal("chat removable")
	}
	// a document is taken off from its side
	d.Meta["items"] = []any{"docs/doc_widget"}
	if r := relationshipRemoval(d, "Weather", hubapi.KBEntry{Kind: "docs", ID: "doc_widget"}); r == nil || r.target != [2]string{"docs", "doc_widget"} {
		t.Fatalf("doc: %+v", r)
	}
	done := [2]hubapi.KBDetail{
		{Kind: "projects", ID: "weather-app", Meta: map[string]any{"people": []any{}, "locked_fields": []any{"people"}}},
		{Kind: "people", ID: "p_han", Meta: map[string]any{"locked_fields": []any{"projects"}}},
	}
	if !removalConfirmed(r0(t), done) {
		t.Fatal("not confirmed")
	}
	done[1].Meta["provenance"] = map[string]any{"projects": "extension"}
	if removalConfirmed(r0(t), done) {
		t.Fatal("confirmed while an extension holds it")
	}
	if canonicalKBID("recordings", "Meeting #1.") != "meeting_1" {
		t.Fatalf("canonical: %q", canonicalKBID("recordings", "Meeting #1."))
	}

	// the dialog: asked, written, read back
	a, _ := signedIn(t, "en")
	a.Navigate("/knowledge/projects/weather-app")
	tt := ui.NewTester(a.View, 1280, 3000)
	tt.Frame()
	tt.Frame()
	st := a.kbEntry("projects", "weather-app")
	st.page.Detail.Meta["people"] = []any{"p_han"}
	tt.Frame()
	if err := tt.Click(a.tr("kbRelAction")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.kb.rel.open || !tt.HasText(a.tr("kbRelLockTitle")) {
		t.Fatalf("dialog: %+v", a.kb.rel)
	}
	shot(t, tt, "kb-relationship")
	tt.SetDark(true)
	shot(t, tt, "kb-relationship-dark")
	tt.SetDark(false)
	a.removeRelationship()
	tt.Frame()
	if rd := a.kb.rel; rd.open && rd.phase != "unconfirmed" && rd.phase != "retry-write" {
		t.Fatalf("after removal: %+v", rd)
	}
}

func r0(t *testing.T) *relRemoval {
	r := relationshipRemoval(hubapi.KBDetail{Kind: "projects", ID: "weather-app", Meta: map[string]any{"people": []any{"p_han"}}}, "Weather",
		hubapi.KBEntry{Kind: "people", ID: "p_han", Name: "Han"})
	if r == nil {
		t.Fatal("no removal")
	}
	return r
}
