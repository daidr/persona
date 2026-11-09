package app

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// signInState is a sign-in a step at a time, for a connector (POST …/login) or a channel's binding (POST …/bind):
// fields to fill in, a device code or a QR code the page asks again about, until the hub says done.
type signInState struct {
	open    bool
	channel bool // a channel's binding, else a connector's sign-in
	id      string
	name    string
	prompt  hubapi.Prompt
	values  map[string]string
	busy    bool
	err     string
	qr      ui.ImageSource
	pollAt  time.Time
	after   func(p hubapi.Prompt)
}

// startSignIn opens the dialog and asks the hub for the first step.
func (a *App) startSignIn(channel bool, id, name string, after func(p hubapi.Prompt)) {
	a.sets.m7.signIn = signInState{open: true, channel: channel, id: id, name: name, values: map[string]string{}, after: after}
	a.signInStep("", nil, false)
}

func (a *App) signInStep(step string, values map[string]string, quiet bool) {
	g := &a.sets.m7.signIn
	if g.busy {
		return
	}
	g.busy = true
	if !quiet {
		g.err = ""
	}
	cn, channel, id := a.conn, g.channel, g.id
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var pr hubapi.Prompt
		var err error
		if channel {
			pr, err = cn.client.BindChannel(ctx, id, step, values)
		} else {
			pr, err = cn.client.ConnectorLogin(ctx, id, step, values)
		}
		a.Host.Post(func() {
			if !a.live(cn) || g.id != id || !g.open {
				return
			}
			g.busy, g.pollAt = false, time.Now()
			if err != nil {
				if !quiet {
					g.err = a.errorText(err)
				}
				return
			}
			if pr.Done {
				g.open = false
				if g.after != nil {
					g.after(pr)
				}
				return
			}
			if pr.Step != g.prompt.Step {
				g.values = map[string]string{}
			}
			g.prompt, g.err = pr, pr.Error
			g.qr = nil
			if pr.QR != nil {
				g.qr = qrImage(pr.QR.Rows)
			}
			if pr.Polled() {
				a.After(pollEvery(pr), func() {})
			}
		})
	})
}

func pollEvery(p hubapi.Prompt) time.Duration {
	if p.Interval > 0 {
		return time.Duration(p.Interval) * time.Second
	}
	if p.Kind == "qr" {
		return 3 * time.Second
	}
	return 5 * time.Second
}

// signInDialog draws the step the hub asked for.
func (a *App) signInDialog(c *ui.Context, p palette) {
	g := &a.sets.m7.signIn
	if !g.open {
		return
	}
	pr := g.prompt
	if pr.Polled() && !g.busy && time.Since(g.pollAt) >= pollEvery(pr) {
		a.signInStep(pr.Step, nil, true)
	}
	ui.Modal(c, &g.open, func() {
		ui.Column(c).Width(460).Padding(20).Gap(12).Children(func() {
			ui.Text(c, g.name).FontSize(heading).Bold()
			if pr.Hint != "" {
				ui.Text(c, pr.Hint).FontSize(small).TextColor(p.ink2)
			}
			switch {
			case g.busy && pr.Step == "" && len(pr.Fields) == 0 && pr.Kind == "" && pr.PairCode == "":
				ui.Spinner(c).Margin(12, ui.Auto)
			case pr.Kind == "qr":
				if g.qr != nil {
					ui.Image(c, g.qr).Size(200, 200).Margin(0, ui.Auto)
				}
				ui.Text(c, a.tr("chanQrFooter", "name", g.name)).FontSize(small).TextColor(p.ink2)
			case pr.PairCode != "":
				a.copyLine(c, p, pr.PairCode)
				ui.Row(c).Gap(8).Children(func() {
					ui.Spinner(c)
					ui.Text(c, a.tr("chanWaitingPair", "name", g.name)).FontSize(small).TextColor(p.ink2)
				})
			case pr.Kind == "device_code":
				if pr.UserCode != "" {
					a.copyLine(c, p, pr.UserCode)
				}
				if pr.URL != "" && ui.Button(c, pr.URL).Clicked() {
					a.Host.OpenURL(pr.URL)
				}
				ui.Row(c).Gap(8).Children(func() {
					ui.Spinner(c)
					ui.Text(c, a.tr("chanWaitingAuth")).FontSize(small).TextColor(p.ink2)
				})
			default:
				for _, f := range pr.Fields {
					v := g.values[f.Name]
					in := ui.TextInput(c.Key("si-"+pr.Step+"-"+f.Name), &v).Label(f.Label).Placeholder(f.Placeholder)
					if f.Secret {
						in.Password()
					}
					g.values[f.Name] = v
				}
				if g.channel {
					ui.Text(c, a.tr("chanBindFooter")).FontSize(small).TextColor(p.ink3)
				}
			}
			if g.err != "" {
				ui.Text(c, g.err).FontSize(small).TextColor(p.red)
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c, a.tr("cancel")).Clicked() {
					g.open = false
				}
				if len(pr.Fields) > 0 && ui.PrimaryButton(c, a.tr("chanNext")).Disabled(g.busy).Clicked() {
					vals := map[string]string{}
					for k, v := range g.values {
						vals[k] = v
					}
					a.signInStep(pr.Step, vals, false)
				}
			})
		})
	})
}

