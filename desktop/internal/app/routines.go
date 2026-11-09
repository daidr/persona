package app

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// routinesPage is Scheduled: the assistant's scheduled tasks and their recent runs, and the task being edited.
type routinesPage struct {
	conn  *conn
	load  loadable
	tasks []hubapi.Task
	runs  []hubapi.Run
	// edit is the task in the editor (open when editing); its fields as typed.
	editing  bool
	edit     hubapi.Task
	repeat   int
	at       string // YYYY-MM-DD HH:MM, local time
	every    string
	confirm  string // the task whose Delete asks to be sure
	editBusy bool
	editErr  string
}

var repeats = []string{"once", "daily", "interval"}

func (a *App) routinesView(c *ui.Context, p palette) {
	g := &a.routines
	if g.conn != a.conn {
		*g = routinesPage{conn: a.conn}
	}
	a.load(&g.load, func(ctx context.Context, cl *hubapi.Client) (func(), error) {
		tasks, err := cl.Tasks(ctx)
		if err != nil {
			return nil, err
		}
		runs, _ := cl.Runs(ctx)
		return func() { g.tasks, g.runs = tasks, runs }, nil
	})
	a.pageHeader(c, p, a.tr("goRoutines"), a.tr("routinesHint"), func() {
		if ui.Button(c, a.tr("routinesNew")).Clicked() {
			a.editTask(hubapi.Task{Repeat: "daily", Time: "09:00", Enabled: true})
		}
	})
	ui.Divider(c)
	now := time.Now()
	ui.Scroll(c.Key("routines")).Grow(1).Children(func() {
		ui.Column(c).MaxWidth(820).Padding(16, pagePad, 32, pagePad).Gap(16).Children(func() {
			if !a.loadState(c, p, &g.load) {
				return
			}
			if len(g.tasks) == 0 {
				a.empty(c, p, "clock", a.tr("routinesNone"), "")
			}
			for i, t := range g.tasks {
				a.taskRow(c.Key("task-"+itoa(i)), p, t, now)
			}
			if len(g.runs) > 0 {
				a.card(c, p, func() {
					ui.Text(c, a.tr("routinesRuns")).Bold()
					for i, r := range g.runs[:min(20, len(g.runs))] {
						a.runRow(c.Key("run-"+itoa(i)), p, r, now)
					}
				})
			}
		})
	})
	a.taskEditor(c, p)
}

// when says when a task runs, in words.
func (a *App) taskWhen(t hubapi.Task) string {
	switch t.Repeat {
	case "daily":
		days := ""
		if len(t.Weekdays) > 0 && len(t.Weekdays) < 7 {
			names := []string{}
			for _, d := range t.Weekdays {
				names = append(names, a.weekday(d))
			}
			days = " · " + strings.Join(names, " ")
		}
		return a.tr("routinesDaily", "time", t.Time) + days
	case "interval":
		return a.tr("routinesEvery", "minutes", itoa(t.EveryMinutes))
	}
	if at := hubapi.ParseTime(t.At); !at.IsZero() {
		return a.tr("routinesOnce", "time", at.Local().Format("2006-01-02 15:04"))
	}
	return a.tr("routinesOnceNoTime")
}

// runStatus says how a run went, in words; a status it doesn't know stays as the hub wrote it.
func (a *App) runStatus(s string) string {
	switch s {
	case "ok", "done":
		return a.tr("runOk")
	case "failed", "error":
		return a.tr("runFailed")
	case "skipped":
		return a.tr("runSkipped")
	case "running":
		return a.tr("runRunning")
	}
	return s
}

// weekday is 1 (Monday) to 7 (Sunday), short.
func (a *App) weekday(d int) string {
	switch d {
	case 1:
		return a.tr("dayMon")
	case 2:
		return a.tr("dayTue")
	case 3:
		return a.tr("dayWed")
	case 4:
		return a.tr("dayThu")
	case 5:
		return a.tr("dayFri")
	case 6:
		return a.tr("daySat")
	}
	return a.tr("daySun")
}

