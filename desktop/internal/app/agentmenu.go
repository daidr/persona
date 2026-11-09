package app

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// agentAction is the dialog an agent's menu opened: rename, session info, close or remove, for one agent.
type agentAction struct {
	open  bool
	kind  string // rename | info | close | delete
	key   string
	title string
	info  *hubapi.SessionInfo
	busy  bool
	err   string
}

// agentMenu is the "…" button of a chat's header: Pin, Rename, Session info, Copy session ID, End and Remove.
// persona is an assistant chat, which isn't pinned or removed (archiving takes it out of the list).
func (a *App) agentMenu(c *ui.Context, p palette, x hubapi.Session, persona bool) {
	d := a.conn.data
	key := x.Key()
	more := ui.ButtonBase(c.Key("more-" + key)).Label(a.tr("agentMore")).Tooltip(a.tr("agentMore")).Padding(6).Radius(rowRadius)
	if more.Hovered() {
		more.Background(p.hover)
	}
	more.Children(func() { ui.Icon(c, icon("more")).TextColor(p.ink2) })
	more.Menu(func(m *ui.Menu) {
		if !persona && d.PinsLoaded {
			label := a.tr("agentPin")
			if d.Pinned(key) {
				label = a.tr("agentUnpin")
			}
			if m.Item(label).Chosen() {
				a.togglePin(key)
			}
		}
		if m.Item(a.tr("agentRename")).Chosen() {
			a.act = agentAction{open: true, kind: "rename", key: key, title: x.Title}
		}
		if m.Item(a.tr("agentInfo")).Chosen() {
			a.openInfo(x)
		}
		if m.Item(a.tr("agentCopyID")).Chosen() {
			c.WriteClipboard(x.SessionID)
			a.toast(a.tr("agentCopied"))
		}
		m.Separator()
		if m.Item(a.tr("agentEnd")).Disabled(x.Status == "offline" || x.State == hubapi.StateOffline).Chosen() {
			a.act = agentAction{open: true, kind: "close", key: key, title: titleOf(x)}
		}
		if !persona && m.Item(a.tr("agentRemove")).Chosen() {
			a.act = agentAction{open: true, kind: "delete", key: key, title: titleOf(x)}
		}
	})
}

func (a *App) togglePin(key string) {
	cn := a.conn
	pins := cn.data.Pins
	pins.Sessions = slices.Clone(pins.Sessions)
	if i := slices.Index(pins.Sessions, key); i >= 0 {
		pins.Sessions = slices.Delete(pins.Sessions, i, i+1)
	} else {
		pins.Sessions = append(pins.Sessions, key)
	}
	before := cn.data.Pins
	cn.data.Pins = pins
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := cn.client.SetPins(ctx, pins)
		a.Host.Post(func() {
			if err != nil && a.live(cn) {
				cn.data.Pins = before
				a.toast(a.errorText(err))
			}
		})
	})
}

func (a *App) openInfo(x hubapi.Session) {
	a.act = agentAction{open: true, kind: "info", key: x.Key(), title: titleOf(x), busy: true}
	cn, key := a.conn, x.Key()
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err := cn.client.SessionInfo(ctx, x.MachineID, x.SessionID)
		a.Host.Post(func() {
			if !a.live(cn) || a.act.key != key || a.act.kind != "info" {
				return
			}
			a.act.busy = false
			if err != nil {
				a.act.err = a.errorText(err)
				return
			}
			a.act.info = &info
		})
	})
}

