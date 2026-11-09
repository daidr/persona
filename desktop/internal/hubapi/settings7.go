package hubapi

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// ── the hub's own updates (docs/operations/hub-update.md) ──

// BusyItem is what an update waiting for idle waits for.
type BusyItem struct {
	MachineID string `json:"machine_id"`
	Machine   string `json:"machine"`
	SessionID string `json:"session_id,omitempty"`
	Title     string `json:"title"`
}

// HubUpdate is GET /hub/update: the hub's version, a newer release and an update under way.
type HubUpdate struct {
	Version   string `json:"version"`
	Build     int64  `json:"build"`
	Channel   string `json:"channel"` // release | docker | dev
	CanUpdate bool   `json:"can_update"`
	WhyNot    string `json:"why_not,omitempty"`
	Latest    *struct {
		Version  string `json:"version"`
		Build    int64  `json:"build"`
		Released string `json:"released,omitempty"`
		Notes    string `json:"notes,omitempty"`
		Newer    bool   `json:"newer"`
		Bad      bool   `json:"bad"`
		Held     string `json:"held,omitempty"`
	} `json:"latest,omitempty"`
	State      string     `json:"state"`
	Progress   string     `json:"progress,omitempty"`
	Busy       []BusyItem `json:"busy"`
	Error      string     `json:"error,omitempty"`
	Auto       bool       `json:"auto"`
	AutoNotice bool       `json:"auto_notice"`
	LastCheck  string     `json:"last_check,omitempty"`
	Image      *struct {
		Name   string `json:"name"`
		Tag    string `json:"tag"`
		Pinned bool   `json:"pinned"`
	} `json:"image,omitempty"`
}

// Working tells an update under way.
func (u HubUpdate) Working() bool {
	switch u.State {
	case "downloading", "waiting_idle", "restarting", "confirming":
		return true
	}
	return false
}

// UpdateRecord is a line of the update history.
type UpdateRecord struct {
	FromVersion string `json:"from_version"`
	ToVersion   string `json:"to_version"`
	ToBuild     int64  `json:"to_build"`
	Started     string `json:"started,omitempty"`
	Ended       string `json:"ended,omitempty"`
	Result      string `json:"result"` // updated | rolled_back | failed | held
	Reason      string `json:"reason,omitempty"`
	Auto        bool   `json:"auto"`
}

// DockerCommand is how a Docker hub takes a new image.
const DockerCommand = "docker compose pull && docker compose up -d"

func (c *Client) HubUpdate(ctx context.Context) (HubUpdate, error) {
	var out HubUpdate
	err := c.Do(ctx, http.MethodGet, "/hub/update", nil, &out)
	return out, err
}

func (c *Client) CheckHubUpdate(ctx context.Context) (HubUpdate, error) {
	var out HubUpdate
	err := c.Do(ctx, http.MethodPost, "/hub/update/check", map[string]any{}, &out)
	return out, err
}

// StartHubUpdate installs the newer release; now doesn't wait for idle.
func (c *Client) StartHubUpdate(ctx context.Context, now bool) (HubUpdate, error) {
	body := map[string]any{}
	if now {
		body["now"] = true
	}
	var out HubUpdate
	err := c.Do(ctx, http.MethodPost, "/hub/update", body, &out)
	return out, err
}

// SetHubUpdate changes auto (and auto_notice_seen).
func (c *Client) SetHubUpdate(ctx context.Context, patch map[string]any) (HubUpdate, error) {
	var out HubUpdate
	err := c.Do(ctx, http.MethodPut, "/hub/update/settings", patch, &out)
	return out, err
}

func (c *Client) HubUpdateHistory(ctx context.Context) ([]UpdateRecord, error) {
	var out struct {
		History []UpdateRecord `json:"history"`
	}
	err := c.Do(ctx, http.MethodGet, "/hub/update/history", nil, &out)
	return out.History, err
}

// ── managers (docs/assistant/managers.md) ──

type Manager struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Online      bool   `json:"online"`
	Default     bool   `json:"default,omitempty"`
	Builtin     bool   `json:"builtin,omitempty"`
	Host        string `json:"host,omitempty"`
	OS          string `json:"os,omitempty"`
	Arch        string `json:"arch,omitempty"`
	Deploy      string `json:"deploy,omitempty"`
	Version     string `json:"version,omitempty"`
	Since       string `json:"since,omitempty"`
	LastSeen    string `json:"last_seen,omitempty"`
	Installing  string `json:"installing,omitempty"`
	NeedsUpdate bool   `json:"needs_update,omitempty"`
}

type InstallCode struct {
	Code      string `json:"code"`
	ExpiresAt string `json:"expires_at"`
	Commands  *struct {
		Service string `json:"service,omitempty"`
		Docker  string `json:"docker,omitempty"`
	} `json:"commands,omitempty"`
}

func (c *Client) Managers(ctx context.Context) ([]Manager, error) {
	var out struct {
		Managers []Manager `json:"managers"`
		Default  string    `json:"default"`
	}
	err := c.Do(ctx, http.MethodGet, "/managers", nil, &out)
	for i := range out.Managers {
		if out.Managers[i].ID == out.Default {
			out.Managers[i].Default = true
		}
	}
	return out.Managers, err
}

