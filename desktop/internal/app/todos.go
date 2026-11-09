package app

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

type todosPage struct {
	list     ui.ListState
	showDone bool
	draft    string
	busy     map[string]bool
	// notes is the open to-do's notes as edited; notesFor names that to-do.
	notes, notesFor string
	deleting        bool
}

func (a *App) todoItems() []hubapi.Todo {
	var out []hubapi.Todo
	for _, t := range a.conn.data.Todos {
		if t.Done() == a.todos.showDone {
			out = append(out, t)
		}
	}
	return out
}

// todosView is the to-do list beside the to-do chosen.
func (a *App) todosView(c *ui.Context, p palette, open string) {
	g := &a.todos
	items := a.todoItems()
	now := time.Now()
	ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(listWidth).BorderWidth(0, 1, 0, 0).BorderColor(p.line).Children(func() {
			ui.Row(c).Padding(16, 16, 8, 16).Children(func() {
				ui.Text(c, a.tr("goTodos")).FontSize(title).Bold().Grow(1)
			})
			in := ui.TextInput(c.Key("todo-new"), &g.draft).Placeholder(a.tr("todoNew")).Label(a.tr("todoNew")).Margin(0, 16, 8, 16).Disabled(g.busy[""])
			if in.Submitted() {
				a.addTodo()
			}
			tab := 0
			if g.showDone {
				tab = 1
			}
			if ui.Segmented(c, &tab, a.tr("todosOpen"), a.tr("todosDone")).Margin(0, 16, 8, 16).Changed() {
				g.showDone = tab == 1
			}
			g.list.Key = func(i int) any { return items[i].ID }
			ui.List(c, &g.list, len(items), func(i int) {
				a.todoRow(c, p, items[i], items[i].ID == open, now)
			}).Grow(1).PaddingY(4).Children(func() {
				if len(items) == 0 {
					if !a.conn.data.TodosLoaded {
						ui.Spinner(c).Margin(24, ui.Auto)
					} else if g.showDone {
						a.empty(c, p, "check", a.tr("todosNoneDone"), "")
					} else {
						a.empty(c, p, "check", a.tr("todosEmpty"), a.tr("todosEmptyHint"))
					}
				}
			})
		})
		ui.Column(c).Grow(1).Children(func() {
			if t, ok := a.conn.data.Todo(open); ok && open != "" {
				a.todoDetail(c, p, t)
				return
			}
			a.empty(c, p, "check", a.tr("todosPick"), "")
		})
	})
}

func (a *App) todoRow(c *ui.Context, p palette, t hubapi.Todo, chosen bool, now time.Time) {
	bg := ui.Transparent
	if chosen {
		bg = p.hover
	}
	row := ui.ButtonBase(c.Key(t.ID)).Margin(0, 8).Padding(8, 10).Radius(rowRadius).Background(bg).Gap(10).AlignItems(ui.Start).Label(t.Title)
	row.Children(func() {
		mark, col := "circle", p.ink3
		if t.Done() {
			mark, col = "check", p.green
		}
		ui.Icon(c, icon(mark)).TextColor(col).Margin(1, 0, 0, 0)
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				tt := ui.Text(c, t.Title).SingleLine().Grow(1).Shrink(1)
				if t.Done() {
					tt.TextColor(p.ink2)
				}
				ui.Text(c, a.ago(t.UpdatedAt, now)).FontSize(small).TextColor(p.ink3).SingleLine()
			})
			if line := a.todoLine(t); line != "" {
				ui.Text(c, line).FontSize(small).TextColor(p.ink2).MaxLines(2)
			}
		})
	})
	if row.Hovered() && !chosen {
		row.Background(p.hover)
	}
	if row.Clicked() {
		a.Navigate("/todos/" + t.ID)
	}
}

// todoLine is what the list says under a to-do: its agents, its due date, or its notes.
func (a *App) todoLine(t hubapi.Todo) string {
	switch n := len(t.Agents); {
	case n == 1:
		return a.tr("todoAgentsOne")
	case n > 1:
		return a.tr("todoAgents", "count", itoa(n))
	}
	if t.DueDate != "" {
		return a.tr("todoDue", "date", t.DueDate)
	}
	return oneLine(t.Notes, 160)
}

