package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// resolve answers a card; one the hub says has ended goes away with a note.
func (a *App) resolve(st *chatState, ix hubapi.Interaction, r hubapi.Resolution) {
	c := a.conn
	if st.busy[ix.ID] {
		return
	}
	st.busy[ix.ID] = true
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		err := c.client.Resolve(ctx, ix.ID, r)
		a.Host.Post(func() {
			delete(st.busy, ix.ID)
			if !a.live(c) {
				return
			}
			switch {
			case err == nil:
				delete(c.data.Interactions, ix.ID)
				a.updateBadge()
			case errors.Is(err, hubapi.ErrGone):
				delete(c.data.Interactions, ix.ID)
				a.updateBadge()
				a.toast(a.tr("cardGone"))
			default:
				a.toast(a.errorText(err))
			}
		})
	})
}

// cardView is an open card of the agent: a permission, a question, a plan, a terminal dialog or a form.
func (a *App) cardView(c *ui.Context, p palette, x hubapi.Session, st *chatState, ix hubapi.Interaction) {
	can := x.Can("approve")
	busy := st.busy[ix.ID]
	ui.Column(c.Key("card:"+ix.ID)).Margin(4, pagePad).Padding(14, 16).Gap(10).Radius(cardRadius).
		Background(p.surface).Border(1, p.attention).Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			ui.Icon(c, icon("bell")).TextColor(p.attention)
			ui.Text(c, a.cardTitle(ix)).Bold().Grow(1).Shrink(1).MaxLines(2)
			if busy {
				ui.Spinner(c)
			}
		})
		if !can {
			ui.Text(c, a.tr("cardReadOnly")).FontSize(small).TextColor(p.ink2)
			return
		}
		switch ix.Kind {
		case "permission":
			a.permissionCard(c, p, st, ix, busy)
		case "question":
			a.questionCard(c, p, st, ix, busy)
		case "plan":
			a.planCard(c, p, st, ix, busy)
		case "screen":
			a.screenCard(c, p, st, ix, busy)
		default:
			a.otherCard(c, p, st, ix, busy)
		}
	})
}

func (a *App) cardTitle(ix hubapi.Interaction) string {
	switch ix.Kind {
	case "permission":
		tool := ix.ToolName
		if tool == "" {
			tool = a.tr("aTool")
		}
		return a.tr("cardPermission", "tool", tool)
	case "question":
		return a.tr("cardQuestion")
	case "plan":
		return a.tr("cardPlan")
	case "screen":
		if ix.Dialog != nil && ix.Dialog.Question != "" {
			return ix.Dialog.Question
		}
		return a.tr("cardScreen")
	}
	if ix.Elicitation != nil && ix.Elicitation.Message != "" {
		return ix.Elicitation.Message
	}
	return a.tr("cardOther")
}

func yes() *bool { b := true; return &b }
func no() *bool  { b := false; return &b }

func (a *App) permissionCard(c *ui.Context, p palette, st *chatState, ix hubapi.Interaction, busy bool) {
	detail := ix.Summary
	if s := toolSummary(ix.ToolInput); s != "" && !strings.Contains(detail, s) {
		if detail != "" {
			detail += "\n"
		}
		detail += s
	}
	if detail != "" {
		ui.Text(c, detail).FontSize(small).Font("monospace").TextColor(p.ink2).Padding(8, 10).Radius(rowRadius).
			Background(p.inset).MaxLines(8).Selectable()
	}
	ui.Row(c).Gap(8).Wrap().Children(func() {
		if ui.PrimaryButton(c, a.tr("allow")).Disabled(busy).Clicked() {
			a.resolve(st, ix, hubapi.Resolution{Allow: yes()})
		}
		if ix.AlwaysLabel != "" {
			if ui.Button(c, ix.AlwaysLabel).Disabled(busy).Clicked() {
				a.resolve(st, ix, hubapi.Resolution{Allow: yes(), Always: true})
			}
		}
		if ui.Button(c, a.tr("deny")).Disabled(busy).Clicked() {
			a.resolve(st, ix, hubapi.Resolution{Allow: no()})
		}
	})
}

