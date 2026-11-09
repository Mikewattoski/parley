// Package server exposes the hub and the REST API over HTTP.
package server

import (
	"context"
	"net/http"
	"net/url"

	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"

	"github.com/Mikewattoski/parley/internal/auth"
	"github.com/Mikewattoski/parley/internal/hub"
	"github.com/Mikewattoski/parley/internal/model"
)

// Store is what the REST handlers need from the database.
type Store interface {
	CreateUser(ctx context.Context, username, passwordHash string) (model.User, error)
	UserByUsername(ctx context.Context, username string) (model.User, error)
	ListRooms(ctx context.Context) ([]string, error)
	History(ctx context.Context, room string, before int64, limit int) ([]model.Message, error)
}

type Config struct {
	Hub       *hub.Hub
	Store     Store
	Auth      *auth.Service
	Origins   []string   // browser origins allowed to open a WebSocket ("*" = all)
	Index     []byte     // the dev test page
	AuthRate  rate.Limit // register/login attempts per second, per IP
	AuthBurst int
}

type Server struct {
	hub         *hub.Hub
	store       Store
	tokens      *auth.Service
	index       []byte
	origins     map[string]struct{}
	allowAll    bool
	upgrader    websocket.Upgrader
	authLimiter *ipLimiter
}

func New(cfg Config) *Server {
	s := &Server{
		hub:         cfg.Hub,
		store:       cfg.Store,
		tokens:      cfg.Auth,
		index:       cfg.Index,
		origins:     make(map[string]struct{}),
		authLimiter: newIPLimiter(cfg.AuthRate, cfg.AuthBurst),
	}
	for _, o := range cfg.Origins {
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

	mux.HandleFunc("POST /api/register", s.limitAuth(s.handleRegister))
	mux.HandleFunc("POST /api/login", s.limitAuth(s.handleLogin))
	mux.HandleFunc("POST /api/ws-ticket", s.requireAuth(s.handleTicket))
	mux.HandleFunc("GET /api/rooms", s.requireAuth(s.handleRooms))
	mux.HandleFunc("GET /api/rooms/{room}/messages", s.requireAuth(s.handleMessages))
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

// handleWS authenticates with a short-lived ticket BEFORE upgrading, so an
// unauthenticated connection never reaches the hub.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	id, err := s.tokens.Verify(r.URL.Query().Get("ticket"), auth.AudienceWS)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade has already written the error response
	}
	hub.NewClient(s.hub, conn, id.Username, id.UserID).Serve()
}
