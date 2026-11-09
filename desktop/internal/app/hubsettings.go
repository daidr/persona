package app

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// settingsPage is Settings' state: the tab shown and what each of the hub's tabs read, for the hub it was read from.
type settingsPage struct {
	conn *conn
	tab  int

	devices    loadable
	tokens     []hubapi.DeviceToken
	current    string
	revoking   string // the device whose Revoke asks to be sure
	pairOpen   bool
	pair       *hubapi.PairCode
	pairQR     ui.ImageSource
	pairErr    string
	pairKnown  map[string]bool
	pairedName string
	polling    bool

	extensions loadable
	exts       []hubapi.Extension
	extRoot    string

	connectors loadable
	conns      []hubapi.Connector

	models    loadable
	modelCfg  hubapi.ModelConfig
	modelEdit string // the use being changed
	modelProv string
	modelName string

	persona   loadable
	asst      hubapi.AssistantSettings
	asstTasks []hubapi.Task
	notes     string
	notesBusy bool
	memory    []hubapi.MemoryFile
	memoryErr string
	memOpen   *hubapi.MemoryEntry
	memIndex  bool
	memEdit   string
	memEditing,
	memAsk bool
	memBusy  bool
	skills   []hubapi.Skill
	skillsOn string
	skillErr string
	asstBusy string

	triage      loadable
	triageSet   hubapi.Triage
	triageNone  bool
	profiles    hubapi.ModelProfiles
	profilesOK  bool
	profileOpen string
	profileBusy bool
	profileErr  string
}

// loadable is a tab's reading: not yet, under way, done, or failed with a reason.
type loadable struct {
	asked, busy, done bool
	err               string
	// busyOn is the row a change is under way for.
	busyOn string
}

// load reads a tab once (again after Retry): fetch runs off the main thread, then apply on it.
func (a *App) load(l *loadable, fetch func(ctx context.Context, cl *hubapi.Client) (func(), error)) {
	if l.asked {
		return
	}
	l.asked, l.busy = true, true
	c := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		apply, err := fetch(ctx, c.client)
		a.Host.Post(func() {
			if !a.live(c) {
				return
			}
			l.busy = false
			if err != nil {
				l.err = a.errorText(err)
				return
			}
			l.err, l.done = "", true
			apply()
		})
	})
}

// change runs a change to one row, says what went wrong, and reads the tab again after it.
func (a *App) change(l *loadable, row string, do func(ctx context.Context, cl *hubapi.Client) error, after func()) {
	if l.busyOn != "" {
		return
	}
	l.busyOn = row
	c := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := do(ctx, c.client)
		a.Host.Post(func() {
			l.busyOn = ""
			if !a.live(c) {
				return
			}
			if err != nil {
				a.toast(a.errorText(err))
			}
			l.asked = false
			if after != nil {
				after()
			}
		})
	})
}

// loadState draws a tab still being read, or failed with Retry; it reports whether the tab can be drawn.
func (a *App) loadState(c *ui.Context, p palette, l *loadable) bool {
	switch {
	case l.err != "" && !l.busy:
		ui.Row(c).Gap(12).Children(func() {
			ui.Text(c, l.err).TextColor(p.red).Grow(1).Shrink(1)
			if ui.Button(c, a.tr("offlineRetry")).Clicked() {
				l.asked, l.err = false, ""
			}
		})
		return false
	case !l.done:
		ui.Spinner(c).Margin(32, ui.Auto)
		return false
	}
	return true
}

// ── Devices ──

