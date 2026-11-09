package app

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// Settings' categories past the first seven (their numbers stay what they were).
const (
	channelsTab  = 7
	sentTab      = 8
	shortcutsTab = 9
	hubTab       = 10
	managersTab  = 11
)

// settingsTabs is the category list in the order the web app shows it.
func (a *App) settingsTabs() []struct {
	tab   int
	label string
} {
	return []struct {
		tab   int
		label string
	}{
		{0, a.tr("settingsGeneral")}, {1, "Persona"}, {modelsTab, a.tr("settingsModels")}, {2, a.tr("settingsAgents")},
		{channelsTab, a.tr("navTabChannels")}, {5, a.tr("settingsConnectors")}, {sentTab, a.tr("navTabSent")},
		{4, a.tr("settingsExtensions")}, {shortcutsTab, a.tr("navTabShortcuts")}, {3, a.tr("settingsDevices")},
		{hubTab, a.tr("settingsHub")}, {managersTab, a.tr("navTabManagers")},
	}
}

// settingsNav is Settings' left column: one row per category.
func (a *App) settingsNav(c *ui.Context, p palette, tab *int) {
	ui.Column(c).Width(200).Padding(12, 8).Gap(2).BorderWidth(0, 1, 0, 0).BorderColor(p.line).Children(func() {
		for _, t := range a.settingsTabs() {
			row := ui.ButtonBase(c.Key("settab-"+itoa(t.tab))).Padding(7, 12).Radius(rowRadius).Label(t.label)
			if *tab == t.tab {
				row.Background(p.hover)
			} else if row.Hovered() {
				row.Background(p.inset)
			}
			row.Children(func() {
				ui.Row(c).Grow(1).Children(func() {
					txt := ui.Text(c, t.label).SingleLine()
					if *tab == t.tab {
						txt.Bold()
					}
				})
			})
			if row.Clicked() {
				*tab = t.tab
			}
		}
	})
}

// more7 is the state of the categories this file draws.
type more7 struct {
	hub        loadable
	hubSt      hubapi.HubUpdate
	hubHist    []hubapi.UpdateRecord
	hubNone    bool // a hub from before /hub/update
	hubBusy    string
	hubNowAsk  bool
	hubPollAt  time.Time
	mgrs       loadable
	mgrList    []hubapi.Manager
	mgrNone    bool
	mgrNew     bool
	mgrName    string
	mgrRename  string // the manager being renamed
	mgrDelete  string
	mgrCode    *hubapi.InstallCode
	mgrCodeFor string
	mgrErr     string
	sent       loadable
	sentItems  []hubapi.SentItem
	sentNext   int64
	sentOpen   int64
	sentWhy    string // no_database | forbidden | unsupported
	chans      loadable
	chanList   []hubapi.Channel
	chanUnbind string
	mail       loadable
	mailList   []hubapi.MailAccount
	mailAdd    bool
	mailForm   hubapi.MailForm
	mailKind   int
	mailTried  bool
	mailBusy   bool
	mailErr    string
	mailGoogle bool
	mailDelete string
	signIn     signInState
}

// ── Hub ──

