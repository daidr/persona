package app

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

type inboxPage struct {
	list     ui.ListState
	showDone bool
	busy     map[string]bool
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
		if it.Connector != nil && it.Connector.URL != "" {
			if ui.Button(c, a.tr("openIn", "app", it.Connector.Label)).Clicked() {
				a.Host.OpenURL(it.Connector.URL)
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
		if it.Preview != "" {
			ui.Text(c, it.Preview).Selectable()
		}
		if it.Count > 1 {
			ui.Text(c, a.tr("inboxCount", "count", itoa(it.Count))).FontSize(small).TextColor(p.ink3)
		}
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
