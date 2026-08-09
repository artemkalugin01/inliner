package edithistory

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	MaxEditsPerFile     = 50
	MaxEditsPerProject  = 500
	MaxHunkBytes        = 4096
	MaxPromptEdits      = 5
	MergeWindow         = 2 * time.Second
	NearbyLineThreshold = 3
	CursorJumpThreshold = 20
)

var tokenPattern = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

type Provider interface {
	ObserveFileUpdate(path string, oldContent string, newContent string)
	ObserveCursor(path string, line int)
	Relevant(query Query) []Edit
}

type Query struct {
	FilePath           string
	ProjectRoot        string
	CursorLine         int
	Prefix             string
	Suffix             string
	VisibleIdentifiers []string
	CurrentFunction    string
}

type Edit struct {
	FilePath     string
	RelativePath string
	StartLine    int
	EndLine      int
	Before       string
	After        string
	Tokens       []string
	CreatedAt    time.Time
}

type MemoryProvider struct {
	mu          sync.Mutex
	files       map[string][]Edit
	pending     map[string]Edit
	cursorLines map[string]int
	now         func() time.Time
}

type MemoryOptions struct {
	Now func() time.Time
}

func NewMemoryProvider(options MemoryOptions) *MemoryProvider {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &MemoryProvider{
		files:       map[string][]Edit{},
		pending:     map[string]Edit{},
		cursorLines: map[string]int{},
		now:         now,
	}
}

func (p *MemoryProvider) ObserveFileUpdate(path string, oldContent string, newContent string) {
	if filepath.Ext(path) != ".go" || oldContent == "" || oldContent == newContent {
		return
	}
	edit, ok := diffEdit(path, oldContent, newContent, p.now())
	if !ok {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if pending, ok := p.pending[path]; ok && p.shouldMerge(pending, edit) {
		p.pending[path] = mergeEdits(pending, edit, oldContent, newContent, p.now())
		return
	}
	if pending, ok := p.pending[path]; ok {
		p.storeLocked(pending)
	}
	p.pending[path] = edit
}

func (p *MemoryProvider) ObserveCursor(path string, line int) {
	if filepath.Ext(path) != ".go" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	previous, ok := p.cursorLines[path]
	p.cursorLines[path] = line
	if !ok || abs(previous-line) <= CursorJumpThreshold {
		return
	}
	p.flushPendingLocked(path)
}

func (p *MemoryProvider) Relevant(query Query) []Edit {
	p.mu.Lock()
	defer p.mu.Unlock()

	for path := range p.pending {
		p.flushPendingLocked(path)
	}

	queryTokens := tokenSet(query.Prefix + "\n" + query.Suffix + "\n" + strings.Join(query.VisibleIdentifiers, " ") + "\n" + query.CurrentFunction)
	type scored struct {
		edit  Edit
		score int
	}
	var scoredEdits []scored
	for _, edits := range p.files {
		for _, edit := range edits {
			if edit.FilePath == query.FilePath && lineInRange(query.CursorLine, edit.StartLine, edit.EndLine) {
				continue
			}
			score := relevanceScore(edit, query, queryTokens)
			if score <= 0 {
				continue
			}
			scoredEdits = append(scoredEdits, scored{edit: edit, score: score})
		}
	}
	sort.SliceStable(scoredEdits, func(i, j int) bool {
		if scoredEdits[i].score != scoredEdits[j].score {
			return scoredEdits[i].score > scoredEdits[j].score
		}
		return scoredEdits[i].edit.CreatedAt.After(scoredEdits[j].edit.CreatedAt)
	})

	limit := MaxPromptEdits
	if len(scoredEdits) < limit {
		limit = len(scoredEdits)
	}
	result := make([]Edit, 0, limit)
	for _, item := range scoredEdits[:limit] {
		edit := item.edit
		edit.RelativePath = relativePath(query.ProjectRoot, edit.FilePath)
		result = append(result, edit)
	}
	return result
}

func (p *MemoryProvider) shouldMerge(previous Edit, next Edit) bool {
	if next.CreatedAt.Sub(previous.CreatedAt) > MergeWindow {
		return false
	}
	if cursor, ok := p.cursorLines[next.FilePath]; ok && distanceFromRange(cursor, previous.StartLine, previous.EndLine) > CursorJumpThreshold {
		return false
	}
	if rangesNear(previous.StartLine, previous.EndLine, next.StartLine, next.EndLine, NearbyLineThreshold) {
		return true
	}
	return false
}

func (p *MemoryProvider) flushPendingLocked(path string) {
	if pending, ok := p.pending[path]; ok {
		p.storeLocked(pending)
		delete(p.pending, path)
	}
}

func (p *MemoryProvider) storeLocked(edit Edit) {
	if strings.TrimSpace(edit.Before) == "" && strings.TrimSpace(edit.After) == "" {
		return
	}
	if len(edit.Before)+len(edit.After) > MaxHunkBytes {
		return
	}
	edits := append(p.files[edit.FilePath], edit)
	if len(edits) > MaxEditsPerFile {
		edits = edits[len(edits)-MaxEditsPerFile:]
	}
	p.files[edit.FilePath] = edits
	p.enforceProjectLimitLocked()
}

func (p *MemoryProvider) enforceProjectLimitLocked() {
	total := 0
	for _, edits := range p.files {
		total += len(edits)
	}
	for total > MaxEditsPerProject {
		var oldestPath string
		var oldestTime time.Time
		for path, edits := range p.files {
			if len(edits) == 0 {
				continue
			}
			if oldestPath == "" || edits[0].CreatedAt.Before(oldestTime) {
				oldestPath = path
				oldestTime = edits[0].CreatedAt
			}
		}
		if oldestPath == "" {
			return
		}
		p.files[oldestPath] = p.files[oldestPath][1:]
		total--
	}
}

func diffEdit(path string, oldContent string, newContent string, createdAt time.Time) (Edit, bool) {
	prefix := commonPrefix(oldContent, newContent)
	suffix := commonSuffix(oldContent[prefix:], newContent[prefix:])
	rawBefore := oldContent[prefix : len(oldContent)-suffix]
	rawAfter := newContent[prefix : len(newContent)-suffix]
	oldStart, oldEnd := expandToLine(oldContent, prefix, len(oldContent)-suffix)
	newStart, newEnd := expandToLine(newContent, prefix, len(newContent)-suffix)
	before := oldContent[oldStart:oldEnd]
	after := newContent[newStart:newEnd]
	if rawBefore == "" {
		before = ""
	}
	if rawAfter == "" {
		after = ""
	}
	if strings.TrimSpace(before) == "" && strings.TrimSpace(after) == "" {
		return Edit{}, false
	}
	if len(before)+len(after) > MaxHunkBytes {
		return Edit{}, false
	}
	startLine := lineNumber(newContent, newStart)
	endLine := lineNumber(newContent, newEnd)
	if endLine < startLine {
		endLine = startLine
	}
	return Edit{FilePath: path, StartLine: startLine, EndLine: endLine, Before: before, After: after, Tokens: sortedTokens(before + "\n" + after), CreatedAt: createdAt}, true
}

func mergeEdits(previous Edit, next Edit, oldContent string, newContent string, now time.Time) Edit {
	startLine := previous.StartLine
	if next.StartLine < startLine {
		startLine = next.StartLine
	}
	endLine := previous.EndLine
	if next.EndLine > endLine {
		endLine = next.EndLine
	}
	before := linesRange(oldContent, startLine, endLine)
	after := linesRange(newContent, startLine, endLine)
	return Edit{FilePath: next.FilePath, StartLine: startLine, EndLine: endLine, Before: before, After: after, Tokens: sortedTokens(before + "\n" + after), CreatedAt: now}
}

func relevanceScore(edit Edit, query Query, queryTokens map[string]bool) int {
	score := 1
	if edit.FilePath == query.FilePath {
		score += 1000
	} else if filepath.Dir(edit.FilePath) == filepath.Dir(query.FilePath) {
		score += 500
	}
	if strings.HasSuffix(edit.FilePath, "_test.go") && strings.HasSuffix(query.FilePath, "_test.go") {
		score += 250
	}
	for _, token := range edit.Tokens {
		if queryTokens[token] {
			score += 50
		}
	}
	return score
}

func commonPrefix(a string, b string) int {
	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}
	for i := 0; i < limit; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return limit
}