func (a *App) hubUpdateView(c *ui.Context, p palette) {
	g := &a.sets.m7
	a.load(&g.hub, func(ctx context.Context, cl *hubapi.Client) (func(), error) {
		st, err := cl.HubUpdate(ctx)
		var he *hubapi.Error
		if errors.As(err, &he) && he.Status == http.StatusNotFound {
			return func() { g.hubNone = true }, nil
		}
		if err != nil {
			return nil, err
		}
		hist, _ := cl.HubUpdateHistory(ctx)
		return func() { g.hubSt, g.hubHist, g.hubPollAt = st, hist, time.Now() }, nil
	})
	if !a.loadState(c, p, &g.hub) {
		return
	}
	if g.hubNone {
		a.card(c, p, func() { ui.Text(c, a.tr("hubUpUnsupported")).FontSize(small).TextColor(p.ink2) })
		return
	}
	st := g.hubSt
	// an update under way is read again every three seconds
	if st.Working() && time.Since(g.hubPollAt) > 3*time.Second {
		g.hubPollAt = time.Now()
		a.hubDo("", func(ctx context.Context, cl *hubapi.Client) (hubapi.HubUpdate, error) { return cl.HubUpdate(ctx) })
	}
	if st.AutoNotice {
		ui.Row(c).Padding(10, 14).Gap(10).Radius(cardRadius).Background(p.inset).Children(func() {
			ui.Text(c, a.tr("hubUpAutoNotice")).Grow(1).Shrink(1)
			if ui.Button(c, a.tr("hubUpTurnOff")).Clicked() {
				a.hubDo("auto", func(ctx context.Context, cl *hubapi.Client) (hubapi.HubUpdate, error) {
					return cl.SetHubUpdate(ctx, map[string]any{"auto": false})
				})
			}
			if ui.Button(c, a.tr("hubUpGotIt")).Clicked() {
				a.hubDo("notice", func(ctx context.Context, cl *hubapi.Client) (hubapi.HubUpdate, error) {
					return cl.SetHubUpdate(ctx, map[string]any{"auto_notice_seen": true})
				})
			}
		})
	}
	now := time.Now()
	a.group(c, p, a.tr("hubUpVersion"), "", func() {
		a.valueRow(c, p, a.tr("hubUpVersion"), st.Version)
		a.valueRow(c, p, a.tr("hubUpChannel"), a.channelTitle(st.Channel))
		a.valueRow(c, p, a.tr("hubUpLastCheck"), a.ago(st.LastCheck, now))
		ui.Row(c).Children(func() {
			ui.Spacer(c)
			label := a.tr("hubUpCheck")
			if g.hubBusy == "check" {
				label = a.tr("hubUpChecking")
			}
			if ui.Button(c, label).Disabled(g.hubBusy != "" || st.Working()).Clicked() {
				a.hubDo("check", func(ctx context.Context, cl *hubapi.Client) (hubapi.HubUpdate, error) { return cl.CheckHubUpdate(ctx) })
			}
		})
	})
	footer := ""
	if st.CanUpdate {
		footer = a.tr("hubUpUpdateFooter")
	}
	a.group(c, p, a.tr("hubUpUpdate"), footer, func() { a.hubUpdateBody(c, p, st, now) })
	if st.CanUpdate {
		a.group(c, p, a.tr("hubUpAutoUpdate"), a.tr("hubUpAutoFooter"), func() {
			on := st.Auto
			if a.switchRow(c, p, a.tr("hubUpAutoUpdate"), "", &on) {
				a.hubDo("auto", func(ctx context.Context, cl *hubapi.Client) (hubapi.HubUpdate, error) {
					return cl.SetHubUpdate(ctx, map[string]any{"auto": on})
				})
			}
		})
	}
	if ms := a.conn.data.Machines; len(ms) > 0 {
		a.group(c, p, a.tr("hubUpNodes"), a.tr("hubUpNodesFooter"), func() {
			for _, m := range ms {
				ui.Row(c.Key("node-" + m.ID)).Gap(8).Children(func() {
					ui.Text(c, m.Name).Grow(1).Shrink(1).SingleLine()
					if m.NodeBehind {
						a.pill(c, a.tr("hubUpVersionDiffers"), p.orange)
					}
					v := m.NodeVer
					if v == "" {
						v = a.tr("hubUpUnknown")
						if !m.Online {
							v = a.tr("hubUpOffline")
						}
					}
					ui.Text(c, v).FontSize(small).TextColor(p.ink3).SingleLine()
				})
			}
		})
	}
	if len(g.hubHist) > 0 {
		a.group(c, p, a.tr("hubUpHistory"), "", func() {
			for i, r := range g.hubHist[:min(10, len(g.hubHist))] {
				ui.Column(c.Key("hist-" + itoa(i))).Gap(2).Children(func() {
					ui.Row(c).Gap(8).Children(func() {
						title, col := a.recordTitle(p, r.Result)
						ui.Text(c, title).Bold().TextColor(col)
						if r.Auto {
							a.pill(c, a.tr("hubUpAuto"), p.ink2)
						}
						ui.Spacer(c)
						at := r.Ended
						if at == "" {
							at = r.Started
						}
						ui.Text(c, a.ago(at, now)).FontSize(small).TextColor(p.ink3)
					})
					ui.Text(c, r.FromVersion+" → "+r.ToVersion).FontSize(small).TextColor(p.ink3).SingleLine()
					if r.Reason != "" {
						ui.Text(c, r.Reason).FontSize(small).TextColor(p.ink3)
					}
				})
			}
		})
	}
	if g.hubNowAsk {
		ui.Modal(c, &g.hubNowAsk, func() {
			ui.Column(c).Width(420).Padding(20).Gap(12).Children(func() {
				ui.Text(c, a.tr("hubUpConfirmTitle")).FontSize(heading).Bold()
				ui.Text(c, a.tr("hubUpConfirmBody")).TextColor(p.ink2)
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, a.tr("cancel")).Clicked() {
						g.hubNowAsk = false
					}
					if ui.PrimaryButton(c, a.tr("hubUpUpdateNow")).Background(p.red).Disabled(g.hubBusy != "").Clicked() {
						g.hubNowAsk = false
						a.hubDo("start", func(ctx context.Context, cl *hubapi.Client) (hubapi.HubUpdate, error) {
							return cl.StartHubUpdate(ctx, true)
						})
					}
				})
			})
		})
	}
}

