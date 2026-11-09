package hubapi

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Socket timings: reconnect backoff, the app-level ping and how long silence means the connection is dead.
const (
	retryFirst = 600 * time.Millisecond
	retryMax   = 15 * time.Second
	pingEvery  = 25 * time.Second
	deadAfter  = 70 * time.Second
)

// Status is the state of the live connection.
type Status struct {
	Online bool
	// Err is why the last attempt failed; ErrUnauthorized stops the socket.
	Err error
	// Retry is when the next attempt goes out while offline.
	Retry time.Time
}

// Socket keeps one WebSocket to the hub open, reconnecting with backoff, and hands every message to OnEvent.
// OnEvent and OnStatus run on the socket's goroutine.
type Socket struct {
	Client   *Client
	DeviceID string
	OnEvent  func(Event)
	OnStatus func(Status)

	mu    sync.Mutex
	conn  *websocket.Conn
	ctx   context.Context
	subs  map[string]bool
	kick  chan struct{}
	retry int
}

// Run connects and reconnects until ctx ends or the hub refuses the token.
func (s *Socket) Run(ctx context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	if s.kick == nil {
		s.kick = make(chan struct{}, 1)
	}
	s.mu.Unlock()
	for ctx.Err() == nil {
		err := s.once(ctx)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, ErrUnauthorized) {
			s.status(Status{Err: err})
			return
		}
		wait := backoff(s.retry)
		s.retry++
		s.status(Status{Err: err, Retry: time.Now().Add(wait)})
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		case <-s.kick:
		}
	}
}

// RetryNow skips the wait before the next attempt.
func (s *Socket) RetryNow() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.kick == nil {
		s.kick = make(chan struct{}, 1)
	}
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

func backoff(n int) time.Duration {
	d := retryFirst
	for i := 0; i < n && d < retryMax; i++ {
		d *= 2
	}
	d = min(d, retryMax)
	return d/2 + time.Duration(rand.Int64N(int64(d/2)+1))
}

func (s *Socket) status(st Status) {
	if s.OnStatus != nil {
		s.OnStatus(st)
	}
}

func (s *Socket) once(ctx context.Context) error {
	c := s.Client
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	conn, resp, err := websocket.Dial(dctx, c.wsURL(), &websocket.DialOptions{
		HTTPClient: c.http,
		HTTPHeader: http.Header{
			"Authorization":    {"Bearer " + c.Token},
			"X-Persona-Client": {Platform + "/" + Version},
		},
	})
	cancel()
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			return ErrUnauthorized
		}
		return err
	}
	conn.SetReadLimit(64 << 20)
	defer conn.CloseNow()

	s.mu.Lock()
	s.conn = conn
	subs := make([]string, 0, len(s.subs))
	for k := range s.subs {
		subs = append(subs, k)
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.conn = nil
		s.mu.Unlock()
	}()

	s.Send(map[string]any{"type": "client_hello", "device_id": s.DeviceID, "platform": Platform, "version": Version,
		"features": Features, "locale": c.Locale})

	last := time.Now()
	var lastMu sync.Mutex
	pctx, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		var seq int
		for {
			select {
			case <-pctx.Done():
				return
			case <-t.C:
			}
			lastMu.Lock()
			quiet := time.Since(last)
			lastMu.Unlock()
			if quiet > deadAfter {
				conn.Close(websocket.StatusGoingAway, "quiet")
				return
			}
			if quiet > pingEvery {
				seq++
				s.Send(map[string]any{"type": "ping", "id": strconv.Itoa(seq)})
			}
		}
	}()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		lastMu.Lock()
		last = time.Now()
		lastMu.Unlock()
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(data, &head) != nil || head.Type == "" {
			continue
		}
		if head.Type == "hello" {
			s.retry = 0
			s.status(Status{Online: true})
			for _, k := range subs {
				s.sendSub("subscribe", k)
			}
		}
		if s.OnEvent != nil {
			s.OnEvent(Event{Type: head.Type, Raw: data})
		}
	}
}

// Send writes a message when connected; it is dropped otherwise (state is re-read after every reconnect).
func (s *Socket) Send(msg any) {
	s.mu.Lock()
	conn, ctx := s.conn, s.ctx
	s.mu.Unlock()
	if conn == nil || ctx == nil {
		return
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = conn.Write(wctx, websocket.MessageText, data)
}

// Subscribe follows a session's transcript, now and after every reconnect.
func (s *Socket) Subscribe(machine, session string) {
	k := machine + "/" + session
	s.mu.Lock()
	if s.subs == nil {
		s.subs = map[string]bool{}
	}
	had := s.subs[k]
	s.subs[k] = true
	s.mu.Unlock()
	if !had {
		s.sendSub("subscribe", k)
	}
}

// Unsubscribe stops following a session.
func (s *Socket) Unsubscribe(machine, session string) {
	k := machine + "/" + session
	s.mu.Lock()
	had := s.subs[k]
	delete(s.subs, k)
	s.mu.Unlock()
	if had {
		s.sendSub("unsubscribe", k)
	}
}

func (s *Socket) sendSub(kind, key string) {
	for i := 0; i < len(key); i++ {
		if key[i] == '/' {
			s.Send(map[string]any{"type": kind, "machine_id": key[:i], "session_id": key[i+1:]})
			return
		}
	}
}
