package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// Mail in the Inbox (docs/extensions/mail.md): a mail entry shows its whole mail (who, the body, attachments to save),
// opening it marks it read, and Reply / Reply all / New email write one by hand, sent by the hub (POST /mail/send).
// Closing the dialog any way but Cancel keeps what was typed for the next time it opens for that mail.

type mailState struct {
	conn     *conn
	accounts []hubapi.MailAccount
	asked    bool
	bodies   map[string]*mailBody
	compose  composeState
	kept     map[string]hubapi.MailDraft
}

type mailBody struct {
	msg    hubapi.MailMessage
	loaded bool
	err    string
	saving bool
}

type composeState struct {
	open, cancelled bool
	key             string
	title           string
	d, initial      hubapi.MailDraft
	from            string
	sending         bool
	err             string
}

func (a *App) mailData() *mailState {
	g := &a.mail
	if g.conn != a.conn {
		*g = mailState{conn: a.conn, bodies: map[string]*mailBody{}, kept: map[string]hubapi.MailDraft{}}
	}
	return g
}

// mailAccounts are the hub's mail accounts, read once (and again when a mail is written).
func (a *App) mailAccounts(again bool) []hubapi.MailAccount {
	g := a.mailData()
	if !g.asked || again {
		g.asked = true
		cn := a.conn
		a.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			list, err := cn.client.MailAccounts(ctx)
			a.Host.Post(func() {
				if err == nil && a.live(cn) {
					g.accounts = list
				}
			})
		})
	}
	return g.accounts
}

func (a *App) mailOf(it hubapi.InboxItem) *mailBody {
	g := a.mailData()
	m := it.Mail
	key := it.ID + "/" + itoa(m.UID())
	b := g.bodies[key]
	if b != nil {
		return b
	}
	b = &mailBody{}
	g.bodies[key] = b
	cn := a.conn
	unread := !it.Done() && !it.Read
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		msg, err := cn.client.Mail(ctx, m.AccountID, m.UID(), m.Box())
		if err == nil && unread && !msg.Seen {
			_ = cn.client.MarkMailSeen(ctx, m.AccountID, m.UID(), m.Box())
		}
		a.Host.Post(func() {
			b.loaded = true
			if err != nil {
				b.err = a.errorText(err)
				return
			}
			b.msg = msg
		})
	})
	return b
}

// quoteHead is the line above a quoted mail: when and who.
func (a *App) quoteHead(msg hubapi.MailMessage) string {
	var parts []string
	if t, err := time.Parse(time.RFC3339, msg.Date); err == nil {
		parts = append(parts, t.Local().Format("2006-01-02 15:04"))
	}
	if msg.From != nil {
		parts = append(parts, msg.From.String())
	}
	return a.tr("mailQuoteHead", "head", strings.Join(parts, a.tr("mailQuoteSep")))
}

func joinAddresses(list []hubapi.MailAddress, sep string) string {
	out := make([]string, len(list))
	for i, x := range list {
		out[i] = x.String()
	}
	return strings.Join(out, sep)
}

