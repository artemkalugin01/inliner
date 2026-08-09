package session

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aokalugin/inliner/inliner-core/internal/completion"
	"github.com/aokalugin/inliner/inliner-core/internal/document"
	"github.com/aokalugin/inliner/inliner-core/internal/edithistory"
	"github.com/aokalugin/inliner/inliner-core/internal/gocontext"
	"github.com/aokalugin/inliner/inliner-core/internal/protocol"
	"github.com/aokalugin/inliner/inliner-core/internal/telemetry"
	"github.com/aokalugin/inliner/inliner-core/internal/version"
)

type Session struct {
	transport *protocol.Transport
	complete  *completion.Service
	documents *document.Store
	history   edithistory.Provider
	collector *gocontext.Collector
	telemetry telemetry.Recorder
	options   Options

	mu        sync.Mutex
	latest    map[string]string
	cancels   map[string]context.CancelFunc
	requests  map[string]completion.Request
	cache     *completion.AcceptanceCache
	dismissed *completion.DismissalCache
	workers   sync.WaitGroup
}

type Options struct {
	Provider          string
	OllamaBaseURL     string
	OllamaModel       string
	OllamaTemperature float64
	OllamaNumPredict  int
	TelemetryEnabled  bool
	TelemetryDir      string
	TelemetryRecorder telemetry.Recorder
	RequestTimeout    time.Duration
	WindowBytes       int
}

type lifecycle struct {
	receivedAt          time.Time
	startedAt           time.Time
	contextCollection   time.Duration
	recentEditSelection time.Duration
	requestBuild        time.Duration
	provider            time.Duration
	responseWrite       time.Duration
	completionItems     int
	completionTextBytes int
	cached              bool
	suppressed          bool
	stale               bool
	cancelled           bool
	status              string
	err                 string
}

func New(transport *protocol.Transport, complete *completion.Service, options ...Options) *Session {
	resolved := Options{WindowBytes: document.DefaultWindowBytes}
	if len(options) > 0 {
		resolved = options[0]
	}
	if resolved.WindowBytes <= 0 {
		resolved.WindowBytes = document.DefaultWindowBytes
	}

	recorder := resolved.TelemetryRecorder
	if recorder == nil {
		recorder = telemetry.NoopRecorder{}
		if resolved.TelemetryEnabled {
			if created, err := telemetry.NewAsyncRecorder(resolved.TelemetryDir); err == nil {
				recorder = created
			}
		}
	}

	return &Session{
		transport: transport,
		complete:  complete,
		documents: document.NewStore(),
		history:   edithistory.NewMemoryProvider(edithistory.MemoryOptions{}),
		collector: gocontext.NewCollector(),
		telemetry: recorder,
		options:   resolved,
		latest:    make(map[string]string),
		cancels:   make(map[string]context.CancelFunc),
		requests:  make(map[string]completion.Request),
		cache:     completion.NewAcceptanceCache(completion.CacheOptions{}),
		dismissed: completion.NewDismissalCache(completion.CacheOptions{}),
	}
}

func (s *Session) Run(ctx context.Context) error {
	defer s.telemetry.Close()
	for {
		msg, err := s.transport.Read()
		receivedAt := time.Now()
		if errors.Is(err, io.EOF) {
			s.cancelAll()
			s.workers.Wait()
			return nil
		}
		if err != nil {
			if sendErr := s.sendError(fmt.Sprintf("failed to read input: %v", err)); sendErr != nil {
				return sendErr
			}
			continue
		}

		if err := s.handle(ctx, msg, receivedAt); err != nil {
			if errors.Is(err, io.EOF) {
				s.cancelAll()
				s.workers.Wait()
				return nil
			}
			if sendErr := s.sendError(err.Error()); sendErr != nil {
				return sendErr
			}
		}
	}
}

func (s *Session) handle(ctx context.Context, msg protocol.RawMessage, receivedAt time.Time) error {
	switch msg.Kind {
	case "greeting":
		var greeting protocol.Greeting
		if err := json.Unmarshal(msg.Raw, &greeting); err != nil {
			return err
		}
		return s.transport.Send(protocol.ConnectionStatus{Kind: "connection_status", IsConnected: true})
	case "state_update":
		var update protocol.StateUpdate
		if err := json.Unmarshal(msg.Raw, &update); err != nil {
			return err
		}
		return s.handleStateUpdate(ctx, update, receivedAt)
	case "health_request":
		var request protocol.HealthRequest
		if err := json.Unmarshal(msg.Raw, &request); err != nil {
			return err
		}
		return s.sendHealthResponse(ctx)
	case "accept_update":
		var update protocol.AcceptUpdate
		if err := json.Unmarshal(msg.Raw, &update); err != nil {
			return err
		}
		return s.handleAcceptUpdate(update)
	case "dismiss_update":
		var update protocol.DismissUpdate
		if err := json.Unmarshal(msg.Raw, &update); err != nil {
			return err
		}
		return s.handleDismissUpdate(update)
	case "shutdown":
		return io.EOF
	default:
		return fmt.Errorf("unknown message kind %q", msg.Kind)
	}
}

