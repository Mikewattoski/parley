package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/time/rate"

	"github.com/Mikewattoski/parley/internal/auth"
	"github.com/Mikewattoski/parley/internal/hub"
	"github.com/Mikewattoski/parley/internal/model"
	"github.com/Mikewattoski/parley/internal/protocol"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestMain(m *testing.M) {
	auth.BcryptCost = bcrypt.MinCost // fast hashing for tests
	os.Exit(m.Run())
}

// memStore is an in-memory implementation of Store and hub.MessageStore.
type memStore struct {
	mu       sync.Mutex
	users    map[string]model.User // keyed by lower-case username
	nextUser int64
	msgs     []model.Message
	nextMsg  int64
}

func newMemStore() *memStore { return &memStore{users: map[string]model.User{}} }

func (m *memStore) CreateUser(_ context.Context, username, hash string) (model.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := strings.ToLower(username)
	if _, ok := m.users[key]; ok {
		return model.User{}, model.ErrConflict
	}
	m.nextUser++
	u := model.User{ID: m.nextUser, Username: username, PasswordHash: hash}
	m.users[key] = u
	return u, nil
}

func (m *memStore) UserByUsername(_ context.Context, username string) (model.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[strings.ToLower(username)]
	if !ok {
		return model.User{}, model.ErrNotFound
	}
	return u, nil
}

func (m *memStore) ListRooms(context.Context) ([]string, error) { return []string{"general"}, nil }

func (m *memStore) History(_ context.Context, room string, before int64, limit int) ([]model.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []model.Message
	for i := len(m.msgs) - 1; i >= 0 && len(out) < limit; i-- {
		msg := m.msgs[i]
		if msg.Room != room || (before != 0 && msg.ID >= before) {
			continue
		}
		out = append(out, msg)
	}
	return out, nil
}

func (m *memStore) SaveMessage(_ context.Context, room string, userID int64, text string) (int64, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name := ""
	for _, u := range m.users {
		if u.ID == userID {
			name = u.Username
		}
	}
	m.nextMsg++
	ts := time.Now().UTC()
	m.msgs = append(m.msgs, model.Message{ID: m.nextMsg, Room: room, From: name, Text: text, TS: ts})
	return m.nextMsg, ts, nil
}

type testEnv struct {
	ts    *httptest.Server
	store *memStore
}

func startServer(t *testing.T, origins []string, authRate rate.Limit, authBurst int) *testEnv {
	t.Helper()
	st := newMemStore()
	svc, err := auth.NewService(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	h := hub.New().WithStore(st)
	go h.Run()
	srv := New(Config{
		Hub: h, Store: st, Auth: svc, Origins: origins, Index: []byte("test page"),
		AuthRate: authRate, AuthBurst: authBurst,
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		h.Stop()
		<-h.Done()
	})
	return &testEnv{ts: ts, store: st}
}

func start(t *testing.T) *testEnv { return startServer(t, nil, rate.Inf, 1000) }

func (e *testEnv) do(t *testing.T, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.ts.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (e *testEnv) signUp(t *testing.T, user string) string {
	t.Helper()
	status, out := e.do(t, http.MethodPost, "/api/register", "", map[string]string{
		"username": user, "password": "correct horse",
	})
	token, _ := out["token"].(string)
	if status != http.StatusCreated || token == "" {
		t.Fatalf("register %s: status %d, body %v", user, status, out)
	}
	return token
}

func (e *testEnv) ticket(t *testing.T, token string) string {
	t.Helper()
	status, out := e.do(t, http.MethodPost, "/api/ws-ticket", token, nil)
	ticket, _ := out["ticket"].(string)
	if status != http.StatusOK || ticket == "" {
		t.Fatalf("ws-ticket: status %d, body %v", status, out)
	}
	return ticket
}

func wsURL(ts *httptest.Server, query string) string {
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws" + query
}

func (e *testEnv) connect(t *testing.T, token string) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.DefaultDialer.Dial(wsURL(e.ts, "?ticket="+url.QueryEscape(e.ticket(t, token))), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func send(t *testing.T, c *websocket.Conn, env protocol.Envelope) {
	t.Helper()
	if err := c.WriteJSON(env); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readUntil(t *testing.T, c *websocket.Conn, pred func(protocol.Envelope) bool) protocol.Envelope {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var env protocol.Envelope
		if err := c.ReadJSON(&env); err != nil {
			t.Fatalf("read: %v", err)
		}
		if pred(env) {
			return env
		}
	}
}

func ofType(typ string) func(protocol.Envelope) bool {
	return func(e protocol.Envelope) bool { return e.Type == typ }
}

func joinRoom(t *testing.T, c *websocket.Conn, room string) {
	t.Helper()
	send(t, c, protocol.Envelope{Type: protocol.TypeJoin, Room: room})
	readUntil(t, c, ofType(protocol.TypeJoined))
}

func statusOf(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}

func texts(items []any) string {
	var out []string
	for _, it := range items {
		m, _ := it.(map[string]any)
		s, _ := m["text"].(string)
		out = append(out, s)
	}
	return strings.Join(out, ",")
}

func TestRegisterAndLogin(t *testing.T) {
	e := start(t)
	e.signUp(t, "alice")

	tests := []struct {
		name, path, user, pass string
		want                   int
	}{
		{"duplicate username (case-insensitive)", "/api/register", "ALICE", "correct horse", http.StatusConflict},
		{"username too short", "/api/register", "al", "correct horse", http.StatusBadRequest},
		{"username with illegal characters", "/api/register", "bad name!", "correct horse", http.StatusBadRequest},
		{"password too short", "/api/register", "bob", "short", http.StatusBadRequest},
		{"login ok", "/api/login", "alice", "correct horse", http.StatusOK},
		{"login with a different case", "/api/login", "Alice", "correct horse", http.StatusOK},
		{"login wrong password", "/api/login", "alice", "wrong password", http.StatusUnauthorized},
		{"login unknown user", "/api/login", "nobody", "whatever123", http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, out := e.do(t, http.MethodPost, tc.path, "", map[string]string{
				"username": tc.user, "password": tc.pass,
			})
			if status != tc.want {
				t.Fatalf("status = %d, want %d (body %v)", status, tc.want, out)
			}
		})
	}
}

func TestLoginDoesNotRevealWhichUsernamesExist(t *testing.T) {
	e := start(t)
	e.signUp(t, "alice")

	_, wrongPassword := e.do(t, http.MethodPost, "/api/login", "", map[string]string{"username": "alice", "password": "wrong password"})
	_, unknownUser := e.do(t, http.MethodPost, "/api/login", "", map[string]string{"username": "nobody", "password": "wrong password"})

	if wrongPassword["error"] != unknownUser["error"] {
		t.Errorf("different errors leak which users exist: %v vs %v", wrongPassword["error"], unknownUser["error"])
	}
}

func TestProtectedEndpointsNeedAnAPIToken(t *testing.T) {
	e := start(t)
	token := e.signUp(t, "alice")
	ticket := e.ticket(t, token)

	tests := []struct{ name, token string }{
		{"no token", ""},
		{"garbage token", "not-a-token"},
		{"a websocket ticket used as an API token", ticket},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if status, _ := e.do(t, http.MethodGet, "/api/rooms", tc.token, nil); status != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", status)
			}
		})
	}
	if status, _ := e.do(t, http.MethodGet, "/api/rooms", token, nil); status != http.StatusOK {
		t.Fatalf("valid token: status = %d, want 200", status)
	}
}

