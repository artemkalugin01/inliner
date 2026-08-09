package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadUsesDefaults(t *testing.T) {
	cfg, err := Load(emptyEnv)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	want := Default()
	if cfg != want {
		t.Fatalf("cfg = %+v, want %+v", cfg, want)
	}
}

func TestLoadOverridesFromEnv(t *testing.T) {
	cfg, err := Load(mapEnv(map[string]string{
		EnvProvider:                  " OLLAMA ",
		EnvOllamaBaseURL:             " http://localhost:1234/ ",
		EnvOllamaModel:               " deepseek-coder ",
		EnvOllamaTemperature:         "0.25",
		EnvOllamaNumPredict:          "256",
		EnvPromptMaxFiles:            "3",
		EnvPromptMaxImports:          "8",
		EnvPromptMaxTypes:            "4",
		EnvPromptMaxInterfaces:       "5",
		EnvPromptMaxInterfaceMethods: "6",
		EnvPromptMaxVisible:          "7",
		EnvPromptMaxSiblings:         "8",
		EnvPromptMaxValues:           "9",
		EnvPromptMaxFunctions:        "10",
		EnvDebugVerbose:              "true",
		EnvDebugDir:                  " /tmp/inliner-logs ",
		EnvRequestTimeout:            "5s",
		EnvWindowBytes:               "4096",
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Provider != ProviderOllama {
		t.Fatalf("Provider = %q, want %q", cfg.Provider, ProviderOllama)
	}
	if cfg.OllamaBaseURL != "http://localhost:1234" {
		t.Fatalf("OllamaBaseURL = %q", cfg.OllamaBaseURL)
	}
	if cfg.OllamaModel != "deepseek-coder" {
		t.Fatalf("OllamaModel = %q", cfg.OllamaModel)
	}
	if cfg.OllamaTemperature != 0.25 {
		t.Fatalf("OllamaTemperature = %v, want 0.25", cfg.OllamaTemperature)
	}
	if cfg.OllamaNumPredict != 256 {
		t.Fatalf("OllamaNumPredict = %d, want 256", cfg.OllamaNumPredict)
	}
	if cfg.PromptMaxFiles != 3 || cfg.PromptMaxImports != 8 || cfg.PromptMaxTypes != 4 || cfg.PromptMaxInterfaces != 5 || cfg.PromptMaxInterfaceMethods != 6 || cfg.PromptMaxVisible != 7 || cfg.PromptMaxSiblings != 8 || cfg.PromptMaxValues != 9 || cfg.PromptMaxFunctions != 10 {
		t.Fatalf("prompt budgets = %+v, want configured values", cfg)
	}
	if !cfg.DebugVerbose || cfg.DebugDir != "/tmp/inliner-logs" {
		t.Fatalf("debug config = %+v, want verbose /tmp/inliner-logs", cfg)
	}
	if cfg.RequestTimeout != 5*time.Second {
		t.Fatalf("RequestTimeout = %v, want 5s", cfg.RequestTimeout)
	}
	if cfg.WindowBytes != 4096 {
		t.Fatalf("WindowBytes = %d, want 4096", cfg.WindowBytes)
	}
}

func TestLoadKeepsDefaultsForBlankOptionalStrings(t *testing.T) {
	cfg, err := Load(mapEnv(map[string]string{
		EnvOllamaBaseURL: " ",
		EnvOllamaModel:   " ",
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	defaults := Default()
	if cfg.OllamaBaseURL != defaults.OllamaBaseURL {
		t.Fatalf("OllamaBaseURL = %q, want default %q", cfg.OllamaBaseURL, defaults.OllamaBaseURL)
	}
	if cfg.OllamaModel != defaults.OllamaModel {
		t.Fatalf("OllamaModel = %q, want default %q", cfg.OllamaModel, defaults.OllamaModel)
	}
}

func TestLoadRejectsInvalidProvider(t *testing.T) {
	_, err := Load(mapEnv(map[string]string{EnvProvider: "openai"}))
	if err == nil || !strings.Contains(err.Error(), EnvProvider) {
		t.Fatalf("error = %v, want %s validation error", err, EnvProvider)
	}
}

func TestLoadRejectsInvalidTimeout(t *testing.T) {
	_, err := Load(mapEnv(map[string]string{EnvRequestTimeout: "forever"}))
	if err == nil || !strings.Contains(err.Error(), EnvRequestTimeout) {
		t.Fatalf("error = %v, want %s validation error", err, EnvRequestTimeout)
	}
}

func TestLoadRejectsInvalidOllamaTemperature(t *testing.T) {
	_, err := Load(mapEnv(map[string]string{EnvOllamaTemperature: "cold"}))
	if err == nil || !strings.Contains(err.Error(), EnvOllamaTemperature) {
		t.Fatalf("error = %v, want %s validation error", err, EnvOllamaTemperature)
	}
}

func TestLoadRejectsOutOfRangeOllamaTemperature(t *testing.T) {
	_, err := Load(mapEnv(map[string]string{EnvOllamaTemperature: "2.5"}))
	if err == nil || !strings.Contains(err.Error(), EnvOllamaTemperature) {
		t.Fatalf("error = %v, want %s validation error", err, EnvOllamaTemperature)
	}
}

func TestLoadRejectsInvalidOllamaNumPredict(t *testing.T) {
	_, err := Load(mapEnv(map[string]string{EnvOllamaNumPredict: "many"}))
	if err == nil || !strings.Contains(err.Error(), EnvOllamaNumPredict) {
		t.Fatalf("error = %v, want %s validation error", err, EnvOllamaNumPredict)
	}
}

func TestLoadRejectsNonPositiveOllamaNumPredict(t *testing.T) {
	_, err := Load(mapEnv(map[string]string{EnvOllamaNumPredict: "0"}))
	if err == nil || !strings.Contains(err.Error(), EnvOllamaNumPredict) {
		t.Fatalf("error = %v, want %s validation error", err, EnvOllamaNumPredict)
	}
}

func TestLoadRejectsInvalidPromptBudget(t *testing.T) {
	_, err := Load(mapEnv(map[string]string{EnvPromptMaxFiles: "many"}))
	if err == nil || !strings.Contains(err.Error(), EnvPromptMaxFiles) {
		t.Fatalf("error = %v, want %s validation error", err, EnvPromptMaxFiles)
	}
}

func TestLoadRejectsNonPositivePromptBudget(t *testing.T) {
	_, err := Load(mapEnv(map[string]string{EnvPromptMaxFunctions: "0"}))
	if err == nil || !strings.Contains(err.Error(), EnvPromptMaxFunctions) {
		t.Fatalf("error = %v, want %s validation error", err, EnvPromptMaxFunctions)
	}
}

func TestLoadRejectsInvalidDebugVerbose(t *testing.T) {
	_, err := Load(mapEnv(map[string]string{EnvDebugVerbose: "sometimes"}))
	if err == nil || !strings.Contains(err.Error(), EnvDebugVerbose) {
		t.Fatalf("error = %v, want %s validation error", err, EnvDebugVerbose)
	}
}

func TestLoadRejectsNonPositiveTimeout(t *testing.T) {
	_, err := Load(mapEnv(map[string]string{EnvRequestTimeout: "0s"}))
	if err == nil || !strings.Contains(err.Error(), EnvRequestTimeout) {
		t.Fatalf("error = %v, want %s validation error", err, EnvRequestTimeout)
	}
}

func TestLoadRejectsInvalidWindowBytes(t *testing.T) {
	_, err := Load(mapEnv(map[string]string{EnvWindowBytes: "large"}))
	if err == nil || !strings.Contains(err.Error(), EnvWindowBytes) {
		t.Fatalf("error = %v, want %s validation error", err, EnvWindowBytes)
	}
}

func TestLoadRejectsNonPositiveWindowBytes(t *testing.T) {
	_, err := Load(mapEnv(map[string]string{EnvWindowBytes: "0"}))
	if err == nil || !strings.Contains(err.Error(), EnvWindowBytes) {
		t.Fatalf("error = %v, want %s validation error", err, EnvWindowBytes)
	}
}

func emptyEnv(string) (string, bool) {
	return "", false
}

func mapEnv(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