// mailView is a mail entry's mail.
func (a *App) mailView(c *ui.Context, p palette, it hubapi.InboxItem) {
	b := a.mailOf(it)
	if !b.loaded {
		ui.Row(c).Gap(8).Children(func() {
			ui.Spinner(c)
			ui.Text(c, a.tr("mailLoading")).FontSize(small).TextColor(p.ink3)
		})
		return
	}
	if b.err != "" {
		ui.Text(c, b.err).FontSize(small).TextColor(p.red).Selectable()
		return
	}
	msg := b.msg
	m := it.Mail
	accounts := a.mailAccounts(false)
	mine := ""
	for _, x := range accounts {
		if x.ID == m.AccountID {
			mine = x.Email
		}
	}
	head := a.quoteHead(msg)
	all, canAll := hubapi.ReplyAllDraft(m.AccountID, msg, mine, head)
	ui.Row(c).Gap(16).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(4).Children(func() {
			line := func(label, value string, strong bool) {
				if value == "" {
					return
				}
				ui.Row(c.Key("mh-" + label)).Gap(10).Children(func() {
					ui.Text(c, label).FontSize(small).TextColor(p.ink3).Width(56).SingleLine()
					t := ui.Text(c, value).FontSize(small).SingleLine().Grow(1).Shrink(1).Selectable()
					if !strong {
						t.TextColor(p.ink2)
					}
				})
			}
			from := ""
			if msg.From != nil {
				from = msg.From.String()
			}
			sep := a.tr("mailAddressSep")
			line(a.tr("mailFrom"), from, true)
			line(a.tr("mailTo"), joinAddresses(msg.To, sep), false)
			line(a.tr("mailCc"), joinAddresses(msg.Cc, sep), false)
			if t, err := time.Parse(time.RFC3339, msg.Date); err == nil {
				line(a.tr("mailDate"), t.Local().Format("2006-01-02 15:04"), false)
			}
		})
		if ui.Button(c, a.tr("mailReply")).Tooltip(a.tr("mailReplyHint")).Clicked() {
			a.openCompose("reply:"+it.ID, a.tr("mailReplyTitle"), hubapi.ReplyDraft(m.AccountID, msg, head))
		}
		hint := a.tr("mailReplyAllHint")
		if !canAll {
			hint = a.tr("mailReplyAllUnavailable")
		}
		if ui.Button(c, a.tr("mailReplyAll")).Tooltip(hint).Disabled(!canAll).Clicked() {
			a.openCompose("reply-all:"+it.ID, a.tr("mailReplyTitle"), all)
		}
	})
	body := msg.Text
	if strings.TrimSpace(body) == "" && msg.HTML != "" {
		body = hubapi.HTMLText(msg.HTML)
	}
	if strings.TrimSpace(body) == "" {
		body = a.tr("mailNoBody")
	}
	a.card(c, p, func() {
		ui.Text(c, strings.TrimRight(body, "\n ")).Selectable()
	})
	if len(msg.Attachments) > 0 {
		ui.Text(c, a.tr("mailAttachments", "count", itoa(len(msg.Attachments)))).FontSize(small).Bold().TextColor(p.ink3)
		ui.Row(c).Wrap().Gap(8).Children(func() {
			for i, f := range msg.Attachments {
				name := f.Filename
				if name == "" {
					name = a.tr("mailAttachment")
				}
				label := name
				if f.Size > 0 {
					label += " · " + sizeText(f.Size)
				}
				chip := ui.ButtonBase(c.Key("att-"+itoa(i))).Padding(6, 10).Radius(rowRadius).Background(p.surface).Border(1, p.line).Gap(6).Label(label).Tooltip(a.tr("filesSave"))
				chip.Children(func() {
					ui.Icon(c, icon("clip")).FontSize(small).TextColor(p.ink3)
					ui.Text(c, name).FontSize(small).SingleLine()
					if f.Size > 0 {
						ui.Text(c, sizeText(f.Size)).FontSize(small).TextColor(p.ink3).SingleLine()
					}
				})
				if chip.Hovered() {
					chip.Background(p.hover)
				}
				if chip.Clicked() && f.ID != "" && !b.saving {
					a.saveAttachment(b, m, f.ID, name)
				}
			}
		})
	}
}

func (a *App) saveAttachment(b *mailBody, m *hubapi.InboxMail, id, name string) {
	cn := a.conn
	a.Host.SaveFile(a.tr("filesSave"), name, func(dest string) {
		b.saving = true
		a.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			err := func() error {
				out, err := os.Create(dest)
				if err != nil {
					return err
				}
				if err := cn.client.Fetch(ctx, hubapi.MailAttachmentPath(m.AccountID, m.UID(), m.Box(), id), out); err != nil {
					out.Close()
					os.Remove(dest)
					return err
				}
				return out.Close()
			}()
			a.Host.Post(func() {
				b.saving = false
				if err != nil {
					a.toast(a.errorText(err))
					return
				}
				a.toast(a.tr("filesSaved", "name", filepath.Base(dest)))
			})
		})
	})
}

// openCompose opens the mail dialog for key, with what was kept for it or fresh.
func (a *App) openCompose(key, title string, fresh hubapi.MailDraft) {
	g := a.mailData()
	accounts := a.mailAccounts(true)
	d := fresh
	if k, ok := g.kept[key]; ok {
		d = k
	}
	g.compose = composeState{open: true, key: key, title: title, d: d, initial: fresh}
	g.compose.from = a.accountLabel(accounts, d.AccountID)
}

func (a *App) accountLabel(accounts []hubapi.MailAccount, id string) string {
	for _, x := range accounts {
		if x.ID == id {
			return x.Label()
		}
	}
	if id == "" {
		return a.tr("mailNoAccount")
	}
	return id
}

// newMail opens New email from the first account.
func (a *App) newMail() {
	g := a.mailData()
	id := ""
	if len(g.accounts) > 0 {
		id = g.accounts[0].ID
	}
	a.openCompose("new", a.tr("mailNew"), hubapi.MailDraft{AccountID: id})
}

func (cs *composeState) dirty() bool {
	d, i := cs.d, cs.initial
	return d.To != i.To || d.Cc != i.Cc || d.Bcc != i.Bcc || d.Subject != i.Subject || d.Body != i.Body || d.AccountID != i.AccountID
}

