package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// boardEvery is how often the open board is read again.
const boardEvery = 10 * time.Second

// boardState is the workflow board (docs/web/workspace-board.md): workspaces as cards in columns the user arranges
// by hand. It is on or off for the hub (Settings › Agents); off, nothing about it is read.
type boardState struct {
	setting     hubapi.BoardSetting
	settingRead bool
	settingBusy bool

	board    *hubapi.Board
	found    hubapi.WorkspaceList
	foundErr string
	at       time.Time
	reading  bool
	busy     bool
	readonly bool
	err      string
	loaded   bool

	query    string
	machine  string // the filter's label
	archived bool

	edit     *boardEdit
	deleting *hubapi.BoardColumn
	moveTo   string // the delete's target column title
	detail   string // the card open
	bind     *boardBind
}

// boardEdit is the name being typed for a new column, or for a column or card being renamed.
type boardEdit struct {
	card bool
	id   string
	name string
}

// boardBind is Add workspace: one the computers offer, or a folder on an online one.
type boardBind struct {
	column   string
	mode     int // 0 existing, 1 folder
	query    string
	machine  string // label
	path     string
	dirs     *hubapi.Dirs
	dirsErr  string
	dirsBusy bool
}

// readBoardSetting reads whether the board is on, once per connection.
func (a *App) readBoardSetting() {
	g := &a.board
	if g.settingRead || g.settingBusy {
		return
	}
	cn := a.conn
	g.settingBusy = true
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		s, err := cn.client.WorkspaceBoardSetting(ctx)
		a.Host.Post(func() {
			if !a.live(cn) {
				return
			}
			g.settingBusy, g.settingRead = false, true
			if err == nil {
				g.setting = s
			}
		})
	})
}

func (a *App) setBoardOn(on bool) {
	g := &a.board
	cn := a.conn
	g.settingBusy = true
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		s, err := cn.client.SetWorkspaceBoard(ctx, on)
		a.Host.Post(func() {
			g.settingBusy = false
			if !a.live(cn) {
				return
			}
			if err != nil {
				a.toast(a.errorText(err))
				return
			}
			g.setting = s
		})
	})
}

// boardSettingGroup is the switch in Settings › Agents.
func (a *App) boardSettingGroup(c *ui.Context, p palette) {
	g := &a.board
	a.readBoardSetting()
	if !g.settingRead {
		return
	}
	footer := a.tr("wbSetSettingHint")
	if !g.setting.CanWrite {
		footer = a.tr("wbSetSettingReadonly")
	}
	a.group(c, p, a.tr("wbSetTitle"), footer, func() {
		on := g.setting.Enabled
		ui.Row(c).Gap(12).Children(func() {
			ui.Text(c, a.tr("wbSetSettingLabel")).Grow(1)
			if ui.Switch(c.Key("board-on"), &on).Label(a.tr("wbSetSettingLabel")).Disabled(g.settingBusy || !g.setting.CanWrite).Changed() {
				a.setBoardOn(on)
			}
		})
	})
}

// boardError is why the hub refused the board, in the page's words.
func (a *App) boardError(err error) string {
	var he *hubapi.Error
	if errors.As(err, &he) {
		switch he.Status {
		case http.StatusForbidden:
			return a.tr("wbLibReadOnly")
		case http.StatusConflict:
			return a.tr("wbLibConflictReloading")
		case http.StatusNotFound, http.StatusServiceUnavailable:
			return a.tr("wbLibUnsupported")
		}
	}
	return a.errorText(err)
}

func forbidden(err error) bool {
	var he *hubapi.Error
	return errors.As(err, &he) && he.Status == http.StatusForbidden
}

