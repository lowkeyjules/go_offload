package offload

import (
	"bytes"
	_ "embed"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

//go:embed template.txt
var wasmTemplate string

type funcMeta struct {
	payload string
}

type fileParser struct {
	fileToParse string
	funcMetas   map[string]funcMeta
}

func newFileParser(file string) *fileParser {
	p := &fileParser{fileToParse: file, funcMetas: make(map[string]funcMeta)}
	p.parse()
	return p
}

func (p *fileParser) parse() {
	makeDir, err := offloadblesDir()
	if err != nil {
		log.Fatal(err)
	}

	dirOffloadables := filepath.Join(makeDir, "offloadables")
	dirTarget := filepath.Join(makeDir, "targetfiles")

	os.Mkdir(dirOffloadables, 0755)
	os.Mkdir(dirTarget, 0755)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, p.fileToParse, nil, parser.ParseComments)
	if err != nil {
		log.Fatal(err)
	}

	typeDecls := collectTypeDecls(fset, file)

	// all imports of the parsed file
	available := fileImports(file)

	typeImports := make(map[string]string)
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		usedImports(gd, available, typeImports)
	}

	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}

		if fn.Doc == nil {
			return true
		}

		if err != nil {
			log.Fatal(err)
		}

		// checks all comments
		for _, comment := range fn.Doc.List {
			if strings.HasPrefix(comment.Text, "// offload") || strings.HasPrefix(comment.Text, "//offload") {

				code := wasmTemplate

				payload, err := payloadType(file, fn)
				if err != nil {
					log.Fatalf("offload %s: %v", fn.Name.Name, err)
				}

				p.funcMetas[fn.Name.Name] = funcMeta{payload: payload}

				used := make(map[string]string)
				for local, path := range typeImports {
					used[local] = path
				}
				usedImports(fn, available, used)

				var fnBuf bytes.Buffer
				printer.Fprint(&fnBuf, fset, fn)

				code = strings.ReplaceAll(code, "{{FUNC_BODY}}", fnBuf.String())
				code = strings.ReplaceAll(code, "{{FUNC_NAME}}", fn.Name.Name)
				code = strings.ReplaceAll(code, "{{PAYLOAD}}", payload)
				code = strings.ReplaceAll(code, "{{TYPE_DECLS}}", typeDecls)
				code = strings.ReplaceAll(code, "{{IMPORTS}}", renderImports(used))

				// formatting for readability
				if pretty, ferr := format.Source([]byte(code)); ferr == nil {
					code = string(pretty)
				}

				input := filepath.Join(dirTarget, fn.Name.Name+".go")
				output := filepath.Join(dirOffloadables, fn.Name.Name+".wasm")

				os.WriteFile(
					input,
					[]byte(code),
					0644,
				)

				// build only works from file
				err = buildWasm(input, output)
				if err != nil {
					panic(err)
				}

				log.Println("Wasm built:", output)

				// TODO comment out for debug
				//os.Remove(input)
			}
		}
		return true
	})
}

func payloadType(file *ast.File, fn *ast.FuncDecl) (string, error) {

	params := fn.Type.Params.List
	if len(params) != 1 || len(params[0].Names) > 1 {
		return "", fmt.Errorf("requires exactly one parameter (the payload struct)")
	}

	ident, ok := params[0].Type.(*ast.Ident)
	if !ok {
		return "", fmt.Errorf("input struct needs to be declared within the parsed file")
	}

	st := findStruct(file, ident.Name)
	if st == nil {
		return "", fmt.Errorf("parameter %s is not declared in the parsed file", ident.Name)
	}

	results := fn.Type.Results
	if results == nil || len(results.List) != 1 {
		return "", fmt.Errorf("function needs to return a single result type")
	}

	return ident.Name, nil
}

func findStruct(file *ast.File, name string) *ast.StructType {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != name {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return nil
			}
			return st
		}
	}
	return nil
}

func fileImports(file *ast.File) map[string]string {
	imports := make(map[string]string)
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		local := lastElem(path)
		if imp.Name != nil {
			if imp.Name.Name == "_" || imp.Name.Name == "." {
				continue
			}
			local = imp.Name.Name
		}
		imports[local] = path
	}
	return imports
}

func usedImports(node ast.Node, available map[string]string, used map[string]string) {
	ast.Inspect(node, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if path, ok := available[ident.Name]; ok {
			used[ident.Name] = path
		}
		return true
	})
}

func renderImports(used map[string]string) string {
	// mapping path to alias
	paths := map[string]string{
		"bytes":           "",
		"encoding/base64": "",
		"encoding/gob":    "",
		"fmt":             "",
		"os":              "",
	}

	for local, path := range used {
		if _, ok := paths[path]; ok {
			continue
		}
		if local == lastElem(path) {
			paths[path] = ""
		} else {
			paths[path] = local
		}
	}

	sorted := make([]string, 0, len(paths))
	for path := range paths {
		sorted = append(sorted, path)
	}
	sort.Strings(sorted)

	var b strings.Builder
	b.WriteString("import (\n")
	for _, path := range sorted {
		if alias := paths[path]; alias != "" {
			fmt.Fprintf(&b, "\t%s %q\n", alias, path)
		} else {
			fmt.Fprintf(&b, "\t%q\n", path)
		}
	}
	b.WriteString(")")

	return b.String()
}

func lastElem(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func buildWasm(inputFile string, outputFile string) error {
	cmd := exec.Command(
		"go", "build",
		"-o", outputFile,
		inputFile,
	)

	cmd.Env = append(os.Environ(),
		"GOOS=wasip1",
		"GOARCH=wasm",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, string(out))
	}

	return nil
}

func collectTypeDecls(fset *token.FileSet, file *ast.File) string {
	var buf bytes.Buffer
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		printer.Fprint(&buf, fset, gd)
		buf.WriteString("\n\n")
	}
	return buf.String()
}

func offloadblesDir() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("could not determine cache dir: %w", err)
	}
	dir := filepath.Join(cacheDir, "go_offload")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("could not create go_offload directory: %w", err)
	}

	return dir, nil
}