func (a *App) taskRow(c *ui.Context, p palette, t hubapi.Task, now time.Time) {
	g := &a.routines
	a.card(c, p, func() {
		ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).Gap(3).Children(func() {
				ui.Text(c, t.Title).Bold().SingleLine()
				line := a.taskWhen(t)
				if next := hubapi.ParseTime(t.NextRun); !next.IsZero() && t.Enabled {
					line += " · " + a.tr("routinesNext", "time", next.Local().Format("01-02 15:04"))
				}
				ui.Text(c, line).FontSize(small).TextColor(p.ink2).SingleLine()
				if t.LastRun != "" {
					last := a.when(t.LastRun, now, a.tr("routinesLastAgo"), a.tr("routinesLastOn"))
					if t.LastStatus != "" {
						last += " · " + a.runStatus(t.LastStatus)
					}
					ui.Text(c, last).FontSize(small).TextColor(p.ink3).SingleLine()
				}
				if t.LastError != "" {
					ui.Text(c, t.LastError).FontSize(small).TextColor(p.red).MaxLines(2)
				}
			})
			on := t.Enabled
			if ui.Switch(c, &on).Label(a.tr("routinesEnabled", "title", t.Title)).Disabled(g.load.busyOn != "").Changed() {
				next := t
				next.Enabled = on
				a.change(&g.load, t.ID, func(ctx context.Context, cl *hubapi.Client) error {
					_, err := cl.SaveTask(ctx, next)
					return err
				}, nil)
			}
		})
		ui.Text(c, t.Prompt).FontSize(small).TextColor(p.ink2).MaxLines(3)
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, a.tr("routinesRunNow")).Disabled(g.load.busyOn != "").Clicked() {
				a.runTask(t)
			}
			if ui.Button(c, a.tr("routinesEdit")).Clicked() {
				a.editTask(t)
			}
			if t.RunMachineID != "" && t.RunSessionID != "" {
				if ui.Button(c, a.tr("routinesOpenChat")).Clicked() {
					a.openSession(t.RunMachineID, t.RunSessionID)
				}
			}
			ui.Spacer(c)
			if g.confirm == t.ID {
				if ui.Button(c, a.tr("cancel")).Clicked() {
					g.confirm = ""
				}
				if ui.Button(c, a.tr("routinesDeleteConfirm")).TextColor(p.red).Clicked() {
					id := t.ID
					a.change(&g.load, id, func(ctx context.Context, cl *hubapi.Client) error { return cl.DeleteTask(ctx, id) },
						func() { g.confirm = "" })
				}
			} else if ui.Button(c, a.tr("todoDelete")).TextColor(p.red).Clicked() {
				g.confirm = t.ID
			}
		})
	})
}

// openSession opens a chat: the assistant's on the Persona page, an agent's on Agents.
func (a *App) openSession(machine, session string) {
	if as := a.conn.data.Assistant; as.MachineID == machine {
		a.Navigate("/persona/" + session)
		return
	}
	a.Navigate("/agents/" + machine + "/" + session)
}

func (a *App) runRow(c *ui.Context, p palette, r hubapi.Run, now time.Time) {
	ui.Row(c).Gap(8).Children(func() {
		col := p.ink2
		switch r.Status {
		case "ok", "done":
			col = p.green
		case "failed", "error":
			col = p.red
		}
		ui.Box(c).Size(8, 8).Radius(4).Background(col)
		ui.Text(c, r.Title).SingleLine().Grow(1).Shrink(1)
		if r.Status != "" {
			ui.Text(c, a.runStatus(r.Status)).FontSize(small).TextColor(col).SingleLine()
		}
		ui.Text(c, a.when(r.At, now, a.tr("routinesLastAgo"), a.tr("routinesLastOn"))).FontSize(small).TextColor(p.ink3).SingleLine()
		if r.MachineID != "" && r.SessionID != "" {
			if ui.Button(c, a.tr("overviewOpen")).FontSize(small).Padding(2, 8).Clicked() {
				a.openSession(r.MachineID, r.SessionID)
			}
		}
	})
}

func (a *App) runTask(t hubapi.Task) {
	g := &a.routines
	c := a.conn
	g.load.busyOn = t.ID
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		machine, session, err := c.client.RunTask(ctx, t.ID)
		a.Host.Post(func() {
			g.load.busyOn = ""
			if !a.live(c) {
				return
			}
			if err != nil {
				a.toast(a.errorText(err))
				return
			}
			g.load.asked = false
			a.toast(a.tr("routinesStarted", "title", t.Title))
			if machine != "" && session != "" {
				a.openSession(machine, session)
			}
		})
	})
}

func (a *App) editTask(t hubapi.Task) {
	g := &a.routines
	g.editing, g.edit, g.editErr, g.editBusy = true, t, "", false
	g.repeat = max(0, slices.Index(repeats, t.Repeat))
	g.at, g.every = "", ""
	if at := hubapi.ParseTime(t.At); !at.IsZero() {
		g.at = at.Local().Format("2006-01-02 15:04")
	} else {
		g.at = time.Now().Add(time.Hour).Truncate(time.Hour).Format("2006-01-02 15:04")
	}
	if t.EveryMinutes > 0 {
		g.every = itoa(t.EveryMinutes)
	} else {
		g.every = "60"
	}
}

