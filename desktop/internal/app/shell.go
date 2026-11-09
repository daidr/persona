package app

import (
	"errors"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
	"github.com/nwverse/persona/desktop/internal/hubs"
)

// View builds the window.
func (a *App) View(c *ui.Context) {
	p := a.theme(c)
	a.flushToasts(c)
	if a.conn == nil {
		a.connectView(c, p)
		return
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		a.sidebar(c, p)
		ui.Column(c).Grow(1).Background(p.page).Children(func() {
			a.statusBanner(c, p)
			a.router.View(c, func(r *ui.Route) {
				switch {
				case r.Match("/"):
					r.Title(a.tr("goPersona"))
					a.personaPage(c, p, "")
				case r.Match("/persona/{id...}"):
					r.Title(a.tr("goPersona"))
					a.personaPage(c, p, r.Param("id"))
				case r.Match("/overview"):
					r.Title(a.tr("goOverview"))
					a.overviewView(c, p)
				case r.Match("/routines"):
					r.Title(a.tr("goRoutines"))
					a.routinesView(c, p)
				case r.Match("/agents"):
					r.Title(a.tr("goAgents"))
					a.agentsView(c, p, "")
				case r.Match("/agents/{machine}/{session...}"):
					r.Title(a.tr("goAgents"))
					a.agentsView(c, p, r.Param("machine")+"/"+r.Param("session"))
				case r.Match("/inbox"):
					r.Title(a.tr("goInbox"))
					a.inboxView(c, p, "")
				case r.Match("/inbox/{id...}"):
					r.Title(a.tr("goInbox"))
					a.inboxView(c, p, r.Param("id"))
				case r.Match("/todos"):
					r.Title(a.tr("goTodos"))
					a.todosView(c, p, "")
				case r.Match("/todos/{id...}"):
					r.Title(a.tr("goTodos"))
					a.todosView(c, p, r.Param("id"))
				case r.Match("/knowledge"):
					r.Title(a.tr("goKnowledge"))
					a.knowledgeView(c, p, "", "")
				case r.Match("/knowledge/{kind}"):
					r.Title(a.tr("goKnowledge"))
					a.knowledgeView(c, p, r.Param("kind"), "")
				case r.Match("/knowledge/{kind}/{id...}"):
					r.Title(a.tr("goKnowledge"))
					a.knowledgeView(c, p, r.Param("kind"), r.Param("id"))
				case r.Match("/machines"):
					r.Title(a.tr("goMachines"))
					a.machinesView(c, p)
				case r.Match("/settings"):
					r.Title(a.tr("goSettings"))
					a.settingsView(c, p)
				default:
					a.personaPage(c, p, "")
				}
			})
		})
	})
	a.startView(c, p)
	a.agentDialog(c, p)
	a.paletteView(c, p)
}

// section is the sidebar item a path belongs to.
func section(path string) string {
	for _, s := range []string{"/overview", "/routines", "/agents", "/todos", "/inbox", "/knowledge", "/machines", "/settings"} {
		if path == s || len(path) > len(s) && path[:len(s)+1] == s+"/" {
			return s
		}
	}
	return "/"
}

func (a *App) sidebar(c *ui.Context, p palette) {
	d := a.conn.data
	ui.Column(c).Width(sidebarWidth).Background(p.inset).BorderWidth(0, 1, 0, 0).BorderColor(p.line).Children(func() {
		ui.Row(c).Padding(16, 16, 8, 16).Gap(8).Children(func() {
			ui.Icon(c, icon("hub")).FontSize(16).TextColor(p.ink2)
			ui.Text(c, a.HubTitle()).Bold().FontSize(heading).SingleLine().Grow(1)
		})
		page := section(a.router.Path())
		ui.Sidebar(c, &page, func() {
			ui.SidebarItem(c, "/", icon("home"), a.tr("goPersona"))
			ui.SidebarItem(c, "/overview", icon("grid"), a.tr("goOverview"))
			ui.SidebarItem(c, "/agents", icon("agent"), a.tr("goAgents")).Children(func() {
				if n := d.NeedsYou(); n > 0 {
					ui.Badge(c, itoa(n))
				}
			})
			ui.SidebarItem(c, "/todos", icon("todo"), a.tr("goTodos")).Children(func() {
				if n := d.TodosOpen(); n > 0 {
					ui.Badge(c, itoa(n)).Background(p.hover).TextColor(p.ink2)
				}
			})
			ui.SidebarItem(c, "/inbox", icon("inbox"), a.tr("goInbox")).Children(func() {
				if n := d.InboxOpen(); n > 0 {
					ui.Badge(c, itoa(n))
				}
			})
			ui.SidebarItem(c, "/routines", icon("clock"), a.tr("goRoutines"))
			ui.SidebarItem(c, "/knowledge", icon("book"), a.tr("goKnowledge"))
			ui.SidebarItem(c, "/machines", icon("machine"), a.tr("goMachines"))
			ui.SidebarItem(c, "/settings", icon("gear"), a.tr("goSettings"))
		}).Grow(1).Background(p.inset).OnChange(func() { a.Navigate(page) })
		a.connectionLine(c, p)
	})
}

