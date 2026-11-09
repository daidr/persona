package app

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

type inboxPage struct {
	list     ui.ListState
	showDone bool
	busy     map[string]bool
	// the reply being written: for which entry, its text, the instruction for a draft
	replyFor, reply, ask string
	drafting, sending    bool
}

func (a *App) inboxItems() []hubapi.InboxItem {
	var out []hubapi.InboxItem
	for _, it := range a.conn.data.Inbox {
		if it.Done() == a.inbox.showDone {
			out = append(out, it)
		}
	}
	return out
}

func (a *App) tierLabel(tier string) string {
	switch tier {
	case "action":
		return a.tr("tierAction")
	case "fyi":
		return a.tr("tierFyi")
	}
	return ""
}

// inboxView is the Inbox beside the entry chosen.
func (a *App) inboxView(c *ui.Context, p palette, open string) {
	g := &a.inbox
	items := a.inboxItems()
	now := time.Now()
	ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(listWidth).BorderWidth(0, 1, 0, 0).BorderColor(p.line).Children(func() {
			ui.Row(c).Padding(16, 16, 8, 16).Children(func() {
				ui.Text(c, a.tr("goInbox")).FontSize(title).Bold().Grow(1)
				accounts := a.mailAccounts(false)
				tip := a.tr("mailNew")
				if len(accounts) == 0 {
					tip = a.tr("mailNewNoAccount")
				}
				if ui.Button(c, "").Label(tip).Tooltip(tip).Disabled(len(accounts) == 0).Children(func() { ui.Icon(c, icon("compose")) }).Clicked() {
					a.newMail()
				}
			})
			tab := 0
			if g.showDone {
				tab = 1
			}
			if ui.Segmented(c, &tab, a.tr("inboxOpen"), a.tr("inboxDone")).Margin(0, 16, 8, 16).Changed() {
				g.showDone = tab == 1
			}
			g.list.Key = func(i int) any { return items[i].ID }
			ui.List(c, &g.list, len(items), func(i int) {
				it := items[i]
				chosen := it.ID == open
				bg := ui.Transparent
				if chosen {
					bg = p.hover
				}
				row := ui.ButtonBase(c.Key(it.ID)).Margin(0, 8).Padding(8, 10).Radius(rowRadius).Background(bg).Gap(4).
					Column().AlignItems(ui.Stretch).Label(it.Title)
				row.Children(func() {
					ui.Row(c).Gap(8).Children(func() {
						t := ui.Text(c, inboxTitle(it)).SingleLine().Grow(1).Shrink(1)
						if !it.Read && !it.Done() {
							t.Bold()
						}
						ui.Text(c, a.ago(it.UpdatedAt, now)).FontSize(small).TextColor(p.ink3).SingleLine()
					})
					line := it.Summary
					if line == "" {
						line = it.Preview
					}
					ui.Text(c, oneLine(line, 200)).FontSize(small).TextColor(p.ink2).MaxLines(2)
					if l := a.tierLabel(it.Tier); l != "" && !it.Done() {
						col := p.ink3
						if it.Tier == "action" {
							col = p.attention
						}
						ui.Text(c, l).FontSize(11).Bold().TextColor(col)
					}
				})
				if row.Hovered() && !chosen {
					row.Background(p.hover)
				}
				if row.Clicked() {
					a.Navigate("/inbox/" + it.ID)
				}
			}).Grow(1).PaddingY(4).Children(func() {
				if len(items) == 0 {
					if !a.conn.data.InboxLoaded {
						ui.Spinner(c).Margin(24, ui.Auto)
					} else if g.showDone {
						a.empty(c, p, "inbox", a.tr("inboxNoneDone"), "")
					} else {
						a.empty(c, p, "check", a.tr("inboxClear"), a.tr("inboxClearHint"))
					}
				}
			})
		})
		ui.Column(c).Grow(1).Children(func() {
			for _, it := range a.conn.data.Inbox {
				if it.ID == open {
					a.inboxDetail(c, p, it)
					return
				}
			}
			a.empty(c, p, "inbox", a.tr("inboxPick"), "")
		})
	})
	a.composeDialog(c, p)
}

