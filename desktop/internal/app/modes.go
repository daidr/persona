package app

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// The chips under a Claude Code agent's composer, as its own bottom bar: the permission mode, the model and the effort
// with ultracode, each changed in place (POST …/modes) and put back when the hub refuses.

// modelLists are the launchers' models by "machine/launcher", read once.
type modelList struct {
	list    hubapi.LauncherModels
	loaded  bool
	loading bool
}

// hasModes tells an agent the chips can change: Claude Code's (or an agent that doesn't say), not the assistant's
// conversations and not another source's.
func hasModes(x hubapi.Session) bool {
	return !x.Assistant && x.SourceKind == "" && (x.Agent == "" || x.Agent == "claude") && x.Can("send")
}

func (a *App) modeLabel(mode string) string {
	switch mode {
	case "acceptEdits":
		return a.tr("modeAcceptEdits")
	case "plan":
		return a.tr("modePlan")
	case "auto":
		return a.tr("modeAuto")
	case "bypassPermissions":
		return a.tr("modeBypass")
	}
	return a.tr("modeDefault")
}

func (a *App) effortLabel(effort string) string {
	switch effort {
	case "low":
		return a.tr("effortLow")
	case "medium":
		return a.tr("effortMedium")
	case "high":
		return a.tr("effortHigh")
	case "xhigh":
		return a.tr("effortXhigh")
	case "max":
		return a.tr("effortMax")
	}
	return a.tr("effortDefault")
}

func (a *App) models(machine, launcher string) *modelList {
	if launcher == "" {
		launcher = "claude"
	}
	if a.modelLists == nil {
		a.modelLists = map[string]*modelList{}
	}
	k := machine + "/" + launcher
	m := a.modelLists[k]
	if m == nil {
		m = &modelList{}
		a.modelLists[k] = m
	}
	if m.loaded || m.loading {
		return m
	}
	m.loading = true
	c := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		list, err := c.client.LauncherModels(ctx, machine, launcher)
		a.Host.Post(func() {
			m.loading, m.loaded = false, true
			if err == nil {
				m.list = list
			}
		})
	})
	return m
}

// setModes changes the agent at once and sends it; a refusal puts it back.
func (a *App) setModes(x hubapi.Session, patch map[string]any) {
	d := a.conn.data
	before := x
	next := x
	for k, v := range patch {
		switch k {
		case "permission_mode":
			next.PermissionMode, _ = v.(string)
		case "effort":
			next.Effort, _ = v.(string)
		case "model":
			next.Model, _ = v.(string)
		case "ultracode":
			next.Ultracode, _ = v.(bool)
		}
	}
	d.PutSession(next)
	c := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := c.client.SetModes(ctx, x.MachineID, x.SessionID, patch)
		a.Host.Post(func() {
			if err == nil || !a.live(c) {
				return
			}
			if cur, ok := c.data.Session(x.Key()); ok {
				cur.PermissionMode, cur.Effort, cur.Model, cur.Ultracode = before.PermissionMode, before.Effort, before.Model, before.Ultracode
				c.data.PutSession(cur)
			}
			a.toast(a.tr("modesFailed", "error", a.errorText(err)))
		})
	})
}

func (a *App) chip(c *ui.Context, p palette, id, label string, danger bool, build func(m *ui.Menu)) {
	chip := ui.ButtonBase(c.Key(id)).Padding(3, 10).Radius(999).Gap(4).Label(label)
	if chip.Hovered() {
		chip.Background(p.hover)
	}
	chip.Children(func() {
		col := p.ink2
		if danger {
			col = p.red
		}
		ui.Text(c, label).FontSize(small).TextColor(col).SingleLine()
		ui.Text(c, "▾").FontSize(9).TextColor(p.ink3)
	})
	chip.Menu(build)
}

// modeChips are the chips under the composer.
func (a *App) modeChips(c *ui.Context, p palette, x hubapi.Session) {
	key := x.Key()
	busy := a.conn.data.StateOf(x) == hubapi.StateWorking || a.conn.data.StateOf(x) == hubapi.StateNeedsYou
	ml := a.models(x.MachineID, x.Launcher)
	ui.Row(c).Margin(0, pagePad, 10, pagePad).Gap(4).Children(func() {
		mode := x.PermissionMode
		if mode == "" {
			mode = "default"
		}
		a.chip(c, p, "mode-"+key, a.modeLabel(mode), mode == "bypassPermissions", func(m *ui.Menu) {
			for _, id := range hubapi.PermissionModes {
				if m.Item(a.modeLabel(id)).Radio(id == mode).Chosen() && id != mode {
					a.setModes(x, map[string]any{"permission_mode": id})
				}
			}
		})
		if models := ml.list.Models; len(models) > 0 {
			current := x.Model
			label := a.tr("modelDefault")
			for _, md := range models {
				if isCurrentModel(md, current) {
					label = md.Name
				}
			}
			if label == a.tr("modelDefault") && current != "" {
				label = current
			}
			a.chip(c, p, "model-"+key, label, false, func(m *ui.Menu) {
				for _, md := range models {
					it := m.Item(md.Name).Radio(isCurrentModel(md, current) || current == "" && md.Default).Disabled(busy || ml.list.Locked != "")
					if md.Description != "" {
						it.Subtitle(md.Description)
					}
					if it.Chosen() {
						a.setModes(x, map[string]any{"model": md.ID})
					}
				}
			})
		}
		effort := x.Effort
		label := a.effortLabel(effort)
		if x.Ultracode {
			label += " · " + a.tr("ultracode")
		}
		a.chip(c, p, "effort-"+key, label, false, func(m *ui.Menu) {
			for _, id := range hubapi.Efforts {
				if m.Item(a.effortLabel(id)).Radio(id == effort).Chosen() && id != effort {
					a.setModes(x, map[string]any{"effort": id})
				}
			}
			m.Separator()
			if m.Item(a.tr("ultracode")).Checked(x.Ultracode).Subtitle(a.tr("ultracodeHint")).Chosen() {
				a.setModes(x, map[string]any{"ultracode": !x.Ultracode})
			}
		})
		if busy {
			ui.Text(c, a.tr("modesBusy")).FontSize(11).TextColor(p.ink3).SingleLine()
		}
	})
}

// isCurrentModel tells the model the agent runs: by its id or what its alias stands for.
func isCurrentModel(m hubapi.AgentModel, current string) bool {
	if current == "" {
		return false
	}
	return slices.ContainsFunc([]string{m.ID, m.Resolved}, func(v string) bool { return v != "" && strings.EqualFold(v, current) })
}