func (a *App) questionCard(c *ui.Context, p palette, st *chatState, ix hubapi.Interaction, busy bool) {
	chosen := st.answers[ix.ID]
	if chosen == nil {
		chosen = map[string][]string{}
		st.answers[ix.ID] = chosen
	}
	for qi, q := range ix.Questions {
		ui.Column(c.Key(qi)).Gap(6).Children(func() {
			if q.Header != "" {
				ui.Text(c, q.Header).FontSize(small).Bold().TextColor(p.ink2)
			}
			ui.Text(c, q.Question).Selectable()
			for _, o := range q.Options {
				on := slices.Contains(chosen[q.Question], o.Label)
				bg, border := p.surface, p.line
				if on {
					bg, border = p.hover, p.accent
				}
				b := ui.ButtonBase(c.Key(o.Label)).Padding(8, 12).Radius(rowRadius).Background(bg).Border(1, border).Gap(10).
					AlignItems(ui.Start).Disabled(busy).Label(o.Label)
				b.Children(func() {
					ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
						ui.Text(c, o.Label).Bold()
						if o.Description != "" {
							ui.Text(c, o.Description).FontSize(small).TextColor(p.ink2)
						}
					})
					if on {
						ui.Icon(c, icon("check")).TextColor(p.accent)
					}
				})
				if b.Clicked() {
					switch {
					case !q.MultiSelect:
						chosen[q.Question] = []string{o.Label}
					case on:
						chosen[q.Question] = slices.DeleteFunc(slices.Clone(chosen[q.Question]), func(s string) bool { return s == o.Label })
					default:
						chosen[q.Question] = append(chosen[q.Question], o.Label)
					}
				}
			}
		})
	}
	ready := len(ix.Questions) > 0
	for _, q := range ix.Questions {
		if len(chosen[q.Question]) == 0 {
			ready = false
		}
	}
	ui.Row(c).Gap(8).Children(func() {
		if ui.PrimaryButton(c, a.tr("answer")).Disabled(busy || !ready).Clicked() {
			ans := map[string]string{}
			for _, q := range ix.Questions {
				ans[q.Question] = strings.Join(chosen[q.Question], ", ")
			}
			a.resolve(st, ix, hubapi.Resolution{Answers: ans})
		}
		if ui.Button(c, a.tr("dismiss")).Disabled(busy).Clicked() {
			a.resolve(st, ix, hubapi.Resolution{Dismiss: true})
		}
	})
}

func (a *App) planCard(c *ui.Context, p palette, st *chatState, ix hubapi.Interaction, busy bool) {
	ui.Column(c).Padding(10, 12).Radius(rowRadius).Background(p.inset).MaxHeight(360).Children(func() {
		ui.Scroll(c).Children(func() { markdown.View(c, ix.Plan, &a.md) })
	})
	fb := st.feedback[ix.ID]
	ui.TextInput(c.Key("feedback"), &fb).Placeholder(a.tr("planFeedback")).Label(a.tr("planFeedback"))
	st.feedback[ix.ID] = fb
	ui.Row(c).Gap(8).Children(func() {
		if ui.PrimaryButton(c, a.tr("approve")).Disabled(busy).Clicked() {
			a.resolve(st, ix, hubapi.Resolution{Approve: yes()})
		}
		if ui.Button(c, a.tr("keepPlanning")).Disabled(busy).Clicked() {
			a.resolve(st, ix, hubapi.Resolution{Approve: no(), Feedback: strings.TrimSpace(fb)})
		}
	})
}

func (a *App) screenCard(c *ui.Context, p palette, st *chatState, ix hubapi.Interaction, busy bool) {
	if ix.Dialog == nil || len(ix.Dialog.Options) == 0 {
		if ix.Screen != "" {
			ui.Text(c, ix.Screen).FontSize(small).Font("monospace").TextColor(p.ink2).MaxLines(12)
		}
		a.otherCard(c, p, st, ix, busy)
		return
	}
	if ix.Dialog.Detail != "" {
		ui.Text(c, ix.Dialog.Detail).FontSize(small).TextColor(p.ink2).MaxLines(8).Selectable()
	}
	ui.Row(c).Gap(8).Wrap().Children(func() {
		for i, o := range ix.Dialog.Options {
			if o.Kind == "input" {
				continue // typed answers need the terminal
			}
			btn := ui.Button
			if i == 0 {
				btn = ui.PrimaryButton
			}
			if btn(c.Key(o.Key), o.Label).Disabled(busy).Clicked() {
				a.resolve(st, ix, hubapi.Resolution{Option: o.Key})
			}
		}
	})
}

// otherCard is a card this app can't fill in yet (an MCP form, fields): what it says, and Dismiss.
func (a *App) otherCard(c *ui.Context, p palette, st *chatState, ix hubapi.Interaction, busy bool) {
	if ix.Summary != "" {
		ui.Text(c, ix.Summary).FontSize(small).TextColor(p.ink2).Selectable()
	}
	ui.Text(c, a.tr("cardElsewhere")).FontSize(small).TextColor(p.ink3)
	ui.Row(c).Gap(8).Children(func() {
		if ix.Elicitation != nil {
			if ui.Button(c, a.tr("decline")).Disabled(busy).Clicked() {
				a.resolve(st, ix, hubapi.Resolution{Action: "decline"})
			}
			return
		}
		if ui.Button(c, a.tr("dismiss")).Disabled(busy).Clicked() {
			a.resolve(st, ix, hubapi.Resolution{Dismiss: true})
		}
	})
}
