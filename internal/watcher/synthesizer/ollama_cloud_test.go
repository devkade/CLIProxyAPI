package synthesizer

import (
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestConfigSynthesizerOllamaCloud(t *testing.T) {
	synth := NewConfigSynthesizer()
	auths, err := synth.Synthesize(&SynthesisContext{
		Config: &config.Config{OllamaCloudKey: []config.OllamaCloudKey{{
			APIKey: "ollama-secret",
			Prefix: "cloud",
		}}},
		Now:         time.Unix(1, 0),
		IDGenerator: NewStableIDGenerator(),
	})
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("auth count = %d, want 1", len(auths))
	}
	auth := auths[0]
	if auth.Provider != "ollama-cloud" || auth.Label != "ollama-cloud-apikey" {
		t.Fatalf("auth provider/label = %q/%q", auth.Provider, auth.Label)
	}
	if auth.Prefix != "cloud" {
		t.Fatalf("prefix = %q, want cloud", auth.Prefix)
	}
	if auth.Attributes["api_key"] != "ollama-secret" {
		t.Fatal("API key was not carried through the config auth mechanism")
	}
	if auth.Attributes["base_url"] != "https://ollama.com/v1" {
		t.Fatalf("base_url = %q, want official cloud OpenAI endpoint", auth.Attributes["base_url"])
	}
}
