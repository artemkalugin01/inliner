package gocontext

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type PackageContext struct {
	PackageName string
	Files       []File
	Imports     []Import
	Current     *Function
	Visible     []VisibleIdentifier
	Functions   []Function
	Types       []Type
	Interfaces  []Interface
}

type File struct {
	Path         string
	RelativePath string
}

type Import struct {
	Path         string
	Name         string
	File         string
	RelativeFile string
}

type Function struct {
	Name      string
	Receiver  string
	Signature string
}

type VisibleIdentifier struct {
	Name string
	Type string
	Kind string
}

type Type struct {
	Name string
	Kind string
}

type Interface struct {
	Name    string
	Methods []string
}

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Collect(currentFile string, projectRoot string) (PackageContext, error) {
	return c.collect(currentFile, projectRoot, nil, -1)
}

func (c *Collector) CollectWithOverlay(currentFile string, projectRoot string, content string) (PackageContext, error) {
	return c.collect(currentFile, projectRoot, &content, -1)
}

func (c *Collector) CollectWithOverlayAt(currentFile string, projectRoot string, content string, offset int) (PackageContext, error) {
	return c.collect(currentFile, projectRoot, &content, offset)
}

func (c *Collector) collect(currentFile string, projectRoot string, overlay *string, offset int) (PackageContext, error) {
	currentFile = filepath.Clean(currentFile)
	projectRoot = filepath.Clean(projectRoot)

	info, err := os.Stat(currentFile)
	if err != nil {
		return PackageContext{}, err
	}
	if info.IsDir() {
		return PackageContext{}, fmt.Errorf("current file %q is a directory", currentFile)
	}

	dir := filepath.Dir(currentFile)
	set := token.NewFileSet()
	packages, err := parser.ParseDir(set, dir, includeSourceFile, parser.SkipObjectResolution)
	if err != nil {
		return PackageContext{}, err
	}

	currentPackage, err := packageNameForFile(set, currentFile, overlay)
	if err != nil {
		return PackageContext{}, err
	}

	pkg, ok := packages[currentPackage]
	if !ok {
		return PackageContext{}, fmt.Errorf("package %q not found in %q", currentPackage, dir)
	}

	ctx := PackageContext{PackageName: currentPackage}
	seenCurrentFile := false
	for path, file := range pkg.Files {
		if filepath.Clean(path) == currentFile && overlay != nil {
			overlayFile, err := parser.ParseFile(set, currentFile, *overlay, parser.SkipObjectResolution)
			if err != nil {
				return PackageContext{}, err
			}
			file = overlayFile
			seenCurrentFile = true
		}
		ctx.Files = append(ctx.Files, File{Path: path, RelativePath: relativePath(projectRoot, path)})
		c.collectFile(set, file, &ctx)
		if filepath.Clean(path) == currentFile {
			ctx.Current, ctx.Visible = currentFunctionContextAtOffset(set, file, offset)
		}
	}

	if overlay != nil && !seenCurrentFile {
		overlayFile, err := parser.ParseFile(set, currentFile, *overlay, parser.SkipObjectResolution)
		if err != nil {
			return PackageContext{}, err
		}
		ctx.Files = append(ctx.Files, File{Path: currentFile, RelativePath: relativePath(projectRoot, currentFile)})
		c.collectFile(set, overlayFile, &ctx)
		ctx.Current, ctx.Visible = currentFunctionContextAtOffset(set, overlayFile, offset)
	}

	sortPackageContext(&ctx)
	return ctx, nil
}

func currentFunctionContextAtOffset(set *token.FileSet, file *ast.File, offset int) (*Function, []VisibleIdentifier) {
	if offset < 0 {
		return nil, nil
	}
	tokenFile := set.File(file.Pos())
	if tokenFile == nil {
		return nil, nil
	}
	if offset > tokenFile.Size() {
		offset = tokenFile.Size()
	}
	cursor := tokenFile.Pos(offset)

	var current *Function
	var visible []VisibleIdentifier
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if fn.Pos() <= cursor && cursor <= fn.End() {
			function := functionFromDecl(set, fn)
			current = &function
			visible = visibleIdentifiersAtOffset(set, fn, cursor)
		}
	}
	return current, visible
}

func includeSourceFile(info os.FileInfo) bool {
	name := info.Name()
	return !info.IsDir() && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

func packageNameForFile(set *token.FileSet, path string, overlay *string) (string, error) {
	var src any
	if overlay != nil {
		src = *overlay
	}

	file, err := parser.ParseFile(set, path, src, parser.PackageClauseOnly)
	if err != nil {
		return "", err
	}
	return file.Name.Name, nil
}

func (c *Collector) collectFile(set *token.FileSet, file *ast.File, ctx *PackageContext) {
	ctx.Imports = append(ctx.Imports, importsFromFile(set, file)...)

	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			ctx.Functions = append(ctx.Functions, functionFromDecl(set, decl))
		case *ast.GenDecl:
			if decl.Tok == token.TYPE {
				collectTypeDecl(set, decl, ctx)
			}
		}
	}
}

