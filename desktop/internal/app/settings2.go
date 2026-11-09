package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// Settings › Persona (what the assistant does on its own, what it knows about you, where it runs), Settings › Agents
// (the session triage switches) and the model profiles above Settings › Models. Each is the hub's, the same on every
// device; a switch changes at once and goes back when the hub refuses.

const modelsTab = 6

// ── Persona ──

func (a *App) personaSettings(c *ui.Context, p palette) {
	g := &a.sets
	a.load(&g.persona, func(ctx context.Context, cl *hubapi.Client) (func(), error) {
		st, err := cl.AssistantSettings(ctx)
		if err != nil {
			return nil, err
		}
		tasks, _ := cl.Tasks(ctx)
		files, merr := cl.Memory(ctx)
		skills, on, serr := cl.Skills(ctx)
		return func() {
			g.asst, g.asstTasks, g.memory, g.skills, g.skillsOn = st, tasks, files, skills, on
			g.notes = st.Notes
			if st.Notes == "" {
				g.notes = st.NotesEffective
			}
			g.memoryErr, g.skillErr = "", ""
			if merr != nil {
				g.memoryErr = a.errorText(merr)
			}
			if serr != nil {
				g.skillErr = a.errorText(serr)
			}
		}, nil
	})
	if !a.loadState(c, p, &g.persona) {
		return
	}
	st := g.asst
	d := a.conn.data
	var machine hubapi.Machine
	for _, m := range d.Machines {
		if m.ID == st.MachineID {
			machine = m
		}
	}
	name := machine.Name
	if name == "" {
		name = st.MachineID
	}
	ui.Row(c).Gap(12).Children(func() {
		ui.Box(c).Size(44, 44).Radius(22).Background(p.ai.Alpha(0.15)).Children(func() {
			ui.Icon(c, icon("spark")).FontSize(18).TextColor(p.ai).Margin(ui.Auto)
		})
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, "Persona").FontSize(heading).Bold()
			sub := a.tr("setPersonaSubtitle")
			if name != "" {
				sub = a.tr("setPersonaSubtitleOn", "machine", name)
			}
			ui.Text(c, sub).FontSize(small).TextColor(p.ink2).SingleLine()
		})
		if machine.ID != "" {
			if machine.Online {
				a.pill(c, a.tr("setPersonaOnline"), p.green)
			} else {
				a.pill(c, a.tr("setPersonaOffline"), p.ink3)
			}
		}
	})

	a.group(c, p, a.tr("setPersonaProactive"), a.tr("setPersonaProactiveFooter"), func() {
		on := 0
		for _, t := range g.asstTasks {
			if t.Enabled {
				on++
			}
		}
		hint := a.tr("setPersonaNothingScheduled")
		if next := upcoming(g.asstTasks, 1); len(next) > 0 {
			at := hubapi.ParseTime(next[0].NextRun).Local()
			hint = a.tr("setPersonaNext", "when", a.dayLabel(at, time.Now())+" "+at.Format("15:04"), "title", next[0].Title)
		}
		value := a.tr("setPersonaRoutinesOff")
		if on > 0 {
			value = a.tr("setPersonaRoutinesOn", "count", itoa(on))
		}
		a.linkRow(c, p, "routines", a.tr("setPersonaRoutines"), hint, value, func() { a.Navigate("/routines") })
	})
	a.group(c, p, a.tr("setPersonaNotifications"), a.tr("setPersonaNotificationsFooter"), func() {
		quiet := hubapi.On(st.MainTasksQuiet)
		if a.switchRow(c, p, a.tr("setPersonaQuietTasks"), "", &quiet) {
			a.setAssistant("main_tasks_quiet", quiet)
		}
	})
	a.group(c, p, a.tr("setPersonaStart"), a.tr("setPersonaStartFooter"), func() {
		start := hubapi.On(st.StartRequestedTasks)
		if a.switchRow(c, p, a.tr("setPersonaStartRequested"), "", &start) {
			a.setAssistant("start_requested_tasks", start)
		}
	})
	a.group(c, p, a.tr("settingsModels"), "", func() {
		a.linkRow(c, p, "task-models", a.tr("setPersonaTaskModels"), "", "", func() { g.tab = modelsTab })
		a.linkRow(c, p, "main-model", a.tr("setPersonaMainModel"), "", "", func() { g.tab = modelsTab })
	})
	a.group(c, p, a.tr("setPersonaPermissions"), a.tr("setPersonaPermissionsHint"), func() {
		unset := a.tr("setPersonaPermissionsUnset")
		opts := []string{unset}
		for _, m := range hubapi.PermissionModes {
			opts = append(opts, a.modeLabel(m))
		}
		cur := unset
		if st.DefaultPermissionMode != "" {
			cur = a.modeLabel(st.DefaultPermissionMode)
		}
		ui.Row(c).Gap(12).Children(func() {
			ui.Text(c, a.tr("setPersonaPermissions")).Grow(1).Shrink(1)
			if ui.Select(c.Key("default-mode"), &cur, opts).Label(a.tr("setPersonaPermissions")).Width(240).Disabled(g.asstBusy != "").Changed() {
				for _, m := range hubapi.PermissionModes {
					if a.modeLabel(m) == cur {
						a.setAssistant("default_permission_mode", m)
					}
				}
			}
		})
	})
	a.group(c, p, a.tr("setPersonaKnows"), a.tr("setPersonaKnowsFooter"), func() {
		a.aboutMe(c, p)
		ui.Divider(c)
		a.memoryList(c, p)
	})
	a.skillsGroup(c, p)
	a.group(c, p, a.tr("setPersonaRunsOn"), a.tr("setPersonaRunsOnFooter", "machine", name), func() {
		if st.MachineID == "" {
			ui.Text(c, a.tr("setPersonaRunsOnNone")).FontSize(small).TextColor(p.ink2)
			return
		}
		a.valueRow(c, p, a.tr("setPersonaMachine"), name)
		a.valueRow(c, p, a.tr("setPersonaCwd"), st.Cwd)
		a.valueRow(c, p, a.tr("setPersonaTimezone"), st.Timezone)
	})
	a.memoryDialog(c, p)
}

