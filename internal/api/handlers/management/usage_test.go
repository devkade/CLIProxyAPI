package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/redisqueue"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestGetUsageQueuePopsRequestedRecords(t *testing.T) {
	withManagementUsageQueue(t, func() {
		redisqueue.Enqueue([]byte(`{"id":1}`))
		redisqueue.Enqueue([]byte(`{"id":2}`))
		redisqueue.Enqueue([]byte(`{"id":3}`))

		rec := httptest.NewRecorder()
		ginCtx, _ := gin.CreateTestContext(rec)
		ginCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/usage-queue?count=2", nil)

		h := &Handler{}
		h.GetUsageQueue(ginCtx)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}

		var payload []json.RawMessage
		if errUnmarshal := json.Unmarshal(rec.Body.Bytes(), &payload); errUnmarshal != nil {
			t.Fatalf("unmarshal response: %v", errUnmarshal)
		}
		if len(payload) != 2 {
			t.Fatalf("response records = %d, want 2", len(payload))
		}
		requireRecordID(t, payload[0], 1)
		requireRecordID(t, payload[1], 2)

		remaining := redisqueue.PopOldest(10)
		if len(remaining) != 1 || string(remaining[0]) != `{"id":3}` {
			t.Fatalf("remaining queue = %q, want third item only", remaining)
		}
	})
}

func TestGetUsageQueueInvalidCountDoesNotPop(t *testing.T) {
	withManagementUsageQueue(t, func() {
		redisqueue.Enqueue([]byte(`{"id":1}`))

		rec := httptest.NewRecorder()
		ginCtx, _ := gin.CreateTestContext(rec)
		ginCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/usage-queue?count=0", nil)

		h := &Handler{}
		h.GetUsageQueue(ginCtx)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}

		remaining := redisqueue.PopOldest(10)
		if len(remaining) != 1 || string(remaining[0]) != `{"id":1}` {
			t.Fatalf("remaining queue = %q, want original item", remaining)
		}
	})
}

func TestGetHealthMetricsIncludesBoundedRedactedAccountCooldowns(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	_, errRegister := manager.Register(context.Background(), &coreauth.Auth{
		ID: "oauth-token-secret", Provider: "claude", Status: coreauth.StatusActive,
		Metadata: map[string]any{"access_token": "must-not-leak", "email": "private@example.com"},
	})
	if errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}
	retryAfter := time.Hour
	manager.MarkResult(context.Background(), coreauth.Result{
		AuthID: "oauth-token-secret", Provider: "claude", Model: "claude-sonnet", Success: false,
		RetryAfter: &retryAfter, Error: &coreauth.Error{Code: "rate_limit", Message: "upstream secret body", HTTPStatus: http.StatusTooManyRequests},
	})

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	ginCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/health-metrics", nil)
	(&Handler{authManager: manager}).GetHealthMetrics(ginCtx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, secret := range []string{"oauth-token-secret", "must-not-leak", "private@example.com", "upstream secret body"} {
		if strings.Contains(body, secret) {
			t.Fatalf("response leaked %q: %s", secret, body)
		}
	}
	var payload struct {
		Accounts []struct {
			Provider       string `json:"provider"`
			Cooldown       int    `json:"cooldown"`
			ModelCooldowns int    `json:"model_cooldowns"`
		} `json:"accounts"`
	}
	if errUnmarshal := json.Unmarshal(rec.Body.Bytes(), &payload); errUnmarshal != nil {
		t.Fatalf("unmarshal response: %v", errUnmarshal)
	}
	if len(payload.Accounts) != 1 || payload.Accounts[0].Provider != "claude" || payload.Accounts[0].Cooldown != 1 || payload.Accounts[0].ModelCooldowns != 1 {
		t.Fatalf("accounts = %#v, want one claude cooldown", payload.Accounts)
	}
}

func TestBuildAccountHealthSeriesReservesOverflowWithinLimit(t *testing.T) {
	providers := []string{"gemini", "vertex", "aistudio", "claude", "codex"}
	auths := make([]*coreauth.Auth, 0, maxAccountHealthSeries+1)
	for _, provider := range providers {
		buckets := make(map[string]struct{}, 64)
		for candidate := 0; len(buckets) < 64 && len(auths) < maxAccountHealthSeries+1; candidate++ {
			id := provider + "-account-" + strconv.Itoa(candidate)
			bucket := coreusage.AccountBucket(id)
			if _, exists := buckets[bucket]; exists {
				continue
			}
			buckets[bucket] = struct{}{}
			auths = append(auths, &coreauth.Auth{ID: id, Provider: provider, Status: coreauth.StatusActive})
		}
	}
	if len(auths) != maxAccountHealthSeries+1 {
		t.Fatalf("generated account series = %d, want %d", len(auths), maxAccountHealthSeries+1)
	}

	series := buildAccountHealthSeries(auths, time.Now())
	if len(series) != maxAccountHealthSeries {
		t.Fatalf("account series = %d, want strict limit %d", len(series), maxAccountHealthSeries)
	}
	var total, overflowTotal int
	for _, item := range series {
		total += item.Total
		if item.Provider == "overflow" && item.AccountBucket == "overflow" {
			overflowTotal = item.Total
		}
	}
	if total != len(auths) {
		t.Fatalf("account total = %d, want preserved total %d", total, len(auths))
	}
	if overflowTotal != 2 {
		t.Fatalf("overflow total = %d, want 2 reserved series", overflowTotal)
	}
}

func TestAccountHealthProviderPreservesOllamaCloud(t *testing.T) {
	if got := accountHealthProvider(" ollama-cloud "); got != "ollama-cloud" {
		t.Fatalf("accountHealthProvider() = %q, want ollama-cloud", got)
	}
}

func withManagementUsageQueue(t *testing.T, fn func()) {
	t.Helper()

	prevQueueEnabled := redisqueue.Enabled()
	redisqueue.SetEnabled(false)
	redisqueue.SetEnabled(true)

	defer func() {
		redisqueue.SetEnabled(false)
		redisqueue.SetEnabled(prevQueueEnabled)
	}()

	fn()
}

func requireRecordID(t *testing.T, raw json.RawMessage, want int) {
	t.Helper()

	var payload struct {
		ID int `json:"id"`
	}
	if errUnmarshal := json.Unmarshal(raw, &payload); errUnmarshal != nil {
		t.Fatalf("unmarshal record: %v", errUnmarshal)
	}
	if payload.ID != want {
		t.Fatalf("record id = %d, want %d", payload.ID, want)
	}
}