// CreateManager adds a manager to the roster and gives the code that installs it.
func (c *Client) CreateManager(ctx context.Context, name string) (Manager, InstallCode, error) {
	var out struct {
		InstallCode
		Manager Manager `json:"manager"`
	}
	err := c.Do(ctx, http.MethodPost, "/managers", map[string]any{"name": name}, &out)
	return out.Manager, out.InstallCode, err
}

func (c *Client) ManagerCode(ctx context.Context, id string) (InstallCode, error) {
	var out InstallCode
	err := c.Do(ctx, http.MethodPost, "/managers/"+url.PathEscape(id)+"/code", map[string]any{}, &out)
	return out, err
}

// PatchManager renames ({name}) or makes the default ({default: true}).
func (c *Client) PatchManager(ctx context.Context, id string, patch map[string]any) error {
	return c.Do(ctx, http.MethodPatch, "/managers/"+url.PathEscape(id), patch, nil)
}

func (c *Client) DeleteManager(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodDelete, "/managers/"+url.PathEscape(id), nil, nil)
}

// ValidManagerName is 1–40 characters.
func ValidManagerName(name string) bool {
	n := len([]rune(strings.TrimSpace(name)))
	return n > 0 && n <= 40
}

// ── sent as the user (GET /connectors/sent) ──

type SentItem struct {
	ID        int64          `json:"id"`
	At        string         `json:"at"`
	Connector string         `json:"connector"`
	Action    string         `json:"action"`
	Target    map[string]any `json:"target"`
	Text      string         `json:"text"`
	Summary   string         `json:"summary"`
	Origin    struct {
		MachineID string `json:"machine_id,omitempty"`
		SessionID string `json:"session_id,omitempty"`
		Extension string `json:"extension,omitempty"`
	} `json:"origin"`
	LetThrough struct {
		Kind       string `json:"kind,omitempty"` // rule | conversation_always | card | auto
		AnsweredBy string `json:"answered_by,omitempty"`
		Setting    string `json:"setting,omitempty"`
		Rule       *struct {
			Connector string `json:"connector,omitempty"`
			Action    string `json:"action,omitempty"`
		} `json:"rule,omitempty"`
	} `json:"let_through"`
	RecalledAt string `json:"recalled_at,omitempty"`
}

// Sent is a page of the ledger, newest first; next is the before of the next page (0 on the last).
func (c *Client) Sent(ctx context.Context, before int64) (items []SentItem, next int64, err error) {
	path := "/connectors/sent?limit=50"
	if before > 0 {
		path += "&before=" + strconv.FormatInt(before, 10)
	}
	var out struct {
		Items []SentItem `json:"items"`
		Next  *int64     `json:"next"`
	}
	err = c.Do(ctx, http.MethodGet, path, nil, &out)
	if out.Next != nil {
		next = *out.Next
	}
	return out.Items, next, err
}

// ── channels (docs/extensions/channels.md) ──

type Channel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`   // app | im | mention | voice
	Source      string `json:"source"` // builtin | plugin | market
	Installed   bool   `json:"installed"`
	Enabled     bool   `json:"enabled"`
	State       string `json:"state"`
	Error       string `json:"error,omitempty"`
	Description string `json:"description,omitempty"`
	Account     *struct {
		Name string `json:"name"`
	} `json:"account,omitempty"`
	Bind        string `json:"bind"` // none | qr | code | fields | config
	LastEventAt string `json:"last_event_at,omitempty"`
	ReadOnly    bool   `json:"read_only,omitempty"`
	Waiting     *struct {
		Answers int `json:"answers"`
		Asks    int `json:"asks"`
	} `json:"waiting,omitempty"`
}

func (c *Client) Channels(ctx context.Context) ([]Channel, error) {
	var out struct {
		Channels []Channel `json:"channels"`
	}
	err := c.Do(ctx, http.MethodGet, "/channels", nil, &out)
	return out.Channels, err
}

func (c *Client) Channel(ctx context.Context, id string) (Channel, error) {
	var out Channel
	err := c.Do(ctx, http.MethodGet, "/channels/"+url.PathEscape(id), nil, &out)
	return out, err
}

// SetChannel switches a channel on or off.
func (c *Client) SetChannel(ctx context.Context, id string, enabled bool) (Channel, error) {
	var out Channel
	err := c.Do(ctx, http.MethodPut, "/channels/"+url.PathEscape(id), map[string]any{"enabled": enabled}, &out)
	return out, err
}

// BindChannel takes a binding's step (step "" starts it).
func (c *Client) BindChannel(ctx context.Context, id, step string, values map[string]string) (Prompt, error) {
	var out Prompt
	err := c.Do(ctx, http.MethodPost, "/channels/"+url.PathEscape(id)+"/bind", map[string]any{"step": step, "values": valuesOf(values)}, &out)
	return out, err
}

func (c *Client) UnbindChannel(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodDelete, "/channels/"+url.PathEscape(id)+"/binding", nil, nil)
}

func (c *Client) TestChannel(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodPost, "/channels/"+url.PathEscape(id)+"/test", map[string]any{}, nil)
}

// ── sign-in steps, shared by connectors and channels ──

type PromptField struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Secret      bool   `json:"secret,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
}