func (a *App) devicesView(c *ui.Context, p palette) {
	g := &a.sets
	a.load(&g.devices, func(ctx context.Context, cl *hubapi.Client) (func(), error) {
		list, current, err := cl.Tokens(ctx)
		return func() {
			g.tokens, g.current = list, current
			a.checkPaired()
		}, err
	})
	if !a.loadState(c, p, &g.devices) {
		a.pairView(c, p)
		return
	}
	var active, revoked []hubapi.DeviceToken
	for _, t := range g.tokens {
		if t.RevokedAt != "" {
			revoked = append(revoked, t)
		} else {
			active = append(active, t)
		}
	}
	slices.SortStableFunc(active, func(x, y hubapi.DeviceToken) int {
		switch {
		case x.ID == g.current:
			return -1
		case y.ID == g.current:
			return 1
		}
		return hubapi.ParseTime(y.LastSeen).Compare(hubapi.ParseTime(x.LastSeen))
	})
	now := time.Now()
	a.card(c, p, func() {
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, a.tr("devicesTitle", "count", itoa(len(active)))).Bold().Grow(1)
			if ui.Button(c, a.tr("devicesAdd")).Clicked() {
				a.openPair()
			}
		})
		ui.Text(c, a.tr("devicesFooter")).FontSize(small).TextColor(p.ink2)
		if len(active) == 0 {
			ui.Text(c, a.tr("devicesNone")).FontSize(small).TextColor(p.ink3)
		}
		for _, t := range active {
			a.deviceRow(c, p, t, false, now)
		}
	})
	if len(revoked) > 0 {
		a.card(c, p, func() {
			ui.Text(c, a.tr("devicesRevoked", "count", itoa(len(revoked)))).Bold()
			for _, t := range revoked {
				a.deviceRow(c, p, t, true, now)
			}
		})
	}
	a.pairView(c, p)
}

// when puts a time into one of two texts: ago ("used {time} ago", at least a minute) within a week, on ("used on
// {time}", a date) after that. Both come translated, with {time} left in.
func (a *App) when(at string, now time.Time, ago, on string) string {
	t := hubapi.ParseTime(at)
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	if d >= 7*24*time.Hour {
		return strings.ReplaceAll(on, "{time}", t.Local().Format("2006-01-02"))
	}
	if d < time.Minute {
		d = time.Minute
	}
	return strings.ReplaceAll(ago, "{time}", a.ago(now.Add(-d).Format(time.RFC3339), now))
}

func (a *App) deviceKind(kind string) string {
	switch kind {
	case "phone":
		return a.tr("deviceKindPhone")
	case "watch":
		return a.tr("deviceKindWatch")
	case "web":
		return a.tr("deviceKindWeb")
	case "desktop":
		return a.tr("deviceKindDesktop")
	case "other":
		return a.tr("deviceKindCLI")
	}
	return kind
}

func (a *App) deviceRow(c *ui.Context, p palette, t hubapi.DeviceToken, gone bool, now time.Time) {
	g := &a.sets
	ui.Row(c.Key("dev-"+t.ID)).Gap(10).Padding(4, 0).Children(func() {
		ink := p.ink
		if gone {
			ink = p.ink3
		}
		ic := "machine"
		if t.Kind == "phone" || t.Kind == "watch" {
			ic = "phone"
		}
		ui.Icon(c, icon(ic)).TextColor(p.ink3)
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Row(c).Gap(6).Children(func() {
				name := t.Name
				if name == "" {
					name = t.ID
				}
				ui.Text(c, name).TextColor(ink).SingleLine().Shrink(1)
				if t.ID == g.current {
					a.pill(c, a.tr("deviceThis"), p.green)
				}
				if t.Role != "" && t.Role != "full" {
					role := t.Role
					if role == "answer" {
						role = a.tr("deviceRoleAnswer")
					}
					a.pill(c, role, p.ink2)
				}
			})
			line := a.deviceKind(t.Kind)
			if gone && t.RevokedAt != "" {
				line += " · " + a.when(t.RevokedAt, now, a.tr("deviceRevokedAgo"), a.tr("deviceRevokedOn"))
			}
			if !gone && t.LastSeen != "" {
				line += " · " + a.when(t.LastSeen, now, a.tr("deviceUsedAgo"), a.tr("deviceUsedOn"))
			}
			if t.CreatedAt != "" {
				line += " · " + a.when(t.CreatedAt, now, a.tr("deviceAddedAgo"), a.tr("deviceAddedOn"))
			}
			ui.Text(c, line).FontSize(small).TextColor(p.ink3).SingleLine()
		})
		if gone || t.ID == g.current {
			return
		}
		if g.revoking == t.ID {
			if ui.Button(c, a.tr("cancel")).Clicked() {
				g.revoking = ""
			}
			if ui.Button(c, a.tr("deviceRevokeConfirm")).TextColor(p.red).Disabled(g.devices.busyOn != "").Clicked() {
				id := t.ID
				a.change(&g.devices, id, func(ctx context.Context, cl *hubapi.Client) error { return cl.RevokeToken(ctx, id) },
					func() { g.revoking = "" })
			}
			return
		}
		if ui.Button(c, a.tr("deviceRevoke")).TextColor(p.red).Clicked() {
			g.revoking = t.ID
		}
	})
}

