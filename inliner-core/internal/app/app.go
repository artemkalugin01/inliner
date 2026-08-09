package app

import (
	"context"
	"fmt"
	"io"

	"github.com/aokalugin/inliner/inliner-core/internal/completion"
	"github.com/aokalugin/inliner/inliner-core/internal/completion/fake"
	"github.com/aokalugin/inliner/inliner-core/internal/completion/ollama"
	"github.com/aokalugin/inliner/inliner-core/internal/config"
	"github.com/aokalugin/inliner/inliner-core/internal/prompt"
	"github.com/aokalugin/inliner/inliner-core/internal/protocol"
	"github.com/aokalugin/inliner/inliner-core/internal/session"
)

func Run(ctx context.Context, input io.Reader, output io.Writer) error {
	cfg, err := config.LoadEnv()
	if err != nil {
		return err
	}

	transport := protocol.NewTransport(input, output)
	service, err := completionService(cfg)
	if err != nil {
		return err
	}
	sess := session.New(transport, service, session.Options{
		Provider:          cfg.Provider,
		OllamaBaseURL:     cfg.OllamaBaseURL,
		OllamaModel:       cfg.OllamaModel,
		OllamaTemperature: cfg.OllamaTemperature,
		OllamaNumPredict:  cfg.OllamaNumPredict,
		RequestTimeout:    cfg.RequestTimeout,
		WindowBytes:       cfg.WindowBytes,
	})

	return sess.Run(ctx)
}

func completionService(cfg config.Config) (*completion.Service, error) {
	switch cfg.Provider {
	case config.ProviderFake:
		return completion.NewService(fake.Provider{}), nil
	case config.ProviderOllama:
		provider, err := ollama.New(ollama.Options{
			BaseURL: cfg.OllamaBaseURL,
			Model:   cfg.OllamaModel,
			Timeout: cfg.RequestTimeout,
			Debug: ollama.DebugOptions{
				Verbose: cfg.DebugVerbose,
				Dir:     cfg.DebugDir,
			},
			Prompt: prompt.GoInlineBuilder{
				MaxFiles:            cfg.PromptMaxFiles,
				MaxImports:          cfg.PromptMaxImports,
				MaxTypes:            cfg.PromptMaxTypes,
				MaxInterfaces:       cfg.PromptMaxInterfaces,
				MaxInterfaceMethods: cfg.PromptMaxInterfaceMethods,
				MaxVisible:          cfg.PromptMaxVisible,
				MaxSiblings:         cfg.PromptMaxSiblings,
				MaxFunctions:        cfg.PromptMaxFunctions,
			},
			Generation: ollama.GenerationOptions{
				Temperature: cfg.OllamaTemperature,
				NumPredict:  cfg.OllamaNumPredict,
			},
		})
		if err != nil {
			return nil, err
		}
		return completion.NewService(provider), nil
	default:
		return nil, fmt.Errorf("unknown provider %q", cfg.Provider)
	}
}
