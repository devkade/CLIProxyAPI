package executor

import "github.com/router-for-me/CLIProxyAPI/v7/internal/config"

const ollamaCloudProvider = "ollama-cloud"

// OllamaCloudExecutor executes direct Ollama Cloud requests through its documented
// OpenAI-compatible API while retaining a distinct provider identity.
type OllamaCloudExecutor struct {
	*OpenAICompatExecutor
}

// NewOllamaCloudExecutor creates an Ollama Cloud executor.
func NewOllamaCloudExecutor(cfg *config.Config) *OllamaCloudExecutor {
	return &OllamaCloudExecutor{OpenAICompatExecutor: NewOpenAICompatExecutor(ollamaCloudProvider, cfg)}
}