// keepDraft keeps what was typed for the next time this mail's dialog opens, or forgets an untouched one.
func (a *App) keepDraft() {
	g := a.mailData()
	cs := &g.compose
	if cs.dirty() {
		g.kept[cs.key] = cs.d
	} else {
		delete(g.kept, cs.key)
	}
}

func (a *App) composeDialog(c *ui.Context, p palette) {
	g := a.mailData()
	cs := &g.compose
	if !cs.open {
		return
	}
	accounts := g.accounts
	opts := make([]string, 0, len(accounts)+1)
	if a.accountLabel(accounts, cs.d.AccountID) == cs.from && !hasAccount(accounts, cs.d.AccountID) {
		opts = append(opts, cs.from)
	}
	for _, x := range accounts {
		opts = append(opts, x.Label())
	}
	ui.Modal(c, &cs.open, func() {
		ui.Column(c).Width(620).Padding(20).Gap(10).Children(func() {
			ui.Text(c, cs.title).FontSize(heading).Bold()
			row := func(label string, fn func()) {
				ui.Row(c.Key("cf-" + label)).Gap(12).Children(func() {
					ui.Text(c, label).FontSize(small).TextColor(p.ink3).Width(56).SingleLine()
					fn()
				})
			}
			row(a.tr("mailFrom"), func() {
				if ui.Select(c.Key("mail-from"), &cs.from, opts).Label(a.tr("mailFrom")).Grow(1).Changed() {
					for _, x := range accounts {
						if x.Label() == cs.from {
							cs.d.AccountID = x.ID
						}
					}
				}
			})
			field := func(label, key string, v *string, placeholder string) {
				row(label, func() {
					in := ui.TextInput(c.Key(key), v).Label(label).Grow(1)
					if placeholder != "" {
						in.Placeholder(placeholder)
					}
					if in.Changed() {
						cs.err = ""
					}
				})
			}
			field(a.tr("mailTo"), "mail-to", &cs.d.To, a.tr("mailToPlaceholder"))
			field(a.tr("mailCc"), "mail-cc", &cs.d.Cc, "")
			field(a.tr("mailBcc"), "mail-bcc", &cs.d.Bcc, "")
			field(a.tr("mailSubject"), "mail-subject", &cs.d.Subject, "")
			ui.TextArea(c.Key("mail-body"), &cs.d.Body).Lines(10, 18).Placeholder(a.tr("mailBody")).Label(a.tr("mailBody"))
			if cs.err != "" {
				ui.Text(c, cs.err).FontSize(small).TextColor(p.red).Selectable()
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if cs.sending {
					ui.Spinner(c)
				}
				if ui.Button(c, a.tr("cancel")).Disabled(cs.sending).Clicked() {
					delete(g.kept, cs.key)
					cs.open, cs.cancelled = false, true
				}
				ready := cs.d.AccountID != "" && len(hubapi.ParseAddresses(cs.d.To+"\n"+cs.d.Cc+"\n"+cs.d.Bcc)) > 0
				if ui.PrimaryButton(c, a.tr("mailSend")).Disabled(cs.sending || !ready).Clicked() {
					a.sendMail()
				}
			})
		})
	})
	// closed some other way than Cancel or a send: keep what was typed
	if !cs.open && !cs.cancelled && !cs.sending {
		a.keepDraft()
	}
}

func hasAccount(list []hubapi.MailAccount, id string) bool {
	for _, x := range list {
		if x.ID == id {
			return true
		}
	}
	return false
}

func (a *App) sendMail() {
	g := a.mailData()
	cs := &g.compose
	d := cs.d
	if len(hubapi.ParseAddresses(d.To+"\n"+d.Cc+"\n"+d.Bcc)) == 0 {
		cs.err = a.tr("mailNeedRecipient")
		return
	}
	var bad []string
	for _, f := range []string{d.To, d.Cc, d.Bcc} {
		bad = append(bad, hubapi.BadAddresses(f)...)
	}
	if len(bad) > 0 {
		cs.err = a.tr("mailBadAddresses", "addresses", strings.Join(bad, a.tr("mailAddressSep")))
		return
	}
	cs.sending, cs.err = true, ""
	key := cs.key
	delete(g.kept, key)
	cn := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		err := cn.client.SendMail(ctx, d)
		a.Host.Post(func() {
			if !a.live(cn) {
				return
			}
			cs := &a.mailData().compose
			same := cs.key == key
			if same {
				cs.sending = false
			}
			if err != nil {
				if same && cs.open {
					cs.err = a.tr("mailSendFailed", "error", a.errorText(err))
				} else {
					a.mailData().kept[key] = d
					a.toast(a.tr("mailSendFailed", "error", a.errorText(err)))
				}
				return
			}
			if same {
				cs.open, cs.cancelled = false, true
			}
			a.toast(a.tr("mailSent"))
		})
	})
}