func (a *App) taskEditor(c *ui.Context, p palette) {
	g := &a.routines
	if !g.editing {
		return
	}
	e := &g.edit
	ui.Modal(c, &g.editing, func() {
		ui.Column(c).Width(560).Padding(20).Gap(12).Children(func() {
			title := a.tr("routinesNew")
			if e.ID != "" {
				title = a.tr("routinesEdit")
			}
			ui.Text(c, title).FontSize(heading).Bold()
			a.field(c, p, a.tr("routinesTitle"), func() {
				ui.TextInput(c.Key("task-title"), &e.Title).Label(a.tr("routinesTitle"))
			})
			a.field(c, p, a.tr("routinesPrompt"), func() {
				ui.TextArea(c.Key("task-prompt"), &e.Prompt).Lines(3, 8).Label(a.tr("routinesPrompt"))
			})
			ui.Segmented(c.Key("task-repeat"), &g.repeat, a.tr("routinesRepeatOnce"), a.tr("routinesRepeatDaily"), a.tr("routinesRepeatInterval"))
			switch repeats[g.repeat] {
			case "once":
				a.field(c, p, a.tr("routinesAt"), func() {
					ui.TextInput(c.Key("task-at"), &g.at).Placeholder("2026-01-31 09:00").Label(a.tr("routinesAt"))
				})
			case "daily":
				a.field(c, p, a.tr("routinesTime"), func() {
					ui.TextInput(c.Key("task-time"), &e.Time).Placeholder("09:00").Label(a.tr("routinesTime")).Width(120)
				})
				ui.Row(c).Gap(10).Wrap().Children(func() {
					for d := 1; d <= 7; d++ {
						on := len(e.Weekdays) == 0 || slices.Contains(e.Weekdays, d)
						if ui.Checkbox(c.Key("day-"+itoa(d)), &on, a.weekday(d)).Changed() {
							all := e.Weekdays
							if len(all) == 0 {
								all = []int{1, 2, 3, 4, 5, 6, 7}
							}
							all = slices.DeleteFunc(slices.Clone(all), func(x int) bool { return x == d })
							if on {
								all = append(all, d)
								slices.Sort(all)
							}
							if len(all) == 7 {
								all = nil
							}
							e.Weekdays = all
						}
					}
				})
			case "interval":
				a.field(c, p, a.tr("routinesEveryMinutes"), func() {
					ui.TextInput(c.Key("task-every"), &g.every).Label(a.tr("routinesEveryMinutes")).Width(120)
				})
			}
			ui.Checkbox(c.Key("task-on"), &e.Enabled, a.tr("routinesOn"))
			if g.editErr != "" {
				ui.Text(c, g.editErr).FontSize(small).TextColor(p.red)
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c, a.tr("cancel")).Clicked() {
					g.editing = false
				}
				if ui.PrimaryButton(c, a.tr("save")).Disabled(g.editBusy || strings.TrimSpace(e.Title) == "" || strings.TrimSpace(e.Prompt) == "").Clicked() {
					a.saveTask()
				}
			})
		})
	})
}

func (a *App) saveTask() {
	g := &a.routines
	t := g.edit
	t.Title, t.Prompt, t.Repeat = strings.TrimSpace(t.Title), strings.TrimSpace(t.Prompt), repeats[g.repeat]
	switch t.Repeat {
	case "once":
		at, err := time.ParseInLocation("2006-01-02 15:04", strings.TrimSpace(g.at), time.Local)
		if err != nil {
			g.editErr = a.tr("routinesBadTime")
			return
		}
		t.At = at.UTC().Format(time.RFC3339)
	case "daily":
		if _, err := time.Parse("15:04", strings.TrimSpace(t.Time)); err != nil {
			g.editErr = a.tr("routinesBadTime")
			return
		}
		t.Time = strings.TrimSpace(t.Time)
	case "interval":
		n, err := strconv.Atoi(strings.TrimSpace(g.every))
		if err != nil || n < 1 {
			g.editErr = a.tr("routinesBadEvery")
			return
		}
		t.EveryMinutes = n
	}
	c := a.conn
	g.editBusy, g.editErr = true, ""
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := c.client.SaveTask(ctx, t)
		a.Host.Post(func() {
			g.editBusy = false
			if !a.live(c) {
				return
			}
			if err != nil {
				g.editErr = a.errorText(err)
				return
			}
			g.editing = false
			g.load.asked = false
		})
	})
}
