// Package auth throttles failed logins.
package auth

import (
	"sync"
	"time"
)

// LoginLimiter throttles failed logins per client IP and across all clients.
// DejaView has one shared token, so the all-clients limit stands in for a
// per-username limit: it caps guessing spread over many IPs, and is the only
// limit when the client IP is unknown. It is higher than the per-IP limit, so
// one client cannot use it up alone. Each attempt is reserved before the
// token is checked, so concurrent requests cannot all get past the limit; a
// successful login releases its IP reservation and clears the all-clients
// failures.
type LoginLimiter struct {
	mu         sync.Mutex
	maxPerIP   int
	maxAll     int
	window     time.Duration
	maxEntries int
	now        func() time.Time
	failures   map[string]failureWindow
	lastSweep  time.Time
}

// maxLimiterEntries caps the tracked keys so unauthenticated callers cannot
// grow the map without bound; past it, the oldest window is evicted.
const maxLimiterEntries = 10000

// allClientsKey counts failures from every client.
const allClientsKey = "all"

type failureWindow struct {
	count int
	start time.Time
}

// NewLoginLimiter allows maxPerIP failures from one client IP, and maxAll
// failures from all clients together, in each window.
func NewLoginLimiter(maxPerIP, maxAll int, window time.Duration) *LoginLimiter {
	return &LoginLimiter{maxPerIP: maxPerIP, maxAll: maxAll, window: window, maxEntries: maxLimiterEntries, now: time.Now, failures: make(map[string]failureWindow)}
}

// Allow reserves a login attempt for ip. An empty ip, used when the real
// client IP is unknown, skips the per-IP limit. When either the IP or all
// clients have reached the failure limit, it returns false and the time
// until the oldest window expires.
func (l *LoginLimiter) Allow(ip string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	keys := limiterKeys(ip)
	var wait time.Duration
	for _, key := range keys {
		f, ok := l.active(key, now)
		if ok && f.count >= l.limit(key) {
			wait = max(wait, f.start.Add(l.window).Sub(now))
		}
	}
	if wait > 0 {
		return wait, false
	}
	for _, key := range keys {
		f, ok := l.active(key, now)
		if !ok {
			l.makeRoom(now)
			f = failureWindow{start: now}
		}
		f.count++
		l.failures[key] = f
	}
	return 0, true
}

// Succeed releases the IP reservation made by Allow and clears the
// all-clients failures.
func (l *LoginLimiter) Succeed(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, allClientsKey)
	if ip == "" {
		return
	}
	key := "ip:" + ip
	if f, ok := l.active(key, l.now()); ok && f.count > 1 {
		f.count--
		l.failures[key] = f
	} else {
		delete(l.failures, key)
	}
}

func (l *LoginLimiter) active(key string, now time.Time) (failureWindow, bool) {
	f, ok := l.failures[key]
	if ok && !now.Before(f.start.Add(l.window)) {
		delete(l.failures, key)
		return failureWindow{}, false
	}
	return f, ok
}

func (l *LoginLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < l.window {
		return
	}
	l.lastSweep = now
	for key := range l.failures {
		l.active(key, now)
	}
}

// makeRoom keeps the map below maxEntries before a new key is added, first by
// dropping expired windows and then by evicting the oldest one.
func (l *LoginLimiter) makeRoom(now time.Time) {
	if len(l.failures) < l.maxEntries {
		return
	}
	l.lastSweep = time.Time{}
	l.sweep(now)
	for len(l.failures) >= l.maxEntries {
		var oldestKey string
		var oldest time.Time
		for key, f := range l.failures {
			if oldestKey == "" || f.start.Before(oldest) {
				oldestKey, oldest = key, f.start
			}
		}
		delete(l.failures, oldestKey)
	}
}

func (l *LoginLimiter) limit(key string) int {
	if key == allClientsKey {
		return l.maxAll
	}
	return l.maxPerIP
}

func limiterKeys(ip string) []string {
	keys := []string{allClientsKey}
	if ip != "" {
		keys = append(keys, "ip:"+ip)
	}
	return keys
}
