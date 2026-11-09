package app

import (
	"context"
	"encoding/base64"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// An agent's Files pane: its folder, folder by folder with a path back to each level, folders first with their size,
// time and Git mark, hidden files when asked for; a file opens beside the list (text with line numbers and later pages
// on demand, Markdown rendered or as source, pictures), and Save a Copy downloads it (docs/protocol/protocol.md "File preview").

type filesState struct {
	dirs     []string // the folders opened from the agent's folder ("" is the folder itself)
	lists    map[string]*listState
	hidden   bool
	file     *fileState
	saving   bool
	mdSource bool
}

type listState struct {
	list    hubapi.FsListing
	loaded  bool
	loading bool
	err     string
}

type fileState struct {
	path    string
	content hubapi.FileContent
	text    string
	image   ui.ImageSource
	loaded  bool
	loading bool
	more    bool
	err     string
}

func (a *App) files(key string) *filesState {
	if a.fileViews == nil {
		a.fileViews = map[string]*filesState{}
	}
	s := a.fileViews[key]
	if s == nil {
		s = &filesState{dirs: []string{""}, lists: map[string]*listState{}}
		a.fileViews[key] = s
	}
	return s
}

func (a *App) readDir(key string, x hubapi.Session, dir string) *listState {
	fs := a.files(key)
	ls := fs.lists[dir]
	if ls == nil {
		ls = &listState{}
		fs.lists[dir] = ls
	}
	if ls.loaded || ls.loading || ls.err != "" {
		return ls
	}
	ls.loading = true
	c := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		l, err := c.client.ListDir(ctx, x.MachineID, dir, x.Cwd)
		a.Host.Post(func() {
			ls.loading = false
			if err != nil {
				ls.err = a.errorText(err)
				return
			}
			ls.list, ls.loaded = l, true
		})
	})
	return ls
}

// sortEntries puts folders first, then by name as people read it.
func sortEntries(list []hubapi.FsEntry, hidden bool) []hubapi.FsEntry {
	out := make([]hubapi.FsEntry, 0, len(list))
	for _, e := range list {
		if hidden || !strings.HasPrefix(e.Name, ".") {
			out = append(out, e)
		}
	}
	slices.SortStableFunc(out, func(x, y hubapi.FsEntry) int {
		if x.Dir != y.Dir {
			if x.Dir {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
	})
	return out
}

// sizeText is a size in bytes as people read it: 12 B, 3.4 KB, 1.2 MB.
func sizeText(n int64) string {
	switch {
	case n < 1024:
		return strconv.FormatInt(n, 10) + " B"
	case n < 1024*1024:
		return strconv.FormatFloat(float64(n)/1024, 'f', 1, 64) + " KB"
	case n < 1024*1024*1024:
		return strconv.FormatFloat(float64(n)/1024/1024, 'f', 1, 64) + " MB"
	}
	return strconv.FormatFloat(float64(n)/1024/1024/1024, 'f', 1, 64) + " GB"
}

// gitMarks are the Git marks of the files under the folder at abs, by name: a folder is marked when something under it changed.
func gitMarks(gs hubapi.GitStatus, abs string) map[string]string {
	out := map[string]string{}
	if !gs.IsRepo || gs.Root == "" {
		return out
	}
	rel, ok := strings.CutPrefix(abs, gs.Root)
	if !ok {
		return out
	}
	rel = strings.Trim(rel, "/")
	for _, f := range gs.Files {
		p := f.Path
		if rel != "" {
			var ok bool
			if p, ok = strings.CutPrefix(p, rel+"/"); !ok {
				continue
			}
		}
		name, rest, sub := strings.Cut(p, "/")
		_ = rest
		if sub {
			if _, ok := out[name]; !ok {
				out[name] = "•"
			}
			continue
		}
		out[name] = fileMark(f)
	}
	return out
}

// filesPane is the agent's folder beside the file open.
func (a *App) filesPane(c *ui.Context, p palette, key string, x hubapi.Session) {
	if x.Cwd == "" {
		a.empty(c, p, "folder", a.tr("changesNoFolder"), "")
		return
	}
	fs := a.files(key)
	dir := fs.dirs[len(fs.dirs)-1]
	ls := a.readDir(key, x, dir)
	ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(320).BorderWidth(0, 1, 0, 0).BorderColor(p.line).Children(func() {
			// the path back to each level
			ui.Row(c).Padding(8, 10).Gap(4).Wrap().BorderWidth(0, 0, 1, 0).BorderColor(p.line).Children(func() {
				for i, d := range fs.dirs {
					name := path.Base(x.Cwd)
					if d != "" {
						name = path.Base(d)
					}
					if i > 0 {
						ui.Text(c.Key("sep"+itoa(i)), "›").FontSize(small).TextColor(p.ink3)
					}
					last := i == len(fs.dirs)-1
					crumb := ui.ButtonBase(c.Key("crumb"+itoa(i))).Padding(2, 4).Radius(4).Label(name).Disabled(last)
					crumb.Children(func() {
						t := ui.Text(c, name).FontSize(small).SingleLine()
						if last {
							t.Bold()
						} else {
							t.TextColor(p.accent)
						}
					})
					if crumb.Clicked() {
						fs.dirs = fs.dirs[:i+1]
					}
				}
			})
			ui.Row(c).Padding(6, 10).Gap(8).Children(func() {
				ui.Checkbox(c.Key("hidden-"+key), &fs.hidden, a.tr("filesHidden")).FontSize(small)
				ui.Spacer(c)
				if ui.Button(c, a.tr("changesRefresh")).FontSize(small).Padding(2, 8).Disabled(ls.loading).Clicked() {
					fs.lists = map[string]*listState{}
					if gs := a.changes(key); !gs.loading {
						a.readChanges(key, x)
					}
				}
			})
			switch {
			case ls.err != "":
				ui.Text(c, ls.err).FontSize(small).TextColor(p.red).Padding(10)
				return
			case !ls.loaded:
				ui.Spinner(c).Margin(24, ui.Auto)
				return
			}
			gst := a.changes(key)
			if !gst.loaded && !gst.loading && gst.err == "" {
				a.readChanges(key, x)
			}
			marks := gitMarks(gst.status, ls.list.Path)
			entries := sortEntries(ls.list.Entries, fs.hidden)
			now := time.Now()
			ui.Scroll(c.Key("fs-" + key + dir)).Grow(1).Children(func() {
				ui.Column(c).Padding(4, 6).Children(func() {
					if len(entries) == 0 {
						ui.Text(c, a.tr("filesEmpty")).FontSize(small).TextColor(p.ink3).Padding(8)
					}
					for _, e := range entries {
						a.entryRow(c, p, key, x, fs, ls.list.Path, e, marks[e.Name], now)
					}
					if ls.list.Truncated {
						ui.Text(c, a.tr("filesTruncated")).FontSize(11).TextColor(p.ink3).Padding(8)
					}
				})
			})
		})
		ui.Column(c).Grow(1).Shrink(1).Children(func() {
			if fs.file == nil {
				a.empty(c, p, "read", a.tr("filesPick"), "")
				return
			}
			a.fileView(c, p, key, x, fs)
		})
	})
}