func TestWebSocketNeedsAValidTicket(t *testing.T) {
	e := start(t)
	token := e.signUp(t, "alice")

	tests := []struct{ name, query string }{
		{"no ticket", ""},
		{"garbage ticket", "?ticket=abc"},
		{"API token instead of a ticket", "?ticket=" + url.QueryEscape(token)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, resp, err := websocket.DefaultDialer.Dial(wsURL(e.ts, tc.query), nil)
			if err == nil {
				_ = c.Close()
				t.Fatal("expected the handshake to be rejected")
			}
			if got := statusOf(resp); got != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", got)
			}
		})
	}

	e.connect(t, token) // a real ticket works
}

func TestOriginCheck(t *testing.T) {
	e := startServer(t, []string{"http://localhost:5173"}, rate.Inf, 1000)
	token := e.signUp(t, "alice")

	tests := []struct {
		name, origin string
		wantOK       bool
	}{
		{"no origin header (non-browser client)", "", true},
		{"allowed origin", "http://localhost:5173", true},
		{"unknown origin", "http://evil.example", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hdr := http.Header{}
			if tc.origin != "" {
				hdr.Set("Origin", tc.origin)
			}
			c, resp, err := websocket.DefaultDialer.Dial(wsURL(e.ts, "?ticket="+url.QueryEscape(e.ticket(t, token))), hdr)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("handshake failed: %v", err)
				}
				_ = c.Close()
				return
			}
			if err == nil {
				_ = c.Close()
				t.Fatal("expected the handshake to be rejected")
			}
			if got := statusOf(resp); got != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", got)
			}
		})
	}
}

func TestChatIsPersistedAndAcked(t *testing.T) {
	e := start(t)
	aliceToken, bobToken := e.signUp(t, "alice"), e.signUp(t, "bob")
	alice, bob := e.connect(t, aliceToken), e.connect(t, bobToken)
	joinRoom(t, alice, "general")
	joinRoom(t, bob, "general")

	send(t, alice, protocol.Envelope{Type: protocol.TypeMessage, Room: "general", Text: "hello bob", CID: "c1"})

	for name, c := range map[string]*websocket.Conn{"alice": alice, "bob": bob} {
		got := readUntil(t, c, ofType(protocol.TypeMessage))
		if got.ID == 0 || got.From != "alice" || got.Text != "hello bob" || got.CID != "c1" {
			t.Errorf("%s received %+v", name, got)
		}
	}

	status, out := e.do(t, http.MethodGet, "/api/rooms/general/messages", aliceToken, nil)
	items, _ := out["items"].([]any)
	if status != http.StatusOK || len(items) != 1 {
		t.Fatalf("history: status %d, body %v", status, out)
	}
	first, _ := items[0].(map[string]any)
	if first["text"] != "hello bob" || first["from"] != "alice" {
		t.Errorf("history item = %v", first)
	}
}

