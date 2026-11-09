package app

import (
	"context"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// What the main conversation still needs (docs/assistant/onboarding.md 4.4, docs/protocol/protocol.md "Main chat readiness"):
// in setup, a card above the composer with the first missing step, one line why and one button; in chat_only, a note.

type readinessState struct {
	conn    *conn
	asked   bool
	busy    bool
	r       *hubapi.Readiness
	read    time.Time
	dismiss bool
}

// readinessBlocking are the steps that decide the mode, in order.
var readinessBlocking = []string{"computer", "claude", "model"}

// blockingStep is the step the card shows: in setup, the first of computer, claude and model still to do or broken
// (a rate limit passes by itself), else the one the hub names next.
func blockingStep(r *hubapi.Readiness) *hubapi.ReadinessStep {
	if r == nil || r.Mode != "setup" {
		return nil
	}
	for _, id := range readinessBlocking {
		i := slices.IndexFunc(r.Steps, func(s hubapi.ReadinessStep) bool { return s.ID == id })
		if i < 0 {
			continue
		}
		s := r.Steps[i]
		switch {
		case s.State == "ok", s.State == "skipped", s.State == "unknown", s.State == "checking", s.Code == "rate_limited":
			continue
		}
		return &r.Steps[i]
	}
	if r.Next == nil || *r.Next == "first_message" || slices.Contains(readinessBlocking, *r.Next) {
		return nil
	}
	if i := slices.IndexFunc(r.Steps, func(s hubapi.ReadinessStep) bool { return s.ID == *r.Next }); i >= 0 && r.Steps[i].State != "ok" && r.Steps[i].State != "skipped" {
		return &r.Steps[i]
	}
	return nil
}

func (a *App) readReadiness(probe bool) {
	g := &a.ready
	if g.conn != a.conn {
		*g = readinessState{conn: a.conn}
	}
	if g.busy || (!probe && g.asked && time.Since(g.read) < time.Minute) {
		return
	}
	g.asked, g.busy = true, true
	c := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var r hubapi.Readiness
		var err error
		if probe {
			r, err = c.client.ReadinessProbe(ctx)
		} else {
			r, err = c.client.Readiness(ctx)
		}
		a.Host.Post(func() {
			g.busy, g.read = false, time.Now()
			if err != nil || !a.live(c) {
				return // a hub without readiness shows nothing here
			}
			g.r = &r
		})
	})
}

func (a *App) readinessAction(id string) string {
	switch id {
	case "add_computer":
		return a.tr("readyAddComputer")
	case "confirm_computer":
		return a.tr("readyConfirmComputer")
	case "wake_computer", "install_claude", "login_claude":
		return a.tr("readyOpenComputers")
	case "connect_model", "edit_provider":
		return a.tr("readyModels")
	case "test_assistant":
		return a.tr("readyTest")
	case "enable_tls", "open_admin":
		return a.tr("readyAdmin")
	}
	return ""
}

func (a *App) runReadiness(id string) {
	switch id {
	case "add_computer", "confirm_computer", "wake_computer", "install_claude", "login_claude":
		a.Navigate("/machines")
	case "connect_model", "edit_provider":
		a.sets.tab = 4
		a.Navigate("/settings")
	case "test_assistant":
		a.readReadiness(true)
	case "enable_tls", "open_admin":
		a.Host.OpenURL(a.conn.hub.URL + "/admin")
	}
}

// readinessCard shows above the main chat's composer what it still needs.
func (a *App) readinessCard(c *ui.Context, p palette) {
	a.readReadiness(false)
	g := &a.ready
	if g.r == nil || g.dismiss {
		return
	}
	if g.r.Mode == "chat_only" {
		ui.Text(c, a.tr("readyChatOnly")).FontSize(small).TextColor(p.ink3).Padding(4, pagePad).TextAlign(ui.Center)
		return
	}
	s := blockingStep(g.r)
	if s == nil {
		return
	}
	ui.Row(c).Margin(4, pagePad).Padding(12, 14).Gap(12).Radius(12).Border(1, p.orange).Background(p.surface).Children(func() {
		ui.Icon(c, icon("warning")).TextColor(p.orange)
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			title := s.Title
			if title == "" {
				title = a.tr("readyOneMore")
			}
			ui.Text(c, title).Bold()
			if s.Detail != "" {
				ui.Text(c, s.Detail).FontSize(small).TextColor(p.ink2).MaxLines(3)
			}
		})
		if s.Action != nil {
			if label := a.readinessAction(s.Action.ID); label != "" {
				if ui.Button(c, label).Disabled(g.busy).Clicked() {
					a.runReadiness(s.Action.ID)
				}
			}
		}
		if ui.Button(c, "").Label(a.tr("readyHide")).Tooltip(a.tr("readyHide")).Padding(4).Children(func() { ui.Icon(c, icon("x")).TextColor(p.ink3) }).Clicked() {
			g.dismiss = true
		}
	})
}
