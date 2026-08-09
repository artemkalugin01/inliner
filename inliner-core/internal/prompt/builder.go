package prompt

import (
	"fmt"
	"strings"

	"github.com/aokalugin/inliner/inliner-core/internal/completion"
	"github.com/aokalugin/inliner/inliner-core/internal/gocontext"
)

type Builder interface {
	Build(request completion.Request) string
}

const (
	DefaultMaxFiles            = 20
	DefaultMaxImports          = 80
	DefaultMaxTypes            = 80
	DefaultMaxInterfaces       = 40
	DefaultMaxInterfaceMethods = 12
	DefaultMaxVisible          = 80
	DefaultMaxSiblings         = 40
	DefaultMaxValues           = 80
	DefaultMaxFunctions        = 120
	DefaultMaxRecentEdits      = 5
)

type GoInlineBuilder struct {
	MaxFiles            int
	MaxImports          int
	MaxTypes            int
	MaxInterfaces       int
	MaxInterfaceMethods int
	MaxVisible          int
	MaxSiblings         int
	MaxValues           int
	MaxFunctions        int
	MaxRecentEdits      int
}

func (b GoInlineBuilder) Build(request completion.Request) string {
	b = b.withDefaults()

	var builder strings.Builder
	builder.WriteString(`You are an inline autocomplete engine for Go code.
Return only the exact Go code that should be inserted at <cursor>.
Do not include markdown, explanations, surrounding quotes, or code fences.
If no useful completion exists, return an empty response.
`)

	builder.WriteString("\nFile: ")
	builder.WriteString(request.FilePath)
	builder.WriteString("\n")

	if request.Package != nil {
		writePackageContext(&builder, *request.Package, b)
	}
	writeRecentEdits(&builder, request.RecentEdits, b.MaxRecentEdits)

	prefix := request.Prefix
	if request.Package != nil && len(request.Package.Imports) > 0 {
		prefix = trimPackageImportPreamble(prefix)
	}

	fmt.Fprintf(&builder, `
<prefix>
%s
</prefix>
<cursor>
<suffix>
%s
</suffix>
`, prefix, request.Suffix)

	return builder.String()
}

func (b GoInlineBuilder) withDefaults() GoInlineBuilder {
	if b.MaxFiles <= 0 {
		b.MaxFiles = DefaultMaxFiles
	}
	if b.MaxImports <= 0 {
		b.MaxImports = DefaultMaxImports
	}
	if b.MaxTypes <= 0 {
		b.MaxTypes = DefaultMaxTypes
	}
	if b.MaxInterfaces <= 0 {
		b.MaxInterfaces = DefaultMaxInterfaces
	}
	if b.MaxInterfaceMethods <= 0 {
		b.MaxInterfaceMethods = DefaultMaxInterfaceMethods
	}
	if b.MaxVisible <= 0 {
		b.MaxVisible = DefaultMaxVisible
	}
	if b.MaxSiblings <= 0 {
		b.MaxSiblings = DefaultMaxSiblings
	}
	if b.MaxValues <= 0 {
		b.MaxValues = DefaultMaxValues
	}
	if b.MaxFunctions <= 0 {
		b.MaxFunctions = DefaultMaxFunctions
	}
	if b.MaxRecentEdits <= 0 {
		b.MaxRecentEdits = DefaultMaxRecentEdits
	}
	return b
}

func writeRecentEdits(builder *strings.Builder, edits []completion.RecentEdit, maxEdits int) {
	if len(edits) == 0 {
		return
	}
	builder.WriteString("\nRecent similar edits:\n")
	for _, edit := range edits[:min(len(edits), maxEdits)] {
		builder.WriteString("- ")
		builder.WriteString(edit.RelativePath)
		builder.WriteString(" lines ")
		builder.WriteString(fmt.Sprintf("%d-%d", edit.StartLine, edit.EndLine))
		builder.WriteString(":\n")
		builder.WriteString("```diff\n")
		writeDiffLines(builder, "- ", edit.Before)
		writeDiffLines(builder, "+ ", edit.After)
		builder.WriteString("```\n")
	}
	writeOmitted(builder, len(edits), maxEdits, "recent edits")
}

func writeDiffLines(builder *strings.Builder, prefix string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	for _, line := range strings.Split(strings.TrimRight(value, "\n"), "\n") {
		builder.WriteString(prefix)
		builder.WriteString(line)
		builder.WriteString("\n")
	}
}

