package app

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// healthState is the health space (docs/assistant/health.md): its conversations, apart from Persona's others, its
// checkup reports and its own memory.
type healthState struct {
	list      loadable
	machine   string
	convs     []hubapi.Session
	draft     string
	starting  bool
	uploading bool

	reports    loadable
	reportList []hubapi.HealthReport
	report     *hubapi.HealthReport
	record     string // the record's text; "" not read (yet)
	recordErr  string
	recordBusy bool
	askDelete  bool

	memory  loadable
	memList []hubapi.MemoryEntry
	memOpen *hubapi.MemoryEntry
	memBusy bool
}

// healthView is the health space's home, or one of its conversations.
func (a *App) healthView(c *ui.Context, p palette, open string) {
	g := &a.health
	a.load(&g.list, func(ctx context.Context, cl *hubapi.Client) (func(), error) {
		mid, list, err := cl.HealthConversations(ctx)
		return func() {
			g.machine = mid
			g.convs = nil
			for _, x := range list {
				if !x.Archived {
					g.convs = append(g.convs, x)
				}
			}
		}, err
	})
	if open != "" {
		switch {
		case g.machine != "":
			title := a.tr("healthTitle")
			for _, x := range g.convs {
				if x.SessionID == open {
					title = titleOf(x)
				}
			}
			a.pageHeader(c, p, title, a.tr("healthTitle"), func() {
				if ui.Button(c, "‹ "+a.tr("healthTitle")).Clicked() {
					a.Navigate("/health")
				}
			})
			ui.Divider(c)
			a.chatView(c, p, g.machine+"/"+open, true)
		case g.list.err != "":
			ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
				a.empty(c, p, "warning", a.tr("healthConversationLoadFailed", "error", g.list.err), "")
				if ui.Button(c, a.tr("healthRetry")).Clicked() {
					g.list.asked, g.list.err = false, ""
				}
			})
		default:
			ui.Spinner(c).Margin(ui.Auto)
		}
		return
	}
	a.pageHeader(c, p, a.tr("healthTitle"), "", func() {
		if ui.Button(c, a.tr("healthRefresh")).Clicked() {
			g.list.asked, g.reports.asked, g.memory.asked = false, false, false
		}
	})
	ui.Divider(c)
	ui.Scroll(c.Key("health")).Grow(1).Children(func() {
		ui.Column(c).MaxWidth(760).Padding(24, pagePad, 32, pagePad).Gap(14).Margin(0, ui.Auto).Children(func() {
			ui.Column(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icon("heart")).FontSize(28).TextColor(p.accent)
				ui.Text(c, a.tr("healthHeroTitle")).FontSize(22).Bold()
				ui.Text(c, a.tr("healthHeroBody")).TextColor(p.ink2).TextAlign(ui.Center).MaxWidth(520)
			})
			a.healthComposer(c, p)
			a.healthReports(c, p)
			a.group(c, p, a.tr("healthRecent"), "", func() {
				switch {
				case g.list.err != "" && len(g.convs) == 0:
					ui.Text(c, a.tr("healthLoadFailed")+": "+g.list.err).FontSize(small).TextColor(p.red)
				case !g.list.done:
					ui.Spinner(c).Margin(12, ui.Auto)
				case len(g.convs) == 0:
					ui.Column(c).Gap(2).Children(func() {
						ui.Text(c, a.tr("healthEmptyTitle")).Bold()
						ui.Text(c, a.tr("healthEmptyBody")).FontSize(small).TextColor(p.ink3)
					})
				default:
					now := time.Now()
					for i, x := range a.healthSorted() {
						a.healthRow(c.Key("hc-"+x.SessionID), p, x, i, now)
					}
				}
			})
			a.group(c, p, a.tr("healthSources"), "", func() {
				for _, s := range [][2]string{
					{a.tr("healthSourcePhone"), a.tr("healthSourcePhoneBody")},
					{a.tr("healthSourceWrite"), a.tr("healthSourceWriteBody")},
					{a.tr("healthSourceUpload"), a.tr("healthSourceUploadBody")},
				} {
					ui.Column(c.Key("src-" + s[0])).Gap(2).Children(func() {
						ui.Text(c, s[0]).Bold()
						ui.Text(c, s[1]).FontSize(small).TextColor(p.ink3)
					})
				}
			})
			a.healthMemory(c, p)
			a.group(c, p, a.tr("healthPrivacyTitle"), "", func() {
				for _, k := range []string{a.tr("healthPrivacySeparate"), a.tr("healthPrivacyMemory"), a.tr("healthPrivacyWhere"), a.tr("healthPrivacyDoctor")} {
					ui.Text(c.Key("pv-"+k), "· "+k).FontSize(small).TextColor(p.ink2)
				}
			})
		})
	})
	a.reportDialog(c, p)
	a.healthMemoryDialog(c, p)
}

