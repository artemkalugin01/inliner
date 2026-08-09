package gocontext

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCollectorCollectsCurrentPackageDeclarations(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "internal", "service")
	mustMkdir(t, pkgDir)
	currentFile := filepath.Join(pkgDir, "service.go")

	writeFile(t, currentFile, `package service

import (
	"context"
	alias "io"
	_ "net/http/pprof"
)

type User struct {
	ID string
}

type userID string

type Store interface {
	FindUser(id string) (User, error)
	SaveUser(user User) error
}

type embedded interface {
	Store
}

func NewStore(path string) (Store, error) { return nil, nil }

func helper(count int) bool { return count > 0 }
`)
	writeFile(t, filepath.Join(pkgDir, "server.go"), `package service

type Server struct{}

func (s *Server) HandleUser(id string) error { return nil }

func (s Server) Name() string { return "" }
`)
	writeFile(t, filepath.Join(pkgDir, "service_test.go"), `package service

func TestOnly(t *testing.T) {}
`)

	ctx, err := NewCollector().Collect(currentFile, root)
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if ctx.PackageName != "service" {
		t.Fatalf("PackageName = %q, want service", ctx.PackageName)
	}
	if got := fileRelativePaths(ctx.Files); !reflect.DeepEqual(got, []string{"internal/service/service.go", "internal/service/server.go"}) {
		t.Fatalf("files = %#v", got)
	}
	if got := functionSignatures(ctx.Functions); !reflect.DeepEqual(got, []string{
		"NewStore(path string) (Store, error)",
		"helper(count int) bool",
		"(*Server) HandleUser(id string) error",
		"(Server) Name() string",
	}) {
		t.Fatalf("functions = %#v", got)
	}
	if got := typeNames(ctx.Types); !reflect.DeepEqual(got, []string{"User:struct", "userID:alias", "Server:struct"}) {
		t.Fatalf("types = %#v", got)
	}
	if got := importSummaries(ctx.Imports); !reflect.DeepEqual(got, []string{
		` "context" internal/service/service.go`,
		`alias "io" internal/service/service.go`,
		`_ "net/http/pprof" internal/service/service.go`,
	}) {
		t.Fatalf("imports = %#v", got)
	}
	if got := interfaceSummaries(ctx.Interfaces); !reflect.DeepEqual(got, []string{
		"Store:FindUser(id string) (User, error),SaveUser(user User) error",
		"embedded:Store",
	}) {
		t.Fatalf("interfaces = %#v", got)
	}
}

func TestCollectorUsesCurrentFilesPackageOnly(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "mixed")
	mustMkdir(t, pkgDir)
	currentFile := filepath.Join(pkgDir, "main.go")

	writeFile(t, currentFile, `package alpha

func Alpha() {}
`)
	writeFile(t, filepath.Join(pkgDir, "other.go"), `package beta

func Beta() {}
`)

	ctx, err := NewCollector().Collect(currentFile, root)
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if ctx.PackageName != "alpha" {
		t.Fatalf("PackageName = %q, want alpha", ctx.PackageName)
	}
	if got := functionSignatures(ctx.Functions); !reflect.DeepEqual(got, []string{"Alpha()"}) {
		t.Fatalf("functions = %#v", got)
	}
}

func TestCollectorCollectWithOverlayUsesUnsavedCurrentFileContent(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "service")
	mustMkdir(t, pkgDir)
	currentFile := filepath.Join(pkgDir, "service.go")

	writeFile(t, currentFile, `package service

type Saved struct{}

func SavedFunc() {}
`)
	writeFile(t, filepath.Join(pkgDir, "other.go"), `package service

type Other struct{}
`)

	ctx, err := NewCollector().CollectWithOverlay(currentFile, root, `package service

type Unsaved struct{}

func UnsavedFunc(value string) error { return nil }
`)
	if err != nil {
		t.Fatalf("CollectWithOverlay returned error: %v", err)
	}

	if got := typeNames(ctx.Types); !reflect.DeepEqual(got, []string{"Unsaved:struct", "Other:struct"}) {
		t.Fatalf("types = %#v, want unsaved current file declarations plus other file", got)
	}
	if got := functionSignatures(ctx.Functions); !reflect.DeepEqual(got, []string{"UnsavedFunc(value string) error"}) {
		t.Fatalf("functions = %#v, want unsaved function", got)
	}
}

func TestCollectorCollectWithOverlayUsesOverlayPackageName(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "mixed")
	mustMkdir(t, pkgDir)
	currentFile := filepath.Join(pkgDir, "main.go")

	writeFile(t, currentFile, `package saved

func Saved() {}
`)
	writeFile(t, filepath.Join(pkgDir, "other.go"), `package unsaved

func Other() {}
`)

	ctx, err := NewCollector().CollectWithOverlay(currentFile, root, `package unsaved

func Current() {}
`)
	if err != nil {
		t.Fatalf("CollectWithOverlay returned error: %v", err)
	}

	if ctx.PackageName != "unsaved" {
		t.Fatalf("PackageName = %q, want unsaved", ctx.PackageName)
	}
	if got := functionSignatures(ctx.Functions); !reflect.DeepEqual(got, []string{"Current()", "Other()"}) {
		t.Fatalf("functions = %#v", got)
	}
}

