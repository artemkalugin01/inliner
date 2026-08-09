package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aokalugin/inliner/inliner-core/internal/prompt"
)

const (
	ProviderFake   = "fake"
	ProviderOllama = "ollama"

	EnvProvider                  = "INLINER_PROVIDER"
	EnvOllamaBaseURL             = "INLINER_OLLAMA_BASE_URL"
	EnvOllamaModel               = "INLINER_OLLAMA_MODEL"
	EnvOllamaTemperature         = "INLINER_OLLAMA_TEMPERATURE"
	EnvOllamaNumPredict          = "INLINER_OLLAMA_NUM_PREDICT"
	EnvPromptMaxFiles            = "INLINER_PROMPT_MAX_FILES"
	EnvPromptMaxImports          = "INLINER_PROMPT_MAX_IMPORTS"
	EnvPromptMaxTypes            = "INLINER_PROMPT_MAX_TYPES"
	EnvPromptMaxInterfaces       = "INLINER_PROMPT_MAX_INTERFACES"
	EnvPromptMaxInterfaceMethods = "INLINER_PROMPT_MAX_INTERFACE_METHODS"
	EnvPromptMaxVisible          = "INLINER_PROMPT_MAX_VISIBLE"
	EnvPromptMaxSiblings         = "INLINER_PROMPT_MAX_SIBLINGS"
	EnvPromptMaxValues           = "INLINER_PROMPT_MAX_VALUES"
	EnvPromptMaxFunctions        = "INLINER_PROMPT_MAX_FUNCTIONS"
	EnvDebugVerbose              = "INLINER_DEBUG_VERBOSE"
	EnvDebugDir                  = "INLINER_DEBUG_DIR"
	EnvTelemetryEnabled          = "INLINER_TELEMETRY_ENABLED"
	EnvRequestTimeout            = "INLINER_REQUEST_TIMEOUT"
	EnvWindowBytes               = "INLINER_WINDOW_BYTES"
)

type Config struct {
	Provider                  string
	OllamaBaseURL             string
	OllamaModel               string
	OllamaTemperature         float64
	OllamaNumPredict          int
	PromptMaxFiles            int
	PromptMaxImports          int
	PromptMaxTypes            int
	PromptMaxInterfaces       int
	PromptMaxInterfaceMethods int
	PromptMaxVisible          int
	PromptMaxSiblings         int
	PromptMaxValues           int
	PromptMaxFunctions        int
	DebugVerbose              bool
	DebugDir                  string
	TelemetryEnabled          bool
	RequestTimeout            time.Duration
	WindowBytes               int
}

func Default() Config {
	return Config{
		Provider:                  ProviderFake,
		OllamaBaseURL:             "http://127.0.0.1:11434",
		OllamaModel:               "qwen2.5-coder:7b",
		OllamaTemperature:         0.1,
		OllamaNumPredict:          128,
		PromptMaxFiles:            prompt.DefaultMaxFiles,
		PromptMaxImports:          prompt.DefaultMaxImports,
		PromptMaxTypes:            prompt.DefaultMaxTypes,
		PromptMaxInterfaces:       prompt.DefaultMaxInterfaces,
		PromptMaxInterfaceMethods: prompt.DefaultMaxInterfaceMethods,
		PromptMaxVisible:          prompt.DefaultMaxVisible,
		PromptMaxSiblings:         prompt.DefaultMaxSiblings,
		PromptMaxValues:           prompt.DefaultMaxValues,
		PromptMaxFunctions:        prompt.DefaultMaxFunctions,
		DebugDir:                  filepath.Join(os.TempDir(), "inliner-debug"),
		RequestTimeout:            3 * time.Second,
		WindowBytes:               2000,
	}
}

func LoadEnv() (Config, error) {
	return Load(lookupEnv)
}