// ── Channels ──

func (a *App) channelsView(c *ui.Context, p palette) {
	g := &a.sets.m7
	a.load(&g.chans, func(ctx context.Context, cl *hubapi.Client) (func(), error) {
		list, err := cl.Channels(ctx)
		return func() { g.chanList = list }, err
	})
	if !a.loadState(c, p, &g.chans) {
		return
	}
	ui.Text(c, a.tr("chanIntro")).FontSize(small).TextColor(p.ink2)
	var installed, market []hubapi.Channel
	for _, ch := range g.chanList {
		if ch.Installed || ch.Source != "market" {
			installed = append(installed, ch)
		} else {
			market = append(market, ch)
		}
	}
	now := time.Now()
	a.group(c, p, a.tr("chanStSectionInstalled"), a.tr("chanDisableFooter"), func() {
		for i, ch := range installed {
			a.channelRow(c.Key("ch-"+ch.ID), p, ch, i, now)
		}
	})
	if len(market) > 0 {
		a.group(c, p, a.tr("chanStSectionMarket"), a.tr("chanStSectionMarketFooter"), func() {
			for i, ch := range market {
				ui.Column(c.Key("chm-"+ch.ID)).Gap(2).Padding(6, 0).BorderWidth(btop(i), 0, 0, 0).BorderColor(p.line).Children(func() {
					ui.Row(c).Gap(6).Children(func() {
						ui.Text(c, ch.Name).Bold().Shrink(1).SingleLine()
						a.pill(c, a.tr("chanStStateNotInstalled"), p.ink3)
					})
					if ch.Description != "" {
						ui.Text(c, ch.Description).FontSize(small).TextColor(p.ink2)
					}
				})
			}
		})
	}
	if g.chanUnbind != "" {
		open := true
		name := g.chanUnbind
		for _, ch := range g.chanList {
			if ch.ID == g.chanUnbind {
				name = ch.Name
			}
		}
		ui.Modal(c, &open, func() {
			ui.Column(c).Width(420).Padding(20).Gap(12).Children(func() {
				ui.Text(c, a.tr("chanUnbindTitle", "name", name)).FontSize(heading).Bold()
				ui.Text(c, a.tr("chanUnbindBody", "name", name)).TextColor(p.ink2)
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, a.tr("cancel")).Clicked() {
						g.chanUnbind = ""
					}
					if ui.PrimaryButton(c, a.tr("chanUnbind")).Background(p.red).Clicked() {
						id := g.chanUnbind
						g.chanUnbind = ""
						a.change(&g.chans, id, func(ctx context.Context, cl *hubapi.Client) error { return cl.UnbindChannel(ctx, id) },
							func() { a.toast(a.tr("chanUnbound")) })
					}
				})
			})
		})
		if !open {
			g.chanUnbind = ""
		}
	}
	a.signInDialog(c, p)
}

func (a *App) channelState(p palette, ch hubapi.Channel) (string, ui.Color) {
	switch {
	case !ch.Enabled:
		return a.tr("chanStStateDisabled"), p.ink3
	case ch.State == "error" || ch.Error != "":
		return a.tr("chanStStateError"), p.red
	case ch.State == "connected" || ch.State == "ok":
		return a.tr("chanStStateConnected"), p.green
	}
	return a.tr("chanStStateNotConnected"), p.attention
}

