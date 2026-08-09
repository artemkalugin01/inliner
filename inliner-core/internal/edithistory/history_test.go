package edithistory

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMemoryProviderSkipsFirstUpdate(t *testing.T) {
	p := NewMemoryProvider(MemoryOptions{})
	p.ObserveFileUpdate("/repo/service_test.go", "", "package service\n")

	if edits := p.Relevant(Query{FilePath: "/repo/service_test.go", ProjectRoot: "/repo", Prefix: "service"}); len(edits) != 0 {
		t.Fatalf("Relevant returned %d edits, want 0", len(edits))
	}
}

func TestMemoryProviderStoresInsertedGoHunk(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	p := NewMemoryProvider(MemoryOptions{Now: func() time.Time { return now }})
	oldContent := "package service\n\nfunc TestA(t *testing.T) {\n}\n"
	newContent := "package service\n\nfunc TestA(t *testing.T) {\n\trepo.EXPECT().Find().Return(nil)\n}\n"

	p.ObserveFileUpdate("/repo/service_test.go", oldContent, newContent)
	edits := p.Relevant(Query{FilePath: "/repo/service_test.go", ProjectRoot: "/repo", Prefix: "repo EXPECT Find"})

	if len(edits) != 1 {
		t.Fatalf("len(edits) = %d, want 1", len(edits))
	}
	if edits[0].RelativePath != "service_test.go" {
		t.Fatalf("RelativePath = %q", edits[0].RelativePath)
	}
	if strings.TrimSpace(edits[0].Before) != "" {
		t.Fatalf("Before = %q, want empty insert", edits[0].Before)
	}
	if !strings.Contains(edits[0].After, "repo.EXPECT().Find().Return(nil)") {
		t.Fatalf("After = %q, want inserted mock", edits[0].After)
	}
}

func TestMemoryProviderMergesNearbyEdits(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	p := NewMemoryProvider(MemoryOptions{Now: func() time.Time { return now }})
	path := "/repo/service_test.go"
	one := "package service\n\nfunc TestA(t *testing.T) {\n}\n"
	two := "package service\n\nfunc TestA(t *testing.T) {\n\trepo.EXPECT().Find().Return(nil)\n}\n"
	three := "package service\n\nfunc TestA(t *testing.T) {\n\trepo.EXPECT().Find().Return(nil)\n\trepo.EXPECT().Save().Return(nil)\n}\n"

	p.ObserveFileUpdate(path, one, two)
	now = now.Add(time.Second)
	p.ObserveFileUpdate(path, two, three)
	edits := p.Relevant(Query{FilePath: path, ProjectRoot: "/repo", Prefix: "repo EXPECT Save"})

	if len(edits) != 1 {
		t.Fatalf("len(edits) = %d, want merged edit", len(edits))
	}
	if !strings.Contains(edits[0].After, "Find") || !strings.Contains(edits[0].After, "Save") {
		t.Fatalf("After = %q, want both nearby edits", edits[0].After)
	}
}

func TestMemoryProviderCursorJumpSeparatesEdits(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	p := NewMemoryProvider(MemoryOptions{Now: func() time.Time { return now }})
	path := "/repo/service_test.go"
	oldContent := numberedLines(80)
	first := strings.Replace(oldContent, "line 10\n", "line 10\nmock one\n", 1)
	second := strings.Replace(first, "line 60\n", "line 60\nmock two\n", 1)

	p.ObserveFileUpdate(path, oldContent, first)
	p.ObserveCursor(path, 60)
	now = now.Add(time.Second)
	p.ObserveFileUpdate(path, first, second)
	edits := p.Relevant(Query{FilePath: path, ProjectRoot: "/repo", Prefix: "mock"})

	if len(edits) != 2 {
		t.Fatalf("len(edits) = %d, want 2 separated edits", len(edits))
	}
}

func TestMemoryProviderOnlyTracksGoFiles(t *testing.T) {
	p := NewMemoryProvider(MemoryOptions{})
	p.ObserveFileUpdate("/repo/readme.md", "old\n", "new\n")

	if edits := p.Relevant(Query{FilePath: "/repo/readme.md", ProjectRoot: "/repo", Prefix: "new"}); len(edits) != 0 {
		t.Fatalf("len(edits) = %d, want 0", len(edits))
	}
}

func TestMemoryProviderRanksRelevantEdits(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	p := NewMemoryProvider(MemoryOptions{Now: func() time.Time { return now }})
	p.ObserveFileUpdate("/repo/a_test.go", "package p\n", "package p\nrepo.EXPECT().Find().Return(nil)\n")
	p.ObserveCursor("/repo/a_test.go", 20)
	now = now.Add(3 * time.Second)
	p.ObserveFileUpdate("/repo/b_test.go", "package p\n", "package p\ncache.Set(value)\n")

	edits := p.Relevant(Query{FilePath: "/repo/a_test.go", ProjectRoot: "/repo", Prefix: "repo EXPECT"})
	if len(edits) != 2 {
		t.Fatalf("len(edits) = %d, want 2", len(edits))
	}
	if got := []string{edits[0].RelativePath, edits[1].RelativePath}; !reflect.DeepEqual(got, []string{"a_test.go", "b_test.go"}) {
		t.Fatalf("order = %#v", got)
	}
}

func numberedLines(count int) string {
	var builder strings.Builder
	for i := 1; i <= count; i++ {
		builder.WriteString("line ")
		builder.WriteString(string(rune('0' + i/10)))
		builder.WriteString(string(rune('0' + i%10)))
		builder.WriteString("\n")
	}
	return builder.String()
}