func (a *App) entryRow(c *ui.Context, p palette, key string, x hubapi.Session, fs *filesState, dir string, e hubapi.FsEntry, mark string, now time.Time) {
	full := path.Join(dir, e.Name)
	on := fs.file != nil && fs.file.path == full
	bg := ui.Transparent
	if on {
		bg = p.hover
	}
	row := ui.ButtonBase(c.Key("e-"+e.Name)).Padding(5, 8).Radius(rowRadius).Gap(8).Background(bg).Label(e.Name)
	row.Children(func() {
		ic := "read"
		if e.Dir {
			ic = "folder"
		}
		ui.Icon(c, icon(ic)).FontSize(small).TextColor(map[bool]ui.Color{true: p.accent, false: p.ink3}[e.Dir])
		name := ui.Text(c, e.Name).FontSize(small).SingleLine().Grow(1).Shrink(1)
		if e.Ignored || strings.HasPrefix(e.Name, ".") {
			name.TextColor(p.ink3)
		}
		if mark != "" {
			col := p.orange
			if mark != "•" {
				col = p.markColor(mark)
			}
			ui.Text(c, mark).Font("monospace").FontSize(11).Bold().TextColor(col)
		}
		if !e.Dir {
			ui.Text(c, sizeText(e.Size)).FontSize(11).TextColor(p.ink3).SingleLine()
		}
		if e.Mtime != "" {
			ui.Text(c, a.ago(e.Mtime, now)).FontSize(11).TextColor(p.ink3).SingleLine()
		}
	})
	if row.Hovered() && !on {
		row.Background(p.hover)
	}
	if row.Clicked() {
		if e.Dir {
			fs.dirs = append(fs.dirs, full)
			return
		}
		a.openFile(key, x, full)
	}
}

// openFile reads a file to show; a later page comes with more.
func (a *App) openFile(key string, x hubapi.Session, abs string) {
	fs := a.files(key)
	f := &fileState{path: abs, loading: true}
	fs.file, fs.mdSource = f, false
	a.readFile(x, f, 0)
}

func (a *App) readFile(x hubapi.Session, f *fileState, offset int64) {
	c := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		fc, err := c.client.ReadFile(ctx, x.MachineID, f.path, x.Cwd, offset)
		var img ui.ImageSource
		if err == nil && fc.Text == nil && fc.Data != "" && strings.HasPrefix(fc.Mime, "image/") {
			if data, derr := base64.StdEncoding.DecodeString(fc.Data); derr == nil {
				img, _ = ui.DecodeImage(data)
			}
		}
		a.Host.Post(func() {
			f.loading, f.more = false, false
			if err != nil {
				f.err = a.errorText(err)
				return
			}
			if offset > 0 && fc.Text != nil {
				f.text += *fc.Text
				f.content.Next, f.content.Truncated = fc.Next, fc.Truncated
				return
			}
			f.content, f.loaded, f.image = fc, true, img
			if fc.Text != nil {
				f.text = *fc.Text
			}
		})
	})
}

