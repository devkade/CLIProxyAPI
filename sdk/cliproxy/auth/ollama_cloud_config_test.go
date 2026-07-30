package auth

import (
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
