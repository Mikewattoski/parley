// Package hub routes chat messages between connected clients and rooms.
//
// One goroutine (Run) owns all room state, so no locks are needed:
// everything else talks to it through channels. The hub never waits on the
// database: messages go to a persister goroutine and come back once saved.
package hub

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Mikewattoski/parley/internal/protocol"
)

const (
	maxRoomsPerClient = 20
	persistQueueSize  = 1024
)

// MessageStore saves chat messages. It is optional: without one, messages
// are broadcast immediately and nothing is kept.
type MessageStore interface {
	SaveMessage(ctx context.Context, room string, userID int64, text string) (id int64, ts time.Time, err error)
}

type inbound struct {
	client *Client
	env    protocol.Envelope
}

type persistJob struct {
	client *Client
	room   string
	text   string
	cid    string
}

type persistResult struct {
	job persistJob
	id  int64
	ts  time.Time
	err error
}

type Hub struct {
	reg     chan *Client
	in      chan inbound
	persist chan persistJob
	saved   chan persistResult
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
	store   MessageStore

	// owned by the Run goroutine, never touched elsewhere
	clients map[*Client]struct{}
	rooms   map[string]map[*Client]struct{}
}

func New() *Hub {
	return &Hub{
		reg:     make(chan *Client),
		in:      make(chan inbound, 256),
		persist: make(chan persistJob, persistQueueSize),
		saved:   make(chan persistResult),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		clients: make(map[*Client]struct{}),
		rooms:   make(map[string]map[*Client]struct{}),
	}
}

// WithStore enables persistence. Call it before Run.
func (h *Hub) WithStore(s MessageStore) *Hub {
	h.store = s
	return h
}

// Run is the hub's event loop. Start it once with `go h.Run()`.
func (h *Hub) Run() {
	defer close(h.done)
	if h.store != nil {
		go h.persistLoop()
	}
	for {
		select {
		case c := <-h.reg:
			h.clients[c] = struct{}{}
			h.sendTo(c, protocol.Envelope{Type: protocol.TypeWelcome, From: c.name})
		case msg := <-h.in:
			h.handle(msg)
		case res := <-h.saved:
			h.handleSaved(res)
		case <-h.stop:
			for c := range h.clients {
				h.removeClient(c)
			}
			if h.store != nil {
				close(h.persist) // only the hub sends on it, so closing here is safe
			}
			return
		}
	}
}

// persistLoop saves messages one at a time (preserving order) and hands the
// results back to the hub.
func (h *Hub) persistLoop() {
	for job := range h.persist {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		id, ts, err := h.store.SaveMessage(ctx, job.room, job.client.userID, job.text)
		cancel()
		select {
		case h.saved <- persistResult{job: job, id: id, ts: ts, err: err}:
		case <-h.done:
			return
		}
	}
}

// Register adds a client. It returns false if the hub has already stopped.
func (h *Hub) Register(c *Client) bool {
	select {
	case h.reg <- c:
		return true
	case <-h.done:
		return false
	}
}

// Unregister removes a client once everything it sent before has been
// handled: the request travels through the same queue as the client's
// frames, so warnings and acks queued earlier are still delivered.
// It is a no-op if the client was already dropped.
func (h *Hub) Unregister(c *Client) {
	h.Submit(c, protocol.Envelope{Type: protocol.TypeDisconnect})
}

// Submit hands a client's request to the hub. It returns false if the hub has stopped.
func (h *Hub) Submit(c *Client, env protocol.Envelope) bool {
	select {
	case h.in <- inbound{client: c, env: env}:
		return true
	case <-h.done:
		return false
	}
}

// Stop asks the hub to close every client and exit.
func (h *Hub) Stop() { h.once.Do(func() { close(h.stop) }) }

// Done is closed once Run has returned.
func (h *Hub) Done() <-chan struct{} { return h.done }

func (h *Hub) handle(in inbound) {
	c := in.client
	if _, ok := h.clients[c]; !ok {
		return // already dropped
	}
	env := in.env
	switch env.Type {
	case protocol.TypeJoin:
		h.join(c, env.Room)
	case protocol.TypeLeave:
		h.leave(c, env.Room)
	case protocol.TypeMessage:
		h.message(c, env)
	case protocol.TypeTyping:
		h.typing(c, env.Room)
	case protocol.TypeInvalid:
		h.sendError(c, "malformed JSON")
	case protocol.TypeRateLimited:
		h.sendError(c, "rate limit exceeded, slow down")
	case protocol.TypeDisconnect:
		h.removeClient(c)
	default:
		h.sendError(c, "unknown message type")
	}
}

