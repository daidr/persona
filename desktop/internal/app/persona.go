package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// personaState is the Persona page: the assistant's chats, a new one being written, the archived ones shown or not.
type personaState struct {
	list         ui.ListState
	showArchived bool
	draft        string
	starting     bool
}

// conversations are the assistant's chats: the main one first, then the most recent; archived ones apart.
func (a *App) conversations() (open, archived []hubapi.Session) {
	as := a.conn.data.Assistant
	for _, x := range as.Conversations {
		if x.MachineID == "" {
			x.MachineID = as.MachineID
		}
		if x.Archived && x.SessionID != as.Main {
			archived = append(archived, x)
		} else {
			open = append(open, x)
		}
	}
	by := func(list []hubapi.Session) {
		sort.SliceStable(list, func(i, j int) bool {
			if (list[i].SessionID == as.Main) != (list[j].SessionID == as.Main) {
				return list[i].SessionID == as.Main
			}
			return list[i].Updated().After(list[j].Updated())
		})
	}
	by(open)
	by(archived)
	return open, archived
}

// personaPage is the assistant's chats beside the one open: the main chat by default, "new" for a new one.
func (a *App) personaPage(c *ui.Context, p palette, open string) {
	d := a.conn.data
	main, ok := d.MainConversation()
	if open == "" && ok {
		open = main.SessionID
	}
	ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(280).BorderWidth(0, 1, 0, 0).BorderColor(p.line).Children(func() {
			a.conversationList(c, p, open, main.SessionID)
		})
		ui.Column(c).Grow(1).Children(func() {
			switch {
			case open == "new":
				a.newConversation(c, p)
			case open != "":
				title := a.tr("personaMain")
				if open != main.SessionID {
					if x, ok := d.Session(d.Assistant.MachineID + "/" + open); ok {
						title = titleOf(x)
					}
				}
				a.pageHeader(c, p, title, a.tr("personaSubtitle"), func() {
					if open != main.SessionID {
						a.archiveButton(c, open)
					}
				})
				ui.Divider(c)
				machine := d.Assistant.MachineID
				if open == main.SessionID {
					machine = main.MachineID
				}
				a.chatView(c, p, machine+"/"+open, true)
			case !a.conn.loaded:
				ui.Spinner(c).Margin(ui.Auto)
			default:
				a.empty(c, p, "spark", a.tr("personaNone"), a.tr("personaNoneHint"))
			}
		})
	})
}

func (a *App) conversationList(c *ui.Context, p palette, open, mainID string) {
	g := &a.persona
	list, archived := a.conversations()
	if g.showArchived {
		list = archived
	}
	ui.Row(c).Padding(16, 16, 8, 16).Gap(8).Children(func() {
		ui.Text(c, a.tr("goPersona")).FontSize(title).Bold().Grow(1)
		if ui.Button(c, "").Label(a.tr("personaNew")).Tooltip(a.tr("personaNew")).Children(func() { ui.Icon(c, icon("plus")) }).Clicked() {
			a.Navigate("/persona/new")
		}
	})
	if len(archived) > 0 || g.showArchived {
		tab := 0
		if g.showArchived {
			tab = 1
		}
		if ui.Segmented(c, &tab, a.tr("personaChats"), a.tr("personaArchived")).Margin(0, 16, 8, 16).Changed() {
			g.showArchived = tab == 1
		}
	}
	now := time.Now()
	g.list.Key = func(i int) any { return list[i].SessionID }
	ui.List(c, &g.list, len(list), func(i int) {
		x := list[i]
		chosen := x.SessionID == open
		bg := ui.Transparent
		if chosen {
			bg = p.hover
		}
		title := titleOf(x)
		if x.SessionID == mainID {
			title = a.tr("personaMain")
		}
		row := ui.ButtonBase(c.Key(x.SessionID)).Margin(0, 8).Padding(8, 10).Radius(rowRadius).Background(bg).Gap(3).
			Column().AlignItems(ui.Stretch).Label(title)
		row.Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				if x.SessionID == mainID {
					ui.Icon(c, icon("spark")).FontSize(small).TextColor(p.ai)
				}
				t := ui.Text(c, title).SingleLine().Grow(1).Shrink(1)
				if x.State == hubapi.StateWorking || x.Status == "working" {
					t.Bold()
				}
				ui.Text(c, a.ago(x.UpdatedAt, now)).FontSize(small).TextColor(p.ink3).SingleLine()
			})
			if line := x.LastText; line != "" {
				ui.Text(c, oneLine(line, 160)).FontSize(small).TextColor(p.ink2).MaxLines(2)
			}
		})
		if row.Hovered() && !chosen {
			row.Background(p.hover)
		}
		if row.Clicked() {
			if x.SessionID == mainID {
				a.Navigate("/")
			} else {
				a.Navigate("/persona/" + x.SessionID)
			}
		}
	}).Grow(1).PaddingY(4)
}