// openPair asks the hub for a pairing link and shows it, with its QR code, until a new device uses it.
func (a *App) openPair() {
	g := &a.sets
	g.pairOpen, g.pair, g.pairQR, g.pairErr, g.pairedName = true, nil, nil, "", ""
	g.pairKnown = map[string]bool{}
	for _, t := range g.tokens {
		g.pairKnown[t.ID] = true
	}
	c := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		code, err := c.client.NewPairCode(ctx)
		a.Host.Post(func() {
			if !a.live(c) || !g.pairOpen {
				return
			}
			if err != nil {
				g.pairErr = a.errorText(err)
				return
			}
			g.pair = &code
			if code.QR != nil {
				g.pairQR = qrImage(code.QR.Rows)
			}
		})
	})
}

// pollPaired reads the devices again in 3 s while the pairing link waits, to say which device took it; the dialog
// arms it once a frame at most.
func (a *App) pollPaired() {
	g := &a.sets
	if g.polling {
		return
	}
	g.polling = true
	c := a.conn
	a.After(3*time.Second, func() {
		g.polling = false
		if a.live(c) && g.pairOpen && g.pairedName == "" {
			g.devices.asked = false
		}
	})
}

// checkPaired notes a device that wasn't there when the link was made.
func (a *App) checkPaired() {
	g := &a.sets
	if !g.pairOpen || g.pair == nil || g.pairedName != "" {
		return
	}
	for _, t := range g.tokens {
		if !g.pairKnown[t.ID] && t.RevokedAt == "" {
			g.pairedName = t.Name
			if g.pairedName == "" {
				g.pairedName = t.ID
			}
		}
	}
}

func (a *App) pairView(c *ui.Context, p palette) {
	g := &a.sets
	if !g.pairOpen {
		return
	}
	ui.Modal(c, &g.pairOpen, func() {
		ui.Column(c).Width(420).Padding(20).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.tr("pairTitle")).FontSize(heading).Bold()
			switch {
			case g.pairErr != "":
				ui.Text(c, g.pairErr).TextColor(p.red).TextAlign(ui.Center)
				if ui.Button(c, a.tr("offlineRetry")).Clicked() {
					a.openPair()
				}
			case g.pair == nil:
				ui.Spinner(c).Margin(24)
			case g.pairedName != "":
				ui.Icon(c, icon("check")).FontSize(28).TextColor(p.green)
				ui.Text(c, a.tr("pairDone", "name", g.pairedName)).Bold().TextAlign(ui.Center)
			default:
				left := time.Until(hubapi.ParseTime(g.pair.ExpiresAt))
				if left <= 0 {
					ui.Text(c, a.tr("pairExpired")).TextColor(p.ink2)
					if ui.PrimaryButton(c, a.tr("pairNew")).Clicked() {
						a.openPair()
					}
					break
				}
				if g.pairQR != nil {
					// a QR code stays black on white in dark mode too, so cameras read it
					ui.Box(c).Padding(8).Radius(8).Background(ui.Hex("#ffffff")).Children(func() {
						ui.Image(c, g.pairQR).Size(196, 196).Label(a.tr("pairQR"))
					})
				}
				ui.Text(c, a.tr("pairHint")).FontSize(small).TextColor(p.ink2).TextAlign(ui.Center).MaxWidth(340)
				if ui.Button(c, a.tr("pairCopy")).Clicked() {
					c.WriteClipboard(g.pair.PairURL)
					a.toast(a.tr("pairCopied"))
				}
				ui.Text(c, a.tr("pairExpires", "time", clock(left))).FontSize(small).TextColor(p.ink3)
				// the countdown moves once a second; the devices are read again to see the link used
				c.After(time.Second)
				a.pollPaired()
			}
			ui.Row(c).Children(func() {
				if ui.PrimaryButton(c, a.tr("pairDoneButton")).Clicked() {
					g.pairOpen = false
				}
			})
		})
	})
}

