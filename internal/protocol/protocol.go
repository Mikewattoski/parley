// Package protocol defines the JSON messages exchanged over the WebSocket.
package protocol

import "regexp"

// Types sent by clients (message and typing are also sent by the server).
const (
	TypeJoin    = "join"
	TypeLeave   = "leave"
	TypeMessage = "message"
	TypeTyping  = "typing"
)

// Types sent only by the server.
const (
	TypeWelcome  = "welcome"
	TypeJoined   = "joined"
	TypeLeft     = "left"
	TypePresence = "presence"
	TypeError    = "error"
)

// Internal markers set by the read pump; never sent by real clients.
const (
	TypeInvalid     = "invalid"     // frame was not valid JSON
	TypeRateLimited = "ratelimited" // frame exceeded the per-connection rate
	TypeDisconnect  = "disconnect"  // the connection ended (queued behind its earlier frames)
)

const (
	MaxTextLen     = 2000 // characters
	MaxCIDLen      = 64
	MinUsernameLen = 3
)

var namePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// IsInternal reports whether t is a marker that only the server may generate.
func IsInternal(t string) bool {
	return t == TypeInvalid || t == TypeRateLimited || t == TypeDisconnect
}

// ValidName reports whether s is a legal room name.
func ValidName(s string) bool { return namePattern.MatchString(s) }

// ValidUsername reports whether s is a legal username.
func ValidUsername(s string) bool { return len(s) >= MinUsernameLen && ValidName(s) }

// Envelope is the single JSON shape used in both directions.
type Envelope struct {
	Type  string   `json:"type"`
	ID    int64    `json:"id,omitempty"`  // database id of a saved message
	CID   string   `json:"cid,omitempty"` // client-chosen id, echoed back as an ack
	Room  string   `json:"room,omitempty"`
	From  string   `json:"from,omitempty"`
	Text  string   `json:"text,omitempty"`
	Event string   `json:"event,omitempty"` // presence: joined | left
	Users []string `json:"users,omitempty"` // joined: current members
	Error string   `json:"error,omitempty"`
	TS    string   `json:"ts,omitempty"`
}
