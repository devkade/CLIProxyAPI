package usage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// MaxHealthSeries is the hard upper bound for distinct request metric series.
// One additional aggregate series retains requests that exceed this limit.
const MaxHealthSeries = 1024

const accountBucketCount = 64

// HealthSeries is one bounded request metric series. Model and account values
// are irreversible, stable buckets; request and response content is never stored.
type HealthSeries struct {
	Provider       string         `json:"provider"`
	ModelBucket    string         `json:"model_bucket"`
	AccountBucket  string         `json:"account_bucket"`
	Protocol       string         `json:"protocol"`
	StatusClass    string         `json:"status_class"`
	Retry          bool           `json:"retry"`
	Fallback       bool           `json:"fallback"`
	Requests       uint64         `json:"requests"`
	Failures       uint64         `json:"failures"`
	LatencyMsTotal uint64         `json:"latency_ms_total"`
	LatencyMsMax   uint64         `json:"latency_ms_max"`
	LatencyBuckets LatencyBuckets `json:"latency_buckets"`
}

// LatencyBuckets holds request counts in fixed, non-overlapping millisecond ranges.
type LatencyBuckets struct {
	LE100  uint64 `json:"le_100_ms"`
	LE500  uint64 `json:"le_500_ms"`
	LE1000 uint64 `json:"le_1000_ms"`
	LE5000 uint64 `json:"le_5000_ms"`
	GT5000 uint64 `json:"gt_5000_ms"`
}

// HealthSnapshot is the operator-facing process-lifetime metrics snapshot.
type HealthSnapshot struct {
	GeneratedAt        time.Time      `json:"generated_at"`
	Series             []HealthSeries `json:"series"`
	OverflowedRequests uint64         `json:"overflowed_requests"`
	MaxSeries          int            `json:"max_series"`
}

type healthSeriesKey struct {
	provider, model, account, protocol, statusClass string
	retry, fallback                                 bool
}

// HealthMetrics concurrently aggregates sanitized usage records.
type HealthMetrics struct {
	mu                 sync.RWMutex
	series             map[healthSeriesKey]*HealthSeries
	overflow           HealthSeries
	overflowedRequests uint64
}

// NewHealthMetrics constructs an empty bounded metrics collector.
func NewHealthMetrics() *HealthMetrics {
	return &HealthMetrics{series: make(map[healthSeriesKey]*HealthSeries)}
}

// HandleUsage implements Plugin.
func (m *HealthMetrics) HandleUsage(_ context.Context, record Record) {
	if m == nil {
		return
	}
	key := healthSeriesKey{
		provider:    normalizeProvider(record.Provider),
		model:       modelBucket(record.Model),
		account:     AccountBucket(record.AuthID),
		protocol:    normalizeProtocol(record.Protocol),
		statusClass: statusClass(record),
		retry:       record.Retry,
		fallback:    record.Fallback,
	}
	latencyMs := durationMilliseconds(record.Latency)

	m.mu.Lock()
	series := m.series[key]
	if series == nil {
		if len(m.series) >= MaxHealthSeries {
			m.overflowedRequests++
			series = &m.overflow
			if series.Provider == "" {
				*series = HealthSeries{Provider: "overflow", ModelBucket: "overflow", AccountBucket: "overflow", Protocol: "other", StatusClass: "other"}
			}
		} else {
			series = &HealthSeries{
				Provider: key.provider, ModelBucket: key.model, AccountBucket: key.account,
				Protocol: key.protocol, StatusClass: key.statusClass, Retry: key.retry, Fallback: key.fallback,
			}
			m.series[key] = series
		}
	}
	observeHealthSeries(series, latencyMs, record.Failed)
	m.mu.Unlock()
}

// Snapshot returns a race-safe, deterministically ordered copy.
func (m *HealthMetrics) Snapshot() HealthSnapshot {
	snapshot := HealthSnapshot{GeneratedAt: time.Now().UTC(), MaxSeries: MaxHealthSeries}
	if m == nil {
		return snapshot
	}
	m.mu.RLock()
	snapshot.Series = make([]HealthSeries, 0, len(m.series)+1)
	for _, series := range m.series {
		if series != nil {
			snapshot.Series = append(snapshot.Series, *series)
		}
	}
	if m.overflow.Requests > 0 {
		snapshot.Series = append(snapshot.Series, m.overflow)
	}
	snapshot.OverflowedRequests = m.overflowedRequests
	m.mu.RUnlock()
	sort.Slice(snapshot.Series, func(i, j int) bool {
		a, b := snapshot.Series[i], snapshot.Series[j]
		return fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%t\x00%t", a.Provider, a.ModelBucket, a.AccountBucket, a.Protocol, a.StatusClass, a.Retry, a.Fallback) <
			fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%t\x00%t", b.Provider, b.ModelBucket, b.AccountBucket, b.Protocol, b.StatusClass, b.Retry, b.Fallback)
	})
	return snapshot
}

func observeHealthSeries(series *HealthSeries, latencyMs uint64, failed bool) {
	series.Requests++
	if failed {
		series.Failures++
	}
	series.LatencyMsTotal += latencyMs
	if latencyMs > series.LatencyMsMax {
		series.LatencyMsMax = latencyMs
	}
	switch {
	case latencyMs <= 100:
		series.LatencyBuckets.LE100++
	case latencyMs <= 500:
		series.LatencyBuckets.LE500++
	case latencyMs <= 1000:
		series.LatencyBuckets.LE1000++
	case latencyMs <= 5000:
		series.LatencyBuckets.LE5000++
	default:
		series.LatencyBuckets.GT5000++
	}
}

func durationMilliseconds(duration time.Duration) uint64 {
	if duration <= 0 {
		return 0
	}
	return uint64(duration / time.Millisecond)
}

func normalizeProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return "unknown"
	}
	if len(provider) > 64 {
		return "other"
	}
	for _, char := range provider {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || strings.ContainsRune("-_.", char) {
			continue
		}
		return "other"
	}
	return provider
}

func normalizeProtocol(protocol string) string {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	switch protocol {
	case "openai", "openai-response", "claude", "gemini", "codex", "antigravity", "interactions":
		return protocol
	case "":
		return "unknown"
	default:
		return "other"
	}
}

func statusClass(record Record) string {
	status := record.Fail.StatusCode
	if status <= 0 {
		if record.Failed {
			return "error"
		}
		return "2xx"
	}
	if status < 100 || status > 599 {
		return "other"
	}
	return fmt.Sprintf("%dxx", status/100)
}

func modelBucket(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return "unknown"
	}
	digest := sha256.Sum256([]byte(model))
	return fmt.Sprintf("sha256:%x", digest[:6])
}

// AccountBucket returns one of 64 stable buckets without exposing credential identifiers.
func AccountBucket(authID string) string {
	authID = strings.TrimSpace(authID)
	if authID == "" {
		return "unknown"
	}
	digest := sha256.Sum256([]byte(authID))
	return fmt.Sprintf("bucket-%02x", digest[0]%(accountBucketCount))
}

var defaultHealthMetrics = NewHealthMetrics()

func init() {
	RegisterNamedPlugin("provider-account-health", defaultHealthMetrics)
}

// DefaultHealthSnapshot returns the process-wide health metrics snapshot.
func DefaultHealthSnapshot() HealthSnapshot { return defaultHealthMetrics.Snapshot() }
