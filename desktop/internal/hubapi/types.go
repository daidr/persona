// Package hubapi talks to a Persona hub the way the other clients do (docs/protocol/protocol.md): REST under
// /api/v1 with a device token, and the WebSocket /api/v1/ws for pushes. It holds the wire types, only the fields
// the desktop app reads, and has no MyGo dependency.
package hubapi

import (
	"bytes"
	"cmp"
	"encoding/json"
	"time"
)

// Agent states (docs/protocol/agent-states.md).
const (
	StateNeedsYou = "needs_you"
	StateWorking  = "working"
	StateUnread   = "unread"
	StateIdle     = "idle"
	StateOffline  = "offline"
)

// Machine is a computer of the hub.
type Machine struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Online     bool   `json:"online"`
	Error      string `json:"error,omitempty"`
	Hostname   string `json:"hostname,omitempty"`
	OS         string `json:"os,omitempty"`
	Arch       string `json:"arch,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Engine     string `json:"engine,omitempty"`
	NodeVer    string `json:"node_version,omitempty"`
	ClaudeVer  string `json:"claude_version,omitempty"`
	HerdrVer   string `json:"herdr_version,omitempty"`
	LastSeen   string `json:"last_seen,omitempty"`
	HubName    string `json:"hub_name,omitempty"`
	Trust      string `json:"trust,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	NodeBehind bool   `json:"node_mismatch,omitempty"`
	System     *struct {
		Name   string `json:"name,omitempty"`
		Model  string `json:"model,omitempty"`
		CPU    string `json:"cpu,omitempty"`
		Cores  int    `json:"cores,omitempty"`
		Memory int64  `json:"memory,omitempty"`
	} `json:"system,omitempty"`
}

// Capabilities say what a session of another source allows (a session without source_kind allows everything).
type Capabilities struct {
	Read      bool `json:"read,omitempty"`
	Send      bool `json:"send,omitempty"`
	Interrupt bool `json:"interrupt,omitempty"`
	Approve   bool `json:"approve,omitempty"`
	Terminal  bool `json:"terminal,omitempty"`
}

