package prompt

import (
	"strings"
	"testing"

	"github.com/aokalugin/inliner/inliner-core/internal/completion"
	"github.com/aokalugin/inliner/inliner-core/internal/gocontext"
)

func TestGoInlineBuilderIncludesCursorContext(t *testing.T) {
	prompt := GoInlineBuilder{}.Build(completion.Request{
		FilePath: "/tmp/main.go",
		Prefix:   "before",
		Suffix:   "after",
	})

	for _, want := range []string{"inline autocomplete engine for Go code", "/tmp/main.go", "before", "after", "<cursor>", "Do not include markdown"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt does not contain %q:\n%s", want, prompt)
		}
	}
}

func TestGoInlineBuilderIncludesPackageContext(t *testing.T) {
	prompt := GoInlineBuilder{}.Build(completion.Request{
		FilePath: "/repo/internal/service/service.go",
		Prefix:   "fmt.",
		Suffix:   "\n",
		Package: &gocontext.PackageContext{
			PackageName: "service",
			Files: []gocontext.File{
				{RelativePath: "internal/service/service.go"},
			},
			Current: &gocontext.Function{Signature: "(*Server) HandleUser(id string) error"},
			Visible: []gocontext.VisibleIdentifier{
				{Name: "s", Type: "*Server", Kind: "receiver"},
				{Name: "id", Type: "string", Kind: "parameter"},
				{Name: "user", Type: "User", Kind: "local variable"},
			},
			Siblings: []gocontext.Function{
				{Signature: "(*Server) ValidateUser(id string) error"},
			},
			Imports: []gocontext.Import{
				{Path: "context", RelativeFile: "internal/service/service.go"},
				{Name: "alias", Path: "io", RelativeFile: "internal/service/service.go"},
			},
			Types: []gocontext.Type{
				{Name: "User", Kind: "struct"},
			},
			Values: []gocontext.Value{
				{Kind: "const", Name: "defaultLimit", Type: "int", Value: "10", RelativeFile: "internal/service/service.go"},
				{Kind: "var", Name: "errNotFound", Value: `errors.New("not found")`, RelativeFile: "internal/service/errors.go"},
			},
			Interfaces: []gocontext.Interface{
				{Name: "Store", Methods: []string{"FindUser(id string) (User, error)"}},
			},
			Functions: []gocontext.Function{
				{Signature: "NewStore(path string) (Store, error)"},
				{Signature: "(*Server) HandleUser(id string) error"},
			},
		},
	})

	for _, want := range []string{
		"Current package context:",
		"Package: service",
		"internal/service/service.go",
		"Current function or method:",
		"Visible identifiers:",
		"s *Server receiver",
		"id string parameter",
		"user User local variable",
		"Sibling methods for current receiver:",
		"(*Server) ValidateUser(id string) error",
		`"context" from internal/service/service.go`,
		`alias "io" from internal/service/service.go`,
		"User struct",
		"Package constants and variables:",
		"const defaultLimit int = 10 from internal/service/service.go",
		`var errNotFound = errors.New("not found") from internal/service/errors.go`,
		"Store",
		"FindUser(id string) (User, error)",
		"NewStore(path string) (Store, error)",
		"(*Server) HandleUser(id string) error",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt does not contain %q:\n%s", want, prompt)
		}
	}
}

func TestGoInlineBuilderOmitsPackageContextWhenUnset(t *testing.T) {
	prompt := GoInlineBuilder{}.Build(completion.Request{})

	if strings.Contains(prompt, "Current package context:") {
		t.Fatalf("prompt unexpectedly contains package context:\n%s", prompt)
	}
}

func TestGoInlineBuilderBudgetsPackageContext(t *testing.T) {
	prompt := GoInlineBuilder{
		MaxFiles:            1,
		MaxImports:          1,
		MaxTypes:            1,
		MaxInterfaces:       1,
		MaxInterfaceMethods: 1,
		MaxVisible:          1,
		MaxSiblings:         1,
		MaxValues:           1,
		MaxFunctions:        1,
	}.Build(completion.Request{Package: &gocontext.PackageContext{
		PackageName: "service",
		Files: []gocontext.File{
			{RelativePath: "a.go"},
			{RelativePath: "b.go"},
		},
		Imports: []gocontext.Import{
			{Path: "context", RelativeFile: "a.go"},
			{Path: "fmt", RelativeFile: "b.go"},
		},
		Visible: []gocontext.VisibleIdentifier{
			{Name: "ctx", Type: "context.Context", Kind: "parameter"},
			{Name: "err", Type: "error", Kind: "local variable"},
		},
		Siblings: []gocontext.Function{
			{Signature: "A.Sibling()"},
			{Signature: "B.Sibling()"},
		},
		Types: []gocontext.Type{
			{Name: "A", Kind: "struct"},
			{Name: "B", Kind: "struct"},
		},
		Values: []gocontext.Value{
			{Kind: "const", Name: "AValue", Value: "1"},
			{Kind: "var", Name: "BValue", Value: "2"},
		},
		Interfaces: []gocontext.Interface{
			{Name: "Reader", Methods: []string{"Read()", "Close()"}},
			{Name: "Writer", Methods: []string{"Write()"}},
		},
		Functions: []gocontext.Function{
			{Signature: "A()"},
			{Signature: "B()"},
		},
	}})

	for _, want := range []string{
		"a.go",
		`"context" from a.go`,
		"ctx context.Context parameter",
		"A.Sibling()",
		"A struct",
		"const AValue = 1",
		"Reader",
		"Read()",
		"A()",
		"1 more files omitted",
		"1 more imports omitted",
		"1 more types omitted",
		"1 more interface methods omitted",
		"1 more interfaces omitted",
		"1 more visible identifiers omitted",
		"1 more sibling methods omitted",
		"1 more package values omitted",
		"1 more functions omitted",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt does not contain %q:\n%s", want, prompt)
		}
	}

	for _, omitted := range []string{`"fmt" from b.go`, "err error local variable", "B.Sibling()", "B struct", "BValue", "Writer", "Write()", "B()"} {
		if strings.Contains(prompt, omitted) {
			t.Fatalf("prompt contains omitted item %q:\n%s", omitted, prompt)
		}
	}
}

