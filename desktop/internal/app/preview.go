package app

import (
	"context"
	"math"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// The Preview pane of an agent's page (docs/web/preview.md): its dev server's page, which a headless browser on that
// computer draws. The pictures come over a WebSocket; the mouse, wheel and keys over it go back to the page.

// phoneSize is a phone's viewport in CSS pixels: what Phone shows.
var phoneSize = hubapi.PreviewSize{W: 390, H: 844, DPR: 2, Mobile: true}

// previewState is one agent's preview.
type previewState struct {
	servers  hubapi.PreviewServers
	loaded   bool
	loading  bool
	err      string
	confirm  *hubapi.PreviewOption // a launch.json entry asked to start
	starting *hubapi.PreviewOption // started, waiting for its port
	since    time.Time
	busy     bool

	pv      *hubapi.Preview
	option  hubapi.PreviewOption
	frame   ui.ImageSource
	fw, fh  int
	page    hubapi.PreviewEvent
	address string
	mobile  int // 0 desktop, 1 phone
	sent    hubapi.PreviewSize
	status  string // why the preview ended or what the page could not load
	extern  string // an address elsewhere the page wanted
	dialog  *hubapi.PreviewEvent
	answer  string
	buttons int    // the mouse buttons held, as CDP counts them
	copied  string // what ⌘C copied in the page, for the clipboard here
}

func previewOn(features []string) bool { return slices.Contains(features, hubapi.FeaturePreview) }

func (a *App) previewOf(key string) *previewState {
	if a.previews == nil {
		a.previews = map[string]*previewState{}
	}
	s := a.previews[key]
	if s == nil {
		s = &previewState{}
		a.previews[key] = s
	}
	return s
}

// closePreviews ends every open preview (a new hub, signing out).
func (a *App) closePreviews() {
	for _, s := range a.previews {
		if s.pv != nil {
			s.pv.Close()
		}
	}
	a.previews = nil
}

// readServers asks the session's computer for its launch.json servers.
func (a *App) readServers(key string, x hubapi.Session) {
	st := a.previewOf(key)
	if st.loading {
		return
	}
	cn := a.conn
	st.loading = true
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := cn.client.PreviewServersOf(ctx, x.MachineID, x.Cwd)
		a.Host.Post(func() {
			if !a.live(cn) {
				return
			}
			st.loading, st.loaded = false, true
			st.err = ""
			if err != nil {
				st.err = a.errorText(err)
				return
			}
			st.servers = res
			if o := st.starting; o != nil {
				for _, n := range hubapi.PreviewOptions(res, x.Ports) {
					if n.Key == o.Key && n.Listening {
						st.starting = nil
						a.openPreview(key, x, n)
						return
					}
				}
				if time.Since(st.since) > 90*time.Second {
					st.starting = nil
					st.status = a.tr("previewStartTimeout", "name", o.Name)
					return
				}
				a.After(1500*time.Millisecond, func() {
					if st.starting != nil {
						a.readServers(key, x)
					}
				})
			}
		})
	})
}

// startServer runs a launch.json entry in a new terminal tab, then waits for its port.
func (a *App) startServer(key string, x hubapi.Session, o hubapi.PreviewOption) {
	st := a.previewOf(key)
	cn := a.conn
	st.busy = true
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := cn.client.StartPreview(ctx, x.MachineID, x.Cwd, o.Name, x.WorkspaceID, x.SpaceKind)
		a.Host.Post(func() {
			if !a.live(cn) {
				return
			}
			st.busy, st.confirm = false, nil
			if err != nil {
				a.toast(a.tr("previewStartFailed", "name", o.Name, "error", a.errorText(err)))
				return
			}
			st.starting, st.since, st.status = &o, time.Now(), ""
			a.readServers(key, x)
		})
	})
}

// previewSize is the size the pane shows the page at: the area's, or a phone's.
func (st *previewState) previewSize(area ui.Rect) hubapi.PreviewSize {
	if st.mobile == 1 {
		return phoneSize
	}
	clamp := func(v float32, lo, hi int) int { return min(max(int(v), lo), hi) }
	if area.W <= 0 || area.H <= 0 {
		return hubapi.PreviewSize{W: 1024, H: 700, DPR: 1}
	}
	return hubapi.PreviewSize{W: clamp(area.W, 200, 3840), H: clamp(area.H, 200, 2400), DPR: 1}
}

