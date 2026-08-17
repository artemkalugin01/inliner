package ollama

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aokalugin/inliner/inliner-core/internal/completion"
	"github.com/aokalugin/inliner/inliner-core/internal/diagnostics"
	"github.com/aokalugin/inliner/inliner-core/internal/prompt"
)

type Provider struct {
	baseURL     string
	model       string
	client      *http.Client
	prompt      prompt.Builder
	generation  GenerationOptions
	debug       DebugOptions
	diagnostics diagnostics.Publisher
}

type GenerationOptions struct {
	Temperature float64
	NumPredict  int
}

type Options struct {
	BaseURL     string
	Model       string
	Timeout     time.Duration
	Client      *http.Client
	Prompt      prompt.Builder
	Generation  GenerationOptions
	Debug       DebugOptions
	Diagnostics diagnostics.Publisher
}

type DebugOptions struct {
	Verbose bool
	Dir     string
}

type generateRequest struct {
	Model   string         `json:"model"`
	Prompt  string         `json:"prompt"`
	Stream  bool           `json:"stream"`
	Options map[string]any `json:"options,omitempty"`
}

type generateResponse struct {
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
}

func New(options Options) (*Provider, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("ollama base URL is required")
	}

	model := strings.TrimSpace(options.Model)
	if model == "" {
		return nil, fmt.Errorf("ollama model is required")
	}

	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: options.Timeout}
	}
	if client.Timeout == 0 && options.Timeout > 0 {
		client.Timeout = options.Timeout
	}

	promptBuilder := options.Prompt
	if promptBuilder == nil {
		promptBuilder = prompt.GoInlineBuilder{}
	}

	generation := options.Generation
	if generation.Temperature == 0 {
		generation.Temperature = 0.1
	}
	if generation.NumPredict == 0 {
		generation.NumPredict = 128
	}

	debug := options.Debug
	if debug.Verbose && strings.TrimSpace(debug.Dir) == "" {
		debug.Dir = filepath.Join(os.TempDir(), "inliner-debug")
	}

	diagnosticPublisher := options.Diagnostics
	if diagnosticPublisher == nil {
		diagnosticPublisher = diagnostics.Noop()
	}

	return &Provider{baseURL: baseURL, model: model, client: client, prompt: promptBuilder, generation: generation, debug: debug, diagnostics: diagnosticPublisher}, nil
}

