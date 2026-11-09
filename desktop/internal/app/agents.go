package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
	"github.com/nwverse/persona/desktop/internal/store"
)

type agentsPage struct {
	query string
	list  ui.ListState
	rows  []agentRow
}

// agentRow is a row of the agents list: a group's heading, or an agent.
type agentRow struct {
	heading string
	state   string
	agent   hubapi.Session
}

func (a *App) stateLabel(state string) string {
	switch state {
	case hubapi.StateNeedsYou:
		return a.tr("stateNeedsYou")
	case hubapi.StateWorking:
		return a.tr("stateWorking")
	case hubapi.StateUnread:
		return a.tr("stateUnread")
	case hubapi.StateOffline:
		return a.tr("stateOffline")
	}
	return a.tr("stateIdle")
}

// agentsView is the agents list beside the chat of the agent chosen.
func (a *App) agentsView(c *ui.Context, p palette, open string) {
	ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(listWidth).BorderWidth(0, 1, 0, 0).BorderColor(p.line).Children(func() {
			a.agentsList(c, p, open)
		})
		ui.Column(c).Grow(1).Children(func() {
			if open == "" {
				a.empty(c, p, "agent", a.tr("agentsPick"), "")
				return
			}
			a.chatView(c, p, open, false)
		})
	})
}

func (a *App) agentsList(c *ui.Context, p palette, open string) {
	g := &a.agents
	d := a.conn.data
	ui.Row(c).Padding(16, 16, 8, 16).Children(func() {
		ui.Text(c, a.tr("goAgents")).FontSize(title).Bold()
	})
	ui.SearchField(c.Key("agents-search"), &g.query).Placeholder(a.tr("agentsSearch")).Margin(0, 16, 8, 16)
	g.rows = g.rows[:0]
	for _, grp := range d.Agents(g.query) {
		g.rows = append(g.rows, agentRow{heading: a.stateLabel(grp.State), state: grp.State})
		for _, x := range grp.Agents {
			g.rows = append(g.rows, agentRow{agent: x, state: grp.State})
		}
	}
	rows := g.rows
	g.list.Key = func(i int) any {
		if rows[i].heading != "" {
			return "h:" + rows[i].state
		}
		return rows[i].agent.Key()
	}
	g.list.Header = func(i int) bool { return rows[i].heading != "" }
	now := time.Now()
	ui.List(c, &g.list, len(rows), func(i int) {
		r := rows[i]
		if r.heading != "" {
			ui.Row(c).Padding(10, 16, 4, 16).Background(p.page).Gap(6).Children(func() {
				ui.Text(c, r.heading).FontSize(small).Bold().TextColor(p.stateColor(r.state))
			})
			return
		}
		a.agentRowView(c, p, d, r, r.agent.Key() == open, now)
	}).Grow(1).PaddingY(4).Children(func() {
		if len(rows) == 0 {
			if !a.conn.loaded {
				ui.Spinner(c).Margin(24, ui.Auto)
			} else if g.query != "" {
				a.empty(c, p, "search", a.tr("agentsNoMatch"), "")
			} else {
				a.empty(c, p, "agent", a.tr("agentsEmpty"), a.tr("agentsEmptyHint"))
			}
		}
	})
}

func (a *App) agentRowView(c *ui.Context, p palette, d *store.Store, r agentRow, chosen bool, now time.Time) {
	x := r.agent
	bg := ui.Transparent
	if chosen {
		bg = p.hover
	}
	row := ui.ButtonBase(c.Key(x.Key())).Margin(0, 8).Padding(8, 10).Radius(rowRadius).Background(bg).Gap(10).AlignItems(ui.Start).Label(x.Title)
	row.Children(func() {
		ui.Box(c).Size(8, 8).Radius(4).Background(p.stateColor(r.state)).Margin(6, 0, 0, 0)
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				t := ui.Text(c, titleOf(x)).SingleLine().Grow(1).Shrink(1)
				if r.state == hubapi.StateNeedsYou || r.state == hubapi.StateUnread {
					t.Bold()
				}
				ui.Text(c, a.ago(x.UpdatedAt, now)).FontSize(small).TextColor(p.ink3).SingleLine()
			})
			ui.Text(c, a.agentLine(d, x)).FontSize(small).TextColor(p.ink2).MaxLines(2)
		})
	})
	if row.Hovered() && !chosen {
		row.Background(p.hover)
	}
	if row.Clicked() {
		a.Navigate("/agents/" + x.Key())
	}
}

func titleOf(x hubapi.Session) string {
	if x.Title != "" {
		return x.Title
	}
	if x.Cwd != "" {
		return x.Cwd
	}
	return x.SessionID
}

// agentLine is what the agents list says under an agent's title: what it asks, does or said, on which computer.
func (a *App) agentLine(d *store.Store, x hubapi.Session) string {
	text := x.Asks
	if text == "" {
		text = x.Activity
	}
	if text == "" {
		text = x.LastText
	}
	where := d.MachineName(x.MachineID)
	if x.AgentLabel != "" {
		where += " · " + x.AgentLabel
	}
	if text == "" {
		return where
	}
	return where + " — " + oneLine(text, 160)
}
