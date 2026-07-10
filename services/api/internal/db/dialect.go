package db

import (
	"fmt"
	"strconv"
	"strings"
)

// dialect carries the few SQL fragments that differ between Postgres and
// SQLite. Everything else in repo.go is portable and shared verbatim.
type dialect struct {
	name string // "postgres" | "sqlite"
	isPG bool
	now  string // current-timestamp expression: now() vs CURRENT_TIMESTAMP
}

func dialectFor(driver string) dialect {
	if driver == "pgx" {
		return dialect{name: "postgres", isPG: true, now: "now()"}
	}
	return dialect{name: "sqlite", isPG: false, now: "CURRENT_TIMESTAMP"}
}

// rebind converts '?' placeholders to '$1,$2,...' for Postgres; SQLite keeps
// '?'. Our queries never contain a literal '?' outside a placeholder, so a
// straight positional scan is safe.
func (d dialect) rebind(q string) string {
	if !d.isPG {
		return q
	}
	var b strings.Builder
	n := 0
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteByte(q[i])
	}
	return b.String()
}

// parseURL maps a DATABASE_URL to a database/sql driver name and DSN.
//   - postgres:// or postgresql:// -> driver "pgx", DSN = the URL unchanged
//     (pgx/stdlib parses the full URL).
//   - sqlite:, file:, or a bare filesystem path -> driver "sqlite", DSN is a
//     modernc file DSN with WAL + busy_timeout + foreign_keys pragmas.
func parseURL(rawURL string) (driver, dsn string, err error) {
	switch {
	case strings.HasPrefix(rawURL, "postgres://"), strings.HasPrefix(rawURL, "postgresql://"):
		return "pgx", rawURL, nil
	case strings.HasPrefix(rawURL, "sqlite:"):
		return "sqlite", sqliteDSN(strings.TrimPrefix(strings.TrimPrefix(rawURL, "sqlite://"), "sqlite:")), nil
	case strings.HasPrefix(rawURL, "file:"):
		return "sqlite", sqliteDSN(strings.TrimPrefix(rawURL, "file:")), nil
	case strings.HasPrefix(rawURL, "/"), strings.HasPrefix(rawURL, "./"):
		return "sqlite", sqliteDSN(rawURL), nil
	default:
		return "", "", fmt.Errorf("db: unrecognized DATABASE_URL scheme in %q", rawURL)
	}
}

// sqliteDSN builds a modernc.org/sqlite DSN with the pragmas the app relies on.
// path is a plain filesystem path (e.g. /data/tcf.db).
func sqliteDSN(path string) string {
	return "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)"
}
