package analyzer

import (
	"reflect"

	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
)

// parseJS parses a JavaScript source file. Returns the program AST or an
// error if parsing fails (likely because the file uses ES2022+ syntax goja
// doesn't support yet, or it's a syntax error). Callers fall back to regex
// behavior on parse failure.
func parseJS(filename, source string) (*ast.Program, error) {
	return parser.ParseFile(nil, filename, source, 0)
}

// walkAST visits every ast.Node reachable from root, calling fn for each.
// Uses reflection so we don't have to enumerate every concrete type in
// goja's AST package (which spans dozens of statement and expression kinds).
// fn returning false skips that subtree.
func walkAST(root ast.Node, fn func(ast.Node) bool) {
	if root == nil {
		return
	}
	if !fn(root) {
		return
	}
	walkChildren(reflect.ValueOf(root), fn)
}

var astNodeType = reflect.TypeOf((*ast.Node)(nil)).Elem()

func walkChildren(v reflect.Value, fn func(ast.Node) bool) {
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			return
		}
		walkChildren(v.Elem(), fn)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			walkField(v.Field(i), fn)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walkField(v.Index(i), fn)
		}
	}
}

func walkField(f reflect.Value, fn func(ast.Node) bool) {
	switch f.Kind() {
	case reflect.Interface:
		if f.IsNil() {
			return
		}
		if f.Type().Implements(astNodeType) {
			if n, ok := f.Interface().(ast.Node); ok {
				walkAST(n, fn)
				return
			}
		}
		walkChildren(f.Elem(), fn)
	case reflect.Ptr:
		if f.IsNil() {
			return
		}
		if f.Type().Implements(astNodeType) {
			if n, ok := f.Interface().(ast.Node); ok {
				walkAST(n, fn)
				return
			}
		}
		walkChildren(f.Elem(), fn)
	case reflect.Slice, reflect.Array:
		for i := 0; i < f.Len(); i++ {
			walkField(f.Index(i), fn)
		}
	case reflect.Struct:
		// Inline struct (no pointer): may itself be a Node, or just contain nodes
		if f.CanAddr() && f.Addr().Type().Implements(astNodeType) {
			if n, ok := f.Addr().Interface().(ast.Node); ok {
				walkAST(n, fn)
				return
			}
		}
		walkChildren(f, fn)
	}
}

// identifierName returns the name of an identifier expression, or "" if
// the expression isn't a plain identifier.
func identifierName(e ast.Expression) string {
	if id, ok := e.(*ast.Identifier); ok {
		return string(id.Name)
	}
	return ""
}

// memberAccess returns ("foo", "bar", true) for a `foo.bar` MemberExpression
// (or DotExpression in goja's AST), false otherwise. Bracket access with a
// string literal property (`foo["bar"]`) also matches.
func memberAccess(e ast.Expression) (object, property string, ok bool) {
	switch m := e.(type) {
	case *ast.DotExpression:
		object = identifierName(m.Left)
		property = string(m.Identifier.Name)
		ok = object != ""
		return
	case *ast.BracketExpression:
		object = identifierName(m.Left)
		if s, sok := m.Member.(*ast.StringLiteral); sok {
			property = s.Value.String()
			ok = object != ""
			return
		}
	}
	return "", "", false
}

// isCallTo reports whether a CallExpression invokes a global identifier with
// the given name (e.g. eval(...)).
func isCallTo(c *ast.CallExpression, name string) bool {
	return identifierName(c.Callee) == name
}

// isMemberCall reports whether a CallExpression invokes <object>.<property>(...).
func isMemberCall(c *ast.CallExpression, object string, properties ...string) bool {
	obj, prop, ok := memberAccess(c.Callee)
	if !ok || obj != object {
		return false
	}
	for _, p := range properties {
		if prop == p {
			return true
		}
	}
	return false
}

// isNewOf reports whether a NewExpression instantiates the given identifier
// (e.g. new Function(...)).
func isNewOf(n *ast.NewExpression, name string) bool {
	return identifierName(n.Callee) == name
}
