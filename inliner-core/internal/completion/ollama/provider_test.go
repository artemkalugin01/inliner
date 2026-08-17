package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aokalugin/inliner/inliner-core/internal/completion"
	"github.com/aokalugin/inliner/inliner-core/internal/diagnostics"
)

type recordingDiagnostics struct {
	mu      sync.Mutex
	verbose bool
	events  []diagnostics.Event
}

func (r *recordingDiagnostics) Publish(event diagnostics.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recordingDiagnostics) Verbose() bool { return r.verbose }
func (r *recordingDiagnostics) Close()        {}

func (r *recordingDiagnostics) Events() []diagnostics.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]diagnostics.Event(nil), r.events...)
}

func TestProviderCompleteSendsGenerateRequest(t *testing.T) {
	var captured generateRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Fatalf("path = %q, want /api/generate", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":"fmt.Println(name)"}`))
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	response, err := provider.Complete(context.Background(), completion.Request{
		Language: "go",
		FilePath: "/tmp/main.go",
		Prefix:   "package main\n\nfunc main() {\n\t",
		Suffix:   "\n}\n",
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	if captured.Model != "test-model" {
		t.Fatalf("Model = %q, want test-model", captured.Model)
	}
	if captured.Stream {
		t.Fatal("Stream = true, want false")
	}
	if !strings.Contains(captured.Prompt, "inline autocomplete engine for Go code") {
		t.Fatalf("Prompt does not contain instruction: %q", captured.Prompt)
	}
	if !strings.Contains(captured.Prompt, "/tmp/main.go") {
		t.Fatalf("Prompt does not contain file path: %q", captured.Prompt)
	}
	if captured.Options["temperature"] != float64(0.1) {
		t.Fatalf("temperature = %#v, want 0.1", captured.Options["temperature"])
	}

	if len(response.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(response.Items))
	}
	if response.Items[0].Kind != "text" || response.Items[0].Text != "fmt.Println(name)" {
		t.Fatalf("first item = %+v, want generated text", response.Items[0])
	}
	if response.Items[1].Kind != "end" {
		t.Fatalf("second item = %+v, want end", response.Items[1])
	}
}

func TestProviderPublishesPromptAndModelTimingDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		_, _ = w.Write([]byte(`{"response":"ok"}`))
	}))
	defer server.Close()

	recorder := &recordingDiagnostics{verbose: true}
	provider, err := New(Options{
		BaseURL:     server.URL,
		Model:       "test-model",
		Prompt:      staticPrompt{value: "diagnostic prompt"},
		Diagnostics: recorder,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	timings := &completion.RequestTimings{}
	timings.SetRequestBuild(4 * time.Millisecond)
	_, err = provider.Complete(context.Background(), completion.Request{StateID: "42", Language: "go", Timings: timings})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	events := recorder.Events()
	if len(events) != 2 {
		t.Fatalf("events = %+v, want prompt and awaiting-model events", events)
	}
	if events[0].Kind != diagnostics.KindPrompt || events[0].Prompt != "diagnostic prompt" {
		t.Fatalf("prompt event = %+v", events[0])
	}
	if events[1].Kind != diagnostics.KindAwaitingModel || events[1].ContextMilliseconds < 4 {
		t.Fatalf("awaiting event = %+v", events[1])
	}
	if timings.ModelWaitMs() < 10 {
		t.Fatalf("model wait = %dms, want at least 10ms", timings.ModelWaitMs())
	}
}

func TestProviderDoesNotPublishPromptWithoutVerboseDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"response":"ok"}`))
	}))
	defer server.Close()

	recorder := &recordingDiagnostics{}
	provider, err := New(Options{BaseURL: server.URL, Model: "test-model", Prompt: staticPrompt{value: "hidden"}, Diagnostics: recorder})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	_, err = provider.Complete(context.Background(), completion.Request{StateID: "42", Language: "go", Timings: &completion.RequestTimings{}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	for _, event := range recorder.Events() {
		if event.Kind == diagnostics.KindPrompt {
			t.Fatalf("non-verbose diagnostics published prompt: %+v", event)
		}
	}
}

func TestProviderCompleteRemovesDuplicatedSuffixOverlap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"response":"fmt.Println(name)\n}\n"}`))
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	response, err := provider.Complete(context.Background(), completion.Request{Language: "go", Suffix: "\n}\n"})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	if len(response.Items) != 2 || response.Items[0].Text != "fmt.Println(name)" {
		t.Fatalf("Items = %+v, want suffix-trimmed completion", response.Items)
	}
}

