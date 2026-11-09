package app

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// viewsPerReply is how many cards one reply draws; the others show their fallback (docs/assistant/assistant-views.md).
const viewsPerReply = 3

// cardState is a card's page stack and the action waiting on its confirmation.
type cardState struct {
	pages   []string
	confirm *hubapi.ViewAction
	asking  bool
	busy    bool
}

func (a *App) cardOf(key string) *cardState {
	if a.cards == nil {
		a.cards = map[string]*cardState{}
	}
	s := a.cards[key]
	if s == nil {
		s = &cardState{}
		a.cards[key] = s
	}
	return s
}

// richText draws an assistant's text: its Markdown and its cards. conversation is the assistant chat whose cards
// can be answered; "" draws them read-only.
func (a *App) richText(c *ui.Context, p palette, it hubapi.Item, conversation string) {
	if !strings.Contains(strings.ToLower(it.Text), "persona-view") {
		markdown.View(c.Key(it.ID), it.Text, &a.md)
		return
	}
	shown := 0
	ui.Column(c.Key(it.ID)).Gap(10).AlignItems(ui.Stretch).Children(func() {
		for i, part := range hubapi.SplitViews(it.Text) {
			switch {
			case !part.IsView:
				markdown.View(c.Key("md"+itoa(i)), part.Markdown, &a.md)
			case part.Open:
				ui.Row(c.Key("open")).Gap(8).Children(func() {
					ui.Spinner(c)
					ui.Text(c, a.tr("cardDrawing")).FontSize(small).TextColor(p.ink3)
				})
			case part.View == nil:
			case shown >= viewsPerReply:
				ui.Text(c.Key("fb"+itoa(i)), part.View.Fallback).TextColor(p.ink2).Selectable()
			default:
				shown++
				live := conversation != "" && it.Seq >= 0
				a.viewCard(c.Key("card"+itoa(i)), p, it.ID+"/"+itoa(part.Index), *part.View, conversation, live)
			}
		}
	})
}

// viewCard draws a card's page: the root, or the named page its actions opened.
func (a *App) viewCard(c *ui.Context, p palette, key string, root hubapi.View, conversation string, live bool) {
	cs := a.cardOf(key)
	v, page := root, ""
	if n := len(cs.pages); n > 0 {
		if pv, ok := root.Pages[cs.pages[n-1]]; ok {
			v, page = pv, cs.pages[n-1]
		}
	}
	unsupported := false
	ui.Column(c).Padding(14).Gap(12).Radius(cardRadius).Background(p.surface).Border(1, p.line).MaxWidth(620).Children(func() {
		if page != "" || v.Title != "" {
			ui.Row(c).Gap(8).Children(func() {
				if page != "" {
					if ui.Button(c, "").Label(a.tr("cardBack")).Tooltip(a.tr("cardBack")).Children(func() { ui.Icon(c, icon("up")) }).Padding(2, 6).Clicked() {
						cs.pages = cs.pages[:len(cs.pages)-1]
					}
				}
				if v.Title != "" {
					ui.Text(c, v.Title).Bold().Grow(1).Shrink(1)
				}
			})
		}
		for si, sec := range v.Sections {
			ui.Column(c.Key("s" + itoa(si))).Gap(8).AlignItems(ui.Stretch).Children(func() {
				if sec.Title != "" {
					ui.Text(c, sec.Title).FontSize(small).Bold().TextColor(p.ink2)
				}
				for ci, comp := range sec.Items {
					k := comp.Key
					if k == "" {
						k = "c" + itoa(ci)
					}
					if !a.viewComponent(c.Key(k), p, comp, func(act hubapi.ViewAction) { a.viewAct(key, page, conversation, act) }, live) {
						unsupported = true
					}
				}
				if sec.Footer != "" {
					ui.Text(c, sec.Footer).FontSize(11).TextColor(p.ink3)
				}
			})
		}
		if unsupported && root.Fallback != "" {
			ui.Text(c, root.Fallback).FontSize(small).TextColor(p.ink2).Selectable()
		}
	})
	if cs.confirm != nil {
		act := *cs.confirm
		switch ui.AlertDialog(c, &cs.asking, act.Confirm, "", a.tr("cancel"), a.tr("cardConfirm")) {
		case 1:
			cs.confirm = nil
			a.runAction(key, page, conversation, act)
		case 0:
			cs.confirm = nil
		}
		if !cs.asking {
			cs.confirm = nil
		}
	}
}

func (p palette) toneColor(tone string) ui.Color {
	switch tone {
	case "positive":
		return p.green
	case "caution":
		return p.orange
	case "danger":
		return p.red
	}
	return p.ink2
}

