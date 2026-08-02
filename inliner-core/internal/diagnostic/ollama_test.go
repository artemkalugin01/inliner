package diagnostic

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aokalugin/inliner/inliner-core/internal/completion"
	"github.com/aokalugin/inliner/inliner-core/internal/config"
)

func TestTestOllamaSendsDiagnosticRequest(t *testing.T) {
	var captured struct {
		Model   string         `json:"model"`
		Prompt  string         `json:"prompt"`
		Stream  bool           `json:"stream"`
		Options map[string]any `json:"options"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Fatalf("path = %q, want /api/generate", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"response":"ok"}`))
	}))
	defer server.Close()

	var output bytes.Buffer
	err := TestOllama(context.Background(), config.Config{
		OllamaBaseURL:     server.URL,
		OllamaModel:       "test-model",
		OllamaTemperature: 0.2,
		OllamaNumPredict:  32,
		RequestTimeout:    time.Second,
	}, &output)
	if err != nil {
		t.Fatalf("TestOllama returned error: %v", err)
	}

	if captured.Model != "test-model" {
		t.Fatalf("Model = %q, want test-model", captured.Model)
	}
	if captured.Prompt != "Return the single word ok." {
		t.Fatalf("Prompt = %q, want diagnostic prompt", captured.Prompt)
	}
	if captured.Stream {
		t.Fatal("Stream = true, want false")
	}
	if captured.Options["temperature"] != float64(0.2) {
		t.Fatalf("temperature = %#v, want 0.2", captured.Options["temperature"])
	}
	if captured.Options["num_predict"] != float64(32) {
		t.Fatalf("num_predict = %#v, want 32", captured.Options["num_predict"])
	}
	if !strings.Contains(output.String(), "ollama ok: model=test-model") {
		t.Fatalf("output = %q, want success message", output.String())
	}
	if !strings.Contains(output.String(), `preview="ok"`) {
		t.Fatalf("output = %q, want response preview", output.String())
	}
}

func TestTestOllamaReturnsProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	var output bytes.Buffer
	err := TestOllama(context.Background(), config.Config{
		OllamaBaseURL:  server.URL,
		OllamaModel:    "test-model",
		RequestTimeout: time.Second,
	}, &output)
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("error = %v, want HTTP error", err)
	}
}

func TestCompletionTextStopsAtEnd(t *testing.T) {
	text := completionText([]completion.Item{
		{Kind: "text", Text: "first"},
		{Kind: "text", Text: " second"},
		{Kind: "end"},
		{Kind: "text", Text: " ignored"},
	})

	if text != "first second" {
		t.Fatalf("completionText = %q, want first second", text)
	}
}

func TestPreviewTrimsAndTruncates(t *testing.T) {
	if got := preview(" ok \n"); got != "ok" {
		t.Fatalf("preview = %q, want ok", got)
	}

	long := strings.Repeat("a", previewLimit+1)
	got := preview(long)
	if len(got) != previewLimit+3 {
		t.Fatalf("len(preview) = %d, want %d", len(got), previewLimit+3)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("preview = %q, want ellipsis suffix", got)
	}
}
