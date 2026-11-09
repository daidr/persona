package hubapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nwverse/persona/desktop/internal/hubs"
)

// Platform and Version name the app to the hub (client_hello, X-Persona-Client).
const (
	Platform = "desktop"
	Version  = "1"
)

// Features are what the app can render beyond the basics (client_hello features).
var Features = []string{"persona_contracts_v1", "app_ping"}

// readTimeout is how long a read waits for the hub, longer than the hub's own cut-offs for asking a computer.
const readTimeout = 45 * time.Second

// ErrUnauthorized is a token the hub refused (signed out on another device, revoked).
var ErrUnauthorized = errors.New("hub refused the sign-in")

// ErrGone is a card that has already ended (410).
var ErrGone = errors.New("already handled")

// Error is a refusal from the hub with its stable code and its text in the user's language.
type Error struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"error"`
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("hub answered %d", e.Status)
}

// Client calls one hub with one device token. It is safe for use from several goroutines.
type Client struct {
	Hub    hubs.Hub
	Token  string
	Locale string
	http   *http.Client
}

// New makes a client for a hub. A hub with a pin (from a pairing link) accepts its own self-signed certificate.
func New(hub hubs.Hub, token, locale string) *Client {
	return &Client{Hub: hub, Token: token, Locale: locale, http: &http.Client{Transport: Transport(hub)}}
}

// Transport is an HTTP transport that trusts what the system trusts and, for a hub with a pin, the certificate
// whose public key is pinned on the hub's host (docs/operations/network.md, "TLS and public-key pinning").
func Transport(hub hubs.Hub) *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = TLSConfig(hub)
	return t
}

// TLSConfig verifies like the system does, then falls back to the hub's pin.
func TLSConfig(hub hubs.Hub) *tls.Config {
	if hub.FP == "" {
		return &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // replaced by VerifyConnection below: system trust, or the pin
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("no certificate")
			}
			leaf := cs.PeerCertificates[0]
			pool := x509.NewCertPool()
			for _, c := range cs.PeerCertificates[1:] {
				pool.AddCert(c)
			}
			if _, err := leaf.Verify(x509.VerifyOptions{DNSName: cs.ServerName, Intermediates: pool}); err == nil {
				return nil
			}
			host := cs.ServerName
			if host == "" {
				u, _ := url.Parse(hub.URL)
				host = u.Hostname()
			}
			if hub.Pinned(host, leaf.Raw) {
				return nil
			}
			return errors.New("the hub's certificate is not the paired one")
		},
	}
}

// OpID is a fresh id for an idempotent write (X-Persona-Op-Id) or a client_msg_id.
func OpID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (c *Client) endpoint(path string) string {
	return strings.TrimRight(c.Hub.URL, "/") + "/api/v1" + path
}

// Do sends a request and decodes the answer into out (when not nil).
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	if method == http.MethodGet {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, readTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(path), rd)
	if err != nil {
		return err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set("X-Persona-Op-Id", OpID())
	}
	req.Header.Set("X-Persona-Client", Platform+"/"+Version)
	if c.Locale != "" {
		req.Header.Set("Accept-Language", c.Locale)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrUnauthorized
	case resp.StatusCode == http.StatusGone:
		return ErrGone
	case resp.StatusCode >= 300:
		e := &Error{Status: resp.StatusCode}
		_ = json.Unmarshal(data, e)
		return e
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// Exchange trades a pairing or sign-in token for this device's own token (POST /auth/tokens). A pairing token
// works only once and only for minutes, so the app keeps only what this returns.
func (c *Client) Exchange(ctx context.Context, name string) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	if err := c.Do(ctx, http.MethodPost, "/auth/tokens", map[string]string{"name": name, "kind": Platform}, &out); err != nil {
		return "", err
	}
	if out.Token == "" {
		return "", errors.New("the hub returned no token")
	}
	return out.Token, nil
}

// Machines lists the computers.
func (c *Client) Machines(ctx context.Context) ([]Machine, error) {
	var out struct {
		Machines []Machine `json:"machines"`
	}
	err := c.Do(ctx, http.MethodGet, "/machines", nil, &out)
	return out.Machines, err
}

// Sessions lists a computer's agents.
func (c *Client) Sessions(ctx context.Context, machine string) ([]Session, error) {
	var out struct {
		Sessions []Session `json:"sessions"`
	}
	err := c.Do(ctx, http.MethodGet, "/sessions?machine_id="+url.QueryEscape(machine), nil, &out)
	return out.Sessions, err
}

// Items reads a page of a transcript: the last two turns, or the turns before a seq.
func (c *Client) Items(ctx context.Context, machine, session string, before int64) (ItemsPage, error) {
	q := url.Values{"turns": {"2"}}
	if before > 0 {
		q.Set("before_seq", fmt.Sprint(before))
	}
	var out ItemsPage
	err := c.Do(ctx, http.MethodGet, "/sessions/"+url.PathEscape(machine)+"/"+url.PathEscape(session)+"/items?"+q.Encode(), nil, &out)
	return out, err
}

// Send sends a message to an agent.
func (c *Client) Send(ctx context.Context, machine, session, text, clientMsg string) error {
	return c.Do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(machine)+"/"+url.PathEscape(session)+"/messages",
		map[string]string{"text": text, "client_msg_id": clientMsg}, nil)
}

// Interrupt stops an agent's turn.
func (c *Client) Interrupt(ctx context.Context, machine, session string) error {
	return c.Do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(machine)+"/"+url.PathEscape(session)+"/interrupt", struct{}{}, nil)
}

// Seen marks an agent read.
func (c *Client) Seen(ctx context.Context, machine, session string) error {
	return c.Do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(machine)+"/"+url.PathEscape(session)+"/seen", struct{}{}, nil)
}

// Interactions lists the open cards.
func (c *Client) Interactions(ctx context.Context) ([]Interaction, error) {
	var out struct {
		Interactions []Interaction `json:"interactions"`
	}
	err := c.Do(ctx, http.MethodGet, "/interactions", nil, &out)
	return out.Interactions, err
}

// Resolve answers a card. ErrGone means it ended meanwhile (answered elsewhere, withdrawn).
func (c *Client) Resolve(ctx context.Context, id string, r Resolution) error {
	return c.Do(ctx, http.MethodPost, "/interactions/"+url.PathEscape(id)+"/resolve", r, nil)
}

// Assistant reads the assistant's conversations.
func (c *Client) Assistant(ctx context.Context) (Assistant, error) {
	var out Assistant
	err := c.Do(ctx, http.MethodGet, "/assistant", nil, &out)
	return out, err
}

// Inbox lists the Inbox.
func (c *Client) Inbox(ctx context.Context) ([]InboxItem, error) {
	var out struct {
		Items []InboxItem `json:"items"`
	}
	err := c.Do(ctx, http.MethodGet, "/inbox", nil, &out)
	return out.Items, err
}

// InboxDone marks an Inbox entry handled, or not.
func (c *Client) InboxDone(ctx context.Context, id string, done bool) error {
	return c.Do(ctx, http.MethodPost, "/inbox/"+url.PathEscape(id)+"/done", map[string]bool{"done": done}, nil)
}

// SignOut revokes this device's token on the hub; a failure leaves it to be revoked in Settings › Devices.
func (c *Client) SignOut(ctx context.Context) error {
	var out struct {
		Current string `json:"current"`
	}
	if err := c.Do(ctx, http.MethodGet, "/auth/tokens", nil, &out); err != nil {
		return err
	}
	if out.Current == "" {
		return nil
	}
	return c.Do(ctx, http.MethodDelete, "/auth/tokens/"+url.PathEscape(out.Current), nil, nil)
}

// wsURL is the WebSocket address of the hub.
func (c *Client) wsURL() string {
	u, _ := url.Parse(c.endpoint("/ws"))
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	return u.String()
}

// Offline tells an error that means the hub is out of reach, rather than refusing.
func Offline(err error) bool {
	var ne net.Error
	var e *Error
	return errors.As(err, &ne) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &e) && e.Status >= 502)
}

// Todos lists the to-dos.
func (c *Client) Todos(ctx context.Context) ([]Todo, error) {
	var out struct {
		Todos []Todo `json:"todos"`
	}
	err := c.Do(ctx, http.MethodGet, "/todos", nil, &out)
	return out.Todos, err
}

// CreateTodo adds a to-do.
func (c *Client) CreateTodo(ctx context.Context, title, notes string) (Todo, error) {
	var out struct {
		Todo Todo `json:"todo"`
	}
	err := c.Do(ctx, http.MethodPost, "/todos", map[string]string{"title": title, "notes": notes}, &out)
	return out.Todo, err
}

// UpdateTodo changes a to-do's title, notes or done ("title", "notes", "done").
func (c *Client) UpdateTodo(ctx context.Context, id string, patch map[string]any) (Todo, error) {
	var out struct {
		Todo Todo `json:"todo"`
	}
	err := c.Do(ctx, http.MethodPatch, "/todos/"+url.PathEscape(id), patch, &out)
	return out.Todo, err
}

// DeleteTodo deletes a to-do and its links; the agents stay.
func (c *Client) DeleteTodo(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodDelete, "/todos/"+url.PathEscape(id), nil, nil)
}

// Launchers lists how agents start on a computer.
func (c *Client) Launchers(ctx context.Context, machine string) (Launchers, error) {
	var out Launchers
	err := c.Do(ctx, http.MethodGet, "/machines/"+url.PathEscape(machine)+"/launchers", nil, &out)
	return out, err
}

// Dirs lists a folder on a computer; "" is the home folder.
func (c *Client) Dirs(ctx context.Context, machine, path string) (Dirs, error) {
	var out Dirs
	err := c.Do(ctx, http.MethodGet, "/machines/"+url.PathEscape(machine)+"/dirs?path="+url.QueryEscape(path), nil, &out)
	return out, err
}

// Start starts an agent.
func (c *Client) Start(ctx context.Context, req StartRequest) (Session, error) {
	var out struct {
		Session Session `json:"session"`
	}
	err := c.Do(ctx, http.MethodPost, "/sessions", req, &out)
	if out.Session.MachineID == "" {
		out.Session.MachineID = req.MachineID
	}
	return out.Session, err
}