// openPreview connects to a server's page: a port no agent listens on is opened first.
func (a *App) openPreview(key string, x hubapi.Session, o hubapi.PreviewOption) {
	st := a.previewOf(key)
	if st.pv != nil {
		st.pv.Close()
		st.pv = nil
	}
	cn := a.conn
	size := st.previewSize(a.previewArea)
	st.option, st.busy, st.status, st.extern, st.frame, st.page = o, true, "", "", nil, hubapi.PreviewEvent{}
	st.address, st.sent = o.Path, size
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var pv *hubapi.Preview
		err := func() error {
			if o.Launch {
				if err := cn.client.OpenPort(ctx, x.MachineID, o.Port); err != nil {
					return err
				}
			}
			var err error
			pv, err = cn.client.OpenPreview(ctx, x.MachineID, o.Port, o.Path, size, func(ev hubapi.PreviewEvent) {
				var img ui.ImageSource
				if ev.T == "frame" {
					img, _ = ui.DecodeImage(ev.Frame)
				}
				a.Host.Post(func() { a.previewEvent(key, pv, ev, img) })
			})
			return err
		}()
		a.Host.Post(func() {
			if !a.live(cn) {
				if pv != nil {
					pv.Close()
				}
				return
			}
			st.busy = false
			if err != nil {
				st.status = a.tr("previewOpenFailed", "error", a.errorText(err))
				return
			}
			st.pv = pv
		})
	})
}

// previewCodes are the reasons a preview could not open or ended, by the code the computer gives.
func (a *App) previewProblem(code, fallback string) string {
	switch code {
	case "port_gone":
		return a.tr("previewPortGone")
	case "no_browser":
		return a.tr("previewNoBrowser")
	case "offline":
		return a.tr("wbMachineOffline")
	case "tab_closed", "browser_exited":
		return a.tr("previewEnded")
	}
	if fallback != "" {
		return fallback
	}
	return a.tr("previewEnded")
}

func (a *App) previewEvent(key string, pv *hubapi.Preview, ev hubapi.PreviewEvent, img ui.ImageSource) {
	st := a.previewOf(key)
	if pv == nil || (st.pv != nil && st.pv != pv) {
		return
	}
	switch ev.T {
	case "frame":
		if img != nil {
			st.frame, st.fw, st.fh = img, ev.FW, ev.FH
		}
		pv.Send(map[string]any{"t": "ack", "seq": ev.Seq})
	case "page":
		if st.page.URL != ev.URL {
			st.address = previewAddress(ev.URL, st.option.Port)
		}
		st.page, st.status = ev, ""
	case "external":
		st.extern = ev.URL
	case "failed":
		st.status = a.tr("previewLoadFailed", "url", ev.URL, "error", ev.Error)
	case "dialog":
		d := ev
		st.dialog, st.answer = &d, ev.Default
	case "dialog_closed":
		st.dialog = nil
	case "copied":
		st.copied = ev.Text
	case "crashed":
		st.status = a.tr("previewCrashed")
	case "error", "closed":
		if ev.T == "closed" && st.status == "" || ev.T == "error" {
			st.status = a.previewProblem(ev.Code, ev.Error)
		}
		if ev.T == "closed" {
			pv.Close()
			if st.pv == pv {
				st.pv = nil
			}
		}
	}
}

// previewAddress is what the address field shows: the path when the page is on the server being previewed.
func previewAddress(raw string, port int) string {
	host, rest, ok := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(raw, "http://"), "https://"), "/")
	if !ok {
		rest = ""
	}
	for _, h := range []string{"localhost", "127.0.0.1", "[::1]"} {
		if host == h+":"+strconv.Itoa(port) {
			return "/" + rest
		}
	}
	return raw
}

// cdpMods are the modifiers as CDP counts them: Alt 1, Ctrl 2, Meta 4, Shift 8.
func cdpMods(m ui.Modifiers) int {
	n := 0
	if m&ui.Alt != 0 {
		n |= 1
	}
	if m&ui.Ctrl != 0 {
		n |= 2
	}
	if m&ui.Super != 0 {
		n |= 4
	}
	if m&ui.Shift != 0 {
		n |= 8
	}
	return n
}

