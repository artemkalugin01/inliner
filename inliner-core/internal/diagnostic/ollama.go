package diagnostic

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aokalugin/inliner/inliner-core/internal/completion"
	"github.com/aokalugin/inliner/inliner-core/internal/completion/ollama"
	"github.com/aokalugin/inliner/inliner-core/internal/config"
)

const previewLimit = 200

func TestOllama(ctx context.Context, cfg config.Config, output io.Writer) error {
	provider, err := ollama.New(ollama.Options{
		BaseURL: cfg.OllamaBaseURL,
		Model:   cfg.OllamaModel,
		Timeout: cfg.RequestTimeout,
		Generation: ollama.GenerationOptions{
			Temperature: cfg.OllamaTemperature,
			NumPredict:  cfg.OllamaNumPredict,
		},
		Prompt: staticPrompt("Return the single word ok."),
	})
	if err != nil {
		return err
	}

	response, err := provider.Complete(ctx, completion.Request{Language: "go", FilePath: "diagnostic.go"})
	if err != nil {
		return err
	}

	fmt.Fprintf(output, "ollama ok: model=%s base_url=%s items=%d preview=%q\n", cfg.OllamaModel, cfg.OllamaBaseURL, len(response.Items), preview(completionText(response.Items)))
	return nil
}

func completionText(items []completion.Item) string {
	var builder strings.Builder
	for _, item := range items {
		if item.Kind == "text" {
			builder.WriteString(item.Text)
			continue
		}
		if item.Kind == "end" {
			break
		}
	}
	return builder.String()
}

func preview(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= previewLimit {
		return text
	}
	return text[:previewLimit] + "..."
}

type staticPrompt string

func (p staticPrompt) Build(completion.Request) string {
	return string(p)
}
