package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/serenize/snaker"
)

// TODO:
// - Generate files

type typeThing struct {
	name       string
	definition *ast.StructType
	parents    []ast.Expr
	funcs      []*ast.FuncDecl
}

var hasName = make(map[string]bool)

var knownPrefixes = make(map[string]bool)

var currentImports = make(map[string]bool)

var importStrings = make(map[string]string)

func init() {
	knownPrefixes["cairo"] = true
	knownPrefixes["gdk"] = true
	knownPrefixes["gtk"] = true
	knownPrefixes["glib"] = true
	knownPrefixes["pango"] = true

	importStrings["cairo"] = "\"github.com/gotk3/gotk3/cairo\""
	importStrings["gdk"] = "\"github.com/gotk3/gotk3/gdk\""
	importStrings["gtk"] = "\"github.com/gotk3/gotk3/gtk\""
	importStrings["glib"] = "\"github.com/gotk3/gotk3/glib\""
	importStrings["pango"] = "\"github.com/gotk3/gotk3/pango\""

	importStrings["cairo_iface"] = "cairo_iface \"github.com/gotk3/gotk3/cairo/iface\""
	importStrings["gdk_iface"] = "gdk_iface \"github.com/gotk3/gotk3/gdk/iface\""
	importStrings["gtk_iface"] = "gtk_iface \"github.com/gotk3/gotk3/gtk/iface\""
	importStrings["glib_iface"] = "glib_iface \"github.com/gotk3/gotk3/glib/iface\""
	importStrings["pango_iface"] = "pango_iface \"github.com/gotk3/gotk3/pango/iface\""
}

func (tt *typeThing) addFunc(f *ast.FuncDecl) {
	tt.funcs = append(tt.funcs, f)
}

func (tt *typeThing) addParent(p ast.Expr) {
	tt.parents = append(tt.parents, p)
}

func getOrCreateTypeThing(m map[string]*typeThing, name string) *typeThing {
	v, ok := m[name]
	if !ok {
		v = &typeThing{
			name: name,
		}
		m[name] = v
	}
	return v
}

func arrayType(l ast.Expr) string {
	if l == nil {
		return ""
	}
	switch l.(type) {
	case *ast.Ellipsis:
		return "..."
	default:
		panic(fmt.Sprintf("Blrag2: %#v\n", l))
	}
}

func genType(f ast.Expr, withIface bool, withIfaceOther bool) string {
	switch ft := f.(type) {
	case *ast.Ident:
		if hasName[ft.Name] {
			if withIface {
				return "iface." + ft.Name
			}
			return ft.Name
		}
		return ft.Name
	case *ast.StarExpr:
		nm := genType(ft.X, withIface, withIfaceOther)
		if (withIface && hasName[nm[6:]]) || hasName[nm] {
			return nm
		}
		if (withIfaceOther && knownPrefixes[strings.Replace(strings.Split(nm, ".")[0], "_iface", "", 1)]) || knownPrefixes[strings.Split(nm, ".")[0]] {
			return nm
		}
		return "*" + nm
	case *ast.SelectorExpr:
		pref := genType(ft.X, false, false)
		if knownPrefixes[pref] && withIfaceOther {
			pref = pref + "_iface"
		}
		currentImports[pref] = true
		return pref + "." + ft.Sel.Name
	case *ast.MapType:
		return "map[" + genType(ft.Key, withIface, withIfaceOther) + "]" + genType(ft.Value, withIface, withIfaceOther)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.ArrayType:
		return "[" + arrayType(ft.Len) + "]" + genType(ft.Elt, withIface, withIfaceOther)
	case *ast.Ellipsis:
		return "..." + genType(ft.Elt, withIface, withIfaceOther)
	case *ast.FuncType:
		return "func" + genFuncType(ft, false, withIface, withIfaceOther)
	default:
		panic(fmt.Sprintf("Blrag: %#v\n", f))
	}
	return ""
}

func genField(f *ast.Field, withNames bool, withIface bool, withIfaceOther bool) string {
	if withNames {
		return fmt.Sprintf("%s %s", f.Names[0].Name, genType(f.Type, withIface, withIfaceOther))
	}

	return genType(f.Type, withIface, withIfaceOther)
}

func genFieldArg(f *ast.Field) string {
	_, ell := f.Type.(*ast.Ellipsis)
	suffix := ""
	if ell {
		suffix = "..."
	}
	return fmt.Sprintf("%s%s", f.Names[0].Name, suffix)
}

