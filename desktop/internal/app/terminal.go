package app

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// ansiBase are the 16 colors of the terminal's palette, as xterm draws them on a dark and a light page.
var ansiDark = [16]string{"#2e3436", "#ef6b73", "#8ae234", "#fce94f", "#729fcf", "#ad7fa8", "#34e2e2", "#d3d7cf",
	"#555753", "#ff8f95", "#a5f06a", "#fff176", "#9ec2ef", "#c9a3c4", "#7ff2f2", "#eeeeec"}
var ansiLight = [16]string{"#2e3436", "#c01c28", "#26a269", "#a2734c", "#12488b", "#a347ba", "#2aa1b3", "#5e5c64",
	"#5e5c64", "#e01b24", "#2ec27e", "#c88800", "#2a7bde", "#c061cb", "#33c7de", "#3d3846"}

// ansiColor is a palette index (0–255) or a 24-bit value as a color for the page.
func ansiColor(v int, rgb, dark bool) ui.Color {
	if rgb {
		return ui.Hex(fmt.Sprintf("#%06x", v&0xffffff))
	}
	switch {
	case v < 16:
		if dark {
			return ui.Hex(ansiDark[v])
		}
		return ui.Hex(ansiLight[v])
	case v < 232:
		v -= 16
		level := func(n int) int {
			if n == 0 {
				return 0
			}
			return 55 + n*40
		}
		return ui.Hex(fmt.Sprintf("#%02x%02x%02x", level(v/36), level(v/6%6), level(v%6)))
	default:
		g := 8 + (v-232)*10
		return ui.Hex(fmt.Sprintf("#%02x%02x%02x", g, g, g))
	}
}

// ansiLine draws one line of the terminal with its colors and emphasis.
func (a *App) ansiLine(c *ui.Context, p palette, line string, onDark bool) {
	runs := hubapi.ParseSGR(line)
	if len(runs) == 0 {
		ui.Text(c, " ").Font("monospace").FontSize(small)
		return
	}
	spans := make([]ui.Span, 0, len(runs))
	for _, r := range runs {
		fg, bg := p.ink, ui.Color{}
		if r.FG >= 0 {
			fg = ansiColor(r.FG, r.FGRGB, onDark)
		}
		hasBG := r.BG >= 0
		if hasBG {
			bg = ansiColor(r.BG, r.BGRGB, onDark)
		}
		if r.Rev {
			if !hasBG {
				bg = p.inset
			}
			fg, bg, hasBG = bg, fg, true
		}
		sp := ui.Span{Text: r.Text, Font: "monospace", Size: small, Color: fg, Italic: r.Italic, Underline: r.Under}
		if r.Bold {
			sp.Weight = 700
		}
		if r.Dim {
			sp.Color = p.ink3
		}
		if hasBG {
			sp.Background = bg
		}
		spans = append(spans, sp)
	}
	ui.RichText(c, spans...).Selectable()
}

// termKey is the name the hub types a key by ("enter", "ctrl+c", "shift+tab"…); "" for a key it leaves to the text.
func termKey(k ui.Key, mods ui.Modifiers) string {
	names := map[ui.Key]string{ui.KeyEnter: "enter", ui.KeyEscape: "esc", ui.KeyBackspace: "backspace", ui.KeyTab: "tab",
		ui.KeyDelete: "delete", ui.KeyHome: "home", ui.KeyEnd: "end", ui.KeyPageUp: "pageup", ui.KeyPageDown: "pagedown",
		ui.KeyLeft: "left", ui.KeyRight: "right", ui.KeyUp: "up", ui.KeyDown: "down", ui.KeyInsert: "insert",
		ui.KeyF1: "f1", ui.KeyF2: "f2", ui.KeyF3: "f3", ui.KeyF4: "f4", ui.KeyF5: "f5", ui.KeyF6: "f6",
		ui.KeyF7: "f7", ui.KeyF8: "f8", ui.KeyF9: "f9", ui.KeyF10: "f10", ui.KeyF11: "f11", ui.KeyF12: "f12"}
	name, ok := names[k]
	if !ok && mods&ui.Ctrl != 0 {
		for i, lk := range []ui.Key{ui.KeyA, ui.KeyB, ui.KeyC, ui.KeyD, ui.KeyE, ui.KeyF, ui.KeyG, ui.KeyH, ui.KeyI, ui.KeyJ, ui.KeyK,
			ui.KeyL, ui.KeyM, ui.KeyN, ui.KeyO, ui.KeyP, ui.KeyQ, ui.KeyR, ui.KeyS, ui.KeyT, ui.KeyU, ui.KeyV, ui.KeyW, ui.KeyX,
			ui.KeyY, ui.KeyZ} {
			if lk == k {
				name, ok = string(rune('a'+i)), true
			}
		}
		if !ok {
			return ""
		}
	}
	if !ok {
		return ""
	}
	if mods&ui.Ctrl != 0 {
		name = "ctrl+" + name
	}
	if mods&ui.Alt != 0 {
		name = "alt+" + name
	}
	if mods&ui.Shift != 0 && (k == ui.KeyTab || k == ui.KeyEnter) {
		name = "shift+" + name
	}
	return name
}

