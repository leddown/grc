package db

import "testing"

func TestRebind(t *testing.T) {
	cases := []struct {
		name    string
		dialect Dialect
		in      string
		want    string
	}{
		{"sqlite is a no-op", DialectSQLite, "SELECT * FROM t WHERE a = ? AND b = ?", "SELECT * FROM t WHERE a = ? AND b = ?"},
		{"postgres numbers placeholders", DialectPostgres, "SELECT * FROM t WHERE a = ? AND b = ?", "SELECT * FROM t WHERE a = $1 AND b = $2"},
		{"postgres no placeholders", DialectPostgres, "SELECT 1", "SELECT 1"},
		{"postgres many placeholders", DialectPostgres, "INSERT INTO t VALUES (?, ?, ?)", "INSERT INTO t VALUES ($1, $2, $3)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rebind(tc.dialect, tc.in); got != tc.want {
				t.Fatalf("rebind(%v, %q) = %q, want %q", tc.dialect, tc.in, got, tc.want)
			}
		})
	}
}

func TestGroupConcatDistinct(t *testing.T) {
	expr := "x"
	if got := (&Conn{dialect: DialectSQLite}).GroupConcatDistinct(expr); got != "GROUP_CONCAT(DISTINCT x)" {
		t.Fatalf("sqlite GroupConcatDistinct = %q", got)
	}
	if got := (&Conn{dialect: DialectPostgres}).GroupConcatDistinct(expr); got != "STRING_AGG(DISTINCT x, ',')" {
		t.Fatalf("postgres GroupConcatDistinct = %q", got)
	}
}
