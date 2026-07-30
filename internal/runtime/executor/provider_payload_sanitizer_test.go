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
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

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
	if count := len(gjson.GetBytes(prepared.body, "tools").Array()); count != helps.XAIMaxTools {
		t.Fatalf("tools length = %d, want %d; body=%s", count, helps.XAIMaxTools, prepared.body)
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

func TestOpenAICompatExecutorDetectsXiaomiAndPreservesPayload(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var errRead error
		gotBody, errRead = io.ReadAll(request.Body)
		if errRead != nil {
			t.Errorf("read request body: %v", errRead)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatible-xiaomi-cn", &config.Config{})
	auth := &cliproxyauth.Auth{Provider: "openai-compatibility", Attributes: map[string]string{
		"base_url":    server.URL,
		"compat_name": "xiaomi-cn",
	}}
	profile := executor.providerProfile(auth, "mimo-v2")
	if !profile.MatchesProvider("xiaomi") {
		t.Fatal("executor profile did not detect Xiaomi provider prefix")
	}
	messages := `[{"role":"assistant","content":null,"reasoning_content":"keep"},{"role":"user","content":"next"}]`
	_, errExecute := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "mimo-v2",
		Payload: []byte(`{"model":"mimo-v2","messages":` + messages + `}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI})
	if errExecute != nil {
		t.Fatalf("Execute: %v", errExecute)
	}
	if got := gjson.GetBytes(gotBody, "messages").Raw; got != messages {
		t.Fatalf("Xiaomi messages changed:\n got: %s\nwant: %s\nbody: %s", got, messages, gotBody)
	}
}

func TestOpenAICompatXAISanitizerUsesNativeEncryptedContentValidation(t *testing.T) {
	valid := testValidGrokEncryptedContent()
	body := []byte(`{"input":[{"type":"reasoning","encrypted_content":""},{"type":"reasoning","encrypted_content":"foreign"}]}`)
	var errSet error
	body, errSet = sjson.SetBytes(body, "input.0.encrypted_content", valid)
	if errSet != nil {
		t.Fatalf("set valid encrypted content: %v", errSet)
	}
	got := sanitizeOpenAICompatProviderPayload(context.Background(), helps.ProviderPayloadProfile{CompatName: "xai"}, body)
	if encrypted := gjson.GetBytes(got, "input.0.encrypted_content").String(); encrypted != valid {
		t.Fatalf("valid xAI encrypted_content changed: got %q", encrypted)
	}
	if gjson.GetBytes(got, "input.1.encrypted_content").Exists() {
		t.Fatalf("invalid xAI encrypted_content survived: %s", got)
	}
}
