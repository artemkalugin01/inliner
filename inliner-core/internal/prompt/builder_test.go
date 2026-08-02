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
			Imports: []gocontext.Import{
				{Path: "context", RelativeFile: "internal/service/service.go"},
				{Name: "alias", Path: "io", RelativeFile: "internal/service/service.go"},
			},
			Types: []gocontext.Type{
				{Name: "User", Kind: "struct"},
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
		`"context" from internal/service/service.go`,
		`alias "io" from internal/service/service.go`,
		"User struct",
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
		Types: []gocontext.Type{
			{Name: "A", Kind: "struct"},
			{Name: "B", Kind: "struct"},
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
		"A struct",
		"Reader",
		"Read()",
		"A()",
		"1 more files omitted",
		"1 more imports omitted",
		"1 more types omitted",
		"1 more interface methods omitted",
		"1 more interfaces omitted",
		"1 more visible identifiers omitted",
		"1 more functions omitted",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt does not contain %q:\n%s", want, prompt)
		}
	}

	for _, omitted := range []string{`"fmt" from b.go`, "err error local variable", "B struct", "Writer", "Write()", "B()"} {
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
	if builder.MaxFunctions != DefaultMaxFunctions {
		t.Fatalf("MaxFunctions = %d, want %d", builder.MaxFunctions, DefaultMaxFunctions)
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