func importsFromFile(set *token.FileSet, file *ast.File) []Import {
	imports := make([]Import, 0, len(file.Imports))
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			path = spec.Path.Value
		}

		name := ""
		if spec.Name != nil {
			name = spec.Name.Name
		}

		position := set.Position(spec.Pos())
		imports = append(imports, Import{Path: path, Name: name, File: position.Filename})
	}
	return imports
}

func functionFromDecl(set *token.FileSet, decl *ast.FuncDecl) Function {
	return Function{
		Name:      decl.Name.Name,
		Receiver:  receiverString(set, decl.Recv),
		Signature: funcSignature(set, decl),
	}
}

func visibleIdentifiersAtOffset(set *token.FileSet, fn *ast.FuncDecl, cursor token.Pos) []VisibleIdentifier {
	seen := map[string]int{}
	var identifiers []VisibleIdentifier
	add := func(identifier VisibleIdentifier) {
		if identifier.Name == "" || identifier.Name == "_" {
			return
		}
		if index, ok := seen[identifier.Name]; ok {
			identifiers[index] = identifier
			return
		}
		seen[identifier.Name] = len(identifiers)
		identifiers = append(identifiers, identifier)
	}

	for _, identifier := range fieldIdentifiers(set, fn.Recv, "receiver") {
		add(identifier)
	}
	for _, identifier := range fieldIdentifiers(set, fn.Type.Params, "parameter") {
		add(identifier)
	}
	for _, identifier := range fieldIdentifiers(set, fn.Type.Results, "result") {
		add(identifier)
	}

	collectVisibleInBlock(set, fn.Body, cursor, add)
	return identifiers
}

func fieldIdentifiers(set *token.FileSet, fields *ast.FieldList, kind string) []VisibleIdentifier {
	if fields == nil {
		return nil
	}

	var identifiers []VisibleIdentifier
	for _, field := range fields.List {
		if len(field.Names) == 0 {
			continue
		}
		fieldType := nodeString(set, field.Type)
		for _, name := range field.Names {
			identifiers = append(identifiers, VisibleIdentifier{Name: name.Name, Type: fieldType, Kind: kind})
		}
	}
	return identifiers
}

func collectVisibleInBlock(set *token.FileSet, block *ast.BlockStmt, cursor token.Pos, add func(VisibleIdentifier)) {
	if block == nil || cursor < block.Pos() || cursor > block.End() {
		return
	}

	for _, stmt := range block.List {
		if stmt.Pos() > cursor {
			return
		}
		if stmt.End() < cursor {
			collectSameScopeIdentifiers(set, stmt, add)
			continue
		}
		collectContainedScopeIdentifiers(set, stmt, cursor, add)
		return
	}
}

func collectSameScopeIdentifiers(set *token.FileSet, stmt ast.Stmt, add func(VisibleIdentifier)) {
	switch stmt := stmt.(type) {
	case *ast.DeclStmt:
		collectDeclIdentifiers(set, stmt.Decl, add)
	case *ast.AssignStmt:
		collectShortAssignIdentifiers(set, stmt, add)
	}
}

func collectContainedScopeIdentifiers(set *token.FileSet, stmt ast.Stmt, cursor token.Pos, add func(VisibleIdentifier)) {
	switch stmt := stmt.(type) {
	case *ast.DeclStmt:
		if stmt.End() <= cursor {
			collectDeclIdentifiers(set, stmt.Decl, add)
		}
	case *ast.AssignStmt:
		if stmt.End() <= cursor {
			collectShortAssignIdentifiers(set, stmt, add)
		}
	case *ast.IfStmt:
		if stmt.Init != nil && stmt.Init.End() < cursor {
			collectSameScopeIdentifiers(set, stmt.Init, add)
		}
		collectVisibleInBlock(set, stmt.Body, cursor, add)
		if stmt.Else != nil && stmt.Else.Pos() <= cursor && cursor <= stmt.Else.End() {
			collectContainedElseIdentifiers(set, stmt.Else, cursor, add)
		}
	case *ast.ForStmt:
		if stmt.Init != nil && stmt.Init.End() < cursor {
			collectSameScopeIdentifiers(set, stmt.Init, add)
		}
		collectVisibleInBlock(set, stmt.Body, cursor, add)
	case *ast.RangeStmt:
		if stmt.Body != nil && stmt.Body.Pos() <= cursor && cursor <= stmt.Body.End() {
			collectRangeIdentifiers(set, stmt, add)
			collectVisibleInBlock(set, stmt.Body, cursor, add)
		}
	case *ast.SwitchStmt:
		if stmt.Init != nil && stmt.Init.End() < cursor {
			collectSameScopeIdentifiers(set, stmt.Init, add)
		}
		collectCaseClauseIdentifiers(set, stmt.Body, cursor, add)
	case *ast.TypeSwitchStmt:
		if stmt.Init != nil && stmt.Init.End() < cursor {
			collectSameScopeIdentifiers(set, stmt.Init, add)
		}
		collectCaseClauseIdentifiers(set, stmt.Body, cursor, add)
	case *ast.SelectStmt:
		collectCaseClauseIdentifiers(set, stmt.Body, cursor, add)
	case *ast.BlockStmt:
		collectVisibleInBlock(set, stmt, cursor, add)
	}
}

