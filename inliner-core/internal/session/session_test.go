package session

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aokalugin/inliner/inliner-core/internal/completion"
	"github.com/aokalugin/inliner/inliner-core/internal/diagnostics"
	"github.com/aokalugin/inliner/inliner-core/internal/protocol"
	"github.com/aokalugin/inliner/inliner-core/internal/telemetry"
)

type recordingProvider struct {
	mu          sync.Mutex
	requests    []completion.Request
	response    completion.Response
	err         error
	diagnostics completion.ProviderDiagnostics
}

type recordingTelemetry struct {
	mu     sync.Mutex
	events []telemetry.Event
}

type recordingDiagnostics struct {
	mu     sync.Mutex
	events []diagnostics.Event
}

func (r *recordingDiagnostics) Publish(event diagnostics.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recordingDiagnostics) Verbose() bool { return false }
func (r *recordingDiagnostics) Close()        {}

func (r *recordingDiagnostics) Events() []diagnostics.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]diagnostics.Event(nil), r.events...)
}

func (r *recordingTelemetry) Record(event telemetry.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recordingTelemetry) Close() {}

func (r *recordingTelemetry) Events() []telemetry.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]telemetry.Event(nil), r.events...)
}

func (p *recordingProvider) Complete(ctx context.Context, request completion.Request) (completion.Response, error) {
	p.mu.Lock()
	p.requests = append(p.requests, request)
	p.mu.Unlock()
	return p.response, p.err
}

func (p *recordingProvider) Requests() []completion.Request {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]completion.Request(nil), p.requests...)
}

func (p *recordingProvider) Diagnostics(ctx context.Context) completion.ProviderDiagnostics {
	select {
	case <-ctx.Done():
		return completion.ProviderDiagnostics{Status: "unreachable", Error: ctx.Err().Error()}
	default:
	}
	if p.diagnostics.Status != "" || p.diagnostics.Error != "" || p.diagnostics.Reachable {
		return p.diagnostics
	}
	return completion.ProviderDiagnostics{Status: "ok", Reachable: true}
}

func TestSessionGreetingSendsConnectionStatus(t *testing.T) {
	provider := &recordingProvider{}
	output := runSession(t, provider, strings.Join([]string{
		`{"kind":"greeting","allowGitignore":false}`,
		`{"kind":"shutdown"}`,
	}, "\n")+"\n")

	messages := decodeOutput(t, output)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1; output=%q", len(messages), output)
	}

	var status protocol.ConnectionStatus
	decodeMessage(t, messages[0], &status)
	if status.Kind != "connection_status" || !status.IsConnected || status.StatusText != nil {
		t.Fatalf("status = %+v, want connected status", status)
	}
}

func TestSessionHealthRequestSendsHealthResponse(t *testing.T) {
	provider := &recordingProvider{diagnostics: completion.ProviderDiagnostics{Status: "ok", Reachable: true}}
	var output bytes.Buffer
	transport := protocol.NewTransport(strings.NewReader(strings.Join([]string{
		`{"kind":"state_update","newId":"1","updates":[{"kind":"file_update","path":"/tmp/main.go","content":"package main\n"}]}`,
		`{"kind":"health_request"}`,
		`{"kind":"shutdown"}`,
	}, "\n")+"\n"), &output)
	sess := New(transport, completion.NewService(provider), Options{
		Provider:          "ollama",
		OllamaBaseURL:     "http://localhost:11434",
		OllamaModel:       "model",
		OllamaTemperature: 0.25,
		OllamaNumPredict:  256,
		RequestTimeout:    5 * time.Second,
		WindowBytes:       4096,
	})

	if err := sess.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	messages := decodeOutput(t, output.String())
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1; output=%q", len(messages), output.String())
	}

	var health protocol.HealthResponse
	decodeMessage(t, messages[0], &health)
	if health.Kind != "health_response" {
		t.Fatalf("Kind = %q, want health_response", health.Kind)
	}
	if health.CoreVersion == "" {
		t.Fatal("CoreVersion is empty")
	}
	if health.Provider != "ollama" || health.OllamaModel != "model" || health.OllamaBaseURL != "http://localhost:11434" {
		t.Fatalf("health provider fields = %+v", health)
	}
	if health.OllamaTemperature != 0.25 || health.OllamaNumPredict != 256 {
		t.Fatalf("health ollama options = %+v", health)
	}
	if health.ProviderStatus != "ok" || !health.ProviderReachable || health.ProviderError != "" {
		t.Fatalf("health provider diagnostics = %+v", health)
	}
	if health.RequestTimeout != "5s" || health.WindowBytes != 4096 {
		t.Fatalf("health request fields = %+v", health)
	}
	if health.OpenDocuments != 1 {
		t.Fatalf("OpenDocuments = %d, want 1", health.OpenDocuments)
	}
	if health.InFlightRequests != 0 {
		t.Fatalf("InFlightRequests = %d, want 0", health.InFlightRequests)
	}
}

