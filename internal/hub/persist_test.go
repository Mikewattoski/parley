package hub

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mikewattoski/parley/internal/protocol"
)

type fakeStore struct {
	mu     sync.Mutex
	err    error
	nextID int64
	saved  []string
}

func (f *fakeStore) SaveMessage(_ context.Context, room string, _ int64, text string) (int64, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, time.Time{}, f.err
	}
	f.nextID++
	f.saved = append(f.saved, room+":"+text)
	return f.nextID, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC), nil
}

func (f *fakeStore) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeStore) savedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.saved)
}

func newStoreHub(t *testing.T, st MessageStore) *Hub {
	t.Helper()
	h := New().WithStore(st)
	go h.Run()
	t.Cleanup(func() {
		h.Stop()
		<-h.Done()
	})
	return h
}

func TestMessageIsSavedThenBroadcastWithItsID(t *testing.T) {
	st := &fakeStore{}
	h := newStoreHub(t, st)
	a := register(t, h, "alice", 16)
	b := register(t, h, "bob", 16)
	joinRoom(t, h, a, "r")
	joinRoom(t, h, b, "r")

	h.Submit(a, protocol.Envelope{Type: protocol.TypeMessage, Room: "r", Text: "hello", CID: "c1"})

	for _, c := range []*Client{a, b} {
		got := waitFor(t, c, ofType(protocol.TypeMessage))
		if got.ID != 1 || got.CID != "c1" || got.Text != "hello" || got.From != "alice" {
			t.Errorf("%s received %+v", c.name, got)
		}
	}
	if n := st.savedCount(); n != 1 {
		t.Errorf("saved %d messages, want 1", n)
	}
}

func TestSaveFailureOnlyNotifiesTheSender(t *testing.T) {
	st := &fakeStore{err: errors.New("db down")}
	h := newStoreHub(t, st)
	a := register(t, h, "alice", 16)
	b := register(t, h, "bob", 16)
	joinRoom(t, h, a, "r")
	joinRoom(t, h, b, "r")

	h.Submit(a, protocol.Envelope{Type: protocol.TypeMessage, Room: "r", Text: "lost", CID: "c9"})
	got := waitFor(t, a, ofType(protocol.TypeError))
	if got.CID != "c9" || got.Error != "could not save message" {
		t.Errorf("sender got %+v", got)
	}

	// once the database is back, bob's first message must be the new one
	st.setErr(nil)
	h.Submit(a, protocol.Envelope{Type: protocol.TypeMessage, Room: "r", Text: "kept", CID: "c10"})
	if first := waitFor(t, b, ofType(protocol.TypeMessage)); first.Text != "kept" {
		t.Errorf("bob saw %q first, want only the saved message", first.Text)
	}
}

func TestOversizedCIDIsRejected(t *testing.T) {
	h := newStoreHub(t, &fakeStore{})
	a := register(t, h, "alice", 16)
	joinRoom(t, h, a, "r")

	h.Submit(a, protocol.Envelope{Type: protocol.TypeMessage, Room: "r", Text: "hi", CID: strings.Repeat("x", 65)})

	if got := waitFor(t, a, ofType(protocol.TypeError)); got.Error != "invalid cid" {
		t.Errorf("error = %q", got.Error)
	}
}
