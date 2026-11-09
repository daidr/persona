package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// A connector's entry in the Inbox opens its thread as the hub reads it (GET /connectors/{id}/thread): a chat app's
// conversation as bubbles, a GitHub pull request or issue as its timeline, earlier pages on demand, and a box to answer
// in when the thread names its reply action (POST …/thread/reply). A thread that can't be read shows why; the entry's
// summary and preview stay above it.

type threadState struct {
	page     hubapi.ThreadPage
	loaded   bool
	loading  bool
	err      string
	older    bool
	text     string
	quote    *hubapi.ThreadMessage
	sending  bool
	openLogs map[string]bool
}

func (a *App) threadOf(it hubapi.InboxItem) *threadState {
	if a.threads == nil || a.threadsConn != a.conn {
		a.threads, a.threadsConn = map[string]*threadState{}, a.conn
	}
	t := a.threads[it.ID]
	if t == nil {
		t = &threadState{openLogs: map[string]bool{}}
		a.threads[it.ID] = t
		a.readThread(it, t)
	}
	return t
}

// readThread reads the newest page and puts it after what is kept.
func (a *App) readThread(it hubapi.InboxItem, t *threadState) {
	if t.loading {
		return
	}
	t.loading = true
	cn := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		pg, err := cn.client.Thread(ctx, it.Source, it.Connector.Thread, "")
		a.Host.Post(func() {
			t.loading = false
			if !a.live(cn) {
				return
			}
			if err != nil {
				if !t.loaded {
					t.err = a.errorText(err)
				}
				return
			}
			if t.loaded {
				t.page = hubapi.NewestThread(t.page, pg)
			} else {
				t.page = pg
			}
			t.loaded, t.err = true, ""
		})
	})
}

func (a *App) olderThread(it hubapi.InboxItem, t *threadState) {
	if t.older || t.page.Before == "" {
		return
	}
	t.older = true
	cn := a.conn
	before := t.page.Before
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		pg, err := cn.client.Thread(ctx, it.Source, it.Connector.Thread, before)
		a.Host.Post(func() {
			t.older = false
			if !a.live(cn) {
				return
			}
			if err != nil {
				a.toast(a.tr("threadOlderFailed", "error", a.errorText(err)))
				return
			}
			if t.page.Before == before {
				t.page = hubapi.OlderThread(t.page, pg)
			}
		})
	})
}

func (a *App) sendInThread(it hubapi.InboxItem, t *threadState) {
	text := strings.TrimSpace(t.text)
	if text == "" || t.sending || t.page.Chat.Reply == "" {
		return
	}
	quote := ""
	if t.quote != nil {
		quote = t.quote.ID
	}
	t.sending = true
	cn := a.conn
	action := t.page.Chat.Reply
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		err := cn.client.ThreadReply(ctx, it.Source, it.Connector.Thread, action, text, quote)
		a.Host.Post(func() {
			t.sending = false
			if !a.live(cn) {
				return
			}
			if err != nil {
				a.toast(a.tr("threadSendFailed", "error", a.errorText(err)))
				return
			}
			// sent: never offered again, even if reading it back fails
			t.text, t.quote = "", nil
			a.readThread(it, t)
		})
	})
}

func (a *App) mediaLine(m hubapi.ThreadMedia) string {
	dur := ""
	if m.Duration > 0 {
		dur = " " + strconv.Itoa(m.Duration/60) + ":" + pad2(m.Duration%60)
	}
	switch m.Kind {
	case "voice":
		return a.tr("threadMediaVoice") + dur
	case "video":
		return a.tr("threadMediaVideo") + dur
	case "image":
		return a.tr("threadMediaImage")
	case "sticker":
		if m.Emoji != "" {
			return m.Emoji
		}
		return a.tr("threadMediaSticker")
	}
	name := m.Name
	if name == "" {
		name = a.tr("threadMediaFile")
	}
	if m.Size > 0 {
		name += " · " + sizeText(m.Size)
	}
	return name
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func (a *App) ghState(state, kind string) string {
	switch state {
	case "open":
		return a.tr("threadStateOpen")
	case "merged":
		return a.tr("threadStateMerged")
	case "closed":
		if kind == "issue" {
			return a.tr("threadStateClosed")
		}
		return a.tr("threadStateClosedUnmerged")
	case "draft":
		return a.tr("threadStateDraft")
	}
	return state
}

func (a *App) reviewLabel(r string) string {
	switch r {
	case "approved":
		return a.tr("threadReviewApproved")
	case "changes_requested":
		return a.tr("threadReviewChanges")
	case "dismissed":
		return a.tr("threadReviewDismissed")
	}
	return a.tr("threadReviewCommented")
}

func (a *App) checkLabel(state string) string {
	switch state {
	case "success":
		return a.tr("threadCheckSuccess")
	case "failure":
		return a.tr("threadCheckFailure")
	case "pending":
		return a.tr("threadCheckPending")
	case "skipped":
		return a.tr("threadCheckSkipped")
	case "cancelled":
		return a.tr("threadCheckCancelled")
	}
	return state
}

func clockOf(at string) string {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return ""
	}
	return t.Local().Format("15:04")
}

