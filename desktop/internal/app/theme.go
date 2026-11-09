package app

import (
	"embed"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// palette is the web app's colors (web/src/styles.css), so the desktop app looks like the other clients.
type palette struct {
	page, surface, inset, hover, line, ink, ink2, ink3, accent, green, orange, red, attention, ai, userBubble, field ui.Color
}

var (
	light = palette{
		page: ui.Hex("#fafafb"), surface: ui.Hex("#ffffff"), inset: ui.Hex("#f7f8f9"), hover: ui.Hex("#f4f5f6"),
		line: ui.Hex("#ecedef"), ink: ui.Hex("#1f2124"), ink2: ui.Hex("#62656b"), ink3: ui.Hex("#66696f"),
		accent: ui.Hex("#0285ff"), green: ui.Hex("#107638"), orange: ui.Hex("#a64c00"), red: ui.Hex("#bd3038"),
		attention: ui.Hex("#8f5c00"), ai: ui.Hex("#5b4fd6"), userBubble: ui.Hex("#dceaff"), field: ui.Hex("#f2f2f3"),
	}
	dark = palette{
		page: ui.Hex("#17181a"), surface: ui.Hex("#232427"), inset: ui.Hex("#1f2022"), hover: ui.Hex("#2a2b2e"),
		line: ui.Hex("#2e3033"), ink: ui.Hex("#f2f3f4"), ink2: ui.Hex("#a5a8ad"), ink3: ui.Hex("#979aa0"),
		accent: ui.Hex("#3d9aff"), green: ui.Hex("#6dd697"), orange: ui.Hex("#eea66d"), red: ui.Hex("#ff9499"),
		attention: ui.Hex("#f1b969"), ai: ui.Hex("#9c94ff"), userBubble: ui.Hex("#1d3557"), field: ui.Hex("#2b2c2f"),
	}
)

// Sizes of the layout, in DIPs.
const (
	sidebarWidth = 220
	listWidth    = 340
	pagePad      = 24
	rowRadius    = 8
	cardRadius   = 12
	small        = 12
	body         = 14
	title        = 20
	heading      = 15
)

func (a *App) colors(c *ui.Context) palette {
	if a.dark(c) {
		return dark
	}
	return light
}

func (a *App) appearance() string {
	if a.Hubs == nil {
		return ""
	}
	return a.Hubs.Settings.Appearance
}

// dark tells the dark palette: chosen in Settings, or the system's.
func (a *App) dark(c *ui.Context) bool {
	switch a.appearance() {
	case "light":
		return false
	case "dark":
		return true
	}
	return c.Theme().Dark
}

// theme sets the window's theme from the palette, following the system's light or dark.
func (a *App) theme(c *ui.Context) palette {
	p := a.colors(c)
	t := *c.Theme()
	switch a.appearance() {
	case "light":
		t = *ui.LightTheme()
	case "dark":
		t = *ui.DarkTheme()
	}
	t.Background, t.Surface, t.SurfaceHover = p.page, p.surface, p.hover
	t.Border, t.Text, t.TextMuted = p.line, p.ink, p.ink2
	t.Accent, t.Danger, t.Success, t.Warning = p.accent, p.red, p.green, p.orange
	t.FontSize, t.Radius = body, rowRadius
	c.SetTheme(&t)
	return p
}

// stateColor is an agent state's color, as the web app's status marks.
func (p palette) stateColor(state string) ui.Color {
	switch state {
	case hubapi.StateNeedsYou:
		return p.attention
	case hubapi.StateWorking:
		return p.orange
	case hubapi.StateUnread:
		return p.green
	}
	return p.ink3
}

//go:embed icons/*.svg
var iconFiles embed.FS

var icons = map[string]*ui.SVG{}

func icon(name string) *ui.SVG {
	if s, ok := icons[name]; ok {
		return s
	}
	data, err := iconFiles.ReadFile("icons/" + name + ".svg")
	if err != nil {
		panic("no icon " + name)
	}
	s := ui.MustParseSVG(data)
	icons[name] = s
	return s
}

// ago is a short time since t: "now", "5m", "3h", "2d", or a date.
func (a *App) ago(at string, now time.Time) string {
	t := hubapi.ParseTime(at)
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return a.tr("agoNow")
	case d < time.Hour:
		return a.tr("agoMinutes", "count", itoa(int(d/time.Minute)))
	case d < 24*time.Hour:
		return a.tr("agoHours", "count", itoa(int(d/time.Hour)))
	case d < 7*24*time.Hour:
		return a.tr("agoDays", "count", itoa(int(d/(24*time.Hour))))
	}
	return t.Local().Format("2006-01-02")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// oneLine is the first line of s, cut to n runes.
func oneLine(s string, n int) string {
	for i, r := range s {
		if r == '\n' {
			s = s[:i]
			break
		}
	}
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
