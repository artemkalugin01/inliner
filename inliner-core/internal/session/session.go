package session

import (
	"context"
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
	"github.com/aokalugin/inliner/inliner-core/internal/version"
)

type Session struct {
	transport *protocol.Transport
	complete  *completion.Service
	documents *document.Store
	history   edithistory.Provider
	collector *gocontext.Collector
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
	RequestTimeout    time.Duration
	WindowBytes       int
}

func New(transport *protocol.Transport, complete *completion.Service, options ...Options) *Session {
	resolved := Options{WindowBytes: document.DefaultWindowBytes}
	if len(options) > 0 {
		resolved = options[0]
	}
	if resolved.WindowBytes <= 0 {
		resolved.WindowBytes = document.DefaultWindowBytes
	}

	return &Session{
		transport: transport,
		complete:  complete,
		documents: document.NewStore(),
		history:   edithistory.NewMemoryProvider(edithistory.MemoryOptions{}),
		collector: gocontext.NewCollector(),
		options:   resolved,
		latest:    make(map[string]string),
		cancels:   make(map[string]context.CancelFunc),
		requests:  make(map[string]completion.Request),
		cache:     completion.NewAcceptanceCache(completion.CacheOptions{}),
		dismissed: completion.NewDismissalCache(completion.CacheOptions{}),
	}
}

func (s *Session) Run(ctx context.Context) error {
	for {
		msg, err := s.transport.Read()
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

		if err := s.handle(ctx, msg); err != nil {
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

func (s *Session) handle(ctx context.Context, msg protocol.RawMessage) error {
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
		return s.handleStateUpdate(ctx, update)
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

func (s *Session) handleStateUpdate(ctx context.Context, update protocol.StateUpdate) error {
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
			if err := s.completeAtCursor(ctx, update.NewID, item.Path, item.Offset); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown state update kind %q", item.Kind)
		}
	}

	return nil
}

func (s *Session) completeAtCursor(ctx context.Context, stateID string, path string, offset int) error {
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
	}
	if pkg, ok := s.collectPackageContext(path, content, offset); ok {
		request.Package = &pkg
	}
	request.RecentEdits = s.recentEdits(path, cursorLine, request)
	s.rememberRequest(request)

	if text, ok := s.cache.Lookup(request); ok {
		s.replaceActiveRequest(path, stateID, nil)
		if s.isLatest(path, stateID) {
			if s.dismissed.IsDismissed(request, text) {
				return s.sendCompletionResponse(stateID, []completion.Item{{Kind: "end"}})
			}
			return s.sendCompletionResponse(stateID, []completion.Item{{Kind: "text", Text: text}, {Kind: "end"}})
		}
		return nil
	}

	completionCtx, cancel := context.WithCancel(ctx)
	s.replaceActiveRequest(path, stateID, cancel)

	s.workers.Add(1)
	go s.runCompletion(completionCtx, request)

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

func (s *Session) runCompletion(ctx context.Context, request completion.Request) {
	defer s.workers.Done()

	response, err := s.complete.Complete(ctx, request)
	if err != nil {
		if ctx.Err() != nil || !s.isLatest(request.FilePath, request.StateID) {
			return
		}
		_ = s.sendError(err.Error())
		return
	}
	if ctx.Err() != nil || !s.isLatest(request.FilePath, request.StateID) {
		return
	}
	if s.shouldSuppressResponse(request, response.Items) {
		_ = s.sendCompletionResponse(request.StateID, []completion.Item{{Kind: "end"}})
		return
	}

	_ = s.sendCompletionResponse(request.StateID, response.Items)
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