// group is a titled card with a line under it, as the web app's settings groups.
func (a *App) group(c *ui.Context, p palette, title, footer string, fn func()) {
	ui.Column(c.Key("group-" + title)).Gap(6).Children(func() {
		ui.Text(c, title).FontSize(small).Bold().TextColor(p.ink2).Padding(0, 4)
		a.card(c, p, fn)
		if footer != "" {
			ui.Text(c, footer).FontSize(small).TextColor(p.ink3).Padding(0, 4)
		}
	})
}

// linkRow is a row that goes somewhere, with what it holds at its end.
func (a *App) linkRow(c *ui.Context, p palette, key, label, hint, value string, open func()) {
	row := ui.ButtonBase(c.Key("link-"+key)).Padding(6, 8).Margin(-6, -8).Radius(rowRadius).Gap(12).Label(label)
	row.Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, label)
			if hint != "" {
				ui.Text(c, hint).FontSize(small).TextColor(p.ink2).SingleLine()
			}
		})
		if value != "" {
			ui.Text(c, value).FontSize(small).TextColor(p.ink2).SingleLine()
		}
		ui.Text(c, "›").TextColor(p.ink3)
	})
	if row.Hovered() {
		row.Background(p.hover)
	}
	if row.Clicked() {
		open()
	}
}

func (a *App) valueRow(c *ui.Context, p palette, label, value string) {
	if value == "" {
		return
	}
	ui.Row(c.Key("value-" + label)).Gap(12).Children(func() {
		ui.Text(c, label).TextColor(p.ink2).Width(140).SingleLine()
		ui.Text(c, value).Grow(1).Shrink(1).Selectable()
	})
}

