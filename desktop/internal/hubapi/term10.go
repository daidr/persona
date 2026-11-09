package hubapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// FeatureTerminalPanes is the hello feature of a hub that lists every computer's terminal panes.
const FeatureTerminalPanes = "terminal_panes"

// ScreenANSI reads the terminal's screen with its colors (SGR sequences kept); a hub that has no colors sends plain text.
func (c *Client) ScreenANSI(ctx context.Context, machine, session string, lines int) (Screen, error) {
	var out Screen
	err := c.Do(ctx, http.MethodGet, sessionPath(machine, session)+"/screen?source=visible&format=ansi&lines="+strconv.Itoa(lines), nil, &out)
	return out, err
}

// ScreenPage is a page of the scrollback above what is shown.
type ScreenPage struct {
	Text    string `json:"text"`
	HasMore bool   `json:"has_more"`
	AtLimit bool   `json:"at_limit"`
}

// ScreenBefore reads the page of scrollback just above anchor (the first plain lines already shown).
func (c *Client) ScreenBefore(ctx context.Context, machine, session string, anchor []string, lines int) (ScreenPage, error) {
	var out ScreenPage
	body := map[string]any{"anchor": anchor, "lines": lines, "format": "ansi"}
	err := c.Do(ctx, http.MethodPost, sessionPath(machine, session)+"/screen/before", body, &out)
	return out, err
}

// Pane is one terminal pane of a computer: an agent's, or a plain shell.
type Pane struct {
	Pane        string `json:"pane"`
	Terminal    string `json:"terminal"`
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Agent       string `json:"agent,omitempty"`
	Status      string `json:"status,omitempty"`
	Title       string `json:"title,omitempty"`
	Cwd         string `json:"cwd,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
}

// PaneSpace is a workspace of a terminal host, PaneTab a tab in one.
type PaneSpace struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Cwd    string `json:"cwd,omitempty"`
	Branch string `json:"branch,omitempty"`
}

type PaneTab struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
}

// PaneHost is a terminal host of a computer (herdr, Orca…) with its workspaces, tabs and panes.
type PaneHost struct {
	Kind       string      `json:"kind"`
	Workspaces []PaneSpace `json:"workspaces"`
	Tabs       []PaneTab   `json:"tabs"`
	Panes      []Pane      `json:"panes"`
}

// Panes lists a computer's terminal panes.
func (c *Client) Panes(ctx context.Context, machine string) ([]PaneHost, error) {
	var out struct {
		Hosts []PaneHost `json:"hosts"`
	}
	err := c.Do(ctx, http.MethodGet, "/machines/"+url.PathEscape(machine)+"/panes", nil, &out)
	return out.Hosts, err
}

// SGR is a run of a terminal line in one style: its text, and the colors as palette indexes (0–255) or 24-bit values
// (FGRGB / BGRGB set), -1 the default.
type SGR struct {
	Text                          string
	FG, BG                        int
	FGRGB, BGRGB                  bool
	Bold, Dim, Italic, Under, Rev bool
}

// ParseSGR splits a line written with ANSI sequences into styled runs; sequences other than colors and emphasis
// (cursor moves, OSC titles and links) are dropped.
func ParseSGR(line string) []SGR {
	cur := SGR{FG: -1, BG: -1}
	var out []SGR
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			r := cur
			r.Text = b.String()
			out = append(out, r)
			b.Reset()
		}
	}
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if ch != 0x1b {
			if ch == '\r' {
				continue
			}
			b.WriteByte(ch)
			continue
		}
		if i+1 >= len(line) {
			break
		}
		switch line[i+1] {
		case '[':
			j := i + 2
			for j < len(line) && (line[j] < 0x40 || line[j] > 0x7e) {
				j++
			}
			if j >= len(line) {
				i = len(line)
				continue
			}
			if line[j] == 'm' {
				flush()
				applySGR(&cur, line[i+2:j])
			}
			i = j
		case ']':
			// OSC: up to BEL or ESC \
			j := i + 2
			for j < len(line) && line[j] != 0x07 && !(line[j] == 0x1b && j+1 < len(line) && line[j+1] == '\\') {
				j++
			}
			if j < len(line) && line[j] == 0x1b {
				j++
			}
			i = j
		default:
			i++
		}
	}
	flush()
	return out
}

func applySGR(s *SGR, params string) {
	if params == "" {
		params = "0"
	}
	ps := strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' })
	num := func(k int) int {
		if k >= len(ps) {
			return 0
		}
		n, _ := strconv.Atoi(ps[k])
		return n
	}
	for k := 0; k < len(ps); k++ {
		n := num(k)
		switch {
		case n == 0:
			*s = SGR{FG: -1, BG: -1}
		case n == 1:
			s.Bold = true
		case n == 2:
			s.Dim = true
		case n == 3:
			s.Italic = true
		case n == 4:
			s.Under = true
		case n == 7:
			s.Rev = true
		case n == 22:
			s.Bold, s.Dim = false, false
		case n == 23:
			s.Italic = false
		case n == 24:
			s.Under = false
		case n == 27:
			s.Rev = false
		case n >= 30 && n <= 37:
			s.FG, s.FGRGB = n-30, false
		case n >= 90 && n <= 97:
			s.FG, s.FGRGB = n-90+8, false
		case n == 39:
			s.FG, s.FGRGB = -1, false
		case n >= 40 && n <= 47:
			s.BG, s.BGRGB = n-40, false
		case n >= 100 && n <= 107:
			s.BG, s.BGRGB = n-100+8, false
		case n == 49:
			s.BG, s.BGRGB = -1, false
		case n == 38 || n == 48:
			var v int
			var rgb bool
			switch num(k + 1) {
			case 5:
				v, k = num(k+2), k+2
			case 2:
				v, rgb, k = num(k+2)<<16|num(k+3)<<8|num(k+4), true, k+4
			default:
				continue
			}
			if n == 38 {
				s.FG, s.FGRGB = v, rgb
			} else {
				s.BG, s.BGRGB = v, rgb
			}
		}
	}
}

// PlainLine is a line without its sequences, trailing spaces trimmed: what the scrollback is found by.
func PlainLine(line string) string {
	var b strings.Builder
	for _, r := range ParseSGR(line) {
		b.WriteString(r.Text)
	}
	return strings.TrimRight(b.String(), " \t")
}
