package migrate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sqlBuilders are the functions whose SQL text is not a constant. Each was read: what it splices is chosen by
// the server (an allowlisted column, a fixed clause, a $N placeholder), never text from a request.
var sqlBuilders = map[string]string{
	"internal/analytics/engine.go:GetTimeToResolution": "fixed team clause",
	"internal/analytics/engine.go:GetWorkload":         "fixed team clause",
	"internal/analytics/engine.go:scanDistribution":    "column from allowedGroupBy",
	"internal/dependency/store.go:GetBlockedBy":        "column list constant",
	"internal/dependency/store.go:GetBlockingIssues":   "column list constant",
	"internal/dependency/store.go:GetRelations":        "column list constant",
	"internal/featureboard/store.go:ListPosts":         "ORDER BY from a switch of literals",
	"internal/issue/store.go:BulkUpdate":               "fixed SET clauses, $N placeholders",
	"internal/issue/store.go:List":                     "ORDER BY from an allowlist, $N placeholders",
	"internal/issue/store.go:Update":                   "columns from updatableFields",
	"internal/issue/store.go:assertRefInWorkspace":     "query literal from issueRefQueries",
	"internal/mcp/server.go:ListMembers":               "column list constant",
	"internal/migrate/migrate.go:applyOne":             "migration files shipped in the binary",
	"internal/milestone/store.go:Update":               "columns from an allowlist",
	"internal/project/roadmap.go:GetRoadmap":           "fixed team clause",
	"internal/project/store.go:Update":                 "columns from an allowlist",
	"internal/realtime/roomauth.go:AuthorizeRoom":      "query literal from roomOwnershipSQL",
	"internal/scoring/store.go:GetPrioritizedBacklog":  "column is rice_score or ice_score",
	"internal/team/store.go:Update":                    "columns from an allowlist",
	"internal/template/store.go:Update":                "columns from an allowlist",
	"internal/tenancy/tenancy.go:AssertRefInWorkspace": "query literal from refQueries",
	"internal/workspace/purge.go:purge":                "statements from purgeOrder",
	"internal/workspace/store.go:Update":               "columns from workspaceUpdatable",
}

// Every Query, QueryRow and Exec takes constant SQL with its values bound as $N, or sits in a function listed
// in sqlBuilders. A new function that builds SQL fails here until someone reads it and adds it.
func TestSQLIsConstantOrReviewed(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	pkgs := map[string][]*ast.File{}
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "frontend" || d.Name() == "node_modules" || d.Name() == "testutil" || strings.HasPrefix(d.Name(), ".")) && p != root {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		pkgs[filepath.Dir(p)] = append(pkgs[filepath.Dir(p)], f)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, files := range pkgs {
		consts := map[string]bool{}
		for _, f := range files {
			for _, d := range f.Decls {
				if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.CONST {
					for _, s := range g.Specs {
						for _, n := range s.(*ast.ValueSpec).Names {
							consts[n.Name] = true
						}
					}
				}
			}
		}
		var constant func(ast.Expr) bool
		constant = func(e ast.Expr) bool {
			switch x := e.(type) {
			case *ast.BasicLit:
				return true
			case *ast.ParenExpr:
				return constant(x.X)
			case *ast.BinaryExpr:
				return constant(x.X) && constant(x.Y)
			case *ast.Ident:
				return consts[x.Name] || (x.Obj != nil && x.Obj.Kind == ast.Con)
			}
			return false
		}
		for _, f := range files {
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				rel, _ := filepath.Rel(root, fset.Position(fn.Pos()).Filename)
				key := filepath.ToSlash(rel) + ":" + fn.Name.Name
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					c, ok := n.(*ast.CallExpr)
					if !ok || len(c.Args) < 2 {
						return true
					}
					sel, ok := c.Fun.(*ast.SelectorExpr)
					if !ok || (sel.Sel.Name != "Query" && sel.Sel.Name != "QueryRow" && sel.Sel.Name != "Exec") {
						return true
					}
					calls++
					if !constant(c.Args[1]) && sqlBuilders[key] == "" {
						t.Errorf("%s: SQL is built at run time in %s — bind values as $N; if what it splices is "+
							"chosen by the server, read it and add the function to sqlBuilders", fset.Position(c.Pos()), key)
					}
					return true
				})
			}
		}
	}
	if calls < 100 {
		t.Fatalf("found only %d query calls — the walk is not reaching the code", calls)
	}
}
