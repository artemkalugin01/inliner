package fake

import (
	"context"
	"strings"

	"github.com/aokalugin/inliner/inliner-core/internal/completion"
)

type Provider struct{}

func (Provider) Complete(ctx context.Context, request completion.Request) (completion.Response, error) {
	select {
	case <-ctx.Done():
		return completion.Response{}, ctx.Err()
	default:
	}

	if !strings.HasSuffix(request.FilePath, ".go") {
		return completion.Response{Items: []completion.Item{{Kind: "end"}}}, nil
	}

	return completion.Response{Items: []completion.Item{
		{Kind: "text", Text: " // inliner"},
		{Kind: "end"},
	}}, nil
}

func (Provider) Diagnostics(ctx context.Context) completion.ProviderDiagnostics {
	select {
	case <-ctx.Done():
		return completion.ProviderDiagnostics{Status: "unreachable", Error: ctx.Err().Error()}
	default:
	}
	return completion.ProviderDiagnostics{Status: "ok", Reachable: true}
}
