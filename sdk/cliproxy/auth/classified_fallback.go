package auth

import (
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

func (t *classifiedModelFallbackTracker) evaluate(auth *Auth, provider, fromModel string, remainingModels []string, err error) (bool, error) {
	if t == nil || err == nil {
		return false, nil
	}
	classification := classifyModelFallbackError(auth, provider, err)
	if !classification.Eligible {
		if t.originalErr != nil {
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

func classifyModelFallbackError(auth *Auth, provider string, err error) modelFallbackClassification {
	if err == nil || statusCodeFromError(err) != http.StatusBadRequest {
		return modelFallbackClassification{}
	}
	family := modelFallbackProviderFamily(auth, provider)
	if family == "" {
		return modelFallbackClassification{}
	}

	message := strings.ToLower(err.Error())
	if containsAny(message,
		"invalid_api_key", "authentication_error", "authentication_required", "unauthorized",
		"permission_denied", "permission denied", "insufficient_permission", "forbidden", "access denied",
	) {
		return modelFallbackClassification{}
	}

	if containsAny(message,
		"model_not_supported", "unsupported_model", "model_not_found", "unknown_model", "model_does_not_exist",
		"requested model is not supported", "requested model is unsupported", "model is not supported",
		"model not supported", "unsupported model", "model is unavailable", "model unavailable",
	) || (strings.Contains(message, "model") && containsAny(message, "not available for your plan", "not available for your account")) {
		return modelFallbackClassification{Eligible: true, Reason: modelFallbackReasonModelUnsupported}
	}

	switch family {
	case "openai", "claude":
		if containsAny(message,
			"does not support tools", "doesn't support tools", "tool use is not supported",
			"tools are not supported", "function calling is not supported", "does not support function calling",
		) {
			return modelFallbackClassification{Eligible: true, Reason: modelFallbackReasonToolsUnsupported}
		}
		if containsAny(message,
			"does not support streaming", "doesn't support streaming", "streaming is not supported",
			"streaming not supported",
		) {
			return modelFallbackClassification{Eligible: true, Reason: modelFallbackReasonStreamingUnsupported}
		}
	case "gemini":
		if containsAny(message, "function calling is not supported", "does not support function calling") {
			return modelFallbackClassification{Eligible: true, Reason: modelFallbackReasonToolsUnsupported}
		}
	}
	return modelFallbackClassification{}
}

func modelFallbackProviderFamily(auth *Auth, provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if auth != nil && auth.Attributes != nil {
		if strings.TrimSpace(auth.Attributes["compat_name"]) != "" || strings.EqualFold(strings.TrimSpace(auth.Attributes["provider_key"]), "openai-compatibility") {
			return "openai"
		}
	}
	switch provider {
	case "openai", "openai-compatibility", "codex", "kimi", "xai":
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
