package completion

import (
	"context"
	"errors"
	"testing"
)

type stubProvider struct {
	request  Request
	response Response
	err      error
}

func (p *stubProvider) Complete(ctx context.Context, request Request) (Response, error) {
	p.request = request
	return p.response, p.err
}

func TestServiceDelegatesToProvider(t *testing.T) {
	provider := &stubProvider{response: Response{Items: []Item{{Kind: "end"}}}}
	service := NewService(provider)

	response, err := service.Complete(context.Background(), Request{StateID: "1", FilePath: "/tmp/main.go"})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	if provider.request.StateID != "1" || provider.request.FilePath != "/tmp/main.go" {
		t.Fatalf("provider request = %+v, want forwarded request", provider.request)
	}
	if len(response.Items) != 1 || response.Items[0].Kind != "end" {
		t.Fatalf("response = %+v, want provider response", response)
	}
}

func TestServiceReturnsProviderError(t *testing.T) {
	wantErr := errors.New("provider failed")
	service := NewService(&stubProvider{err: wantErr})

	_, err := service.Complete(context.Background(), Request{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}
