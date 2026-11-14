package store

import (
	"strings"
	"testing"
)

func TestPostgresSQLRebindsAndPreservesQuotedQuestionMarks(t *testing.T) {
	got := postgresSQL("SELECT '?' AS literal FROM inquiries WHERE tenant_id=? AND id=?")
	want := "SELECT '?' AS literal FROM inquiries WHERE tenant_id=$1 AND id=$2"
	if got != want {
		t.Fatalf("postgresSQL() = %q, want %q", got, want)
	}
}

func TestPostgresSQLTranslatesSharedUpserts(t *testing.T) {
	ignore := postgresSQL("INSERT OR IGNORE INTO tenants(id,name) VALUES(?,?)")
	if strings.Contains(ignore, "OR IGNORE") || !strings.Contains(ignore, "ON CONFLICT DO NOTHING") {
		t.Fatalf("ignore insert was not translated: %s", ignore)
	}
	replace := postgresSQL("INSERT OR REPLACE INTO kb_articles(id,tenant_id,title,intent_tags,body,version,status,updated_at) VALUES(?,?,?,?,?,?,?,?)")
	if strings.Contains(replace, "OR REPLACE") || !strings.Contains(replace, "ON CONFLICT (id) DO UPDATE") {
		t.Fatalf("replace insert was not translated: %s", replace)
	}
}

func TestPostgresSQLAddsWorkerRowLock(t *testing.T) {
	got := postgresSQL("SELECT id FROM runs WHERE state='queued' OR state='running' ORDER BY created_at LIMIT 1")
	if !strings.Contains(got, "FOR UPDATE SKIP LOCKED LIMIT 1") {
		t.Fatalf("claim query lacks row lock: %s", got)
	}
}

func TestPostgresSQLReplacesSQLiteRowIDOrdering(t *testing.T) {
	got := postgresSQL("SELECT id FROM run_steps WHERE run_id=? ORDER BY rowid")
	if strings.Contains(got, "rowid") || !strings.Contains(got, "ORDER BY started_at, id") {
		t.Fatalf("rowid ordering was not translated: %s", got)
	}
}

func TestPostgresSQLCastsJSONForApprovalLookup(t *testing.T) {
	got := postgresSQL("SELECT id FROM approvals WHERE selected_action_ids LIKE ?")
	if !strings.Contains(got, "selected_action_ids::text LIKE $1") {
		t.Fatalf("json lookup was not translated: %s", got)
	}
}
