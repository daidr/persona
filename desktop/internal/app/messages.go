package app

import (
	"context"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// messagesState is Messages (docs/extensions/lark-migration.md "Entry"): every Feishu private and group chat in one
// list, and the one open beside it.
type messagesState struct {
	list   loadable
	chats  []hubapi.LarkConversation
	pinned []hubapi.LarkConversation
	next   string
	more   bool
	query  string
	open   map[string]*larkChat
}

// larkChat is one chat as read: its window, the box, and what's on its way.
type larkChat struct {
	page          hubapi.LarkPage
	loaded, older bool
	err           string
	draft         string
	reported      bool
	scroll        ui.ScrollState
}

func (a *App) larkOn() bool {
	return a.conn != nil && slices.Contains(a.conn.data.Hello.Features, hubapi.FeatureLark)
}

func (a *App) messagesView(c *ui.Context, p palette, open string) {
	g := &a.msgs
	if !a.larkOn() {
		a.empty(c, p, "chat", a.tr("msgNoLarkTitle"), a.tr("msgNoLarkHint"))
		return
	}
	a.load(&g.list, func(ctx context.Context, cl *hubapi.Client) (func(), error) {
		chats, next, err := cl.LarkChats(ctx, "")
		if err != nil {
			return nil, err
		}
		pinned, _ := cl.LarkPinned(ctx)
		return func() { g.chats, g.next, g.pinned = chats, next, pinned }, nil
	})
	ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(340).BorderWidth(0, 1, 0, 0).BorderColor(p.line).Children(func() {
			a.larkList(c, p, open)
		})
		ui.Column(c).Grow(1).Children(func() {
			var chat *hubapi.LarkConversation
			for _, x := range append(slices.Clone(g.pinned), g.chats...) {
				if x.ID == open {
					chat = &x
					break
				}
			}
			switch {
			case open == "":
				a.empty(c, p, "chat", a.tr("msgPickTitle"), a.tr("msgPickHint"))
			default:
				a.larkChatView(c, p, open, chat)
			}
		})
	})
}

func (a *App) larkList(c *ui.Context, p palette, open string) {
	g := &a.msgs
	ui.Row(c).Padding(16, 16, 8, 16).Gap(8).Children(func() {
		ui.Text(c, a.tr("msgTitle")).FontSize(title).Bold().Grow(1)
		if ui.Button(c, a.tr("msgRefresh")).Disabled(g.list.busy).Clicked() {
			g.list.asked = false
		}
	})
	ui.SearchField(c.Key("lark-search"), &g.query).Placeholder(a.tr("msgSearchPlaceholder")).Margin(0, 16, 8, 16)
	switch {
	case g.list.err != "" && len(g.chats) == 0:
		ui.Column(c).Padding(16).Gap(8).Children(func() {
			ui.Text(c, a.tr("msgLoadFailed", "error", g.list.err)).FontSize(small).TextColor(p.red)
			if ui.Button(c, a.tr("msgRetry")).Clicked() {
				g.list.asked, g.list.err = false, ""
			}
		})
		return
	case !g.list.done:
		ui.Spinner(c).Margin(24, ui.Auto)
		return
	}
	q := strings.ToLower(strings.TrimSpace(g.query))
	match := func(x hubapi.LarkConversation) bool {
		return q == "" || strings.Contains(strings.ToLower(x.Name+" "+x.Preview+" "+x.PreviewSender), q)
	}
	seen := map[string]bool{}
	var rows []hubapi.LarkConversation
	for _, x := range append(slices.Clone(g.pinned), g.chats...) {
		if !seen[x.ID] && match(x) {
			seen[x.ID] = true
			rows = append(rows, x)
		}
	}
	if len(rows) == 0 {
		if q != "" {
			a.empty(c, p, "search", a.tr("msgEmptyNoMatch"), a.tr("msgEmptyNoMatchHint"))
		} else {
			a.empty(c, p, "chat", a.tr("msgEmptyNone"), a.tr("msgEmptyNoneHint"))
		}
		return
	}
	now := time.Now()
	ui.Scroll(c.Key("lark-list")).Grow(1).Children(func() {
		ui.Column(c).Padding(0, 8, 8, 8).Gap(2).Children(func() {
			for _, x := range rows {
				a.larkRow(c.Key("lk-"+x.ID), p, x, x.ID == open, now)
			}
			if g.next != "" {
				ui.Row(c).Padding(8).Children(func() {
					ui.Spacer(c)
					if g.more {
						ui.Spinner(c)
					} else if ui.Button(c, a.tr("filesMore")).Clicked() {
						a.moreLark()
					}
					ui.Spacer(c)
				})
			}
		})
	})
}