func TestSessionStateUpdateCompletesAtCursor(t *testing.T) {
	provider := &recordingProvider{response: completion.Response{Items: []completion.Item{
		{Kind: "text", Text: "println"},
		{Kind: "end"},
	}}}
	output := runStateUpdate(t, provider, protocol.StateUpdate{NewID: "7", Updates: []protocol.Update{
		{Kind: "file_update", Path: "/tmp/main.go", Content: "package main\n\nfunc main() {\n\tprin\n}\n"},
		{Kind: "cursor_update", Path: "/tmp/main.go", Offset: 29},
	}})

	requests := provider.Requests()
	if len(requests) != 1 {
		t.Fatalf("len(requests) = %d, want 1", len(requests))
	}
	req := requests[0]
	if req.StateID != "7" {
		t.Fatalf("StateID = %q, want 7", req.StateID)
	}
	if req.FilePath != "/tmp/main.go" {
		t.Fatalf("FilePath = %q, want /tmp/main.go", req.FilePath)
	}
	if req.Language != "go" {
		t.Fatalf("Language = %q, want go", req.Language)
	}
	if req.Prefix == "" {
		t.Fatal("Prefix is empty, want cursor context")
	}

	messages := decodeOutput(t, output)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1; output=%q", len(messages), output)
	}

	var response protocol.Response
	decodeMessage(t, messages[0], &response)
	if response.Kind != "response" || response.StateID != "7" {
		t.Fatalf("response = %+v, want state response for 7", response)
	}
	if len(response.Items) != 2 || response.Items[0].Text != "println" || response.Items[1].Kind != "end" {
		t.Fatalf("response.Items = %+v, want text println and end", response.Items)
	}
}

func TestSessionUsesConfiguredWindowBytes(t *testing.T) {
	provider := &recordingProvider{response: completion.Response{Items: []completion.Item{{Kind: "end"}}}}
	runStateUpdate(t, provider, protocol.StateUpdate{NewID: "1", Updates: []protocol.Update{
		{Kind: "file_update", Path: "/tmp/main.go", Content: "0123456789"},
		{Kind: "cursor_update", Path: "/tmp/main.go", Offset: 5},
	}}, Options{WindowBytes: 2})

	requests := provider.Requests()
	if len(requests) != 1 {
		t.Fatalf("len(requests) = %d, want 1", len(requests))
	}
	if requests[0].Prefix != "34" {
		t.Fatalf("Prefix = %q, want %q", requests[0].Prefix, "34")
	}
	if requests[0].Suffix != "56" {
		t.Fatalf("Suffix = %q, want %q", requests[0].Suffix, "56")
	}
}