// viewComponent draws one component; false says the desktop app can't draw it.
func (a *App) viewComponent(c *ui.Context, p palette, comp hubapi.ViewComponent, act func(hubapi.ViewAction), live bool) bool {
	actionable := func(x *hubapi.ViewAction) bool {
		return x != nil && live && (x.Reply != "" || x.Open != "" || x.Push != nil || x.Pop)
	}
	switch comp.Type {
	case "text":
		t := ui.Text(c, comp.Text).Selectable()
		switch comp.Style {
		case "secondary":
			t.TextColor(p.ink2)
		case "caption":
			t.FontSize(small).TextColor(p.ink3)
		case "code":
			t.Font("monospace").FontSize(small)
		case "title":
			t.FontSize(title).Bold()
		case "headline":
			t.Bold()
		}
	case "markdown":
		markdown.View(c, comp.Text, &a.md)
	case "header":
		ui.Column(c).Gap(4).Children(func() {
			ui.Text(c, comp.Title).FontSize(heading).Bold()
			if comp.Subtitle != "" {
				ui.Text(c, comp.Subtitle).FontSize(small).TextColor(p.ink2)
			}
			if len(comp.Badges) > 0 {
				ui.Row(c).Wrap().Gap(6).Children(func() {
					for _, b := range comp.Badges {
						a.pill(c, b.Text, p.toneColor(b.Tone))
					}
				})
			}
			for _, f := range comp.Facts {
				ui.Row(c.Key("f" + f.Label)).Gap(8).Children(func() {
					ui.Text(c, f.Label).FontSize(small).TextColor(p.ink3)
					ui.Text(c, f.Value).FontSize(small).Selectable()
				})
			}
		})
	case "row":
		on := actionable(comp.Action)
		row := ui.ButtonBase(c).Padding(8, 10).Radius(rowRadius).Gap(10).Background(p.inset).Label(comp.Title).Disabled(!on)
		row.Children(func() {
			ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
				ui.Text(c, comp.Title).Bold()
				if comp.Subtitle != "" {
					ui.Text(c, comp.Subtitle).FontSize(small).TextColor(p.ink2)
				}
			})
			if comp.Value != "" {
				ui.Text(c, comp.Value).FontSize(small).TextColor(p.ink2)
			}
			if comp.Badge != nil {
				a.pill(c, comp.Badge.Text, p.toneColor(comp.Badge.Tone))
			}
		})
		if on && row.Hovered() {
			row.Background(p.hover)
		}
		if on && row.Clicked() {
			act(*comp.Action)
		}
	case "button":
		if b := ui.Button(c, comp.Title).Disabled(!actionable(comp.Action)); b.Clicked() {
			act(*comp.Action)
		}
	case "link":
		if strings.HasPrefix(comp.URL, "https://") {
			if ui.Button(c, comp.Title).Tooltip(comp.URL).Clicked() {
				a.Host.OpenURL(comp.URL)
			}
		}
	case "divider":
		ui.Divider(c)
	case "metric":
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, comp.Label).FontSize(small).TextColor(p.ink2)
			ui.Text(c, comp.Value).FontSize(22).Bold().TextColor(metricColor(p, comp.Tone))
			if comp.Detail != "" {
				ui.Text(c, comp.Detail).FontSize(small).TextColor(p.ink3)
			}
		})
	case "stat_grid":
		ui.Row(c).Wrap().Gap(10).Children(func() {
			for i, s := range comp.Stats {
				ui.Column(c.Key("st"+itoa(i))).Width(130).Padding(8, 10).Radius(rowRadius).Background(p.inset).Gap(2).Children(func() {
					ui.Text(c, s.Label).FontSize(11).TextColor(p.ink2).SingleLine()
					ui.Text(c, s.Value).Bold().TextColor(metricColor(p, s.Tone)).SingleLine()
					if s.Detail != "" {
						ui.Text(c, s.Detail).FontSize(11).TextColor(p.ink3).SingleLine()
					}
				})
			}
		})
	case "progress":
		ratio := min(1, max(0, comp.Ratio))
		ui.Column(c).Gap(4).Children(func() {
			ui.Row(c).Children(func() {
				ui.Text(c, comp.Label).FontSize(small).Grow(1)
				ui.Text(c, strconv.Itoa(int(ratio*100+0.5))+"%").FontSize(small).TextColor(p.ink2)
			})
			ui.Box(c).Height(6).Radius(3).Background(p.field).Children(func() {
				col := p.accent
				if comp.Tone != "" && comp.Tone != "neutral" {
					col = p.toneColor(comp.Tone)
				}
				ui.Box(c).WidthPercent(float32(ratio * 100)).Height(6).Radius(3).Background(col)
			})
			if comp.Detail != "" {
				ui.Text(c, comp.Detail).FontSize(11).TextColor(p.ink3)
			}
		})
	case "table":
		ui.Column(c).Radius(rowRadius).Border(1, p.line).Children(func() {
			tableRow := func(k string, cells []string, head bool) {
				r := ui.Row(c.Key(k)).Padding(6, 10).Gap(12)
				if head {
					r.Background(p.inset)
				}
				r.Children(func() {
					for i := range comp.Columns {
						cell := ""
						if i < len(cells) {
							cell = cells[i]
						}
						t := ui.Text(c.Key("c"+itoa(i)), cell).FontSize(small).Grow(1).Shrink(1).Basis(0).Selectable()
						if head {
							t.Bold().TextColor(p.ink2)
						}
						if comp.Columns[i].Align == "end" || comp.Columns[i].Align == "right" {
							t.TextAlign(ui.End)
						}
					}
				})
			}
			titles := make([]string, len(comp.Columns))
			for i, col := range comp.Columns {
				titles[i] = col.Title
			}
			tableRow("head", titles, true)
			for i, r := range comp.Rows {
				tableRow("r"+itoa(i), r, false)
			}
		})
		if comp.Caption != "" {
			ui.Text(c, comp.Caption).FontSize(11).TextColor(p.ink3)
		}
	case "code":
		ui.Column(c).Padding(8, 10).Radius(rowRadius).Background(p.inset).Gap(4).Children(func() {
			if comp.Path != "" {
				ui.Text(c, comp.Path).FontSize(11).TextColor(p.ink3)
			}
			ui.Text(c, comp.Text).Font("monospace").FontSize(small).Selectable()
		})
	case "empty":
		ui.Column(c).Gap(4).Padding(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, comp.Title).Bold().TextColor(p.ink2)
			if comp.Detail != "" {
				ui.Text(c, comp.Detail).FontSize(small).TextColor(p.ink3)
			}
		})
	default:
		return false
	}
	return true
}