func (a *App) hubUpdateBody(c *ui.Context, p palette, st hubapi.HubUpdate, now time.Time) {
	g := &a.sets.m7
	switch {
	case st.Channel == "docker":
		a.dockerSteps(c, p, st)
	case !st.CanUpdate:
		why := a.tr("hubStCannotUpdate")
		if st.WhyNot != "" {
			why = a.tr("hubStCannotUpdateWhy", "why", st.WhyNot)
		}
		ui.Text(c, why).FontSize(small).TextColor(p.ink2)
	case st.Latest != nil && st.Latest.Newer:
		l := st.Latest
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, a.tr("hubUpNewVersion", "version", l.Version)).Bold().Shrink(1).SingleLine()
			if l.Released != "" {
				ui.Text(c, a.tr("hubUpReleased", "time", a.ago(l.Released, now))).FontSize(small).TextColor(p.ink3)
			}
		})
		if l.Notes != "" {
			ui.Text(c, l.Notes).FontSize(small).TextColor(p.ink2)
		}
		switch {
		case l.Bad:
			ui.Text(c, a.tr("hubStWarningBad")).FontSize(small).TextColor(p.orange)
		case l.Held != "" && st.Auto:
			ui.Text(c, a.tr("hubStWarningHeld", "why", l.Held)).FontSize(small).TextColor(p.orange)
		}
		if st.Working() {
			a.hubProgress(c, p, st)
		} else {
			ui.Row(c).Children(func() {
				ui.Spacer(c)
				if ui.PrimaryButton(c, a.tr("hubUpUpdateHub")).Disabled(g.hubBusy != "").Clicked() {
					a.hubDo("start", func(ctx context.Context, cl *hubapi.Client) (hubapi.HubUpdate, error) {
						return cl.StartHubUpdate(ctx, false)
					})
				}
			})
		}
	case st.Working():
		a.hubProgress(c, p, st)
	default:
		ui.Row(c).Gap(6).Children(func() {
			ui.Icon(c, icon("check")).TextColor(p.green)
			ui.Text(c, a.tr("hubUpUpToDate")).TextColor(p.green)
		})
	}
	if st.Error != "" && !st.Working() {
		col := p.red
		if st.State == "rolled_back" {
			col = p.orange
		}
		ui.Text(c, st.Error).FontSize(small).TextColor(col)
	}
}

func (a *App) hubProgress(c *ui.Context, p palette, st hubapi.HubUpdate) {
	ui.Row(c).Gap(8).Children(func() {
		ui.Spinner(c)
		ui.Text(c, a.updateStateTitle(st)).Grow(1).Shrink(1).SingleLine()
		if st.Progress != "" && st.State == "downloading" {
			ui.Text(c, st.Progress).FontSize(small).TextColor(p.ink3)
		}
	})
	if st.State == "waiting_idle" && len(st.Busy) > 0 {
		for i, b := range st.Busy {
			ui.Row(c.Key("busy-"+itoa(i))).Gap(8).Padding(0, 0, 0, 4).Children(func() {
				ui.Box(c).Size(6, 6).Radius(3).Background(p.accent)
				ui.Text(c, b.Title).FontSize(small).Grow(1).Shrink(1).SingleLine()
				ui.Text(c, b.Machine).FontSize(small).TextColor(p.ink3)
			})
		}
		ui.Row(c).Children(func() {
			ui.Spacer(c)
			if ui.Button(c, a.tr("hubUpUpdateNowEllipsis")).TextColor(p.red).Clicked() {
				a.sets.m7.hubNowAsk = true
			}
		})
	}
}

func (a *App) dockerSteps(c *ui.Context, p palette, st hubapi.HubUpdate) {
	type step struct{ text, copy string }
	var head string
	var steps []step
	switch l := st.Latest; {
	case l != nil && l.Newer && st.Image != nil && st.Image.Pinned:
		head = a.tr("hubStNewImage", "version", l.Version)
		steps = []step{{a.tr("hubStDockerSetImage"), st.Image.Name + ":" + l.Version}, {a.tr("hubStDockerThenRun"), hubapi.DockerCommand}}
	case l != nil && l.Newer:
		head = a.tr("hubStNewImage", "version", l.Version)
		steps = []step{{a.tr("hubStDockerRun"), hubapi.DockerCommand}}
	default:
		steps = []step{{a.tr("hubStDockerHow"), hubapi.DockerCommand}}
	}
	if head != "" {
		ui.Text(c, head).Bold()
	}
	for i, s := range steps {
		ui.Column(c.Key("dock-" + itoa(i))).Gap(4).Children(func() {
			ui.Text(c, s.text).FontSize(small).TextColor(p.ink2)
			a.copyLine(c, p, s.copy)
		})
	}
}