func (a *App) moreLark() {
	g := &a.msgs
	cn, cursor := a.conn, g.next
	g.more = true
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		chats, next, err := cn.client.LarkChats(ctx, cursor)
		a.Host.Post(func() {
			g.more = false
			if !a.live(cn) {
				return
			}
			if err != nil {
				a.toast(a.tr("msgLoadFailed", "error", a.errorText(err)))
				return
			}
			have := map[string]bool{}
			for _, x := range g.chats {
				have[x.ID] = true
			}
			for _, x := range chats {
				if !have[x.ID] {
					g.chats = append(g.chats, x)
				}
			}
			g.next = next
		})
	})
}

// larkPreview is a row's second line: "Me: …", the sender in a group, or the text.
func (a *App) larkPreview(x hubapi.LarkConversation) string {
	text := strings.TrimSpace(x.Preview)
	switch {
	case text == "":
		return ""
	case x.IsMine:
		return a.tr("msgPreviewMine", "text", text)
	case x.IsGroup && x.PreviewSender != "":
		return a.tr("msgPreviewSender", "sender", x.PreviewSender, "text", text)
	}
	return text
}

func (a *App) larkRow(c *ui.Context, p palette, x hubapi.LarkConversation, on bool, now time.Time) {
	btn := ui.ButtonBase(c).Label(x.Name).Padding(8, 10).Radius(rowRadius)
	switch {
	case on:
		btn.Background(p.hover)
	case btn.Hovered():
		btn.Background(p.hover)
	}
	btn.Children(func() {
		ui.Row(c).Gap(10).Grow(1).Children(func() {
			a.monogram(c, p, x.Name, x.IsGroup)
			ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
				ui.Row(c).Gap(6).Children(func() {
					ui.Text(c, x.Name).Bold().SingleLine().Shrink(1)
					if x.Pinned {
						ui.Icon(c, icon("pin")).FontSize(small).TextColor(p.ink3)
					}
					ui.Spacer(c)
					ui.Text(c, a.ago(x.UpdatedAt, now)).FontSize(11).TextColor(p.ink3).SingleLine()
				})
				ui.Row(c).Gap(6).Children(func() {
					switch {
					case x.AtMe:
						ui.Text(c, a.tr("msgAtMe")).FontSize(small).TextColor(p.red).SingleLine()
					case x.AtAll:
						ui.Text(c, a.tr("msgAtAll")).FontSize(small).TextColor(p.orange).SingleLine()
					}
					ui.Text(c, a.larkPreview(x)).FontSize(small).TextColor(p.ink3).SingleLine().Grow(1).Shrink(1)
					if n := x.Unread; n > 0 || x.MarkedUnread {
						b := ui.Badge(c, itoa(max(n, 1)))
						if x.Muted {
							b.Background(p.hover).TextColor(p.ink2)
						}
					}
				})
			})
		})
	})
	if btn.Clicked() {
		a.Navigate("/messages/" + x.ID)
	}
}