// setAssistant changes one of the assistant's settings, shown at once and put back when the hub refuses.
func (a *App) setAssistant(field string, value any) {
	g := &a.sets
	if g.asstBusy != "" {
		return
	}
	before := g.asst
	switch field {
	case "main_tasks_quiet":
		v := value.(bool)
		g.asst.MainTasksQuiet = &v
	case "start_requested_tasks":
		v := value.(bool)
		g.asst.StartRequestedTasks = &v
	case "default_permission_mode":
		g.asst.DefaultPermissionMode = value.(string)
	}
	g.asstBusy = field
	cn := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		got, err := cn.client.SetAssistant(ctx, map[string]any{field: value})
		a.Host.Post(func() {
			g.asstBusy = ""
			if !a.live(cn) {
				return
			}
			if err == nil && field == "default_permission_mode" && got.DefaultPermissionMode != value {
				err = errUnconfirmed
			}
			if err != nil {
				g.asst = before
				msg := a.errorText(err)
				if err == errUnconfirmed {
					msg = a.tr("setPersonaPermissionsUnconfirmed")
				}
				a.toast(a.tr("setSaveFailed", "error", msg))
				return
			}
			switch field {
			case "main_tasks_quiet":
				if got.MainTasksQuiet != nil {
					g.asst.MainTasksQuiet = got.MainTasksQuiet
				}
			case "start_requested_tasks":
				if got.StartRequestedTasks != nil {
					g.asst.StartRequestedTasks = got.StartRequestedTasks
				}
			}
		})
	})
}

type unconfirmed struct{}

func (unconfirmed) Error() string { return "unconfirmed" }

var errUnconfirmed error = unconfirmed{}

// aboutMe is what you tell Persona, added to the end of its instructions.
func (a *App) aboutMe(c *ui.Context, p palette) {
	g := &a.sets
	st := g.asst
	saved := st.Notes
	if saved == "" {
		saved = st.NotesEffective
	}
	ui.Row(c).Gap(8).Children(func() {
		ui.Text(c, a.tr("setPersonaAboutMe")).Bold().Grow(1)
		if st.Notes == "" && st.NotesEffective != "" {
			a.pill(c, a.tr("setPersonaFromHub"), p.ink3)
		}
	})
	ui.TextArea(c.Key("about-me"), &g.notes).Lines(3, 12).Placeholder(a.tr("setPersonaAboutPlaceholder")).Label(a.tr("setPersonaAboutMe"))
	note := a.tr("setPersonaAboutNote")
	if st.Notes == "" && st.NotesEffective != "" {
		note += " " + a.tr("setPersonaAboutNoteHub")
	}
	ui.Text(c, note).FontSize(small).TextColor(p.ink3)
	changed := g.notes != saved
	ui.Row(c).Gap(8).Children(func() {
		ui.Spacer(c)
		if g.notesBusy {
			ui.Spinner(c)
		}
		if ui.Button(c, a.tr("setPersonaRevert")).Disabled(!changed || g.notesBusy).Clicked() {
			g.notes = saved
		}
		if ui.PrimaryButton(c, a.tr("save")).Disabled(!changed || g.notesBusy).Clicked() {
			a.saveNotes()
		}
	})
}

func (a *App) saveNotes() {
	g := &a.sets
	notes := g.notes
	g.notesBusy = true
	cn := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := cn.client.SetAssistant(ctx, map[string]any{"notes": notes})
		a.Host.Post(func() {
			g.notesBusy = false
			if !a.live(cn) {
				return
			}
			if err != nil {
				a.toast(a.tr("setPersonaAboutSaveFailed", "error", a.errorText(err)))
				return
			}
			g.asst.Notes, g.asst.NotesEffective = notes, strings.TrimSpace(notes)
			a.toast(a.tr("setPersonaAboutSaved"))
		})
	})
}

func (a *App) memoryTypeLabel(t string) string {
	switch t {
	case "user":
		return a.tr("memoryTypeUser")
	case "feedback":
		return a.tr("memoryTypeFeedback")
	case "project":
		return a.tr("memoryTypeProject")
	case "reference":
		return a.tr("memoryTypeReference")
	}
	return t
}

func (p palette) memoryTone(t string) ui.Color {
	switch t {
	case "user":
		return p.accent
	case "feedback":
		return p.orange
	case "project":
		return p.green
	}
	return p.ink2
}

