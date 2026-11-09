package app

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// chatState is what a chat keeps between frames: the draft, the list's place, the cards being filled in.
type chatState struct {
	draft    string
	list     ui.ListState
	loading  bool
	older    bool
	open     map[string]bool
	answers  map[string]map[string][]string
	feedback map[string]string
	busy     map[string]bool
	// pane is what the agent's page shows: its chat, terminal or changes.
	pane int
	// files are the pictures waiting to go with the next message; uploads and reading count files on their way.
	files            []attachment
	uploads, reading int
}

func (a *App) chat(key string) *chatState {
	s := a.chats[key]
	if s == nil {
		s = &chatState{open: map[string]bool{}, answers: map[string]map[string][]string{}, feedback: map[string]string{}, busy: map[string]bool{}}
		s.list.FollowEnd = true
		s.list.FollowAnimation = &ui.Smooth
		a.chats[key] = s
	}
	return s
}

// watch follows the chat shown: its transcript is read once, the socket sends what comes after, and it is
// marked read.
func (a *App) watch(key string) {
	c := a.conn
	if c.watching == key {
		return
	}
	machine, session, _ := strings.Cut(key, "/")
	if c.watching != "" {
		m, s, _ := strings.Cut(c.watching, "/")
		c.socket.Unsubscribe(m, s)
	}
	c.watching = key
	c.socket.Subscribe(machine, session)
	t := c.data.Transcript(key)
	st := a.chat(key)
	if !t.Loaded && !st.loading {
		st.loading = true
		a.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			page, err := c.client.Items(ctx, machine, session, 0)
			a.Host.Post(func() {
				st.loading = false
				if !a.live(c) {
					return
				}
				if err != nil {
					t.Err = a.errorText(err)
					return
				}
				t.Merge(page.Items)
				t.HasMore, t.Loaded, t.Err = page.HasMore, true, ""
			})
		})
	}
	a.markSeen(c, machine, session)
}

func (a *App) markSeen(c *conn, machine, session string) {
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = c.client.Seen(ctx, machine, session)
	})
}

func (a *App) loadOlder(key string) {
	c := a.conn
	t := c.data.Transcript(key)
	st := a.chat(key)
	if st.older || !t.HasMore {
		return
	}
	st.older = true
	machine, session, _ := strings.Cut(key, "/")
	before := t.Oldest()
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		page, err := c.client.Items(ctx, machine, session, before)
		a.Host.Post(func() {
			st.older = false
			if !a.live(c) || err != nil {
				return
			}
			t.Merge(page.Items)
			t.HasMore = page.HasMore
		})
	})
}

// send sends the draft to the agent, with the pictures attached to it, showing it at once and taking it back if
// the hub refuses.
func (a *App) send(key, text string) {
	text = strings.TrimSpace(text)
	if a.conn == nil {
		return
	}
	st := a.chat(key)
	files := st.files
	if text == "" && len(files) == 0 {
		return
	}
	st.files = nil
	c := a.conn
	machine, session, _ := strings.Cut(key, "/")
	id := hubapi.OpID()
	t := c.data.Transcript(key)
	pics := images(files)
	t.Pending(text, id, pics...)
	st.list.ScrollToEnd()
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		err := c.client.Send(ctx, machine, session, text, id, pics...)
		a.Host.Post(func() {
			if err != nil && a.live(c) {
				t.Drop(id)
				st := a.chat(key)
				if st.draft == "" {
					st.draft = text
				}
				if len(st.files) == 0 {
					st.files = files
				}
				a.toast(a.tr("sendFailed", "error", a.errorText(err)))
			}
		})
	})
}

func (a *App) interrupt(key string) {
	c := a.conn
	machine, session, _ := strings.Cut(key, "/")
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := c.client.Interrupt(ctx, machine, session); err != nil {
			a.Host.Post(func() { a.toast(a.errorText(err)) })
		}
	})
}

// toast shows a short message at the bottom of the window in the next frame.
func (a *App) toast(text string) { a.toasts = append(a.toasts, text) }

func (a *App) flushToasts(c *ui.Context) {
	for _, t := range a.toasts {
		c.Toast(t)
	}
	a.toasts = a.toasts[:0]
}