// cdpButton is a pointer button (0 primary, 1 secondary, 2 middle) as CDP names it, and its bit in `buttons`.
func cdpButton(b int) (string, int) {
	switch b {
	case 1:
		return "right", 2
	case 2:
		return "middle", 4
	}
	return "left", 1
}

// domKey is a key that types nothing as the page knows it: its key, code and key code; ok false for the others.
func domKey(k ui.Key) (key string, code int, ok bool) {
	keys := map[ui.Key]struct {
		name string
		code int
	}{ui.KeyEnter: {"Enter", 13}, ui.KeyBackspace: {"Backspace", 8}, ui.KeyTab: {"Tab", 9}, ui.KeyEscape: {"Escape", 27},
		ui.KeyDelete: {"Delete", 46}, ui.KeyLeft: {"ArrowLeft", 37}, ui.KeyUp: {"ArrowUp", 38}, ui.KeyRight: {"ArrowRight", 39},
		ui.KeyDown: {"ArrowDown", 40}, ui.KeyHome: {"Home", 36}, ui.KeyEnd: {"End", 35}, ui.KeyPageUp: {"PageUp", 33},
		ui.KeyPageDown: {"PageDown", 34}}
	v, ok := keys[k]
	return v.name, v.code, ok
}

// editCommand is the page's editing command a shortcut stands for (⌘A / Ctrl+A…), or "".
func editCommand(k ui.Key, m ui.Modifiers, mac bool) string {
	mod := m&ui.Ctrl != 0 && m&ui.Super == 0
	if mac {
		mod = m&ui.Super != 0 && m&ui.Ctrl == 0
	}
	if !mod || m&ui.Alt != 0 {
		return ""
	}
	shift := m&ui.Shift != 0
	switch k {
	case ui.KeyA:
		if !shift {
			return "selectAll"
		}
	case ui.KeyC:
		if !shift {
			return "copy"
		}
	case ui.KeyX:
		if !shift {
			return "cut"
		}
	case ui.KeyZ:
		if shift {
			return "redo"
		}
		return "undo"
	case ui.KeyY:
		if !mac && !shift {
			return "redo"
		}
	}
	return ""
}

// fitIn is where a w×h picture is drawn inside an area (scaled down to fit, centered): its offset and scale.
func fitIn(area ui.Rect, w, h int) (x, y, scale float32) {
	if w <= 0 || h <= 0 || area.W <= 0 || area.H <= 0 {
		return 0, 0, 1
	}
	scale = min(area.W/float32(w), area.H/float32(h), 1)
	return (area.W - float32(w)*scale) / 2, (area.H - float32(h)*scale) / 2, scale
}

// previewInput sends what the pointer and keys do over the picture to the page.
func (a *App) previewInput(st *previewState, area ui.Rect, ev ui.InputEvent) bool {
	pv := st.pv
	if pv == nil || st.frame == nil {
		return false
	}
	ox, oy, scale := fitIn(area, st.fw, st.fh)
	px := int(math.Round(float64((ev.X - ox) / scale)))
	py := int(math.Round(float64((ev.Y - oy) / scale)))
	px, py = min(max(px, 0), st.fw), min(max(py, 0), st.fh)
	mods := cdpMods(ev.Mods)
	switch ev.Kind {
	case ui.InputPointerDown, ui.InputPointerUp:
		name, bit := cdpButton(ev.Button)
		kind := "down"
		if ev.Kind == ui.InputPointerDown {
			st.buttons |= bit
		} else {
			kind = "up"
			st.buttons &^= bit
		}
		pv.Send(map[string]any{"t": "mouse", "type": kind, "x": px, "y": py, "button": name, "buttons": st.buttons, "clicks": max(ev.Clicks, 1), "mods": mods})
		return true
	case ui.InputPointerMove:
		pv.Send(map[string]any{"t": "mouse", "type": "move", "x": px, "y": py, "button": "none", "buttons": st.buttons, "clicks": 0, "mods": mods})
		return true
	case ui.InputScroll:
		pv.Send(map[string]any{"t": "wheel", "x": px, "y": py, "dx": ev.DX / scale, "dy": ev.DY / scale, "mods": mods})
		return true
	case ui.InputText:
		if ev.Text != "" {
			pv.Send(map[string]any{"t": "text", "text": ev.Text})
			return true
		}
	case ui.InputKeyDown, ui.InputKeyUp:
		if ev.Kind == ui.InputKeyDown {
			if cmd := editCommand(ev.Key, ev.Mods, runtime.GOOS == "darwin"); cmd != "" {
				pv.Send(map[string]any{"t": "cmd", "name": cmd})
				return true
			}
		}
		name, code, ok := domKey(ev.Key)
		if !ok {
			return false
		}
		msg := map[string]any{"t": "key", "type": "up", "key": name, "code": name, "key_code": code, "mods": mods, "location": 0}
		if ev.Kind == ui.InputKeyDown {
			msg["type"] = "down"
			if ev.Repeat {
				msg["repeat"] = true
			}
			if name == "Enter" {
				msg["text"] = "\r"
			}
		}
		pv.Send(msg)
		return true
	}
	return false
}