func metricColor(p palette, tone string) ui.Color {
	if tone == "" || tone == "neutral" {
		return p.ink
	}
	return p.toneColor(tone)
}

// viewAct runs an action, asking first when it says to.
func (a *App) viewAct(key, page, conversation string, act hubapi.ViewAction) {
	if act.Confirm != "" {
		cs := a.cardOf(key)
		cs.confirm, cs.asking = &act, true
		return
	}
	a.runAction(key, page, conversation, act)
}

func (a *App) runAction(key, page, conversation string, act hubapi.ViewAction) {
	cs := a.cardOf(key)
	switch {
	case act.Pop:
		if n := len(cs.pages); n > 0 {
			cs.pages = cs.pages[:n-1]
		}
	case act.Push != nil && act.Push.Page != "":
		if len(cs.pages) < 4 {
			cs.pages = append(cs.pages, act.Push.Page)
		}
	case act.Open != "":
		a.openTarget(act.Open)
	case act.Reply != "":
		a.replyAction(key, page, conversation, act)
	}
}

// openTarget opens an action's target: an https page in the browser, or an agent, a to-do or the knowledge base in the app.
func (a *App) openTarget(target string) {
	if strings.HasPrefix(target, "https://") {
		a.Host.OpenURL(target)
		return
	}
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "persona" {
		return
	}
	seg := strings.Split(strings.Trim(u.Opaque+u.Host+u.EscapedPath(), "/"), "/")
	if u.Opaque != "" {
		seg = strings.Split(strings.Trim(u.Opaque, "/"), "/")
	}
	for i := range seg {
		if s, err := url.PathUnescape(seg[i]); err == nil {
			seg[i] = s
		}
	}
	switch {
	case len(seg) == 3 && seg[0] == "session":
		a.Navigate("/agents/" + seg[1] + "/" + seg[2])
	case len(seg) == 2 && seg[0] == "task":
		a.Navigate("/todos/" + seg[1])
	case len(seg) >= 2 && seg[0] == "kb":
		a.Navigate("/knowledge")
	}
}

// replyAction answers the card through the hub, which sends the reply the card declared; a hub without card
// actions gets the reply as a message.
func (a *App) replyAction(key, page, conversation string, act hubapi.ViewAction) {
	c := a.conn
	cs := a.cardOf(key)
	if cs.busy {
		return
	}
	cs.busy = true
	sessionKey := c.data.Assistant.MachineID + "/" + conversation
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := c.client.ViewAction(ctx, conversation, key, act.ID, page, hubapi.OpID())
		a.Host.Post(func() {
			cs.busy = false
			if !a.live(c) {
				return
			}
			var he *hubapi.Error
			switch {
			case err == nil:
			case errors.As(err, &he) && he.Status == 404:
				a.send(sessionKey, act.Reply)
			default:
				a.toast(a.errorText(err))
			}
		})
	})
}