func TestCollectorCollectWithOverlayReturnsParseError(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "service")
	mustMkdir(t, pkgDir)
	currentFile := filepath.Join(pkgDir, "service.go")
	writeFile(t, currentFile, `package service`)

	_, err := NewCollector().CollectWithOverlay(currentFile, root, `package service

func broken(`)
	if err == nil {
		t.Fatal("CollectWithOverlay returned nil error for invalid overlay")
	}
}

func TestCollectorCollectWithOverlayAtDetectsCurrentFunction(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "service")
	mustMkdir(t, pkgDir)
	currentFile := filepath.Join(pkgDir, "service.go")
	content := `package service

func First() {}

func Target(name string) error {
	return nil
}

func Last() {}
`
	writeFile(t, currentFile, content)

	offset := strings.Index(content, "return nil")
	ctx, err := NewCollector().CollectWithOverlayAt(currentFile, root, content, offset)
	if err != nil {
		t.Fatalf("CollectWithOverlayAt returned error: %v", err)
	}

	if ctx.Current == nil {
		t.Fatal("Current is nil, want Target")
	}
	if ctx.Current.Signature != "Target(name string) error" {
		t.Fatalf("Current.Signature = %q, want Target(name string) error", ctx.Current.Signature)
	}
}

func TestCollectorCollectWithOverlayAtDetectsCurrentMethod(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "service")
	mustMkdir(t, pkgDir)
	currentFile := filepath.Join(pkgDir, "service.go")
	content := `package service

type Server struct{}

func (s *Server) Handle(id string) error {
	return nil
}
`
	writeFile(t, currentFile, content)

	ctx, err := NewCollector().CollectWithOverlayAt(currentFile, root, content, strings.Index(content, "return nil"))
	if err != nil {
		t.Fatalf("CollectWithOverlayAt returned error: %v", err)
	}

	if ctx.Current == nil {
		t.Fatal("Current is nil, want method")
	}
	if ctx.Current.Signature != "(*Server) Handle(id string) error" {
		t.Fatalf("Current.Signature = %q", ctx.Current.Signature)
	}
}

func TestCollectorCollectWithOverlayAtNoCurrentFunction(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "service")
	mustMkdir(t, pkgDir)
	currentFile := filepath.Join(pkgDir, "service.go")
	content := `package service

type Server struct{}
`
	writeFile(t, currentFile, content)

	ctx, err := NewCollector().CollectWithOverlayAt(currentFile, root, content, strings.Index(content, "Server"))
	if err != nil {
		t.Fatalf("CollectWithOverlayAt returned error: %v", err)
	}

	if ctx.Current != nil {
		t.Fatalf("Current = %+v, want nil", ctx.Current)
	}
}

func TestCollectorCollectWithOverlayAtCollectsVisibleIdentifiers(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "service")
	mustMkdir(t, pkgDir)
	currentFile := filepath.Join(pkgDir, "service.go")
	content := `package service

type Server struct{}
type User struct{}

func (s *Server) Handle(ctx context.Context, id string) (err error) {
	var count int
	user := User{}
	repo := &Repository{}
	if hidden := true; hidden {
		branchValue := 1
		_ = branchValue
	}
	for index, item := range users {
		_ = item
		return nil
	}
	return err
}
`
	writeFile(t, currentFile, content)

	ctx, err := NewCollector().CollectWithOverlayAt(currentFile, root, content, strings.Index(content, "return nil"))
	if err != nil {
		t.Fatalf("CollectWithOverlayAt returned error: %v", err)
	}

	if got := visibleSummaries(ctx.Visible); !reflect.DeepEqual(got, []string{
		"s:*Server:receiver",
		"ctx:context.Context:parameter",
		"id:string:parameter",
		"err:error:result",
		"count:int:local variable",
		"user:User:local variable",
		"repo:*Repository:local variable",
		"index::range variable",
		"item::range variable",
	}) {
		t.Fatalf("visible identifiers = %#v", got)
	}
}

func TestCollectorCollectWithOverlayAtKeepsNestedBlockLocalsOutOfScope(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "service")
	mustMkdir(t, pkgDir)
	currentFile := filepath.Join(pkgDir, "service.go")
	content := `package service

func Handle() {
	if true {
		inside := 1
		_ = inside
	}
	after := 2
	_ = after
}
`
	writeFile(t, currentFile, content)

	ctx, err := NewCollector().CollectWithOverlayAt(currentFile, root, content, strings.Index(content, "_ = after"))
	if err != nil {
		t.Fatalf("CollectWithOverlayAt returned error: %v", err)
	}

	if got := visibleSummaries(ctx.Visible); !reflect.DeepEqual(got, []string{"after::local variable"}) {
		t.Fatalf("visible identifiers = %#v", got)
	}
}