// memoryList is what Persona noted on its own, newest first, each opening to read, edit or delete.
func (a *App) memoryList(c *ui.Context, p palette) {
	g := &a.sets
	entries := hubapi.MemoryEntries(g.memory)
	ui.Row(c).Gap(8).Children(func() {
		ui.Text(c, a.tr("setPersonaMemory")).Bold().Grow(1)
		if len(entries) > 0 {
			ui.Text(c, a.tr("setPersonaMemoryCount", "count", itoa(len(entries)))).FontSize(small).TextColor(p.ink3)
		}
	})
	switch {
	case g.memoryErr != "":
		ui.Text(c, a.tr("setPersonaMemoryLoadFailed", "error", g.memoryErr)).FontSize(small).TextColor(p.red)
	case len(entries) == 0:
		ui.Text(c, a.tr("setPersonaMemoryEmpty")).FontSize(small).TextColor(p.ink3)
	}
	now := time.Now()
	for _, e := range entries {
		row := ui.ButtonBase(c.Key("mem-"+e.File.Name)).Padding(6, 8).Margin(0, -8).Radius(rowRadius).Gap(10).Label(e.Title)
		row.Children(func() {
			ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
				ui.Row(c).Gap(8).Children(func() {
					ui.Text(c, e.Title).SingleLine().Shrink(1)
					if l := a.memoryTypeLabel(e.Type); l != "" {
						a.pill(c, l, p.memoryTone(e.Type))
					}
				})
				if e.Detail != "" {
					ui.Text(c, e.Detail).FontSize(small).TextColor(p.ink2).MaxLines(2)
				}
			})
			if e.File.UpdatedAt != "" {
				ui.Text(c, a.ago(e.File.UpdatedAt, now)).FontSize(small).TextColor(p.ink3)
			}
		})
		if row.Hovered() {
			row.Background(p.hover)
		}
		if row.Clicked() {
			x := e
			g.memOpen, g.memIndex, g.memEditing, g.memAsk = &x, false, false, false
		}
	}
	for _, f := range g.memory {
		if f.Name != hubapi.MemoryIndex {
			continue
		}
		a.linkRow(c, p, "mem-index", a.tr("setPersonaIndex"), a.tr("setPersonaIndexHint"), "", func() {
			g.memOpen = &hubapi.MemoryEntry{File: f, Title: a.tr("setPersonaIndex"), Body: f.Content}
			g.memIndex, g.memEditing, g.memAsk = true, false, false
		})
	}
}

func (a *App) memoryDialog(c *ui.Context, p palette) {
	g := &a.sets
	if g.memOpen == nil {
		return
	}
	e := g.memOpen
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(620).Padding(20).Gap(12).Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.Text(c, e.Title).FontSize(heading).Bold().Grow(1).Shrink(1)
				if l := a.memoryTypeLabel(e.Type); l != "" {
					a.pill(c, l, p.memoryTone(e.Type))
				}
			})
			if e.File.UpdatedAt != "" {
				ui.Text(c, a.tr("setPersonaUpdated", "when", a.ago(e.File.UpdatedAt, time.Now()))+" · "+e.File.Name).FontSize(small).TextColor(p.ink3)
			}
			if g.memEditing {
				ui.TextArea(c.Key("mem-edit"), &g.memEdit).Lines(12, 22).Font("monospace").FontSize(small).Label(a.tr("setPersonaEditLabel", "name", e.File.Name))
			} else {
				ui.Scroll(c.Key("mem-body")).MaxHeight(420).Children(func() {
					body := e.Body
					if strings.TrimSpace(body) == "" {
						body = a.tr("setPersonaEmptyBody")
					}
					markdown.View(c.Key("mem-md"), body, &a.md)
				})
			}
			if g.memAsk {
				ui.Text(c, a.tr("setPersonaDeleteConfirm", "title", e.Title)).FontSize(small).TextColor(p.red)
			}
			ui.Row(c).Gap(8).Children(func() {
				if !g.memIndex && !g.memEditing {
					label := a.tr("setPersonaDelete")
					if ui.Button(c, label).TextColor(p.red).Disabled(g.memBusy).Clicked() {
						if g.memAsk {
							a.deleteMemory(e)
						} else {
							g.memAsk = true
						}
					}
				}
				ui.Spacer(c)
				if g.memBusy {
					ui.Spinner(c)
				}
				switch {
				case g.memEditing:
					if ui.Button(c, a.tr("cancel")).Disabled(g.memBusy).Clicked() {
						g.memEditing = false
					}
					if ui.PrimaryButton(c, a.tr("save")).Disabled(g.memBusy || g.memEdit == e.File.Content).Clicked() {
						a.saveMemory(e)
					}
				default:
					if ui.Button(c, a.tr("setPersonaClose")).Clicked() {
						g.memOpen = nil
					}
					if !g.memIndex && ui.PrimaryButton(c, a.tr("setPersonaEdit")).Clicked() {
						g.memEditing, g.memEdit, g.memAsk = true, e.File.Content, false
					}
				}
			})
		})
	})
	if !open && !g.memBusy {
		g.memOpen = nil
	}
}