func TestSessionAddsGoPackageContextToCompletionRequest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	pkgDir := filepath.Join(root, "internal", "service")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("MkdirAll package dir: %v", err)
	}
	currentFile := filepath.Join(pkgDir, "service.go")
	content := "package service\n\ntype User struct{}\n\nfunc NewUser() User { return User{} }\n"
	if err := os.WriteFile(currentFile, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile service.go: %v", err)
	}

	provider := &recordingProvider{response: completion.Response{Items: []completion.Item{{Kind: "end"}}}}
	runStateUpdate(t, provider, protocol.StateUpdate{NewID: "1", Updates: []protocol.Update{
		{Kind: "file_update", Path: currentFile, Content: content},
		{Kind: "cursor_update", Path: currentFile, Offset: len(content)},
	}})

	requests := provider.Requests()
	if len(requests) != 1 {
		t.Fatalf("len(requests) = %d, want 1", len(requests))
	}
	if requests[0].Package == nil {
		t.Fatal("Package is nil, want package context")
	}
	if requests[0].Package.PackageName != "service" {
		t.Fatalf("PackageName = %q, want service", requests[0].Package.PackageName)
	}
	if len(requests[0].Package.Types) != 1 || requests[0].Package.Types[0].Name != "User" {
		t.Fatalf("Types = %+v, want User", requests[0].Package.Types)
	}
	if len(requests[0].Package.Functions) != 1 || requests[0].Package.Functions[0].Signature != "NewUser() User" {
		t.Fatalf("Functions = %+v, want NewUser() User", requests[0].Package.Functions)
	}
	if len(requests[0].Package.Files) != 1 || requests[0].Package.Files[0].RelativePath != "internal/service/service.go" {
		t.Fatalf("Files = %+v, want relative project path", requests[0].Package.Files)
	}
}

func TestSessionPackageContextUsesUnsavedBufferContent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	currentFile := filepath.Join(root, "main.go")
	if err := os.WriteFile(currentFile, []byte("package main\n\ntype Saved struct{}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile main.go: %v", err)
	}

	unsaved := "package main\n\ntype Unsaved struct{}\n\nfunc UnsavedFunc() {}\n"
	provider := &recordingProvider{response: completion.Response{Items: []completion.Item{{Kind: "end"}}}}
	runStateUpdate(t, provider, protocol.StateUpdate{NewID: "1", Updates: []protocol.Update{
		{Kind: "file_update", Path: currentFile, Content: unsaved},
		{Kind: "cursor_update", Path: currentFile, Offset: len(unsaved)},
	}})

	requests := provider.Requests()
	if len(requests) != 1 {
		t.Fatalf("len(requests) = %d, want 1", len(requests))
	}
	if requests[0].Package == nil {
		t.Fatal("Package is nil, want package context")
	}
	if len(requests[0].Package.Types) != 1 || requests[0].Package.Types[0].Name != "Unsaved" {
		t.Fatalf("Types = %+v, want Unsaved only", requests[0].Package.Types)
	}
	if len(requests[0].Package.Functions) != 1 || requests[0].Package.Functions[0].Signature != "UnsavedFunc()" {
		t.Fatalf("Functions = %+v, want UnsavedFunc", requests[0].Package.Functions)
	}
}

func TestSessionPackageContextIncludesCurrentFunction(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	currentFile := filepath.Join(root, "main.go")
	content := "package main\n\nfunc main() {\n\tmessage := \"hi\"\n\tfmt.Println(message)\n}\n"
	if err := os.WriteFile(currentFile, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile main.go: %v", err)
	}

	provider := &recordingProvider{response: completion.Response{Items: []completion.Item{{Kind: "end"}}}}
	runStateUpdate(t, provider, protocol.StateUpdate{NewID: "1", Updates: []protocol.Update{
		{Kind: "file_update", Path: currentFile, Content: content},
		{Kind: "cursor_update", Path: currentFile, Offset: strings.Index(content, "Println")},
	}})

	requests := provider.Requests()
	if len(requests) != 1 {
		t.Fatalf("len(requests) = %d, want 1", len(requests))
	}
	if requests[0].Package == nil || requests[0].Package.Current == nil {
		t.Fatalf("Package.Current = %+v, want current function", requests[0].Package)
	}
	if requests[0].Package.Current.Signature != "main()" {
		t.Fatalf("Current.Signature = %q, want main()", requests[0].Package.Current.Signature)
	}
	if len(requests[0].Package.Visible) != 1 || requests[0].Package.Visible[0].Name != "message" {
		t.Fatalf("Package.Visible = %+v, want message", requests[0].Package.Visible)
	}
}