func (s *Session) sendHealthResponse(ctx context.Context) error {
	diagnosticCtx := ctx
	var cancel context.CancelFunc
	if s.options.RequestTimeout > 0 {
		diagnosticCtx, cancel = context.WithTimeout(ctx, s.options.RequestTimeout)
		defer cancel()
	}
	diagnostics := s.complete.Diagnostics(diagnosticCtx)

	return s.transport.Send(protocol.HealthResponse{
		Kind:              "health_response",
		CoreVersion:       version.Core,
		Provider:          s.options.Provider,
		OllamaBaseURL:     s.options.OllamaBaseURL,
		OllamaModel:       s.options.OllamaModel,
		OllamaTemperature: s.options.OllamaTemperature,
		OllamaNumPredict:  s.options.OllamaNumPredict,
		ProviderStatus:    diagnostics.Status,
		ProviderReachable: diagnostics.Reachable,
		ProviderError:     diagnostics.Error,
		RequestTimeout:    s.options.RequestTimeout.String(),
		WindowBytes:       s.options.WindowBytes,
		OpenDocuments:     s.documents.Len(),
		InFlightRequests:  s.inFlightRequests(),
	})
}

func (s *Session) inFlightRequests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cancels)
}

func (s *Session) handleDismissUpdate(update protocol.DismissUpdate) error {
	if update.StateID == "" {
		return fmt.Errorf("dismiss_update stateId is required")
	}
	if update.Path == "" {
		return fmt.Errorf("dismiss_update path is required")
	}

	request, ok := s.requestForState(update.StateID)
	if ok && request.FilePath == update.Path {
		s.dismissed.Store(request, update.Text)
	}
	return nil
}

func (s *Session) handleAcceptUpdate(update protocol.AcceptUpdate) error {
	if update.StateID == "" {
		return fmt.Errorf("accept_update stateId is required")
	}
	if update.Path == "" {
		return fmt.Errorf("accept_update path is required")
	}

	request, ok := s.requestForState(update.StateID)
	if ok && request.FilePath == update.Path {
		s.cache.Store(request, update.Text)
	}
	return nil
}

func (s *Session) handleStateUpdate(ctx context.Context, update protocol.StateUpdate, receivedAt ...time.Time) error {
	received := time.Now()
	if len(receivedAt) > 0 && !receivedAt[0].IsZero() {
		received = receivedAt[0]
	}
	for _, item := range update.Updates {
		switch item.Kind {
		case "file_update":
			oldContent, hadOldContent := s.documents.Get(item.Path)
			if hadOldContent {
				s.history.ObserveFileUpdate(item.Path, oldContent, item.Content)
			}
			s.documents.Set(item.Path, item.Content)
			s.cancelPath(item.Path)
		case "cursor_update":
			if err := s.completeAtCursor(ctx, update.NewID, item.Path, item.Offset, received); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown state update kind %q", item.Kind)
		}
	}

	return nil
}

func (s *Session) completeAtCursor(ctx context.Context, stateID string, path string, offset int, receivedAt time.Time) error {
	life := lifecycle{receivedAt: receivedAt, startedAt: time.Now(), status: "started"}
	requestStarted := time.Now()
	content, ok := s.documents.Get(path)
	if !ok {
		return fmt.Errorf("no document content for %q", path)
	}

	window := document.AroundCursor(content, offset, s.options.WindowBytes)
	cursorLine := lineNumber(content, offset)
	s.history.ObserveCursor(path, cursorLine)
	request := completion.Request{
		StateID:  stateID,
		FilePath: path,
		Language: "go",
		Prefix:   window.Prefix,
		Suffix:   window.Suffix,
		Timings:  &completion.RequestTimings{},
	}
	contextStarted := time.Now()
	if pkg, ok := s.collectPackageContext(path, content, offset); ok {
		request.Package = &pkg
	}
	life.contextCollection = time.Since(contextStarted)
	recentStarted := time.Now()
	request.RecentEdits = s.recentEdits(path, cursorLine, request)
	life.recentEditSelection = time.Since(recentStarted)
	life.requestBuild = time.Since(requestStarted)
	s.rememberRequest(request)

	if text, ok := s.cache.Lookup(request); ok {
		s.replaceActiveRequest(path, stateID, nil)
		if s.isLatest(path, stateID) {
			life.cached = true
			if s.dismissed.IsDismissed(request, text) {
				life.suppressed = true
				return s.sendCompletionResponseWithTelemetry(request, []completion.Item{{Kind: "end"}}, life, nil)
			}
			return s.sendCompletionResponseWithTelemetry(request, []completion.Item{{Kind: "text", Text: text}, {Kind: "end"}}, life, nil)
		}
		life.stale = true
		s.recordTelemetry(request, life)
		return nil
	}

	completionCtx, cancel := context.WithCancel(ctx)
	s.replaceActiveRequest(path, stateID, cancel)

	s.workers.Add(1)
	go s.runCompletion(completionCtx, request, life)

	return nil
}

