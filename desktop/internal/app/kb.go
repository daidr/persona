package app

import (
	"context"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// kbPage is the knowledge base (docs/assistant/knowledge.md): what Persona knows, by kind, searchable, one entry beside the list.
type kbPage struct {
	kind  string
	tags  map[string]string // the tag chosen per kind
	lists map[string]*kbListState
	list  ui.ListState

	query     string
	searchSeq atomic.Int64
	found     []hubapi.KBResult
	foundQ    string
	searching bool
	searchErr string

	entries map[string]*kbEntryState
	// from is the search an entry was opened from, by its key.
	from map[string]string

	editing bool
	form    tagForm
	saving  bool
	asking  bool

	facts factsEdit
	rel   relDialog
}

type kbListState struct {
	list    hubapi.KBList
	loaded  bool
	loading bool
	err     string
}

type kbEntryState struct {
	page    hubapi.KBPage
	loaded  bool
	loading bool
	err     string
	allLog  bool
}

// tagForm is the Tagging dialog.
type tagForm struct {
	key         string
	importance  int // 0–3 for P0–P3
	relevance   string
	tags        string
	status      string // its label
	projectLong int
}

var kbStatuses = []string{"active", "paused", "done", "ignored", "merged"}

func (a *App) kindTitle(kind string) string {
	switch kind {
	case "projects":
		return a.tr("kbKindProjects")
	case "chats":
		return a.tr("kbKindChats")
	case "people":
		return a.tr("kbKindPeople")
	case "docs":
		return a.tr("kbKindDocs")
	case "meetings":
		return a.tr("kbKindMeetings")
	case "events":
		return a.tr("kbKindEvents")
	case "recordings":
		return a.tr("kbKindRecordings")
	}
	return kind
}

func (a *App) statusTitle(status string) string {
	switch status {
	case "paused":
		return a.tr("kbStatusPaused")
	case "done":
		return a.tr("kbStatusDone")
	case "ignored":
		return a.tr("kbStatusIgnored")
	case "merged":
		return a.tr("kbStatusMerged")
	}
	return a.tr("kbStatusActive")
}

func (a *App) projectKindTitle(kind string) string {
	if kind == "long" {
		return a.tr("kbProjectLong")
	}
	return a.tr("kbProjectShort")
}

// projectInfo reads a project's kind (short / long) and status from its list's extra ("long · done").
func projectInfo(e hubapi.KBEntry) (kind, status string) {
	k, st, _ := strings.Cut(e.Extra, " · ")
	kind, status = "short", strings.TrimSpace(st)
	if strings.TrimSpace(k) == "long" {
		kind = "long"
	}
	if status == "" {
		status = "active"
	}
	return kind, status
}

func importanceRank(p string) int {
	if len(p) == 2 && p[0] == 'P' && p[1] >= '0' && p[1] <= '3' {
		return int(p[1] - '0')
	}
	return 4
}

// rankKB orders entries as the hub's kb.Rank: importance, then relevance, then the most recently updated.
func rankKB(list []hubapi.KBEntry) []hubapi.KBEntry {
	out := append([]hubapi.KBEntry(nil), list...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if d := importanceRank(a.Importance) - importanceRank(b.Importance); d != 0 {
			return d < 0
		}
		if a.Relevance != b.Relevance {
			return a.Relevance > b.Relevance
		}
		return hubapi.ParseTime(a.Updated).After(hubapi.ParseTime(b.Updated))
	})
	return out
}

func (p palette) importanceColor(imp string) ui.Color {
	switch imp {
	case "P0":
		return p.red
	case "P1":
		return p.orange
	case "P2":
		return p.accent
	}
	return p.ink3
}

func (a *App) kbState() *kbPage {
	g := &a.kb
	if g.lists == nil {
		g.lists, g.entries, g.tags, g.from = map[string]*kbListState{}, map[string]*kbEntryState{}, map[string]string{}, map[string]string{}
	}
	if g.kind == "" {
		g.kind = "projects"
	}
	return g
}

