# Parley

Real-time multi-room chat server in Go, built on WebSockets.

**Status:** work in progress. Phase 1 (real-time core) is done: rooms, broadcast, presence, typing events, heartbeats and backpressure. Accounts, history, a React frontend, scaling and load-test results come next.

## Run

```bash
make run          # http://localhost:8081 serves a minimal test page
make test         # go test -race ./...
```