// readBoard reads the board and what the computers offer; a timer wakes a frame for the next read.
func (a *App) readBoard(force bool) {
	g := &a.board
	if g.reading || g.busy || (!force && !g.at.IsZero() && time.Since(g.at) < boardEvery) {
		return
	}
	cn := a.conn
	g.reading, g.at = true, time.Now()
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		b, err := cn.client.WorkspaceBoard(ctx)
		found, ferr := cn.client.Workspaces(ctx)
		a.Host.Post(func() {
			if !a.live(cn) {
				return
			}
			g.reading, g.loaded = false, true
			switch {
			case err != nil:
				g.err = a.boardError(err)
				g.readonly = g.readonly || forbidden(err)
			case !g.busy && (g.board == nil || b.Revision >= g.board.Revision):
				g.board, g.err = &b, ""
				if b.CanWrite != nil {
					g.readonly = !*b.CanWrite
				}
			}
			if ferr != nil {
				g.foundErr = a.errorText(ferr)
			} else {
				g.found, g.foundErr = found, ""
			}
			a.After(boardEvery, func() {})
		})
	})
}

// changeBoard makes one change; a conflict reads the board again and says so.
func (a *App) changeBoard(change map[string]any, done func()) {
	g := &a.board
	if g.board == nil || g.busy || g.readonly {
		return
	}
	cn, rev := a.conn, g.board.Revision
	g.busy, g.err = true, ""
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		b, err := cn.client.ChangeBoard(ctx, rev, change)
		a.Host.Post(func() {
			g.busy = false
			if !a.live(cn) {
				return
			}
			if err != nil {
				var he *hubapi.Error
				if errors.As(err, &he) && he.Status == http.StatusConflict {
					g.err = a.tr("wbLibConflictReloaded")
					a.readBoard(true)
					return
				}
				g.err = a.boardError(err)
				g.readonly = g.readonly || forbidden(err)
				a.toast(g.err)
				return
			}
			g.board = &b
			if b.CanWrite != nil {
				g.readonly = !*b.CanWrite
			}
			if done != nil {
				done()
			}
		})
	})
}

func (a *App) machineLabels() (labels, ids []string) {
	for _, m := range a.conn.data.Machines {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		labels, ids = append(labels, name), append(ids, m.ID)
	}
	return labels, ids
}

