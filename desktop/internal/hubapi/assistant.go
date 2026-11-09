package hubapi

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Settings › Persona and the hub-wide settings beside it: what the assistant does on its own, what it knows about you
// (About me, its memory files, its skills), the session triage switches and the model profiles.

// AssistantSettings is GET /assistant's settings; a PUT changes only the fields it sends.
type AssistantSettings struct {
	DefaultPermissionMode string `json:"default_permission_mode,omitempty"`
	MachineID             string `json:"machine_id,omitempty"`
	Launcher              string `json:"launcher,omitempty"`
	Cwd                   string `json:"cwd,omitempty"`
	Timezone              string `json:"timezone,omitempty"`
	Notes                 string `json:"notes,omitempty"`
	NotesEffective        string `json:"notes_effective,omitempty"`
	MainTasksQuiet        *bool  `json:"main_tasks_quiet,omitempty"`
	StartRequestedTasks   *bool  `json:"start_requested_tasks,omitempty"`
}

// On is a switch that is on unless the hub says off.
func On(b *bool) bool { return b == nil || *b }

// AssistantSettings reads the assistant's settings.
func (c *Client) AssistantSettings(ctx context.Context) (AssistantSettings, error) {
	var out AssistantSettings
	err := c.Do(ctx, http.MethodGet, "/assistant", nil, &out)
	return out, err
}

// SetAssistant changes the fields patch names and answers the settings as they are now.
func (c *Client) SetAssistant(ctx context.Context, patch map[string]any) (AssistantSettings, error) {
	var out AssistantSettings
	err := c.Do(ctx, http.MethodPut, "/assistant", patch, &out)
	return out, err
}

// MemoryIndex is the memory's table of contents.
const MemoryIndex = "MEMORY.md"

// MemoryFile is one of the assistant's memory files.
type MemoryFile struct {
	Name      string `json:"name"`
	Content   string `json:"content"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// Memory lists the assistant's memory files.
func (c *Client) Memory(ctx context.Context) ([]MemoryFile, error) {
	var out struct {
		Files []MemoryFile `json:"files"`
	}
	err := c.Do(ctx, http.MethodGet, "/assistant/memory", nil, &out)
	return out.Files, err
}

// SaveMemory writes a memory file whole.
func (c *Client) SaveMemory(ctx context.Context, name, content string) error {
	return c.Do(ctx, http.MethodPut, "/assistant/memory/"+url.PathEscape(name), map[string]string{"content": content}, nil)
}

// DeleteMemory forgets a memory.
func (c *Client) DeleteMemory(ctx context.Context, name string) error {
	return c.Do(ctx, http.MethodDelete, "/assistant/memory/"+url.PathEscape(name), nil, nil)
}

// MemoryEntry is a memory as listed: its file, title, type, line about it and body.
type MemoryEntry struct {
	File                      MemoryFile
	Title, Type, Detail, Body string
}

var indexLine = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)(.*)$`)

// MemoryEntries are the memories (not the index), newest first, titled from the index, the frontmatter or the name.
func MemoryEntries(files []MemoryFile) []MemoryEntry {
	titles := map[string][2]string{}
	for _, f := range files {
		if f.Name != MemoryIndex {
			continue
		}
		for _, l := range strings.Split(f.Content, "\n") {
			if m := indexLine.FindStringSubmatch(l); m != nil {
				hook := strings.TrimSpace(strings.TrimLeft(m[3], " \t—–-:：")) // i18n-ignore: separators written in the index
				titles[m[2]] = [2]string{strings.TrimSpace(m[1]), hook}
			}
		}
	}
	var out []MemoryEntry
	for _, f := range files {
		if f.Name == MemoryIndex {
			continue
		}
		front, body := frontmatter(f.Content)
		e := MemoryEntry{File: f, Title: f.Name, Type: front["type"], Detail: front["description"], Body: body}
		if t := titles[f.Name]; t[0] != "" {
			e.Title = t[0]
		} else if front["name"] != "" {
			e.Title = front["name"]
		}
		if e.Detail == "" {
			e.Detail = titles[f.Name][1]
		}
		out = append(out, e)
	}
	at := func(e MemoryEntry) time.Time { t, _ := time.Parse(time.RFC3339, e.File.UpdatedAt); return t }
	sort.SliceStable(out, func(i, j int) bool {
		if ti, tj := at(out[i]), at(out[j]); !ti.Equal(tj) {
			return ti.After(tj)
		}
		return out[i].File.Name < out[j].File.Name
	})
	return out
}

