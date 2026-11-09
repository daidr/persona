package app

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// startForm is the New agent dialog: a computer, a folder on it, a launcher and a first message.
type startForm struct {
	open bool
	// todo is the to-do the agent works on, and its title.
	todo, todoTitle string
	machine         string // the computer's id
	machineLabel    string
	cwd             string
	dirs            hubapi.Dirs
	dirsBusy        bool
	launchers       []hubapi.Launcher
	launcher        string // the launcher's label
	launchersFor    string
	// tiers are the computer's presets, tier the one picked ("" none) and tierFor the launcher it picked, so a
	// launcher chosen by hand afterwards unmarks it.
	tiers        []hubapi.Tier
	tiersProfile string
	tier         string
	tierFor      string
	prompt       string
	busy         bool
	err          string
}

// openStart opens the New agent dialog, for a to-do when todo isn't "".
func (a *App) openStart(todo, todoTitle string) {
	f := &a.start
	*f = startForm{open: true, todo: todo, todoTitle: todoTitle, machine: f.machine, cwd: f.cwd, launcher: f.launcher}
	if todo != "" {
		f.prompt = todoTitle
	}
	labels, ids := a.startMachines()
	if i := indexOf(ids, f.machine); i >= 0 {
		f.machineLabel = labels[i]
	} else if len(ids) > 0 {
		f.machine, f.machineLabel, f.cwd = ids[0], labels[0], ""
	}
	a.loadStart(f.machine, f.cwd)
}

func indexOf(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return -1
}

// startMachines are the computers agents can start on: online ones that aren't the hub's own assistant.
func (a *App) startMachines() (labels, ids []string) {
	seen := map[string]bool{}
	for _, m := range a.conn.data.Machines {
		if !m.Online {
			continue
		}
		name := m.Name
		if name == "" {
			name = m.ID
		}
		if seen[name] {
			name += " (" + m.ID + ")"
		}
		seen[name] = true
		labels = append(labels, name)
		ids = append(ids, m.ID)
	}
	return labels, ids
}

func launcherLabel(l hubapi.Launcher) string {
	s := l.Label
	if s == "" {
		s = l.Name
	}
	if l.Model != "" && !strings.Contains(s, l.Model) {
		s += " · " + l.Model
	}
	return s
}

// loadStart reads the computer's launchers when it changed, and the folder path on it.
func (a *App) loadStart(machine, path string) {
	if machine == "" {
		return
	}
	c := a.conn
	f := &a.start
	f.dirsBusy = true
	needLaunchers := f.launchersFor != machine
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		dirs, dirsErr := c.client.Dirs(ctx, machine, path)
		var ls hubapi.Launchers
		var lsErr error
		if needLaunchers {
			ls, lsErr = c.client.Launchers(ctx, machine)
		}
		a.Host.Post(func() {
			if !a.live(c) || a.start.machine != machine {
				return
			}
			f.dirsBusy = false
			if dirsErr != nil {
				f.err = a.errorText(dirsErr)
			} else {
				f.err = ""
				f.dirs = dirs
				f.cwd = dirs.Path
			}
			if needLaunchers && lsErr == nil {
				f.launchers, f.launchersFor = ls.Launchers, machine
				f.tiers, f.tier, f.tiersProfile = ls.Tiers, "", ""
				if ls.TiersProfile != nil {
					f.tiersProfile = ls.TiersProfile.Name
				}
				labels := make([]string, len(ls.Launchers))
				for i, l := range ls.Launchers {
					labels[i] = launcherLabel(l)
				}
				if indexOf(labels, f.launcher) < 0 {
					f.launcher = ""
					if ls.Default != nil {
						for i, l := range ls.Launchers {
							if l.Name == ls.Default.Provider && (ls.Default.Model == "" || l.Model == ls.Default.Model) {
								f.launcher = labels[i]
								break
							}
						}
					}
					if f.launcher == "" && len(labels) > 0 {
						f.launcher = labels[0]
					}
				}
			}
		})
	})
}