// threadView is a connector entry's thread.
func (a *App) threadView(c *ui.Context, p palette, it hubapi.InboxItem) {
	t := a.threadOf(it)
	if !t.loaded {
		if t.err != "" {
			ui.Text(c, a.tr("threadFailed", "error", t.err)).FontSize(small).TextColor(p.ink2).Selectable()
			if it.Preview != "" {
				ui.Text(c, it.Preview).Selectable()
			}
			return
		}
		ui.Row(c).Gap(8).Children(func() {
			ui.Spinner(c)
			ui.Text(c, a.tr("threadLoading", "name", it.AccountName)).FontSize(small).TextColor(p.ink3)
		})
		return
	}
	chat := t.page.Chat
	if chat.GitHub() {
		a.githubHeader(c, p, chat)
	}
	if t.page.Before != "" {
		ui.Row(c).Children(func() {
			ui.Spacer(c)
			if t.older {
				ui.Spinner(c)
			} else if ui.Button(c, a.tr("threadOlder")).Clicked() {
				a.olderThread(it, t)
			}
			ui.Spacer(c)
		})
	}
	if len(t.page.Messages) == 0 {
		ui.Text(c, a.tr("threadEmpty")).FontSize(small).TextColor(p.ink3)
	}
	prev := ""
	for _, m := range t.page.Messages {
		ui.Column(c.Key("tm-" + m.ID)).AlignItems(ui.Stretch).Children(func() {
			if chat.GitHub() {
				a.githubItem(c, p, t, m)
			} else {
				a.chatBubble(c, p, t, m, m.Sender.Name != prev || m.Event)
			}
		})
		prev = m.Sender.Name
		if m.Event {
			prev = ""
		}
	}
	if chat.Reply == "" {
		return
	}
	placeholder := a.tr("threadSendTo", "name", chat.Name)
	if chat.GitHub() {
		placeholder = a.tr("threadCommentOn", "ref", chat.ID)
	}
	a.card(c, p, func() {
		if q := t.quote; q != nil {
			ui.Row(c).Gap(8).Children(func() {
				who := q.Sender.Name
				if q.Mine {
					who = a.tr("threadMe")
				}
				line := q.Text
				if line == "" && len(q.Media) > 0 {
					line = a.mediaLine(q.Media[0])
				}
				ui.Text(c, a.tr("threadQuoted", "name", who)).FontSize(small).Bold().SingleLine()
				ui.Text(c, oneLine(line, 120)).FontSize(small).TextColor(p.ink3).SingleLine().Grow(1).Shrink(1)
				if ui.Button(c, "").Label(a.tr("threadCancelQuote")).Tooltip(a.tr("threadCancelQuote")).Children(func() { ui.Icon(c, icon("x")) }).Clicked() {
					t.quote = nil
				}
			})
		}
		ui.TextArea(c.Key("thread-"+it.ID), &t.text).Lines(2, 8).Placeholder(placeholder).Label(placeholder)
		ui.Row(c).Gap(8).Children(func() {
			if it.Connector.ReplyTo != "" {
				ui.Text(c, a.tr("threadReplyGoes", "how", it.Connector.ReplyTo)).FontSize(small).TextColor(p.ink3).Grow(1).Shrink(1)
			} else {
				ui.Spacer(c)
			}
			if t.sending {
				ui.Spinner(c)
			}
			if ui.PrimaryButton(c, a.tr("inboxSend")).Disabled(t.sending || strings.TrimSpace(t.text) == "").Clicked() {
				a.sendInThread(it, t)
			}
		})
	})
}

