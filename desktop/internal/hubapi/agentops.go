package hubapi

import (
	"context"
	"net/http"
	"net/url"
)

// Scheduled tasks, agent actions and pins: the Overview, Scheduled and agent menus (docs/protocol/protocol.md).

// Task is one of the assistant's scheduled tasks (GET /assistant/tasks).
type Task struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Prompt       string `json:"prompt"`
	Repeat       string `json:"repeat"` // once | daily | interval
	At           string `json:"at,omitempty"`
	Time         string `json:"time,omitempty"` // HH:MM, daily
	Weekdays     []int  `json:"weekdays,omitempty"`
	EveryMinutes int    `json:"every_minutes,omitempty"`
	Enabled      bool   `json:"enabled"`
	SessionID    string `json:"session_id,omitempty"`
	Until        string `json:"until,omitempty"`
	LastRun      string `json:"last_run,omitempty"`
	LastStatus   string `json:"last_status,omitempty"`
	LastError    string `json:"last_error,omitempty"`
	NextRun      string `json:"next_run,omitempty"`
	RunMachineID string `json:"run_machine_id,omitempty"`
	RunSessionID string `json:"run_session_id,omitempty"`
}

// Run is a scheduled task's run (GET /assistant/runs).
type Run struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	RefID     string `json:"ref_id"`
	Title     string `json:"title"`
	At        string `json:"at"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	Manual    bool   `json:"manual,omitempty"`
	MachineID string `json:"machine_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

func (c *Client) Tasks(ctx context.Context) ([]Task, error) {
	var out struct {
		Tasks []Task `json:"tasks"`
	}
	err := c.Do(ctx, http.MethodGet, "/assistant/tasks", nil, &out)
	return out.Tasks, err
}

func (c *Client) Runs(ctx context.Context) ([]Run, error) {
	var out struct {
		Runs []Run `json:"runs"`
	}
	err := c.Do(ctx, http.MethodGet, "/assistant/runs", nil, &out)
	return out.Runs, err
}

// SaveTask creates a task (no id) or saves the fields people edit.
func (c *Client) SaveTask(ctx context.Context, t Task) (Task, error) {
	body := map[string]any{"title": t.Title, "prompt": t.Prompt, "repeat": t.Repeat, "enabled": t.Enabled,
		"time": "", "weekdays": []int{}, "every_minutes": 0}
	switch t.Repeat {
	case "daily":
		body["time"], body["weekdays"] = t.Time, t.Weekdays
		if t.Weekdays == nil {
			body["weekdays"] = []int{}
		}
	case "interval":
		body["every_minutes"] = t.EveryMinutes
	}
	if t.Repeat != "daily" && t.At != "" {
		body["at"] = t.At
	}
	var out struct {
		Task Task `json:"task"`
	}
	method, path := http.MethodPost, "/assistant/tasks"
	if t.ID != "" {
		method, path = http.MethodPut, "/assistant/tasks/"+url.PathEscape(t.ID)
	}
	err := c.Do(ctx, method, path, body, &out)
	return out.Task, err
}

func (c *Client) DeleteTask(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodDelete, "/assistant/tasks/"+url.PathEscape(id), nil, nil)
}

// RunTask runs a task now and says where it runs.
func (c *Client) RunTask(ctx context.Context, id string) (machine, session string, err error) {
	var out struct {
		MachineID string `json:"machine_id"`
		SessionID string `json:"session_id"`
	}
	err = c.Do(ctx, http.MethodPost, "/assistant/tasks/"+url.PathEscape(id)+"/run", map[string]any{}, &out)
	return out.MachineID, out.SessionID, err
}

// Rename gives an agent a title.
func (c *Client) Rename(ctx context.Context, machine, session, title string) error {
	return c.Do(ctx, http.MethodPost, sessionPath(machine, session)+"/rename", map[string]string{"title": title}, nil)
}

// CloseSession quits the agent and closes its tab.
func (c *Client) CloseSession(ctx context.Context, machine, session string) error {
	return c.Do(ctx, http.MethodPost, sessionPath(machine, session)+"/close", map[string]any{}, nil)
}

// DeleteSession takes a chat out of every list; its transcript stays on the computer.
func (c *Client) DeleteSession(ctx context.Context, machine, session string) error {
	return c.Do(ctx, http.MethodDelete, sessionPath(machine, session), nil, nil)
}

// SessionInfo is what Session info shows (GET …/info).
type SessionInfo struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	Cwd            string `json:"cwd,omitempty"`
	Model          string `json:"model,omitempty"`
	Launcher       string `json:"launcher,omitempty"`
	GitBranch      string `json:"git_branch,omitempty"`
	Version        string `json:"version,omitempty"`
	PermissionMode string `json:"permission_mode,omitempty"`
	Effort         string `json:"effort,omitempty"`
	ContextTokens  int    `json:"context_tokens,omitempty"`
	OutputTokens   int    `json:"output_tokens,omitempty"`
	Compactions    int    `json:"compactions,omitempty"`
	Turns          int    `json:"turns,omitempty"`
	StartedAt      string `json:"started_at,omitempty"`
	LastActivity   string `json:"last_activity,omitempty"`
}

func (c *Client) SessionInfo(ctx context.Context, machine, session string) (SessionInfo, error) {
	var out SessionInfo
	err := c.Do(ctx, http.MethodGet, sessionPath(machine, session)+"/info", nil, &out)
	return out, err
}

// Pins are the pinned agents ("machine/session id") and spaces, kept on the hub.
type Pins struct {
	Spaces   []string `json:"spaces"`
	Sessions []string `json:"sessions"`
}

func (c *Client) Pins(ctx context.Context) (Pins, error) {
	var out Pins
	err := c.Do(ctx, http.MethodGet, "/pins", nil, &out)
	return out, err
}

func (c *Client) SetPins(ctx context.Context, p Pins) error {
	if p.Spaces == nil {
		p.Spaces = []string{}
	}
	if p.Sessions == nil {
		p.Sessions = []string{}
	}
	return c.Do(ctx, http.MethodPut, "/pins", p, nil)
}