func writePackageContext(builder *strings.Builder, ctx gocontext.PackageContext, budget GoInlineBuilder) {
	builder.WriteString("\nCurrent package context:\n")
	builder.WriteString("Package: ")
	builder.WriteString(ctx.PackageName)
	builder.WriteString("\n")

	if len(ctx.Files) > 0 {
		builder.WriteString("Files:\n")
		for _, file := range ctx.Files[:min(len(ctx.Files), budget.MaxFiles)] {
			builder.WriteString("- ")
			builder.WriteString(file.RelativePath)
			builder.WriteString("\n")
		}
		writeOmitted(builder, len(ctx.Files), budget.MaxFiles, "files")
	}

	if ctx.Current != nil {
		builder.WriteString("Current function or method:\n")
		builder.WriteString("- ")
		builder.WriteString(ctx.Current.Signature)
		builder.WriteString("\n")
	}

	if len(ctx.Visible) > 0 {
		builder.WriteString("Visible identifiers:\n")
		for _, identifier := range ctx.Visible[:min(len(ctx.Visible), budget.MaxVisible)] {
			builder.WriteString("- ")
			builder.WriteString(identifier.Name)
			if identifier.Type != "" {
				builder.WriteString(" ")
				builder.WriteString(identifier.Type)
			}
			if identifier.Kind != "" {
				builder.WriteString(" ")
				builder.WriteString(identifier.Kind)
			}
			builder.WriteString("\n")
		}
		writeOmitted(builder, len(ctx.Visible), budget.MaxVisible, "visible identifiers")
	}

	if len(ctx.Siblings) > 0 {
		builder.WriteString("Sibling methods for current receiver:\n")
		for _, fn := range ctx.Siblings[:min(len(ctx.Siblings), budget.MaxSiblings)] {
			builder.WriteString("- ")
			builder.WriteString(fn.Signature)
			builder.WriteString("\n")
		}
		writeOmitted(builder, len(ctx.Siblings), budget.MaxSiblings, "sibling methods")
	}

	if len(ctx.Imports) > 0 {
		builder.WriteString("Imports:\n")
		for _, imp := range ctx.Imports[:min(len(ctx.Imports), budget.MaxImports)] {
			builder.WriteString("- ")
			if imp.Name != "" {
				builder.WriteString(imp.Name)
				builder.WriteString(" ")
			}
			builder.WriteString(strconvQuote(imp.Path))
			if imp.RelativeFile != "" {
				builder.WriteString(" from ")
				builder.WriteString(imp.RelativeFile)
			}
			builder.WriteString("\n")
		}
		writeOmitted(builder, len(ctx.Imports), budget.MaxImports, "imports")
	}

	if len(ctx.Types) > 0 {
		builder.WriteString("Types:\n")
		for _, typ := range ctx.Types[:min(len(ctx.Types), budget.MaxTypes)] {
			builder.WriteString("- ")
			builder.WriteString(typ.Name)
			builder.WriteString(" ")
			builder.WriteString(typ.Kind)
			builder.WriteString("\n")
		}
		writeOmitted(builder, len(ctx.Types), budget.MaxTypes, "types")
	}

	if len(ctx.Values) > 0 {
		builder.WriteString("Package constants and variables:\n")
		for _, value := range ctx.Values[:min(len(ctx.Values), budget.MaxValues)] {
			builder.WriteString("- ")
			builder.WriteString(value.Kind)
			builder.WriteString(" ")
			builder.WriteString(value.Name)
			if value.Type != "" {
				builder.WriteString(" ")
				builder.WriteString(value.Type)
			}
			if value.Value != "" {
				builder.WriteString(" = ")
				builder.WriteString(value.Value)
			}
			if value.RelativeFile != "" {
				builder.WriteString(" from ")
				builder.WriteString(value.RelativeFile)
			}
			builder.WriteString("\n")
		}
		writeOmitted(builder, len(ctx.Values), budget.MaxValues, "package values")
	}

	if len(ctx.Interfaces) > 0 {
		builder.WriteString("Interfaces:\n")
		for _, iface := range ctx.Interfaces[:min(len(ctx.Interfaces), budget.MaxInterfaces)] {
			builder.WriteString("- ")
			builder.WriteString(iface.Name)
			builder.WriteString("\n")
			for _, method := range iface.Methods[:min(len(iface.Methods), budget.MaxInterfaceMethods)] {
				builder.WriteString("  - ")
				builder.WriteString(method)
				builder.WriteString("\n")
			}
			writeOmitted(builder, len(iface.Methods), budget.MaxInterfaceMethods, "interface methods")
		}
		writeOmitted(builder, len(ctx.Interfaces), budget.MaxInterfaces, "interfaces")
	}

	if len(ctx.Functions) > 0 {
		builder.WriteString("Functions and methods:\n")
		for _, fn := range ctx.Functions[:min(len(ctx.Functions), budget.MaxFunctions)] {
			builder.WriteString("- ")
			builder.WriteString(fn.Signature)
			builder.WriteString("\n")
		}
		writeOmitted(builder, len(ctx.Functions), budget.MaxFunctions, "functions")
	}
}

func trimPackageImportPreamble(prefix string) string {
	start := skipSpace(prefix, 0)
	if !strings.HasPrefix(prefix[start:], "package ") {
		return prefix
	}

	afterPackage := strings.IndexByte(prefix[start:], '\n')
	if afterPackage < 0 {
		return ""
	}
	pos := start + afterPackage + 1
	pos = skipSpace(prefix, pos)

	if !strings.HasPrefix(prefix[pos:], "import") {
		return prefix[pos:]
	}

	pos += len("import")
	pos = skipSpace(prefix, pos)
	if pos < len(prefix) && prefix[pos] == '(' {
		end := strings.Index(prefix[pos:], ")")
		if end < 0 {
			return ""
		}
		pos += end + 1
	} else {
		end := strings.IndexByte(prefix[pos:], '\n')
		if end < 0 {
			return ""
		}
		pos += end + 1
	}

	if pos < len(prefix) && prefix[pos] == '\n' {
		pos++
	}
	return prefix[pos:]
}

func skipSpace(value string, pos int) int {
	for pos < len(value) {
		switch value[pos] {
		case ' ', '\t', '\r', '\n':
			pos++
		default:
			return pos
		}
	}
	return pos
}

func strconvQuote(value string) string {
	return fmt.Sprintf("%q", value)
}

func writeOmitted(builder *strings.Builder, total int, kept int, label string) {
	if total <= kept {
		return
	}
	builder.WriteString("- ... ")
	builder.WriteString(fmt.Sprintf("%d more %s omitted", total-kept, label))
	builder.WriteString("\n")
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