func (a *App) channelRow(c *ui.Context, p palette, ch hubapi.Channel, i int, now time.Time) {
	g := &a.sets.m7
	ui.Row(c).Gap(12).Padding(6, 0).AlignItems(ui.Start).BorderWidth(btop(i), 0, 0, 0).BorderColor(p.line).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(3).Children(func() {
			ui.Row(c).Gap(6).Children(func() {
				ui.Text(c, ch.Name).Bold().Shrink(1).SingleLine()
				text, col := a.channelState(p, ch)
				a.pill(c, text, col)
			})
			line := ""
			if ch.Account != nil && ch.Account.Name != "" {
				line = a.tr("chanBoundAccount") + " · " + ch.Account.Name
			}
			if ch.LastEventAt != "" {
				line = strings.TrimSpace(line + " " + a.tr("chanLastEvent", "when", a.ago(ch.LastEventAt, now)))
			}
			if line != "" {
				ui.Text(c, line).FontSize(small).TextColor(p.ink2).SingleLine()
			}
			if ch.Error != "" {
				ui.Text(c, ch.Error).FontSize(small).TextColor(p.red)
			} else if ch.Description != "" {
				ui.Text(c, ch.Description).FontSize(small).TextColor(p.ink3)
			}
			if ch.ReadOnly {
				return
			}
			ui.Row(c).Gap(8).Children(func() {
				bound := ch.Account != nil && ch.Account.Name != ""
				canBind := ch.Bind == "qr" || ch.Bind == "code" || ch.Bind == "fields"
				if canBind && !bound && ui.Button(c, a.tr("chanBinding")).Disabled(!ch.Enabled).Clicked() {
					id := ch.ID
					a.startSignIn(true, id, ch.Name, func(pr hubapi.Prompt) {
						if pr.Account != "" {
							a.toast(a.tr("chanBoundAccountToast", "account", pr.Account))
						} else {
							a.toast(a.tr("chanBoundChannelToast", "name", ch.Name))
						}
						g.chans.asked = false
					})
				}
				if bound && canBind && ui.Button(c, a.tr("chanUnbind")).Clicked() {
					g.chanUnbind = ch.ID
				}
				if bound && ch.Enabled && ui.Button(c, a.tr("chanSendTest")).Tooltip(a.tr("chanTestFooter")).Disabled(g.chans.busyOn != "").Clicked() {
					id, name := ch.ID, ch.Name
					a.change(&g.chans, id, func(ctx context.Context, cl *hubapi.Client) error { return cl.TestChannel(ctx, id) },
						func() { a.toast(a.tr("chanTestSent", "name", name)) })
				}
			})
		})
		if ch.Kind != "app" && !ch.ReadOnly {
			on := ch.Enabled
			if ui.Switch(c.Key("chsw-"+ch.ID), &on).Disabled(g.chans.busyOn != "").Changed() {
				id := ch.ID
				a.change(&g.chans, id, func(ctx context.Context, cl *hubapi.Client) error {
					_, err := cl.SetChannel(ctx, id, on)
					return err
				}, nil)
			}
		}
	})
}

// ── Mail accounts (Settings › Connectors) ──

var mailKinds = []string{"imap", "feishu", "gmail"}

func (a *App) mailKindLabels() []string {
	return []string{a.tr("mailLibMailKindImap"), a.tr("mailLibMailKindFeishu"), "Gmail"}
}

func (a *App) mailAccountLine(m hubapi.MailAccount) string {
	switch {
	case m.Status == "error":
		if m.Error != "" {
			return m.Error
		}
		return a.tr("mailLibMailUnreachable")
	case m.Auth == "oauth2" && m.Provider == "outlook":
		return a.tr("mailLibMailLineOutlook")
	case m.Auth == "oauth2":
		return a.tr("mailLibMailLineGmail")
	case m.Provider == "feishu":
		return a.tr("mailLibMailLineFeishu")
	case m.IMAPHost != "":
		return "IMAP · " + m.IMAPHost
	}
	return "IMAP"
}

