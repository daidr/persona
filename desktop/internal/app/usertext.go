package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// userBubble is the user's own message: its words, with what came with them (contexts, files, photos) above.
func (a *App) userBubble(c *ui.Context, p palette, it hubapi.Item) {
	u := hubapi.ParseUserText(it.Text)
	ui.Column(c).AlignItems(ui.End).Gap(4).Children(func() {
		a.attachedChips(c, p, u, "message-source")
		if u.Text == "" {
			return
		}
		b := ui.Column(c).Padding(8, 12).Radius(14).Background(p.userBubble).MaxWidthPercent(78).MaxWidth(620).Children(func() {
			ui.Text(c, u.Text).Selectable()
		})
		if it.Seq < 0 {
			b.Opacity(0.6)
		}
	})
}

// attachedChips are a message's contexts, files and photos, a chip each; skip is a context kind not shown.
func (a *App) attachedChips(c *ui.Context, p palette, u hubapi.UserText, skip string) {
	var chips [][2]string
	for _, x := range u.Contexts {
		if x.Kind == skip {
			continue
		}
		title := x.Title
		if title == "" {
			title = a.tr("contextUntitled")
		}
		chips = append(chips, [2]string{"book", title})
	}
	for _, f := range u.Files {
		chips = append(chips, [2]string{"folder", f.Name})
	}
	if n := len(u.Photos); n > 0 {
		chips = append(chips, [2]string{"read", a.tr("photosAttached", "count", itoa(n))})
	}
	if len(chips) == 0 {
		return
	}
	ui.Row(c).Gap(6).Wrap().Children(func() {
		for _, ch := range chips {
			ui.Row(c).Gap(4).Padding(2, 8).Radius(999).Border(1, p.line).Background(p.surface).Children(func() {
				ui.Icon(c, icon(ch[0])).FontSize(small).TextColor(p.ink3)
				ui.Text(c, ch[1]).FontSize(small).TextColor(p.ink2).SingleLine().MaxWidth(260)
			})
		}
	})
}

func (a *App) sourceLabel(src *hubapi.Source) string {
	if t := strings.TrimSpace(src.Title); t != "" {
		return t
	}
	switch src.Kind {
	case "conversation":
		return a.tr("sourceConversation")
	case "patrol":
		return a.tr("sourcePatrol")
	case "followup":
		return a.tr("sourceFollowup")
	case "supervision":
		return a.tr("sourceSupervision")
	}
	return a.tr("sourceOther")
}

// sourceView is a message another conversation, a scheduled run, a follow-up or supervision wrote: never a user
// bubble, but "@where it came from" over its words, folded past three lines.
func (a *App) sourceView(c *ui.Context, p palette, st *chatState, it hubapi.Item, src *hubapi.Source) {
	u := hubapi.ParseUserText(it.Text)
	text := u.Text
	if in := it.Injected; in != nil {
		text = in.Detail
		if text == "" {
			text = in.Line
		}
	}
	ui.Column(c).AlignItems(ui.Start).Children(func() {
		a.sourceBox(c, p, st, it, src, u, text)
	})
}

func (a *App) sourceBox(c *ui.Context, p palette, st *chatState, it hubapi.Item, src *hubapi.Source, u hubapi.UserText, text string) {
	long := len(text) > 180 || strings.Count(text, "\n") >= 3
	open := st.open["src:"+it.ID]
	ui.Column(c).Gap(4).Padding(8, 12).Radius(10).Border(1, p.line).Background(p.inset).MaxWidthPercent(90).MaxWidth(720).Children(func() {
		ui.Text(c, "@"+a.sourceLabel(src)).FontSize(small).Bold().TextColor(p.ai).SingleLine()
		t := ui.Text(c, text).FontSize(small).TextColor(p.ink2).Selectable()
		if long && !open {
			t.MaxLines(3)
		}
		a.attachedChips(c, p, u, "message-source")
		if long {
			label := a.tr("sourceMore")
			if open {
				label = a.tr("sourceLess")
			}
			if ui.ButtonBase(c.Key("src-" + it.ID)).Children(func() {
				ui.Text(c, label).FontSize(small).TextColor(p.accent)
			}).Clicked() {
				st.open["src:"+it.ID] = !open
			}
		}
	})
}