// clock is m:ss.
func clock(d time.Duration) string {
	s := int(d.Round(time.Second) / time.Second)
	sec := s % 60
	out := itoa(s/60) + ":"
	if sec < 10 {
		out += "0"
	}
	return out + itoa(sec)
}

// qrImage draws a QR code's rows, with its quiet zone, as a picture the window scales up crisply enough.
func qrImage(rows []string) ui.ImageSource {
	n := len(rows)
	if n == 0 {
		return nil
	}
	const scale, quiet = 8, 4
	side := (n + 2*quiet) * scale
	img := image.NewGray(image.Rect(0, 0, side, side))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for y, row := range rows {
		for x, ch := range row {
			if ch != '1' {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.SetGray((x+quiet)*scale+dx, (y+quiet)*scale+dy, color.Gray{})
				}
			}
		}
	}
	var buf bytes.Buffer
	if png.Encode(&buf, img) != nil {
		return nil
	}
	src, err := ui.DecodeImage(buf.Bytes())
	if err != nil {
		return nil
	}
	return src
}

// ── Extensions ──

func (a *App) extensionsView(c *ui.Context, p palette) {
	g := &a.sets
	a.load(&g.extensions, func(ctx context.Context, cl *hubapi.Client) (func(), error) {
		list, root, err := cl.Extensions(ctx)
		return func() { g.exts, g.extRoot = list, root }, err
	})
	if !a.loadState(c, p, &g.extensions) {
		return
	}
	a.card(c, p, func() {
		ui.Text(c, a.tr("extensionsTitle", "count", itoa(len(g.exts)))).Bold()
		ui.Text(c, a.tr("extensionsFooter")).FontSize(small).TextColor(p.ink2)
		if len(g.exts) == 0 {
			ui.Text(c, a.tr("extensionsNone")).FontSize(small).TextColor(p.ink3)
		}
		// ids can repeat: each manager has its own extensions
		for i, x := range g.exts {
			a.extensionRow(c.Key("ext-"+itoa(i)), p, x)
		}
	})
}

func (a *App) extStatus(p palette, status string) (string, ui.Color) {
	switch status {
	case "running":
		return a.tr("extRunning"), p.green
	case "starting":
		return a.tr("extStarting"), p.ink2
	case "stopped":
		return a.tr("extStopped"), p.ink2
	case "needs_setup":
		return a.tr("extNeedsSetup"), p.attention
	case "incompatible":
		return a.tr("extIncompatible"), p.orange
	case "error":
		return a.tr("extError"), p.red
	case "disabled":
		return a.tr("extDisabled"), p.ink3
	}
	return status, p.ink2
}

func (a *App) extensionRow(c *ui.Context, p palette, x hubapi.Extension) {
	g := &a.sets
	ui.Row(c).Gap(12).Padding(6, 0).AlignItems(ui.Start).BorderWidth(1, 0, 0, 0).BorderColor(p.line).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(3).Children(func() {
			ui.Row(c).Gap(6).Children(func() {
				name := x.Name
				if name == "" {
					name = x.ID
				}
				ui.Text(c, name).Bold().SingleLine().Shrink(1)
				text, col := a.extStatus(p, x.Status)
				a.pill(c, text, col)
				if x.Version != "" {
					ui.Text(c, x.Version).FontSize(small).TextColor(p.ink3).SingleLine()
				}
				if x.Manager != "" {
					ui.Text(c, a.tr("extManager", "manager", x.Manager)).FontSize(small).TextColor(p.ink3).SingleLine()
				}
			})
			if x.Description != "" {
				ui.Text(c, x.Description).FontSize(small).TextColor(p.ink2).MaxLines(2)
			}
			for _, need := range x.NeedsSetup {
				ui.Text(c, "· "+need).FontSize(small).TextColor(p.attention)
			}
			if x.Error != "" {
				ui.Text(c, x.Error).FontSize(small).TextColor(p.red).MaxLines(3).Selectable()
			}
		})
		on := x.On()
		if ui.Switch(c, &on).Label(a.tr("extSwitch", "name", x.Name)).Disabled(g.extensions.busyOn != "").Changed() {
			id, root := x.ID, g.extRoot
			a.change(&g.extensions, id, func(ctx context.Context, cl *hubapi.Client) error {
				return cl.SetExtensionEnabled(ctx, root, id, on)
			}, nil)
		}
	})
}

