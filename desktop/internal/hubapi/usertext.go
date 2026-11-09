package hubapi

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"
)

// Source is who wrote a message the runtime counts as the user's when it wasn't the user: another of Persona's
// conversations, a scheduled run, a follow-up or supervision (docs/protocol/protocol.md "Message source").
type Source struct {
	Kind           string `json:"kind"`
	ConversationID string `json:"conversation_id,omitempty"`
	Title          string `json:"title,omitempty"`
}

// Context is a <persona-context> block a message carries: something chosen to go with it, as Ask Persona's entry.
type Context struct {
	Kind, Title, URL, Text string
}

// File is a <persona-file> tag: a file sent to the agent's computer with the message.
type File struct {
	Kind, Path, Name string
}

// UserText is a user message as people read it: its words, apart from the blocks around them.
type UserText struct {
	Text     string
	Contexts []Context
	Files    []File
	Photos   []string
}

var (
	contextRE  = regexp.MustCompile(`(?s)<persona-context\b([^>]*)>(.*?)</persona-context>`)
	leadingRE  = regexp.MustCompile(`(?s)^\s*<persona-context\b([^>]*)>(.*?)</persona-context>\s*`)
	fileRE     = regexp.MustCompile(`(?s)<persona-file\b([^>]*?)/?>(?:\s*</persona-file>)?`)
	attrRE     = regexp.MustCompile(`(\w+)="([^"]*)"`)
	pastedRE   = regexp.MustCompile(`(?s)<pasted_content\b[^>]*>(.*?)</pasted_content>`)
	imagesRE   = regexp.MustCompile(`(?s)<attached_images>(.*?)</attached_images>`)
	imageRefRE = regexp.MustCompile(`\[Image #\d+\]\s?`)
)

func attrs(s string) map[string]string {
	out := map[string]string{}
	for _, m := range attrRE.FindAllStringSubmatch(s, -1) {
		out[m[1]] = html.UnescapeString(m[2])
	}
	return out
}

// ParseUserText takes the context blocks, file tags and attached photos out of a user message, as the web app does.
func ParseUserText(raw string) UserText {
	var u UserText
	text := contextRE.ReplaceAllStringFunc(raw, func(m string) string {
		sub := contextRE.FindStringSubmatch(m)
		a := attrs(sub[1])
		kind := a["kind"]
		if kind == "" {
			kind = "context"
		}
		u.Contexts = append(u.Contexts, Context{Kind: kind, Title: a["title"], URL: a["url"], Text: strings.TrimSpace(sub[2])})
		return ""
	})
	text = fileRE.ReplaceAllStringFunc(text, func(m string) string {
		a := attrs(fileRE.FindStringSubmatch(m)[1])
		if a["path"] == "" {
			return ""
		}
		name := a["name"]
		if name == "" {
			name = a["path"][strings.LastIndex(a["path"], "/")+1:]
		}
		kind := a["kind"]
		if kind == "" {
			kind = "file"
		}
		u.Files = append(u.Files, File{Kind: kind, Path: a["path"], Name: name})
		return ""
	})
	text = pastedRE.ReplaceAllString(text, "$1")
	text = imagesRE.ReplaceAllStringFunc(text, func(m string) string {
		for _, line := range strings.Split(imagesRE.FindStringSubmatch(m)[1], "\n") {
			p := strings.TrimSpace(line)
			if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") {
				u.Photos = append(u.Photos, p)
			}
		}
		return ""
	})
	u.Text = strings.TrimSpace(imageRefRE.ReplaceAllString(text, ""))
	return u
}

// MessageSource is who wrote a user-role message that isn't the user's: the hub's explicit source, then the
// message-source block the manager keeps in the transcript, then what the injected line says. Nil for the user's own.
func (it Item) MessageSource() *Source {
	if it.Source != nil && strings.TrimSpace(it.Source.Kind) != "" {
		return it.Source
	}
	if it.ClientMsg != "" || it.From != "" {
		return nil
	}
	rest := it.Text
	for m := leadingRE.FindStringSubmatch(rest); m != nil; m = leadingRE.FindStringSubmatch(rest) {
		rest = rest[len(m[0]):]
		if attrs(m[1])["kind"] != "message-source" {
			continue
		}
		// the first such block decides, and a malformed one or one saying the user wrote it means the user's
		var meta struct {
			Kind           string `json:"kind"`
			Author         string `json:"author"`
			ConversationID string `json:"conversation_id"`
			Title          string `json:"title"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(m[2])), &meta) != nil || meta.Author == "user" ||
			strings.TrimSpace(meta.Kind) == "" {
			return nil
		}
		return &Source{Kind: meta.Kind, ConversationID: meta.ConversationID, Title: meta.Title}
	}
	if in := it.Injected; in != nil {
		switch {
		case in.State == "supervise":
			return &Source{Kind: "supervision", Title: in.Title}
		case in.Kind == "followup" && (in.State == "daily" || in.State == "interval"):
			return &Source{Kind: "patrol", Title: in.Title}
		case in.Kind == "followup":
			return &Source{Kind: "followup", Title: in.Title}
		}
	}
	return nil
}
