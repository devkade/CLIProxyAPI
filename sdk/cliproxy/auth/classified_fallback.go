package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	log "github.com/sirupsen/logrus"
)

const maxClassifiedModelFallbacks = 2

type modelFallbackReason string

const (
	modelFallbackReasonModelUnsupported     modelFallbackReason = "model_unsupported"
	modelFallbackReasonToolsUnsupported     modelFallbackReason = "tools_unsupported"
	modelFallbackReasonStreamingUnsupported modelFallbackReason = "streaming_unsupported"
)

type modelFallbackClassification struct {
	Eligible bool
	Reason   modelFallbackReason
}

type classifiedModelFallbackTracker struct {
	remaining   int
	used        int
	originalErr error
	reason      modelFallbackReason
}

func newClassifiedModelFallbackTracker() *classifiedModelFallbackTracker {
	return &classifiedModelFallbackTracker{remaining: maxClassifiedModelFallbacks}
}

func (t *classifiedModelFallbackTracker) evaluate(classification modelFallbackClassification, provider, fromModel string, remainingModels []string, err error) (bool, error) {
	if t == nil || err == nil {
		return false, nil
	}
	if !classification.Eligible {
		if t.originalErr != nil && len(remainingModels) == 0 {
			t.logExhausted(provider, fromModel)
			return false, t.originalErr
		}
		if statusCodeFromError(err) == http.StatusBadRequest && !isInvalidGrantError(err) {
			return false, err
		}
		return false, nil
	}

	if len(remainingModels) == 0 {
		if t.originalErr != nil {
			t.logExhausted(provider, fromModel)
			return false, t.originalErr
		}
		return false, nil
	}
	if t.originalErr == nil {
		t.originalErr = err
		t.reason = classification.Reason
	}
	if t.remaining == 0 {
		t.logExhausted(provider, fromModel)
		return false, t.originalErr
	}

	nextModel := remainingModels[0]
	t.remaining--
	t.used++
	log.WithFields(log.Fields{
		"provider":   strings.ToLower(strings.TrimSpace(provider)),
		"from_model": strings.TrimSpace(fromModel),
		"to_model":   strings.TrimSpace(nextModel),
		"reason":     classification.Reason,
		"attempt":    t.used,
	}).Debug("classified model fallback selected")
	return true, nil
}

func (t *classifiedModelFallbackTracker) logExhausted(provider, model string) {
	log.WithFields(log.Fields{
		"provider":       strings.ToLower(strings.TrimSpace(provider)),
		"model":          strings.TrimSpace(model),
		"reason":         t.reason,
		"fallbacks_used": t.used,
	}).Warn("classified model fallback exhausted")
}

type structuredProviderError struct {
	Code    string `json:"code"`
	Type    string `json:"type"`
	Param   string `json:"param"`
	Field   string `json:"field"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

type structuredProviderErrorEnvelope struct {
	Error json.RawMessage `json:"error"`
	structuredProviderError
}

func classifyModelFallbackError(auth *Auth, provider string, err error) modelFallbackClassification {
	if err == nil || statusCodeFromError(err) != http.StatusBadRequest || modelFallbackProviderFamily(auth, provider) == "" {
		return modelFallbackClassification{}
	}

	detail, ok := parseStructuredProviderError(err)
	if !ok {
		return modelFallbackClassification{}
	}
	code := normalizeErrorIdentifier(detail.Code)
	if code == "" {
		code = normalizeErrorIdentifier(detail.Type)
	}
	field := normalizeErrorIdentifier(strings.Join([]string{detail.Param, detail.Field, detail.Path}, " "))

	switch code {
	case "model_not_supported", "unsupported_model", "model_not_found", "unknown_model", "model_does_not_exist", "model_does_not_exist_error":
		return modelFallbackClassification{Eligible: true, Reason: modelFallbackReasonModelUnsupported}
	case "tools_not_supported", "tool_use_not_supported", "function_calling_not_supported":
		return modelFallbackClassification{Eligible: true, Reason: modelFallbackReasonToolsUnsupported}
	case "streaming_not_supported", "stream_not_supported":
		return modelFallbackClassification{Eligible: true, Reason: modelFallbackReasonStreamingUnsupported}
	case "unsupported_feature":
		switch {
		case containsAny(field, "tools", "tool_use", "function_call", "functions"):
			return modelFallbackClassification{Eligible: true, Reason: modelFallbackReasonToolsUnsupported}
		case containsAny(field, "stream", "streaming"):
			return modelFallbackClassification{Eligible: true, Reason: modelFallbackReasonStreamingUnsupported}
		case containsAny(field, "model"):
			return modelFallbackClassification{Eligible: true, Reason: modelFallbackReasonModelUnsupported}
		}
	}
	return modelFallbackClassification{}
}

func parseStructuredProviderError(err error) (structuredProviderError, bool) {
	message := err.Error()
	var authErr *Error
	if errors.As(err, &authErr) && authErr != nil {
		message = authErr.Message
	}
	var envelope structuredProviderErrorEnvelope
	if json.Unmarshal([]byte(strings.TrimSpace(message)), &envelope) != nil {
		return structuredProviderError{}, false
	}
	detail := envelope.structuredProviderError
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		if json.Unmarshal(envelope.Error, &detail) != nil {
			return structuredProviderError{}, false
		}
	}
	if strings.TrimSpace(detail.Code) == "" && strings.TrimSpace(detail.Type) == "" {
		return structuredProviderError{}, false
	}
	return detail, true
}

func normalizeErrorIdentifier(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer("-", "_", " ", "_", ".", "_", "/", "_").Replace(value)
}

func modelFallbackProviderFamily(auth *Auth, provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if auth != nil && auth.Attributes != nil {
		if strings.TrimSpace(auth.Attributes["compat_name"]) != "" || strings.EqualFold(strings.TrimSpace(auth.Attributes["provider_key"]), "openai-compatibility") {
			return "openai"
		}
	}
	switch provider {
	case "openai", "openai-compatibility", "ollama-cloud", "codex", "kimi", "xai":
		return "openai"
	case "claude", "anthropic":
		return "claude"
	case "gemini", "gemini-cli", "gemini-interactions", "vertex", "aistudio", "antigravity":
		return "gemini"
	default:
		return ""
	}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