func inboxTitle(it hubapi.InboxItem) string {
	if it.Title != "" {
		return it.Title
	}
	return it.Sender
}

func (a *App) inboxDetail(c *ui.Context, p palette, it hubapi.InboxItem) {
	sub := it.Sender
	if it.AccountName != "" {
		if sub != "" {
			sub += " · "
		}
		sub += it.AccountName
	}
	busy := a.inbox.busy[it.ID]
	a.pageHeader(c, p, inboxTitle(it), sub, func() {
		if it.Connector != nil && (it.Connector.URL != "" || it.Connector.Web != "") {
			app := it.AccountName
			if app == "" {
				app = it.Source
			}
			if ui.Button(c, a.tr("openIn", "app", app)).Clicked() {
				if it.Connector.URL != "" {
					a.Host.OpenURL(it.Connector.URL)
				} else {
					a.Host.OpenURL(it.Connector.Web)
				}
			}
		}
		label := a.tr("markDone")
		if it.Done() {
			label = a.tr("reopen")
		}
		if ui.PrimaryButton(c, label).Disabled(busy).Clicked() {
			a.markInbox(it, !it.Done())
		}
	})
	ui.Divider(c)
	ui.Scroll(c).Grow(1).Padding(16, pagePad).Gap(14).Children(func() {
		if it.Summary != "" {
			a.card(c, p, func() {
				ui.Row(c).Gap(6).Children(func() {
					ui.Icon(c, icon("spark")).FontSize(small).TextColor(p.ai)
					ui.Text(c, a.tr("inboxSummary")).FontSize(small).Bold().TextColor(p.ai)
				})
				ui.Text(c, it.Summary).Selectable()
				if it.Reason != "" {
					ui.Text(c, it.Reason).FontSize(small).TextColor(p.ink2).Selectable()
				}
			})
		}
		switch {
		case it.Mail != nil && it.Mail.UID() > 0:
			a.mailView(c, p, it)
		case it.Connector != nil && it.Connector.Thread != "":
			a.threadView(c, p, it)
		case it.Preview != "":
			ui.Text(c, it.Preview).Selectable()
		}
		if it.Count > 1 {
			ui.Text(c, a.tr("inboxCount", "count", itoa(it.Count))).FontSize(small).TextColor(p.ink3)
		}
		if !it.Done() {
			a.replyCard(c, p, it)
		}
	})
}

// replyCard answers an entry: a reply written here or drafted by Persona, sent where the entry came from; or the
// entry handed to Persona; or ignored.
func (a *App) replyCard(c *ui.Context, p palette, it hubapi.InboxItem) {
	g := &a.inbox
	if g.replyFor != it.ID {
		g.replyFor, g.reply, g.ask = it.ID, it.Drafted(), ""
	}
	// a connector's thread has its own box to answer in
	canReply := it.CanReply() && (it.Connector == nil || it.Connector.Thread == "")
	a.card(c, p, func() {
		if canReply {
			ui.Text(c, a.tr("inboxReply")).Bold()
			if d := it.Drafted(); d != "" {
				ui.Row(c).Gap(6).Children(func() {
					ui.Icon(c, icon("spark")).FontSize(small).TextColor(p.ai)
					ui.Text(c, a.tr("inboxDrafted")).FontSize(small).TextColor(p.ai)
				})
				if it.Agent.Note != "" {
					ui.Text(c, it.Agent.Note).FontSize(small).TextColor(p.ink2).Selectable()
				}
			}
			ui.TextArea(c.Key("reply-"+it.ID), &g.reply).Lines(3, 10).Placeholder(a.tr("inboxReplyPlaceholder")).Label(a.tr("inboxReply"))
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c.Key("ask-"+it.ID), &g.ask).Placeholder(a.tr("inboxAskPlaceholder")).Label(a.tr("inboxAskPlaceholder")).Grow(1)
				if g.drafting {
					ui.Spinner(c)
				}
				if ui.Button(c, a.tr("inboxDraft")).Disabled(g.drafting || g.sending).Clicked() {
					a.draftReply(it)
				}
			})
		}
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, a.tr("inboxHandoff")).Disabled(g.sending).Clicked() {
				a.handoff(it)
			}
			if ui.Button(c, a.tr("inboxIgnore")).Disabled(g.sending).Clicked() {
				a.ignoreInbox(it)
			}
			ui.Spacer(c)
			if canReply {
				if g.sending {
					ui.Spinner(c)
				}
				if ui.PrimaryButton(c, a.tr("inboxSend")).Disabled(g.sending || strings.TrimSpace(g.reply) == "").Clicked() {
					a.sendReply(it)
				}
			}
		})
	})
}