// ── Connectors ──

func (a *App) connectorsView(c *ui.Context, p palette) {
	g := &a.sets
	a.load(&g.connectors, func(ctx context.Context, cl *hubapi.Client) (func(), error) {
		list, err := cl.Connectors(ctx)
		return func() { g.conns = list }, err
	})
	if !a.loadState(c, p, &g.connectors) {
		return
	}
	now := time.Now()
	a.card(c, p, func() {
		ui.Text(c, a.tr("connectorsTitle")).Bold()
		ui.Text(c, a.tr("connectorsFooter")).FontSize(small).TextColor(p.ink2)
		if len(g.conns) == 0 {
			ui.Text(c, a.tr("connectorsNone")).FontSize(small).TextColor(p.ink3)
		}
		for i, x := range g.conns {
			a.connectorRow(c.Key("conn-"+itoa(i)), p, x, now)
		}
	})
}

func (a *App) connectorStatus(p palette, status string) (string, ui.Color) {
	switch status {
	case "ok":
		return a.tr("connOK"), p.green
	case "not_configured":
		return a.tr("connNotConfigured"), p.ink2
	case "not_signed_in":
		return a.tr("connNotSignedIn"), p.attention
	case "no_permission":
		return a.tr("connNoPermission"), p.orange
	case "rate_limited":
		return a.tr("connRateLimited"), p.orange
	case "error":
		return a.tr("extError"), p.red
	case "disabled":
		return a.tr("extDisabled"), p.ink3
	}
	return status, p.ink2
}

func (a *App) connectorRow(c *ui.Context, p palette, x hubapi.Connector, now time.Time) {
	g := &a.sets
	ui.Row(c).Gap(12).Padding(6, 0).AlignItems(ui.Start).BorderWidth(1, 0, 0, 0).BorderColor(p.line).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(3).Children(func() {
			ui.Row(c).Gap(6).Children(func() {
				name := x.Name
				if name == "" {
					name = x.ID
				}
				ui.Text(c, name).Bold().SingleLine().Shrink(1)
				text, col := a.connectorStatus(p, x.Status)
				a.pill(c, text, col)
			})
			var line string
			if x.Account != "" {
				line = x.Account
			}
			if x.Via != "" {
				line = joinDot(line, x.Via)
			}
			if x.LastSync != "" {
				line = joinDot(line, a.when(x.LastSync, now, a.tr("connSynced"), a.tr("connSyncedOn")))
			}
			if line != "" {
				ui.Text(c, line).FontSize(small).TextColor(p.ink3).SingleLine()
			}
			if x.Error != "" {
				ui.Text(c, x.Error).FontSize(small).TextColor(p.red).MaxLines(3).Selectable()
			}
			for _, step := range x.Guide {
				ui.Text(c, "· "+step).FontSize(small).TextColor(p.ink2)
			}
		})
		if x.Enabled == nil {
			return // a hub from before the switch: always on
		}
		on := x.On()
		if ui.Switch(c, &on).Label(a.tr("extSwitch", "name", x.Name)).Disabled(g.connectors.busyOn != "").Changed() {
			id := x.ID
			a.change(&g.connectors, id, func(ctx context.Context, cl *hubapi.Client) error {
				_, err := cl.SetConnectorEnabled(ctx, id, on)
				return err
			}, nil)
		}
	})
}

func joinDot(a, b string) string {
	if a == "" {
		return b
	}
	return a + " · " + b
}

// ── Models ──

