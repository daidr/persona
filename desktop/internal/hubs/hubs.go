// Package hubs keeps the desktop app's list of hubs and reads the links that add one: a hub's address, the apps'
// pairing link (`persona://pair?…`, which `personad hub pair` prints and Settings › Devices shows) and the web
// sign-in link (`<hub>/#token=…`). It has no MyGo dependency, so its rules are tested without a window.
package hubs

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// MaxHubs is how many hubs the list keeps, as the web app's list does (web/src/lib/hubs.ts).
const MaxHubs = 20

// Hub is one hub the app can open.
type Hub struct {
	// URL is where the hub's web app is: scheme://host[:port] and, behind a reverse proxy, its path prefix, with no
	// trailing slash. It also identifies the hub in the list.
	URL string `json:"url"`
	// Name is the hub's own name, Label the one the user gave, which wins.
	Name  string `json:"name,omitempty"`
	Label string `json:"label,omitempty"`
	// FP pins the hub's TLS public key: SHA-256 of the certificate's SubjectPublicKeyInfo, unpadded base64url, as a
	// pairing link carries it (docs/operations/network.md, "TLS and public-key pinning"). Only a pairing link sets it.
	FP string `json:"fp,omitempty"`
	// HubID is the hub's id from the pairing link, kept to tell hubs apart.
	HubID string `json:"hub_id,omitempty"`
	// Opened is when the app last opened the hub.
	Opened time.Time `json:"opened,omitzero"`
}

// Title is what the app calls the hub: the user's label, the hub's name, or its address.
func (h Hub) Title() string {
	if h.Label != "" {
		return h.Label
	}
	if h.Name != "" {
		return h.Name
	}
	u, err := url.Parse(h.URL)
	if err != nil {
		return h.URL
	}
	return u.Host + strings.TrimSuffix(u.Path, "/")
}

// OriginOf is the origin of an http(s) URL, with the scheme and host in lower case and a default port left out, or ""
// for anything else.
func OriginOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	scheme, host, port := strings.ToLower(u.Scheme), strings.ToLower(u.Hostname()), u.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	return scheme + "://" + host
}

// Errors of Normalize and ParseInput, which the app says in the user's language.
var (
	ErrEmpty       = errors.New("empty")
	ErrAddress     = errors.New("not a hub address")
	ErrMachineLink = errors.New("a computer's pairing link")
	ErrPinNoPort   = errors.New("pairing link has a fingerprint but no TLS port")
	ErrNoToken     = errors.New("pairing link has no token")
)

// pagePaths are the first segments of the web app's own pages (web/src/lib/router.ts): an address ending in one is a
// page of a hub, not a reverse proxy's prefix.
var pagePaths = map[string]bool{
	"s": true, "t": true, "a": true, "persona": true, "assistant": true, "overview": true, "history": true,
	"health": true, "inbox": true, "messages": true, "routines": true, "kb": true, "space": true, "machines": true,
	"terminal": true, "agents": true, "settings": true, "extension": true, "plugin": true, "todos": true,
	"workspace-board": true, "api": true, "admin": true,
}

// Normalize turns an address the user typed into a hub's URL. With no scheme it assumes https, as the web app does.
// It turns down a user name, a query, a fragment and a path that is a page of the web app.
func Normalize(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", ErrEmpty
	}
	if !strings.Contains(addr, "://") {
		addr = "https://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", ErrAddress
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return "", ErrAddress
		}
	}
	path := strings.TrimSuffix(u.EscapedPath(), "/")
	for _, seg := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if seg == "." || seg == ".." || (seg == "" && path != "") || pagePaths[seg] && path != "" {
			return "", ErrAddress
		}
	}
	return OriginOf(u.String()) + path, nil
}

// Input is what the user pasted or a deep link carried: the hub, and a token to sign in with when there is one.
type Input struct {
	Hub   Hub
	Token string
}

// ParseInput reads an address, a `persona://pair?…` link or a web sign-in link (`<hub>/#token=…` or `?token=…`).
func ParseInput(raw string) (Input, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Input{}, ErrEmpty
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "persona:") {
		return parsePairLink(raw)
	}
	if strings.Contains(raw, "token=") {
		u, err := url.Parse(raw)
		if err != nil {
			return Input{}, ErrAddress
		}
		token := u.Query().Get("token")
		frag, _ := url.ParseQuery(u.Fragment)
		if token == "" {
			token = frag.Get("token")
		}
		u.RawQuery, u.Fragment, u.RawFragment = "", "", ""
		base, err := Normalize(u.String())
		if err != nil {
			return Input{}, err
		}
		return Input{Hub: Hub{URL: base}, Token: strings.TrimSpace(token)}, nil
	}
	base, err := Normalize(raw)
	if err != nil {
		return Input{}, err
	}
	return Input{Hub: Hub{URL: base}}, nil
}