// kbList is the chosen kind's list, read once and again after a save.
func (a *App) kbList() *kbListState {
	g := a.kbState()
	key := g.kind + "|" + g.tags[g.kind]
	st := g.lists[key]
	if st == nil {
		st = &kbListState{}
		g.lists[key] = st
	}
	if !st.loaded && !st.loading && st.err == "" {
		st.loading = true
		c, kind, tag := a.conn, g.kind, g.tags[g.kind]
		a.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			l, err := c.client.KB(ctx, kind, tag)
			a.Host.Post(func() {
				st.loading = false
				if !a.live(c) {
					return
				}
				if err != nil {
					st.err = a.errorText(err)
					return
				}
				st.list, st.loaded = l, true
			})
		})
	}
	return st
}

// kbEntry is an entry's page, read when first shown.
func (a *App) kbEntry(kind, id string) *kbEntryState {
	g := a.kbState()
	key := kind + "/" + id
	st := g.entries[key]
	if st == nil {
		st = &kbEntryState{}
		g.entries[key] = st
	}
	if !st.loaded && !st.loading && st.err == "" {
		st.loading = true
		c, q := a.conn, g.from[key]
		a.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			pg, err := c.client.KBEntry(ctx, kind, id, q)
			a.Host.Post(func() {
				st.loading = false
				if !a.live(c) {
					return
				}
				if err != nil {
					st.err = a.errorText(err)
					return
				}
				st.page, st.loaded = pg, true
			})
		})
	}
	return st
}

// search asks the hub once typing paused.
func (a *App) kbSearch() {
	g := a.kbState()
	q := strings.TrimSpace(g.query)
	seq := g.searchSeq.Add(1)
	g.searchErr = ""
	if q == "" {
		g.found, g.foundQ, g.searching = nil, "", false
		return
	}
	g.searching = true
	c := a.conn
	a.After(300*time.Millisecond, func() {
		if g.searchSeq.Load() != seq {
			return
		}
		a.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			res, err := c.client.KBSearch(ctx, q)
			a.Host.Post(func() {
				if g.searchSeq.Load() != seq || !a.live(c) {
					return
				}
				g.searching = false
				if err != nil {
					g.searchErr = a.errorText(err)
					return
				}
				g.found, g.foundQ = res, q
			})
		})
	})
}

// kbReload forgets what was read, after a save.
func (a *App) kbReload() {
	g := a.kbState()
	g.lists, g.entries = map[string]*kbListState{}, map[string]*kbEntryState{}
	if g.foundQ != "" {
		a.kbSearch()
	}
}

func (a *App) openKB(e hubapi.KBEntry, query string) {
	g := a.kbState()
	if query != "" {
		g.from[e.Key()] = query
	} else {
		delete(g.from, e.Key())
	}
	delete(g.entries, e.Key())
	a.Navigate("/knowledge/" + e.Key())
}

// knowledgeView is the knowledge list beside the entry chosen.
func (a *App) knowledgeView(c *ui.Context, p palette, kind, id string) {
	g := a.kbState()
	if kind != "" && id == "" {
		g.kind = kind
	}
	ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(listWidth+40).BorderWidth(0, 1, 0, 0).BorderColor(p.line).Children(func() {
			a.kbListPane(c, p, kind+"/"+id)
		})
		ui.Column(c).Grow(1).Children(func() {
			if id == "" {
				a.empty(c, p, "book", a.tr("kbPick"), a.tr("kbPickHint"))
				return
			}
			a.kbDetail(c, p, kind, id)
		})
	})
	a.tagDialog(c, p)
	a.relationshipDialog(c, p)
}

// kbRow is a row of the list: an entry, or what a search found in it.
type kbRow struct {
	entry hubapi.KBEntry
	hit   string
	count int
}

