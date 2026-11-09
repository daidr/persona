package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// ── An extension's own settings ──

func (a *App) openExtension(id string) {
	g := &a.sets.m8
	g.ext, g.extD, g.extErr, g.extDraft, g.extSecret = id, nil, "", map[string]string{}, map[string]string{}
	a.readExtension()
}

func (a *App) readExtension() {
	g := &a.sets.m8
	cn, root, id := a.conn, a.sets.extRoot, g.ext
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		d, err := cn.client.ExtensionDetail(ctx, root, id)
		a.Host.Post(func() {
			if !a.live(cn) || g.ext != id {
				return
			}
			if err != nil {
				g.extErr = a.tr("extDetailError", "error", a.errorText(err))
				return
			}
			a.showExtension(d)
		})
	})
}

func (a *App) showExtension(d hubapi.ExtDetail) {
	g := &a.sets.m8
	g.extD, g.extDraft = &d, map[string]string{}
	if d.Form != nil {
		fillDraft(g.extDraft, "", d.Form.Fields, d.Config)
	}
}

// fillDraft puts the settings' values (or the fields' defaults) into the form, by dotted name.
func fillDraft(draft map[string]string, prefix string, fields []hubapi.ExtField, cfg map[string]any) {
	for _, f := range fields {
		name := prefix + f.Name
		v, ok := cfg[f.Name]
		if !ok {
			v = f.Default
		}
		if f.Type == "object" {
			sub, _ := v.(map[string]any)
			fillDraft(draft, name+".", f.Fields, sub)
			continue
		}
		draft[name] = valueText(v)
	}
}

func valueText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case []any:
		var parts []string
		for _, e := range x {
			parts = append(parts, valueText(e))
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// configOf is what Save sends: the form's values by the types the schema names; bad names a number that isn't one.
func configOf(draft map[string]string, prefix string, fields []hubapi.ExtField) (cfg map[string]any, bad bool) {
	cfg = map[string]any{}
	for _, f := range fields {
		name := prefix + f.Name
		if f.Type == "object" {
			sub, b := configOf(draft, name+".", f.Fields)
			cfg[f.Name], bad = sub, bad || b
			continue
		}
		s := strings.TrimSpace(draft[name])
		switch f.Type {
		case "boolean":
			cfg[f.Name] = s == "true"
		case "integer", "number":
			if s == "" {
				continue
			}
			n, err := strconv.ParseFloat(s, 64)
			if err != nil || (f.Type == "integer" && n != float64(int64(n))) {
				bad = true
				continue
			}
			cfg[f.Name] = n
		case "array":
			list := []any{}
			for _, line := range strings.FieldsFunc(draft[name], func(r rune) bool { return r == '\n' || r == ',' }) {
				if line = strings.TrimSpace(line); line != "" {
					list = append(list, line)
				}
			}
			cfg[f.Name] = list
		default:
			if e := enumValue(f.Enum, s); e != nil {
				cfg[f.Name] = e
			} else {
				cfg[f.Name] = s
			}
		}
	}
	return cfg, bad
}

// enumValue is the choice whose value reads as s (choices can be numbers).
func enumValue(choices []hubapi.ExtChoice, s string) any {
	for _, ch := range choices {
		if valueText(ch.Value) == s {
			return ch.Value
		}
	}
	return nil
}

func (a *App) extensionView(c *ui.Context, p palette) {
	g := &a.sets.m8
	ui.Row(c).Gap(8).Children(func() {
		if ui.Button(c, "‹ "+a.tr("settingsExtensions")).Clicked() {
			g.ext, g.extD = "", nil
		}
	})
	if g.extErr != "" {
		ui.Text(c, g.extErr).TextColor(p.red)
		return
	}
	d := g.extD
	if d == nil {
		ui.Spinner(c).Margin(24, ui.Auto)
		return
	}
	ui.Row(c).Gap(8).Children(func() {
		name := d.Name
		if name == "" {
			name = d.ID
		}
		ui.Text(c, name).FontSize(title).Bold().Shrink(1).SingleLine()
		text, col := a.extStatus(p, d.Status)
		a.pill(c, text, col)
	})
	if d.Description != "" {
		ui.Text(c, d.Description).TextColor(p.ink2)
	}
	if d.Error != "" {
		ui.Text(c, d.Error).FontSize(small).TextColor(p.red).Selectable()
	}
	if d.ReadOnly {
		ui.Text(c, a.tr("extReadOnly")).FontSize(small).TextColor(p.ink2)
	}
	if d.Form != nil && len(d.Form.Fields) > 0 {
		a.group(c, p, a.tr("extSettings"), "", func() {
			a.extFields(c, p, "", d.Form.Fields, d.ReadOnly)
			if !d.ReadOnly {
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, a.tr("extRevert")).Disabled(g.extBusy).Clicked() {
						a.showExtension(*d)
					}
					if ui.PrimaryButton(c, a.tr("extSave")).Disabled(g.extBusy).Clicked() {
						a.saveExtension()
					}
				})
			}
		})
	}
	if len(d.Secrets) > 0 {
		a.group(c, p, a.tr("extSecretsTitle"), a.tr("extSecretsFooter"), func() {
			for _, s := range d.Secrets {
				a.extSecretRow(c.Key("sec-"+s.Name), p, s, d.ReadOnly)
			}
		})
	}
	if len(d.Permissions) > 0 {
		a.group(c, p, a.tr("extPermissionsTitle"), a.tr("extPermissionsFooter"), func() {
			for _, pm := range d.Permissions {
				ui.Column(c.Key("perm-" + pm.Key)).Gap(2).Children(func() {
					ui.Text(c, "· "+pm.Title)
					if pm.Detail != "" {
						ui.Text(c, pm.Detail).FontSize(small).TextColor(p.ink3).Padding(0, 0, 0, 10)
					}
				})
			}
		})
	}
	a.group(c, p, a.tr("extAboutTitle"), "", func() {
		a.valueRow(c, p, a.tr("extVersion"), d.Version)
		a.valueRow(c, p, a.tr("extSource"), d.Source)
		a.valueRow(c, p, a.tr("extInstalledFrom"), strings.TrimPrefix(d.Origin, "market:"))
		a.valueRow(c, p, a.tr("extPublisher"), d.Publisher)
		a.valueRow(c, p, a.tr("extHomepage"), d.Homepage)
	})
	if d.FromMarket() {
		ui.Row(c).Children(func() {
			if ui.Button(c, a.tr("mktUninstall")).TextColor(p.red).Clicked() {
				g.extAskOut = true
			}
		})
	}
	if g.extAskOut {
		ui.Modal(c, &g.extAskOut, func() {
			ui.Column(c).Width(440).Padding(20).Gap(12).Children(func() {
				ui.Text(c, a.tr("mktUninstallTitle", "name", d.Name)).FontSize(heading).Bold()
				ui.Text(c, a.tr("mktUninstallBody")).TextColor(p.ink2)
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, a.tr("cancel")).Clicked() {
						g.extAskOut = false
					}
					if ui.PrimaryButton(c, a.tr("mktUninstall")).Background(p.red).Disabled(g.extBusy).Clicked() {
						a.uninstallExtension(d.ID, d.Name)
					}
				})
			})
		})
	}
}