// copyLine is a command or code to copy: monospace, with Copy.
func (a *App) copyLine(c *ui.Context, p palette, text string) {
	ui.Row(c).Gap(8).Padding(6, 10).Radius(rowRadius).Background(p.inset).Children(func() {
		ui.Text(c, text).FontSize(small).Font("monospace").Grow(1).Shrink(1).Selectable()
		if ui.Button(c, a.tr("hubUpCopy")).FontSize(small).Padding(2, 8).Clicked() {
			c.WriteClipboard(text)
			a.toast(a.tr("hubUpCopied"))
		}
	})
}

func (a *App) channelTitle(ch string) string {
	switch ch {
	case "release":
		return a.tr("hubStChannelRelease")
	case "docker":
		return "Docker"
	}
	return a.tr("hubStChannelDev")
}

func (a *App) updateStateTitle(st hubapi.HubUpdate) string {
	switch st.State {
	case "checking":
		return a.tr("hubStStateChecking")
	case "downloading":
		return a.tr("hubStStateDownloading")
	case "waiting_idle":
		if len(st.Busy) > 0 {
			return a.tr("hubStStateWaitingBusy", "count", itoa(len(st.Busy)))
		}
		return a.tr("hubStStateWaitingIdle")
	case "restarting":
		return a.tr("hubStStateRestarting")
	case "confirming":
		return a.tr("hubStStateConfirming")
	}
	return a.tr("hubUpUpToDate")
}

func (a *App) recordTitle(p palette, result string) (string, ui.Color) {
	switch result {
	case "updated":
		return a.tr("hubStResultUpdated"), p.green
	case "rolled_back":
		return a.tr("hubStResultRolledBack"), p.orange
	case "held":
		return a.tr("hubStResultHeld"), p.orange
	}
	return a.tr("hubStResultFailed"), p.red
}

// hubDo runs a change to the hub's update and shows what it answered.
func (a *App) hubDo(what string, do func(ctx context.Context, cl *hubapi.Client) (hubapi.HubUpdate, error)) {
	g := &a.sets.m7
	if what != "" {
		if g.hubBusy != "" {
			return
		}
		g.hubBusy = what
	}
	cn := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		st, err := do(ctx, cn.client)
		var hist []hubapi.UpdateRecord
		if err == nil {
			hist, _ = cn.client.HubUpdateHistory(ctx)
		}
		a.Host.Post(func() {
			if !a.live(cn) {
				return
			}
			if what != "" {
				g.hubBusy = ""
			}
			if err != nil {
				msg := a.errorText(err)
				switch what {
				case "check":
					a.toast(a.tr("hubUpCheckFailed", "error", msg))
				case "start":
					a.toast(a.tr("hubUpStartFailed", "error", msg))
				case "":
				default:
					a.toast(a.tr("hubUpSaveFailed", "error", msg))
				}
				return
			}
			g.hubSt, g.hubPollAt = st, time.Now()
			if hist != nil {
				g.hubHist = hist
			}
			if st.Working() {
				a.After(3*time.Second, func() {})
			}
		})
	})
}

// ── Managers ──