func (s *Session) recentEdits(path string, cursorLine int, request completion.Request) []completion.RecentEdit {
	projectRoot := detectProjectRoot(path)
	var visible []string
	currentFunction := ""
	if request.Package != nil {
		for _, identifier := range request.Package.Visible {
			visible = append(visible, identifier.Name, identifier.Type)
		}
		if request.Package.Current != nil {
			currentFunction = request.Package.Current.Signature
		}
	}
	edits := s.history.Relevant(edithistory.Query{
		FilePath:           path,
		ProjectRoot:        projectRoot,
		CursorLine:         cursorLine,
		Prefix:             request.Prefix,
		Suffix:             request.Suffix,
		VisibleIdentifiers: visible,
		CurrentFunction:    currentFunction,
	})
	result := make([]completion.RecentEdit, 0, len(edits))
	for _, edit := range edits {
		result = append(result, completion.RecentEdit{RelativePath: edit.RelativePath, StartLine: edit.StartLine, EndLine: edit.EndLine, Before: edit.Before, After: edit.After})
	}
	return result
}

func lineNumber(content string, offset int) int {
	if offset < 0 {
		offset = 0
	}
	if offset > len(content) {
		offset = len(content)
	}
	return strings.Count(content[:offset], "\n") + 1
}

func (s *Session) collectPackageContext(path string, content string, offset int) (gocontext.PackageContext, bool) {
	if filepath.Ext(path) != ".go" {
		return gocontext.PackageContext{}, false
	}

	projectRoot := detectProjectRoot(path)
	pkg, err := s.collector.CollectWithOverlayAt(path, projectRoot, content, offset)
	if err != nil {
		return gocontext.PackageContext{}, false
	}
	return pkg, true
}