// readHistory reads the page of scrollback above what is shown.
func (a *App) readHistory(key string) {
	st := a.term(key)
	if st.histBusy {
		return
	}
	shown := append(slices.Clone(st.history), strings.Split(st.screen.Text, "\n")...)
	var plain []string
	for _, l := range shown {
		plain = append(plain, hubapi.PlainLine(l))
	}
	for len(plain) > 0 && plain[len(plain)-1] == "" {
		plain = plain[:len(plain)-1]
	}
	anchor := plain[:min(20, len(plain))]
	st.histBusy, st.histErr = true, ""
	c := a.conn
	machine, session, _ := strings.Cut(key, "/")
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pg, err := c.client.ScreenBefore(ctx, machine, session, anchor, 200)
		a.Host.Post(func() {
			st.histBusy = false
			if !a.live(c) {
				return
			}
			if err != nil {
				st.histErr = a.errorText(err)
				return
			}
			lines := strings.Split(strings.ReplaceAll(pg.Text, "\r\n", "\n"), "\n")
			if pg.Text == "" {
				lines = nil
			}
			st.history = append(lines, st.history...)
			st.histMore, st.histRead = pg.HasMore, true
		})
	})
}

// terminalPane is a terminal: its screen with its colors, read every moment while shown, the scrollback above it on
// request, and typing straight into it (or a line and keys) where the session takes keys. A plain shell's pane
// (`pane:` address) is shown only.
func (a *App) terminalPane(c *ui.Context, p palette, key string, x hubapi.Session) {
	st := a.term(key)
	a.readScreen(key)
	_, session, _ := strings.Cut(key, "/")
	can := x.Can("terminal") && !strings.HasPrefix(session, "pane:")
	isDark := p.page == dark.page
	if st.follow {
		st.scroll.Y = math.MaxFloat32
	}
	box := ui.Scroll(c.Key("term-" + key)).Grow(1).Background(p.inset).TrackScroll(&st.scroll)
	if can {
		box.Focusable().FocusRing(true).HandleInput(func(ev ui.InputEvent) bool {
			switch ev.Kind {
			case ui.InputText:
				if ev.Text != "" {
					a.sendKeys(key, ev.Text)
					st.follow = true
					return true
				}
			case ui.InputKeyDown:
				if name := termKey(ev.Key, ev.Mods); name != "" {
					a.sendKeys(key, "", name)
					st.follow = true
					return true
				}
			}
			return false
		})
	}
	box.Children(func() {
		ui.Column(c).Padding(12, pagePad).Children(func() {
			if st.loaded && (!st.histRead || st.histMore) {
				ui.Row(c).Padding(0, 0, 8, 0).Children(func() {
					if st.histBusy {
						ui.Spinner(c)
					} else if ui.Button(c, a.tr("terminalEarlier")).FontSize(small).Padding(2, 10).Clicked() {
						st.follow = false
						a.readHistory(key)
					}
					if st.histErr != "" {
						ui.Text(c, st.histErr).FontSize(small).TextColor(p.red).Padding(0, 8)
					}
				})
			}
			switch {
			case st.loaded:
				for i, l := range st.history {
					ui.Column(c.Key("h" + itoa(i))).Children(func() { a.ansiLine(c, p, l, isDark) })
				}
				text := strings.TrimRight(st.screen.Text, "\n ")
				if text == "" && len(st.history) == 0 {
					ui.Text(c, a.tr("terminalBlank")).Font("monospace").FontSize(small).TextColor(p.ink3)
				}
				for i, l := range strings.Split(text, "\n") {
					ui.Column(c.Key("s" + itoa(i))).Children(func() { a.ansiLine(c, p, l, isDark) })
				}
			case st.err != "":
				a.empty(c, p, "warning", st.err, "")
			default:
				ui.Spinner(c).Margin(24, ui.Auto)
			}
		})
	})
	if !st.scroll.Scrolling() && st.scroll.MaxY > 0 && st.scroll.Y < st.scroll.MaxY-4 {
		st.follow = false
	}
	if st.loaded && st.err != "" {
		ui.Text(c, st.err).FontSize(small).TextColor(p.red).Padding(4, pagePad)
	}
	ui.Divider(c)
	ui.Column(c).Padding(10, pagePad, 14, pagePad).Gap(8).Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			in := ui.TextInput(c.Key("term-input-"+key), &st.input).Placeholder(a.tr("terminalType")).Label(a.tr("terminalType")).
				Font("monospace").Grow(1).Disabled(!can)
			if in.Submitted() && can {
				a.sendKeys(key, st.input, "enter")
				st.input, st.follow = "", true
			}
			if st.sending {
				ui.Spinner(c)
			}
		})
		ui.Row(c).Gap(6).Wrap().Children(func() {
			for _, k := range []struct{ label, name string }{
				{"Esc", "esc"}, {"Tab", "tab"}, {"⇧Tab", "shift+tab"}, {"⌃C", "ctrl+c"}, {"⌃D", "ctrl+d"}, {"←", "left"}, {"↑", "up"}, {"↓", "down"}, {"→", "right"}, {"⏎", "enter"},
			} {
				if ui.Button(c.Key("key-"+k.name), k.label).Font("monospace").FontSize(small).Padding(3, 10).
					Label(k.name).Tooltip(k.name).Disabled(!can).Clicked() {
					a.sendKeys(key, "", k.name)
				}
			}
			ui.Spacer(c)
			switch {
			case !can:
				ui.Text(c, a.tr("terminalReadOnly")).FontSize(small).TextColor(p.ink3)
			default:
				ui.Text(c, a.tr("terminalTypeHint")).FontSize(small).TextColor(p.ink3)
			}
		})
	})
}