func collectContainedElseIdentifiers(set *token.FileSet, stmt ast.Stmt, cursor token.Pos, add func(VisibleIdentifier)) {
	switch stmt := stmt.(type) {
	case *ast.BlockStmt:
		collectVisibleInBlock(set, stmt, cursor, add)
	case *ast.IfStmt:
		collectContainedScopeIdentifiers(set, stmt, cursor, add)
	}
}

func collectCaseClauseIdentifiers(set *token.FileSet, block *ast.BlockStmt, cursor token.Pos, add func(VisibleIdentifier)) {
	if block == nil || cursor < block.Pos() || cursor > block.End() {
		return
	}
	for _, stmt := range block.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok || cursor < clause.Pos() || cursor > clause.End() {
			continue
		}
		for _, clauseStmt := range clause.Body {
			if clauseStmt.Pos() > cursor {
				return
			}
			if clauseStmt.End() < cursor {
				collectSameScopeIdentifiers(set, clauseStmt, add)
				continue
			}
			collectContainedScopeIdentifiers(set, clauseStmt, cursor, add)
			return
		}
	}
}

func collectDeclIdentifiers(set *token.FileSet, decl ast.Decl, add func(VisibleIdentifier)) {
	genDecl, ok := decl.(*ast.GenDecl)
	if !ok || genDecl.Tok != token.VAR {
		return
	}
	for _, spec := range genDecl.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for i, name := range valueSpec.Names {
			identifierType := ""
			if valueSpec.Type != nil {
				identifierType = nodeString(set, valueSpec.Type)
			} else if i < len(valueSpec.Values) {
				identifierType = inferredExprType(set, valueSpec.Values[i])
			}
			add(VisibleIdentifier{Name: name.Name, Type: identifierType, Kind: "local variable"})
		}
	}
}

func collectShortAssignIdentifiers(set *token.FileSet, stmt *ast.AssignStmt, add func(VisibleIdentifier)) {
	if stmt.Tok != token.DEFINE {
		return
	}
	for i, lhs := range stmt.Lhs {
		name, ok := lhs.(*ast.Ident)
		if !ok {
			continue
		}
		identifierType := ""
		if i < len(stmt.Rhs) {
			identifierType = inferredExprType(set, stmt.Rhs[i])
		}
		add(VisibleIdentifier{Name: name.Name, Type: identifierType, Kind: "local variable"})
	}
}

func collectRangeIdentifiers(set *token.FileSet, stmt *ast.RangeStmt, add func(VisibleIdentifier)) {
	if stmt.Tok != token.DEFINE {
		return
	}
	if name, ok := stmt.Key.(*ast.Ident); ok {
		add(VisibleIdentifier{Name: name.Name, Kind: "range variable"})
	}
	if name, ok := stmt.Value.(*ast.Ident); ok {
		add(VisibleIdentifier{Name: name.Name, Kind: "range variable"})
	}
}

func inferredExprType(set *token.FileSet, expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.CompositeLit:
		return nodeString(set, expr.Type)
	case *ast.UnaryExpr:
		if expr.Op == token.AND {
			if lit, ok := expr.X.(*ast.CompositeLit); ok {
				return "*" + nodeString(set, lit.Type)
			}
		}
	case *ast.CallExpr:
		return "result of " + nodeString(set, expr.Fun)
	}
	return ""
}

func collectTypeDecl(set *token.FileSet, decl *ast.GenDecl, ctx *PackageContext) {
	for _, spec := range decl.Specs {
		typeSpec, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}

		switch typ := typeSpec.Type.(type) {
		case *ast.StructType:
			ctx.Types = append(ctx.Types, Type{Name: typeSpec.Name.Name, Kind: "struct"})
		case *ast.InterfaceType:
			ctx.Interfaces = append(ctx.Interfaces, Interface{Name: typeSpec.Name.Name, Methods: interfaceMethods(set, typ)})
		default:
			ctx.Types = append(ctx.Types, Type{Name: typeSpec.Name.Name, Kind: "alias"})
		}
	}
}

