package app

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// more8 is the state of model providers, an extension's own settings and the market.
type more8 struct {
	provs     loadable
	provList  []hubapi.Provider
	provAsk   string // the provider whose Delete / Hide asks first
	wiz       providerWizard
	ext       string // the extension whose settings are open
	extD      *hubapi.ExtDetail
	extErr    string
	extBusy   bool
	extDraft  map[string]string // the form's values as typed, by dotted name
	extSecret map[string]string
	extAskOut bool // uninstall asks first
	mkt       bool // the market is open
	mktLoad   loadable
	mktQuery  string
	mktMarket int
	markets   []hubapi.Market
	catalog   []hubapi.CatalogItem
	pv        *hubapi.MarketPreview
	pvItem    hubapi.CatalogItem
	pvBusy    bool
	pvErr     string
	signOut   string
}

// ── Connectors: signing in here ──

func (a *App) connectorSignIn(c *ui.Context, p palette, x hubapi.Connector) {
	if !slices.Contains(x.Capabilities, "login") {
		return
	}
	g := &a.sets
	ui.Row(c).Gap(8).Padding(4, 0, 0, 0).Children(func() {
		if x.Account == "" || x.Status == "not_signed_in" {
			if ui.Button(c, a.tr("connSignIn")).Disabled(!x.On()).Clicked() {
				name := x.Name
				a.startSignIn(false, x.ID, a.tr("connSignInTo", "name", name), func(pr hubapi.Prompt) {
					who := pr.Account
					if who == "" {
						who = name
					}
					a.toast(a.tr("connSignedIn", "name", who))
					g.connectors.asked = false
				})
			}
			return
		}
		if ui.Button(c, a.tr("connSignOut")).Clicked() {
			g.m8.signOut = x.ID
		}
	})
}

// connectorDialogs are the sign-in and Sign out's question.
func (a *App) connectorDialogs(c *ui.Context, p palette) {
	g := &a.sets
	a.signInDialog(c, p)
	if g.m8.signOut == "" {
		return
	}
	open := true
	id, name := g.m8.signOut, g.m8.signOut
	for _, x := range g.conns {
		if x.ID == id {
			name = x.Name
		}
	}
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(400).Padding(20).Gap(12).Children(func() {
			ui.Text(c, a.tr("connSignOutTitle", "name", name)).Bold()
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c, a.tr("cancel")).Clicked() {
					g.m8.signOut = ""
				}
				if ui.PrimaryButton(c, a.tr("connSignOut")).Background(p.red).Clicked() {
					g.m8.signOut = ""
					a.change(&g.connectors, id, func(ctx context.Context, cl *hubapi.Client) error { return cl.ConnectorLogout(ctx, id) }, nil)
				}
			})
		})
	})
	if !open {
		g.m8.signOut = ""
	}
}

// ── Model providers ──

// providerWizard is Add provider: a preset (only its key) or a compatible gateway typed by hand.
type providerWizard struct {
	open    bool
	presets []hubapi.LauncherPreset
	loaded  bool
	preset  *hubapi.LauncherPreset
	gateway bool
	name    string
	baseURL string
	authKey bool // x-api-key, else Bearer
	key     string
	model   string
	models  []string
	assign  bool
	busy    string // test | discover | save
	result  string
	ok      bool
	err     string
}

