package completion

import (
	"context"

	"github.com/aokalugin/inliner/inliner-core/internal/gocontext"
)

type Request struct {
	StateID  string
	FilePath string
	Language string
	Prefix   string
	Suffix   string
	Package  *gocontext.PackageContext
}

type Response struct {
	Items []Item
}

type Item struct {
	Kind   string
	Text   string
	Verify string
}

type Provider interface {
	Complete(ctx context.Context, request Request) (Response, error)
}

type ProviderDiagnostics struct {
	Status    string
	Reachable bool
	Error     string
}

type DiagnosticProvider interface {
	Diagnostics(ctx context.Context) ProviderDiagnostics
}