// chatView is an agent's chat: its header, transcript, open cards and the composer. persona is the main chat.
func (a *App) chatView(c *ui.Context, p palette, key string, persona bool) {
	d := a.conn.data
	x, known := d.Session(key)
	a.watch(key)
	t := d.Transcript(key)
	st := a.chat(key)
	if !persona {
		sub := d.MachineName(x.MachineID)
		if x.Cwd != "" {
			sub += " · " + x.Cwd
		}
		if x.Branch != "" {
			sub += " · " + a.tr("onBranch", "branch", x.Branch)
		}
		a.pageHeader(c, p, titleOf(x), sub, func() {
			if known && x.Can("read") {
				ui.Segmented(c.Key("pane-"+key), &st.pane, a.tr("paneChat"), a.tr("paneTerminal"), a.tr("paneChanges"), a.tr("paneFiles"))
			}
			if known {
				state := d.StateOf(x)
				ui.Row(c).Gap(6).Padding(3, 10).Radius(999).Background(p.inset).Children(func() {
					ui.Box(c).Size(7, 7).Radius(4).Background(p.stateColor(state))
					ui.Text(c, a.stateLabel(state)).FontSize(small).TextColor(p.ink2).SingleLine()
				})
				a.agentMenu(c, p, x, false)
			}
		})
		ui.Divider(c)
		switch st.pane {
		case paneTerminal:
			a.terminalPane(c, p, key, x)
			return
		case paneChanges:
			a.changesPane(c, p, key, x)
			return
		case paneFiles:
			a.filesPane(c, p, key, x)
			return
		}
	}
	// an assistant chat's cards can be answered
	conversation := ""
	if persona || x.Assistant {
		_, conversation, _ = strings.Cut(key, "/")
	}
	items := visibleItems(t.Items)
	var feed []hubapi.FeedItem
	if conversation != "" {
		main, _ := d.MainConversation()
		a.loadFeed(conversation, conversation == main.SessionID)
		feed = d.Feeds[conversation]
	}
	rows := chatRows(items, feed)
	live := strings.TrimSpace(t.Live)
	n := len(rows)
	extra := 0
	if t.HasMore {
		extra = 1
	}
	if live != "" {
		n++
	}
	st.list.Key = func(i int) any {
		if i < extra {
			return "older"
		}
		if i-extra < len(rows) {
			return rows[i-extra].key()
		}
		return "live"
	}
	ui.List(c, &st.list, n+extra, func(i int) {
		ui.Column(c).Padding(6, pagePad).Children(func() {
			if i < extra {
				if st.older {
					ui.Spinner(c).Margin(0, ui.Auto)
				} else if ui.Button(c, a.tr("loadEarlier")).Margin(0, ui.Auto).Clicked() {
					a.loadOlder(key)
				}
				return
			}
			if i-extra < len(rows) {
				if r := rows[i-extra]; r.feed != nil {
					a.feedView(c, p, *r.feed)
				} else {
					a.itemView(c, p, st, *r.item, conversation)
				}
				return
			}
			markdown.Stream(c, live, &a.md)
		})
	}).Grow(1).PaddingY(12).Justify(ui.End).Children(func() {
		switch {
		case t.Err != "":
			a.empty(c, p, "warning", t.Err, "")
		case !t.Loaded:
			ui.Spinner(c).Margin(ui.Auto)
		case n == 0:
			a.empty(c, p, "chat", a.tr("chatEmpty"), "")
		}
	})
	if first, _ := st.list.Visible(); first == 0 && t.HasMore && t.Loaded {
		a.loadOlder(key)
	}
	if cards := d.SessionInteractions(key); len(cards) > 0 {
		// several cards scroll above the composer rather than push the chat away
		ui.Scroll(c.Key("cards")).MaxHeightPercent(60).PaddingY(4).Children(func() {
			for _, ix := range cards {
				a.cardView(c, p, x, st, ix)
			}
		})
	}
	if persona {
		if main, _ := d.MainConversation(); main.MachineID+"/"+main.SessionID == key {
			a.readinessCard(c, p)
		}
	}
	a.composer(c, p, key, x, known, st)
}

// visibleItems leaves out what the transcript shows nowhere: results of tools that went well, empty texts.
func visibleItems(all []hubapi.Item) []hubapi.Item {
	out := make([]hubapi.Item, 0, len(all))
	for _, it := range all {
		switch {
		case it.Kind == "tool_result" && !it.IsError:
		case it.Kind == "text" && strings.TrimSpace(it.Text) == "":
		default:
			out = append(out, it)
		}
	}
	return out
}