func (a *App) fileView(c *ui.Context, p palette, key string, x hubapi.Session, fs *filesState) {
	f := fs.file
	fc := f.content
	isMD := strings.HasSuffix(strings.ToLower(f.path), ".md") || fc.Mime == "text/markdown"
	ui.Row(c).Padding(10, 16).Gap(8).BorderWidth(0, 0, 1, 0).BorderColor(p.line).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, path.Base(f.path)).Font("monospace").FontSize(small).Bold().SingleLine()
			if f.loaded {
				ui.Text(c, sizeText(fc.Size)+" · "+f.path).FontSize(11).TextColor(p.ink3).SingleLine()
			}
		})
		if f.loaded && (isMD || viewerKind(f.path) == "table" || viewerKind(f.path) == "notebook") && fc.Text != nil {
			label := a.tr("filesSource")
			if fs.mdSource {
				label = a.tr("filesRendered")
			}
			if ui.Button(c, label).FontSize(small).Padding(2, 8).Clicked() {
				fs.mdSource = !fs.mdSource
			}
		}
		if ui.Button(c, a.tr("filesSave")).FontSize(small).Padding(2, 8).Disabled(fs.saving).Clicked() {
			a.saveCopy(key, x, f.path)
		}
		a.fileMenu(c, p, x, f)
	})
	switch {
	case f.err != "":
		a.empty(c, p, "warning", f.err, "")
		return
	case !f.loaded:
		ui.Spinner(c).Margin(ui.Auto)
		return
	case fc.IsDir:
		a.empty(c, p, "folder", a.tr("filesIsFolder"), "")
		return
	case f.image != nil:
		w, h := ui.ImageSize(f.image)
		ui.Scroll(c.Key("img-" + f.path)).Grow(1).Children(func() {
			ui.Column(c).Padding(16).AlignItems(ui.Center).Children(func() {
				if w > 900 {
					w, h = 900, h*900/w
				}
				ui.Image(c, f.image).Size(w, h).Fit(ui.Contain).Label(path.Base(f.path))
			})
		})
		return
	case fc.Text == nil:
		a.empty(c, p, "read", a.tr("filesNoPreview"), fc.Mime)
		return
	case viewerKind(f.path) == "table" && !fs.mdSource:
		a.tableView(c, p, f)
		return
	case viewerKind(f.path) == "notebook" && !fs.mdSource:
		a.notebookView(c, p, f)
		return
	}
	ui.Scroll(c.Key("text-" + f.path)).Grow(1).Children(func() {
		if isMD && !fs.mdSource {
			ui.Column(c).Padding(16, 24).MaxWidth(860).Children(func() {
				markdown.View(c.Key("md-"+f.path), f.text, &a.md)
			})
		} else {
			lines := strings.Split(strings.TrimSuffix(f.text, "\n"), "\n")
			width := float32(len(itoa(len(lines))))*8 + 8
			ui.Column(c).PaddingY(8).Children(func() {
				for i, l := range lines {
					ui.Row(c.Key("ln"+itoa(i))).Padding(0, 16, 0, 8).Gap(12).AlignItems(ui.Start).Children(func() {
						ui.Text(c, itoa(i+1)).Font("monospace").FontSize(small).TextColor(p.ink3).Width(width).TextAlign(ui.End)
						ui.Text(c, l).Font("monospace").FontSize(small).Selectable().Grow(1).Shrink(1)
					})
				}
			})
		}
		if f.content.Next > 0 {
			if ui.Button(c, a.tr("filesMore")).FontSize(small).Margin(8, 16).Disabled(f.more).Clicked() {
				f.more = true
				a.readFile(x, f, f.content.Next)
			}
		} else if f.content.Truncated {
			ui.Text(c, a.tr("changesTruncated")).FontSize(small).TextColor(p.ink3).Padding(8, 16)
		}
	})
}

// saveCopy asks where to save a file and downloads it there.
func (a *App) saveCopy(key string, x hubapi.Session, abs string) {
	fs := a.files(key)
	a.Host.SaveFile(a.tr("filesSave"), path.Base(abs), func(dest string) {
		fs.saving = true
		c := a.conn
		a.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			err := func() error {
				out, err := os.Create(dest)
				if err != nil {
					return err
				}
				if err := c.client.Download(ctx, x.MachineID, abs, x.Cwd, out); err != nil {
					out.Close()
					os.Remove(dest)
					return err
				}
				return out.Close()
			}()
			a.Host.Post(func() {
				fs.saving = false
				if err != nil {
					a.toast(a.errorText(err))
					return
				}
				a.toast(a.tr("filesSaved", "name", path.Base(dest)))
			})
		})
	})
}