func (a *App) boardView(c *ui.Context, p palette) {
	g := &a.board
	a.readBoardSetting()
	if !g.settingRead {
		ui.Spinner(c).Margin(ui.Auto)
		return
	}
	if !g.setting.Enabled {
		ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
			a.empty(c, p, "layers", a.tr("wbSetOffTitle"), a.tr("wbSetOffBody"))
			if ui.PrimaryButton(c, a.tr("wbSetOpenSettings")).Clicked() {
				a.sets.tab = 2
				a.Navigate("/settings")
			}
		})
		return
	}
	a.readBoard(false)
	a.pageHeader(c, p, a.tr("wbTitle"), a.tr("wbSubtitle"), func() {
		if ui.Button(c, a.tr("wbAddColumn")).Disabled(g.board == nil || g.busy || g.readonly).Clicked() {
			g.edit = &boardEdit{}
		}
	})
	labels, ids := a.machineLabels()
	if g.machine == "" {
		g.machine = a.tr("wbAllMachines")
	}
	ui.Row(c).Padding(0, pagePad, 10, pagePad).Gap(8).Children(func() {
		ui.SearchField(c.Key("board-search"), &g.query).Placeholder(a.tr("wbSearchPlaceholder")).Grow(1)
		ui.Select(c.Key("board-machine"), &g.machine, append([]string{a.tr("wbAllMachines")}, labels...)).Width(180)
		label := a.tr("wbArchived")
		if g.archived {
			label = a.tr("wbBackToBoard")
		}
		if ui.Button(c, label).Clicked() {
			g.archived = !g.archived
		}
		if ui.Button(c, a.tr("wbRefresh")).Disabled(g.reading).Clicked() {
			a.readBoard(true)
		}
	})
	ui.Divider(c)
	if g.readonly {
		ui.Text(c, a.tr("wbReadonly")).FontSize(small).TextColor(p.ink2).Padding(8, pagePad).Background(p.inset)
	}
	if g.err != "" && g.board != nil {
		ui.Text(c, g.err).FontSize(small).TextColor(p.red).Padding(8, pagePad)
	}
	if g.foundErr != "" || len(g.found.Errors) > 0 {
		ui.Column(c).Padding(8, pagePad).Gap(2).Background(p.inset).Children(func() {
			if g.foundErr != "" {
				ui.Text(c, a.tr("wbDiscoveryFailed", "error", g.foundErr)).FontSize(small).TextColor(p.orange)
			}
			for i, e := range g.found.Errors {
				ui.Text(c.Key("fe"+itoa(i)), a.tr("wbMachineError", "machine", a.conn.data.MachineName(e.MachineID), "error", e.Error)).FontSize(small).TextColor(p.orange)
			}
			ui.Text(c, a.tr("wbDiscoveryKept")).FontSize(small).TextColor(p.ink3)
		})
	}
	switch {
	case g.board == nil && !g.loaded:
		ui.Spinner(c).Margin(ui.Auto)
		return
	case g.board == nil:
		ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
			a.empty(c, p, "layers", a.tr("wbUnavailable"), g.err)
			if ui.Button(c, a.tr("wbRetry")).Clicked() {
				a.readBoard(true)
			}
		})
		return
	}
	machine := ""
	if i := indexOf(labels, g.machine); i >= 0 {
		machine = ids[i]
	}
	b := *g.board
	sessions := a.allSessions()
	ui.ScrollHorizontal(c.Key("board-cols")).Grow(1).AlignItems(ui.Start).Children(func() {
		ui.Row(c).Padding(16, pagePad).Gap(14).AlignItems(ui.Start).Children(func() {
			for ci, col := range b.Columns {
				cards := hubapi.ColumnCards(b, col.ID, g.query, machine, g.archived)
				ui.Column(c.Key("col-"+col.ID)).Width(280).Radius(cardRadius).Background(p.inset).Border(1, p.line).Children(func() {
					ui.Row(c).Padding(10, 12).Gap(8).Children(func() {
						ui.Box(c).Size(8, 8).Radius(4).Background(p.accent)
						ui.Text(c, col.Title).Bold().SingleLine().Grow(1).Shrink(1)
						ui.Text(c, itoa(len(cards))).FontSize(small).TextColor(p.ink3)
						a.columnMenu(c, p, b, ci)
					})
					ui.Column(c).Padding(0, 10, 10, 10).Gap(8).Children(func() {
						for _, card := range cards {
							a.boardCard(c.Key("card-"+card.ID), p, b, card, hubapi.CardSessions(card, sessions))
						}
						if len(cards) == 0 {
							msg := a.tr("wbEmpty")
							if g.query != "" || machine != "" {
								msg = a.tr("wbNoMatch")
							}
							ui.Text(c, msg).FontSize(small).TextColor(p.ink3).Padding(16, 8).TextAlign(ui.Center)
						}
						if !g.archived && ui.Button(c, "+ "+a.tr("wbBindWorkspace")).Disabled(g.busy || g.readonly).Clicked() {
							g.bind = &boardBind{column: col.ID}
						}
					})
				})
			}
		})
	})
	a.boardDialogs(c, p, b, sessions)
}

// allSessions are every computer's agents, as one list.
func (a *App) allSessions() []hubapi.Session {
	var out []hubapi.Session
	for _, list := range a.conn.data.Sessions {
		out = append(out, list...)
	}
	return out
}

