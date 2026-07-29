package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	proxyconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func testRateLimitConfig(burst, max int) proxyconfig.ClientKeyRateLimitConfig {
	return proxyconfig.ClientKeyRateLimitConfig{Enabled: true, RequestsPerMinute: 60, Burst: burst, MaxTrackedKeys: max}
}

func TestClientKeyRateLimiterBurstRefillAndIndependentKeys(t *testing.T) {
	now := time.Unix(100, 0)
	limiter := newClientKeyRateLimiter(func() time.Time { return now })
	limiter.Configure(testRateLimitConfig(2, 10))
	for i := 0; i < 2; i++ {
		if ok, _ := limiter.Allow("key-a"); !ok {
			t.Fatalf("request %d limited", i+1)
		}
	}
	if ok, retry := limiter.Allow("key-a"); ok || retry != time.Second {
		t.Fatalf("overflow = (%t, %s)", ok, retry)
	}
	if ok, _ := limiter.Allow("key-b"); !ok {
		t.Fatal("key-b did not receive independent bucket")
	}
	now = now.Add(time.Second)
	if ok, _ := limiter.Allow("key-a"); !ok {
		t.Fatal("token did not refill")
	}
}

func TestClientKeyRateLimiterConcurrentBurst(t *testing.T) {
	limiter := newClientKeyRateLimiter(func() time.Time { return time.Unix(100, 0) })
	limiter.Configure(testRateLimitConfig(8, 10))
	start := make(chan struct{})
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if ok, _ := limiter.Allow("shared-key"); ok {
				allowed.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := allowed.Load(); got != 8 {
		t.Fatalf("allowed = %d, want 8", got)
	}
}

func TestClientKeyRateLimiterDeterministicBoundedEviction(t *testing.T) {
	limiter := newClientKeyRateLimiter(func() time.Time { return time.Unix(100, 0) })
	limiter.Configure(testRateLimitConfig(1, 2))
	limiter.Allow("key-a")
	limiter.Allow("key-b")
	limiter.Allow("key-a")
	limiter.Allow("key-c")
	if got := limiter.trackedKeys(); got != 2 {
		t.Fatalf("tracked = %d, want 2", got)
	}
	if limiter.hasKey("key-b") || !limiter.hasKey("key-a") || !limiter.hasKey("key-c") {
		t.Fatal("wrong bucket evicted")
	}
}

func TestClientKeyRateLimitMiddlewareDisabledAnd429Format(t *testing.T) {
	server := newTestServer(t)
	request := func(key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		rr := httptest.NewRecorder()
		server.engine.ServeHTTP(rr, req)
		return rr
	}
	for i := 0; i < 3; i++ {
		if rr := request("test-key"); rr.Code == http.StatusTooManyRequests {
			t.Fatal("disabled limiter returned 429")
		}
	}
	cfg := *server.cfg
	cfg.ClientKeyRateLimit = testRateLimitConfig(1, 10)
	server.UpdateClients(&cfg)
	if rr := request("test-key"); rr.Code == http.StatusTooManyRequests {
		t.Fatalf("first request: %s", rr.Body.String())
	}
	rr := request("test-key")
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q", got)
	}
	var body struct {
		Error struct{ Message, Type, Code string } `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Type != "rate_limit_error" || body.Error.Code != "rate_limit_exceeded" {
		t.Fatalf("error = %#v", body.Error)
	}
	if strings.Contains(rr.Body.String(), "test-key") {
		t.Fatalf("key leaked: %s", rr.Body.String())
	}
}

func TestClientKeyRateLimitRunsAfterAuthAndHotReloads(t *testing.T) {
	server := newTestServer(t)
	cfg := *server.cfg
	cfg.ClientKeyRateLimit = testRateLimitConfig(1, 10)
	server.UpdateClients(&cfg)
	invalid := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	invalid.Header.Set("Authorization", "Bearer invalid-secret")
	invalidRR := httptest.NewRecorder()
	server.engine.ServeHTTP(invalidRR, invalid)
	if invalidRR.Code != http.StatusUnauthorized {
		t.Fatalf("invalid status = %d", invalidRR.Code)
	}
	if got := server.clientKeyRateLimiter.trackedKeys(); got != 0 {
		t.Fatalf("invalid key created %d buckets", got)
	}
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		req.Header.Set("Authorization", "Bearer test-key")
		rr := httptest.NewRecorder()
		server.engine.ServeHTTP(rr, req)
		return rr
	}
	request()
	if rr := request(); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("pre-reload status = %d", rr.Code)
	}
	disabled := cfg
	disabled.ClientKeyRateLimit.Enabled = false
	server.UpdateClients(&disabled)
	if rr := request(); rr.Code == http.StatusTooManyRequests {
		t.Fatal("disabled hot reload still limited")
	}
	if got := server.clientKeyRateLimiter.trackedKeys(); got != 0 {
		t.Fatalf("disabled retained %d buckets", got)
	}
}
