package app

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// The unified entry in an assistant conversation (docs/assistant/unified-entry.md): the hub's items among the messages,
// each before the first message sent after it. Plans start or are dismissed here; a task card follows its to-do and its
// agents; Today and briefs list what waits, what happened and what to verify (Accept, Handled); receipts, answered cards,
// messages and what Persona sent to a task show as short cards or lines.

// feedState is what was asked of the hub for each conversation's feed, by connection.
type feedState struct {
	conn  *conn
	asked map[string]bool
	today map[string]string // the day the conversation's Today card was asked for
	busy  map[string]bool
}

// chatRow is a line of an assistant conversation: a message, or one of the hub's items.
type chatRow struct {
	item *hubapi.Item
	feed *hubapi.FeedItem
}

func (r chatRow) key() string {
	if r.feed != nil {
		return "feed-" + r.feed.ID
	}
	return r.item.ID
}

// chatRows puts the feed among the messages: each item before the first message the user sent after it, the rest at the end.
func chatRows(items []hubapi.Item, feed []hubapi.FeedItem) []chatRow {
	out := make([]chatRow, 0, len(items)+len(feed))
	next := 0
	for i := range items {
		it := &items[i]
		if it.Role == "user" && it.TS != "" {
			at := hubapi.ParseTime(it.TS)
			for next < len(feed) && hubapi.ParseTime(feed[next].At).Before(at) {
				out = append(out, chatRow{feed: &feed[next]})
				next++
			}
		}
		out = append(out, chatRow{item: it})
	}
	for ; next < len(feed); next++ {
		out = append(out, chatRow{feed: &feed[next]})
	}
	return out
}

// loadFeed reads a conversation's feed once per connection, and the main conversation's Today card once a day.
func (a *App) loadFeed(conversation string, main bool) {
	g := &a.feeds
	if g.conn != a.conn {
		*g = feedState{conn: a.conn, asked: map[string]bool{}, today: map[string]string{}, busy: map[string]bool{}}
	}
	c := a.conn
	if !g.asked[conversation] {
		g.asked[conversation] = true
		a.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			items, plans, err := c.client.Feed(ctx, conversation, "")
			a.Host.Post(func() {
				if err != nil || !a.live(c) {
					return // an older hub has no feed: the conversation shows as before
				}
				for _, p := range plans {
					c.data.PutPlan(p)
				}
				c.data.PutFeed(items...)
			})
		})
	}
	day := time.Now().Format("2006-01-02")
	if main && g.today[conversation] != day {
		g.today[conversation] = day
		a.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			it, err := c.client.Today(ctx, conversation)
			a.Host.Post(func() {
				if err == nil && a.live(c) && it.ID != "" {
					c.data.PutFeed(it)
				}
			})
		})
	}
}

// feedView draws one of the hub's items.
func (a *App) feedView(c *ui.Context, p palette, it hubapi.FeedItem) {
	switch it.Kind {
	case "plan":
		a.taskPlanCard(c, p, it)
	case "task":
		a.taskCard(c, p, it)
	case "today":
		a.todayCard(c, p, it)
	case "brief":
		a.briefCard(c, p, it)
	case "receipt":
		a.receiptCard(c, p, it)
	case "answered":
		a.feedLine(c, p, "check", a.tr("feedAnswered", "question", oneLine(it.Str("question"), 80)), it.Str("answer"))
	case "routed":
		title := it.Ref
		if t, ok := a.conn.data.Todo(it.Ref); ok {
			title = t.Title
		}
		a.feedLine(c, p, "send", a.tr("feedSentTo", "title", title), it.Str("text"))
	case "message":
		a.messageCard(c, p, it)
	case "notice", "report", "review":
		text := it.Str("text")
		if text == "" {
			text = it.Str("summary")
		}
		if text == "" {
			text = it.Str("title")
		}
		if text != "" {
			a.feedLine(c, p, "bell", text, "")
		}
	}
}

// feedBox is a card in the conversation, at most as wide as a reply.
func (a *App) feedBox(c *ui.Context, p palette, fn func()) {
	ui.Column(c).AlignItems(ui.Start).Children(func() {
		ui.Column(c).MaxWidth(640).Padding(12, 14).Gap(8).Radius(12).Border(1, p.line).Background(p.surface).Children(fn)
	})
}

func (a *App) feedLine(c *ui.Context, p palette, ic, line, detail string) {
	ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
		ui.Icon(c, icon(ic)).FontSize(small).TextColor(p.ink3).Margin(2, 0, 0, 0)
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, line).FontSize(small).TextColor(p.ink2).MaxLines(2)
			if detail != "" {
				ui.Text(c, detail).FontSize(small).TextColor(p.ink3).MaxLines(3)
			}
		})
	})
}

