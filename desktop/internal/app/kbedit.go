package app

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// Corrections to a knowledge-base entry (docs/assistant/knowledge.md): its confirmed facts ("pinned", kept apart from
// the inferred profile and over Persona's updates) and removing a relationship to another entry. A removal locks the
// two lists against automatic changes; both ends are read back afterwards and only both confirming it counts as done.

type factsEdit struct {
	key     string
	open    bool
	draft   string
	initial string
	saving  bool
	err     string
}

type relRemoval struct {
	ends   [2]relEnd
	target [2]string // kind, id written to
	patch  map[string]any
}

type relEnd struct {
	kind, id, canonical, name, field, member string
}

type relDialog struct {
	open     bool
	intent   *relRemoval
	phase    string // confirm, writing, reading, retry-write, read-error, unconfirmed
	accepted bool
	writeErr string
	err      string
	// done is the last removal confirmed, by the entry it was made from
	done string
}

var relItems = []string{"docs", "meetings", "events", "recordings"}

var unsafeID = regexp.MustCompile(`[^\p{L}\p{N}._@+-]+`)

// canonicalKBID is the knowledge base's storage id for a source id (SafeID).
func canonicalKBID(kind, id string) string {
	if kind == "recordings" {
		id = strings.ToLower(id)
	}
	s := strings.Trim(unsafeID.ReplaceAllString(id, "_"), "._")
	out, n := "", 0
	for _, r := range s {
		n += len(string(r))
		if n > 120 {
			break
		}
		out += string(r)
	}
	return out
}

