package hub

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Mikewattoski/parley/internal/protocol"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = pongWait * 9 / 10
	maxMessageSize = 4096
	sendBuffer     = 256
)

// Client is one WebSocket connection.
type Client struct {
	hub   *Hub
	conn  *websocket.Conn
	name  string
	send  chan []byte         // closed only by the hub
	rooms map[string]struct{} // owned by the hub goroutine
}

func NewClient(h *Hub, conn *websocket.Conn, name string) *Client {
	return newClient(h, conn, name, sendBuffer)
}

func newClient(h *Hub, conn *websocket.Conn, name string, buf int) *Client {
	return &Client{
		hub:   h,
		conn:  conn,
		name:  name,
		send:  make(chan []byte, buf),
		rooms: make(map[string]struct{}),
	}
}

func (c *Client) inRoom(room string) bool {
	_, ok := c.rooms[room]
	return ok
}

// Serve registers the client and runs its pumps. It blocks until the
// connection ends, so call it from the HTTP handler goroutine.
func (c *Client) Serve() {
	if !c.hub.Register(c) {
		_ = c.conn.Close()
		return
	}
	go c.writePump()
	c.readPump()
}

// readPump reads frames from the socket and forwards them to the hub.
func (c *Client) readPump() {
	defer func() {
		c.hub.Unregister(c)
		_ = c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
				websocket.CloseNoStatusReceived) {
				log.Printf("client %q read error: %v", c.name, err)
			}
			return
		}
		var env protocol.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			env = protocol.Envelope{Type: protocol.TypeInvalid}
		}
		if !c.hub.Submit(c, env) {
			return
		}
	}
}

// writePump is the only goroutine that writes to the socket
// (gorilla/websocket allows one concurrent writer).
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok { // the hub closed the channel: say goodbye properly
				_ = c.conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