// healthSorted are the space's conversations, the live sessions' titles and last lines over the list's, newest first.
func (a *App) healthSorted() []hubapi.Session {
	g := &a.health
	d := a.conn.data
	out := make([]hubapi.Session, 0, len(g.convs))
	for _, x := range g.convs {
		if live, ok := d.Session(g.machine + "/" + x.SessionID); ok && live.Space == hubapi.HealthSpace {
			x = live
		}
		out = append(out, x)
	}
	sortNewest(out)
	return out
}

func sortNewest(list []hubapi.Session) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].Updated().After(list[j-1].Updated()); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

func (a *App) healthRow(c *ui.Context, p palette, x hubapi.Session, i int, now time.Time) {
	btn := ui.ButtonBase(c).Label(titleOf(x)).Padding(8, 6).Radius(rowRadius).BorderWidth(btop(i), 0, 0, 0).BorderColor(p.line)
	if btn.Hovered() {
		btn.Background(p.hover)
	}
	btn.Children(func() {
		ui.Row(c).Gap(10).Grow(1).Children(func() {
			ui.Icon(c, icon("heart")).TextColor(p.accent)
			ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
				ui.Text(c, titleOf(x)).Bold().SingleLine()
				if last := strings.TrimSpace(x.LastText); last != "" {
					ui.Text(c, last).FontSize(small).TextColor(p.ink3).SingleLine()
				}
			})
			ui.Text(c, a.ago(x.UpdatedAt, now)).FontSize(small).TextColor(p.ink3).SingleLine()
		})
	})
	if btn.Clicked() {
		a.Navigate("/health/" + x.SessionID)
	}
}

func (a *App) healthComposer(c *ui.Context, p palette) {
	g := &a.health
	ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
		in := ui.TextArea(c.Key("health-new"), &g.draft).Lines(1, 8).Placeholder(a.tr("healthAskPlaceholder")).Label(a.tr("healthAskPlaceholder")).Grow(1)
		send := false
		in.OnTextKey(func(k ui.TextKey) bool {
			if k.Key == ui.KeyEnter && k.Mods == 0 {
				send = true
				return true
			}
			return false
		})
		if g.starting {
			ui.Spinner(c)
		}
		if ui.PrimaryButton(c, "").Label(a.tr("send")).Tooltip(a.tr("send")).Children(func() { ui.Icon(c, icon("send")) }).
			Disabled(g.starting || strings.TrimSpace(g.draft) == "").Clicked() {
			send = true
		}
		if send {
			a.startHealth(strings.TrimSpace(g.draft), true)
		}
	})
	label := a.tr("healthReportUpload")
	if g.uploading {
		label = a.tr("healthReportUploading")
	}
	if ui.Button(c, label).Disabled(g.uploading || g.machine == "").Clicked() {
		a.Host.PickFiles(a.tr("healthReportUpload"), a.uploadReports)
	}
	ui.Row(c).Gap(6).Wrap().Children(func() {
		for _, s := range []string{a.tr("healthSuggestionSleep"), a.tr("healthSuggestionSteps"), a.tr("healthSuggestionHeart"), a.tr("healthSuggestionLog")} {
			if ui.Button(c.Key("sug-"+s), s).FontSize(small).Clicked() {
				g.draft = s
			}
		}
	})
}

// startHealth starts a conversation in the space and opens it; fromDraft clears the box once it has started.
func (a *App) startHealth(text string, fromDraft bool) {
	g := &a.health
	if text == "" || g.starting {
		return
	}
	g.starting = true
	cn := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		s, err := cn.client.StartHealthConversation(ctx, text)
		a.Host.Post(func() {
			g.starting = false
			if !a.live(cn) {
				return
			}
			if err != nil {
				a.toast(a.tr("healthStartFailed", "error", a.errorText(err)))
				return
			}
			if fromDraft {
				g.draft = ""
			}
			if s.MachineID == "" {
				s.MachineID = g.machine
			}
			if g.machine == "" {
				g.machine = s.MachineID
			}
			s.Assistant = true
			g.convs = append(g.convs, s)
			cn.data.Transcript(s.MachineID+"/"+s.SessionID).Pending(text, hubapi.OpID())
			a.Navigate("/health/" + s.SessionID)
		})
	})
}