func (a *App) kbListPane(c *ui.Context, p palette, open string) {
	g := a.kbState()
	ui.Row(c).Padding(16, 16, 8, 16).Children(func() {
		ui.Text(c, a.tr("goKnowledge")).FontSize(title).Bold().Grow(1)
		if g.searching {
			ui.Spinner(c)
		}
	})
	if ui.SearchField(c.Key("kb-search"), &g.query).Placeholder(a.tr("kbSearch")).Margin(0, 16, 8, 16).Changed() {
		a.kbSearch()
	}
	searching := strings.TrimSpace(g.query) != ""
	var rows []kbRow
	var st *kbListState
	if searching {
		for _, r := range g.found {
			hit := ""
			if len(r.Hits) > 0 {
				hit = strings.TrimSpace(r.Hits[0].Line)
			}
			rows = append(rows, kbRow{entry: r.Entry, hit: hit, count: r.Count})
		}
	} else {
		ui.Row(c).Wrap().Gap(6).Padding(0, 16, 8, 16).Children(func() {
			for _, k := range hubapi.KBKinds {
				a.kindChip(c, p, k)
			}
		})
		st = a.kbList()
		if len(st.list.Tags) > 0 {
			opts := append([]string{a.tr("kbAllTags")}, st.list.Tags...)
			cur := g.tags[g.kind]
			if cur == "" {
				cur = opts[0]
			}
			if ui.Select(c.Key("kb-tag-"+g.kind), &cur, opts).Label(a.tr("kbTag")).Margin(0, 16, 8, 16).Changed() {
				if cur == opts[0] {
					cur = ""
				}
				g.tags[g.kind] = cur
			}
		}
		for _, e := range rankKB(st.list.Items) {
			rows = append(rows, kbRow{entry: e})
		}
	}
	now := time.Now()
	g.list.Key = func(i int) any { return rows[i].entry.Key() }
	ui.List(c, &g.list, len(rows), func(i int) {
		a.kbRowView(c, p, rows[i], rows[i].entry.Key() == open, searching, now)
	}).Grow(1).PaddingY(4).Children(func() {
		if len(rows) > 0 {
			return
		}
		switch {
		case searching && g.searchErr != "":
			a.empty(c, p, "warning", g.searchErr, "")
		case searching && (g.searching || g.foundQ != strings.TrimSpace(g.query)):
			ui.Spinner(c).Margin(24, ui.Auto)
		case searching:
			a.empty(c, p, "search", a.tr("kbNoMatch"), "")
		case st.err != "":
			a.empty(c, p, "warning", st.err, "")
			if ui.Button(c, a.tr("offlineRetry")).Margin(0, ui.Auto).Clicked() {
				st.err = ""
			}
		case !st.loaded:
			ui.Spinner(c).Margin(24, ui.Auto)
		default:
			a.empty(c, p, "book", a.tr("kbEmpty", "kind", a.kindTitle(g.kind)), "")
		}
	})
}

func (a *App) kindChip(c *ui.Context, p palette, kind string) {
	g := &a.kb
	on := g.kind == kind
	chip := ui.ButtonBase(c.Key("kind-"+kind)).Padding(4, 10).Radius(999).Label(a.kindTitle(kind))
	bg, fg := p.field, p.ink2
	if on {
		bg, fg = p.accent, ui.Hex("#ffffff")
	} else if chip.Hovered() {
		bg = p.hover
	}
	chip.Background(bg).Children(func() {
		ui.Text(c, a.kindTitle(kind)).FontSize(small).TextColor(fg).SingleLine()
	})
	if chip.Clicked() && !on {
		g.kind = kind
		a.Navigate("/knowledge/" + kind)
	}
}

func (a *App) kbRowView(c *ui.Context, p palette, r kbRow, chosen, searching bool, now time.Time) {
	e := r.entry
	bg := ui.Transparent
	if chosen {
		bg = p.hover
	}
	row := ui.ButtonBase(c.Key(e.Key())).Margin(0, 8).Padding(8, 10).Radius(rowRadius).Background(bg).Gap(3).
		Column().AlignItems(ui.Stretch).Label(e.Name)
	row.Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, e.Name).SingleLine().Grow(1).Shrink(1)
			if e.Importance != "" {
				ui.Text(c, e.Importance).FontSize(11).Bold().TextColor(p.importanceColor(e.Importance))
			}
			ui.Text(c, a.ago(e.Updated, now)).FontSize(small).TextColor(p.ink3).SingleLine()
		})
		var line []string
		if searching {
			line = append(line, a.kindTitle(e.Kind))
		}
		if e.Kind == "projects" {
			if _, st := projectInfo(e); st != "active" {
				line = append(line, a.statusTitle(st))
			}
		}
		text := e.OneLine
		if r.hit != "" {
			text = r.hit
		}
		if text != "" {
			line = append(line, oneLine(text, 200))
		}
		if len(line) > 0 {
			ui.Text(c, strings.Join(line, " · ")).FontSize(small).TextColor(p.ink2).MaxLines(2)
		}
	})
	if row.Hovered() && !chosen {
		row.Background(p.hover)
	}
	if row.Clicked() {
		q := ""
		if searching {
			q = strings.TrimSpace(a.kb.query)
		}
		a.openKB(e, q)
	}
}