func (a *App) feedHeading(c *ui.Context, p palette, ic, title string) {
	ui.Row(c).Gap(6).Children(func() {
		ui.Icon(c, icon(ic)).FontSize(small).TextColor(p.ai)
		ui.Text(c, title).Bold().SingleLine()
	})
}

// taskPlanCard is a plan Persona proposed: its tasks with where they run and why; Start and Dismiss while it's open.
func (a *App) taskPlanCard(c *ui.Context, p palette, it hubapi.FeedItem) {
	plan, ok := a.conn.data.Plans[it.Ref]
	if !ok {
		return
	}
	d := a.conn.data
	a.feedBox(c, p, func() {
		title := a.tr("feedPlan", "count", itoa(len(plan.Tasks)))
		a.feedHeading(c, p, "spark", title)
		for i, t := range plan.Tasks {
			ui.Column(c.Key("pt"+itoa(i))).Gap(2).Padding(8, 10).Radius(8).Background(p.inset).Children(func() {
				ui.Text(c, t.Title).SingleLine()
				where := d.MachineName(t.Machine) + " · " + t.Cwd
				if t.Readonly {
					where += " · " + a.tr("feedReadOnly")
				}
				if t.Model != "" {
					where += " · " + t.Model
				}
				ui.Text(c, where).FontSize(11).TextColor(p.ink3).SingleLine()
				if t.Why != "" {
					ui.Text(c, t.Why).FontSize(small).TextColor(p.ink2).MaxLines(3)
				}
				if t.Error != "" {
					ui.Text(c, t.Error).FontSize(small).TextColor(p.red).MaxLines(2)
				}
			})
		}
		switch plan.State {
		case "open":
			busy := a.feeds.busy[plan.ID]
			ui.Row(c).Gap(8).Children(func() {
				if ui.PrimaryButton(c, a.tr("feedStart")).Disabled(busy).Clicked() {
					a.planDo(plan.ID, true)
				}
				if ui.Button(c, a.tr("feedDismiss")).Disabled(busy).Clicked() {
					a.planDo(plan.ID, false)
				}
			})
		case "started":
			ui.Text(c, a.tr("feedStarted")).FontSize(small).TextColor(p.green)
		case "dismissed":
			ui.Text(c, a.tr("feedDismissed")).FontSize(small).TextColor(p.ink3)
		}
	})
}

func (a *App) planDo(id string, start bool) {
	g := &a.feeds
	g.busy[id] = true
	c := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var plan hubapi.TaskPlan
		var err error
		if start {
			plan, err = c.client.StartPlan(ctx, id)
		} else {
			plan, err = c.client.DismissPlan(ctx, id)
		}
		a.Host.Post(func() {
			delete(g.busy, id)
			if !a.live(c) {
				return
			}
			if err != nil {
				a.toast(a.errorText(err))
				return
			}
			c.data.PutPlan(plan)
			if start {
				a.refreshTodos(c)
			}
		})
	})
}

// taskCard follows a task's to-do: its state, stage and agents, each opening its chat.
func (a *App) taskCard(c *ui.Context, p palette, it hubapi.FeedItem) {
	d := a.conn.data
	t, ok := d.Todo(it.Ref)
	if !ok {
		return
	}
	a.feedBox(c, p, func() {
		ui.Row(c).Gap(8).Children(func() {
			dot := p.orange
			state := a.tr("feedTaskWorking")
			switch {
			case t.Done():
				dot, state = p.green, a.tr("feedTaskDone")
			case len(t.Agents) == 0:
				dot, state = p.ink3, a.tr("feedTaskStarting")
			}
			for _, ag := range t.Agents {
				if x, ok := d.Session(ag.Key()); ok && d.StateOf(x) == hubapi.StateNeedsYou {
					dot, state = p.stateColor(hubapi.StateNeedsYou), a.tr("stateNeedsYou")
				}
			}
			ui.Box(c).Size(8, 8).Radius(4).Background(dot)
			ui.Text(c, t.Title).Bold().SingleLine().Grow(1).Shrink(1)
			ui.Text(c, state).FontSize(small).TextColor(p.ink2).SingleLine()
		})
		if t.Stage != "" {
			ui.Text(c, a.tr("todoStage", "stage", t.Stage)).FontSize(small).TextColor(p.ink2)
		}
		for i, ag := range t.Agents {
			if ag.StartError != "" {
				ui.Text(c.Key("se"+itoa(i)), ag.StartError).FontSize(small).TextColor(p.red).MaxLines(2)
				continue
			}
			x, _ := d.Session(ag.Key())
			title := ag.Title
			if x.SessionID != "" {
				title = titleOf(x)
			}
			row := ui.ButtonBase(c.Key("ta"+itoa(i))).Padding(6, 8).Radius(8).Background(p.inset).Gap(8).Label(title)
			row.Children(func() {
				ui.Icon(c, icon("agent")).FontSize(small).TextColor(p.ink2)
				ui.Text(c, title).FontSize(small).SingleLine().Grow(1).Shrink(1)
				ui.Text(c, d.MachineName(ag.MachineID)).FontSize(11).TextColor(p.ink3).SingleLine()
			})
			if row.Clicked() {
				a.Navigate("/agents/" + ag.Key())
			}
		}
		ui.Row(c).Children(func() {
			if ui.Button(c, a.tr("feedOpenTask")).FontSize(small).Padding(2, 8).Clicked() {
				a.Navigate("/todos/" + t.ID)
			}
		})
	})
}