func (a *App) providersGroup(c *ui.Context, p palette) {
	g := &a.sets.m8
	a.load(&g.provs, func(ctx context.Context, cl *hubapi.Client) (func(), error) {
		list, err := cl.Providers(ctx)
		return func() { g.provList = list }, err
	})
	ui.Column(c).Gap(6).Children(func() {
		ui.Row(c).Gap(8).Padding(0, 4).Children(func() {
			ui.Text(c, a.tr("provNavTabProviders")).FontSize(small).Bold().TextColor(p.ink2).Grow(1)
			if ui.Button(c, a.tr("provAddProvider")).FontSize(small).Padding(2, 8).Clicked() {
				a.openWizard()
			}
		})
		a.card(c, p, func() {
			switch {
			case g.provs.err != "":
				ui.Text(c, a.tr("provLaunchersFailed", "error", g.provs.err)).FontSize(small).TextColor(p.red)
			case !g.provs.done:
				ui.Spinner(c)
			case len(g.provList) == 0:
				ui.Text(c, a.tr("provManagedEmpty")).FontSize(small).TextColor(p.ink3)
			}
			for i, l := range g.provList {
				ui.Row(c.Key("prov-"+l.Name)).Gap(12).Padding(6, 0).BorderWidth(btop(i), 0, 0, 0).BorderColor(p.line).Children(func() {
					col := ui.Column(c).Grow(1).Shrink(1).Gap(2)
					if l.Hidden {
						col.Opacity(0.6)
					}
					col.Children(func() {
						ui.Row(c).Gap(6).Children(func() {
							ui.Text(c, l.Title()).Bold().Shrink(1).SingleLine()
							switch {
							case l.Hidden:
								a.pill(c, a.tr("provHidden"), p.ink3)
							case l.Managed():
								a.pill(c, a.tr("provManaged"), p.accent)
							default:
								a.pill(c, l.Source, p.ink2)
							}
						})
						line := hostOf(l.BaseURL)
						if l.Model != "" {
							line = joinDot(line, l.Model)
						}
						if len(l.Machines) > 0 {
							line = joinDot(line, strings.Join(l.Machines, ", "))
						}
						if line != "" {
							ui.Text(c, line).FontSize(small).TextColor(p.ink2).SingleLine()
						}
					})
					if l.Hidden {
						if ui.Button(c, a.tr("provRestore")).Clicked() {
							name := l.Name
							a.change(&g.provs, name, func(ctx context.Context, cl *hubapi.Client) error { return cl.RestoreLauncher(ctx, name) },
								func() { a.toast(a.tr("provRestored", "name", name)) })
						}
						return
					}
					label := a.tr("provHideInPersona")
					if l.Managed() {
						label = a.tr("provDeleteProvider")
					}
					if ui.Button(c, label).Clicked() {
						g.provAsk = l.Name
					}
				})
			}
		})
	})
	if g.provAsk != "" {
		open := true
		name := g.provAsk
		managed := false
		for _, l := range g.provList {
			if l.Name == name {
				managed = l.Managed()
			}
		}
		ui.Modal(c, &open, func() {
			ui.Column(c).Width(400).Padding(20).Gap(12).Children(func() {
				label := a.tr("provHideInPersona")
				if managed {
					label = a.tr("provDeleteProvider")
				}
				ui.Text(c, label+" · "+name).Bold()
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, a.tr("cancel")).Clicked() {
						g.provAsk = ""
					}
					if ui.PrimaryButton(c, label).Background(p.red).Clicked() {
						g.provAsk = ""
						a.change(&g.provs, name, func(ctx context.Context, cl *hubapi.Client) error { return cl.DeleteLauncher(ctx, name) }, func() {
							if managed {
								a.toast(a.tr("provDeleted", "name", name))
							} else {
								a.toast(a.tr("provHid", "name", name))
							}
						})
					}
				})
			})
		})
		if !open {
			g.provAsk = ""
		}
	}
	a.wizardDialog(c, p)
}

func hostOf(u string) string {
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	if i := strings.IndexByte(u, '/'); i >= 0 {
		u = u[:i]
	}
	return u
}

func (a *App) openWizard() {
	w := &a.sets.m8.wiz
	*w = providerWizard{open: true, presets: w.presets, loaded: w.loaded, assign: true}
	if w.loaded {
		return
	}
	cn := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		ps, err := cn.client.LauncherPresets(ctx)
		a.Host.Post(func() {
			if !a.live(cn) {
				return
			}
			w.loaded = true
			if err != nil {
				w.err = a.errorText(err)
				return
			}
			w.presets = ps
		})
	})
}

func (a *App) pickPreset(pr *hubapi.LauncherPreset) {
	w := &a.sets.m8.wiz
	w.preset, w.gateway, w.result, w.err = pr, pr == nil, "", ""
	w.models, w.model = nil, ""
	if pr != nil {
		for _, m := range pr.Models {
			w.models = append(w.models, m.ID)
		}
		w.model = pr.DefaultModel()
	}
}

// wizardBody is what Test and Add send.
func (a *App) wizardBody(test bool) map[string]any {
	w := &a.sets.m8.wiz
	key := strings.TrimSpace(w.key)
	if w.preset != nil {
		pr := w.preset
		if test {
			auth := pr.Auth
			if auth == "" {
				auth = "api_key"
			}
			b := map[string]any{"preset": pr.ID, "api_key": key, "auth": auth}
			if w.model != "" {
				b["model"] = w.model
			}
			return b
		}
		b := map[string]any{"api_key": key, "preset": pr.ID}
		if n := strings.TrimSpace(w.name); n != "" {
			b["name"] = n
		}
		if w.model != "" && w.model != pr.DefaultModel() {
			b["model"] = w.model
		}
		if w.assign {
			b["assign"] = []string{"assistant", "agent.new"}
		}
		return b
	}
	auth := "bearer"
	if w.authKey {
		auth = "api_key"
	}
	b := map[string]any{"base_url": strings.TrimSpace(w.baseURL), "auth": auth, "api": "messages"}
	if key != "" {
		b["api_key"] = key
	}
	if test {
		b["model"] = w.model
		return b
	}
	b["name"], b["kind"], b["model"] = strings.TrimSpace(w.name), "launcher", w.model
	var models []map[string]any
	for _, m := range w.models {
		models = append(models, map[string]any{"id": m})
	}
	if len(models) > 0 {
		b["models"] = models
	}
	return b
}

