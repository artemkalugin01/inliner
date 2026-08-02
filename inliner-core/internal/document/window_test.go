package document

import "testing"

func TestAroundCursorReturnsPrefixAndSuffix(t *testing.T) {
	window := AroundCursor("0123456789", 5, 3)

	if window.Prefix != "234" {
		t.Fatalf("Prefix = %q, want %q", window.Prefix, "234")
	}
	if window.Suffix != "567" {
		t.Fatalf("Suffix = %q, want %q", window.Suffix, "567")
	}
}

func TestAroundCursorClampsNegativeOffset(t *testing.T) {
	window := AroundCursor("abc", -10, 2)

	if window.Prefix != "" {
		t.Fatalf("Prefix = %q, want empty", window.Prefix)
	}
	if window.Suffix != "ab" {
		t.Fatalf("Suffix = %q, want %q", window.Suffix, "ab")
	}
}

func TestAroundCursorClampsOffsetPastEnd(t *testing.T) {
	window := AroundCursor("abc", 10, 2)

	if window.Prefix != "bc" {
		t.Fatalf("Prefix = %q, want %q", window.Prefix, "bc")
	}
	if window.Suffix != "" {
		t.Fatalf("Suffix = %q, want empty", window.Suffix)
	}
}

func TestAroundCursorUsesDefaultForInvalidLimit(t *testing.T) {
	content := stringsOfLength('a', DefaultWindowBytes+10) + "|" + stringsOfLength('b', DefaultWindowBytes+10)
	offset := DefaultWindowBytes + 10

	window := AroundCursor(content, offset, 0)

	if len(window.Prefix) != DefaultWindowBytes {
		t.Fatalf("len(Prefix) = %d, want %d", len(window.Prefix), DefaultWindowBytes)
	}
	if len(window.Suffix) != DefaultWindowBytes {
		t.Fatalf("len(Suffix) = %d, want %d", len(window.Suffix), DefaultWindowBytes)
	}
}

func stringsOfLength(ch byte, length int) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = ch
	}
	return string(b)
}