func (a *App) messageText(c *ui.Context, key string, m hubapi.ThreadMessage) {
	if m.Text == "" {
		return
	}
	if m.Format == "markdown" {
		markdown.View(c.Key(key), m.Text, &a.md)
		return
	}
	ui.Text(c, m.Text).Selectable()
}

// chatBubble is a chat app's message: mine on the right, the sender's name over a run of theirs.
func (a *App) chatBubble(c *ui.Context, p palette, t *threadState, m hubapi.ThreadMessage, head bool) {
	if m.Event {
		ui.Text(c, m.Text).FontSize(11).TextColor(p.ink3).TextAlign(ui.Center).Margin(4, 0)
		return
	}
	ui.Row(c).Gap(8).Children(func() {
		if m.Mine {
			ui.Spacer(c)
		}
		ui.Column(c).MaxWidth(520).Gap(2).Children(func() {
			if head && !m.Mine {
				ui.Text(c, m.Sender.Name).FontSize(11).Bold().TextColor(p.ink2).Padding(0, 4)
			}
			bg := p.surface
			if m.Mine {
				bg = p.userBubble
			}
			ui.Column(c).Padding(8, 12).Radius(cardRadius).Background(bg).Border(1, p.line).Gap(4).Children(func() {
				if q := m.ReplyTo; q != nil {
					line := q.Text
					if line == "" {
						line = a.tr("threadAMessage")
					}
					if q.From != "" {
						line = a.tr("threadQuoted", "name", q.From) + line
					}
					ui.Text(c, oneLine(line, 160)).FontSize(small).TextColor(p.ink3).SingleLine()
				}
				a.messageText(c, "tx-"+m.ID, m)
				for i, md := range m.Media {
					ui.Row(c.Key("md" + itoa(i))).Gap(6).Children(func() {
						ui.Icon(c, icon("clip")).FontSize(small).TextColor(p.ink3)
						ui.Text(c, a.mediaLine(md)).FontSize(small).TextColor(p.ink2)
					})
				}
			})
			ui.Row(c).Gap(8).Padding(0, 4).Children(func() {
				line := clockOf(m.At)
				if m.Edited {
					line += " · " + a.tr("threadEdited")
				}
				ui.Text(c, line).FontSize(11).TextColor(p.ink3)
				if t.page.Chat.Reply != "" && a.textLink(c, p, "reply-"+m.ID, a.tr("threadReply"), 11) {
					q := m
					t.quote = &q
				}
			})
		})
		if !m.Mine {
			ui.Spacer(c)
		}
	})
}

// githubHeader is a pull request's or issue's state, author, branch, labels and description.
func (a *App) githubHeader(c *ui.Context, p palette, chat hubapi.ThreadChat) {
	a.card(c, p, func() {
		ui.Row(c).Wrap().Gap(8).Children(func() {
			col := p.green
			switch chat.State {
			case "merged":
				col = p.ai
			case "closed":
				col = p.red
			case "draft":
				col = p.ink3
			}
			a.pill(c, a.ghState(chat.State, chat.Kind), col)
			if chat.Author != nil {
				ui.Text(c, a.tr("threadOpenedBy", "name", chat.Author.Name)).FontSize(small).TextColor(p.ink2)
			}
			if b := chat.Branch; b != nil {
				ui.Text(c, b.Head+" → "+b.Base).FontSize(small).Font("monospace").TextColor(p.ink2)
			}
		})
		if len(chat.Labels) > 0 {
			ui.Row(c).Wrap().Gap(6).Children(func() {
				for _, l := range chat.Labels {
					a.pill(c, l.Name, p.ink2)
				}
			})
		}
		if strings.TrimSpace(chat.Body) != "" {
			if chat.BodyFormat == "markdown" {
				markdown.View(c.Key("gh-body"), chat.Body, &a.md)
			} else {
				ui.Text(c, chat.Body).Selectable()
			}
		}
	})
}

