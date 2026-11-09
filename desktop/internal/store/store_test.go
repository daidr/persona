package store

import (
	"testing"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

func ev(typ, raw string) hubapi.Event { return hubapi.Event{Type: typ, Raw: []byte(raw)} }

func TestApply(t *testing.T) {
	s := New()
	if fx := s.Apply(ev("hello", `{"type":"hello","hub_name":"Home"}`)); !fx.Inbox || s.Hello.HubName != "Home" {
		t.Fatal("hello")
	}
	s.Apply(ev("machines", `{"machines":[{"id":"b","name":"beta"},{"id":"a","name":"Alpha"}]}`))
	if s.Machines[0].ID != "a" || s.MachineName("b") != "beta" || s.MachineName("zz") != "zz" {
		t.Fatalf("machines %+v", s.Machines)
	}
	s.Apply(ev("sessions", `{"machine_id":"a","sessions":[
		{"session_id":"1","title":"one","state":"working","updated_at":"2026-01-01T00:00:00Z"},
		{"session_id":"2","title":"two","state":"idle","updated_at":"2026-01-02T00:00:00Z"},
		{"session_id":"3","title":"three","state":"idle","updated_at":"2026-01-03T00:00:00Z"},
		{"session_id":"x","machine_id":"b","title":"elsewhere"},
		{"session_id":"as","title":"assistant","assistant":true},
		{"session_id":"old","title":"archived","archived":true}]}`))
	s.Apply(ev("interaction", `{"interaction":{"id":"i1","machine_id":"a","session_id":"2","kind":"permission"}}`))

	g := s.Agents("")
	if len(g) != 3 || g[0].State != hubapi.StateNeedsYou || g[0].Agents[0].SessionID != "2" ||
		g[1].State != hubapi.StateWorking || g[2].Agents[0].SessionID != "3" {
		t.Fatalf("groups %+v", g)
	}
	if s.NeedsYou() != 1 {
		t.Fatalf("needs you %d", s.NeedsYou())
	}
	if g := s.Agents("THREE"); len(g) != 1 || g[0].Agents[0].Title != "three" {
		t.Fatalf("query %+v", g)
	}
	if g := s.Agents("alpha"); len(g) != 3 {
		t.Fatal("a query matches the computer's name")
	}
	s.Apply(ev("interaction_resolved", `{"id":"i1"}`))
	if s.NeedsYou() != 0 {
		t.Fatal("resolved card still counted")
	}
	if fx := s.Apply(ev("notify", `{"kind":"done","title":"T","body":"B","session_id":"1"}`)); fx.Notify == nil || fx.Notify.Title != "T" {
		t.Fatal("notify")
	}
	if fx := s.Apply(ev("inbox", `{}`)); !fx.Inbox {
		t.Fatal("inbox hint")
	}
	s.Apply(ev("machines", `{"machines":[{"id":"b"}]}`))
	if _, ok := s.Sessions["a"]; ok {
		t.Fatal("a removed computer keeps its agents")
	}
}

func TestTranscript(t *testing.T) {
	s := New()
	tr := s.Transcript("a/1")
	tr.Merge([]hubapi.Item{{ID: "b", Seq: 2, Text: "two"}, {ID: "a", Seq: 1, Text: "one"}})
	tr.Pending("hi", "c1")
	s.Apply(ev("delta", `{"machine_id":"a","session_id":"1","text":"writ"}`))
	if tr.Live != "writ" || tr.Items[2].Seq != -1 {
		t.Fatalf("%+v", tr)
	}
	s.Apply(ev("items", `{"machine_id":"a","session_id":"1","items":[{"id":"c","seq":3,"role":"user","text":"hi","client_msg_id":"c1"},{"id":"b","seq":2,"text":"TWO"}]}`))
	if len(tr.Items) != 3 || tr.Items[1].Text != "TWO" || tr.Items[2].ID != "c" || tr.Live != "" || tr.Oldest() != 1 {
		t.Fatalf("%+v", tr.Items)
	}
	tr.Pending("lost", "c2")
	tr.Drop("c2")
	if len(tr.Items) != 3 {
		t.Fatal("drop")
	}
}

func TestMainConversation(t *testing.T) {
	s := New()
	if _, ok := s.MainConversation(); ok {
		t.Fatal("no assistant")
	}
	s.Assistant = hubapi.Assistant{MachineID: "srv", Main: "m", Conversations: []hubapi.Session{{SessionID: "m", Title: "Today"}}}
	x, ok := s.MainConversation()
	if !ok || x.MachineID != "srv" || x.Title != "Today" {
		t.Fatalf("%+v", x)
	}
	if got, ok := s.Session("srv/m"); !ok || got.Title != "Today" {
		t.Fatalf("session %+v", got)
	}
}

func TestInbox(t *testing.T) {
	s := New()
	s.SetInbox([]hubapi.InboxItem{{ID: "1"}, {ID: "2", DoneAt: "x"}})
	if s.InboxOpen() != 1 {
		t.Fatal("open")
	}
	s.MarkInbox("1", true, "now")
	if s.InboxOpen() != 0 {
		t.Fatal("mark")
	}
}

func TestTodos(t *testing.T) {
	s := New()
	if fx := s.Apply(ev("todos", `{"type":"todos"}`)); !fx.Todos {
		t.Fatal("todos hint")
	}
	s.SetTodos([]hubapi.Todo{{ID: "a", Title: "A"}, {ID: "b", DoneAt: "x"}})
	s.PutTodo(hubapi.Todo{ID: "c", Title: "C"})
	s.PutTodo(hubapi.Todo{ID: "a", Title: "A2"})
	if s.TodosOpen() != 2 || s.Todos[0].ID != "c" {
		t.Fatalf("%+v", s.Todos)
	}
	if x, ok := s.Todo("a"); !ok || x.Title != "A2" {
		t.Fatal("put replaces")
	}
	s.PutSession(hubapi.Session{MachineID: "m", SessionID: "new", Title: "N"})
	if x, ok := s.Session("m/new"); !ok || x.Title != "N" {
		t.Fatal("put session")
	}
	s.DropTodo("c")
	if _, ok := s.Todo("c"); ok {
		t.Fatal("drop")
	}
}

// A message sent with pictures shows them at once; the hub's copy replaces the text and the pictures alike.
func TestPendingImages(t *testing.T) {
	var tr Transcript
	tr.Pending("look", "c1", hubapi.Image{Mime: "image/png", Data: "AA=="}, hubapi.Image{Mime: "image/png", Data: "AQ=="})
	if len(tr.Items) != 3 || tr.Items[0].Kind != "image" || tr.Items[2].Kind != "text" {
		t.Fatalf("pending: %+v", tr.Items)
	}
	tr.Merge([]hubapi.Item{{ID: "u1", Seq: 10, Role: "user", Kind: "text", Text: "look", ClientMsg: "c1"}, {ID: "u2", Seq: 11, Role: "user", Kind: "image"}})
	if len(tr.Items) != 2 || tr.Items[0].ID != "u1" || tr.Items[1].ID != "u2" {
		t.Fatalf("merged: %+v", tr.Items)
	}
	tr.Pending("", "c2", hubapi.Image{Mime: "image/png"})
	tr.Drop("c2")
	if len(tr.Items) != 2 {
		t.Fatalf("dropped: %+v", tr.Items)
	}
}