// wizardProblem is what's missing before Test / Add.
func (a *App) wizardProblem(save bool) string {
	w := &a.sets.m8.wiz
	switch {
	case strings.TrimSpace(w.key) == "":
		return a.tr("provLibKeyRequired")
	case w.gateway && strings.TrimSpace(w.baseURL) == "":
		return a.tr("provLibUrlRequired")
	case w.gateway && save && strings.TrimSpace(w.name) == "":
		return a.tr("provLibNameRequired")
	}
	return ""
}

func (a *App) wizardDo(what string) {
	w := &a.sets.m8.wiz
	if w.busy != "" {
		return
	}
	if prob := a.wizardProblem(what == "save"); prob != "" && what != "discover" {
		w.err = prob
		return
	}
	cn := a.conn
	body := a.wizardBody(what != "save")
	w.busy, w.err, w.result = what, "", ""
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var res hubapi.TestResult
		var found []hubapi.PresetModel
		var assigned []string
		var err error
		switch what {
		case "test":
			res, err = cn.client.TestLauncher(ctx, body)
		case "discover":
			delete(body, "model")
			found, err = cn.client.DiscoverModels(ctx, body)
		default:
			assigned, err = cn.client.AddLauncher(ctx, body)
		}
		a.Host.Post(func() {
			if !a.live(cn) {
				return
			}
			w.busy = ""
			switch {
			case err != nil && what == "discover":
				w.err = a.tr("wizDiscoverFailed", "error", a.errorText(err))
			case err != nil:
				w.err = a.errorText(err)
			case what == "test":
				w.ok = res.OK
				if res.OK {
					w.result = a.tr("provLibTestOk", "ms", itoa(res.MS))
				} else if res.Error != "" {
					w.result = res.Error
				} else {
					w.result = a.tr("provLibTestFailed")
				}
			case what == "discover":
				for _, m := range found {
					if !slices.Contains(w.models, m.ID) {
						w.models = append(w.models, m.ID)
					}
				}
				if w.model == "" && len(w.models) > 0 {
					w.model = w.models[0]
				}
				w.ok, w.result = true, a.tr("wizDiscoverFound", "count", itoa(len(found)))
			default:
				name := strings.TrimSpace(w.name)
				if name == "" && w.preset != nil {
					name = w.preset.Title()
				}
				msg := a.tr("wizAdded", "name", name)
				if len(assigned) > 0 {
					var ps []string
					for _, x := range assigned {
						switch x {
						case "assistant":
							ps = append(ps, a.tr("presetPurposeAssistant"))
						case "agent.new":
							ps = append(ps, a.tr("presetPurposeAgentNew"))
						default:
							ps = append(ps, x)
						}
					}
					msg += " " + a.tr("presetAssigned", "purposes", strings.Join(ps, ", "))
				}
				a.toast(msg)
				w.open = false
				a.sets.m8.provs.asked = false
				a.sets.models.asked = false
			}
		})
	})
}

func (a *App) wizardDialog(c *ui.Context, p palette) {
	w := &a.sets.m8.wiz
	if !w.open {
		return
	}
	ui.Modal(c, &w.open, func() {
		ui.Column(c).Width(520).Padding(20).Gap(12).Children(func() {
			ui.Text(c, a.tr("wizTitleNew")).FontSize(heading).Bold()
			if w.preset == nil && !w.gateway {
				a.wizardPick(c, p)
				return
			}
			a.wizardForm(c, p)
		})
	})
}