func (a *App) columnMenu(c *ui.Context, p palette, b hubapi.Board, i int) {
	g := &a.board
	col := b.Columns[i]
	more := ui.ButtonBase(c.Key("colm-" + col.ID)).Label(a.tr("wbColumnMenu", "column", col.Title)).Padding(4).Radius(rowRadius).Disabled(g.busy || g.readonly)
	more.Children(func() { ui.Icon(c, icon("more")).TextColor(p.ink2) })
	more.Menu(func(m *ui.Menu) {
		if m.Item(a.tr("wbRenameColumn")).Chosen() {
			g.edit = &boardEdit{id: col.ID, name: col.Title}
		}
		if m.Item(a.tr("wbMoveLeft")).Disabled(i == 0).Chosen() {
			a.changeBoard(map[string]any{"action": "move_column", "id": col.ID, "before_id": b.Columns[i-1].ID}, nil)
		}
		if m.Item(a.tr("wbMoveRight")).Disabled(i == len(b.Columns)-1).Chosen() {
			ch := map[string]any{"action": "move_column", "id": col.ID}
			if i+2 < len(b.Columns) {
				ch["before_id"] = b.Columns[i+2].ID
			}
			a.changeBoard(ch, nil)
		}
		if m.Item(a.tr("wbDeleteColumnMenu")).Disabled(len(b.Columns) <= 1).Chosen() {
			c := col
			g.deleting, g.moveTo = &c, ""
		}
	})
}

func (a *App) cardMenu(c *ui.Context, p palette, b hubapi.Board, card hubapi.BoardCard) {
	g := &a.board
	more := ui.ButtonBase(c.Key("cardm-" + card.ID)).Label(a.tr("wbCardMenu", "card", card.Title)).Padding(4).Radius(rowRadius).Disabled(g.busy || g.readonly)
	more.Children(func() { ui.Icon(c, icon("more")).TextColor(p.ink2) })
	more.Menu(func(m *ui.Menu) {
		var peers []hubapi.BoardCard
		for _, x := range hubapi.ColumnCards(b, card.ColumnID, "", "", card.Archived) {
			if x.Pinned == card.Pinned {
				peers = append(peers, x)
			}
		}
		i := -1
		for k, x := range peers {
			if x.ID == card.ID {
				i = k
			}
		}
		if m.Item(a.tr("wbRenameCard")).Chosen() {
			g.edit = &boardEdit{card: true, id: card.ID, name: card.Title}
		}
		pin := a.tr("wbPin")
		if card.Pinned {
			pin = a.tr("wbUnpin")
		}
		if m.Item(pin).Chosen() {
			a.changeBoard(map[string]any{"action": "update_card", "id": card.ID, "pinned": !card.Pinned}, nil)
		}
		if m.Item(a.tr("wbMoveUp")).Disabled(i <= 0).Chosen() {
			a.changeBoard(map[string]any{"action": "move_card", "id": card.ID, "column_id": card.ColumnID, "before_id": peers[i-1].ID}, nil)
		}
		if m.Item(a.tr("wbMoveDown")).Disabled(i < 0 || i >= len(peers)-1).Chosen() {
			ch := map[string]any{"action": "move_card", "id": card.ID, "column_id": card.ColumnID}
			if i+2 < len(peers) {
				ch["before_id"] = peers[i+2].ID
			}
			a.changeBoard(ch, nil)
		}
		for _, col := range b.Columns {
			if col.ID != card.ColumnID && m.Item(a.tr("wbMoveTo", "column", col.Title)).Chosen() {
				a.changeBoard(map[string]any{"action": "move_card", "id": card.ID, "column_id": col.ID}, nil)
			}
		}
		label := a.tr("wbArchiveCard")
		if card.Archived {
			label = a.tr("wbRestoreCard")
		}
		if m.Item(label).Chosen() {
			a.changeBoard(map[string]any{"action": "update_card", "id": card.ID, "archived": !card.Archived}, nil)
		}
	})
}

// cardWorkspace is what the computers say of a card's space, if they offered it.
func (a *App) cardWorkspace(card hubapi.BoardCard) *hubapi.Workspace {
	for _, w := range a.board.found.Workspaces {
		if w.ID == card.WorkspaceID || (w.MachineID == card.MachineID && hubapi.SameDirectory(w.Cwd, card.Cwd)) {
			return &w
		}
	}
	return nil
}

