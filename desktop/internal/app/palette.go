package app

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

// paletteState is the command palette (⌘K / Ctrl+K): a field and what matches it, one of them chosen.
type paletteState struct {
	open  bool
	query string
	sel   int
	// shown is the query the list was last built for, so the choice goes back to the top when it changes;
	// scrolled is the choice last scrolled into view.
	shown    string
	scrolled int
}

// command is a row of the palette: a place, an agent, a chat, a to-do, a hub or an action.
type command struct {
	id    string
	icon  string
	title string
	sub   string
	run   func()
}

// OpenPalette opens the command palette; the Go menu and ⌘K call it.
func (a *App) OpenPalette() {
	if a.conn == nil {
		return
	}
	a.palette = paletteState{open: true, scrolled: -1}
}

// commands are everything the palette reaches, in the order it lists them with nothing typed.
func (a *App) commands() []command {
	d := a.conn.data
	var out []command
	add := func(id, ic, title, sub string, run func()) {
		out = append(out, command{id: id, icon: ic, title: title, sub: sub, run: run})
	}
	add("new-agent", "plus", a.tr("startTitle"), a.tr("paletteAction"), func() { a.openStart("", "") })
	add("new-chat", "spark", a.tr("personaNew"), a.tr("paletteAction"), func() { a.Navigate("/persona/new") })
	for _, pg := range []struct{ key, ic, path string }{
		{"goPersona", "home", "/"}, {"goAgents", "agent", "/agents"}, {"goTodos", "todo", "/todos"}, {"goInbox", "inbox", "/inbox"},
		{"goKnowledge", "book", "/knowledge"}, {"goMachines", "machine", "/machines"}, {"goSettings", "gear", "/settings"},
	} {
		path := pg.path
		add("page:"+path, pg.ic, a.tr(pg.key), a.tr("palettePage"), func() { a.Navigate(path) })
	}
	for _, grp := range d.Agents("") {
		for _, x := range grp.Agents {
			path := "/agents/" + x.Key()
			add("agent:"+x.Key(), "agent", titleOf(x), d.MachineName(x.MachineID)+" · "+a.stateLabel(grp.State), func() { a.Navigate(path) })
		}
	}
	open, _ := a.conversations()
	main, _ := d.MainConversation()
	for _, x := range open {
		title := titleOf(x)
		if x.SessionID == main.SessionID {
			title = a.tr("personaMain")
		}
		path := "/persona/" + x.SessionID
		add("chat:"+x.SessionID, "chat", title, a.tr("goPersona"), func() { a.Navigate(path) })
	}
	for _, t := range d.Todos {
		if t.Done() {
			continue
		}
		path := "/todos/" + t.ID
		add("todo:"+t.ID, "todo", t.Title, a.tr("goTodos"), func() { a.Navigate(path) })
	}
	current := a.Hubs.Settings.Current
	for _, h := range a.Hubs.Hubs {
		if h.URL == current {
			continue
		}
		url := h.URL
		add("hub:"+url, "hub", a.tr("paletteSwitchHub", "hub", h.Title()), a.tr("paletteHub"), func() { a.Switch(url) })
	}
	return out
}

// matchCommands keeps the commands whose title or detail has every word of the query, titles starting with it first.
func matchCommands(all []command, query string) []command {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return all
	}
	type hit struct {
		cmd  command
		rank int
	}
	var hits []hit
	for _, cmd := range all {
		title, hay := strings.ToLower(cmd.title), strings.ToLower(cmd.title+" "+cmd.sub)
		ok := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		rank := 2
		switch {
		case strings.HasPrefix(title, words[0]):
			rank = 0
		case strings.Contains(title, words[0]):
			rank = 1
		}
		hits = append(hits, hit{cmd, rank})
	}
	slices.SortStableFunc(hits, func(x, y hit) int { return x.rank - y.rank })
	out := make([]command, len(hits))
	for i, h := range hits {
		out[i] = h.cmd
	}
	return out
}

// paletteView draws the palette over the window while it's open, and opens it on ⌘K / Ctrl+K.
func (a *App) paletteView(c *ui.Context, p palette) {
	g := &a.palette
	// only opens: the Go menu's item has the same shortcut, and where both see the key it must not close again
	if c.Shortcut(ui.Cmd, ui.KeyK) && !g.open {
		a.OpenPalette()
	}
	if !g.open {
		return
	}
	list := matchCommands(a.commands(), g.query)
	if len(list) > 60 {
		list = list[:60]
	}
	if g.query != g.shown {
		g.sel, g.shown = 0, g.query
	}
	g.sel = max(0, min(g.sel, len(list)-1))
	run := func(cmd command) {
		g.open = false
		cmd.run()
	}
	ui.Modal(c, &g.open, func() {
		ui.Column(c).Width(600).Padding(12).Gap(8).Children(func() {
			// a TextInput, not a SearchField, so the arrows and Enter reach OnTextKey
			ui.TextInput(c.Key("palette-query"), &g.query).Placeholder(a.tr("palettePlaceholder")).Label(a.tr("palettePlaceholder")).
				FontSize(heading).Padding(8, 10).AutoFocus().OnTextKey(func(k ui.TextKey) bool {
				switch {
				case k.Key == ui.KeyDown:
					g.sel = min(g.sel+1, len(list)-1)
				case k.Key == ui.KeyUp:
					g.sel = max(g.sel-1, 0)
				case k.Key == ui.KeyEnter && k.Mods == 0:
					if g.sel < len(list) {
						run(list[g.sel])
					}
				default:
					return false
				}
				return true
			})
			if len(list) == 0 {
				ui.Text(c, a.tr("paletteNothing")).TextColor(p.ink2).Padding(16).TextAlign(ui.Center)
				return
			}
			ui.Scroll(c.Key("palette-list")).MaxHeight(420).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					for i, cmd := range list {
						a.commandRow(c, p, cmd, i == g.sel, func() { run(cmd) })
					}
				})
			})
		})
	})
}

func (a *App) commandRow(c *ui.Context, p palette, cmd command, chosen bool, run func()) {
	row := ui.ButtonBase(c.Key("cmd:"+cmd.id)).Padding(8, 10).Radius(rowRadius).Gap(10).Label(cmd.title)
	if chosen {
		row.Background(p.hover)
		if g := &a.palette; g.scrolled != g.sel {
			g.scrolled = g.sel
			row.ScrollIntoView()
		}
	}
	if row.Hovered() && !chosen {
		row.Background(p.inset)
	}
	if row.Children(func() {
		ui.Icon(c, icon(cmd.icon)).TextColor(p.ink2)
		ui.Text(c, cmd.title).SingleLine().Grow(1).Shrink(1)
		if cmd.sub != "" {
			ui.Text(c, cmd.sub).FontSize(small).TextColor(p.ink3).SingleLine().Shrink(2)
		}
	}).Clicked() {
		run()
	}
}