func (a *App) itemView(c *ui.Context, p palette, st *chatState, it hubapi.Item, conversation string) {
	switch {
	case it.Role == "user" && it.Kind == "text" && it.MessageSource() != nil:
		a.sourceView(c, p, st, it, it.MessageSource())
	case it.Role == "user" && it.Kind == "text" && it.Injected != nil && it.Injected.Line != "":
		ui.Text(c, it.Injected.Line).FontSize(small).TextColor(p.ink3).TextAlign(ui.Center)
	case it.Role == "user" && it.Kind == "text":
		a.userBubble(c, p, it)
	case it.Kind == "text":
		a.richText(c, p, it, conversation)
	case it.Kind == "thinking":
		open := st.open[it.ID]
		ui.Collapsible(c.Key(it.ID), a.tr("thinking"), &open, func() {
			ui.Text(c, it.Text).FontSize(small).TextColor(p.ink3).Selectable()
		}).TextColor(p.ink3)
		st.open[it.ID] = open
	case it.Kind == "tool_use":
		ui.Row(c).Gap(8).Children(func() {
			ui.Icon(c, icon("tool")).FontSize(small).TextColor(p.ink3)
			ui.Text(c, it.ToolName).FontSize(small).Bold().TextColor(p.ink2).SingleLine()
			ui.Text(c, toolSummary(it.ToolInput)).FontSize(small).TextColor(p.ink3).SingleLine().Shrink(1)
		})
	case it.Kind == "tool_result":
		ui.Text(c, oneLine(it.Text, 300)).FontSize(small).TextColor(p.red).MaxLines(4).Selectable()
	case it.Kind == "image":
		a.imageView(c, p, it, it.Role == "user")
	case it.Kind == "fold" && it.Fold != nil:
		ui.Text(c, a.tr("folded", "count", itoa(it.Fold.Items))).FontSize(small).TextColor(p.ink3)
	case it.Injected != nil && it.Injected.Line != "":
		ui.Text(c, it.Injected.Line).FontSize(small).TextColor(p.ink3).TextAlign(ui.Center)
	default:
		if strings.TrimSpace(it.Text) != "" {
			ui.Text(c, oneLine(it.Text, 400)).FontSize(small).TextColor(p.ink3).TextAlign(ui.Center).MaxLines(3)
		}
	}
}

// toolSummary is the one line a tool call shows: its command, file, pattern or address.
func toolSummary(in map[string]any) string {
	for _, k := range []string{"description", "command", "file_path", "path", "pattern", "url", "query", "prompt"} {
		if s, ok := in[k].(string); ok && strings.TrimSpace(s) != "" {
			return oneLine(s, 140)
		}
	}
	return ""
}

func (a *App) composer(c *ui.Context, p palette, key string, x hubapi.Session, known bool, st *chatState) {
	can := !known || x.Can("send")
	working := known && a.conn.data.StateOf(x) == hubapi.StateWorking
	a.attachmentsRow(c, p, key, st)
	chips := known && hasModes(x)
	bottom := float32(16)
	if chips {
		bottom = 6
	}
	box := ui.Row(c).Margin(4, pagePad, bottom, pagePad).Padding(8, 8, 8, 14).Gap(8).Radius(cardRadius).Background(p.surface).
		Border(1, p.line).AlignItems(ui.End).Children(func() {
		place := a.tr("composerPlaceholder")
		if !can {
			place = a.tr("composerReadOnly")
		}
		ui.TextArea(c.Key("draft:"+key), &st.draft).Lines(1, 8).Placeholder(place).Label(place).Disabled(!can).
			Grow(1).Background(ui.Transparent).Border(0, ui.Transparent).
			OnTextKey(func(k ui.TextKey) bool {
				if k.Key == ui.KeyEnter && k.Mods == 0 {
					text := k.Text()
					k.SetText("")
					a.send(key, text)
					return true
				}
				return false
			})
		if working && x.Can("interrupt") {
			if ui.Button(c, "").Label(a.tr("stop")).Tooltip(a.tr("stop")).Children(func() { ui.Icon(c, icon("stop")) }).Clicked() {
				a.interrupt(key)
			}
		}
		if ui.Button(c, "").Label(a.tr("attach")).Tooltip(a.tr("attach")).Disabled(!can).
			Children(func() { ui.Icon(c, icon("clip")) }).Clicked() {
			a.pickFiles(key)
		}
		if ui.PrimaryButton(c, "").Label(a.tr("send")).Tooltip(a.tr("send")).
			Disabled(!can || strings.TrimSpace(st.draft) == "" && len(st.files) == 0).
			Children(func() { ui.Icon(c, icon("send")) }).Clicked() {
			text := st.draft
			st.draft = ""
			a.send(key, text)
		}
	})
	if chips {
		a.modeChips(c, p, x)
	}
	// files dragged onto the box are attached as the paper clip's are
	if dropped := box.DroppedFiles(); len(dropped) > 0 && can {
		a.addFiles(key, dropped)
	}
}
