package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// usageEvery is how often the sidebar asks the hub for the subscription windows again.
const usageEvery = time.Minute

// usageState is the sidebar's subscription bars (docs/server/usage-windows.md): the accounts as the hub last gave them.
type usageState struct {
	accounts []hubapi.UsageAccount
	at       time.Time // when they were last asked for
	busy     bool
	open     bool
}

// pollUsage asks again once a minute; a timer wakes a frame for the next one.
func (a *App) pollUsage() {
	g := &a.usage
	if g.busy || (!g.at.IsZero() && time.Since(g.at) < usageEvery) {
		return
	}
	cn := a.conn
	g.busy, g.at = true, time.Now()
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		accounts, err := cn.client.UsageWindows(ctx)
		a.Host.Post(func() {
			if !a.live(cn) {
				return
			}
			g.busy = false
			if err == nil {
				g.accounts = accounts
			}
			a.After(usageEvery, func() {})
		})
	})
}

func shownPercent(p float64) string { return fmt.Sprintf("%d%%", int(p+0.5)) }

func (p palette) usageColor(used float64) ui.Color {
	if used >= hubapi.UsageWarn {
		return p.orange
	}
	return p.accent
}

// meter is a bar: the track, the used part (caution colour from 80%) and, with expected ≥ 0, a tick for an even pace.
func (a *App) meter(c *ui.Context, p palette, used, expected float64, tall bool) {
	h := float32(4)
	if tall {
		h = 6
	}
	used = min(max(used, 0), 100)
	ui.Row(c).Height(h).Radius(h / 2).Background(p.hover).Children(func() {
		if used > 0 {
			ui.Box(c).Height(h).WidthPercent(float32(used)).Radius(h / 2).Background(p.usageColor(used))
		}
		if expected >= 0 {
			ui.Box(c).Absolute().LeftPercent(float32(min(expected, 100))).Top(-1).Size(2, h+2).Radius(1).Background(p.ink2)
		}
	})
}

// usageBars is the foot of the sidebar: the two fullest accounts as thin bars; a click opens every window of every account.
// Nothing is drawn while the hub has no data.
func (a *App) usageBars(c *ui.Context, p palette) {
	a.pollUsage()
	g := &a.usage
	now := time.Now()
	live := hubapi.LiveUsage(g.accounts, now)
	top := hubapi.TopUsage(live, 2)
	if len(top) == 0 {
		return
	}
	btn := ui.ButtonBase(c.Key("usage-bars")).Label(a.tr("usageTitle")).Tooltip(a.tr("usageTitle")).Padding(8, 10).Radius(rowRadius).Margin(0, 10, 8, 10)
	if btn.Hovered() {
		btn.Background(p.hover)
	}
	btn.Children(func() {
		ui.Column(c).Gap(6).Grow(1).Children(func() {
			for _, acc := range top {
				used := acc.Fullest()
				col := ui.Column(c.Key("usage-" + acc.ID)).Gap(4)
				if acc.Stale {
					col.Opacity(0.6)
				}
				col.Children(func() {
					ui.Row(c).Gap(8).Children(func() {
						ui.Text(c, acc.Name).FontSize(small).TextColor(p.ink3).Grow(1).Shrink(1).SingleLine()
						tone := p.ink2
						if used >= hubapi.UsageWarn {
							tone = p.orange
						}
						ui.Text(c, shownPercent(used)).FontSize(small).TextColor(tone).SingleLine()
					})
					a.meter(c, p, used, -1, false)
				})
			}
		})
	})
	if btn.Clicked() {
		g.open = !g.open
	}
	ui.Popover(c, btn, &g.open, func() {
		ui.Scroll(c).MaxHeight(480).Width(300).Children(func() {
			ui.Column(c).Padding(12).Gap(14).Children(func() {
				for _, acc := range live {
					a.usageAccount(c, p, acc, now)
				}
			})
		})
	})
}

func (a *App) usageAccount(c *ui.Context, p palette, acc hubapi.UsageAccount, now time.Time) {
	col := ui.Column(c.Key("usage-acc-" + acc.ID)).Gap(8)
	if acc.Stale {
		col.Opacity(0.7)
	}
	col.Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.Text(c, acc.Name).Bold().Grow(1).Shrink(1).SingleLine()
				age := a.ago(acc.SampledAt, now)
				if acc.Stale {
					age = a.tr("usageStale", "age", age)
				}
				ui.Text(c, age).FontSize(small).TextColor(p.ink3).SingleLine()
			})
			var from []string
			if n := acc.PlanNote(); n != "" {
				from = append(from, n)
			}
			if acc.Local {
				from = append(from, a.tr("usageLocal"))
			}
			from = append(from, acc.Sources...)
			if len(from) > 0 {
				ui.Text(c, strings.Join(from, " · ")).FontSize(small).TextColor(p.ink3).SingleLine()
			}
			switch acc.Problem {
			case "unreachable":
				ui.Text(c, a.tr("usageUnreachable")).FontSize(small).TextColor(p.orange)
			case "error":
				ui.Text(c, a.tr("usageError")).FontSize(small).TextColor(p.orange)
			}
		})
		for i, w := range acc.Windows {
			state, expected, ok := w.Pace(now)
			if !ok {
				expected = -1
			}
			ui.Column(c.Key(fmt.Sprintf("w-%s-%d", acc.ID, i))).Gap(4).Children(func() {
				if w.Model != "" {
					ui.Text(c, w.Model).FontSize(small).TextColor(p.ink3).SingleLine()
				}
				ui.Row(c).Gap(8).Children(func() {
					ui.Text(c, w.Label).FontSize(small).TextColor(p.ink2).Width(28).SingleLine()
					tone := p.ink
					if w.UsedPercent >= hubapi.UsageWarn {
						tone = p.orange
					}
					ui.Text(c, shownPercent(w.UsedPercent)).FontSize(small).TextColor(tone).SingleLine()
					switch state {
					case "over":
						ui.Text(c, a.tr("usagePaceOver")).FontSize(small).TextColor(p.orange).SingleLine()
					case "under":
						ui.Text(c, a.tr("usagePaceUnder")).FontSize(small).TextColor(p.green).SingleLine()
					}
					ui.Spacer(c)
					ui.Text(c, a.resetLabel(w.ResetsAt, now)).FontSize(small).TextColor(p.ink3).SingleLine()
				})
				a.meter(c, p, w.UsedPercent, expected, true)
			})
		}
	})
}

// resetLabel is when a window starts over: "Resets in 55m", "Resets in 2d 3h", "Resets at 13:05" today, or "Resets soon".
func (a *App) resetLabel(at string, now time.Time) string {
	t := hubapi.ParseTime(at)
	if t.IsZero() {
		return ""
	}
	d := t.Sub(now)
	if d <= 0 {
		return a.tr("usageDue")
	}
	mins := int((d + time.Minute - 1) / time.Minute)
	if mins < 60 {
		return a.tr("usageResetIn", "countdown", a.tr("usageMinutes", "m", itoa(mins)))
	}
	if lt, ln := t.Local(), now.Local(); lt.YearDay() == ln.YearDay() && lt.Year() == ln.Year() {
		return a.tr("usageResetAt", "time", lt.Format("15:04"))
	}
	hours := mins / 60
	span := a.tr("usageHours", "h", itoa(hours))
	if days := hours / 24; days > 0 {
		span = a.tr("usageDaysHours", "d", itoa(days), "h", itoa(hours%24))
	}
	return a.tr("usageResetIn", "countdown", span)
}