func receiverString(set *token.FileSet, fields *ast.FieldList) string {
	if fields == nil || len(fields.List) == 0 {
		return ""
	}
	return nodeString(set, fields.List[0].Type)
}

func funcSignature(set *token.FileSet, decl *ast.FuncDecl) string {
	var builder strings.Builder
	if decl.Recv != nil {
		builder.WriteString("(")
		builder.WriteString(receiverString(set, decl.Recv))
		builder.WriteString(") ")
	}
	builder.WriteString(decl.Name.Name)
	builder.WriteString(fieldListString(set, decl.Type.Params))
	if decl.Type.Results != nil && len(decl.Type.Results.List) > 0 {
		builder.WriteString(" ")
		builder.WriteString(resultListString(set, decl.Type.Results))
	}
	return builder.String()
}

func interfaceMethods(set *token.FileSet, typ *ast.InterfaceType) []string {
	var methods []string
	if typ.Methods == nil {
		return methods
	}

	for _, field := range typ.Methods.List {
		if len(field.Names) == 0 {
			methods = append(methods, nodeString(set, field.Type))
			continue
		}

		funcType, ok := field.Type.(*ast.FuncType)
		if !ok {
			for _, name := range field.Names {
				methods = append(methods, name.Name+" "+nodeString(set, field.Type))
			}
			continue
		}

		for _, name := range field.Names {
			methods = append(methods, name.Name+fieldListString(set, funcType.Params)+resultSuffix(set, funcType.Results))
		}
	}

	sort.Strings(methods)
	return methods
}

func resultSuffix(set *token.FileSet, results *ast.FieldList) string {
	if results == nil || len(results.List) == 0 {
		return ""
	}
	return " " + resultListString(set, results)
}

func fieldListString(set *token.FileSet, fields *ast.FieldList) string {
	if fields == nil || len(fields.List) == 0 {
		return "()"
	}

	parts := make([]string, 0, len(fields.List))
	for _, field := range fields.List {
		fieldType := nodeString(set, field.Type)
		if len(field.Names) == 0 {
			parts = append(parts, fieldType)
			continue
		}

		names := make([]string, 0, len(field.Names))
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
		parts = append(parts, strings.Join(names, ", ")+" "+fieldType)
	}

	return "(" + strings.Join(parts, ", ") + ")"
}

func resultListString(set *token.FileSet, results *ast.FieldList) string {
	if results == nil || len(results.List) == 0 {
		return ""
	}

	parts := make([]string, 0, len(results.List))
	for _, field := range results.List {
		fieldType := nodeString(set, field.Type)
		if len(field.Names) == 0 {
			parts = append(parts, fieldType)
			continue
		}

		names := make([]string, 0, len(field.Names))
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
		parts = append(parts, strings.Join(names, ", ")+" "+fieldType)
	}

	if len(parts) == 1 && len(results.List[0].Names) == 0 {
		return parts[0]
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func nodeString(set *token.FileSet, node ast.Node) string {
	var buf bytes.Buffer
	if err := format.Node(&buf, set, node); err != nil {
		return ""
	}
	return buf.String()
}

func relativePath(root string, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return rel
}

func sortPackageContext(ctx *PackageContext) {
	sort.Slice(ctx.Files, func(i, j int) bool { return ctx.Files[i].RelativePath < ctx.Files[j].RelativePath })
	for i := range ctx.Imports {
		ctx.Imports[i].RelativeFile = relativePathForKnownFiles(ctx.Files, ctx.Imports[i].File)
	}
	sort.Slice(ctx.Imports, func(i, j int) bool {
		if ctx.Imports[i].RelativeFile == ctx.Imports[j].RelativeFile {
			if ctx.Imports[i].Path == ctx.Imports[j].Path {
				return ctx.Imports[i].Name < ctx.Imports[j].Name
			}
			return ctx.Imports[i].Path < ctx.Imports[j].Path
		}
		return ctx.Imports[i].RelativeFile < ctx.Imports[j].RelativeFile
	})
	sort.Slice(ctx.Functions, func(i, j int) bool { return ctx.Functions[i].Signature < ctx.Functions[j].Signature })
	sort.Slice(ctx.Types, func(i, j int) bool { return ctx.Types[i].Name < ctx.Types[j].Name })
	sort.Slice(ctx.Interfaces, func(i, j int) bool { return ctx.Interfaces[i].Name < ctx.Interfaces[j].Name })
}

func relativePathForKnownFiles(files []File, path string) string {
	for _, file := range files {
		if filepath.Clean(file.Path) == filepath.Clean(path) {
			return file.RelativePath
		}
	}
	return path
}