func TestSessionIncludesRecentEditsInCompletionRequest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	currentFile := filepath.Join(root, "service_test.go")
	initial := "package main\n\nfunc TestA(t *testing.T) {\n}\n\nfunc TestB(t *testing.T) {\n}\n"
	updated := "package main\n\nfunc TestA(t *testing.T) {\n\trepo.EXPECT().Find().Return(nil)\n}\n\nfunc TestB(t *testing.T) {\n}\n"
	if err := os.WriteFile(currentFile, []byte(initial), 0o644); err != nil {
		t.Fatalf("WriteFile service_test.go: %v", err)
	}

	provider := &recordingProvider{response: completion.Response{Items: []completion.Item{{Kind: "end"}}}}
	var output bytes.Buffer
	transport := protocol.NewTransport(strings.NewReader(strings.Join([]string{
		mustJSON(t, protocol.StateUpdate{Kind: "state_update", NewID: "1", Updates: []protocol.Update{{Kind: "file_update", Path: currentFile, Content: initial}}}),
		mustJSON(t, protocol.StateUpdate{Kind: "state_update", NewID: "2", Updates: []protocol.Update{{Kind: "file_update", Path: currentFile, Content: updated}}}),
		mustJSON(t, protocol.StateUpdate{Kind: "state_update", NewID: "3", Updates: []protocol.Update{{Kind: "cursor_update", Path: currentFile, Offset: strings.Index(updated, "func TestB") + len("func TestB(t *testing.T) {\n")}}}),
		`{"kind":"shutdown"}`,
	}, "\n")+"\n"), &output)
	sess := New(transport, completion.NewService(provider))
	if err := sess.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	requests := provider.Requests()
	if len(requests) != 1 {
		t.Fatalf("len(requests) = %d, want 1", len(requests))
	}
	if len(requests[0].RecentEdits) != 1 {
		t.Fatalf("RecentEdits = %+v, want one edit", requests[0].RecentEdits)
	}
	if !strings.Contains(requests[0].RecentEdits[0].After, "repo.EXPECT().Find().Return(nil)") {
		t.Fatalf("RecentEdits[0].After = %q, want mock call", requests[0].RecentEdits[0].After)
	}
}

func TestSessionRecordsCompletionTelemetry(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	currentFile := filepath.Join(root, "main.go")
	content := "package main\n\nconst defaultLimit = 10\n\nfunc main() {\n\tdefaultLimit\n}\n"
	if err := os.WriteFile(currentFile, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile main.go: %v", err)
	}

	recorder := &recordingTelemetry{}
	provider := &recordingProvider{response: completion.Response{Items: []completion.Item{{Kind: "text", Text: "fmt.Println(defaultLimit)"}, {Kind: "end"}}}}
	output := runStateUpdate(t, provider, protocol.StateUpdate{NewID: "telemetry-1", Updates: []protocol.Update{
		{Kind: "file_update", Path: currentFile, Content: content},
		{Kind: "cursor_update", Path: currentFile, Offset: strings.Index(content, "\tdefaultLimit") + 1},
	}}, Options{Provider: "fake", OllamaModel: "test-model", TelemetryRecorder: recorder})

	if len(decodeOutput(t, output)) == 0 {
		t.Fatal("output is empty, want completion response")
	}
	events := recorder.Events()
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	event := events[0]
	if event.Kind != "completion_request" || event.StateID != "telemetry-1" || event.Status != "ok" {
		t.Fatalf("event = %+v, want ok completion_request", event)
	}
	if event.Provider != "fake" || event.Model != "test-model" {
		t.Fatalf("provider/model = %q/%q", event.Provider, event.Model)
	}
	if event.FileHash == "" || event.ProjectHash == "" || strings.Contains(event.FileHash, currentFile) {
		t.Fatalf("hashes = file:%q project:%q", event.FileHash, event.ProjectHash)
	}
	if event.PackageValues != 1 || event.PackageFunctions != 1 || event.CompletionTextBytes != len("fmt.Println(defaultLimit)") {
		t.Fatalf("event counts = %+v", event)
	}
	if event.PrefixBytes <= 0 || event.SuffixBytes <= 0 {
		t.Fatalf("prefix/suffix bytes = %d/%d", event.PrefixBytes, event.SuffixBytes)
	}
}

