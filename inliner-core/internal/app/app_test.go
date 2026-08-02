package app

import (
	"strings"
	"testing"

	"github.com/aokalugin/inliner/inliner-core/internal/config"
)

func TestCompletionServiceAcceptsFakeProvider(t *testing.T) {
	service, err := completionService(config.Config{Provider: config.ProviderFake})
	if err != nil {
		t.Fatalf("completionService returned error: %v", err)
	}
	if service == nil {
		t.Fatal("completionService returned nil service")
	}
}

func TestCompletionServiceAcceptsOllamaProvider(t *testing.T) {
	cfg := config.Default()
	cfg.Provider = config.ProviderOllama

	service, err := completionService(cfg)
	if err != nil {
		t.Fatalf("completionService returned error: %v", err)
	}
	if service == nil {
		t.Fatal("completionService returned nil service")
	}
}

func TestCompletionServiceRejectsUnknownProvider(t *testing.T) {
	_, err := completionService(config.Config{Provider: "unknown"})
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("error = %v, want unknown provider error", err)
	}
}