func (a *App) draftReply(it hubapi.InboxItem) {
	g := &a.inbox
	cn := a.conn
	ask := strings.TrimSpace(g.ask)
	g.drafting = true
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		text, err := cn.client.InboxDraft(ctx, it.ID, ask)
		a.Host.Post(func() {
			g.drafting = false
			if !a.live(cn) {
				return
			}
			if err != nil {
				a.toast(a.errorText(err))
				return
			}
			if g.replyFor == it.ID {
				g.reply = text
			}
		})
	})
}

func (a *App) sendReply(it hubapi.InboxItem) {
	g := &a.inbox
	cn := a.conn
	text := strings.TrimSpace(g.reply)
	g.sending = true
	id := hubapi.OpID()
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		got, err := cn.client.InboxReply(ctx, it.ID, text, id, it.Version)
		a.Host.Post(func() {
			g.sending = false
			if !a.live(cn) {
				return
			}
			if err != nil {
				// the text stays to send again; the same id won't send twice
				a.toast(a.errorText(err))
				return
			}
			g.reply, g.ask = "", ""
			if got.ID == "" {
				got = it
				got.DoneAt = time.Now().UTC().Format(time.RFC3339)
			}
			cn.data.PutInbox(got)
			a.toast(a.tr("inboxSent"))
		})
	})
}

func (a *App) handoff(it hubapi.InboxItem) {
	g := &a.inbox
	cn := a.conn
	text := strings.TrimSpace(g.ask)
	if text == "" {
		text = a.tr("inboxHandoffText")
	}
	g.sending = true
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_, err := cn.client.InboxHandoff(ctx, it.ID, text, hubapi.OpID())
		a.Host.Post(func() {
			g.sending = false
			if !a.live(cn) {
				return
			}
			if err != nil {
				a.toast(a.errorText(err))
				return
			}
			a.refreshAssistant(cn)
			a.Navigate("/")
		})
	})
}

func (a *App) ignoreInbox(it hubapi.InboxItem) {
	cn := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := cn.client.InboxDismiss(ctx, it.ID)
		a.Host.Post(func() {
			if !a.live(cn) {
				return
			}
			if err != nil {
				a.toast(a.errorText(err))
				return
			}
			cn.data.MarkInbox(it.ID, true, time.Now().UTC().Format(time.RFC3339))
		})
	})
}

func (a *App) markInbox(it hubapi.InboxItem, done bool) {
	c := a.conn
	if a.inbox.busy == nil {
		a.inbox.busy = map[string]bool{}
	}
	a.inbox.busy[it.ID] = true
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := c.client.InboxDone(ctx, it.ID, done)
		a.Host.Post(func() {
			delete(a.inbox.busy, it.ID)
			if !a.live(c) {
				return
			}
			if err != nil {
				a.toast(a.errorText(err))
				return
			}
			// the hub's inbox event brings the list as it now is
			c.data.MarkInbox(it.ID, done, time.Now().UTC().Format(time.RFC3339))
		})
	})
}

// inboxItem is the entry with id, as listed.
func (a *App) inboxItem(id string) hubapi.InboxItem {
	for _, it := range a.conn.data.Inbox {
		if it.ID == id {
			return it
		}
	}
	return hubapi.InboxItem{ID: id}
}
