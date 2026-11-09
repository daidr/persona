package hubapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// The rest of Settings: paired devices, extensions, connectors and the models each use takes (docs/desktop/desktop.md).

// DeviceToken is a device paired with the hub, or one taken away (GET /auth/tokens).
type DeviceToken struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at,omitempty"`
	LastSeen  string `json:"last_seen,omitempty"`
	RevokedAt string `json:"revoked_at,omitempty"`
}

// QRImage is a QR code as the hub draws it: rows of "0" and "1".
type QRImage struct {
	Size int      `json:"size"`
	Rows []string `json:"rows"`
}

// PairCode is a one-time pairing link for a new device, valid for ten minutes (POST /auth/pair-codes).
type PairCode struct {
	Token     string   `json:"token"`
	ExpiresAt string   `json:"expires_at"`
	PairURL   string   `json:"pair_url"`
	QR        *QRImage `json:"qr,omitempty"`
}

// Tokens lists the paired devices and which one this is.
func (c *Client) Tokens(ctx context.Context) (list []DeviceToken, current string, err error) {
	var out struct {
		Tokens  []DeviceToken `json:"tokens"`
		Current string        `json:"current"`
	}
	err = c.Do(ctx, http.MethodGet, "/auth/tokens", nil, &out)
	return out.Tokens, out.Current, err
}

// RevokeToken takes a device's access away.
func (c *Client) RevokeToken(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodDelete, "/auth/tokens/"+url.PathEscape(id), nil, nil)
}

// NewPairCode asks for a pairing link for an app.
func (c *Client) NewPairCode(ctx context.Context) (PairCode, error) {
	var out PairCode
	err := c.Do(ctx, http.MethodPost, "/auth/pair-codes", map[string]any{}, &out)
	return out, err
}

// Extension is an extension on the hub: built in, from hub.json or installed (GET /extensions).
type Extension struct {
	// Manager is the manager whose extension it is, in the hub's merged list; "" is the default one's.
	Manager     string   `json:"manager,omitempty"`
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Status      string   `json:"status"` // disabled | needs_setup | incompatible | stopped | starting | running | error
	Error       string   `json:"error,omitempty"`
	Version     string   `json:"version,omitempty"`
	Source      string   `json:"source,omitempty"`
	Enabled     *bool    `json:"enabled,omitempty"`
	Description string   `json:"description,omitempty"`
	NeedsSetup  []string `json:"needs_setup,omitempty"`
}

// On tells a switched-on extension; one from a hub without the switch is always on.
func (e Extension) On() bool { return e.Enabled == nil || *e.Enabled }

// Extensions lists the hub's extensions and the routes' root they came from: /extensions, or /plugins on an older hub.
func (c *Client) Extensions(ctx context.Context) ([]Extension, string, error) {
	var out struct {
		Extensions []Extension `json:"extensions"`
		Plugins    []Extension `json:"plugins"`
	}
	root := "/extensions"
	err := c.Do(ctx, http.MethodGet, root, nil, &out)
	var he *Error
	if errors.As(err, &he) && he.Status == http.StatusNotFound {
		root = "/plugins"
		err = c.Do(ctx, http.MethodGet, root, nil, &out)
	}
	if out.Extensions == nil {
		out.Extensions = out.Plugins
	}
	return out.Extensions, root, err
}

// SetExtensionEnabled switches an extension on or off, under the root Extensions answered with.
func (c *Client) SetExtensionEnabled(ctx context.Context, root, id string, on bool) error {
	return c.Do(ctx, http.MethodPut, root+"/"+url.PathEscape(id)+"/-/enabled", map[string]bool{"enabled": on}, nil)
}

// Connector is one of the hub's connectors: GitHub, Messages, an extension's (GET /connectors).
type Connector struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Status   string   `json:"status"` // ok | not_configured | not_signed_in | no_permission | rate_limited | error | disabled
	Account  string   `json:"account,omitempty"`
	Via      string   `json:"via,omitempty"`
	Error    string   `json:"error,omitempty"`
	Guide    []string `json:"guide,omitempty"`
	LastSync string   `json:"last_sync,omitempty"`
	Enabled  *bool    `json:"enabled,omitempty"`
}

// On tells a switched-on connector; one from a hub without the switch is always on.
func (x Connector) On() bool { return x.Enabled == nil || *x.Enabled }

// Connectors lists the hub's connectors.
func (c *Client) Connectors(ctx context.Context) ([]Connector, error) {
	var out struct {
		Connectors []Connector `json:"connectors"`
	}
	err := c.Do(ctx, http.MethodGet, "/connectors", nil, &out)
	return out.Connectors, err
}

// SetConnectorEnabled switches a connector on or off.
func (c *Client) SetConnectorEnabled(ctx context.Context, id string, on bool) (Connector, error) {
	var out Connector
	err := c.Do(ctx, http.MethodPut, "/connectors/"+url.PathEscape(id)+"/settings", map[string]bool{"enabled": on}, &out)
	return out, err
}

// ModelBinding is a model chosen for a use: a provider, its model and effort.
type ModelBinding struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Effort   string `json:"effort,omitempty"`
}

// ModelPurpose is something the hub uses a model for, and the model it takes (docs/assistant/model-config.md).
type ModelPurpose struct {
	ID            string       `json:"id"`
	CategoryID    string       `json:"category_id,omitempty"`
	Name          string       `json:"name"`
	Group         string       `json:"group"`
	Kind          string       `json:"kind"`
	Override      ModelBinding `json:"override"`
	Effective     ModelBinding `json:"effective"`
	Revision      int          `json:"revision"`
	InheritedFrom string       `json:"inherited_from,omitempty"`
	Status        string       `json:"status,omitempty"`
	Fields        []string     `json:"fields,omitempty"`
	ProviderIDs   []string     `json:"provider_ids,omitempty"`
}

// Editable tells a use whose model can be chosen here; the others follow their category.
func (m ModelPurpose) Editable() bool {
	for _, f := range m.Fields {
		if f == "provider" {
			return true
		}
	}
	return false
}

// ModelProvider is a way to reach models: an agent launcher, an API connection or an extension's.
type ModelProvider struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Model  string `json:"model,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
}

// ModelConfig is every use and every provider (GET /model-config).
type ModelConfig struct {
	Purposes  []ModelPurpose  `json:"purposes"`
	Providers []ModelProvider `json:"providers"`
}

func (c *Client) ModelConfig(ctx context.Context) (ModelConfig, error) {
	var out ModelConfig
	err := c.Do(ctx, http.MethodGet, "/model-config", nil, &out)
	return out, err
}

// SetModel chooses a use's provider and model ("" for each goes back to what it inherits), at the revision read.
func (c *Client) SetModel(ctx context.Context, m ModelPurpose, provider, model string) error {
	body := map[string]any{"revision": m.Revision}
	null := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	for _, f := range m.Fields {
		switch f {
		case "provider":
			body["provider"] = null(provider)
		case "model":
			body["model"] = null(model)
		}
	}
	return c.Do(ctx, http.MethodPatch, "/model-config/purposes/"+url.PathEscape(m.ID), body, nil)
}