// entryOf is the entry as lists show it, from what its page says.
func entryOf(kind, id string, d hubapi.KBDetail) hubapi.KBEntry {
	e := hubapi.KBEntry{Kind: kind, ID: id}
	m := d.Meta
	str := func(k string) string { s, _ := m[k].(string); return s }
	e.Name, e.OneLine, e.Importance = str("name"), str("one_line"), str("importance")
	if f, ok := m["relevance"].(float64); ok {
		e.Relevance = int(f)
	}
	if list, ok := m["tags"].([]any); ok {
		for _, t := range list {
			if s, ok := t.(string); ok {
				e.Tags = append(e.Tags, s)
			}
		}
	}
	if kind == "projects" {
		k, st := str("kind"), str("status")
		if k == "" {
			k = "short"
		}
		if st == "" {
			st = "active"
		}
		e.Extra = k + " · " + st
	}
	if e.Name == "" {
		e.Name = id
	}
	return e
}

// profile is an entry's summary without the title the page already shows and its one line.
func profile(md, name, one string) string {
	var out []string
	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "#") && strings.TrimSpace(strings.TrimLeft(t, "#")) == strings.TrimSpace(name) {
			continue
		}
		if one != "" && t == strings.TrimSpace(one) {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// logLine splits a timeline line ("- 2026-10-24 09:10 · source · what") into its time and the rest.
func logLine(s string) (when, what string) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "- "))
	if w, rest, ok := strings.Cut(s, " · "); ok && len(w) >= 10 && w[4] == '-' {
		return w, rest
	}
	return "", s
}

const logShown = 6

