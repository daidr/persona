package hubapi

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ── The health space (docs/assistant/health.md) ──

// HealthSpace is the space health conversations and memory carry (`?space=health`).
const HealthSpace = "health"

// HealthConversations lists the health space's conversations and the machine they run on (GET /assistant?space=health).
func (c *Client) HealthConversations(ctx context.Context) (machine string, list []Session, err error) {
	var out struct {
		MachineID     string    `json:"machine_id"`
		Conversations []Session `json:"conversations"`
	}
	err = c.Do(ctx, http.MethodGet, "/assistant?space="+HealthSpace, nil, &out)
	for i := range out.Conversations {
		if out.Conversations[i].MachineID == "" {
			out.Conversations[i].MachineID = out.MachineID
		}
	}
	return out.MachineID, out.Conversations, err
}

// StartHealthConversation starts a conversation in the health space.
func (c *Client) StartHealthConversation(ctx context.Context, prompt string) (Session, error) {
	var out struct {
		Session Session `json:"session"`
	}
	err := c.Do(ctx, http.MethodPost, "/assistant/conversations?space="+HealthSpace, map[string]any{"prompt": prompt, "space": HealthSpace}, &out)
	return out.Session, err
}

// SpaceMemory lists a space's own memory files.
func (c *Client) SpaceMemory(ctx context.Context, space string) ([]MemoryFile, error) {
	var out struct {
		Files []MemoryFile `json:"files"`
	}
	err := c.Do(ctx, http.MethodGet, "/assistant/memory?space="+url.QueryEscape(space), nil, &out)
	return out.Files, err
}

// DeleteSpaceMemory forgets one of a space's memories.
func (c *Client) DeleteSpaceMemory(ctx context.Context, space, name string) error {
	return c.Do(ctx, http.MethodDelete, "/assistant/memory/"+url.PathEscape(name)+"?space="+url.QueryEscape(space), nil, nil)
}