func (a *App) wizardPick(c *ui.Context, p palette) {
	w := &a.sets.m8.wiz
	ui.Text(c, a.tr("wizStepPick")).FontSize(small).Bold().TextColor(p.ink2)
	if !w.loaded {
		ui.Spinner(c).Margin(12, ui.Auto)
		return
	}
	ui.Scroll(c).MaxHeight(420).Children(func() {
		ui.Column(c).Gap(4).Children(func() {
			for i := range w.presets {
				pr := &w.presets[i]
				sub := pr.Description
				if sub == "" {
					sub = hostOf(pr.BaseURL)
				}
				kind := a.tr("presetKindLauncher")
				if pr.Kind == "api" {
					kind = a.tr("presetKindApi")
				}
				a.linkRow(c, p, "preset-"+pr.ID, pr.Title(), sub, kind, func() { a.pickPreset(pr) })
			}
			a.linkRow(c, p, "preset-custom", a.tr("wizCustom"), a.tr("wizCustomDetail"), "", func() { a.pickPreset(nil) })
		})
	})
	if w.err != "" {
		ui.Text(c, w.err).FontSize(small).TextColor(p.red)
	}
	ui.Row(c).Children(func() {
		ui.Spacer(c)
		if ui.Button(c, a.tr("cancel")).Clicked() {
			w.open = false
		}
	})
}

func (a *App) wizardForm(c *ui.Context, p palette) {
	w := &a.sets.m8.wiz
	title := a.tr("provGateway")
	if w.preset != nil {
		title = w.preset.Title()
	}
	ui.Text(c, title).Bold()
	if w.gateway {
		ui.TextInput(c.Key("wiz-name"), &w.name).Label(a.tr("wizName")).Placeholder("my_gateway")
		ui.Text(c, a.tr("wizNameHint")).FontSize(small).TextColor(p.ink3)
		ui.TextInput(c.Key("wiz-url"), &w.baseURL).Label(a.tr("wizBaseUrl")).Placeholder("https://gateway.example.com")
		ui.Text(c, a.tr("wizUrlGatewayHint")).FontSize(small).TextColor(p.ink3)
		auth := 0
		if w.authKey {
			auth = 1
		}
		ui.Text(c, a.tr("wizAuth")).FontSize(small).Bold().TextColor(p.ink2)
		if ui.Segmented(c.Key("wiz-auth"), &auth, "Bearer", "x-api-key").Changed() {
			w.authKey = auth == 1
		}
		hint := a.tr("wizAuthBearerHint")
		if w.authKey {
			hint = a.tr("wizAuthKeyHint")
		}
		ui.Text(c, hint).FontSize(small).TextColor(p.ink3)
	}
	ui.TextInput(c.Key("wiz-key"), &w.key).Label(a.tr("provKey")).Placeholder(a.tr("provKey")).Password()
	hint := a.tr("wizKeyHint")
	if w.preset != nil && w.preset.KeyHint != "" {
		hint = w.preset.KeyHint + " " + hint
	}
	ui.Text(c, hint).FontSize(small).TextColor(p.ink3)
	if len(w.models) > 0 {
		ui.Text(c, a.tr("wizTestModel")).FontSize(small).Bold().TextColor(p.ink2)
		ui.Select(c.Key("wiz-model"), &w.model, w.models)
	} else if w.gateway {
		ui.TextInput(c.Key("wiz-model-typed"), &w.model).Label(a.tr("wizTestModel")).Placeholder("claude-sonnet-5-5")
	}
	if w.preset != nil && w.preset.Kind != "api" {
		a.switchRow(c, p, a.tr("provAssign"), "", &w.assign)
	}
	ui.Row(c).Gap(8).Children(func() {
		if ui.Button(c, a.tr("wizTest")).Tooltip(a.tr("wizTestCost")).Disabled(w.busy != "").Clicked() {
			a.wizardDo("test")
		}
		if w.gateway && ui.Button(c, a.tr("wizDiscover")).Disabled(w.busy != "" || strings.TrimSpace(w.baseURL) == "").Clicked() {
			a.wizardDo("discover")
		}
		if w.busy != "" {
			ui.Spinner(c)
		}
	})
	if w.result != "" {
		col := p.green
		if !w.ok {
			col = p.red
		}
		ui.Text(c, w.result).FontSize(small).TextColor(col)
	}
	if w.err != "" {
		ui.Text(c, w.err).FontSize(small).TextColor(p.red)
	}
	ui.Row(c).Gap(8).Children(func() {
		if ui.Button(c, a.tr("wizBack")).Disabled(w.busy != "").Clicked() {
			w.preset, w.gateway, w.err, w.result = nil, false, "", ""
		}
		ui.Spacer(c)
		if ui.Button(c, a.tr("cancel")).Clicked() {
			w.open = false
		}
		if ui.PrimaryButton(c, a.tr("provAddProvider")).Disabled(w.busy != "").Clicked() {
			a.wizardDo("save")
		}
	})
}
