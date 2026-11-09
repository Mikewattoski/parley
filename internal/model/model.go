// Package model holds the types shared between packages.
package model

import (
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
)

type User struct {
	ID           int64
	Username     string
	PasswordHash string
}

type Message struct {
	ID   int64     `json:"id"`
	Room string    `json:"room"`
	From string    `json:"from"`
	Text string    `json:"text"`
	TS   time.Time `json:"ts"`
}
