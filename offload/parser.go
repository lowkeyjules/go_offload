package offload

import (
	"bytes"
	_ "embed"
	"fmt"
	"go/ast"
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

// newFileParser returns new fileParser, essentially just passing down data from orchestrator.
func newFileParser(file string) *fileParser {
	p := &fileParser{fileToParse: file, funcMetas: make(map[string]funcMeta)}
	p.parse()
	return p
}

// parse parses target file and extracts vital information for the building of Wasm modules.
// Like imports, helper functions, offloaded function and structs.
func (p *fileParser) parse() {
	makeDir, err := makeOffloadablesDir()
	if err != nil {
		log.Fatal(err)
	}

	dirOffloadables := filepath.Join(makeDir, "offloadables")
	dirTarget := filepath.Join(makeDir, "targetfiles")

	os.Mkdir(dirOffloadables, 0755)
	os.Mkdir(dirTarget, 0755)

	// parsing starts here
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, p.fileToParse, nil, parser.ParseComments)
	if err != nil {
		log.Fatal(err)
	}

	// all struct declarations
	var typeDecls string
	typeDecls = getTypeDecls(fset, file)

	// all imports of the parsed file
	available := fileImports(file)
	typeImports := returnTypeImports(file, available)

	// Finds all declared functions (not methods) of parsed file
	allDeclFuncs := returnAllDeclFuncs(file)

	// find fn with Doc != nil
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
				// code template
				code := wasmTemplate

				// helper functions
				helperFuncs := getHelperFuncs(allDeclFuncs, fn)

				var helpFuncBuf bytes.Buffer
				fillHelpFuncBuf(&helpFuncBuf, fset, helperFuncs)

				// check input of functions
				inputOffloadable, err := inputTypeOffloadable(file, fn)
				if err != nil {
					log.Fatalf("offload %s: wrong input format: %v", fn.Name.Name, err)
				}

				// write offloadable into funcMetas
				p.funcMetas[fn.Name.Name] = funcMeta{payload: inputOffloadable}

				// Imports
				used := make(map[string]string)
				for loc, path := range typeImports {
					used[loc] = path
				}
				// find used in fn
				usedImports(fn, available, used)

				// write function into buffer
				var fnBuf bytes.Buffer
				printer.Fprint(&fnBuf, fset, fn)

				code = strings.ReplaceAll(code, "{{IMPORTS}}", writeImports(used))
				code = strings.ReplaceAll(code, "{{TYPE_DECLS}}", typeDecls)
				code = strings.ReplaceAll(code, "{{HELPER_FUNCS}}", helpFuncBuf.String())
				code = strings.ReplaceAll(code, "{{FUNC_BODY}}", fnBuf.String())
				code = strings.ReplaceAll(code, "{{FUNC_NAME}}", fn.Name.Name)
				code = strings.ReplaceAll(code, "{{PAYLOAD}}", inputOffloadable)

				input := filepath.Join(dirTarget, fn.Name.Name+".go")
				output := filepath.Join(dirOffloadables, fn.Name.Name+".wasm")

				os.WriteFile(
					input,
					[]byte(code),
					0755,
				)

				// build only works from file
				err = buildWasm(input, output)
				if err != nil {
					panic(err)
				}

				log.Println("Wasm built:", output)

				// TODO comment out for debug
				//os.Remove(output)
			}
		}
		return true
	})
}

// inputTypeOffloadable checks whether the input into the function is valid struct.
func inputTypeOffloadable(file *ast.File, fn *ast.FuncDecl) (string, error) {
	// get input params from funcdecl
	params := fn.Type.Params.List
	// get results
	results := fn.Type.Results

	if len(params) != 1 {
		return "", fmt.Errorf("requires exactly one parameter (the payload struct)")
	}
	// get type from first (and only) input param
	ident, ok := params[0].Type.(*ast.Ident)
	if !ok {
		return "", fmt.Errorf("input struct needs to be declared within the parsed file")
	}

	// check if struct is in parsed file
	inFile := structInFile(file, ident.Name)
	if !inFile {
		return "", fmt.Errorf("parameter %s is not declared within the parsed file", ident.Name)
	}

	// check for single result, nil currently unsupported
	if results == nil || len(results.List) != 1 {
		return "", fmt.Errorf("function needs to return a single result type")
	}

	return ident.Name, nil
}

// structInFile filters for the struct of input name within the file.
func structInFile(file *ast.File, name string) bool {
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		// looking for token.TYPE
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}
		for _, spec := range genDecl.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != name {
				continue
			}

			structType, ok := ts.Type.(*ast.StructType)
			if !ok {
				return false
			}
			return structType != nil
		}
	}
	return false
}

