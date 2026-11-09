// Package store is the PostgreSQL implementation of the persistence layer.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Mikewattoski/parley/internal/model"
)

type Postgres struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

func (p *Postgres) CreateUser(ctx context.Context, username, passwordHash string) (model.User, error) {
	u := model.User{}
	err := p.pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id, username`,
		username, passwordHash).Scan(&u.ID, &u.Username)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return model.User{}, model.ErrConflict
	}
	if err != nil {
		return model.User{}, err
	}
	return u, nil
}

func (p *Postgres) UserByUsername(ctx context.Context, username string) (model.User, error) {
	var u model.User
	err := p.pool.QueryRow(ctx,
		`SELECT id, username, password_hash FROM users WHERE lower(username) = lower($1)`,
		username).Scan(&u.ID, &u.Username, &u.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.User{}, model.ErrNotFound
	}
	return u, err
}

func (p *Postgres) ListRooms(ctx context.Context) ([]string, error) {
	rows, err := p.pool.Query(ctx, `SELECT name FROM rooms ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// SaveMessage stores a message, creating the room on first use.
func (p *Postgres) SaveMessage(ctx context.Context, room string, userID int64, text string) (int64, time.Time, error) {
	if _, err := p.pool.Exec(ctx,
		`INSERT INTO rooms (name) VALUES ($1) ON CONFLICT (name) DO NOTHING`, room); err != nil {
		return 0, time.Time{}, err
	}
	var id int64
	var ts time.Time
	err := p.pool.QueryRow(ctx,
		`INSERT INTO messages (room, user_id, text) VALUES ($1, $2, $3) RETURNING id, created_at`,
		room, userID, text).Scan(&id, &ts)
	return id, ts, err
}

// History returns up to limit messages of a room, newest first.
// before = 0 starts from the newest; otherwise only messages with id < before.
func (p *Postgres) History(ctx context.Context, room string, before int64, limit int) ([]model.Message, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT m.id, m.room, u.username, m.text, m.created_at
		FROM messages m
		JOIN users u ON u.id = m.user_id
		WHERE m.room = $1 AND ($2::bigint = 0 OR m.id < $2)
		ORDER BY m.id DESC
		LIMIT $3`, room, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Message{}
	for rows.Next() {
		var m model.Message
		if err := rows.Scan(&m.ID, &m.Room, &m.From, &m.Text, &m.TS); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