func (p *Provider) Complete(ctx context.Context, request completion.Request) (completion.Response, error) {
	if request.Language != "go" {
		return completion.Response{Items: []completion.Item{{Kind: "end"}}}, nil
	}

	promptStarted := time.Now()
	promptText := p.prompt.Build(request)
	request.Timings.SetPromptBuild(time.Since(promptStarted))
	p.writeDebugPrompt(request, promptText)
	if p.diagnostics.Verbose() {
		p.diagnostics.Publish(diagnostics.Event{
			Kind:      diagnostics.KindPrompt,
			RequestID: request.StateID,
			StateID:   request.StateID,
			Prompt:    promptText,
		})
	}

	body, err := json.Marshal(generateRequest{
		Model:  p.model,
		Prompt: promptText,
		Stream: false,
		Options: map[string]any{
			"temperature": p.generation.Temperature,
			"num_predict": p.generation.NumPredict,
		},
	})
	if err != nil {
		return completion.Response{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return completion.Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	started := time.Now()
	p.diagnostics.Publish(diagnostics.Event{
		Kind:                diagnostics.KindAwaitingModel,
		RequestID:           request.StateID,
		StateID:             request.StateID,
		ContextMilliseconds: request.Timings.ContextPreparationMs(),
	})
	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		duration := time.Since(started)
		request.Timings.SetModelWait(duration)
		p.writeDebugTiming(request, duration, err)
		return completion.Response{}, err
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	duration := time.Since(started)
	request.Timings.SetModelWait(duration)
	if err != nil {
		p.writeDebugTiming(request, duration, err)
		return completion.Response{}, err
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		err := fmt.Errorf("ollama generate failed with status %d: %s", httpResp.StatusCode, strings.TrimSpace(string(respBody)))
		p.writeDebugTiming(request, duration, err)
		return completion.Response{}, err
	}

	var generated generateResponse
	if err := json.Unmarshal(respBody, &generated); err != nil {
		p.writeDebugTiming(request, duration, err)
		return completion.Response{}, err
	}
	if generated.Error != "" {
		err := fmt.Errorf("ollama generate failed: %s", generated.Error)
		p.writeDebugTiming(request, duration, err)
		return completion.Response{}, err
	}
	p.writeDebugTiming(request, duration, nil)

	text := postProcessCompletion(request, generated.Response)
	if text == "" {
		return completion.Response{Items: []completion.Item{{Kind: "end"}}}, nil
	}

	return completion.Response{Items: []completion.Item{
		{Kind: "text", Text: text},
		{Kind: "end"},
	}}, nil
}

func (p *Provider) Diagnostics(ctx context.Context) completion.ProviderDiagnostics {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/api/tags", nil)
	if err != nil {
		return completion.ProviderDiagnostics{Status: "unreachable", Error: err.Error()}
	}

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return completion.ProviderDiagnostics{Status: "unreachable", Error: err.Error()}
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return completion.ProviderDiagnostics{Status: "unhealthy", Error: fmt.Sprintf("ollama tags failed with status %d", httpResp.StatusCode)}
	}

	return completion.ProviderDiagnostics{Status: "ok", Reachable: true}
}

func (p *Provider) writeDebugPrompt(request completion.Request, promptText string) {
	if !p.debug.Verbose {
		return
	}
	dir := filepath.Join(p.debug.Dir, "prompts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	name := moscowTime(time.Now()).Format("2006-01-02_15-04-05.000_MSK") + ".prompt.txt"
	content := "projectHash: " + projectHash(request.FilePath) + "\n" +
		"model: " + p.model + " provider=ollama baseURL=" + p.baseURL + "\n" +
		"file: " + request.FilePath + "\n" +
		"state: " + request.StateID + "\n\n" + promptText
	_ = os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
}

func moscowTime(t time.Time) time.Time {
	location := time.FixedZone("MSK", 3*60*60)
	return t.In(location)
}

func projectHash(filePath string) string {
	root := projectRoot(filePath)
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}
	sum := sha256.Sum256([]byte(absRoot))
	return fmt.Sprintf("%x", sum[:])
}

func projectRoot(filePath string) string {
	if filePath == "" {
		return ""
	}
	dir := filepath.Dir(filePath)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Dir(filePath)
		}
		dir = parent
	}
}

func (p *Provider) writeDebugTiming(request completion.Request, duration time.Duration, requestErr error) {
	if !p.debug.Verbose {
		return
	}
	if err := os.MkdirAll(p.debug.Dir, 0o755); err != nil {
		return
	}
	status := "ok"
	message := ""
	if requestErr != nil {
		status = "error"
		message = requestErr.Error()
	}
	line := fmt.Sprintf("%s\tmodel=%s\tstate=%s\tfile=%s\tduration=%s\tstatus=%s\terror=%s\n",
		time.Now().UTC().Format(time.RFC3339Nano), p.model, request.StateID, request.FilePath, duration, status, message)
	file, err := os.OpenFile(filepath.Join(p.debug.Dir, "completion-timings.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.WriteString(line)
}

func postProcessCompletion(request completion.Request, text string) string {
	text = sanitizeCompletion(text)
	text = removeSuffixOverlap(text, request.Suffix)
	return strings.TrimSpace(text)
}

func sanitizeCompletion(text string) string {
	text = strings.TrimSpace(text)
	text = strings.Trim(text, "` \t\r\n")

	for _, prefix := range []string{"go\n", "golang\n", "go\r\n", "golang\r\n"} {
		if strings.HasPrefix(strings.ToLower(text), prefix) {
			text = text[len(prefix):]
			break
		}
	}
	if strings.EqualFold(text, "go") || strings.EqualFold(text, "golang") {
		return ""
	}

	return strings.TrimSpace(text)
}

func removeSuffixOverlap(text string, suffix string) string {
	if text == "" || suffix == "" {
		return text
	}

	limit := len(text)
	if len(suffix) < limit {
		limit = len(suffix)
	}

	for overlap := limit; overlap > 0; overlap-- {
		if strings.HasSuffix(text, suffix[:overlap]) {
			return text[:len(text)-overlap]
		}
	}

	return text
}
