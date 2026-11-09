package app

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

func TestTermKey(t *testing.T) {
	for _, c := range []struct {
		k    ui.Key
		m    ui.Modifiers
		want string
	}{{ui.KeyEnter, 0, "enter"}, {ui.KeyC, ui.Ctrl, "ctrl+c"}, {ui.KeyTab, ui.Shift, "shift+tab"}, {ui.KeyLeft, ui.Alt, "alt+left"},
		{ui.KeyA, 0, ""}, {ui.KeyF5, 0, "f5"}} {
		if got := termKey(c.k, c.m); got != c.want {
			t.Errorf("%v %v: %q, want %q", c.k, c.m, got, c.want)
		}
	}
	if editCommand(ui.KeyA, ui.Super, true) != "selectAll" || editCommand(ui.KeyA, ui.Ctrl, true) != "" ||
		editCommand(ui.KeyZ, ui.Ctrl|ui.Shift, false) != "redo" || editCommand(ui.KeyY, ui.Ctrl, false) != "redo" {
		t.Fatal("edit commands")
	}
	if cdpMods(ui.Alt|ui.Shift) != 9 || previewAddress("http://localhost:5173/a?b", 5173) != "/a?b" ||
		previewAddress("http://localhost:3000/", 5173) != "http://localhost:3000/" {
		t.Fatal("mods / address")
	}
	if x, y, s := fitIn(ui.Rect{W: 400, H: 400}, 800, 400); s != 0.5 || x != 0 || y != 100 {
		t.Fatalf("fit: %v %v %v", x, y, s)
	}
}

func TestTerminalPage(t *testing.T) {
	a, _ := signedInTo(t, settingsMockURL, "en")
	a.conn.data.Hello.Features = append(a.conn.data.Hello.Features, "terminal_panes")
	a.Navigate("/terminal")
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	tt.Frame()
	g := &a.terms10
	if len(g.hosts) == 0 || !tt.HasText(a.tr("termsTitle")) || !tt.HasText(a.tr("termsPickTitle")) {
		t.Fatalf("panes: %v %v", g.errs, tt.Texts())
	}
	shot(t, tt, "terminals-light")
	// open the first pane an agent runs in: its screen, typed into directly
	var open string
	for mid, hosts := range g.hosts {
		for _, h := range hosts {
			for _, pn := range h.Panes {
				if open == "" && pn.SessionID != "" {
					open = mid + "/" + pn.SessionID
				}
			}
		}
	}
	if open == "" {
		t.Fatal("no agent pane")
	}
	a.Navigate("/terminal/" + open)
	tt.Frame()
	tt.Frame()
	tt.Frame()
	st := a.term(open)
	if !st.loaded || !tt.HasText(a.tr("terminalTypeHint")) {
		t.Fatalf("screen: %q %v", st.err, tt.Texts())
	}
	shot(t, tt, "terminal-pane-light")
	tt.SetDark(true)
	shot(t, tt, "terminal-pane-dark")
	tt.SetDark(false)
	// earlier output
	a.readHistory(open)
	tt.Frame()
	if !st.histRead || st.histErr != "" {
		t.Fatalf("history: %v %q", st.histRead, st.histErr)
	}
}