// returnTypeImports scans for type declarations and writes them to return map.
func returnTypeImports(file *ast.File, available map[string]string) map[string]string {
	impUsed := make(map[string]string)
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}
		usedImports(genDecl, available, impUsed)
	}
	return impUsed
}

// returnAllDeclFuncs returns all declared files within the function
func returnAllDeclFuncs(file *ast.File) map[string]*ast.FuncDecl {
	allDeclFuncs := make(map[string]*ast.FuncDecl)
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Recv != nil {
			continue
		}

		allDeclFuncs[funcDecl.Name.Name] = funcDecl

	}
	return allDeclFuncs
}

// fileImports returns imports available from parsed file
func fileImports(file *ast.File) map[string]string {
	imports := make(map[string]string)
	for _, imp := range file.Imports {
		// unquoting import path string
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}

		// local names
		local := lastPath(path)
		if imp.Name != nil {
			// skip imports with _ or .
			if imp.Name.Name == "_" || imp.Name.Name == "." {
				log.Println("Skipping " + imp.Path.Value + " due to '.' or '_' usage.")
				continue
			}
			local = imp.Name.Name
		}
		imports[local] = path
	}
	return imports
}

// usedImports returns by reference, from available, all imports that are used.
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

// writeImports extracts imports as well as aliases from file
// needs to check for alias, because alias influences calls.
// Builds import string. Minimal imports are pre-defined.
func writeImports(used map[string]string) string {
	// mapping path to alias
	baseImports := map[string]string{
		"bytes":           "",
		"encoding/base64": "",
		"encoding/gob":    "",
		"fmt":             "",
		"os":              "",
	}

	for local, path := range used {
		if _, ok := baseImports[path]; ok {
			continue
		}
		if local == lastPath(path) {
			baseImports[path] = ""
		} else {
			baseImports[path] = local
		}
	}

	sorted := make([]string, 0, len(baseImports))
	for path := range baseImports {
		sorted = append(sorted, path)
	}
	// imports sorted in an array
	sort.Strings(sorted)

	// builds import string
	var b strings.Builder
	b.WriteString("import (\n")
	for _, path := range sorted {
		alias := baseImports[path]
		if alias != "" {
			// format with alias
			fmt.Fprintf(&b, "\t%s %q\n", alias, path)
		} else {
			// format no alias
			fmt.Fprintf(&b, "\t%q\n", path)
		}
	}
	b.WriteString(")\n")

	return b.String()
}

// fillHelpFuncBuf builds string for help functionso
func fillHelpFuncBuf(buf *bytes.Buffer, fset *token.FileSet, helpFuncs map[string]*ast.FuncDecl) {
	nameFuncs := make([]string, 0, len(helpFuncs))
	for name := range helpFuncs {
		nameFuncs = append(nameFuncs, name)
	}

	for _, name := range nameFuncs {
		printer.Fprint(buf, fset, helpFuncs[name])
		buf.WriteString("\n")
	}
}

// lastPath is used for Import paths. Some use alias, meaning it has to be treated differently
// for offloadable module.
// Returns string after last "/".
func lastPath(path string) string {
	// checks for last index of substring
	i := strings.LastIndex(path, "/")
	if i >= 0 {
		return path[i+1:]
	}
	return path
}

// buildWasm builds command for execution, uses wasip1 as target.
// output has to be written to disc, cannot intercept into byte format.
func buildWasm(inputFile string, outputFile string) error {
	cmd := exec.Command(
		"go", "build",
		"-o", outputFile,
		inputFile,
	)
	// appends args to prior command
	cmd.Env = append(os.Environ(),
		"GOOS=wasip1",
		"GOARCH=wasm",
	)
	// executed command and gives stdout back
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, string(out))
	}

	return nil
}

// getTypeDecls returns string to insert every type declaration from parsed file into wasm file.
// This is so that different structs can be utilized aside from the payload struct.
func getTypeDecls(fset *token.FileSet, file *ast.File) string {
	var buf bytes.Buffer
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)

		// skip if its a var, const or import
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		printer.Fprint(&buf, fset, genDecl)
		buf.WriteString("\n")
	}
	return buf.String()
}

// getHelperFuncs returns a map of function declarations (callExpr) that are used within a function as helpers.
// Only scans one layer deep.
func getHelperFuncs(allFuncs map[string]*ast.FuncDecl, fn *ast.FuncDecl) map[string]*ast.FuncDecl {
	calledFuncs := make(map[string]*ast.FuncDecl)

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		ident, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}

		target, ok := allFuncs[ident.Name]
		if !ok || target == fn {
			return true
		}

		calledFuncs[ident.Name] = target
		return true
	})
	return calledFuncs
}

// makeOffloadablesDir makes a directory for caching of wasm binaries and pre-confiled go files.
// Can be found under ".../Caches/go_offload/".
func makeOffloadablesDir() (string, error) {
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