func TestSessionPublishesCompletionDiagnostics(t *testing.T) {
	recorder := &recordingDiagnostics{}
	provider := &recordingProvider{response: completion.Response{Items: []completion.Item{{Kind: "text", Text: "fmt.Println(name)"}, {Kind: "end"}}}}
	output := runStateUpdate(t, provider, protocol.StateUpdate{NewID: "diagnostic-1", Updates: []protocol.Update{
		{Kind: "file_update", Path: "/tmp/main.go", Content: "package main\n"},
		{Kind: "cursor_update", Path: "/tmp/main.go", Offset: len("package main\n")},
	}}, Options{Provider: "fake", Diagnostics: recorder})

	if len(decodeOutput(t, output)) == 0 {
		t.Fatal("output is empty, want completion response")
	}
	events := recorder.Events()
	if len(events) != 2 {
		t.Fatalf("events = %+v, want request start and result", events)
	}
	if events[0].Kind != diagnostics.KindRequestStarted || events[0].RequestID != "diagnostic-1" || events[0].FilePath != "/tmp/main.go" {
		t.Fatalf("start event = %+v", events[0])
	}
	if events[1].Kind != diagnostics.KindResult || events[1].Status != diagnostics.StatusOK || events[1].Empty {
		t.Fatalf("result event = %+v", events[1])
	}
}

func TestCompletionDiagnosticStatuses(t *testing.T) {
	tests := []struct {
		name string
		life lifecycle
		want diagnostics.Status
	}{
		{name: "ok", life: lifecycle{completionTextBytes: 1}, want: diagnostics.StatusOK},
		{name: "error", life: lifecycle{status: "error", err: "failed"}, want: diagnostics.StatusError},
		{name: "cancelled", life: lifecycle{cancelled: true}, want: diagnostics.StatusCancelled},
		{name: "stale", life: lifecycle{stale: true}, want: diagnostics.StatusStale},
		{name: "suppressed", life: lifecycle{suppressed: true}, want: diagnostics.StatusSuppressed},
		{name: "cached", life: lifecycle{cached: true, completionTextBytes: 1}, want: diagnostics.StatusCached},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := &recordingDiagnostics{}
			sess := &Session{diagnostics: recorder}
			sess.recordDiagnostics(completion.Request{StateID: "1", Timings: &completion.RequestTimings{}}, test.life)
			events := recorder.Events()
			if len(events) != 1 || events[0].Status != test.want {
				t.Fatalf("events = %+v, want status %q", events, test.want)
			}
		})
	}
}

func TestSessionCursorUpdateWithoutFileSendsError(t *testing.T) {
	provider := &recordingProvider{}
	output := runSession(t, provider, strings.Join([]string{
		`{"kind":"state_update","newId":"1","updates":[{"kind":"cursor_update","path":"/tmp/main.go","offset":0}]}`,
		`{"kind":"shutdown"}`,
	}, "\n")+"\n")

	if requests := provider.Requests(); len(requests) != 0 {
		t.Fatalf("len(requests) = %d, want 0", len(requests))
	}

	messages := decodeOutput(t, output)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1; output=%q", len(messages), output)
	}

	var msg protocol.Error
	decodeMessage(t, messages[0], &msg)
	if msg.Kind != "error" || !strings.Contains(msg.Message, "no document content") {
		t.Fatalf("error = %+v, want no document content error", msg)
	}
}

func TestSessionUnknownMessageSendsErrorAndContinues(t *testing.T) {
	provider := &recordingProvider{}
	output := runSession(t, provider, strings.Join([]string{
		`{"kind":"unknown"}`,
		`{"kind":"greeting","allowGitignore":false}`,
		`{"kind":"shutdown"}`,
	}, "\n")+"\n")

	messages := decodeOutput(t, output)
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2; output=%q", len(messages), output)
	}

	var errMsg protocol.Error
	decodeMessage(t, messages[0], &errMsg)
	if errMsg.Kind != "error" || !strings.Contains(errMsg.Message, "unknown message kind") {
		t.Fatalf("first message = %+v, want unknown kind error", errMsg)
	}

	var status protocol.ConnectionStatus
	decodeMessage(t, messages[1], &status)
	if status.Kind != "connection_status" || !status.IsConnected {
		t.Fatalf("second message = %+v, want connected status", status)
	}
}

func TestSessionAcceptUpdateIsAccepted(t *testing.T) {
	provider := &recordingProvider{}
	output := runSession(t, provider, strings.Join([]string{
		`{"kind":"accept_update","stateId":"7","path":"/tmp/main.go","text":"fmt.Println(name)"}`,
		`{"kind":"shutdown"}`,
	}, "\n")+"\n")

	if output != "" {
		t.Fatalf("output = %q, want no response for accept_update", output)
	}
}

