package server

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	limiterCleanEvery = 5 * time.Minute
	limiterMaxIdle    = 10 * time.Minute
)

// ipLimiter is a token bucket per client IP, with idle entries swept out
// periodically so the map cannot grow without bound.
type ipLimiter struct {
	mu        sync.Mutex
	limit     rate.Limit
	burst     int
	entries   map[string]*ipEntry
	lastClean time.Time
}

type ipEntry struct {
	limiter *rate.Limiter
	seen    time.Time
}

func newIPLimiter(limit rate.Limit, burst int) *ipLimiter {
	return &ipLimiter{
		limit:     limit,
		burst:     burst,
		entries:   make(map[string]*ipEntry),
		lastClean: time.Now(),
	}
}

func (l *ipLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if now.Sub(l.lastClean) > limiterCleanEvery {
		for k, e := range l.entries {
			if now.Sub(e.seen) > limiterMaxIdle {
				delete(l.entries, k)
			}
		}
		l.lastClean = now
	}

	e, ok := l.entries[ip]
	if !ok {
		e = &ipEntry{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.entries[ip] = e
	}
	e.seen = now
	return e.limiter.Allow()
}