// uploadReports sends the picked files into the space's reports/ one after another, then starts their conversation.
// A file over the limit, or one that fails, starts nothing.
func (a *App) uploadReports(files []string) {
	g := &a.health
	if len(files) == 0 || g.machine == "" {
		return
	}
	for _, f := range files {
		if info, err := os.Stat(f); err != nil || info.IsDir() || info.Size() > maxFile {
			a.toast(a.tr("healthReportFailed", "error", a.tr("attachFileTooBig", "name", filepath.Base(f))))
			return
		}
	}
	cn, mid := a.conn, g.machine
	g.uploading = true
	a.Go(func() {
		var paths []string
		var err error
		for _, f := range files {
			var fh *os.File
			fh, err = os.Open(f)
			if err != nil {
				break
			}
			info, _ := fh.Stat()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			var where string
			where, err = cn.client.UploadTo(ctx, mid, filepath.Base(f), hubapi.HealthSpace, fh, info.Size())
			cancel()
			fh.Close()
			if err != nil {
				break
			}
			paths = append(paths, quotePath(where))
		}
		a.Host.Post(func() {
			g.uploading = false
			if !a.live(cn) {
				return
			}
			if err != nil {
				a.toast(a.tr("healthReportFailed", "error", a.errorText(err)))
				return
			}
			g.reports.asked = false
			a.startHealth(a.tr("healthReportPrompt", "paths", strings.Join(paths, " ")), false)
		})
	})
}

func (a *App) healthReports(c *ui.Context, p palette) {
	g := &a.health
	if g.machine != "" {
		mid := g.machine
		a.load(&g.reports, func(ctx context.Context, cl *hubapi.Client) (func(), error) {
			list, err := cl.HealthReports(ctx, mid)
			return func() { g.reportList = list }, err
		})
	}
	a.group(c, p, a.tr("healthReports"), "", func() {
		switch {
		case g.reports.err != "":
			ui.Text(c, a.tr("healthReportsLoadFailed", "error", g.reports.err)).FontSize(small).TextColor(p.ink3)
		case !g.reports.done:
			ui.Spinner(c).Margin(12, ui.Auto)
		case len(g.reportList) == 0:
			ui.Text(c, a.tr("healthReportsEmpty")).FontSize(small).TextColor(p.ink3)
		}
		for i, r := range g.reportList {
			btn := ui.ButtonBase(c.Key("rep-"+r.Name)).Label(r.Title).Padding(8, 6).Radius(rowRadius).BorderWidth(btop(i), 0, 0, 0).BorderColor(p.line)
			if btn.Hovered() {
				btn.Background(p.hover)
			}
			btn.Children(func() {
				ui.Row(c).Gap(10).Grow(1).Children(func() {
					ui.Icon(c, icon("read")).TextColor(p.ink2)
					ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
						ui.Text(c, r.Title).Bold().SingleLine()
						state := a.tr("healthReportUnrecorded")
						if r.RecordPath != "" {
							state = a.tr("healthReportRecorded")
						}
						ui.Text(c, state+" · "+sizeText(r.Size)).FontSize(small).TextColor(p.ink3).SingleLine()
					})
					if t := hubapi.ParseTime(r.ModifiedAt); !t.IsZero() {
						ui.Text(c, t.Local().Format("2006-01-02")).FontSize(small).TextColor(p.ink3)
					}
				})
			})
			if btn.Clicked() {
				a.openReport(r)
			}
		}
	})
}

func (a *App) openReport(r hubapi.HealthReport) {
	g := &a.health
	g.report, g.record, g.recordErr, g.askDelete = &r, "", "", false
	if r.RecordPath == "" {
		return
	}
	cn, mid := a.conn, g.machine
	g.recordBusy = true
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		f, err := cn.client.ReadFile(ctx, mid, r.RecordPath, "", 0)
		a.Host.Post(func() {
			if !a.live(cn) || g.report == nil || g.report.Name != r.Name {
				return
			}
			g.recordBusy = false
			switch {
			case err != nil:
				g.recordErr = a.errorText(err)
			case f.Text != nil:
				g.record = *f.Text
			}
		})
	})
}

func (a *App) reportDialog(c *ui.Context, p palette) {
	g := &a.health
	if g.report == nil {
		return
	}
	r := *g.report
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(640).Padding(20).Gap(12).Children(func() {
			ui.Text(c, r.Title).FontSize(heading).Bold()
			switch {
			case g.askDelete:
				ui.Text(c, a.tr("healthReportDeleteBody")).TextColor(p.ink2)
			case g.recordErr != "":
				ui.Text(c, a.tr("healthReportsLoadFailed", "error", g.recordErr)).TextColor(p.ink3)
			case g.recordBusy:
				ui.Spinner(c).Margin(12, ui.Auto)
			case g.record != "":
				ui.Scroll(c).MaxHeight(420).Children(func() { markdown.View(c, g.record, &a.md) })
			default:
				ui.Text(c, a.tr("healthReportNoRecord")).TextColor(p.ink3)
			}
			ui.Row(c).Gap(8).Children(func() {
				if g.askDelete {
					ui.Spacer(c)
					if ui.Button(c, a.tr("healthReportKeep")).Clicked() {
						g.askDelete = false
					}
					if ui.PrimaryButton(c, a.tr("healthReportDeleteConfirm")).Background(p.red).Clicked() {
						a.deleteReport(r)
					}
					return
				}
				if ui.Button(c, a.tr("healthReportDelete")).TextColor(p.red).Clicked() {
					g.askDelete = true
				}
				ui.Spacer(c)
				if ui.Button(c, a.tr("healthClose")).Clicked() {
					g.report = nil
				}
				if r.Path != "" && ui.PrimaryButton(c, a.tr("filesSave")).Clicked() {
					a.saveRemote(g.machine, r.Path)
				}
			})
		})
	})
	if !open {
		g.report = nil
	}
}

