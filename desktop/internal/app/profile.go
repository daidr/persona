package app

import (
	"context"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// Persona's profile (/persona/profile): where it runs and whether that computer is online, what it does on its own
// next (the scheduled tasks' next runs), and what it can reach.

type profileState struct {
	conn  *conn
	asked bool
	tasks []hubapi.Task
}

func (a *App) profileView(c *ui.Context, p palette) {
	g := &a.profile
	if g.conn != a.conn {
		*g = profileState{conn: a.conn}
	}
	if !g.asked {
		g.asked = true
		cn := a.conn
		a.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			tasks, err := cn.client.Tasks(ctx)
			a.Host.Post(func() {
				if err == nil && a.live(cn) {
					g.tasks = tasks
				}
			})
		})
	}
	d := a.conn.data
	as := d.Assistant
	var machine hubapi.Machine
	for _, m := range d.Machines {
		if m.ID == as.MachineID {
			machine = m
		}
	}
	a.pageHeader(c, p, a.tr("profileTitle"), "", func() {
		if as.Main != "" && ui.Button(c, a.tr("personaMain")).Clicked() {
			a.Navigate("/persona/" + as.Main)
		}
	})
	ui.Divider(c)
	now := time.Now()
	ui.Scroll(c.Key("profile")).Grow(1).Children(func() {
		ui.Column(c).MaxWidth(720).Padding(16, pagePad, 32, pagePad).Gap(16).Children(func() {
			a.card(c, p, func() {
				ui.Row(c).Gap(12).Children(func() {
					ui.Box(c).Size(48, 48).Radius(24).Background(p.ai.Alpha(0.15)).Children(func() {
						ui.Icon(c, icon("spark")).FontSize(22).TextColor(p.ai).Margin(ui.Auto)
					})
					ui.Column(c).Grow(1).Shrink(1).Gap(4).Children(func() {
						ui.Text(c, a.tr("goPersona")).FontSize(heading).Bold()
						where := a.tr("profileNoMachine")
						if name := d.MachineName(as.MachineID); as.MachineID != "" {
							where = a.tr("profileRunsOn", "machine", name)
						}
						ui.Text(c, where).FontSize(small).TextColor(p.ink2).SingleLine()
						ui.Row(c).Gap(6).Wrap().Children(func() {
							if machine.ID != "" {
								if machine.Online {
									a.pill(c, a.tr("online"), p.green)
								} else {
									a.pill(c, a.tr("stateOffline"), p.ink3)
								}
							}
							if slices.ContainsFunc(as.Conversations, func(x hubapi.Session) bool { return x.State == hubapi.StateWorking }) {
								a.pill(c, a.tr("stateWorking"), p.orange)
							}
						})
					})
				})
			})
			if next := upcoming(g.tasks, 3); len(next) > 0 {
				a.card(c, p, func() {
					ui.Row(c).Children(func() {
						ui.Text(c, a.tr("profileUpcoming")).Bold().Grow(1)
						if ui.Button(c, a.tr("overviewSeeAll")).FontSize(small).Padding(2, 8).Clicked() {
							a.Navigate("/routines")
						}
					})
					for _, t := range next {
						at := hubapi.ParseTime(t.NextRun).Local()
						ui.Row(c.Key("up-" + t.ID)).Gap(12).Children(func() {
							ui.Column(c).Width(64).Children(func() {
								ui.Text(c, at.Format("15:04")).Bold()
								ui.Text(c, a.dayLabel(at, now)).FontSize(11).TextColor(p.ink3)
							})
							ui.Column(c).Grow(1).Shrink(1).Children(func() {
								ui.Text(c, t.Title).SingleLine()
								ui.Text(c, a.taskWhen(t)).FontSize(small).TextColor(p.ink3).SingleLine()
							})
						})
					}
				})
			}
			a.card(c, p, func() {
				ui.Text(c, a.tr("profileReach")).Bold()
				ui.Text(c, a.tr("profileReachHint")).FontSize(small).TextColor(p.ink3)
				names := make([]string, 0, len(d.Machines))
				for _, m := range d.Machines {
					names = append(names, d.MachineName(m.ID))
				}
				rows := [][2]string{{"machine", a.tr("profileReachComputers")}}
				if len(names) > 0 {
					rows[0][1] = a.tr("profileReachMachines", "machines", joinNames(names))
				}
				rows = append(rows, [][2]string{{"agent", a.tr("profileReachAgents")}, {"inbox", a.tr("profileReachInbox")},
					{"book", a.tr("profileReachKnowledge")}, {"phone", a.tr("profileReachPhone")}}...)
				for _, r := range rows {
					ui.Row(c.Key("reach-" + r[0])).Gap(10).Children(func() {
						ui.Icon(c, icon(r[0])).FontSize(small).TextColor(p.ink3)
						ui.Text(c, r[1]).FontSize(small).Grow(1).Shrink(1)
					})
				}
			})
		})
	})
}

// upcoming are the enabled tasks' next runs, soonest first.
func upcoming(tasks []hubapi.Task, n int) []hubapi.Task {
	var out []hubapi.Task
	for _, t := range tasks {
		if t.Enabled && !hubapi.ParseTime(t.NextRun).IsZero() {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(x, y hubapi.Task) int { return hubapi.ParseTime(x.NextRun).Compare(hubapi.ParseTime(y.NextRun)) })
	return out[:min(n, len(out))]
}

// dayLabel is Today, Tomorrow or the date.
func (a *App) dayLabel(at, now time.Time) string {
	y1, m1, d1 := at.Date()
	y2, m2, d2 := now.Date()
	t2 := now.AddDate(0, 0, 1)
	y3, m3, d3 := t2.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return a.tr("today")
	case y1 == y3 && m1 == m3 && d1 == d3:
		return a.tr("tomorrow")
	}
	return at.Format("01-02")
}

func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}