func (a *App) managersView(c *ui.Context, p palette) {
	g := &a.sets.m7
	a.load(&g.mgrs, func(ctx context.Context, cl *hubapi.Client) (func(), error) {
		list, err := cl.Managers(ctx)
		var he *hubapi.Error
		if errors.As(err, &he) && he.Status == http.StatusNotFound {
			return func() { g.mgrNone = true }, nil
		}
		return func() { g.mgrList = list }, err
	})
	if !a.loadState(c, p, &g.mgrs) {
		return
	}
	if g.mgrNone {
		a.card(c, p, func() { ui.Text(c, a.tr("mgrUnsupported")).FontSize(small).TextColor(p.ink2) })
		return
	}
	now := time.Now()
	ui.Row(c).Gap(12).Children(func() {
		ui.Text(c, a.tr("mgrHint")).FontSize(small).TextColor(p.ink2).Grow(1).Shrink(1)
		if ui.Button(c, a.tr("mgrNew")).Clicked() {
			g.mgrNew, g.mgrName, g.mgrErr = true, "", ""
		}
	})
	a.card(c, p, func() {
		for i, m := range g.mgrList {
			ui.Row(c.Key("mgr-"+m.ID)).Gap(12).Padding(6, 0).AlignItems(ui.Start).BorderWidth(btop(i), 0, 0, 0).BorderColor(p.line).Children(func() {
				ui.Column(c).Grow(1).Shrink(1).Gap(3).Children(func() {
					ui.Row(c).Gap(6).Children(func() {
						ui.Text(c, m.Name).Bold().Shrink(1).SingleLine()
						if m.Default {
							a.pill(c, a.tr("mgrDefault"), p.accent)
						}
						if m.NeedsUpdate {
							a.pill(c, a.tr("mgrNeedsUpdate"), p.orange)
						}
					})
					status, col := a.tr("mgrOnline"), p.green
					switch {
					case m.Installing != "":
						col = p.attention
						if left := time.Until(hubapi.ParseTime(m.Installing)); left > 0 {
							status = a.tr("mgrInstalling", "count", itoa(int(left.Minutes())+1))
						} else {
							status = a.tr("mgrInstallingExpired")
						}
					case !m.Online && m.LastSeen != "":
						status, col = a.tr("mgrOfflineSince", "time", a.ago(m.LastSeen, now)), p.ink3
					case !m.Online:
						status, col = a.tr("mgrOffline"), p.ink3
					}
					ui.Text(c, status).FontSize(small).TextColor(col)
					where := joinDot(a.deployTitle(m.Deploy), m.Host)
					if m.OS != "" {
						where = joinDot(where, m.OS+"/"+m.Arch)
					}
					if where != "" {
						ui.Text(c, where).FontSize(small).TextColor(p.ink3).SingleLine()
					}
					if m.Version != "" {
						ui.Text(c, m.Version).FontSize(small).TextColor(p.ink3).SingleLine()
					}
				})
				a.managerMenu(c, p, m)
			})
		}
	})
	a.managerDialogs(c, p)
}

func btop(i int) float32 {
	if i == 0 {
		return 0
	}
	return 1
}

func (a *App) deployTitle(d string) string {
	switch d {
	case "child":
		return a.tr("mgrDeployHub")
	case "service":
		return a.tr("mgrDeployService")
	case "docker":
		return a.tr("mgrDeployContainer")
	case "external":
		return a.tr("mgrDeployExternal")
	}
	return d
}

func (a *App) managerMenu(c *ui.Context, p palette, m hubapi.Manager) {
	g := &a.sets.m7
	more := ui.ButtonBase(c.Key("mgr-more-" + m.ID)).Label(a.tr("agentMore")).Padding(6).Radius(rowRadius)
	more.Children(func() { ui.Icon(c, icon("more")).TextColor(p.ink2) })
	more.Menu(func(mn *ui.Menu) {
		if m.Installing == "" && mn.Item(a.tr("mgrRename")).Chosen() {
			g.mgrRename, g.mgrName, g.mgrErr = m.ID, m.Name, ""
		}
		if m.Installing == "" && !m.Default && mn.Item(a.tr("mgrMakeDefault")).Chosen() {
			a.managerDo(m.ID, func(ctx context.Context, cl *hubapi.Client) error {
				return cl.PatchManager(ctx, m.ID, map[string]any{"default": true})
			})
		}
		if !m.Builtin && (m.Installing != "" || !m.Online) && mn.Item(a.tr("mgrNewCode")).Chosen() {
			a.managerCode(m)
		}
		label := a.tr("mgrDelete")
		switch {
		case m.Builtin:
			label = a.tr("mgrDeleteBuiltin")
		case m.Default:
			label = a.tr("mgrDeleteDefault")
		}
		if mn.Item(label).Disabled(m.Builtin || m.Default).Chosen() {
			g.mgrDelete = m.ID
		}
	})
}

func (a *App) managerDo(row string, do func(ctx context.Context, cl *hubapi.Client) error) {
	g := &a.sets.m7
	a.change(&g.mgrs, row, do, nil)
}

func (a *App) managerCode(m hubapi.Manager) {
	g := &a.sets.m7
	cn := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		code, err := cn.client.ManagerCode(ctx, m.ID)
		a.Host.Post(func() {
			if !a.live(cn) {
				return
			}
			if err != nil {
				a.toast(a.errorText(err))
				return
			}
			g.mgrCode, g.mgrCodeFor = &code, m.Name
		})
	})
}

