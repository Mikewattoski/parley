package server

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Mikewattoski/parley/internal/hub"
	"github.com/Mikewattoski/parley/internal/protocol"
)

func startServer(t *testing.T, origins []string) *httptest.Server {
	t.Helper()
	h := hub.New()
	go h.Run()
	ts := httptest.NewServer(New(h, origins, []byte("test page")).Handler())
	t.Cleanup(func() {
		ts.Close()
		h.Stop()
		<-h.Done()
	})
	return ts
}

func wsURL(ts *httptest.Server, query string) string {
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws" + query
}

func dial(t *testing.T, ts *httptest.Server, user string) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.DefaultDialer.Dial(wsURL(ts, "?user="+url.QueryEscape(user)), nil)
	if err != nil {
		t.Fatalf("dial %s: %v", user, err)
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

func statusOf(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}

func TestChatBetweenTwoClients(t *testing.T) {
	ts := startServer(t, nil)
	alice, bob := dial(t, ts, "alice"), dial(t, ts, "bob")

	for _, c := range []*websocket.Conn{alice, bob} {
		send(t, c, protocol.Envelope{Type: protocol.TypeJoin, Room: "general"})
		readUntil(t, c, ofType(protocol.TypeJoined))
	}

	send(t, alice, protocol.Envelope{Type: protocol.TypeMessage, Room: "general", Text: "hello bob"})

	for name, c := range map[string]*websocket.Conn{"alice": alice, "bob": bob} {
		got := readUntil(t, c, ofType(protocol.TypeMessage))
		if got.From != "alice" || got.Text != "hello bob" || got.Room != "general" || got.TS == "" {
			t.Errorf("%s received %+v", name, got)
		}
	}
}

func TestPresenceWhenAClientDisconnects(t *testing.T) {
	ts := startServer(t, nil)
	alice, bob := dial(t, ts, "alice"), dial(t, ts, "bob")
	for _, c := range []*websocket.Conn{alice, bob} {
		send(t, c, protocol.Envelope{Type: protocol.TypeJoin, Room: "general"})
		readUntil(t, c, ofType(protocol.TypeJoined))
	}

	_ = alice.Close() // abrupt: no close frame

	got := readUntil(t, bob, func(e protocol.Envelope) bool {
		return e.Type == protocol.TypePresence && e.Event == "left"
	})
	if got.From != "alice" {
		t.Errorf("left event from %q, want alice", got.From)
	}
}

func TestInvalidUserNameIsRejected(t *testing.T) {
	ts := startServer(t, nil)
	c, resp, err := websocket.DefaultDialer.Dial(wsURL(ts, "?user="+url.QueryEscape("bad name!")), nil)
	if err == nil {
		_ = c.Close()
		t.Fatal("expected the handshake to be rejected")
	}
	if got := statusOf(resp); got != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", got)
	}
}

func TestOriginCheck(t *testing.T) {
	ts := startServer(t, []string{"http://localhost:5173"})
	tests := []struct {
		name   string
		origin string
		wantOK bool
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
			c, resp, err := websocket.DefaultDialer.Dial(wsURL(ts, ""), hdr)
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

func TestMalformedJSONKeepsTheConnectionOpen(t *testing.T) {
	ts := startServer(t, nil)
	c := dial(t, ts, "alice")

	if err := c.WriteMessage(websocket.TextMessage, []byte("not json")); err != nil {
		t.Fatal(err)
	}
	got := readUntil(t, c, ofType(protocol.TypeError))
	if got.Error != "malformed JSON" {
		t.Errorf("error = %q", got.Error)
	}

	send(t, c, protocol.Envelope{Type: protocol.TypeJoin, Room: "general"})
	readUntil(t, c, ofType(protocol.TypeJoined)) // still works
}

func TestOversizedFrameClosesTheConnection(t *testing.T) {
	ts := startServer(t, nil)
	c := dial(t, ts, "alice")

	if err := c.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", 5000))); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		_, _, err := c.ReadMessage()
		if err == nil {
			continue // welcome message, then the close
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			t.Fatal("server did not close the connection")
		}
		return
	}
}
