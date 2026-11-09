package hubapi

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Mail beyond the Inbox's own reply (docs/extensions/mail.md): a mail's whole message, writing a new one or answering it
// by hand, and a connector's thread (GitHub, a chat app) as the hub reads it.

// InboxMail is a mail entry's place in its mailbox.
type InboxMail struct {
	AccountID string       `json:"account_id"`
	Mailbox   string       `json:"mailbox,omitempty"`
	UIDs      []int        `json:"uids"`
	From      *MailAddress `json:"from,omitempty"`
}

// UID is the thread's newest mail.
func (m InboxMail) UID() int {
	if len(m.UIDs) == 0 {
		return 0
	}
	return m.UIDs[len(m.UIDs)-1]
}

// Box is the mailbox, INBOX when the entry doesn't say.
func (m InboxMail) Box() string {
	if m.Mailbox == "" {
		return "INBOX"
	}
	return m.Mailbox
}

// MailAddress is a name and an address.
type MailAddress struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

// String is "Name <a@b>", or whichever of them there is.
func (a MailAddress) String() string {
	if a.Name != "" && a.Email != "" {
		return a.Name + " <" + a.Email + ">"
	}
	if a.Email != "" {
		return a.Email
	}
	return a.Name
}

// MailAttachment is a file a mail carries.
type MailAttachment struct {
	ID       string `json:"id,omitempty"`
	Filename string `json:"filename,omitempty"`
	MIME     string `json:"mime,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// MailMessage is one mail (GET /mail/messages/{account}/{uid}).
type MailMessage struct {
	UID         int              `json:"uid"`
	MessageID   string           `json:"message_id,omitempty"`
	Subject     string           `json:"subject,omitempty"`
	From        *MailAddress     `json:"from,omitempty"`
	To          []MailAddress    `json:"to,omitempty"`
	Cc          []MailAddress    `json:"cc,omitempty"`
	Date        string           `json:"date,omitempty"`
	Text        string           `json:"text,omitempty"`
	HTML        string           `json:"html,omitempty"`
	Seen        bool             `json:"seen,omitempty"`
	References  []string         `json:"references,omitempty"`
	Attachments []MailAttachment `json:"attachments,omitempty"`
}

// MailAccount is one of the hub's mail accounts.
type MailAccount struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Email    string `json:"email"`
	Provider string `json:"provider,omitempty"` // gmail | outlook | feishu | imap
	Auth     string `json:"auth,omitempty"`     // password | oauth2
	IMAPHost string `json:"imap_host,omitempty"`
	Status   string `json:"status,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Label is "Name · address", or the address.
func (a MailAccount) Label() string {
	if a.Name != "" && a.Name != a.Email {
		return a.Name + " · " + a.Email
	}
	return a.Email
}

// MailAccounts lists the hub's mail accounts.
func (c *Client) MailAccounts(ctx context.Context) ([]MailAccount, error) {
	var out struct {
		Accounts []MailAccount `json:"accounts"`
	}
	err := c.Do(ctx, http.MethodGet, "/mail/accounts", nil, &out)
	return out.Accounts, err
}

func mailPath(account string, uid int) string {
	return "/mail/messages/" + url.PathEscape(account) + "/" + strconv.Itoa(uid)
}

// Mail reads one mail.
func (c *Client) Mail(ctx context.Context, account string, uid int, mailbox string) (MailMessage, error) {
	var out struct {
		Message MailMessage `json:"message"`
	}
	err := c.Do(ctx, http.MethodGet, mailPath(account, uid)+"?mailbox="+url.QueryEscape(mailbox), nil, &out)
	return out.Message, err
}

// MarkMailSeen marks a mail read in its mailbox.
func (c *Client) MarkMailSeen(ctx context.Context, account string, uid int, mailbox string) error {
	return c.Do(ctx, http.MethodPost, mailPath(account, uid)+"/flags", map[string]any{"mailbox": mailbox, "seen": true}, nil)
}

// MailAttachmentPath is where an attachment downloads from.
func MailAttachmentPath(account string, uid int, mailbox, id string) string {
	return mailPath(account, uid) + "/attachments/" + url.PathEscape(id) + "?mailbox=" + url.QueryEscape(mailbox)
}

// MailDraft is a mail being written.
type MailDraft struct {
	AccountID     string
	To, Cc, Bcc   string
	Subject, Body string
	InReplyTo     string
	References    []string
}

var addressSeps = regexp.MustCompile("[,，;；\n]") // i18n-ignore: full-width separators people type
var namedAddress = regexp.MustCompile(`^(.*?)\s*<([^<>\s]+)>$`)
var emailShape = regexp.MustCompile(`^[^\s@<>]+@[^\s@<>]+\.[^\s@<>]+$`)

// ParseAddresses reads a field of addresses: separated by commas, semicolons (either width) or new lines; "Name <a@b>"
// keeps the name.
func ParseAddresses(s string) []MailAddress {
	var out []MailAddress
	for _, x := range addressSeps.Split(s, -1) {
		x = strings.TrimSpace(x)
		if x == "" {
			continue
		}
		if m := namedAddress.FindStringSubmatch(x); m != nil {
			out = append(out, MailAddress{Name: strings.TrimSpace(strings.Trim(m[1], `"`)), Email: m[2]})
			continue
		}
		out = append(out, MailAddress{Email: x})
	}
	return out
}

// BadAddresses are the addresses in a field that don't look like one.
func BadAddresses(s string) []string {
	var out []string
	for _, a := range ParseAddresses(s) {
		if !emailShape.MatchString(a.Email) {
			out = append(out, a.Email)
		}
	}
	return out
}

// ReplyDraft answers msg from the account it came to: Re: subject, the thread's ids, the first 40 lines quoted under
// head (the line naming when and who).
func ReplyDraft(account string, msg MailMessage, head string) MailDraft {
	subject := msg.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	lines := strings.Split(msg.Text, "\n")
	if len(lines) > 40 {
		lines = lines[:40]
	}
	for i, l := range lines {
		lines[i] = "> " + l
	}
	d := MailDraft{AccountID: account, Subject: subject, Body: "\n\n" + head + "\n" + strings.Join(lines, "\n"), InReplyTo: msg.MessageID}
	if msg.From != nil {
		d.To = msg.From.Email
	}
	d.References = append(append([]string{}, msg.References...), msg.MessageID)
	if msg.MessageID == "" {
		d.References = d.References[:len(d.References)-1]
	}
	return d
}

func bareEmail(s string) string {
	s = strings.TrimSpace(s)
	if !emailShape.MatchString(s) || strings.ContainsAny(s, ",;，；") {
		return ""
	}
	return s
}

// ReplyAllDraft answers everyone visible on msg once each, never mine; ok is false without mine (it would include
// itself). A mail from me goes to its other recipients; when only Cc is left, its first one moves to To.
func ReplyAllDraft(account string, msg MailMessage, mine, head string) (MailDraft, bool) {
	me := bareEmail(mine)
	if me == "" {
		return MailDraft{}, false
	}
	seen := map[string]bool{strings.ToLower(me): true}
	keep := func(list []MailAddress) []string {
		var out []string
		for _, a := range list {
			e := bareEmail(a.Email)
			if e == "" || seen[strings.ToLower(e)] {
				continue
			}
			seen[strings.ToLower(e)] = true
			out = append(out, e)
		}
		return out
	}
	var from []MailAddress
	if msg.From != nil {
		from = append(from, *msg.From)
	}
	to := keep(append(from, msg.To...))
	cc := keep(msg.Cc)
	if len(to) == 0 && len(cc) > 0 {
		to, cc = cc[:1], cc[1:]
	}
	d := ReplyDraft(account, msg, head)
	d.To, d.Cc = strings.Join(to, ", "), strings.Join(cc, ", ")
	return d, true
}

// SendMail sends a mail (POST /mail/send).
func (c *Client) SendMail(ctx context.Context, d MailDraft) error {
	body := map[string]any{
		"account_id": d.AccountID,
		"to":         nonNil(ParseAddresses(d.To)),
		"cc":         nonNil(ParseAddresses(d.Cc)),
		"bcc":        nonNil(ParseAddresses(d.Bcc)),
		"subject":    d.Subject,
		"text":       d.Body,
	}
	if d.InReplyTo != "" {
		body["in_reply_to"] = d.InReplyTo
	}
	var refs []string
	for _, r := range d.References {
		if r != "" {
			refs = append(refs, r)
		}
	}
	if len(refs) > 0 {
		body["references"] = strings.Join(refs, " ")
	}
	return c.Do(ctx, http.MethodPost, "/mail/send", body, nil)
}

func nonNil(a []MailAddress) []MailAddress {
	if a == nil {
		return []MailAddress{}
	}
	return a
}

var (
	htmlDrop   = regexp.MustCompile(`(?is)<(script|style|head)[^>]*>.*?</(script|style|head)>`)
	htmlBreak  = regexp.MustCompile(`(?i)<br\s*/?>|</(p|div|tr|li|h[1-6]|blockquote)>`)
	htmlTag    = regexp.MustCompile(`<[^>]*>`)
	blankLines = regexp.MustCompile(`\n[ \t]*\n(\s*\n)+`)
)

// HTMLText is a mail's HTML as plain text: no scripts or styles, a line per paragraph, entities decoded.
func HTMLText(s string) string {
	s = htmlDrop.ReplaceAllString(s, "")
	s = htmlBreak.ReplaceAllString(s, "\n")
	s = htmlTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(strings.ReplaceAll(l, " ", " "))
	}
	return strings.TrimSpace(blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