func detectProjectRoot(path string) string {
	dir := filepath.Dir(path)
	for {
		if exists(filepath.Join(dir, "go.mod")) || exists(filepath.Join(dir, ".git")) {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Dir(path)
		}
		dir = parent
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (s *Session) runCompletion(ctx context.Context, request completion.Request, life lifecycle) {
	defer s.workers.Done()

	providerStarted := time.Now()
	response, err := s.complete.Complete(ctx, request)
	life.provider = time.Since(providerStarted)
	if err != nil {
		if ctx.Err() != nil || !s.isLatest(request.FilePath, request.StateID) {
			life.cancelled = ctx.Err() != nil
			life.stale = ctx.Err() == nil
			life.status = "cancelled"
			if life.stale {
				life.status = "stale"
			}
			s.recordTelemetry(request, life)
			return
		}
		life.status = "error"
		life.err = err.Error()
		s.recordTelemetry(request, life)
		_ = s.sendError(err.Error())
		return
	}
	if ctx.Err() != nil || !s.isLatest(request.FilePath, request.StateID) {
		life.cancelled = ctx.Err() != nil
		life.stale = ctx.Err() == nil
		life.status = "cancelled"
		if life.stale {
			life.status = "stale"
		}
		s.recordTelemetry(request, life)
		return
	}
	if s.shouldSuppressResponse(request, response.Items) {
		life.suppressed = true
		_ = s.sendCompletionResponseWithTelemetry(request, []completion.Item{{Kind: "end"}}, life, nil)
		return
	}

	_ = s.sendCompletionResponseWithTelemetry(request, response.Items, life, nil)
}

func (s *Session) shouldSuppressResponse(request completion.Request, items []completion.Item) bool {
	text := completionText(items)
	return s.dismissed.IsDismissed(request, text)
}

func completionText(items []completion.Item) string {
	var text string
	for _, item := range items {
		if item.Kind == "text" {
			text += item.Text
			continue
		}
		if item.Kind == "end" {
			break
		}
	}
	return text
}

func (s *Session) sendCompletionResponse(stateID string, completionItems []completion.Item) error {
	items := make([]protocol.ResponseItem, 0, len(completionItems))
	for _, item := range completionItems {
		items = append(items, protocol.ResponseItem{Kind: item.Kind, Text: item.Text, Verify: item.Verify})
	}

	return s.transport.Send(protocol.Response{Kind: "response", StateID: stateID, Items: items})
}

func (s *Session) sendCompletionResponseWithTelemetry(request completion.Request, items []completion.Item, life lifecycle, err error) error {
	started := time.Now()
	sendErr := s.sendCompletionResponse(request.StateID, items)
	life.responseWrite = time.Since(started)
	if err != nil {
		life.err = err.Error()
	}
	if sendErr != nil {
		life.err = sendErr.Error()
		life.status = "error"
	}
	life.completionItems = len(items)
	life.completionTextBytes = len(completionText(items))
	s.recordTelemetry(request, life)
	return sendErr
}

func (s *Session) recordTelemetry(request completion.Request, life lifecycle) {
	if life.status == "" || life.status == "started" {
		life.status = "ok"
	}
	if life.startedAt.IsZero() {
		life.startedAt = time.Now()
	}
	if life.receivedAt.IsZero() {
		life.receivedAt = life.startedAt
	}

	event := telemetry.Event{
		Kind:                  "completion_request",
		Timestamp:             time.Now().UTC().Format(time.RFC3339Nano),
		StateID:               request.StateID,
		Language:              request.Language,
		Provider:              s.options.Provider,
		Model:                 s.options.OllamaModel,
		Status:                life.status,
		Error:                 truncateError(life.err),
		ProjectHash:           hashString(detectProjectRoot(request.FilePath)),
		FileHash:              hashString(request.FilePath),
		PrefixBytes:           len(request.Prefix),
		SuffixBytes:           len(request.Suffix),
		RecentEdits:           len(request.RecentEdits),
		CompletionItems:       life.completionItems,
		CompletionTextBytes:   life.completionTextBytes,
		Cached:                life.cached,
		Suppressed:            life.suppressed,
		Stale:                 life.stale,
		Cancelled:             life.cancelled,
		CoreReceiveToStartMs:  life.startedAt.Sub(life.receivedAt).Milliseconds(),
		ContextCollectionMs:   life.contextCollection.Milliseconds(),
		RecentEditSelectionMs: life.recentEditSelection.Milliseconds(),
		RequestBuildMs:        life.requestBuild.Milliseconds(),
		PromptBuildMs:         request.Timings.PromptBuildMs(),
		ProviderMs:            life.provider.Milliseconds(),
		ResponseWriteMs:       life.responseWrite.Milliseconds(),
		TotalCoreMs:           time.Since(life.receivedAt).Milliseconds(),
	}
	if request.Package != nil {
		event.PackageFiles = len(request.Package.Files)
		event.PackageImports = len(request.Package.Imports)
		event.PackageValues = len(request.Package.Values)
		event.PackageTypes = len(request.Package.Types)
		event.PackageInterfaces = len(request.Package.Interfaces)
		event.PackageFunctions = len(request.Package.Functions)
		event.CurrentDeclaration = request.Package.Declaration != nil
		event.VisibleIdentifiers = len(request.Package.Visible)
		event.SiblingMethods = len(request.Package.Siblings)
	}
	s.telemetry.Record(event)
}

func truncateError(value string) string {
	const maxErrorBytes = 300
	if len(value) <= maxErrorBytes {
		return value
	}
	return value[:maxErrorBytes]
}

func hashString(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:])
}

func (s *Session) sendError(message string) error {
	return s.transport.Send(protocol.Error{Kind: "error", Message: message})
}

func (s *Session) replaceActiveRequest(path string, stateID string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if previous, ok := s.cancels[path]; ok {
		previous()
	}
	s.latest[path] = stateID
	if cancel == nil {
		delete(s.cancels, path)
		return
	}
	s.cancels[path] = cancel
}

func (s *Session) cancelPath(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cancel, ok := s.cancels[path]; ok {
		cancel()
		delete(s.cancels, path)
	}
	delete(s.latest, path)
}

func (s *Session) cancelAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for path, cancel := range s.cancels {
		cancel()
		delete(s.cancels, path)
	}
	for path := range s.latest {
		delete(s.latest, path)
	}
}

func (s *Session) isLatest(path string, stateID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.latest[path] == stateID
}

func (s *Session) rememberRequest(request completion.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.requests[request.StateID] = request
}

func (s *Session) requestForState(stateID string) (completion.Request, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	request, ok := s.requests[stateID]
	return request, ok
}
