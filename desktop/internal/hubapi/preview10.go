package hubapi

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/coder/websocket"
)

// Dev server previews (docs/web/preview.md): the page an agent's dev server serves on one of your computers, drawn by
// a headless browser there and streamed here as pictures, with this app's mouse and keys sent back.

// FeaturePreview is the hub feature that says it has previews.
const FeaturePreview = "preview"

// Port is a port an agent of the session listens on.
type Port struct {
	Port    int    `json:"port"`
	Addr    string `json:"addr,omitempty"`
	Process string `json:"process,omitempty"`
	Title   string `json:"title,omitempty"`
}

// PreviewServer is a .claude/launch.json entry as the computer read it.
type PreviewServer struct {
	Name      string            `json:"name"`
	Port      int               `json:"port,omitempty"`
	URL       string            `json:"url,omitempty"`
	Command   []string          `json:"command,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Cwd       string            `json:"cwd,omitempty"`
	Listening bool              `json:"listening"`
	Title     string            `json:"title,omitempty"`
	Problem   string            `json:"problem,omitempty"`
}

// PreviewServers is the session folder's launch.json and the browser previews open in ("" when there is none).
type PreviewServers struct {
	File    string          `json:"file,omitempty"`
	Servers []PreviewServer `json:"servers"`
	Error   string          `json:"error,omitempty"`
	Browser string          `json:"browser,omitempty"`
}

func previewPath(machine string) string { return "/machines/" + url.PathEscape(machine) + "/preview" }

// PreviewServersOf reads the servers the session's folder declares.
func (c *Client) PreviewServersOf(ctx context.Context, machine, cwd string) (PreviewServers, error) {
	var out PreviewServers
	err := c.Do(ctx, http.MethodGet, previewPath(machine)+"/servers?"+url.Values{"cwd": {cwd}}.Encode(), nil, &out)
	return out, err
}

// StartPreview runs a launch.json entry in a new terminal tab of the session's workspace.
func (c *Client) StartPreview(ctx context.Context, machine, cwd, name, workspace, kind string) (string, error) {
	var out struct {
		Terminal string `json:"terminal"`
	}
	err := c.Do(ctx, http.MethodPost, previewPath(machine)+"/start",
		map[string]string{"cwd": cwd, "name": name, "workspace": workspace, "kind": kind}, &out)
	return out.Terminal, err
}

// OpenPort lets the user's devices reach a port of a computer that no agent listens on (a server they picked).
func (c *Client) OpenPort(ctx context.Context, machine string, port int) error {
	return c.Do(ctx, http.MethodPost, "/machines/"+url.PathEscape(machine)+"/ports/"+strconv.Itoa(port)+"/open", nil, nil)
}

// PreviewOption is one server a preview can show: a launch.json entry, or a port an agent of the session listens on.
type PreviewOption struct {
	Key       string
	Name      string
	Port      int
	Path      string // where it opens on its server
	Listening bool
	Title     string
	Command   []string // a launch.json entry's command: it can be started
	Env       map[string]string
	Launch    bool
}

// PreviewOptions are the usable launch.json entries in the file's order, then the agents' ports none of them has.
func PreviewOptions(res PreviewServers, ports []Port) []PreviewOption {
	var out []PreviewOption
	has := func(port int) bool {
		for _, o := range out {
			if o.Port == port {
				return true
			}
		}
		return false
	}
	for _, s := range res.Servers {
		if s.Problem != "" || s.Port == 0 {
			continue
		}
		path := "/"
		if u, err := url.Parse(s.URL); err == nil && s.URL != "" {
			if p := u.RequestURI(); p != "" {
				path = p
			}
			if u.Fragment != "" {
				path += "#" + u.Fragment
			}
		}
		listening := s.Listening
		for _, p := range ports {
			listening = listening || p.Port == s.Port
		}
		out = append(out, PreviewOption{Key: "launch:" + s.Name, Name: s.Name, Port: s.Port, Path: path, Listening: listening,
			Title: s.Title, Command: s.Command, Env: s.Env, Launch: true})
	}
	for _, p := range ports {
		if has(p.Port) {
			continue
		}
		name := p.Title
		if name == "" {
			name = ":" + strconv.Itoa(p.Port)
		}
		out = append(out, PreviewOption{Key: "port:" + strconv.Itoa(p.Port), Name: name, Port: p.Port, Path: "/", Listening: true, Title: p.Title})
	}
	return out
}

// CommandLine is the command an entry runs, each word quoted as the computer quotes it.
func CommandLine(o PreviewOption) string {
	q := func(s string) string {
		plain := s != ""
		for _, r := range s {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_./-", r)) {
				plain = false
			}
		}
		if plain {
			return s
		}
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	var words []string
	if len(o.Env) > 0 {
		keys := make([]string, 0, len(o.Env))
		for k := range o.Env {
			keys = append(keys, k)
		}
		sortStrings(keys)
		words = append(words, "env")
		for _, k := range keys {
			words = append(words, q(k+"="+o.Env[k]))
		}
	}
	for _, w := range o.Command {
		words = append(words, q(w))
	}
	return strings.Join(words, " ")
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// PreviewSize is the preview's size in CSS pixels; Mobile emulates a phone.
type PreviewSize struct {
	W, H   int
	DPR    float64
	Mobile bool
}

// PreviewEvent is what the preview says: a picture (Frame) or one of its JSON messages (T is its type).
type PreviewEvent struct {
	T string `json:"t"`
	// a picture: Seq, the page's CSS size, the JPEG
	Seq   uint32 `json:"-"`
	FW    int    `json:"-"`
	FH    int    `json:"-"`
	Frame []byte `json:"-"`
	// ready, page, external, failed, dialog, closed, error, copied
	Browser string `json:"browser,omitempty"`
	URL     string `json:"url,omitempty"`
	Title   string `json:"title,omitempty"`
	Loading bool   `json:"loading,omitempty"`
	Back    bool   `json:"back,omitempty"`
	Forward bool   `json:"forward,omitempty"`
	Error   string `json:"error,omitempty"`
	Code    string `json:"code,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Message string `json:"message,omitempty"`
	Default string `json:"default,omitempty"`
	Text    string `json:"text,omitempty"`
}

