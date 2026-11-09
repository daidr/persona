package app

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// overviewView is the Overview: the cards waiting on you, answered in place; agents stuck without a card; open
// to-dos; agents working and done but not seen (with Mark all as seen); and the computers.
func (a *App) overviewView(c *ui.Context, p palette) {
	d := a.conn.data
	var needs, stuck, working, unread []hubapi.Session
	for _, g := range d.Agents("") {
		switch g.State {
		case hubapi.StateNeedsYou:
			needs = append(needs, g.Agents...)
		case hubapi.StateWorking:
			working = append(working, g.Agents...)
		case hubapi.StateUnread:
			unread = append(unread, g.Agents...)
		}
	}
	var cards []hubapi.Interaction
	for _, x := range needs {
		ix := d.SessionInteractions(x.Key())
		if len(ix) == 0 {
			stuck = append(stuck, x)
		}
		cards = append(cards, ix...)
	}
	var todos []hubapi.Todo
	for _, t := range d.Todos {
		if !t.Done() {
			todos = append(todos, t)
		}
	}
	a.pageHeader(c, p, a.tr("goOverview"), a.overviewSummary(len(cards)+len(stuck), len(working), len(d.Machines)), func() {
		if len(d.Machines) > 0 && ui.Button(c, a.tr("startTitle")).Clicked() {
			a.openStart("", "")
		}
	})
	ui.Divider(c)
	now := time.Now()
	ui.Scroll(c.Key("overview")).Grow(1).Children(func() {
		ui.Column(c).MaxWidth(960).Padding(16, pagePad, 32, pagePad).Gap(18).Children(func() {
			if len(d.Machines) == 0 {
				a.card(c, p, func() {
					ui.Text(c, a.tr("overviewNoComputers")).Bold()
					ui.Text(c, a.tr("overviewNoComputersHint")).FontSize(small).TextColor(p.ink2)
					ui.Row(c).Children(func() {
						if ui.Button(c, a.tr("goMachines")).Clicked() {
							a.Navigate("/machines")
						}
					})
				})
				return
			}
			if len(cards) == 0 && len(stuck) == 0 && len(working) == 0 && len(unread) == 0 {
				a.card(c, p, func() {
					ui.Row(c).Gap(8).Children(func() {
						ui.Icon(c, icon("check")).TextColor(p.green)
						ui.Text(c, a.tr("overviewAllSet")).Bold()
					})
				})
			}
			if len(cards) > 0 {
				a.overviewSection(c, p, a.tr("overviewNeedsYou", "count", itoa(len(cards))), nil, func() {
					for _, ix := range cards {
						x, _ := d.Session(ix.MachineID + "/" + ix.SessionID)
						if x.SessionID == "" {
							x = hubapi.Session{MachineID: ix.MachineID, SessionID: ix.SessionID, Title: ix.SessionTitle}
						}
						key := x.Key()
						ui.Row(c.Key("ov-ix-"+ix.ID)).Gap(6).Padding(0, pagePad-4).Children(func() {
							ui.Text(c, titleOf(x)+" · "+d.MachineName(x.MachineID)).FontSize(small).TextColor(p.ink2).SingleLine().Grow(1).Shrink(1)
							if ui.Button(c, a.tr("overviewOpen")).FontSize(small).Padding(2, 8).Clicked() {
								a.Navigate("/agents/" + key)
							}
						})
						a.cardView(c, p, x, a.chat(key), ix)
					}
				})
			}
			if len(stuck) > 0 {
				a.overviewSection(c, p, a.tr("overviewStuck"), nil, func() {
					ui.Text(c, a.tr("overviewStuckHint")).FontSize(small).TextColor(p.ink3)
					for _, x := range stuck {
						a.overviewRow(c, p, x, hubapi.StateNeedsYou, now)
					}
				})
			}
			if len(todos) > 0 {
				a.overviewSection(c, p, a.tr("overviewTodos"), func() {
					if ui.Button(c, a.tr("overviewSeeAll")).FontSize(small).Padding(2, 8).Clicked() {
						a.Navigate("/todos")
					}
				}, func() {
					for _, t := range todos[:min(6, len(todos))] {
						a.overviewTodo(c, p, t)
					}
				})
			}
			if len(working) > 0 {
				a.overviewSection(c, p, a.tr("stateWorking"), nil, func() {
					for _, x := range working {
						a.overviewRow(c, p, x, hubapi.StateWorking, now)
					}
				})
			}
			if len(unread) > 0 {
				a.overviewSection(c, p, a.tr("stateUnread"), func() {
					if ui.Button(c, a.tr("overviewMarkAllSeen")).FontSize(small).Padding(2, 8).Clicked() {
						a.markAllSeen(unread)
					}
				}, func() {
					for _, x := range unread {
						a.overviewRow(c, p, x, hubapi.StateUnread, now)
					}
				})
			}
			a.overviewSection(c, p, a.tr("goMachines"), nil, func() {
				ui.Row(c).Gap(8).Wrap().Children(func() {
					for _, m := range d.Machines {
						name := m.Name
						if name == "" {
							name = m.ID
						}
						dot := p.ink3
						if m.Online {
							dot = p.green
						}
						ui.Row(c.Key("ov-m-"+m.ID)).Gap(6).Padding(6, 10).Radius(rowRadius).Border(1, p.line).Background(p.surface).Children(func() {
							ui.Box(c).Size(8, 8).Radius(4).Background(dot)
							ui.Text(c, name).FontSize(small).SingleLine()
						})
					}
				})
			})
		})
	})
}

