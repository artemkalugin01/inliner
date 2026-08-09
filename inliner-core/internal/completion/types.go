package completion

import (
	"context"
	"sync"
	"time"

	"github.com/aokalugin/inliner/inliner-core/internal/gocontext"
)

type Request struct {
	StateID     string
	FilePath    string
	Language    string
	Prefix      string
	Suffix      string
	Package     *gocontext.PackageContext
	RecentEdits []RecentEdit
	Timings     *RequestTimings
}

type RequestTimings struct {
	mu            sync.Mutex
	promptBuildMs int64
}

func (t *RequestTimings) SetPromptBuild(duration time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.promptBuildMs = duration.Milliseconds()
	t.mu.Unlock()
}

func (t *RequestTimings) PromptBuildMs() int64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.promptBuildMs
}

type RecentEdit struct {
	RelativePath string
	StartLine    int
	EndLine      int
	Before       string
	After        string
}

type Response struct {
	Items []Item
}

type Item struct {
	Kind   string
	Text   string
	Verify string
}

type Provider interface {
	Complete(ctx context.Context, request Request) (Response, error)
}

type ProviderDiagnostics struct {
	Status    string
	Reachable bool
	Error     string
}

type DiagnosticProvider interface {
	Diagnostics(ctx context.Context) ProviderDiagnostics
}
