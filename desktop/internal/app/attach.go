package app

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// Limits on what a message carries, as in the web and phone apps (docs/protocol/protocol.md "Sending files").
const (
	maxImage  = 8 << 20
	maxFile   = 200 << 20
	maxImages = 4
)

// attachment is a picture waiting in the composer, sent with the next message.
type attachment struct {
	name string
	mime string
	data []byte
	img  ui.ImageSource
}

// imageMime is a picture's media type by its name, "" for anything else (which goes as a file).
func imageMime(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return ""
}

// quotePath puts a path in single quotes when a shell would split or expand it, as the web app does.
func quotePath(path string) string {
	if !strings.ContainsAny(path, " \t\n'\"\\$`") {
		return path
	}
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

// pickFiles asks for files to attach to the draft of the chat key.
func (a *App) pickFiles(key string) {
	a.Host.PickFiles(a.tr("attachTitle"), func(paths []string) { a.addFiles(key, paths) })
}

// addFiles attaches files to a draft: pictures go with the message, other files are sent to the agent's computer and
// their path lands in the draft, as the web app does. Nothing is sent until the user sends the message.
func (a *App) addFiles(key string, paths []string) {
	if a.conn == nil {
		return
	}
	c := a.conn
	st := a.chat(key)
	machine, _, _ := strings.Cut(key, "/")
	refused := false
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			a.toast(a.tr("attachNotFile", "name", filepath.Base(path)))
			continue
		}
		name := filepath.Base(path)
		if mime := imageMime(name); mime != "" {
			if len(st.files)+st.reading >= maxImages {
				refused = true
				continue
			}
			if info.Size() > maxImage {
				a.toast(a.tr("attachImageTooBig", "name", name))
				continue
			}
			st.reading++
			a.Go(func() {
				data, err := os.ReadFile(path)
				var img ui.ImageSource
				if err == nil {
					img, err = ui.DecodeImage(data)
				}
				a.Host.Post(func() {
					st.reading--
					if !a.live(c) {
						return
					}
					if err != nil {
						a.toast(a.tr("attachUnreadable", "name", name))
						return
					}
					st.files = append(st.files, attachment{name: name, mime: mime, data: data, img: img})
				})
			})
			continue
		}
		if info.Size() > maxFile {
			a.toast(a.tr("attachFileTooBig", "name", name))
			continue
		}
		st.uploads++
		size := info.Size()
		a.Go(func() {
			var where string
			f, err := os.Open(path)
			if err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				where, err = c.client.Upload(ctx, machine, name, f, size)
				cancel()
				f.Close()
			}
			a.Host.Post(func() {
				st.uploads--
				if !a.live(c) {
					return
				}
				if err != nil {
					a.toast(a.tr("attachUploadFailed", "name", name, "error", a.errorText(err)))
					return
				}
				st.draft = insertPath(st.draft, quotePath(where))
			})
		})
	}
	if refused {
		a.toast(a.tr("attachTooMany", "count", itoa(maxImages)))
	}
}

// insertPath puts a path at the end of the draft, a space apart from what's there.
func insertPath(draft, path string) string {
	if draft != "" && !strings.HasSuffix(draft, " ") && !strings.HasSuffix(draft, "\n") {
		draft += " "
	}
	return draft + path + " "
}

// images are the draft's pictures as the hub takes them.
func images(files []attachment) []hubapi.Image {
	out := make([]hubapi.Image, 0, len(files))
	for _, f := range files {
		out = append(out, hubapi.Image{Mime: f.mime, Data: base64.StdEncoding.EncodeToString(f.data)})
	}
	return out
}

// attachmentsRow shows the pictures waiting in the composer, each with a button that takes it off, and the files
// still on their way.
func (a *App) attachmentsRow(c *ui.Context, p palette, key string, st *chatState) {
	if len(st.files) == 0 && st.uploads == 0 && st.reading == 0 {
		return
	}
	ui.Row(c).Margin(4, pagePad, 0, pagePad).Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
		remove := -1
		for i, f := range st.files {
			ui.Box(c.Key("att:"+key+"#"+itoa(i))).Size(64, 64).Radius(8).Border(1, p.line).Background(p.inset).Clip().Children(func() {
				if f.img != nil {
					ui.Image(c, f.img).Fill().Fit(ui.Cover).Label(f.name).Tooltip(f.name)
				}
				if ui.Button(c, "").Label(a.tr("attachRemove", "name", f.name)).Tooltip(a.tr("attachRemove", "name", f.name)).
					Absolute().Top(2).Right(2).Padding(2).Radius(10).Background(p.surface).
					Children(func() { ui.Icon(c, icon("x")).FontSize(small) }).Clicked() {
					remove = i
				}
			})
		}
		if remove >= 0 {
			st.files = append(st.files[:remove:remove], st.files[remove+1:]...)
		}
		if n := st.uploads + st.reading; n > 0 {
			ui.Row(c).Gap(6).Children(func() {
				ui.Spinner(c).Size(14, 14)
				ui.Text(c, a.tr("attachSending", "count", itoa(n))).FontSize(small).TextColor(p.ink2)
			})
		}
	})
}

// imageView is a picture in a chat, decoded once.
func (a *App) imageView(c *ui.Context, p palette, it hubapi.Item, mine bool) {
	src, ok := a.images[it.ID]
	if !ok && it.Image != nil && it.Image.Data != "" {
		if data, err := base64.StdEncoding.DecodeString(it.Image.Data); err == nil {
			src, _ = ui.DecodeImage(data)
		}
		if len(a.images) > 200 {
			a.images = map[string]ui.ImageSource{}
		}
		a.images[it.ID] = src
	}
	ui.Column(c).AlignItems(map[bool]ui.Align{true: ui.End, false: ui.Start}[mine]).Children(func() {
		if src == nil {
			ui.Row(c).Gap(6).Children(func() {
				ui.Icon(c, icon("read")).FontSize(small).TextColor(p.ink3)
				ui.Text(c, a.tr("image")).FontSize(small).TextColor(p.ink3)
			})
			return
		}
		w, h := ui.ImageSize(src)
		if w > 320 {
			w, h = 320, h*320/w
		}
		if h > 320 {
			w, h = w*320/h, 320
		}
		img := ui.Image(c, src).Size(w, h).Radius(10).Fit(ui.Contain).Label(a.tr("image"))
		if it.Seq < 0 {
			img.Opacity(0.6)
		}
	})
}
