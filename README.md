# Parley

Real-time multi-room chat server in Go, built on WebSockets, PostgreSQL and JWT.

**Status:** work in progress. Done: real-time core (rooms, presence, typing events, heartbeats, backpressure), accounts, authenticated sockets, saved messages with paginated history, and rate limiting. Next: a React frontend, Redis fan-out across instances, load-test results, Docker and CI.

## Run

```bash
cp .env.example .env
sed -i "s/^JWT_SECRET=.*/JWT_SECRET=$(openssl rand -hex 32)/" .env

make up           # PostgreSQL + migrations (port 5433)
make run          # http://localhost:8081 serves a minimal test page
make test         # go test -race ./...
```

## Frontend

A React + TypeScript + Redux Toolkit app in `web/`.

```bash
make web-install  # once
make run          # terminal 1: Go server on :8081
make web-dev      # terminal 2: http://localhost:5173 (proxies /api and /ws to the server)
make web-test     # Vitest + Testing Library
```

- **The WebSocket lives in a Redux middleware**, not in a component. It survives re-renders, components only dispatch actions, and the server's frames come back as ordinary actions.
- **Optimistic sending:** a message appears immediately with a client-generated `cid`. The server's echo carries the same `cid` and the saved `id`, which confirms it. An error with that `cid`, a lost connection, or a 10 s timeout marks it failed, with a Retry button.
- **Auto-reconnect:** exponential backoff with jitter (up to 30 s), a fresh 30-second ticket for every attempt, rooms rejoined, and the latest history re-fetched to fill the gap. A 401 on the ticket means the session expired, so the app returns to the login screen.
- **History:** the latest 50 messages, then "Load older" by message-id cursor with the scroll position preserved. The view only follows new messages when the reader is already at the bottom.
- **Responsive and accessible:** the room list becomes a drawer on phones, messages are announced through a live region, Enter sends and Shift+Enter adds a line, and motion respects `prefers-reduced-motion`.

## How it works

- **Accounts:** passwords are hashed with bcrypt. Login returns a 24-hour JWT for the REST API.
- **WebSocket auth:** `POST /api/ws-ticket` returns a 30-second, single-purpose JWT, passed as `/ws?ticket=...`. It is verified before the connection is upgraded, so unauthenticated sockets never reach the hub, and the long-lived token never appears in a URL.
- **Save, then broadcast:** the hub never waits on the database. A persister goroutine saves each message and hands it back, and the hub then broadcasts it with its database ID and the sender's `cid` as an acknowledgement.
- **History:** paginated by message ID, which is unique and monotonic.
- **Limits:** 4 KB frames, 2,000-character messages, 5 frames/s per connection (then a warning, then a disconnect), and per-IP throttling on register and login.

## API

| Method | Path | Auth | Description |
|---|---|---|---|
| POST | `/api/register` | none | Create an account, returns a token |
| POST | `/api/login` | none | Returns a token |
| POST | `/api/ws-ticket` | Bearer | 30-second WebSocket ticket |
| GET | `/api/rooms` | Bearer | List rooms |
| GET | `/api/rooms/{room}/messages?before=&limit=` | Bearer | History, newest first |
| GET | `/ws?ticket=` | ticket | WebSocket |

WebSocket frames are JSON: `join`, `leave`, `message` (with an optional `cid`) and `typing` from clients; `welcome`, `joined`, `presence`, `message`, `typing` and `error` from the server.
