package app

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// panesEvery is how often the Terminal page reads the computers' panes again.
const panesEvery = 8 * time.Second

// terminalsState is the Terminal page: every computer's terminal panes (agents and plain shells) as computer →
// workspace → pane, and the one open beside the list.
type terminalsState struct {
	hosts map[string][]hubapi.PaneHost
	errs  map[string]string
	at    time.Time
	busy  int
	query string
}

func (a *App) panesOn() bool {
	return a.conn != nil && slices.Contains(a.conn.data.Hello.Features, hubapi.FeatureTerminalPanes)
}

func (a *App) readPanes(force bool) {
	g := &a.terms10
	if g.busy > 0 || (!force && !g.at.IsZero() && time.Since(g.at) < panesEvery) {
		return
	}
	if g.hosts == nil {
		g.hosts, g.errs = map[string][]hubapi.PaneHost{}, map[string]string{}
	}
	cn := a.conn
	g.at = time.Now()
	for _, m := range cn.data.Machines {
		if !m.Online {
			continue
		}
		g.busy++
		mid := m.ID
		a.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			hosts, err := cn.client.Panes(ctx, mid)
			a.Host.Post(func() {
				g.busy--
				if !a.live(cn) {
					return
				}
				if err != nil {
					g.errs[mid] = a.errorText(err)
				} else {
					g.hosts[mid], g.errs[mid] = hosts, ""
				}
				if g.busy == 0 {
					a.After(panesEvery, func() {})
				}
			})
		})
	}
}

// terminalsView is the Terminal page; open is "<machine>/<pane address or session>".
func (a *App) terminalsView(c *ui.Context, p palette, open string) {
	g := &a.terms10
	if !a.panesOn() {
		a.empty(c, p, "machine", a.tr("termsUnsupportedTitle"), a.tr("termsUnsupportedBody"))
		return
	}
	a.readPanes(false)
	ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(320).BorderWidth(0, 1, 0, 0).BorderColor(p.line).Children(func() {
			ui.Row(c).Padding(16, 16, 8, 16).Gap(8).Children(func() {
				ui.Text(c, a.tr("termsTitle")).FontSize(title).Bold().Grow(1)
				if ui.Button(c, a.tr("msgRefresh")).Disabled(g.busy > 0).Clicked() {
					a.readPanes(true)
				}
			})
			ui.SearchField(c.Key("terms-search"), &g.query).Placeholder(a.tr("termsSearch")).Margin(0, 16, 8, 16)
			ui.Scroll(c.Key("terms-list")).Grow(1).Children(func() {
				ui.Column(c).Padding(0, 8, 12, 8).Gap(2).Children(func() { a.paneTree(c, p, open) })
			})
		})
		ui.Column(c).Grow(1).Children(func() {
			if open == "" {
				a.empty(c, p, "machine", a.tr("termsPickTitle"), a.tr("termsPickHint"))
				return
			}
			mid, id, _ := strings.Cut(open, "/")
			title := id
			for _, h := range g.hosts[mid] {
				for _, pn := range h.Panes {
					if pn.Terminal == id || pn.SessionID == id {
						title = paneTitle(pn)
					}
				}
			}
			sub := a.conn.data.MachineName(mid)
			x, known := a.conn.data.Session(mid + "/" + id)
			if known {
				title = titleOf(x)
			}
			a.pageHeader(c, p, title, sub, func() {
				if known && ui.Button(c, a.tr("termsOpenAgent")).Clicked() {
					a.Navigate("/agents/" + mid + "/" + id)
				}
			})
			ui.Divider(c)
			a.terminalPane(c, p, open, x)
		})
	})
}

func paneTitle(pn hubapi.Pane) string {
	if t := strings.TrimSpace(pn.Title); t != "" {
		return t
	}
	return pn.Pane
}

func (a *App) paneTree(c *ui.Context, p palette, open string) {
	g := &a.terms10
	d := a.conn.data
	q := strings.ToLower(strings.TrimSpace(g.query))
	shown := false
	for _, m := range d.Machines {
		if !m.Online {
			continue
		}
		hosts := g.hosts[m.ID]
		if err := g.errs[m.ID]; err != "" && len(hosts) == 0 {
			ui.Text(c.Key("me-"+m.ID), d.MachineName(m.ID)+" · "+err).FontSize(small).TextColor(p.red).Padding(6, 8)
			continue
		}
		for hi, h := range hosts {
			for _, ws := range h.Workspaces {
				var panes []hubapi.Pane
				for _, pn := range h.Panes {
					if pn.WorkspaceID != ws.ID {
						continue
					}
					hay := strings.ToLower(m.Name + " " + ws.Label + " " + paneTitle(pn) + " " + pn.Cwd + " " + pn.Agent)
					if q == "" || strings.Contains(hay, q) {
						panes = append(panes, pn)
					}
				}
				if len(panes) == 0 {
					continue
				}
				shown = true
				ui.Row(c.Key("ws-"+m.ID+itoa(hi)+ws.ID)).Padding(10, 8, 2, 8).Gap(6).Children(func() {
					ui.Text(c, ws.Label).FontSize(small).Bold().TextColor(p.ink2).SingleLine().Shrink(1)
					line := d.MachineName(m.ID)
					if ws.Branch != "" {
						line += " · " + ws.Branch
					}
					ui.Text(c, line).FontSize(11).TextColor(p.ink3).SingleLine()
				})
				for _, pn := range panes {
					id := pn.Terminal
					if pn.SessionID != "" {
						id = pn.SessionID
					}
					a.paneRow(c.Key("pn-"+m.ID+"/"+id), p, m.ID+"/"+id, pn, open)
				}
			}
		}
	}
	if !shown {
		if g.busy > 0 && len(g.hosts) == 0 {
			ui.Spinner(c).Margin(24, ui.Auto)
		} else {
			ui.Text(c, a.tr("termsEmpty")).FontSize(small).TextColor(p.ink3).Padding(12, 8)
		}
	}
}

func (a *App) paneRow(c *ui.Context, p palette, key string, pn hubapi.Pane, open string) {
	btn := ui.ButtonBase(c).Label(paneTitle(pn)).Padding(6, 10).Radius(rowRadius)
	switch {
	case key == open:
		btn.Background(p.hover)
	case btn.Hovered():
		btn.Background(p.hover)
	}
	btn.Children(func() {
		ui.Row(c).Gap(8).Grow(1).Children(func() {
			ic := "machine"
			if pn.Agent != "" {
				ic = "agent"
			}
			ui.Icon(c, icon(ic)).TextColor(p.ink3)
			ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
				ui.Text(c, paneTitle(pn)).SingleLine()
				detail := pn.Cwd
				if pn.Agent != "" {
					detail = joinDot(pn.Agent, pn.Cwd)
				}
				if detail != "" {
					ui.Text(c, detail).FontSize(11).TextColor(p.ink3).SingleLine()
				}
			})
			if pn.SessionID != "" {
				if x, ok := a.conn.data.Session(strings.SplitN(key, "/", 2)[0] + "/" + pn.SessionID); ok {
					st := a.conn.data.StateOf(x)
					ui.Box(c).Size(7, 7).Radius(4).Background(p.stateColor(st))
				}
			}
		})
	})
	if btn.Clicked() {
		a.Navigate("/terminal/" + key)
	}
}