func (a *App) previewPane(c *ui.Context, p palette, key string, x hubapi.Session) {
	st := a.previewOf(key)
	if st.copied != "" {
		c.WriteClipboard(st.copied)
		st.copied = ""
	}
	if !st.loaded && st.err == "" {
		a.readServers(key, x)
	}
	if st.pv != nil || (st.busy && st.starting == nil && st.confirm == nil) {
		a.previewView(c, p, key, x, st)
	} else {
		a.previewServers(c, p, key, x, st)
	}
	a.previewDialogs(c, p, key, x, st)
}

// previewServers is the list of servers to open, with Start for a launch.json entry that is not running.
func (a *App) previewServers(c *ui.Context, p palette, key string, x hubapi.Session, st *previewState) {
	opts := hubapi.PreviewOptions(st.servers, x.Ports)
	ui.Scroll(c.Key("pv-servers-" + key)).Grow(1).Children(func() {
		ui.Column(c).Padding(16, pagePad).Gap(12).MaxWidth(720).Children(func() {
			if st.status != "" {
				ui.Text(c, st.status).FontSize(small).TextColor(p.red)
			}
			switch {
			case !st.loaded:
				ui.Spinner(c).Margin(24, ui.Auto)
				return
			case st.err != "":
				a.empty(c, p, "warning", a.tr("previewServersFailed"), st.err)
				return
			}
			if st.loaded && st.servers.Browser == "" {
				ui.Text(c, a.tr("previewNoBrowser")).FontSize(small).TextColor(p.orange)
			}
			if st.servers.Error != "" {
				ui.Text(c, a.tr("previewLaunchBad", "error", st.servers.Error)).FontSize(small).TextColor(p.orange)
			}
			if len(opts) == 0 {
				a.empty(c, p, "globe", a.tr("previewNoServers"), a.tr("previewNoServersHint"))
				ui.Text(c, previewExample).Font("monospace").FontSize(small).Selectable().Padding(10, 12).Radius(rowRadius).Background(p.inset)
			}
			a.group(c, p, a.tr("previewServers"), "", func() {
				for _, o := range opts {
					ui.Row(c.Key("pv-"+o.Key)).Gap(10).Padding(10, 14).Children(func() {
						dot := p.ink3
						if o.Listening {
							dot = p.green
						}
						ui.Box(c).Size(7, 7).Radius(4).Background(dot)
						ui.Column(c).Gap(2).Grow(1).Shrink(1).Children(func() {
							ui.Text(c, o.Name).SingleLine()
							sub := joinDot(":"+itoa(o.Port), o.Title)
							if !o.Listening {
								sub = joinDot(sub, a.tr("previewNotRunning"))
							}
							ui.Text(c, sub).FontSize(small).TextColor(p.ink3).SingleLine()
						})
						switch {
						case st.starting != nil && st.starting.Key == o.Key:
							ui.Spinner(c)
							ui.Text(c, a.tr("previewWaiting")).FontSize(small).TextColor(p.ink3).SingleLine()
							if ui.Button(c, a.tr("cancel")).FontSize(small).Padding(2, 8).Clicked() {
								st.starting = nil
							}
						case o.Listening:
							if ui.PrimaryButton(c, a.tr("previewOpen")).FontSize(small).Padding(2, 10).Disabled(st.servers.Browser == "").Clicked() {
								a.openPreview(key, x, o)
							}
						case len(o.Command) > 0 && x.WorkspaceID != "":
							if ui.Button(c, a.tr("previewStart")).FontSize(small).Padding(2, 10).Clicked() {
								oc := o
								st.confirm = &oc
							}
						}
					})
				}
			})
			for _, s := range st.servers.Servers {
				if s.Problem != "" {
					ui.Text(c.Key("pb-"+s.Name), joinDot(s.Name, a.previewProblemText(s.Problem))).FontSize(small).TextColor(p.ink3)
				}
			}
			ui.Row(c).Children(func() {
				if ui.Button(c, a.tr("previewRefresh")).FontSize(small).Padding(2, 10).Disabled(st.loading).Clicked() {
					a.readServers(key, x)
				}
			})
		})
	})
}

