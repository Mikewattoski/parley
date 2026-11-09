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

// TypeInvalid is set by the read pump when a frame is not valid JSON.
const TypeInvalid = "invalid"

const MaxTextLen = 2000 // characters

var namePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// ValidName reports whether s is a legal user or room name.
func ValidName(s string) bool { return namePattern.MatchString(s) }

// Envelope is the single JSON shape used in both directions.
type Envelope struct {
	Type  string   `json:"type"`
	Room  string   `json:"room,omitempty"`
	From  string   `json:"from,omitempty"`
	Text  string   `json:"text,omitempty"`
	Event string   `json:"event,omitempty"` // presence: joined | left
	Users []string `json:"users,omitempty"` // joined: current members
	Error string   `json:"error,omitempty"`
	TS    string   `json:"ts,omitempty"`
}
