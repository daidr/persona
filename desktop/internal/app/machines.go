package app

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// machinesView lists the computers: online or not, their system, and what runs there.
func (a *App) machinesView(c *ui.Context, p palette) {
	d := a.conn.data
	a.pageHeader(c, p, a.tr("goMachines"), a.tr("machinesSubtitle"), nil)
	ui.Divider(c)
	if len(d.Machines) == 0 {
		if !a.conn.loaded {
			ui.Spinner(c).Margin(ui.Auto)
			return
		}
		a.empty(c, p, "machine", a.tr("machinesEmpty"), a.tr("machinesEmptyHint"))
		return
	}
	now := time.Now()
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Row(c).Wrap().AlignItems(ui.Start).Gap(14).Padding(16, pagePad).Children(func() {
			for _, m := range d.Machines {
				a.machineCard(c.Key(m.ID), p, m, len(d.Sessions[m.ID]), now)
			}
		})
	})
}

func (a *App) machineCard(c *ui.Context, p palette, m hubapi.Machine, agents int, now time.Time) {
	a.card(c, p, func() {
		ui.Row(c).Gap(10).Children(func() {
			ui.Icon(c, icon("machine")).FontSize(20).TextColor(p.ink2)
			ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
				ui.Text(c, nameOf(m)).Bold().SingleLine()
				if m.Hostname != "" && m.Hostname != m.Name {
					ui.Text(c, m.Hostname).FontSize(small).TextColor(p.ink2).SingleLine()
				}
			})
			dot, text := p.green, a.tr("online")
			if !m.Online {
				dot, text = p.ink3, a.tr("offline")
			}
			ui.Row(c).Gap(6).Children(func() {
				ui.Box(c).Size(8, 8).Radius(4).Background(dot)
				ui.Text(c, text).FontSize(small).TextColor(p.ink2)
			})
		})
		if m.System != nil && m.System.Name != "" {
			a.fact(c, p, a.tr("machineSystem"), m.System.Name)
		} else if m.OS != "" {
			a.fact(c, p, a.tr("machineSystem"), m.OS+" "+m.Arch)
		}
		if m.System != nil && m.System.CPU != "" {
			cpu := m.System.CPU
			if m.System.Cores > 0 {
				cpu += fmt.Sprintf(" · %d", m.System.Cores)
			}
			a.fact(c, p, a.tr("machineCPU"), cpu)
		}
		if m.System != nil && m.System.Memory > 0 {
			a.fact(c, p, a.tr("machineMemory"), fmt.Sprintf("%d GB", (m.System.Memory+(1<<29))>>30))
		}
		a.fact(c, p, a.tr("goAgents"), itoa(agents))
		if m.ClaudeVer != "" {
			a.fact(c, p, "Claude Code", m.ClaudeVer)
		}
		if !m.Online && m.LastSeen != "" {
			a.fact(c, p, a.tr("machineLastSeen"), a.ago(m.LastSeen, now))
		}
		if m.Error != "" {
			ui.Text(c, m.Error).FontSize(small).TextColor(p.red).MaxLines(3)
		}
	}).Width(300)
}

func nameOf(m hubapi.Machine) string {
	if m.Name != "" {
		return m.Name
	}
	return m.ID
}

func (a *App) fact(c *ui.Context, p palette, k, v string) {
	ui.Row(c).Gap(8).Children(func() {
		ui.Text(c, k).FontSize(small).TextColor(p.ink2).Width(96).SingleLine()
		ui.Text(c, v).FontSize(small).SingleLine().Grow(1).Shrink(1)
	})
}
