package hubapi

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/desktop/internal/hubs"
)

func TestParseSGR(t *testing.T) {
	runs := ParseSGR("plain \x1b[1;31mred\x1b[0m \x1b[38;2;10;20;30mrgb\x1b[m\x1b]0;title\x07 end\x1b[2K")
	var got []string
	for _, r := range runs {
		got = append(got, r.Text)
	}
	if strings.Join(got, "|") != "plain |red| |rgb| end" {
		t.Fatalf("runs: %q", got)
	}
	if !runs[1].Bold || runs[1].FG != 1 || runs[1].FGRGB || runs[2].FG != -1 {
		t.Fatalf("red: %+v %+v", runs[1], runs[2])
	}
	if !runs[3].FGRGB || runs[3].FG != 10<<16|20<<8|30 {
		t.Fatalf("rgb: %+v", runs[3])
	}
	if PlainLine("a\x1b[32mb\x1b[0mc") != "abc" {
		t.Fatal("plain")
	}
}

func TestPreviewOptions(t *testing.T) {
	res := PreviewServers{Browser: "Chrome", Servers: []PreviewServer{
		{Name: "web", Port: 5173, Command: []string{"npm", "run", "dev"}, Env: map[string]string{"B": "x y", "A": "1"}},
		{Name: "docs", Port: 4000, URL: "http://localhost:4000/guide?x=1", Listening: true},
		{Name: "broken", Problem: "no_port"},
	}}
	opts := PreviewOptions(res, []Port{{Port: 5173}, {Port: 8080, Title: "API"}})
	if len(opts) != 3 || !opts[0].Listening || !opts[0].Launch || opts[1].Path != "/guide?x=1" || opts[2].Name != "API" || opts[2].Launch {
		t.Fatalf("options: %+v", opts)
	}
	if got := CommandLine(opts[0]); got != "env 'A=1' 'B=x y' npm run dev" {
		t.Fatalf("command: %q", got)
	}
	if ev, ok := ParsePreviewBinary([]byte{'f', 0, 0, 0, 7, 1, 0, 0, 200, 0xff}); !ok || ev.Seq != 7 || ev.FW != 256 || ev.FH != 200 || len(ev.Frame) != 1 {
		t.Fatalf("frame: %+v", ev)
	}
	if _, ok := ParsePreviewBinary([]byte{'s', 0, 0, 0, 1, 9}); ok {
		t.Fatal("a screenshot is not a picture")
	}
}

// TestOpenPreview drives a preview against a stand-in for the hub: the address it asks for, a picture and the page,
// the input it sends back in order, and the close.
func TestOpenPreview(t *testing.T) {
	got := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/machines/m1/preview/connect" || r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no", http.StatusNotFound)
			return
		}
		q := r.URL.Query()
		if q.Get("port") != "5173" || q.Get("path") != "/app" || q.Get("w") != "800" || q.Get("mobile") != "" {
			http.Error(w, "query", http.StatusBadRequest)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ctx := r.Context()
		frame := make([]byte, 9, 12)
		frame[0] = 'f'
		binary.BigEndian.PutUint32(frame[1:], 3)
		binary.BigEndian.PutUint16(frame[5:], 800)
		binary.BigEndian.PutUint16(frame[7:], 600)
		_ = c.Write(ctx, websocket.MessageBinary, append(frame, 1, 2, 3))
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"t":"page","url":"http://localhost:5173/app","title":"App","back":true}`))
		for range 3 {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			got <- string(data)
		}
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"t":"closed","code":"tab_closed"}`))
		_ = c.Close(websocket.StatusNormalClosure, "")
	}))
	defer srv.Close()
	cl := New(hubs.Hub{URL: srv.URL}, "tok", "en")
	events := make(chan PreviewEvent, 8)
	pv, err := cl.OpenPreview(context.Background(), "m1", 5173, "/app", PreviewSize{W: 800, H: 600, DPR: 1}, func(ev PreviewEvent) { events <- ev })
	if err != nil {
		t.Fatal(err)
	}
	defer pv.Close()
	next := func() PreviewEvent {
		select {
		case ev := <-events:
			return ev
		case <-time.After(5 * time.Second):
			t.Fatal("no event")
		}
		return PreviewEvent{}
	}
	if ev := next(); ev.T != "frame" || ev.Seq != 3 || ev.FW != 800 || len(ev.Frame) != 3 {
		t.Fatalf("frame: %+v", ev)
	}
	if ev := next(); ev.T != "page" || ev.Title != "App" || !ev.Back {
		t.Fatalf("page: %+v", ev)
	}
	pv.Send(map[string]any{"t": "ack", "seq": 3})
	pv.Send(map[string]any{"t": "mouse", "type": "down"})
	pv.Send(map[string]any{"t": "mouse", "type": "up"})
	var order []string
	for range 3 {
		var m map[string]any
		_ = json.Unmarshal([]byte(<-got), &m)
		order = append(order, m["t"].(string)+":"+func() string { s, _ := m["type"].(string); return s }())
	}
	if strings.Join(order, ",") != "ack:,mouse:down,mouse:up" {
		t.Fatalf("order: %v", order)
	}
	if ev := next(); ev.T != "closed" || ev.Code != "tab_closed" {
		t.Fatalf("closed: %+v", ev)
	}
}