func (a *App) modelsView(c *ui.Context, p palette) {
	g := &a.sets
	a.load(&g.models, func(ctx context.Context, cl *hubapi.Client) (func(), error) {
		cfg, err := cl.ModelConfig(ctx)
		return func() { g.modelCfg = cfg }, err
	})
	if !a.loadState(c, p, &g.models) {
		return
	}
	var groups []string
	for _, m := range g.modelCfg.Purposes {
		if !slices.Contains(groups, m.Group) {
			groups = append(groups, m.Group)
		}
	}
	ui.Text(c, a.tr("modelsFooter")).FontSize(small).TextColor(p.ink2)
	for _, grp := range groups {
		a.card(c, p, func() {
			if grp != "" {
				ui.Text(c, grp).Bold()
			}
			for _, m := range g.modelCfg.Purposes {
				if m.Group == grp {
					a.modelRow(c, p, m)
				}
			}
		})
	}
}

func (a *App) providerName(id string) string {
	for _, pr := range a.sets.modelCfg.Providers {
		if pr.ID == id {
			return pr.Name
		}
	}
	return id
}

func (a *App) modelRow(c *ui.Context, p palette, m hubapi.ModelPurpose) {
	g := &a.sets
	ui.Column(c.Key("model-"+m.ID)).Gap(6).Padding(6, 0).BorderWidth(1, 0, 0, 0).BorderColor(p.line).Children(func() {
		ui.Row(c).Gap(10).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
				ui.Text(c, m.Name).SingleLine()
				eff := a.providerName(m.Effective.Provider)
				if m.Effective.Model != "" {
					eff += " · " + m.Effective.Model
				}
				if m.Effective.Effort != "" {
					eff += " · " + m.Effective.Effort
				}
				if m.Override.Provider == "" && m.InheritedFrom != "" {
					eff = joinDot(eff, a.tr("modelInherited", "from", m.InheritedFrom))
				}
				ui.Text(c, eff).FontSize(small).TextColor(p.ink2).SingleLine()
				if m.Status != "" {
					ui.Text(c, m.Status).FontSize(small).TextColor(p.ink3).SingleLine()
				}
			})
			if m.Editable() && g.modelEdit != m.ID {
				if ui.Button(c, a.tr("modelChange")).Clicked() {
					g.modelEdit, g.modelProv, g.modelName = m.ID, a.providerName(m.Override.Provider), m.Override.Model
					if m.Override.Provider == "" {
						g.modelProv = a.tr("modelDefault")
					}
				}
			}
		})
		if g.modelEdit == m.ID {
			a.modelEditor(c, p, m)
		}
	})
}

// modelEditor picks a use's provider ("Default" goes back to what it inherits) and, where it takes one, its model.
func (a *App) modelEditor(c *ui.Context, p palette, m hubapi.ModelPurpose) {
	g := &a.sets
	labels := []string{a.tr("modelDefault")}
	ids := []string{""}
	for _, id := range m.ProviderIDs {
		for _, pr := range g.modelCfg.Providers {
			if pr.ID == id && !pr.Hidden {
				labels = append(labels, pr.Name)
				ids = append(ids, pr.ID)
			}
		}
	}
	ui.Row(c).Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
		ui.Select(c.Key("model-prov"), &g.modelProv, labels).Label(a.tr("modelProvider")).Width(220)
		if slices.Contains(m.Fields, "model") && g.modelProv != a.tr("modelDefault") {
			ui.TextInput(c.Key("model-name"), &g.modelName).Placeholder(a.tr("modelName")).Label(a.tr("modelName")).Width(220)
		}
		ui.Spacer(c)
		if ui.Button(c, a.tr("cancel")).Clicked() {
			g.modelEdit = ""
		}
		if ui.PrimaryButton(c, a.tr("save")).Disabled(g.models.busyOn != "").Clicked() {
			provider, model := "", g.modelName
			if i := slices.Index(labels, g.modelProv); i > 0 {
				provider = ids[i]
			} else {
				model = ""
			}
			a.change(&g.models, m.ID, func(ctx context.Context, cl *hubapi.Client) error {
				return cl.SetModel(ctx, m, provider, model)
			}, func() { g.modelEdit = "" })
		}
	})
}