func (a *App) extFields(c *ui.Context, p palette, prefix string, fields []hubapi.ExtField, readOnly bool) {
	g := &a.sets.m8
	for _, f := range fields {
		name := prefix + f.Name
		if f.Type == "object" {
			ui.Column(c.Key("obj-"+name)).Gap(8).Padding(0, 0, 0, 12).BorderWidth(0, 0, 0, 2).BorderColor(p.line).Children(func() {
				ui.Text(c, f.Label()).Bold()
				a.extFields(c, p, name+".", f.Fields, readOnly)
			})
			continue
		}
		ui.Column(c.Key("fld-" + name)).Gap(4).Children(func() {
			v := g.extDraft[name]
			switch {
			case f.Type == "boolean":
				on := v == "true"
				ui.Row(c).Gap(12).Children(func() {
					ui.Text(c, f.Label()).Grow(1).Shrink(1)
					if ui.Switch(c.Key("sw-"+name), &on).Label(f.Label()).Disabled(readOnly).Changed() {
						g.extDraft[name] = strconv.FormatBool(on)
					}
				})
			case len(f.Enum) > 0:
				ui.Text(c, f.Label()).FontSize(small).Bold().TextColor(p.ink2)
				var titles []string
				cur := ""
				for _, ch := range f.Enum {
					t := ch.Title
					if t == "" {
						t = valueText(ch.Value)
					}
					titles = append(titles, t)
					if valueText(ch.Value) == v {
						cur = t
					}
				}
				if ui.Select(c.Key("sel-"+name), &cur, titles).Disabled(readOnly).Changed() {
					for i, t := range titles {
						if t == cur {
							g.extDraft[name] = valueText(f.Enum[i].Value)
						}
					}
				}
			case f.Type == "array":
				ui.Text(c, f.Label()).FontSize(small).Bold().TextColor(p.ink2)
				ui.TextArea(c.Key("arr-"+name), &v).Lines(2, 6).Placeholder(a.tr("extAddPlaceholder")).Disabled(readOnly)
				g.extDraft[name] = v
			default:
				ui.Text(c, f.Label()).FontSize(small).Bold().TextColor(p.ink2)
				in := ui.TextInput(c.Key("in-"+name), &v).Label(f.Label()).Placeholder(f.Label()).Disabled(readOnly)
				if f.Format == "password" {
					in.Password()
				}
				g.extDraft[name] = v
			}
			if f.Description != "" {
				ui.Text(c, f.Description).FontSize(small).TextColor(p.ink3)
			}
		})
	}
}

