package app

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// archivedPage is the Archived agents page: what the hub keeps archived, searched, each with Restore.
type archivedPage struct {
	loaded, busy bool
	err          string
	rows         []hubapi.Session
	query        string
	restoring    string
}

func (a *App) archivesOn() bool {
	return slices.Contains(a.conn.data.Hello.Features, hubapi.FeatureArchives)
}

// archiveText is why the hub refused an archive, in the page's words.
func (a *App) archiveText(err error, close bool) string {
	var he *hubapi.Error
	if errors.As(err, &he) {
		switch he.Status {
		case http.StatusConflict:
			if close {
				return a.tr("archiveErrCloseBusy")
			}
			return a.tr("archiveErrBusy")
		case http.StatusNotImplemented:
			if close {
				return a.tr("archiveErrCloseUpgrade")
			}
			return a.tr("archiveErrUpgrade")
		}
	}
	return a.tr("archiveErrFailed", "error", a.errorText(err))
}

// archiveDone is the toast after an archive: with close, what closing did.
func (a *App) archiveDone(title string, close bool, r *hubapi.CloseResult) string {
	switch {
	case !close:
		return a.tr("archiveDone", "title", title)
	case r == nil:
		return a.tr("archiveDoneUnknown", "title", title)
	case r.Closed:
		return a.tr("archiveDoneClosed", "title", title)
	case r.Outcome == "not_running":
		return a.tr("archiveDoneNotRunning", "title", title)
	}
	reason := r.Reason
	if reason == "" {
		reason = r.Outcome
	}
	return a.tr("archiveDoneStillOpen", "title", title, "reason", reason)
}

// archiveAgent archives x (and with close, closes it on its computer). A plain archive runs straight from the menu, so
// its result is a toast; "Archive and close" runs from its dialog, where a refusal stays.
func (a *App) archiveAgent(x hubapi.Session, close bool) {
	cn, key, title := a.conn, x.Key(), titleOf(x)
	if close {
		a.act.busy, a.act.err = true, ""
	}
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		r, err := cn.client.ArchiveSession(ctx, x.MachineID, x.SessionID, close)
		a.Host.Post(func() {
			if !a.live(cn) {
				return
			}
			if close && a.act.key == key {
				a.act.busy = false
			}
			if err != nil {
				if close && a.act.key == key && a.act.open {
					a.act.err = a.archiveText(err, true)
				} else {
					a.toast(a.archiveText(err, close))
				}
				return
			}
			if close && a.act.key == key {
				a.act.open = false
			}
			cn.data.DropSession(key)
			if cn.data.Pinned(key) {
				a.togglePin(key)
			}
			a.archived.loaded = false
			if strings.HasSuffix(a.Path(), "/"+key) {
				a.Navigate("/agents")
			}
			a.toast(a.archiveDone(title, close, r))
		})
	})
}

func (a *App) loadArchived() {
	g := &a.archived
	if g.busy {
		return
	}
	cn := a.conn
	g.busy, g.err = true, ""
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rows, err := cn.client.ArchivedSessions(ctx)
		a.Host.Post(func() {
			if !a.live(cn) {
				return
			}
			g.busy, g.loaded = false, true
			if err != nil {
				g.err = a.errorText(err)
				return
			}
			g.rows = rows
		})
	})
}

func (a *App) restoreAgent(x hubapi.Session) {
	g := &a.archived
	cn, key, title := a.conn, x.Key(), titleOf(x)
	g.restoring = key
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := cn.client.UnarchiveSession(ctx, x.MachineID, x.SessionID)
		a.Host.Post(func() {
			if !a.live(cn) {
				return
			}
			g.restoring = ""
			if err != nil {
				a.toast(a.tr("archiveRestoreFailed", "error", a.errorText(err)))
				return
			}
			g.rows = slices.DeleteFunc(g.rows, func(s hubapi.Session) bool { return s.Key() == key })
			x.Archived = false
			cn.data.PutSession(x)
			a.toast(a.tr("archiveRestored", "title", title))
		})
	})
}

// archivedView is the Archived agents page.
func (a *App) archivedView(c *ui.Context, p palette) {
	g := &a.archived
	if !a.archivesOn() {
		if a.conn.loaded {
			a.empty(c, p, "archive", a.tr("archivedUnsupportedTitle"), a.tr("archivedUnsupportedBody"))
		} else {
			ui.Spinner(c).Margin(32, ui.Auto)
		}
		return
	}
	if !g.loaded && !g.busy {
		a.loadArchived()
	}
	a.pageHeader(c, p, a.tr("archivedTitle"), a.tr("archivedDescription"), func() {
		if ui.Button(c, a.tr("archivedRefresh")).Disabled(g.busy).Clicked() {
			a.loadArchived()
		}
	})
	ui.SearchField(c.Key("archived-search"), &g.query).Placeholder(a.tr("archivedSearch")).Margin(0, pagePad, 8, pagePad)
	ui.Divider(c)
	q := strings.ToLower(strings.TrimSpace(g.query))
	var rows []hubapi.Session
	d := a.conn.data
	for _, x := range g.rows {
		if q == "" || strings.Contains(strings.ToLower(titleOf(x)+" "+x.Cwd+" "+d.MachineName(x.MachineID)), q) {
			rows = append(rows, x)
		}
	}
	switch {
	case g.err != "" && len(g.rows) == 0:
		ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
			a.empty(c, p, "warning", a.tr("archivedLoadFailed"), g.err)
			if ui.Button(c, a.tr("archivedRetry")).Clicked() {
				a.loadArchived()
			}
		})
		return
	case !g.loaded:
		ui.Spinner(c).Margin(32, ui.Auto)
		return
	case len(g.rows) == 0:
		a.empty(c, p, "archive", a.tr("archivedEmptyTitle"), a.tr("archivedEmptyBody"))
		return
	case len(rows) == 0:
		a.empty(c, p, "search", a.tr("archivedNoResults", "query", g.query), "")
		return
	}
	now := time.Now()
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Padding(8, pagePad).Gap(2).Children(func() {
			for _, x := range rows {
				key := x.Key()
				ui.Row(c.Key("arch-"+key)).Padding(10, 12).Gap(12).Radius(rowRadius).Children(func() {
					ui.Icon(c, icon("agent")).TextColor(p.ink3)
					ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
						ui.Text(c, titleOf(x)).Bold().SingleLine()
						sub := d.MachineName(x.MachineID)
						if x.Cwd != "" {
							sub += " · " + x.Cwd
						}
						if ago := a.ago(x.UpdatedAt, now); ago != "" {
							sub += " · " + ago
						}
						ui.Text(c, sub).FontSize(small).TextColor(p.ink2).SingleLine()
					})
					if ui.Button(c, a.tr("archivedRestore")).Disabled(g.restoring != "").Clicked() {
						a.restoreAgent(x)
					}
				})
			}
		})
	})
}
