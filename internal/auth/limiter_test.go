package auth

import (
	"fmt"
	"testing"
	"time"
)

func newTestLimiter(now *time.Time, maxPerIP, maxAll int) *LoginLimiter {
	limiter := NewLoginLimiter(maxPerIP, maxAll, 15*time.Minute)
	limiter.now = func() time.Time { return *now }
	return limiter
}

func TestLoginLimiterBlocksAllClientsAfterFailures(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	limiter := newTestLimiter(&now, 2, 5)
	for i := range 5 {
		if _, ok := limiter.Allow(fmt.Sprintf("192.0.2.%d", i+1)); !ok {
			t.Fatalf("attempt %d blocked", i+1)
		}
	}
	now = now.Add(time.Minute)
	wait, ok := limiter.Allow("198.51.100.9")
	if ok || wait != 14*time.Minute {
		t.Fatalf("Allow = %v, %v; want blocked for 14m", wait, ok)
	}
	if _, ok := limiter.Allow(""); ok {
		t.Fatal("unknown IP was not blocked by the all-clients limit")
	}
	now = now.Add(14 * time.Minute)
	if _, ok := limiter.Allow("198.51.100.9"); !ok {
		t.Fatal("still blocked after the window expired")
	}
}

func TestLoginLimiterBlocksOneIPFirst(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	limiter := newTestLimiter(&now, 3, 10)
	for i := range 3 {
		if _, ok := limiter.Allow("192.0.2.1"); !ok {
			t.Fatalf("attempt %d blocked", i+1)
		}
	}
	if _, ok := limiter.Allow("192.0.2.1"); ok {
		t.Fatal("IP was not blocked after three failures")
	}
	if _, ok := limiter.Allow("192.0.2.2"); !ok {
		t.Fatal("different IP was blocked")
	}
	if f := limiter.failures[allClientsKey]; f.count != 4 {
		t.Fatalf("all-clients failures = %d, want 4 (a blocked attempt is not counted)", f.count)
	}
}

func TestLoginLimiterSuccessClearsFailures(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	limiter := newTestLimiter(&now, 5, 20)
	for range 3 {
		limiter.Allow("192.0.2.1")
	}
	limiter.Allow("192.0.2.2")
	limiter.Succeed("192.0.2.2")
	if _, ok := limiter.failures[allClientsKey]; ok {
		t.Fatal("success did not clear the all-clients failures")
	}
	if _, ok := limiter.failures["ip:192.0.2.2"]; ok {
		t.Fatal("success counted against the client IP")
	}
	if f := limiter.failures["ip:192.0.2.1"]; f.count != 3 {
		t.Fatalf("other IP failures = %d, want 3", f.count)
	}
	for i := range 5 {
		if _, ok := limiter.Allow("192.0.2.3"); !ok {
			t.Fatalf("attempt %d after success blocked", i+1)
		}
	}
}

func TestLoginLimiterSweepsExpiredEntries(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	limiter := newTestLimiter(&now, 5, 20)
	limiter.Allow("192.0.2.1")
	now = now.Add(16 * time.Minute)
	limiter.Allow("192.0.2.2")
	if len(limiter.failures) != 2 || limiter.failures["ip:192.0.2.1"].count != 0 {
		t.Fatalf("failures = %v, want only the latest attempt", limiter.failures)
	}
}

func TestLoginLimiterSkipsUnknownIP(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	limiter := newTestLimiter(&now, 5, 20)
	for i := range 5 {
		if _, ok := limiter.Allow(""); !ok {
			t.Fatalf("attempt %d with unknown IP blocked", i+1)
		}
	}
	if _, ok := limiter.failures["ip:"]; ok {
		t.Fatal("unknown IP was tracked")
	}
	limiter.Succeed("")
	if len(limiter.failures) != 0 {
		t.Fatalf("failures = %v after success, want none", limiter.failures)
	}
}

func TestLoginLimiterCapsEntries(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	limiter := newTestLimiter(&now, 100, 100)
	limiter.maxEntries = 3
	for i := range 10 {
		now = now.Add(time.Second)
		limiter.Allow(fmt.Sprintf("192.0.2.%d", i))
		if len(limiter.failures) > 3 {
			t.Fatalf("entries = %d, want at most 3", len(limiter.failures))
		}
	}
	for _, key := range []string{"ip:192.0.2.8", "ip:192.0.2.9"} {
		if _, ok := limiter.failures[key]; !ok {
			t.Fatalf("newest entry %s was evicted: %v", key, limiter.failures)
		}
	}
}