func (a *App) saveExtension() {
	g := &a.sets.m8
	d := g.extD
	cfg, bad := configOf(g.extDraft, "", d.Form.Fields)
	if bad {
		a.toast(a.tr("extNotANumber"))
		return
	}
	cn, root, id := a.conn, a.sets.extRoot, d.ID
	g.extBusy = true
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		nd, err := cn.client.SetExtensionConfig(ctx, root, id, cfg)
		a.Host.Post(func() {
			g.extBusy = false
			if !a.live(cn) || g.ext != id {
				return
			}
			if err != nil {
				a.toast(a.tr("extNotSaved", "error", a.errorText(err)))
				return
			}
			a.showExtension(nd)
			a.sets.extensions.asked = false
			a.toast(a.tr("extSavedRestart"))
		})
	})
}

func (a *App) extSecretRow(c *ui.Context, p palette, s hubapi.ExtSecret, readOnly bool) {
	g := &a.sets.m8
	ui.Column(c).Gap(4).Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, s.Title).Grow(1).Shrink(1)
			if s.Set {
				label := a.tr("extSet")
				if s.Hint != "" {
					label += " · …" + s.Hint
				}
				a.pill(c, label, p.green)
			}
		})
		if s.Description != "" {
			ui.Text(c, s.Description).FontSize(small).TextColor(p.ink3)
		}
		if readOnly {
			return
		}
		v := g.extSecret[s.Name]
		ui.Row(c).Gap(8).Children(func() {
			ui.TextInput(c.Key("secv-"+s.Name), &v).Label(s.Title).Placeholder(a.tr("extSecretReplacePlaceholder")).Password().Grow(1)
			g.extSecret[s.Name] = v
			if ui.Button(c, a.tr("extSave")).Disabled(g.extBusy || strings.TrimSpace(v) == "").Clicked() {
				a.extSecretDo(s, v)
			}
			if s.Set && ui.Button(c, a.tr("extClear")).Disabled(g.extBusy).Clicked() {
				a.extSecretDo(s, "")
			}
		})
	})
}

// extSecretDo saves a secret, or with "" clears it.
func (a *App) extSecretDo(s hubapi.ExtSecret, value string) {
	g := &a.sets.m8
	cn, root, id := a.conn, a.sets.extRoot, g.ext
	g.extBusy = true
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var nd hubapi.ExtDetail
		var err error
		if value == "" {
			nd, err = cn.client.ClearExtensionSecret(ctx, root, id, s.Name)
		} else {
			nd, err = cn.client.SetExtensionSecret(ctx, root, id, s.Name, value)
		}
		a.Host.Post(func() {
			g.extBusy = false
			if !a.live(cn) || g.ext != id {
				return
			}
			if err != nil {
				a.toast(a.tr("extNotSaved", "error", a.errorText(err)))
				return
			}
			delete(g.extSecret, s.Name)
			draft := g.extDraft
			g.extD = &nd
			g.extDraft = draft
			if value == "" {
				a.toast(a.tr("extSecretCleared", "title", s.Title))
			} else {
				a.toast(a.tr("extSecretSaved", "title", s.Title))
			}
		})
	})
}

func (a *App) uninstallExtension(id, name string) {
	g := &a.sets.m8
	cn, root := a.conn, a.sets.extRoot
	g.extBusy = true
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		err := cn.client.UninstallExtension(ctx, root, id)
		a.Host.Post(func() {
			g.extBusy, g.extAskOut = false, false
			if !a.live(cn) {
				return
			}
			if err != nil {
				a.toast(a.tr("mktNotUninstalled", "error", a.errorText(err)))
				return
			}
			g.ext, g.extD = "", nil
			a.sets.extensions.asked, g.mktLoad.asked = false, false
			a.toast(a.tr("mktUninstalled", "name", name))
		})
	})
}

// ── The market ──

