package executor

import (
	"bytes"
	"context"
	"strconv"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const xaiMaxTools = 200

type openAICompatProviderProfile struct {
	executorName string
	providerName string
	compatName   string
	baseURL      string
	model        string
}

func (e *OpenAICompatExecutor) providerProfile(auth *cliproxyauth.Auth, model string) openAICompatProviderProfile {
	profile := openAICompatProviderProfile{executorName: e.provider, model: model}
	profile.baseURL, _ = e.resolveCredentials(auth)
	if auth != nil {
		if auth.Provider != "" {
			profile.providerName = auth.Provider
		}
		if auth.Attributes != nil {
			profile.compatName = auth.Attributes["compat_name"]
		}
	}
	if compat := e.resolveCompatConfig(auth); compat != nil {
		profile.compatName = compat.Name
		if profile.baseURL == "" {
			profile.baseURL = compat.BaseURL
		}
	}
	return profile
}

func (p openAICompatProviderProfile) matchesProvider(prefix string) bool {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	for _, name := range []string{p.compatName, p.providerName, p.executorName} {
		name = strings.ToLower(strings.TrimSpace(name))
		name = strings.TrimPrefix(name, "openai-compatible-")
		if name == prefix || strings.HasPrefix(name, prefix+"-") || strings.HasPrefix(name, prefix+".") {
			return true
		}
	}
	return false
}

func (p openAICompatProviderProfile) isDeepSeekLike() bool {
	baseURL := strings.ToLower(strings.TrimSpace(p.baseURL))
	model := strings.ToLower(strings.TrimSpace(p.model))
	leaf := model
	if slash := strings.LastIndex(leaf, "/"); slash >= 0 {
		leaf = leaf[slash+1:]
	}
	return p.matchesProvider("deepseek") || p.matchesProvider("nanogpt") ||
		strings.Contains(baseURL, "api.deepseek.com") || strings.Contains(baseURL, "nano-gpt.com") ||
		strings.HasPrefix(model, "deepseek") || strings.Contains(model, "/deepseek") || strings.HasPrefix(leaf, "deepseek")
}

func sanitizeOpenAICompatProviderPayload(ctx context.Context, profile openAICompatProviderProfile, body []byte) []byte {
	if !gjson.ValidBytes(body) {
		return body
	}

	if profile.isDeepSeekLike() && gjson.GetBytes(body, "interleaved").Exists() {
		if updated, errDelete := sjson.DeleteBytes(body, "interleaved"); errDelete == nil {
			body = updated
			helps.LogWithRequestID(ctx).WithField("component", "provider_payload_sanitizer").Debug("openai compat: removed interleaved control for DeepSeek-like upstream")
		}
	}
	if profile.matchesProvider("mistral") {
		body = dropEmptyMistralAssistantMessages(ctx, body)
	}
	if profile.matchesProvider("xai") {
		body = dropXAIReasoningEncryptedContent(ctx, body)
		body = limitXAITools(ctx, body, xaiMaxTools)
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
	helps.LogWithRequestID(ctx).
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
	content := message.Get("content")
	switch {
	case !content.Exists() || content.Type == gjson.Null:
		return true
	case content.Type == gjson.String:
		return strings.TrimSpace(content.String()) == ""
	case content.IsArray():
		for _, part := range content.Array() {
			if part.Type == gjson.String && strings.TrimSpace(part.String()) != "" {
				return false
			}
			if part.IsObject() && part.Raw != "{}" {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func dropXAIReasoningEncryptedContent(ctx context.Context, body []byte) []byte {
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return body
	}
	updated := body
	dropped := 0
	for index, item := range input.Array() {
		if strings.TrimSpace(item.Get("type").String()) != "reasoning" || !item.Get("encrypted_content").Exists() {
			continue
		}
		path := "input." + strconv.Itoa(index) + ".encrypted_content"
		next, errDelete := sjson.DeleteBytes(updated, path)
		if errDelete != nil {
			continue
		}
		updated = next
		dropped++
	}
	if dropped > 0 {
		helps.LogWithRequestID(ctx).
			WithField("component", "provider_payload_sanitizer").
			WithField("dropped_reasoning_entries", dropped).
			Debug("openai compat: removed encrypted_content from xAI reasoning input")
	}
	return updated
}

func limitXAITools(ctx context.Context, body []byte, limit int) []byte {
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
	helps.LogWithRequestID(ctx).
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
