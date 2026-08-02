package completion

import (
	"testing"
	"time"
)

func TestAcceptanceCacheHit(t *testing.T) {
	cache := NewAcceptanceCache(CacheOptions{})
	req := Request{Language: "go", FilePath: "/tmp/main.go", Prefix: "fmt.", Suffix: "\n"}

	cache.Store(req, "Println(name)")

	text, ok := cache.Lookup(req)
	if !ok {
		t.Fatal("Lookup returned ok=false")
	}
	if text != "Println(name)" {
		t.Fatalf("text = %q, want %q", text, "Println(name)")
	}
}

func TestAcceptanceCacheMissesDifferentContext(t *testing.T) {
	cache := NewAcceptanceCache(CacheOptions{})
	cache.Store(Request{Language: "go", FilePath: "/tmp/main.go", Prefix: "fmt.", Suffix: "\n"}, "Println(name)")

	if _, ok := cache.Lookup(Request{Language: "go", FilePath: "/tmp/main.go", Prefix: "log.", Suffix: "\n"}); ok {
		t.Fatal("Lookup returned hit for different prefix")
	}
	if _, ok := cache.Lookup(Request{Language: "go", FilePath: "/tmp/other.go", Prefix: "fmt.", Suffix: "\n"}); ok {
		t.Fatal("Lookup returned hit for different file")
	}
}

func TestAcceptanceCacheIgnoresEmptyText(t *testing.T) {
	cache := NewAcceptanceCache(CacheOptions{})
	req := Request{Language: "go", FilePath: "/tmp/main.go", Prefix: "fmt."}

	cache.Store(req, "")

	if _, ok := cache.Lookup(req); ok {
		t.Fatal("Lookup returned hit for empty stored text")
	}
}

func TestAcceptanceCacheUpdatesExistingEntry(t *testing.T) {
	cache := NewAcceptanceCache(CacheOptions{})
	req := Request{Language: "go", FilePath: "/tmp/main.go", Prefix: "fmt."}

	cache.Store(req, "Print(name)")
	cache.Store(req, "Println(name)")

	text, ok := cache.Lookup(req)
	if !ok {
		t.Fatal("Lookup returned ok=false")
	}
	if text != "Println(name)" {
		t.Fatalf("text = %q, want updated text", text)
	}
}

func TestAcceptanceCacheExpiresEntries(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	cache := NewAcceptanceCache(CacheOptions{TTL: time.Minute, Now: func() time.Time { return now }})
	req := Request{Language: "go", FilePath: "/tmp/main.go", Prefix: "fmt."}

	cache.Store(req, "Println(name)")
	now = now.Add(2 * time.Minute)

	if _, ok := cache.Lookup(req); ok {
		t.Fatal("Lookup returned hit for expired entry")
	}
}

func TestAcceptanceCacheEvictsOldestEntries(t *testing.T) {
	cache := NewAcceptanceCache(CacheOptions{MaxEntries: 2})
	req1 := Request{Language: "go", FilePath: "/tmp/1.go", Prefix: "a"}
	req2 := Request{Language: "go", FilePath: "/tmp/2.go", Prefix: "b"}
	req3 := Request{Language: "go", FilePath: "/tmp/3.go", Prefix: "c"}

	cache.Store(req1, "one")
	cache.Store(req2, "two")
	cache.Store(req3, "three")

	if _, ok := cache.Lookup(req1); ok {
		t.Fatal("Lookup returned hit for evicted entry")
	}
	if text, ok := cache.Lookup(req2); !ok || text != "two" {
		t.Fatalf("req2 lookup = %q/%v, want two/true", text, ok)
	}
	if text, ok := cache.Lookup(req3); !ok || text != "three" {
		t.Fatalf("req3 lookup = %q/%v, want three/true", text, ok)
	}
}

func TestAcceptanceCacheUsesNearbyContext(t *testing.T) {
	cache := NewAcceptanceCache(CacheOptions{})
	prefix := stringsOfLength('a', cacheContextBytes+10) + "fmt."
	req := Request{Language: "go", FilePath: "/tmp/main.go", Prefix: prefix, Suffix: stringsOfLength('b', cacheContextBytes+10)}

	cache.Store(req, "Println(name)")

	matching := Request{
		Language: "go",
		FilePath: "/tmp/main.go",
		Prefix:   stringsOfLength('x', 50) + tail(prefix, cacheContextBytes),
		Suffix:   head(req.Suffix, cacheContextBytes) + stringsOfLength('y', 50),
	}
	if _, ok := cache.Lookup(matching); !ok {
		t.Fatal("Lookup returned miss for matching nearby context")
	}
}

func TestDismissalCacheHit(t *testing.T) {
	cache := NewDismissalCache(CacheOptions{})
	req := Request{Language: "go", FilePath: "/tmp/main.go", Prefix: "fmt.", Suffix: "\n"}

	cache.Store(req, "Println(name)")

	if !cache.IsDismissed(req, "Println(name)") {
		t.Fatal("IsDismissed returned false, want true")
	}
}

func TestDismissalCacheMissesDifferentTextOrContext(t *testing.T) {
	cache := NewDismissalCache(CacheOptions{})
	req := Request{Language: "go", FilePath: "/tmp/main.go", Prefix: "fmt.", Suffix: "\n"}
	cache.Store(req, "Println(name)")

	if cache.IsDismissed(req, "Print(name)") {
		t.Fatal("IsDismissed returned true for different text")
	}
	if cache.IsDismissed(Request{Language: "go", FilePath: "/tmp/main.go", Prefix: "log.", Suffix: "\n"}, "Println(name)") {
		t.Fatal("IsDismissed returned true for different context")
	}
}

func TestDismissalCacheIgnoresEmptyText(t *testing.T) {
	cache := NewDismissalCache(CacheOptions{})
	req := Request{Language: "go", FilePath: "/tmp/main.go", Prefix: "fmt."}

	cache.Store(req, "")

	if cache.IsDismissed(req, "") {
		t.Fatal("IsDismissed returned true for empty text")
	}
}

func TestDismissalCacheExpiresEntries(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	cache := NewDismissalCache(CacheOptions{TTL: time.Minute, Now: func() time.Time { return now }})
	req := Request{Language: "go", FilePath: "/tmp/main.go", Prefix: "fmt."}

	cache.Store(req, "Println(name)")
	now = now.Add(2 * time.Minute)

	if cache.IsDismissed(req, "Println(name)") {
		t.Fatal("IsDismissed returned true for expired entry")
	}
}

func TestDismissalCacheEvictsOldestEntries(t *testing.T) {
	cache := NewDismissalCache(CacheOptions{MaxEntries: 2})
	req1 := Request{Language: "go", FilePath: "/tmp/1.go", Prefix: "a"}
	req2 := Request{Language: "go", FilePath: "/tmp/2.go", Prefix: "b"}
	req3 := Request{Language: "go", FilePath: "/tmp/3.go", Prefix: "c"}

	cache.Store(req1, "one")
	cache.Store(req2, "two")
	cache.Store(req3, "three")

	if cache.IsDismissed(req1, "one") {
		t.Fatal("IsDismissed returned true for evicted entry")
	}
	if !cache.IsDismissed(req2, "two") {
		t.Fatal("IsDismissed returned false for retained req2")
	}
	if !cache.IsDismissed(req3, "three") {
		t.Fatal("IsDismissed returned false for retained req3")
	}
}

func stringsOfLength(ch byte, length int) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = ch
	}
	return string(b)
}