// connectionLine is the foot of the sidebar: online, connecting, or offline with Retry.
func (a *App) connectionLine(c *ui.Context, p palette) {
	st := a.conn.status
	ui.Row(c).Padding(10, 16).Gap(8).BorderWidth(1, 0, 0, 0).BorderColor(p.line).Children(func() {
		dot, text := p.green, a.tr("online")
		switch {
		case st.Online:
		case st.Err != nil:
			dot, text = p.red, a.tr("offline")
		default:
			dot, text = p.ink3, a.tr("connecting")
		}
		ui.Box(c).Size(8, 8).Radius(4).Background(dot)
		ui.Text(c, text).FontSize(small).TextColor(p.ink2).Grow(1).SingleLine()
		if !st.Online && st.Err != nil {
			if ui.Button(c, a.tr("offlineRetry")).FontSize(small).Padding(2, 8).Clicked() {
				a.RetryNow()
			}
		}
	})
}

// statusBanner says the hub can't be reached when the first read failed.
func (a *App) statusBanner(c *ui.Context, p palette) {
	if a.conn.err == "" || a.conn.loaded {
		return
	}
	ui.Row(c).Padding(10, pagePad).Gap(10).Background(p.inset).BorderWidth(0, 0, 1, 0).BorderColor(p.line).Children(func() {
		ui.Icon(c, icon("warning")).TextColor(p.red)
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, a.tr("offlineTitle", "hub", a.HubTitle())).Bold()
			ui.Text(c, a.tr("offlineBody")).FontSize(small).TextColor(p.ink2)
		})
		if ui.Button(c, a.tr("offlineRetry")).Clicked() {
			a.refresh(a.conn)
			a.conn.socket.RetryNow()
		}
	})
}

// connectForm is the sign-in's state.
type connectForm struct {
	input, label, token string
	needToken           bool
	busy                bool
	err                 string
}

func (a *App) errorText(err error) string {
	var he *hubapi.Error
	switch {
	case errors.Is(err, hubs.ErrEmpty):
		return a.tr("errorEmpty")
	case errors.Is(err, hubs.ErrAddress):
		return a.tr("errorAddress")
	case errors.Is(err, hubs.ErrMachineLink):
		return a.tr("errorMachineLink")
	case errors.Is(err, hubs.ErrPinNoPort):
		return a.tr("errorPinNoPort")
	case errors.Is(err, hubs.ErrNoToken):
		return a.tr("errorNoToken")
	case errors.Is(err, errNeedToken):
		return a.tr("errorNeedToken")
	case errors.Is(err, hubapi.ErrUnauthorized):
		return a.tr("errorRefused")
	case errors.As(err, &he) && he.Message != "":
		return he.Message
	case hubapi.Offline(err):
		return a.tr("errorUnreachable")
	}
	return a.tr("errorOther", "error", err.Error())
}