func metaStrings(meta map[string]any, field string) ([]string, bool) {
	v, ok := meta[field]
	if !ok || v == nil {
		return nil, false
	}
	switch x := v.(type) {
	case []string:
		return x, true
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

// relationshipRemoval is how linked can be taken off the entry d, or nil when that relationship isn't one a person
// keeps (links also carry where an entry came from).
func relationshipRemoval(d hubapi.KBDetail, name string, linked hubapi.KBEntry) *relRemoval {
	aid := d.ID
	if s, ok := d.Meta["id"].(string); ok && s != "" {
		aid = s
	}
	var af, bf string
	reverse := false
	switch {
	case d.Kind == "projects" && (linked.Kind == "people" || linked.Kind == "chats"):
		af, bf = linked.Kind, "projects"
	case d.Kind == "projects" && slices.Contains(relItems, linked.Kind):
		af, bf, reverse = "items", "projects", true
	case linked.Kind == "projects" && (d.Kind == "people" || d.Kind == "chats" || slices.Contains(relItems, d.Kind)):
		af, bf = "projects", "items"
		if d.Kind == "people" || d.Kind == "chats" {
			bf = d.Kind
		}
	case d.Kind == "people" && linked.Kind == "chats":
		af, bf = "chats", "people"
	case d.Kind == "chats" && linked.Kind == "people":
		af, bf = "people", "chats"
	default:
		return nil
	}
	bid := canonicalKBID(linked.Kind, linked.ID)
	am := linked.ID
	if af == "items" {
		am = linked.Kind + "/" + bid
	}
	bm := aid
	if bf == "items" {
		bm = d.Kind + "/" + d.ID
	}
	members, ok := metaStrings(d.Meta, af)
	if !ok || !slices.Contains(members, am) {
		return nil
	}
	r := &relRemoval{ends: [2]relEnd{
		{kind: d.Kind, id: aid, canonical: d.ID, name: name, field: af, member: am},
		{kind: linked.Kind, id: linked.ID, canonical: bid, name: linked.Name, field: bf, member: bm},
	}}
	if reverse {
		r.target = [2]string{linked.Kind, linked.ID}
		r.patch = map[string]any{"remove_projects": []string{aid}}
	} else {
		r.target = [2]string{d.Kind, aid}
		r.patch = map[string]any{"remove_" + af: []string{linked.ID}}
	}
	return r
}

// removalConfirmed tells both ends without the member, the list locked and held by the user.
func removalConfirmed(r *relRemoval, details [2]hubapi.KBDetail) bool {
	for i, end := range r.ends {
		d := details[i]
		if d.Kind != end.kind || d.ID != end.canonical {
			return false
		}
		if members, ok := metaStrings(d.Meta, end.field); ok && slices.Contains(members, end.member) {
			return false
		} else if !ok && d.Meta[end.field] != nil {
			return false
		}
		locks, ok := metaStrings(d.Meta, "locked_fields")
		if !ok || !slices.Contains(locks, end.field) {
			return false
		}
		switch pv := d.Meta["provenance"].(type) {
		case nil:
		case map[string]any:
			if _, held := pv[end.field]; held {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (a *App) relListName(field string) string {
	switch field {
	case "people":
		return a.tr("kbRelListPeople")
	case "chats":
		return a.tr("kbRelListChats")
	case "projects":
		return a.tr("kbRelListProjects")
	}
	return a.tr("kbRelListItems")
}

// rereadKB reads an entry again into its page.
func (a *App) rereadKB(kind, id string, done func(err error)) {
	st := a.kbEntry(kind, id)
	cn := a.conn
	q := a.kbState().from[kind+"/"+id]
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pg, err := cn.client.KBEntry(ctx, kind, id, q)
		a.Host.Post(func() {
			if !a.live(cn) {
				return
			}
			if err == nil {
				st.page, st.loaded, st.err = pg, true, ""
			}
			done(err)
		})
	})
}

// factsCard is the entry's confirmed facts, edited in place.
func (a *App) factsCard(c *ui.Context, p palette, kind, id string, d hubapi.KBDetail) {
	g := a.kbState()
	f := &g.facts
	key := kind + "/" + id
	value, _ := d.Meta["pinned"].(string)
	editing := f.open && f.key == key
	a.card(c, p, func() {
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, a.tr("kbFacts")).Bold().Grow(1)
			if !editing {
				label := a.tr("kbFactsAdd")
				if value != "" {
					label = a.tr("kbFactsEdit")
				}
				if ui.Button(c, label).Clicked() {
					*f = factsEdit{key: key, open: true, draft: value, initial: value}
				}
			}
		})
		if !editing {
			text := value
			if text == "" {
				text = a.tr("kbFactsEmpty")
			}
			ui.Text(c, text).Selectable()
			ui.Text(c, a.tr("kbFactsHint")).FontSize(small).TextColor(p.ink3)
			return
		}
		ui.TextArea(c.Key("kb-facts-"+key), &f.draft).Lines(4, 12).Label(a.tr("kbFacts"))
		ui.Text(c, a.tr("kbFactsEditHint")).FontSize(small).TextColor(p.ink3)
		if f.err != "" {
			ui.Text(c, f.err).FontSize(small).TextColor(p.red).Selectable()
		}
		ui.Row(c).Gap(8).Children(func() {
			ui.Spacer(c)
			if f.saving {
				ui.Spinner(c)
			}
			if ui.Button(c, a.tr("cancel")).Disabled(f.saving).Clicked() {
				f.open = false
			}
			if ui.PrimaryButton(c, a.tr("save")).Disabled(f.saving || f.draft == f.initial).Clicked() {
				a.saveFacts(kind, id)
			}
		})
	})
}

func (a *App) saveFacts(kind, id string) {
	f := &a.kbState().facts
	f.saving, f.err = true, ""
	cn := a.conn
	draft, key := f.draft, f.key
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := cn.client.KBPatch(ctx, kind, id, map[string]any{"pinned": draft})
		a.Host.Post(func() {
			if !a.live(cn) || f.key != key {
				return
			}
			if err != nil {
				f.saving = false
				f.err = a.tr("kbFactsSaveFailed", "error", a.errorText(err))
				return
			}
			a.rereadKB(kind, id, func(err error) {
				f.saving = false
				if err != nil {
					f.err = a.tr("kbFactsRefreshFailed", "error", a.errorText(err))
					return
				}
				f.open = false
			})
		})
	})
}

// askRemoval opens the dialog for removing a relationship.
func (a *App) askRemoval(r *relRemoval) {
	g := a.kbState()
	g.rel = relDialog{open: true, intent: r, phase: "confirm"}
}

func (a *App) removeRelationship() {
	g := a.kbState()
	rd := &g.rel
	r := rd.intent
	if r == nil || (rd.phase != "confirm" && rd.phase != "retry-write") {
		return
	}
	rd.phase, rd.err, rd.accepted, rd.writeErr = "writing", "", false, ""
	cn := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := cn.client.KBPatch(ctx, r.target[0], r.target[1], r.patch)
		a.Host.Post(func() {
			if !a.live(cn) || rd.intent != r {
				return
			}
			rd.accepted = err == nil
			if err != nil {
				rd.writeErr = a.errorText(err)
			}
			// a failed write may still have changed either end: read both before offering it again
			a.verifyRemoval()
		})
	})
}

