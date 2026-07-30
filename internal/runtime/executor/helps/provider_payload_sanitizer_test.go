package helps

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/tidwall/gjson"
)

func TestSanitizeOpenAICompatProviderPayload(t *testing.T) {
	tests := []struct {
		name        string
		profile     ProviderPayloadProfile
		input       string
		absentPaths []string
		wantTools   int
		unchanged   bool
	}{
		{
			name:        "deepseek model drops only interleaved control",
			profile:     ProviderPayloadProfile{Model: "vendor/deepseek-v3.2"},
			input:       `{"thinking":{"type":"enabled"},"interleaved":{"field":"reasoning_content"},"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`,
			absentPaths: []string{"interleaved"},
		},
		{
			name:        "nanogpt provider drops interleaved control",
			profile:     ProviderPayloadProfile{CompatName: "nanogpt-us-east"},
			input:       `{"interleaved":true,"messages":[{"role":"user","content":"hi"}]}`,
			absentPaths: []string{"interleaved"},
		},
		{
			name:      "mistral preserves reasoning-only assistant message",
			profile:   ProviderPayloadProfile{CompatName: "mistral.ai"},
			input:     `{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"  "},{"role":"assistant","content":null},{"role":"assistant","content":null,"reasoning_content":"keep"},{"role":"assistant","content":[{"type":"text","text":"kept"}]},{"role":"assistant","tool_calls":[{"id":"call_1"}]},{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}}]}]}`,
			wantTools: -1,
		},
		{
			name:      "xai limits tools without duplicating encrypted content sanitation",
			profile:   ProviderPayloadProfile{ProviderName: "openai-compatible-xai"},
			input:     xaiProviderPayloadFixture(202),
			wantTools: 200,
		},
		{
			name:      "xiaomi prefix is recognized without rewriting payload",
			profile:   ProviderPayloadProfile{CompatName: "xiaomi-cn"},
			input:     `{ "messages" : [{"role":"assistant","reasoning_content":"keep","tool_calls":[{"id":"call_1"}]}] }`,
			unchanged: true,
		},
		{
			name:      "generic provider remains byte identical",
			profile:   ProviderPayloadProfile{CompatName: "generic"},
			input:     `{ "interleaved" : true, "input" : [{"type":"reasoning","encrypted_content":"opaque"}], "messages" : [{"role":"assistant","content":null}] }`,
			unchanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte(tt.input)
			got := SanitizeOpenAICompatProviderPayload(context.Background(), tt.profile, input)
			if tt.unchanged && !bytes.Equal(got, input) {
				t.Fatalf("payload changed:\n got: %s\nwant: %s", got, input)
			}
			for _, path := range tt.absentPaths {
				if gjson.GetBytes(got, path).Exists() {
					t.Fatalf("%s still exists in payload: %s", path, got)
				}
			}
			if tt.profile.Model != "" && !gjson.GetBytes(got, "thinking").Exists() {
				t.Fatalf("DeepSeek thinking field was removed: %s", got)
			}
			if tt.profile.CompatName == "mistral.ai" {
				messages := gjson.GetBytes(got, "messages").Array()
				if len(messages) != 5 || messages[1].Get("reasoning_content").String() != "keep" {
					t.Fatalf("reasoning-only assistant message was not preserved: %s", got)
				}
			}
			if tt.profile.ProviderName == "openai-compatible-xai" && gjson.GetBytes(got, "input.0.encrypted_content").String() != "foreign" {
				t.Fatalf("support sanitizer duplicated xAI encrypted-content validation: %s", got)
			}
			if tt.profile.CompatName == "xiaomi-cn" && !tt.profile.MatchesProvider("xiaomi") {
				t.Fatal("Xiaomi provider prefix was not recognized")
			}
			if tt.wantTools >= 0 {
				if count := len(gjson.GetBytes(got, "tools").Array()); count != tt.wantTools {
					t.Fatalf("tools length = %d, want %d; payload=%s", count, tt.wantTools, got)
				}
			}
		})
	}
}

func TestLimitXAIToolsLeavesPayloadByteIdenticalWithinLimit(t *testing.T) {
	input := []byte(`{ "tools" : [{"type":"function","name":"one"}], "input" : "hi" }`)
	if got := LimitXAITools(context.Background(), input, 200); !bytes.Equal(got, input) {
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