func TestSessionAcceptUpdateRequiresStateID(t *testing.T) {
	provider := &recordingProvider{}
	output := runSession(t, provider, strings.Join([]string{
		`{"kind":"accept_update","path":"/tmp/main.go","text":"fmt.Println(name)"}`,
		`{"kind":"shutdown"}`,
	}, "\n")+"\n")

	messages := decodeOutput(t, output)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1; output=%q", len(messages), output)
	}

	var msg protocol.Error
	decodeMessage(t, messages[0], &msg)
	if msg.Kind != "error" || !strings.Contains(msg.Message, "stateId is required") {
		t.Fatalf("error = %+v, want stateId validation error", msg)
	}
}

func TestSessionAcceptUpdateRequiresPath(t *testing.T) {
	provider := &recordingProvider{}
	output := runSession(t, provider, strings.Join([]string{
		`{"kind":"accept_update","stateId":"7","text":"fmt.Println(name)"}`,
		`{"kind":"shutdown"}`,
	}, "\n")+"\n")

	messages := decodeOutput(t, output)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1; output=%q", len(messages), output)
	}

	var msg protocol.Error
	decodeMessage(t, messages[0], &msg)
	if msg.Kind != "error" || !strings.Contains(msg.Message, "path is required") {
		t.Fatalf("error = %+v, want path validation error", msg)
	}
}

func TestSessionDismissUpdateIsAccepted(t *testing.T) {
	provider := &recordingProvider{}
	output := runSession(t, provider, strings.Join([]string{
		`{"kind":"dismiss_update","stateId":"7","path":"/tmp/main.go","text":"fmt.Println(name)"}`,
		`{"kind":"shutdown"}`,
	}, "\n")+"\n")

	if output != "" {
		t.Fatalf("output = %q, want no response for dismiss_update", output)
	}
}

func TestSessionDismissUpdateRequiresStateID(t *testing.T) {
	provider := &recordingProvider{}
	output := runSession(t, provider, strings.Join([]string{
		`{"kind":"dismiss_update","path":"/tmp/main.go","text":"fmt.Println(name)"}`,
		`{"kind":"shutdown"}`,
	}, "\n")+"\n")

	messages := decodeOutput(t, output)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1; output=%q", len(messages), output)
	}

	var msg protocol.Error
	decodeMessage(t, messages[0], &msg)
	if msg.Kind != "error" || !strings.Contains(msg.Message, "stateId is required") {
		t.Fatalf("error = %+v, want stateId validation error", msg)
	}
}

func TestSessionDismissUpdateRequiresPath(t *testing.T) {
	provider := &recordingProvider{}
	output := runSession(t, provider, strings.Join([]string{
		`{"kind":"dismiss_update","stateId":"7","text":"fmt.Println(name)"}`,
		`{"kind":"shutdown"}`,
	}, "\n")+"\n")

	messages := decodeOutput(t, output)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1; output=%q", len(messages), output)
	}

	var msg protocol.Error
	decodeMessage(t, messages[0], &msg)
	if msg.Kind != "error" || !strings.Contains(msg.Message, "path is required") {
		t.Fatalf("error = %+v, want path validation error", msg)
	}
}

