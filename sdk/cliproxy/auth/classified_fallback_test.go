package auth

import (
	"net/http"
	"testing"
)

func TestClassifyModelFallbackError_OllamaCloudUsesOpenAICompatibleFamily(t *testing.T) {
	err := &Error{
		HTTPStatus: http.StatusBadRequest,
		Message:    `{"error":{"code":"model_not_supported","type":"invalid_request_error","param":"model"}}`,
	}

	classification := classifyModelFallbackError(&Auth{Provider: "ollama-cloud"}, "ollama-cloud", err)
	if !classification.Eligible || classification.Reason != modelFallbackReasonModelUnsupported {
		t.Fatalf("classification = %+v, want eligible model fallback", classification)
	}
}
