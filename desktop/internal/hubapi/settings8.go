package hubapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// ── model providers (docs/integrations/launchers.md) ──

// Provider is one of GET /launchers: a managed provider or one read from a computer's rc file.
type Provider struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name,omitempty"`
	Source      string   `json:"source"` // persona | zshrc | path | agy | builtin
	Kind        string   `json:"kind,omitempty"`
	API         string   `json:"api,omitempty"`
	BaseURL     string   `json:"base_url,omitempty"`
	Model       string   `json:"model,omitempty"`
	Machines    []string `json:"machines,omitempty"`
	Hidden      bool     `json:"hidden,omitempty"`
	HasKey      bool     `json:"has_key,omitempty"`
	Origin      string   `json:"origin,omitempty"`
}

// Title is what the list calls it.
func (l Provider) Title() string {
	if l.DisplayName != "" {
		return l.DisplayName
	}
	return l.Name
}

// Managed tells a provider made in Persona: deleted, not hidden.
func (l Provider) Managed() bool { return l.Source == "persona" }

// PresetModel is a model a preset offers.
type PresetModel struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// LauncherPreset is a provider the hub knows: only its key is asked for.
type LauncherPreset struct {
	ID          string        `json:"id"`
	Plugin      string        `json:"plugin"`
	Name        string        `json:"name"`
	DisplayName string        `json:"display_name,omitempty"`
	Description string        `json:"description,omitempty"`
	Kind        string        `json:"kind"`
	API         string        `json:"api,omitempty"`
	BaseURL     string        `json:"base_url"`
	Model       string        `json:"model,omitempty"`
	Models      []PresetModel `json:"models,omitempty"`
	KeyHint     string        `json:"key_hint,omitempty"`
	Auth        string        `json:"auth,omitempty"`
	SignupURL   string        `json:"signup_url,omitempty"`
}

func (p LauncherPreset) Title() string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return p.Name
}

// DefaultModel is the model a preset starts with.
func (p LauncherPreset) DefaultModel() string {
	if p.Model != "" {
		return p.Model
	}
	if len(p.Models) > 0 {
		return p.Models[0].ID
	}
	return ""
}

// TestResult is POST /launchers/test's answer.
type TestResult struct {
	OK    bool   `json:"ok"`
	MS    int    `json:"ms"`
	Model string `json:"model,omitempty"`
	Error string `json:"error,omitempty"`
}

// Providers lists the model providers (GET /launchers).
func (c *Client) Providers(ctx context.Context) ([]Provider, error) {
	var out struct {
		Launchers []Provider `json:"launchers"`
	}
	err := c.Do(ctx, http.MethodGet, "/launchers", nil, &out)
	return out.Launchers, err
}

func (c *Client) LauncherPresets(ctx context.Context) ([]LauncherPreset, error) {
	var out struct {
		Presets []LauncherPreset `json:"presets"`
	}
	err := c.Do(ctx, http.MethodGet, "/launchers/presets", nil, &out)
	return out.Presets, err
}

// TestLauncher tries a key and address (or a preset) with one short request.
func (c *Client) TestLauncher(ctx context.Context, body map[string]any) (TestResult, error) {
	var out TestResult
	err := c.Do(ctx, http.MethodPost, "/launchers/test", body, &out)
	return out, err
}

// DiscoverModels asks a gateway for its models.
func (c *Client) DiscoverModels(ctx context.Context, body map[string]any) ([]PresetModel, error) {
	var out struct {
		Models []PresetModel `json:"models"`
		Error  string        `json:"error"`
	}
	err := c.Do(ctx, http.MethodPost, "/launchers/discover", body, &out)
	if err == nil && out.Error != "" {
		err = &Error{Message: out.Error}
	}
	return out.Models, err
}

// AddLauncher saves a provider: from a preset ({preset, api_key, model?, assign?}) or a gateway's fields.
func (c *Client) AddLauncher(ctx context.Context, body map[string]any) (assigned []string, err error) {
	var out struct {
		Assigned []string `json:"assigned"`
	}
	err = c.Do(ctx, http.MethodPost, "/launchers", body, &out)
	return out.Assigned, err
}

