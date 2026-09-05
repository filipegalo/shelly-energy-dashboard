package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// DB keeps repository SQL portable while the PostgreSQL driver uses $n placeholders.
type DB struct {
	raw      *sql.DB
	postgres bool
}
type Tx struct {
	raw      *sql.Tx
	postgres bool
}
type postgresInsertResult struct{ id int64 }

func (r postgresInsertResult) LastInsertId() (int64, error) { return r.id, nil }
func (r postgresInsertResult) RowsAffected() (int64, error) { return 1, nil }

func (d *DB) Close() error                          { return d.raw.Close() }
func (d *DB) PingContext(ctx context.Context) error { return d.raw.PingContext(ctx) }
func (d *DB) ExecContext(ctx context.Context, q string, a ...any) (sql.Result, error) {
	a = postgresArgs(d.postgres, a)
	if d.postgres && isInsert(q) {
		var id int64
		err := d.raw.QueryRowContext(ctx, bind(true, q)+" RETURNING id", a...).Scan(&id)
		return postgresInsertResult{id}, err
	}
	return d.raw.ExecContext(ctx, bind(d.postgres, q), a...)
}
func (d *DB) QueryContext(ctx context.Context, q string, a ...any) (*sql.Rows, error) {
	rows, err := d.raw.QueryContext(ctx, bind(d.postgres, q), postgresArgs(d.postgres, a)...)
	if err != nil { return nil, fmt.Errorf("query %q: %w", q, err) }
	return rows, nil
}
func (d *DB) QueryRowContext(ctx context.Context, q string, a ...any) *sql.Row {
	return d.raw.QueryRowContext(ctx, bind(d.postgres, q), postgresArgs(d.postgres, a)...)
}
func (d *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := d.raw.BeginTx(ctx, opts)
	return &Tx{raw: tx, postgres: d.postgres}, err
}
func (t *Tx) ExecContext(ctx context.Context, q string, a ...any) (sql.Result, error) {
	a = postgresArgs(t.postgres, a)
	if t.postgres && isInsert(q) {
		var id int64
		err := t.raw.QueryRowContext(ctx, bind(true, q)+" RETURNING id", a...).Scan(&id)
		return postgresInsertResult{id}, err
	}
	return t.raw.ExecContext(ctx, bind(t.postgres, q), a...)
}
func (t *Tx) QueryContext(ctx context.Context, q string, a ...any) (*sql.Rows, error) {
	return t.raw.QueryContext(ctx, bind(t.postgres, q), postgresArgs(t.postgres, a)...)
}
func (t *Tx) QueryRowContext(ctx context.Context, q string, a ...any) *sql.Row {
	return t.raw.QueryRowContext(ctx, bind(t.postgres, q), postgresArgs(t.postgres, a)...)
}
func (t *Tx) Commit() error   { return t.raw.Commit() }
func (t *Tx) Rollback() error { return t.raw.Rollback() }

func bind(postgres bool, query string) string {
	if !postgres {
		return query
	}
	var b strings.Builder
	index := 1
	inQuote := false
	for _, r := range query {
		if r == '\'' {
			inQuote = !inQuote
		}
		if r == '?' && !inQuote {
			fmt.Fprintf(&b, "$%d", index)
			index++
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isInsert(query string) bool {
	upper := strings.ToUpper(strings.TrimSpace(query))
	return strings.HasPrefix(upper, "INSERT ") && !strings.Contains(upper, " RETURNING ")
}

func postgresArgs(postgres bool, args []any) []any {
	if !postgres {
		return args
	}
	converted := make([]any, len(args))
	for i, value := range args {
		if boolean, ok := value.(bool); ok {
			if boolean {
				converted[i] = 1
			} else {
				converted[i] = 0
			}
		} else {
			converted[i] = value
		}
	}
	return converted
}
