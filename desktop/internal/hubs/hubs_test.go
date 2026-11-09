package hubs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"math/big"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalize(t *testing.T) {
	ok := map[string]string{
		"hub.example.com":                    "https://hub.example.com",
		"HTTPS://Hub.Example.com:443/":       "https://hub.example.com",
		"http://192.0.2.10:18661":            "http://192.0.2.10:18661",
		"https://[2001:db8::1]:18662/":       "https://[2001:db8::1]:18662",
		"https://proxy.example.com/persona/": "", // a page of the web app, not a prefix
		"https://proxy.example.com/p":        "https://proxy.example.com/p",
		"  https://hub.example.com:8443  ":   "https://hub.example.com:8443",
	}
	for in, want := range ok {
		got, err := Normalize(in)
		if want == "" {
			if err == nil {
				t.Errorf("Normalize(%q) = %q, want an error", in, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "ftp://hub.example.com", "https://me@hub.example.com", "https://hub.example.com/?a=1", "https://hub.example.com/#x", "https://hub.example.com/settings", "https://hub.example.com:99999", "https://hub.example.com/a/../b"} {
		if got, err := Normalize(in); err == nil {
			t.Errorf("Normalize(%q) = %q, want an error", in, got)
		}
	}
}

func TestParsePairLink(t *testing.T) {
	in, err := ParseInput("persona://pair?url=http%3A%2F%2F192.0.2.10%3A18661&token=jp_abc&fp=FP43&tls=18662&hub=h1")
	if err != nil {
		t.Fatal(err)
	}
	if in.Token != "jp_abc" || in.Hub.URL != "https://192.0.2.10:18662" || in.Hub.FP != "FP43" || in.Hub.HubID != "h1" {
		t.Errorf("pinned link: %+v", in)
	}
	in, err = ParseInput("persona://pair?url=https%3A%2F%2Fproxy.example.com%2Fp&token=jp_x")
	if err != nil || in.Hub.URL != "https://proxy.example.com/p" || in.Hub.FP != "" || in.Token != "jp_x" {
		t.Errorf("proxy link: %+v %v", in, err)
	}
	if _, err := ParseInput("persona://link?url=https%3A%2F%2Fh&token=t&machine=m"); !errors.Is(err, ErrMachineLink) {
		t.Errorf("machine link: %v", err)
	}
	if _, err := ParseInput("persona://pair?url=https%3A%2F%2Fh&fp=x&token=t"); !errors.Is(err, ErrPinNoPort) {
		t.Errorf("fp without port: %v", err)
	}
	if _, err := ParseInput("persona://pair?url=https%3A%2F%2Fh"); !errors.Is(err, ErrNoToken) {
		t.Errorf("no token: %v", err)
	}
}

func TestParseWebLink(t *testing.T) {
	in, err := ParseInput("https://hub.example.com:18662/#token=jp_web")
	if err != nil || in.Hub.URL != "https://hub.example.com:18662" || in.Token != "jp_web" {
		t.Errorf("web link: %+v %v", in, err)
	}
	in, err = ParseInput("https://proxy.example.com/p/?token=jd_1")
	if err != nil || in.Hub.URL != "https://proxy.example.com/p" || in.Token != "jd_1" {
		t.Errorf("query link: %+v %v", in, err)
	}
	in, err = ParseInput("hub.example.com")
	if err != nil || in.Hub.URL != "https://hub.example.com" || in.Token != "" {
		t.Errorf("address: %+v %v", in, err)
	}
}

func TestHubPages(t *testing.T) {
	h := Hub{URL: "https://proxy.example.com/p"}
	for u, want := range map[string]bool{
		"https://proxy.example.com/p":          true,
		"https://proxy.example.com/p/settings": true,
		"https://proxy.example.com/pp":         false,
		"https://proxy.example.com/":           false,
		"https://other.example.com/p/":         false,
		"https://PROXY.example.com:443/p/a":    true,
	} {
		if h.Contains(u) != want {
			t.Errorf("Contains(%q) = %v", u, !want)
		}
	}
	if got := h.OpenURL("jp a"); got != "https://proxy.example.com/p/#token=jp+a" {
		t.Errorf("OpenURL = %q", got)
	}
	if got := (Hub{URL: "https://hub.example.com"}).OpenURL(""); got != "https://hub.example.com/" {
		t.Errorf("OpenURL = %q", got)
	}
}

func TestPinned(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "hub"}, NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	spki, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	sum := sha256.Sum256(spki)
	fp := base64.RawURLEncoding.EncodeToString(sum[:])
	if SPKIFingerprint(der) != fp || len(fp) != 43 {
		t.Fatalf("fingerprint %q, want %q", SPKIFingerprint(der), fp)
	}
	h := Hub{URL: "https://192.0.2.10:18662", FP: fp}
	if !h.Pinned("192.0.2.10", der) {
		t.Error("pinned key refused")
	}
	if h.Pinned("198.51.100.1", der) {
		t.Error("another host accepted")
	}
	if (Hub{URL: h.URL, FP: "other"}).Pinned("192.0.2.10", der) || (Hub{URL: h.URL}).Pinned("192.0.2.10", der) {
		t.Error("unpinned or wrong pin accepted")
	}
	if h.Pinned("192.0.2.10", []byte("junk")) {
		t.Error("junk accepted")
	}
}

func TestBadgeFromTitle(t *testing.T) {
	for title, want := range map[string]int{"(2) Persona": 2, "(12) Settings · Persona": 12, "Persona": 0, "(x) Persona": 0, "Persona (3) ": 0} {
		if got := BadgeFromTitle(title); got != want {
			t.Errorf("BadgeFromTitle(%q) = %d, want %d", title, got, want)
		}
	}
}

func TestStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "hubs.json")
	s := Load(path)
	s.Upsert(Hub{URL: "https://a.example.com", FP: "fp1", Label: "Home"})
	s.Upsert(Hub{URL: "https://a.example.com", HubID: "h"}) // keeps the pin and the label
	s.Upsert(Hub{URL: "https://b.example.com/p"})
	s.Settings.Current = "https://b.example.com/p"
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s = Load(path)
	a, ok := s.Get("https://a.example.com")
	if !ok || a.FP != "fp1" || a.Label != "Home" || a.HubID != "h" || a.Title() != "Home" {
		t.Errorf("a = %+v", a)
	}
	if h, ok := s.ForPage("https://b.example.com/p/settings"); !ok || h.URL != "https://b.example.com/p" {
		t.Errorf("ForPage = %+v", h)
	}
	s.Rename("https://b.example.com/p", "Work")
	if s.Hubs[1].Title() != "Work" {
		t.Errorf("rename: %+v", s.Hubs[1])
	}
	s.Remove("https://b.example.com/p")
	if len(s.Hubs) != 1 || s.Settings.Current != "" {
		t.Errorf("remove: %+v", s)
	}
	for i := range MaxHubs + 3 {
		s.Upsert(Hub{URL: "https://h" + string(rune('a'+i)) + ".example.com", Opened: time.Unix(int64(1000+i), 0)})
	}
	if len(s.Hubs) != MaxHubs {
		t.Errorf("kept %d hubs", len(s.Hubs))
	}
	if _, ok := s.Get("https://a.example.com"); ok {
		t.Error("the hub never opened should go first")
	}
}
