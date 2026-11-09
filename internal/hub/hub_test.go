package hub

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Mikewattoski/parley/internal/protocol"
)

func newTestHub(t *testing.T) *Hub {
	t.Helper()
	h := New()
	go h.Run()
	t.Cleanup(func() {
		h.Stop()
		<-h.Done()
	})
	return h
}

// register adds a fake client (no real connection) with the given send buffer.
func register(t *testing.T, h *Hub, name string, buf int) *Client {
	t.Helper()
	c := newClient(h, nil, name, buf)
	if !h.Register(c) {
		t.Fatalf("register %s failed", name)
	}
	return c
}

// waitFor reads the client's outgoing messages until pred matches.
func waitFor(t *testing.T, c *Client, pred func(protocol.Envelope) bool) protocol.Envelope {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case b, ok := <-c.send:
			if !ok {
				t.Fatalf("%s: send channel closed while waiting", c.name)
			}
			var env protocol.Envelope
			if err := json.Unmarshal(b, &env); err != nil {
				t.Fatal(err)
			}
			if pred(env) {
				return env
			}
		case <-timeout:
			t.Fatalf("%s: timed out waiting for the expected message", c.name)
		}
	}
}

func ofType(typ string) func(protocol.Envelope) bool {
	return func(e protocol.Envelope) bool { return e.Type == typ }
}

func joinRoom(t *testing.T, h *Hub, c *Client, room string) {
	t.Helper()
	h.Submit(c, protocol.Envelope{Type: protocol.TypeJoin, Room: room})
	waitFor(t, c, func(e protocol.Envelope) bool {
		return e.Type == protocol.TypeJoined && e.Room == room
	})
}

func drainUntilClosed(t *testing.T, c *Client) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-c.send:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatalf("%s was not dropped", c.name)
		}
	}
}

func TestSlowClientIsDropped(t *testing.T) {
	h := newTestHub(t)
	slow := register(t, h, "slow", 3) // never reads its channel
	fast := register(t, h, "fast", 64)

	h.Submit(slow, protocol.Envelope{Type: protocol.TypeJoin, Room: "r"}) // welcome + joined
	joinRoom(t, h, fast, "r")                                             // presence fills slow's buffer
	for i := 0; i < 5; i++ {
		h.Submit(fast, protocol.Envelope{Type: protocol.TypeMessage, Room: "r", Text: "hi"})
	}

	// the room keeps working for everyone else...
	waitFor(t, fast, ofType(protocol.TypeMessage))
	// ...and fast sees slow get dropped. Nothing has read slow's channel up to
	// this point, so the decision was made by the hub and not by the test.
	waitFor(t, fast, func(e protocol.Envelope) bool {
		return e.Type == protocol.TypePresence && e.Event == "left" && e.From == "slow"
	})
	drainUntilClosed(t, slow)
}

func TestPresenceWhenClientDisconnects(t *testing.T) {
	h := newTestHub(t)
	a := register(t, h, "a", 16)
	b := register(t, h, "b", 16)
	joinRoom(t, h, a, "r")
	joinRoom(t, h, b, "r")

	h.Unregister(a)

	got := waitFor(t, b, func(e protocol.Envelope) bool {
		return e.Type == protocol.TypePresence && e.Event == "left"
	})
	if got.From != "a" {
		t.Errorf("left event from %q, want a", got.From)
	}
}

func TestRoomsAreIsolated(t *testing.T) {
	h := newTestHub(t)
	a := register(t, h, "a", 16)
	b := register(t, h, "b", 16)
	joinRoom(t, h, a, "room-a")
	joinRoom(t, h, b, "room-b")

	h.Submit(a, protocol.Envelope{Type: protocol.TypeMessage, Room: "room-a", Text: "secret"})
	waitFor(t, a, ofType(protocol.TypeMessage))

	// b sends a marker to its own room: anything from room-a would arrive first
	h.Submit(b, protocol.Envelope{Type: protocol.TypeMessage, Room: "room-b", Text: "marker"})
	got := waitFor(t, b, ofType(protocol.TypeMessage))
	if got.Text != "marker" {
		t.Fatalf("b received %+v, want only its own room's message", got)
	}
}

func TestInvalidRequestsGetErrors(t *testing.T) {
	tests := []struct {
		name    string
		env     protocol.Envelope
		wantErr string
	}{
		{"join with invalid room name", protocol.Envelope{Type: protocol.TypeJoin, Room: "bad room!"}, "invalid room name"},
		{"message to a room not joined", protocol.Envelope{Type: protocol.TypeMessage, Room: "nope", Text: "hi"}, "join the room first"},
		{"typing in a room not joined", protocol.Envelope{Type: protocol.TypeTyping, Room: "nope"}, "join the room first"},
		{"leave a room not joined", protocol.Envelope{Type: protocol.TypeLeave, Room: "nope"}, "not in that room"},
		{"unknown type", protocol.Envelope{Type: "dance"}, "unknown message type"},
		{"malformed JSON marker", protocol.Envelope{Type: protocol.TypeInvalid}, "malformed JSON"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHub(t)
			c := register(t, h, "alice", 16)
			h.Submit(c, tc.env)
			got := waitFor(t, c, ofType(protocol.TypeError))
			if got.Error != tc.wantErr {
				t.Errorf("error = %q, want %q", got.Error, tc.wantErr)
			}
		})
	}
}

func TestEmptyMessageIsRejected(t *testing.T) {
	h := newTestHub(t)
	c := register(t, h, "alice", 16)
	joinRoom(t, h, c, "r")

	h.Submit(c, protocol.Envelope{Type: protocol.TypeMessage, Room: "r", Text: "   "})

	got := waitFor(t, c, ofType(protocol.TypeError))
	if got.Error != "text must be 1-2000 characters" {
		t.Errorf("error = %q", got.Error)
	}
}

func TestDisconnectIsOrderedAfterEarlierMessages(t *testing.T) {
	h := newTestHub(t)
	a := register(t, h, "a", 16)
	b := register(t, h, "b", 16)
	joinRoom(t, h, a, "r")
	joinRoom(t, h, b, "r")

	h.Submit(a, protocol.Envelope{Type: protocol.TypeMessage, Room: "r", Text: "last words"})
	h.Unregister(a)

	if got := waitFor(t, b, ofType(protocol.TypeMessage)); got.Text != "last words" {
		t.Errorf("b received %+v, want the message sent just before disconnecting", got)
	}
}