func (a *App) mailAccountsGroup(c *ui.Context, p palette) {
	g := &a.sets.m7
	a.load(&g.mail, func(ctx context.Context, cl *hubapi.Client) (func(), error) {
		list, err := cl.MailAccounts(ctx)
		return func() { g.mailList = list }, err
	})
	ui.Column(c).Gap(6).Children(func() {
		ui.Row(c).Gap(8).Padding(0, 4).Children(func() {
			ui.Text(c, a.tr("mailAccMail")).FontSize(small).Bold().TextColor(p.ink2).Grow(1)
			if ui.Button(c, a.tr("mailAccAddMail")).FontSize(small).Padding(2, 8).Clicked() {
				a.openAddMail()
			}
		})
		a.card(c, p, func() {
			switch {
			case g.mail.err != "":
				ui.Text(c, a.tr("mailAccMailLoadFailed", "error", g.mail.err)).FontSize(small).TextColor(p.red)
			case !g.mail.done:
				ui.Spinner(c)
			case len(g.mailList) == 0:
				ui.Text(c, a.tr("mailAccMailEmpty")).FontSize(small).TextColor(p.ink3)
			}
			for i, m := range g.mailList {
				ui.Row(c.Key("mail-"+m.ID)).Gap(12).Padding(6, 0).BorderWidth(btop(i), 0, 0, 0).BorderColor(p.line).Children(func() {
					ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
						ui.Text(c, m.Email).SingleLine()
						col := p.ink3
						if m.Status == "error" {
							col = p.red
						}
						ui.Text(c, a.mailAccountLine(m)).FontSize(small).TextColor(col).SingleLine()
					})
					if ui.Button(c, "").Label(a.tr("mailAccDeleteAccount", "email", m.Email)).Tooltip(a.tr("mailAccDeleteAccount", "email", m.Email)).
						Children(func() { ui.Icon(c, icon("x")) }).Clicked() {
						g.mailDelete = m.ID
					}
				})
			}
		})
		ui.Text(c, a.tr("mailAccMailFooter")).FontSize(small).TextColor(p.ink3).Padding(0, 4)
	})
	a.mailDialogs(c, p)
}

func (a *App) openAddMail() {
	g := &a.sets.m7
	g.mailAdd, g.mailForm, g.mailKind, g.mailTried, g.mailErr, g.mailGoogle = true, hubapi.MailForm{Kind: "imap", IMAPPort: "993", SMTPPort: "465"}, 0, false, "", true
	cn := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		google := cn.client.GoogleMailSignIn(ctx)
		a.Host.Post(func() {
			if a.live(cn) {
				g.mailGoogle = google
			}
		})
	})
}

func (a *App) addMail() {
	g := &a.sets.m7
	g.mailTried = true
	f := g.mailForm
	if len(hubapi.MailFormErrors(f)) > 0 || g.mailBusy {
		return
	}
	cn := a.conn
	g.mailBusy, g.mailErr = true, ""
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		err := cn.client.AddMailAccount(ctx, f)
		a.Host.Post(func() {
			g.mailBusy = false
			if !a.live(cn) {
				return
			}
			if err != nil {
				g.mailErr = a.errorText(err)
				return
			}
			g.mailAdd, g.mail.asked = false, false
			a.toast(a.tr("mailAccMailAdded", "email", f.Email))
		})
	})
}

