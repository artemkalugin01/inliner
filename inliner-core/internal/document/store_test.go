package document

import "testing"

func TestStoreSetAndGet(t *testing.T) {
	store := NewStore()
	store.Set("/tmp/main.go", "package main\n")

	content, ok := store.Get("/tmp/main.go")
	if !ok {
		t.Fatal("Get returned ok=false")
	}
	if content != "package main\n" {
		t.Fatalf("content = %q, want %q", content, "package main\n")
	}
}

func TestStoreGetMissing(t *testing.T) {
	store := NewStore()

	_, ok := store.Get("/tmp/missing.go")
	if ok {
		t.Fatal("Get returned ok=true for missing file")
	}
}

func TestStoreLen(t *testing.T) {
	store := NewStore()
	if store.Len() != 0 {
		t.Fatalf("Len = %d, want 0", store.Len())
	}

	store.Set("/tmp/a.go", "package a\n")
	store.Set("/tmp/b.go", "package b\n")
	store.Set("/tmp/a.go", "package a\n")

	if store.Len() != 2 {
		t.Fatalf("Len = %d, want 2", store.Len())
	}
}