// previewExample is what to put in .claude/launch.json.
const previewExample = `{
  "version": "0.0.1",
  "configurations": [
    {
      "name": "web",
      "runtimeExecutable": "npm",
      "runtimeArgs": ["run", "dev"],
      "port": 5173
    }
  ]
}`

func (a *App) previewProblemText(code string) string {
	switch code {
	case "no_name":
		return a.tr("previewProblemName")
	case "no_port":
		return a.tr("previewProblemPort")
	case "not_local":
		return a.tr("previewProblemLocal")
	case "no_run":
		return a.tr("previewProblemRun")
	}
	return a.tr("previewProblemEnv")
}

// previewView is the open page: the toolbar and the picture, which takes the mouse and keys.
func (a *App) previewView(c *ui.Context, p palette, key string, x hubapi.Session, st *previewState) {
	ui.Row(c).Gap(6).Padding(8, pagePad).Children(func() {
		if a.iconButton(c, p, "back", a.tr("previewBack"), !st.page.Back || st.pv == nil) {
			st.pv.Send(map[string]any{"t": "back"})
		}
		if a.iconButton(c, p, "forward", a.tr("previewForward"), !st.page.Forward || st.pv == nil) {
			st.pv.Send(map[string]any{"t": "forward"})
		}
		if a.iconButton(c, p, "refresh", a.tr("previewReload"), st.pv == nil) {
			st.pv.Send(map[string]any{"t": "reload"})
		}
		in := ui.TextInput(c.Key("pv-addr-"+key), &st.address).Label(a.tr("previewAddress")).Placeholder("/").Grow(1).Font("monospace").FontSize(small)
		if in.Submitted() && st.pv != nil {
			st.pv.Send(map[string]any{"t": "nav", "url": strings.TrimSpace(st.address)})
		}
		if st.page.Loading || st.busy {
			ui.Spinner(c)
		}
		before := st.mobile
		ui.Segmented(c.Key("pv-size-"+key), &st.mobile, a.tr("previewDesktop"), a.tr("previewPhone"))
		if before != st.mobile && st.pv != nil {
			st.sent = st.previewSize(a.previewArea)
			st.pv.Send(map[string]any{"t": "size", "w": st.sent.W, "h": st.sent.H, "dpr": st.sent.DPR, "mobile": st.sent.Mobile})
		}
		if a.iconButton(c, p, "x", a.tr("previewClose"), false) {
			if st.pv != nil {
				st.pv.Close()
			}
			st.pv, st.frame, st.busy, st.status = nil, nil, false, ""
		}
	})
	if st.extern != "" {
		ui.Row(c).Gap(8).Padding(6, pagePad).Background(p.inset).Children(func() {
			ui.Text(c, a.tr("previewExternal", "url", st.extern)).FontSize(small).TextColor(p.ink2).Grow(1).Shrink(1).SingleLine()
			if strings.HasPrefix(st.extern, "http://") || strings.HasPrefix(st.extern, "https://") {
				if ui.Button(c, a.tr("viewOpenBrowser")).FontSize(small).Padding(2, 8).Clicked() {
					a.Host.OpenURL(st.extern)
					st.extern = ""
				}
			}
			if a.iconButton(c, p, "x", a.tr("previewDismiss"), false) {
				st.extern = ""
			}
		})
	}
	if st.status != "" {
		ui.Text(c, st.status).FontSize(small).TextColor(p.red).Padding(4, pagePad)
	}
	ui.Divider(c)
	area := ui.Column(c.Key("pv-area-" + key)).Grow(1).Clip().Background(p.inset).AlignItems(ui.Center).Justify(ui.Center).
		Focusable().FocusRing(false).Label(a.tr("previewPage"))
	bounds := area.Bounds()
	a.previewArea = bounds
	area.HandleInput(func(ev ui.InputEvent) bool { return a.previewInput(st, bounds, ev) })
	area.Children(func() {
		if st.frame == nil {
			ui.Spinner(c)
			return
		}
		_, _, scale := fitIn(bounds, st.fw, st.fh)
		ui.Image(c, st.frame).Size(float32(st.fw)*scale, float32(st.fh)*scale).Label(st.page.Title)
	})
	// the panel was resized: the page follows (a phone keeps its size)
	if st.pv != nil && st.mobile == 0 && bounds.W > 0 {
		if want := st.previewSize(bounds); abs(want.W-st.sent.W) > 4 || abs(want.H-st.sent.H) > 4 {
			st.sent = want
			st.pv.Send(map[string]any{"t": "size", "w": want.W, "h": want.H, "dpr": want.DPR, "mobile": false})
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// previewDialogs are Start's confirmation (the exact command) and the page's own alert / confirm / prompt.
func (a *App) previewDialogs(c *ui.Context, p palette, key string, x hubapi.Session, st *previewState) {
	if o := st.confirm; o != nil {
		open := true
		ui.Modal(c, &open, func() {
			ui.Column(c).Width(520).Padding(20).Gap(12).Children(func() {
				ui.Text(c, a.tr("previewStartTitle", "name", o.Name)).FontSize(heading).Bold()
				ui.Text(c, a.tr("previewStartBody")).TextColor(p.ink2)
				ui.Text(c, hubapi.CommandLine(*o)).Font("monospace").FontSize(small).Selectable().Padding(10, 12).Radius(rowRadius).Background(p.inset)
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, a.tr("cancel")).Clicked() {
						st.confirm = nil
					}
					if ui.PrimaryButton(c, a.tr("previewStart")).Disabled(st.busy).Clicked() {
						a.startServer(key, x, *o)
					}
				})
			})
		})
		if !open {
			st.confirm = nil
		}
	}
	if d := st.dialog; d != nil && st.pv != nil {
		open := true
		answer := func(accept bool) {
			msg := map[string]any{"t": "dialog", "accept": accept}
			if d.Kind == "prompt" && accept {
				msg["text"] = st.answer
			}
			st.pv.Send(msg)
			st.dialog = nil
		}
		ui.Modal(c, &open, func() {
			ui.Column(c).Width(440).Padding(20).Gap(12).Children(func() {
				ui.Text(c, a.tr("previewDialogTitle")).FontSize(heading).Bold()
				ui.Text(c, d.Message).TextColor(p.ink2).Selectable()
				if d.Kind == "prompt" {
					ui.TextInput(c.Key("pv-prompt"), &st.answer).Label(d.Message)
				}
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if d.Kind != "alert" && ui.Button(c, a.tr("cancel")).Clicked() {
						answer(false)
					}
					if ui.PrimaryButton(c, a.tr("previewOK")).Clicked() {
						answer(true)
					}
				})
			})
		})
		if !open {
			answer(d.Kind == "alert")
		}
	}
}

// iconButton is a toolbar button drawn as an icon, its label the tooltip; it answers whether it was clicked.
func (a *App) iconButton(c *ui.Context, p palette, name, l string, disabled bool) bool {
	return ui.Button(c.Key("ib-"+name+l), "").Label(l).Tooltip(l).Padding(4).Disabled(disabled).
		Children(func() { ui.Icon(c, icon(name)).TextColor(p.ink2) }).Clicked()
}