func (a *App) mailDialogs(c *ui.Context, p palette) {
	g := &a.sets.m7
	if g.mailAdd {
		ui.Modal(c, &g.mailAdd, func() {
			ui.Column(c).Width(480).Padding(20).Gap(12).Children(func() {
				ui.Text(c, a.tr("mailAccAddMailTitle")).FontSize(heading).Bold()
				if ui.Segmented(c.Key("mail-kind"), &g.mailKind, a.mailKindLabels()...).Changed() {
					g.mailForm.Kind, g.mailTried, g.mailErr = mailKinds[g.mailKind], false, ""
				}
				f := &g.mailForm
				gmail := f.Kind == "gmail"
				if gmail && g.mailGoogle {
					ui.Text(c, a.tr("mailPhoneOnly")).FontSize(small).TextColor(p.ink2)
				} else {
					if gmail {
						ui.Text(c, a.tr("mailAccGmailNotConfiguredTitle")).Bold()
						ui.Text(c, a.tr("mailAccGmailNotConfiguredBody")).FontSize(small).TextColor(p.ink2)
					}
					bad := map[string]bool{}
					if g.mailTried {
						bad = hubapi.MailFormErrors(*f)
					}
					emailLabel, pwLabel, hint := a.tr("mailAccEmail"), a.tr("mailAccPassword"), a.tr("mailAccImapHint")
					switch f.Kind {
					case "gmail":
						emailLabel, pwLabel, hint = a.tr("mailAccGmailAddress"), a.tr("mailAccGmailAppPassword"), a.tr("mailAccGmailAppPasswordHint")
					case "feishu":
						pwLabel, hint = a.tr("mailAccPasswordLark"), a.tr("mailAccLarkHint")
					}
					ui.TextInput(c.Key("mail-email"), &f.Email).Label(emailLabel).Placeholder("me@example.com")
					if bad["email"] {
						ui.Text(c, a.tr("mailLibMailEmailInvalid")).FontSize(small).TextColor(p.red)
					}
					ui.TextInput(c.Key("mail-pw"), &f.Password).Label(pwLabel).Password()
					if bad["password"] {
						ui.Text(c, a.tr("mailLibMailPasswordEmpty")).FontSize(small).TextColor(p.red)
					}
					ui.Text(c, hint).FontSize(small).TextColor(p.ink3)
					if f.Kind == "imap" {
						ui.Text(c, a.tr("mailAccServersOptional")).FontSize(small).Bold().TextColor(p.ink2)
						ui.Row(c).Gap(8).Children(func() {
							ui.TextInput(c.Key("mail-imap"), &f.IMAPHost).Label(a.tr("mailAccImapServer")).Placeholder("imap.example.com").Grow(1)
							ui.TextInput(c.Key("mail-imapport"), &f.IMAPPort).Label(a.tr("mailAccPort")).Width(88)
						})
						ui.Row(c).Gap(8).Children(func() {
							ui.TextInput(c.Key("mail-smtp"), &f.SMTPHost).Label(a.tr("mailAccSmtpServer")).Placeholder("smtp.example.com").Grow(1)
							ui.TextInput(c.Key("mail-smtpport"), &f.SMTPPort).Label(a.tr("mailAccPort")).Width(88)
						})
						if bad["imapPort"] || bad["smtpPort"] {
							ui.Text(c, a.tr("mailLibMailPortInvalid")).FontSize(small).TextColor(p.red)
						}
					}
				}
				if g.mailErr != "" {
					ui.Text(c, g.mailErr).FontSize(small).TextColor(p.red)
				}
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, a.tr("cancel")).Disabled(g.mailBusy).Clicked() {
						g.mailAdd = false
					}
					if !(gmail && g.mailGoogle) {
						label := a.tr("mailAccConnect")
						if g.mailBusy {
							label = a.tr("mailAccConnecting")
						}
						if ui.PrimaryButton(c, label).Disabled(g.mailBusy).Clicked() {
							a.addMail()
						}
					}
				})
			})
		})
	}
	if g.mailDelete != "" {
		open := true
		email := g.mailDelete
		for _, m := range g.mailList {
			if m.ID == g.mailDelete {
				email = m.Email
			}
		}
		ui.Modal(c, &open, func() {
			ui.Column(c).Width(400).Padding(20).Gap(12).Children(func() {
				ui.Text(c, a.tr("mailAccDeleteAccount", "email", email)+"?").Bold()
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, a.tr("cancel")).Clicked() {
						g.mailDelete = ""
					}
					if ui.PrimaryButton(c, a.tr("mgrDelete")).Background(p.red).Clicked() {
						id := g.mailDelete
						g.mailDelete = ""
						a.change(&g.mail, id, func(ctx context.Context, cl *hubapi.Client) error { return cl.DeleteMailAccount(ctx, id) },
							func() { a.toast(a.tr("mailAccMailDeleted", "email", email)) })
					}
				})
			})
		})
		if !open {
			g.mailDelete = ""
		}
	}
}
