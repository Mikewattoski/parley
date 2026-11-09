// Package hub routes chat messages between connected clients and rooms.
//
// One goroutine (Run) owns all room state, so no locks are needed:
// everything else talks to it through channels.
package hub

import (
	"encoding/json"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Mikewattoski/parley/internal/protocol"
)

const maxRoomsPerClient = 20

type inbound struct {
	client *Client
	env    protocol.Envelope
}

type Hub struct {
	reg   chan *Client
	unreg chan *Client
	in    chan inbound
	stop  chan struct{}
	done  chan struct{}
	once  sync.Once

	// owned by the Run goroutine, never touched elsewhere
	clients map[*Client]struct{}
	rooms   map[string]map[*Client]struct{}
}

func New() *Hub {
	return &Hub{
		reg:     make(chan *Client),
		unreg:   make(chan *Client),
		in:      make(chan inbound, 256),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		clients: make(map[*Client]struct{}),
		rooms:   make(map[string]map[*Client]struct{}),
	}
}

// Run is the hub's event loop. Start it once with `go h.Run()`.
func (h *Hub) Run() {
	defer close(h.done)
	for {
		select {
		case c := <-h.reg:
			h.clients[c] = struct{}{}
			h.sendTo(c, protocol.Envelope{Type: protocol.TypeWelcome, From: c.name})
		case c := <-h.unreg:
			h.removeClient(c)
		case msg := <-h.in:
			h.handle(msg)
		case <-h.stop:
			for c := range h.clients {
				h.removeClient(c)
			}
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

// Unregister removes a client (a no-op if it was already dropped).
func (h *Hub) Unregister(c *Client) {
	select {
	case h.unreg <- c:
	case <-h.done:
	}
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
	if !c.inRoom(env.Room) {
		h.sendError(c, "join the room first")
		return
	}
	text := strings.TrimSpace(env.Text)
	if text == "" || utf8.RuneCountInString(text) > protocol.MaxTextLen {
		h.sendError(c, "text must be 1-2000 characters")
		return
	}
	h.broadcast(env.Room, protocol.Envelope{
		Type: protocol.TypeMessage,
		Room: env.Room,
		From: c.name,
		Text: text,
		TS:   time.Now().UTC().Format(time.RFC3339Nano),
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

func (h *Hub) sendError(c *Client, msg string) {
	h.sendTo(c, protocol.Envelope{Type: protocol.TypeError, Error: msg})
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