func (a *App) reloadMemory(cn *conn) {
	g := &a.sets
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	files, err := cn.client.Memory(ctx)
	a.Host.Post(func() {
		if !a.live(cn) {
			return
		}
		if err != nil {
			g.memoryErr = a.errorText(err)
			return
		}
		g.memory, g.memoryErr = files, ""
	})
}

func (a *App) saveMemory(e *hubapi.MemoryEntry) {
	g := &a.sets
	g.memBusy = true
	cn := a.conn
	content := g.memEdit
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := cn.client.SaveMemory(ctx, e.File.Name, content)
		if err == nil {
			a.reloadMemory(cn)
		}
		a.Host.Post(func() {
			g.memBusy = false
			if err != nil {
				a.toast(a.tr("setSaveFailed", "error", a.errorText(err)))
				return
			}
			g.memOpen = nil
			a.toast(a.tr("setPersonaMemorySaved"))
		})
	})
}

func (a *App) deleteMemory(e *hubapi.MemoryEntry) {
	g := &a.sets
	g.memBusy = true
	cn := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := cn.client.DeleteMemory(ctx, e.File.Name)
		if err == nil {
			a.reloadMemory(cn)
		}
		a.Host.Post(func() {
			g.memBusy = false
			if err != nil {
				a.toast(a.tr("setSaveFailed", "error", a.errorText(err)))
				return
			}
			g.memOpen = nil
			a.toast(a.tr("setPersonaDeleted", "title", e.Title))
		})
	})
}

// skillsGroup is the assistant's skills on the computer that runs it, read only.
func (a *App) skillsGroup(c *ui.Context, p palette) {
	g := &a.sets
	footer := ""
	if len(g.skills) > 0 {
		footer = a.tr("setSkillsFooter")
		if g.skillsOn != "" {
			footer = a.tr("setSkillsFooterMachine", "machine", g.skillsOn)
		}
	}
	title := "Skills"
	if len(g.skills) > 0 {
		title += " · " + a.tr("setSkillsCount", "count", itoa(len(g.skills)))
	}
	a.group(c, p, title, footer, func() {
		switch {
		case g.skillErr != "":
			ui.Text(c, a.tr("setSkillsLoadFailed", "error", g.skillErr)).FontSize(small).TextColor(p.ink3)
		case len(g.skills) == 0:
			ui.Text(c, a.tr("setSkillsEmpty")).FontSize(small).TextColor(p.ink3)
		}
		for _, s := range g.skills {
			ui.Column(c.Key("skill-" + s.Name)).Gap(2).Children(func() {
				ui.Row(c).Gap(8).Children(func() {
					ui.Text(c, s.Name).Bold().SingleLine().Grow(1).Shrink(1)
					if s.Source == "builtin" {
						a.pill(c, a.tr("setSkillsBuiltin"), p.ink3)
					} else {
						a.pill(c, a.tr("setSkillsMine"), p.accent)
					}
				})
				if s.Description != "" {
					ui.Text(c, s.Description).FontSize(small).TextColor(p.ink2).MaxLines(3)
				}
			})
		}
	})
}

// ── Agents ──

