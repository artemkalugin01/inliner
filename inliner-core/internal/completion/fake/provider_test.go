package fake

import (
	"context"
	"testing"

	"github.com/aokalugin/inliner/inliner-core/internal/completion"
)

func TestProviderCompletesGoFiles(t *testing.T) {
	provider := Provider{}

	response, err := provider.Complete(context.Background(), completion.Request{FilePath: "/tmp/main.go"})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	if len(response.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(response.Items))
	}
	if response.Items[0].Kind != "text" || response.Items[0].Text == "" {
		t.Fatalf("first item = %+v, want non-empty text item", response.Items[0])
	}
	if response.Items[1].Kind != "end" {
		t.Fatalf("second item = %+v, want end item", response.Items[1])
	}
}

func TestProviderEndsNonGoFiles(t *testing.T) {
	provider := Provider{}

	response, err := provider.Complete(context.Background(), completion.Request{FilePath: "/tmp/main.lua"})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	if len(response.Items) != 1 || response.Items[0].Kind != "end" {
		t.Fatalf("Items = %+v, want single end item", response.Items)
	}
}

func TestProviderHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := (Provider{}).Complete(ctx, completion.Request{FilePath: "/tmp/main.go"})
	if err == nil {
		t.Fatal("Complete returned nil error for cancelled context")
	}
}
