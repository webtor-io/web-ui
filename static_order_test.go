package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// gin applies Use() only to the routes registered after it, so which
// middleware a static file goes through is decided by nothing but the order
// of the calls in serve(). Pin that order: behind the host rewrites (on the
// S3 and API hosts the path belongs to them), ahead of every middleware that
// needs a dependency. On 2026-10-05 the files sat behind the claims middleware
// and /assets and the manifest answered 503 for the five minutes
// claims-provider could not reach Postgres.
func TestStaticIsRegisteredBeforeDependentMiddleware(t *testing.T) {
	pos := callPositions(t, "serve.go", "serve")
	static := pos["sta.RegisterHandler"]
	if static == 0 {
		t.Fatal("serve no longer calls sta.RegisterHandler")
	}
	for _, rewrite := range []string{"s3svc.RegisterHostMiddleware", "libapi.RegisterHostMiddleware"} {
		if p := pos[rewrite]; p == 0 || p > static {
			t.Errorf("%s must be registered before the static files", rewrite)
		}
	}
	for _, dep := range []string{
		"sess.RegisterHandler", // session, CSRF: Redis
		"a.RegisterHandler",    // SuperTokens, user row: Postgres
		"uc.RegisterHandler",   // claims-provider: Postgres
		"sapi.RegisterHandler", // reads the claims above
	} {
		if p := pos[dep]; p == 0 || p < static {
			t.Errorf("%s is registered before the static files; they would go through it", dep)
		}
	}
}

// callPositions maps "pkg.Func" (or "recv.Method") to the offset of its first
// call inside the named function of file.
func callPositions(t *testing.T, file, fn string) map[string]token.Pos {
	t.Helper()
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]token.Pos{}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Name.Name != fn {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); ok {
				k := x.Name + "." + sel.Sel.Name
				if _, seen := pos[k]; !seen {
					pos[k] = call.Pos()
				}
			}
			return true
		})
	}
	return pos
}