func (a *App) marketView(c *ui.Context, p palette) {
	g := &a.sets.m8
	root := a.sets.extRoot
	var market string
	if g.mktMarket > 0 && g.mktMarket <= len(g.markets) {
		market = g.markets[g.mktMarket-1].ID
	}
	a.load(&g.mktLoad, func(ctx context.Context, cl *hubapi.Client) (func(), error) {
		ms, items, err := cl.Catalog(ctx, root, "", market)
		return func() {
			if len(ms) > 0 {
				g.markets = ms
			}
			g.catalog = items
		}, err
	})
	ui.Row(c).Gap(8).Children(func() {
		if ui.Button(c, "‹ "+a.tr("settingsExtensions")).Clicked() {
			g.mkt = false
		}
	})
	ui.Text(c, a.tr("mktMarketTitle")).FontSize(title).Bold()
	ui.Text(c, a.tr("mktMarketIntro")).FontSize(small).TextColor(p.ink2)
	ui.SearchField(c.Key("mkt-search"), &g.mktQuery).Placeholder(a.tr("mktSearchPlaceholder"))
	if len(g.markets) > 1 {
		names := []string{a.tr("mktAll")}
		for _, m := range g.markets {
			n := m.Name
			if m.Official {
				n = a.tr("mktOfficial")
			}
			names = append(names, n)
		}
		if ui.Segmented(c.Key("mkt-market"), &g.mktMarket, names...).Changed() {
			g.mktLoad.asked = false
		}
	}
	if g.mktLoad.err != "" {
		ui.Text(c, a.tr("mktCatalogFailed", "error", g.mktLoad.err)).FontSize(small).TextColor(p.red)
		return
	}
	if !g.mktLoad.done {
		ui.Spinner(c).Margin(24, ui.Auto)
		return
	}
	q := strings.ToLower(strings.TrimSpace(g.mktQuery))
	var items []hubapi.CatalogItem
	for _, it := range g.catalog {
		hay := strings.ToLower(it.Name + " " + it.ID + " " + it.Description + " " + it.Publisher + " " + strings.Join(it.Tags, " "))
		if q == "" || strings.Contains(hay, q) {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		msg := a.tr("mktMarketEmpty")
		if q != "" {
			msg = a.tr("mktNotFound", "query", g.mktQuery)
		}
		a.card(c, p, func() { ui.Text(c, msg).FontSize(small).TextColor(p.ink3) })
		return
	}
	a.card(c, p, func() {
		for i, it := range items {
			ui.Row(c.Key("cat-"+it.Market+"/"+it.ID)).Gap(12).Padding(6, 0).AlignItems(ui.Start).BorderWidth(btop(i), 0, 0, 0).BorderColor(p.line).Children(func() {
				ui.Column(c).Grow(1).Shrink(1).Gap(3).Children(func() {
					ui.Row(c).Gap(6).Children(func() {
						ui.Text(c, it.Name).Bold().Shrink(1).SingleLine()
						ui.Text(c, it.Version).FontSize(small).TextColor(p.ink3)
						if it.Installed != "" {
							a.pill(c, a.tr("mktLibInstalledVersion", "version", it.Installed), p.green)
						}
					})
					if it.Description != "" {
						ui.Text(c, it.Description).FontSize(small).TextColor(p.ink2).MaxLines(2)
					}
					ui.Text(c, joinDot(it.Publisher, it.MarketName)).FontSize(small).TextColor(p.ink3).SingleLine()
					if it.Problem != "" {
						ui.Text(c, a.tr("mktCantInstall", "problem", it.Problem)).FontSize(small).TextColor(p.orange)
					}
				})
				switch {
				case it.Problem != "":
				case it.Upgradable:
					if ui.PrimaryButton(c, a.tr("mktUpgradeTo", "version", it.Version)).Disabled(g.pvBusy).Clicked() {
						a.previewExtension(it)
					}
				case it.Installed == "":
					if ui.PrimaryButton(c, a.tr("mktInstall")).Disabled(g.pvBusy).Clicked() {
						a.previewExtension(it)
					}
				}
			})
		}
	})
	a.previewDialog(c, p)
}

func (a *App) previewExtension(it hubapi.CatalogItem) {
	g := &a.sets.m8
	cn, root := a.conn, a.sets.extRoot
	g.pvBusy, g.pvErr, g.pvItem = true, "", it
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		pv, err := cn.client.PreviewExtension(ctx, root, it.Market, it.ID)
		a.Host.Post(func() {
			g.pvBusy = false
			if !a.live(cn) {
				return
			}
			if err != nil {
				a.toast(a.tr("mktPreviewFailed", "error", a.errorText(err)))
				return
			}
			g.pv = &pv
		})
	})
}

