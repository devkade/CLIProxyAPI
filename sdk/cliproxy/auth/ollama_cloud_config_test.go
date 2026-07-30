package auth

import (
	"context"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestOllamaCloudConfigMatchingUsesKeyAndNormalizedBase(t *testing.T) {
	cfg := &internalconfig.Config{OllamaCloudKey: []internalconfig.OllamaCloudKey{
		{
			APIKey:  "shared-key",
			BaseURL: "https://gateway.example.com/v1/",
			Models:  []internalconfig.OllamaCloudModel{{Name: "custom-upstream", Alias: "custom"}},
		},
		{
			APIKey: "shared-key",
			Models: []internalconfig.OllamaCloudModel{{Name: "default-upstream", Alias: "default"}},
		},
	}}

	tests := []struct {
		name     string
		baseURL  string
		wantName string
	}{
		{name: "default endpoint", baseURL: internalconfig.DefaultOllamaCloudBaseURL + "/", wantName: "default-upstream"},
		{name: "custom endpoint", baseURL: "https://gateway.example.com/v1", wantName: "custom-upstream"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := resolveOllamaCloudAPIKeyConfig(cfg, &Auth{Attributes: map[string]string{
				"api_key":  "shared-key",
				"base_url": tt.baseURL,
			}})
			if entry == nil || len(entry.Models) != 1 || entry.Models[0].Name != tt.wantName {
				t.Fatalf("resolved entry = %#v, want model %q", entry, tt.wantName)
			}
		})
	}
}

func TestOllamaCloudAliasResolvesAllConfiguredCandidates(t *testing.T) {
	const alias = "cloud-alias"
	cfg := &internalconfig.Config{OllamaCloudKey: []internalconfig.OllamaCloudKey{{
		APIKey: "test-key",
		Models: []internalconfig.OllamaCloudModel{
			{Name: "candidate-a", Alias: alias},
			{Name: "candidate-b", Alias: alias},
		},
	}}}
	manager := NewManager(nil, nil, nil)
	manager.SetConfig(cfg)
	auth := &Auth{
		ID:       "ollama-cloud:test",
		Provider: "ollama-cloud",
		Status:   StatusActive,
		Attributes: map[string]string{
			"api_key":  "test-key",
			"base_url": internalconfig.DefaultOllamaCloudBaseURL,
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	got, _, _ := manager.executionModelCandidatesWithAlias(auth, alias)
	want := []string{"candidate-a", "candidate-b"}
	if len(got) != len(want) {
		t.Fatalf("execution candidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("execution candidate %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestOllamaCloudConfigMatchingRejectsAmbiguousKeyOnlyLookup(t *testing.T) {
	cfg := &internalconfig.Config{OllamaCloudKey: []internalconfig.OllamaCloudKey{
		{APIKey: "shared-key", Models: []internalconfig.OllamaCloudModel{{Name: "default-upstream"}}},
		{APIKey: "shared-key", BaseURL: "https://gateway.example.com/v1", Models: []internalconfig.OllamaCloudModel{{Name: "custom-upstream"}}},
	}}
	auth := &Auth{Attributes: map[string]string{"api_key": "shared-key"}}
	if got := resolveOllamaCloudAPIKeyConfig(cfg, auth); got != nil {
		t.Fatalf("resolved ambiguous key-only entry = %#v, want nil", got)
	}
}
