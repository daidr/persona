package hubapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// The unified entry (docs/assistant/unified-entry.md): what the hub puts into an assistant conversation among its
// messages (plans, task cards, Today, briefs, receipts, answered cards, messages routed to tasks) and what they do.

// FeedItem is one thing the hub put into a conversation; Body's shape depends on Kind.
type FeedItem struct {
	ID           string         `json:"id"`
	Conversation string         `json:"conversation"`
	At           string         `json:"at"`
	Kind         string         `json:"kind"`
	Ref          string         `json:"ref,omitempty"`
	Body         map[string]any `json:"body,omitempty"`
}

// Str is a body field as text ("" when missing or not text).
func (it FeedItem) Str(key string) string {
	s, _ := it.Body[key].(string)
	return s
}

// List is a body field that is a list of objects.
func (it FeedItem) List(key string) []map[string]any {
	raw, _ := it.Body[key].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		if m, ok := r.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// Num is a body field as a number (0 when missing).
func (it FeedItem) Num(key string) int {
	f, _ := it.Body[key].(float64)
	return int(f)
}

// PlanTask is one task of a plan Persona proposed.
type PlanTask struct {
	Title      string `json:"title"`
	Prompt     string `json:"prompt"`
	Machine    string `json:"machine"`
	Cwd        string `json:"cwd"`
	Launcher   string `json:"launcher,omitempty"`
	Model      string `json:"model,omitempty"`
	Tier       string `json:"tier,omitempty"`
	Permission string `json:"permission,omitempty"`
	Ultracode  *bool  `json:"ultracode,omitempty"`
	Readonly   bool   `json:"readonly,omitempty"`
	Why        string `json:"why,omitempty"`
	TodoID     string `json:"todo_id,omitempty"`
	Error      string `json:"error,omitempty"`
}

// TaskPlan is a plan of tasks: open until started or dismissed.
type TaskPlan struct {
	ID           string     `json:"id"`
	Conversation string     `json:"conversation"`
	Tasks        []PlanTask `json:"tasks"`
	State        string     `json:"state"` // open | started | dismissed
	CreatedAt    string     `json:"created_at,omitempty"`
}

// Feed reads a conversation's items after since (all of them when since is empty), with the plans they name.
func (c *Client) Feed(ctx context.Context, conversation, since string) ([]FeedItem, map[string]TaskPlan, error) {
	var out struct {
		Items []FeedItem          `json:"items"`
		Plans map[string]TaskPlan `json:"plans"`
	}
	path := "/assistant/conversations/" + url.PathEscape(conversation) + "/feed"
	if since != "" {
		path += "?since=" + url.QueryEscape(since)
	}
	err := c.Do(ctx, http.MethodGet, path, nil, &out)
	return out.Items, out.Plans, err
}

// Today asks for the conversation's Today card (the hub makes it on the first ask of the day).
func (c *Client) Today(ctx context.Context, conversation string) (FeedItem, error) {
	var out struct {
		Item FeedItem `json:"item"`
	}
	err := c.Do(ctx, http.MethodPost, "/assistant/conversations/"+url.PathEscape(conversation)+"/today", map[string]any{}, &out)
	return out.Item, err
}

// StartPlan starts a plan's tasks as they are.
func (c *Client) StartPlan(ctx context.Context, id string) (TaskPlan, error) {
	var out struct {
		Plan TaskPlan `json:"plan"`
	}
	err := c.Do(ctx, http.MethodPost, "/task-plans/"+url.PathEscape(id)+"/start", map[string]any{"remember": false}, &out)
	return out.Plan, err
}

func (c *Client) DismissPlan(ctx context.Context, id string) (TaskPlan, error) {
	var out struct {
		Plan TaskPlan `json:"plan"`
	}
	err := c.Do(ctx, http.MethodPost, "/task-plans/"+url.PathEscape(id)+"/dismiss", map[string]any{}, &out)
	return out.Plan, err
}

// AcceptTodo accepts a task a brief asks to verify; askedAt is when the brief asked.
func (c *Client) AcceptTodo(ctx context.Context, id, askedAt string) error {
	return c.Do(ctx, http.MethodPost, "/todos/"+url.PathEscape(id)+"/accept", map[string]string{"asked_at": askedAt}, nil)
}

// LaterHandled marks a question that waited for a briefing handled.
func (c *Client) LaterHandled(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodPost, "/later/"+url.PathEscape(id)+"/handled", map[string]any{}, nil)
}

// ReadinessStep is one of what the main conversation needs (computer, claude, model, first message).
type ReadinessStep struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Code   string `json:"code,omitempty"`
	Title  string `json:"title,omitempty"`
	Detail string `json:"detail,omitempty"`
	Action *struct {
		ID string `json:"id"`
	} `json:"action,omitempty"`
}

// Readiness is what the main conversation still needs (GET /assistant/readiness).
type Readiness struct {
	Mode      string          `json:"mode"` // full | setup | chat_only
	Ready     bool            `json:"ready"`
	Next      *string         `json:"next"`
	Steps     []ReadinessStep `json:"steps"`
	Dismissed bool            `json:"dismissed"`
}

func (c *Client) Readiness(ctx context.Context) (Readiness, error) {
	var out Readiness
	err := c.Do(ctx, http.MethodGet, "/assistant/readiness", nil, &out)
	return out, err
}

// DecodeFeedItem reads an assistant_feed event's item.
func DecodeFeedItem(raw json.RawMessage) (FeedItem, bool) {
	var m struct {
		Item *FeedItem `json:"item"`
	}
	if json.Unmarshal(raw, &m) != nil || m.Item == nil {
		return FeedItem{}, false
	}
	return *m.Item, true
}

// ReadinessProbe asks again with a test message to the assistant (the "Test" action).
func (c *Client) ReadinessProbe(ctx context.Context) (Readiness, error) {
	var out Readiness
	err := c.Do(ctx, http.MethodGet, "/assistant/readiness?probe=1", nil, &out)
	return out, err
}
