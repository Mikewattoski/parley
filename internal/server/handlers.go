package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/Mikewattoski/parley/internal/auth"
	"github.com/Mikewattoski/parley/internal/model"
	"github.com/Mikewattoski/parley/internal/protocol"
)

const (
	defaultHistoryLimit = 50
	maxHistoryLimit     = 100
)

type ctxKey struct{}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type userView struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type authResponse struct {
	Token string   `json:"token"`
	User  userView `json:"user"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func identityFrom(r *http.Request) auth.Identity {
	id, _ := r.Context().Value(ctxKey{}).(auth.Identity)
	return id
}

// limitAuth throttles register/login attempts per client IP.
func (s *Server) limitAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authLimiter.Allow(clientIP(r)) {
			w.Header().Set("Retry-After", "10")
			writeError(w, http.StatusTooManyRequests, "too many attempts, try again shortly")
			return
		}
		next(w, r)
	}
}

// requireAuth accepts only API tokens (audience "api"), not WebSocket tickets.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, prefix) {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		id, err := s.tokens.Verify(strings.TrimPrefix(header, prefix), auth.AudienceAPI)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
	}
}

func (s *Server) respondWithToken(w http.ResponseWriter, status int, u model.User) {
	token, err := s.tokens.Issue(u.ID, u.Username, auth.AudienceAPI, auth.APITokenTTL)
	if err != nil {
		log.Printf("issue token: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, status, authResponse{Token: token, User: userView{ID: u.ID, Username: u.Username}})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decodeJSON(w, r, &in) {
		return
	}
	if !protocol.ValidUsername(in.Username) {
		writeError(w, http.StatusBadRequest, "username must be 3-32 characters: letters, digits, _ or -")
		return
	}
	if err := auth.ValidatePassword(in.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		log.Printf("hash password: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	u, err := s.store.CreateUser(r.Context(), in.Username, hash)
	switch {
	case errors.Is(err, model.ErrConflict):
		writeError(w, http.StatusConflict, "username already taken")
		return
	case err != nil:
		log.Printf("create user: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.respondWithToken(w, http.StatusCreated, u)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decodeJSON(w, r, &in) {
		return
	}
	u, err := s.store.UserByUsername(r.Context(), in.Username)
	switch {
	case errors.Is(err, model.ErrNotFound):
		// same cost and same answer as a wrong password: no user enumeration
		auth.BurnPasswordCheck(in.Password)
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	case err != nil:
		log.Printf("login lookup: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !auth.CheckPassword(u.PasswordHash, in.Password) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	s.respondWithToken(w, http.StatusOK, u)
}

func (s *Server) handleTicket(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r)
	ticket, err := s.tokens.Issue(id.UserID, id.Username, auth.AudienceWS, auth.WSTicketTTL)
	if err != nil {
		log.Printf("issue ticket: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ticket":     ticket,
		"expires_in": int(auth.WSTicketTTL.Seconds()),
	})
}

func (s *Server) handleRooms(w http.ResponseWriter, r *http.Request) {
	rooms, err := s.store.ListRooms(r.Context())
	if err != nil {
		log.Printf("list rooms: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if rooms == nil {
		rooms = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": rooms})
}

// handleMessages returns a room's history, newest first, paginated by id.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	room := r.PathValue("room")
	if !protocol.ValidName(room) {
		writeError(w, http.StatusBadRequest, "invalid room name")
		return
	}

	var before int64
	if v := r.URL.Query().Get("before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "before must be a non-negative integer")
			return
		}
		before = n
	}

	limit := defaultHistoryLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = min(n, maxHistoryLimit)
	}

	// fetch one extra row to know whether another page exists
	items, err := s.store.History(r.Context(), room, before, limit+1)
	if err != nil {
		log.Printf("history: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if items == nil {
		items = []model.Message{}
	}

	var next *int64
	if len(items) > limit {
		items = items[:limit]
		id := items[limit-1].ID
		next = &id
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
}