// todayCard is the day's summary: done, waiting, to verify, working, meetings, mentions and what didn't go through.
func (a *App) todayCard(c *ui.Context, p palette, it hubapi.FeedItem) {
	a.feedBox(c, p, func() {
		a.feedHeading(c, p, "spark", a.tr("feedToday"))
		a.titledList(c, p, a.tr("feedTodayDone"), it.List("done"), "title", true)
		a.titledList(c, p, a.tr("feedTodayWaiting"), it.List("waiting"), "title", true)
		a.titledList(c, p, a.tr("feedTodayReview"), it.List("pending_review"), "title", true)
		if n := it.Num("working"); n > 0 {
			ui.Text(c, a.tr("feedTodayWorking", "count", itoa(n))).FontSize(small).TextColor(p.ink2)
		}
		if ms := it.List("meetings"); len(ms) > 0 {
			ui.Text(c, a.tr("feedTodayMeetings")).FontSize(11).Bold().TextColor(p.ink3)
			for i, m := range ms {
				start := hubapi.ParseTime(str(m, "start"))
				line := str(m, "title")
				if !start.IsZero() {
					line = start.Local().Format("15:04") + "  " + line
				}
				ui.Text(c.Key("mt"+itoa(i)), line).FontSize(small).SingleLine()
			}
		}
		for i, m := range it.List("at_me") {
			line := str(m, "name")
			if pv := str(m, "preview"); pv != "" {
				line += " · " + pv
			}
			ui.Text(c.Key("am"+itoa(i)), "@ "+line).FontSize(small).TextColor(p.ink2).SingleLine()
		}
		a.failedList(c, p, it)
	})
}

// briefCard is a morning or evening run's fixed part: what waits, what failed, what's done, what to verify (Accept),
// questions that waited for it (Handled) and what's in progress.
func (a *App) briefCard(c *ui.Context, p palette, it hubapi.FeedItem) {
	a.feedBox(c, p, func() {
		title := it.Str("title")
		if title == "" {
			title = a.tr("feedBrief")
		}
		a.feedHeading(c, p, "spark", title)
		a.titledList(c, p, a.tr("feedTodayWaiting"), it.List("waiting"), "title", false)
		a.failedList(c, p, it)
		a.titledList(c, p, a.tr("feedTodayDone"), it.List("done"), "title", true)
		if acc := it.List("accept"); len(acc) > 0 {
			ui.Text(c, a.tr("feedBriefAccept")).FontSize(11).Bold().TextColor(p.ink3)
			for i, r := range acc {
				id, asked := str(r, "id"), str(r, "asked_at")
				a.briefRow(c.Key("acc"+itoa(i)), p, r, a.tr("feedAccept"), "accept:"+id, func(ctx context.Context, cl *hubapi.Client) error {
					return cl.AcceptTodo(ctx, id, asked)
				})
			}
		}
		if later := it.List("later"); len(later) > 0 {
			ui.Text(c, a.tr("feedBriefLater")).FontSize(11).Bold().TextColor(p.ink3)
			for i, r := range later {
				id := str(r, "id")
				a.briefRow(c.Key("lt"+itoa(i)), p, r, a.tr("feedHandled"), "later:"+id, func(ctx context.Context, cl *hubapi.Client) error {
					return cl.LaterHandled(ctx, id)
				})
			}
		}
		a.titledList(c, p, a.tr("feedBriefProgress"), it.List("progress"), "title", true)
	})
}