func TestProviderCompleteUsesInjectedPromptBuilder(t *testing.T) {
	var captured generateRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"response":"ok"}`))
	}))
	defer server.Close()

	provider, err := New(Options{BaseURL: server.URL, Model: "test-model", Prompt: staticPrompt{value: "custom prompt"}})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, err = provider.Complete(context.Background(), completion.Request{Language: "go"})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	if captured.Prompt != "custom prompt" {
		t.Fatalf("Prompt = %q, want custom prompt", captured.Prompt)
	}
}

func TestProviderCompleteUsesConfiguredGenerationOptions(t *testing.T) {
	var captured generateRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"response":"ok"}`))
	}))
	defer server.Close()

	provider, err := New(Options{
		BaseURL: server.URL,
		Model:   "test-model",
		Generation: GenerationOptions{
			Temperature: 0.35,
			NumPredict:  512,
		},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, err = provider.Complete(context.Background(), completion.Request{Language: "go"})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	if captured.Options["temperature"] != float64(0.35) {
		t.Fatalf("temperature = %#v, want 0.35", captured.Options["temperature"])
	}
	if captured.Options["num_predict"] != float64(512) {
		t.Fatalf("num_predict = %#v, want 512", captured.Options["num_predict"])
	}
}

func TestNewUsesDefaultGenerationOptions(t *testing.T) {
	provider, err := New(Options{BaseURL: "http://localhost:11434", Model: "test-model"})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if provider.generation.Temperature != 0.1 {
		t.Fatalf("Temperature = %v, want 0.1", provider.generation.Temperature)
	}
	if provider.generation.NumPredict != 128 {
		t.Fatalf("NumPredict = %d, want 128", provider.generation.NumPredict)
	}
}

func TestProviderCompleteEndsForNonGoLanguage(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	response, err := provider.Complete(context.Background(), completion.Request{Language: "lua", FilePath: "/tmp/init.lua"})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if called {
		t.Fatal("server was called for non-Go language")
	}
	if len(response.Items) != 1 || response.Items[0].Kind != "end" {
		t.Fatalf("Items = %+v, want single end item", response.Items)
	}
}

func TestProviderCompleteEndsForBlankGeneratedText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"response":"   "}`))
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	response, err := provider.Complete(context.Background(), completion.Request{Language: "go"})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if len(response.Items) != 1 || response.Items[0].Kind != "end" {
		t.Fatalf("Items = %+v, want single end item", response.Items)
	}
}

func TestProviderCompleteReturnsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	_, err := provider.Complete(context.Background(), completion.Request{Language: "go"})
	if err == nil || !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "model unavailable") {
		t.Fatalf("error = %v, want HTTP status/body error", err)
	}
}

func TestProviderCompleteReturnsOllamaError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	_, err := provider.Complete(context.Background(), completion.Request{Language: "go"})
	if err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("error = %v, want ollama error", err)
	}
}

func TestProviderCompleteReturnsInvalidJSONError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	_, err := provider.Complete(context.Background(), completion.Request{Language: "go"})
	if err == nil {
		t.Fatal("Complete returned nil error for invalid JSON")
	}
}

func TestProviderCompleteHonorsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := provider.Complete(ctx, completion.Request{Language: "go"})
	if err == nil {
		t.Fatal("Complete returned nil error for cancelled context")
	}
}

func TestProviderDiagnosticsChecksOllamaTags(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/api/tags" {
			t.Fatalf("path = %q, want /api/tags", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Fatalf("method = %q, want GET", r.Method)
		}
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	diagnostics := provider.Diagnostics(context.Background())

	if !called {
		t.Fatal("server was not called")
	}
	if diagnostics.Status != "ok" || !diagnostics.Reachable || diagnostics.Error != "" {
		t.Fatalf("Diagnostics = %+v, want ok reachable", diagnostics)
	}
}

func TestProviderDiagnosticsReportsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL)
	diagnostics := provider.Diagnostics(context.Background())

	if diagnostics.Status != "unhealthy" || diagnostics.Reachable || !strings.Contains(diagnostics.Error, "503") {
		t.Fatalf("Diagnostics = %+v, want unhealthy status error", diagnostics)
	}
}

func TestProviderDebugLoggingWritesPromptAndTiming(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"response":"fmt.Println(name)"}`))
	}))
	defer server.Close()

	debugDir := t.TempDir()
	provider, err := New(Options{
		BaseURL: server.URL,
		Model:   "test-model",
		Prompt:  staticPrompt{value: "debug prompt body"},
		Debug:   DebugOptions{Verbose: true, Dir: debugDir},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	filePath := filepath.Join(projectDir, "main.go")

	_, err = provider.Complete(context.Background(), completion.Request{Language: "go", FilePath: filePath, StateID: "42"})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	prompts, err := filepath.Glob(filepath.Join(debugDir, "prompts", "*.prompt.txt"))
	if err != nil {
		t.Fatalf("Glob returned error: %v", err)
	}
	if len(prompts) != 1 {
		t.Fatalf("len(prompts) = %d, want 1", len(prompts))
	}
	if matched := regexp.MustCompile(`\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}\.\d{3}_MSK\.prompt\.txt`).MatchString(filepath.Base(prompts[0])); !matched {
		t.Fatalf("prompt filename = %q, want readable Moscow timestamp", filepath.Base(prompts[0]))
	}
	promptContent, err := os.ReadFile(prompts[0])
	if err != nil {
		t.Fatalf("ReadFile prompt: %v", err)
	}
	promptText := string(promptContent)
	lines := strings.Split(promptText, "\n")
	if len(lines) < 2 {
		t.Fatalf("prompt content has too few lines: %q", promptText)
	}
	if !strings.HasPrefix(lines[0], "projectHash: ") {
		t.Fatalf("first prompt line = %q, want projectHash", lines[0])
	}
	if lines[0] != "projectHash: "+projectHash(filePath) {
		t.Fatalf("first prompt line = %q, want hash for project path", lines[0])
	}
	if lines[1] != "model: test-model provider=ollama baseURL="+server.URL {
		t.Fatalf("second prompt line = %q, want model info", lines[1])
	}
	if !strings.Contains(promptText, "debug prompt body") || !strings.Contains(promptText, "state: 42") {
		t.Fatalf("prompt content = %q, want prompt and state", string(promptContent))
	}

	timingContent, err := os.ReadFile(filepath.Join(debugDir, "completion-timings.log"))
	if err != nil {
		t.Fatalf("ReadFile timings: %v", err)
	}
	for _, want := range []string{"model=test-model", "state=42", "file=" + filePath, "duration=", "status=ok"} {
		if !strings.Contains(string(timingContent), want) {
			t.Fatalf("timing log does not contain %q: %q", want, string(timingContent))
		}
	}
}

func TestNewValidatesOptions(t *testing.T) {
	if _, err := New(Options{Model: "m"}); err == nil || !strings.Contains(err.Error(), "base URL") {
		t.Fatalf("missing base URL error = %v", err)
	}
	if _, err := New(Options{BaseURL: "http://localhost"}); err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("missing model error = %v", err)
	}
}