func (a *App) kbDetail(c *ui.Context, p palette, kind, id string) {
	g := a.kbState()
	st := a.kbEntry(kind, id)
	if !st.loaded {
		if st.err != "" {
			a.empty(c, p, "warning", st.err, "")
			ui.Row(c).Children(func() {
				ui.Spacer(c)
				if ui.Button(c, a.tr("offlineRetry")).Clicked() {
					st.err = ""
				}
				ui.Spacer(c)
			})
			return
		}
		ui.Spinner(c).Margin(ui.Auto)
		return
	}
	d := st.page.Detail
	e := entryOf(kind, id, d)
	sub := a.kindTitle(kind)
	if kind == "projects" {
		pk, status := projectInfo(e)
		sub += " · " + a.projectKindTitle(pk) + " · " + a.statusTitle(status)
	}
	a.pageHeader(c, p, e.Name, sub, func() {
		if ui.Button(c, a.tr("kbTagging")).Clicked() {
			a.openTagDialog(e)
		}
		if ui.PrimaryButton(c, a.tr("kbAsk")).Disabled(g.asking).Clicked() {
			a.askPersona(e, d)
		}
	})
	ui.Divider(c)
	ui.Scroll(c.Key("kb-" + kind + "/" + id)).Grow(1).Children(func() {
		ui.Column(c).MaxWidth(760).Padding(16, pagePad, 32, pagePad).Gap(16).Children(func() {
			ui.Row(c).Wrap().Gap(8).Children(func() {
				if e.Importance != "" {
					a.pill(c, e.Importance, p.importanceColor(e.Importance))
				}
				a.pill(c, a.tr("kbRelevance", "n", itoa(e.Relevance)), p.ink2)
				for _, t := range e.Tags {
					a.pill(c, "#"+t, p.ink2)
				}
			})
			if e.OneLine != "" {
				ui.Text(c, e.OneLine).TextColor(p.ink2).Selectable()
			}
			if len(st.page.Matches) > 0 {
				a.card(c, p, func() {
					ui.Text(c, a.tr("kbFound", "query", g.from[e.Key()])).Bold()
					for i, m := range st.page.Matches {
						ui.Column(c.Key("m" + itoa(i))).Gap(2).Children(func() {
							if m.Day != "" {
								ui.Text(c, m.Day).FontSize(11).TextColor(p.ink3)
							}
							for _, b := range m.Before {
								ui.Text(c, b).FontSize(small).TextColor(p.ink3).MaxLines(2)
							}
							ui.Text(c, plainLine(m.Line)).FontSize(small).Selectable()
							for _, b := range m.After {
								ui.Text(c, b).FontSize(small).TextColor(p.ink3).MaxLines(2)
							}
						})
					}
				})
			}
			if len(st.page.Agents) > 0 {
				a.card(c, p, func() {
					ui.Text(c, a.tr("goAgents")).Bold()
					for _, x := range st.page.Agents {
						a.kbAgentRow(c, p, x)
					}
				})
			}
			if md := profile(d.Summary, e.Name, e.OneLine); md != "" {
				a.card(c, p, func() {
					ui.Text(c, a.tr("kbProfile")).Bold()
					markdown.View(c.Key("kb-summary"), md, &a.md)
				})
			}
			if len(d.Log) > 0 {
				a.card(c, p, func() {
					ui.Text(c, a.tr("kbTimeline")).Bold()
					n := 0
					for i := len(d.Log) - 1; i >= 0; i-- {
						if strings.TrimSpace(d.Log[i]) == "" {
							continue
						}
						if n == logShown && !st.allLog {
							if ui.Button(c, a.tr("kbShowAll", "count", itoa(len(d.Log)))).Clicked() {
								st.allLog = true
							}
							break
						}
						n++
						when, what := logLine(d.Log[i])
						ui.Row(c.Key("log" + itoa(i))).Gap(12).AlignItems(ui.Start).Children(func() {
							ui.Text(c, when).FontSize(small).TextColor(p.ink3).Width(110).SingleLine()
							ui.Text(c, what).FontSize(small).Grow(1).Shrink(1).Selectable()
						})
					}
				})
			}
			if slices.Contains(hubapi.KBKinds, kind) {
				a.factsCard(c, p, kind, id, d)
			}
			if strings.TrimSpace(d.Recent) != "" {
				a.card(c, p, func() {
					ui.Text(c, a.tr("kbRecent")).Bold()
					markdown.View(c.Key("kb-recent"), d.Recent, &a.md)
				})
			}
			if len(d.Moments) > 0 {
				a.card(c, p, func() {
					ui.Text(c, a.tr("kbMoments")).Bold()
					for _, m := range d.Moments {
						ui.Column(c.Key("mo" + m.ID)).Gap(2).Children(func() {
							ui.Text(c, m.Author+" · "+a.ago(m.At, time.Now())+" · "+a.tr("kbComments", "count", itoa(m.Comments))).FontSize(11).TextColor(p.ink3)
							ui.Text(c, m.Text).FontSize(small).Selectable()
							if m.Note != "" {
								ui.Text(c, m.Note).FontSize(small).TextColor(p.ai)
							}
						})
					}
				})
			}
			if len(d.Links) > 0 {
				a.card(c, p, func() {
					ui.Text(c, a.tr("kbRelated")).Bold()
					for _, k := range hubapi.KBKinds {
						var of []hubapi.KBEntry
						for _, l := range d.Links {
							if l.Kind == k {
								of = append(of, l)
							}
						}
						if len(of) == 0 {
							continue
						}
						ui.Text(c, a.kindTitle(k)).FontSize(11).Bold().TextColor(p.ink3)
						for _, l := range rankKB(of) {
							a.kbLinkRow(c, p, l, relationshipRemoval(d, e.Name, l))
						}
					}
				})
			}
			if g.rel.done == kind+"/"+id {
				ui.Text(c, a.tr("kbRelSuccess")).FontSize(small).TextColor(p.green)
			}
			if d.Path != "" {
				ui.Text(c, d.Path).FontSize(11).Font("monospace").TextColor(p.ink3).Selectable()
			}
		})
	})
}

func (a *App) pill(c *ui.Context, text string, col ui.Color) {
	ui.Box(c).Padding(2, 8).Radius(999).Border(1, col).Children(func() {
		ui.Text(c, text).FontSize(11).TextColor(col).SingleLine()
	})
}