// DeleteLauncher deletes a provider made in Persona, or hides one from a computer.
func (c *Client) DeleteLauncher(ctx context.Context, name string) error {
	return c.Do(ctx, http.MethodDelete, "/launchers/"+url.PathEscape(name), nil, nil)
}

func (c *Client) RestoreLauncher(ctx context.Context, name string) error {
	return c.Do(ctx, http.MethodPost, "/launchers/"+url.PathEscape(name)+"/restore", map[string]any{}, nil)
}

// ── an extension's own settings (GET {root}/{id}/-/) ──

type ExtChoice struct {
	Value any    `json:"value"`
	Title string `json:"title,omitempty"`
}

// ExtField is one field of an extension's form, the hub's reading of its manifest's schema.
type ExtField struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"` // string | integer | number | boolean | array | object
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description,omitempty"`
	Default     any         `json:"default,omitempty"`
	Required    bool        `json:"required,omitempty"`
	Enum        []ExtChoice `json:"enum,omitempty"`
	Format      string      `json:"format,omitempty"`
	Items       *struct {
		Type   string      `json:"type"`
		Enum   []ExtChoice `json:"enum,omitempty"`
		Format string      `json:"format,omitempty"`
	} `json:"items,omitempty"`
	Fields []ExtField `json:"fields,omitempty"`
}

// Label is the field's title, else its name.
func (f ExtField) Label() string {
	if f.Title != "" {
		return f.Title
	}
	return f.Name
}

type ExtSecret struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Set         bool   `json:"set"`
	Hint        string `json:"hint,omitempty"`
}

type ExtPermission struct {
	Key    string `json:"key"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

// ExtDetail is an extension with its form, settings, secrets and permissions.
type ExtDetail struct {
	Extension
	Publisher string `json:"publisher,omitempty"`
	Homepage  string `json:"homepage,omitempty"`
	Origin    string `json:"origin,omitempty"`
	ReadOnly  bool   `json:"read_only,omitempty"`
	Form      *struct {
		Fields []ExtField `json:"fields,omitempty"`
	} `json:"form,omitempty"`
	Config      map[string]any  `json:"config,omitempty"`
	Secrets     []ExtSecret     `json:"secrets,omitempty"`
	Permissions []ExtPermission `json:"permissions,omitempty"`
}

// FromMarket tells one installed from a market, which can be uninstalled.
func (d ExtDetail) FromMarket() bool { return len(d.Origin) > 7 && d.Origin[:7] == "market:" }

func extBase(root, id string) string { return root + "/" + url.PathEscape(id) + "/-" }

func (c *Client) ExtensionDetail(ctx context.Context, root, id string) (ExtDetail, error) {
	var out ExtDetail
	err := c.Do(ctx, http.MethodGet, extBase(root, id)+"/", nil, &out)
	return out, err
}

func (c *Client) SetExtensionConfig(ctx context.Context, root, id string, config map[string]any) (ExtDetail, error) {
	var out ExtDetail
	err := c.Do(ctx, http.MethodPut, extBase(root, id)+"/config", map[string]any{"config": config}, &out)
	return out, err
}

func (c *Client) SetExtensionSecret(ctx context.Context, root, id, name, value string) (ExtDetail, error) {
	var out ExtDetail
	err := c.Do(ctx, http.MethodPut, extBase(root, id)+"/secrets/"+url.PathEscape(name), map[string]any{"value": value}, &out)
	return out, err
}

func (c *Client) ClearExtensionSecret(ctx context.Context, root, id, name string) (ExtDetail, error) {
	var out ExtDetail
	err := c.Do(ctx, http.MethodDelete, extBase(root, id)+"/secrets/"+url.PathEscape(name), nil, &out)
	return out, err
}

// UninstallExtension removes one installed from a market.
func (c *Client) UninstallExtension(ctx context.Context, root, id string) error {
	return c.Do(ctx, http.MethodDelete, extBase(root, id)+"/", nil, nil)
}

// ── the market ──

type Market struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Official   bool   `json:"official,omitempty"`
	Extensions int    `json:"extensions"`
	Error      string `json:"error,omitempty"`
}

type CatalogItem struct {
	Market      string          `json:"market"`
	MarketName  string          `json:"market_name"`
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description,omitempty"`
	Publisher   string          `json:"publisher,omitempty"`
	Tags        []string        `json:"tags,omitempty"`
	Permissions []ExtPermission `json:"permissions,omitempty"`
	Problem     string          `json:"problem,omitempty"`
	Installed   string          `json:"installed,omitempty"`
	Upgradable  bool            `json:"upgradable,omitempty"`
	RenameFrom  string          `json:"rename_from,omitempty"`
}