func (a *App) createManager() {
	g := &a.sets.m7
	name := strings.TrimSpace(g.mgrName)
	if !hubapi.ValidManagerName(name) {
		g.mgrErr = a.tr("mgrErrName")
		return
	}
	cn := a.conn
	g.mgrs.busyOn = "new"
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, code, err := cn.client.CreateManager(ctx, name)
		a.Host.Post(func() {
			g.mgrs.busyOn = ""
			if !a.live(cn) {
				return
			}
			if err != nil {
				g.mgrErr = a.errorText(err)
				return
			}
			g.mgrNew, g.mgrCode, g.mgrCodeFor = false, &code, name
			g.mgrs.asked = false
		})
	})
}

func (a *App) managerDialogs(c *ui.Context, p palette) {
	g := &a.sets.m7
	if g.mgrNew || g.mgrRename != "" {
		open := true
		ui.Modal(c, &open, func() {
			ui.Column(c).Width(420).Padding(20).Gap(12).Children(func() {
				title := a.tr("mgrNew")
				if g.mgrRename != "" {
					title = a.tr("mgrRename")
				}
				ui.Text(c, title).FontSize(heading).Bold()
				if g.mgrNew {
					ui.Text(c, a.tr("mgrNewHint")).FontSize(small).TextColor(p.ink2)
				}
				ui.TextInput(c.Key("mgr-name"), &g.mgrName).Label(a.tr("mgrName")).Placeholder(a.tr("mgrNamePlaceholder")).AutoFocus()
				if g.mgrErr != "" {
					ui.Text(c, g.mgrErr).FontSize(small).TextColor(p.red)
				}
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, a.tr("cancel")).Clicked() {
						g.mgrNew, g.mgrRename = false, ""
					}
					label := a.tr("mgrCreate")
					if g.mgrRename != "" {
						label = a.tr("mgrSave")
					}
					if ui.PrimaryButton(c, label).Disabled(g.mgrs.busyOn != "").Clicked() {
						if g.mgrNew {
							a.createManager()
						} else if name := strings.TrimSpace(g.mgrName); !hubapi.ValidManagerName(name) {
							g.mgrErr = a.tr("mgrErrName")
						} else {
							id := g.mgrRename
							g.mgrRename = ""
							a.managerDo(id, func(ctx context.Context, cl *hubapi.Client) error {
								return cl.PatchManager(ctx, id, map[string]any{"name": name})
							})
						}
					}
				})
			})
		})
		if !open {
			g.mgrNew, g.mgrRename = false, ""
		}
	}
	if g.mgrCode != nil {
		open := true
		code := g.mgrCode
		ui.Modal(c, &open, func() {
			ui.Column(c).Width(520).Padding(20).Gap(12).Children(func() {
				ui.Text(c, a.tr("mgrCodeFor", "name", g.mgrCodeFor)).FontSize(heading).Bold()
				left := time.Until(hubapi.ParseTime(code.ExpiresAt))
				if left > 0 {
					ui.Text(c, a.tr("mgrExpiresIn", "time", itoa(int(left.Minutes())+1)+"m")).FontSize(small).TextColor(p.ink2)
				} else {
					ui.Text(c, a.tr("mgrExpired")).FontSize(small).TextColor(p.red)
				}
				if cm := code.Commands; cm != nil && (cm.Service != "" || cm.Docker != "") {
					ui.Text(c, a.tr("mgrCommandsNote")).FontSize(small).TextColor(p.ink2)
					if cm.Service != "" {
						ui.Text(c, a.tr("mgrInstallService")).FontSize(small).Bold()
						a.copyLine(c, p, cm.Service)
					}
					if cm.Docker != "" {
						ui.Text(c, a.tr("mgrInstallDocker")).FontSize(small).Bold()
						a.copyLine(c, p, cm.Docker)
					}
				} else {
					ui.Text(c, a.tr("mgrCodeNote")).FontSize(small).TextColor(p.ink2)
					a.copyLine(c, p, code.Code)
				}
				ui.Row(c).Children(func() {
					ui.Spacer(c)
					if ui.PrimaryButton(c, a.tr("mgrDone")).Clicked() {
						g.mgrCode = nil
					}
				})
			})
		})
		if !open {
			g.mgrCode = nil
		}
	}
	if g.mgrDelete != "" {
		open := true
		var name string
		for _, m := range g.mgrList {
			if m.ID == g.mgrDelete {
				name = m.Name
			}
		}
		ui.Modal(c, &open, func() {
			ui.Column(c).Width(420).Padding(20).Gap(12).Children(func() {
				ui.Text(c, a.tr("mgrDeleteConfirm", "name", name)).Bold()
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, a.tr("cancel")).Clicked() {
						g.mgrDelete = ""
					}
					if ui.PrimaryButton(c, a.tr("mgrDelete")).Background(p.red).Clicked() {
						id := g.mgrDelete
						g.mgrDelete = ""
						a.managerDo(id, func(ctx context.Context, cl *hubapi.Client) error { return cl.DeleteManager(ctx, id) })
					}
				})
			})
		})
		if !open {
			g.mgrDelete = ""
		}
	}
}