func (a *App) machineOnline(id string) (online, known bool) {
	for _, m := range a.conn.data.Machines {
		if m.ID == id {
			return m.Online, true
		}
	}
	return false, false
}

func (a *App) boardCard(c *ui.Context, p palette, b hubapi.Board, card hubapi.BoardCard, sessions []hubapi.Session) {
	d := a.conn.data
	ui.Column(c).Padding(10, 12).Gap(6).Radius(rowRadius).Background(p.surface).Border(1, p.line).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Start).Children(func() {
			btn := ui.ButtonBase(c.Key("open")).Label(card.Title).Grow(1).Shrink(1)
			btn.Children(func() {
				ui.Column(c).Gap(4).Grow(1).Children(func() {
					ui.Row(c).Gap(4).Children(func() {
						if card.Pinned {
							ui.Icon(c, icon("pin")).FontSize(small).TextColor(p.accent)
						}
						ui.Text(c, card.Title).Bold().SingleLine().Shrink(1)
					})
					ui.Text(c, card.Cwd).Font("monospace").FontSize(11).TextColor(p.ink3).SingleLine()
				})
			})
			if btn.Clicked() {
				a.board.detail = card.ID
			}
			a.cardMenu(c, p, b, card)
		})
		line := d.MachineName(card.MachineID)
		if card.Branch != "" {
			line += " · " + card.Branch
		}
		ui.Text(c, line).FontSize(small).TextColor(p.ink3).SingleLine()
		ui.Row(c).Gap(6).Wrap().Children(func() {
			n := a.tr("wbNoAgents")
			if len(sessions) > 0 {
				n = a.tr("wbAgentCount", "count", itoa(len(sessions)))
			}
			ui.Text(c, n).FontSize(small).TextColor(p.ink2)
			if txt := a.cardAvailability(card); txt != "" {
				a.pill(c, txt, p.ink3)
			}
			state := ""
			for _, s := range sessions {
				switch d.StateOf(s) {
				case hubapi.StateNeedsYou:
					state = hubapi.StateNeedsYou
				case hubapi.StateWorking:
					if state == "" {
						state = hubapi.StateWorking
					}
				}
			}
			if state != "" {
				a.pill(c, a.stateLabel(state), p.stateColor(state))
			}
		})
	})
}

func (a *App) cardAvailability(card hubapi.BoardCard) string {
	w := a.cardWorkspace(card)
	online, known := a.machineOnline(card.MachineID)
	switch {
	case known && !online, w != nil && w.Online != nil && !*w.Online:
		return a.tr("wbMachineOffline")
	case w != nil && w.Available != nil && !*w.Available:
		return a.tr("wbSpaceUnavailable")
	case w == nil:
		return a.tr("wbSpaceNotFound")
	}
	return ""
}