// HealthReport is one checkup report in the health space's reports/: an original, its record, or both.
type HealthReport struct {
	Name       string `json:"name"`
	Title      string `json:"title"`
	Path       string `json:"path,omitempty"`
	RecordPath string `json:"record_path,omitempty"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}

func reportsPath(machine string) string {
	return "/machines/" + url.PathEscape(machine) + "/health/reports"
}

// HealthReports lists the checkup reports on the space's machine.
func (c *Client) HealthReports(ctx context.Context, machine string) ([]HealthReport, error) {
	var out struct {
		Reports []HealthReport `json:"reports"`
	}
	err := c.Do(ctx, http.MethodGet, reportsPath(machine), nil, &out)
	return out.Reports, err
}

// DeleteHealthReport deletes a report: the original and its record.
func (c *Client) DeleteHealthReport(ctx context.Context, machine, name string) error {
	return c.Do(ctx, http.MethodDelete, reportsPath(machine)+"/"+url.PathEscape(name), nil, nil)
}

// UploadTo sends a file to a machine into a place the hub names (`dest`: "health" puts it in the health space's reports/).
func (c *Client) UploadTo(ctx context.Context, machine, name, dest string, body io.Reader, size int64) (string, error) {
	var out struct {
		Path string `json:"path"`
	}
	err := c.send(ctx, http.MethodPost, "/machines/"+url.PathEscape(machine)+"/files?name="+url.QueryEscape(name)+"&dest="+url.QueryEscape(dest),
		"application/octet-stream", body, size, &out)
	return out.Path, err
}

// ── Messages: Feishu chats (docs/extensions/lark-migration.md "Entry") ──

// FeatureLark is the hello feature of a hub that reads Feishu.
const FeatureLark = "lark"

// LarkConversation is one private or group chat.
type LarkConversation struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	IsGroup       bool   `json:"is_group"`
	IsTopic       bool   `json:"is_topic,omitempty"`
	Unread        int    `json:"unread"`
	MarkedUnread  bool   `json:"marked_unread,omitempty"`
	AtMe          bool   `json:"at_me"`
	AtAll         bool   `json:"at_all,omitempty"`
	Muted         bool   `json:"muted"`
	Pinned        bool   `json:"pinned,omitempty"`
	Preview       string `json:"preview"`
	PreviewSender string `json:"preview_sender,omitempty"`
	IsMine        bool   `json:"is_mine"`
	UpdatedAt     string `json:"updated_at"`
}

// LarkPart is a piece of a message: text, a mention, a link, a picture, a file and so on.
type LarkPart struct {
	Kind string `json:"kind"`
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

// LarkMessage is one message of a chat.
type LarkMessage struct {
	ID            string     `json:"id"`
	Position      string     `json:"position"`
	SenderName    string     `json:"sender_name,omitempty"`
	SenderType    string     `json:"sender_type,omitempty"`
	IsMine        bool       `json:"is_mine,omitempty"`
	Type          string     `json:"type,omitempty"`
	Text          string     `json:"text,omitempty"`
	CreatedAt     string     `json:"created_at,omitempty"`
	Recalled      bool       `json:"recalled,omitempty"`
	Edited        bool       `json:"edited,omitempty"`
	ReplyCount    int        `json:"reply_count,omitempty"`
	ThreadReplies int        `json:"thread_reply_count,omitempty"`
	IsRead        *bool      `json:"is_read,omitempty"`
	BadgeCount    *int       `json:"badge_count,omitempty"`
	Parts         []LarkPart `json:"parts,omitempty"`
	ReplyTo       *struct {
		SenderName string `json:"sender_name,omitempty"`
		Text       string `json:"text,omitempty"`
	} `json:"reply_to,omitempty"`
	ClientID string `json:"client_id,omitempty"`
	// Sending and Failed are this app's own: a message on its way, or one that didn't go.
	Sending bool `json:"-"`
	Failed  bool `json:"-"`
}

// System tells a line the chat itself wrote (someone joined, the name changed).
func (m LarkMessage) System() bool { return m.Type == "SYSTEM" }

// LarkPage is a window of a chat, oldest first.
type LarkPage struct {
	Messages    []LarkMessage `json:"messages"`
	HasOlder    bool          `json:"has_older,omitempty"`
	OlderCursor string        `json:"older_cursor,omitempty"`
	Topic       bool          `json:"topic,omitempty"`
}

// LarkChats reads a page of the chats (and, with an empty cursor, the pinned ones).
func (c *Client) LarkChats(ctx context.Context, cursor string) (chats []LarkConversation, next string, err error) {
	q := url.Values{"scope": {"chats"}, "limit": {"60"}, "account_bound": {"1"}}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var out struct {
		Conversations []LarkConversation `json:"conversations"`
		Next          string             `json:"next_cursor"`
	}
	err = c.Do(ctx, http.MethodGet, "/lark/conversations?"+q.Encode(), nil, &out)
	return out.Conversations, out.Next, err
}

// LarkPinned reads the pinned chats.
func (c *Client) LarkPinned(ctx context.Context) ([]LarkConversation, error) {
	var out struct {
		Conversations []LarkConversation `json:"conversations"`
	}
	err := c.Do(ctx, http.MethodGet, "/lark/pinned?account_bound=1", nil, &out)
	return out.Conversations, err
}

// LarkMessages reads a chat's newest page, or the page before `before`.
func (c *Client) LarkMessages(ctx context.Context, feed, before string) (LarkPage, error) {
	q := url.Values{"limit": {"40"}}
	if before != "" {
		q.Set("before", before)
	}
	var out LarkPage
	err := c.Do(ctx, http.MethodGet, "/lark/conversations/"+url.PathEscape(feed)+"/messages?"+q.Encode(), nil, &out)
	return out, err
}

// SendLark sends text to a chat; clientID matches the hub's copy to the one shown while it was on its way.
func (c *Client) SendLark(ctx context.Context, feed, text, clientID string) error {
	return c.Do(ctx, http.MethodPost, "/lark/conversations/"+url.PathEscape(feed)+"/messages", map[string]any{"text": text, "client_id": clientID}, nil)
}

// LarkRead reports what was read: up to the newest message shown, and which of the others' messages that covers.
func (c *Client) LarkRead(ctx context.Context, feed string, page LarkPage) error {
	if len(page.Messages) == 0 {
		return nil
	}
	top := page.Messages[len(page.Messages)-1]
	body := map[string]any{"position": top.Position}
	if top.BadgeCount != nil {
		body["badge_count"] = *top.BadgeCount
	}
	var ids []string
	for _, m := range page.Messages {
		if !m.IsMine && (m.IsRead == nil || !*m.IsRead) && !m.System() {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) > 0 {
		body["message_ids"] = ids
	}
	return c.Do(ctx, http.MethodPost, "/lark/conversations/"+url.PathEscape(feed)+"/read", body, nil)
}

// LarkText is a message as plain text: its text, else its parts in words a list can show.
func LarkText(m LarkMessage) string {
	if t := strings.TrimSpace(m.Text); t != "" {
		return t
	}
	var b strings.Builder
	for _, p := range m.Parts {
		switch {
		case p.Text != "":
			b.WriteString(p.Text)
		case p.Name != "":
			b.WriteString(p.Name)
		}
	}
	return strings.TrimSpace(b.String())
}

// OlderLark puts an older page before what is shown, one copy of each message.
func OlderLark(have, older LarkPage) LarkPage {
	ids := map[string]bool{}
	for _, m := range have.Messages {
		ids[m.ID] = true
	}
	var msgs []LarkMessage
	for _, m := range older.Messages {
		if !ids[m.ID] {
			msgs = append(msgs, m)
		}
	}
	return LarkPage{Messages: append(msgs, have.Messages...), HasOlder: older.HasOlder, OlderCursor: older.OlderCursor, Topic: have.Topic}
}

// ── The workflow board (docs/web/workspace-board.md) ──

// BoardColumn is a column of the board.
type BoardColumn struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// BoardCard is a workspace on the board: a folder on a machine, in a column.
type BoardCard struct {
	ID          string `json:"id"`
	MachineID   string `json:"machine_id"`
	WorkspaceID string `json:"workspace_id"`
	Title       string `json:"title"`
	Cwd         string `json:"cwd"`
	Source      string `json:"source"`
	Branch      string `json:"branch,omitempty"`
	ColumnID    string `json:"column_id"`
	Pinned      bool   `json:"pinned"`
	Archived    bool   `json:"archived"`
}

// Board is the board as the hub keeps it.
type Board struct {
	Revision int           `json:"revision"`
	Columns  []BoardColumn `json:"columns"`
	Cards    []BoardCard   `json:"cards"`
	CanWrite *bool         `json:"can_write,omitempty"`
}

// Workspace is a space an engine or a folder offers to put on the board.
type Workspace struct {
	ID        string `json:"id"`
	MachineID string `json:"machine_id"`
	Source    string `json:"source"`
	Title     string `json:"title"`
	Cwd       string `json:"cwd"`
	Branch    string `json:"branch,omitempty"`
	Online    *bool  `json:"online,omitempty"`
	Available *bool  `json:"available,omitempty"`
}

// WorkspaceList is what the computers offer, and which of them couldn't say.
type WorkspaceList struct {
	Workspaces []Workspace `json:"workspaces"`
	Errors     []struct {
		MachineID string `json:"machine_id"`
		Error     string `json:"error"`
	} `json:"errors,omitempty"`
}

// BoardSetting is whether the board is on, and whether this device may change that.
type BoardSetting struct {
	Enabled  bool `json:"enabled"`
	CanWrite bool `json:"can_write"`
}

// WorkspaceBoardSetting reads whether the board is on.
func (c *Client) WorkspaceBoardSetting(ctx context.Context) (BoardSetting, error) {
	var out BoardSetting
	err := c.Do(ctx, http.MethodGet, "/workspace-board/settings", nil, &out)
	return out, err
}

// SetWorkspaceBoard turns the board on or off (its contents stay).
func (c *Client) SetWorkspaceBoard(ctx context.Context, on bool) (BoardSetting, error) {
	var out BoardSetting
	err := c.Do(ctx, http.MethodPut, "/workspace-board/settings", map[string]bool{"enabled": on}, &out)
	return out, err
}

// WorkspaceBoard reads the board.
func (c *Client) WorkspaceBoard(ctx context.Context) (Board, error) {
	var out Board
	err := c.Do(ctx, http.MethodGet, "/workspace-board", nil, &out)
	return out, err
}

// Workspaces lists what can be put on the board.
func (c *Client) Workspaces(ctx context.Context) (WorkspaceList, error) {
	var out WorkspaceList
	err := c.Do(ctx, http.MethodGet, "/workspaces", nil, &out)
	return out, err
}

// ChangeBoard makes one change (add_card, update_card, move_card, add_column, update_column, move_column,
// delete_column) to the board at revision; the hub answers with the board after it (409: someone changed it first).
func (c *Client) ChangeBoard(ctx context.Context, revision int, change map[string]any) (Board, error) {
	body := map[string]any{"revision": revision}
	for k, v := range change {
		body[k] = v
	}
	var out Board
	err := c.Do(ctx, http.MethodPost, "/workspace-board", body, &out)
	return out, err
}

// ColumnCards are a column's cards (archived ones or not), pinned first, filtered by a query and a machine.
func ColumnCards(b Board, column, query, machine string, archived bool) []BoardCard {
	q := strings.ToLower(strings.TrimSpace(query))
	var pinned, rest []BoardCard
	for _, x := range b.Cards {
		if x.ColumnID != column || x.Archived != archived || (machine != "" && x.MachineID != machine) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(x.Title+" "+x.Cwd+" "+x.Branch+" "+x.MachineID), q) {
			continue
		}
		if x.Pinned {
			pinned = append(pinned, x)
		} else {
			rest = append(rest, x)
		}
	}
	return append(pinned, rest...)
}

// SameDirectory tells two folders apart the way the hub does: trailing slashes and, on Windows paths, case don't count.
func SameDirectory(a, b string) bool {
	norm := func(p string) string {
		p = strings.TrimSpace(p)
		if len(p) >= 2 && p[1] == ':' || strings.HasPrefix(p, `\\`) {
			p = strings.ToLower(strings.ReplaceAll(p, `\`, "/"))
		}
		if len(p) > 1 {
			p = strings.TrimRight(p, "/")
		}
		return p
	}
	return a != "" && norm(a) == norm(b)
}

// CardSessions are the agents working in a card's folder on its machine.
func CardSessions(card BoardCard, sessions []Session) []Session {
	var out []Session
	for _, s := range sessions {
		if s.MachineID == card.MachineID && SameDirectory(s.Cwd, card.Cwd) && !s.Assistant && !s.Archived {
			out = append(out, s)
		}
	}
	return out
}