// archiveButton archives one of the assistant's chats, or brings an archived one back.
func (a *App) archiveButton(c *ui.Context, id string) {
	d := a.conn.data
	x, _ := d.Session(d.Assistant.MachineID + "/" + id)
	label := a.tr("personaArchive")
	if x.Archived {
		label = a.tr("personaUnarchive")
	}
	if !ui.Button(c, label).Disabled(a.persona.starting).Clicked() {
		return
	}
	cn := a.conn
	archive := !x.Archived
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := cn.client.ArchiveConversation(ctx, id, archive)
		a.Host.Post(func() {
			if !a.live(cn) {
				return
			}
			if err != nil {
				a.toast(a.errorText(err))
				return
			}
			for i := range cn.data.Assistant.Conversations {
				if cn.data.Assistant.Conversations[i].SessionID == id {
					cn.data.Assistant.Conversations[i].Archived = archive
				}
			}
			if archive {
				a.Navigate("/")
			}
			a.refreshAssistant(cn)
		})
	})
}

// newConversation is a new chat with the assistant: its first message starts it.
func (a *App) newConversation(c *ui.Context, p palette) {
	g := &a.persona
	a.pageHeader(c, p, a.tr("personaNew"), a.tr("personaNewHint"), nil)
	ui.Divider(c)
	a.empty(c, p, "spark", a.tr("personaNewTitle"), "")
	ui.Row(c).Padding(10, pagePad, 16, pagePad).Gap(8).AlignItems(ui.End).Children(func() {
		in := ui.TextArea(c.Key("new-chat"), &g.draft).Lines(1, 8).Placeholder(a.tr("composerPlaceholder")).Label(a.tr("composerPlaceholder")).Grow(1).AutoFocus()
		send := false
		in.OnTextKey(func(k ui.TextKey) bool {
			if k.Key == ui.KeyEnter && k.Mods == 0 {
				send = true
				return true
			}
			return false
		})
		if g.starting {
			ui.Spinner(c)
		}
		if ui.PrimaryButton(c, "").Label(a.tr("send")).Tooltip(a.tr("send")).Children(func() { ui.Icon(c, icon("send")) }).
			Disabled(g.starting || strings.TrimSpace(g.draft) == "").Clicked() {
			send = true
		}
		if send {
			a.startConversation()
		}
	})
}

func (a *App) startConversation() {
	g := &a.persona
	text := strings.TrimSpace(g.draft)
	if text == "" || g.starting {
		return
	}
	g.starting = true
	cn := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		s, err := cn.client.StartConversation(ctx, text)
		a.Host.Post(func() {
			g.starting = false
			if !a.live(cn) {
				return
			}
			if err != nil {
				a.toast(a.errorText(err))
				return
			}
			g.draft = ""
			if s.MachineID == "" {
				s.MachineID = cn.data.Assistant.MachineID
			}
			s.Assistant = true
			cn.data.Assistant.Conversations = append(cn.data.Assistant.Conversations, s)
			cn.data.Transcript(s.Key()).Pending(text, hubapi.OpID())
			a.refreshAssistant(cn)
			a.Navigate("/persona/" + s.SessionID)
		})
	})
}
