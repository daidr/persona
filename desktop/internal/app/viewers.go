package app

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// Viewers for files the Files pane can't show as text: tables (CSV / TSV), notebooks (.ipynb), and anything else
// opened in this computer's own app (a PDF, an Office document converted to PDF first, an HTML page in the browser).

// viewerKind is how a file is shown: "table", "notebook", "office", "html" or "" (as text).
func viewerKind(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".csv", ".tsv":
		return "table"
	case ".ipynb":
		return "notebook"
	case ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx", ".odt", ".odp", ".ods", ".rtf", ".key", ".pages", ".numbers":
		return "office"
	case ".html", ".htm":
		return "html"
	}
	return ""
}

// maxTableRows is how many rows of a table are drawn.
const maxTableRows = 500

// parseTable reads a CSV or TSV text as rows; a ragged or broken one still gives what it could read.
func parseTable(name, text string) [][]string {
	r := csv.NewReader(strings.NewReader(text))
	if strings.EqualFold(path.Ext(name), ".tsv") {
		r.Comma = '\t'
	}
	r.FieldsPerRecord, r.LazyQuotes = -1, true
	var rows [][]string
	for len(rows) <= maxTableRows {
		rec, err := r.Read()
		if err != nil {
			break
		}
		rows = append(rows, rec)
	}
	return rows
}

func (a *App) tableView(c *ui.Context, p palette, f *fileState) {
	rows := parseTable(f.path, f.text)
	if len(rows) == 0 {
		a.empty(c, p, "read", a.tr("viewEmptyTable"), "")
		return
	}
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	ui.ScrollBoth(c.Key("table-" + f.path)).Grow(1).Children(func() {
		ui.Column(c).Padding(12, 16).Children(func() {
			for i, r := range rows {
				row := ui.Row(c.Key("r"+itoa(i))).BorderWidth(0, 0, 1, 0).BorderColor(p.line)
				if i == 0 {
					row.Background(p.inset)
				}
				row.Children(func() {
					ui.Text(c, itoa(i+1)).Font("monospace").FontSize(11).TextColor(p.ink3).Width(44).Padding(6, 6).TextAlign(ui.End)
					for j := range cols {
						v := ""
						if j < len(r) {
							v = r[j]
						}
						t := ui.Text(c, v).FontSize(small).Width(160).Padding(6, 8).SingleLine().BorderWidth(0, 0, 0, 1).BorderColor(p.line).Tooltip(v)
						if i == 0 {
							t.Bold()
						}
					}
				})
			}
			if len(rows) > maxTableRows || f.content.Truncated {
				ui.Text(c, a.tr("viewTableCut", "rows", itoa(min(len(rows), maxTableRows)))).FontSize(small).TextColor(p.ink3).Padding(8, 0)
			}
		})
	})
}

// notebook is the part of a Jupyter notebook the viewer draws.
type notebook struct {
	Cells []struct {
		Type    string          `json:"cell_type"`
		Source  json.RawMessage `json:"source"`
		Count   *int            `json:"execution_count"`
		Outputs []struct {
			Type   string                     `json:"output_type"`
			Text   json.RawMessage            `json:"text"`
			Data   map[string]json.RawMessage `json:"data"`
			Ename  string                     `json:"ename"`
			Evalue string                     `json:"evalue"`
		} `json:"outputs"`
	} `json:"cells"`
}

// joined is a notebook string field, which is a string or a list of lines.
func joined(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return strings.Join(list, "")
	}
	return ""
}