func TestSessionAcceptedCompletionIsServedFromCache(t *testing.T) {
	provider := &recordingProvider{response: completion.Response{Items: []completion.Item{
		{Kind: "text", Text: "provider completion"},
		{Kind: "end"},
	}}}
	var output bytes.Buffer
	sess := New(
		protocol.NewTransport(strings.NewReader(""), &output),
		completion.NewService(provider),
	)
	update := protocol.StateUpdate{Updates: []protocol.Update{
		{Kind: "file_update", Path: "/tmp/main.go", Content: "package main\n\nfunc main() {\n\tfmt.\n}\n"},
		{Kind: "cursor_update", Path: "/tmp/main.go", Offset: 31},
	}}

	update.NewID = "1"
	if err := sess.handleStateUpdate(context.Background(), update); err != nil {
		t.Fatalf("first handleStateUpdate returned error: %v", err)
	}
	sess.workers.Wait()
	if requests := provider.Requests(); len(requests) != 1 {
		t.Fatalf("len(requests) = %d, want 1", len(requests))
	}

	if err := sess.handleAcceptUpdate(protocol.AcceptUpdate{StateID: "1", Path: "/tmp/main.go", Text: "Println(name)"}); err != nil {
		t.Fatalf("handleAcceptUpdate returned error: %v", err)
	}
	output.Reset()

	update.NewID = "2"
	if err := sess.handleStateUpdate(context.Background(), update); err != nil {
		t.Fatalf("second handleStateUpdate returned error: %v", err)
	}
	sess.workers.Wait()

	if requests := provider.Requests(); len(requests) != 1 {
		t.Fatalf("len(requests) = %d, want provider bypassed after cache hit", len(requests))
	}

	messages := decodeOutput(t, output.String())
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1; output=%q", len(messages), output.String())
	}

	var response protocol.Response
	decodeMessage(t, messages[0], &response)
	if response.StateID != "2" {
		t.Fatalf("StateID = %q, want 2", response.StateID)
	}
	if len(response.Items) != 2 || response.Items[0].Text != "Println(name)" || response.Items[1].Kind != "end" {
		t.Fatalf("Items = %+v, want cached accepted completion", response.Items)
	}
}

func TestSessionDismissedCompletionIsSuppressed(t *testing.T) {
	provider := &recordingProvider{response: completion.Response{Items: []completion.Item{
		{Kind: "text", Text: "Println(name)"},
		{Kind: "end"},
	}}}
	var output bytes.Buffer
	sess := New(
		protocol.NewTransport(strings.NewReader(""), &output),
		completion.NewService(provider),
	)
	update := protocol.StateUpdate{Updates: []protocol.Update{
		{Kind: "file_update", Path: "/tmp/main.go", Content: "package main\n\nfunc main() {\n\tfmt.\n}\n"},
		{Kind: "cursor_update", Path: "/tmp/main.go", Offset: 31},
	}}

	update.NewID = "1"
	if err := sess.handleStateUpdate(context.Background(), update); err != nil {
		t.Fatalf("first handleStateUpdate returned error: %v", err)
	}
	sess.workers.Wait()
	if err := sess.handleDismissUpdate(protocol.DismissUpdate{StateID: "1", Path: "/tmp/main.go", Text: "Println(name)"}); err != nil {
		t.Fatalf("handleDismissUpdate returned error: %v", err)
	}
	output.Reset()

	update.NewID = "2"
	if err := sess.handleStateUpdate(context.Background(), update); err != nil {
		t.Fatalf("second handleStateUpdate returned error: %v", err)
	}
	sess.workers.Wait()

	if requests := provider.Requests(); len(requests) != 2 {
		t.Fatalf("len(requests) = %d, want provider called again before suppression", len(requests))
	}

	messages := decodeOutput(t, output.String())
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1; output=%q", len(messages), output.String())
	}

	var response protocol.Response
	decodeMessage(t, messages[0], &response)
	if response.StateID != "2" {
		t.Fatalf("StateID = %q, want 2", response.StateID)
	}
	if len(response.Items) != 1 || response.Items[0].Kind != "end" {
		t.Fatalf("Items = %+v, want suppressed end response", response.Items)
	}
}

func TestSessionCancelsPreviousCompletionForSameFile(t *testing.T) {
	provider := &controlledProvider{
		started:    make(chan completion.Request, 2),
		releaseOld: make(chan struct{}),
	}
	var output bytes.Buffer
	sess := New(
		protocol.NewTransport(strings.NewReader(""), &output),
		completion.NewService(provider),
	)
	ctx := context.Background()

	if err := sess.handleStateUpdate(ctx, protocol.StateUpdate{NewID: "1", Updates: []protocol.Update{
		{Kind: "file_update", Path: "/tmp/main.go", Content: "package main\n"},
		{Kind: "cursor_update", Path: "/tmp/main.go", Offset: 5},
	}}); err != nil {
		t.Fatalf("first handleStateUpdate returned error: %v", err)
	}
	waitStarted(t, provider.started, "1")

	if err := sess.handleStateUpdate(ctx, protocol.StateUpdate{NewID: "2", Updates: []protocol.Update{
		{Kind: "cursor_update", Path: "/tmp/main.go", Offset: 6},
	}}); err != nil {
		t.Fatalf("second handleStateUpdate returned error: %v", err)
	}
	waitStarted(t, provider.started, "2")

	close(provider.releaseOld)
	sess.workers.Wait()

	messages := decodeOutput(t, output.String())
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want only latest response; output=%q", len(messages), output.String())
	}

	var response protocol.Response
	decodeMessage(t, messages[0], &response)
	if response.StateID != "2" {
		t.Fatalf("StateID = %q, want latest state 2", response.StateID)
	}
	if len(response.Items) == 0 || response.Items[0].Text != "new" {
		t.Fatalf("Items = %+v, want new completion", response.Items)
	}
}

