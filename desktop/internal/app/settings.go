package app

import (
	"github.com/egoist/mygo/ui"
)

// settingsView is Settings: this app's own (General) and the hub's devices, extensions, connectors and models,
// each read when first shown.
func (a *App) settingsView(c *ui.Context, p palette) {
	g := &a.sets
	if g.conn != a.conn {
		*g = settingsPage{conn: a.conn, tab: g.tab}
	}
	a.pageHeader(c, p, a.tr("goSettings"), "", func() {
		ui.Segmented(c.Key("settings-tab"), &g.tab, a.tr("settingsGeneral"), a.tr("settingsDevices"), a.tr("settingsExtensions"),
			a.tr("settingsConnectors"), a.tr("settingsModels"))
	})
	ui.Divider(c)
	ui.Scroll(c.Key("settings-" + itoa(g.tab))).Grow(1).Children(func() {
		ui.Column(c).MaxWidth(720).Padding(16, pagePad, 32, pagePad).Gap(16).Children(func() {
			switch g.tab {
			case 1:
				a.devicesView(c, p)
			case 2:
				a.extensionsView(c, p)
			case 3:
				a.connectorsView(c, p)
			case 4:
				a.modelsView(c, p)
			default:
				a.generalSettings(c, p)
			}
		})
	})
}

// generalSettings holds the hubs, adding one, this app's own settings and signing out.
func (a *App) generalSettings(c *ui.Context, p palette) {
	s := &a.Hubs.Settings
	a.card(c, p, func() {
		ui.Text(c, a.tr("hubsHeading")).Bold()
		for _, h := range a.Hubs.Hubs {
			a.hubRow(c, p, h, true)
		}
		ui.Row(c).Children(func() {
			if ui.Button(c, a.tr("addHeading")).Clicked() {
				a.Stop()
				a.connect = connectForm{}
			}
		})
	})
	a.card(c, p, func() {
		ui.Text(c, a.tr("appHeading")).Bold()
		notify := !s.NoNotifications
		if a.switchRow(c, p, a.tr("settingNotifications"), a.tr("settingNotificationsHint"), &notify) {
			s.NoNotifications = !notify
			_ = a.Hubs.Save()
		}
		login := a.Host.OpenAtLogin()
		if a.switchRow(c, p, a.tr("settingOpenAtLogin"), "", &login) {
			if err := a.Host.SetOpenAtLogin(login); err != nil {
				a.toast(a.errorText(err))
			}
		}
		if sc := a.Host.Shortcut(); sc != "" {
			on := !s.NoShortcut
			if a.switchRow(c, p, a.tr("settingShortcut", "shortcut", sc), "", &on) {
				s.NoShortcut = !on
				_ = a.Hubs.Save()
				a.Host.SetShortcut(on)
			}
		}
	})
	a.card(c, p, func() {
		ui.Text(c, a.tr("accountHeading")).Bold()
		ui.Text(c, a.tr("signedInAs", "device", DeviceName(), "hub", a.HubTitle())).FontSize(small).TextColor(p.ink2)
		ui.Row(c).Children(func() {
			if ui.Button(c, a.tr("signOut")).TextColor(p.red).Clicked() {
				a.SignOut()
			}
		})
	})
	ui.Text(c, a.tr("versionLine", "version", a.Version)).FontSize(small).TextColor(p.ink3)
}

// switchRow is a setting with a switch; it reports a change.
func (a *App) switchRow(c *ui.Context, p palette, label, hint string, on *bool) bool {
	changed := false
	ui.Row(c.Key(label)).Gap(12).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, label)
			if hint != "" {
				ui.Text(c, hint).FontSize(small).TextColor(p.ink2)
			}
		})
		changed = ui.Switch(c, on).Label(label).Changed()
	})
	return changed
}