func genResults(f *ast.FieldList, withIface bool, withIfaceOther bool) string {
	result := []string{}

	if f != nil && f.List != nil {
		for _, field := range f.List {
			result = append(result, genField(field, false, withIface, withIfaceOther))
		}
	}

	switch len(result) {
	case 0:
		return ""
	case 1:
		return " " + result[0]
	default:
		return " (" + strings.Join(result, ", ") + ")"
	}
}

func genParams(f *ast.FieldList, withNames bool, withIface bool, withIfaceOther bool) string {
	result := []string{}

	for _, field := range f.List {
		result = append(result, genField(field, withNames, withIface, withIfaceOther))
	}

	return strings.Join(result, ", ")
}

func genFuncArgs(fd *ast.FuncType) string {
	result := []string{}

	for _, field := range fd.Params.List {
		result = append(result, genFieldArg(field))
	}

	return strings.Join(result, ", ")
}

func genFuncType(fd *ast.FuncType, withNames bool, withIface bool, withIfaceOther bool) string {
	return fmt.Sprintf("(%s)%s", genParams(fd.Params, withNames, withIface, withIfaceOther), genResults(fd.Results, withIface, withIfaceOther))
}

func genInterfaceDef(fd *ast.FuncDecl) string {
	return fmt.Sprintf("%s%s", fd.Name.Name, genFuncType(fd.Type, false, false, true))
}

func genMethodDef(fd *ast.FuncDecl) string {
	return fmt.Sprintf("%s%s", fd.Name.Name, genFuncType(fd.Type, true, true, true))
}

func genInvocation(fd *ast.FuncDecl) string {
	tp := fd.Type
	prefix := ""
	if tp.Results != nil {
		prefix = "return "
	}

	return fmt.Sprintf("%s%s(%s)", prefix, fd.Name.Name, genFuncArgs(tp))
}

func addParents(f *ast.FieldList, tt *typeThing) {
	if f != nil && f.List != nil {
		for _, field := range f.List {
			if field.Names == nil {
				tt.addParent(field.Type)
			}
		}
	}
}

