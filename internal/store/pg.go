package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PgStore is the PostgreSQL repository used by the Compose deployment. The
// business methods are shared with SQLiteStore so both database paths enforce
// the same capability checks, tenant filters, approval binding, checkpointing,
// and idempotency behavior.
type PgStore struct {
	*SQLiteStore
}

// postgresQuerier adapts the deliberately small, database/sql-based query
// surface used by SQLiteStore to PostgreSQL. It rebinds positional parameters
// and translates the three SQLite upsert forms used by the shared repository.
type postgresQuerier struct{ q querier }

func (q postgresQuerier) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return q.q.ExecContext(ctx, postgresSQL(query), args...)
}

func (q postgresQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return q.q.QueryContext(ctx, postgresSQL(query), args...)
}

func (q postgresQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return q.q.QueryRowContext(ctx, postgresSQL(query), args...)
}

func OpenPG(url string, role Role) (*PgStore, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(12)
	db.SetMaxIdleConns(4)
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	base := &SQLiteStore{db: db, role: role, q: postgresQuerier{q: db}, dialect: "postgres"}
	return &PgStore{SQLiteStore: base}, nil
}

// postgresSQL converts queries at the driver boundary. Question marks inside
// quoted JSON/text are left intact, although repository queries currently use
// placeholders only outside literals.
func postgresSQL(query string) string {
	query = strings.ReplaceAll(query, "selected_action_ids LIKE ?", "selected_action_ids::text LIKE ?")
	query = strings.ReplaceAll(query, "ORDER BY rowid", "ORDER BY started_at, id")

	upsert := ""
	switch {
	case strings.HasPrefix(query, "INSERT OR REPLACE INTO kb_articles"):
		query = strings.Replace(query, "INSERT OR REPLACE", "INSERT", 1)
		upsert = " ON CONFLICT (id) DO UPDATE SET tenant_id=EXCLUDED.tenant_id,title=EXCLUDED.title,intent_tags=EXCLUDED.intent_tags,body=EXCLUDED.body,version=EXCLUDED.version,status=EXCLUDED.status,updated_at=EXCLUDED.updated_at"
	case strings.HasPrefix(query, "INSERT OR REPLACE INTO kb_chunks"):
		query = strings.Replace(query, "INSERT OR REPLACE", "INSERT", 1)
		upsert = " ON CONFLICT (id) DO UPDATE SET tenant_id=EXCLUDED.tenant_id,article_id=EXCLUDED.article_id,chunk_index=EXCLUDED.chunk_index,text=EXCLUDED.text,tokenized_text=EXCLUDED.tokenized_text,index_version=EXCLUDED.index_version"
	case strings.HasPrefix(query, "INSERT OR IGNORE"):
		query = strings.Replace(query, "INSERT OR IGNORE", "INSERT", 1)
		upsert = " ON CONFLICT DO NOTHING"
	}
	query += upsert

	// The shared leasing code runs this SELECT inside a transaction. PostgreSQL
	// can therefore prevent two workers from choosing the same row.
	if (strings.Contains(query, "FROM runs WHERE state='queued'") ||
		strings.Contains(query, "FROM execution_jobs WHERE state='queued'")) &&
		strings.Contains(query, " ORDER BY created_at LIMIT 1") {
		query = strings.Replace(query, " ORDER BY created_at LIMIT 1", " ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1", 1)
	}

	return rebindPostgres(query)
}

func rebindPostgres(query string) string {
	var b strings.Builder
	b.Grow(len(query) + 8)
	arg := 1
	var quote rune
	for _, r := range query {
		if quote != 0 {
			b.WriteRune(r)
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			b.WriteRune(r)
			continue
		}
		if r == '?' {
			fmt.Fprintf(&b, "$%d", arg)
			arg++
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimRightFunc(b.String(), unicode.IsSpace)
}

var _ Repository = (*PgStore)(nil)