// ── Sent as you ──

func (a *App) sentView(c *ui.Context, p palette) {
	g := &a.sets.m7
	a.load(&g.sent, func(ctx context.Context, cl *hubapi.Client) (func(), error) {
		items, next, err := cl.Sent(ctx, 0)
		var he *hubapi.Error
		if errors.As(err, &he) {
			why := ""
			switch {
			case he.Status == http.StatusNotFound:
				why = "unsupported"
			case he.Status == http.StatusForbidden:
				why = "forbidden"
			case he.Status == http.StatusServiceUnavailable || he.Code == "no_database":
				why = "no_database"
			}
			if why != "" {
				return func() { g.sentWhy = why }, nil
			}
		}
		return func() { g.sentItems, g.sentNext = items, next }, err
	})
	if !a.loadState(c, p, &g.sent) {
		return
	}
	switch g.sentWhy {
	case "unsupported":
		a.card(c, p, func() { ui.Text(c, a.tr("sentUnsupported")).FontSize(small).TextColor(p.ink2) })
		return
	case "forbidden":
		a.card(c, p, func() { ui.Text(c, a.tr("sentForbidden")).FontSize(small).TextColor(p.ink2) })
		return
	case "no_database":
		a.card(c, p, func() { ui.Text(c, a.tr("sentNoDatabase")).FontSize(small).TextColor(p.ink2) })
		return
	}
	ui.Text(c, a.tr("sentIntro")).FontSize(small).TextColor(p.ink2)
	if len(g.sentItems) == 0 {
		a.card(c, p, func() {
			ui.Text(c, a.tr("sentEmptyTitle")).Bold()
			ui.Text(c, a.tr("sentEmptyRecorded")).FontSize(small).TextColor(p.ink2)
			ui.Text(c, a.tr("sentEmptyNotRecorded")).FontSize(small).TextColor(p.ink2)
		})
		return
	}
	now := time.Now()
	a.card(c, p, func() {
		for i, it := range g.sentItems {
			row := ui.ButtonBase(c.Key("sent-"+itoa(int(it.ID)))).Padding(8, 0).BorderWidth(btop(i), 0, 0, 0).BorderColor(p.line).Label(it.Summary)
			row.Children(func() { ui.Column(c).Gap(4).Grow(1).Shrink(1).Children(func() { a.sentRow(c, p, it, now) }) })
			if g.sentOpen == it.ID {
				a.sentDetail(c.Key("sentd-"+itoa(int(it.ID))), p, it)
			}
			if row.Hovered() {
				row.Background(p.hover)
			}
			if row.Clicked() {
				if g.sentOpen == it.ID {
					g.sentOpen = 0
				} else {
					g.sentOpen = it.ID
				}
			}
		}
	})
	if g.sentNext > 0 {
		ui.Row(c).Children(func() {
			ui.Spacer(c)
			if ui.Button(c, a.tr("sentLoadEarlier")).Disabled(g.sent.busyOn != "").Clicked() {
				a.moreSent()
			}
			ui.Spacer(c)
		})
	}
}

// sentRow is a send: what and when, how it was allowed, and when opened its details and text.
func (a *App) sentRow(c *ui.Context, p palette, it hubapi.SentItem, now time.Time) {
	ui.Row(c).Gap(8).Children(func() {
		ui.Text(c, it.Summary).Bold().Grow(1).Shrink(1).SingleLine()
		ui.Text(c, a.ago(it.At, now)).FontSize(small).TextColor(p.ink3)
	})
	ui.Text(c, joinDot(it.Connector, a.sentAllowed(it))).FontSize(small).TextColor(p.ink2).SingleLine()
}

