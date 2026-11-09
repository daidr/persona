package l10n

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

var placeholder = regexp.MustCompile(`\{[a-zA-Z]+\}`)

// Every language has every text, with the same placeholders.
func TestCatalogsMatch(t *testing.T) {
	en := catalogs["en"]
	for _, lang := range Languages[1:] {
		for k, v := range en {
			other, ok := catalogs[lang][k]
			if !ok || other == "" {
				t.Errorf("%s lacks %q", lang, k)
				continue
			}
			a, b := placeholder.FindAllString(v, -1), placeholder.FindAllString(other, -1)
			slices.Sort(a)
			slices.Sort(b)
			if !slices.Equal(a, b) {
				t.Errorf("%s %q has placeholders %v, en has %v", lang, k, b, a)
			}
		}
		for k := range catalogs[lang] {
			if _, ok := en[k]; !ok {
				t.Errorf("%s has %q, which en doesn't", lang, k)
			}
		}
	}
}

// Every key the app's Go code uses is in the catalogs.
func TestKeysUsed(t *testing.T) {
	uses := []struct {
		glob string
		re   *regexp.Regexp
	}{
		{"../../*.go", regexp.MustCompile(`\btr\("([a-zA-Z]+)"`)},
		{"../../*.go", regexp.MustCompile(`\{"(go[A-Z][a-zA-Z]+)",`)},
		{"../app/*.go", regexp.MustCompile(`\btr\("([a-zA-Z]+)"`)},
	}
	seen := 0
	used := map[string]bool{}
	for _, u := range uses {
		files, _ := filepath.Glob(u.glob)
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range u.re.FindAllSubmatch(data, -1) {
				seen++
				used[string(m[1])] = true
				if _, ok := catalogs["en"][string(m[1])]; !ok {
					t.Errorf("%s uses %q, which isn't in the catalogs", f, m[1])
				}
			}
		}
	}
	for k := range catalogs["en"] {
		if !used[k] {
			t.Errorf("%q is in the catalogs but nothing uses it", k)
		}
	}
	if seen < 100 {
		t.Errorf("found only %d uses: did the files move?", seen)
	}
}

func TestMatch(t *testing.T) {
	for in, want := range map[string]string{"zh-Hans-CN": "zh-Hans", "zh-CN": "zh-Hans", "zh": "zh-Hans", "ja-JP": "ja", "en-GB": "en", "de": "en", "zh-Hant-TW": "en", "zh_CN": "zh-Hans"} {
		if got := Match([]string{in}); got != want {
			t.Errorf("Match(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Match([]string{"de-DE", "ja"}); got != "ja" {
		t.Errorf("fallback through the list: %q", got)
	}
}

func TestTr(t *testing.T) {
	if got := Tr("ja", "trayNeedsYou", "count", "3"); got != "3 件が対応待ち" {
		t.Errorf("Tr = %q", got)
	}
	if got := Tr("fr", "signIn"); got != "Sign in" {
		t.Errorf("fallback = %q", got)
	}
	if got := Tr("en", "missingKey"); got != "missingKey" {
		t.Errorf("missing = %q", got)
	}
}