// Prompt is where a sign-in stands: the step to fill in next, a device code, a QR code, or done.
type Prompt struct {
	Step   string        `json:"step,omitempty"`
	Fields []PromptField `json:"fields,omitempty"`
	Kind   string        `json:"kind,omitempty"` // "" | device_code | qr
	URL    string        `json:"url,omitempty"`
	QR     *struct {
		Size int      `json:"size"`
		Rows []string `json:"rows"`
	} `json:"qr,omitempty"`
	UserCode string `json:"user_code,omitempty"`
	Interval int    `json:"interval,omitempty"`
	Hint     string `json:"hint,omitempty"`
	Error    string `json:"error,omitempty"`
	Done     bool   `json:"done,omitempty"`
	Account  string `json:"account,omitempty"`
	// PairCode is a channel's code to send to its bot.
	PairCode string `json:"pair_code,omitempty"`
}

// Polled tells a step the page asks again for on its own: a device code or a QR code on screen.
func (p Prompt) Polled() bool {
	return !p.Done && (p.Kind == "device_code" || p.Kind == "qr" || p.PairCode != "")
}

func valuesOf(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// ConnectorLogin takes a connector's sign-in step (step "" starts it).
func (c *Client) ConnectorLogin(ctx context.Context, id, step string, values map[string]string) (Prompt, error) {
	var out Prompt
	err := c.Do(ctx, http.MethodPost, "/connectors/"+url.PathEscape(id)+"/login", map[string]any{"step": step, "values": valuesOf(values)}, &out)
	return out, err
}

func (c *Client) ConnectorLogout(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodPost, "/connectors/"+url.PathEscape(id)+"/logout", map[string]any{}, nil)
}

// ── mail accounts ──

// MailForm is the Add mail account form.
type MailForm struct {
	Kind                                   string // gmail (app password) | feishu | imap
	Email, Password                        string
	IMAPHost, IMAPPort, SMTPHost, SMTPPort string
}

var emailRE = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// MailFormErrors names the fields that aren't right: email, password, imapPort, smtpPort.
func MailFormErrors(f MailForm) map[string]bool {
	bad := map[string]bool{}
	if !emailRE.MatchString(strings.TrimSpace(f.Email)) {
		bad["email"] = true
	}
	if f.Password == "" {
		bad["password"] = true
	}
	if f.Kind == "imap" {
		for k, v := range map[string][2]string{"imapPort": {f.IMAPHost, f.IMAPPort}, "smtpPort": {f.SMTPHost, f.SMTPPort}} {
			if strings.TrimSpace(v[0]) == "" {
				continue
			}
			if n, err := strconv.Atoi(strings.TrimSpace(v[1])); err != nil || n < 1 || n > 65535 {
				bad[k] = true
			}
		}
	}
	return bad
}

// MailRequest is POST /mail/accounts's body.
func MailRequest(f MailForm) map[string]any {
	email := strings.TrimSpace(f.Email)
	req := map[string]any{"name": email, "email": email, "provider": f.Kind, "auth": "password", "password": f.Password}
	port := func(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }
	switch f.Kind {
	case "gmail":
		req["imap_host"], req["imap_port"], req["smtp_host"], req["smtp_port"], req["smtp_security"] = "imap.gmail.com", 993, "smtp.gmail.com", 465, "tls"
	case "imap":
		if h := strings.TrimSpace(f.IMAPHost); h != "" {
			req["imap_host"], req["imap_port"] = h, port(f.IMAPPort)
		}
		if h := strings.TrimSpace(f.SMTPHost); h != "" {
			sec := "tls"
			if strings.TrimSpace(f.SMTPPort) == "587" {
				sec = "starttls"
			}
			req["smtp_host"], req["smtp_port"], req["smtp_security"] = h, port(f.SMTPPort), sec
		}
	}
	return req
}

func (c *Client) AddMailAccount(ctx context.Context, f MailForm) error {
	return c.Do(ctx, http.MethodPost, "/mail/accounts", MailRequest(f), nil)
}

func (c *Client) DeleteMailAccount(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodDelete, "/mail/accounts/"+url.PathEscape(id), nil, nil)
}

// GoogleMailSignIn tells whether this hub signs Gmail in with Google (GET /mail/oauth/google/config); without it Gmail
// goes in with an app password. An older hub or a failed read: signed in with Google, on the phone.
func (c *Client) GoogleMailSignIn(ctx context.Context) bool {
	var out struct {
		Configured *bool `json:"configured"`
	}
	if err := c.Do(ctx, http.MethodGet, "/mail/oauth/google/config", nil, &out); err != nil || out.Configured == nil {
		return true
	}
	return *out.Configured
}