func TestNewTrimsBaseURL(t *testing.T) {
	provider, err := New(Options{BaseURL: " http://localhost:11434/ ", Model: " model "})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if provider.baseURL != "http://localhost:11434" {
		t.Fatalf("baseURL = %q, want trimmed URL", provider.baseURL)
	}
	if provider.model != "model" {
		t.Fatalf("model = %q, want trimmed model", provider.model)
	}
}

func TestSanitizeCompletion(t *testing.T) {
	tests := map[string]string{
		" plain ":                         "plain",
		"```go\nfmt.Println(name)\n```":   "fmt.Println(name)",
		"```golang\nreturn nil\n```":      "return nil",
		"```\nreturn nil\n```":            "return nil",
		"`fmt.Println(name)`":             "fmt.Println(name)",
		"go\nfmt.Println(name)":           "fmt.Println(name)",
		"GOLANG\r\nfmt.Println(name)\r\n": "fmt.Println(name)",
		"go":                              "",
		"GOLANG":                          "",
	}

	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			if got := sanitizeCompletion(input); got != want {
				t.Fatalf("sanitizeCompletion() = %q, want %q", got, want)
			}
		})
	}
}

func TestRemoveSuffixOverlap(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		suffix string
		want   string
	}{
		{name: "full suffix", text: "fmt.Println(name)\n}\n", suffix: "\n}\n", want: "fmt.Println(name)"},
		{name: "partial suffix", text: "return nil\n}", suffix: "\n}\n", want: "return nil"},
		{name: "no overlap", text: "return nil", suffix: "\n}\n", want: "return nil"},
		{name: "empty suffix", text: "return nil", suffix: "", want: "return nil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := removeSuffixOverlap(tt.text, tt.suffix); got != tt.want {
				t.Fatalf("removeSuffixOverlap() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPostProcessCompletionTrimsAfterSuffixRemoval(t *testing.T) {
	got := postProcessCompletion(completion.Request{Suffix: "\n}\n"}, "```go\nfmt.Println(name)\n}\n```")
	if got != "fmt.Println(name)" {
		t.Fatalf("postProcessCompletion() = %q, want fmt.Println(name)", got)
	}
}

type staticPrompt struct {
	value string
}

func (p staticPrompt) Build(completion.Request) string {
	return p.value
}

func newTestProvider(t *testing.T, baseURL string) *Provider {
	t.Helper()

	provider, err := New(Options{BaseURL: baseURL, Model: "test-model", Timeout: time.Second})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	return provider
}
