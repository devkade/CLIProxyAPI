package management

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/redisqueue"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

type usageQueueRecord []byte

func (r usageQueueRecord) MarshalJSON() ([]byte, error) {
	if json.Valid(r) {
		return append([]byte(nil), r...), nil
	}
	return json.Marshal(string(r))
}

// GetUsageQueue pops queued usage records from the usage queue.
func (h *Handler) GetUsageQueue(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler unavailable"})
		return
	}

	count, errCount := parseUsageQueueCount(c.Query("count"))
	if errCount != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errCount.Error()})
		return
	}

	items := redisqueue.PopOldest(count)
	records := make([]usageQueueRecord, 0, len(items))
	for _, item := range items {
		records = append(records, usageQueueRecord(append([]byte(nil), item...)))
	}

	c.JSON(http.StatusOK, records)
}

const maxAccountHealthSeries = 256

type accountHealthSeries struct {
	Provider       string `json:"provider"`
	AccountBucket  string `json:"account_bucket"`
	Total          int    `json:"total"`
	Active         int    `json:"active"`
	Cooldown       int    `json:"cooldown"`
	Disabled       int    `json:"disabled"`
	Error          int    `json:"error"`
	ModelCooldowns int    `json:"model_cooldowns"`
}

// GetHealthMetrics exposes bounded process-lifetime request metrics and current
// account cooldown state through the authenticated management API.
func (h *Handler) GetHealthMetrics(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler unavailable"})
		return
	}
	var auths []*coreauth.Auth
	if h.authManager != nil {
		auths = h.authManager.List()
	}
	c.JSON(http.StatusOK, gin.H{
		"schema_version": 1,
		"requests":       coreusage.DefaultHealthSnapshot(),
		"accounts":       buildAccountHealthSeries(auths, time.Now()),
		"limits": gin.H{
			"request_series": coreusage.MaxHealthSeries,
			"account_series": maxAccountHealthSeries,
		},
	})
}

func buildAccountHealthSeries(auths []*coreauth.Auth, now time.Time) []accountHealthSeries {
	seriesByKey := make(map[string]*accountHealthSeries, 12*64)
	for _, auth := range auths {
		if auth == nil {
			continue
		}
		provider := accountHealthProvider(auth.Provider)
		bucket := coreusage.AccountBucket(auth.ID)
		key := provider + "\x00" + bucket
		series := seriesByKey[key]
		if series == nil {
			series = &accountHealthSeries{Provider: provider, AccountBucket: bucket}
			seriesByKey[key] = series
		}
		series.Total++
		modelCooldowns := activeModelCooldowns(auth, now)
		series.ModelCooldowns += modelCooldowns
		disabled := auth.Disabled || auth.Status == coreauth.StatusDisabled
		cooldown := !disabled && (modelCooldowns > 0 || authCooldownActive(auth, now))
		switch {
		case disabled:
			series.Disabled++
		case cooldown:
			series.Cooldown++
		case auth.Status == coreauth.StatusError || auth.Unavailable:
			series.Error++
		default:
			series.Active++
		}
	}

	out := make([]accountHealthSeries, 0, len(seriesByKey))
	for _, series := range seriesByKey {
		out = append(out, *series)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].AccountBucket < out[j].AccountBucket
	})
	if len(out) <= maxAccountHealthSeries {
		return out
	}
	overflow := accountHealthSeries{Provider: "overflow", AccountBucket: "overflow"}
	for _, series := range out[maxAccountHealthSeries-1:] {
		overflow.Total += series.Total
		overflow.Active += series.Active
		overflow.Cooldown += series.Cooldown
		overflow.Disabled += series.Disabled
		overflow.Error += series.Error
		overflow.ModelCooldowns += series.ModelCooldowns
	}
	return append(out[:maxAccountHealthSeries-1], overflow)
}

func accountHealthProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "gemini", "vertex", "aistudio", "claude", "codex", "antigravity", "xai", "kimi", "qwen", "iflow", "openai-compatibility", "ollama-cloud":
		return provider
	case "":
		return "unknown"
	default:
		return "other"
	}
}

func authCooldownActive(auth *coreauth.Auth, now time.Time) bool {
	return auth != nil && (auth.NextRetryAfter.After(now) || auth.Quota.NextRecoverAt.After(now))
}

func activeModelCooldowns(auth *coreauth.Auth, now time.Time) int {
	if auth == nil {
		return 0
	}
	count := 0
	for _, state := range auth.ModelStates {
		if state != nil && (state.NextRetryAfter.After(now) || state.Quota.NextRecoverAt.After(now)) {
			count++
		}
	}
	return count
}

func parseUsageQueueCount(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 1, nil
	}
	count, errCount := strconv.Atoi(value)
	if errCount != nil || count <= 0 {
		return 0, errors.New("count must be a positive integer")
	}
	return count, nil
}
