package app

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// An agent's page shows its chat, its terminal or its changes.
const (
	paneChat = iota
	paneTerminal
	paneChanges
)

// termState is an agent's terminal as last read, and what is being typed into it.
type termState struct {
	screen  hubapi.Screen
	loaded  bool
	loading bool
	read    time.Time
	err     string
	input   string
	sending bool
}

// changesState is the agent's folder's Git status and the diffs read.
type changesState struct {
	status  hubapi.GitStatus
	loaded  bool
	loading bool
	err     string
	chosen  string
	diffs   map[string]*diffState
}

type diffState struct {
	diff    hubapi.GitDiff
	loaded  bool
	loading bool
	err     string
}

// termEvery is how often the terminal is read while it is shown.
const termEvery = 1500 * time.Millisecond

func (a *App) term(key string) *termState {
	if a.terms == nil {
		a.terms = map[string]*termState{}
	}
	s := a.terms[key]
	if s == nil {
		s = &termState{}
		a.terms[key] = s
	}
	return s
}

// readScreen reads the terminal when it is due, and asks for a frame when the next read is.
func (a *App) readScreen(key string) {
	st := a.term(key)
	if st.loading || time.Since(st.read) < termEvery {
		return
	}
	st.loading = true
	c := a.conn
	machine, session, _ := strings.Cut(key, "/")
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		scr, err := c.client.Screen(ctx, machine, session, 60)
		a.Host.Post(func() {
			st.loading, st.read = false, time.Now()
			if !a.live(c) {
				return
			}
			if err != nil {
				st.err = a.errorText(err)
			} else {
				st.screen, st.loaded, st.err = scr, true, ""
			}
			// a frame when the next read is due; a page that no longer shows the terminal reads nothing
			a.After(termEvery, func() {})
		})
	})
}

// sendKeys types into the terminal and reads it again soon after.
func (a *App) sendKeys(key, text string, keys ...string) {
	st := a.term(key)
	c := a.conn
	machine, session, _ := strings.Cut(key, "/")
	st.sending = true
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := c.client.Keys(ctx, machine, session, text, keys...)
		a.Host.Post(func() {
			st.sending = false
			if !a.live(c) {
				return
			}
			if err != nil {
				a.toast(a.errorText(err))
				return
			}
			st.read = time.Time{}
		})
	})
}

// terminalPane is the agent's terminal: its screen, read every moment while shown, and a line and keys to type with.
func (a *App) terminalPane(c *ui.Context, p palette, key string, x hubapi.Session) {
	st := a.term(key)
	a.readScreen(key)
	ui.Scroll(c.Key("term-" + key)).Grow(1).Background(p.inset).Children(func() {
		ui.Column(c).Padding(12, pagePad).Children(func() {
			switch {
			case st.loaded:
				text := strings.TrimRight(st.screen.Text, "\n ")
				if text == "" {
					text = a.tr("terminalBlank")
				}
				ui.Text(c, text).Font("monospace").FontSize(small).Selectable()
			case st.err != "":
				a.empty(c, p, "warning", st.err, "")
			default:
				ui.Spinner(c).Margin(24, ui.Auto)
			}
		})
	})
	if st.loaded && st.err != "" {
		ui.Text(c, st.err).FontSize(small).TextColor(p.red).Padding(4, pagePad)
	}
	can := x.Can("terminal")
	ui.Divider(c)
	ui.Column(c).Padding(10, pagePad, 14, pagePad).Gap(8).Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			in := ui.TextInput(c.Key("term-input-"+key), &st.input).Placeholder(a.tr("terminalType")).Label(a.tr("terminalType")).
				Font("monospace").Grow(1).Disabled(!can)
			if in.Submitted() && can {
				a.sendKeys(key, st.input, "enter")
				st.input = ""
			}
			if st.sending {
				ui.Spinner(c)
			}
		})
		ui.Row(c).Gap(6).Wrap().Children(func() {
			for _, k := range []struct{ label, name string }{
				{"Esc", "esc"}, {"Tab", "tab"}, {"⌃C", "ctrl+c"}, {"←", "left"}, {"↑", "up"}, {"↓", "down"}, {"→", "right"}, {"⏎", "enter"},
			} {
				if ui.Button(c.Key("key-"+k.name), k.label).Font("monospace").FontSize(small).Padding(3, 10).
					Label(k.name).Tooltip(k.name).Disabled(!can).Clicked() {
					a.sendKeys(key, "", k.name)
				}
			}
			ui.Spacer(c)
			if !can {
				ui.Text(c, a.tr("terminalReadOnly")).FontSize(small).TextColor(p.ink3)
			}
		})
	})
}