func TestHistoryPagination(t *testing.T) {
	e := start(t)
	token := e.signUp(t, "alice") // user id 1
	for i := 1; i <= 5; i++ {
		if _, _, err := e.store.SaveMessage(context.Background(), "general", 1, fmt.Sprintf("m%d", i)); err != nil {
			t.Fatal(err)
		}
	}

	status, out := e.do(t, http.MethodGet, "/api/rooms/general/messages?limit=2", token, nil)
	items, _ := out["items"].([]any)
	cursor, _ := out["next_cursor"].(float64)
	if status != http.StatusOK || texts(items) != "m5,m4" || cursor == 0 {
		t.Fatalf("page 1: status %d, items %q, cursor %v", status, texts(items), out["next_cursor"])
	}

	_, out = e.do(t, http.MethodGet, fmt.Sprintf("/api/rooms/general/messages?limit=2&before=%d", int64(cursor)), token, nil)
	items, _ = out["items"].([]any)
	cursor, _ = out["next_cursor"].(float64)
	if texts(items) != "m3,m2" || cursor == 0 {
		t.Fatalf("page 2: items %q, cursor %v", texts(items), out["next_cursor"])
	}

	_, out = e.do(t, http.MethodGet, fmt.Sprintf("/api/rooms/general/messages?limit=2&before=%d", int64(cursor)), token, nil)
	items, _ = out["items"].([]any)
	if texts(items) != "m1" || out["next_cursor"] != nil {
		t.Fatalf("page 3: items %q, cursor %v (want the last page with no cursor)", texts(items), out["next_cursor"])
	}

	for _, bad := range []string{"limit=0", "limit=abc", "before=-1"} {
		if status, _ := e.do(t, http.MethodGet, "/api/rooms/general/messages?"+bad, token, nil); status != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", bad, status)
		}
	}
}

func TestAuthEndpointsAreRateLimited(t *testing.T) {
	e := startServer(t, nil, rate.Every(time.Hour), 3) // 3 attempts, then (practically) no refill

	for i := 0; i < 3; i++ {
		status, _ := e.do(t, http.MethodPost, "/api/login", "", map[string]string{"username": "nobody", "password": "wrong password"})
		if status != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, status)
		}
	}
	if status, _ := e.do(t, http.MethodPost, "/api/login", "", map[string]string{"username": "nobody", "password": "wrong password"}); status != http.StatusTooManyRequests {
		t.Fatalf("4th attempt: status = %d, want 429", status)
	}
}

func TestFloodingClientIsWarnedThenDisconnected(t *testing.T) {
	e := start(t)
	c := e.connect(t, e.signUp(t, "alice"))
	joinRoom(t, c, "general")

	type result struct{ warned, timedOut bool }
	done := make(chan result, 1)

	// read while writing: the warnings must arrive before the connection closes
	go func() {
		var r result
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			var env protocol.Envelope
			if err := c.ReadJSON(&env); err != nil {
				var ne net.Error
				r.timedOut = errors.As(err, &ne) && ne.Timeout()
				done <- r
				return
			}
			if env.Type == protocol.TypeError && strings.Contains(env.Error, "rate limit") {
				r.warned = true
			}
		}
	}()

	for i := 0; i < 100; i++ {
		if err := c.WriteJSON(protocol.Envelope{Type: protocol.TypeTyping, Room: "general"}); err != nil {
			break // the server already hung up
		}
	}

	r := <-done
	if r.timedOut {
		t.Fatal("the server did not disconnect the flooding client")
	}
	if !r.warned {
		t.Error("the client was disconnected without a rate-limit warning")
	}
}

func TestMalformedJSONKeepsTheConnectionOpen(t *testing.T) {
	e := start(t)
	c := e.connect(t, e.signUp(t, "alice"))

	if err := c.WriteMessage(websocket.TextMessage, []byte("not json")); err != nil {
		t.Fatal(err)
	}
	if got := readUntil(t, c, ofType(protocol.TypeError)); got.Error != "malformed JSON" {
		t.Errorf("error = %q", got.Error)
	}
	joinRoom(t, c, "general") // still works
}

func TestOversizedFrameClosesTheConnection(t *testing.T) {
	e := start(t)
	c := e.connect(t, e.signUp(t, "alice"))

	if err := c.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", 5000))); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		_, _, err := c.ReadMessage()
		if err == nil {
			continue // the welcome message, then the close
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			t.Fatal("the server did not close the connection")
		}
		return
	}
}