func (a *App) briefRow(c *ui.Context, p palette, r map[string]any, label, busyKey string, do func(ctx context.Context, cl *hubapi.Client) error) {
	g := &a.feeds
	ui.Row(c).Gap(8).Padding(6, 8).Radius(8).Background(p.inset).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, str(r, "title")).FontSize(small).MaxLines(2)
			if d := str(r, "detail"); d != "" {
				ui.Text(c, d).FontSize(11).TextColor(p.ink2).MaxLines(2)
			}
			if f := str(r, "from"); f != "" {
				ui.Text(c, f).FontSize(11).TextColor(p.ink3).SingleLine()
			}
		})
		if g.busy[busyKey+":done"] {
			ui.Icon(c, icon("check")).TextColor(p.green)
			return
		}
		if ui.Button(c, label).FontSize(small).Padding(2, 8).Disabled(g.busy[busyKey]).Clicked() {
			g.busy[busyKey] = true
			cn := a.conn
			a.Go(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				err := do(ctx, cn.client)
				a.Host.Post(func() {
					delete(g.busy, busyKey)
					if !a.live(cn) {
						return
					}
					if err != nil {
						a.toast(a.errorText(err))
						return
					}
					g.busy[busyKey+":done"] = true
					a.refreshTodos(cn)
				})
			})
		}
	})
}

// titledList is a heading and its rows' titles; a row naming a to-do opens it when open is set.
func (a *App) titledList(c *ui.Context, p palette, heading string, rows []map[string]any, field string, open bool) {
	if len(rows) == 0 {
		return
	}
	ui.Text(c.Key("h-"+heading), heading).FontSize(11).Bold().TextColor(p.ink3)
	for i, r := range rows {
		title := str(r, field)
		id := str(r, "id")
		if _, ok := a.conn.data.Todo(id); open && ok {
			row := ui.ButtonBase(c.Key(heading + itoa(i))).Label(title)
			row.Children(func() { ui.Text(c, "· "+title).FontSize(small).TextColor(p.accent).SingleLine() })
			if row.Clicked() {
				a.Navigate("/todos/" + id)
			}
			continue
		}
		line := "· " + title
		if f := str(r, "from"); f != "" {
			line += " — " + f
		}
		ui.Text(c.Key(heading+itoa(i)), line).FontSize(small).SingleLine()
	}
}

func (a *App) failedList(c *ui.Context, p palette, it hubapi.FeedItem) {
	failed := it.List("failed")
	if len(failed) == 0 {
		return
	}
	heading := a.tr("feedFailed", "count", itoa(max(it.Num("failed_total"), len(failed))))
	ui.Text(c, heading).FontSize(11).Bold().TextColor(p.red)
	for i, f := range failed {
		line := "· " + str(f, "title")
		if why := str(f, "why"); why != "" {
			line += " — " + why
		}
		ui.Text(c.Key("fl"+itoa(i)), line).FontSize(small).TextColor(p.ink2).MaxLines(2)
	}
}

// receiptCard is how a task ended: done or not, its summary and its pull request.
func (a *App) receiptCard(c *ui.Context, p palette, it hubapi.FeedItem) {
	a.feedBox(c, p, func() {
		ui.Row(c).Gap(8).Children(func() {
			ic, col := "check", p.green
			if st := it.Str("state"); st == "failed" || st == "interrupted" {
				ic, col = "warning", p.red
			}
			ui.Icon(c, icon(ic)).FontSize(small).TextColor(col)
			ui.Text(c, it.Str("title")).Bold().SingleLine().Grow(1).Shrink(1)
			if m := it.Str("machine"); m != "" {
				ui.Text(c, m).FontSize(11).TextColor(p.ink3)
			}
		})
		if s := it.Str("summary"); s != "" {
			ui.Text(c, s).FontSize(small).TextColor(p.ink2).Selectable()
		}
		ui.Row(c).Gap(8).Children(func() {
			if pr := it.Str("pr"); strings.HasPrefix(pr, "https://") && ui.Button(c, a.tr("feedOpenPR")).FontSize(small).Padding(2, 8).Clicked() {
				a.Host.OpenURL(pr)
			}
			if _, ok := a.conn.data.Todo(it.Ref); ok && ui.Button(c, a.tr("feedOpenTask")).FontSize(small).Padding(2, 8).Clicked() {
				a.Navigate("/todos/" + it.Ref)
			}
		})
	})
}

// messageCard is a message from a connector about a task, with the draft Persona wrote.
func (a *App) messageCard(c *ui.Context, p palette, it hubapi.FeedItem) {
	a.feedBox(c, p, func() {
		from := it.Str("sender")
		if ch := it.Str("chat"); ch != "" {
			from += " · " + ch
		}
		a.feedHeading(c, p, "inbox", from)
		ui.Text(c, it.Str("text")).FontSize(small).Selectable()
		if d := it.Str("draft"); d != "" {
			ui.Text(c, a.tr("feedDraft")).FontSize(11).Bold().TextColor(p.ink3)
			ui.Text(c, d).FontSize(small).TextColor(p.ink2).Selectable()
		}
	})
}

func str(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}
