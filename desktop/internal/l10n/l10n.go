// Package l10n holds the desktop app's own texts, in en, zh-Hans and ja like the other clients (docs/protocol/i18n.md).
// The pages of the hub bring their own; these are the menus, the tray, notifications of the app and its connect page.
package l10n

import (
	"embed"
	"encoding/json"
	"strings"
)

//go:embed locales/*.json
var files embed.FS

// Languages are the catalogs, the first being the source every other falls back to.
var Languages = []string{"en", "zh-Hans", "ja"}

var catalogs = map[string]map[string]string{}

func init() {
	for _, lang := range Languages {
		data, err := files.ReadFile("locales/" + lang + ".json")
		if err != nil {
			panic(err)
		}
		m := map[string]string{}
		if err := json.Unmarshal(data, &m); err != nil {
			panic("l10n: " + lang + ": " + err.Error())
		}
		catalogs[lang] = m
	}
}

// Match picks the catalog for the user's preferred languages, most preferred first: Simplified Chinese for zh
// other than Traditional, Japanese for ja, else English.
func Match(preferred []string) string {
	for _, tag := range preferred {
		t := strings.ToLower(strings.ReplaceAll(tag, "_", "-"))
		switch {
		case t == "en" || strings.HasPrefix(t, "en-"):
			return "en"
		case t == "ja" || strings.HasPrefix(t, "ja-"):
			return "ja"
		case t == "zh" || strings.HasPrefix(t, "zh-hans") || t == "zh-cn" || t == "zh-sg" || strings.HasPrefix(t, "zh-cn-") || strings.HasPrefix(t, "zh-sg-"):
			return "zh-Hans"
		}
	}
	return "en"
}

// Catalog returns every text of lang, with the source language's for any it lacks.
func Catalog(lang string) map[string]string {
	out := map[string]string{}
	for k, v := range catalogs["en"] {
		out[k] = v
	}
	for k, v := range catalogs[lang] {
		out[k] = v
	}
	return out
}

// Tr says key in lang, with each {name} replaced by its value from args, given as name, value pairs.
func Tr(lang, key string, args ...string) string {
	s, ok := catalogs[lang][key]
	if !ok {
		s, ok = catalogs["en"][key]
	}
	if !ok {
		return key
	}
	for i := 0; i+1 < len(args); i += 2 {
		s = strings.ReplaceAll(s, "{"+args[i]+"}", args[i+1])
	}
	return s
}