func (a *App) kbLinkRow(c *ui.Context, p palette, l hubapi.KBEntry, removal *relRemoval) {
	ui.Row(c.Key("linkrow-" + l.Key())).Gap(4).Children(func() {
		a.kbLinkButton(c, p, l)
		if removal != nil && ui.Button(c, "").Label(a.tr("kbRelAction")).Tooltip(a.tr("kbRelAction")).Disabled(a.kb.rel.open).Children(func() { ui.Icon(c, icon("x")) }).Clicked() {
			a.askRemoval(removal)
		}
	})
}

func (a *App) kbLinkButton(c *ui.Context, p palette, l hubapi.KBEntry) {
	row := ui.ButtonBase(c.Key("link-"+l.Key())).Padding(6, 8).Radius(rowRadius).Gap(10).Label(l.Name).Grow(1).Shrink(1)
	row.Children(func() {
		ui.Text(c, l.Name).SingleLine().Shrink(0)
		ui.Text(c, oneLine(l.OneLine, 120)).FontSize(small).TextColor(p.ink2).SingleLine().Grow(1).Shrink(1)
		if l.Importance != "" {
			ui.Text(c, l.Importance).FontSize(11).Bold().TextColor(p.importanceColor(l.Importance))
		}
	})
	if row.Hovered() {
		row.Background(p.hover)
	}
	if row.Clicked() {
		a.openKB(l, "")
	}
}

func (a *App) kbAgentRow(c *ui.Context, p palette, x hubapi.Session) {
	d := a.conn.data
	if live, ok := d.Session(x.Key()); ok {
		x = live
	}
	state := d.StateOf(x)
	row := ui.ButtonBase(c.Key("agent-"+x.Key())).Padding(6, 8).Radius(rowRadius).Gap(10).Label(titleOf(x))
	row.Children(func() {
		ui.Box(c).Size(8, 8).Radius(4).Background(p.stateColor(state))
		ui.Text(c, titleOf(x)).SingleLine().Grow(1).Shrink(1)
		ui.Text(c, d.MachineName(x.MachineID)).FontSize(small).TextColor(p.ink2).SingleLine()
		ui.Text(c, a.stateLabel(state)).FontSize(small).TextColor(p.stateColor(state))
	})
	if row.Hovered() {
		row.Background(p.hover)
	}
	if row.Clicked() {
		a.Navigate("/agents/" + x.Key())
	}
}

var contextEnd = regexp.MustCompile(`(?i)</\s*persona-context\s*>`)

