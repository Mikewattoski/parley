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