// agentDialog draws the dialog the menu opened.
func (a *App) agentDialog(c *ui.Context, p palette) {
	g := &a.act
	if !g.open {
		return
	}
	ui.Modal(c, &g.open, func() {
		ui.Column(c).Width(480).Padding(20).Gap(12).Children(func() {
			switch g.kind {
			case "rename":
				ui.Text(c, a.tr("agentRename")).FontSize(heading).Bold()
				ui.TextInput(c.Key("rename-title"), &g.title).Label(a.tr("agentRenameTitle")).Placeholder(a.tr("agentRenameTitle")).AutoFocus().
					OnTextKey(func(k ui.TextKey) bool {
						if k.Key == ui.KeyEnter && k.Mods == 0 {
							a.renameAgent()
							return true
						}
						return false
					})
			case "info":
				ui.Text(c, a.tr("agentInfo")).FontSize(heading).Bold()
				ui.Text(c, g.title).TextColor(p.ink2).SingleLine()
				switch {
				case g.busy:
					ui.Spinner(c).Margin(12, ui.Auto)
				case g.info != nil:
					a.infoRows(c, p, *g.info)
				}
			case "close":
				ui.Text(c, a.tr("agentEndTitle", "title", g.title)).FontSize(heading).Bold()
				ui.Text(c, a.tr("agentEndBody")).TextColor(p.ink2)
			case "delete":
				ui.Text(c, a.tr("agentRemoveTitle", "title", g.title)).FontSize(heading).Bold()
				ui.Text(c, a.tr("agentRemoveBody")).TextColor(p.ink2)
			}
			if g.err != "" {
				ui.Text(c, g.err).FontSize(small).TextColor(p.red)
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if g.kind == "info" {
					if ui.Button(c, a.tr("done")).Clicked() {
						g.open = false
					}
					return
				}
				if ui.Button(c, a.tr("cancel")).Clicked() {
					g.open = false
				}
				switch g.kind {
				case "rename":
					if ui.PrimaryButton(c, a.tr("save")).Disabled(g.busy || strings.TrimSpace(g.title) == "").Clicked() {
						a.renameAgent()
					}
				case "close":
					if ui.PrimaryButton(c, a.tr("agentEnd")).Disabled(g.busy).Clicked() {
						a.agentDo(func(ctx context.Context, cl *hubapi.Client, m, s string) error { return cl.CloseSession(ctx, m, s) }, nil)
					}
				case "delete":
					if ui.PrimaryButton(c, a.tr("agentRemove")).Disabled(g.busy).Clicked() {
						key := g.key
						a.agentDo(func(ctx context.Context, cl *hubapi.Client, m, s string) error { return cl.DeleteSession(ctx, m, s) }, func() {
							a.conn.data.DropSession(key)
							if a.conn.data.Pinned(key) {
								a.togglePin(key)
							}
							a.Navigate("/agents")
						})
					}
				}
			})
		})
	})
}

func (a *App) infoRows(c *ui.Context, p palette, in hubapi.SessionInfo) {
	rows := [][2]string{
		{a.tr("infoSession"), in.SessionID}, {a.tr("infoLauncher"), in.Launcher}, {a.tr("infoModel"), in.Model},
		{a.tr("infoEffort"), in.Effort}, {a.tr("infoMode"), in.PermissionMode}, {a.tr("infoFolder"), in.Cwd},
		{a.tr("infoBranch"), in.GitBranch}, {a.tr("infoVersion"), in.Version}, {a.tr("infoTranscript"), in.TranscriptPath},
	}
	if in.Turns > 0 {
		rows = append(rows, [2]string{a.tr("infoTurns"), itoa(in.Turns)})
	}
	if in.ContextTokens > 0 {
		rows = append(rows, [2]string{a.tr("infoContext"), itoa(in.ContextTokens)})
	}
	if in.Compactions > 0 {
		rows = append(rows, [2]string{a.tr("infoCompactions"), itoa(in.Compactions)})
	}
	ui.Column(c).Gap(6).Children(func() {
		for _, r := range rows {
			if r[1] == "" {
				continue
			}
			ui.Row(c.Key("info-" + r[0])).Gap(12).AlignItems(ui.Start).Children(func() {
				ui.Text(c, r[0]).FontSize(small).TextColor(p.ink2).Width(110).SingleLine()
				ui.Text(c, r[1]).FontSize(small).Selectable().Grow(1).Shrink(1)
			})
		}
	})
}

func (a *App) renameAgent() {
	g := &a.act
	title := strings.TrimSpace(g.title)
	if title == "" || g.busy {
		return
	}
	key := g.key
	a.agentDo(func(ctx context.Context, cl *hubapi.Client, m, s string) error { return cl.Rename(ctx, m, s, title) }, func() {
		d := a.conn.data
		if x, ok := d.Session(key); ok {
			x.Title = title
			d.PutSession(x)
		}
		for i := range d.Assistant.Conversations {
			if x := &d.Assistant.Conversations[i]; x.MachineID+"/"+x.SessionID == key || d.Assistant.MachineID+"/"+x.SessionID == key {
				x.Title = title
			}
		}
	})
}

// agentDo runs the dialog's call for its agent, then after on the main thread and closes the dialog; an error stays in it.
func (a *App) agentDo(do func(ctx context.Context, cl *hubapi.Client, machine, session string) error, after func()) {
	g := &a.act
	cn, key := a.conn, g.key
	machine, session, _ := strings.Cut(key, "/")
	g.busy, g.err = true, ""
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := do(ctx, cn.client, machine, session)
		a.Host.Post(func() {
			if !a.live(cn) || g.key != key {
				return
			}
			g.busy = false
			if err != nil {
				g.err = a.errorText(err)
				return
			}
			g.open = false
			if after != nil {
				after()
			}
		})
	})
}