func xmlAttr(s string) string {
	return strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// askText is what Ask Persona sends: the entry's profile, recent timeline and folder as context, then the ask
// (the web app's askPersonaText).
func (a *App) askText(e hubapi.KBEntry, d hubapi.KBDetail) string {
	text := d.Summary
	if text == "" {
		text = e.OneLine
	}
	if len(d.Log) > 0 {
		log := d.Log
		if len(log) > 10 {
			log = log[len(log)-10:]
		}
		text += "\n\n" + a.tr("kbContextTimeline") + "\n" + strings.Join(log, "\n")
	}
	if d.Path != "" {
		text += "\n\n" + a.tr("kbContextFolder", "path", d.Path)
	}
	text = contextEnd.ReplaceAllString(strings.TrimSpace(text), "")
	return `<persona-context kind="context" title="` + xmlAttr(a.tr("kbContextTitle", "name", e.Name)) + `" url="">` + "\n" +
		text + "\n</persona-context>\n\n" + a.tr("kbAskText", "name", e.Name)
}

// askPersona sends the entry to the assistant's main chat and opens it.
func (a *App) askPersona(e hubapi.KBEntry, d hubapi.KBDetail) {
	main, ok := a.conn.data.MainConversation()
	if !ok {
		a.toast(a.tr("personaNone"))
		return
	}
	a.send(main.Key(), a.askText(e, d))
	a.Navigate("/")
}

func (a *App) openTagDialog(e hubapi.KBEntry) {
	f := tagForm{key: e.Key(), importance: 3, relevance: itoa(e.Relevance), tags: strings.Join(e.Tags, ", ")}
	if r := importanceRank(e.Importance); r < 4 {
		f.importance = r
	}
	if e.Kind == "projects" {
		pk, st := projectInfo(e)
		f.status = a.statusTitle(st)
		if pk == "long" {
			f.projectLong = 1
		}
	}
	a.kb.form, a.kb.editing = f, true
}

// tagDialog is Tagging: importance, relevance, tags and, for a project, its kind and status.
func (a *App) tagDialog(c *ui.Context, p palette) {
	g := &a.kb
	if !g.editing {
		return
	}
	f := &g.form
	kind, id, _ := strings.Cut(f.key, "/")
	ui.Modal(c, &g.editing, func() {
		ui.Column(c).Width(460).Padding(20).Gap(14).Children(func() {
			ui.Text(c, a.tr("kbTagging")).FontSize(heading).Bold()
			ui.Text(c, a.tr("kbTaggingHint")).FontSize(small).TextColor(p.ink2)
			a.field(c, p, a.tr("kbImportance"), func() {
				ui.Segmented(c.Key("kb-importance"), &f.importance, "P0", "P1", "P2", "P3")
			})
			a.field(c, p, a.tr("kbRelevanceLabel"), func() {
				ui.TextInput(c.Key("kb-relevance"), &f.relevance).Label(a.tr("kbRelevanceLabel")).Width(100)
			})
			a.field(c, p, a.tr("kbTags"), func() {
				ui.TextInput(c.Key("kb-tags"), &f.tags).Placeholder(a.tr("kbTagsPlaceholder")).Label(a.tr("kbTags"))
			})
			if kind == "projects" {
				a.field(c, p, a.tr("kbProjectKind"), func() {
					ui.Segmented(c.Key("kb-pkind"), &f.projectLong, a.tr("kbProjectShort"), a.tr("kbProjectLong"))
				})
				opts := make([]string, len(kbStatuses))
				for i, s := range kbStatuses {
					opts[i] = a.statusTitle(s)
				}
				a.field(c, p, a.tr("kbStatus"), func() {
					ui.Select(c.Key("kb-status"), &f.status, opts).Label(a.tr("kbStatus"))
				})
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if g.saving {
					ui.Spinner(c)
				}
				if ui.Button(c, a.tr("cancel")).Clicked() {
					g.editing = false
				}
				if ui.PrimaryButton(c, a.tr("save")).Disabled(g.saving).Clicked() {
					a.saveTags(kind, id)
				}
			})
		})
	})
}

// tagUpdate is the form as the hub takes it.
func (a *App) tagUpdate(kind string) hubapi.KBUpdate {
	f := a.kb.form
	rel, _ := strconv.Atoi(strings.TrimSpace(f.relevance))
	u := hubapi.KBUpdate{Importance: "P" + itoa(f.importance), Relevance: min(100, max(0, rel)), Tags: []string{}}
	for _, t := range strings.FieldsFunc(f.tags, func(r rune) bool { return r == ',' || r == '，' || r == '、' }) {
		if t = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "#")); t != "" {
			u.Tags = append(u.Tags, t)
		}
	}
	if kind == "projects" {
		u.ProjectKind = "short"
		if f.projectLong == 1 {
			u.ProjectKind = "long"
		}
		u.Status = "active"
		for _, s := range kbStatuses {
			if a.statusTitle(s) == f.status {
				u.Status = s
			}
		}
	}
	return u
}

func (a *App) saveTags(kind, id string) {
	g := &a.kb
	c := a.conn
	u := a.tagUpdate(kind)
	g.saving = true
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := c.client.KBUpdate(ctx, kind, id, u)
		a.Host.Post(func() {
			g.saving = false
			if !a.live(c) {
				return
			}
			if err != nil {
				a.toast(a.errorText(err))
				return
			}
			g.editing = false
			a.kbReload()
		})
	})
}

// plainLine is a Markdown line as plain text: no list mark, emphasis or heading marks.
func plainLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "#")
	s = strings.TrimPrefix(strings.TrimSpace(s), "- ")
	return strings.TrimSpace(strings.NewReplacer("**", "", "__", "", "`", "").Replace(s))
}
