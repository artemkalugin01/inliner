package completion

import "context"

type Service struct {
	provider Provider
}

func NewService(provider Provider) *Service {
	return &Service{provider: provider}
}

func (s *Service) Complete(ctx context.Context, request Request) (Response, error) {
	return s.provider.Complete(ctx, request)
}

func (s *Service) Diagnostics(ctx context.Context) ProviderDiagnostics {
	provider, ok := s.provider.(DiagnosticProvider)
	if !ok {
		return ProviderDiagnostics{Status: "diagnostics unavailable"}
	}
	return provider.Diagnostics(ctx)
}
