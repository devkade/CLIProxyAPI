package api

import (
	"crypto/sha256"
	"math"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

type clientKeyBucket struct {
	tokens   float64
	refilled time.Time
	lastUsed uint64
}

type clientKeyRateLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	config  config.ClientKeyRateLimitConfig
	buckets map[[sha256.Size]byte]*clientKeyBucket
	useSeq  uint64
}

func newClientKeyRateLimiter(now func() time.Time) *clientKeyRateLimiter {
	if now == nil {
		now = time.Now
	}
	return &clientKeyRateLimiter{now: now, buckets: make(map[[sha256.Size]byte]*clientKeyBucket)}
}

func (l *clientKeyRateLimiter) Configure(cfg config.ClientKeyRateLimitConfig) {
	if cfg.MaxTrackedKeys == 0 {
		cfg.MaxTrackedKeys = config.DefaultClientKeyRateLimitMaxTrackedKeys
	}
	l.mu.Lock()
	if l.config == cfg {
		l.mu.Unlock()
		return
	}
	l.config = cfg
	l.buckets = make(map[[sha256.Size]byte]*clientKeyBucket)
	l.useSeq = 0
	l.mu.Unlock()
}

func (l *clientKeyRateLimiter) Allow(clientKey string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	cfg := l.config
	if !cfg.Enabled {
		return true, 0
	}

	identity := sha256.Sum256([]byte(clientKey))
	now := l.now()
	bucket := l.buckets[identity]
	if bucket == nil {
		if len(l.buckets) >= cfg.MaxTrackedKeys {
			l.evictLeastRecentlyUsed()
		}
		bucket = &clientKeyBucket{tokens: float64(cfg.Burst), refilled: now}
		l.buckets[identity] = bucket
	}

	l.useSeq++
	bucket.lastUsed = l.useSeq
	ratePerSecond := cfg.RequestsPerMinute / 60
	if elapsed := now.Sub(bucket.refilled).Seconds(); elapsed > 0 {
		bucket.tokens = math.Min(float64(cfg.Burst), bucket.tokens+elapsed*ratePerSecond)
		bucket.refilled = now
	}
	if bucket.tokens >= 1 {
		bucket.tokens--
		return true, 0
	}

	retry := time.Duration(math.Ceil((1-bucket.tokens)/ratePerSecond)) * time.Second
	if retry < time.Second {
		retry = time.Second
	}
	return false, retry
}

func (l *clientKeyRateLimiter) evictLeastRecentlyUsed() {
	var oldestIdentity [sha256.Size]byte
	var oldestSequence uint64
	found := false
	for identity, bucket := range l.buckets {
		if !found || bucket.lastUsed < oldestSequence {
			oldestIdentity = identity
			oldestSequence = bucket.lastUsed
			found = true
		}
	}
	if found {
		delete(l.buckets, oldestIdentity)
	}
}

func (l *clientKeyRateLimiter) trackedKeys() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

func (l *clientKeyRateLimiter) hasKey(clientKey string) bool {
	identity := sha256.Sum256([]byte(clientKey))
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.buckets[identity]
	return ok
}
