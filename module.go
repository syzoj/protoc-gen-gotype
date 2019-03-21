package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"

	pgs "github.com/lyft/protoc-gen-star"
	pgsgo "github.com/lyft/protoc-gen-star/lang/go"
	"github.com/syzoj/protoc-gen-gotype/gotype"
)

type module struct {
	*pgs.ModuleBase
	ctx pgsgo.Context
}

func newModule() pgs.Module {
	return &module{ModuleBase: &pgs.ModuleBase{}}
}

func (m *module) InitContext(c pgs.BuildContext) {
	m.ModuleBase.InitContext(c)
	m.ctx = pgsgo.InitContext(c.Parameters())
}

func (m *module) Name() string {
	return "gotype"
}

func (m *module) Execute(targets map[string]pgs.File, packages map[string]pgs.Package) []pgs.Artifact {
	for _, f := range targets {
		v := makeVisitor(m)
		if err := pgs.Walk(v, f); err != nil {
			panic(err)
		}

		f2 := m.BuildContext.JoinPath(f.InputPath().SetExt(".pb.go").Base())
		fs := token.NewFileSet()
		fn, err := parser.ParseFile(fs, f2, nil, parser.ParseComments)
		if err != nil {
			panic(err)
		}
		v2 := &goVisitor{nodes: v.nodes}
		ast.Walk(v2, fn)
		var b strings.Builder
		err = printer.Fprint(&b, fs, fn)
		if err != nil {
			panic(err)
		}
		m.OverwriteGeneratorFile(f2, b.String())
	}
	return m.Artifacts()
}

type visitor struct {
	pgs.Visitor
	pgs.DebuggerCommon
	nodes map[string]map[string]string
}

func makeVisitor(d pgs.DebuggerCommon) *visitor {
	return &visitor{
		Visitor:        pgs.NilVisitor(),
		DebuggerCommon: d,
		nodes:          make(map[string]map[string]string),
	}
}

func (v *visitor) VisitPackage(pgs.Package) (pgs.Visitor, error) { return v, nil }
func (v *visitor) VisitFile(pgs.File) (pgs.Visitor, error)       { return v, nil }
func (v *visitor) VisitMessage(pgs.Message) (pgs.Visitor, error) { return v, nil }
func (v *visitor) VisitField(f pgs.Field) (pgs.Visitor, error) {
	var gtype string
	ok, err := f.Extension(gotype.E_Gotype, &gtype)
	if err != nil {
		return nil, err
	}
	if ok {
		var msgName string
		if f.InOneOf() {
			msgName = f.Message().Name().UpperCamelCase().String() + "_" + f.Name().UpperCamelCase().String()
		} else {
			msgName = f.Message().Name().UpperCamelCase().String()
		}
		m := v.nodes[msgName]
		if m == nil {
			m = make(map[string]string)
		}
		m[f.Name().UpperCamelCase().String()] = gtype
		v.nodes[msgName] = m
	}
	return v, nil
}

type goVisitor struct {
	nodes map[string]map[string]string
}

func (v *goVisitor) Visit(n ast.Node) ast.Visitor {
	if t, ok := n.(*ast.TypeSpec); ok {
		if _, ok := t.Type.(*ast.StructType); ok {
			return &fieldVisitor{fields: v.nodes[t.Name.String()]}
		}
	}
	if f, ok := n.(*ast.FuncDecl); ok {
		var typeName string
		if f.Recv != nil {
			if starExpr, ok := f.Recv.List[0].Type.(*ast.StarExpr); ok {
				if n, ok := starExpr.X.(*ast.Ident); ok {
					typeName = n.Name
				}
			}
		}
		if typeName == "" {
			return nil
		}
		fields := v.nodes[typeName]
		if fields == nil {
			return nil
		}
		var fieldName string
		if f.Name != nil {
			name := f.Name.Name
			if strings.HasPrefix(name, "Get") {
				fieldName = f.Name.Name[3:]
			}
		}
		if fieldName == "" {
			return nil
		}
		newType, ok := fields[fieldName]
		if !ok {
			return nil
		}
		if f.Type.Results == nil || len(f.Type.Results.List) != 1 {
			panic(fmt.Sprintf("Message %s method %s does not have exactly one result"))
		}
		res := f.Type.Results.List[0]
		res2 := new(ast.Field)
		*res2 = *res
		res2.Type = ast.NewIdent(newType)
		f.Type.Results.List[0] = res2
		return nil
	}
	return v
}

type fieldVisitor struct {
	fields map[string]string
}

func (v *fieldVisitor) Visit(n ast.Node) ast.Visitor {
	if f, ok := n.(*ast.Field); ok {
		newName, ok := v.fields[f.Names[0].String()]
		if !ok {
			return nil
		}
		f.Type = &ast.StarExpr{X: ast.NewIdent(newName)}
		return nil
	}
	return v
}
