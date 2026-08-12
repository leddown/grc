package db

import (
	"database/sql"
	"strconv"
	"strings"
)

// Open opens a database from a backend spec, auto-detecting the engine: specs
// beginning with postgres:// or postgresql:// use the external PostgreSQL
// backend, anything else is treated as a SQLite file path. Either way the
// schema is ensured before returning.
func Open(spec string) (*Conn, error) {
	if strings.HasPrefix(spec, "postgres://") || strings.HasPrefix(spec, "postgresql://") {
		return OpenPostgres(spec)
	}
	return OpenSQLite(spec)
}

// Dialect identifies which SQL backend a Conn is talking to. The two backends
// differ in placeholder syntax (? vs $N), how a generated primary key is read
// back (LastInsertId vs RETURNING), and a handful of aggregate function names.
type Dialect int

const (
	DialectSQLite Dialect = iota
	DialectPostgres
)

// Conn wraps *sql.DB with the active Dialect. It embeds *sql.DB so every
// standard method (Ping, Close, Prepare, Stats, ...) is still available, but it
// shadows Query/QueryRow/Exec/Begin so that queries written with ? placeholders
// are transparently rebound for Postgres. Repositories therefore keep writing
// SQLite-flavoured ? placeholders and stay backend-agnostic.
type Conn struct {
	*sql.DB
	dialect Dialect
}

// Dialect reports the backend this connection targets.
func (c *Conn) Dialect() Dialect { return c.dialect }

// Rebind converts ? placeholders to $1, $2, ... for Postgres; it is a no-op for
// SQLite. Queries in this codebase never contain a literal ? outside of a
// placeholder, so a simple sequential rewrite is sufficient.
func (c *Conn) Rebind(query string) string { return rebind(c.dialect, query) }

func rebind(dialect Dialect, query string) string {
	if dialect != DialectPostgres || !strings.Contains(query, "?") {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteByte(query[i])
	}
	return b.String()
}

func (c *Conn) Query(query string, args ...any) (*sql.Rows, error) {
	return c.DB.Query(c.Rebind(query), args...)
}

func (c *Conn) QueryRow(query string, args ...any) *sql.Row {
	return c.DB.QueryRow(c.Rebind(query), args...)
}

func (c *Conn) Exec(query string, args ...any) (sql.Result, error) {
	return c.DB.Exec(c.Rebind(query), args...)
}

func (c *Conn) Begin() (*Tx, error) {
	tx, err := c.DB.Begin()
	if err != nil {
		return nil, err
	}
	return &Tx{Tx: tx, dialect: c.dialect}, nil
}

// Insert runs an INSERT written with ? placeholders and returns the generated
// primary key. Every insertable table in this schema uses an "id" column as its
// auto-generated key, so Postgres appends RETURNING id while SQLite falls back
// to LastInsertId.
func (c *Conn) Insert(query string, args ...any) (int64, error) {
	if c.dialect == DialectPostgres {
		var id int64
		// #nosec G202 -- query is a constant INSERT from the caller; only the
		// fixed " RETURNING id" suffix is appended.
		err := c.DB.QueryRow(c.Rebind(query)+" RETURNING id", args...).Scan(&id)
		return id, err
	}
	result, err := c.DB.Exec(c.Rebind(query), args...)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// GroupConcatDistinct returns a dialect-appropriate aggregate that joins the
// distinct values of expr with a comma. SQLite spells this GROUP_CONCAT while
// Postgres uses STRING_AGG with an explicit separator.
func (c *Conn) GroupConcatDistinct(expr string) string {
	if c.dialect == DialectPostgres {
		return "STRING_AGG(DISTINCT " + expr + ", ',')"
	}
	return "GROUP_CONCAT(DISTINCT " + expr + ")"
}

// Tx wraps *sql.Tx with the active Dialect, mirroring Conn's rebinding so that
// transactional statements written with ? placeholders also work on Postgres.
type Tx struct {
	*sql.Tx
	dialect Dialect
}

func (t *Tx) rebind(query string) string { return rebind(t.dialect, query) }

func (t *Tx) Query(query string, args ...any) (*sql.Rows, error) {
	return t.Tx.Query(t.rebind(query), args...)
}

func (t *Tx) QueryRow(query string, args ...any) *sql.Row {
	return t.Tx.QueryRow(t.rebind(query), args...)
}

func (t *Tx) Exec(query string, args ...any) (sql.Result, error) {
	return t.Tx.Exec(t.rebind(query), args...)
}

func (t *Tx) Prepare(query string) (*sql.Stmt, error) {
	return t.Tx.Prepare(t.rebind(query))
}

// Insert is Conn.Insert inside a transaction: an INSERT written with ?
// placeholders, returning the generated primary key. It exists so a parent row
// and its children can be written atomically without the caller reimplementing
// the SQLite/Postgres difference in how a generated key is read back.
func (t *Tx) Insert(query string, args ...any) (int64, error) {
	if t.dialect == DialectPostgres {
		var id int64
		// #nosec G202 -- query is a constant INSERT from the caller; only the
		// fixed " RETURNING id" suffix is appended.
		err := t.Tx.QueryRow(t.rebind(query)+" RETURNING id", args...).Scan(&id)
		return id, err
	}
	result, err := t.Tx.Exec(t.rebind(query), args...)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}