func (a *App) previewDialog(c *ui.Context, p palette) {
	g := &a.sets.m8
	if g.pv == nil {
		return
	}
	pv := g.pv
	open := true
	expired := hubapi.ParseTime(pv.ExpiresAt).Before(time.Now())
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(520).Padding(20).Gap(12).Children(func() {
			title := a.tr("mktInstallTitle", "name", pv.Manifest.Name)
			if pv.Upgrade {
				title = a.tr("mktUpgradeTitle", "name", pv.Manifest.Name)
			}
			ui.Text(c, title).FontSize(heading).Bold()
			ui.Text(c, joinDot(pv.Manifest.Version, pv.Manifest.Publisher)).FontSize(small).TextColor(p.ink3)
			if pv.Manifest.Description != "" {
				ui.Text(c, pv.Manifest.Description).TextColor(p.ink2)
			}
			if pv.MorePermissions {
				ui.Text(c, a.tr("mktMorePermissions")).FontSize(small).TextColor(p.orange)
			}
			head := a.tr("mktCanTitle")
			if pv.Upgrade {
				head = a.tr("mktNewVersionCan")
			}
			ui.Text(c, head).FontSize(small).Bold().TextColor(p.ink2)
			if len(pv.Permissions) == 0 {
				ui.Text(c, a.tr("mktNoPermissions")).FontSize(small).TextColor(p.ink3)
			}
			added := map[string]bool{}
			for _, x := range pv.AddedPermissions {
				added[x.Key] = true
			}
			ui.Scroll(c).MaxHeight(260).Children(func() {
				ui.Column(c).Gap(6).Children(func() {
					for _, pm := range pv.Permissions {
						ui.Column(c.Key("pv-" + pm.Key)).Gap(2).Children(func() {
							ui.Row(c).Gap(6).Children(func() {
								ui.Text(c, "· "+pm.Title).Shrink(1)
								if added[pm.Key] {
									a.pill(c, a.tr("mktAdded"), p.orange)
								}
							})
							if pm.Detail != "" {
								ui.Text(c, pm.Detail).FontSize(small).TextColor(p.ink3).Padding(0, 0, 0, 10)
							}
						})
					}
				})
			})
			ui.Text(c, a.tr("mktCanFooter")).FontSize(small).TextColor(p.ink3)
			for i, w := range pv.Warnings {
				ui.Text(c.Key("pvw-"+itoa(i)), w).FontSize(small).TextColor(p.orange)
			}
			if len(pv.Unresolved) > 0 {
				ui.Text(c, a.tr("mktUnresolved", "items", strings.Join(pv.Unresolved, ", "))).FontSize(small).TextColor(p.red)
			}
			if expired {
				ui.Text(c, a.tr("mktPreviewExpired")).FontSize(small).TextColor(p.red)
			}
			if g.pvErr != "" {
				ui.Text(c, g.pvErr).FontSize(small).TextColor(p.red)
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c, a.tr("cancel")).Clicked() {
					g.pv = nil
				}
				label := a.tr("mktLibAgreeInstall")
				if pv.Upgrade {
					label = a.tr("mktLibUpgrade")
					if pv.MorePermissions {
						label = a.tr("mktLibAgreeUpgradeMore")
					}
				}
				if ui.PrimaryButton(c, label).Disabled(g.pvBusy || expired || len(pv.Unresolved) > 0).Clicked() {
					a.installExtension()
				}
			})
		})
	})
	if !open {
		g.pv = nil
	}
}

func (a *App) installExtension() {
	g := &a.sets.m8
	pv, it := *g.pv, g.pvItem
	cn, root := a.conn, a.sets.extRoot
	upgrade := ""
	if pv.Upgrade {
		upgrade = it.ID
	}
	g.pvBusy, g.pvErr = true, ""
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		err := cn.client.InstallExtension(ctx, root, pv, upgrade)
		a.Host.Post(func() {
			g.pvBusy = false
			if !a.live(cn) {
				return
			}
			if err != nil {
				g.pvErr = a.tr("mktNotDone", "error", a.errorText(err))
				return
			}
			g.pv = nil
			g.mktLoad.asked, a.sets.extensions.asked = false, false
			if pv.Upgrade {
				a.toast(a.tr("mktUpgraded", "name", pv.Manifest.Name, "version", pv.Manifest.Version))
			} else {
				a.toast(a.tr("mktInstalled", "name", pv.Manifest.Name))
			}
		})
	})
}