func TestFileViewers(t *testing.T) {
	if viewerKind("a/b.TSV") != "table" || viewerKind("x.ipynb") != "notebook" || viewerKind("deck.pptx") != "office" || viewerKind("x.go") != "" {
		t.Fatal("kinds")
	}
	rows := parseTable("t.csv", "name,city\n\"Doe, J\",Paris\nbad\"quote,x\n")
	if len(rows) != 3 || rows[1][0] != "Doe, J" || len(rows[2]) != 2 {
		t.Fatalf("rows: %q", rows)
	}
	if joined([]byte(`["a\n","b"]`)) != "a\nb" || joined([]byte(`"c"`)) != "c" {
		t.Fatal("joined")
	}
	a, _ := signedInTo(t, gitMockURL, "en")
	x := weatherAgent(t, a)
	key := x.Key()
	a.Navigate("/agents/" + key)
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	a.chat(key).pane = paneFiles
	tt.Frame()
	tt.Frame()
	fs := a.files(key)
	show := func(name, text string) {
		fs.file = &fileState{path: x.Cwd + "/" + name, loaded: true, text: text, content: hubapi.FileContent{Text: &text, Size: int64(len(text))}}
		tt.Frame()
		tt.Frame()
	}
	show("forecast.csv", "day,high,low\nMon,21,12\nTue,23,14\n")
	if !tt.HasText("Tue") || !tt.HasText(a.tr("filesSource")) {
		t.Fatalf("table: %v", tt.Texts())
	}
	shot(t, tt, "file-table")
	show("analysis.ipynb", `{"cells":[{"cell_type":"markdown","source":["# Rain\n","Hourly totals"]},`+
		`{"cell_type":"code","execution_count":3,"source":"print(sum(rain))","outputs":[{"output_type":"stream","text":["42\n"]}]},`+
		`{"cell_type":"code","execution_count":4,"source":"1/0","outputs":[{"output_type":"error","ename":"ZeroDivisionError","evalue":"division by zero"}]}]}`)
	if !tt.HasText("[3]") || !tt.HasText("Hourly totals") || !tt.HasText("ZeroDivisionError: division by zero") {
		t.Fatalf("notebook: %v", tt.Texts())
	}
	shot(t, tt, "file-notebook")
	fs.file = &fileState{path: x.Cwd + "/plan.docx", loaded: true}
	tt.Frame()
	if err := tt.Click(a.tr("fileMoreActions")); err != nil {
		t.Fatal(err)
	}
	if items := strings.Join(tt.Menu(), "|"); items != a.tr("filesCopyPath")+"|"+a.tr("viewOpenApp")+"|"+a.tr("viewOpenPDF") || !tt.HasText(a.tr("filesNoPreview")) {
		t.Fatalf("office: %q", items)
	}
	tt.CloseMenu()
}

func TestPreviewPane(t *testing.T) {
	a, _ := signedInTo(t, gitMockURL, "en")
	a.conn.data.Hello.Features = append(a.conn.data.Hello.Features, hubapi.FeaturePreview)
	x := weatherAgent(t, a)
	key := x.Key()
	a.Navigate("/agents/" + key)
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	if !tt.HasText(a.tr("panePreview")) {
		t.Fatalf("no Preview tab: %v", tt.Texts())
	}
	// the servers as the computer would list them (the mock has no previews)
	st := a.previewOf(key)
	st.loaded = true
	st.servers = hubapi.PreviewServers{Browser: "Chrome", Servers: []hubapi.PreviewServer{
		{Name: "web", Port: 5173, Command: []string{"npm", "run", "dev"}},
		{Name: "storybook", Port: 6006, Listening: true, Title: "Storybook"},
		{Name: "remote", Problem: "not_local"},
	}}
	a.chat(key).pane = panePreview
	tt.Frame()
	tt.Frame()
	if !tt.HasText("storybook") || !tt.HasText(a.tr("previewOpen")) || !tt.HasText(a.previewProblemText("not_local")) {
		t.Fatalf("servers: %v", tt.Texts())
	}
	shot(t, tt, "preview-servers")
	if x.WorkspaceID != "" {
		if err := tt.Click(a.tr("previewStart")); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if !tt.HasText("npm run dev") {
			t.Fatalf("confirm: %v", tt.Texts())
		}
		st.confirm = nil
		tt.Frame()
	}
	// a page being shown: its picture and address, and the input that goes back
	img := image.NewRGBA(image.Rect(0, 0, 640, 400))
	for i := range img.Pix {
		img.Pix[i] = 0xee
	}
	img.Set(10, 10, color.Black)
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, nil)
	frame, err := ui.DecodeImage(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	st.busy, st.frame, st.fw, st.fh = true, frame, 640, 400
	st.page = hubapi.PreviewEvent{T: "page", URL: "http://localhost:6006/docs", Title: "Storybook", Back: true}
	st.address = previewAddress(st.page.URL, 6006)
	st.extern = "https://example.com/"
	tt.Frame()
	tt.Frame()
	if !tt.HasText(a.tr("previewDesktop")) || !strings.Contains(strings.Join(tt.Texts(), "\n"), "example.com") {
		t.Fatalf("page: %v", tt.Texts())
	}
	if a.previewArea.W <= 0 {
		t.Fatalf("area: %+v", a.previewArea)
	}
	shot(t, tt, "preview-page")
	tt.SetDark(true)
	shot(t, tt, "preview-page-dark")
	tt.SetDark(false)
}