func (a *App) changes(key string) *changesState {
	if a.gits == nil {
		a.gits = map[string]*changesState{}
	}
	s := a.gits[key]
	if s == nil {
		s = &changesState{diffs: map[string]*diffState{}}
		a.gits[key] = s
	}
	return s
}

func (a *App) readChanges(key string, x hubapi.Session) {
	st := a.changes(key)
	if st.loading {
		return
	}
	st.loading, st.err = true, ""
	c := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		gs, err := c.client.GitStatus(ctx, x.MachineID, x.Cwd)
		a.Host.Post(func() {
			st.loading = false
			if !a.live(c) {
				return
			}
			if err != nil {
				st.err = a.errorText(err)
				return
			}
			st.status, st.loaded, st.diffs = gs, true, map[string]*diffState{}
			if st.chosen == "" && len(gs.Files) > 0 {
				st.chosen = gs.Files[0].Path
			}
		})
	})
}

// staged tells a change only in the index, whose diff is the staged one.
func staged(f hubapi.GitFile) bool {
	return f.Index != "." && f.Index != "?" && f.Index != "" && (f.Worktree == "." || f.Worktree == "")
}

func (a *App) readDiff(key string, x hubapi.Session, f hubapi.GitFile) *diffState {
	st := a.changes(key)
	ds := st.diffs[f.Path]
	if ds == nil {
		ds = &diffState{}
		st.diffs[f.Path] = ds
	}
	if ds.loaded || ds.loading || ds.err != "" {
		return ds
	}
	ds.loading = true
	c := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		d, err := c.client.GitDiff(ctx, x.MachineID, x.Cwd, f.Path, staged(f))
		a.Host.Post(func() {
			ds.loading = false
			if err != nil {
				ds.err = a.errorText(err)
				return
			}
			ds.diff, ds.loaded = d, true
		})
	})
	return ds
}

// fileMark is a changed file's one letter: A added, D deleted, R renamed, ? new and not tracked, M changed.
func fileMark(f hubapi.GitFile) string {
	for _, s := range []string{f.Worktree, f.Index} {
		if s == "?" {
			return "?"
		}
	}
	for _, s := range []string{f.Index, f.Worktree} {
		if s != "" && s != "." {
			return s
		}
	}
	return "M"
}

func (p palette) markColor(mark string) ui.Color {
	switch mark {
	case "A", "?":
		return p.green
	case "D":
		return p.red
	case "R":
		return p.accent
	}
	return p.orange
}

// changesPane is what the agent changed in its folder: the files beside the chosen file's diff.
func (a *App) changesPane(c *ui.Context, p palette, key string, x hubapi.Session) {
	if x.Cwd == "" {
		a.empty(c, p, "git", a.tr("changesNoFolder"), "")
		return
	}
	st := a.changes(key)
	if !st.loaded && !st.loading && st.err == "" {
		a.readChanges(key, x)
	}
	gs := st.status
	ui.Row(c).Padding(10, pagePad).Gap(10).BorderWidth(0, 0, 1, 0).BorderColor(p.line).Children(func() {
		ui.Icon(c, icon("git")).TextColor(p.ink2)
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			line := gs.Branch
			if gs.Upstream != "" {
				line += " → " + gs.Upstream
			}
			if gs.Ahead > 0 {
				line += "  ↑" + itoa(gs.Ahead)
			}
			if gs.Behind > 0 {
				line += "  ↓" + itoa(gs.Behind)
			}
			ui.Text(c, line).Bold().SingleLine()
			if gs.Head != nil {
				ui.Text(c, gs.Head.Short+" · "+gs.Head.Subject).FontSize(small).TextColor(p.ink2).SingleLine()
			}
		})
		if st.loading {
			ui.Spinner(c)
		}
		if ui.Button(c, a.tr("changesRefresh")).Disabled(st.loading).Clicked() {
			a.readChanges(key, x)
		}
	})
	switch {
	case st.err != "":
		a.empty(c, p, "warning", st.err, "")
		return
	case !st.loaded:
		ui.Spinner(c).Margin(ui.Auto)
		return
	case !gs.IsRepo:
		a.empty(c, p, "git", a.tr("changesNotRepo"), x.Cwd)
		return
	case len(gs.Files) == 0:
		a.empty(c, p, "check", a.tr("changesNone"), a.tr("changesNoneHint"))
		return
	}
	var chosen hubapi.GitFile
	for _, f := range gs.Files {
		if f.Path == st.chosen {
			chosen = f
		}
	}
	ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
		ui.Scroll(c.Key("files-"+key)).Width(280).BorderWidth(0, 1, 0, 0).BorderColor(p.line).Children(func() {
			ui.Column(c).Padding(6).Children(func() {
				ui.Text(c, a.tr("changesFiles", "count", itoa(len(gs.Files)))).FontSize(11).Bold().TextColor(p.ink3).Padding(6, 8)
				for _, f := range gs.Files {
					a.fileRow(c, p, st, f)
				}
			})
		})
		ui.Column(c).Grow(1).Shrink(1).Children(func() {
			if chosen.Path == "" {
				a.empty(c, p, "read", a.tr("changesPick"), "")
				return
			}
			a.diffView(c, p, key, x, chosen)
		})
	})
}