// agentsSettings is the session triage: what the hub does when an agent stops.
func (a *App) agentsSettings(c *ui.Context, p palette) {
	g := &a.sets
	a.load(&g.triage, func(ctx context.Context, cl *hubapi.Client) (func(), error) {
		t, err := cl.Triage(ctx)
		var he *hubapi.Error
		if asHubError(err, &he) && he.Status == 404 {
			return func() { g.triageNone = true }, nil
		}
		return func() { g.triageSet = t }, err
	})
	title := a.tr("setTriageTitle")
	a.group(c, p, title, a.tr("setTriageFooter", "model", a.tr("setTriageModelPath")), func() {
		if !a.loadState(c, p, &g.triage) {
			return
		}
		if g.triageNone {
			ui.Text(c, a.tr("setTriageUnsupported")).FontSize(small).TextColor(p.ink3)
			return
		}
		t := g.triageSet
		busy := g.triage.busyOn != ""
		flip := func(label, hint string, on *bool, needsMain bool) {
			ui.Column(c.Key("tri-" + label)).Children(func() {
				v := *on
				changed := false
				ui.Row(c).Gap(12).Children(func() {
					ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
						ui.Text(c, label)
						ui.Text(c, hint).FontSize(small).TextColor(p.ink2)
					})
					changed = ui.Switch(c, &v).Label(label).Disabled(busy || needsMain && !t.Enabled).Changed()
				})
				if changed {
					*on = v
					a.saveTriage(t)
				}
			})
		}
		flip(title, a.tr("setTriageEnabledHint"), &t.Enabled, false)
		flip(a.tr("setTriageDecide"), a.tr("setTriageDecideHint"), &t.Decide, true)
		flip(a.tr("setTriageAutoContinue"), a.tr("setTriageAutoContinueHint"), &t.AutoContinue, true)
		flip(a.tr("setTriageWake"), a.tr("setTriageWakeHint"), &t.Wake, true)
		flip(a.tr("setTriageToolLoop"), a.tr("setTriageToolLoopHint"), &t.ToolLoop, true)
		remind := hubapi.On(t.ReplyReminder)
		mins := t.ReminderMins
		if mins == 0 {
			mins = 30
		}
		before := remind
		flip(a.tr("setTriageReplyReminder"), a.tr("setTriageReplyReminderHint", "minutes", itoa(mins)), &remind, false)
		if remind != before {
			t.ReplyReminder = &remind
			a.saveTriage(t)
		}
	})
}

func (a *App) saveTriage(t hubapi.Triage) {
	g := &a.sets
	if g.triage.busyOn != "" {
		return
	}
	before := g.triageSet
	g.triageSet = t
	g.triage.busyOn = "triage"
	cn := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		got, err := cn.client.SetTriage(ctx, t)
		a.Host.Post(func() {
			g.triage.busyOn = ""
			if !a.live(cn) {
				return
			}
			if err != nil {
				g.triageSet = before
				a.toast(a.tr("setSaveFailed", "error", a.errorText(err)))
				return
			}
			g.triageSet = got
		})
	})
}

// ── Model profiles ──

// profilesGroup is the model profiles above the models; a hub without them shows nothing.
func (a *App) profilesGroup(c *ui.Context, p palette) {
	g := &a.sets
	if !g.profilesOK {
		return
	}
	pr := g.profiles
	footer := a.tr("setProfilesFooter")
	if pr.Active == "" {
		footer += " " + a.tr("setProfilesNoneActive")
	}
	a.group(c, p, a.tr("setProfilesTitle"), footer, func() {
		for _, x := range pr.Profiles {
			value := ""
			if x.Active {
				value = a.tr("setProfilesActive")
			}
			id := x.ID
			a.linkRow(c, p, "profile-"+id, x.Name, x.Description, value, func() { g.profileOpen, g.profileErr = id, "" })
		}
		if rb := pr.Rollback; rb != nil {
			to := rb.To
			if to == "" {
				to = a.tr("setProfilesCustom")
			}
			ui.Row(c).Gap(12).Children(func() {
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, a.tr("setProfilesRollback"))
					ui.Text(c, to).FontSize(small).TextColor(p.ink2)
				})
				if ui.Button(c, a.tr("setProfilesRollback")).Disabled(g.profileBusy).Clicked() {
					a.applyProfile("")
				}
			})
		}
	})
	if g.profileErr != "" && g.profileOpen == "" {
		ui.Text(c, g.profileErr).FontSize(small).TextColor(p.red)
	}
	a.profileDialog(c, p)
}