func (h *Hub) join(c *Client, room string) {
	switch {
	case !protocol.ValidName(room):
		h.sendError(c, "invalid room name")
		return
	case c.inRoom(room):
		h.sendError(c, "already in that room")
		return
	case len(c.rooms) >= maxRoomsPerClient:
		h.sendError(c, "too many rooms")
		return
	}

	members := h.rooms[room]
	if members == nil {
		members = make(map[*Client]struct{})
		h.rooms[room] = members
	}
	members[c] = struct{}{}
	c.rooms[room] = struct{}{}

	h.sendTo(c, protocol.Envelope{Type: protocol.TypeJoined, Room: room, Users: h.users(room)})
	if _, ok := h.clients[c]; !ok {
		return // dropped while replying
	}
	h.broadcast(room, protocol.Envelope{
		Type: protocol.TypePresence, Room: room, From: c.name, Event: "joined",
	}, c)
}

func (h *Hub) leave(c *Client, room string) {
	if !c.inRoom(room) {
		h.sendError(c, "not in that room")
		return
	}
	h.leaveRoom(c, room)
	h.sendTo(c, protocol.Envelope{Type: protocol.TypeLeft, Room: room})
}

func (h *Hub) message(c *Client, env protocol.Envelope) {
	if len(env.CID) > protocol.MaxCIDLen {
		h.sendError(c, "invalid cid")
		return
	}
	cid := env.CID
	if !c.inRoom(env.Room) {
		h.sendErrorCID(c, "join the room first", cid)
		return
	}
	text := strings.TrimSpace(env.Text)
	if text == "" || utf8.RuneCountInString(text) > protocol.MaxTextLen {
		h.sendErrorCID(c, "text must be 1-2000 characters", cid)
		return
	}

	if h.store == nil {
		h.broadcast(env.Room, protocol.Envelope{
			Type: protocol.TypeMessage, Room: env.Room, From: c.name, Text: text, CID: cid,
			TS: time.Now().UTC().Format(time.RFC3339Nano),
		}, nil)
		return
	}

	select {
	case h.persist <- persistJob{client: c, room: env.Room, text: text, cid: cid}:
	default:
		h.sendErrorCID(c, "server busy, try again", cid)
	}
}

// handleSaved runs when the persister has finished one message.
func (h *Hub) handleSaved(res persistResult) {
	c := res.job.client
	if res.err != nil {
		log.Printf("save message: %v", res.err)
		if _, ok := h.clients[c]; ok {
			h.sendErrorCID(c, "could not save message", res.job.cid)
		}
		return
	}
	h.broadcast(res.job.room, protocol.Envelope{
		Type: protocol.TypeMessage,
		ID:   res.id,
		CID:  res.job.cid,
		Room: res.job.room,
		From: c.name,
		Text: res.job.text,
		TS:   res.ts.UTC().Format(time.RFC3339Nano),
	}, nil)
}

func (h *Hub) typing(c *Client, room string) {
	if !c.inRoom(room) {
		h.sendError(c, "join the room first")
		return
	}
	h.broadcast(room, protocol.Envelope{Type: protocol.TypeTyping, Room: room, From: c.name}, c)
}

// broadcast sends env to every member of room except `except`.
// A member whose send buffer is full is dropped, so one slow reader
// can never stall the room.
func (h *Hub) broadcast(room string, env protocol.Envelope, except *Client) {
	data, err := json.Marshal(env)
	if err != nil {
		log.Printf("marshal: %v", err)
		return
	}
	var slow []*Client
	for c := range h.rooms[room] {
		if c == except {
			continue
		}
		select {
		case c.send <- data:
		default:
			slow = append(slow, c)
		}
	}
	// remove after the loop: removal changes the maps being iterated above
	for _, c := range slow {
		log.Printf("dropping slow client %q", c.name)
		h.removeClient(c)
	}
}

func (h *Hub) sendTo(c *Client, env protocol.Envelope) {
	data, err := json.Marshal(env)
	if err != nil {
		log.Printf("marshal: %v", err)
		return
	}
	select {
	case c.send <- data:
	default:
		log.Printf("dropping slow client %q", c.name)
		h.removeClient(c)
	}
}

func (h *Hub) sendError(c *Client, msg string) { h.sendErrorCID(c, msg, "") }

func (h *Hub) sendErrorCID(c *Client, msg, cid string) {
	h.sendTo(c, protocol.Envelope{Type: protocol.TypeError, Error: msg, CID: cid})
}

func (h *Hub) leaveRoom(c *Client, room string) {
	members := h.rooms[room]
	delete(members, c)
	delete(c.rooms, room)
	if len(members) == 0 {
		delete(h.rooms, room)
		return
	}
	h.broadcast(room, protocol.Envelope{
		Type: protocol.TypePresence, Room: room, From: c.name, Event: "left",
	}, nil)
}

// removeClient is the only place a client's send channel is closed,
// and it runs only on the hub goroutine, so it can never close twice.
func (h *Hub) removeClient(c *Client) {
	if _, ok := h.clients[c]; !ok {
		return
	}
	delete(h.clients, c)
	for room := range c.rooms {
		h.leaveRoom(c, room)
	}
	close(c.send)
}

func (h *Hub) users(room string) []string {
	names := make([]string, 0, len(h.rooms[room]))
	for c := range h.rooms[room] {
		names = append(names, c.name)
	}
	sort.Strings(names)
	return names
}