func (a *App) fileRow(c *ui.Context, p palette, st *changesState, f hubapi.GitFile) {
	on := f.Path == st.chosen
	bg := ui.Transparent
	if on {
		bg = p.hover
	}
	name, dir := f.Path, ""
	if i := strings.LastIndex(f.Path, "/"); i >= 0 {
		name, dir = f.Path[i+1:], f.Path[:i]
	}
	row := ui.ButtonBase(c.Key("f-"+f.Path)).Padding(6, 8).Radius(rowRadius).Gap(8).Background(bg).Label(f.Path)
	row.Children(func() {
		mark := fileMark(f)
		ui.Text(c, mark).Font("monospace").FontSize(small).Bold().TextColor(p.markColor(mark)).Width(12)
		ui.Column(c).Grow(1).Shrink(1).Children(func() {
			ui.Text(c, name).FontSize(small).SingleLine()
			if dir != "" {
				ui.Text(c, dir).FontSize(11).TextColor(p.ink3).SingleLine()
			}
		})
		if !f.Binary {
			if f.Added > 0 {
				ui.Text(c, "+"+itoa(f.Added)).FontSize(11).TextColor(p.green)
			}
			if f.Deleted > 0 {
				ui.Text(c, "−"+itoa(f.Deleted)).FontSize(11).TextColor(p.red)
			}
		}
	})
	if row.Hovered() && !on {
		row.Background(p.hover)
	}
	if row.Clicked() {
		st.chosen = f.Path
	}
}

// diffLines are a diff's lines from its first hunk on, without git's header.
func diffLines(diff string) []string {
	lines := strings.Split(strings.TrimRight(diff, "\n"), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "@@") {
			return lines[i:]
		}
	}
	return nil
}

func (a *App) diffView(c *ui.Context, p palette, key string, x hubapi.Session, f hubapi.GitFile) {
	ds := a.readDiff(key, x, f)
	ui.Row(c).Padding(10, 16).Gap(8).BorderWidth(0, 0, 1, 0).BorderColor(p.line).Children(func() {
		ui.Text(c, f.Path).Font("monospace").FontSize(small).Bold().SingleLine().Grow(1).Shrink(1).Selectable()
		if f.OrigPath != "" {
			ui.Text(c, a.tr("changesRenamed", "path", f.OrigPath)).FontSize(11).TextColor(p.ink3).SingleLine()
		}
	})
	switch {
	case f.Binary:
		a.empty(c, p, "read", a.tr("changesBinary"), "")
		return
	case ds.err != "":
		a.empty(c, p, "warning", ds.err, "")
		return
	case !ds.loaded:
		ui.Spinner(c).Margin(ui.Auto)
		return
	}
	lines := diffLines(ds.diff.Diff)
	if len(lines) == 0 {
		a.empty(c, p, "read", a.tr("changesNoDiff"), "")
		return
	}
	addBg, delBg := p.green.Alpha(0.12), p.red.Alpha(0.12)
	ui.Scroll(c.Key("diff-" + key + "/" + f.Path)).Grow(1).Children(func() {
		ui.Column(c).PaddingY(6).Children(func() {
			for i, l := range lines {
				row := ui.Row(c.Key("l"+itoa(i))).Padding(0, 16)
				fg := p.ink
				switch {
				case strings.HasPrefix(l, "@@"):
					row.Background(p.inset).Margin(4, 0)
					fg = p.ink3
				case strings.HasPrefix(l, "+"):
					row.Background(addBg)
				case strings.HasPrefix(l, "-"):
					row.Background(delBg)
				case strings.HasPrefix(l, `\`):
					fg = p.ink3
				}
				row.Children(func() {
					ui.Text(c, l).Font("monospace").FontSize(small).TextColor(fg).Selectable()
				})
			}
			if ds.diff.Truncated {
				ui.Text(c, a.tr("changesTruncated")).FontSize(small).TextColor(p.ink3).Padding(8, 16)
			}
		})
	})
}
