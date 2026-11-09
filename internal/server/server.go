// Package server exposes the hub over HTTP and WebSocket.
package server

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"

	"github.com/gorilla/websocket"

	"github.com/Mikewattoski/parley/internal/hub"
	"github.com/Mikewattoski/parley/internal/protocol"
)

type Server struct {
	hub      *hub.Hub
	index    []byte
	origins  map[string]struct{}
	allowAll bool
	upgrader websocket.Upgrader
}

// New builds the server. origins lists browser origins allowed to connect
// ("*" allows all); pages served by this server itself are always allowed.
func New(h *hub.Hub, origins []string, index []byte) *Server {
	s := &Server{hub: h, index: index, origins: make(map[string]struct{})}
	for _, o := range origins {
		if o == "*" {
			s.allowAll = true
			continue
		}
		s.origins[o] = struct{}{}
	}
	s.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     s.checkOrigin,
	}
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /ws", s.handleWS)
	return mux
}

func (s *Server) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // not a browser (CLI tools, tests)
	}
	if s.allowAll {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Host == r.Host {
		return true // the page came from this server
	}
	_, ok := s.origins[origin]
	return ok
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(s.index)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("user")
	if name == "" {
		name = fmt.Sprintf("guest-%04d", rand.IntN(10000))
	}
	if !protocol.ValidName(name) {
		http.Error(w, "invalid user name", http.StatusBadRequest)
		return
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade has already written the error response
	}
	hub.NewClient(s.hub, conn, name).Serve()
}