// ParsePreviewBinary reads a binary message: 'f' a picture (seq, width, height, JPEG); anything else is skipped.
func ParsePreviewBinary(b []byte) (PreviewEvent, bool) {
	if len(b) >= 9 && b[0] == 'f' {
		return PreviewEvent{T: "frame", Seq: binary.BigEndian.Uint32(b[1:5]), FW: int(binary.BigEndian.Uint16(b[5:7])),
			FH: int(binary.BigEndian.Uint16(b[7:9])), Frame: b[9:]}, true
	}
	return PreviewEvent{}, false
}

// Preview is an open preview.
type Preview struct {
	conn   *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	out    chan []byte
	once   sync.Once
}

// previewURL is the preview's WebSocket address.
func (c *Client) previewURL(machine string, port int, path string, size PreviewSize) string {
	u, _ := url.Parse(c.endpoint(previewPath(machine) + "/connect"))
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	if path == "" {
		path = "/"
	}
	q := url.Values{"port": {strconv.Itoa(port)}, "path": {path}, "w": {strconv.Itoa(size.W)}, "h": {strconv.Itoa(size.H)},
		"dpr": {strconv.FormatFloat(size.DPR, 'f', -1, 64)}}
	if size.Mobile {
		q.Set("mobile", "1")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// OpenPreview connects to a preview of a port; each message it says goes to on, from a goroutine of its own, until
// it ends (on gets a last {t: "closed"} with the reason in Error, if any).
func (c *Client) OpenPreview(ctx context.Context, machine string, port int, path string, size PreviewSize, on func(PreviewEvent)) (*Preview, error) {
	conn, resp, err := websocket.Dial(ctx, c.previewURL(machine, port, path, size), &websocket.DialOptions{
		HTTPClient: c.http,
		HTTPHeader: http.Header{"Authorization": {"Bearer " + c.Token}, "X-Persona-Client": {Platform + "/" + Version}},
	})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	conn.SetReadLimit(32 << 20)
	pctx, cancel := context.WithCancel(context.Background())
	pv := &Preview{conn: conn, ctx: pctx, cancel: cancel, out: make(chan []byte, 256)}
	go func() { // one writer, so input goes in the order it was sent
		for {
			select {
			case <-pctx.Done():
				return
			case data := <-pv.out:
				if conn.Write(pctx, websocket.MessageText, data) != nil {
					return
				}
			}
		}
	}()
	go func() {
		defer cancel()
		for {
			typ, data, err := conn.Read(pctx)
			if err != nil {
				ev := PreviewEvent{T: "closed"}
				if !errors.Is(err, context.Canceled) && websocket.CloseStatus(err) != websocket.StatusNormalClosure {
					ev.Error = err.Error()
				}
				on(ev)
				return
			}
			if typ == websocket.MessageBinary {
				if ev, ok := ParsePreviewBinary(data); ok {
					on(ev)
				}
				continue
			}
			var ev PreviewEvent
			if json.Unmarshal(data, &ev) == nil && ev.T != "" {
				on(ev)
			}
		}
	}()
	return pv, nil
}

// Send queues one input message ({"t": "mouse", …}); a preview that has ended drops it.
func (p *Preview) Send(msg map[string]any) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	select {
	case p.out <- data:
	default: // a stalled link: drop it rather than block the window
	}
}

// Close ends the preview.
func (p *Preview) Close() {
	p.once.Do(func() {
		_ = p.conn.Close(websocket.StatusNormalClosure, "")
		p.cancel()
	})
}
