package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestSanitizeOpenAICompatProviderPayload(t *testing.T) {
	tests := []struct {
		name          string
		profile       openAICompatProviderProfile
		input         string
		absentPaths   []string
		wantTools     int
		unchanged     bool
		matchedPrefix string
	}{
		{
			name:        "deepseek model drops only interleaved control",
			profile:     openAICompatProviderProfile{model: "vendor/deepseek-v3.2"},
			input:       `{"thinking":{"type":"enabled"},"interleaved":{"field":"reasoning_content"},"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`,
			absentPaths: []string{"interleaved"},
		},
		{
			name:        "nanogpt provider drops interleaved control",
			profile:     openAICompatProviderProfile{compatName: "nanogpt-us-east"},
			input:       `{"interleaved":true,"messages":[{"role":"user","content":"hi"}]}`,
			absentPaths: []string{"interleaved"},
		},
		{
			name:      "mistral drops only empty assistant messages",
			profile:   openAICompatProviderProfile{compatName: "mistral.ai"},
			input:     `{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"  "},{"role":"assistant","content":null},{"role":"assistant","content":[{"type":"text","text":"kept"}]},{"role":"assistant","tool_calls":[{"id":"call_1"}]},{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}}]}]}`,
			wantTools: -1,
		},
		{
			name:        "xai reasoning drops encrypted content and limits tools",
			profile:     openAICompatProviderProfile{providerName: "openai-compatible-xai"},
			input:       xaiProviderPayloadFixture(202),
			absentPaths: []string{"input.0.encrypted_content"},
			wantTools:   200,
		},
		{
			name:          "xiaomi prefix is recognized without rewriting payload",
			profile:       openAICompatProviderProfile{compatName: "xiaomi-cn"},
			input:         `{"messages":[{"role":"assistant","reasoning_content":"keep","tool_calls":[{"id":"call_1"}]}]}`,
			unchanged:     true,
			matchedPrefix: "xiaomi",
		},
		{
			name:      "generic provider remains byte identical",
			profile:   openAICompatProviderProfile{compatName: "generic"},
			input:     "{ \"interleaved\" : true, \"input\" : [{\"type\":\"reasoning\",\"encrypted_content\":\"opaque\"}], \"messages\" : [{\"role\":\"assistant\",\"content\":null}] }",
			unchanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte(tt.input)
			got := sanitizeOpenAICompatProviderPayload(context.Background(), tt.profile, input)
			if tt.unchanged && !bytes.Equal(got, input) {
				t.Fatalf("payload changed:\n got: %s\nwant: %s", got, input)
			}
			if tt.matchedPrefix != "" && !tt.profile.matchesProvider(tt.matchedPrefix) {
				t.Fatalf("provider profile did not match prefix %q", tt.matchedPrefix)
			}
			for _, path := range tt.absentPaths {
				if gjson.GetBytes(got, path).Exists() {
					t.Fatalf("%s still exists in payload: %s", path, got)
				}
			}
			if tt.profile.model != "" && !gjson.GetBytes(got, "thinking").Exists() {
				t.Fatalf("DeepSeek thinking field was removed: %s", got)
			}
			if tt.profile.compatName == "mistral.ai" {
				messages := gjson.GetBytes(got, "messages").Array()
				if len(messages) != 4 {
					t.Fatalf("messages length = %d, want 4; payload=%s", len(messages), got)
				}
				if messages[1].Get("content.0.text").String() != "kept" || len(messages[2].Get("tool_calls").Array()) != 1 {
					t.Fatalf("non-empty assistant messages were not preserved: %s", got)
				}
			}
			if tt.wantTools >= 0 {
				if count := len(gjson.GetBytes(got, "tools").Array()); count != tt.wantTools {
					t.Fatalf("tools length = %d, want %d", count, tt.wantTools)
				}
			}
		})
	}
}

func TestOpenAICompatProviderSanitizerRunsForStreamingAndNonStreaming(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			var gotBody []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				var errRead error
				gotBody, errRead = io.ReadAll(request.Body)
				if errRead != nil {
					t.Errorf("read request body: %v", errRead)
				}
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte("data: [DONE]\n\n"))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
			}))
			defer server.Close()

			cfg := &config.Config{OpenAICompatibility: []config.OpenAICompatibility{{
				Name:    "nanogpt-us-east",
				BaseURL: server.URL,
			}}}
			executor := NewOpenAICompatExecutor("openai-compatible-nanogpt-us-east", cfg)
			auth := &cliproxyauth.Auth{Provider: "openai-compatibility", Attributes: map[string]string{
				"base_url":    server.URL,
				"compat_name": "nanogpt-us-east",
			}}
			req := cliproxyexecutor.Request{
				Model:   "chat-model",
				Payload: []byte(`{"model":"chat-model","interleaved":true,"thinking":{"type":"enabled"},"messages":[{"role":"user","content":"hi"}]}`),
			}
			opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, Stream: stream}
			if stream {
				result, errExecute := executor.ExecuteStream(context.Background(), auth, req, opts)
				if errExecute != nil {
					t.Fatalf("ExecuteStream: %v", errExecute)
				}
				for chunk := range result.Chunks {
					if chunk.Err != nil {
						t.Fatalf("stream chunk: %v", chunk.Err)
					}
				}
			} else if _, errExecute := executor.Execute(context.Background(), auth, req, opts); errExecute != nil {
				t.Fatalf("Execute: %v", errExecute)
			}
			if gjson.GetBytes(gotBody, "interleaved").Exists() {
				t.Fatalf("interleaved reached upstream: %s", gotBody)
			}
			if !gjson.GetBytes(gotBody, "thinking").Exists() {
				t.Fatalf("thinking was removed: %s", gotBody)
			}
		})
	}
}

func TestXAIPrepareResponsesRequestLimitsTools(t *testing.T) {
	executor := NewXAIExecutor(&config.Config{})
	prepared, errPrepare := executor.prepareResponsesRequest(context.Background(), cliproxyexecutor.Request{
		Model:   "grok-4.3",
		Payload: []byte(xaiProviderPayloadFixture(202)),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse}, false)
	if errPrepare != nil {
		t.Fatalf("prepareResponsesRequest: %v", errPrepare)
	}
	if count := len(gjson.GetBytes(prepared.body, "tools").Array()); count != xaiMaxTools {
		t.Fatalf("tools length = %d, want %d; body=%s", count, xaiMaxTools, prepared.body)
	}
}

func TestLimitXAIToolsLeavesPayloadByteIdenticalWithinLimit(t *testing.T) {
	input := []byte(`{ "tools" : [{"type":"function","name":"one"}], "input" : "hi" }`)
	if got := limitXAITools(context.Background(), input, 200); !bytes.Equal(got, input) {
		t.Fatalf("payload changed:\n got: %s\nwant: %s", got, input)
	}
}

func xaiProviderPayloadFixture(toolCount int) string {
	var payload bytes.Buffer
	payload.WriteString(`{"input":[{"type":"reasoning","encrypted_content":"foreign","summary":[]},{"type":"message","role":"user","content":"hi"}],"tools":[`)
	for i := 0; i < toolCount; i++ {
		if i > 0 {
			payload.WriteByte(',')
		}
		fmt.Fprintf(&payload, `{"type":"function","name":"tool_%d"}`, i)
	}
	payload.WriteString(`]}`)
	return payload.String()
}