func TestGoInlineBuilderUsesDefaultBudgetsForZeroValues(t *testing.T) {
	builder := GoInlineBuilder{}.withDefaults()

	if builder.MaxFiles != DefaultMaxFiles {
		t.Fatalf("MaxFiles = %d, want %d", builder.MaxFiles, DefaultMaxFiles)
	}
	if builder.MaxImports != DefaultMaxImports {
		t.Fatalf("MaxImports = %d, want %d", builder.MaxImports, DefaultMaxImports)
	}
	if builder.MaxTypes != DefaultMaxTypes {
		t.Fatalf("MaxTypes = %d, want %d", builder.MaxTypes, DefaultMaxTypes)
	}
	if builder.MaxInterfaces != DefaultMaxInterfaces {
		t.Fatalf("MaxInterfaces = %d, want %d", builder.MaxInterfaces, DefaultMaxInterfaces)
	}
	if builder.MaxInterfaceMethods != DefaultMaxInterfaceMethods {
		t.Fatalf("MaxInterfaceMethods = %d, want %d", builder.MaxInterfaceMethods, DefaultMaxInterfaceMethods)
	}
	if builder.MaxVisible != DefaultMaxVisible {
		t.Fatalf("MaxVisible = %d, want %d", builder.MaxVisible, DefaultMaxVisible)
	}
	if builder.MaxSiblings != DefaultMaxSiblings {
		t.Fatalf("MaxSiblings = %d, want %d", builder.MaxSiblings, DefaultMaxSiblings)
	}
	if builder.MaxValues != DefaultMaxValues {
		t.Fatalf("MaxValues = %d, want %d", builder.MaxValues, DefaultMaxValues)
	}
	if builder.MaxFunctions != DefaultMaxFunctions {
		t.Fatalf("MaxFunctions = %d, want %d", builder.MaxFunctions, DefaultMaxFunctions)
	}
	if builder.MaxRecentEdits != DefaultMaxRecentEdits {
		t.Fatalf("MaxRecentEdits = %d, want %d", builder.MaxRecentEdits, DefaultMaxRecentEdits)
	}
}

func TestGoInlineBuilderIncludesRecentEdits(t *testing.T) {
	prompt := GoInlineBuilder{}.Build(completion.Request{
		RecentEdits: []completion.RecentEdit{
			{
				RelativePath: "service_test.go",
				StartLine:    10,
				EndLine:      12,
				Before:       "",
				After:        "\trepo.EXPECT().Find().Return(nil)\n",
			},
		},
	})

	for _, want := range []string{
		"Recent similar edits:",
		"service_test.go lines 10-12",
		"+ \trepo.EXPECT().Find().Return(nil)",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt does not contain %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "-\n") {
		t.Fatalf("pure insert rendered an empty removed line:\n%s", prompt)
	}
}

func TestGoInlineBuilderBudgetsRecentEdits(t *testing.T) {
	prompt := GoInlineBuilder{MaxRecentEdits: 1}.Build(completion.Request{
		RecentEdits: []completion.RecentEdit{
			{RelativePath: "a.go", StartLine: 1, EndLine: 1, After: "first\n"},
			{RelativePath: "b.go", StartLine: 2, EndLine: 2, After: "second\n"},
		},
	})

	for _, want := range []string{"a.go lines 1-1", "+ first", "1 more recent edits omitted"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt does not contain %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "b.go") || strings.Contains(prompt, "second") {
		t.Fatalf("prompt contains omitted edit:\n%s", prompt)
	}
}

func TestGoInlineBuilderTrimsPackageImportPreambleWhenImportsAreSeparate(t *testing.T) {
	prompt := GoInlineBuilder{}.Build(completion.Request{
		Prefix: "package main\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n)\n\nfunc main() {\n\tfmt.",
		Suffix: "\n}\n",
		Package: &gocontext.PackageContext{
			PackageName: "main",
			Imports:     []gocontext.Import{{Path: "fmt"}, {Path: "strings"}},
		},
	})

	if strings.Contains(prompt, "package main\n\nimport") {
		t.Fatalf("prompt still contains package/import preamble:\n%s", prompt)
	}
	if !strings.Contains(prompt, "func main() {\n\tfmt.") {
		t.Fatalf("prompt does not preserve code after imports:\n%s", prompt)
	}
}

func TestGoInlineBuilderTrimsSingleImportPreamble(t *testing.T) {
	prompt := GoInlineBuilder{}.Build(completion.Request{
		Prefix:  "package main\nimport \"fmt\"\n\nfunc main() {\n\tfmt.",
		Package: &gocontext.PackageContext{PackageName: "main", Imports: []gocontext.Import{{Path: "fmt"}}},
	})

	if strings.Contains(prompt, "import \"fmt\"") {
		t.Fatalf("prompt still contains single import declaration:\n%s", prompt)
	}
	if !strings.Contains(prompt, "func main()") {
		t.Fatalf("prompt does not preserve code after import:\n%s", prompt)
	}
}