// sentDetail is an opened send's details and text.
func (a *App) sentDetail(c *ui.Context, p palette, it hubapi.SentItem) {
	ui.Column(c).Gap(4).Padding(0, 0, 10, 0).Children(func() {
		a.valueRow(c, p, a.tr("sentDSent"), hubapi.ParseTime(it.At).Local().Format("2006-01-02 15:04"))
		a.valueRow(c, p, a.tr("sentDAction"), it.Action)
		a.valueRow(c, p, a.tr("sentDTarget"), targetText(it.Target))
		a.valueRow(c, p, a.tr("sentDAllowed"), a.sentAllowed(it))
		text := it.Text
		if text == "" {
			text = a.tr("sentNoText")
		}
		ui.Text(c, a.tr("sentDText")).FontSize(small).TextColor(p.ink2)
		ui.Text(c, text).Selectable().Padding(8).Radius(rowRadius).Background(p.inset)
		ui.Row(c).Gap(8).Children(func() {
			if it.Text != "" && ui.Button(c, a.tr("sentCopyText")).Clicked() {
				c.WriteClipboard(it.Text)
				a.toast(a.tr("hubUpCopied"))
			}
			if it.Origin.MachineID != "" && it.Origin.SessionID != "" && ui.Button(c, a.tr("sentOpenChat")).Clicked() {
				a.Navigate("/agents/" + it.Origin.MachineID + "/" + it.Origin.SessionID)
			}
			if it.Origin.Extension != "" {
				a.pill(c, a.tr("sentOriginExtension"), p.ink2)
			}
		})
	})
}

func (a *App) moreSent() {
	g := &a.sets.m7
	cn, before := a.conn, g.sentNext
	g.sent.busyOn = "more"
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		items, next, err := cn.client.Sent(ctx, before)
		a.Host.Post(func() {
			g.sent.busyOn = ""
			if !a.live(cn) {
				return
			}
			if err != nil {
				a.toast(a.tr("sentLoadFailed", "error", a.errorText(err)))
				return
			}
			seen := map[int64]bool{}
			for _, it := range g.sentItems {
				seen[it.ID] = true
			}
			for _, it := range items {
				if !seen[it.ID] {
					g.sentItems = append(g.sentItems, it)
				}
			}
			g.sentNext = next
		})
	})
}

// sentAllowed says how the gate let a send through.
func (a *App) sentAllowed(it hubapi.SentItem) string {
	lt := it.LetThrough
	switch lt.Kind {
	case "rule":
		return a.tr("sentByRule")
	case "conversation_always":
		return a.tr("sentByAlways")
	case "card":
		switch lt.AnsweredBy {
		case "":
			return a.tr("sentByCard")
		case "assistant":
			return a.tr("sentByAssistant")
		}
		return a.tr("sentByNamed", "name", lt.AnsweredBy)
	case "auto":
		if lt.Setting != "" {
			return a.tr("sentByAutoSetting", "setting", lt.Setting)
		}
		return a.tr("sentByAuto")
	}
	return lt.Kind
}

// targetText is where a send went, as the connector named it: its strings, joined.
func targetText(t map[string]any) string {
	var parts []string
	for _, k := range []string{"to", "repo", "number", "chat", "chat_name", "title", "name", "email", "id"} {
		switch v := t[k].(type) {
		case string:
			if v != "" {
				parts = append(parts, v)
			}
		case float64:
			parts = append(parts, "#"+itoa(int(v)))
		case []any:
			for _, x := range v {
				if s, ok := x.(string); ok {
					parts = append(parts, s)
				}
			}
		}
	}
	return strings.Join(parts, " · ")
}

// ── Shortcuts ──

func (a *App) shortcutsView(c *ui.Context, p palette) {
	mod := "Ctrl+"
	if runtime.GOOS == "darwin" {
		mod = "⌘"
	}
	rows := [][2]string{
		{a.tr("shortcutsPalette"), mod + "K"},
		{a.tr("shortcutsSettings"), mod + ","},
		{a.tr("shortcutsPages"), mod + "1–" + mod + "8"},
		{a.tr("shortcutsBack"), mod + "["},
		{a.tr("shortcutsForward"), mod + "]"},
		{a.tr("shortcutsHubs"), mod + "⇧H"},
		{a.tr("shortcutsSend"), "Enter"},
		{a.tr("shortcutsNewline"), "Shift+Enter"},
	}
	if sc := a.Host.Shortcut(); sc != "" && !a.Hubs.Settings.NoShortcut {
		rows = append([][2]string{{a.tr("shortcutsWindow"), sc}}, rows...)
	}
	ui.Text(c, a.tr("shortcutsIntro")).FontSize(small).TextColor(p.ink2)
	a.card(c, p, func() {
		for i, r := range rows {
			ui.Row(c.Key("sc-"+itoa(i))).Gap(12).Padding(4, 0).Children(func() {
				ui.Text(c, r[0]).Grow(1).Shrink(1)
				ui.Text(c, r[1]).FontSize(small).Font("monospace").Padding(2, 8).Radius(6).Background(p.inset).SingleLine()
			})
		}
	})
}