// frontmatter splits a memory file into its name / description / type and its body.
func frontmatter(content string) (map[string]string, string) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	out := map[string]string{}
	if strings.TrimSpace(lines[0]) != "---" {
		return out, strings.TrimSpace(content)
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return out, strings.TrimSpace(content)
	}
	for _, l := range lines[1:end] {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		if v != "" && (k == "name" || k == "description" || k == "type") {
			out[k] = v
		}
	}
	return out, strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
}

// Skill is one of the assistant's skills.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"`
}

// Skills lists the assistant's skills and the computer they are on.
func (c *Client) Skills(ctx context.Context) ([]Skill, string, error) {
	var out struct {
		MachineID string  `json:"machine_id"`
		Machine   string  `json:"machine"`
		Skills    []Skill `json:"skills"`
	}
	err := c.Do(ctx, http.MethodGet, "/skills", nil, &out)
	if out.Machine == "" {
		out.Machine = out.MachineID
	}
	return out.Skills, out.Machine, err
}

// Triage is the session triage's switches (GET / PUT /agents/triage), written as a whole set.
type Triage struct {
	Enabled       bool  `json:"enabled"`
	AutoContinue  bool  `json:"auto_continue"`
	Wake          bool  `json:"wake"`
	Decide        bool  `json:"decide"`
	ToolLoop      bool  `json:"tool_loop"`
	ReplyReminder *bool `json:"reply_reminder,omitempty"`
	ReminderMins  int   `json:"reply_reminder_minutes,omitempty"`
}

// Triage reads the switches.
func (c *Client) Triage(ctx context.Context) (Triage, error) {
	var out Triage
	err := c.Do(ctx, http.MethodGet, "/agents/triage", nil, &out)
	return out, err
}

// SetTriage writes the switches and answers them as saved.
func (c *Client) SetTriage(ctx context.Context, t Triage) (Triage, error) {
	var out Triage
	err := c.Do(ctx, http.MethodPut, "/agents/triage", t, &out)
	return out, err
}

// ModelProfile is a set of model choices applied at once, with its Agent presets.
type ModelProfile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Tiers       map[string]struct {
		Launcher    string `json:"launcher"`
		Model       string `json:"model,omitempty"`
		Effort      string `json:"effort,omitempty"`
		Description string `json:"description,omitempty"`
	} `json:"tiers,omitempty"`
	Active  bool     `json:"active,omitempty"`
	Changes []string `json:"changes"`
}

// ModelProfiles is GET /model-profiles.
type ModelProfiles struct {
	Active   string         `json:"active,omitempty"`
	Profiles []ModelProfile `json:"profiles"`
	Rollback *struct {
		To      string   `json:"to,omitempty"`
		Changes []string `json:"changes"`
	} `json:"rollback,omitempty"`
}

// ModelProfiles reads the profiles.
func (c *Client) ModelProfiles(ctx context.Context) (ModelProfiles, error) {
	var out ModelProfiles
	err := c.Do(ctx, http.MethodGet, "/model-profiles", nil, &out)
	return out, err
}

// ApplyModelProfile switches to a profile, or back to what was before when id is "".
func (c *Client) ApplyModelProfile(ctx context.Context, id string) (ModelProfiles, error) {
	path := "/model-profiles/rollback"
	if id != "" {
		path = "/model-profiles/" + url.PathEscape(id) + "/apply"
	}
	var out ModelProfiles
	err := c.Do(ctx, http.MethodPost, path, map[string]any{}, &out)
	return out, err
}