func (a *App) boardDialogs(c *ui.Context, p palette, b hubapi.Board, sessions []hubapi.Session) {
	g := &a.board
	if e := g.edit; e != nil {
		open := true
		title := a.tr("wbAddColumn")
		switch {
		case e.card:
			title = a.tr("wbRenameCard")
		case e.id != "":
			title = a.tr("wbRenameColumn")
		}
		ui.Modal(c, &open, func() {
			ui.Column(c).Width(420).Padding(20).Gap(12).Children(func() {
				ui.Text(c, title).FontSize(heading).Bold()
				ui.TextInput(c.Key("board-name"), &e.name).Label(a.tr("wbName")).Placeholder(a.tr("wbName")).AutoFocus()
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, a.tr("cancel")).Clicked() {
						g.edit = nil
					}
					if ui.PrimaryButton(c, a.tr("extSave")).Disabled(g.busy || strings.TrimSpace(e.name) == "").Clicked() {
						action := "add_column"
						switch {
						case e.card:
							action = "update_card"
						case e.id != "":
							action = "update_column"
						}
						ch := map[string]any{"action": action, "title": strings.TrimSpace(e.name)}
						if e.id != "" {
							ch["id"] = e.id
						}
						a.changeBoard(ch, func() { g.edit = nil })
					}
				})
			})
		})
		if !open {
			g.edit = nil
		}
	}
	if col := g.deleting; col != nil {
		open := true
		has := false
		var targets []string
		for _, x := range b.Cards {
			has = has || x.ColumnID == col.ID
		}
		for _, x := range b.Columns {
			if x.ID != col.ID {
				targets = append(targets, x.Title)
			}
		}
		ui.Modal(c, &open, func() {
			ui.Column(c).Width(440).Padding(20).Gap(12).Children(func() {
				ui.Text(c, a.tr("wbDeleteColumnTitle", "column", col.Title)).FontSize(heading).Bold()
				ui.Text(c, a.tr("wbDeleteColumnBody")).TextColor(p.ink2)
				if has {
					ui.Text(c, a.tr("wbMoveCardsTo")).FontSize(small).Bold().TextColor(p.ink2)
					ui.Select(c.Key("move-to"), &g.moveTo, targets)
				}
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, a.tr("cancel")).Clicked() {
						g.deleting = nil
					}
					if ui.PrimaryButton(c, a.tr("wbDeleteColumn")).Background(p.red).Disabled(g.busy || (has && g.moveTo == "")).Clicked() {
						ch := map[string]any{"action": "delete_column", "id": col.ID}
						for _, x := range b.Columns {
							if x.ID != col.ID && x.Title == g.moveTo {
								ch["move_to"] = x.ID
							}
						}
						a.changeBoard(ch, func() { g.deleting = nil })
					}
				})
			})
		})
		if !open {
			g.deleting = nil
		}
	}
	if g.detail != "" {
		a.cardDetail(c, p, b, sessions)
	}
	if g.bind != nil {
		a.bindDialog(c, p, b)
	}
}

func (a *App) cardDetail(c *ui.Context, p palette, b hubapi.Board, sessions []hubapi.Session) {
	g := &a.board
	var card *hubapi.BoardCard
	for _, x := range b.Cards {
		if x.ID == g.detail {
			card = &x
		}
	}
	if card == nil {
		g.detail = ""
		return
	}
	d := a.conn.data
	column := ""
	for _, x := range b.Columns {
		if x.ID == card.ColumnID {
			column = x.Title
		}
	}
	here := hubapi.CardSessions(*card, sessions)
	online, _ := a.machineOnline(card.MachineID)
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(520).Padding(20).Gap(12).Children(func() {
			ui.Text(c, card.Title).FontSize(heading).Bold()
			parts := []string{column}
			if card.Archived {
				parts = append(parts, a.tr("wbArchived"))
			}
			parts = append(parts, d.MachineName(card.MachineID))
			if !online {
				parts = append(parts, a.tr("wbOffline"))
			}
			ui.Text(c, strings.Join(parts, " · ")).FontSize(small).TextColor(p.ink3)
			ui.Text(c, card.Cwd).Font("monospace").FontSize(small).TextColor(p.ink2).Padding(10).Radius(rowRadius).Background(p.inset).Selectable()
			if card.Branch != "" {
				ui.Text(c, a.tr("wbBranch", "branch", card.Branch)).FontSize(small).TextColor(p.ink2)
			}
			ui.Row(c).Children(func() {
				if ui.Button(c, a.tr("wbNewAgent")).Disabled(g.readonly || !online).Clicked() {
					g.detail = ""
					a.start.machine, a.start.cwd = card.MachineID, card.Cwd
					a.openStart("", "")
				}
			})
			ui.Divider(c)
			ui.Text(c, a.tr("wbAgentsHere")).FontSize(small).Bold().TextColor(p.ink3)
			if len(here) == 0 {
				ui.Text(c, a.tr("wbNoAgentsDetail")).FontSize(small).TextColor(p.ink3)
			}
			for _, s := range here {
				btn := ui.ButtonBase(c.Key("ag-"+s.Key())).Label(titleOf(s)).Padding(6, 8).Radius(rowRadius)
				if btn.Hovered() {
					btn.Background(p.hover)
				}
				btn.Children(func() {
					ui.Row(c).Gap(8).Grow(1).Children(func() {
						ui.Icon(c, icon("agent")).TextColor(p.ink3)
						ui.Text(c, titleOf(s)).SingleLine().Grow(1).Shrink(1)
						st := d.StateOf(s)
						a.pill(c, a.stateLabel(st), p.stateColor(st))
					})
				})
				if btn.Clicked() {
					g.detail = ""
					a.Navigate("/agents/" + s.Key())
				}
			}
			ui.Row(c).Children(func() {
				ui.Spacer(c)
				if ui.Button(c, a.tr("healthClose")).Clicked() {
					g.detail = ""
				}
			})
		})
	})
	if !open {
		g.detail = ""
	}
}

