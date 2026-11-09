package hubapi

import (
	"context"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// UsageWindow is one rate-limit window of a subscription account (5h, 7d …), docs/server/usage-windows.md.
type UsageWindow struct {
	Label         string  `json:"label"`
	WindowMinutes int     `json:"window_minutes"`
	UsedPercent   float64 `json:"used_percent"`
	ResetsAt      string  `json:"resets_at,omitempty"`
	// Model is set for an allowance a provider keeps for one model next to the plan's own windows.
	Model string `json:"model,omitempty"`
}

// UsageAccount is an account the machines' agents are signed in to, with its windows.
type UsageAccount struct {
	ID        string        `json:"id"`
	Provider  string        `json:"provider"`
	Name      string        `json:"name"`
	Plan      string        `json:"plan,omitempty"`
	Windows   []UsageWindow `json:"windows"`
	SampledAt string        `json:"sampled_at"`
	Stale     bool          `json:"stale,omitempty"`
	Local     bool          `json:"local,omitempty"`
	Sources   []string      `json:"sources,omitempty"`
	Problem   string        `json:"problem,omitempty"` // unreachable | error
}

// UsageWindows is GET /usage/windows.
func (c *Client) UsageWindows(ctx context.Context) ([]UsageAccount, error) {
	var out struct {
		Accounts []UsageAccount `json:"accounts"`
	}
	err := c.Do(ctx, http.MethodGet, "/usage/windows", nil, &out)
	return out.Accounts, err
}

const (
	// UsageMaxAge drops samples older than this: a source quiet for days says nothing about now.
	UsageMaxAge = 72 * time.Hour
	// UsageWarn is the used percentage from which a bar turns to the caution colour.
	UsageWarn = 80
	paceQuiet = 0.05
	paceSlack = 5
)

var windowLabel = regexp.MustCompile(`(?i)^(\d+(?:\.\d+)?)\s*([mhdw])$`)

// Minutes is the window's length from its label ("5h", "7d"), else its window_minutes.
func (w UsageWindow) Minutes() int {
	if m := windowLabel.FindStringSubmatch(strings.TrimSpace(w.Label)); m != nil {
		n, _ := strconv.ParseFloat(m[1], 64)
		unit := map[string]float64{"m": 1, "h": 60, "d": 1440, "w": 10080}[strings.ToLower(m[2])]
		return int(math.Round(n * unit))
	}
	return max(w.WindowMinutes, 0)
}

// Pace is how use compares with an even spread over the window: over, under or on, and where even use would be (0–100).
// ok is false without a reset time or length, and in the window's first 5%.
func (w UsageWindow) Pace(now time.Time) (state string, expected float64, ok bool) {
	total := time.Duration(w.Minutes()) * time.Minute
	resets := ParseTime(w.ResetsAt)
	if total <= 0 || resets.IsZero() {
		return "", 0, false
	}
	elapsed := total - resets.Sub(now)
	if elapsed < 0 || elapsed > total || float64(elapsed)/float64(total) < paceQuiet {
		return "", 0, false
	}
	expected = float64(elapsed) / float64(total) * 100
	switch diff := w.UsedPercent - expected; {
	case diff > paceSlack:
		state = "over"
	case diff < -paceSlack:
		state = "under"
	default:
		state = "on"
	}
	return state, expected, true
}

// LiveUsage keeps samples within three days, windows that have not reset, and accounts that still have one.
func LiveUsage(accounts []UsageAccount, now time.Time) []UsageAccount {
	var out []UsageAccount
	for _, a := range accounts {
		if t := ParseTime(a.SampledAt); !t.IsZero() && now.Sub(t) > UsageMaxAge {
			continue
		}
		var ws []UsageWindow
		for _, w := range a.Windows {
			if t := ParseTime(w.ResetsAt); w.ResetsAt == "" || t.IsZero() || t.After(now) {
				ws = append(ws, w)
			}
		}
		if len(ws) > 0 {
			a.Windows = ws
			out = append(out, a)
		}
	}
	return out
}

// Fullest is the used percentage of the account's fullest window; a per-model allowance counts only without plan windows.
func (a UsageAccount) Fullest() float64 {
	var plan, all float64
	hasPlan := false
	for _, w := range a.Windows {
		all = max(all, w.UsedPercent)
		if w.Model == "" {
			hasPlan, plan = true, max(plan, w.UsedPercent)
		}
	}
	if hasPlan {
		return plan
	}
	return all
}

// PlanNote is the plan to write beside the name, unless the name already says it ("Codex Pro", plan "pro").
func (a UsageAccount) PlanNote() string {
	plan := strings.TrimSpace(a.Plan)
	if plan == "" || strings.Contains(strings.ToLower(a.Name), strings.ToLower(plan)) {
		return ""
	}
	return plan
}

// TopUsage is what the sidebar draws: the n accounts with the fullest window, stale ones last.
func TopUsage(accounts []UsageAccount, n int) []UsageAccount {
	s := append([]UsageAccount(nil), accounts...)
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].Stale != s[j].Stale {
			return !s[i].Stale
		}
		return s[i].Fullest() > s[j].Fullest()
	})
	return s[:min(n, len(s))]
}