// Session is an agent: a conversation of Claude Code, Codex or another runtime on one of the computers.
type Session struct {
	MachineID string `json:"machine_id"`
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	Cwd       string `json:"cwd"`
	// RepoRoot, Worktree and Branch place the agent in its project: the main checkout, the git working tree and
	// the branch checked out there ("" when detached or not in git), as the node reads them.
	RepoRoot       string        `json:"repo_root,omitempty"`
	Worktree       string        `json:"worktree,omitempty"`
	Branch         string        `json:"branch,omitempty"`
	Status         string        `json:"status"`
	State          string        `json:"state,omitempty"`
	StateSince     string        `json:"state_since,omitempty"`
	Agent          string        `json:"agent,omitempty"`
	AgentLabel     string        `json:"agent_label,omitempty"`
	SourceKind     string        `json:"source_kind,omitempty"`
	ReadOnly       string        `json:"read_only_reason,omitempty"`
	Capabilities   *Capabilities `json:"capabilities,omitempty"`
	Model          string        `json:"model,omitempty"`
	PermissionMode string        `json:"permission_mode,omitempty"`
	Effort         string        `json:"effort,omitempty"`
	Launcher       string        `json:"launcher,omitempty"`
	Ultracode      bool          `json:"ultracode,omitempty"`
	Assistant      bool          `json:"assistant,omitempty"`
	Space          string        `json:"space,omitempty"`
	SpaceName      string        `json:"space_name,omitempty"`
	SlotName       string        `json:"slot_name,omitempty"`
	UpdatedAt      string        `json:"updated_at,omitempty"`
	SeenAt         string        `json:"seen_at,omitempty"`
	LastText       string        `json:"last_text,omitempty"`
	Asks           string        `json:"asks,omitempty"`
	Activity       string        `json:"activity,omitempty"`
	TurnError      string        `json:"turn_error,omitempty"`
	Pending        int           `json:"pending_interactions,omitempty"`
	Queued         []string      `json:"queued,omitempty"`
	Rev            int64         `json:"rev,omitempty"`
	Suggestion     string        `json:"suggestion,omitempty"`
	Archived       bool          `json:"archived,omitempty"`
	Quiet          bool          `json:"quiet,omitempty"`
	Muted          bool          `json:"muted,omitempty"`
	IsSubagent     bool          `json:"is_subagent,omitempty"`
	ContextTokens  int           `json:"context_tokens,omitempty"`
	ContextWindow  int           `json:"context_window,omitempty"`
	Todo           *struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"todo,omitempty"`
}

// Key identifies a session across machines.
func (s Session) Key() string { return s.MachineID + "/" + s.SessionID }

// Can tells whether the session allows an operation: "send", "interrupt" or "approve".
func (s Session) Can(op string) bool {
	if s.SourceKind == "" {
		return true
	}
	if s.Capabilities == nil {
		return false
	}
	switch op {
	case "send":
		return s.Capabilities.Send
	case "interrupt":
		return s.Capabilities.Interrupt
	case "approve":
		return s.Capabilities.Approve
	case "terminal":
		return s.Capabilities.Terminal
	}
	return s.Capabilities.Read
}

// Updated is when the session last changed, or the zero time.
func (s Session) Updated() time.Time { return parseTime(s.UpdatedAt) }

// Item is one entry of a session's transcript.
type Item struct {
	ID        string         `json:"id"`
	Seq       int64          `json:"seq"`
	TS        string         `json:"ts,omitempty"`
	Role      string         `json:"role"`
	Kind      string         `json:"kind"`
	Text      string         `json:"text,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	ToolName  string         `json:"tool_name,omitempty"`
	ToolInput map[string]any `json:"tool_input,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
	FullSize  int            `json:"full_size,omitempty"`
	Queued    bool           `json:"queued,omitempty"`
	From      string         `json:"from,omitempty"`
	ClientMsg string         `json:"client_msg_id,omitempty"`
	Image     *Image         `json:"image,omitempty"`
	Fold      *struct {
		Items    int   `json:"items"`
		Tools    int   `json:"tools"`
		Thoughts int   `json:"thoughts"`
		Through  int64 `json:"through_seq"`
	} `json:"fold,omitempty"`
	Injected *Injected `json:"injected,omitempty"`
	// Source names who wrote a user-role message that isn't the user's (MessageSource).
	Source *Source `json:"source,omitempty"`
}

// ItemsPage is a page of a transcript.
type ItemsPage struct {
	Items   []Item `json:"items"`
	HasMore bool   `json:"has_more,omitempty"`
}

// Question is one question of a question card.
type Question struct {
	Question    string `json:"question"`
	Header      string `json:"header,omitempty"`
	MultiSelect bool   `json:"multi_select,omitempty"`
	Options     []struct {
		Label       string `json:"label"`
		Description string `json:"description,omitempty"`
	} `json:"options,omitempty"`
}

// DialogOption is a choice of a terminal dialog the hub read off the screen.
type DialogOption struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	Kind   string `json:"kind,omitempty"`
}

// Interaction is a card an agent waits on: a question, a plan, a permission, a terminal dialog or an MCP form.
type Interaction struct {
	ID           string         `json:"id"`
	MachineID    string         `json:"machine_id"`
	SessionID    string         `json:"session_id"`
	SessionTitle string         `json:"session_title,omitempty"`
	Cwd          string         `json:"cwd,omitempty"`
	Kind         string         `json:"kind"`
	CreatedAt    string         `json:"created_at,omitempty"`
	ToolName     string         `json:"tool_name,omitempty"`
	Questions    []Question     `json:"questions,omitempty"`
	Plan         string         `json:"plan,omitempty"`
	ToolInput    map[string]any `json:"tool_input,omitempty"`
	Summary      string         `json:"summary,omitempty"`
	Screen       string         `json:"screen,omitempty"`
	Secret       bool           `json:"secret,omitempty"`
	Reason       string         `json:"reason,omitempty"`
	AlwaysLabel  string         `json:"always_label,omitempty"`
	Agent        string         `json:"agent,omitempty"`
	Delegating   bool           `json:"delegating,omitempty"`
	Dialog       *struct {
		Header   string         `json:"header,omitempty"`
		Question string         `json:"question,omitempty"`
		Detail   string         `json:"detail,omitempty"`
		Options  []DialogOption `json:"options,omitempty"`
		Multi    bool           `json:"multi,omitempty"`
	} `json:"dialog,omitempty"`
	Fields []struct {
		Key    string `json:"key"`
		Label  string `json:"label"`
		Secret bool   `json:"secret,omitempty"`
	} `json:"fields,omitempty"`
	Elicitation   *Elicitation `json:"elicitation,omitempty"`
	MCPServerName string       `json:"mcp_server_name,omitempty"`
}

// Elicitation is an MCP server asking the user: a form to fill in, or a link to open (docs/protocol/protocol.md "MCP elicitation").
type Elicitation struct {
	Mode    string      `json:"mode"`
	Message string      `json:"message,omitempty"`
	URL     string      `json:"url,omitempty"`
	Schema  *FormSchema `json:"requested_schema,omitempty"`
}

// FormSchema is a form's flat JSON Schema: its fields in the order the server wrote them.
type FormSchema struct {
	Fields   []FormField
	Required []string
}

// FormField is one field of a form.
type FormField struct {
	Name        string
	Type        string   `json:"type"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	EnumNames   []string `json:"enumNames,omitempty"`
	OneOf       []struct {
		Const string `json:"const"`
		Title string `json:"title,omitempty"`
	} `json:"oneOf,omitempty"`
	Items *struct {
		Enum  []string `json:"enum,omitempty"`
		AnyOf []struct {
			Const string `json:"const"`
			Title string `json:"title,omitempty"`
		} `json:"anyOf,omitempty"`
	} `json:"items,omitempty"`
	Default any `json:"default,omitempty"`
}

