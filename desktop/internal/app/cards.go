package app

import (
	"context"
	"errors"
	"slices"
	"strconv"
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
		case "elicitation":
			a.formCard(c, p, st, ix, busy)
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

// formCard answers an MCP server's question: a form to fill in, or a link to open and confirm.
func (a *App) formCard(c *ui.Context, p palette, st *chatState, ix hubapi.Interaction, busy bool) {
	e := ix.Elicitation
	if e == nil {
		a.otherCard(c, p, st, ix, busy)
		return
	}
	if ix.MCPServerName != "" {
		ui.Text(c, a.tr("formFrom", "server", ix.MCPServerName)).FontSize(small).TextColor(p.ink2)
	}
	if e.Mode == "url" {
		if e.URL != "" {
			ui.Text(c, e.URL).FontSize(small).TextColor(p.ink3).SingleLine().Selectable()
		}
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, a.tr("decline")).Disabled(busy).Clicked() {
				a.resolve(st, ix, hubapi.Resolution{Action: "decline"})
			}
			ui.Spacer(c)
			if strings.HasPrefix(e.URL, "https://") || strings.HasPrefix(e.URL, "http://") {
				if ui.Button(c, a.tr("formOpenLink")).Clicked() {
					a.Host.OpenURL(e.URL)
				}
			}
			if ui.PrimaryButton(c, a.tr("formDone")).Disabled(busy).Clicked() {
				a.resolve(st, ix, hubapi.Resolution{Action: "accept"})
			}
		})
		return
	}
	vals := st.answers[ix.ID]
	if vals == nil {
		vals = formDefaults(e.Schema)
		st.answers[ix.ID] = vals
	}
	var fields []hubapi.FormField
	var required []string
	if e.Schema != nil {
		fields, required = e.Schema.Fields, e.Schema.Required
	}
	for _, f := range fields {
		a.formField(c, p, ix.ID, f, slices.Contains(required, f.Name), vals)
	}
	content, missing := formContent(fields, required, vals)
	ui.Row(c).Gap(8).Children(func() {
		if ui.Button(c, a.tr("decline")).Disabled(busy).Clicked() {
			a.resolve(st, ix, hubapi.Resolution{Action: "decline"})
		}
		ui.Spacer(c)
		if missing != "" {
			ui.Text(c, a.tr("formMissing", "field", missing)).FontSize(small).TextColor(p.ink3).SingleLine().Shrink(1)
		}
		if ui.PrimaryButton(c, a.tr("formSubmit")).Disabled(busy || missing != "").Clicked() {
			a.resolve(st, ix, hubapi.Resolution{Action: "accept", Content: content})
		}
	})
}

func fieldTitle(f hubapi.FormField) string {
	if f.Title != "" {
		return f.Title
	}
	return f.Name
}

// formDefaults are a form's starting values, from its schema's defaults.
func formDefaults(s *hubapi.FormSchema) map[string][]string {
	out := map[string][]string{}
	if s == nil {
		return out
	}
	for _, f := range s.Fields {
		switch d := f.Default.(type) {
		case string:
			out[f.Name] = []string{d}
		case bool:
			out[f.Name] = []string{strconv.FormatBool(d)}
		case float64:
			out[f.Name] = []string{strconv.FormatFloat(d, 'f', -1, 64)}
		case []any:
			for _, x := range d {
				if v, ok := x.(string); ok {
					out[f.Name] = append(out[f.Name], v)
				}
			}
		}
	}
	return out
}

// formContent is what a form sends, by the types its schema names; missing is the first required field left empty
// or a number that doesn't read.
func formContent(fields []hubapi.FormField, required []string, vals map[string][]string) (map[string]any, string) {
	out := map[string]any{}
	missing := ""
	need := func(f hubapi.FormField) {
		if missing == "" && slices.Contains(required, f.Name) {
			missing = fieldTitle(f)
		}
	}
	for _, f := range fields {
		v := vals[f.Name]
		one := ""
		if len(v) > 0 {
			one = strings.TrimSpace(v[0])
		}
		switch f.Type {
		case "boolean":
			out[f.Name] = one == "true"
		case "array":
			if len(v) == 0 {
				need(f)
				continue
			}
			out[f.Name] = append([]string(nil), v...)
		case "number", "integer":
			if one == "" {
				need(f)
				continue
			}
			n, err := strconv.ParseFloat(one, 64)
			if err != nil || (f.Type == "integer" && n != float64(int64(n))) {
				if missing == "" {
					missing = fieldTitle(f)
				}
				continue
			}
			out[f.Name] = n
		default:
			if one == "" {
				need(f)
				continue
			}
			out[f.Name] = one
		}
	}
	return out, missing
}

// formField is one field: a switch, a drop-down, check boxes, or a text input.
func (a *App) formField(c *ui.Context, p palette, id string, f hubapi.FormField, required bool, vals map[string][]string) {
	label := fieldTitle(f)
	if required {
		label += " *"
	}
	values, labels := f.Choices()
	ui.Column(c.Key("field:" + id + ":" + f.Name)).Gap(4).Children(func() {
		if f.Type == "boolean" {
			on := len(vals[f.Name]) > 0 && vals[f.Name][0] == "true"
			if a.switchRow(c, p, label, f.Description, &on) {
				vals[f.Name] = []string{strconv.FormatBool(on)}
			}
			return
		}
		ui.Text(c, label).FontSize(small).Bold().TextColor(p.ink2)
		if f.Description != "" {
			ui.Text(c, f.Description).FontSize(small).TextColor(p.ink3)
		}
		switch {
		case f.Type == "array" && len(values) > 0:
			ui.Row(c).Wrap().Gap(12).Children(func() {
				for i, v := range values {
					on := slices.Contains(vals[f.Name], v)
					if ui.Checkbox(c.Key("opt:"+v), &on, labels[i]).Changed() {
						if on {
							vals[f.Name] = append(vals[f.Name], v)
						} else {
							vals[f.Name] = slices.DeleteFunc(vals[f.Name], func(x string) bool { return x == v })
						}
					}
				}
			})
		case len(values) > 0:
			cur := ""
			if v := vals[f.Name]; len(v) > 0 {
				if i := slices.Index(values, v[0]); i >= 0 {
					cur = labels[i]
				}
			}
			if ui.Select(c.Key("sel"), &cur, labels).Label(label).Changed() {
				if i := slices.Index(labels, cur); i >= 0 {
					vals[f.Name] = []string{values[i]}
				}
			}
		default:
			text := ""
			if v := vals[f.Name]; len(v) > 0 {
				text = v[0]
			}
			if ui.TextInput(c.Key("in"), &text).Label(label).Changed() {
				vals[f.Name] = []string{text}
			}
		}
	})
}