func (a *App) deleteReport(r hubapi.HealthReport) {
	g := &a.health
	cn, mid := a.conn, g.machine
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := cn.client.DeleteHealthReport(ctx, mid, r.Name)
		a.Host.Post(func() {
			if !a.live(cn) {
				return
			}
			if err != nil {
				a.toast(a.errorText(err))
				return
			}
			g.report, g.reports.asked = nil, false
			a.toast(a.tr("healthReportDeleted", "title", r.Title))
		})
	})
}

// saveRemote asks where to save a file of a machine and downloads it there.
func (a *App) saveRemote(machine, abs string) {
	a.Host.SaveFile(a.tr("filesSave"), path.Base(abs), func(dest string) {
		cn := a.conn
		a.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			err := func() error {
				out, err := os.Create(dest)
				if err != nil {
					return err
				}
				if err := cn.client.Download(ctx, machine, abs, "", out); err != nil {
					out.Close()
					os.Remove(dest)
					return err
				}
				return out.Close()
			}()
			a.Host.Post(func() {
				if err != nil {
					a.toast(a.errorText(err))
					return
				}
				a.toast(a.tr("filesSaved", "name", filepath.Base(dest)))
			})
		})
	})
}

func (a *App) healthMemory(c *ui.Context, p palette) {
	g := &a.health
	a.load(&g.memory, func(ctx context.Context, cl *hubapi.Client) (func(), error) {
		files, err := cl.SpaceMemory(ctx, hubapi.HealthSpace)
		return func() { g.memList = hubapi.MemoryEntries(files) }, err
	})
	a.group(c, p, a.tr("healthMemory"), "", func() {
		switch {
		case g.memory.err != "":
			ui.Text(c, a.tr("healthMemoryLoadFailed", "error", g.memory.err)).FontSize(small).TextColor(p.ink3)
		case !g.memory.done:
			ui.Spinner(c).Margin(12, ui.Auto)
		case len(g.memList) == 0:
			ui.Text(c, a.tr("healthMemoryEmpty")).FontSize(small).TextColor(p.ink3)
		}
		for i, e := range g.memList {
			btn := ui.ButtonBase(c.Key("mem-"+e.File.Name)).Label(e.Title).Padding(8, 6).Radius(rowRadius).BorderWidth(btop(i), 0, 0, 0).BorderColor(p.line)
			if btn.Hovered() {
				btn.Background(p.hover)
			}
			btn.Children(func() {
				ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
					ui.Text(c, e.Title).Bold().SingleLine()
					if e.Detail != "" {
						ui.Text(c, e.Detail).FontSize(small).TextColor(p.ink3).SingleLine()
					}
				})
			})
			if btn.Clicked() {
				g.memOpen = &e
			}
		}
	})
}

func (a *App) healthMemoryDialog(c *ui.Context, p palette) {
	g := &a.health
	if g.memOpen == nil {
		return
	}
	e := *g.memOpen
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(520).Padding(20).Gap(12).Children(func() {
			ui.Text(c, e.Title).FontSize(heading).Bold()
			ui.Scroll(c).MaxHeight(360).Children(func() { ui.Text(c, e.Body).TextColor(p.ink2).Selectable() })
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c, a.tr("healthClose")).Clicked() {
					g.memOpen = nil
				}
				if ui.PrimaryButton(c, a.tr("healthMemoryDelete")).Background(p.red).Disabled(g.memBusy).Clicked() {
					a.deleteHealthMemory(e)
				}
			})
		})
	})
	if !open {
		g.memOpen = nil
	}
}

func (a *App) deleteHealthMemory(e hubapi.MemoryEntry) {
	g := &a.health
	cn := a.conn
	g.memBusy = true
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := cn.client.DeleteSpaceMemory(ctx, hubapi.HealthSpace, e.File.Name)
		a.Host.Post(func() {
			g.memBusy = false
			if !a.live(cn) {
				return
			}
			if err != nil {
				a.toast(a.errorText(err))
				return
			}
			g.memOpen, g.memory.asked = nil, false
			a.toast(a.tr("healthMemoryDeleted", "title", e.Title))
		})
	})
}
