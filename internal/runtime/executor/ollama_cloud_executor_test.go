package executor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

func TestOllamaCloudExecutorPreservesToolsAndMultimodalPayloads(t *testing.T) {
	tests := []struct {
		name    string
		model   string
		payload []byte
	}{
		{
			name:    "tools",
			model:   "qwen3:8b",
			payload: []byte(`{"model":"qwen3:8b","messages":[{"role":"user","content":"weather"}],"tools":[{"type":"function","function":{"name":"weather","description":"Get weather","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}],"tool_choice":"auto"}`),
		},
		{
			name:    "multimodal",
			model:   "qwen3-vl:235b",
			payload: []byte(`{"model":"qwen3-vl:235b","messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgo="}}]}]}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPayload []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPayload, _ = io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"chatcmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
			}))
			defer server.Close()

			exec := NewOllamaCloudExecutor(&config.Config{})
			_, err := exec.Execute(context.Background(), &cliproxyauth.Auth{Attributes: map[string]string{
				"base_url": server.URL + "/v1",
				"api_key":  "not-a-real-key",
			}}, cliproxyexecutor.Request{
				Model:   tt.model,
				Payload: tt.payload,
			}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai")})
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !bytes.Equal(gotPayload, tt.payload) {
				t.Fatalf("upstream payload = %s, want exact %s", gotPayload, tt.payload)
			}
		})
	}
}

func TestOllamaCloudExecutorChatAndAuthentication(t *testing.T) {
	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	exec := NewOllamaCloudExecutor(&config.Config{})
	_, err := exec.Execute(context.Background(), &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL + "/v1",
		"api_key":  "not-a-real-key",
	}}, cliproxyexecutor.Request{
		Model:   "gpt-oss:120b",
		Payload: []byte(`{"model":"gpt-oss:120b","messages":[{"role":"user","content":"hi"}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai")})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if exec.Identifier() != "ollama-cloud" {
		t.Fatalf("Identifier() = %q", exec.Identifier())
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer not-a-real-key" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
}

func TestOllamaCloudExecutorStreaming(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	exec := NewOllamaCloudExecutor(&config.Config{})
	result, err := exec.ExecuteStream(context.Background(), &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL + "/v1",
		"api_key":  "not-a-real-key",
	}}, cliproxyexecutor.Request{Model: "gpt-oss:120b", Payload: []byte(`{"model":"gpt-oss:120b","messages":[{"role":"user","content":"hi"}]}`)}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai"), Stream: true})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	var chunks int
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error = %v", chunk.Err)
		}
		chunks++
	}
	if gotPath != "/v1/chat/completions" || chunks == 0 {
		t.Fatalf("path/chunks = %q/%d", gotPath, chunks)
	}
}

func TestOllamaCloudExecutorInvalidKeyDoesNotLeakKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"unauthorized"}`)
	}))
	defer server.Close()

	const key = "super-secret-ollama-key"
	exec := NewOllamaCloudExecutor(&config.Config{})
	_, err := exec.Execute(context.Background(), &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL + "/v1",
		"api_key":  key,
	}}, cliproxyexecutor.Request{Model: "gpt-oss:120b", Payload: []byte(`{"model":"gpt-oss:120b"}`)}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai")})
	if err == nil {
		t.Fatal("Execute() error = nil")
	}
	if got := err.Error(); got == "" || strings.Contains(got, key) {
		t.Fatalf("error leaked API key: %q", got)
	}
	if status, ok := err.(interface{ StatusCode() int }); !ok || status.StatusCode() != http.StatusUnauthorized {
		t.Fatalf("error = %T %v, want status 401", err, err)
	}
}