// MarketPreview is what installing would do, before it does.
type MarketPreview struct {
	PreviewID string `json:"preview_id"`
	Manifest  struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Version     string `json:"version"`
		Description string `json:"description,omitempty"`
		Publisher   string `json:"publisher,omitempty"`
	} `json:"manifest"`
	Bundle            []MarketPreview `json:"bundle,omitempty"`
	Unresolved        []string        `json:"unresolved,omitempty"`
	Permissions       []ExtPermission `json:"permissions"`
	PermissionsDigest string          `json:"permissions_digest"`
	Warnings          []string        `json:"warnings,omitempty"`
	ExpiresAt         string          `json:"expires_at"`
	Upgrade           bool            `json:"upgrade,omitempty"`
	RenameFrom        string          `json:"rename_from,omitempty"`
	MorePermissions   bool            `json:"more_permissions,omitempty"`
	AddedPermissions  []ExtPermission `json:"added_permissions,omitempty"`
}

// Consent is what installing sends back: the preview and the permissions agreed to.
func (pv MarketPreview) Consent() map[string]any {
	out := map[string]any{"preview_id": pv.PreviewID, "permissions_digest": pv.PermissionsDigest}
	if len(pv.Bundle) > 0 {
		var b []map[string]any
		for _, x := range pv.Bundle {
			b = append(b, map[string]any{"preview_id": x.PreviewID, "permissions_digest": x.PermissionsDigest})
		}
		out["bundle"] = b
	}
	return out
}

// Catalog lists the markets' extensions against what is installed.
func (c *Client) Catalog(ctx context.Context, root, q, market string) ([]Market, []CatalogItem, error) {
	v := url.Values{}
	if q != "" {
		v.Set("q", q)
	}
	if market != "" {
		v.Set("market", market)
	}
	path := root + "/-/catalog"
	if len(v) > 0 {
		path += "?" + v.Encode()
	}
	var out struct {
		Markets    []Market      `json:"markets"`
		Extensions []CatalogItem `json:"extensions"`
		Plugins    []CatalogItem `json:"plugins"`
	}
	err := c.Do(ctx, http.MethodGet, path, nil, &out)
	if out.Extensions == nil {
		out.Extensions = out.Plugins
	}
	return out.Markets, out.Extensions, err
}

func (c *Client) PreviewExtension(ctx context.Context, root, market, id string) (MarketPreview, error) {
	var out MarketPreview
	err := c.Do(ctx, http.MethodPost, root+"/-/catalog/preview", map[string]any{"market": market, "extension": id, "plugin": id}, &out)
	return out, err
}

// InstallExtension installs (or with upgrade, upgrades id to) what the preview showed.
func (c *Client) InstallExtension(ctx context.Context, root string, pv MarketPreview, upgrade string) error {
	path := root + "/-/install"
	if pv.RenameFrom != "" {
		upgrade = pv.RenameFrom
	}
	if upgrade != "" {
		path = extBase(root, upgrade) + "/upgrade"
	}
	var out json.RawMessage
	return c.Do(ctx, http.MethodPost, path, pv.Consent(), &out)
}
