// Package store is what the app knows about one hub: computers, agents, open cards, transcripts, the assistant
// and the Inbox, kept current from REST reads and WebSocket events. It is plain data with no locking: the app
// changes it only on the UI thread.
package store

import (
	"cmp"
	"slices"
	"strings"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// Transcript is a session's items as far as the app has read them.
type Transcript struct {
	Items   []hubapi.Item
	Loaded  bool
	HasMore bool
	// Live is the reply the agent is writing, not yet in the transcript.
	Live string
	// Err is why the last read failed.
	Err string
}

// Notify is a notification the hub asks connected clients to post.
type Notify struct {
	Kind          string `json:"kind"`
	MachineID     string `json:"machine_id"`
	SessionID     string `json:"session_id"`
	InteractionID string `json:"interaction_id"`
	ID            string `json:"id"`
	Title         string `json:"title"`
	Subtitle      string `json:"subtitle"`
	Body          string `json:"body"`
	Quiet         bool   `json:"quiet"`
}

// Effect is what the app should do after an event: re-read something, or post a notification.
type Effect struct {
	Inbox     bool
	Assistant bool
	Notify    *Notify
}

// Store is one hub's state.
type Store struct {
	Hello        hubapi.Hello
	Machines     []hubapi.Machine
	Sessions     map[string][]hubapi.Session
	Interactions map[string]hubapi.Interaction
	Transcripts  map[string]*Transcript
	Assistant    hubapi.Assistant
	Inbox        []hubapi.InboxItem
	InboxLoaded  bool
}

// New is an empty store.
func New() *Store {
	return &Store{
		Sessions:     map[string][]hubapi.Session{},
		Interactions: map[string]hubapi.Interaction{},
		Transcripts:  map[string]*Transcript{},
	}
}

// Apply takes one WebSocket event into the store.
func (s *Store) Apply(ev hubapi.Event) Effect {
	var fx Effect
	switch ev.Type {
	case "hello":
		_ = ev.Decode(&s.Hello)
		fx.Inbox, fx.Assistant = true, true
	case "machines":
		var m struct {
			Machines []hubapi.Machine `json:"machines"`
		}
		if ev.Decode(&m) == nil {
			s.SetMachines(m.Machines)
		}
	case "sessions":
		var m struct {
			MachineID string           `json:"machine_id"`
			Sessions  []hubapi.Session `json:"sessions"`
		}
		if ev.Decode(&m) == nil && m.MachineID != "" {
			s.SetSessions(m.MachineID, m.Sessions)
			fx.Assistant = s.hasAssistant(m.Sessions)
		}
	case "revs":
		var m struct {
			MachineID string           `json:"machine_id"`
			Revs      map[string]int64 `json:"revs"`
		}
		if ev.Decode(&m) == nil {
			list := s.Sessions[m.MachineID]
			for i := range list {
				if r, ok := m.Revs[list[i].SessionID]; ok {
					list[i].Rev = r
				}
			}
		}
	case "items":
		var m struct {
			MachineID string        `json:"machine_id"`
			SessionID string        `json:"session_id"`
			Items     []hubapi.Item `json:"items"`
		}
		if ev.Decode(&m) == nil {
			t := s.Transcript(m.MachineID + "/" + m.SessionID)
			t.Merge(m.Items)
			t.Loaded = true
			if len(m.Items) > 0 {
				t.Live = ""
			}
		}
	case "delta":
		var m struct {
			MachineID string `json:"machine_id"`
			SessionID string `json:"session_id"`
			Text      string `json:"text"`
		}
		if ev.Decode(&m) == nil {
			s.Transcript(m.MachineID + "/" + m.SessionID).Live = m.Text
		}
	case "interaction":
		var m struct {
			Interaction hubapi.Interaction `json:"interaction"`
		}
		if ev.Decode(&m) == nil && m.Interaction.ID != "" {
			s.Interactions[m.Interaction.ID] = m.Interaction
		}
	case "interaction_resolved":
		var m struct {
			ID string `json:"id"`
		}
		if ev.Decode(&m) == nil {
			delete(s.Interactions, m.ID)
		}
	case "notify":
		var n Notify
		if ev.Decode(&n) == nil {
			fx.Notify = &n
		}
	case "inbox":
		fx.Inbox = true
	}
	return fx
}

func (s *Store) hasAssistant(list []hubapi.Session) bool {
	for _, x := range list {
		if x.Assistant {
			return true
		}
	}
	return false
}

// SetMachines replaces the computers, keeping their order by name.
func (s *Store) SetMachines(list []hubapi.Machine) {
	list = slices.Clone(list)
	slices.SortStableFunc(list, func(a, b hubapi.Machine) int {
		return cmp.Compare(strings.ToLower(machineName(a)), strings.ToLower(machineName(b)))
	})
	s.Machines = list
	keep := map[string]bool{}
	for _, m := range list {
		keep[m.ID] = true
	}
	for id := range s.Sessions {
		if !keep[id] {
			delete(s.Sessions, id)
		}
	}
}

func machineName(m hubapi.Machine) string {
	if m.Name != "" {
		return m.Name
	}
	return m.ID
}

// MachineName is a computer's name, or its id.
func (s *Store) MachineName(id string) string {
	for _, m := range s.Machines {
		if m.ID == id {
			return machineName(m)
		}
	}
	return id
}

// SetSessions replaces a computer's agents.
func (s *Store) SetSessions(machine string, list []hubapi.Session) {
	mine := make([]hubapi.Session, 0, len(list))
	for _, x := range list {
		if x.MachineID == "" {
			x.MachineID = machine
		}
		if x.MachineID == machine {
			mine = append(mine, x)
		}
	}
	s.Sessions[machine] = mine
}

// SetInteractions replaces the open cards.
func (s *Store) SetInteractions(list []hubapi.Interaction) {
	s.Interactions = map[string]hubapi.Interaction{}
	for _, ix := range list {
		s.Interactions[ix.ID] = ix
	}
}

// SetInbox replaces the Inbox.
func (s *Store) SetInbox(list []hubapi.InboxItem) {
	s.Inbox = list
	s.InboxLoaded = true
}

// MarkInbox marks an entry handled or not until the next read.
func (s *Store) MarkInbox(id string, done bool, at string) {
	for i := range s.Inbox {
		if s.Inbox[i].ID == id {
			if done {
				s.Inbox[i].DoneAt = at
			} else {
				s.Inbox[i].DoneAt = ""
			}
		}
	}
}

// Session finds an agent by key.
func (s *Store) Session(key string) (hubapi.Session, bool) {
	machine, id, ok := strings.Cut(key, "/")
	if !ok {
		return hubapi.Session{}, false
	}
	for _, x := range s.Sessions[machine] {
		if x.SessionID == id {
			return x, true
		}
	}
	for _, x := range s.Assistant.Conversations {
		if x.MachineID == "" {
			x.MachineID = s.Assistant.MachineID
		}
		if x.MachineID == machine && x.SessionID == id {
			return x, true
		}
	}
	return hubapi.Session{}, false
}

// Transcript is a session's transcript, made empty when new.
func (s *Store) Transcript(key string) *Transcript {
	t := s.Transcripts[key]
	if t == nil {
		t = &Transcript{}
		s.Transcripts[key] = t
	}
	return t
}

// Merge takes items into the transcript: the same id replaces, and the list stays in seq order.
func (t *Transcript) Merge(items []hubapi.Item) {
	if len(items) == 0 {
		return
	}
	at := make(map[string]int, len(t.Items))
	for i, it := range t.Items {
		at[it.ID] = i
	}
	for _, it := range items {
		if i, ok := at[it.ID]; ok && it.ID != "" {
			t.Items[i] = it
			continue
		}
		// the hub's copy of a message sent from here replaces the one shown while sending
		if it.ClientMsg != "" {
			if i := slices.IndexFunc(t.Items, func(x hubapi.Item) bool { return x.ClientMsg == it.ClientMsg && x.Seq < 0 }); i >= 0 {
				t.Items[i] = it
				continue
			}
		}
		at[it.ID] = len(t.Items)
		t.Items = append(t.Items, it)
	}
	slices.SortStableFunc(t.Items, func(a, b hubapi.Item) int {
		// items being sent (negative seq) stay last
		if (a.Seq < 0) != (b.Seq < 0) {
			if a.Seq < 0 {
				return 1
			}
			return -1
		}
		return cmp.Compare(a.Seq, b.Seq)
	})
}

// Pending adds a message being sent, shown until the hub's copy arrives.
func (t *Transcript) Pending(text, clientMsg string) {
	t.Items = append(t.Items, hubapi.Item{ID: "local-" + clientMsg, Seq: -1, Role: "user", Kind: "text", Text: text, ClientMsg: clientMsg})
}

// Drop removes a message being sent that failed.
func (t *Transcript) Drop(clientMsg string) {
	t.Items = slices.DeleteFunc(t.Items, func(x hubapi.Item) bool { return x.ClientMsg == clientMsg && x.Seq < 0 })
}

// Oldest is the smallest seq read, for reading earlier turns.
func (t *Transcript) Oldest() int64 {
	for _, it := range t.Items {
		if it.Seq > 0 {
			return it.Seq
		}
	}
	return 0
}

// SessionInteractions are the open cards of an agent, oldest first.
func (s *Store) SessionInteractions(key string) []hubapi.Interaction {
	var out []hubapi.Interaction
	for _, ix := range s.Interactions {
		if ix.SessionKey() == key && !ix.Delegating {
			out = append(out, ix)
		}
	}
	slices.SortFunc(out, func(a, b hubapi.Interaction) int {
		return cmp.Or(cmp.Compare(a.CreatedAt, b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	return out
}

// Group is a heading of the agents list and its agents.
type Group struct {
	State  string
	Agents []hubapi.Session
}

var stateOrder = []string{hubapi.StateNeedsYou, hubapi.StateWorking, hubapi.StateUnread, hubapi.StateIdle, hubapi.StateOffline}

// StateOf is an agent's state, from its open cards first.
func (s *Store) StateOf(x hubapi.Session) string {
	if x.Pending > 0 || len(s.SessionInteractions(x.Key())) > 0 {
		return hubapi.StateNeedsYou
	}
	if x.State == "" {
		return hubapi.StateIdle
	}
	return x.State
}

// Agents are the agents of every computer (not the assistant's conversations, not archived ones), grouped by
// state in the order the other clients use, most recently changed first in each group. A query filters by title,
// folder and computer.
func (s *Store) Agents(query string) []Group {
	query = strings.ToLower(strings.TrimSpace(query))
	by := map[string][]hubapi.Session{}
	for machine, list := range s.Sessions {
		name := strings.ToLower(s.MachineName(machine))
		for _, x := range list {
			if x.Assistant || x.Archived || x.IsSubagent {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(x.Title), query) &&
				!strings.Contains(strings.ToLower(x.Cwd), query) && !strings.Contains(name, query) {
				continue
			}
			st := s.StateOf(x)
			by[st] = append(by[st], x)
		}
	}
	var out []Group
	for _, st := range stateOrder {
		list := by[st]
		if len(list) == 0 {
			continue
		}
		slices.SortStableFunc(list, func(a, b hubapi.Session) int {
			return cmp.Or(cmp.Compare(b.UpdatedAt, a.UpdatedAt), cmp.Compare(a.Key(), b.Key()))
		})
		out = append(out, Group{State: st, Agents: list})
	}
	return out
}

// NeedsYou counts the agents waiting on the user: the number on the Dock / taskbar and the tray.
func (s *Store) NeedsYou() int {
	n := 0
	for _, g := range s.Agents("") {
		if g.State == hubapi.StateNeedsYou {
			n += len(g.Agents)
		}
	}
	return n
}

// InboxOpen counts the Inbox entries not handled yet.
func (s *Store) InboxOpen() int {
	n := 0
	for _, it := range s.Inbox {
		if !it.Done() {
			n++
		}
	}
	return n
}

// MainConversation is the assistant's main conversation (the Persona tab), if there is one.
func (s *Store) MainConversation() (hubapi.Session, bool) {
	a := s.Assistant
	for _, x := range a.Conversations {
		if x.SessionID == a.Main {
			if x.MachineID == "" {
				x.MachineID = a.MachineID
			}
			return x, true
		}
	}
	if a.Main != "" && a.MachineID != "" {
		return hubapi.Session{MachineID: a.MachineID, SessionID: a.Main, Assistant: true}, true
	}
	return hubapi.Session{}, false
}
