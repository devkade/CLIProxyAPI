package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHealthMetricsAggregatesRequestDimensions(t *testing.T) {
	metrics := NewHealthMetrics()
	metrics.HandleUsage(context.Background(), Record{
		Provider: "claude", Model: "claude-sonnet", AuthID: "account-secret",
		Protocol: "openai", Retry: true, Fallback: false,
		Latency: 1500 * time.Millisecond, Failed: true, Fail: Failure{StatusCode: 429, Body: "token-secret"},
	})
	metrics.HandleUsage(context.Background(), Record{
		Provider: "claude", Model: "claude-sonnet", AuthID: "account-secret",
		Protocol: "openai", Retry: true, Fallback: false,
		Latency: 500 * time.Millisecond,
	})

	snapshot := metrics.Snapshot()
	if len(snapshot.Series) != 2 {
		t.Fatalf("series = %d, want 2 status classes", len(snapshot.Series))
	}
	var requests, failures uint64
	for _, series := range snapshot.Series {
		requests += series.Requests
		failures += series.Failures
		if series.AccountBucket == "" || strings.Contains(series.AccountBucket, "secret") {
			t.Fatalf("account bucket = %q, want stable redacted bucket", series.AccountBucket)
		}
	}
	if requests != 2 || failures != 1 {
		t.Fatalf("requests/failures = %d/%d, want 2/1", requests, failures)
	}
	raw, errMarshal := json.Marshal(snapshot)
	if errMarshal != nil {
		t.Fatal(errMarshal)
	}
	if strings.Contains(string(raw), "account-secret") || strings.Contains(string(raw), "token-secret") {
		t.Fatalf("snapshot leaked sensitive input: %s", raw)
	}
}

func TestHealthMetricsBoundsCardinalityDuringConcurrentUpdates(t *testing.T) {
	metrics := NewHealthMetrics()
	const updates = 4096
	var wg sync.WaitGroup
	for i := 0; i < updates; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			metrics.HandleUsage(context.Background(), Record{
				Provider: fmt.Sprintf("provider-%d", i),
				Model:    fmt.Sprintf("model-%d", i),
				AuthID:   fmt.Sprintf("account-%d", i),
				Protocol: "attacker-controlled-protocol",
				Latency:  time.Duration(i) * time.Millisecond,
			})
		}(i)
	}
	wg.Wait()

	snapshot := metrics.Snapshot()
	if len(snapshot.Series) > MaxHealthSeries {
		t.Fatalf("series = %d, exceeds strict bounded maximum %d including overflow", len(snapshot.Series), MaxHealthSeries)
	}
	var requests uint64
	for _, series := range snapshot.Series {
		requests += series.Requests
	}
	if requests != updates {
		t.Fatalf("requests = %d, want %d", requests, updates)
	}
	if snapshot.OverflowedRequests == 0 {
		t.Fatal("overflowed_requests = 0, want bounded overflow accounting")
	}
}

func TestHealthMetricsNormalizesProviderToExplicitAllowlist(t *testing.T) {
	tests := map[string]string{
		"attacker-chosen-provider-label":               "other",
		"openai-compatible-adversarial-provider-label": "other",
		" ollama-cloud ":                               "ollama-cloud",
		"":                                             "unknown",
	}
	for provider, want := range tests {
		t.Run(provider, func(t *testing.T) {
			metrics := NewHealthMetrics()
			metrics.HandleUsage(context.Background(), Record{Provider: provider})
			snapshot := metrics.Snapshot()
			if len(snapshot.Series) != 1 || snapshot.Series[0].Provider != want {
				t.Fatalf("provider series = %#v, want provider %q", snapshot.Series, want)
			}
		})
	}
}

func TestHealthRequestTracksRetriesAndFallback(t *testing.T) {
	ctx := WithHealthRequest(context.Background(), "openai-response")

	protocol, retry, fallback := ObserveHealthAttempt(ctx, "claude", "claude-sonnet")
	if protocol != "openai-response" || retry || fallback {
		t.Fatalf("first protocol/retry/fallback = %q/%v/%v", protocol, retry, fallback)
	}
	protocol, retry, fallback = ObserveHealthAttempt(ctx, "claude", "claude-sonnet")
	if protocol != "openai-response" || !retry || fallback {
		t.Fatalf("retry protocol/retry/fallback = %q/%v/%v", protocol, retry, fallback)
	}
	protocol, retry, fallback = ObserveHealthAttempt(ctx, "claude", "claude-opus")
	if protocol != "openai-response" || !retry || !fallback {
		t.Fatalf("model fallback protocol/retry/fallback = %q/%v/%v", protocol, retry, fallback)
	}
	protocol, retry, fallback = ObserveHealthAttempt(ctx, "gemini", "gemini-pro")
	if protocol != "openai-response" || !retry || !fallback {
		t.Fatalf("provider fallback protocol/retry/fallback = %q/%v/%v", protocol, retry, fallback)
	}
}