// startView is the New agent dialog over whatever page is open.
func (a *App) startView(c *ui.Context, p palette) {
	f := &a.start
	if !f.open {
		return
	}
	ui.Modal(c, &f.open, func() {
		ui.Column(c).Width(560).Padding(20).Gap(14).Children(func() {
			ui.Text(c, a.tr("startTitle")).FontSize(heading).Bold()
			if f.todoTitle != "" {
				ui.Row(c).Gap(6).Children(func() {
					ui.Icon(c, icon("todo")).FontSize(small).TextColor(p.ink2)
					ui.Text(c, a.tr("startForTodo", "title", f.todoTitle)).FontSize(small).TextColor(p.ink2).SingleLine().Shrink(1)
				})
			}
			labels, ids := a.startMachines()
			if len(ids) == 0 {
				ui.Text(c, a.tr("startNoComputers")).TextColor(p.ink2)
				ui.Row(c).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, a.tr("cancel")).Clicked() {
						f.open = false
					}
				})
				return
			}
			a.field(c, p, a.tr("startComputer"), func() {
				if ui.Select(c.Key("start-machine"), &f.machineLabel, labels).Label(a.tr("startComputer")).Changed() {
					if i := indexOf(labels, f.machineLabel); i >= 0 && ids[i] != f.machine {
						f.machine, f.cwd, f.dirs = ids[i], "", hubapi.Dirs{}
						a.loadStart(f.machine, "")
					}
				}
			})
			a.field(c, p, a.tr("startFolder"), func() {
				a.folderPicker(c, p)
			})
			if len(f.tiers) > 0 {
				a.presetField(c, p)
			}
			if len(f.launchers) > 0 {
				opts := make([]string, len(f.launchers))
				for i, l := range f.launchers {
					opts[i] = launcherLabel(l)
				}
				a.field(c, p, a.tr("startLauncher"), func() {
					ui.Select(c.Key("start-launcher"), &f.launcher, opts).Label(a.tr("startLauncher"))
				})
			}
			a.field(c, p, a.tr("startPrompt"), func() {
				ui.TextArea(c.Key("start-prompt"), &f.prompt).Lines(3, 8).Placeholder(a.tr("startPromptPlaceholder")).Label(a.tr("startPrompt"))
			})
			if f.err != "" {
				ui.Text(c, f.err).FontSize(small).TextColor(p.red)
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if f.busy {
					ui.Spinner(c)
				}
				if ui.Button(c, a.tr("cancel")).Clicked() {
					f.open = false
				}
				if ui.PrimaryButton(c, a.tr("startGo")).Disabled(f.busy || f.cwd == "").Clicked() {
					a.startAgent()
				}
			})
		})
	})
}

// field is a labelled control in a form.
func (a *App) field(c *ui.Context, p palette, label string, fn func()) {
	ui.Column(c.Key("field-" + label)).Gap(6).Children(func() {
		ui.Text(c, label).FontSize(small).Bold().TextColor(p.ink2)
		fn()
	})
}

// folderPicker is the folder chosen, the folders agents started in lately, and the chosen folder's subfolders.
func (a *App) folderPicker(c *ui.Context, p palette) {
	f := &a.start
	ui.Row(c).Gap(8).Children(func() {
		in := ui.TextInput(c.Key("start-cwd"), &f.cwd).Label(a.tr("startFolder")).Font("monospace").FontSize(small).Grow(1).Shrink(1)
		if in.Submitted() {
			a.loadStart(f.machine, f.cwd)
		}
		if f.dirs.Parent != "" {
			if ui.Button(c, "").Label(a.tr("startUp")).Tooltip(a.tr("startUp")).Children(func() { ui.Icon(c, icon("up")) }).Disabled(f.dirsBusy).Clicked() {
				a.loadStart(f.machine, f.dirs.Parent)
			}
		}
	})
	ui.Scroll(c.Key("start-dirs")).MaxHeight(220).Radius(rowRadius).Border(1, p.line).Background(p.inset).Children(func() {
		ui.Column(c).Padding(4).Children(func() {
			if f.dirsBusy && len(f.dirs.Entries) == 0 {
				ui.Spinner(c).Margin(12, ui.Auto)
				return
			}
			if len(f.dirs.Recent) > 0 {
				ui.Text(c, a.tr("startRecent")).FontSize(11).Bold().TextColor(p.ink3).Padding(6, 8, 2, 8)
				for _, d := range f.dirs.Recent {
					a.dirRow(c, p, "r:", d, "clock")
				}
				ui.Text(c, a.tr("startSubfolders")).FontSize(11).Bold().TextColor(p.ink3).Padding(8, 8, 2, 8)
			}
			if len(f.dirs.Entries) == 0 {
				ui.Text(c, a.tr("startNoSubfolders")).FontSize(small).TextColor(p.ink3).Padding(6, 8)
			}
			for _, d := range f.dirs.Entries {
				ic := "folder"
				if d.IsGit {
					ic = "git"
				}
				a.dirRow(c, p, "e:", d, ic)
			}
		})
	})
}