func (a *App) overviewSummary(needs, working, machines int) string {
	switch {
	case machines == 0:
		return a.tr("overviewSummaryNoComputers")
	case needs > 0:
		return a.tr("overviewSummaryNeeds", "count", itoa(needs))
	case working > 0:
		return a.tr("overviewSummaryWorking", "count", itoa(working))
	}
	return a.tr("overviewSummaryQuiet")
}

func (a *App) overviewSection(c *ui.Context, p palette, title string, right func(), body func()) {
	ui.Column(c.Key("ov-" + title)).Gap(8).Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, title).Bold().Grow(1)
			if right != nil {
				right()
			}
		})
		body()
	})
}

func (a *App) overviewRow(c *ui.Context, p palette, x hubapi.Session, state string, now time.Time) {
	d := a.conn.data
	row := ui.ButtonBase(c.Key("ov-"+x.Key())).Padding(8, 12).Radius(rowRadius).Background(p.surface).Border(1, p.line).Gap(10).Label(titleOf(x))
	if row.Hovered() {
		row.Background(p.hover)
	}
	row.Children(func() {
		ui.Box(c).Size(8, 8).Radius(4).Background(p.stateColor(state))
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, titleOf(x)).SingleLine()
			ui.Text(c, a.agentLine(d, x)).FontSize(small).TextColor(p.ink2).SingleLine()
		})
		ui.Text(c, d.MachineName(x.MachineID)).FontSize(small).TextColor(p.ink3).SingleLine()
		ui.Text(c, a.ago(x.UpdatedAt, now)).FontSize(small).TextColor(p.ink3).SingleLine()
	})
	if row.Clicked() {
		a.Navigate("/agents/" + x.Key())
	}
}

func (a *App) overviewTodo(c *ui.Context, p palette, t hubapi.Todo) {
	row := ui.ButtonBase(c.Key("ov-todo-"+t.ID)).Padding(8, 12).Radius(rowRadius).Background(p.surface).Border(1, p.line).Gap(10).Label(t.Title)
	if row.Hovered() {
		row.Background(p.hover)
	}
	row.Children(func() {
		ui.Icon(c, icon("todo")).TextColor(p.ink2)
		ui.Text(c, t.Title).SingleLine().Grow(1).Shrink(1)
		if t.Stage != "" {
			a.pill(c, t.Stage, p.ink2)
		}
		if n := len(t.Agents); n > 0 {
			ui.Text(c, a.agentCount(n)).FontSize(small).TextColor(p.ink3)
		}
	})
	if row.Clicked() {
		a.Navigate("/todos/" + t.ID)
	}
}

func (a *App) agentCount(n int) string {
	if n == 1 {
		return a.tr("todoAgentsOne")
	}
	return a.tr("todoAgents", "count", itoa(n))
}

// markAllSeen marks every finished agent not seen yet as seen.
func (a *App) markAllSeen(list []hubapi.Session) {
	c := a.conn
	keys := make([]hubapi.Session, len(list))
	copy(keys, list)
	for _, x := range keys {
		x.State = hubapi.StateIdle
		c.data.PutSession(x)
	}
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, x := range keys {
			_ = c.client.Seen(ctx, x.MachineID, x.SessionID)
		}
	})
}