func (a *App) loadProfiles() {
	g := &a.sets
	cn := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		pr, err := cn.client.ModelProfiles(ctx)
		a.Host.Post(func() {
			if a.live(cn) && err == nil {
				g.profiles, g.profilesOK = pr, true
			}
		})
	})
}

func (a *App) profileDialog(c *ui.Context, p palette) {
	g := &a.sets
	var x *hubapi.ModelProfile
	for i := range g.profiles.Profiles {
		if g.profiles.Profiles[i].ID == g.profileOpen {
			x = &g.profiles.Profiles[i]
		}
	}
	if x == nil {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(520).Padding(20).Gap(10).Children(func() {
			ui.Text(c, x.Name).FontSize(heading).Bold()
			if x.Description != "" {
				ui.Text(c, x.Description).FontSize(small).TextColor(p.ink2)
			}
			ui.Text(c, a.tr("setProfilesChanges", "count", itoa(len(x.Changes)))).FontSize(small).TextColor(p.ink3)
			ui.Text(c, a.tr("setProfilesTiers")).FontSize(small).Bold().TextColor(p.ink2)
			names := make([]string, 0, len(x.Tiers))
			for n := range x.Tiers {
				names = append(names, n)
			}
			sort.Strings(names)
			if len(names) == 0 {
				ui.Text(c, a.tr("setProfilesNoTiers")).FontSize(small).TextColor(p.ink3)
			}
			for _, n := range names {
				t := x.Tiers[n]
				ui.Column(c.Key("tier-"+n)).Padding(8, 12).Radius(rowRadius).Background(p.field).Gap(2).Children(func() {
					var parts []string
					for _, s := range []string{t.Launcher, t.Model, t.Effort} {
						if s != "" {
							parts = append(parts, s)
						}
					}
					ui.Row(c).Gap(8).Children(func() {
						ui.Text(c, n).FontSize(small).Font("monospace")
						ui.Text(c, strings.Join(parts, " · ")).FontSize(small).TextColor(p.ink3)
					})
					if t.Description != "" {
						ui.Text(c, t.Description).FontSize(small).TextColor(p.ink2)
					}
				})
			}
			if g.profileErr != "" {
				ui.Text(c, g.profileErr).FontSize(small).TextColor(p.red)
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if g.profileBusy {
					ui.Spinner(c)
				}
				if ui.Button(c, a.tr("cancel")).Disabled(g.profileBusy).Clicked() {
					g.profileOpen = ""
				}
				if ui.PrimaryButton(c, a.tr("setProfilesApply")).Disabled(g.profileBusy).Clicked() {
					a.applyProfile(x.ID)
				}
			})
		})
	})
	if !open && !g.profileBusy {
		g.profileOpen = ""
	}
}

func (a *App) applyProfile(id string) {
	g := &a.sets
	g.profileBusy, g.profileErr = true, ""
	cn := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pr, err := cn.client.ApplyModelProfile(ctx, id)
		a.Host.Post(func() {
			g.profileBusy = false
			if !a.live(cn) {
				return
			}
			if err != nil {
				g.profileErr = a.errorText(err)
				return
			}
			g.profiles, g.profileOpen = pr, ""
			g.models.asked = false
		})
	})
}

// ── Appearance ──

// appearanceRow chooses light, dark or the system's.
func (a *App) appearanceRow(c *ui.Context, p palette) {
	s := &a.Hubs.Settings
	i := 0
	switch s.Appearance {
	case "light":
		i = 1
	case "dark":
		i = 2
	}
	ui.Row(c).Gap(12).Children(func() {
		ui.Text(c, a.tr("settingAppearance")).Grow(1)
		if ui.Segmented(c.Key("appearance"), &i, a.tr("appearanceSystem"), a.tr("appearanceLight"), a.tr("appearanceDark")).Changed() {
			s.Appearance = []string{"", "light", "dark"}[i]
			_ = a.Hubs.Save()
		}
	})
}