// monogram is a chat's face: its first letter on a tile, rounder for a person than for a group.
func (a *App) monogram(c *ui.Context, p palette, name string, group bool) {
	r := []rune(strings.TrimSpace(name))
	letter := "?"
	if len(r) > 0 {
		letter = strings.ToUpper(string(r[0]))
	}
	radius := float32(18)
	if group {
		radius = 8
	}
	ui.Column(c).Size(36, 36).Radius(radius).Background(p.inset).Center().Children(func() {
		ui.Text(c, letter).Bold().TextColor(p.ink2)
	})
}

func (a *App) larkChat(id string) *larkChat {
	g := &a.msgs
	if g.open == nil {
		g.open = map[string]*larkChat{}
	}
	t, ok := g.open[id]
	if !ok {
		t = &larkChat{}
		g.open[id] = t
		a.readLark(id, t)
	}
	return t
}

func (a *App) readLark(id string, t *larkChat) {
	cn := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pg, err := cn.client.LarkMessages(ctx, id, "")
		a.Host.Post(func() {
			if !a.live(cn) {
				return
			}
			t.loaded = true
			if err != nil {
				t.err = a.errorText(err)
				return
			}
			t.err = ""
			// keep what is on its way or failed: the hub's copy (same client id) takes a sent one's place
			echoed := map[string]bool{}
			for _, m := range pg.Messages {
				if m.ClientID != "" {
					echoed[m.ClientID] = true
				}
			}
			for _, m := range t.page.Messages {
				if (m.Sending || m.Failed) && !echoed[m.ClientID] {
					pg.Messages = append(pg.Messages, m)
				}
			}
			if t.page.HasOlder || len(t.page.Messages) > len(pg.Messages) {
				pg = hubapi.OlderLark(pg, hubapi.LarkPage{Messages: olderThan(t.page.Messages, pg), HasOlder: t.page.HasOlder, OlderCursor: t.page.OlderCursor})
			}
			t.page = pg
			t.scroll.Y = math.MaxFloat32
			a.reportLarkRead(id, t)
		})
	})
}

// olderThan are the messages of what was shown from before the new window.
func olderThan(have []hubapi.LarkMessage, pg hubapi.LarkPage) []hubapi.LarkMessage {
	if len(pg.Messages) == 0 {
		return nil
	}
	first := pg.Messages[0].Position
	var out []hubapi.LarkMessage
	for _, m := range have {
		if !m.Sending && !m.Failed && comparePos(m.Position, first) < 0 {
			out = append(out, m)
		}
	}
	return out
}

func comparePos(a, b string) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}

// reportLarkRead tells Feishu the chat was read once it has been opened with something unread; a topic group's read
// state is per topic, so it is left alone.
func (a *App) reportLarkRead(id string, t *larkChat) {
	g := &a.msgs
	if t.reported || t.page.Topic {
		return
	}
	idx := slices.IndexFunc(g.chats, func(x hubapi.LarkConversation) bool { return x.ID == id })
	if idx < 0 || (g.chats[idx].Unread == 0 && !g.chats[idx].MarkedUnread) {
		return
	}
	t.reported = true
	cn, page := a.conn, t.page
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := cn.client.LarkRead(ctx, id, page)
		a.Host.Post(func() {
			if !a.live(cn) || err != nil {
				return
			}
			for i := range g.chats {
				if g.chats[i].ID == id {
					g.chats[i].Unread, g.chats[i].MarkedUnread, g.chats[i].AtMe, g.chats[i].AtAll = 0, false, false, false
				}
			}
		})
	})
}

func (a *App) olderLark(id string, t *larkChat) {
	cn, before := a.conn, t.page.OlderCursor
	t.older = true
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pg, err := cn.client.LarkMessages(ctx, id, before)
		a.Host.Post(func() {
			t.older = false
			if !a.live(cn) {
				return
			}
			if err != nil {
				a.toast(a.tr("msgOlderFailed"))
				return
			}
			t.page = hubapi.OlderLark(t.page, pg)
		})
	})
}