// Choices are a field's values and what to call them, when it has a fixed set.
func (f FormField) Choices() (values, labels []string) {
	switch {
	case len(f.OneOf) > 0:
		for _, o := range f.OneOf {
			values, labels = append(values, o.Const), append(labels, cmp.Or(o.Title, o.Const))
		}
	case len(f.Enum) > 0:
		for i, v := range f.Enum {
			l := v
			if i < len(f.EnumNames) && f.EnumNames[i] != "" {
				l = f.EnumNames[i]
			}
			values, labels = append(values, v), append(labels, l)
		}
	case f.Items != nil && len(f.Items.AnyOf) > 0:
		for _, o := range f.Items.AnyOf {
			values, labels = append(values, o.Const), append(labels, cmp.Or(o.Title, o.Const))
		}
	case f.Items != nil:
		values, labels = f.Items.Enum, f.Items.Enum
	}
	return values, labels
}

// UnmarshalJSON reads the schema keeping its properties' order.
func (s *FormSchema) UnmarshalJSON(b []byte) error {
	var raw struct {
		Properties json.RawMessage `json:"properties"`
		Required   []string        `json:"required"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	s.Required = raw.Required
	if len(raw.Properties) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw.Properties))
	if _, err := dec.Token(); err != nil {
		return err
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		name, _ := tok.(string)
		var f FormField
		if err := dec.Decode(&f); err != nil {
			return err
		}
		f.Name = name
		s.Fields = append(s.Fields, f)
	}
	return nil
}

// SessionKey is the key of the interaction's session.
func (ix Interaction) SessionKey() string { return ix.MachineID + "/" + ix.SessionID }

// Resolution answers a card (POST /interactions/{id}/resolve; server/internal/proto/interaction.go).
type Resolution struct {
	Allow    *bool             `json:"allow,omitempty"`
	Always   bool              `json:"always,omitempty"`
	Approve  *bool             `json:"approve,omitempty"`
	Mode     string            `json:"mode,omitempty"`
	Feedback string            `json:"feedback,omitempty"`
	Answers  map[string]string `json:"answers,omitempty"`
	Option   string            `json:"option,omitempty"`
	Text     string            `json:"text,omitempty"`
	Message  string            `json:"message,omitempty"`
	Values   map[string]string `json:"values,omitempty"`
	Action   string            `json:"action,omitempty"`
	Content  map[string]any    `json:"content,omitempty"`
	Dismiss  bool              `json:"dismiss,omitempty"`
}

// Assistant is the assistant's setup and its conversations (GET /api/v1/assistant).
type Assistant struct {
	MachineID     string    `json:"machine_id"`
	Main          string    `json:"main,omitempty"`
	Conversations []Session `json:"conversations"`
}

// InboxItem is an entry of the Inbox (docs/protocol/protocol.md "Inbox API").
type InboxItem struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Kind        string `json:"kind"`
	AccountName string `json:"account_name,omitempty"`
	Title       string `json:"title"`
	Sender      string `json:"sender,omitempty"`
	Preview     string `json:"preview,omitempty"`
	Count       int    `json:"count,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	Tier        string `json:"tier,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Read        bool   `json:"read,omitempty"`
	DoneAt      string `json:"done_at,omitempty"`
	DoneBy      string `json:"done_by,omitempty"`
	Version     string `json:"version,omitempty"`
	Connector   *struct {
		Label    string `json:"label,omitempty"`
		URL      string `json:"open_url,omitempty"`
		CanReply bool   `json:"can_reply,omitempty"`
	} `json:"connector,omitempty"`
	// Agent is the AI twin's work on the entry: its state and the reply it drafted.
	Agent *struct {
		State string `json:"state,omitempty"`
		Draft string `json:"draft,omitempty"`
		Note  string `json:"note,omitempty"`
	} `json:"agent,omitempty"`
}

// CanReply tells an entry Persona can answer: a chat or a mail, or a connector's that says so.
func (it InboxItem) CanReply() bool { return it.Connector == nil || it.Connector.CanReply }

// Drafted is the reply the AI twin wrote, if it did.
func (it InboxItem) Drafted() string {
	if it.Agent != nil && it.Agent.State == "drafted" {
		return it.Agent.Draft
	}
	return ""
}

// Done tells a handled entry.
func (it InboxItem) Done() bool { return it.DoneAt != "" }

// Hello is the hub's first WebSocket message.
type Hello struct {
	ServerVersion string   `json:"server_version"`
	HubID         string   `json:"hub_id"`
	HubName       string   `json:"hub_name"`
	Features      []string `json:"features"`
	FP            string   `json:"fp,omitempty"`
	TLSPort       int      `json:"tls_port,omitempty"`
	Locale        string   `json:"locale,omitempty"`
}

// Event is one WebSocket message from the hub: its type, and the whole message to decode by type.
type Event struct {
	Type string
	Raw  json.RawMessage
}

// Decode reads the event into v.
func (e Event) Decode(v any) error { return json.Unmarshal(e.Raw, v) }

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// ParseTime reads the hub's RFC 3339 times, or the zero time.
func ParseTime(s string) time.Time { return parseTime(s) }

// TodoAgent is an agent working on a to-do, with what the hub remembered of it.
type TodoAgent struct {
	MachineID  string `json:"machine_id"`
	SessionID  string `json:"session_id"`
	Title      string `json:"title,omitempty"`
	Cwd        string `json:"cwd,omitempty"`
	Agent      string `json:"agent,omitempty"`
	StartError string `json:"start_error,omitempty"`
	ReleasedAt string `json:"released_at,omitempty"`
}

// Key identifies the agent's session.
func (a TodoAgent) Key() string { return a.MachineID + "/" + a.SessionID }

// Todo is a to-do and the agents on it (docs/assistant/todos.md).
type Todo struct {
	ID        string      `json:"id"`
	Title     string      `json:"title"`
	Notes     string      `json:"notes,omitempty"`
	DueAt     string      `json:"due_at,omitempty"`
	DueDate   string      `json:"due_date,omitempty"`
	Stage     string      `json:"stage,omitempty"`
	PRURL     string      `json:"pr_url,omitempty"`
	DoneAt    string      `json:"done_at,omitempty"`
	DoneBy    string      `json:"done_by,omitempty"`
	CreatedAt string      `json:"created_at,omitempty"`
	UpdatedAt string      `json:"updated_at,omitempty"`
	Agents    []TodoAgent `json:"agents,omitempty"`
	Ask       *struct {
		Verdict   string `json:"verdict,omitempty"`
		Recommend string `json:"recommend,omitempty"`
	} `json:"ask,omitempty"`
}

// Done tells a finished to-do.
func (t Todo) Done() bool { return t.DoneAt != "" }

// Launcher is a way to start an agent on a computer: a CLI with its settings (docs/integrations/launchers.md).
type Launcher struct {
	Name      string `json:"name"`
	Label     string `json:"label,omitempty"`
	Model     string `json:"model,omitempty"`
	Agent     string `json:"agent,omitempty"`
	AgentType string `json:"agent_type,omitempty"`
}

// Launchers are a computer's launchers and the one new agents use by default.
type Launchers struct {
	Launchers []Launcher `json:"launchers"`
	// Tiers are New agent's presets: the tiers of the model profile in use whose launcher this computer starts.
	Tiers        []Tier `json:"tiers,omitempty"`
	TiersProfile *struct {
		Name string `json:"name"`
	} `json:"tiers_profile,omitempty"`
	Default *struct {
		Provider string `json:"provider,omitempty"`
		Model    string `json:"model,omitempty"`
	} `json:"default,omitempty"`
}

// Tier is a preset for starting an agent: a launcher with its model and effort ("" for their defaults), and when to use it.
type Tier struct {
	Name        string `json:"name"`
	Launcher    string `json:"launcher"`
	Model       string `json:"model,omitempty"`
	Effort      string `json:"effort,omitempty"`
	Description string `json:"description,omitempty"`
}

// Dir is a folder on a computer.
type Dir struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsGit bool   `json:"is_git,omitempty"`
}

