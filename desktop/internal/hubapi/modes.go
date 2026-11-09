package hubapi

import (
	"context"
	"net/http"
	"net/url"
)

// PermissionModes and Efforts are what an agent's mode chips offer (Claude Code's), in order.
var (
	PermissionModes = []string{"default", "acceptEdits", "plan", "auto", "bypassPermissions"}
	Efforts         = []string{"low", "medium", "high", "xhigh", "max"}
)

// SetModes changes a running agent's permission mode, effort, model or ultracode (POST …/modes); only the fields given.
func (c *Client) SetModes(ctx context.Context, machine, session string, patch map[string]any) error {
	return c.Do(ctx, http.MethodPost, sessionPath(machine, session)+"/modes", patch, nil)
}

// AgentModel is a model a launcher's agent can run.
type AgentModel struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Resolved    string   `json:"resolved,omitempty"`
	Default     bool     `json:"default,omitempty"`
	Efforts     []string `json:"efforts,omitempty"`
}

// LauncherModels are a launcher's models (none: no picker); Locked says why a pick wouldn't reach the agent.
type LauncherModels struct {
	Agent  string       `json:"agent"`
	Models []AgentModel `json:"models"`
	Locked string       `json:"locked,omitempty"`
}

func (c *Client) LauncherModels(ctx context.Context, machine, launcher string) (LauncherModels, error) {
	var out LauncherModels
	err := c.Do(ctx, http.MethodGet, "/machines/"+url.PathEscape(machine)+"/launchers/"+url.PathEscape(launcher)+"/models", nil, &out)
	return out, err
}
