package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const FileName = "request-lifecycle.jsonl"

type Event struct {
	Kind                  string `json:"kind"`
	Timestamp             string `json:"timestamp"`
	StateID               string `json:"stateId"`
	Language              string `json:"language"`
	Provider              string `json:"provider,omitempty"`
	Model                 string `json:"model,omitempty"`
	Status                string `json:"status"`
	Error                 string `json:"error,omitempty"`
	ProjectHash           string `json:"projectHash,omitempty"`
	FileHash              string `json:"fileHash,omitempty"`
	PrefixBytes           int    `json:"prefixBytes"`
	SuffixBytes           int    `json:"suffixBytes"`
	PackageFiles          int    `json:"packageFiles,omitempty"`
	PackageImports        int    `json:"packageImports,omitempty"`
	PackageValues         int    `json:"packageValues,omitempty"`
	PackageTypes          int    `json:"packageTypes,omitempty"`
	PackageInterfaces     int    `json:"packageInterfaces,omitempty"`
	PackageFunctions      int    `json:"packageFunctions,omitempty"`
	CurrentDeclaration    bool   `json:"currentDeclaration,omitempty"`
	VisibleIdentifiers    int    `json:"visibleIdentifiers,omitempty"`
	SiblingMethods        int    `json:"siblingMethods,omitempty"`
	RecentEdits           int    `json:"recentEdits,omitempty"`
	CompletionItems       int    `json:"completionItems"`
	CompletionTextBytes   int    `json:"completionTextBytes"`
	Cached                bool   `json:"cached,omitempty"`
	Suppressed            bool   `json:"suppressed,omitempty"`
	Stale                 bool   `json:"stale,omitempty"`
	Cancelled             bool   `json:"cancelled,omitempty"`
	CoreReceiveToStartMs  int64  `json:"coreReceiveToStartMs"`
	ContextCollectionMs   int64  `json:"contextCollectionMs"`
	RecentEditSelectionMs int64  `json:"recentEditSelectionMs"`
	RequestBuildMs        int64  `json:"requestBuildMs"`
	PromptBuildMs         int64  `json:"promptBuildMs"`
	ProviderMs            int64  `json:"providerMs"`
	ResponseWriteMs       int64  `json:"responseWriteMs"`
	TotalCoreMs           int64  `json:"totalCoreMs"`
}

type Recorder interface {
	Record(Event)
	Close()
}

type NoopRecorder struct{}

func (NoopRecorder) Record(Event) {}
func (NoopRecorder) Close()       {}

type AsyncRecorder struct {
	ch   chan Event
	done chan struct{}
	once sync.Once
}

func NewAsyncRecorder(dir string) (*AsyncRecorder, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(dir, FileName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}

	recorder := &AsyncRecorder{ch: make(chan Event, 256), done: make(chan struct{})}
	go func() {
		defer close(recorder.done)
		defer file.Close()
		encoder := json.NewEncoder(file)
		for event := range recorder.ch {
			_ = encoder.Encode(event)
		}
	}()
	return recorder, nil
}

func (r *AsyncRecorder) Record(event Event) {
	if event.Timestamp == "" {
		event.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	select {
	case r.ch <- event:
	default:
	}
}

func (r *AsyncRecorder) Close() {
	r.once.Do(func() {
		close(r.ch)
		<-r.done
	})
}