func (a *App) verifyRemoval() {
	g := a.kbState()
	rd := &g.rel
	r := rd.intent
	rd.phase = "reading"
	cn := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var details [2]hubapi.KBDetail
		var pages [2]hubapi.KBPage
		var failure error
		for i, end := range r.ends {
			pg, err := cn.client.KBEntry(ctx, end.kind, end.id, "")
			if err != nil {
				if failure == nil {
					failure = err
				}
				continue
			}
			pages[i], details[i] = pg, pg.Detail
		}
		a.Host.Post(func() {
			if !a.live(cn) || rd.intent != r {
				return
			}
			// what was read is the entry as it is now, even if the other end failed
			for i, end := range r.ends {
				if details[i].Kind == end.kind {
					if st, ok := g.entries[end.kind+"/"+end.id]; ok && st.loaded {
						st.page = pages[i]
					}
				}
			}
			switch {
			case failure != nil:
				rd.err, rd.phase = a.errorText(failure), "read-error"
			case removalConfirmed(r, details):
				rd.done = r.ends[0].kind + "/" + r.ends[0].id
				rd.open, rd.intent = false, nil
				a.kbReload()
			case rd.accepted:
				rd.phase = "unconfirmed"
			default:
				rd.err, rd.phase = rd.writeErr, "retry-write"
			}
		})
	})
}

// relationshipDialog asks before removing, then says how it went.
func (a *App) relationshipDialog(c *ui.Context, p palette) {
	g := a.kbState()
	rd := &g.rel
	if !rd.open || rd.intent == nil {
		return
	}
	r := rd.intent
	working := rd.phase == "writing" || rd.phase == "reading"
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(480).Padding(20).Gap(12).Children(func() {
			ui.Text(c, a.tr("kbRelTitle")).FontSize(heading).Bold()
			ui.Text(c, a.tr("kbRelConfirm", "first", r.ends[0].name, "second", r.ends[1].name))
			ui.Text(c, a.tr("kbRelKeep")).FontSize(small).TextColor(p.ink2)
			ui.Column(c).Padding(12).Radius(rowRadius).Background(p.field).Gap(6).Children(func() {
				ui.Text(c, a.tr("kbRelLockTitle")).FontSize(small).Bold()
				for _, end := range r.ends {
					ui.Text(c.Key("lock-"+end.kind+end.id), a.tr("kbRelLock", "name", end.name, "list", a.relListName(end.field))).FontSize(small).TextColor(p.ink2)
				}
				ui.Text(c, a.tr("kbRelLockHint")).FontSize(small).TextColor(p.ink2)
			})
			switch rd.phase {
			case "writing":
				ui.Text(c, a.tr("kbRelRemoving")).FontSize(small).TextColor(p.ink2)
			case "reading":
				ui.Text(c, a.tr("kbRelReading")).FontSize(small).TextColor(p.ink2)
			case "read-error":
				msg := a.tr("kbRelRejectedReadError", "error", rd.err, "writeError", rd.writeErr)
				if rd.accepted {
					msg = a.tr("kbRelAcceptedReadError", "error", rd.err)
				}
				ui.Text(c, msg).FontSize(small).TextColor(p.red).Selectable()
			case "retry-write":
				ui.Text(c, a.tr("kbRelPartial", "error", rd.err)).FontSize(small).TextColor(p.red).Selectable()
			case "unconfirmed":
				ui.Text(c, a.tr("kbRelUnconfirmed")).FontSize(small).TextColor(p.red)
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if working {
					ui.Spinner(c)
				}
				label := a.tr("kbRelClose")
				if rd.phase == "confirm" {
					label = a.tr("cancel")
				}
				if ui.Button(c, label).Disabled(working).Clicked() {
					rd.open, rd.intent = false, nil
				}
				switch rd.phase {
				case "confirm", "retry-write":
					label := a.tr("kbRelRemove")
					if rd.phase == "retry-write" {
						label = a.tr("kbRelRetryRemove")
					}
					if ui.Button(c, label).TextColor(p.red).Clicked() {
						a.removeRelationship()
					}
				case "read-error", "unconfirmed":
					if ui.Button(c, a.tr("kbRelRetryRead")).Clicked() {
						a.verifyRemoval()
					}
				}
			})
		})
	})
	// a click outside closes it only while nothing is being written
	if !open && !working {
		rd.open, rd.intent = false, nil
	}
}