func TestCollectorRanksPackageContextByRelevance(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "service")
	mustMkdir(t, pkgDir)
	currentFile := filepath.Join(pkgDir, "server.go")
	content := `package service

import (
	"fmt"
	alias "example.com/project/aliaspkg"
)

type Server struct{}

func (s *Server) Handle() {
	repo := &Repository{}
	alias.Use(repo)
	fmt.Println(repo)
	return
}
`
	writeFile(t, currentFile, content)
	writeFile(t, filepath.Join(pkgDir, "repository.go"), `package service

type Repository struct{}

type Account struct{}

func (s *Server) Shutdown() {}

func (a *Account) Save() {}

func BuildRepository() Repository { return Repository{} }
`)

	ctx, err := NewCollector().CollectWithOverlayAt(currentFile, root, content, strings.Index(content, "return"))
	if err != nil {
		t.Fatalf("CollectWithOverlayAt returned error: %v", err)
	}

	if got := functionSignatures(ctx.Functions); !reflect.DeepEqual(got, []string{
		"(*Server) Handle()",
		"(*Server) Shutdown()",
		"BuildRepository() Repository",
		"(*Account) Save()",
	}) {
		t.Fatalf("functions = %#v", got)
	}
	if got := functionSignatures(ctx.Siblings); !reflect.DeepEqual(got, []string{"(*Server) Shutdown()"}) {
		t.Fatalf("siblings = %#v", got)
	}
	if got := typeNames(ctx.Types); !reflect.DeepEqual(got, []string{"Server:struct", "Repository:struct", "Account:struct"}) {
		t.Fatalf("types = %#v", got)
	}
	if got := importSummaries(ctx.Imports); !reflect.DeepEqual(got, []string{
		`alias "example.com/project/aliaspkg" service/server.go`,
		` "fmt" service/server.go`,
	}) {
		t.Fatalf("imports = %#v", got)
	}
}

func TestCollectorDoesNotCollectSiblingMethodsForPlainFunction(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "service")
	mustMkdir(t, pkgDir)
	currentFile := filepath.Join(pkgDir, "service.go")
	content := `package service

type Server struct{}

func Handle() {
	return
}

func (s *Server) Shutdown() {}
`
	writeFile(t, currentFile, content)

	ctx, err := NewCollector().CollectWithOverlayAt(currentFile, root, content, strings.Index(content, "return"))
	if err != nil {
		t.Fatalf("CollectWithOverlayAt returned error: %v", err)
	}

	if len(ctx.Siblings) != 0 {
		t.Fatalf("siblings = %+v, want none", ctx.Siblings)
	}
}

func TestCollectorReturnsErrorForMissingCurrentFile(t *testing.T) {
	_, err := NewCollector().Collect(filepath.Join(t.TempDir(), "missing.go"), t.TempDir())
	if err == nil {
		t.Fatal("Collect returned nil error for missing current file")
	}
}

func TestCollectorReturnsErrorForDirectoryCurrentFile(t *testing.T) {
	root := t.TempDir()
	_, err := NewCollector().Collect(root, root)
	if err == nil {
		t.Fatal("Collect returned nil error for directory current file")
	}
}

func TestRelativePathFallsBackOutsideRoot(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	path := filepath.Join(other, "main.go")

	if got := relativePath(root, path); got != path {
		t.Fatalf("relativePath = %q, want absolute fallback %q", got, path)
	}
}

func fileRelativePaths(files []File) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.RelativePath)
	}
	return paths
}

func functionSignatures(functions []Function) []string {
	signatures := make([]string, 0, len(functions))
	for _, function := range functions {
		signatures = append(signatures, function.Signature)
	}
	return signatures
}

func typeNames(types []Type) []string {
	names := make([]string, 0, len(types))
	for _, typ := range types {
		names = append(names, typ.Name+":"+typ.Kind)
	}
	return names
}

func interfaceSummaries(interfaces []Interface) []string {
	summaries := make([]string, 0, len(interfaces))
	for _, iface := range interfaces {
		summaries = append(summaries, iface.Name+":"+strings.Join(iface.Methods, ","))
	}
	return summaries
}

func importSummaries(imports []Import) []string {
	summaries := make([]string, 0, len(imports))
	for _, imp := range imports {
		summaries = append(summaries, imp.Name+" \""+imp.Path+"\" "+imp.RelativeFile)
	}
	return summaries
}

func visibleSummaries(identifiers []VisibleIdentifier) []string {
	summaries := make([]string, 0, len(identifiers))
	for _, identifier := range identifiers {
		summaries = append(summaries, identifier.Name+":"+identifier.Type+":"+identifier.Kind)
	}
	return summaries
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
