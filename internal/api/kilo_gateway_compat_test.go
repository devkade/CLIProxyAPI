package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	proxyconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	runtimeexecutor "github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

const (
	kiloFixtureAlias         = "kilo/kilo-fixture"
	kiloFixtureUpstreamModel = "provider/kilo-fixture"
	kiloFixtureBearer        = "deterministic-mock-only"
)

type kiloGatewayFixture struct {
	t            *testing.T
	proxy        *Server
	observations <-chan kiloGatewayObservation
}

type kiloGatewayObservation struct {
	path          string
	authorization string
	model         string
	stream        bool
}

func TestKiloGatewayOpenAICompatWiring(t *testing.T) {
	fixture := newKiloGatewayFixture(t)
	fixture.verifyModels()
	fixture.verifyChat(false)
	fixture.verifyChat(true)
}

func newKiloGatewayFixture(t *testing.T) *kiloGatewayFixture {
	t.Helper()

	observations := make(chan kiloGatewayObservation, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, errRead := io.ReadAll(r.Body)
		if errRead != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var request struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if errUnmarshal := json.Unmarshal(body, &request); errUnmarshal != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		observations <- kiloGatewayObservation{
			path:          r.URL.Path,
			authorization: r.Header.Get("Authorization"),
			model:         request.Model,
			stream:        request.Stream,
		}

		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-kilo-stream\",\"object\":\"chat.completion.chunk\",\"model\":\"provider/kilo-fixture\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"wired\"},\"finish_reason\":null}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-kilo-stream\",\"object\":\"chat.completion.chunk\",\"model\":\"provider/kilo-fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-kilo","object":"chat.completion","model":"provider/kilo-fixture","choices":[{"index":0,"message":{"role":"assistant","content":"wired"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	t.Cleanup(upstream.Close)

	proxy := newTestServer(t)
	providerKey := util.OpenAICompatibleProviderKey("kilo-gateway")
	proxy.cfg.OpenAICompatibility = []proxyconfig.OpenAICompatibility{{
		Name:    "kilo-gateway",
		Prefix:  "kilo",
		BaseURL: upstream.URL + "/api/gateway",
		APIKeyEntries: []proxyconfig.OpenAICompatibilityAPIKey{{
			APIKey: kiloFixtureBearer,
		}},
		Models: []proxyconfig.OpenAICompatibilityModel{{
			Name:  kiloFixtureUpstreamModel,
			Alias: "kilo-fixture",
		}},
	}}
	proxy.handlers.AuthManager.SetConfig(proxy.cfg)
	proxy.handlers.AuthManager.RegisterExecutor(runtimeexecutor.NewOpenAICompatExecutor(providerKey, proxy.cfg))
	credential := &cliproxyauth.Auth{
		ID:       "kilo-gateway-wiring-fixture",
		Provider: providerKey,
		Label:    "kilo-gateway",
		Prefix:   "kilo",
		Status:   cliproxyauth.StatusActive,
		Attributes: map[string]string{
			"api_key":      kiloFixtureBearer,
			"base_url":     upstream.URL + "/api/gateway",
			"compat_name":  "kilo-gateway",
			"provider_key": providerKey,
		},
	}
	if _, errRegister := proxy.handlers.AuthManager.Register(context.Background(), credential); errRegister != nil {
		t.Fatalf("register Kilo wiring credential: %v", errRegister)
	}
	registry.GetGlobalRegistry().RegisterClient(credential.ID, providerKey, []*registry.ModelInfo{{
		ID:      kiloFixtureAlias,
		Object:  "model",
		OwnedBy: "kilo-gateway",
		Type:    "openai-compatibility",
	}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(credential.ID)
	})

	return &kiloGatewayFixture{
		t:            t,
		proxy:        proxy,
		observations: observations,
	}
}

func (f *kiloGatewayFixture) verifyModels() {
	f.t.Helper()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("Authorization", "Bearer test-key")
	f.proxy.engine.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		f.t.Fatalf("models status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if errUnmarshal := json.Unmarshal(recorder.Body.Bytes(), &response); errUnmarshal != nil {
		f.t.Fatalf("decode models response: %v", errUnmarshal)
	}
	for _, model := range response.Data {
		if model.ID == kiloFixtureAlias {
			return
		}
	}
	f.t.Fatalf("models response does not contain %q: %s", kiloFixtureAlias, recorder.Body.String())
}

func (f *kiloGatewayFixture) verifyChat(stream bool) {
	f.t.Helper()

	payload := `{"model":"` + kiloFixtureAlias + `","messages":[{"role":"user","content":"wiring check"}],"stream":` + map[bool]string{false: "false", true: "true"}[stream] + `}`
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(payload))
	request.Header.Set("Authorization", "Bearer test-key")
	request.Header.Set("Content-Type", "application/json")
	f.proxy.engine.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		f.t.Fatalf("chat stream=%t status = %d, want %d; body=%s", stream, recorder.Code, http.StatusOK, recorder.Body.String())
	}

	observation := <-f.observations
	if observation.path != "/api/gateway/chat/completions" {
		f.t.Fatalf("upstream path = %q, want /api/gateway/chat/completions", observation.path)
	}
	if observation.authorization != "Bearer "+kiloFixtureBearer {
		f.t.Fatalf("upstream bearer was not forwarded")
	}
	if observation.model != kiloFixtureUpstreamModel {
		f.t.Fatalf("upstream model = %q, want %q", observation.model, kiloFixtureUpstreamModel)
	}
	if observation.stream != stream {
		f.t.Fatalf("upstream stream = %t, want %t", observation.stream, stream)
	}

	if stream {
		if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "text/event-stream") {
			f.t.Fatalf("stream content type = %q, want text/event-stream", contentType)
		}
		if body := recorder.Body.String(); !strings.Contains(body, `"content":"wired"`) || !strings.Contains(body, "data: [DONE]") {
			f.t.Fatalf("stream response did not preserve SSE content and terminator: %s", body)
		}
		return
	}
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if errUnmarshal := json.Unmarshal(recorder.Body.Bytes(), &response); errUnmarshal != nil {
		f.t.Fatalf("decode chat response: %v", errUnmarshal)
	}
	if len(response.Choices) != 1 || response.Choices[0].Message.Content != "wired" {
		f.t.Fatalf("chat response = %s, want wired assistant content", recorder.Body.String())
	}
}