func (a *App) sendLark(id string, t *larkChat) {
	text := strings.TrimSpace(t.draft)
	if text == "" {
		return
	}
	cid := hubapi.OpID()
	t.draft = ""
	t.page.Messages = append(t.page.Messages, hubapi.LarkMessage{ID: "temp-" + cid, ClientID: cid, IsMine: true, Text: text, Sending: true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339)})
	t.scroll.Y = math.MaxFloat32
	a.pushLark(id, t, cid, text)
}

func (a *App) pushLark(id string, t *larkChat, cid, text string) {
	cn := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		err := cn.client.SendLark(ctx, id, text, cid)
		a.Host.Post(func() {
			if !a.live(cn) {
				return
			}
			for i := range t.page.Messages {
				if t.page.Messages[i].ClientID == cid {
					t.page.Messages[i].Sending = false
					t.page.Messages[i].Failed = err != nil
				}
			}
			if err != nil {
				a.toast(a.tr("msgSendFailed", "error", a.errorText(err)))
				return
			}
			a.readLark(id, t)
		})
	})
}

func (a *App) larkChatView(c *ui.Context, p palette, id string, chat *hubapi.LarkConversation) {
	t := a.larkChat(id)
	name, kind := id, a.tr("msgKindDm")
	if chat != nil {
		name = chat.Name
		switch {
		case chat.IsTopic:
			kind = a.tr("msgKindTopic")
		case chat.IsGroup:
			kind = a.tr("msgKindGroup")
		}
	}
	a.pageHeader(c, p, name, kind, func() {
		if ui.Button(c, a.tr("msgRefresh")).Clicked() {
			a.readLark(id, t)
		}
	})
	ui.Divider(c)
	if !t.loaded {
		ui.Spinner(c).Margin(ui.Auto)
		return
	}
	if t.err != "" && len(t.page.Messages) == 0 {
		ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
			a.empty(c, p, "warning", a.tr("msgChatFailed"), t.err)
			if ui.Button(c, a.tr("msgRetry")).Clicked() {
				a.readLark(id, t)
			}
		})
		return
	}
	ui.Scroll(c.Key("lark-chat-" + id)).Grow(1).TrackScroll(&t.scroll).Children(func() {
		ui.Column(c).Padding(12, pagePad).Gap(6).Children(func() {
			if t.page.HasOlder {
				ui.Row(c).Children(func() {
					ui.Spacer(c)
					if t.older {
						ui.Spinner(c)
					} else if ui.Button(c, a.tr("msgOlder")).Clicked() {
						a.olderLark(id, t)
					}
					ui.Spacer(c)
				})
			}
			if len(t.page.Messages) == 0 {
				ui.Text(c, a.tr("msgEmpty")).FontSize(small).TextColor(p.ink3).TextAlign(ui.Center)
			}
			prev := ""
			for _, m := range t.page.Messages {
				a.larkBubble(c.Key("lm-"+m.ID), p, id, t, m, m.SenderName != prev)
				prev = m.SenderName
				if m.System() {
					prev = ""
				}
			}
		})
	})
	ui.Row(c).Padding(10, pagePad, 16, pagePad).Gap(8).AlignItems(ui.End).Children(func() {
		placeholder := a.tr("msgSendTo")
		in := ui.TextArea(c.Key("lark-box-"+id), &t.draft).Lines(1, 8).Placeholder(placeholder).Label(placeholder).Grow(1)
		send := false
		in.OnTextKey(func(k ui.TextKey) bool {
			if k.Key == ui.KeyEnter && k.Mods == 0 {
				send = true
				return true
			}
			return false
		})
		if ui.PrimaryButton(c, "").Label(a.tr("send")).Tooltip(a.tr("send")).Children(func() { ui.Icon(c, icon("send")) }).
			Disabled(strings.TrimSpace(t.draft) == "").Clicked() {
			send = true
		}
		if send {
			a.sendLark(id, t)
		}
	})
}

