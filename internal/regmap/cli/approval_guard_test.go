package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// approvalWriters lists the packages allowed to write StatusApproved. The whole
// human-in-the-loop guarantee rests on this being exactly one package: the
// interactive review gates.
// regmapRoot is the tree this guard scans: the whole regmap subsystem,
// reached from the cli package this test lives in.
const regmapRoot = ".."

var approvalWriters = map[string]bool{
	filepath.Join(regmapRoot, "review"): true,
}

// TestOnlyReviewCanApprove walks every non-test source file and fails if any
// package outside internal/review assigns StatusApproved to anything.
//
// Reading the constant is fine — reports and status counters have to compare
// against it. What must not exist anywhere else is a *write*: an assignment or
// a struct literal field that sets a status to approved. If this test fails,
// some code path can mark an item approved without a human ever seeing it.
func TestOnlyReviewCanApprove(t *testing.T) {
	fset := token.NewFileSet()
	var violations []string
	scanned := 0

	err := filepath.WalkDir(regmapRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "data", "profiles":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++

		dir := filepath.Dir(path)
		if approvalWriters[filepath.Clean(dir)] {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.AssignStmt:
				// Two shapes count as a write:
				//   x = requirement.StatusApproved        (RHS is the constant)
				//   x.Status = <anything mentioning it>   (LHS is a status field)
				for i, rhs := range node.Rhs {
					if isApprovedExpr(rhs) {
						violations = append(violations, location(fset, rhs.Pos(), path, "assignment"))
						continue
					}
					if i < len(node.Lhs) && assignsStatusField(node.Lhs[i]) && mentionsApproved(rhs) {
						violations = append(violations, location(fset, rhs.Pos(), path, "status assignment"))
					}
				}
			case *ast.KeyValueExpr:
				// e.g. Entry{Status: requirement.StatusApproved}
				if key, ok := node.Key.(*ast.Ident); ok && key.Name == "Status" && mentionsApproved(node.Value) {
					violations = append(violations, location(fset, node.Pos(), path, "struct literal field"))
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk source tree: %v", err)
	}
	if scanned == 0 {
		t.Fatal("scanned no Go files; the guard would pass vacuously")
	}

	for _, v := range violations {
		t.Errorf("StatusApproved is written outside internal/review at %s", v)
	}
	if len(violations) > 0 {
		t.Log("Only the interactive review gates may approve an item. " +
			"If this is intentional, the human-in-the-loop guarantee has been broken.")
	}
}

// TestApprovalGuardDetectsAViolation verifies the guard itself works, so a
// passing TestOnlyReviewCanApprove means something.
func TestApprovalGuardDetectsAViolation(t *testing.T) {
	const bad = `package sneaky

import "grc/internal/regmap/requirement"

func approveEverything(r *requirement.Requirement) {
	r.Status = requirement.StatusApproved
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sneaky.go", bad, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if assign, ok := n.(*ast.AssignStmt); ok {
			for _, rhs := range assign.Rhs {
				if mentionsApproved(rhs) {
					found = true
				}
			}
		}
		return true
	})
	if !found {
		t.Error("the guard failed to flag an obvious out-of-band approval")
	}
}

// isApprovedExpr reports whether an expression *is* the StatusApproved constant
// rather than merely containing it. Reading the constant — comparing against it
// or using it as a map key, as the reports and status counters do — is fine;
// assigning it is what this guard exists to catch.
func isApprovedExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name == "StatusApproved"
	case *ast.SelectorExpr:
		return e.Sel != nil && e.Sel.Name == "StatusApproved"
	}
	return false
}

// assignsStatusField reports whether an assignment target is a .Status field.
func assignsStatusField(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && sel.Sel != nil && sel.Sel.Name == "Status"
}

// mentionsApproved reports whether an expression references StatusApproved.
func mentionsApproved(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.Ident:
			if node.Name == "StatusApproved" {
				found = true
			}
		case *ast.SelectorExpr:
			if node.Sel != nil && node.Sel.Name == "StatusApproved" {
				found = true
			}
		}
		return !found
	})
	return found
}

func location(fset *token.FileSet, pos token.Pos, path, kind string) string {
	p := fset.Position(pos)
	return path + ":" + itoa(p.Line) + " (" + kind + ")"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