// Dirs is a folder's subfolders and the folders agents started in lately.
type Dirs struct {
	Path    string `json:"path"`
	Parent  string `json:"parent,omitempty"`
	Entries []Dir  `json:"entries"`
	Recent  []Dir  `json:"recent,omitempty"`
}

// StartRequest starts an agent (POST /api/v1/sessions).
type StartRequest struct {
	MachineID string `json:"machine_id"`
	Cwd       string `json:"cwd"`
	Launcher  string `json:"launcher,omitempty"`
	AgentType string `json:"agent_type,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
	TodoID    string `json:"todo_id,omitempty"`
	// Model and Effort override the model settings for this start only (a preset's).
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
	// PermissionMode is the agent's first permission mode; Ultracode starts Claude Code's ultracode on.
	PermissionMode string `json:"permission_mode,omitempty"`
	Ultracode      bool   `json:"ultracode,omitempty"`
}

// KBKinds are the knowledge base's kinds of entries, in the order the apps show them (docs/assistant/knowledge.md).
var KBKinds = []string{"projects", "chats", "people", "docs", "meetings", "events", "recordings"}

// KBEntry is a knowledge-base entry as lists show it.
type KBEntry struct {
	Kind       string   `json:"kind"`
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	OneLine    string   `json:"one_line,omitempty"`
	Importance string   `json:"importance,omitempty"`
	Relevance  int      `json:"relevance"`
	Tags       []string `json:"tags,omitempty"`
	Updated    string   `json:"updated,omitempty"`
	// Extra is a project's "short · active" (kind · status), a chat's dm / group.
	Extra string `json:"extra,omitempty"`
}

// Key identifies an entry: kind/id.
func (e KBEntry) Key() string { return e.Kind + "/" + e.ID }

// KBList is a kind's entries and the tags they use.
type KBList struct {
	Items []KBEntry `json:"items"`
	Tags  []string  `json:"tags,omitempty"`
}

// KBMoment is a feed post an entry links to.
type KBMoment struct {
	ID       string `json:"id"`
	Author   string `json:"author"`
	Text     string `json:"text"`
	At       string `json:"at,omitempty"`
	Comments int    `json:"comments"`
	Note     string `json:"note,omitempty"`
	Feed     string `json:"feed,omitempty"`
}

// KBDetail is one entry's page: its profile, timeline, recent messages and related entries.
type KBDetail struct {
	Kind    string         `json:"kind"`
	ID      string         `json:"id"`
	Path    string         `json:"path"`
	Meta    map[string]any `json:"meta,omitempty"`
	Summary string         `json:"summary,omitempty"`
	Log     []string       `json:"log,omitempty"`
	Recent  string         `json:"recent,omitempty"`
	Links   []KBEntry      `json:"links,omitempty"`
	Moments []KBMoment     `json:"moments,omitempty"`
}

// KBMatch is a place in an entry that matched the search it was opened from.
type KBMatch struct {
	Path   string   `json:"path"`
	N      int      `json:"n"`
	Line   string   `json:"line"`
	Day    string   `json:"day,omitempty"`
	Before []string `json:"before,omitempty"`
	After  []string `json:"after,omitempty"`
}

// KBPage is GET /kb/{kind}/{id}: the entry, the agents on a project, and the search's matches.
type KBPage struct {
	Detail  KBDetail  `json:"detail"`
	Agents  []Session `json:"agents,omitempty"`
	Matches []KBMatch `json:"matches,omitempty"`
}

// KBFound is a line a search found.
type KBFound struct {
	Path string `json:"path"`
	Line string `json:"line"`
	N    int    `json:"n,omitempty"`
}

// KBResult is an entry a search found, with how many lines matched and the first few.
type KBResult struct {
	Entry  KBEntry   `json:"entry"`
	Count  int       `json:"count"`
	Hits   []KBFound `json:"hits,omitempty"`
	InName bool      `json:"in_name,omitempty"`
}

// KBUpdate is PUT /kb/{kind}/{id}: tagging set by hand, kept over the automatic one.
type KBUpdate struct {
	Importance  string   `json:"importance,omitempty"`
	Relevance   int      `json:"relevance"`
	Tags        []string `json:"tags"`
	Status      string   `json:"status,omitempty"`
	ProjectKind string   `json:"project_kind,omitempty"`
}

// Screen is what an agent's terminal shows (GET /sessions/{mid}/{sid}/screen).
type Screen struct {
	Text   string `json:"text"`
	Status string `json:"status,omitempty"`
}

// GitFile is a changed file in a checkout.
type GitFile struct {
	Path     string `json:"path"`
	OrigPath string `json:"orig_path,omitempty"`
	// Index and Worktree are git's status letters ("M", "A", "?", "." for none).
	Index    string `json:"index"`
	Worktree string `json:"worktree"`
	Added    int    `json:"added"`
	Deleted  int    `json:"deleted"`
	Binary   bool   `json:"binary,omitempty"`
}

// GitCommit is a commit in a checkout's history.
type GitCommit struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
	Author  string `json:"author,omitempty"`
	Date    string `json:"date,omitempty"`
	Refs    string `json:"refs,omitempty"`
}

// GitStatus is a checkout's branch, changes and latest commits (GET /machines/{mid}/git/status).
type GitStatus struct {
	IsRepo   bool       `json:"is_repo"`
	Root     string     `json:"root,omitempty"`
	Branch   string     `json:"branch,omitempty"`
	Head     *GitCommit `json:"head,omitempty"`
	Upstream string     `json:"upstream,omitempty"`
	// Gone: the upstream is configured but its branch was deleted on the remote.
	Gone    bool        `json:"gone,omitempty"`
	Ahead   int         `json:"ahead"`
	Behind  int         `json:"behind"`
	Files   []GitFile   `json:"files"`
	Stashes int         `json:"stashes,omitempty"`
	Commits []GitCommit `json:"commits,omitempty"`
}

// GitDiff is one file's diff.
type GitDiff struct {
	Path      string `json:"path"`
	Diff      string `json:"diff"`
	Truncated bool   `json:"truncated,omitempty"`
}

// Image is a picture in a message: its type and its bytes in base64.
type Image struct {
	Mime string `json:"mime"`
	Data string `json:"data,omitempty"`
}

// Injected is a line the hub or the assistant put into a chat as the user's: a task's notice, a follow-up, supervision.
type Injected struct {
	Kind   string `json:"kind"`
	State  string `json:"state,omitempty"`
	Level  string `json:"level,omitempty"`
	Title  string `json:"title,omitempty"`
	Line   string `json:"line"`
	Detail string `json:"detail,omitempty"`
}