// githubItem is one entry of the timeline: an event line, a check, a review, a comment on a line, or a comment.
func (a *App) githubItem(c *ui.Context, p palette, t *threadState, m hubapi.ThreadMessage) {
	if m.Event {
		ui.Row(c).Gap(8).Padding(2, 4).Children(func() {
			ui.Text(c, m.Text).FontSize(small).TextColor(p.ink3).Grow(1).Shrink(1)
			ui.Text(c, a.ago(m.At, time.Now())).FontSize(11).TextColor(p.ink3)
		})
		return
	}
	if ck := m.Check; m.Kind == "check" && ck != nil {
		col := p.ink3
		switch ck.State {
		case "success":
			col = p.green
		case "failure":
			col = p.red
		case "pending":
			col = p.attention
		}
		ui.Row(c).Gap(8).Padding(2, 4).Children(func() {
			ui.Box(c).Size(8, 8).Radius(4).Background(col)
			ui.Text(c, ck.Name).FontSize(small).Bold()
			ui.Text(c, a.checkLabel(ck.State)).FontSize(small).TextColor(col)
			ui.Spacer(c)
			if ck.Log != "" {
				label := a.tr("threadShowLog")
				if t.openLogs[m.ID] {
					label = a.tr("threadHideLog")
				}
				if a.textLink(c, p, "log-"+m.ID, label, small) {
					t.openLogs[m.ID] = !t.openLogs[m.ID]
				}
			}
			if ck.URL != "" && a.textLink(c, p, "ckopen-"+m.ID, a.tr("threadOpenGitHub"), small) {
				a.Host.OpenURL(ck.URL)
			}
		})
		if ck.Log != "" && t.openLogs[m.ID] {
			ui.Box(c).Padding(8, 10).Radius(rowRadius).Background(p.inset).Children(func() {
				ui.Text(c, ck.Log).FontSize(11).Font("monospace").Selectable()
			})
		}
		return
	}
	who := m.Sender.Name
	if m.Mine {
		who = a.tr("threadMe")
	}
	title := who
	col := p.ink
	if m.Kind == "review" {
		title = who + " " + a.reviewLabel(m.Review)
		switch m.Review {
		case "approved":
			col = p.green
		case "changes_requested":
			col = p.red
		}
	}
	a.card(c, p, func() {
		ui.Row(c).Gap(8).Children(func() {
			if m.Kind == "code" && m.Code != nil {
				ref := m.Code.Path
				if m.Code.Line > 0 {
					ref += ":" + itoa(m.Code.Line)
				}
				ui.Text(c, ref).FontSize(small).Font("monospace").Bold().SingleLine().Grow(1).Shrink(1)
			} else {
				ui.Text(c, title).FontSize(small).Bold().TextColor(col).SingleLine().Grow(1).Shrink(1)
			}
			ui.Text(c, a.ago(m.At, time.Now())).FontSize(11).TextColor(p.ink3)
			if m.URL != "" && a.textLink(c, p, "open-"+m.ID, a.tr("threadOpenGitHub"), 11) {
				a.Host.OpenURL(m.URL)
			}
		})
		if m.Kind == "code" && m.Code != nil {
			if m.Code.Hunk != "" {
				ui.Box(c).Padding(8, 10).Radius(rowRadius).Background(p.inset).Children(func() {
					ui.Text(c, m.Code.Hunk).FontSize(11).Font("monospace").Selectable()
				})
			}
			if q := m.ReplyTo; q != nil {
				line := q.Text
				if q.From != "" {
					line = a.tr("threadQuoted", "name", q.From) + line
				}
				ui.Text(c, oneLine(line, 160)).FontSize(small).TextColor(p.ink3).SingleLine()
			}
			ui.Text(c, who).FontSize(11).Bold().TextColor(p.ink2)
		}
		a.messageText(c, "gx-"+m.ID, m)
	})
}

// textLink is a button drawn as a link's text.
func (a *App) textLink(c *ui.Context, p palette, key, label string, size float32) bool {
	b := ui.ButtonBase(c.Key(key)).Label(label).Radius(4).Padding(0, 2)
	b.Children(func() {
		t := ui.Text(c, label).FontSize(size).TextColor(p.accent).SingleLine()
		if b.Hovered() {
			t.Underline()
		}
	})
	return b.Clicked()
}
