package hubapi

import (
	"encoding/json"
	"strings"
)

// View is a card the assistant draws in its reply (schema/extension/view.schema.json, docs/assistant/assistant-views.md):
// sections of components, a plain-text fallback, and named pages its actions open.
type View struct {
	Title    string          `json:"title,omitempty"`
	Fallback string          `json:"fallback,omitempty"`
	Sections []ViewSection   `json:"sections"`
	Pages    map[string]View `json:"pages,omitempty"`
}

// ViewSection is a group of components with an optional title and footer.
type ViewSection struct {
	Title  string          `json:"title,omitempty"`
	Footer string          `json:"footer,omitempty"`
	Items  []ViewComponent `json:"items"`
}

// ViewComponent is one component; Type says which of its fields mean something.
type ViewComponent struct {
	Type     string       `json:"type"`
	Key      string       `json:"key,omitempty"`
	Text     string       `json:"text,omitempty"`
	Style    string       `json:"style,omitempty"`
	Title    string       `json:"title,omitempty"`
	Subtitle string       `json:"subtitle,omitempty"`
	Label    string       `json:"label,omitempty"`
	Value    string       `json:"value,omitempty"`
	Detail   string       `json:"detail,omitempty"`
	Tone     string       `json:"tone,omitempty"`
	Ratio    float64      `json:"ratio,omitempty"`
	URL      string       `json:"url,omitempty"`
	Caption  string       `json:"caption,omitempty"`
	Language string       `json:"language,omitempty"`
	Path     string       `json:"path,omitempty"`
	Badge    *ViewBadge   `json:"badge,omitempty"`
	Badges   []ViewBadge  `json:"badges,omitempty"`
	Action   *ViewAction  `json:"action,omitempty"`
	Stats    []ViewStat   `json:"stats,omitempty"`
	Columns  []ViewColumn `json:"columns,omitempty"`
	Rows     [][]string   `json:"rows,omitempty"`
	Facts    []ViewFact   `json:"facts,omitempty"`
}

// ViewBadge is a short colored label.
type ViewBadge struct {
	Text string `json:"text"`
	Tone string `json:"tone,omitempty"`
}

// ViewAction is what tapping a component does: exactly one of reply, open, push and pop.
type ViewAction struct {
	ID      string `json:"id"`
	Confirm string `json:"confirm,omitempty"`
	Reply   string `json:"reply,omitempty"`
	Open    string `json:"open,omitempty"`
	Pop     bool   `json:"pop,omitempty"`
	Push    *struct {
		Page  string `json:"page,omitempty"`
		Title string `json:"title,omitempty"`
	} `json:"push,omitempty"`
}

// ViewStat is a cell of a stat grid.
type ViewStat struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Detail string `json:"detail,omitempty"`
	Tone   string `json:"tone,omitempty"`
}

// ViewColumn is a table's column.
type ViewColumn struct {
	Title string `json:"title"`
	Align string `json:"align,omitempty"`
}

// ViewFact is a label and value under a header.
type ViewFact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// TextPart is a stretch of an assistant's text: Markdown, or a persona-view block.
type TextPart struct {
	Markdown string
	// View is the card when the block parsed; Index is the block's place among the text's persona-view blocks.
	View   *View
	Index  int
	IsView bool
	// Open tells a block not closed yet (a reply still being written).
	Open bool
}

// SplitViews cuts an assistant's text into Markdown and persona-view blocks (the shared fence rules:
// backticks or tildes, three or more, any case of the language, a closing fence at least as long).
// Invalid blocks still take an index.
func SplitViews(text string) []TextPart {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	var parts []TextPart
	var md []string
	flush := func() {
		if s := strings.Trim(strings.Join(md, "\n"), "\n"); strings.TrimSpace(s) != "" {
			parts = append(parts, TextPart{Markdown: s})
		}
		md = nil
	}
	lines := strings.Split(text, "\n")
	n := 0
	for i := 0; i < len(lines); i++ {
		ch, width, lang := fence(lines[i])
		if width == 0 {
			md = append(md, lines[i])
			continue
		}
		// find the closing fence
		end := -1
		for j := i + 1; j < len(lines); j++ {
			if c, w, l := fence(lines[j]); w >= width && c == ch && l == "" {
				end = j
				break
			}
		}
		if !strings.EqualFold(lang, "persona-view") {
			// another code block: Markdown as it is, persona-view examples inside it included
			stop := end
			if stop < 0 {
				stop = len(lines) - 1
			}
			md = append(md, lines[i:stop+1]...)
			i = stop
			continue
		}
		flush()
		part := TextPart{IsView: true, Index: n}
		n++
		if end < 0 {
			part.Open = true
			parts = append(parts, part)
			break
		}
		var v View
		if err := json.Unmarshal([]byte(strings.Join(lines[i+1:end], "\n")), &v); err == nil {
			if len(v.Sections) == 0 {
				// the single-component shorthand
				var one struct {
					ViewComponent
					Fallback string `json:"fallback"`
				}
				if json.Unmarshal([]byte(strings.Join(lines[i+1:end], "\n")), &one) == nil && one.Type != "" {
					v.Fallback = one.Fallback
					v.Sections = []ViewSection{{Items: []ViewComponent{one.ViewComponent}}}
				}
			}
			if len(v.Sections) > 0 {
				part.View = &v
			}
		}
		parts = append(parts, part)
		i = end
	}
	flush()
	return parts
}

// fence reads a standalone fence line: its character, its length (0: not a fence) and its language.
func fence(line string) (byte, int, string) {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 || len(t) < 3 || (t[0] != '`' && t[0] != '~') {
		return 0, 0, ""
	}
	ch := t[0]
	w := 0
	for w < len(t) && t[w] == ch {
		w++
	}
	if w < 3 {
		return 0, 0, ""
	}
	lang := strings.TrimSpace(t[w:])
	if ch == '`' && strings.Contains(lang, "`") {
		return 0, 0, ""
	}
	if f := strings.Fields(lang); len(f) > 0 {
		lang = f[0]
	}
	return ch, w, lang
}