func commonSuffix(a string, b string) int {
	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}
	for i := 0; i < limit; i++ {
		if a[len(a)-1-i] != b[len(b)-1-i] {
			return i
		}
	}
	return limit
}

func expandToLine(content string, start int, end int) (int, int) {
	for start > 0 && content[start-1] != '\n' {
		start--
	}
	for end < len(content) && content[end] != '\n' {
		end++
	}
	if end < len(content) {
		end++
	}
	return start, end
}

func lineNumber(content string, offset int) int {
	if offset > len(content) {
		offset = len(content)
	}
	return strings.Count(content[:offset], "\n") + 1
}

func linesRange(content string, startLine int, endLine int) string {
	if startLine < 1 {
		startLine = 1
	}
	if endLine < startLine {
		endLine = startLine
	}
	start := offsetForLine(content, startLine)
	end := offsetForLine(content, endLine+1)
	return content[start:end]
}

func offsetForLine(content string, line int) int {
	if line <= 1 {
		return 0
	}
	current := 1
	for i, ch := range content {
		if ch == '\n' {
			current++
			if current == line {
				return i + 1
			}
		}
	}
	return len(content)
}

func tokenSet(value string) map[string]bool {
	result := map[string]bool{}
	for _, token := range tokenPattern.FindAllString(value, -1) {
		result[token] = true
	}
	return result
}

func sortedTokens(value string) []string {
	set := tokenSet(value)
	tokens := make([]string, 0, len(set))
	for token := range set {
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	return tokens
}

func rangesNear(aStart int, aEnd int, bStart int, bEnd int, threshold int) bool {
	return bStart <= aEnd+threshold && aStart <= bEnd+threshold
}

func lineInRange(line int, start int, end int) bool {
	return line >= start && line <= end
}

func distanceFromRange(line int, start int, end int) int {
	if line < start {
		return start - line
	}
	if line > end {
		return line - end
	}
	return 0
}

func relativePath(root string, path string) string {
	if root == "" {
		return path
	}
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