func Load(lookup func(string) (string, bool)) (Config, error) {
	cfg := Default()

	if value, ok := lookup(EnvProvider); ok {
		provider := strings.ToLower(strings.TrimSpace(value))
		switch provider {
		case ProviderFake, ProviderOllama:
			cfg.Provider = provider
		default:
			return Config{}, fmt.Errorf("%s must be one of %q or %q", EnvProvider, ProviderFake, ProviderOllama)
		}
	}

	if value, ok := lookup(EnvOllamaBaseURL); ok {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			cfg.OllamaBaseURL = strings.TrimRight(trimmed, "/")
		}
	}

	if value, ok := lookup(EnvOllamaModel); ok {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			cfg.OllamaModel = trimmed
		}
	}

	if value, ok := lookup(EnvOllamaTemperature); ok {
		temperature, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", EnvOllamaTemperature, err)
		}
		if temperature < 0 || temperature > 2 {
			return Config{}, fmt.Errorf("%s must be between 0 and 2", EnvOllamaTemperature)
		}
		cfg.OllamaTemperature = temperature
	}

	if value, ok := lookup(EnvOllamaNumPredict); ok {
		numPredict, err := parsePositiveInt(EnvOllamaNumPredict, value)
		if err != nil {
			return Config{}, err
		}
		cfg.OllamaNumPredict = numPredict
	}

	if value, ok := lookup(EnvPromptMaxFiles); ok {
		parsed, err := parsePositiveInt(EnvPromptMaxFiles, value)
		if err != nil {
			return Config{}, err
		}
		cfg.PromptMaxFiles = parsed
	}
	if value, ok := lookup(EnvPromptMaxImports); ok {
		parsed, err := parsePositiveInt(EnvPromptMaxImports, value)
		if err != nil {
			return Config{}, err
		}
		cfg.PromptMaxImports = parsed
	}
	if value, ok := lookup(EnvPromptMaxTypes); ok {
		parsed, err := parsePositiveInt(EnvPromptMaxTypes, value)
		if err != nil {
			return Config{}, err
		}
		cfg.PromptMaxTypes = parsed
	}
	if value, ok := lookup(EnvPromptMaxInterfaces); ok {
		parsed, err := parsePositiveInt(EnvPromptMaxInterfaces, value)
		if err != nil {
			return Config{}, err
		}
		cfg.PromptMaxInterfaces = parsed
	}
	if value, ok := lookup(EnvPromptMaxInterfaceMethods); ok {
		parsed, err := parsePositiveInt(EnvPromptMaxInterfaceMethods, value)
		if err != nil {
			return Config{}, err
		}
		cfg.PromptMaxInterfaceMethods = parsed
	}
	if value, ok := lookup(EnvPromptMaxVisible); ok {
		parsed, err := parsePositiveInt(EnvPromptMaxVisible, value)
		if err != nil {
			return Config{}, err
		}
		cfg.PromptMaxVisible = parsed
	}
	if value, ok := lookup(EnvPromptMaxSiblings); ok {
		parsed, err := parsePositiveInt(EnvPromptMaxSiblings, value)
		if err != nil {
			return Config{}, err
		}
		cfg.PromptMaxSiblings = parsed
	}
	if value, ok := lookup(EnvPromptMaxValues); ok {
		parsed, err := parsePositiveInt(EnvPromptMaxValues, value)
		if err != nil {
			return Config{}, err
		}
		cfg.PromptMaxValues = parsed
	}
	if value, ok := lookup(EnvPromptMaxFunctions); ok {
		parsed, err := parsePositiveInt(EnvPromptMaxFunctions, value)
		if err != nil {
			return Config{}, err
		}
		cfg.PromptMaxFunctions = parsed
	}

	if value, ok := lookup(EnvDebugVerbose); ok {
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", EnvDebugVerbose, err)
		}
		cfg.DebugVerbose = parsed
	}

	if value, ok := lookup(EnvTelemetryEnabled); ok {
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", EnvTelemetryEnabled, err)
		}
		cfg.TelemetryEnabled = parsed
	}

	if value, ok := lookup(EnvDebugDir); ok {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			cfg.DebugDir = trimmed
		}
	}

	if value, ok := lookup(EnvRequestTimeout); ok {
		duration, err := time.ParseDuration(strings.TrimSpace(value))
		if err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", EnvRequestTimeout, err)
		}
		if duration <= 0 {
			return Config{}, fmt.Errorf("%s must be greater than zero", EnvRequestTimeout)
		}
		cfg.RequestTimeout = duration
	}

	if value, ok := lookup(EnvWindowBytes); ok {
		windowBytes, err := parsePositiveInt(EnvWindowBytes, value)
		if err != nil {
			return Config{}, err
		}
		cfg.WindowBytes = windowBytes
	}

	return cfg, nil
}

func parsePositiveInt(name string, value string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	return parsed, nil
}

func lookupEnv(key string) (string, bool) {
	return os.LookupEnv(key)
}
