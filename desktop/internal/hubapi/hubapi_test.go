package hubapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/desktop/internal/hubs"
)

func tlsHub(t *testing.T, h http.Handler) (*httptest.Server, hubs.Hub) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	// httptest's certificate is for 127.0.0.1 and example.com, signed by a CA nobody trusts
	return srv, hubs.Hub{URL: srv.URL, FP: hubs.SPKIFingerprint(srv.Certificate().Raw)}
}

func TestPinnedTLS(t *testing.T) {
	srv, hub := tlsHub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer jd_test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"machines": []Machine{{ID: "m1", Name: "Box", Online: true}}})
	}))
	ctx := context.Background()

	got, err := New(hub, "jd_test", "en").Machines(ctx)
	if err != nil || len(got) != 1 || got[0].Name != "Box" {
		t.Fatalf("pinned hub: %v %v", got, err)
	}
	if _, err := New(hub, "wrong", "en").Machines(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("bad token: %v", err)
	}
	other := hub
	other.FP = "AAAA"
	if _, err := New(other, "jd_test", "en").Machines(ctx); err == nil {
		t.Fatal("a certificate with another key was accepted")
	}
	none := hubs.Hub{URL: srv.URL}
	if _, err := New(none, "jd_test", "en").Machines(ctx); err == nil {
		t.Fatal("an untrusted certificate was accepted without a pin")
	}
}

func TestErrorsAndWrites(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	_, hub := tlsHub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path+" op="+boolText(r.Header.Get("X-Persona-Op-Id") != ""))
		mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/interactions/gone/resolve":
			w.WriteHeader(http.StatusGone)
		case "/api/v1/interactions/bad/resolve":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"bad_request","error":"No such option"}`))
		case "/api/v1/auth/tokens":
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in["kind"] != Platform || in["name"] == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"id":"d1","token":"jd_new","role":"full"}`))
		}
	}))
	c := New(hub, "jp_pair", "en")
	ctx := context.Background()
	yes := true
	if err := c.Resolve(ctx, "gone", Resolution{Allow: &yes}); !errors.Is(err, ErrGone) {
		t.Fatalf("410: %v", err)
	}
	var e *Error
	if err := c.Resolve(ctx, "bad", Resolution{Option: "x"}); !errors.As(err, &e) || e.Code != "bad_request" || e.Error() != "No such option" {
		t.Fatalf("400: %v", err)
	}
	tok, err := c.Exchange(ctx, "Persona Desktop")
	if err != nil || tok != "jd_new" {
		t.Fatalf("exchange: %q %v", tok, err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, s := range seen {
		if !strings.HasSuffix(s, "op=yes") {
			t.Errorf("write without an op id: %s", s)
		}
	}
}

func boolText(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func TestResolutionShapes(t *testing.T) {
	no := false
	cases := map[string]Resolution{
		`{"allow":false}`:                    {Allow: &no},
		`{"approve":false,"feedback":"why"}`: {Approve: &no, Feedback: "why"},
		`{"answers":{"Q":"A"}}`:              {Answers: map[string]string{"Q": "A"}},
		`{"option":"2"}`:                     {Option: "2"},
		`{"dismiss":true}`:                   {Dismiss: true},
	}
	for want, r := range cases {
		b, _ := json.Marshal(r)
		if string(b) != want {
			t.Errorf("got %s, want %s", b, want)
		}
	}
}

func TestSocket(t *testing.T) {
	var mu sync.Mutex
	var got []string
	conns := 0
	_, hub := tlsHub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer jd_test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		mu.Lock()
		conns++
		n := conns
		mu.Unlock()
		ctx := r.Context()
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"hello","hub_name":"Home","features":["app_ping"]}`))
		for i := 0; ; i++ {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var m map[string]any
			_ = json.Unmarshal(data, &m)
			mu.Lock()
			got = append(got, m["type"].(string)+":"+str(m["session_id"]))
			mu.Unlock()
			if n == 1 && m["type"] == "subscribe" {
				// drop the first connection once subscribed: the socket reconnects and subscribes again
				_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"sessions","machine_id":"m1","sessions":[]}`))
				c.Close(websocket.StatusGoingAway, "restart")
				return
			}
		}
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	events := make(chan string, 16)
	s := &Socket{Client: New(hub, "jd_test", "en"), DeviceID: "dev1"}
	s.OnEvent = func(e Event) {
		events <- e.Type
		if e.Type == "hello" {
			s.Subscribe("m1", "s1")
		}
	}
	go s.Run(ctx)
	want := []string{"hello", "sessions", "hello"}
	for _, w := range want {
		select {
		case e := <-events:
			if e != w {
				t.Fatalf("event %q, want %q", e, w)
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for", w)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		subs := strings.Count(strings.Join(got, ","), "subscribe:s1")
		mu.Unlock()
		if subs >= 2 {
			mu.Lock()
			defer mu.Unlock()
			if got[0] != "client_hello:" {
				t.Fatalf("first message %q", got[0])
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("not subscribed again after reconnecting: %v", got)
}

func TestSocketStopsWhenRefused(t *testing.T) {
	_, hub := tlsHub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var last Status
	s := &Socket{Client: New(hub, "x", "en"), OnStatus: func(st Status) { last = st }}
	s.Run(ctx)
	if ctx.Err() != nil || !errors.Is(last.Err, ErrUnauthorized) {
		t.Fatalf("Run kept going or ended with %v", last.Err)
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func TestCan(t *testing.T) {
	if !(Session{}).Can("send") {
		t.Fatal("a Persona session allows everything")
	}
	ext := Session{SourceKind: "ext", Capabilities: &Capabilities{Read: true, Approve: true}}
	if ext.Can("send") || !ext.Can("approve") || (Session{SourceKind: "ext"}).Can("approve") {
		t.Fatal("capabilities")
	}
}

func TestSplitViews(t *testing.T) {
	text := "Here:\r\n\r\n~~~~Persona-View\n{\"fallback\":\"F\",\"sections\":[{\"items\":[{\"type\":\"row\",\"title\":\"A\",\"action\":{\"id\":\"a\",\"reply\":\"pick A\"}}]}]}\n~~~~\n" +
		"```go\n```persona-view\nnot a card\n```\n" +
		"```persona-view\n{bad\n```\n" +
		"```persona-view\n{\"type\":\"metric\",\"label\":\"Steps\",\"value\":\"8432\",\"fallback\":\"8432 steps\"}\n```\n" +
		"after\n```persona-view\n{\"sec"
	parts := SplitViews(text)
	if len(parts) != 7 {
		t.Fatalf("%d parts: %+v", len(parts), parts)
	}
	if parts[0].Markdown != "Here:" || parts[1].View == nil || parts[1].Index != 0 || parts[1].View.Sections[0].Items[0].Action.Reply != "pick A" {
		t.Fatalf("first card %+v", parts[1])
	}
	if !strings.Contains(parts[2].Markdown, "not a card") {
		t.Fatalf("code kept: %+v", parts[2])
	}
	if !parts[3].IsView || parts[3].View != nil || parts[3].Index != 1 {
		t.Fatalf("invalid block %+v", parts[3])
	}
	if parts[4].View == nil || parts[4].View.Fallback != "8432 steps" || parts[4].View.Sections[0].Items[0].Value != "8432" || parts[4].Index != 2 {
		t.Fatalf("shorthand %+v", parts[4])
	}
	if parts[5].Markdown != "after" || !parts[6].Open || parts[6].Index != 3 {
		t.Fatalf("open block %+v %+v", parts[5], parts[6])
	}
}
