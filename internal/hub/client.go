package hub

import (
	"encoding/json"
	"log"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"

	"github.com/Mikewattoski/parley/internal/protocol"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = pongWait * 9 / 10
	maxMessageSize = 4096
	sendBuffer     = 256

	// per-connection limit: 5 frames/s, bursts of 10, disconnect after
	// 20 consecutive frames over the limit
	messageRate    rate.Limit = 5
	messageBurst              = 10
	maxRateStrikes            = 20
)

// Client is one authenticated WebSocket connection.
type Client struct {
	hub       *Hub
	conn      *websocket.Conn
	name      string
	userID    int64
	send      chan []byte         // closed only by the hub
	rooms     map[string]struct{} // owned by the hub goroutine
	closeCode atomic.Int32        // close code the write pump sends (0 = normal closure)
}

func NewClient(h *Hub, conn *websocket.Conn, name string, userID int64) *Client {
	c := newClient(h, conn, name, sendBuffer)
	c.userID = userID
	return c
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
// connection has been closed, so call it from the HTTP handler goroutine.
func (c *Client) Serve() {
	if !c.hub.Register(c) {
		_ = c.conn.Close()
		return
	}

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		c.writePump()
	}()

	c.readPump()

	// After Unregister the hub closes c.send. The write pump then flushes
	// whatever is still queued (warnings, acks) and sends the close frame.
	<-writerDone
	_ = c.conn.Close()
}

// readPump reads frames from the socket and forwards them to the hub.
func (c *Client) readPump() {
	defer c.hub.Unregister(c)

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	limiter := rate.NewLimiter(messageRate, messageBurst)
	strikes := 0

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

		if !limiter.Allow() {
			strikes++
			if strikes >= maxRateStrikes {
				log.Printf("client %q disconnected for flooding", c.name)
				c.closeCode.Store(websocket.ClosePolicyViolation)
				return // the deferred Unregister queues the disconnect behind the warnings
			}
			if !c.hub.Submit(c, protocol.Envelope{Type: protocol.TypeRateLimited}) {
				return
			}
			continue
		}
		strikes = 0

		var env protocol.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			env = protocol.Envelope{Type: protocol.TypeInvalid}
		} else if protocol.IsInternal(env.Type) {
			env.Type = "" // internal markers cannot be sent by clients
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
				code, reason := int(c.closeCode.Load()), ""
				switch code {
				case 0:
					code = websocket.CloseNormalClosure
				case websocket.ClosePolicyViolation:
					reason = "rate limit exceeded"
				}
				_ = c.conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(code, reason))
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
