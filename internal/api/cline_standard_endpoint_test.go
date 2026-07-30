package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	proxyconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	runtimeexecutor "github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"gopkg.in/yaml.v3"
)

func TestClineStandardEndpointThroughOpenAICompatProxy(t *testing.T) {
	const (
		clientKey      = "cline-local-client-key"
		upstreamKey    = "cline-upstream-api-key"
		requestedModel = "cline/claude-sonnet-4-6"
		upstreamModel  = "anthropic/claude-sonnet-4-6"
	)

	var upstreamRequests int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamRequests++
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/chat/completions" {
			t.Errorf("upstream request = %s %s, want POST /api/v1/chat/completions", r.Method, r.URL.Path)
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+upstreamKey {
			t.Errorf("upstream Authorization = %q, want bearer credential", got)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upstream body: %v", err)
			http.Error(w, "read failure", http.StatusBadRequest)
			return
		}
		var request struct {
			Model string `json:"model"`
		}
		if err = json.Unmarshal(body, &request); err != nil {
			t.Errorf("decode upstream body: %v", err)
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if request.Model != upstreamModel {
			t.Errorf("upstream model = %q, want %q", request.Model, upstreamModel)
			http.Error(w, "wrong model", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl_cline","object":"chat.completion","model":"anthropic/claude-sonnet-4-6","choices":[{"index":0,"message":{"role":"assistant","content":"standard endpoint ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":3,"total_tokens":4}}`)
	}))
	t.Cleanup(upstream.Close)

	fixturePath := filepath.Join("..", "..", "test", "fixtures", "cline-standard-endpoint.yaml")
	fixture, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read Cline standard endpoint fixture: %v", err)
	}
	fixture = []byte(strings.ReplaceAll(string(fixture), "${CLINE_MOCK_BASE_URL}", upstream.URL+"/api/v1"))
	var cfg proxyconfig.Config
	if err = yaml.Unmarshal(fixture, &cfg); err != nil {
		t.Fatalf("parse Cline standard endpoint fixture: %v", err)
	}
	if len(cfg.OpenAICompatibility) != 1 {
		t.Fatalf("openai-compatibility entries = %d, want 1", len(cfg.OpenAICompatibility))
	}
	compat := cfg.OpenAICompatibility[0]
	if len(compat.APIKeyEntries) != 1 || compat.APIKeyEntries[0].APIKey != upstreamKey {
		t.Fatalf("fixture upstream credential mismatch")
	}

	server := newTestServer(t)
	cfg.AuthDir = server.cfg.AuthDir
	cfg.Debug = true
	cfg.LoggingToFile = false
	cfg.UsageStatisticsEnabled = false
	cfg.APIKeys = []string{clientKey}
	server.UpdateClients(&cfg)
	server.handlers.AuthManager.SetConfig(&cfg)

	providerKey := util.OpenAICompatibleProviderKey(compat.Name)
	server.handlers.AuthManager.RegisterExecutor(runtimeexecutor.NewOpenAICompatExecutor(providerKey, &cfg))
	credential := &auth.Auth{
		ID:       "cline-standard-endpoint",
		Provider: providerKey,
		Prefix:   compat.Prefix,
		Status:   auth.StatusActive,
		Attributes: map[string]string{
			"api_key":      upstreamKey,
			"base_url":     compat.BaseURL,
			"compat_name":  compat.Name,
			"provider_key": providerKey,
		},
	}
	if _, err = server.handlers.AuthManager.Register(context.Background(), credential); err != nil {
		t.Fatalf("register Cline fixture credential: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(credential.ID, providerKey, []*registry.ModelInfo{{
		ID:      requestedModel,
		Object:  "model",
		OwnedBy: "cline-api",
		Type:    "openai",
	}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(credential.ID) })

	unauthorized := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	unauthorizedRecorder := httptest.NewRecorder()
	server.engine.ServeHTTP(unauthorizedRecorder, unauthorized)
	if unauthorizedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized models status = %d, want %d", unauthorizedRecorder.Code, http.StatusUnauthorized)
	}

	models := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	models.Header.Set("Authorization", "Bearer "+clientKey)
	modelsRecorder := httptest.NewRecorder()
	server.engine.ServeHTTP(modelsRecorder, models)
	if modelsRecorder.Code != http.StatusOK {
		t.Fatalf("models status = %d, want %d; body=%s", modelsRecorder.Code, http.StatusOK, modelsRecorder.Body.String())
	}
	if !strings.Contains(modelsRecorder.Body.String(), `"id":"`+requestedModel+`"`) {
		t.Fatalf("models response does not contain %q: %s", requestedModel, modelsRecorder.Body.String())
	}

	chat := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"cline/claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`))
	chat.Header.Set("Authorization", "Bearer "+clientKey)
	chat.Header.Set("Content-Type", "application/json")
	chatRecorder := httptest.NewRecorder()
	server.engine.ServeHTTP(chatRecorder, chat)
	if chatRecorder.Code != http.StatusOK {
		t.Fatalf("chat status = %d, want %d; body=%s", chatRecorder.Code, http.StatusOK, chatRecorder.Body.String())
	}
	if upstreamRequests != 1 {
		t.Fatalf("upstream requests = %d, want 1", upstreamRequests)
	}
	if !strings.Contains(chatRecorder.Body.String(), `"content":"standard endpoint ok"`) {
		t.Fatalf("chat response mismatch: %s", chatRecorder.Body.String())
	}
}