// larkPartText is a part a list can't draw, in words: a picture, a file, a voice note, a card.
func (a *App) larkPartText(pt hubapi.LarkPart) string {
	switch pt.Kind {
	case "image":
		return a.tr("msgImage")
	case "sticker":
		return a.tr("msgSticker")
	case "audio", "voice":
		return a.tr("msgVoice")
	case "file", "media", "video":
		if pt.Name != "" {
			return a.tr("msgFile") + " · " + pt.Name
		}
		return a.tr("msgFile")
	case "card", "interactive":
		return a.tr("msgCard")
	case "doc", "docs":
		if pt.Name != "" {
			return a.tr("msgDoc") + " · " + pt.Name
		}
		return a.tr("msgDoc")
	}
	return ""
}

func (a *App) larkBubble(c *ui.Context, p palette, id string, t *larkChat, m hubapi.LarkMessage, head bool) {
	if m.System() {
		ui.Text(c, hubapi.LarkText(m)).FontSize(11).TextColor(p.ink3).TextAlign(ui.Center).Margin(4, 0)
		return
	}
	ui.Row(c).Gap(8).Children(func() {
		if m.IsMine {
			ui.Spacer(c)
		}
		ui.Column(c).MaxWidth(520).Gap(2).Children(func() {
			if head && !m.IsMine && m.SenderName != "" {
				ui.Text(c, m.SenderName).FontSize(11).Bold().TextColor(p.ink2).Padding(0, 4)
			}
			bg := p.surface
			if m.IsMine {
				bg = p.userBubble
			}
			ui.Column(c).Padding(8, 12).Radius(cardRadius).Background(bg).Border(1, p.line).Gap(4).Children(func() {
				if q := m.ReplyTo; q != nil {
					line := oneLine(q.Text, 160)
					if q.SenderName != "" {
						line = q.SenderName + ": " + line
					}
					ui.Text(c, line).FontSize(small).TextColor(p.ink3).SingleLine()
				}
				if m.Recalled {
					ui.Text(c, a.tr("msgRecalled")).FontSize(small).TextColor(p.ink3)
					return
				}
				text := strings.TrimSpace(m.Text)
				if text != "" {
					ui.Text(c, text).Selectable()
				}
				for i, pt := range m.Parts {
					if text == "" && pt.Text != "" {
						ui.Text(c.Key("pt"+itoa(i)), pt.Text).Selectable()
						continue
					}
					if line := a.larkPartText(pt); line != "" {
						ui.Row(c.Key("pm" + itoa(i))).Gap(6).Children(func() {
							ui.Icon(c, icon("clip")).FontSize(small).TextColor(p.ink3)
							ui.Text(c, line).FontSize(small).TextColor(p.ink2)
						})
					}
				}
			})
			ui.Row(c).Gap(8).Padding(0, 4).Children(func() {
				line := clockOf(m.CreatedAt)
				if m.Edited {
					line += " · " + a.tr("msgEdited")
				}
				if n := max(m.ReplyCount, m.ThreadReplies); n > 0 {
					line += " · " + a.tr("msgReplies", "count", itoa(n))
				}
				ui.Text(c, line).FontSize(11).TextColor(p.ink3)
				switch {
				case m.Sending:
					ui.Text(c, a.tr("msgSending")).FontSize(11).TextColor(p.ink3)
				case m.Failed:
					if a.textLink(c, p, "retry-"+m.ClientID, a.tr("msgFailedRetry"), 11) {
						for i := range t.page.Messages {
							if t.page.Messages[i].ClientID == m.ClientID {
								t.page.Messages[i].Failed, t.page.Messages[i].Sending = false, true
							}
						}
						a.pushLark(id, t, m.ClientID, m.Text)
					}
				}
			})
		})
		if !m.IsMine {
			ui.Spacer(c)
		}
	})
}