// connectView is the sign-in: the hubs signed in before, and a field for a pairing link or an address.
func (a *App) connectView(c *ui.Context, p palette) {
	f := &a.connect
	ui.Scroll(c).Fill().Background(p.page).Children(func() {
		ui.Column(c).Width(440).Margin(64, ui.Auto, 32, ui.Auto).Gap(20).Children(func() {
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, a.tr("connectTitle")).FontSize(28).Bold()
				ui.Text(c, a.tr("connectSubtitle")).TextColor(p.ink2)
			})
			if len(a.Hubs.Hubs) > 0 {
				a.card(c, p, func() {
					ui.Text(c, a.tr("hubsHeading")).Bold()
					for _, h := range a.Hubs.Hubs {
						a.hubRow(c, p, h, false)
					}
				})
			}
			a.card(c, p, func() {
				ui.Text(c, a.tr("addHeading")).Bold()
				in := ui.TextInput(c.Key("connect-input"), &f.input).Placeholder(a.tr("addPlaceholder")).Label(a.tr("addPlaceholder")).AutoFocus()
				if in.Changed() {
					f.err = ""
					parsed, err := hubs.ParseInput(f.input)
					f.needToken = err == nil && parsed.Token == ""
				}
				submit := in.Submitted()
				if f.needToken {
					if ui.TextInput(c.Key("connect-token"), &f.token).Placeholder(a.tr("tokenPlaceholder")).Label(a.tr("tokenPlaceholder")).Password().Submitted() {
						submit = true
					}
				}
				ui.TextInput(c.Key("connect-label"), &f.label).Placeholder(a.tr("addLabelPlaceholder")).Label(a.tr("addLabelPlaceholder"))
				if f.err != "" {
					ui.Text(c, f.err).TextColor(p.red).FontSize(small)
				}
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if f.busy {
						ui.Spinner(c)
					}
					if ui.PrimaryButton(c, a.tr("signIn")).Disabled(f.busy).Clicked() {
						submit = true
					}
				})
				ui.Text(c, a.tr("addHint")).FontSize(small).TextColor(p.ink2)
				if submit && !f.busy {
					f.busy, f.err = true, ""
					a.SignIn(f.input, f.label, f.token, func(err error) {
						a.connect.busy = false
						if err != nil {
							a.connect.err = a.errorText(err)
							if errors.Is(err, errNeedToken) {
								a.connect.needToken = true
							}
							return
						}
						a.connect = connectForm{}
					})
				}
			})
			ui.Text(c, a.tr("versionLine", "version", a.Version)).FontSize(small).TextColor(p.ink3).Margin(0, ui.Auto)
		})
	})
}

// hubRow is a hub in a list: its name and address, Open, and Remove.
func (a *App) hubRow(c *ui.Context, p palette, h hubs.Hub, settings bool) {
	current := a.conn != nil && a.conn.hub.URL == h.URL
	ui.Row(c.Key(h.URL)).Gap(10).Padding(8, 0).Children(func() {
		ui.Icon(c, icon("hub")).TextColor(p.ink2)
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Row(c).Gap(6).Children(func() {
				ui.Text(c, h.Title()).Bold().SingleLine()
				if h.FP != "" {
					ui.Icon(c, icon("lock")).FontSize(small).TextColor(p.ink3).Tooltip(a.tr("pinnedHint"))
				}
			})
			ui.Text(c, h.URL).FontSize(small).TextColor(p.ink2).SingleLine()
		})
		if current {
			ui.Text(c, a.tr("currentHub")).FontSize(small).TextColor(p.green)
		} else if ui.Button(c, a.tr("openHub")).Clicked() {
			a.Switch(h.URL)
		}
		if settings || !current {
			if ui.Button(c, "").Label(a.tr("remove")).Tooltip(a.tr("remove")).Children(func() { ui.Icon(c, icon("x")) }).Clicked() {
				a.RemoveHub(h.URL)
			}
		}
	})
}

// card is a white box with a border, as the web app's settings groups.
func (a *App) card(c *ui.Context, p palette, fn func()) ui.Element {
	return ui.Column(c).Padding(16).Gap(12).Radius(cardRadius).Background(p.surface).Border(1, p.line).Children(fn)
}

// pageHeader is a page's title row; extra builds what goes at its end.
func (a *App) pageHeader(c *ui.Context, p palette, text, sub string, extra func()) {
	ui.Row(c).Padding(18, pagePad, 12, pagePad).Gap(12).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, text).FontSize(title).Bold().SingleLine()
			if sub != "" {
				ui.Text(c, sub).FontSize(small).TextColor(p.ink2).SingleLine()
			}
		})
		if extra != nil {
			extra()
		}
	})
}

// empty is a page's message when it has nothing to show.
func (a *App) empty(c *ui.Context, p palette, ic, text, sub string) {
	ui.Column(c).Grow(1).Center().Gap(8).Padding(32).Children(func() {
		ui.Icon(c, icon(ic)).FontSize(32).TextColor(p.ink3)
		ui.Text(c, text).Bold().TextColor(p.ink2)
		if sub != "" {
			ui.Text(c, sub).FontSize(small).TextColor(p.ink3).TextAlign(ui.Center).MaxWidth(360)
		}
	})
}