func (a *App) dirRow(c *ui.Context, p palette, prefix string, d hubapi.Dir, ic string) {
	name := d.Name
	if name == "" {
		name = d.Path
	}
	row := ui.ButtonBase(c.Key(prefix+d.Path)).Padding(5, 8).Radius(rowRadius).Gap(8).Label(name)
	row.Children(func() {
		ui.Icon(c, icon(ic)).FontSize(small).TextColor(p.ink2)
		ui.Text(c, name).FontSize(small).SingleLine().Shrink(1)
		if prefix == "r:" && d.Path != name {
			ui.Text(c, d.Path).FontSize(11).TextColor(p.ink3).SingleLine().Shrink(1)
		}
		ui.Spacer(c)
	})
	if row.Hovered() {
		row.Background(p.hover)
	}
	if row.Clicked() {
		a.loadStart(a.start.machine, d.Path)
	}
}

func (a *App) startAgent() {
	c := a.conn
	f := &a.start
	req := hubapi.StartRequest{MachineID: f.machine, Cwd: strings.TrimSpace(f.cwd), Prompt: strings.TrimSpace(f.prompt), TodoID: f.todo}
	for _, l := range f.launchers {
		if launcherLabel(l) == f.launcher {
			req.Launcher, req.AgentType = l.Name, l.AgentType
		}
	}
	if t, ok := f.pickedTier(); ok {
		req.Model, req.Effort = t.Model, t.Effort
	}
	f.busy, f.err = true, ""
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		s, err := c.client.Start(ctx, req)
		a.Host.Post(func() {
			f.busy = false
			if !a.live(c) {
				return
			}
			if err != nil {
				f.err = a.errorText(err)
				return
			}
			f.open, f.prompt = false, ""
			if s.SessionID == "" {
				a.Navigate("/agents")
				return
			}
			c.data.PutSession(s)
			if req.TodoID != "" {
				a.refreshTodos(c)
			}
			a.Navigate("/agents/" + s.Key())
		})
	})
}

// pickedTier is the preset still in force: picked, and its launcher not changed by hand since.
func (f *startForm) pickedTier() (hubapi.Tier, bool) {
	if f.tier == "" || f.launcher != f.tierFor {
		return hubapi.Tier{}, false
	}
	for _, t := range f.tiers {
		if t.Name == f.tier {
			return t, true
		}
	}
	return hubapi.Tier{}, false
}

// presetField picks one of the model profile's tiers: it chooses the launcher, and its model and effort go with the start.
func (a *App) presetField(c *ui.Context, p palette) {
	f := &a.start
	none := a.tr("startPresetNone")
	opts := []string{none}
	for _, t := range f.tiers {
		opts = append(opts, t.Name)
	}
	picked := none
	t, ok := f.pickedTier()
	if ok {
		picked = t.Name
	}
	a.field(c, p, a.tr("startPreset"), func() {
		if ui.Select(c.Key("start-preset"), &picked, opts).Label(a.tr("startPreset")).Changed() {
			f.tier, f.tierFor = "", ""
			for _, x := range f.tiers {
				if x.Name != picked {
					continue
				}
				for _, l := range f.launchers {
					if l.Name == x.Launcher {
						f.tier, f.launcher, f.tierFor = x.Name, launcherLabel(l), launcherLabel(l)
						break
					}
				}
			}
			t, ok = f.pickedTier()
		}
		hint := a.tr("startPresetHint", "profile", f.tiersProfile)
		if ok {
			hint = t.Description
			if hint == "" {
				hint = strings.Join(strings.Fields(t.Launcher+" "+t.Model+" "+t.Effort), " · ")
			}
		}
		ui.Text(c, hint).FontSize(small).TextColor(p.ink3)
	})
}
