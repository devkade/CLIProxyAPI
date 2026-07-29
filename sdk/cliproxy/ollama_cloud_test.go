package cliproxy

import (
	"context"
	"testing"

	internalregistry "github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	runtimeexecutor "github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestOllamaCloudRegistrationIsDistinctAndListsConfiguredModels(t *testing.T) {
	service := &Service{
		cfg: &config.Config{OllamaCloudKey: []config.OllamaCloudKey{{
			APIKey: "test-key",
			Models: []config.OllamaCloudModel{{
				Name:            "qwen3-vl:235b",
				Alias:           "ollama-vision",
				InputModalities: []string{"text", "image"},
			}},
		}}},
		coreManager: coreauth.NewManager(nil, nil, nil),
	}
	auth := &coreauth.Auth{
		ID:         "ollama-cloud:test",
		Provider:   "ollama-cloud",
		Attributes: map[string]string{"api_key": "test-key", "base_url": "https://ollama.com/v1"},
	}
	registry := internalregistry.GetGlobalRegistry()
	registry.UnregisterClient(auth.ID)
	t.Cleanup(func() { registry.UnregisterClient(auth.ID) })

	service.registerExecutorForAuth(auth, false)
	registered, ok := service.coreManager.Executor("ollama-cloud")
	if !ok {
		t.Fatal("ollama-cloud executor was not registered")
	}
	if _, ok = registered.(*runtimeexecutor.OllamaCloudExecutor); !ok {
		t.Fatalf("executor type = %T", registered)
	}

	service.registerModelsForAuth(context.Background(), auth)
	models := registry.GetModelsForClient(auth.ID)
	if len(models) != 1 || models[0].ID != "ollama-vision" {
		t.Fatalf("models = %#v", models)
	}
	if models[0].Type != "ollama-cloud" || models[0].OwnedBy != "ollama" {
		t.Fatalf("model metadata = %#v", models[0])
	}
	if got := models[0].SupportedInputModalities; len(got) != 2 || got[1] != "image" {
		t.Fatalf("input modalities = %#v", got)
	}
}
