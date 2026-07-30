package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoadConfigRejectsOllamaCloudWithoutValidModels(t *testing.T) {
	tests := []struct {
		name   string
		models string
	}{
		{name: "empty", models: "[]"},
		{name: "blank name", models: "\n      - name: '   '"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := t.TempDir() + "/config.yaml"
			contents := "ollama-cloud-api-key:\n  - api-key: test-key\n    models: " + tt.models + "\n"
			if errWrite := os.WriteFile(path, []byte(contents), 0o600); errWrite != nil {
				t.Fatalf("WriteFile() error = %v", errWrite)
			}

			_, errLoad := LoadConfig(path)
			if errLoad == nil {
				t.Fatal("LoadConfig() error = nil, want invalid Ollama Cloud model mapping")
			}
			if !strings.Contains(errLoad.Error(), "ollama-cloud-api-key[0].models") {
				t.Fatalf("LoadConfig() error = %q", errLoad)
			}
		})
	}
}

func TestLoadConfigNormalizesOllamaCloudBaseAndModels(t *testing.T) {
	path := t.TempDir() + "/config.yaml"
	contents := `ollama-cloud-api-key:
  - api-key: " test-key "
    base-url: " https://ollama.com/v1/ "
    models:
      - name: " qwen3:8b "
        alias: " cloud-qwen "
`
	if errWrite := os.WriteFile(path, []byte(contents), 0o600); errWrite != nil {
		t.Fatalf("WriteFile() error = %v", errWrite)
	}

	cfg, errLoad := LoadConfig(path)
	if errLoad != nil {
		t.Fatalf("LoadConfig() error = %v", errLoad)
	}
	entry := cfg.OllamaCloudKey[0]
	if entry.APIKey != "test-key" || entry.BaseURL != DefaultOllamaCloudBaseURL {
		t.Fatalf("normalized credential = key %q base %q", entry.APIKey, entry.BaseURL)
	}
	if entry.Models[0].Name != "qwen3:8b" || entry.Models[0].Alias != "cloud-qwen" {
		t.Fatalf("normalized model = %#v", entry.Models[0])
	}
}
