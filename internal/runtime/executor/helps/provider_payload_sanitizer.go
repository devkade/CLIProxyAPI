package helps

import (
	"bytes"
	"context"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const XAIMaxTools = 200

// ProviderPayloadProfile contains the provider identity used to select
// OpenAI-compatible request sanitation rules.
type ProviderPayloadProfile struct {
	ExecutorName string
	ProviderName string
	CompatName   string
	BaseURL      string
	Model        string
}

// MatchesProvider reports whether any configured provider identity starts with
// the normalized provider prefix.
func (p ProviderPayloadProfile) MatchesProvider(prefix string) bool {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	for _, name := range []string{p.CompatName, p.ProviderName, p.ExecutorName} {
		name = strings.ToLower(strings.TrimSpace(name))
		name = strings.TrimPrefix(name, "openai-compatible-")
		if name == prefix || strings.HasPrefix(name, prefix+"-") || strings.HasPrefix(name, prefix+".") {
			return true
		}
	}
	return false
}

func (p ProviderPayloadProfile) isDeepSeekLike() bool {
	baseURL := strings.ToLower(strings.TrimSpace(p.BaseURL))
	model := strings.ToLower(strings.TrimSpace(p.Model))
	leaf := model
	if slash := strings.LastIndex(leaf, "/"); slash >= 0 {
		leaf = leaf[slash+1:]
	}
	return p.MatchesProvider("deepseek") || p.MatchesProvider("nanogpt") ||
		strings.Contains(baseURL, "api.deepseek.com") || strings.Contains(baseURL, "nano-gpt.com") ||
		strings.HasPrefix(model, "deepseek") || strings.Contains(model, "/deepseek") || strings.HasPrefix(leaf, "deepseek")
}

// SanitizeOpenAICompatProviderPayload applies documented provider-specific
// request transformations. Detection without a documented transformation,
// such as Xiaomi, preserves the original bytes.
func SanitizeOpenAICompatProviderPayload(ctx context.Context, profile ProviderPayloadProfile, body []byte) []byte {
	if !gjson.ValidBytes(body) {
		return body
	}

	if profile.isDeepSeekLike() && gjson.GetBytes(body, "interleaved").Exists() {
		if updated, errDelete := sjson.DeleteBytes(body, "interleaved"); errDelete == nil {
			body = updated
			LogWithRequestID(ctx).WithField("component", "provider_payload_sanitizer").Debug("openai compat: removed interleaved control for DeepSeek-like upstream")
		}
	}
	if profile.MatchesProvider("mistral") {
		body = dropEmptyMistralAssistantMessages(ctx, body)
	}
	if profile.MatchesProvider("xai") {
		body = LimitXAITools(ctx, body, XAIMaxTools)
	}
	if profile.MatchesProvider("xiaomi") {
		LogWithRequestID(ctx).WithField("component", "provider_payload_sanitizer").Debug("openai compat: detected Xiaomi upstream; preserving payload")
	}
	return body
}

func dropEmptyMistralAssistantMessages(ctx context.Context, body []byte) []byte {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return body
	}
	items := messages.Array()
	kept := make([][]byte, 0, len(items))
	dropped := 0
	for _, message := range items {
		if isEmptyAssistantMessage(message) {
			dropped++
			continue
		}
		kept = append(kept, []byte(message.Raw))
	}
	if dropped == 0 {
		return body
	}
	updated, errSet := sjson.SetRawBytes(body, "messages", joinRawJSONArray(kept))
	if errSet != nil {
		return body
	}
	LogWithRequestID(ctx).
		WithField("component", "provider_payload_sanitizer").
		WithField("dropped_assistant_messages", dropped).
		Debug("openai compat: removed empty assistant messages for Mistral upstream")
	return updated
}

func isEmptyAssistantMessage(message gjson.Result) bool {
	if !strings.EqualFold(strings.TrimSpace(message.Get("role").String()), "assistant") {
		return false
	}
	if toolCalls := message.Get("tool_calls"); toolCalls.IsArray() && len(toolCalls.Array()) > 0 {
		return false
	}
	if functionCall := message.Get("function_call"); functionCall.Exists() && functionCall.Type != gjson.Null && functionCall.Raw != "{}" {
		return false
	}
	for key, value := range message.Map() {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if (strings.Contains(normalizedKey, "reasoning") || strings.Contains(normalizedKey, "thinking")) && hasPayloadValue(value) {
			return false
		}
	}
	return !hasPayloadValue(message.Get("content"))
}

func hasPayloadValue(value gjson.Result) bool {
	switch {
	case !value.Exists() || value.Type == gjson.Null:
		return false
	case value.Type == gjson.String:
		return strings.TrimSpace(value.String()) != ""
	case value.IsArray():
		for _, part := range value.Array() {
			if hasPayloadValue(part) {
				return true
			}
		}
		return false
	case value.IsObject():
		return value.Raw != "{}"
	default:
		return true
	}
}

// LimitXAITools truncates an oversized xAI tool list while preserving payloads
// that are already within the provider limit byte-for-byte.
func LimitXAITools(ctx context.Context, body []byte, limit int) []byte {
	if limit <= 0 {
		return body
	}
	tools := gjson.GetBytes(body, "tools")
	if !tools.IsArray() {
		return body
	}
	items := tools.Array()
	if len(items) <= limit {
		return body
	}
	kept := make([][]byte, 0, limit)
	for _, tool := range items[:limit] {
		kept = append(kept, []byte(tool.Raw))
	}
	updated, errSet := sjson.SetRawBytes(body, "tools", joinRawJSONArray(kept))
	if errSet != nil {
		return body
	}
	LogWithRequestID(ctx).
		WithField("component", "provider_payload_sanitizer").
		WithField("tool_count", len(items)).
		WithField("tool_limit", limit).
		Debug("xai: limited tools to provider maximum")
	return updated
}

func joinRawJSONArray(items [][]byte) []byte {
	var result bytes.Buffer
	result.WriteByte('[')
	for index, item := range items {
		if index > 0 {
			result.WriteByte(',')
		}
		result.Write(item)
	}
	result.WriteByte(']')
	return result.Bytes()
}