// parsePairLink reads the apps' pairing link the way Persona Link does (server/internal/link/client_pair.go): with
// a fingerprint the hub is its own TLS port on the host of url, pinned; without one, url is the hub (or the HTTPS
// reverse proxy in front of it).
func parsePairLink(raw string) (Input, error) {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "persona") {
		return Input{}, ErrAddress
	}
	switch strings.ToLower(u.Host) {
	case "pair":
	case "link":
		return Input{}, ErrMachineLink
	default:
		return Input{}, ErrAddress
	}
	q := u.Query()
	token := strings.TrimSpace(q.Get("token"))
	if token == "" {
		return Input{}, ErrNoToken
	}
	h := Hub{FP: strings.TrimSpace(q.Get("fp")), HubID: q.Get("hub")}
	if h.FP != "" {
		port := q.Get("tls")
		base, err := url.Parse(q.Get("url"))
		if port == "" {
			return Input{}, ErrPinNoPort
		}
		if err != nil || base.Hostname() == "" {
			return Input{}, ErrAddress
		}
		if h.URL, err = Normalize("https://" + net.JoinHostPort(base.Hostname(), port)); err != nil {
			return Input{}, err
		}
	} else if h.URL, err = Normalize(q.Get("url")); err != nil {
		return Input{}, err
	}
	return Input{Hub: h, Token: token}, nil
}

// SPKIFingerprint is a certificate's public-key fingerprint as pairing links carry it, or "" for a certificate that
// doesn't parse.
func SPKIFingerprint(der []byte) string {
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Pinned reports whether a certificate that the system doesn't trust is the hub's pinned one: the hub has a pin,
// the host is the hub's, and the certificate's public key is the pinned key.
func (h Hub) Pinned(host string, der []byte) bool {
	if h.FP == "" {
		return false
	}
	u, err := url.Parse(h.URL)
	if err != nil || !strings.EqualFold(strings.Trim(host, "[]"), u.Hostname()) {
		return false
	}
	fp := SPKIFingerprint(der)
	return fp != "" && fp == h.FP
}

// Settings is what the app keeps besides the hubs.
type Settings struct {
	// Current is the URL of the hub the app opened last, which it opens at launch.
	Current string `json:"current,omitempty"`
	// NoShortcut turns the global shortcut off.
	NoShortcut bool `json:"no_shortcut,omitempty"`
	// NoNotifications turns the app's notifications off.
	NoNotifications bool `json:"no_notifications,omitempty"`
	// DeviceID names this install to the hub (client_hello device_id); made once.
	DeviceID string `json:"device_id,omitempty"`
}

// Store is the list of hubs and the settings, kept as JSON in the app's data directory. It holds no secrets: each
// hub's device token is in the system's credential store (Keychain, Credential Manager, Secret Service).
type Store struct {
	path     string
	Hubs     []Hub    `json:"hubs"`
	Settings Settings `json:"settings"`
}

// Load reads the store at path; a missing or unreadable file is an empty store.
func Load(path string) *Store {
	s := &Store{path: path}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, s)
	}
	s.path = path
	return s
}

// Save writes the store, readable by the user alone.
func (s *Store) Save() error {
	if s.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Get returns the hub with the URL.
func (s *Store) Get(rawURL string) (Hub, bool) {
	for _, h := range s.Hubs {
		if h.URL == rawURL {
			return h, true
		}
	}
	return Hub{}, false
}

// Upsert adds the hub, or updates the one with its URL: a new pin or hub id replaces the old (it comes from a
// pairing link, which is trusted), and the user's label stays unless a new one is given. The list keeps the hubs
// opened most recently when it is full.
func (s *Store) Upsert(h Hub) Hub {
	for i, old := range s.Hubs {
		if old.URL != h.URL {
			continue
		}
		if h.Label == "" {
			h.Label = old.Label
		}
		if h.Name == "" {
			h.Name = old.Name
		}
		if h.FP == "" {
			h.FP = old.FP
		}
		if h.HubID == "" {
			h.HubID = old.HubID
		}
		if h.Opened.IsZero() {
			h.Opened = old.Opened
		}
		s.Hubs[i] = h
		return h
	}
	s.Hubs = append(s.Hubs, h)
	if len(s.Hubs) > MaxHubs {
		oldest := 0
		for i, x := range s.Hubs[:len(s.Hubs)-1] {
			if x.Opened.Before(s.Hubs[oldest].Opened) {
				oldest = i
			}
		}
		s.Hubs = append(s.Hubs[:oldest], s.Hubs[oldest+1:]...)
	}
	return h
}

// Remove takes the hub off the list.
func (s *Store) Remove(rawURL string) {
	for i, h := range s.Hubs {
		if h.URL == rawURL {
			s.Hubs = append(s.Hubs[:i], s.Hubs[i+1:]...)
			break
		}
	}
	if s.Settings.Current == rawURL {
		s.Settings.Current = ""
	}
}

// Rename sets the user's label of the hub; an empty label goes back to the hub's own name.
func (s *Store) Rename(rawURL, label string) {
	for i, h := range s.Hubs {
		if h.URL == rawURL {
			s.Hubs[i].Label = strings.TrimSpace(label)
		}
	}
}
