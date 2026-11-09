package hubapi

import (
	"context"
	"net/http"
	"net/url"
)

// A connector's thread (GET /connectors/{id}/thread): a chat app's conversation, or a GitHub pull request or issue as
// its timeline, with what answers in it.

// ThreadPerson is who wrote a message.
type ThreadPerson struct {
	ID     string `json:"id,omitempty"`
	Name   string `json:"name"`
	Avatar string `json:"avatar,omitempty"`
}

// ThreadMedia is a picture, sticker, video, voice note or file in a message.
type ThreadMedia struct {
	Kind     string `json:"kind"`
	Ref      string `json:"ref,omitempty"`
	Name     string `json:"name,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Duration int    `json:"duration,omitempty"`
	Emoji    string `json:"emoji,omitempty"`
}

// ThreadMessage is one message, comment, review, line comment or check.
type ThreadMessage struct {
	ID      string        `json:"id"`
	At      string        `json:"at"`
	Sender  ThreadPerson  `json:"sender"`
	Mine    bool          `json:"mine,omitempty"`
	Event   bool          `json:"event,omitempty"`
	Text    string        `json:"text,omitempty"`
	Format  string        `json:"format,omitempty"`
	Media   []ThreadMedia `json:"media,omitempty"`
	ReplyTo *struct {
		ID   string `json:"id"`
		From string `json:"from,omitempty"`
		Text string `json:"text,omitempty"`
	} `json:"reply_to,omitempty"`
	Edited bool   `json:"edited,omitempty"`
	URL    string `json:"url,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Review string `json:"review,omitempty"`
	Code   *struct {
		Path string `json:"path"`
		Line int    `json:"line,omitempty"`
		Hunk string `json:"hunk,omitempty"`
	} `json:"code,omitempty"`
	Check *struct {
		Name  string `json:"name"`
		State string `json:"state"`
		URL   string `json:"url,omitempty"`
		Log   string `json:"log,omitempty"`
	} `json:"check,omitempty"`
}

// ThreadChat is the thread itself.
type ThreadChat struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	URL     string `json:"url,omitempty"`
	Members int    `json:"members,omitempty"`
	// Reply is the action that answers here; "": it can't be answered.
	Reply  string        `json:"reply,omitempty"`
	State  string        `json:"state,omitempty"`
	Author *ThreadPerson `json:"author,omitempty"`
	Labels []struct {
		Name  string `json:"name"`
		Color string `json:"color,omitempty"`
	} `json:"labels,omitempty"`
	Branch *struct {
		Head string `json:"head"`
		Base string `json:"base"`
	} `json:"branch,omitempty"`
	Body       string `json:"body,omitempty"`
	BodyFormat string `json:"body_format,omitempty"`
}

// GitHub tells a pull request or an issue.
func (c ThreadChat) GitHub() bool { return c.Kind == "pr" || c.Kind == "issue" }

// ThreadPage is a page of a thread, oldest first; Before reads the page before it.
type ThreadPage struct {
	Chat     ThreadChat      `json:"chat"`
	Messages []ThreadMessage `json:"messages"`
	Before   string          `json:"before,omitempty"`
}

// Thread reads a connector's thread, the page before `before` when it isn't "".
func (c *Client) Thread(ctx context.Context, connector, thread, before string) (ThreadPage, error) {
	q := url.Values{"thread": {thread}}
	if before != "" {
		q.Set("before", before)
	}
	var out ThreadPage
	err := c.Do(ctx, http.MethodGet, "/connectors/"+url.PathEscape(connector)+"/thread?"+q.Encode(), nil, &out)
	return out, err
}

// ThreadReply answers in a thread with its reply action, quoting replyTo when it isn't "".
func (c *Client) ThreadReply(ctx context.Context, connector, thread, action, text, replyTo string) error {
	body := map[string]string{"thread": thread, "action": action, "text": text}
	if replyTo != "" {
		body["reply_to"] = replyTo
	}
	return c.Do(ctx, http.MethodPost, "/connectors/"+url.PathEscape(connector)+"/thread/reply", body, nil)
}

// OlderThread puts an older page before what is shown, one copy of each message.
func OlderThread(have, older ThreadPage) ThreadPage {
	ids := map[string]bool{}
	for _, m := range have.Messages {
		ids[m.ID] = true
	}
	var msgs []ThreadMessage
	for _, m := range older.Messages {
		if !ids[m.ID] {
			msgs = append(msgs, m)
		}
	}
	return ThreadPage{Chat: have.Chat, Messages: append(msgs, have.Messages...), Before: older.Before}
}

// NewestThread puts the newest page after what is shown and kept from older pages.
func NewestThread(have, newest ThreadPage) ThreadPage {
	if len(newest.Messages) == 0 {
		return ThreadPage{Chat: newest.Chat, Messages: have.Messages, Before: have.Before}
	}
	first := newest.Messages[0].ID
	for i, m := range have.Messages {
		if m.ID == first {
			return ThreadPage{Chat: newest.Chat, Messages: append(append([]ThreadMessage{}, have.Messages[:i]...), newest.Messages...), Before: have.Before}
		}
	}
	return newest
}
