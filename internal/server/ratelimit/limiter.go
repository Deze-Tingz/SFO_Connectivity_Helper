package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter provides per-IP rate limiting
type Limiter struct {
	mu       sync.Mutex
	limiters map[string]*clientLimiter
	rate     rate.Limit
	burst    int
	cleanup  time.Duration
}

type clientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewLimiter creates a new rate limiter
func NewLimiter(r float64, burst int) *Limiter {
	l := &Limiter{
		limiters: make(map[string]*clientLimiter),
		rate:     rate.Limit(r),
		burst:    burst,
		cleanup:  3 * time.Minute,
	}
	go l.cleanupLoop()
	return l
}

// Allow checks if a request from the given IP is allowed
func (l *Limiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	client, ok := l.limiters[ip]
	if !ok {
		client = &clientLimiter{
			limiter: rate.NewLimiter(l.rate, l.burst),
		}
		l.limiters[ip] = client
	}

	client.lastSeen = time.Now()
	return client.limiter.Allow()
}

func (l *Limiter) cleanupLoop() {
	ticker := time.NewTicker(l.cleanup)
	for range ticker.C {
		l.doCleanup()
	}
}

func (l *Limiter) doCleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	threshold := time.Now().Add(-l.cleanup)
	for ip, client := range l.limiters {
		if client.lastSeen.Before(threshold) {
			delete(l.limiters, ip)
		}
	}
}

// MultiLimiter provides different rate limits for different operations
type MultiLimiter struct {
	create *Limiter
	join   *Limiter
}

// NewMultiLimiter creates limiters for create and join operations
func NewMultiLimiter() *MultiLimiter {
	return &MultiLimiter{
		create: NewLimiter(10.0/60.0, 3),
		join:   NewLimiter(30.0/60.0, 10),
	}
}

// AllowCreate checks if a create request is allowed
func (m *MultiLimiter) AllowCreate(ip string) bool {
	return m.create.Allow(ip)
}

// AllowJoin checks if a join request is allowed
func (m *MultiLimiter) AllowJoin(ip string) bool {
	return m.join.Allow(ip)
}