func (a *App) bindDialog(c *ui.Context, p palette, b hubapi.Board) {
	g := &a.board
	w := g.bind
	open := true
	labels, ids := a.startMachines()
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(560).Padding(20).Gap(12).Children(func() {
			ui.Text(c, a.tr("wbBindWorkspace")).FontSize(heading).Bold()
			if ui.Segmented(c.Key("bind-mode"), &w.mode, a.tr("wbModeExisting"), a.tr("wbModeDirectory")).Changed() && w.mode == 1 && w.machine == "" && len(labels) > 0 {
				w.machine = labels[0]
				a.browseBind("")
			}
			if w.mode == 0 {
				ui.SearchField(c.Key("bind-q"), &w.query).Placeholder(a.tr("wbSearchLabel"))
				q := strings.ToLower(w.query)
				ui.Scroll(c).MaxHeight(340).Children(func() {
					ui.Column(c).Gap(6).Children(func() {
						if len(g.found.Workspaces) == 0 {
							ui.Text(c, a.tr("wbNoneDiscovered")).FontSize(small).TextColor(p.ink3)
						}
						for _, ws := range g.found.Workspaces {
							if !strings.Contains(strings.ToLower(ws.Title+" "+ws.Cwd), q) {
								continue
							}
							a.bindRow(c.Key("ws-"+ws.ID), p, b, ws)
						}
					})
				})
			} else {
				ui.Text(c, a.tr("wbDirectoryMachine")).FontSize(small).Bold().TextColor(p.ink2)
				if ui.Select(c.Key("bind-m"), &w.machine, labels).Changed() {
					w.path = ""
					a.browseBind("")
				}
				ui.TextInput(c.Key("bind-path"), &w.path).Label(a.tr("wbDirectoryLabel")).Placeholder(a.tr("wbDirectoryPlaceholder"))
				if w.dirsErr != "" {
					ui.Text(c, w.dirsErr).FontSize(small).TextColor(p.red)
				}
				if w.dirsBusy {
					ui.Spinner(c)
				}
				if dirs := w.dirs; dirs != nil {
					ui.Column(c).Radius(rowRadius).Border(1, p.line).Children(func() {
						ui.Row(c).Padding(6, 8).Gap(8).BorderWidth(0, 0, 1, 0).BorderColor(p.line).Children(func() {
							if ui.Button(c, "‹").Label(a.tr("wbParentDirectory")).Disabled(dirs.Parent == "").Clicked() {
								a.browseBind(dirs.Parent)
							}
							ui.Text(c, dirs.Path).Font("monospace").FontSize(small).TextColor(p.ink2).SingleLine().Grow(1).Shrink(1)
							if ui.Button(c, a.tr("wbChooseThis")).Clicked() {
								w.path = dirs.Path
							}
						})
						ui.Scroll(c).MaxHeight(220).Children(func() {
							ui.Column(c).Padding(4, 8).Children(func() {
								for _, e := range dirs.Entries {
									ui.Row(c.Key("d-" + e.Path)).Gap(8).Children(func() {
										btn := ui.ButtonBase(c.Key("go")).Label(e.Name).Padding(4).Grow(1).Shrink(1)
										btn.Children(func() {
											ui.Row(c).Gap(6).Children(func() {
												ui.Icon(c, icon("folder")).TextColor(p.ink3)
												ui.Text(c, e.Name).SingleLine()
											})
										})
										if btn.Clicked() {
											a.browseBind(e.Path)
										}
										if ui.Button(c, a.tr("wbChoose")).FontSize(small).Clicked() {
											w.path = e.Path
										}
									})
								}
							})
						})
					})
				}
				ui.Text(c, a.tr("wbBindDirectoryHint")).FontSize(small).TextColor(p.ink3)
				ui.Row(c).Children(func() {
					ui.Spacer(c)
					mid := ""
					if i := indexOf(labels, w.machine); i >= 0 {
						mid = ids[i]
					}
					if ui.PrimaryButton(c, a.tr("wbBindDirectory")).Disabled(g.busy || g.readonly || mid == "" || strings.TrimSpace(w.path) == "").Clicked() {
						a.changeBoard(map[string]any{"action": "add_card", "column_id": w.column, "machine_id": mid, "cwd": strings.TrimSpace(w.path)}, func() { g.bind = nil })
					}
				})
			}
		})
	})
	if !open {
		g.bind = nil
	}
}