func (a *App) todoDetail(c *ui.Context, p palette, t hubapi.Todo) {
	g := &a.todos
	if g.notesFor != t.ID {
		g.notesFor, g.notes, g.deleting = t.ID, t.Notes, false
	}
	busy := g.busy[t.ID]
	sub := ""
	if t.DueDate != "" {
		sub = a.tr("todoDue", "date", t.DueDate)
	}
	a.pageHeader(c, p, t.Title, sub, func() {
		if ui.Button(c, a.tr("startAgent")).Disabled(t.Done()).Clicked() {
			a.openStart(t.ID, t.Title)
		}
		label := a.tr("todoMarkDone")
		if t.Done() {
			label = a.tr("reopen")
		}
		if ui.PrimaryButton(c, label).Disabled(busy).Clicked() {
			a.patchTodo(t.ID, map[string]any{"done": !t.Done()})
		}
	})
	ui.Divider(c)
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).MaxWidth(720).Padding(16, pagePad, 32, pagePad).Gap(16).Children(func() {
			a.card(c, p, func() {
				ui.Text(c, a.tr("todoNotes")).Bold()
				ui.TextArea(c.Key("todo-notes"), &g.notes).Lines(3, 12).Placeholder(a.tr("todoNotesPlaceholder")).Label(a.tr("todoNotes"))
				if g.notes != t.Notes {
					ui.Row(c).Gap(8).Children(func() {
						ui.Spacer(c)
						if ui.Button(c, a.tr("cancel")).Clicked() {
							g.notes = t.Notes
						}
						if ui.PrimaryButton(c, a.tr("save")).Disabled(busy).Clicked() {
							a.patchTodo(t.ID, map[string]any{"notes": g.notes})
						}
					})
				}
			})
			if t.Stage != "" || t.PRURL != "" || (t.Ask != nil && t.Ask.Recommend != "") {
				a.card(c, p, func() {
					if t.Stage != "" {
						ui.Text(c, a.tr("todoStage", "stage", t.Stage)).FontSize(small).TextColor(p.ink2)
					}
					if t.Ask != nil && t.Ask.Recommend != "" {
						ui.Row(c).Gap(6).Children(func() {
							ui.Icon(c, icon("spark")).FontSize(small).TextColor(p.ai)
							ui.Text(c, t.Ask.Recommend).FontSize(small).Selectable().Grow(1).Shrink(1)
						})
					}
					if t.PRURL != "" {
						ui.Row(c).Children(func() {
							if ui.Button(c, a.tr("todoOpenPR")).Clicked() {
								a.Host.OpenURL(t.PRURL)
							}
						})
					}
				})
			}
			a.card(c, p, func() {
				ui.Text(c, a.tr("todoAgentsHeading")).Bold()
				if len(t.Agents) == 0 {
					ui.Text(c, a.tr("todoNoAgents")).FontSize(small).TextColor(p.ink2)
				}
				for _, ag := range t.Agents {
					a.todoAgentRow(c, p, ag)
				}
			})
			ui.Row(c).Children(func() {
				if ui.Button(c, a.tr("todoDelete")).TextColor(p.red).Disabled(busy).Clicked() {
					g.deleting = true
				}
			})
		})
	})
	switch ui.AlertDialog(c, &g.deleting, a.tr("todoDeleteTitle", "title", t.Title), a.tr("todoDeleteBody"), a.tr("cancel"), a.tr("todoDelete")) {
	case 1:
		a.deleteTodo(t.ID)
	}
}

func (a *App) todoAgentRow(c *ui.Context, p palette, ag hubapi.TodoAgent) {
	d := a.conn.data
	title := ag.Title
	state := hubapi.StateIdle
	if x, ok := d.Session(ag.Key()); ok {
		title = titleOf(x)
		state = d.StateOf(x)
	}
	if title == "" {
		title = ag.Cwd
	}
	row := ui.ButtonBase(c.Key(ag.Key())).Padding(8, 10).Radius(rowRadius).Gap(10).Label(title)
	row.Children(func() {
		ui.Box(c).Size(8, 8).Radius(4).Background(p.stateColor(state))
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, title).SingleLine()
			line := d.MachineName(ag.MachineID)
			if ag.Agent != "" {
				line += " · " + ag.Agent
			}
			ui.Text(c, line).FontSize(small).TextColor(p.ink2).SingleLine()
			if ag.StartError != "" {
				ui.Text(c, ag.StartError).FontSize(small).TextColor(p.red).MaxLines(3)
			}
		})
		ui.Text(c, a.stateLabel(state)).FontSize(small).TextColor(p.stateColor(state))
	})
	if row.Hovered() {
		row.Background(p.hover)
	}
	if row.Clicked() {
		a.Navigate("/agents/" + ag.Key())
	}
}

// todoCall runs a write on a to-do and puts what the hub answers in the list.
func (a *App) todoCall(id string, call func(context.Context, *hubapi.Client) (hubapi.Todo, error), after func(hubapi.Todo)) {
	c := a.conn
	if a.todos.busy == nil {
		a.todos.busy = map[string]bool{}
	}
	a.todos.busy[id] = true
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		t, err := call(ctx, c.client)
		a.Host.Post(func() {
			delete(a.todos.busy, id)
			if !a.live(c) {
				return
			}
			if err != nil {
				a.toast(a.errorText(err))
				return
			}
			after(t)
		})
	})
}

func (a *App) addTodo() {
	g := &a.todos
	title := g.draft
	// a new to-do's call is busy under no id
	if oneLine(title, 1000) == "" || g.busy[""] {
		return
	}
	a.todoCall("", func(ctx context.Context, cl *hubapi.Client) (hubapi.Todo, error) {
		return cl.CreateTodo(ctx, title, "")
	}, func(t hubapi.Todo) {
		a.conn.data.PutTodo(t)
		g.draft, g.showDone = "", false
		a.Navigate("/todos/" + t.ID)
	})
}

func (a *App) patchTodo(id string, patch map[string]any) {
	a.todoCall(id, func(ctx context.Context, cl *hubapi.Client) (hubapi.Todo, error) {
		return cl.UpdateTodo(ctx, id, patch)
	}, func(t hubapi.Todo) {
		a.conn.data.PutTodo(t)
		if a.todos.notesFor == t.ID {
			a.todos.notes = t.Notes
		}
	})
}

func (a *App) deleteTodo(id string) {
	a.todoCall(id, func(ctx context.Context, cl *hubapi.Client) (hubapi.Todo, error) {
		return hubapi.Todo{}, cl.DeleteTodo(ctx, id)
	}, func(hubapi.Todo) {
		a.conn.data.DropTodo(id)
		a.Navigate("/todos")
	})
}