func collectAllFrom(dir string) map[string]*typeThing {
	fset := token.NewFileSet()

	typeThings := make(map[string]*typeThing)

	f, err := parser.ParseDir(fset, dir, nil, 0)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	for _, pa := range f {
		for fn, ff := range pa.Files {
			if !strings.HasSuffix(fn, "_test.go") && !strings.Contains(fn, "_since_") {
				for _, s := range ff.Decls {
					switch ss := s.(type) {
					case *ast.FuncDecl:
						if ss.Name.IsExported() {
							recv := "<global>"
							if ss.Recv != nil {
								tt := ss.Recv.List[0].Type
								switch bla := tt.(type) {
								case *ast.StarExpr:
									recv = bla.X.(*ast.Ident).Name
								case *ast.Ident:
									recv = bla.Name
								}
							}
							getOrCreateTypeThing(typeThings, recv).addFunc(ss)
						}
					case *ast.GenDecl:
						switch ss.Tok {
						case token.TYPE:
							for _, sp := range ss.Specs {
								sss := sp.(*ast.TypeSpec)
								if sss.Name.IsExported() {
									switch st := sss.Type.(type) {
									case *ast.StructType:
										tt := getOrCreateTypeThing(typeThings, sss.Name.Name)
										hasName[sss.Name.Name] = true
										tt.definition = st
										addParents(st.Fields, tt)
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return typeThings
}

func underscore(s string) string {
	return snaker.CamelToSnake(s) + ".go"
}

func exportInterface(name string, tp *typeThing, outDir, pname string) {
	dir := outDir + "/" + pname + "/iface"
	os.MkdirAll(dir, 0755)
	fname := underscore(name)
	fullname := fmt.Sprintf("%s/%s", dir, fname)

	out := new(bytes.Buffer)
	currentImports = make(map[string]bool)

	fmt.Fprintf(out, "type %s interface {\n", name)
	hasParents := false
	for _, pd := range tp.parents {
		fmt.Fprintf(out, "    %s\n", genType(pd, false, true))
		hasParents = true
	}
	if hasParents && len(tp.funcs) > 0 {
		fmt.Fprintf(out, "\n")
	}
	funcs := tp.funcs
	sort.Sort(ByName(funcs))
	for _, fd := range funcs {
		fmt.Fprintf(out, "    %s\n", genInterfaceDef(fd))
	}
	fmt.Fprintf(out, "} // end of %s\n\n", name)

	fmt.Fprintf(out, "func Assert%s(_ %s) {}\n", name, name)

	realOut, _ := os.Create(fullname)
	defer realOut.Close()

	fmt.Fprintf(realOut, "package iface\n\n")
	for ki := range currentImports {
		is, ok := importStrings[ki]
		if !ok {
			is = ki
		}
		fmt.Fprintf(realOut, "import %s\n", is)
	}
	fmt.Fprintf(realOut, "\n%s", out.String())
}

func exportAllInterfaces(typeThings map[string]*typeThing, outDir, pname string) {
	for _, tp := range sortedTypeThings(typeThings) {
		if tp.name != "<global>" {
			exportInterface(tp.name, tp, outDir, pname)
		}
	}
}

func exportGlobalInterface(typeThings map[string]*typeThing, name, outDir, pname string) {
	tp, ok := typeThings["<global>"]
	if ok {
		exportInterface(name, tp, outDir, pname)
	}
}

func genProxyMethod(out io.Writer, iname string, fd *ast.FuncDecl) {
	fmt.Fprintf(out, "func (*Real%s) %s {\n", iname, genMethodDef(fd))
	fmt.Fprintf(out, "  %s\n", genInvocation(fd))
	fmt.Fprintf(out, "}\n\n")
}

func exportGlobalImpl(typeThings map[string]*typeThing, iname, pname, proot, outDir string) {
	tp, ok := typeThings["<global>"]
	if ok {
		dir := outDir + "/" + pname
		os.MkdirAll(dir, 0755)
		fname := "real_" + pname + ".go"
		fullname := fmt.Sprintf("%s/%s", dir, fname)

		out := new(bytes.Buffer)
		currentImports = make(map[string]bool)

		fmt.Fprintf(out, "type Real%s struct{}\n\n", iname)
		fmt.Fprintf(out, "var Real = &Real%s{}\n\n", iname)

		funcs := tp.funcs
		sort.Sort(ByName(funcs))
		for _, fd := range funcs {
			genProxyMethod(out, iname, fd)
		}

		realOut, _ := os.Create(fullname)
		defer realOut.Close()

		fmt.Fprintf(realOut, "package %s\n\n", pname)
		fmt.Fprintf(realOut, "import \"%s/%s/iface\"\n", proot, pname)

		for ki := range currentImports {
			is, ok := importStrings[ki]
			if !ok {
				is = ki
			}
			fmt.Fprintf(realOut, "import %s\n", is)
		}
		fmt.Fprintf(realOut, "\n%s", out.String())
	}
}

func sortedTypeThings(typeThings map[string]*typeThing) []*typeThing {
	var result []*typeThing
	for _, tp := range typeThings {
		result = append(result, tp)
	}
	sort.Sort(IfaceByName(result))
	return result
}

func exportTesterFile(typeThings map[string]*typeThing, iname, pname, proot, outDir string) {
	dir := outDir + "/" + pname
	os.MkdirAll(dir, 0755)
	fname := pname + "_iface_testers.go"
	fullname := fmt.Sprintf("%s/%s", dir, fname)

	out, _ := os.Create(fullname)
	defer out.Close()

	fmt.Fprintf(out, "package %s\n\n", pname)
	fmt.Fprintf(out, "import \"%s/%s/iface\"\n\n", proot, pname)
	fmt.Fprintf(out, "func init() {\n")

	for _, tp := range sortedTypeThings(typeThings) {
		name := tp.name
		testName := tp.name
		if name == "<global>" {
			name = iname
			testName = "Real" + iname
		}
		fmt.Fprintf(out, "  iface.Assert%s(&%s{})\n", name, testName)
	}
	fmt.Fprintf(out, "}\n")
}

func main() {
	if len(os.Args) < 6 {
		fmt.Printf("usages: gen.go <dir> <interface-name> <package-root> <package-name> <out-dir>\n")
		os.Exit(1)
	}

	fromDir := os.Args[1]
	ifname := os.Args[2]
	proot := os.Args[3]
	pname := os.Args[4]
	outDir := os.Args[5]

	typeThings := collectAllFrom(fromDir)

	exportAllInterfaces(typeThings, outDir, pname)
	exportGlobalInterface(typeThings, ifname, outDir, pname)
	exportGlobalImpl(typeThings, ifname, pname, proot, outDir)
	exportTesterFile(typeThings, ifname, pname, proot, outDir)
}

type ByName []*ast.FuncDecl

func (a ByName) Len() int           { return len(a) }
func (a ByName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByName) Less(i, j int) bool { return a[i].Name.Name < a[j].Name.Name }

type IfaceByName []*typeThing

func (a IfaceByName) Len() int           { return len(a) }
func (a IfaceByName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a IfaceByName) Less(i, j int) bool { return a[i].name < a[j].name }