func (a *App) bindRow(c *ui.Context, p palette, b hubapi.Board, ws hubapi.Workspace) {
	g := &a.board
	var bound *hubapi.BoardCard
	for _, x := range b.Cards {
		if x.WorkspaceID == ws.ID || (x.MachineID == ws.MachineID && hubapi.SameDirectory(x.Cwd, ws.Cwd)) {
			bound = &x
		}
	}
	online, _ := a.machineOnline(ws.MachineID)
	online = online && (ws.Online == nil || *ws.Online)
	ui.Row(c).Gap(8).Padding(8, 10).Radius(rowRadius).Background(p.inset).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, ws.Title).SingleLine()
			where := ws.Cwd
			if where == "" {
				where = a.tr("wbNoDirectory")
			}
			ui.Text(c, a.conn.data.MachineName(ws.MachineID)+" · "+where).FontSize(small).TextColor(p.ink3).SingleLine()
		})
		switch {
		case bound != nil && bound.Archived:
			if ui.Button(c, a.tr("wbRestore")).Disabled(g.busy || g.readonly).Clicked() {
				a.changeBoard(map[string]any{"action": "update_card", "id": bound.ID, "archived": false}, func() { g.bind = nil })
			}
		case bound != nil:
			ui.Button(c, a.tr("wbBound")).Disabled(true)
		case !online:
			ui.Button(c, a.tr("wbOffline")).Disabled(true)
		default:
			if ui.Button(c, a.tr("wbBind")).Disabled(g.busy || g.readonly || ws.Cwd == "" || (ws.Available != nil && !*ws.Available)).Clicked() {
				a.changeBoard(map[string]any{"action": "add_card", "column_id": g.bind.column, "machine_id": ws.MachineID, "workspace_id": ws.ID, "cwd": ws.Cwd}, func() { g.bind = nil })
			}
		}
	})
}

func (a *App) browseBind(path string) {
	g := &a.board
	w := g.bind
	if w == nil {
		return
	}
	labels, ids := a.startMachines()
	i := indexOf(labels, w.machine)
	if i < 0 {
		return
	}
	cn, mid := a.conn, ids[i]
	w.dirsBusy, w.dirsErr = true, ""
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		dirs, err := cn.client.Dirs(ctx, mid, path)
		a.Host.Post(func() {
			if !a.live(cn) || g.bind != w {
				return
			}
			w.dirsBusy = false
			if err != nil {
				w.dirsErr = a.errorText(err)
				return
			}
			w.dirs = &dirs
		})
	})
}