func TestSessionFileUpdateCancelsInFlightCompletion(t *testing.T) {
	provider := &controlledProvider{
		started:    make(chan completion.Request, 1),
		releaseOld: make(chan struct{}),
	}
	var output bytes.Buffer
	sess := New(
		protocol.NewTransport(strings.NewReader(""), &output),
		completion.NewService(provider),
	)

	if err := sess.handleStateUpdate(context.Background(), protocol.StateUpdate{NewID: "1", Updates: []protocol.Update{
		{Kind: "file_update", Path: "/tmp/main.go", Content: "package main\n"},
		{Kind: "cursor_update", Path: "/tmp/main.go", Offset: 5},
	}}); err != nil {
		t.Fatalf("handleStateUpdate returned error: %v", err)
	}
	waitStarted(t, provider.started, "1")

	if err := sess.handleStateUpdate(context.Background(), protocol.StateUpdate{NewID: "2", Updates: []protocol.Update{
		{Kind: "file_update", Path: "/tmp/main.go", Content: "package main\n\n"},
	}}); err != nil {
		t.Fatalf("file update returned error: %v", err)
	}

	close(provider.releaseOld)
	sess.workers.Wait()

	if output.String() != "" {
		t.Fatalf("output = %q, want stale completion suppressed", output.String())
	}
}

type controlledProvider struct {
	started    chan completion.Request
	releaseOld chan struct{}
}

func (p *controlledProvider) Complete(ctx context.Context, request completion.Request) (completion.Response, error) {
	p.started <- request
	if request.StateID == "1" {
		<-p.releaseOld
		return completion.Response{Items: []completion.Item{{Kind: "text", Text: "old"}, {Kind: "end"}}}, nil
	}
	return completion.Response{Items: []completion.Item{{Kind: "text", Text: "new"}, {Kind: "end"}}}, nil
}

func waitStarted(t *testing.T, started <-chan completion.Request, stateID string) {
	t.Helper()

	select {
	case request := <-started:
		if request.StateID != stateID {
			t.Fatalf("started request StateID = %q, want %q", request.StateID, stateID)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for request %s to start", stateID)
	}
}

func runSession(t *testing.T, provider completion.Provider, input string) string {
	t.Helper()

	var output bytes.Buffer
	transport := protocol.NewTransport(strings.NewReader(input), &output)
	sess := New(transport, completion.NewService(provider))

	if err := sess.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	return output.String()
}

func runStateUpdate(t *testing.T, provider completion.Provider, update protocol.StateUpdate, options ...Options) string {
	t.Helper()

	var output bytes.Buffer
	transport := protocol.NewTransport(strings.NewReader(""), &output)
	sess := New(transport, completion.NewService(provider), options...)

	if err := sess.handleStateUpdate(context.Background(), update); err != nil {
		t.Fatalf("handleStateUpdate returned error: %v", err)
	}
	sess.workers.Wait()

	return output.String()
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	return string(data)
}

func decodeOutput(t *testing.T, output string) []json.RawMessage {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}

	messages := make([]json.RawMessage, 0, len(lines))
	for _, line := range lines {
		if !strings.HasPrefix(line, protocol.MessagePrefix) {
			t.Fatalf("line %q does not have prefix %q", line, protocol.MessagePrefix)
		}
		messages = append(messages, json.RawMessage(strings.TrimPrefix(line, protocol.MessagePrefix)))
	}

	return messages
}

func decodeMessage(t *testing.T, raw json.RawMessage, out any) {
	t.Helper()

	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("failed to decode message %s: %v", raw, err)
	}
}