func (a *App) notebookView(c *ui.Context, p palette, f *fileState) {
	var nb notebook
	if err := json.Unmarshal([]byte(f.text), &nb); err != nil || len(nb.Cells) == 0 {
		a.empty(c, p, "warning", a.tr("viewNotebookBad"), "")
		return
	}
	ui.Scroll(c.Key("nb-" + f.path)).Grow(1).Children(func() {
		ui.Column(c).Padding(16, 24).Gap(12).MaxWidth(900).Children(func() {
			for i, cell := range nb.Cells {
				src := joined(cell.Source)
				ui.Column(c.Key("cell" + itoa(i))).Gap(6).Children(func() {
					if cell.Type == "markdown" {
						markdown.View(c.Key("md"), src, &a.md)
						return
					}
					ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
						n := " "
						if cell.Count != nil {
							n = itoa(*cell.Count)
						}
						ui.Text(c, "["+n+"]").Font("monospace").FontSize(11).TextColor(p.ink3).Width(40)
						ui.Text(c, src).Font("monospace").FontSize(small).Selectable().Padding(8, 10).Radius(rowRadius).Background(p.inset).Grow(1).Shrink(1)
					})
					for k, out := range cell.Outputs {
						ui.Column(c.Key("o"+itoa(k))).Padding(0, 0, 0, 48).Children(func() {
							switch {
							case out.Type == "error":
								ui.Text(c, out.Ename+": "+out.Evalue).Font("monospace").FontSize(small).TextColor(p.red).Selectable()
							case out.Data["image/png"] != nil:
								if data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(joined(out.Data["image/png"]), "\n", "")); err == nil {
									if img, err := ui.DecodeImage(data); err == nil {
										w, h := ui.ImageSize(img)
										if w > 700 {
											w, h = 700, h*700/w
										}
										ui.Image(c, img).Size(w, h).Fit(ui.Contain).Label(a.tr("viewOutputImage"))
									}
								}
							case out.Data["text/plain"] != nil:
								ui.Text(c, joined(out.Data["text/plain"])).Font("monospace").FontSize(small).TextColor(p.ink2).Selectable()
							case out.Text != nil:
								ui.Text(c, joined(out.Text)).Font("monospace").FontSize(small).TextColor(p.ink2).Selectable()
							}
						})
					}
				})
			}
		})
	})
}

// openLocally downloads a file of the computer (an Office document converted to PDF first) into a folder of this
// computer's temporary files and opens it in the app the system picks.
func (a *App) openLocally(machine, abs, cwd string, office bool) {
	cn := a.conn
	a.toast(a.tr("viewOpening", "name", path.Base(abs)))
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		dest, err := func() (string, error) {
			src := abs
			if office {
				pdf, err := cn.client.ConvertFile(ctx, machine, abs, cwd, "pdf")
				if err != nil {
					return "", err
				}
				src = pdf
			}
			dir := filepath.Join(os.TempDir(), "persona-open")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return "", err
			}
			dest := filepath.Join(dir, path.Base(src))
			out, err := os.Create(dest)
			if err != nil {
				return "", err
			}
			if err := cn.client.Download(ctx, machine, src, cwd, out); err != nil {
				out.Close()
				os.Remove(dest)
				return "", err
			}
			return dest, out.Close()
		}()
		a.Host.Post(func() {
			if err != nil {
				a.toast(a.tr("viewOpenFailed", "error", a.errorText(err)))
				return
			}
			a.Host.OpenURL((&url.URL{Scheme: "file", Path: filepath.ToSlash(dest)}).String())
		})
	})
}

// fileMenu is the file's more menu: copy its path, open it in this computer's app (an Office document also as a PDF).
func (a *App) fileMenu(c *ui.Context, p palette, x hubapi.Session, f *fileState) {
	l := a.tr("fileMoreActions")
	more := ui.ButtonBase(c.Key("file-more")).Label(l).Tooltip(l).Padding(4, 6).Radius(rowRadius).
		Children(func() { ui.Icon(c, icon("more")).TextColor(p.ink2) })
	kind := viewerKind(f.path)
	more.Menu(func(m *ui.Menu) {
		if m.Item(a.tr("filesCopyPath")).Chosen() {
			c.WriteClipboard(f.path)
			a.toast(a.tr("filesPathCopied"))
		}
		open := a.tr("viewOpenApp")
		if kind == "html" {
			open = a.tr("viewOpenBrowser")
		}
		if m.Item(open).Chosen() {
			a.openLocally(x.MachineID, f.path, x.Cwd, false)
		}
		if kind == "office" && m.Item(a.tr("viewOpenPDF")).Chosen() {
			a.openLocally(x.MachineID, f.path, x.Cwd, true)
		}
	})
}
